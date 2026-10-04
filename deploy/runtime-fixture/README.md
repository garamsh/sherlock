# Runtime fixture

The real-runtime acceptance fixture (#937). It starts pinned images on fresh storage, runs them against the real model, and checks identity, the task's effect, the durable reply, restart continuity, replay, the single-writer refusal and the snapshot boundary (`docs/architecture/deployment.md` §Snapshot and restore). The acceptance sequence is `runFixture` in `tests/runtime_fixture_test.go`, and both variants below run it.

## Files

- `docker-compose.yml`: compose project `sherlock-937`. It runs text-embeddings-inference (`BAAI/bge-base-en-v1.5`, 1g) on `127.0.0.1:18938`, the agent (512m) with its gateway on `127.0.0.1:18937`, and the workspace (512m) in the agent's network namespace. Images are `sherlock-937/agent:<tag>` and `sherlock-937/workspace:<tag>`. Every path and value comes from a required `FIXTURE_*` variable, which the compose test sets.
- `agent.yaml`: the agent config. It has a fixed `revision`, mTLS, MiniMax-M2 through `model.api-key-env: MINIMAX_KEY`, and the TEI embeddings endpoint. The agent always starts with `--strict-config`, so every key in this file has to be one a setting reads.
- `snapshot.sh`: the copy, verify and restore procedure that `deployment.md` §Snapshot and restore describes. It needs bash, coreutils, util-linux `flock` and `python3`.

## Running it

**Offline.** `TestRuntimeFixture_Offline` runs the built binary on the host with the mock provider. It uses the same `agent.yaml` under `--strict-config` and two mTLS client identities. It needs no container and no model, and `make test` runs it. The mock answers one turn per process and never fills a four-message batch, so this variant asserts that the note and episodes are empty and leaves their read-back to the real run.

**Real model.** `TestRuntimeFixture_Compose` runs only when `SHERLOCK_FIXTURE_COMPOSE=1`. Build both images first from the commit under test, so that what is built is that commit's tree. Name each image by the `--tag` given here, label it with the full commit, and leave attestations off (`deployment.md` §Images and tags):

```sh
git archive <commit> | tar -x -C <empty dir>
BUILDX_NO_DEFAULT_ATTESTATIONS=1 docker build -f build/agent.Dockerfile \
  --build-arg REVISION=<full commit> -t sherlock-937/agent:<short commit> <dir>
BUILDX_NO_DEFAULT_ATTESTATIONS=1 docker build -f build/workspace.Dockerfile \
  --build-arg REVISION=<full commit> -t sherlock-937/workspace:<short commit> <dir>
```

The run reads these variables:

| Variable | Value |
|---|---|
| `DOCKER_HOST` | the daemon the images are on |
| `SHERLOCK_FIXTURE_STORAGE` | a path that does not exist yet; the run creates it and keeps all of its state there |
| `SHERLOCK_FIXTURE_IMAGE_TAG` | the `<short commit>` the images are tagged with |
| `SHERLOCK_FIXTURE_AGENT_ENV` | an owner-only file holding the single line `MINIMAX_KEY=…`; compose reads it into the agent container's environment and nowhere else |
| `SHERLOCK_FIXTURE_METER_LISTEN` | the exact `<ip>:<port>` on the host the meter binds; an unspecified address is refused, and it answers only connections from that address and from loopback |
| `SHERLOCK_FIXTURE_METER_URL` | the meter as the agent container reaches it, including the `/v1` path |
| `SHERLOCK_FIXTURE_MAX_CALLS` | optional: the call ceiling, 150 by default |
| `SHERLOCK_FIXTURE_EVIDENCE` | optional: where to write the run's evidence record |
| `SHERLOCK_FIXTURE_METER_LOG` | optional: a file the meter appends its log lines to |

```sh
SHERLOCK_FIXTURE_COMPOSE=1 go test ./tests/ -run TestRuntimeFixture_Compose -count=1 -v -timeout 3h
```

**The meter.** It sits between the agent and the model endpoint that `agent.yaml` names, and counts calls and tokens from each answer's `usage`. It runs inside the test process and only for as long as the test does. The key transits it in memory: the meter forwards the agent's own `Authorization` header, keeps no copy, and never enters the key into the test's environment. It logs only each call's method, path, status, latency and usage, and the source address of each refused connection. `TestFixtureMeter_ForwardsTheKeyAndWritesItNowhere` sends a sentinel key through every path that logs or answers and checks that it appears in none of them. On HTTP 429 with MiniMax code 2056, which means the subscription's usage limit, it pauses: every later call is refused without being forwarded, nothing is retried and no credit is bought, and the test fails with that reason.

## Evidence and cleanup

The evidence record holds identifiers, hashes, statuses and exit codes, and no message or memory content. It covers the three status answers and their generations, the completed lookups, the second writer's refusal, the refused copy of a held store, replay and the 409, sender scope, the snapshot manifest, `verify`'s reports for the snapshot and the restored copy, the follow-up's recalled IDs, and the meter's count.

When the run ends, the compose test brings the project down, which keeps the images. The storage stays where `SHERLOCK_FIXTURE_STORAGE` put it, for inspection. The workspace directory belongs to the exec uid inside the daemon's user namespace, so on a rootless daemon it is removed from a container of the workspace image, not from the host.
