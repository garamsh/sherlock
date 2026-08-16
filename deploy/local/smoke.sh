#!/usr/bin/env bash
# Stands up the deployment README.md documents and fails unless the agent
# starts and answers through an installed tool. `make smoke-local` runs
# it, and so does the docker job in .github/workflows/ci.yml.
set -euo pipefail

cd "$(dirname "$0")"

export GAGENT_DATA_DIR="${GAGENT_DATA_DIR:-$PWD/data}"
export GAGENT_AGENT_UID="${GAGENT_AGENT_UID:-$(id -u)}"

readonly MESSAGES_URL="https://localhost:8080/agents/demo/messages"
readonly ATTEMPTS=60
CURL_OPTS=(--cacert tls/ca.pem --cert tls/client.pem --key tls/client-key.pem)
readonly CURL_OPTS

# Compose logs are the diagnosis when a step fails, so print them before
# tearing the deployment down.
teardown() {
  local status=$?
  if [ "$status" -ne 0 ]; then
    docker compose logs --no-color || true
  fi
  docker compose down --volumes --remove-orphans >/dev/null 2>&1 || true
  exit "$status"
}
trap teardown EXIT

echo "==> tool executables"
make -C ../.. build-tools

echo "==> throwaway mTLS material"
rm -rf tls
install -d tls
openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
  -subj "/CN=gagent-local-ca" -keyout tls/ca-key.pem -out tls/ca.pem
openssl req -newkey rsa:2048 -nodes -subj "/CN=localhost" \
  -keyout tls/server-key.pem -out tls/server.csr
openssl x509 -req -days 1 -CA tls/ca.pem -CAkey tls/ca-key.pem \
  -extfile <(printf "subjectAltName=DNS:localhost,IP:127.0.0.1") \
  -in tls/server.csr -out tls/server.pem
openssl req -newkey rsa:2048 -nodes -subj "/CN=local-tester" \
  -keyout tls/client-key.pem -out tls/client.csr
openssl x509 -req -days 1 -CA tls/ca.pem -CAkey tls/ca-key.pem \
  -in tls/client.csr -out tls/client.pem
chmod 0600 tls/*key* gagent.yaml

echo "==> deployment"
install -d -m 0777 "$GAGENT_DATA_DIR"
docker compose build
docker compose up -d

# The gateway serves only after the registry loaded the mounted tools,
# so a 202 is the agent's startup proof.
echo "==> the gateway accepts a message"
status=""
for _ in $(seq 1 "$ATTEMPTS"); do
  status=$(curl -sS -o /dev/null -w '%{http_code}' "${CURL_OPTS[@]}" \
    -X POST "$MESSAGES_URL" -d '{"content":"hello"}' 2>/dev/null || true)
  if [ "$status" = "202" ]; then
    break
  fi
  sleep 1
done
if [ "$status" != "202" ]; then
  echo "the agent did not accept a message (last status: ${status:-none})" >&2
  exit 1
fi

# The reply file exists only if the tool client spawned message_send from
# the mounted directory, which is the whole point of the check.
echo "==> the agent replies through message_send"
reply=""
for _ in $(seq 1 "$ATTEMPTS"); do
  reply=$(find "$GAGENT_DATA_DIR/outbox" -type f -name '*.json' 2>/dev/null | head -n 1 || true)
  if [ -n "$reply" ]; then
    break
  fi
  sleep 1
done
if [ -z "$reply" ]; then
  echo "no reply in $GAGENT_DATA_DIR/outbox: the agent ran no message_send" >&2
  ls -la "$GAGENT_DATA_DIR" "$GAGENT_DATA_DIR/outbox" >&2 || true
  exit 1
fi
cat "$reply"
echo
