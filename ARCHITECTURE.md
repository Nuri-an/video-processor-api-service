```bash
code ~/video-processor-api-service
```

Cria o arquivo `ARCHITECTURE.md` com:

```markdown
# Arquitetura — FIAP X Video Processor

## Visão Geral

Sistema distribuído para processamento assíncrono de vídeos. O usuário envia um vídeo pela API, que registra o job e publica na fila. O Worker consome a fila, extrai os frames com FFmpeg e compacta em ZIP para download.

## Microsserviços

| Serviço     | Linguagem | Repositório                    |
| ----------- | --------- | ------------------------------ |
| API Gateway | Go        | video-processor-api-service    |
| Worker      | Go        | video-processor-worker-service |

## Diagrama de Arquitetura
```

┌─────────────┐ POST /upload ┌─────────────────────┐
│ Cliente │ ───────────────────▶ │ API Gateway │
│ (Browser/ │ GET /api/status │ (Go + Gin) │
│ Curl) │ ◀─────────────────── │ Porta: 8080 │
└─────────────┘ GET /download/:zip └──────────┬──────────┘
│
┌────────────────────────┼────────────────────────┐
│ │ │
▼ ▼ ▼
┌─────────────┐ ┌─────────────────┐ ┌─────────────────┐
│ PostgreSQL │ │ RabbitMQ │ │ MongoDB │
│ (jobs + │ │ video_jobs │ │ (logs) │
│ usuarios) │ │ video_jobs.dlx │ │ │
└─────────────┘ └────────┬────────┘ └─────────────────┘
│
▼
┌─────────────────────┐
│ Worker │
│ (Go + FFmpeg) │
│ Processa videos │
│ Gera ZIP │
└──────────┬──────────┘
│
┌──────────────────┼──────────────────┐
▼ ▼ ▼
┌───────────┐ ┌──────────────┐ ┌──────────────┐
│PostgreSQL │ │ MongoDB │ │ SMTP │
│(atualiza │ │ (logs) │ │(notificacao │
│ status) │ │ │ │ de erro) │
└───────────┘ └──────────────┘ └──────────────┘

```

## Fluxo Principal

1. Cliente autentica via `POST /auth/login` e recebe JWT
2. Cliente envia vídeo via `POST /upload` com o token
3. API salva o vídeo no storage local
4. API registra o job no PostgreSQL com status `Pendente`
5. API publica o job na fila `video_jobs` via outbox transacional
6. Worker consome a mensagem da fila
7. Worker executa FFmpeg para extrair frames (1 frame/segundo)
8. Worker compacta os frames em ZIP
9. Worker atualiza o job no PostgreSQL para `Concluido`
10. Cliente consulta status via `GET /api/status`
11. Cliente faz download do ZIP via `GET /download/:filename`

## Tratamento de Falhas

- **Retry com backoff exponencial**: até 3 tentativas com delay crescente
- **Dead Letter Queue**: mensagens que excedem tentativas vão para `video_jobs.dead`
- **Notificação por e-mail**: usuário notificado em caso de erro permanente
- **Outbox transacional**: garante que nenhum job seja perdido entre o banco e a fila

## Persistência

| Banco | Uso |
|-------|-----|
| PostgreSQL | Jobs, status, usuários |
| MongoDB | Logs de eventos da aplicação |
| Filesystem | Vídeos recebidos e ZIPs gerados |

## Infraestrutura

- **Containers**: Docker + Kubernetes
- **Mensageria**: RabbitMQ com publisher confirms e QoS/prefetch
- **Autenticação**: JWT (HS256) com senhas em bcrypt
- **Escalabilidade**: HPA no Kubernetes para a API; concorrência configurável no Worker

## CI/CD

Ambos os repositórios usam GitHub Actions:

| Evento | Ação |
|--------|------|
| Push / PR | Testes + Build |
| Push main | Publicar imagem no Docker Hub |

## Repositórios

- API: https://github.com/Nuri-an/video-processor-api-service
- Worker: https://github.com/Nuri-an/video-processor-worker-service
```
