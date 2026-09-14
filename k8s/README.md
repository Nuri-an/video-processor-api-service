# Kubernetes

Os manifests deste diretorio permitem executar localmente a API, o worker e as
dependencias PostgreSQL, RabbitMQ, MongoDB e Mailpit.

## Requisitos

- Kubernetes 1.25+;
- Docker Desktop com Kubernetes habilitado, ou outro cluster;
- Metrics Server instalado para os HPAs;
- imagem local `video-processor-api:latest` para desenvolvimento ou
  `nuriancoelho/video-processor-api:latest` para producao;
- para producao, um StorageClass `ReadWriteMany` para o volume compartilhado
  entre API e workers.

O Docker Desktop usa o StorageClass `standard`, que suporta `ReadWriteOnce`.
Esse modo e suficiente para o teste local porque os pods sao agendados no mesmo
no. Para producao com replicas em nos diferentes, altere [pvc.yaml](pvc.yaml)
para um StorageClass com suporte a `ReadWriteMany`.

## Imagens e Secrets por ambiente

O overlay `local` usa a imagem local `video-processor-api:latest` com
`IfNotPresent`. O overlay `prod` usa
`nuriancoelho/video-processor-api:latest` com `Always`.

Os Secrets sao gerados a partir de arquivos de ambiente. Os arquivos reais sao
ignorados pelo Git; somente os exemplos ficam versionados:

- `k8s/env/local.env.example`;
- `k8s/env/production.env.example`.

## Deploy local

Execute os comandos a partir de `video-processor-api`:

```bash
kubectl config use-context docker-desktop
cp k8s/env/local.env.example k8s/env/local.env
kubectl kustomize --load-restrictor LoadRestrictionsNone k8s/overlays/local | kubectl apply -f -
```

Este procedimento instala a API e suas dependencias.

Para producao, copie o exemplo, preencha valores reais e aplique o overlay:

```bash
cp k8s/env/production.env.example k8s/env/production.env
# edite k8s/env/production.env com valores fortes
kubectl kustomize --load-restrictor LoadRestrictionsNone k8s/overlays/prod | kubectl apply -f -
```

Nunca versione `local.env` ou `production.env`. Em um ambiente de producao,
prefira criar o arquivo fora do repositorio ou usar um Secret Manager.

## Verificacao

```bash
kubectl -n video-processor get pods,pvc,svc,deploy,statefulset,hpa
kubectl -n video-processor get events --sort-by=.metadata.creationTimestamp
kubectl -n video-processor logs deployment/video-processor-api --tail=100
kubectl -n video-processor logs deployment/video-processor-worker --tail=100
```

A API fica acessivel localmente usando port-forward:

```bash
kubectl -n video-processor port-forward service/video-processor-api 8080:80
```

Depois abra `http://localhost:8080` ou consulte o Swagger em
`http://localhost:8080/swagger`.

O Mailpit fica acessivel com:

```bash
kubectl -n video-processor port-forward service/mailpit 8025:8025
```

Abra `http://localhost:8025` para visualizar os e-mails.


O RabbitMQ fica acessivel com:

```bash
kubectl -n video-processor port-forward service/mailpit 15672:15672
```

Abra `http://localhost:8025` para visualizar os e-mails.

## Autoscaling

O HPA da API escala de 2 a 6 pods conforme o uso de CPU. O Metrics Server
precisa estar instalado:

```bash
kubectl top nodes
kubectl -n video-processor get hpa
```

Se `kubectl top` falhar ou o HPA mostrar `cpu: <unknown>`, instale ou habilite o
Metrics Server no cluster. Para escalar workers pela quantidade de mensagens
RabbitMQ, use KEDA com uma metrica de fila; o HPA de CPU e apenas o mecanismo
inicial deste projeto.

## Limpeza

```bash
kubectl delete namespace video-processor
```

Esse comando remove os recursos e os dados persistidos do ambiente local.
