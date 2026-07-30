# Local deployment (testing only)

Run the gagent platform in Docker against the host's Docker daemon. **Local testing only, on a trusted host: the gateway has no authentication** — anyone who can reach the port can drive the agent.

## Requirements

The gateway container runs as nonroot (uid 65532) and drives the host Docker daemon: environment containers are siblings created by that daemon, not children of the gateway. Two variables must be exported before `docker compose up`:

| Variable | Value | Why |
|---|---|---|
| `DOCKER_GID` | group ID owning the daemon socket | grants the nonroot user access to the mounted socket |
| `GAGENT_DATA_DIR` | absolute host path for environment data | the daemon resolves sibling bind-mount sources on the host, so the data root must be the same absolute path on host and in the container |

- `DOCKER_GID`: on Linux, `stat -c '%g' /var/run/docker.sock` (usually the `docker` group); on Docker Desktop, `0` (the socket is `root:root` inside the VM).
- `GAGENT_DATA_DIR`: any absolute host path writable by uid 65532; on Docker Desktop it must sit under a shared path (the project directory works).
- If the socket is not at `/var/run/docker.sock` (rootless Docker, some Docker Desktop setups), also export `DOCKER_SOCK` with its host path.

## Run

From this directory:

```sh
export DOCKER_GID=$(stat -c '%g' /var/run/docker.sock)   # Docker Desktop: export DOCKER_GID=0
export GAGENT_DATA_DIR="$PWD/data"
install -d -m 0777 "$GAGENT_DATA_DIR"
docker compose build
docker compose up -d
curl -N localhost:8080/envs/demo/events &                # SSE event stream
curl -X POST localhost:8080/envs/demo/messages -d '{"content":"hello"}'   # returns 202
```

The sample `gagent.yaml` uses the mock model provider, so the agent replies without any API key. The mock script drives a canned run of the full loop (store → `shell_exec` → `message_send` → write back); the SSE stream shows the resulting tool calls. Agent replies land in `$GAGENT_DATA_DIR/demo/outbox/`.

## Cleanup

```sh
docker compose down
```

Environment data persists in `$GAGENT_DATA_DIR`; files are owned by uid 65532 (gateway) and root (environment containers).
