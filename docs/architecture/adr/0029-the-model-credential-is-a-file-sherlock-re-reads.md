# ADR 0029: The model credential is a file Sherlock re-reads

> Status: accepted
> Date: 2026-10-28

Drafted for #1161, under the owner direction recorded on #1162. The per-turn identifier #1161 also asked for is not decided here; it is #1166's. Facts about Sherlock hold at `dev` `90e403add20b670fae1794f2e3e7b02961581710`. Facts about garam hold at garamsh/garam `25f2b1fabb055c373cd46e84fd40639ee542ac04`, file `docs/architecture/ai-gateway.md`.

## Context

- **garam's agents reach models only through its AI gateway**, which speaks the chat-completions protocol and authenticates an agent with a short-lived token the platform mints for it (garam ADR-0126, ADR-0100; `ai-gateway.md` §Current decisions, "Authentication"). The token expires and is replaced while the agent runs.
- **Sherlock read its model key once.** `cmd/sherlock/model_provider.go:24` and `:41` read `os.Getenv(cfg.APIKeyEnv)` when the client is built and pass it to `openai.New` / `anthropic.New` as a fixed `APIKey`. A token that expires mid-session is never read again.
- **An environment variable cannot change under a running process**, so the token needs another channel. #1161 named two: a file the platform rewrites and Sherlock re-reads, or a call to the garam adapter beside the agent.
- **The credential must stay out of the execution domain** (ADR 0003): out of the workspace, its Exec children and the conversation.
- **The gateway is platform-neutral** (ADR 0007), and so is everything Sherlock sends a platform.

## Decision

The model's API key can come from a file that the platform rewrites. Sherlock reads that file for every model request, and when a request is rejected as unauthenticated it reads the file once more and retries once. `model.api-key-env` keeps working unchanged.

### 1. The credential source is a file

- **`model.api-key-file` names the file.** When it is set, the file is the key's only source and `model.api-key-env` is not read. A config that names both, in the file or through `SHERLOCK_MODEL_API_KEY_ENV`, fails at startup, because otherwise the variable the operator named would go unread without a word. `api-key-env`'s default, `OPENAI_API_KEY`, does not count as naming it.
- **The file holds the key and nothing else.** Surrounding whitespace is trimmed, and a file that is empty after trimming is unreadable.
- **It is read for every request**, so a key the platform rewrote reaches the next request with no restart and no watch. The read is of one small local file, made beside a network round trip to the model. The platform should replace the file atomically, by renaming a new file over the old one, so a read never sees a partial key.
- **It is read once at startup too.** A path the process cannot read, or an empty file, fails the start, as an unset `api-key-env` variable does (ADR 0004). This catches a wrong path while the operator is still watching.
- **A file that becomes unreadable later fails the model call, not the agent.** The error names the setting and the path (`model.api-key-file <path>`). The turn fails as any failed model call fails it. The process keeps running, and the next request reads the file again.
- **The file is the platform's to mount into the agent container only.** Nothing in Sherlock reads it into the workspace, an Exec child or the conversation, so ADR 0003's absence holds as long as the deployment never mounts it into the workspace container. It is not mode-checked: it sits in the agent's own environment, beside the variables every installed tool already inherits (`security.md` §Current decisions, "Tool trust posture").
- **Only the chat model reads it.** The embedder keeps `embedding.api-key-env` and a fixed key. #1161 asks for the chat model, and garam ADR-0126 decides a gateway for chat completions only (#1162).

### 2. A rejected request reads the file once more and retries once

- **"Rejected as unauthenticated" is HTTP 401.** 403 means the endpoint knew the caller and refused it, and a newer key does not change that, so it is not retried.
- **On a 401 the client reads the file again and sends the same request once more**. A token rewritten between the first read and the rejection recovers the call.
- **A second 401 fails the call.** The error is the provider's HTTP error, wrapped so that it names the source and still matches `errors.As` for the provider's `APIError`. It never carries the key.
- **A fixed `api-key-env` key is not retried.** Reading it again returns the same key, so a second request could only be refused again. Its behaviour is exactly what it was before this decision.

## Rejected alternatives

- **A call to garam's adapter beside the agent.** The adapter already holds the agent's activation token (garamsh/garam `docs/architecture/adapter.md` §Activation and runtime status), so it could hand out the gateway token too. But Sherlock would then depend on one platform's adapter API, against ADR 0007. A file is a contract any platform can meet. It is the pattern of a Kubernetes projected service-account token, which #1161's decisions name.
- **Re-reading only on a 401, with the key cached in between.** Every rotation would cost one rejected request, and a request made after the file changed would still carry the old key until the endpoint refused it.
- **Watching the file for changes.** It adds a goroutine and a notification dependency to save a read the request already pays for, and it still needs the 401 retry for a token that expires before its replacement is written.
- **Retrying a 401 more than once, or with backoff.** A second read of an unchanged file returns the same token, so a further retry is the same request refused again. One retry covers the rotation race, which is all a retry can fix.
- **Keeping `api-key-env` and restarting the agent on every rotation.** A restart drains the turn in flight first (ADR 0022), so the token's lifetime, not the work, would decide when the agent stops.

## Consequences

- An operator points `model.base-url` at a gateway, sets `model.api-key-file` to the path the platform writes, and the agent survives token rotation without a restart. Every existing `api-key-env` configuration runs as before.
- garam owes Sherlock the file and its rewrite, in the agent container only. That was requested on garamsh/garam#1704 and is garam's to build.
- Each model request performs one extra file read, and a rotation that races a request costs one extra request.
- The provider clients gain a `Credential` setting beside `APIKey`. The two are exclusive, and setting both fails at construction.
- **Records updated in place with this one** (`docs/architecture/README.md` §Rules, "ADR records are living"): ADR 0003's "Explicit channels only", which now admits an operator-named file in the agent's environment beside the process environment.
- **Responsibility documents paired with this record** (`docs/architecture/README.md` §Rules, "Decisions land in pairs"): `agent.md` (model access: the key file and the retry), `security.md` (where the model credential may live), `deployment.md` (how the key reaches the process, and the startup failures), `packages.md` (the `Credential` interface each provider client declares).

## Signs the decision was wrong

- Platforms other than garam deliver short-lived credentials by a call rather than a file, so every one of them needs a sidecar that writes the file. That would mean the adapter-call shape was the common one.
- Calls fail with 401 even though a valid token is on disk, because the platform rewrites the file in place and a read sees half a token. That would mean the atomic-replacement obligation is not met in practice, and the reader has to defend against it.
- The per-request read shows up in latency, or the file sits on a filesystem where a read can block. That would mean a cached read with a refresh interval was worth its staleness.
