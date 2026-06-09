# AGENTS.md

## Git

- Always commit with `git commit -s` (sign-off). Never add co-author trailers.

## Commands

```bash
make generate        # Proto codegen: buf generate for external + internal
make lint            # buf lint
make test            # go test ./...
make test-integration  # requires CORDIS_TEST_POSTGRES_DSN env var

# Single-package / focused tests
go test ./services/message/v1/internal/server/... -v -count=1
go test ./services/message/v1/internal/store/... -v -count=1

# Build check (no output = ok)
go build ./services/message/v1/...
```

`make test-integration` runs integration tests (`//go:build integration`) for all three internal services (user, authenticator, message) and outbox relay.

The only required env var for integration tests is `CORDIS_TEST_POSTGRES_DSN`, e.g.:
```
CORDIS_TEST_POSTGRES_DSN="postgres://cordis:cordis@127.0.0.1:5432/cordis?sslmode=disable"
```

With Docker:
```
docker run -d --name cordis-test-pg -e POSTGRES_USER=cordis -e POSTGRES_PASSWORD=cordis -e POSTGRES_DB=cordis -p 5433:5432 postgres:18-alpine
CORDIS_TEST_POSTGRES_DSN="postgres://cordis:cordis@127.0.0.1:5433/cordis?sslmode=disable" make test-integration
```

## Architecture

```
Client ──(Connect-RPC/HTTP)──> API Gateway (:8080) ──(gRPC)──> Authenticator (:3001)
                                                                    │
                                                                    └──(gRPC)──> User (:3000)

                                                                Message (:3002)
```

- **API Gateway** (`services/api/v1/`): uses `net/http` directly (not go-zero's server). Connect-RPC. Thin proxy.
- **Authenticator** (`services/authenticator/v1/`): go-zero `zrpc`. JWT HS256, separate access/refresh secrets.
- **User** (`services/user/v1/`): go-zero `zrpc`. Argon2id password hashing.
- **Message** (`services/message/v1/`): go-zero `zrpc`. Cursor-based pagination, reactions, mentions, outbox events to Kafka.

All generated code lives under `gen/`. Never edit it directly.

## Proto

Edition 2023 with **implicit field presence** (no `optional`, no `features.field_presence = EXPLICIT`). Zero value = "not set". Always use generated `SetXxx(value)` setters — never struct literals.

Two codegen pipelines:
- `buf.gen.external.yaml`: `proto/api/` → Connect-Go + protobuf-es (web clients)
- `buf.gen.internal.yaml`: `proto/authenticator/`, `proto/user/`, `proto/message/` → gRPC + protobuf-go

After editing any `.proto`, run `make generate` to regenerate `gen/`.

## Service wiring

Every service: `Config → NewDependencies(cfg) → NewServiceContextWithDependencies(cfg, deps)`.

- `NewDependencies`: builds real DB, RPC clients, snowflake, token manager, Kafka client.
- `NewServiceContextWithDependencies`: panics on nil deps (fail-fast at startup).
- Tests inject fakes into `ServiceContext` directly — never call `NewDependencies`.

## Store layer

- Each store defines an `interface`. `SQLStore` holds `*sqlx.DB` and `sqlx.ExtContext` (`q` field).
- `q` is `*sqlx.DB` normally, `*sqlx.Tx` inside `Transact`.
- `Transact`: begins tx, creates child `SQLStore` with `q = tx`, calls callback. Deferred rollback on panic or error (both User and Message stores).
- Dynamic SQL uses positional params (`$1, $2, ...`).
- Migrations embedded via `//go:embed *.sql`, applied by `pkg/migration.Apply()` lexicographically.

### Update/Delete authorization

`UpdateMessage` and `DeleteMessage` have both author and moderator paths:
- `HasModPermission` / `hasModPermission`: when false, SQL includes `author_id = $N`.
- When true, owner check is skipped. Caller determines moderator status.
- Two `DeleteMessage` statements: `DeleteMessageStatement` (with author_id) and `DeleteMessageModStatement` (without).

## DB conventions

- **No foreign keys.** Integrity enforced at app layer.
- **CHECK constraints** on tables (e.g. `content <> '' OR jsonb_array_length(attachments) > 0`, `type <> 19 OR referenced_message_id IS NOT NULL`).
- **Soft delete**: `deleted_at BIGINT NOT NULL DEFAULT 0`. Queries filter `deleted_at = 0` for active.
- **Emoji model**: `emojis` table stores only custom guild emojis. Unicode emojis use `emoji_id = 0` in `reactions`. Reaction summaries LEFT JOIN `emojis` — use `COALESCE(e.animated, FALSE)`, `COALESCE(e.image_key, '')` for null safety.

## Message service specifics

### Pagination (cursor-based)
- `before` / `after`: single query with `id < cursor` or `id > cursor`.
- `around`: two queries — older (`id <= anchor DESC`) and newer (`id > anchor ASC`). Each queries full `limit` rows; the combined result is centered around the anchor and trimmed to `limit`. Unused capacity on one side backfills from the other.

### Message types & flags validation
- `normalizeMessageType`: only accepts `DEFAULT` and `REPLY`. `UNSPECIFIED` → `DEFAULT`.
- `validateFlags`: only `SUPPRESS_NOTIFICATIONS` is client-settable. `HAS_THREAD` is rejected.
- `ReferencedMessageID` / `ReferencedChannelID` only valid on `MESSAGE_TYPE_REPLY`.
- `THREAD_STARTER` is rejected from client requests (reserved for future thread service).

### Events (outbox → Kafka)
Event types: `message_created`, `message_updated`, `message_deleted`, `reaction_added`, `reaction_removed`.

- Create/update/delete/reaction handlers all write outbox events inside `Transact`, then call `Relay.Notify()`.
- Both `message_created` and `message_updated` payloads include `mention_user_ids`.
- Reaction events include `message_id`, `channel_id`, `user_id`, `emoji_id`, `emoji_name`.
- Events are partitioned by `channel_id` using `outbox.PartitionForKey()`.

### Emoji CDN URLs
- `messageServer.resolveEmojiImageURLs()` constructs `ImageURL` from `Emoji.ImageKey` + `EmojiCDNBaseURL` config.
- Called before converting summaries to proto in `GetMessage` and `ListMessages`.

## Error handling

1. Internal gRPC services: `rpcerror.New(code, domain, reason, message)` with `google.rpc.ErrorInfo`.
2. API Gateway: `apierror.FromRPC(err)` maps domain/reason → Connect error codes.
3. Handlers use `mapStoreError()`: `sql.ErrNoRows → notFound()`, `store.ErrPermissionDenied → permissionDenied()`, PG check violation `23514 → invalidRequest()`.

Domain/reason constants: `pkg/rpcerror/{authenticator,user,message}.go`. Mappings: `pkg/apierror/error.go`.

## Input validation

- Email format validated in User service (`isValidEmail` in `services/user/v1/internal/server/validation.go`) and Authenticator (`services/authenticator/v1/server/validation.go`).
- Name: trimmed, max 64 chars (`validateName` in user server).
- Password: non-empty check in `ChangePassword` before hashing.
- `GetUser` and other User handlers map `sql.ErrNoRows` → gRPC `NotFound` via `mapStoreError()`.

## Test patterns

Three isolation layers:

- **Fake stores**: in-memory maps implementing store interface. Used by server tests. No dependencies.
- **sqlmock**: `sqlmock.QueryMatcherRegexp` mode with `sqlPattern()` helpers. Exact query verification.
- **Integration tests** (`//go:build integration`): per-test Postgres schemas via `search_path` (`cordis_test_<nanos>`). Use `internal/testpostgres.New(t, migrationsFS)`. Requires `CORDIS_TEST_POSTGRES_DSN`.

Test assertions use `github.com/stretchr/testify/require` (not manual `if/t.Fatalf`). The `require` package fails immediately on assertion failure; use it for all test checks.

Outbox relay tests use `FakeProducer` (`pkg/outbox/fake_producer.go`) — an async mock that matches `kgo.Client` semantics (buffer on `Produce`, call promises on `Flush`). Supports `Err` for global failure and `FailTopics` for per-topic control.

## Observability

- API Gateway: own OpenTelemetry, Prometheus on `:6060` (not public), `slog` JSON to stdout, `/health` on metrics port (204).
- Internal gRPC services: go-zero devServer for metrics/health, config-based tracing/middlewares.

## Snowflake IDs

Custom node ID: hash non-loopback IP with FNV-32a → 16-bit node ID. Epoch = 2025-01-01. 16 node bits, 8 step bits. Shared across all services.
