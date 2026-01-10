#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

echo "Starting qfactory development infrastructure..."

cd "${REPO_ROOT}/infra/docker-compose"
docker compose up -d

echo ""
echo "Waiting for services to be healthy..."
sleep 5

echo ""
echo "=========================================="
echo "qfactory infrastructure is starting"
echo "=========================================="
echo ""
echo "Service URLs:"
echo "  Temporal UI:  http://localhost:8088"
echo "  Qdrant:       http://localhost:6333/dashboard"
echo "  Postgres:     localhost:5432 (user: qfactory, db: qfactory)"
echo "  Redis:        localhost:6379"
echo "  Temporal:     localhost:7233"
echo ""
echo "Run './scripts/check.sh' to verify all services are healthy."
