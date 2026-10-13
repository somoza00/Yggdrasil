# ADR-001 — Linguagem por camada

- **Status:** ACEITO
- **Data:** 2026-10-13
- **Autor:** JP (SOM), revisado por Hermes

## Contexto

Yggdrasil é uma plataforma SOAR com workloads heterogêneos: uma API web com
orquestração, um pipeline de ingest/parsing de alto throughput, e uma camada de
agentic AI. Nenhuma linguagem é ótima para os três. Usar uma única linguagem
imporia trade-offs onde não são necessários.

## Decisão

Cada serviço usa a ferramenta certa para a sua camada:

- **ygg-api (Go)** — API gateway, auth JWT, playbooks, incidentes. Servidor HTTP
  concorrente, binário único, baixa latência, ótimo ecossistema de tooling.
- **huginn (Rust)** — ingest via Redis Streams, parsing CEF/syslog, enriquecimento
  de IOCs, correlação. Performance de parsing e segurança de memória.
- **muninn (Python)** — agentes de IA (Pydantic AI), gateway LLM (LiteLLM),
  observabilidade (Langfuse). Ecossistema de IA dominado por Python; saída
  estruturada e tipada.

Cada serviço é um módulo independente no monorepo, com sua própria toolchain,
tests e gates. Isolamento de linguagem também isola blast radius de dependências.

## Consequências

- Três toolchains a manter (Go, Rust, Python). Mitigado por Makefile raiz que
  orquestra os gates.
- Contrato síncrono entre serviços precisa ser bem definido → motivador do
  ADR-002 (gRPC vs REST).
- Onboarding custa mais (3 linguagens), mas cada serviço é pequeno e
  idiomaticamente simples.