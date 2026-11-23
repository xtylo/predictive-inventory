#!/usr/bin/env bash
# Runs loadtest/k6.js with the official k6 image, on the dev container's
# Docker network, so k6 reaches the API as http://dev:8080.
#
# Usage (inside the dev container, from the repository root):
#   RATE=2500 DURATION=60s bash scripts/loadtest.sh
#
# The output is printed when k6 finishes, not live. From this dev container,
# Docker's live output stream (docker run -i, docker start -a) gets cut off
# after k6's banner, while the copy Docker keeps (docker logs) is complete.
# So k6 runs detached and its log is read at the end.
set -euo pipefail

RATE="${RATE:-2500}"
DURATION="${DURATION:-60s}"
NAME="k6-$$"

docker create --name "$NAME" \
  --network predictive-inventory_devcontainer_default \
  -e RATE="$RATE" \
  -e DURATION="$DURATION" \
  -e BASE_URL="${BASE_URL:-http://dev:8080}" \
  grafana/k6 run /tmp/k6.js >/dev/null

# Remove the container on any exit, success or failure.
trap 'docker rm -f "$NAME" >/dev/null 2>&1 || true' EXIT

# Copying the script in avoids stdin, and also avoids bind mounts, whose
# paths would be resolved on the host rather than in the dev container.
docker cp loadtest/k6.js "$NAME:/tmp/k6.js" >/dev/null

docker start "$NAME" >/dev/null
echo "k6 running at ${RATE} req/s for ${DURATION}; results appear when it finishes..." >&2

# docker wait blocks until the container exits and prints its exit code:
# 0 = all thresholds met, 99 = a threshold failed.
exit_code=$(docker wait "$NAME")
docker logs "$NAME" 2>&1
exit "$exit_code"
