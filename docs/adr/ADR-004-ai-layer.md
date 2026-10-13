# ADR-004 — AI layer: LiteLLM + Pydantic AI + Langfuse

- **Status:** ACEITO
- **Data:** 2026-10-13

## Contexto

A triagem de alertas é feita por agentes de IA. Precisamos de: um gateway único
que fale com vários provedores de LLM (evitar lock-in e rotacionar modelos),
saída estruturada/typada para garantir que a decisão de triagem seja consumível
por código, e observabilidade para auditar e debugar as decisões da IA
(essencial em SIEM/SOAR — a decisão tem que ser rastreável e explicável).

## Decisão

Toda a camada de IA fica no serviço **muninn (Python)** e usa:

- **LiteLLM** como gateway de LLM — acesso uniforme a múltiplos provedores
  (OpenAI, Anthropic, modelos open, corporate/automação endpoints) com uma API.
- **Pydantic AI** para agentes — agentes tipados, com tools e saídas
  estruturadas (o schema de triagem é um Pydantic model).
- **Langfuse** para observabilidade — traces das chamadas, audit da decisão,
  suporte a debug e avaliação da qualidade das triagens.

## Consequências

- A lógica de IA fica isolada em muninn; ygg-api/huginn pedem triagem via
  contrato (ver ADR-002) — não dependem da implementação do modelo.
- Complexidade de config (provedores, keys, traces) concentrada em muninn.
- Langfuse permite "avaliar as triagens da IA contra o veredito do analista"
  — ciclo de melhoria contínua (feature futura).