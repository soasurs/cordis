# AGENTS.md

## Git

- Commits in this repo must use `git commit -s` for sign-off. Do not add co-author trailers.

## Commands

```bash
make generate          # buf generate for external and internal protos
make lint              # buf lint
make test              # go test ./...
make test-integration  # go test -tags=integration ./... (needs Docker; no pre-existing services)
make compose-up        # fixed-version local Postgres/Redis/Kafka/etcd for manual runs
make compose-down      # stop compose stack (named volumes kept; use `docker compose down -v` to wipe)

# Focused checks
go test ./services/gateway/v1/internal/server/... -v -count=1
go test ./services/session/v1/internal/server/... -v -count=1
go test ./services/dispatcher/v1/internal/server/... -v -count=1
go test ./services/message/v1/internal/server/... -v -count=1
go test ./services/message/v1/internal/store/... -v -count=1
go build ./services/message/v1/...
go test ./services/guild/v1/internal/server/... -v -count=1
go test ./services/guild/v1/internal/store/... -v -count=1
go build ./services/guild/v1/...
```

- Go module is `github.com/soasurs/cordis`; `go.mod` declares Go `1.26`.
- Codegen tools are pinned by the `tool` block in `go.mod` and invoked through `go tool`; no separate `PATH` installation is required.
- After editing any `.proto`, run `make generate`; generated outputs under `gen/` are not hand-edited.

## Services

- `services/api/v1`: public Connect-RPC HTTP API on `:8080`; authenticates callers, proxies User/Message/Guild operations to internal gRPC services, and maps errors through `pkg/apierror`. Public requests use Redis-backed named rate-limit policies with a bounded local fallback; every request consumes a high-volume source-IP guard while successful access-token verification consumes the primary per-user quota. Register/Login additionally consume endpoint IP and normalized-email-hash policies; password recovery, public profile reads, and email-availability checks have dedicated IP policies. Authenticated business policies cover message creation, relationship writes, Guild resource creation, and invite joins. `GetReadStates` also uses a process-local keyed semaphore to bound concurrent requests per user. Forwarded client addresses are honored only from explicitly configured trusted proxy CIDRs.
- `services/gateway/v1`: websocket gateway on `:8081`; forwards websocket frames over a bidirectional gRPC stream to Session and owns no logical session, replay, subscription, or Kafka state.
- `services/session/v1`: stateful gRPC service on `:3006`; owns logical sessions, sequence numbers, 2048-event in-memory replay windows, subscriptions, Presence updates, and Gateway stream bindings. Each logical session has a configuration-driven hard cap on distinct subscribed channels (default 500); over-limit subscription requests fail atomically. No-op Presence updates are discarded; changed updates are limited per logical session and by a distributed cross-device user quota.
- `services/dispatcher/v1`: Kafka consumer for the Guild, Message, User, and Presence topics; resolves aggregate Session-node routes in Redis and dispatches events to Session gRPC. Presence transitions fan out along two paths - the payload `guild_ids` through guild routes and the user's friends (paged through User `ListRelationships`) plus the user's own devices through user routes; recipients reachable through both paths receive duplicates, which is accepted because presence updates are idempotent state.
- `services/presence/v1`: gRPC on `:3003`; Redis-backed gateway liveness, channel routing, and user presence TTLs. Publishes `presence.updated` to its own optional Kafka topic (default `cordis.presence.events.v1`) only when the multi-device aggregate status actually transitions; heartbeat renewals stay silent. Same-user mutations and synchronous publication are serialized by a Redis lock. The pre/post aggregate snapshot happens in the server layer, and a failed pre-read skips the event rather than guessing.
- `services/authenticator/v1`: go-zero `zrpc` on `:3001`; JWT access/refresh tokens, sessions in Postgres, calls User gRPC. Owns password credentials in `user_credentials` (Argon2id through `pkg/password`): Login resolves the user through User gRPC and verifies locally, `ChangePassword` checks the old password and revokes the other sessions in one transaction, and Register writes the User row first and the credential second - a user row without a credential is a half-registration that the same email may claim by registering again. Process-local weighted semaphore capacity bounds all Argon2 work. Also owns account recovery: password reset and email verification tokens (sha256 hashes, single-use, per-user upsert) with delivery through the optional Mailer gRPC client (skipped when unconfigured). `RequestPasswordReset` always reports success to avoid account enumeration; `ConfirmPasswordReset` prechecks the token before Argon2, then rechecks under row lock while atomically consuming the token, upserting the credential, and revoking all sessions. Recovery requests are throttled per target through optional Redis (fail-open when Redis is down or unconfigured); throttled requests still report success.
- `services/user/v1`: go-zero `zrpc` on `:3000`; user identity, profiles, and social relationships in Postgres, no credential material. Emails are lowercased at every entry point (a CHECK constraint backstops storage). `users.email_verified_at` tracks verification; `UpdateEmail` clears it only when the address actually changes and `MarkEmailVerified` only applies while the supplied email is still current. Profiles carry a globally unique lowercase handle (`user_profiles.username`, `^[a-z0-9_]{2,32}$`, unique index plus CHECK backstop) chosen at registration and resolved through `GetUserProfileByUsername` (public `LookupUser`).
- `services/mailer/v1`: go-zero `zrpc` on `:3007`; stateless transactional mail delivery behind a `SendEmail(to, template, variables)` RPC. Template names and variable keys live in `pkg/mail`; the only provider today is `noop` (redacted logging, no delivery). Template variables may contain secrets and must never be logged verbatim.
- `services/message/v1`: go-zero `zrpc` on `:3002`; messages, mentions, and 1:1 DM channels in Postgres, publishes message events directly to Kafka when Kafka is configured, and calls User gRPC for relationship checks on DM flows. `GetReadStates` bounds Guild authorization fan-out and acquires process-local weighted capacity based on the deduplicated channel count. Message create/update enforce configuration-driven attachment and unique-mention limits (defaults 10 and 100).
- `services/guild/v1`: go-zero `zrpc` on `:3005`; guilds/members/bans/roles/channels in Postgres, calls User gRPC when directly adding or banning users, and publishes guild events to its own Kafka topic. Configuration-driven hard quotas bound owned/joined guilds, roles, channels, active invites, and permission overwrites; count-and-create paths use transaction-scoped PostgreSQL advisory locks.

## Proto

- Protos use edition 2023. Internal generation uses opaque Go API (`default_api_level=API_OPAQUE`), so use generated getters/setters/builders instead of field access or struct literals for `gen/{authenticator,user,message,guild,presence,session}`.
- External `proto/api` generation is open Go API plus Connect-Go under `gen/api`; existing API code uses pointer fields and struct literals there.
- `buf.gen.external.yaml` only includes `proto/api`; `buf.gen.internal.yaml` includes `proto/authenticator`, `proto/user`, `proto/message`, `proto/guild`, `proto/presence`, `proto/session`, and `proto/mailer`.

## Service Wiring

- Services other than Dispatcher use the construction pattern `Config -> NewDependencies(cfg) -> NewServiceContextWithDependencies(cfg, deps)`; Dispatcher constructs its Kafka consumer and route resolver directly.
- For ServiceContext-based services, `NewDependencies` creates real DB/RPC/Redis/Kafka/Snowflake/token dependencies; tests inject fakes via `NewServiceContextWithDependencies` or direct `ServiceContext` literals instead.
- `NewServiceContextWithDependencies` is fail-fast and panics on missing required deps.
- Config is loaded with `conf.LoadConfig(..., conf.UseEnv())`, so `${CORDIS_*}` values in YAML are environment-expanded.

## Storage And Migrations

- Postgres services embed SQL migrations with `//go:embed *.sql`; `pkg/migration.Apply()` applies lexicographically and skips `*.down.sql`.
- Stores define interfaces. SQL stores keep both `*sqlx.DB` and `sqlx.ExtContext` (`q`), where `q` is the DB normally and a `*sqlx.Tx` inside `Transact`.
- User, Message, and Guild stores have `Transact` rollback-on-error/panic behavior; Authenticator store does not currently expose transactions.
- DB integrity is mostly app-enforced: migrations have no foreign keys; use soft delete fields (`deleted_at = 0`) and CHECK constraints.

## User Relationships

- `user_relationships` stores one directed row per side: `(user_id, target_id, type)` with types outgoing(1)/incoming(2)/friend(3)/blocked(4). Mutations maintain both directions inside one transaction.
- State machine: sending a request creates outgoing/incoming; sending when an incoming request exists auto-accepts into a friendship; repeating a pending request is a silent no-op; an existing friendship returns `AlreadyExists`; any block in either direction rejects requests with `PermissionDenied`.
- `RemoveFriend` deletes friendships or retracts pending requests in either direction; blocks are lifted only through `UnblockUser`, which removes the caller's row alone (a mutual block held by the other side survives). Blocking overwrites the caller's row and deletes the reverse row unless it is itself a block.
- Blocks are private: the blocker's devices receive `relationship.updated`, the blocked side only ever sees `relationship.removed`. MVP scope: blocks gate friend requests (and later DMs); profiles stay visible.
- User publishes `relationship.updated`/`relationship.removed` to its own Kafka topic (default `cordis.user.events.v1`, optional config like Guild/Message) after the transaction commits. The Kafka key is the decimal recipient `user_id`; each mutation emits one event per affected user.
- Dispatcher consumes the user topic and routes `relationship.*` events by the payload `user_id` through Redis user routes to Session `DispatchUserEvent`, which forwards user events verbatim without type-specific handling.
- Relationship target existence is checked locally in the User store; the flows make no cross-service calls.

## Message Service

- Message mutations commit their database transaction before publishing directly to Kafka; do not introduce a transactional outbox unless explicitly requested.
- Message has an independent Kafka topic, defaulting to `cordis.message.events.v1`.
- Message event values use the lightweight `{"t":"message.created","d":{...}}` envelope. Guild message records carry `guild_id` and are keyed by decimal Guild ID. DM message records remain channel-keyed until the user-route producer migration is delivered.
- Realtime domain event names use dot-separated hierarchy only. Shared names live in `pkg/realtime`; do not introduce underscore variants such as `message_created`.
- Kafka publication is best-effort: publication failures are logged but do not turn an already-committed mutation into an RPC failure.
- `message.created` and `message.updated` payloads include `mention_user_ids`.
- Message list pagination is cursor-based: `before`/`after` use one query; `around` queries older and newer sides and trims/backfills to the requested limit.
- Client-settable message types are only `DEFAULT` and `REPLY`; `UNSPECIFIED` normalizes to `DEFAULT`, and `THREAD_STARTER` is reserved.
- Client-settable flags only include `SUPPRESS_NOTIFICATIONS`; `HAS_THREAD` is rejected.
- Reply fields must be set together, and the referenced channel must match the referenced message.
- Message operations are supported only in text channels; category and voice channels are rejected.
- Reaction and custom emoji RPCs are not currently implemented; the latest Message migration removes the old `reactions` and `emojis` tables.
- Message creation and updates accept at most 10 attachments and 100 unique mention user IDs by default; both limits are configuration-driven and return `ResourceExhausted` when exceeded.

## Guild Service

- Guild metadata RPCs cover create/get/list/update/delete. Creating a guild transactionally creates the owner membership and the `@everyone` default role; the default role ID equals the guild ID.
- Guild metadata uses soft deletion and a `revision` starting at 1. Updates and deletion increment the revision.
- Guild reads require active membership. Non-members and deleted guilds are returned as not found. Metadata updates require `MANAGE_GUILD`; deletion and ownership transfer remain owner-only.
- `ListUserGuilds` uses descending Snowflake IDs and a `before` cursor.
- Member RPCs cover direct add, get/list, updating the caller's nickname, kick, leave, and ownership transfer.
- Direct member addition requires `MANAGE_MEMBERS` and verifies the target through User gRPC. Kicking requires `KICK_MEMBERS` plus a strictly higher top role. Banning requires `BAN_MEMBERS`, supports current members and non-members, and also enforces role hierarchy for active members.
- The owner cannot leave or be kicked and must transfer ownership to another active member first.
- Active duplicate membership returns `AlreadyExists`. A removed member may rejoin; the existing row is restored and its membership `revision` continues increasing. A banned user cannot be added until unbanned.
- Member lists use descending `user_id` and a `before_user_id` cursor. Nicknames are trimmed, may be cleared, and are limited to 32 Unicode code points.
- Guild-level permissions are `ADMINISTRATOR`, `MANAGE_GUILD`, `MANAGE_ROLES`, `MANAGE_MEMBERS`, `KICK_MEMBERS`, `BAN_MEMBERS`, and `CREATE_INVITE`. Effective permissions OR the implicit `@everyone` role with explicitly assigned active roles.
- Channel permissions add `VIEW_CHANNEL`, `SEND_MESSAGES`, `MANAGE_CHANNELS`, and `MANAGE_MESSAGES`. New and migrated `@everyone` roles grant `VIEW_CHANNEL | SEND_MESSAGES | CREATE_INVITE` by default.
- Guild owners implicitly receive all Guild permissions. `ADMINISTRATOR` expands to all current Guild permissions, but role hierarchy still applies to non-owner moderation and role operations.
- `guild_member_roles` stores explicit role assignments. The `@everyone` role is implicit, cannot be assigned or deleted, keeps position 0, and only its permissions may be updated.
- Role operations require `MANAGE_ROLES`. Non-owners may only manage roles and members strictly below their highest role and cannot create, edit, or assign permissions they do not hold.
- Role deletion and member removal delete explicit role assignments transactionally. Deleted roles are excluded from permission calculation.
- Guild owns text, category, and voice channel metadata plus channel permission overwrites. Text and voice channels may reference a category through `parent_id`; categories cannot be nested. Deleting a category moves its children to the Guild root. Voice functionality beyond metadata is not implemented.
- Guild invites live in `guild_invites` with random unique codes, optional `max_uses`, and optional TTL (`expires_at`, 0 = never). Creating requires `CREATE_INVITE`; listing requires `MANAGE_GUILD`; deleting is allowed for the creator or `MANAGE_GUILD`. `GetGuildInvite` returns an authenticated pre-join preview (guild name/icon/member count) without membership.
- `JoinGuildByInvite` consumes one use and creates or restores the membership in the same transaction; banned users, exhausted or expired codes, and deleted guilds fail without burning a use. Joins reuse the `guild.member.joined` event; invite CRUD publishes no events.
- Guild hard quotas default to 10 owned and 100 joined guilds per user, 250 roles and 500 channels per guild, 100 active invites per guild, and 100 permission overwrites per channel. The `@everyone` role counts toward the role limit; expired and exhausted invites do not count. Quota checks use `READ COMMITTED` transactions plus scope-keyed `pg_advisory_xact_lock`, with the count issued after lock acquisition.
- Channel overwrite precedence is deterministic: `@everyone`, aggregated assigned-role denies/allows, then the member overwrite. Owner and `ADMINISTRATOR` bypass channel overwrites.
- Denying `VIEW_CHANNEL` also removes `SEND_MESSAGES` and `MANAGE_MESSAGES`. Guild channel reads hide non-visible channels as not found.
- Role overwrite targets must be manageable by the actor; member overwrite targets must be below the actor's highest role. Overwrite allow/deny bitsets cannot overlap.
- Guild has an independent Kafka topic, defaulting to `cordis.guild.events.v1`; do not mix Guild events into the Message topic.
- Guild publishes directly to Kafka after the database transaction commits and does not use an outbox.
- Guild event values use the same lightweight envelope as Message: `{"t":"guild.updated","d":{...}}`. The Kafka key is the decimal `guild_id`.
- Snowflake IDs and permission bitsets in Kafka JSON are strings; revisions, timestamps, and enums remain JSON numbers.
- Guild publishes metadata, member, ban, role, channel, and channel-overwrite events; `pkg/realtime/events.go` is the canonical list of event names.
- Message calls Guild `AuthorizeGuildChannel` for every create/read/list/update/delete operation. Message no longer trusts a caller-provided moderator boolean; non-author edits/deletes require `MANAGE_MESSAGES`.
- DM channels live in `dm_channels` (`user_lo < user_hi`, one channel per pair). Channel authorization checks the local table first; a hit applies DM semantics and a miss falls through to the Guild path.
- `channel_read_states` stores `(user_id, channel_id, last_read_message_id)` per channel; `AckMessage` moves the cursor forward with `GREATEST` semantics and `GetReadStates` computes the unread message count (author-excluded) and the unread @-mention count in real time by joining `message_mentions` through a dedicated index. API bounds concurrent `GetReadStates` calls per user with a process-local keyed semaphore; Message bounds aggregate work with a process-local weighted semaphore whose weight is the deduplicated channel count. Both capacities and the authorization worker count are configuration-driven.
- Opening a DM requires an active friendship (`CreateDmChannel` is idempotent per pair and races resolve through insert-if-absent). Sending in a DM requires that neither side blocks the other, checked through one snapshot `CheckRelationships(include_reverse)` call; reading history stays available while blocked. DMs have no moderators: non-authors can never edit or delete.
- `dm.channel.created` is published to the Message topic but is user-routed: one record per participant keyed by the decimal recipient user ID. Dispatcher routes `dm.*` and `relationship.*` records by the payload `user_id`; Session subscriptions fall back to Message `AuthorizeDmChannel` when Guild reports the channel as not found.
- Later Guild phases own permission-change-driven immediate subscription revocation, limits, and richer ordering.

## Gateway, Session, Dispatcher, And Presence

- Gateway websocket protocol opcodes are in `services/gateway/v1/internal/server/protocol.go`; the first client message after `HELLO` may be `IDENTIFY` (`op=2`) or `RESUME` (`op=6`).
- Snowflake IDs in WebSocket JSON are decimal strings. This includes `READY` IDs, `SUBSCRIBE.channel_ids` input, `SUBSCRIBED.channel_ids`, and domain event payload IDs; sequences, revisions, and timestamps remain JSON numbers.
- Gateway is a transport adapter. It forwards `connection_id`, Gateway ID/generation, and client operations to Session, then writes Session's `op/s/t/d` frames to the websocket. Heartbeats are the exception: Gateway validates and ACKs them locally, rejects heartbeats arriving more than 10% early without extending liveness, expires silent sockets after two advertised intervals, and batches changed acknowledged sequences back to the owning Session node every five seconds (up to 500 checkpoints per RPC by default).
- API and Gateway normalize IP safety-limit scopes to IPv4 `/32` and IPv6 `/64`; IPv4 policies use looser CGNAT-aware thresholds.
- Gateway keeps connection capacity entirely process-local: defaults are 50,000 total sockets, 5,000 pending handshakes, and pending per-source limits of 100 for IPv4 and 20 for IPv6 `/64`. Successful IDENTIFY/RESUME releases the pending slot, and each connection may send 120 Gateway events per minute by default. Redis is used only for discrete upgrade/IDENTIFY/RESUME rate limits. IDENTIFY is limited by source scope, while RESUME is limited by source scope plus logical Session ID.
- IDENTIFY selects a ready Session node from the etcd `/cordis/session/nodes` directory. RESUME resolves `session:owners:{session_id}` in Redis and the exact node ID/generation in etcd, then connects directly to the owning Session node.
- Session stores state in process memory. A disconnected Session remains resumable for two minutes; a Session-node crash loses its Sessions and clients must IDENTIFY again.
- After token validation, Session limits IDENTIFY by user ID and authenticator Session ID. Redis atomically claims one live logical Session per authenticator Session and renews the claim throughout the detached resume window.
- Each Session has an independent monotonically increasing sequence and a 2048-entry sliding replay window. Heartbeat `d` is the acknowledged sequence; Gateway coalesces advances and Session applies the resulting checkpoints to remove acknowledged replay entries. Binding epochs reject delayed checkpoints from replaced connections.
- Session owns `user/guild/channel -> local sessions` indexes and checks Guild `VIEW_CHANNEL` when adding Channel subscriptions or distributing visibility-sensitive Channel metadata events.
- Dispatcher resolves Guild `message.*` through aggregate Guild routes, then uses the existing channel dispatch RPC so Session still filters by local channel subscriptions. It accepts future user-routed DM message records, while legacy records without aggregate route IDs continue through channel routes during migration.
- Session nodes register with leases under etcd `/cordis/session/nodes/{node_id}`. Redis owners use `session:owners:{session_id}`; aggregate ZSET routes use `gateway:routes:users:{id}:nodes`, `gateway:routes:guilds:{id}:nodes`, and `gateway:routes:channels:{id}:nodes`. The braces around each Redis ID are literal Cluster hash tags.
- etcd stores only the low-cardinality live Session-node directory and is configured with multiple endpoints in production. Redis stores resume ownership and aggregate routing metadata and must remain Redis Cluster compatible. Neither stores replay payloads.
- Dispatcher instances share consumer group `cordis.dispatcher.v1` and consume the guild, message, user, and presence topics. Dispatcher requires a User gRPC endpoint for friend fan-out. Aggregate routes are deduplicated per dispatch attempt, but retrying a record can call a previously successful Session node again; delivery is at least once and has no general event-ID deduplication.
- Session graceful drain marks the node `draining`, rejects new attachments, and spreads `INVALID_SESSION(false)` notifications across the configured drain window. It does not transfer in-memory Session state.
- Presence is updated by Session and continues to aggregate per-device user status. Invisible presence resolves as offline and hides sessions; switching to or from invisible produces a normal `presence.updated` transition.
- Session passes its in-memory guild membership snapshot (`guild_ids`) on every presence RPC so Presence can stamp transition events with fan-out targets. Detached sessions keep their presence TTL refreshed by `refreshSessionLeases`, so the offline transition fires when the two-minute resume window expires (`removeSession` -> `RemoveUserSession`), not at disconnect. A Session-node crash still loses this and the TTL expires silently.
- READY includes `guild_ids` (decimal strings) alongside the session identifiers.

## Errors

- Internal domain errors use `pkg/rpcerror.New(code, domain, reason, message)` with `google.rpc.ErrorInfo`.
- Public API errors go through `apierror.FromRPC(err)`, which maps known domain/reason pairs to Connect errors with `api.v1.PublicErrorInfo` details.
- Message handlers use `mapStoreError()`: `sql.ErrNoRows` to message not found, `store.ErrPermissionDenied` to permission denied, Postgres CHECK violation `23514` to invalid request.
- Gateway, Session, and Presence currently use plain gRPC `status.Error` for validation errors.

## Tests

- Unit tests use `github.com/stretchr/testify/require`; follow that style for new assertions.
- Store unit tests commonly use `sqlmock.QueryMatcherRegexp` plus local `sqlPattern()` helpers; keep SQL expectations exact enough to catch query changes.

### Integration Tests

- Integration tests are behind the `integration` build tag: `make test-integration` (or `go test -tags=integration ./... -count=1 -timeout=10m`). They need Docker but no pre-existing services; `internal/testkit` starts fixed-version PostgreSQL, Redis, Kafka (KRaft), and etcd via Testcontainers.
- The Presence Redis integration test auto-starts Redis through testkit when `CORDIS_TEST_REDIS_ADDR` is unset; the env var only overrides the target.
- Testing layers: RPC branch logic (validation, permissions, error mapping) stays in unit tests with fakes; store interface methods are fully covered by integration tests against real backends; cross-system seams get one integration path per interaction shape.
- Store integration standard: every `Store` interface method is exercised against the real backend. Structure: one top-level `Test*WithPostgres` starts one container, applies embedded migrations, and dispatches domain-scoped subtest functions that each use a disjoint ID space. Constraint checks assert `pq.Error` codes (`23514` CHECK, `23505` unique). User/Authenticator/Guild/Message stores follow this; sub-100% remainder is driver error propagation only.
- Dispatcher integration (`services/dispatcher/v1/internal/server`) uses a harness with run-scoped Kafka topics, consumer groups, and etcd prefixes. It covers channel and guild routes, `guild.created`/`guild.member.joined` user-route merge/dedupe, retry with offset-uncommitted assertions, and poison-pill drop-and-commit. Committed offsets are read with `kmsg.OffsetFetchRequest` (both legacy and group-style fields); do not add `kadm`.
- `sessionregistry.EtcdDirectory` binds one instance to one node ID and one lease by design. Tests that simulate multiple Session nodes must create one registry instance per node (shared prefix); closing an instance revokes its lease and simulates a node crash. Read-side directories (`Ready`/`Resolve`) never register.
- Gateway discovery integration covers IDENTIFY (no ready node, draining excluded) and RESUME failures (missing owner, expired owner, node crash via lease revoke, stale generation, draining node).

### Cross-Service Composition Tests

- Go `internal/` boundaries prevent one test package from importing two services' internal servers. Composition tests therefore run the caller in-process and dependencies as real service binaries: `testkit.BuildService` compiles the main package, `testkit.StartService` runs it with a temporary YAML config (real `conf.LoadConfig` + zrpc wiring) and SIGTERM cleanup; `testkit.FreeAddress` and `testkit.WaitServiceReady` handle ports and readiness probes.
- Multiple services' embedded migrations may be applied to one shared PostgreSQL container; table names do not collide.
- `services/message/v1/internal/server/composition_integration_test.go`: Message in-process against real User + Guild binaries; covers Guild→User member verification and Message→Guild channel authorization (allow, non-member NotFound, category InvalidArgument, MANAGE_MESSAGES, `@everyone` permission revocation, owner bypass).
- `services/authenticator/v1/server/composition_integration_test.go`: Authenticator in-process against a real User binary; covers Register, duplicate-email `AlreadyExists` with `rpcerror` domain/reason propagation, Argon2id login, refresh-token rotation, the password reset flow (session revocation, single-use tokens), and email verification including stale-token rejection after an email change.

## Runtime Env

- Authenticator config requires `CORDIS_ACCESS_TOKEN_SECRET` and `CORDIS_REFRESH_TOKEN_SECRET` for real token manager startup.
- Optional tracing endpoint is read from `CORDIS_OTEL_ENDPOINT` in service configs.
- Message config requires a Guild gRPC endpoint for channel authorization. Message and Guild Kafka producer configs are optional; Dispatcher Kafka seeds are required.
- Snowflake IDs use a custom node derived from non-loopback IP hash, epoch `2025-01-01`, 16 node bits, and 8 step bits.
