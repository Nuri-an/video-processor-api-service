# Video Processor API

API Gateway responsável por autenticar usuários, receber vídeos, registrar tarefas, armazenar arquivos em Object Storage, consultar o status do processamento e disponibilizar os arquivos processados.

O processamento dos vídeos é executado pelo repositório `video-processor-worker`.

## Responsabilidades

- autenticar usuários com tokens JWT;
- receber vídeos em `POST /upload`;
- armazenar vídeos no Object Storage compatível com S3;
- registrar jobs no PostgreSQL;
- publicar jobs na fila `video_jobs` do RabbitMQ;
- manter uma outbox transacional para garantir a publicação dos jobs;
- consultar o status dos jobs por usuário;
- disponibilizar os arquivos processados;
- disponibilizar o Swagger da API;
- registrar logs da aplicação no MongoDB.

Quando o worker falhar no processamento do vídeo, no FFmpeg ou na geração do ZIP, ele atualiza o job para `Erro` com a mensagem da falha e pode notificar o usuário por e-mail.

O consumo da fila e o processamento dos vídeos continuam no repositório `video-processor-worker`.

## Arquitetura

```text
                    ┌─────────────────┐
                    │     Cliente     │
                    └────────┬────────┘
                             │ HTTP
                             ▼
                    ┌─────────────────┐
                    │   API Gateway   │
                    │      :8080      │
                    └────┬────┬───┬───┘
                         │    │   │
             ┌───────────┘    │   └──────────────┐
             ▼                ▼                  ▼
      ┌────────────┐   ┌────────────┐     ┌────────────┐
      │ PostgreSQL │   │  RabbitMQ  │     │   MongoDB  │
      │    Jobs    │   │ video_jobs │     │    Logs    │
      └────────────┘   └─────┬──────┘     └────────────┘
                              │
                              ▼
                     ┌─────────────────┐
                     │      Worker     │
                     │ FFmpeg + frames │
                     └────────┬────────┘
                              │
                              ▼
                     ┌─────────────────┐
                     │   SeaweedFS     │
                     │   S3 API :8333  │
                     └─────────────────┘
```

### Fluxo de upload

1. O cliente autentica na API e recebe um JWT.
2. O cliente envia o vídeo para `POST /upload`.
3. A API armazena o vídeo no bucket S3.
4. A API registra o job no PostgreSQL.
5. O job é registrado na outbox transacional.
6. O dispatcher publica o job no RabbitMQ.
7. O worker consome o job.
8. O worker baixa o vídeo do Object Storage.
9. O FFmpeg extrai os frames.
10. O worker gera o ZIP.
11. O ZIP é armazenado novamente no Object Storage.
12. O worker atualiza o job para `Concluido`.
13. O cliente pode baixar o ZIP pela API.

## Storage

O projeto utiliza um Object Storage compatível com a API S3.

No ambiente Docker local, é utilizado o **SeaweedFS**.

### Bucket

```text
video-processor
```

### Estrutura dos objetos

Vídeos recebidos:

```text
videos/<job_id>_<nome_original>
```

Exemplo:

```text
videos/1790445249382523719_Aula errada.mp4
```

Arquivos processados:

```text
outputs/frames_<job_id>.zip
```

Exemplo:

```text
outputs/frames_1790445249382523719.zip
```

O cliente não precisa conhecer a chave física do objeto. Para download, utiliza:

```text
GET /download/frames_<job_id>.zip
```

A API valida se o job pertence ao usuário autenticado e internamente busca o objeto em:

```text
outputs/frames_<job_id>.zip
```

## Rotas

| Método | Rota | Descrição |
|---|---|---|
| `GET` | `/` | Verifica se a API está disponível |
| `GET` | `/swagger` | Swagger UI |
| `GET` | `/swagger/openapi.yaml` | Especificação OpenAPI |
| `POST` | `/auth/login` | Autentica um usuário |
| `POST` | `/upload` | Recebe o vídeo no campo `video` |
| `GET` | `/api/status` | Lista os jobs do usuário autenticado |
| `GET` | `/download/{filename}` | Baixa um ZIP processado |

As rotas de upload, status e download exigem:

```text
Authorization: Bearer <token>
```

## Autenticação

Obtenha o token enviando as credenciais para `POST /auth/login`.

Os usuários são persistidos no PostgreSQL e as senhas são armazenadas somente como hashes bcrypt.

O usuário seed padrão `admin` é criado a partir de `API_PASSWORD` e `API_USER_EMAIL`.

`API_USER` pode ser configurado para utilizar outro nome no seed.

O claim `email` do JWT é enviado junto com cada job.

Exemplo:

```bash
curl -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"user":"admin","password":"<senha-configurada>"}'
```

A especificação da API está em [`docs/openapi.yaml`](docs/openapi.yaml).

## Dependências

| Serviço | Utilização |
|---|---|
| PostgreSQL | Usuários, jobs, status e outbox |
| RabbitMQ | Fila `video_jobs` e processamento assíncrono |
| MongoDB | Logs da aplicação |
| SeaweedFS | Object Storage compatível com S3 |
| Mailpit | SMTP local para notificações por e-mail |

## Variáveis de ambiente

### API

| Variável | Padrão | Uso |
|---|---|---|
| `API_USER` | `admin` | Usuário seed da API |
| `API_PASSWORD` | vazio | Senha usada para gerar o hash do usuário seed |
| `API_USER_EMAIL` | vazio | E-mail do usuário |
| `JWT_SECRET` | vazio | Chave utilizada para assinar tokens JWT |

### Object Storage / S3

| Variável | Padrão | Uso |
|---|---|---|
| `S3_ENDPOINT` | `http://localhost:9000` | Endpoint da API S3 |
| `S3_BUCKET` | `video-processor` | Bucket utilizado pela aplicação |
| `S3_ACCESS_KEY` | `minio` | Access key do Object Storage |
| `S3_SECRET_KEY` | `minio123` | Secret key do Object Storage |
| `S3_REGION` | `us-east-1` | Região utilizada na assinatura S3 |

No Docker Compose local:

```text
S3_ENDPOINT=http://s3:8333
```

O serviço `s3` corresponde ao container SeaweedFS.

### PostgreSQL

| Variável | Padrão | Uso |
|---|---|---|
| `POSTGRES_DSN` | vazio | DSN completo opcional |
| `POSTGRES_HOST` | `localhost` | Host do PostgreSQL |
| `POSTGRES_PORT` | `5432` | Porta do PostgreSQL |
| `POSTGRES_USER` | `video_processor` | Usuário do PostgreSQL |
| `POSTGRES_PASSWORD` | vazio | Senha do PostgreSQL |
| `POSTGRES_DB` | `video_processor` | Banco utilizado |

### RabbitMQ

| Variável | Padrão | Uso |
|---|---|---|
| `RABBITMQ_URL` | vazio | URL completa opcional |
| `RABBITMQ_HOST` | `localhost` | Host do RabbitMQ |
| `RABBITMQ_PORT` | `5672` | Porta do RabbitMQ |
| `RABBITMQ_USER` | vazio | Usuário do RabbitMQ |
| `RABBITMQ_PASSWORD` | vazio | Senha do RabbitMQ |
| `RABBITMQ_QUEUE` | `video_jobs` | Fila de jobs |
| `RABBITMQ_DLX` | `video_jobs.dlx` | Exchange de dead-letter |
| `RABBITMQ_CONFIRM_TIMEOUT` | `5s` | Timeout dos publisher confirms |

> Em ambientes com TLS habilitado no RabbitMQ, a URL pode utilizar `amqps://` e a porta correspondente ao listener TLS.

### MongoDB

| Variável | Padrão | Uso |
|---|---|---|
| `MONGO_URI` | `mongodb://localhost:27017` | Conexão com MongoDB |
| `MONGO_DATABASE` | `video_processor_logs` | Banco dos logs |
| `MONGO_COLLECTION` | `application_logs` | Collection dos logs |

### SMTP

| Variável | Padrão | Uso |
|---|---|---|
| `SMTP_HOST` | `localhost` | Host do servidor SMTP |
| `SMTP_PORT` | `25` | Porta SMTP |
| `SMTP_USERNAME` | vazio | Usuário SMTP |
| `SMTP_PASSWORD` | vazio | Senha SMTP |
| `SMTP_FROM` | `noreply@video-processor.local` | Remetente das notificações |

No ambiente Docker local, o projeto utiliza o Mailpit.

## Execução local

Para executar diretamente no host, é necessário ter Go instalado e as dependências PostgreSQL, RabbitMQ, MongoDB e um Object Storage S3 compatível disponíveis.

```bash
go mod tidy
go run .
```

A API ficará disponível em:

```text
http://localhost:8080
```

Swagger:

```text
http://localhost:8080/swagger
```

## Docker Compose

O ambiente recomendado para desenvolvimento utiliza Docker Compose.

Configure os segredos localmente:

```bash
cp .env.example .env
```

Edite `.env` e preencha os valores necessários, como:

```text
API_PASSWORD
JWT_SECRET
POSTGRES_PASSWORD
RABBITMQ_USER
RABBITMQ_PASSWORD
```

Suba a API e suas dependências:

```bash
docker compose up -d --build
```

Verifique os containers:

```bash
docker compose ps
```

A API:

```text
http://localhost:8080
```

Swagger:

```text
http://localhost:8080/swagger
```

SeaweedFS S3:

```text
http://localhost:8333
```

O endpoint `8333` é a API S3 e não uma interface web de gerenciamento. Uma requisição HTTP direta ao `/` pode retornar `AccessDenied`, o que é esperado sem uma requisição S3 autenticada.

Para verificar os objetos armazenados:

```bash
docker run --rm \
  --network video-processor \
  -e AWS_ACCESS_KEY_ID=minio \
  -e AWS_SECRET_ACCESS_KEY=minio123 \
  amazon/aws-cli \
  --endpoint-url http://s3:8333 \
  s3 ls s3://video-processor/ --recursive
```

Exemplo:

```text
2026-09-26 17:57:40    8438051 outputs/frames_1790445249382523719.zip
2026-09-26 17:54:09   11916170 videos/1790445249382523719_Aula errada.mp4
```

### Rede Docker

A API e o worker utilizam a rede Docker compartilhada:

```text
video-processor
```

Isso permite que os serviços sejam acessados pelo nome do container/serviço:

```text
postgres:5432
rabbitmq:5672
mongo:27017
s3:8333
```

O worker possui seu próprio `docker-compose.yml`, mas utiliza a rede externa `video-processor`.

Por isso, a API deve ser iniciada antes do worker:

```bash
docker compose up -d --build
```

Depois, no projeto do worker:

```bash
docker compose up -d --build
```

O worker não utiliza `depends_on` para serviços definidos no Compose da API.

## Outbox transacional

Os jobs são gravados em uma outbox transacional antes da resposta do upload.

Um dispatcher verifica a outbox e publica as mensagens no RabbitMQ.

A mensagem somente é marcada como enviada após o publisher confirm do RabbitMQ.

Isso evita perder um job entre o registro no PostgreSQL e a publicação na fila.

## Testar upload

Primeiro obtenha um JWT:

```bash
curl -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"user":"admin","password":"<senha-configurada>"}'
```

Depois faça o upload:

```bash
curl -X POST http://localhost:8080/upload \
  -H 'Authorization: Bearer <token>' \
  -F 'video=@./video.mp4'
```

Consultar status:

```bash
curl http://localhost:8080/api/status \
  -H 'Authorization: Bearer <token>'
```

Quando o job estiver `Concluido`, o ZIP poderá ser baixado utilizando o nome retornado pelo processamento:

```bash
curl -L \
  http://localhost:8080/download/frames_<job_id>.zip \
  -H 'Authorization: Bearer <token>' \
  -o frames.zip
```

O objeto correspondente no S3 estará em:

```text
outputs/frames_<job_id>.zip
```

## Tratamento de erros

Quando o processamento falha, o worker atualiza o job para `Erro` e registra a mensagem da falha.

Exemplo:

```go
if err := processVideo(job); err != nil {
    message := err.Error()
    _ = jobs.UpdateStatus(job.ID, "Erro", message)
    _ = notifier.NotifyProcessingError(job.User, job, err)
    return
}

_ = jobs.UpdateStatus(job.ID, "Concluido", "")
```

O processamento do vídeo e a geração do ZIP são responsabilidades do repositório `video-processor-worker`.

## Kubernetes

Os manifests para Kubernetes estão em [`k8s/`](k8s/).

Eles incluem os componentes necessários para a execução da aplicação, como:

- API;
- PostgreSQL;
- RabbitMQ;
- MongoDB;
- Mailpit;
- PVCs;
- Services;
- PDB;
- HPA da API.

Para executar localmente ou em produção, consulte [`k8s/README.md`](k8s/README.md).

Use o overlay `local` para a imagem local e o overlay `prod` para:

```text
nuriancoelho/video-processor-api:latest
```

## CI/CD

`.github/workflows/ci.yml` executa:

- testes;
- compilação;
- build da imagem Docker.

Pushes para `main` também publicam a imagem no Docker Hub utilizando:

```text
DOCKERHUB_USERNAME
DOCKERHUB_TOKEN
```

## Estrutura principal

```text
.
├── main.go
├── repository.go
├── queue.go
├── s3.go
├── logger.go
├── notifier.go
├── docs/
│   └── openapi.yaml
├── k8s/
├── Dockerfile
├── docker-compose.yml
├── go.mod
└── go.sum
```

## Worker

O processamento de vídeos é realizado separadamente pelo projeto:

```text
video-processor-worker
```

O worker:

1. consome jobs do RabbitMQ;
2. baixa o vídeo do Object Storage;
3. executa o FFmpeg;
4. gera os frames;
5. cria o ZIP;
6. envia o ZIP para o Object Storage;
7. atualiza o status do job no PostgreSQL;
8. envia notificações em caso de erro.

A API e o worker compartilham o mesmo PostgreSQL, RabbitMQ, MongoDB e Object Storage.
```

Esse README já fica alinhado com o estado atual do projeto: **SeaweedFS/S3, outbox, worker separado, Docker Compose e estrutura real das chaves de storage**.