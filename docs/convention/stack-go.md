# Go — Architecture & Style Conventions

> How a Go module is laid out and where each kind of code belongs:
> directories, naming, errors, logging, comments, tests, imports.
>
> Checked against Go 1.21. A claim below that names no version holds
> for it.

## Contents
- 0. Folder & file naming — strict
- 1. Directory layout
- 2. Module / package boundary
- 3. Naming
- 4. Error handling
- 5. Logging & observability
- 6. Comments & docs
- 7. Testing
- 8. Imports & dependencies
- 9. Verification commands

## 0. Folder & file naming — strict

**Banned at any level:** `model.go`.

Files named for a concept: `password_hashing.go`, `format_currency.go`.

## 1. Directory layout

See §0 for the banned-name list.

### Layout A — small service (default)

- `cmd/<binary>/main.go` — the binary's entry point.
- `internal/<domain>/<domain>.go` — the aggregate: domain types, DTOs
  and sentinel errors (`User`, `CreateUserInput`, `ErrUserNotFound`).
- `internal/<domain>/service.go` — the contract: `type Service
  interface { ... }`, unexported `type service struct { ... }`,
  `NewService(...) Service`, and the default dependency interfaces
  (`Repository`, `Mailer`, …).
- `internal/<domain>/<verb>.go` — one file per verb (`create.go`,
  `update.go`, `query.go`); method bodies split by responsibility.
- `internal/<domain>/service_test.go` — the service's tests (§7).
- `internal/<domain>/postgres.go`, `internal/<domain>/memory.go` —
  concrete implementations of the dependency interfaces; the
  in-memory one serves tests and dev.
- `internal/<crosscutting>/` — a concern extracted below the domains,
  named by what it is.
- `go.mod`, `go.sum` — module root.

**Rules for `internal/<domain>/`:**

- Split into `<verb>.go` only when the service has more than one
  verb. A single-method service lives entirely in `service.go`.
- Promote a dependency interface into
  `internal/<domain>/repository.go` (same package) when it grows and
  **other domain files** need it.
- Concrete impls sit where the interface is declared: `postgres.go`,
  `memory.go` and `<impl>/` live inside `internal/<domain>/`.

**Single file vs package** for an impl:

- **Single file** (`postgres.go`, `memory.go`) when impl ≤ ~300 LoC
  and has no private helpers worth isolating.
- **Package** (`postgres/`, `memory/`) when impl > ~300 LoC or owns
  private helpers / connection-pool / per-SQL constants.

### Layout B — domain-rich service (5k–30k LoC)

Layout A's shape, with verb files grouped by responsibility and impls
promoted to packages:

- `internal/<domain>/lifecycle.go`, `internal/<domain>/billing.go`,
  `internal/<domain>/permissions.go` — method bodies grouped by
  responsibility (create/activate/deactivate, charge/refund,
  authorize/deny).
- `internal/<domain>/postgres/postgres.go`,
  `internal/<domain>/postgres/queries.go` — impl too big for one
  file.
- `internal/<domain>/memory/memory.go` — the in-memory impl, promoted
  the same way.
- Every other domain repeats the shape.

`service.go` is optional: a domain that is only types and persistence
has none. It arrives with business behaviour that coordinates multiple
dependencies.

A cross-cutting concern moves to its own `internal/<thing>/` package
past ~300 LoC.

### Layout C — library

- `<pkg>.go`, `<pkg>_test.go`, `go.mod`, `README.md` — all at the
  module root. `command.go`, `args.go` — one file per thing it owns.

### Root-level files

A root-level file exists when the module has that concern, and it is
named after the concern:

- `errors.go` — sentinels shared across domains (§4).
- `config.go` — the module's configuration type and its loading.
- `logger.go` — only when logger setup goes past `slog.Default()`
  (§5). A project that calls `slog.Default()` directly has no
  root-level logger file.
- `httpserver.go` — server construction and route wiring, when the
  module serves HTTP.

Sizing, for each of them:

- **Single file (`errors.go`, `logger.go`, `config.go`, …)** when ≤
  ~300 LoC. Filename = what's inside.
- **Promotion to `internal/<thing>/`** when > ~300 LoC or owns private
  helpers. Folder name describes what's inside
  (`internal/logger/`, `internal/httpserver/`, `internal/config/`);
  the §0 banned-name list applies.

Multiple root-level files are fine: `errors.go` + `logger.go` +
`config.go`, each named after its concern.

### Project envelope

- `cmd/<binary>/main.go` is the **only place** that constructs concrete types and passes them to interfaces. Keep it thin.
- `internal/` is enforced by the Go toolchain. Use it for everything not explicitly public.
- `pkg/` is for code other modules import. Most services don't need it.

## 2. Module / package boundary

§1 owns the per-domain file inventory. This section states which Go
file declares and which depends.

- `internal/<domain>/<domain>.go` and `internal/<domain>/service.go`
  declare the types, sentinels, `Service` interface and dependency
  interfaces the rest of the domain is written against.
- `internal/<domain>/<verb>.go` and the impls
  (`internal/<domain>/postgres.go`, or `internal/<domain>/postgres/`
  past ~300 LoC) depend on those declarations.
- **The implementation lives with the interface it satisfies.**
  `postgres.go`, `memory.go` and `<impl>/` are in the same package, or
  a sub-package of it, as `service.go`.
- Go rejects an import cycle at compile time but does not detect a
  `type → struct → type` cycle. Those are found by reading.

## 3. Naming

- **Packages:** single word, lowercase, no underscores. Singular for
  one kind of thing (`user`, `order`); pluralize only for genuine
  collections (`errors`, `flags`).
- **Types:** `MixedCaps`, no underscores (`UserService`).
- **Interfaces:** the behaviour required, in the agent form of its
  verb (`Storer`, `Mailer`, `Validator`). A dependency that names a
  thing rather than an action keeps the thing's name — `Model`,
  `Session`, `Repository` — and most of them do, so the suffix is
  what a verb takes and not the default: a `Model` does not become a
  `Modeller`. Where the name would also be one of its own domain's
  verbs, the agent form breaks the tie, because the verb already owns
  `<verb>.go` (§1) and the two cannot share it.
- **Functions / methods:** `MixedCaps`, verb-noun (`GetUser`,
  `ParseToken`).
- **Constants:** `MixedCaps` (not `MAX_SIZE`). Group in `const ( ... )`.
- **Variables:** short in small scopes, `MixedCaps` for package-level.
- **Acronyms:** all-caps for the common form, consistent case
  (`HTTPClient`, `URLParser`, `ID`).
- **Receivers:** short, consistent across methods of the same type
  (`s *Service`).
- **Initialisms:** `URL`, `ID`, `HTTP`, `JSON`, `XML`, `API`, `SQL` —
  always uppercase or lowercase, never mixed.

## 4. Error handling

- **An adapter translates before it returns.** `postgres.go` turns
  `pgx.ErrNoRows` into `ErrUserNotFound`; the driver's error does not
  leave the file that imports the driver. The errors an
  implementation returns are part of the interface declared in
  `service.go`, so one that leaks its library's errors is not one
  `memory.go` can stand in for.
- **Sentinel** when the caller branches on which failure and the
  failure carries no data: `var ErrNotFound = errors.New("not
  found")`, read with `errors.Is`. Domain sentinels in `<domain>.go`
  (`internal/user/user.go` for `ErrUserNotFound`); shared sentinels
  in `errors.go`.
- **Typed** when the caller needs data out of the failure: a struct with `Error()`, read with `errors.As`, and `Is(target error) bool` or `Unwrap() error` where it wraps another.
- **Opaque** when the caller has no business branching. A sentinel or a type a caller can match on is API you have to keep working; where nothing needs to branch, publish neither.
- **`%w` publishes the error it wraps.** A caller can reach through it with `errors.Is` and `errors.As`, so replacing what is inside breaks them later. Wrap with `%w` where a caller is meant to branch on the wrapped error, `%v` where it is not, `errors.Join` where the caller needs all of several.
- Add context at a boundary — network, IO, an external call — not on every line: `fmt.Errorf("create user: %w", err)`.
- **Each transport translates in one place.** Whatever implements a transport owns the mapping from domain error to that transport's codes — `httpserver.go` answering 404 to `ErrUserNotFound`, or the type satisfying a generated gRPC service — and nothing else names one. A second transport gets its own translator, not a share of the first. A domain outlives the transport it is served over, and a mapping spread across handlers gives one sentinel several codes.
- **Don't log and return.** Return, and let the outermost boundary log once: the transport translator where the module has one, `main` where it has none. Logging on the way up prints one failure several times, some lines carrying the request's `trace_id` and some not.
- **A panic does not cross a package boundary.** A violated invariant may panic — recovering from one hides the bug that caused it — but that ends the program rather than answering the caller. An HTTP server still holds a recover middleware: `net/http` recovers a handler's panic and logs the stack itself, but aborts the response instead of answering — the client gets a closed connection, or an HTTP/2 `RST_STREAM` — so the middleware exists to reply 500.

## 5. Logging & observability

- Stdlib `log/slog` (Go 1.21+). Prefer it over `log` and third-party
  loggers for new code.
- Levels: `Debug`, `Info`, `Warn`, `Error`.
- Required fields for HTTP / RPC handlers: `trace_id` (from
  OpenTelemetry's request context — falls back to a
  locally-generated UUID if no span exists) and `user_id`
  / `account_id` when the principal is known. The older
  `request_id` field still appears in many log pipelines but is
  really a stand-in for `trace_id`; new code emits the latter.
- Emit the operation name and key parameters as well.
- **Never** log secrets, passwords, tokens, full request bodies that
  may contain PII.
- Metrics: `prometheus/client_golang`. Define the registry once.
- Tracing: OpenTelemetry (`go.opentelemetry.io/otel`).

## 6. Comments & docs

This section states the Go comment form only.

- Doc comments begin with the name being declared.
- Package comment in `doc.go` (one short sentence — e.g.
  `// Package auth provides ...`) is conventional and surfaces on
  pkg.go.dev. Long descriptions belong in `service.go` as a doc
  comment on the service struct.

## 7. Testing

This section states the Go test placement, tooling and test-name
form.

**Runner:** stdlib `testing`, files end with `_test.go`.
**Assertions:** `testify/assert` + `testify/require` (default unless
project says otherwise).

**Organization:**

- **External tests** (`package user_test`): next to source. Reach the
  unit under test through its exported API. **Default to this.**
- **Internal tests** (`package user`): same directory and package as
  the code under test. Use only when you genuinely need a white-box
  seam (uncommon).
- **`tests/` at module root:** integration / E2E tests that wire
  multiple domains. Separate binary.
- **In-process integration test client:** `httptest.NewServer`.
- **E2E tooling:** the binary under test is what `go build` produces;
  real services come from containers
  (`github.com/testcontainers/testcontainers-go`).

Within `internal/<domain>/`:

- Tests for `<group>.go` live in `<group>_test.go`, whichever package
  they declare. Where one group needs both an external and an internal
  file, the plain name stays with the external one and the internal one
  is `<group>_internal_test.go`. A package whose only test file for a
  group is internal keeps the plain name.
- Table-driven subtests: `tests := []struct{ name string; ... }{...}`,
  run through `t.Run("case name", ...)`.
- Test names: `TestFunctionName` or `TestFunctionName_Scenario`.
- Benchmarks: `func BenchmarkXxx(b *testing.B)`.

### Test doubles

Write the double by hand, in the package whose test uses it. No mock
generator is configured and none is required.

Most doubles here are stateful fakes — a map, a slice or a channel the
test reads back afterwards — which is a different thing from the
expectation-based mock a generator emits. The rest are one-method
stubs, shorter than the directive that would generate them. A generator
modelling the wrong idea costs more than writing each double out.

Nothing has to keep a double in sync with its interface: one that stops
satisfying it fails to compile.

## 8. Imports & dependencies

Three groups, separated by blank lines:

1. Standard library
2. Third-party
3. Internal module

`goimports -local <module path>` produces the third group. Plain
`goimports` emits two — standard library and everything else — so the
`-local` flag is what separates the internal module.
Use `go mod tidy` after every change. Don't commit `go.sum` updates
you don't recognize. `go vet ./...`. `golangci-lint run` if
configured.

## 9. Verification commands

These are the commands that sit behind the project's entry-point
names.

`go test` prints only `ok <pkg>` for a package that passes and discards
the rest of its output; the full output appears on failure, or under
`-v`. So anything a test binary prints about what it covered is missing
from exactly the run that needed it. An entry point that has something
to report prints it itself, rather than leaving it to the tool it
invokes.

The same holds wherever a tool reports its findings and not its
coverage. `gofmt -l .` names the files that are unformatted and never
how many it read, so a run that walked nothing prints what a clean run
prints; the count comes from the entry point, which walks the same set
the tool does and states how it derived the number.

| Task | Command |
|------|---------|
| Build | `go build ./...` |
| Test | `go test ./...` |
| Test (one) | `go test -run TestName ./pkg/...` |
| Vet | `go vet ./...` |
| Format check | `gofmt -l .` |
| Format apply | `gofmt -w .` |
| Module tidy | `go mod tidy` |
| Lint (if configured) | `golangci-lint run` |
| Coverage | `go test -cover ./...` |

Every tool the module invokes is built with a toolchain at least the
module's `go` directive. `go install tool@version` and `go run
tool@version` take the toolchain from the tool's own `go.mod`, not from
this module, so a tool whose directive is older yields a binary that
refuses to load the module's packages, fatally and at package-load
time. Set that floor once for the module rather than per tool:
`GOTOOLCHAIN` with an `+auto` suffix sets a minimum without stopping a
tool that needs more.

A task runner (Taskfile, Mage, Make) wraps these under the entry-point
names; the underlying go commands stay the same.

