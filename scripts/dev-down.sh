#!/usr/bin/env bash
# Derruba o ambiente de desenvolvimento local.
set -euo pipefail
cd "$(dirname "$0")/.."
docker compose down
echo "Ambiente dev derrubado."