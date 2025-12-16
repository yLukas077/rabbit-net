package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Estrutura que representa o voto enviado por um cliente simulado.
type Voto struct {
	UserID string `json:"userId"`
	Option string `json:"opcao"`
}

func main() {
	// Quantidade de clientes simultâneos simulados.
	const totalClients = 20000

	// Limite seguro de canais por conexão (RabbitMQ padrão aceita 2047, ocupando o 0 para controle interno, então sobram 2026 canais, o que foi testado e comprovado, logo vamos usar 1000 para segurança)
	const clientsPerConnection = 1000

	start := time.Now()
	var wg sync.WaitGroup

	fmt.Printf("Iniciando teste de carga com %d clientes simultâneos.\n", totalClients)

	// 1. Calcula quantas conexões TCP reais precisamos abrir
	numConnections := int(math.Ceil(float64(totalClients) / float64(clientsPerConnection)))
	conns := make([]*amqp.Connection, numConnections)

	fmt.Printf("Abrindo %d conexões TCP para distribuir a carga...\n", numConnections)

	// 2. Abre o Pool de Conexões
	for i := 0; i < numConnections; i++ {
		c, err := amqp.Dial("amqp://admin:admin@localhost:5672/")
		if err != nil {
			log.Fatalf("Falha ao abrir conexão %d: %v", i, err)
		}
		conns[i] = c
		defer c.Close()
	}

	for i := 1; i <= totalClients; i++ {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			// 3. Round-Robin ( que é um algoritmo padrão para distribuir carga ): Distribui o cliente para uma das conexões abertas
			connIndex := id % numConnections
			selectedConn := conns[connIndex]

			// Cria o canal leve dentro da conexão selecionada
			ch, err := selectedConn.Channel()
			if err != nil {
				// Se falhar aqui, é provável que atingiu o limite daquela conexão específica
				log.Printf("Erro crítico ao criar canal (cliente %d na conn %d): %v", id, connIndex, err)
				return
			}
			defer ch.Close()

			// Monta o JSON de voto.
			body, _ := json.Marshal(Voto{
				UserID: fmt.Sprintf("loadtest_%d", id),
				Option: "A",
			})

			// Envia o voto utilizando PublishWithContext, que é a API moderna.
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			err = ch.PublishWithContext(
				ctx,
				"votacao.votos", // Exchange de votos.
				"voto",          // Routing key.
				false,
				false,
				amqp.Publishing{
					ContentType: "application/json",
					Body:        body,
				},
			)

			if err != nil {
				log.Printf("Falha ao enviar voto (cliente %d): %v\n", id, err)
				return
			}
		}(i)
	}

	// Aguarda todos os clientes terminarem.
	wg.Wait()
	duration := time.Since(start)

	// Estatísticas finais
	reqPerSec := float64(totalClients) / duration.Seconds()
	fmt.Printf("Teste concluído.\n")
	fmt.Printf("Total: %d votos\nTempo: %v\nPerformance: %.2f req/s\n", totalClients, duration, reqPerSec)
}
