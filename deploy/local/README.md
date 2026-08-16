# Local deployment (testing only)

Run the gagent platform in Docker — two services per the pod shape (ADR 0017): the `agent` service serves the gateway, and the `workspace` service shares the agent's network namespace (`network_mode: service:agent`) so the unauthenticated gRPC listener stays on loopback. **Local testing only, on a trusted host.** The published port reaches beyond the container's loopback, so the gateway requires mTLS (ADR 0013): it serves HTTPS and verifies client certificates against a throwaway CA you generate below.

## Contents
- Requirements
- Certificates
- Run
- Check
- Cleanup

## Requirements

Two variables must be exported before `docker compose up`:

| Variable | Value | Why |
|---|---|---|
| `GAGENT_DATA_DIR` | absolute host path for agent data | bind-mounted into both containers at the same absolute path; holds `memory.db`, `outbox/`, and `workspace/` |
| `GAGENT_AGENT_UID` | `$(id -u)` | the uid the agent container runs as, so it can read the owner-only config file and server key |

- `GAGENT_DATA_DIR`: any absolute host path; the workspace container runs its Exec children as uid 10001 (and chowns `workspace/` to the same uid), so the path must allow both containers. On Docker Desktop it must sit under a shared path (the project directory works).
- `GAGENT_AGENT_UID`: the gateway refuses a group- or other-readable config file or TLS key, so only their owner can read them — running the agent as that owner is what makes the mounts readable. The workspace container keeps the image's user: it needs root to drop Exec children to the exec uid.

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
chmod 0600 tls/*key* gagent.yaml
```

The files are valid for one day; regenerate them when they expire. The compose file mounts `./tls` read-only at `/etc/gagent/tls`, where the sample `gagent.yaml`'s `tls:` section points. The server key and the config file must stay owner-only — the gateway refuses a group/other-readable key or config at startup — which is why the agent container runs as `GAGENT_AGENT_UID`, the uid that owns them.

## Run

From this directory:

```sh
export GAGENT_DATA_DIR="$PWD/data"
export GAGENT_AGENT_UID="$(id -u)"
install -d -m 0777 "$GAGENT_DATA_DIR"
make -C ../.. build-tools                                  # the tools the agent loads at startup
docker compose build
docker compose up -d
curl -N --cacert tls/ca.pem --cert tls/client.pem --key tls/client-key.pem \
  https://localhost:8080/agents/demo/events &              # SSE event stream
curl --cacert tls/ca.pem --cert tls/client.pem --key tls/client-key.pem \
  -X POST https://localhost:8080/agents/demo/messages -d '{"content":"hello"}'   # returns 202
```

A client without a certificate — or with one from a different CA — is rejected at the TLS handshake.

`make build-tools` builds for the host platform, so on a non-Linux host build for the containers instead: `GOOS=linux make -C ../.. build-tools`.

The sample `gagent.yaml` uses the mock model provider, so the agent replies without any API key. The mock script drives a canned run of the full loop: `shell_exec` writes a note into the workspace and computes the answer, `files` reads that note back through the gRPC contract, `message_send` replies, and the turn ends with a memory write stating the answer at the address of the question it answers. The SSE stream shows the resulting tool calls. The run creates what it reads, so it needs no file seeded into the workspace first. The agent holds no tool code: at startup it reads a `manifest.json` per tool from its tools directory and spawns the tool's executable on each call (ADR 0017), and the tools that need files or exec dial the workspace over the shared network namespace's loopback; the workspace serves the gRPC data plane — files and exec — so the agent itself executes nothing. The agent image ships no tools — the tool set is the operator's choice (ADR 0017) — so this deployment installs the repository's first-party ones: `make build-tools` builds the executable each manifest names, the compose file mounts `tools/` read-only at `/opt/gagent/tools`, and `GAGENT_TOOLS_DIR` points there. A missing tools directory fails startup closed, as does a missing required tool. shell_exec crosses the Exec RPC: the workspace runs the command in its own directory (`$GAGENT_DATA_DIR/workspace`), dropped to uid 10001 in a fresh network namespace, with exactly the capabilities the executor needs (SETUID, SETGID, CHOWN, SYS_ADMIN — see `docker-compose.yml`) and under the executor's five resource caps. The exec UID is a dedicated one on purpose: a container shares the host's UID space, and the child's process cap counts every process of that UID, so an account the host also uses leaves the child unable to fork. Agent replies land in `$GAGENT_DATA_DIR/outbox/`; memory persists in `$GAGENT_DATA_DIR/memory.db` across restarts.

## Check

`./smoke.sh` runs the whole sequence above — tools, certificates, `docker compose up` — and then asserts three things against the running deployment:

- **The mTLS ingress accepts a caller.** One `POST /agents/demo/messages` over the published port, with the client certificate generated above, must return 202. This is the deployment's wiring — these certificates, the client CA `gagent.yaml` names, that port — rather than the gateway's mTLS code, which the Go tests cover with certificates of their own. A client CA that loads but matches nobody starts cleanly and rejects every real caller, so only a real request catches it.
- **The canned turn runs clean.** It runs one turn in the agent container (`gagent agent --message`) against these mounted tools and this workspace sidecar, and takes that run's exit status as the answer. A failed tool call does not end a turn — the model is told and carries on — so a one-shot run reports one in its exit status; nothing here reads the monitoring stream to find out.
- **Both writers share one owner** (ADR 0008, ADR 0010). A write over the gRPC contract and a write by an exec'd child land under the exec account, each able to pick up what the other left, and Exec children carry all five resource caps.

The `docker` job in `.github/workflows/ci.yml` runs the same script, so a change that stops this deployment from starting — or breaks its mTLS wiring, or starts a tool call failing inside it, or breaks the shared-owner contract — fails CI.

## Cleanup

```sh
docker compose down
```

Agent data persists in `$GAGENT_DATA_DIR` (`memory.db`, `outbox/`, `workspace/`); `workspace/` is owned by uid 10001 (the exec UID), the rest by `GAGENT_AGENT_UID`.
