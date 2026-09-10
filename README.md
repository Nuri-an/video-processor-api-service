# Video Processor API

API Gateway responsavel por receber videos, registrar tarefas e disponibilizar status e arquivos processados.

## Responsabilidades

- autenticar com tokens JWT;
- receber videos em `POST /upload`;
- salvar o video no storage local;
- registrar jobs no PostgreSQL;
- publicar jobs na fila `video_jobs` do RabbitMQ;
- consultar status por usuario;
- disponibilizar o Swagger da API.

Quando o worker falhar no FFmpeg ou na geracao do ZIP, ele deve atualizar o job para
`Erro` com a mensagem da falha e notificar o usuario por e-mail. Este repositorio
fornece a operacao `UpdateStatus` e o `SMTPNotifier` para essa integracao; o consumo
da fila e o processamento continuam no repositorio `video-processor-worker`.

O processamento do video e executado pelo repositorio `video-processor-worker`.

## Rotas

| Metodo | Rota | Descricao |
|---|---|---|
| `GET` | `/` | Verifica se a API esta disponivel |
| `GET` | `/swagger` | Swagger UI |
| `GET` | `/swagger/openapi.yaml` | Especificacao OpenAPI |
| `POST` | `/upload` | Recebe o video no campo `video` |
| `GET` | `/api/status` | Lista os jobs do usuario autenticado |
| `GET` | `/download/{filename}` | Baixa um ZIP processado |

As rotas de upload, status e download exigem:

```text
Authorization: Bearer <token>
```

Obtenha o token enviando as credenciais para `POST /auth/login`. Os usuarios sao
persistidos no PostgreSQL e as senhas sao armazenadas somente como hashes bcrypt.
O usuario inicial e criado a partir de `API_USER`, `API_PASSWORD_HASH` e `API_USER_EMAIL`.
O claim `email` do JWT e enviado junto com cada job:

```bash
curl -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"user":"admin","password":"admin"}'
```

A especificacao esta em [docs/openapi.yaml](docs/openapi.yaml).

## Dependencias

- PostgreSQL: jobs e status;
- RabbitMQ: fila `video_jobs`;
- MongoDB: logs da aplicacao;
- filesystem local: videos recebidos e ZIPs.

## Variaveis de ambiente

| Variavel | Padrao | Uso |
|---|---|---|
| `API_USER` | `admin` | Usuario da API |
| `API_PASSWORD_HASH` | vazio | Hash bcrypt da senha do usuario inicial |
| `API_USER_EMAIL` | vazio | E-mail do usuario para notificacoes do worker |
| `JWT_SECRET` | vazio | Chave usada para assinar tokens JWT |
| `API_STORAGE_DIR` | `uploads` | Videos recebidos |
| `API_OUTPUT_DIR` | `outputs` | ZIPs gerados |
| `POSTGRES_DSN` | vazio | DSN completo opcional |
| `POSTGRES_HOST` | `localhost` | Host do PostgreSQL |
| `POSTGRES_PORT` | `5432` | Porta do PostgreSQL |
| `POSTGRES_USER` | `video_processor` | Usuario do PostgreSQL |
| `POSTGRES_PASSWORD` | `video_processor` | Senha do PostgreSQL |
| `POSTGRES_DB` | `video_processor` | Banco do PostgreSQL |
| `RABBITMQ_URL` | vazio | URL completa opcional |
| `RABBITMQ_HOST` | `localhost` | Host do RabbitMQ |
| `RABBITMQ_PORT` | `5672` | Porta do RabbitMQ |
| `RABBITMQ_USER` | `video_processor` | Usuario do RabbitMQ |
| `RABBITMQ_PASSWORD` | `video_processor` | Senha do RabbitMQ |
| `RABBITMQ_QUEUE` | `video_jobs` | Fila de jobs |
| `MONGO_URI` | `mongodb://localhost:27017` | Conexao dos logs |
| `MONGO_DATABASE` | `video_processor_logs` | Banco dos logs |
| `MONGO_COLLECTION` | `application_logs` | Collection dos logs |
| `SMTP_HOST` | `localhost` | Host do servidor SMTP |
| `SMTP_PORT` | `25` | Porta do servidor SMTP |
| `SMTP_USERNAME` | vazio | Usuario SMTP |
| `SMTP_PASSWORD` | vazio | Senha SMTP |
| `SMTP_FROM` | `noreply@video-processor.local` | Remetente das notificacoes |

## Execucao local

Com PostgreSQL, RabbitMQ e MongoDB disponiveis:

```bash
go mod tidy
go run .
```

A API ficara disponivel em `http://localhost:8080`.

Swagger:

```text
http://localhost:8080/swagger
```

## Docker

```bash
docker build -t video-processor-api .
docker run --name video-api -p 8080:8080 \
  -e POSTGRES_HOST=host.docker.internal \
  -e RABBITMQ_HOST=host.docker.internal \
  -e MONGO_URI=mongodb://host.docker.internal:27017 \
  video-processor-api
```

Quando as dependencias estiverem em containers, coloque-os na mesma rede Docker e use seus nomes como hosts.

## Testar upload

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

Exemplo de tratamento no worker:

```go
if err := processVideo(job); err != nil {
  message := err.Error()
  _ = jobs.UpdateStatus(job.ID, "Erro", message)
  _ = notifier.NotifyProcessingError(job.User, job, err)
  return
}
_ = jobs.UpdateStatus(job.ID, "Concluido", "")
```

## CI/CD

`.github/workflows/ci.yml` executa testes, compilacao e build da imagem. Pushes para `main` tambem publicam a imagem no Docker Hub com `DOCKERHUB_USERNAME` e `DOCKERHUB_TOKEN`.

## Pendencia de integracao

Os arquivos de logging presentes neste repositorio usam o cliente MongoDB, mas o entrypoint precisa chamar o construtor correspondente (`NewMongoLogger`) e manter as dependencias do `go.mod` alinhadas. Resolva essa nomenclatura antes do build final.
