# ADR-002 — Protocolo síncrono entre serviços (gRPC vs REST)

- **Status:** PROPOSTA / EM ABERTO — **requer spike** `.spike-pending`
- **Data:** 2026-10-13 (rascunho inicial)

> ⚠️ DECISÃO NÃO TOMADA. Enquanto este spike não rodar, não criar dependências
> síncronas de contrato entre serviços que travem decisão futura.

## Contexto

Os três serviços (ygg-api / huginn / muninn) precisam se comunicar de forma
síncrona em pontos específicos, com latência baixa e contratos claros:

- ygg-api (Go) → muninn (Python): pede uma triagem IA de um alerta.
- muninn (Python) → ygg-api (Go): registra/atualiza decisão e incidente.
- huginn (Rust) → muninn (Python): envia evento normalizado para enriquecimento
  por IA / recebe enriquecimento.
- ygg-api (Go) → huginn (Rust): consulta estado de um evento/correlação.

Fluxo pesado (streams de eventos) é assíncrono via Redis Streams (fora do escopo
deste ADR). Este ADR trata só da **chamada síncrona request/response** de baixo
volume relativo (triagens, consultas de incidente) — mas com requisito de baixa
latência e saída estruturada.

## Opções

### gRPC (proto3)
- **Prós** (na arquitetura polyglot): schemas versionados em `.proto`
  independem de linguagem; HTTP/2 com multiplex e baixa latência; codegen
  eficiente para Go e Rust; Python suportado (grpcio).
- **Contras**: tooling (grpclib), curva de aprendizado, debuggability pior
  (binário por default — mitigável com reflection/gateway), e o Python é o elo
  mais fraco do ecossistema gRPC.

### REST (OpenAPI)
- **Prós**: ubíquo, fácil de debugar/curl, tooling maduro em Python (FastAPI
  gera OpenAPI nativo), Go tem ótimo suporte.
- **Contras**: schemas versionados ficam como spec externa (snake em go vs
  snake/camel cross-language), tipagem compartilhada mais solta, latência e
  eficiência menores que HTTP/2 binário (na prática irrelevante para este volume).

## Critérios de decisão (para o spike medir)

1. Latência p99 na chamada de triagem (ygg-api→muninn).
2. Esforço de codegen e manutenção de contrato em Go+Rust+Python.
3. Facilidade de debug em dev.
4. Suporte a tooling de observabilidade (trace IDs atravessando).
5. Riscos de dependency hell em Python (grpcio vs fastapi/clients).

## Decisão (a ser preenchida APÓS o spike)

> **DECISÃO:** [pendente]

## Consequências

> [pendente — dependerá da escolha]