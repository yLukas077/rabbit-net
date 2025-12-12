# Sistema Distribuído de Votação em Tempo Real com RabbitMQ (Publish/Subscribe)

Este projeto implementa um sistema distribuído cliente-servidor para votação em tempo real utilizando mensageria assíncrona via RabbitMQ, empregando os padrões Direct Exchange e Fanout Exchange.  
O sistema substitui o uso tradicional de sockets TCP por uma abordagem moderna baseada em broker de mensagens, permitindo alta escalabilidade, desacoplamento e confiabilidade na comunicação entre clientes e servidor.

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
- Biblioteca AMQP para Go:  
  `github.com/rabbitmq/amqp091-go`

---

## 3. Estrutura do Projeto

```
votacao-rabbitmq/
│
├── docker-compose.yml          # Inicialização do RabbitMQ
│
├── server/
│   ├── main.go                 # Lógica do servidor
│   └── go.mod
│
├── client/
│   ├── main.go                 # Cliente interativo
│   └── go.mod
│
└── loadtest/
    ├── main.go                 # Teste de carga
    └── go.mod
```

---

## 4. Como Executar o Sistema

### 4.1. Inicializar o RabbitMQ

```bash
docker compose up -d
```

Verificar se o container está em execução:

```bash
docker ps
```

Acessar o painel administrativo:

```
http://localhost:15672
usuário: admin
senha: admin
```

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

O diretório `loadtest/` contém um simulador de múltiplos clientes enviando votos simultâneos.

A constante abaixo controla o número de clientes simulados:

```go
const totalClients = 500
```

---

## 6. Funcionamento Interno do Sistema

### 6.1. Fluxo de Execução do Servidor

1. O servidor inicia e declara duas exchanges.  
2. Vincula a fila `votos` à exchange `votacao.votos`.  
3. Consome mensagens de votos enviadas pelos clientes.  
4. Para cada voto:
   - Verifica duplicação.  
   - Valida a opção.  
   - Registra o voto.  
   - Publica confirmação ao autor.  
   - Envia parcial para todos os clientes.
5. Após o timeout configurado, publica o resultado final e finaliza.

---

### 6.2. Fluxo de Execução do Cliente

1. Solicita um ID ao usuário.  
2. Conecta ao RabbitMQ.  
3. Cria uma fila exclusiva para receber broadcast.  
4. Associa essa fila à exchange `votacao.broadcast`.  
5. Consume mensagens em segundo plano.  
6. Envia o voto para a exchange `votacao.votos`.  
7. Permanece aguardando confirmações, parciais e o resultado final.

---

## 7. Tipos de Mensagens Utilizadas

### 7.1. Voto enviado pelo cliente

```json
{
  "userId": "usuario123",
  "opcao": "A"
}
```

### 7.2. Mensagens enviadas pelo servidor

**Confirmação**
```json
{
  "tipo": "confirmacao",
  "userId": "usuario123",
  "mensagem": "Voto registrado com sucesso!"
}
```

**Erro**
```json
{
  "tipo": "erro",
  "userId": "usuario123",
  "mensagem": "Você já votou"
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

## 8. Conclusão

O sistema demonstra o funcionamento de um ambiente distribuído utilizando mensageria assíncrona, suportando múltiplos clientes simultâneos, controle de votação, distribuição de resultados em tempo real e execução eficiente sob carga.

A abordagem via RabbitMQ confere escalabilidade, desacoplamento, tolerância a falhas e robustez ao sistema.