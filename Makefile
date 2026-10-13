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
	@echo "  make fmt            gofmt + cargo fmt + ruff format"
	@echo "  make lint           go vet + cargo clippy + ruff check"
	@echo "  make proto-gen      regenera stubs de proto (quando ADR-002 decidir)"

# ── Dev local ────────────────────────────────────────────────
.PHONY: dev-up
dev-up:
	docker compose up -d --build

.PHONY: dev-down
dev-down:
	docker compose down

.PHONY: dev-logs
dev-logs:
	docker compose logs -f

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