# Local deployment (testing only)

Run the gagent platform in Docker — two services per the pod shape (ADR 0018): the `agent` service serves the gateway, and the `sidecar` service shares the agent's network namespace (`network_mode: service:agent`) so the unauthenticated contract listener stays on loopback. **Local testing only, on a trusted host.** The published port reaches beyond the container's loopback, so the gateway requires mTLS (ADR 0013): it serves HTTPS and verifies client certificates against a throwaway CA you generate below.

## Requirements

One variable must be exported before `docker compose up`:

| Variable | Value | Why |
|---|---|---|
| `GAGENT_DATA_DIR` | absolute host path for agent data | bind-mounted into both containers at the same absolute path; holds `memory.db`, `outbox/`, and `workspace/` |

- `GAGENT_DATA_DIR`: any absolute host path writable by uid 65532 (the image runs as nonroot); on Docker Desktop it must sit under a shared path (the project directory works).

## Certificates

Generate a throwaway CA, a server certificate, and a client certificate into `./tls` (gitignored; never commit real keys):

```sh
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
chmod 0600 tls/*key*
```

The files are valid for one day; regenerate them when they expire. The compose file mounts `./tls` read-only at `/etc/gagent/tls`, where the sample `gagent.yaml`'s `tls:` section points. The server key must stay mode 0600 owner-only — the gateway refuses a group/other-readable key at startup. On Linux hosts the container reads the mount as uid 65532, so chown the server key to that uid if startup reports a permission error.

## Run

From this directory:

```sh
export GAGENT_DATA_DIR="$PWD/data"
install -d -m 0777 "$GAGENT_DATA_DIR"
docker compose build
docker compose up -d
curl -N --cacert tls/ca.pem --cert tls/client.pem --key tls/client-key.pem \
  https://localhost:8080/agents/demo/events &              # SSE event stream
curl --cacert tls/ca.pem --cert tls/client.pem --key tls/client-key.pem \
  -X POST https://localhost:8080/agents/demo/messages -d '{"content":"hello"}'   # returns 202
```

A client without a certificate — or with one from a different CA — is rejected at the TLS handshake.

The sample `gagent.yaml` uses the mock model provider, so the agent replies without any API key. The mock script drives a canned run of the full loop (discovered `shell_exec` → `message_send` → write back); the SSE stream shows the resulting tool calls. The agent discovers the sidecar's tools at startup over the shared network namespace's loopback. The sidecar container runs with exactly the capabilities the executor needs (SETUID, SETGID, CHOWN, SYS_ADMIN — see `docker-compose.yml`), so the privilege drop is real: `shell_exec` succeeds and runs as the agent UID inside the sidecar (see `docs/architecture/environment.md`). Agent replies land in `$GAGENT_DATA_DIR/outbox/`; memory persists in `$GAGENT_DATA_DIR/memory.db` across restarts.

## Cleanup

```sh
docker compose down
```

Agent data persists in `$GAGENT_DATA_DIR` (`memory.db`, `outbox/`, `workspace/`); files are owned by uid 65532.
