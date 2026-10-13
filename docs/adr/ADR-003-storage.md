# ADR-003 — Storage: PostgreSQL + Redis

- **Status:** ACEITO
- **Data:** 2026-10-13

## Contexto

Dados estruturados (incidentes, playbooks, usuários, run history) precisam de
consistência e queries relacionais. Eventos de segurança entram em alto volume,
em formato de stream, e alimentam um pipeline de ingest/correlação que precisa
de fila e cache de baixa latência.

## Decisão

- **PostgreSQL**: fonte de verdade para dados estruturados das APIs
  (incidentes, playbooks, usuários, audit). Schema versionado por migrations.
- **Redis (Streams)**: ingest de eventos do pipeline — huginn lê/escreve nesse
  stream; cache de dados quentes (resultados de enriquecimento, sessões).

## Consequências

- Dois sistemas a operar (Postgres + Redis) — aceitável para o domínio.
- Redis Streams dá consumer groups → 1 evento processado pelo grupo huginn
  (sem perda por concorrência), com ack. Ideal para ingest tolerante a falha.
- Migrations de schema do Postgres são aplicadas por ygg-api (ou serviço de
  migrations dedicado no futuro).