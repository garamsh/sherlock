# Testing Principles

## Contents
- Three layers — unit / integration / e2e
- Behavior over implementation
- Mocking strategy
- Coverage and naming
- Placement

## Three layers — different goals, different scopes

The Test client column lists concrete tools by stack as orientation; if your stack isn't listed, see the relevant stack convention reference for the current in-process client.

| Layer | Scope | Mocks | Test client | External systems |
|---|---|---|---|---|
| **Unit** | single function/class in isolation | replace at module boundaries (DB, HTTP, queue, time, FS) | direct call | none |
| **Integration** | modules cooperating | external systems mocked or substituted; SUT is real | in-process client (`httpx.AsyncClient`+`ASGITransport` / `supertest`+`INestApplication` / `httptest.NewServer` / route fetcher) | mocked |
| **E2E** | built binary against real infra | nothing | real client | **real** (testcontainers OK) |

The app is *not yet a built binary* in integration. **No testcontainers in integration** — real Postgres for an integration test is e2e.

## Behavior over implementation

Assert on outputs and side-effects only:
- return values
- emitted events, log lines
- HTTP responses (status + body)
- DB rows (from a testcontainer, at the e2e layer)

Do not assert on: call order, unexported helper shape, private type structure, internal refactors. **If a refactor forces test updates, the tests were testing implementation.**

## Mocking strategy

- **Unit**: mock any module boundary you control.
- **Integration**: external systems mocked or substituted; SUT itself is real.
- **E2E**: nothing mocked. Real binary, real infrastructure.

### Substitutes for state

Time → inject a clock. Network → substitute. Filesystem → tmpdir. Random → seed.

## Coverage and naming

- Target meaningful branches, not 100%.
- Names read like a spec:
  - Go: `TestFunction_Scenario`
  - Jest/Vitest: `describe(...) > it(...)`
- A failing test name should tell you what broke without opening the file.

## Placement

- Unit: co-located with the source it tests.
- Integration: co-located with the boundary it exercises, or in a dedicated integration directory.
- E2E: a dedicated top-level directory, outside the application source tree.

Concrete directory and file names follow the project's stack convention file — the stack file owns placement specifics.