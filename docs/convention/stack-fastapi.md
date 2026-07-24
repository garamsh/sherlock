# FastAPI — Architecture & Style Conventions

> Defaults for FastAPI projects. The canonical production
> convention is **domain-by-package** (a bounded context owns its
> folder, with router / schemas / models / service / dependencies /
> exceptions all in that folder) — the shape used by
> `zhanymkanov/fastapi-best-practices` (17.7k stars) and
> inspired by Netflix's Dispatch. The official
> `fastapi/full-stack-fastapi-template` is a role-based
> starter template that works well for small projects and
> microservices; for production multi-domain codebases the
> domain-by-package layout scales better. Adopting the
> production layout by default; diverging to a small-project
> layer-based layout (`api/`, `services/`, `repositories/`,
> `models/`, `schemas/`, `core/`) is acceptable when the project
> has only 2-3 domains — note the deviation in PLAN.md §8.

## Contents
- 0. Folder & file naming
- 1. Project layout — domain-by-package (production)
- 2. Application bootstrap
- 3. Configuration
- 4. Database
- 5. Schemas (Pydantic v2)
- 6. Service layer (with optional repository)
- 7. Routers
- 8. Streaming (SSE / JSON Lines / bytes)
- 9. Dependencies
- 10. Tests
- 11. Migrations & errors
- 12. Hierarchy (stack-specific MUST/NEVER)
- 13. Sources (URL index)
- 14. Ecosystem versions (verify live)

## 0. Folder & file naming

Names describe what they own. Adopt the names the canonical
zhanymkanov production layout uses:

- **Per-domain `utils.py`** is fine (single file at the root of
  a domain — non-business logic helpers, response normalization,
  data enrichment). Cross-cutting `utils.py` at the project root
  is also fine.
- **`config.py`** (domain-local config, `BaseSettings` subclass
  with `env_prefix="<DOMAIN>_"`), **`constants.py`**,
  **`exceptions.py`**, **`dependencies.py`**, **`router.py`**,
  **`schemas.py`**, **`models.py`**, **`service.py`** are the
  conventional file names in each domain folder. Adopt them.

Names that are still banned (vague, never canonical for FastAPI):

- `helpers.py` / `helpers/` (folder)
- `common.py` / `common/`
- `misc.py` / `misc/`
- `shared.py` / `shared/`

These were never the FastAPI canonical layout and are still
wrong for it. Two helpers sharing an idea → name the idea:
`src/auth/password_hashing.py`, `src/billing/format_currency.py`.

## 1. Project layout — domain-by-package (production)

A bounded context owns its folder. URL versioning is a **URL
prefix** at `include_router` time — never a directory name like
`api/v1/`. This is the zhanymkanov production layout; the
official FastAPI starter template is mentioned at the end of
this section as a small-project alternative.

```
fastapi-project/
├── alembic/                          # `alembic init -t async`
├── src/
│   ├── main.py                       # FastAPI() + lifespan + CORS + include_router loop
│   ├── config.py                     # global Settings (BaseSettings)
│   ├── database.py                   # async_engine, sessionmaker, get_db, Base
│   ├── exceptions.py                 # global exception → HTTP handler in main
│   ├── pagination.py                 # global pagination helper (optional)
│   ├── <domain>/                     # one folder per bounded context
│   │   ├── router.py                 # APIRouter(prefix="/<domain>", tags=[...])
│   │   ├── schemas.py                # pydantic models (Create / Update / Public / List)
│   │   ├── models.py                 # SQLAlchemy 2.x ORM (one file per aggregate)
│   │   ├── service.py                # business logic, transactions
│   │   ├── repository.py             # DB access (optional — when service grows)
│   │   ├── dependencies.py           # domain-local FastAPI deps
│   │   ├── config.py                 # domain-local Settings (env_prefix="<DOMAIN>_")
│   │   ├── constants.py              # StrEnum error codes
│   │   ├── exceptions.py             # domain-specific exceptions
│   │   └── utils.py                  # non-business helpers (response normalization, etc.)
│   └── <external_service>/            # e.g. `aws/`, `s3/`, `payment_provider/`
│       └── client.py                  # client model for external service
├── tests/
│   ├── conftest.py                   # shared async client + db fixtures
│   ├── <domain>/
│   │   ├── test_router.py
│   │   ├── test_service.py
│   │   └── test_dependencies.py
│   └── <external_service>/
├── pyproject.toml                    # uv / poetry (NOT requirements.txt)
├── uv.lock                           # uv-generated lockfile
├── .env
├── .gitignore
├── docker-compose.yml                # local dev (db, redis, etc.)
├── Dockerfile                        # production image
├── logging.ini
└── alembic.ini
```

For **small projects (≤3 domains, ≤5 tables)** where the
domain-by-package shape is overkill, the layer-based layout
(`src/api/`, `src/services/`, `src/repositories/`, `src/models/`,
`src/schemas/`, `src/core/`) is acceptable. Switch to
domain-by-package the moment either threshold is crossed.

For **microservices / very small services** the official
`fastapi/full-stack-fastapi-template` flat layout
(`backend/app/{main,models,crud,utils}.py` +
`backend/app/{api,core,alembic}/`) is the canonical starter. The
template uses `app/utils.py` for cross-cutting helpers — that
name is fine for that layout. Adopt the official template for
its intended use case (small / single-service projects), not for
multi-domain production code.

### File responsibilities inside `src/<domain>/`

| File | Owns |
|------|------|
| `router.py` | `APIRouter(...)`. Routes stay thin: parse via Pydantic, call `service.<method>`, return `schemas.<Domain>Public`. |
| `schemas.py` | Pydantic v2 input/output. **Never** merged with ORM models. |
| `models.py` | SQLAlchemy 2.x ORM tables. One file per aggregate. |
| `service.py` | Business logic, transactions, cross-aggregate calls. |
| `repository.py` | (Optional) DB-only access. Add when `service.py` starts mixing I/O orchestration with raw query building. |
| `dependencies.py` | Domain-local FastAPI dependencies (`valid_<x>_id`, etc.). |
| `config.py` | `BaseSettings` subclass with `env_prefix="<DOMAIN>_"` — auth-specific env, billing-specific env, etc. |
| `constants.py` | `class <Domain>ErrorCode(StrEnum)`. Replaces magic strings. |
| `exceptions.py` | Domain exceptions, mapped to HTTP by global handlers in `main.py`. |
| `utils.py` | Non-business logic helpers (response normalization, data enrichment, etc.). |

Cross-domain imports use explicit aliases:
`from src.billing import service as billing_service`. **Never**
`from src.<domain> import *`.

## 2. Application bootstrap

```python
# src/main.py
from contextlib import asynccontextmanager
from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from src.config import settings
from src.database import sessionmanager
from src.<domain> import router as <domain>_router


@asynccontextmanager
async def lifespan(app: FastAPI):
    await sessionmanager.init(settings.DATABASE_URL)
    yield
    await sessionmanager.close()


app = FastAPI(title=settings.PROJECT_NAME, lifespan=lifespan)
app.add_middleware(CORSMiddleware, allow_origins=settings.CORS_ORIGINS,
                   allow_credentials=True, allow_methods=["*"], allow_headers=["*"])

for domain_router in (<domain>_router, ...):
    app.include_router(domain_router, prefix=settings.API_V1_STR)
```

`@app.on_event("startup")` / `@app.on_event("shutdown")` are
deprecated; use the `lifespan` `asynccontextmanager` above. The
sessionmanager initialises the connection pool on startup and
closes it on shutdown.

`API_V1_STR` lives in `Settings`; only URL prefix at
`include_router` time. v1 → v2 migration = one-line constant
change. URL versioning stays in the prefix, never in the
directory name (no `src/v1/`).

### Serving a built SPA (optional)

For monorepos with a built frontend (Vite / Astro / Angular /
Svelte / Vue / etc.), use `app.frontend()` / `router.frontend()`
to serve the static assets — they're low-priority routes, so
regular API routes match first and client-side routing
fallbacks fill the rest. Avoid `StaticFiles` for SPA mount
when the frontend needs client-side routing.

```python
# src/main.py
from fastapi import FastAPI

app = FastAPI()
app.frontend("/", directory="dist")  # serves ./dist at /
```

```python
# inside an APIRouter
router = APIRouter(prefix="/admin")
router.frontend("/", directory="admin-dist")
app.include_router(router)
```

## 3. Configuration

```python
# app/core/config.py
from functools import lru_cache
from pydantic import PostgresDsn
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", env_ignore_empty=True, extra="ignore")
    PROJECT_NAME: str = "Backend"
    API_V1_STR: str = "/api/v1"
    DATABASE_URL: PostgresDsn
    SECRET_KEY: str
    CORS_ORIGINS: list[str] = []
    ENVIRONMENT: str = "production"  # local / staging / production
    SENTRY_DSN: str | None = None


@lru_cache
def get_settings() -> Settings:
    return Settings()


settings = get_settings()
```

`@lru_cache` + `get_settings()` lets tests use
`app.dependency_overrides[get_settings] = ...`. **Avoid one
mega-Settings class** — split into a global `Settings` and small
per-domain `<Domain>Config(BaseSettings)` classes when warranted
(see §1 for the per-domain `config.py`).

**Package manager:** `uv` (Astral) is the production standard in
2026. `pyproject.toml` is the manifest; `uv.lock` is the
lockfile. `pip install -r requirements.txt` is legacy.

### Running the app

Use the official `fastapi` CLI rather than `uvicorn` / `gunicorn`
directly — it handles the import path resolution, reload, and
production mode for you.

```bash
fastapi dev                 # localhost with reload
fastapi run                 # production server (uvicorn under the hood)
fastapi dev src/main.py     # explicit entrypoint if not declared
```

Prefer declaring the entrypoint in `pyproject.toml` so the CLI
finds it without an argument:

```toml
[tool.fastapi]
entrypoint = "src.main:app"
```

`fastapi run` defaults to `0.0.0.0:8000` and is the
production-recommended path (single-process by default; front
with a process manager / `gunicorn` with `uvicorn.workers.UvicornWorker`
only when horizontal scaling is needed).

## 4. Database

```python
# src/database.py
from collections.abc import AsyncGenerator
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker, create_async_engine
from sqlalchemy.orm import DeclarativeBase

from src.config import settings


class Base(DeclarativeBase):
    pass


engine = create_async_engine(str(settings.DATABASE_URL), pool_pre_ping=True)
SessionLocal = async_sessionmaker(engine, expire_on_commit=False, class_=AsyncSession)


async def get_db() -> AsyncGenerator[AsyncSession, None]:
    async with SessionLocal() as session:
        yield session
```

- Per-domain models in `src/<domain>/models.py` with
  SQLAlchemy 2.x typed `Mapped[T]` annotations.
- **Async-first.** Sync DB calls inside `async def` are a
  classic deadlock source — pick the async API. The Postgres
  driver is `asyncpg` (used by the `postgresql+asyncpg://` URL).
- Use Core `select()/insert()/update()/delete()` — not legacy
  Query API.
- Migrations: `alembic init -t async`. Import each
  `<domain>.models` in `alembic/env.py` so autogenerate sees
  them.
- Naming: `lower_case_snake`, **singular** tables. Group with
  prefix (`payment_account`). `_at` for datetimes, `_date` for
  dates.
- A reproducible constraint / index naming scheme is set via
  `MetaData(naming_convention={...})` on `Base`, with a dict
  you define yourself (SQLAlchemy does not ship a `POSTGRES_*`
  constant — see zhanymkanov for an authoritative recipe).
  Predictable names matter more for Alembic autogenerate than
  any specific convention.

## 5. Schemas (Pydantic v2)

Per domain: `Base`, `Create`, `Update`, `Public`. Add
`<Resource>Public` (list response) when pagination exists.

```python
# src/auth/schemas.py
from pydantic import BaseModel, ConfigDict, EmailStr


class UserBase(BaseModel):
    email: EmailStr
    full_name: str | None = None


class UserCreate(UserBase):
    password: str


class UserUpdate(BaseModel):
    full_name: str | None = None
    password: str | None = None


class UserPublic(UserBase):
    model_config = ConfigDict(from_attributes=True)
    id: UUID
    created_at: datetime
```

- `ConfigDict(from_attributes=True)` enables
  `<Resource>Public.model_validate(orm_row)`.
- **Never** `ConfigDict(json_encoders={...})` — removed in v2.
- **Never** `Field(ge=18, default=None)` — constraint contradicts
  default.
- **Never** merge schema with ORM model in one file.
- **Never use `...` (Ellipsis) for required parameters or model
  fields.** It's not needed and is not recommended in modern
  FastAPI / Pydantic v2. For required inputs, declare the type
  and the constraint; for required body fields, declare the
  field without a default. FastAPI and Pydantic v2 infer
  requirement from the absence of a default.

  ```python
  # Correct
  class Item(BaseModel):
      name: str
      price: float = Field(gt=0)
      project_id: int  # required, no default


  # Avoid
  class Item(BaseModel):
      name: str = ...  # Ellipsis, not needed
      price: float = Field(gt=0)
      project_id: int = ...
  ```

- **Never use Pydantic `RootModel`** — instead use
  `Annotated[..., Body()]` and let FastAPI build a `TypeAdapter`
  for you. This works with all FastAPI features (validation,
  OpenAPI, dependency injection):

  ```python
  # Correct
  async def create_items(
      items: Annotated[list[int], Field(min_length=1), Body()],
  ):
      ...


  # Avoid
  class ItemsRoot(RootModel[list[int]]):
      root: list[int]

  async def create_items(items: ItemsRoot):
      ...
  ```

## 6. Service layer (with optional repository)

Plain `async def` functions. Class form acceptable only when
service is genuinely stateful.

```python
# src/posts/service.py
async def get_posts(
    session: AsyncSession, creator_id: UUID4, *, limit: int = 10, offset: int = 0
) -> list[dict[str, Any]]:
    return await posts_repository.list_for_creator(session, creator_id, limit, offset)


async def create_user(session: AsyncSession, data: UserCreate) -> User:
    user = User(email=data.email, ...)
    session.add(user)
    await session.commit()
    await session.refresh(user)
    return user
```

Transactions live here. `async with session.begin():` for
multi-statement work. Cross-aggregate calls go through other
services. Joins / aggregations are SQL. Services never import
from `app.routers` (back-edge). Services may raise domain
exceptions from `src.<domain>.exceptions`.

**Layer split (production standard):**

- `service.py` orchestrates business logic — accepts a session,
  calls the repository, raises domain exceptions, returns
  schemas.
- `repository.py` is DB-only — accepts a session, runs queries,
  returns ORM rows or domain types. **SQL-first**: do joins,
  filters, and aggregations in SQL, not in Python loops.
- `router.py` calls `service.py`. Routers never touch the
  session directly; they ask the service to do the work.

For small domains the repository can be inlined into
`service.py`. Pull it out when the same query needs to be
called from more than one service, or when the service file
starts mixing orchestration with raw query building.

**Mixing async and blocking code.** When a service is `async def`
but part of its work calls a sync library (e.g. `requests`,
synchronous ORM, file I/O, or a third-party SDK that doesn't
support `async`), use **[Asyncer](https://asyncer.tiangolo.com/)**
(also from the FastAPI / Tiangolo team) to run the blocking
call in a threadpool without manually managing
`run_in_threadpool` or wrapping the whole service in `def`.
Asyncer is the canonical answer for "I have an async
endpoint but I need to call a sync library cleanly."

```python
from asyncer import asyncify
import requests  # sync library

async def fetch_user_profile(user_id: int) -> dict:
    response = await asyncify(requests.get)(
        f"https://api.example.com/users/{user_id}", timeout=5
    )
    return response.json()
```

## 7. Routers

**Always declare router-level `prefix`, `tags`, and shared
`dependencies=` on the `APIRouter` itself** — not at the
`include_router` call site. That keeps the router
self-describing and makes `include_router(app)` a one-liner.

```python
# src/users/router.py
from typing import Annotated
from fastapi import APIRouter, Depends
from sqlalchemy.ext.asyncio import AsyncSession

from src.dependencies import get_db
from src.auth.schemas import UserCreate, UserPublic
from src.auth import service as user_service

router = APIRouter(
    prefix="/users",
    tags=["users"],
    dependencies=[Depends(get_current_user)],  # every route is authed
)


DbSession = Annotated[AsyncSession, Depends(get_db)]


@router.post("/", response_model=UserPublic, status_code=201)
async def create_user(payload: UserCreate, session: DbSession):
    return await user_service.create_user(session, payload)


@router.get("/{user_id}", response_model=UserPublic)
async def get_user(user: ValidUser):
    return user
```

```python
# src/main.py
app.include_router(user_service.router)  # no per-call prefix / tags
```

**One HTTP operation per function.** Don't mix `@router.get("/")
+ @router.post("/")` in the same function — separation
keeps OpenAPI / docs / tests / cache invalidation sane.

**Return type or `response_model`.** When the function's
return type already matches the public schema, write the
return type (`-> User`) and skip `response_model` —
Pydantic v2 serializes on the Rust side for performance.
Use `response_model=...` only when the public schema differs
from the internal return value (filtering fields, computed
properties, etc.).

**Never `ORJSONResponse` or `UJSONResponse`.** Both are
deprecated — declaring a return type (or `response_model`) lets
Pydantic v2 handle JSON serialization on the Rust side, which
is faster than either and avoids the
`jsonable_encoder` round-trip in the route.

Thin: parse, call `service`, return. Always
`Annotated[T, Depends(...)]` — **never** `def foo(x: T =
Depends(...))`. `async def` for I/O deps; `def` for pure deps.
`response_model=...` validates; don't also `return` a Pydantic
instance.

## 8. Streaming (SSE / JSON Lines / bytes)

For Server-Sent Events, use `response_class=EventSourceResponse`
and `yield` items from the endpoint. Plain objects are
auto-serialized as JSON `data:` fields; use `ServerSentEvent`
when you need explicit `event` / `id` / `retry` / `comment`
fields.

```python
from collections.abc import AsyncIterable
from fastapi import FastAPI
from fastapi.sse import EventSourceResponse, ServerSentEvent

app = FastAPI()


@app.get("/events", response_class=EventSourceResponse)
async def stream_events() -> AsyncIterable[ServerSentEvent]:
    yield ServerSentEvent(data={"status": "started"}, event="status", id="1")
    # ... yield more as they arrive
```

For JSON Lines or byte streaming, use `StreamingResponse` (from
`starlette.responses`) directly.

## 9. Dependencies

```python
# src/users/dependencies.py
from typing import Annotated
from fastapi import Depends, HTTPException
from sqlalchemy.ext.asyncio import AsyncSession

from src.database import get_db


async def valid_user_id(
    user_id: UUID, session: Annotated[AsyncSession, Depends(get_db)]
) -> User:
    user = await user_service.get_user(session, user_id)
    if not user:
        raise HTTPException(status_code=404, detail="user not found")
    return user


ValidUser = Annotated[User, Depends(valid_user_id)]
```

**Prefer small, decoupled dependencies.** FastAPI caches
dependency results within a request's scope, so splitting
auth and ownership checks into separate `Depends(...)` calls
costs nothing and makes both reusable. For example:

```python
async def parse_jwt_data(
    token: str = Depends(OAuth2PasswordBearer(tokenUrl="/auth/token"))
) -> dict:
    try:
        payload = jwt.decode(token, settings.JWT_SECRET, algorithms=[settings.JWT_ALG])
    except InvalidTokenError:
        raise InvalidCredentials()
    return {"user_id": payload["id"]}


async def valid_owned_post(
    post: Mapping = Depends(valid_post_id),
    token_data: dict = Depends(parse_jwt_data),
) -> Mapping:
    if post["creator_id"] != token_data["user_id"]:
        raise UserNotOwner()
    return post
```

Cross-cutting deps live in `src/dependencies.py`. Promote a
domain dep to `src/dependencies.py` only when ≥ 3 resources share
it. Per-request cache: same dep five times in one request runs
once.

## 10. Tests

```python
# tests/conftest.py
import pytest
from httpx import ASGITransport, AsyncClient

from src.main import app


@pytest.fixture
async def client() -> AsyncGenerator[AsyncClient, None]:
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as ac:
        yield ac
```

`pytest` + `pytest-asyncio` + `httpx.AsyncClient`. **Always**
`httpx.AsyncClient(transport=ASGITransport(app=app))`. Never
`TestClient` once project uses `AsyncSession`. **Never**
`from async_asgi_testclient import TestClient` — unmaintained.
**Never** mock the DB in integration tests. Override deps
(`app.dependency_overrides[parse_jwt_data] = fake_user`), don't
monkeypatch internals.

> **Behavior over implementation.** Assert on HTTP responses,
> log lines, and emitted events — never on internal call order
> or the shape of private dependencies.
>
> **Mocking by layer:**
> - Unit / Integration: `httpx.AsyncClient(transport=ASGITransport
>   (app=app))` exercises the app's real route handlers /
>   services in-process. External systems (DB, downstream HTTP,
>   queues) mocked or in-process substituted — `SQLite`
>   sessions, MSW / `httpx_mock` for downstream HTTP, fake
>   SMTP.
> - E2E: `uvicorn`/`gunicorn` running the built app against
>   real services via `testcontainers-python` (Postgres,
>   Redis, etc.).
>
> See `references/testing-principles.md` for the full guidance.

## 11. Migrations & errors

- `alembic init -t async`. Import every `<domain>.models` in
  `alembic/env.py`. Set a human-readable file template:
  `file_template = %%(year)d-%%(month).2d-%%(day).2d_%%(slug)s`
  (e.g. `2022-08-24_post_content_idx.py`).
- **Migrations must be static and reversible.** If a migration
  depends on dynamic data, only the data is dynamic — never
  the schema.
- Generate migrations with descriptive names and slugs.
- Review each migration before merge. Schema changes touch
  every environment; they deserve a second pair of eyes.
- `synchronize: true` is **never** used in production.
- `HTTPException` for HTTP errors in routes/deps.
- Cross-cutting domain exceptions live in
  `src/<domain>/exceptions.py` and are mapped to HTTP in
  `app/main.py`'s exception handlers
  (`@app.exception_handler(MyDomainError)`).
- **Never** `except Exception:` in routes. Catch the narrowest
  class.
- **Never** `BackgroundTasks` for anything you'd page on. If
  the task is short (< 1s) and failure can be silently dropped,
  `BackgroundTasks` is fine. Otherwise use Celery + Redis (or
  arq / Taskiq for async-native).

## 12. Hierarchy

Stack-specific MUST/NEVER:

- **Never** `synchronize: true` in production (drops data on
  schema change).
- **Never** mock the DB in integration tests (mock/prod drift).
- **Never** `from jose import jwt` / `from async_asgi_testclient
  import TestClient` (unmaintained footguns).
- **Never** sync DB session inside `async def` (may deadlock
  the pool).
- **Never** sync `requests` inside `async def` (blocks the
  event loop).
- **Never** `@app.on_event("startup")` — deprecated since
  FastAPI lifespan.

## 13. Sources (URL index)

- production convention: github.com/zhanymkanov/fastapi-best-practices
- official template (small / microservices): github.com/fastapi/full-stack-fastapi-template
- official docs: fastapi.tiangolo.com/{tutorial/bigger-applications, advanced/settings, advanced/async-tests}
- secondary repo (real-world): github.com/nsidnev/fastapi-realworld-example-app
- inspiration: Netflix Dispatch
- ecosystem: docs.sqlalchemy.org/en/20/orm/extensions/asyncio, docs.pydantic.dev/latest/migration

## 14. Ecosystem versions (verify live)

Stack conventions above are stable; library versions change. Pick libraries via `code-plan` live tech discovery (PyPI / official docs) at PLAN time.
