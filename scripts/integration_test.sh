#!/usr/bin/env bash
# End-to-end test of the whole pipeline against the dev container stack:
#
#   POST /sales -> background forecast -> GET /inventory -> SQS reorder message
#
# It starts its own API on a separate port with the fake predictor, so it
# costs nothing and does not clash with an API you already have running.
#
# Usage (inside the dev container, from the repository root):
#   bash scripts/integration_test.sh
set -euo pipefail

SKU="${SKU:-SKU-8}"
PORT="${PORT:-18080}"
API="http://localhost:${PORT}"
TF_DIR="infra/envs/local"
PG_CONTAINER="predictive-inventory_devcontainer-postgres-1"
REDIS_CONTAINER="predictive-inventory_devcontainer-redis-1"

step() { printf '\n==> %s\n' "$*"; }
fail() {
  printf '\nFAIL: %s\n' "$*" >&2
  exit 1
}

step "Infrastructure"
# Idempotent: creates the queues if LocalStack was restarted, else does nothing.
terraform -chdir="$TF_DIR" apply -auto-approve -input=false >/dev/null
QUEUE_URL=$(terraform -chdir="$TF_DIR" output -raw reorder_queue_url)
# Start from an empty queue so the message read below is this run's.
aws sqs purge-queue --queue-url "$QUEUE_URL"

step "Seed data and reset ${SKU}"
docker exec -i "$PG_CONTAINER" psql -q -U inventory <scripts/seed.sql
# Without this, the 5-minute dedup lock from a previous run would skip the forecast.
docker exec "$REDIS_CONTAINER" redis-cli DEL "forecast:${SKU}" "lock:forecast:${SKU}" >/dev/null

step "Start the API on port ${PORT} (fake predictor)"
go build -o bin/api ./cmd/api
LOG=$(mktemp)
# env -u removes a variable for this process only: the run never calls a model.
env -u ANTHROPIC_API_KEY PORT="$PORT" SQS_REORDER_QUEUE_URL="$QUEUE_URL" \
  ./bin/api >"$LOG" 2>&1 &
API_PID=$!
# The trap runs on any exit, success or failure, so the API never outlives
# the test. SIGINT triggers the same graceful shutdown as Ctrl+C.
trap 'kill -INT "$API_PID" 2>/dev/null; wait "$API_PID" 2>/dev/null || true' EXIT

for _ in $(seq 30); do
  curl -fs "${API}/health" >/dev/null && break
  sleep 0.5
done
curl -fs "${API}/health" >/dev/null || {
  cat "$LOG"
  fail "API did not start"
}

step "Record a sale of ${SKU}"
curl -fs -X POST "${API}/sales" \
  -H 'Content-Type: application/json' \
  -H "Idempotency-Key: e2e-$(date +%s%N)" \
  -d "{\"sku\":\"${SKU}\",\"quantity\":1}"
echo

step "Wait for the forecast"
INVENTORY=""
for _ in $(seq 20); do
  INVENTORY=$(curl -fs "${API}/inventory/${SKU}")
  [[ $INVENTORY == *'"status":"ok"'* ]] && break
  sleep 0.5
done
echo "$INVENTORY"
[[ $INVENTORY == *'"status":"ok"'* ]] || {
  cat "$LOG"
  fail "no forecast for ${SKU}"
}
EXPECTED=$(grep -o '"reorder_quantity":[0-9]*' <<<"$INVENTORY" | cut -d: -f2)
[[ $EXPECTED -gt 0 ]] || fail "expected a reorder, got reorder_quantity ${EXPECTED}"

step "Read the reorder message"
# Long polling: wait up to 5 seconds for the message instead of failing fast.
BODY=$(aws sqs receive-message --queue-url "$QUEUE_URL" --wait-time-seconds 5 \
  --query 'Messages[0].Body' --output text)
echo "$BODY"
[[ $BODY == *"\"sku\":\"${SKU}\""* ]] || {
  cat "$LOG"
  fail "no reorder message for ${SKU}"
}
GOT=$(grep -o '"quantity":[0-9]*' <<<"$BODY" | cut -d: -f2)
[[ $GOT == "$EXPECTED" ]] || fail "message quantity ${GOT}, inventory says ${EXPECTED}"

printf '\nPASS: sale -> forecast -> reorder of %s units for %s\n' "$GOT" "$SKU"
