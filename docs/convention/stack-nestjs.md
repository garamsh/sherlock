# NestJS — Architecture & Style Conventions

> Defaults for NestJS projects in 2025. Source: NestJS official docs,
> `nestjs/schematics` (source of truth for `nest new`),
> `nestjs/nest/sample/01-cats-app`, TypeORM/Mongoose/testing guides.
> 2025 patterns: class-validator DTOs as runtime classes,
> `@nestjs/config` with `registerAs`, TypeORM `forRootAsync`/`forFeature`,
> co-located unit tests, e2e under `test/`. Defaults, not laws —
> divergence → follow code, note in PLAN.md §8.

## Contents
- 0. Folder & file naming — strict
- 1. Project layout — feature modules
- 2. Example feature module
- 3. Module kinds
- 4. Configuration
- 5. Database
- 6. Validation
- 7. Testing — two-track
- 8. CLI conventions
- 9. Strict-mode TypeScript
- 10. Hierarchy (stack-specific MUST/NEVER)
- 11. Ecosystem versions (verify live)
- 12. Sources (URL index)

## 0. Folder & file naming — strict

Names describe what they own. Banned at any level: `src/utils/`,
`src/helpers/`, `src/shared/`, `src/misc/`, `*.utils.ts`,
`*.helpers.ts`, `*.shared.ts`.

**Mandatory file suffixes** (matches `nest g`):

| Suffix | Construct | Example |
|---|---|---|
| `*.module.ts` | `@Module()` | `users.module.ts` |
| `*.controller.ts` | `@Controller(path)` | `users.controller.ts` |
| `*.service.ts` | `@Injectable()` business logic | `users.service.ts` |
| `*.guard.ts` | `implements CanActivate` | `roles.guard.ts` |
| `*.interceptor.ts` | `implements NestInterceptor` | `logging.interceptor.ts` |
| `*.pipe.ts` | `implements PipeTransform` | `validation.pipe.ts` |
| `*.filter.ts` | `implements ExceptionFilter` | `http-exception.filter.ts` |
| `*.middleware.ts` | `implements NestMiddleware` | `auth.middleware.ts` |
| `*.decorator.ts` | parameter/method decorator | `current-user.decorator.ts` |
| `*.gateway.ts` | WebSocket gateway | `events.gateway.ts` |
| `*.dto.ts` | DTO with `class-validator` | `create-user.dto.ts` |
| `*.entity.ts` | TypeORM `@Entity()` | `user.entity.ts` |
| `*.schema.ts` | Mongoose `@Schema()` | `cat.schema.ts` |
| `*.spec.ts` | co-located unit test | `users.service.spec.ts` |
| `*.e2e-spec.ts` | e2e (in `test/`) | `users.e2e-spec.ts` |

Class names match files: `users.service.ts` → `UsersService`. IDE
navigation depends on it.

> The table above lists the **filename suffix**; §1 shows where
> each file lives within the feature folder (e.g.
> `*.entity.ts` goes in `entities/`).

## 1. Project layout — feature modules

```
project/
├── .env / .env.example              # at repo root, NEVER inside src/
├── eslint.config.mjs
├── nest-cli.json
├── package.json
├── tsconfig.json / tsconfig.build.json
├── src/
│   ├── main.ts                      # bootstrap (global pipes/filters)
│   ├── app.module.ts                # imports feature modules + CoreModule
│   ├── config/                      # @nestjs/config wrappers
│   │   ├── configuration.ts
│   │   ├── env.validation.ts
│   │   ├── database.config.ts       # registerAs('database', ...)
│   │   └── app.config.ts
│   ├── database/                    # (OR prisma/, orm/, mongo/)
│   │   ├── database.module.ts       # TypeOrmModule.forRootAsync
│   │   ├── data-source.ts           # CLI data source for migrations
│   │   └── migrations/
│   ├── common/                      # cross-cutting stateless, SORTED BY KIND
│   │   ├── decorators/              # @CurrentUser(), @Roles()
│   │   ├── guards/                  # JwtAuthGuard, RolesGuard
│   │   ├── interceptors/            # LoggingInterceptor
│   │   ├── pipes/                   # ParseIntPipe, custom ZodPipe
│   │   ├── filters/                 # AllExceptionsFilter
│   │   └── middleware/              # correlation-id, logger
│   ├── auth/                        # cross-cutting shared MODULE
│   │   ├── auth.module.ts
│   │   ├── auth.service.ts
│   │   ├── strategies/
│   │   └── decorators/
│   └── <feature>/                   # ONE folder per bounded context
│       ├── <feature>.module.ts
│       ├── <feature>.controller.ts
│       ├── <feature>.service.ts
│       ├── <feature>.controller.spec.ts
│       ├── <feature>.service.spec.ts
│       ├── dto/
│       │   ├── create-<feature>.dto.ts
│       │   ├── update-<feature>.dto.ts   # PartialType(CreateXxxDto)
│       │   └── query-<feature>.dto.ts
│       └── entities/                # OR schemas/ for Mongoose
│           └── <feature>.entity.ts
└── test/                            # E2E ONLY
    ├── jest-e2e.json
    └── <feature>/<feature>.e2e-spec.ts
```

### Feature file responsibilities

| File | Owns |
|---|---|
| `<feature>.module.ts` | `@Module()`: imports deps, declares controllers/providers, exports public services. |
| `<feature>.controller.ts` | **Thin.** Parse, call service, return. No ORM access. |
| `<feature>.service.ts` | Business logic, transactions, cross-aggregate calls. |
| `dto/create-<feature>.dto.ts` | Input validation shape (class). |
| `dto/update-<feature>.dto.ts` | `extends PartialType(CreateXxxDto)`. |
| `dto/query-<feature>.dto.ts` | Pagination / filters. |
| `entities/<feature>.entity.ts` | TypeORM `@Entity()` (or `schemas/<feature>.schema.ts` for Mongoose). |

### Cross-cutting: `src/common/<kind>/`

Each subfolder holds exactly one Nest enhancer kind: `decorators/`,
`guards/`, `interceptors/`, `pipes/`, `filters/`, `middleware/`.
One class per file. No `index.ts` barrel across feature boundaries.

## 2. Example feature module

```typescript
@Module({
  imports: [TypeOrmModule.forFeature([UserEntity])],
  controllers: [UsersController],
  providers: [UsersService],
  exports: [UsersService],  // only when another module needs it
})
export class UsersModule {}

@Controller('users')
export class UsersController {
  constructor(private readonly users: UsersService) {}

  @Post()
  create(@Body() dto: CreateUserDto): Promise<UserEntity> {
    return this.users.create(dto);
  }

  @Get(':id')
  findOne(@Param('id', ParseUUIDPipe) id: string): Promise<UserEntity> {
    return this.users.findOne(id);
  }
}

@Injectable()
export class UsersService {
  constructor(
    @InjectRepository(UserEntity) private readonly users: Repository<UserEntity>,
  ) {}

  async create(dto: CreateUserDto): Promise<UserEntity> {
    const entity = this.users.create(dto);
    await this.users.save(entity);
    return entity;
  }

  async findOne(id: string): Promise<UserEntity> {
    // TypeORM 0.3+ canonical: findOneBy / findOneByOrFail.
    // findOneByOrFail auto-throws EntityNotFoundError, which
    // a global exception filter maps to NotFoundException.
    const entity = await this.users.findOneBy({ id });
    if (!entity) throw new NotFoundException(`user ${id} not found`);
    return entity;
  }
}
```

```typescript
// src/users/dto/create-user.dto.ts
import { IsEmail, IsString, MinLength } from 'class-validator';

export class CreateUserDto {
  @IsEmail() email!: string;
  @IsString() @MinLength(8) password!: string;
}

// src/users/dto/update-user.dto.ts
import { PartialType } from '@nestjs/mapped-types';
import { CreateUserDto } from './create-user.dto';
export class UpdateUserDto extends PartialType(CreateUserDto) {}
```

DTOs are **classes**, not interfaces. TS interfaces are erased at
compile time; `ValidationPipe` reads metadata at runtime.

## 3. Module kinds

| Kind | Example | Notes |
|---|---|---|
| Root | `AppModule` | One per project. Imports `ConfigModule.forRoot({ isGlobal: true })`, `DatabaseModule`, every feature. |
| Feature | `UsersModule` | Each owns a folder; nothing leaks across without `exports`. |
| Shared | `DatabaseModule`, `AuthModule` | Imported once into root; re-exported services via `forFeature()`. |
| Dynamic | `ConfigModule`, `TypeOrmModule` | `forRoot()` once in root; `forFeature()` per feature. |

Avoid `@Global()` unless the provider is genuinely used everywhere
(logger, request context). Docs say so explicitly.

## 4. Configuration

```typescript
// src/config/configuration.ts
export default () => ({ port: parseInt(process.env.PORT, 10) || 3000 });

// src/config/env.validation.ts
import { plainToInstance } from 'class-transformer';
import { IsEnum, IsNumber, IsString, validateSync } from 'class-validator';

export enum Environment {
  Local = 'local', Development = 'development',
  Production = 'production', Test = 'test',
}

export class EnvironmentVariables {
  @IsEnum(Environment) NODE_ENV: Environment = Environment.Local;
  @IsNumber() PORT: number = 3000;
  @IsString() DATABASE_HOST!: string;
}

export function validate(config: Record<string, unknown>) {
  const validated = plainToInstance(EnvironmentVariables, config, { enableImplicitConversion: true });
  const errors = validateSync(validated, { skipMissingProperties: false });
  if (errors.length) throw new Error(errors.toString());
  return validated;
}

@Module({
  imports: [
    ConfigModule.forRoot({ isGlobal: true, load: [configuration, databaseConfig], validate }),
    DatabaseModule, UsersModule, AuthModule,
  ],
})
export class AppModule {}
```

## 5. Database

### TypeORM (default)

```typescript
TypeOrmModule.forRootAsync({
  imports: [ConfigModule],
  inject: [ConfigService],
  useFactory: (cfg: ConfigService) => ({
    type: 'postgres', host: cfg.get('database.host'),
    autoLoadEntities: true,
    synchronize: false,           // NEVER true in production
    migrationsRun: true,
  }),
})
```

`synchronize: false` in production. Migrations are the only schema
history.

### Mongoose

```typescript
@Schema()
export class Cat {
  @Prop() name!: string;
  @Prop() age!: number;
}
export const CatSchema = SchemaFactory.createForClass(Cat);
export type CatDocument = HydratedDocument<Cat>;

@Injectable()
export class CatsService {
  constructor(@InjectModel(Cat.name) private catModel: Model<Cat>) {}
  // Mongoose 8+ canonical: Model.create (replaces `new Model(dto).save()`)
  create(dto: CreateCatDto): Promise<Cat> { return this.catModel.create(dto); }
}
```

### Prisma

No official `@nestjs/prisma` package. Community pattern: `@Global()`
`PrismaModule` exporting `PrismaService extends PrismaClient`:

```
src/prisma/
├── prisma.module.ts
└── prisma.service.ts     # OnModuleInit ($connect) / OnModuleDestroy ($disconnect)
```

Features inject `PrismaService` directly.

## 6. Validation

```typescript
// src/main.ts
app.useGlobalPipes(new ValidationPipe({
  whitelist: true,            // strip non-decorated properties
  forbidNonWhitelisted: true, // reject request if extras
  transform: true,            // cast query/path params to declared types
}));
```

DTOs are classes (interfaces are erased at compile time). **Never**
`import type { CreateUserDto }` — always `import { CreateUserDto }`.
Use `PartialType`/`PickType`/`OmitType`/`IntersectionType` from
`@nestjs/mapped-types` to derive DTOs.

## 7. Testing — two-track

| Track | Pattern | Location | Runner |
|---|---|---|---|
| Unit | `*.spec.ts` | **co-located** with source | `jest` from `npm test` |
| E2E | `*.e2e-spec.ts` | `test/` folder | `jest --config ./test/jest-e2e.json` |

```typescript
// src/users/users.service.spec.ts
describe('UsersService', () => {
  let service: UsersService;
  let repo: { findOne: jest.Mock };

  beforeEach(async () => {
    repo = { findOne: jest.fn() };
    const module = await Test.createTestingModule({ providers: [UsersService] })
      .useMocker((token) => {
        if (token === getRepositoryToken(UserEntity)) return repo;
        return {};
      })
      .compile();
    service = module.get(UsersService);
  });

  it('returns the user when found', async () => {
    repo.findOne.mockResolvedValue({ id: 'u1', email: 'a@b.c' });
    const user = await service.findOne('u1');
    expect(user).toEqual({ id: 'u1', email: 'a@b.c' });
  });

  it('throws NotFoundException when the row is missing', async () => {
    repo.findOne.mockResolvedValue(null);
    await expect(service.findOne('missing')).rejects.toBeInstanceOf(NotFoundException);
  });
});
```

```typescript
// test/users.e2e-spec.ts
describe('UsersController (e2e)', () => {
  let app: INestApplication;
  beforeEach(async () => {
    const moduleRef = await Test.createTestingModule({ imports: [AppModule] }).compile();
    app = moduleRef.createNestApplication();
    await app.init();
  });
  afterEach(async () => await app.close());
  it('POST /users returns 201', () => {
    return request(app.getHttpServer()).post('/users')
      .send({ email: 'x@y.z', password: 'long-enough' }).expect(201);
  });
});
```

Use `Test.createTestingModule(...)` for anything touching DI. Mock
repositories with `getRepositoryToken(Entity)` — not `useMocker()`
unless the dependency graph is huge. For globally-registered
enhancers you can't swap, replace `useClass` with `useExisting` so
the bound provider can be overridden.

> **Behavior over implementation.** Assert on controller /
> service behavior — return values, emitted events, log lines,
> HTTP responses — never the order of internal calls.
>
> **Mocking by layer:**
> - Unit / Integration: `supertest` over a real
>   `INestApplication` exercises the app's controllers in-
>   process. External systems (DB, downstream HTTP, queues)
>   mocked via `@nestjs/testing` providers or substituted with
>   SQLite / in-memory drivers. `useMocker` only at module
>   boundaries.
> - E2E: `nest start --prod` (or built binary) running against
>   real services via `@testcontainers/postgresql`,
>   `@testcontainers/mongodb`, etc.
>
> See `references/testing-principles.md` for the full guidance.

### Scripts (from schematics)

```jsonc
{
  "scripts": {
    "build": "nest build",
    "format": "prettier --write \"src/**/*.ts\" \"test/**/*.ts\"",
    "start": "nest start", "start:dev": "nest start --watch",
    "lint": "eslint \"{src,apps,libs,test}/**/*.ts\"",
    "test": "jest", "test:watch": "jest --watch",
    "test:cov": "jest --coverage",
    "test:e2e": "jest --config ./test/jest-e2e.json"
  }
}
```

## 8. CLI conventions

**Feature scaffold** (goes straight into `src/<name>/`):

```bash
nest g module users          # src/users/users.module.ts
nest g controller users      # src/users/users.controller.ts
nest g service users         # src/users/users.service.ts
nest g resource users        # full CRUD: module + controller + service + dto + entity
nest g resolver cats         # src/cats/cats.resolver.ts (GraphQL)
nest g gateway events        # src/events/events.gateway.ts (WebSocket)
```

By default `nest g` creates a folder named after the class. Pass
`--flat` to skip the folder.

**Enhancers** (CLI drops them at `src/<name>/<name>.<kind>.ts` — move
them into `src/common/<kind>/`):

```bash
nest g guard auth              # → src/auth/auth.guard.ts (or --path src/common/guards)
nest g interceptor logging     # → src/logging/logging.interceptor.ts (or --path src/common/interceptors)
nest g pipe validation         # → src/validation/validation.pipe.ts (or --path src/common/pipes)
nest g filter http-exception   # → src/http-exception/http-exception.filter.ts (or --path src/common/filters)
nest g decorator current-user  # → src/current-user/current-user.decorator.ts (or --path src/common/decorators)
```

The CLI's default output is **not** where the file should live — it
exists because the CLI has no opinion on cross-cutting layout. Our
layout places enhancers in `src/common/<kind>/`. Use `--path
src/common/<kind>` at generation time, or `mv` after generation.
Pick one and stick to it across the project.

Always use the CLI's suffixes — tooling and IDE rely on them.

## 9. Strict-mode TypeScript

`nest new --strict` enables: `strictNullChecks`, `noImplicitAny`,
`strictBindCallApply`, `forceConsistentCasingInFileNames`,
`noFallthroughCasesInSwitch`. Default to `--strict` for new projects.

## 10. Hierarchy

Stack-specific MUST/NEVER:

- **Never** `synchronize: true` in TypeORM production (drops data
  on schema change).
- **Never** `@Global()` on every shared module (hides dependencies;
  defeats DI legibility).
- **Never** DTOs as TS interfaces (erased at compile time;
  `ValidationPipe` can't read them).
- **Never** `import type { CreateUserDto }` (erases runtime
  metadata `ValidationPipe` needs).

## 11. Ecosystem versions (verify live)

Stack conventions above are stable; library versions change. Pick libraries via `code-plan` live tech discovery (`npm view`; official docs) at PLAN time.

## 12. Sources (URL index)

- docs: docs.nestjs.com/{modules, controllers, providers, techniques/validation, techniques/configuration, techniques/database, techniques/mongo, fundamentals/testing, cli/usages, first-steps}
- repos: github.com/{nestjs/schematics, nestjs/nest/sample/01-cats-app}