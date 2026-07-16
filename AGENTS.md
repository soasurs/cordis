# AGENTS.md

## Git

- Commits in this repo must use `git commit -s` for sign-off. Do not add co-author trailers.

## Commands

```bash
make generate          # buf generate for external and internal protos
make lint              # buf lint
make test              # go test ./...

# Focused checks
go test ./services/gateway/v1/internal/server/... -v -count=1
go test ./services/message/v1/internal/server/... -v -count=1
go test ./services/message/v1/internal/store/... -v -count=1
go build ./services/message/v1/...
go test ./services/guild/v1/internal/server/... -v -count=1
go test ./services/guild/v1/internal/store/... -v -count=1
go build ./services/guild/v1/...
```

- Go module is `github.com/soasurs/cordis`; `go.mod` declares Go `1.26`.
- Codegen needs `buf`, `protoc-gen-go`, `protoc-gen-connect-go`, `protoc-gen-go-grpc`, and `protoc-gen-es` on `PATH`.
- After editing any `.proto`, run `make generate`; generated outputs under `gen/` are not hand-edited.

## Services

- `services/api/v1`: public Connect-RPC HTTP API on `:8080`; authenticates callers, proxies User/Message/Guild operations to internal gRPC services, and maps errors through `pkg/apierror`.
- `services/gateway/v1`: websocket gateway on `:8081` plus internal gRPC on `:3004`; verifies access tokens via Authenticator and records sessions/routes via Presence.
- `services/presence/v1`: gRPC on `:3003`; Redis-backed gateway liveness, channel routing, and user presence TTLs.
- `services/authenticator/v1`: go-zero `zrpc` on `:3001`; JWT access/refresh tokens, sessions in Postgres, calls User gRPC.
- `services/user/v1`: go-zero `zrpc` on `:3000`; users/profiles in Postgres, Argon2id password hashing.
- `services/message/v1`: go-zero `zrpc` on `:3002`; messages/reactions/mentions in Postgres, publishes message events directly to Kafka when Kafka is configured.
- `services/guild/v1`: go-zero `zrpc` on `:3005`; guilds/members/roles in Postgres, calls User gRPC when directly adding members, and publishes guild events to its own Kafka topic.

## Proto

- Protos use edition 2023. Internal generation uses opaque Go API (`default_api_level=API_OPAQUE`), so use generated getters/setters/builders instead of field access or struct literals for `gen/{authenticator,user,message,guild,presence,gateway}`.
- External `proto/api` generation is open Go API plus Connect-Go and protobuf-es TypeScript under `gen/web`; existing API code uses pointer fields and struct literals there.
- `buf.gen.external.yaml` only includes `proto/api`; `buf.gen.internal.yaml` includes `proto/authenticator`, `proto/user`, `proto/message`, `proto/guild`, `proto/presence`, and `proto/gateway`.

## Service Wiring

- Service construction pattern is `Config -> NewDependencies(cfg) -> NewServiceContextWithDependencies(cfg, deps)`.
- `NewDependencies` creates real DB/RPC/Redis/Kafka/Snowflake/token dependencies; tests inject fakes via `NewServiceContextWithDependencies` or direct `ServiceContext` literals instead.
- `NewServiceContextWithDependencies` is fail-fast and panics on missing required deps.
- Config is loaded with `conf.LoadConfig(..., conf.UseEnv())`, so `${CORDIS_*}` values in YAML are environment-expanded.

## Storage And Migrations

- Postgres services embed SQL migrations with `//go:embed *.sql`; `pkg/migration.Apply()` applies lexicographically and skips `*.down.sql`.
- Stores define interfaces. SQL stores keep both `*sqlx.DB` and `sqlx.ExtContext` (`q`), where `q` is the DB normally and a `*sqlx.Tx` inside `Transact`.
- User, Message, and Guild stores have `Transact` rollback-on-error/panic behavior; Authenticator store does not currently expose transactions.
- DB integrity is mostly app-enforced: migrations have no foreign keys; use soft delete fields (`deleted_at = 0`) and CHECK constraints.

## Message Service

- Message mutations commit their database transaction before publishing directly to Kafka; do not introduce a transactional outbox unless explicitly requested.
- Message event values use the lightweight `{"t":"message.created","d":{...}}` envelope and are keyed by decimal `channel_id`.
- Kafka publication is best-effort: publication failures are logged but do not turn an already-committed mutation into an RPC failure.
- `message.created` and `message.updated` payloads include `mention_user_ids`.
- Message list pagination is cursor-based: `before`/`after` use one query; `around` queries older and newer sides and trims/backfills to the requested limit.
- Client-settable message types are only `DEFAULT` and `REPLY`; `UNSPECIFIED` normalizes to `DEFAULT`, and `THREAD_STARTER` is reserved.
- Client-settable flags only include `SUPPRESS_NOTIFICATIONS`; `HAS_THREAD` is rejected.
- Reply fields must be set together, and the referenced channel must match the referenced message.
- Custom guild emojis live in `emojis`; Unicode reactions use `emoji_id = 0`. Reaction summaries left join emojis and use `COALESCE` for null-safe `animated` and `image_key`.

## Guild Service

- Guild metadata RPCs cover create/get/list/update/delete. Creating a guild transactionally creates the owner membership and the `@everyone` default role; the default role ID equals the guild ID.
- Guild metadata uses soft deletion and a `revision` starting at 1. Updates and deletion increment the revision.
- Guild reads require active membership. Non-members and deleted guilds are returned as not found. Metadata updates require `MANAGE_GUILD`; deletion and ownership transfer remain owner-only.
- `ListUserGuilds` uses descending Snowflake IDs and a `before` cursor.
- Member RPCs cover direct add, get/list, updating the caller's nickname, kick, leave, and ownership transfer.
- Direct member addition requires `MANAGE_MEMBERS` and verifies the target through User gRPC. Kicking requires `KICK_MEMBERS` plus a strictly higher top role.
- The owner cannot leave or be kicked and must transfer ownership to another active member first.
- Active duplicate membership returns `AlreadyExists`. A removed member may rejoin; the existing row is restored and its membership `revision` continues increasing.
- Member lists use descending `user_id` and a `before_user_id` cursor. Nicknames are trimmed, may be cleared, and are limited to 32 Unicode code points.
- Guild-level permissions are `ADMINISTRATOR`, `MANAGE_GUILD`, `MANAGE_ROLES`, `MANAGE_MEMBERS`, and `KICK_MEMBERS`. Effective permissions OR the implicit `@everyone` role with explicitly assigned active roles.
- Channel permissions add `VIEW_CHANNEL`, `SEND_MESSAGES`, `MANAGE_CHANNELS`, and `MANAGE_MESSAGES`. New and migrated `@everyone` roles grant `VIEW_CHANNEL | SEND_MESSAGES` by default.
- Guild owners implicitly receive all Guild permissions. `ADMINISTRATOR` expands to all current Guild permissions, but role hierarchy still applies to non-owner moderation and role operations.
- `guild_member_roles` stores explicit role assignments. The `@everyone` role is implicit, cannot be assigned or deleted, keeps position 0, and only its permissions may be updated.
- Role operations require `MANAGE_ROLES`. Non-owners may only manage roles and members strictly below their highest role and cannot create, edit, or assign permissions they do not hold.
- Role deletion and member removal delete explicit role assignments transactionally. Deleted roles are excluded from permission calculation.
- Guild owns text channels and channel permission overwrites. Channel ordering uses ascending `position`; only `GUILD_CHANNEL_TYPE_TEXT` is currently supported.
- Channel overwrite precedence is deterministic: `@everyone`, aggregated assigned-role denies/allows, then the member overwrite. Owner and `ADMINISTRATOR` bypass channel overwrites.
- Denying `VIEW_CHANNEL` also removes `SEND_MESSAGES` and `MANAGE_MESSAGES`. Guild channel reads hide non-visible channels as not found.
- Role overwrite targets must be manageable by the actor; member overwrite targets must be below the actor's highest role. Overwrite allow/deny bitsets cannot overlap.
- Guild has an independent Kafka topic, defaulting to `cordis.guild.events.v1`; do not mix Guild events into the Message topic.
- Guild publishes directly to Kafka after the database transaction commits and does not use an outbox.
- Guild event values use the same lightweight envelope as Message: `{"t":"guild.updated","d":{...}}`. The Kafka key is the decimal `guild_id`.
- Snowflake IDs and permission bitsets in Kafka JSON are strings; revisions, timestamps, and enums remain JSON numbers.
- Current event types additionally include `guild.channel.created`, `guild.channel.updated`, `guild.channel.deleted`, `guild.channel.overwrite.updated`, and `guild.channel.overwrite.deleted`.
- Message calls Guild `AuthorizeGuildChannel` for every create/read/list/update/delete operation. Message no longer trusts a caller-provided moderator boolean; non-author edits/deletes require `MANAGE_MESSAGES`.
- Later Guild phases own Gateway authorization integration, realtime distribution, then invites/bans/audit/threads.

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
- Presence Redis integration tests are separate: run `CORDIS_TEST_REDIS_ADDR=127.0.0.1:6379 go test -tags=integration ./services/presence/v1/internal/store -v -count=1`.

## Runtime Env

- Authenticator config requires `CORDIS_ACCESS_TOKEN_SECRET` and `CORDIS_REFRESH_TOKEN_SECRET` for real token manager startup.
- Optional tracing endpoint is read from `CORDIS_OTEL_ENDPOINT` in service configs.
- Message config requires a Guild gRPC endpoint for channel authorization. Message and Guild Kafka configs are optional; if no Kafka seeds are configured, no Kafka producer is created and mutations still succeed without event publication.
- Snowflake IDs use a custom node derived from non-loopback IP hash, epoch `2025-01-01`, 16 node bits, and 8 step bits.
