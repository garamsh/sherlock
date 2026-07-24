# Go — Architecture & Style Conventions

> Defaults for Go projects. Defaults, not laws — if a project's
> existing code clearly diverges, follow the code and note the
> divergence in PLAN.md §8.

## Contents
- 0. Folder & file naming — strict
- 1. Directory Layout
- 2. Module / Package Boundary
- 3. Naming
- 4. Error Handling
- 5. Logging & Observability
- 6. Comments & Docs
- 7. Testing
- 8. Imports & Dependencies
- 9. Verification Commands
- 10. Hierarchy (stack-specific MUST/NEVER)
- 11. Ecosystem versions (verify live)
- 12. Sources (URL index)

## 0. Folder & file naming — strict

Names describe **what they own**. **Banned at any level:**
`model.go`, `utils/`, `helpers/`, `common/`, `ext/`, `adapter/`,
`driver/`, `platform/`, `infra/`, `kit/`, `repo/`.

If two helpers share a concept, **give the concept a name**:
`password_hashing.go`, `format_currency.go`. The path tells you what
the file does.

(Standard practice in the Go community: package names after the
concept they own, not the role they play — `auth` over
`auth_utils`. `spf13/cobra` and `go-kit/kit` both follow this.)

## 1. Directory Layout

**Two non-negotiables:** shallow structure and named-what-it-is.
See §0 for the banned-name list (`model.go`, `utils/`, `helpers/`,
`common/`, `ext/`, `adapter/`, `driver/`, `platform/`, `infra/`,
`kit/`, `repo/`).

### Layout A — small service (default)

```
<module>/
├── cmd/<binary>/main.go                # thin: constructs concrete types
├── internal/
│   ├── <domain>/
│   │   ├── <domain>.go                  # types, DTOs, sentinel errors
│   │   ├── service.go                   # Service interface + *service struct + NewService + dep interfaces
│   │   ├── create.go                    # verb split when >1 method
│   │   ├── update.go
│   │   ├── query.go
│   │   ├── service_test.go
│   │   ├── postgres.go                  # ≤ ~200 LoC → single file
│   │   └── memory.go                    # in-memory impl (tests/dev)
│   └── <crosscutting>/                  # ≥3 domains share, named by what it is
├── go.mod
└── go.sum
```

**Three rules for `internal/<domain>/`:**

- **`<domain>.go`** owns the aggregate: `User` struct,
  `CreateUserInput` DTO, `ErrUserNotFound` sentinel.
- **`service.go`** owns the contract: `type Service interface { ... }`,
  unexported `type service struct { ... }`, `NewService(...) Service`,
  default dependency interfaces (`Repository`, `Mailer`, …). If a
  dependency interface grows and **other domain files** need it,
  promote it into `<domain>/repository.go` (same package).
- **`<verb>.go`** splits method bodies by responsibility. Only split
  when the service has more than one verb — single-method services live
  entirely in `service.go`.

Concrete impls sit where the interface is declared: `postgres.go` /
`memory.go` / `<impl>/` live inside `internal/<domain>/`.

**Single file vs package** for an impl:

- **Single file** (`postgres.go`, `memory.go`) when impl ≤ ~200 LoC
  and has no private helpers worth isolating. Filename = vendor.
- **Package** (`postgres/`, `memory/`) when impl > ~300 LoC or owns
  private helpers / connection-pool / per-SQL constants. Folder name
  = vendor.

### Layout B — domain-rich service (5k–30k LoC)

```
<module>/
├── cmd/<binary>/main.go
├── internal/
│   ├── <domain>/
│   │   ├── <domain>.go
│   │   ├── service.go
│   │   ├── lifecycle.go                 # create/activate/deactivate
│   │   ├── billing.go                   # charge/refund
│   │   ├── permissions.go               # authorize/deny
│   │   ├── postgres/                    # too big for one file
│   │   │   ├── postgres.go
│   │   │   └── queries.go
│   │   ├── memory/memory.go
│   │   └── service_test.go
│   └── ...                              # other domains, same shape
├── go.mod
└── go.sum
```

**Not every domain has a service.** Pure-domain-model projects (only
types + persistence) can skip `service.go` entirely. Add `service.go`
(and verb-splits) only when there's business behaviour that coordinates
multiple dependencies.

Promote a cross-cutting concern to its own `internal/<thing>/` package
**only when** that file is past ~500 LoC **or** ≥3 domains import it.
Never pre-emptively create `internal/infra/`, `internal/platform/`,
`internal/common/`, `internal/kit/`.

### Layout C — library

```
<module>/
├── <pkg>.go
├── <pkg>_test.go
├── go.mod
└── README.md
```

Libraries stay flat. `spf13/cobra` style — files named after what
they own (`command.go`, `args.go`).

### Root-level files (decide per project)

Whether `errors.go`, `logger.go`, `config.go`, `httpserver.go` live at
the module root is **a per-project decision**. `log/slog` is stdlib
since Go 1.21, so a small project may want **no root-level logger
file at all** (`slog.Default()` directly). Ask the user which of
these files the project wants.

When the decision is "yes, we need this at the root":

- **Single file (`errors.go`, `logger.go`, `config.go`, …)** when ≤
  ~200 LoC. Filename = what's inside.
- **Promotion to `internal/<thing>/`** when > ~300 LoC or owns private
  helpers. Folder name describes what's inside
  (`internal/logger/`, `internal/httpserver/`, `internal/config/`).
  Banned: `internal/platform/`, `internal/infra/`, `internal/common/`,
  `internal/kit/`.

Multiple root-level files are fine: `errors.go` + `logger.go` +
`config.go`, each named after its concern.

### Project envelope

- `cmd/<binary>/main.go` is the **only place** that constructs concrete types and passes them to interfaces. Keep it thin.
- `internal/` is enforced by the Go toolchain. Use it for everything not explicitly public.
- `pkg/` is for code other modules import. Most services don't need it.

## 2. Module / Package Boundary

A **top-level domain** owns the public contract for everything it produces:

- `<domain>.go` — domain types and sentinels.
- `service.go` — `type Service interface { ... }`, unexported `type
  service struct { ... }`, `NewService(...) Service`, default
  dependency interfaces.
- `<verb>.go` (`create.go`, `update.go`, `query.go`) — method bodies,
  split by responsibility when the service has > 1 verb.
- `<impl>.go` (or `<impl>/`) — concrete implementations of the
  dependency interfaces. Same package. Filename/folder = vendor
  (`postgres.go`, `memory.go`, `stripe.go`, …). Never `ext/`,
  `adapter/`, `driver/`, `repo/`.

### Dependency direction

```
internal/<domain>/<domain>.go     ← types + sentinels
internal/<domain>/service.go      ← Service interface + *service struct + dep interfaces
         │
         ▼
internal/<domain>/<verb>.go       ← (s *service) Create/Update/Query
internal/<domain>/postgres.go     ← implements Repository
(or internal/<domain>/postgres/   ← when impl > ~300 LoC)
         │
         ▼
mocks/                            ← generated by mockery, §7
```

**Three rules:**

1. **Implementation satisfies interface in `service.go`.**
   `postgres.go`, `memory.go`, `<impl>/` are inside the same package
   (or sub-package) as `service.go`.
2. **Cross-domain interfaces are importable directly.** A consumer
   (`order`) imports the producer's interface (`billing.Charger`).
   Whether to call through the interface or import a concrete sibling
   type is a developer judgement call — the compiler doesn't enforce
   either way.
3. **Dependency cycles are the developer's responsibility.** Go's
   import graph is a DAG the compiler compiles, but `type -> struct
   -> type` cycles are not detected. Watch for `A → B → A` in your
   code; restructure or accept the cycle knowingly.

### When a new package is justified

Create a new top-level domain when:

- The concept has its own lifecycle and identity.
- Other domains need to depend on it.
- It owns a stable interface decoupled from its implementation.

Do not create a new package to hide one function. Never `pkg/utils/`,
`internal/common/`, `internal/platform/`, `internal/kit/`,
`internal/infra/`. If two helpers share an idea, give that idea a
name and make it the package.

## 3. Naming

- **Packages:** single word, lowercase, no underscores. Singular for
  one kind of thing (`user`, `order`); pluralize only for genuine
  collections (`errors`, `flags`).
- **Types:** `MixedCaps`, no underscores (`UserService`).
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

## 4. Error Handling

- Errors are values. Use `error` interface, never panics in libraries.
- Wrap: `fmt.Errorf("op x: %w", err)` or `errors.Join`.
- Sentinels: `var ErrNotFound = errors.New("not found")` +
  `errors.Is(err, ErrNotFound)`. Domain sentinels in `<domain>.go`
  (`internal/user/user.go` for `ErrUserNotFound`); shared sentinels
  in `errors.go`.
- Custom error types: implement `Error()` and possibly
  `Is(target error) bool` / `Unwrap() error`.
- Don't log and return — pick one (usually return; let the caller
  decide to log).
- Wrap at boundaries (network, IO, external calls), not on every line.

## 5. Logging & Observability

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

## 6. Comments & Docs

- Every exported name: doc comment starting with the name.
- Doc comments begin with the name being declared.
- `// TODO(name):` with owner. `// FIXME:` and `// XXX:` discouraged.
- Package comment in `doc.go` (one short sentence — e.g.
  `// Package auth provides ...`) is conventional and surfaces on
  pkg.go.dev. Long descriptions belong in `service.go` as a doc
  comment on the service struct.

## 7. Testing

**Runner:** stdlib `testing`, files end with `_test.go`.
**Assertions:** `testify/assert` + `testify/require` (default unless
project says otherwise).

**Organization:**

- **External tests** (`package user_test`): next to source. Black-box
  — public contract only. **Default to this**; reach for the unit
  under test through its interfaces, not via unexported helpers.
- **Internal tests** (`package user`): same directory as code under
  test. Use only when you genuinely need a white-box seam (uncommon).
  Even then, **assert on behavior, not on the shape of unexported
  helpers** — if you can only express a test by reaching into
  internals, the unit is too tightly coupled.
- **`tests/` at module root:** integration / E2E tests that wire
  multiple domains. Separate binary.

Within `internal/<domain>/`:

- Tests for `<group>.go` live in `<group>_test.go`.
- Table-driven subtests: `tests := []struct{ name string; ... }{...}`.
- `TestFunctionName` or `TestFunctionName_Scenario`. Subtests via
  `t.Run("case name", ...)`.
- Benchmarks: `func BenchmarkXxx(b *testing.B)`.
- Coverage: target meaningful branches, not 100%.

### Mocks with mockery

Use **[mockery v3](https://vektra.github.io/mockery/)** (v3.7.x).

- **Config:** `<repo-root>/mockery.yaml` (v3 — no leading dot)
  declares which interfaces to mock, output directory, package
  names, per-interface overrides.
- **Generated location:** declared in `mockery.yaml` via `outdir` per
  `interface:` block. Default: `mocks/<package>/<Interface>.go` at
  module root. When interface is scoped to one domain, set
  `outdir: internal/<domain>/mocks/`. Pick one convention per project.
- **Generation:** `mockery` (reads config) or `go generate ./...`
  when interfaces carry `//go:generate mockery` directives. Pick one.
- **In tests:** the generated mock satisfies the interface; pass it
  as a constructor argument (`NewService(repo, mailer, logger)`).

When a top-level domain depends on another top-level's interface, the
test for the consumer uses the consumer-side mock (generated from the
interface declared in the **producer's** top level).

> **Behavior over implementation.** Assert on return values, DB
> rows, log lines, and emitted events — never on call ordering
> or the shape of unexported helpers.
>
> **Mocking by layer:**
> - Unit / Integration: real modules of the app wired together;
>   external systems mocked or in-process substituted (SQLite,
>   `httptest.NewServer`, fake SMTP, in-memory queues).
>   Mockery v3 generates interface mocks at boundaries.
> - E2E: built binary (`go build`) against real services via
>   `go-testcontainers` for Postgres / Redis / MongoDB / etc.
>
> See `references/testing-principles.md` for the full guidance.

## 8. Imports & Dependencies

Three groups, separated by blank lines:

1. Standard library
2. Third-party
3. Internal module

`goimports` enforces this automatically (`gofmt -w .` + `goimports -w .`).
Use `go mod tidy` after every change. Don't commit `go.sum` updates
you don't recognize. `go vet ./...`. `golangci-lint run` if
configured.

## 9. Verification Commands

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
| Generate (mockery) | `mockery` (config) or `go generate ./...` |

If the project uses a task runner (Taskfile, Mage, Make), adapt — but
the underlying go commands stay the same.

## 10. Hierarchy

Stack-specific MUST/NEVER:

- `internal/` location is enforced by the Go toolchain.
- No circular imports (`go build` fails on them).
- `go.mod` versioning — unlisted modules refuse to build.
- Mocks cannot substitute test code at runtime (`go test`).

## 11. Ecosystem versions (verify live)

Stack conventions above are stable; library versions change. Pick libraries via `code-plan` live tech discovery (`go list -m -versions`; `go.dev`) at PLAN time.

## 12. Sources (URL index)

- Go standard library: <https://pkg.go.dev/std>
- Effective Go: <https://go.dev/doc/effective_go>
- Go Code Review Comments: <https://github.com/golang/go/wiki/CodeReviewComments>
- Standard Go Project Layout: <https://github.com/golang-standards/project-layout>
- spf13/cobra (CLI conventions): <https://github.com/spf13/cobra>
- testify (test framework): <https://github.com/stretchr/testify>
- mockery v3 (mock generator): <https://github.com/vektra/mockery>
- pgx/v5 (Postgres driver): <https://github.com/jackc/pgx>
- go-kit (canonical example of package naming): <https://github.com/go-kit/kit>