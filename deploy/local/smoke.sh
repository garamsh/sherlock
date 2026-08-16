#!/usr/bin/env bash
# Stands up the deployment README.md documents and fails unless the agent
# starts and runs the canned turn clean: every tool call succeeds and the
# reply lands. `make smoke-local` runs it, and so does the docker job in
# .github/workflows/ci.yml.
set -euo pipefail

cd "$(dirname "$0")"

export GAGENT_DATA_DIR="${GAGENT_DATA_DIR:-$PWD/data}"
export GAGENT_AGENT_UID="${GAGENT_AGENT_UID:-$(id -u)}"

readonly AGENT_URL="https://localhost:8080/agents/demo"
readonly MESSAGES_URL="$AGENT_URL/messages"
readonly EVENTS_URL="$AGENT_URL/events"
readonly ATTEMPTS=60
CURL_OPTS=(--cacert tls/ca.pem --cert tls/client.pem --key tls/client-key.pem)
readonly CURL_OPTS

events=$(mktemp)
event_headers=$(mktemp)
subscriber=""

# Compose logs are the diagnosis when a step fails, so print them before
# tearing the deployment down.
teardown() {
  local status=$?
  if [ -n "$subscriber" ]; then
    kill "$subscriber" 2>/dev/null || true
  fi
  if [ "$status" -ne 0 ]; then
    docker compose logs --no-color || true
  fi
  docker compose down --volumes --remove-orphans >/dev/null 2>&1 || true
  rm -f "$events" "$event_headers"
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

# A failed tool call shows on the monitoring stream and nowhere else, so
# the subscription has to be live before the message that starts the
# turn. The response headers land on connect; the stream itself stays
# silent until the first event.
echo "==> the monitoring stream is subscribed"
# Retry until the gateway listens: subscribing is also what proves it is
# up, and it enqueues nothing.
subscribe() {
  for _ in $(seq 1 "$ATTEMPTS"); do
    if curl -sN -D "$event_headers" "${CURL_OPTS[@]}" "$EVENTS_URL" >>"$events" 2>/dev/null; then
      return
    fi
    sleep 1
  done
}
subscribe &
subscriber=$!
for _ in $(seq 1 "$ATTEMPTS"); do
  if grep -qiE '^HTTP/[0-9.]+ 200' "$event_headers" 2>/dev/null; then
    break
  fi
  sleep 1
done
if ! grep -qiE '^HTTP/[0-9.]+ 200' "$event_headers" 2>/dev/null; then
  echo "the agent did not serve its monitoring stream" >&2
  exit 1
fi

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

# A reply proves the turn reached message_send, not that the rest of the
# run was clean: a failed tool call is reported to the model and the loop
# carries on. The monitoring stream is where that shows.
echo "==> the canned run completes with every tool call succeeding"
for _ in $(seq 1 "$ATTEMPTS"); do
  if grep -q '"kind":"turn_finished"' "$events"; then
    break
  fi
  sleep 1
done
if ! grep -q '"kind":"turn_finished"' "$events"; then
  echo "the turn did not finish" >&2
  cat "$events" >&2
  exit 1
fi
if grep -q '"tool_failed":true' "$events"; then
  echo "a tool call in the canned run failed" >&2
  grep '"tool_failed":true' "$events" >&2
  exit 1
fi
for tool in files shell_exec message_send; do
  if ! grep -q "\"tool_name\":\"$tool\"" "$events"; then
    echo "the canned run did not call $tool" >&2
    cat "$events" >&2
    exit 1
  fi
done
echo "files, shell_exec, message_send, then the turn finished"

# Exec children carry the executor's five caps (ADR 0008, ADR 0010).
# The check runs the deployment's own shell_exec tool against the
# deployment's own Exec RPC, and reads the caps from the kernel's view
# of the child rather than from a log line.
echo "==> Exec children carry every resource cap"
limits=$(docker compose exec -T -e GAGENT_WORKSPACE_ADDR=127.0.0.1:8081 \
  agent /opt/gagent/tools/shell_exec/shell_exec \
  <<<'{"command":"cat /proc/self/limits"}' || true)
missing=0
while IFS='|' read -r label value; do
  if ! grep -qE "$label +$value +$value " <<<"$limits"; then
    echo "the Exec child does not carry $label = $value" >&2
    missing=1
  fi
done <<'CAPS'
Max cpu time|60
Max address space|536870912
Max file size|104857600
Max open files|1024
Max processes|64
CAPS
if [ "$missing" -ne 0 ]; then
  echo "$limits" >&2
  exit 1
fi
echo "cpu 60s, address space 512MiB, file size 100MiB, open files 1024, processes 64"
