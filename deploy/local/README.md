# Local deployment (testing only)

Run the gagent platform in Docker. **Local testing only, on a trusted host.** The published port reaches beyond the container's loopback, so the gateway requires mTLS (ADR 0017): it serves HTTPS and verifies client certificates against a throwaway CA you generate below.

## Requirements

One variable must be exported before `docker compose up`:

| Variable | Value | Why |
|---|---|---|
| `GAGENT_DATA_DIR` | absolute host path for environment data | bind-mounted into the container at the same absolute path, so the platform sees one data root either way |

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
  https://localhost:8080/envs/demo/events &                # SSE event stream
curl --cacert tls/ca.pem --cert tls/client.pem --key tls/client-key.pem \
  -X POST https://localhost:8080/envs/demo/messages -d '{"content":"hello"}'   # returns 202
```

A client without a certificate — or with one from a different CA — is rejected at the TLS handshake.

The sample `gagent.yaml` uses the mock model provider, so the agent replies without any API key. The mock script drives a canned run of the full loop (store → `shell_exec` → `message_send` → write back); the SSE stream shows the resulting tool calls. Because the image runs as nonroot, the `shell_exec` call reports `agent_uid unavailable` and the mock's fallback path answers instead — that is the intended contract (see `docs/architecture/environment.md`). Agent replies land in `$GAGENT_DATA_DIR/demo/outbox/`.

## Cleanup

```sh
docker compose down
```

Environment data persists in `$GAGENT_DATA_DIR`; files are owned by uid 65532.
