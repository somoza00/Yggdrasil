#!/usr/bin/env bash
# Roda os gates de qualidade dos 3 serviços a partir da raiz.
set -euo pipefail
cd "$(dirname "$0")/.."

make test
echo "✓ Todos os gates passaram."