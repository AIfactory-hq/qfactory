#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

echo "Stopping qfactory development infrastructure..."

cd "${REPO_ROOT}/infra/docker-compose"
docker compose down

echo ""
echo "Infrastructure stopped."
echo "Note: Data volumes are preserved. Use 'docker compose down -v' to remove volumes."
