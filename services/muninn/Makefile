.PHONY: all test lint fmt

all:
	@echo "Use: uv sync && uv run pytest"

# Alvos padrão compartilhados com o Makefile raiz
test:
	uv run pytest -q

lint:
	uv run ruff check .
	uv run mypy src

fmt:
	uv run ruff format .