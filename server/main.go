package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

//
// Estruturas de mensagens trocadas entre clientes e servidor.
//

// Estrutura de voto enviada pelos clientes.
type Voto struct {
	UserID string `json:"userId"`
	Option string `json:"opcao"`
}

// Estrutura usada pelo servidor para enviar confirmações, erros,
// parciais e o resultado final.
type BroadcastMsg struct {
	Tipo     string         `json:"tipo"`
	UserID   string         `json:"userId,omitempty"`
	Mensagem string         `json:"mensagem,omitempty"`
	Result   map[string]int `json:"resultado,omitempty"`
}

func main() {

	// Tempo limite da votação. Pode ser sobrescrito pela variável de ambiente.
	timeout := 30 * time.Second
	if v := os.Getenv("VOTING_TIMEOUT"); v != "" {
		if t, err := time.ParseDuration(v); err == nil {
			timeout = t
		}
	}

	// Conexão com RabbitMQ.
	conn, err := amqp.Dial("amqp://admin:admin@localhost:5672/")
	if err != nil {
		log.Fatalf("Erro ao conectar no RabbitMQ: %v", err)
	}
	defer conn.Close()

	// Canal de comunicação.
	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Erro ao criar canal: %v", err)
	}
	defer ch.Close()

	// Declaração das exchanges utilizadas pelo sistema.
	// Direct para votos, Fanout para broadcast.
	ch.ExchangeDeclare("votacao.votos", "direct", true, false, false, false, nil)
	ch.ExchangeDeclare("votacao.broadcast", "fanout", true, false, false, false, nil)

	// Fila que recebe todos os votos dos clientes.
	q, _ := ch.QueueDeclare("votos", true, false, false, false, nil)
	ch.QueueBind(q.Name, "voto", "votacao.votos", false, nil)

	// Inicia consumo da fila de votos.
	msgs, err := ch.Consume(q.Name, "", true, false, false, false, nil)
	if err != nil {
		log.Fatalf("Erro ao consumir fila de votos: %v", err)
	}

	log.Println("Servidor de votação iniciado.")
	log.Printf("Tempo máximo de votação: %v\n", timeout)

	// Armazenamento interno dos votos.
	votos := map[string]string{}
	contagem := map[string]int{"A": 0, "B": 0, "C": 0}

	// Timer que encerra a votação automaticamente.
	go func() {
		time.Sleep(timeout)
		log.Println("Encerrando votação por timeout.")
		enviarFinal(ch, contagem)
		os.Exit(0)
	}()

	// Loop principal: processa cada voto recebido.
	for msg := range msgs {
		var v Voto

		// Converte o JSON recebido.
		if err := json.Unmarshal(msg.Body, &v); err != nil {
			log.Printf("Erro ao interpretar voto: %v\n", err)
			continue
		}

		log.Printf("Voto recebido: %s votou em %s\n", v.UserID, v.Option)

		// Impede voto duplicado.
		if _, exists := votos[v.UserID]; exists {
			enviarErro(ch, v.UserID, "Você já votou.")
			continue
		}

		// Validação da opção.
		if v.Option != "A" && v.Option != "B" && v.Option != "C" {
			enviarErro(ch, v.UserID, "Opção inválida.")
			continue
		}

		// Registrando voto.
		votos[v.UserID] = v.Option
		contagem[v.Option]++

		enviarConfirmacao(ch, v.UserID)
		enviarParcial(ch, contagem)
	}
}

//
// Funções auxiliares responsáveis por envio de mensagens de broadcast.
//


// Função geral de envio de mensagens JSON para a exchange de broadcast.
func publishJSON(ch *amqp.Channel, msg BroadcastMsg) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	body, _ := json.Marshal(msg)

	ch.PublishWithContext(
		ctx,
		"votacao.broadcast", // Exchange fanout.
		"",
		false,
		false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        body,
		},
	)
}

func enviarConfirmacao(ch *amqp.Channel, user string) {
	publishJSON(ch, BroadcastMsg{
		Tipo:     "confirmacao",
		UserID:   user,
		Mensagem: "Voto registrado com sucesso.",
	})
}

func enviarErro(ch *amqp.Channel, user, texto string) {
	publishJSON(ch, BroadcastMsg{
		Tipo:     "erro",
		UserID:   user,
		Mensagem: texto,
	})
}

func enviarParcial(ch *amqp.Channel, res map[string]int) {
	publishJSON(ch, BroadcastMsg{
		Tipo:   "parcial",
		Result: res,
	})
}

func enviarFinal(ch *amqp.Channel, res map[string]int) {
	publishJSON(ch, BroadcastMsg{
		Tipo:   "final",
		Result: res,
	})
	log.Println("Resultado final enviado a todos os clientes.")
}
