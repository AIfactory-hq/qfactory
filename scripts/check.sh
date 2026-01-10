#!/usr/bin/env bash
set -euo pipefail

echo "qfactory Environment Check"
echo "=========================="
echo ""

ERRORS=0

# Check Docker
echo -n "Docker: "
if command -v docker &> /dev/null; then
    DOCKER_VERSION=$(docker --version | cut -d' ' -f3 | tr -d ',')
    echo "OK (${DOCKER_VERSION})"
else
    echo "NOT FOUND"
    ERRORS=$((ERRORS + 1))
fi

# Check Docker Compose
echo -n "Docker Compose: "
if docker compose version &> /dev/null; then
    COMPOSE_VERSION=$(docker compose version --short)
    echo "OK (${COMPOSE_VERSION})"
else
    echo "NOT FOUND"
    ERRORS=$((ERRORS + 1))
fi

# Check Go
echo -n "Go: "
if command -v go &> /dev/null; then
    GO_VERSION=$(go version | cut -d' ' -f3)
    echo "OK (${GO_VERSION})"
else
    echo "NOT FOUND"
    ERRORS=$((ERRORS + 1))
fi

echo ""
echo "Service Health Checks"
echo "---------------------"

# Helper function to check container health
check_container() {
    local name=$1
    local status=$(docker inspect --format='{{.State.Health.Status}}' "$name" 2>/dev/null || echo "not_found")
    case "$status" in
        healthy) echo "HEALTHY"; return 0 ;;
        unhealthy) echo "UNHEALTHY"; return 1 ;;
        starting) echo "STARTING"; return 1 ;;
        not_found) echo "NOT FOUND"; return 1 ;;
        *) echo "UNKNOWN ($status)"; return 1 ;;
    esac
}

# Check Postgres
echo -n "Postgres: "
if check_container qfactory-postgres; then
    :
else
    ERRORS=$((ERRORS + 1))
fi

# Check Redis
echo -n "Redis: "
if check_container qfactory-redis; then
    :
else
    ERRORS=$((ERRORS + 1))
fi

# Check Qdrant (no healthcheck in container, check endpoint directly)
echo -n "Qdrant: "
if curl -sf http://localhost:6333/readyz &>/dev/null; then
    echo "HEALTHY"
else
    echo "NOT READY"
    ERRORS=$((ERRORS + 1))
fi

# Check Temporal
echo -n "Temporal: "
if check_container qfactory-temporal; then
    :
else
    ERRORS=$((ERRORS + 1))
fi

# Check Temporal UI (no healthcheck, check if running)
echo -n "Temporal UI: "
if docker ps --format '{{.Names}}' | grep -q qfactory-temporal-ui; then
    if curl -sf http://localhost:8088 &>/dev/null; then
        echo "HEALTHY"
    else
        echo "RUNNING (UI may take a moment)"
    fi
else
    echo "NOT RUNNING"
    ERRORS=$((ERRORS + 1))
fi

echo ""
if [ $ERRORS -eq 0 ]; then
    echo "All checks passed!"
    echo ""
    echo "Service URLs:"
    echo "  Temporal UI:  http://localhost:8088"
    echo "  Qdrant:       http://localhost:6333/dashboard"
    exit 0
else
    echo "Some checks failed. Run './scripts/dev-up.sh' to start services."
    exit 1
fi
