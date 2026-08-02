# Local deployment (testing only)

Run the gagent platform in Docker. **Local testing only, on a trusted host: the gateway has no authentication** — anyone who can reach the port can drive the agent.

## Requirements

One variable must be exported before `docker compose up`:

| Variable | Value | Why |
|---|---|---|
| `GAGENT_DATA_DIR` | absolute host path for environment data | bind-mounted into the container at the same absolute path, so the platform sees one data root either way |

- `GAGENT_DATA_DIR`: any absolute host path writable by uid 65532 (the image runs as nonroot); on Docker Desktop it must sit under a shared path (the project directory works).

## Run

From this directory:

```sh
export GAGENT_DATA_DIR="$PWD/data"
install -d -m 0777 "$GAGENT_DATA_DIR"
docker compose build
docker compose up -d
curl -N localhost:8080/envs/demo/events &                # SSE event stream
curl -X POST localhost:8080/envs/demo/messages -d '{"content":"hello"}'   # returns 202
```

The sample `gagent.yaml` uses the mock model provider, so the agent replies without any API key. The mock script drives a canned run of the full loop (store → `shell_exec` → `message_send` → write back); the SSE stream shows the resulting tool calls. Because the image runs as nonroot, the `shell_exec` call reports `agent_uid unavailable` and the mock's fallback path answers instead — that is the intended contract (see `docs/architecture/environment.md`). Agent replies land in `$GAGENT_DATA_DIR/demo/outbox/`.

## Cleanup

```sh
docker compose down
```

Environment data persists in `$GAGENT_DATA_DIR`; files are owned by uid 65532.
