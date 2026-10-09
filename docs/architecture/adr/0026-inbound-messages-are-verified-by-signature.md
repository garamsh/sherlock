# ADR 0026: An inbound message is accepted on the signature of an issuer the agent is configured to trust

> Status: proposed
> Date: 2026-10-09

Drafted for #993. Written against garam ADR-0101 ("garam signs every message it delivers, and the agent verifies it alone") at `garamsh/garam@1b1189b`, the revision the owner accepted. Facts about Sherlock hold at `08c32c2` (`08c32c227d17acb850858189b3b3cad9a7a7d178`).

## Context

Today the gateway believes whatever reaches `POST /agents/{agent-id}/messages`. The sender is the transport's: `callerID` returns the verified mTLS peer certificate's subject, or the empty string over plain-HTTP loopback (`internal/gateway/handler.go`, `callerID`), and a message ID is unique within that principal (ADR 0007, `gateway.md` §Current decisions). garam's adapter posts over loopback, so every message it delivers arrives under the empty principal, and the sender it names inside `garam-message.v1` is a string Sherlock never checks. Any process on the pod's loopback interface can post content naming any sender.

The owner decided that Sherlock is to be usable outside garam, hold the credentials it judges messages by, and decide for itself whether a message is for it, by verifying a signature (#993, owner's decision of 2026-10-09). garam's half is ADR-0101: an ES256 JWS in compact serialization over a claims set holding `iss`, `sub`, `aud`, `org`, `message_id`, `content_sha256`, `iat` and `exp`, signed afresh at each hand-out, travelling beside the content and never inside it, with keys published as a JWK Set. Its §What a verifier checks is the contract Sherlock must perform; its §What this does not decide leaves to Sherlock where issuers are configured, how replay state survives a restart, what refuses what, whether verification is opt-in first, and how `(iss, sub)` becomes the sender.

Facts this has to fit:

- **The ingress body is closed.** `postMessageRequest` holds `id` and `content` and is decoded with `DisallowUnknownFields`, so a new member is refused until a release names it (`internal/gateway/handler.go`).
- **The redelivery record already exists and is never shortened.** A completed message is a row of `conversation_sources` keyed by `(sender, external_id)`, compared with redacted content (`internal/memory/sqlite/conversation.go`, `RecordTurn` and `HoldsInbound`). Nothing deletes from that table: `rg -U -n -i "delete\s+from|drop\s+table" internal --type go -g '!*_test.go'`, which matches across line breaks, finds statements on `memories_active`, `conversation_mid`, `conversation_short` and `conversation_mid_fts`, and none on `conversation_sources`.
- **Stored content is redacted.** The store holds content with credentials redacted, and redelivery compares as stored (`gateway.md`, "Content is compared as stored"; `TestHoldsInbound_ComparesAsStored`).
- **The queue does not re-read a message.** A message waits in the inbox (`DefaultQueueCapacity`, 64, `internal/gateway/queue.go`) until a turn drains it, up to `MaxDrainBatch`, 8, at a time (`internal/gateway/runner.go`); `Enqueue` blocks while the inbox is full.
- **Every outbound destination is one the operator chose** (ADR 0013), and a non-loopback bind needs mTLS (ADR 0012).
- **The agent's identity is `--agent-id`, used verbatim** (ADR 0016); the garam renderers give it the GRN garam signs as `aud` (ADR-0101 §What is signed).

ADR 0012 rejected OIDC/JWT service accounts because they need an identity provider the project does not have. This decision needs none: an issuer is any signer whose key set the operator supplies.

## Decision

The gateway verifies a signature carried beside a message's content against key sets of issuers the operator configures, performs ADR-0101's checks in its order with `--agent-id` as the audience, and takes the verified `(iss, sub)` as the message's sender. The first accepting release verifies a signature when one is present, and a `require-signature` setting makes one mandatory.

### 1. Trusted issuers

- **Configured in the config file, under `issuers`.** Each entry names `issuer`, the exact `iss` value, and exactly one source for its key set: `keys-file`, a path to a JWK Set document, or `keys-url`, an `https` URL that serves one. An entry naming both, neither, an empty issuer, or an issuer another entry already names refuses startup. `issuers` is a list, so like `tools.pins` it has no environment form, and `SHERLOCK_ISSUERS` refuses startup rather than being ignored. An empty or absent list trusts no issuer.
- **An issuer is compared byte for byte.** Nothing normalises a trailing slash, case or scheme; `https://x` and `https://x/` are two issuers (ADR-0101 §Keys).
- **The set's location comes from the entry, never from the message or the issuer.** Sherlock does not derive a URL from `iss`, so no garam path appears in code, and a deployment trusting no garam configures whatever issuer it does trust the same way.
- **One cache serves both sources.** A set is read when the agent starts, held for its cache lifetime, read again when a message names a `kid` the set lacks (at most once a minute per issuer, ADR-0101 step 2), and replaced whole by each successful read, so a key that leaves the source leaves the cache at the next read. The lifetime of a fetched set is its response's `max-age`, one hour when absent; a file's is one hour. When a read fails, the agent keeps the set it holds for up to 24 hours past that set's lifetime, and after that accepts nothing from the issuer until a read succeeds (ADR-0101 §What a verifier checks).
- **At startup a file must read, and a URL need not.** An unreadable or unparseable `keys-file` refuses startup, as unreadable TLS material does. A failed first fetch does not: the issuer has no set until a fetch succeeds, and its messages are answered as unavailable (§4).
- **Rotation.** garam publishes a key at least two hours before it signs with it (ADR-0101). A fetched set meets that through `max-age` and the unknown-`kid` refetch. A file meets it if its renderer replaces the file within those two hours; the agent then picks the new key up on the first message that names it. A key withdrawn at once leaves the agent's trust at its next read, so within one cache lifetime for either source.
- **Fetching is egress the operator chose.** The agent process itself fetches a `keys-url`, carrying no conversation content and no credential, over TLS verified against the system roots. It is a destination in the operator's configuration, as the model and embeddings base URLs are, so ADR 0013's guarantee holds; ADR 0013's list of such destinations gains it in the pairing round. A deployment that cannot or will not reach the issuer, or whose issuer presents a certificate under a private root (garam's machine listener does, ADR-0101 §What this does not decide), uses `keys-file` and makes no fetch.
- **Trust in an issuer is the whole accept policy.** Any message that passes verification under a configured issuer is accepted, whatever its `sub`. `org` is not checked, because `aud` already names this agent. Authorization above that stays the orchestration layer's (ADR 0014).

Rejected:

- **`keys-url` only.** It makes every deployment a network client of its issuer and needs a TLS root the agent may not hold; garam's own renderers can hand over a file and make no fetch at all.
- **`keys-file` only.** A deployment with no reconciler cannot replace a file within the two-hour lead, so it would stop accepting at each rotation (ADR-0101 §Signs the decision was wrong). Being usable outside garam is the requirement it fails.
- **Both sources for one issuer.** Two sources disagree sometimes, and each answer to which wins is a rule nobody asked for.
- **Discovering the set from `iss`** (`<iss>/message-signing-keys`, or OpenID discovery). It puts one issuer's path in code, and lets the message steer the fetch.
- **A per-issuer TLS root option for `keys-url`.** The file covers the private-root case today; the option is added when a deployment needs a fetch under a private root.
- **A per-issuer subject allow-list.** No case asks for it, and ADR 0014 puts authorization above the platform.

### 2. Verification

The gateway performs ADR-0101's checks in its order and refuses at the first that fails:

1. **Issuer.** `iss`, read from the still-unverified payload only to choose a set, equals a configured `issuer` byte for byte. Nothing is fetched for any other value.
2. **Header.** The protected header is exactly `alg` `ES256`, `typ` `agent-message+jwt` and a `kid` that names a key in the chosen set, after at most one refetch.
3. **Signature.** It verifies under that key.
4. **Audience.** `aud` is one string equal to `--agent-id` byte for byte (ADR 0016).
5. **Time.** `exp` is later than now minus 60 seconds, and `iat` earlier than now plus 60 seconds.
6. **Content.** `content_sha256` equals the unpadded base64url SHA-256 of the UTF-8 octets of `content` as received, taken before redaction or any other transformation; and `message_id` equals the body's `id`. A body without `id` fails this check, since the message arrived under no identifier.
7. **Replay and redelivery** are §3's.

- **The digest is over what arrived, not over what is stored.** The store redacts credentials before it writes, so a digest over the stored form would refuse every message carrying one.
- **Verification is the gateway's alone.** It runs in the request, before the message is queued, and no later stage repeats it.

Rejected:

- **Audience as the organization, or a list of accepted audiences.** `--agent-id` is the agent's one identity (ADR 0016), and ADR-0101 signs one `aud` string.
- **A wider skew allowance.** ADR-0101 states 60 seconds as the whole allowance garam's validity assumes; a wider one accepts what garam considers expired.

### 3. Replay and redelivery

- **The existing record is the answer, unchanged and not shortened.** A message whose ID is queued or running in this process is answered 202 `queued`, one whose turn committed its sources 200 `completed`, either with other content 409, and the record of a committed one lasts as long as the store (§Context). ADR-0101 asks for a record that outlasts garam's whole redelivery window; one kept for the store's life does.
- **A replayed signature needs no record of its own.** Presented again within its validity, it binds the same `message_id` and digest, so the record answers it as the redelivery it is: `queued` or `completed`, never a second run beyond the at-least-once delivery already stated (ADR 0007). Once expired, check 5 refuses it.
- **The scope becomes `(iss, sub)`.** A signed message's ID is unique within the pair, which replaces the transport principal for it. The record carries the issuer beside the sender, so a pair and a transport principal never share a scope: a transport principal has an empty issuer, and a configured issuer is never empty. Rows written before the change carry the empty issuer and keep their meaning.
- **A redelivery across the switch reads as new, once, and this is accepted.** A message garam delivered unsigned committed under the empty loopback principal. If garam hands the same message out again after its adapter starts signing, the pair's scope does not hold it, and its turn runs a second time. That is the at-least-once delivery `gateway.md` already states, confined to messages committed but not yet acknowledged to garam when its adapter switched, so to the turns around one restart.
- **The switch is the adapter's, not this release's.** Under §5's default a Sherlock release that accepts signatures changes no message's scope until the adapter sends one.

Rejected:

- **A separate replay cache keyed on the signature or a `jti`.** The record already answers it, and a cache keyed on `jti` would refuse the second attempt of one message (ADR-0101 §Rejected alternatives).
- **Mapping the loopback scope across the switch** — looking up `("", id)` when `(iss, sub, id)` is unknown. It keeps a compatibility branch forever for a window of one restart, and lets any loopback poster's old ID answer for a signed sender.
- **Shortening the record to the signature's validity.** ADR-0101 forbids it: garam hands a message out again for as long as thirty days.

### 4. The ingress

- **The signature travels as the body member `signature`**, a string holding the compact JWS, beside `id` and `content`: `{"id","content","signature"}`. This is the name ADR-0101 step 2 and its adapter use.
- **Verification happens at ingress only.** A message that passes is queued and is not checked again, so one that expires while it waits in the inbox, or while `Enqueue` blocks on a full inbox, still runs.
- **Refusals.** Each is the existing problem body, `{"error":"<text>"}`:

| Condition | Status | `error` |
|---|---|---|
| The body is not the JSON object the gateway accepts, or `signature` is not a string | 400 | `invalid message body: …`, as today |
| `require-signature` is on and the body has no `signature` | 403 | `signature required` |
| A check of §2 fails | 403 | `signature rejected: <check>`, `<check>` one of `malformed`, `issuer`, `header`, `key`, `signature`, `audience`, `time`, `content`, `message id` |
| `iss` is configured, but the issuer has no usable set: never read, or past its 24-hour grace | 503 | `issuer keys unavailable` |

- **403, not 401.** HTTP 401 obliges a `WWW-Authenticate` challenge, and no HTTP authentication scheme applies to a message's signature. 403 says that this signature will not be accepted, and that a fresh one may be.
- **The refused check is named.** Every check is public, so the name tells a forger nothing, and it tells an operator clock skew from a wrong audience. A refusal is logged with the check, `iss` and `kid`, never the content (as today, `logRequests`).
- **Verification precedes the redelivery answer.** A redelivery carrying an expired or invalid signature is refused even if its message completed; garam re-signs each attempt.

Rejected:

- **An HTTP header**, such as `Authorization: Bearer`. A message is the body; a header puts half of it where proxies and relays may drop it, while ADR-0101 has every relay forward content and signature as a pair. `Authorization` would also claim to authenticate the request, which mTLS does.
- **The signature inside `content`.** ADR-0101 rules it out: a signature renewed per attempt would make each redelivery new content, answered 409.
- **A 400 for a failed check.** The body is well formed; what fails is the claim it carries.

### 5. Migration

Two options for the first accepting release:

- **Verify when present (recommended).** A signed message is verified and scoped to `(iss, sub)`. An unsigned one is accepted as today, under its transport principal. A message carrying a signature that fails is refused, never downgraded to unsigned.
- **Required.** Every message needs a signature once the release runs.

Verify-when-present is recommended:

- **The two sides ship in order.** garam's adapter sends the signature only to a runtime release that accepts it (ADR-0101 §Consequences, step 2). A required release that starts before that adapter refuses every message the deployment receives.
- **Deployments with no issuer keep working.** The development loop, the tests, and a deployment that configures no issuer post unsigned over loopback, as they do now.
- **It weakens nothing.** Stripping a signature gains a poster only today's footing, the transport principal and its scope; it cannot claim the `(iss, sub)` it stripped.

**A launcher turns it on with `require-signature: true`.** The setting resolves through the ordinary precedence — `--require-signature`, `SHERLOCK_REQUIRE_SIGNATURE`, the config file's `require-signature` — and defaults to false. With it on and no issuer configured, startup refuses, since every message would be. A launcher sets it once its adapter signs; garam's renders it beside `issuers`.

- **Unlike `--require-store`, it is not a flag only.** ADR 0025 keeps that flag off the persistent layers because the first start must create the store and later starts must not. Whether messages must be signed is a property of the deployment for its whole life, which is what the persistent layers are for.

Rejected:

- **Required whenever an issuer is configured.** It ties two decisions together: a deployment could not configure its issuer ahead of its adapter's switch, which is the order the switch needs.

### 6. The sender

- **The verified pair replaces `callerID` for a signed message, everywhere the sender is read:** the ID's scope in the queue and in the redelivery record (§3), the sender the turn shows the model (ADR 0015's envelope), and the sender stored with the source. The turn names `sub` as the sender and `iss` as who vouched for it, so the model replies to `sub` through `message_send`.
- **A message whose `sub` equals its `iss` is the issuer's own notice, and has no sender.** ADR-0101 signs a message the issuer authored itself, such as garam's cross-organization refusal (ADR-0035), with `sub` set to `iss`. The turn shows such a message as a notice from the issuer, not as a sender's message, and the turn's contract tells the model that a notice has no sender to reply to, so a model following it never sends to the issuer's URL. The rule compares the two claims and names no issuer. A notice's ID is scoped to `(iss, iss)`, like any other pair's.
- **mTLS stays transport security only.** It decides who may connect, and for an unsigned message under §5's default it is still the scope. For a signed message it decides nothing about the sender. The bind gate is unchanged: a non-loopback listener still needs mTLS (ADR 0012), because the lookup, events and status routes carry no signature.
- **The request log keeps `caller_id`** as the transport identity, and a signed message's line adds `iss` and `sub`.
- **The lookup answers the party that posted.** `GET /agents/{agent-id}/messages/{id}` carries no signature, so it is still answered by transport principal. A signed message therefore records, beside its `(iss, sub)` scope, the transport principal that posted it: in the queue while it is pending, and in the source the turn commits. A lookup under that principal answers `pending`, `completed` or `unknown` for the signed message as it does for an unsigned one. Under any other principal the signed message reads unknown. The poster recorded is the one whose post the queue accepted, and a redelivery from another principal learns where the message stands from its post's answer.
- **Where one poster's ID names more than one message, the lookup answers conservatively.** One poster can post one ID unsigned and signed, or under two issuers. The lookup then answers `completed` only when every message it names has completed, `pending` when any of them is pending, and `unknown` otherwise. A wrong `unknown` costs a redelivery, which the post's answer resolves; a wrong `completed` would lose a message.

Rejected:

- **Keeping `callerID` as the scope and recording `(iss, sub)` beside it.** Over loopback every signed message would still share the empty principal, so two issuers' equal IDs would collide.
- **Keeping the lookup transport-scoped alone, so it reads every signed message as unknown.** garam's adapter posts over loopback under the empty principal, and recovers a lost completion event through this lookup. garam's `adapter.md` §Foreign facts this rests on, at `garamsh/garam@1b1189b`, lists "`GET /agents/{agent}/messages/{id}` answers `pending`, `completed` or `unknown`", with "A lost event could not be recovered" as what breaks without it. Reading every signed message as unknown would send the adapter to redeliver a message that completed. It cannot do that once the signature it holds has expired, because only garam re-signs.
- **A signature on the lookup.** It binds no content to compare. It would be a second signed shape for a question the recorded poster already answers, and it has the same expiry problem.

## Consequences

- Sherlock trusts a message because a configured issuer signed it for this agent, not because it reached the gateway. garam is one configured issuer among any, and nothing in code names it.
- Until a launcher sets `require-signature`, an unsigned message is believed as it is today. The protection starts when the launcher says so.
- The ingress contract gains `signature`, and the adapter of the adjacent garam release must send it only to a Sherlock release that accepts it; an older Sherlock answers it 400 (`DisallowUnknownFields`).
- A deployment using `keys-url` gains one outbound destination from the agent process; one using `keys-file` gains none and owes its renderer a two-hour refresh deadline.
- The agent now refuses on time. A host clock more than 60 seconds off refuses every signed message with `signature rejected: time`.
- A message whose post outlasts its `exp` is refused; garam hands it out again with a new signature when the lease lapses (ADR-0101 §Attempts and relaying).
- `memory.db` gains two values on each source: the issuer beside the sender, and, for a signed message, the transport principal that posted it. Existing rows read as transport principals.
- garam's `adapter.md` §Foreign facts this rests on, at `1b1189b`, says a post "keys the message by the transport caller and that id". For a signed message, the key becomes `(iss, sub)` and that id. The property that fact protects still holds: two claims of one message carry the same pair and ID, so they remain one source. The lookup fact holds unchanged. garam records the new key when its adapter starts signing (ADR-0101 §Consequences, step 3).
- Implementation waits for this record's acceptance and lands in round 2 on the same branch. This round does not choose the JOSE library.
- **Records updated in place when this is implemented** (rule 2): ADR 0007, whose scope sentence names the mTLS identity; ADR 0013, whose list of operator-chosen destinations gains `keys-url`; ADR 0012, whose caller identity stops being a signed message's sender.
- **Responsibility documents paired in round 2** (rule 4): `gateway.md` (the `signature` member, verification, refusals, scope and sender, the issuer's notice, the lookup by the recorded poster); `deployment.md` (§Configuration and secrets for `issuers` and `require-signature`, §Startup for the new refusals, §Load-bearing facts for the ingress member, and the `keys-url` egress); `security.md` (message authenticity rests on issuer keys; the egress list); `memory.md` (the issuer stored beside the sender, and a signed message's poster). The index row in `README.md` lands with this record and drops `(proposed)` on acceptance.

## Signs the decision was wrong

- Launchers set `require-signature` on every start and nothing posts unsigned, which would mean the default is backwards, as ADR 0025 watches for its own flag.
- Deployments using `keys-file` stop accepting at rotations, which would mean renderers cannot meet the two-hour lead and the fetch, or a longer lead, is what they need.
- Operators ask for per-issuer TLS roots or allow-lists, which would mean an option rejected above was needed after all.
- garam's adapter reads `unknown` for a signed message whose turn completed, which would mean the recorded poster is not the principal the adapter looks up under.
- A message garam redelivers is answered 409 or runs twice outside the switch, which would mean the record, the digest point, or the scope is not where this put it.
- Refusals on `time` appear in ordinary operation, which would mean the 60-second allowance is too narrow for the hosts agents run on.
