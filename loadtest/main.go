package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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
	const totalClients = 500

	start := time.Now()
	var wg sync.WaitGroup

	fmt.Printf("Iniciando teste de carga com %d clientes simultâneos.\n", totalClients)

	for i := 1; i <= totalClients; i++ {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			// Cada cliente estabelece sua própria conexão com o RabbitMQ.
			conn, err := amqp.Dial("amqp://admin:admin@localhost:5672/")
			if err != nil {
				log.Printf("Falha ao conectar (cliente %d): %v\n", id, err)
				return
			}
			defer conn.Close()

			// Cada cliente cria seu próprio canal.
			ch, err := conn.Channel()
			if err != nil {
				log.Printf("Falha ao abrir canal (cliente %d): %v\n", id, err)
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

	// Calcula o tempo total gasto no teste.
	duration := time.Since(start)
	fmt.Printf("Teste concluído. %d votos enviados em %v.\n", totalClients, duration)
}