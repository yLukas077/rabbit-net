package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"sync"
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

// Mutex para proteger o Canal AMQP (Publish não é thread-safe).
var amqpMu sync.Mutex

// Mutex para proteger os mapas de votos e contagem.
var stateMu sync.Mutex

func main() {

	// Tempo limite da votação.
	timeout := 180 * time.Second
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
	// OBS: Qos (Quality of Service) ajuda a distribuir melhor as mensagens entre workers
	ch.Qos(50, 0, false)
	msgs, err := ch.Consume(q.Name, "", true, false, false, false, nil)
	if err != nil {
		log.Fatalf("Erro ao consumir fila de votos: %v", err)
	}

	log.Println("Servidor de votação iniciado com Worker Pool.")
	log.Printf("Tempo máximo de votação: %v\n", timeout)

	// Armazenamento interno dos votos.
	votos := map[string]string{}
	contagem := map[string]int{"A": 0, "B": 0, "C": 0}

	// Timer que encerra a votação automaticamente.
	go func() {
		time.Sleep(timeout)
		log.Println("Encerrando votação por timeout.")

		// Proteção ao ler o estado final
		stateMu.Lock()
		finalResult := copiaMapa(contagem)
		stateMu.Unlock()

		enviarFinal(ch, finalResult)
		os.Exit(0)
	}()

	// Configuração do Worker Pool
	const numWorkers = 20
	var wg sync.WaitGroup

	log.Printf("Iniciando %d workers...", numWorkers)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			// Loop principal do worker: processa mensagens concorrentemente
			for msg := range msgs {
				var v Voto

				// Converte o JSON recebido.
				if err := json.Unmarshal(msg.Body, &v); err != nil {
					log.Printf("[Worker %d] Erro ao interpretar voto: %v\n", workerID, err)
					continue
				}

				// Acesso à memória compartilhada
				stateMu.Lock()

				// Impede voto duplicado.
				if _, exists := votos[v.UserID]; exists {
					stateMu.Unlock() // Liberando a trava antes de enviar rede
					enviarErro(ch, v.UserID, "Você já votou.")
					continue
				}

				// Validação da opção.
				if v.Option != "A" && v.Option != "B" && v.Option != "C" {
					stateMu.Unlock()
					enviarErro(ch, v.UserID, "Opção inválida.")
					continue
				}

				// Registrando voto.
				votos[v.UserID] = v.Option
				contagem[v.Option]++

				// Cria snapshot do resultado para enviar fora do Lock
				resultadoAtual := copiaMapa(contagem)

				stateMu.Unlock()

				log.Printf("[Worker %d] Voto recebido: %s -> %s\n", workerID, v.UserID, v.Option)

				enviarConfirmacao(ch, v.UserID)
				enviarParcial(ch, resultadoAtual)
			}
		}(i)
	}

	// Aguarda os workers
	wg.Wait()
}

//
// Funções auxiliares
//

// Cria uma cópia segura do mapa para evitar Data Race durante JSON Marshal
func copiaMapa(original map[string]int) map[string]int {
	novo := make(map[string]int, len(original))
	for k, v := range original {
		novo[k] = v
	}
	return novo
}

// Função geral de envio de mensagens JSON para a exchange de broadcast.
func publishJSON(ch *amqp.Channel, msg BroadcastMsg) {
	// Proteção: O canal AMQP não é thread-safe para publish concorrente
	amqpMu.Lock()
	defer amqpMu.Unlock()

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
