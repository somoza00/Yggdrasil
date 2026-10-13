# Yggdrasil

Plataforma **SOAR open source e AI-powered** para automatizar a **triagem de
alertas de segurança** que hoje o SOC faz manualmente.

Origem do produto: SOC N1 numa MSSP, triando alertas de SentinelOne, Rapid7,
Jira e n8n. O Yggdrasil ingere eventos, normaliza/enriquece/correlaciona, e
usa agentes de IA para decidir se um alerta é verdadeiro positivo, falso
positivo, ou precisa de atenção humana — com decisão rastreável e explicável.

## Serviços

| Serviço | Linguagem | Papel |
|---------|-----------|-------|
| `ygg-api` | Go | API gateway, auth JWT, playbooks, incidentes |
| `huginn` | Rust | Ingest de eventos (Redis Streams), parsing CEF/syslog, enriquecimento de IOC, correlação |
| `muninn` | Python | Agentes de IA (Pydantic AI), gateway LLM (LiteLLM), observabilidade (Langfuse) |

## Stack de deploy e dados

- **Kubernetes** com **kustomize** (`deploy/kustomize/`)
- **PostgreSQL** — dados estruturados
- **Redis Streams / Redis** — fila de eventos e cache

## Decisões de arquitetura

Todas em `docs/adr/`. **Leia antes de codar.**

- ADR-001 linguagem por camada — ACEITO
- ADR-002 protocolo síncrono gRPC vs REST — **EM ABERTO (spike pendente)**
- ADR-003 storage Postgres + Redis — ACEITO
- ADR-004 AI layer LiteLLM + Pydantic AI + Langfuse — ACEITO

## Começar rapido (dev local)

```sh
make dev-up      # sobe Postgres + Redis + serviços via compose
make test        # roda testes dos 3 serviços
make dev-down    # derruba
```

Cada serviço tem `.env.example` próprio → copie para `.env` (jamais commitar).

## Contexto para agentes de IA

Leia [`AGENTS.md`](./AGENTS.md) — contém arquitetura, regras de git, gates e
convenções que toda sessão futura deve carregar.

## Licença

Apache-2.0 (open source). Consulte `LICENSE`.