#!/usr/bin/env bash
# Stands up the deployment README.md documents and fails unless its
# mTLS ingress accepts a request, the canned turn runs clean, and the
# workspace's two writers share one owner. Contributors invoke it
# directly, as does the docker job in .github/workflows/ci.yml.
set -euo pipefail

cd "$(dirname "$0")"

export SHERLOCK_DATA_DIR="${SHERLOCK_DATA_DIR:-$PWD/data}"
export SHERLOCK_AGENT_UID="${SHERLOCK_AGENT_UID:-$(id -u)}"

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
  -subj "/CN=sherlock-local-ca" -keyout tls/ca-key.pem -out tls/ca.pem
openssl req -newkey rsa:2048 -nodes -subj "/CN=localhost" \
  -keyout tls/server-key.pem -out tls/server.csr
openssl x509 -req -days 1 -CA tls/ca.pem -CAkey tls/ca-key.pem \
  -extfile <(printf "subjectAltName=DNS:localhost,IP:127.0.0.1") \
  -in tls/server.csr -out tls/server.pem
openssl req -newkey rsa:2048 -nodes -subj "/CN=local-tester" \
  -keyout tls/client-key.pem -out tls/client.csr
openssl x509 -req -days 1 -CA tls/ca.pem -CAkey tls/ca-key.pem \
  -in tls/client.csr -out tls/client.pem
chmod 0600 tls/*key* agent.yaml

# --wait holds until compose reports both services up, so the readiness
# wait is compose's rather than a retry loop here.
echo "==> deployment"
install -d "$SHERLOCK_DATA_DIR/agent" "$SHERLOCK_DATA_DIR/workspace"
docker compose build
docker compose up -d --wait

# The ingress wiring this deployment is assembled from: the certificates
# generated above, the client CA agent.yaml names, and the port compose
# publishes. A wrong-but-loadable client-ca starts clean and rejects
# every real caller, so startup cannot catch it and neither can a Go test
# holding its own certificates — only a real request over this port can.
# A 202 also re-proves the gateway serves only after the tool registry
# loaded. One request: --retry-connrefused waits for the listener to bind
# without retrying a rejected handshake, so bad certificate wiring fails
# here instead of being polled over.
echo "==> the gateway accepts an mTLS request on the published port"
status=$(curl -sS -o /dev/null -w '%{http_code}' \
  --retry 30 --retry-delay 1 --retry-connrefused \
  --cacert tls/ca.pem --cert tls/client.pem --key tls/client-key.pem \
  -X POST "https://localhost:8080/agents/demo/messages" \
  -d '{"content":"hello"}' || true)
if [ "$status" != "202" ]; then
  echo "the gateway did not accept an mTLS request (status: ${status:-none})" >&2
  exit 1
fi

# One turn, run in the agent container against the deployment's own
# mounted tools and its workspace sidecar. A failed tool call does not
# fail the turn — it goes back to the model, which is the loop working —
# so a one-shot run reports it in its exit status instead, and that
# status is this check. Nothing here parses the monitoring stream.
# The serving agent holds the writer lock on its memory store, so the
# one-shot run opens a store of its own (deployment.md, "One agent process
# on a host writes a memory store").
echo "==> the canned run completes with every tool call succeeding"
docker compose exec -T agent /sherlock agent \
  --config /etc/sherlock/config.yaml --agent-id demo --message "hello" \
  --memory-path "$SHERLOCK_DATA_DIR/agent/one-shot/memory.db"

# ADR 0008's file side: a write over the gRPC contract and a write by an
# exec'd child land under one owner, so either can pick up what the other
# left. The check writes through the deployment's own file_write tool, reads
# the owner from the workspace container, and then has an exec'd child
# modify that file and the contract overwrite the child's.
echo "==> a gRPC write lands in the workspace under the exec account"
call_tool() {
  docker compose exec -T -e SHERLOCK_WORKSPACE_ADDR=127.0.0.1:8081 \
    agent "/opt/sherlock/tools/$1/$1" || true
}
written=$(call_tool file_write <<<'{"path":"contract-write.txt","content":"written over the contract\n"}')
if ! grep -q '"bytes_written"' <<<"$written"; then
  echo "the file_write tool could not write into the workspace: ${written:-no output}" >&2
  exit 1
fi

# Either read can come back empty — the exec fails, or the file is not
# there — and an empty value compared against a uid reports a wrong owner
# for a file that was never read. Each is checked before it is compared,
# so "could not read it" and "it is the wrong uid" stay separate failures.
exec_uid=$(docker compose exec -T workspace printenv SHERLOCK_EXEC_UID | tr -d '\r\n' || true)
if [ -z "$exec_uid" ]; then
  echo "could not read SHERLOCK_EXEC_UID from the workspace container" >&2
  exit 1
fi

# The owner comes from a directory read, not from stat on the path. Write
# chowns a temporary file to the exec account and renames it into place
# (internal/workspace/files/), and where the workspace directory is a
# bind mount shared through a filesystem that caches per-path attributes
# — Docker Desktop's is one — stat of the new name keeps returning the
# pre-chown owner for as long as it is asked, while reading the directory
# returns the real one. Both agree on a native bind mount, so this is the
# read that is correct in CI and on a developer machine alike.
owner=$(docker compose exec -T workspace \
  find "$SHERLOCK_DATA_DIR/workspace" -maxdepth 1 -name contract-write.txt \
  -printf '%U' | tr -d '\r\n' || true)
if [ -z "$owner" ]; then
  echo "could not read the owner of contract-write.txt in the workspace" >&2
  docker compose exec -T workspace ls -ln "$SHERLOCK_DATA_DIR/workspace" >&2 || true
  exit 1
fi
if [ "$owner" != "$exec_uid" ]; then
  echo "the contract's file is owned by uid $owner, not the exec account $exec_uid" >&2
  docker compose exec -T workspace ls -ln "$SHERLOCK_DATA_DIR/workspace" >&2 || true
  exit 1
fi

appended=$(call_tool shell_exec <<<'{"command":"echo appended >> contract-write.txt && cat contract-write.txt","timeout_nanos":null}')
if ! grep -q 'appended' <<<"$appended"; then
  echo "an exec'd child could not modify what the contract wrote: ${appended:-no output}" >&2
  exit 1
fi

overwritten=$(call_tool file_write <<<'{"path":"question.txt","content":"6 * 7 = 42\n"}')
if ! grep -q '"bytes_written"' <<<"$overwritten"; then
  echo "the contract could not overwrite the exec child's file: ${overwritten:-no output}" >&2
  exit 1
fi
echo "both writers share the workspace: uid $exec_uid owns what either one writes"

# Exec children carry the executor's five caps (ADR 0008, ADR 0010).
# The check runs the deployment's own shell_exec tool against the
# deployment's own Exec RPC, and reads the caps from the kernel's view
# of the child rather than from a log line.
echo "==> Exec children carry every resource cap"
limits=$(docker compose exec -T -e SHERLOCK_WORKSPACE_ADDR=127.0.0.1:8081 \
  agent /opt/sherlock/tools/shell_exec/shell_exec \
  <<<'{"command":"cat /proc/self/limits"}' || true)
missing=0
while IFS='|' read -r label value; do
  if ! grep -qE "$label +$value +$value " <<<"$limits"; then
    echo "the Exec child does not carry $label = $value" >&2
    missing=1
  fi
done <<'CAPS'
Max cpu time|60
Max address space|2147483648
Max file size|104857600
Max open files|1024
Max processes|64
CAPS
if [ "$missing" -ne 0 ]; then
  echo "$limits" >&2
  exit 1
fi
echo "cpu 60s, address space 2GiB, file size 100MiB, open files 1024, processes 64"
