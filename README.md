# Sistema Distribuído de Votação em Tempo Real com RabbitMQ (Publish/Subscribe)

Este projeto implementa um sistema distribuído cliente-servidor para votação em tempo real utilizando mensageria assíncrona via RabbitMQ, empregando os padrões **Direct Exchange** e **Fanout Exchange**.

O sistema substitui o uso tradicional de sockets TCP diretos por uma abordagem moderna baseada em broker de mensagens, permitindo alta escalabilidade, desacoplamento e confiabilidade na comunicação entre clientes e servidor.

---

## 1. Arquitetura Geral

A comunicação ocorre por meio de duas exchanges:

1. **votacao.votos** (tipo: direct)
   - Os clientes enviam seus votos para esta exchange.
   - O servidor consome da fila vinculada a ela.

2. **votacao.broadcast** (tipo: fanout)
   - O servidor publica confirmações, mensagens de erro, resultados parciais e resultado final.
   - Todos os clientes recebem automaticamente as mensagens.

Cada cliente cria uma fila exclusiva e temporária, permitindo que receba mensagens do broadcast sem gerar conflitos com outros usuários.

---

## 2. Tecnologias Utilizadas

- Linguagem Go 1.22  
- RabbitMQ (imagem oficial 3-management)  
- Docker e Docker Compose  
- Biblioteca AMQP para Go: `github.com/rabbitmq/amqp091-go`  
- **Conceitos-chave**: Goroutines, Channels, Mutex, TCP Sockets, Multiplexing  

---

## 3. Estrutura do Projeto

```

votacao-rabbitmq/
│
├── docker-compose.yml         # Inicialização do RabbitMQ
│
├── server/
│   ├── main.go                # Servidor com Worker Pool
│   └── go.mod
│
├── client/
│   ├── main.go                # Cliente interativo
│   └── go.mod
│
└── loadtest/
├── main.go                # Teste de carga com Connection Pooling
└── go.mod

````

---

## 4. Como Executar o Sistema

### 4.1. Inicializar o RabbitMQ

```bash
docker compose up -d
````

Verificar se o container está em execução:

```bash
docker ps
```

Acessar o painel administrativo:

* URL: `http://localhost:15672`
* Usuário: `admin`
* Senha: `admin`

---

### 4.2. Executar o Servidor

```bash
cd server
go mod tidy
go run main.go
```

---

### 4.3. Executar um Cliente

```bash
cd client
go mod tidy
go run main.go
```

---

### 4.4. Executar o Teste de Carga

```bash
cd loadtest
go mod tidy
go run main.go
```

---

## 5. Teste de Carga

O diretório `loadtest/` contém um simulador robusto de múltiplos clientes enviando votos simultâneos.

Você pode ajustar a constante no código para definir a carga:

```go
const totalClients = 20000 // Exemplo para 20 mil conexões
```

---

## 6. Desafios de Escala e Otimizações de Performance

Durante o desenvolvimento, identificamos gargalos críticos ao simular alta concorrência e implementamos soluções arquiteturais para superá-los.

### 6.1. Otimização do Cliente (Load Test): De TCP Storm para Connection Pooling

**O Problema (TCP Storm):**
Inicialmente, criar uma conexão TCP (`amqp.Dial`) para cada cliente simulado causou esgotamento de sockets no sistema operacional (erro `connectex`), falhando com apenas **500 clientes**.

**O Problema (Channel Exhaustion):**
Ao tentar compartilhar uma única conexão para todos os clientes, atingimos o limite do protocolo AMQP de **2047 canais por conexão** (erro `channel id space exhausted`).

**A Solução (Connection Pooling):**
Implementamos um gerenciador inteligente de conexões que simula o comportamento de um Backend/API Gateway real:

* **Sockets TCP:** Calculamos dinamicamente o número de conexões necessárias (1 conexão física a cada 1000 clientes virtuais).
* **Multiplexing:** Utilizamos múltiplos canais AMQP leves dentro de cada conexão TCP pesada.
* **Resultado:** Capacidade de escalar para dezenas de milhares de clientes sem estourar limites do SO ou do RabbitMQ.

---

### 6.2. Otimização do Servidor: Worker Pools e Concorrência

**O Problema:**
O processamento serial (um voto por vez) gerava latência acumulada em cargas altas, atrasando o broadcast de resultados.

**A Solução:**
Implementamos um padrão de **Worker Pool** com **20 goroutines** processando votos simultaneamente via *Round-Robin*.

* **Thread Safety:** Utilizamos `sync.Mutex` para proteger o mapa de votos e o canal de publicação, garantindo integridade dos dados sem condições de corrida (Race Conditions).
* **Eficiência:** O servidor agora processa múltiplos votos e envia broadcasts em paralelo.

---

### 6.3. Resultados Finais

Após as otimizações, o sistema foi submetido a um teste de estresse em uma máquina de recursos modestos:

| Métrica          | Antes (TCP Storm / Serial) | Depois (Pooling / Workers) |
| ---------------- | -------------------------- | -------------------------- |
| **Votos Totais** | Falha em ~500              | **20.000** (Sucesso)       |
| **Tempo Total**  | N/A (Erro)                 | **< 20 segundos**          |
| **Throughput**   | Baixo                      | **> 1.000 req/s**          |
| **Estabilidade** | Erros de Socket/Canal      | Zero erros                 |

---

## 7. Funcionamento Interno do Sistema

### 7.1. Fluxo de Execução do Servidor

1. O servidor inicia e declara duas exchanges.
2. Vincula a fila `votos` à exchange `votacao.votos`.
3. Inicia um **Worker Pool** (ex.: 20 goroutines) consumindo da mesma fila.
4. Para cada voto recebido por um worker:

   * Bloqueia recurso (Mutex), valida e registra o voto.
   * Cria snapshot do resultado parcial.
   * Desbloqueia recurso.
   * Publica confirmação e parcial via broadcast.
5. Após o timeout, publica o resultado final e finaliza.

---

### 7.2. Fluxo de Execução do Cliente

1. Solicita um ID ao usuário.
2. Conecta ao RabbitMQ.
3. Cria uma fila exclusiva para receber broadcast.
4. Associa essa fila à exchange `votacao.broadcast`.
5. Consome mensagens em segundo plano.
6. Envia o voto para a exchange `votacao.votos`.
7. Permanece aguardando confirmações, parciais e o resultado final.

---

## 8. Tipos de Mensagens Utilizadas

### 8.1. Voto enviado pelo cliente

```json
{
  "userId": "usuario123",
  "opcao": "A"
}
```

### 8.2. Mensagens enviadas pelo servidor

**Confirmação**

```json
{
  "tipo": "confirmacao",
  "userId": "usuario123",
  "mensagem": "Voto registrado com sucesso."
}
```

**Erro**

```json
{
  "tipo": "erro",
  "userId": "usuario123",
  "mensagem": "Você já votou."
}
```

**Parcial**

```json
{
  "tipo": "parcial",
  "resultado": { "A": 3, "B": 5, "C": 1 }
}
```

**Resultado final**

```json
{
  "tipo": "final",
  "resultado": { "A": 10, "B": 13, "C": 4 }
}
```

---

## 9. Conclusão

O sistema demonstra o funcionamento de um ambiente distribuído utilizando mensageria assíncrona, suportando múltiplos clientes simultâneos, controle de votação, distribuição de resultados em tempo real e execução eficiente sob carga.

A abordagem via RabbitMQ, aliada às otimizações de **Connection Pooling** e **Worker Pools** em Go, confere escalabilidade, desacoplamento, tolerância a falhas e robustez ao sistema.