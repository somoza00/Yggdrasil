# Spike — ADR-002: gRPC vs REST para o protocolo síncrono entre serviços

- **Objetivo:** decidir o protocolo síncrono entre ygg-api (Go), huginn (Rust)
  e muninn (Python).
- **Janela:** 1 semana. **Escopo:** apenas a chamada síncrona request/response,
  NÃO o fluxo de streams (isso é Redis, já decidido).
- **Entrega:** evidências mensuráveis + recomendação para preencher o ADR-002.

## O que vamos construir para medir (provas reais, não opinião)

Um "hello world" realista do contrato crítico — a chamada de triagem
**ygg-api (Go) → muninn (Python)**, que devolve `TriageDecision`. Montar o
mesmo endpoint **duas vezes** (gRPC e REST), com os mesmos dados, e medir:

1. **Latência p99** da chamada completa (ygg-api → muninn → resposta).
2. **Tamanho do payload** (gRPC protobuf vs REST JSON) para um alerta típico.
3. **Esforço de codegen**: tempo/linhas para gerar e manter o contrato em
   Go + Rust + Python (proto/OpenAPI; ferramentas: buf, protoc-gen-go,
   prost/tonic, grpcio / fastapi-openapi).
4. **Facilidade de debug em dev**: curl/insomnia/postman, logs legíveis.
5. **Rastreamento de trace-id** atravessando a chamada (bom suporte a
   OTel em ambos; medir facilidade).
6. **Dependency hell** em Python: `grpcio` vs fastapi/httpx.

## Métricas mínimas de aceite do spike

| Métrica | gRPC | REST |
|---------|------|------|
| Latência p99 (ms) | ? | ? |
| Payload (bytes, alerta típico) | ? | ? |
| Código de contrato (LOC por serviço) | ? | ? |
| Ferramenta de debug funciona em dev? | ? | ? |
| Trace-id atravessa a chamada? | ? | ? |

## Plano proposto

1. **Setup do spike** — branch `spike/adr-002-protocol` no monorepo. Esqueleto
   mínimo nos 3 serviços (sem lógica real de negócio).
2. **Caminho A (gRPC)** — definir `.proto` da triagem; gerar stubs; subir
   ygg-api (cliente) + muninn (servidor); medir latência/payload.
3. **Caminho B (REST)** — OpenAPI do mesmo contrato; cliente Go (net/http)
   + servidor muninn (FastAPI já expõe `/triage` hoje); medir o mesmo.
4. **Nota do Rust** — o principal ponto gRPC x REST é entre Go↔Python. Rust
   (huginn) consome Redis micrifico; o contrato síncrono de huginn pode ser
   tratado depois (não é o caminho crítico do spike).
5. **Comparar tabela**, escrever recomendação e atualizar o ADR-002
   (status → ACEITO com texto da decisão).

> Microbench pode ficar sujeito a ruído; usar 1000 iterações, warmup, e medir
> com um único proxy local (localhost) e também com roundtrip real via compose.

## Riscos/neutralidade

- gRPC tende a vencer em latência p99 p/ alto throughput, mas este contrato é
  de baixo volume. REST com FastAPI tende a ganhar em simplicidade/debug.
- Peso da decisão: escolher errado custa reescrever o contrato da triagem +
  ygg-api↔muninn e huginn↔muninn. O spike existe para não decidir no chute.

## Perguntas para o revisor do spike

- Devemos padronizar um único protocolo para TODOS os pares, ou usar gRPC
  só onde a latência importa e REST no resto? (Recomendação: provavelmente 1
  protocolo só, por simplicidade — mas o spike deve pesar isso.)
- Posta o spike em `docs/spikes/` e PR para revisão? Sim — segue o fluxo
  git do repo (branch + PR, sem merge direto em main).

## Referências

- ADR-002 rascunho: `docs/adr/ADR-002-inter-service-protocol.md`