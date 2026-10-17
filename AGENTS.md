# AGENTS.md — Yggdrasil

Guia de contexto para agentes de IA (e humanos) que trabalham neste monorepo.
Leia isto inteiro antes de qualquer modificação.

## O que é

Yggdrasil é uma plataforma **SOAR open source e AI-powered** (Security
Orchestration, Automation and Response). O objetivo de produto é automatizar a
**triagem de alertas de segurança** que hoje um SOC (Security Operations Center)
faz manualmente. O Yggdrasil é um projeto open source de portfólio e agnóstico
de vendor: integra fontes via conectores genéricos (EDR, SIEM, ticketing,
automação) e não cita nenhum produto comercial específico.

O que resolve concretamente: ingerir eventos de várias fontes, normalizar/
enriquecer/correlacionar, e usar agentes de IA para decidir se um alerta é
verdadeiro (true positive), falso (false positive) ou precisa de atenção humana —
sempre com rastreabilidade da decisão.

## Monorepo — topologia

```
yggdrasil/
├── AGENTS.md            <- este arquivo (contexto para agentes)
├── Makefile             <- alvos de dev/test/doc, orquestra tudo
├── compose.yaml         <- ambiente de desenvolvimento local (docker)
├── docs/adr/            <- Architecture Decision Records (fonte de verdade p/ decisões)
├── services/
│   ├── ygg-api/         <- GO — API gateway, auth JWT, playbooks e incidentes
│   ├── huginn/          <- RUST — ingest de eventos (Redis Streams), parsing CEF/syslog, enriquecimento de IOC, correlação
│   └── muninn/          <- PYTHON — agentes de IA (Pydantic AI), gateway LLM (LiteLLM), observabilidade (Langfuse)
├── deploy/kustomize/    <- DUPLO de configuração do deploy (base + overlays)
├── proto/               <- schemas e fixtures de contrato (ADR-002, ADR-005)
└── scripts/             <- utilitários de dev/CI
```

## Decisões arquiteturais (ADR — ler antes de codar)

As ADRs vivem em `docs/adr/` e são a fonte de verdade. Não "descubra" a
arquitetura no código; leia as ADRs.

| ADR | Tema | Status |
|-----|------|--------|
| ADR-001 | Linguagem por camada (Go/Rust/Python) | **ACEITO** |
| ADR-002 | Protocolo síncrono entre serviços: REST, duas chamadas | **FECHADO** |
| ADR-003 | Storage: PostgreSQL (estruturado) + Redis (streams/cache) | **ACEITO** |
| ADR-004 | AI layer: LiteLLM + Pydantic AI + Langfuse | **ACEITO** |
| ADR-005 | Contrato de eventos assíncrono (Redis Streams) | **ACEITO** |
| ADR-006 | Autenticação do ygg-api (JWT HS256, token de serviço) | **ACEITO** |

### Linguagem por camada (ADR-001)

Cada serviço é isolado do ponto de vista de linguagem/toolchain — usar a
linguagem certa da camada, não "tudo em uma só":

- **ygg-api (Go)** — API gateway, auth JWT, gerenciamento de playbooks e
  incidentes. Go porque é excelente para servidores HTTP concorrentes, deploy
  binário único, baixa latência.
- **huginn (Rust)** — ingest de eventos via Redis Streams, parsing de logs
  CEF/syslog, enriquecimento de IOCs e correlação. Rust pela performance de
  parsing de alto throughput e segurança de memória.
- **muninn (Python)** — agentes de IA com **Pydantic AI**, gateway LLM via
  **LiteLLM**, observabilidade com **Langfuse**. Python porque o ecossistema de
  IA/ML é dominado por Python.

### Storage (ADR-003)

- **PostgreSQL**: dados estruturados — incidentes, playbooks, usuários, run history.
  Migrations em `services/ygg-api/migrations/` (goose, aplicadas na subida do ygg-api).
- **Redis**: streams (fila de eventos ingeridos) e cache.

### AI layer (ADR-004)

- **LiteLLM** como gateway uniforme para múltiplos provedores de LLM.
- **Pydantic AI** para modelar agentes e saídas estruturadas (typed).
- **Langfuse** para observabilidade/traces das chamadas de IA.

## Fluxo de dados na borda (visão geral)

```
fontes (EDR, SIEM, syslog, CEF, webhooks/automação)
      │
      ▼
huginn (Rust) ──ingest──▶ Redis Streams
      │ parsing CEF/syslog · enriquecimento IOC · correlação
      ▼
      eventos normalizados
      ▼
muninn (Python) ── agente de triagem IA (Pydantic AI → LiteLLM → Langfuse)
      ▼
      decisão de triagem (TP / FP / review)
      ▼
ygg-api (Go) ── incidente (PostgreSQL) ── playbook ── notificação/ticketing
```

## Regras de desenvolvimento (IMPORTANTE — seguir SEMPRE)

- **Git:** NUNCA commitar diretamente em `main`. Sempre criar branch nova
  (`feat/...`, `fix/...`, `chore/...`). Commits em formato conventional
  (`feat:`, `fix:`, `docs:`, `chore:`, `test:`, `refactor:`).
- **Patches > rewrites:** Ao modificar arquivos, faça mudanças cirúrgicas. Não
  reescreva arquivos inteiros se uma mudança pontual resolve.
- **Docs são referência pura:** documentação NÃO deve alterar comportamento do
  programa. Se encontrar uma doc que muda comportamento, o do comportamento é o
  código — corrija o código, não "conserta" via doc.
- **Env discipline:** não duplicar variáveis no `.env.example`; manter
  `sync-env.sh` em sintonia com todo novo segredo.
- **Pré-requisito ao editar código:** código evolui fora da sessão. Re-ler
  arquivos dependentes antes de editar. Após falha de verificação, checar o
  estado atual antes de corrigir.
- **TDD quando aplicável:** escrever o teste que falha antes do código
  (RED-GREEN-REFACTOR) para lógica de negócio em cada serviço.
- **Segredos:** credenciais reais jamais no código. Usar `.env` local e
  Secrets (K8s/OpenBao) em produção.

## Toolchains e gates por serviço

| Serviço | Ling | Gate de qualidade | Testes |
|---------|------|-------------------|--------|
| ygg-api | Go 1.27 | `go vet` + `gofmt` + `go test ./...` | `go test ./...` |
| huginn  | Rust (edition 2021) | `cargo fmt` + `cargo clippy` + `cargo test` | `cargo test` |
| muninn  | Python 3.13+ / uv | `ruff` + `mypy` + `pytest --cov` | `pytest` |

Cada serviço tem o seu `Makefile` herdando os alvos do Makefile raiz
(`make ygg-api-test`, `make huginn-test`, `make muninn-test`) ou `make test`
(roda tudo).

**Estado das toolchains na máquina de dev (verificar antes de assumir):**
Go e Python/uv instalados; **Rust (rustc/cargo), kubectl, kustomize e helm NÃO
instalados** — instalar (rustup) ou usar container antes de depender deles.

## Como rodar local

Dev local usa Docker (`compose.yaml`). Ver `Makefile`:

```sh
make setup       # prepara toolchains/venv (parcial)
make dev-up      # sobe banco + redis + serviços (compose)
make dev-down    # derruba tudo
make test        # roda testes dos 3 serviços
```

Cada serviço tem `.env.example` próprio — copie para `.env` (nunca commitar `.env`).

## ADR-002 (FECHADO) — resumo p/ contexto

Protocolo síncrono: **REST**, com exatamente duas chamadas:

1. ygg-api → muninn `POST /triage` (re-triagem sob demanda).
2. muninn → ygg-api `POST /api/v1/incidents` (após a triagem assíncrona;
   idempotente por `event_id`).

Todo o resto passa pelo Redis Streams (ADR-005). Uma chamada síncrona nova
exige revisar o ADR-002. Ver `docs/adr/ADR-002-inter-service-protocol.md`.

## Convenções

- Pasta de skill/review/dogfood para quem usa ferramentas de agente: veja o
  skill de review do projeto quando aplicável.
- Comentários de commit e PRs em inglês (convenção repo). Mensagens/contexto de
  produto podem ser em português se for o caso.
- Todo comportamento observável precisa de teste. Toda decisão de arquitetura
  precisa de ADR.