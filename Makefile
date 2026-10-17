# ─────────────────────────────────────────────────────────────
# Yggdrasil — Makefile raiz
# Orquestra toolchains, dev local e gates de teste dos 3 serviços.
# ─────────────────────────────────────────────────────────────
SHELL := /bin/bash

.DEFAULT_GOAL := help

.PHONY: help
help:
	@echo "Yggdrasil — alvos:"
	@echo "  make setup          pre-requisitos/apt (parcial, ver notas)"
	@echo "  make dev-up         sobe Postgres+Redis+serviços (compose)"
	@echo "  make dev-down       derruba o compose"
	@echo "  make dev-logs       logs do compose (seguindo)"
	@echo "  make test           roda gates dos 3 serviços"
	@echo "  make ygg-api-test   testes Go"
	@echo "  make huginn-test    testes Rust"
	@echo "  make muninn-test    testes Python (pytest --cov)"
	@echo "  make e2e            sobe a stack e roda o e2e (CEF -> incidente)"
	@echo "  make fmt            gofmt + cargo fmt + ruff format"
	@echo "  make lint           go vet + cargo clippy + ruff check"
	@echo "  make proto-gen      regenera stubs de proto (quando ADR-002 decidir)"

# ── Dev local ────────────────────────────────────────────────
# Tokens JWT de serviço assinados com o JWT_SECRET de dev do compose
# (ygg-api com ENV=development). Esse segredo nunca vale fora do ambiente local.
JWT_SECRET ?= dev-jwt-secret
ygg-token = $(shell cd services/ygg-api && JWT_SECRET='$(JWT_SECRET)' go run ./cmd/ygg-token -sub $(1) -ttl 720h)

.PHONY: dev-up
dev-up: export YGG_API_SERVICE_TOKEN ?= $(call ygg-token,muninn)
dev-up:
	docker compose up -d --build

.PHONY: dev-down
dev-down:
	docker compose down

.PHONY: dev-logs
dev-logs:
	docker compose logs -f

# Container one-shot: CEF em events:ingest:raw -> incidente no ygg-api.
.PHONY: e2e
e2e: export YGG_API_SERVICE_TOKEN ?= $(call ygg-token,muninn)
e2e: export E2E_YGG_API_TOKEN ?= $(call ygg-token,e2e)
e2e:
	docker compose --profile e2e build
	docker compose --profile e2e run --rm e2e

# ── Gate de qualidade por serviço ────────────────────────────
.PHONY: ygg-api-test
ygg-api-test:
	$(MAKE) -C services/ygg-api test

.PHONY: huginn-test
huginn-test:
	$(MAKE) -C services/huginn test

.PHONY: muninn-test
muninn-test:
	$(MAKE) -C services/muninn test

.PHONY: test
test: ygg-api-test huginn-test muninn-test

.PHONY: fmt
fmt:
	$(MAKE) -C services/ygg-api fmt
	$(MAKE) -C services/huginn fmt
	$(MAKE) -C services/muninn fmt

.PHONY: lint
lint:
	$(MAKE) -C services/ygg-api lint
	$(MAKE) -C services/huginn lint
	$(MAKE) -C services/muninn lint

# ── Setup ────────────────────────────────────────────────────
.PHONY: setup
setup:
	@echo "Verifique: rustup (rustc/cargo), kubectl, kustomize, helm."
	@echo "Go, Python/uv, Docker, psql, valkey-cli já detectados nesta máquina."
	@echo "Dica Rust: curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh"
	@echo "Dica kustomize: go install sigs.k8s.io/kustomize/kustomize/v5@latest"

# ── Proto ────────────────────────────────────────────────────
.PHONY: proto-gen
proto-gen:
	@echo "Aguardando decisão do ADR-002. Se gRPC: usar buf generate."