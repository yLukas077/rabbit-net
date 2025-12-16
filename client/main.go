package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Estrutura enviada pelo cliente ao servidor contendo ID e voto.
type Voto struct {
	UserID string `json:"userId"`
	Option string `json:"opcao"`
}

// Estrutura usada para receber mensagens de broadcast do servidor.
type BroadcastMsg struct {
	Tipo     string         `json:"tipo"`
	Mensagem string         `json:"mensagem,omitempty"`
	UserID   string         `json:"userId,omitempty"`
	Result   map[string]int `json:"resultado,omitempty"`
}

func main() {
	reader := bufio.NewReader(os.Stdin)

	// Loop que garante que o usuário informe um ID válido.
	var id string
	// Estado local do cliente (thread-safe)
	var jaVotou atomic.Bool

	for {
		fmt.Print("Digite seu ID único ou seu Nome: ")
		raw, _ := reader.ReadString('\n')
		id = strings.TrimSpace(raw)

		if id != "" {
			break
		}

		fmt.Println("O ID não pode ser vazio. Tente novamente.")
	}

	// Conexão com RabbitMQ.
	conn, err := amqp.Dial("amqp://admin:admin@localhost:5672/")
	if err != nil {
		log.Fatalf("Erro ao conectar com RabbitMQ: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Erro ao abrir canal: %v", err)
	}
	defer ch.Close()

	// Fila exclusiva para receber mensagens de broadcast.
	q, err := ch.QueueDeclare(
		"",
		false,
		true,
		true,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("Erro ao declarar fila: %v", err)
	}

	err = ch.QueueBind(
		q.Name,
		"",
		"votacao.broadcast",
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("Erro ao associar fila à exchange: %v", err)
	}

	msgs, err := ch.Consume(
		q.Name,
		"",
		true,
		true,
		false,
		false,
		nil,
	)

	if err != nil {
		log.Fatalf("Erro ao iniciar consumo de mensagens: %v", err)
	}

	// Goroutine que trata mensagens vindas do servidor.
	go func() {
		for m := range msgs {
			var msg BroadcastMsg
			json.Unmarshal(m.Body, &msg)

			switch msg.Tipo {

			case "confirmacao":
				if msg.UserID == id {
					jaVotou.Store(true)
					fmt.Printf("\nConfirmação: %s\n", msg.Mensagem)
				}

			case "erro":
				if msg.UserID == id {
					fmt.Printf("\nErro: %s\n", msg.Mensagem)
				}

			case "parcial":
				fmt.Println("\nParcial da votação:")
				for op, val := range msg.Result {
					fmt.Printf("  %s: %d votos\n", op, val)
				}

				if !jaVotou.Load() {
					fmt.Println("\nOpções de voto: A, B, C")
					fmt.Print("Digite sua opção: ")
				}

			case "final":
				fmt.Println("\nResultado final da votação:")
				for op, val := range msg.Result {
					fmt.Printf("  %s: %d votos\n", op, val)
				}
				fmt.Println("\nEncerrando cliente.")
				os.Exit(0)
			}
		}
	}()

	// Loop de validação do voto.
	var op string
	for {
		fmt.Println("\nOpções de voto: A, B, C")
		fmt.Print("Digite sua opção: ")

		raw, _ := reader.ReadString('\n')
		op = strings.ToUpper(strings.TrimSpace(raw))

		if op == "A" || op == "B" || op == "C" {
			break
		}

		fmt.Println("Opção inválida. Tente novamente.")
	}

	// Monta o JSON do voto.
	v := Voto{
		UserID: id,
		Option: op,
	}
	body, _ := json.Marshal(v)

	// Envio do voto usando PublishWithContext.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = ch.PublishWithContext(
		ctx,
		"votacao.votos",
		"voto",
		false,
		false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        body,
		},
	)

	if err != nil {
		log.Fatalf("Erro ao enviar voto: %v", err)
	}

	fmt.Println("\nVoto enviado. Aguardando confirmação e atualizações do servidor...\n")

	// Bloqueia tentativas de enviar voto novamente.
	// O usuário pode digitar, mas nunca enviará outro voto.
	go func() {
		for {
			_, _ = reader.ReadString('\n')
			fmt.Println("Voto duplicado não é permitido. Você já participou desta votação.")
		}
	}()

	// Mantém o cliente ativo para receber mensagens.
	select {}
}
