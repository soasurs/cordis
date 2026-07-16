# AGENTS.md

## Git

- Commits in this repo must use `git commit -s` for sign-off. Do not add co-author trailers.

## Commands

```bash
make generate          # buf generate for external and internal protos
make lint              # buf lint
make test              # go test ./...
make test-integration  # Postgres integration tests only; requires CORDIS_TEST_POSTGRES_DSN

# Focused checks
go test ./services/gateway/v1/internal/server/... -v -count=1
go test ./services/message/v1/internal/server/... -v -count=1
go test ./services/message/v1/internal/store/... -v -count=1
go build ./services/message/v1/...
```

- Go module is `github.com/soasurs/cordis`; `go.mod` declares Go `1.26`.
- Codegen needs `buf`, `protoc-gen-go`, `protoc-gen-connect-go`, `protoc-gen-go-grpc`, and `protoc-gen-es` on `PATH`.
- After editing any `.proto`, run `make generate`; generated outputs under `gen/` are not hand-edited.

## Services

- `services/api/v1`: public Connect-RPC HTTP API on `:8080`; currently proxies auth requests to Authenticator gRPC and maps errors through `pkg/apierror`.
- `services/gateway/v1`: websocket gateway on `:8081` plus internal gRPC on `:3004`; verifies access tokens via Authenticator and records sessions/routes via Presence.
- `services/presence/v1`: gRPC on `:3003`; Redis-backed gateway liveness, channel routing, and user presence TTLs.
- `services/authenticator/v1`: go-zero `zrpc` on `:3001`; JWT access/refresh tokens, sessions in Postgres, calls User gRPC.
- `services/user/v1`: go-zero `zrpc` on `:3000`; users/profiles in Postgres, Argon2id password hashing.
- `services/message/v1`: go-zero `zrpc` on `:3002`; messages/reactions/mentions in Postgres, outbox events to Kafka when Kafka is configured.

## Proto

- Protos use edition 2023. Internal generation uses opaque Go API (`default_api_level=API_OPAQUE`), so use generated getters/setters/builders instead of field access or struct literals for `gen/{authenticator,user,message,presence,gateway}`.
- External `proto/api` generation is open Go API plus Connect-Go and protobuf-es TypeScript under `gen/web`; existing API code uses pointer fields and struct literals there.
- `buf.gen.external.yaml` only includes `proto/api`; `buf.gen.internal.yaml` includes `proto/authenticator`, `proto/user`, `proto/message`, `proto/presence`, and `proto/gateway`.

## Service Wiring

- Service construction pattern is `Config -> NewDependencies(cfg) -> NewServiceContextWithDependencies(cfg, deps)`.
- `NewDependencies` creates real DB/RPC/Redis/Kafka/Snowflake/token dependencies; tests inject fakes via `NewServiceContextWithDependencies` or direct `ServiceContext` literals instead.
- `NewServiceContextWithDependencies` is fail-fast and panics on missing required deps.
- Config is loaded with `conf.LoadConfig(..., conf.UseEnv())`, so `${CORDIS_*}` values in YAML are environment-expanded.

## Storage And Migrations

- Postgres services embed SQL migrations with `//go:embed *.sql`; `pkg/migration.Apply()` applies lexicographically and skips `*.down.sql`.
- Integration tests create per-test schemas named `cordis_test_<nanos>` via `search_path`; do not assume public schema state.
- Stores define interfaces. SQL stores keep both `*sqlx.DB` and `sqlx.ExtContext` (`q`), where `q` is the DB normally and a `*sqlx.Tx` inside `Transact`.
- User and Message stores have `Transact` rollback-on-error/panic behavior; Authenticator store does not currently expose transactions.
- DB integrity is mostly app-enforced: migrations have no foreign keys; use soft delete fields (`deleted_at = 0`) and CHECK constraints.

## Message Service

- `CreateMessage`, `UpdateMessage`, `DeleteMessage`, `AddReaction`, and `RemoveReaction` write outbox events inside the DB transaction, then call `Relay.Notify()` after commit.
- Event types are `message_created`, `message_updated`, `message_deleted`, `reaction_added`, and `reaction_removed`; events are partitioned by `channel_id` via `outbox.PartitionForKey()`.
- `message_created` and `message_updated` payloads include `mention_user_ids`; reaction events include `message_id`, `channel_id`, `user_id`, `emoji_id`, and `emoji_name`.
- Message list pagination is cursor-based: `before`/`after` use one query; `around` queries older and newer sides and trims/backfills to the requested limit.
- Client-settable message types are only `DEFAULT` and `REPLY`; `UNSPECIFIED` normalizes to `DEFAULT`, and `THREAD_STARTER` is reserved.
- Client-settable flags only include `SUPPRESS_NOTIFICATIONS`; `HAS_THREAD` is rejected.
- Reply fields must be set together, and the referenced channel must match the referenced message.
- Custom guild emojis live in `emojis`; Unicode reactions use `emoji_id = 0`. Reaction summaries left join emojis and use `COALESCE` for null-safe `animated` and `image_key`.

## Gateway And Presence

- Gateway websocket protocol opcodes are in `services/gateway/v1/internal/server/protocol.go`; first client message after `HELLO` must be `IDENTIFY` (`op=2`) with an access token.
- Gateway tracks local subscriptions in memory and refreshes aggregate channel routes in Presence; callers resolve target gateways via Presence before calling gateway dispatch gRPC.
- Presence Redis keys are TTL-based; stale gateway generations and expired sessions/routes are filtered during reads.
- Invisible presence resolves as offline and hides sessions.

## Errors

- Internal domain errors use `pkg/rpcerror.New(code, domain, reason, message)` with `google.rpc.ErrorInfo`.
- Public API errors go through `apierror.FromRPC(err)`, which maps known domain/reason pairs to Connect errors with `api.v1.PublicErrorInfo` details.
- Message handlers use `mapStoreError()`: `sql.ErrNoRows` to message not found, `store.ErrPermissionDenied` to permission denied, Postgres CHECK violation `23514` to invalid request.
- Gateway and Presence currently use plain gRPC `status.Error` for validation errors.

## Tests

- Unit tests use `github.com/stretchr/testify/require`; follow that style for new assertions.
- Store unit tests commonly use `sqlmock.QueryMatcherRegexp` plus local `sqlPattern()` helpers; keep SQL expectations exact enough to catch query changes.
- `make test-integration` runs Postgres integration tests for User, Authenticator, Message, and outbox packages. Example DSN: `CORDIS_TEST_POSTGRES_DSN="postgres://cordis:cordis@127.0.0.1:5432/cordis?sslmode=disable"`.
- Presence Redis integration tests are separate: run `CORDIS_TEST_REDIS_ADDR=127.0.0.1:6379 go test -tags=integration ./services/presence/v1/internal/store -v -count=1`.
- Outbox relay tests use `pkg/outbox.FakeProducer`, which buffers async produce calls and resolves callbacks on `Flush`; it supports global `Err` and per-topic `FailTopics`.

## Runtime Env

- Authenticator config requires `CORDIS_ACCESS_TOKEN_SECRET` and `CORDIS_REFRESH_TOKEN_SECRET` for real token manager startup.
- Optional tracing endpoint is read from `CORDIS_OTEL_ENDPOINT` in service configs.
- Message Kafka config is optional; if no Kafka seeds are configured, the Kafka client/relay are not created and outbox rows accumulate.
- Snowflake IDs use a custom node derived from non-loopback IP hash, epoch `2025-01-01`, 16 node bits, and 8 step bits.
