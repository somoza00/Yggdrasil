#!/usr/bin/env bash
# Sobe o ambiente de desenvolvimento local (compose).
set -euo pipefail
cd "$(dirname "$0")/.."

if [[ ! -f .env ]]; then
  echo "Aviso: .env ausente. Copie de .env.example antes de subir."
fi

docker compose up -d --build
echo "Ambiente dev no ar. Logs: docker compose logs -f"