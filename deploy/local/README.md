# Local deployment (testing only)

Run the sherlock platform in Docker — two services per the pod shape (ADR 0017): the `agent` service serves the gateway, and the `workspace` service shares the agent's network namespace (`network_mode: service:agent`) so the unauthenticated gRPC listener stays on loopback. **Local testing only, on a trusted host.** The published port reaches beyond the container's loopback, so the gateway requires mTLS (ADR 0012): it serves HTTPS and verifies client certificates against a throwaway CA you generate below.

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
| `SHERLOCK_DATA_DIR` | absolute host path for agent data | holds `agent/` (`memory.db`, its sidecars, `outbox/`), bind-mounted into the agent container only, and `workspace/`, bind-mounted into the workspace container only, each at the same absolute path as on the host |
| `SHERLOCK_AGENT_UID` | `$(id -u)` | the uid the agent container runs as, so it can read the owner-only config file and server key |

- `SHERLOCK_DATA_DIR`: any absolute host path; create `agent/` and `workspace/` under it as the host user before starting (§Run), so the agent container, running as that user, can write `agent/`. The workspace container chowns `workspace/` to uid 10001, the exec uid its Exec children run as. On Docker Desktop it must sit under a shared path (the project directory works).
- `SHERLOCK_AGENT_UID`: the gateway refuses a group- or other-readable config file or TLS key, so only their owner can read them — running the agent as that owner is what makes the mounts readable. The workspace container keeps the image's user: it needs root to drop Exec children to the exec uid.

**The two containers mount separate directories.** They exchange no file — the tools reach the workspace over gRPC — so the workspace container does not mount `agent/`, and no Exec child reaches the agent's store under any uid. What the workload requires of storage is in `docs/architecture/deployment.md` §Volumes and ownership.

## Certificates

Generate a throwaway CA, a server certificate, and a client certificate into `./tls` (gitignored; never commit real keys):

```sh
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
```

The files are valid for one day; regenerate them when they expire. The compose file mounts `./tls` read-only at `/etc/sherlock/tls`, where the sample `agent.yaml`'s `tls:` section points. The server key and the config file must stay owner-only — the gateway refuses a group/other-readable key or config at startup — which is why the agent container runs as `SHERLOCK_AGENT_UID`, the uid that owns them.

## Run

From this directory:

```sh
export SHERLOCK_DATA_DIR="$PWD/data"
export SHERLOCK_AGENT_UID="$(id -u)"
install -d "$SHERLOCK_DATA_DIR/agent" "$SHERLOCK_DATA_DIR/workspace"
make -C ../.. build-tools                                  # the tools the agent loads at startup
docker compose build
docker compose up -d
curl -N --cacert tls/ca.pem --cert tls/client.pem --key tls/client-key.pem \
  https://localhost:8080/agents/demo/events &              # SSE event stream
curl --cacert tls/ca.pem --cert tls/client.pem --key tls/client-key.pem \
  -X POST https://localhost:8080/agents/demo/messages -d '{"content":"hello"}'   # returns 202
```

`demo` in those paths is the agent ID `docker-compose.yml` passes with `--agent-id`; the agent has no default ID and answers on the one it is given (ADR 0016).

A client without a certificate — or with one from a different CA — is rejected at the TLS handshake.

`make build-tools` builds for the host platform, so on a non-Linux host build for the containers instead: `GOOS=linux make -C ../.. build-tools`.

The sample `agent.yaml` uses the mock model provider, so the agent replies without any API key. The mock script drives a canned run of the full loop: `shell_exec` writes a note into the workspace and computes the answer, `file_read` reads that note back through the gRPC contract, `message_send` replies, and the turn ends with a terminal response and saves visible messages as source evidence. The script serves one turn a process and each turn stores three visible messages, so the turn after a restart fills a four-message batch; the mock synthesizes it into a fixed summary, so a reused `data/` restarts cleanly. The SSE stream shows the resulting tool calls. The run creates what it reads, so it needs no file seeded into the workspace first. The agent holds no tool code: at startup it reads a `manifest.json` per tool from its tools directory and spawns the tool's executable on each call (ADR 0017), and the tools that need files or exec dial the workspace over the shared network namespace's loopback; the workspace serves the gRPC data plane — files and exec — so the agent itself executes nothing. The sherlock-agent image carries the first-party tools baked at `/opt/sherlock/tools/` (ADR 0021); `make build-tools` populates the host directory the compose mounts on top of that path, so a contributor's edit to `tools/<name>/<name>` or `manifest.json` is reflected on the next container restart without rebuilding the image. `SHERLOCK_TOOLS_DIR` points there either way. A missing tools directory fails startup closed, as does a missing required tool. shell_exec crosses the Exec RPC: the workspace runs the command in its own directory (`$SHERLOCK_DATA_DIR/workspace`), dropped to uid 10001 in a fresh network namespace, with exactly the capabilities the executor needs (SETUID, SETGID, CHOWN, SYS_ADMIN, KILL — see `docker-compose.yml`, which records why KILL is among them) and under the executor's five resource caps. The exec UID is a dedicated one on purpose: a container shares the host's UID space, and the child's process cap counts every process of that UID, so an account the host also uses leaves the child unable to fork. Agent replies land in `$SHERLOCK_DATA_DIR/agent/outbox/`; memory persists in `$SHERLOCK_DATA_DIR/agent/memory.db` across restarts.

## Check

`./smoke.sh` runs the whole sequence above — tools, certificates, `docker compose up` — and then asserts three things against the running deployment:

- **The mTLS ingress accepts a caller.** One `POST /agents/demo/messages` over the published port, with the client certificate generated above, must return 202. This is the deployment's wiring — these certificates, the client CA `agent.yaml` names, that port — rather than the gateway's mTLS code, which the Go tests cover with certificates of their own. A client CA that loads but matches nobody starts cleanly and rejects every real caller, so only a real request catches it.
- **The canned turn runs clean.** It runs one turn in the agent container (`sherlock agent --message`) against these mounted tools and this workspace sidecar, and takes that run's exit status as the answer. The run opens its own memory store, `$SHERLOCK_DATA_DIR/agent/one-shot/memory.db`, because the serving agent holds the writer lock on `memory.db` and refuses a second writer. A failed tool call does not end a turn — the model is told and carries on — so a one-shot run reports one in its exit status; nothing here reads the monitoring stream to find out.
- **Both writers share one owner** (ADR 0008, ADR 0010). A write over the gRPC contract and a write by an exec'd child land under the exec account, each able to pick up what the other left, and Exec children carry all five resource caps.

The `docker` job in `.github/workflows/ci.yml` runs the same script, so a change that stops this deployment from starting — or breaks its mTLS wiring, or starts a tool call failing inside it, or breaks the shared-owner contract — fails CI.

## Cleanup

```sh
docker compose down
```

Agent data persists in `$SHERLOCK_DATA_DIR` (`agent/memory.db`, `agent/outbox/`, `agent/one-shot/` once `./smoke.sh` has run, and `workspace/`); `workspace/` is owned by uid 10001 (the exec UID), `agent/` by `SHERLOCK_AGENT_UID`, which alone can read `memory.db`, its sidecars and `outbox/` (`0600` files, `0700` outbox).
