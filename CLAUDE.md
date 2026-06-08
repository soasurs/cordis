# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & test commands

```bash
make generate        # Proto code generation (requires buf, protoc-gen-go, protoc-gen-connect-go, protoc-gen-go-grpc)
make test            # All unit tests (go test ./...)
make lint            # Proto lint (buf lint)

# Integration tests (requires real Postgres)
CORDIS_TEST_POSTGRES_DSN="postgres://..." make test-integration

# Single-package tests
go test ./services/message/v1/internal/server/... -v -count=1
go test ./pkg/apierror/... -v -count=1
```

All generated code lives under `gen/` — never edit it directly. External-facing proto messages (`proto/api/`) use `buf.gen.external.yaml` (Connect-Go + protobuf-es for web). Internal proto messages (`proto/authenticator/`, `proto/user/`, `proto/message/`) use `buf.gen.internal.yaml` (gRPC + protobuf-go). Both use `buf` managed mode with `go_package_prefix: github.com/soasurs/cordis/gen`.

Proto is edition 2023. Fields use implicit presence (no `optional` keyword, no `features.field_presence = EXPLICIT`). Zero value means "not set". Generated Go setters use `SetXxx(value)` — always use setters, never struct literals for proto messages.

## Architecture

```
Client ──(Connect-RPC/HTTP)──> API Gateway (:8080) ──(gRPC)──> Authenticator (:3001)
                                                                    │
                                                                    └──(gRPC)──> User (:3000)

                                                                Message (:3002)
```

**API Gateway** (`services/api/v1/`) — the only internet-facing service. Connect-RPC protocol, thin translation layer: extracts `User-Agent` / client IP, proxies to internal gRPC services, maps gRPC errors to Connect errors with public error codes. Uses standard `net/http` (not go-zero's server framework).

**Authenticator** (`services/authenticator/v1/`) — registration, login, token refresh/revocation, access token verification. JWT HS256 with separate access/refresh secrets. Refresh token hash (SHA-256) stored in sessions table; rotation on each refresh. go-zero `zrpc` server.

**User** (`services/user/v1/`) — user records and profiles. Creates user + profile in a single transaction. Password hashing via Argon2id (PHC string format). go-zero `zrpc` server.

**Message** (`services/message/v1/`) — message CRUD, reactions, mentions. Cursor-based pagination (before/after/around). Channel/thread management belongs to the future guild service; message service treats `channel_id` as an opaque reference. go-zero `zrpc` server.

## Proto & DB conventions

- **No foreign keys**. Referential integrity is enforced at the application layer. The project is consistent on this across all services.
- **CHECK constraints** are used aggressively to enforce business rules at the DB level (e.g. `content <> '' OR jsonb_array_length(attachments) > 0`, `type <> 19 OR referenced_message_id IS NOT NULL`). Application validation catches most issues; CHECK acts as a safety net.
- **Soft delete**: all tables use `deleted_at BIGINT NOT NULL DEFAULT 0`. Queries filter `WHERE deleted_at = $N` with `0` for active records. `UpdateMessage`/`DeleteMessage` use `UPDATE … SET deleted_at = $1` rather than actual deletes.

## Error handling chain

1. Internal gRPC services return `rpcerror.New(code, domain, reason, message)` — attaches `google.rpc.ErrorInfo` details to the gRPC status.
2. API Gateway's `apierror.FromRPC(err)` maps known domain/reason pairs to Connect errors with `PublicErrorInfo` details. Unknown reasons map to `CodeInternal`.
3. Connect interceptors extract the public code for Prometheus labels and structured logging.

Domain/reason constants live in `pkg/rpcerror/{authenticator,user,message}.go`. Mappings in `pkg/apierror/error.go`.

Server handlers use `mapStoreError()` to translate DB errors (e.g. `sql.ErrNoRows → notFound()`, `store.ErrPermissionDenied → permissionDenied()`, PostgreSQL CHECK violation `23514 → invalidRequest()`).

## Service context pattern

Every service uses the same dependency wiring: `config.Config → NewDependencies(cfg) → NewServiceContextWithDependencies(cfg, deps)`. `NewDependencies` constructs real infrastructure (DB, RPC clients, snowflake node, token manager). `NewServiceContextWithDependencies` panics on nil dependencies (fail-fast at startup). Tests bypass `NewDependencies` and inject fakes into `ServiceContext` directly.

## Store layer

Each store defines an interface, implemented by `SQLStore` holding `*sqlx.DB` and `sqlx.ExtContext` (the latter is either `*sqlx.DB` or `*sqlx.Tx` for transactions). `Transact` replaces `q` with a transaction-bound `*sqlx.Tx`. Dynamic query building uses positional parameters (`$1, $2, ...`).

SQL migration files are embedded via `//go:embed *.sql` and applied by `pkg/migration.Apply()` in lexicographic order.

## Emoji model

Only custom guild emojis are stored in the `emojis` table (id, guild_id, name, image_key, animated, created_by). Unicode emojis have no database representation — in the `reactions` table they use `emoji_id = 0, emoji_name = "🔥"`. Reaction summaries JOIN `emojis` via LEFT JOIN (Unicode emojis won't match, image_url comes back empty).

## Message update/delete authorization

`UpdateMessage` and `DeleteMessage` support both author and moderator paths. `UpdateMessageParams.HasModPermission` / `DeleteMessage(…, hasModPermission)` controls whether the SQL includes `author_id = $N`. When true (moderator), the owner check is skipped. The caller (API Gateway or guild service) determines moderator status; the message service trusts the flag.

Two separate `DeleteMessage` SQL statements exist: `DeleteMessageStatement` (with `author_id`) and `DeleteMessageModStatement` (without). `UpdateMessage` builds the WHERE clause dynamically. Both paths use the primary key index on `id` — there is no index concern.

## Test patterns

Three layers of test isolation:

- **Fake stores** (e.g. `fakeStore`): in-memory maps implementing the store interface. Used by server-level tests. Fast, no dependencies.
- **sqlmock** tests: verify exact SQL queries and arguments. Use `sqlmock.QueryMatcherRegexp` mode; build expected patterns via `sqlPattern()`.
- **Integration tests** (`//go:build integration`): real Postgres with per-test temporary schemas (via `search_path`). Schema name is `cordis_test_<nanos>`. Requires `CORDIS_TEST_POSTGRES_DSN` env var. Use `internal/testpostgres.New(t, migrationsFS)`.

## Observability

API Gateway has its own observability layer (`services/api/v1/observability/`): OpenTelemetry tracing (OTLP gRPC exporter), Prometheus metrics (served on separate port `:6060`, not exposed publicly), structured logging via `slog` (JSON to stdout). Health check lives on the metrics port at `/health` (204 No Content).

Internal gRPC services use go-zero's built-in telemetry (devServer for metrics/health, config-based tracing/middlewares).

## Snowflake IDs

Custom node ID generation in `pkg/snowflake`: hashes the machine's non-loopback IP address with FNV-32a to produce a 16-bit node ID. Epoch is 2025-01-01. All services share the same config (16 node bits, 8 step bits).
