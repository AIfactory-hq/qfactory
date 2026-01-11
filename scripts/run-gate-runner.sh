#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${REPO_ROOT}"

# Default configuration
export GATE_RUNNER_ADDR="${GATE_RUNNER_ADDR:-:8095}"
export GATE_RUNNER_NAME="${GATE_RUNNER_NAME:-local}"
export GATE_RUNNER_REPO_ROOT="${GATE_RUNNER_REPO_ROOT:-${REPO_ROOT}}"

echo "Starting gate-runner '${GATE_RUNNER_NAME}' on ${GATE_RUNNER_ADDR}..."
echo "Repo root: ${GATE_RUNNER_REPO_ROOT}"
go run ./apps/gate-runner
