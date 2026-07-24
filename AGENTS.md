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

- `services/api/v1`: public Connect-RPC HTTP API on `:8080`; authenticates callers, proxies User/Message/Guild operations to internal gRPC services, and maps errors through `pkg/apierror`. Public requests use Redis-backed named rate-limit policies with a bounded local fallback; every request consumes a high-volume source-IP guard while successful access-token verification consumes the primary per-user quota. Register/Login additionally consume endpoint IP and normalized-email-hash policies; password recovery, email-verification resend, public profile reads, and email-availability checks have dedicated IP policies. Authenticated business policies cover message creation, relationship writes, Guild resource creation, and invite joins. Scoped `GetReadStates` reconciliation is additionally bounded by a process-local per-user concurrency limiter. Forwarded client addresses are honored only from explicitly configured trusted proxy CIDRs.
- `services/gateway/v1`: websocket gateway on `:8081`; forwards websocket frames over a bidirectional gRPC stream to Session and owns no logical session, replay, routing, or Kafka state.
- `services/session/v1`: stateful gRPC service on `:3006`; owns logical sessions, sequence numbers, 2048-event in-memory replay windows, shared per-user Guild visibility snapshots, Presence updates, and Gateway stream bindings. No-op Presence updates are discarded; changed updates are limited per logical session and by a distributed cross-device user quota.
- `services/dispatcher/v1`: Kafka consumer for the Guild, Message, User, and Presence topics; resolves aggregate Session-node routes in Redis and dispatches events to Session gRPC. Presence transitions fan out along two paths - the payload `guild_ids` through guild routes and the user's friends (paged through User `ListRelationships`) plus the user's own devices through user routes; recipients reachable through both paths receive duplicates, which is accepted because presence updates are idempotent state.
- `services/presence/v1`: gRPC on `:3003`; Redis-backed per-device user presence TTLs and aggregate status. Publishes `presence.updated` to its own optional Kafka topic (default `cordis.presence.events.v1`) only when the multi-device aggregate status actually transitions; heartbeat renewals stay silent. Same-user mutations and synchronous publication are serialized by a Redis lock. The pre/post aggregate snapshot happens in the server layer, and a failed pre-read skips the event rather than guessing.
- `services/authenticator/v1`: go-zero `zrpc` on `:3001`; JWT access/refresh tokens, sessions in Postgres, calls User gRPC. Owns password credentials in `user_credentials` (Argon2id through `pkg/password`): Login resolves the user through User gRPC, verifies locally, and only creates a session when the current email is verified; unknown users, wrong passwords, and unverified emails all return the same invalid-credentials error. `ChangePassword` checks the old password and revokes the other sessions in one transaction. Register writes the User row first, then atomically stores the credential, redeems any invitation, and upserts an email-verification token without creating a session; a user row without a credential is a half-registration that the same email may claim by registering again. Process-local weighted semaphore capacity bounds all Argon2 work. Also owns account recovery: password reset and email verification tokens (sha256 hashes, single-use, per-user upsert) with delivery through the optional Mailer gRPC client (skipped when unconfigured). `RequestPasswordReset` and anonymous email-verification resend always report success for syntactically valid emails to avoid account enumeration; `ConfirmPasswordReset` prechecks the token before Argon2, then rechecks under row lock while atomically consuming the token, upserting the credential, and revoking all sessions. Recovery requests are throttled per target through optional Redis (fail-open when Redis is down or unconfigured); throttled requests still report success.
- `services/user/v1`: go-zero `zrpc` on `:3000`; user identity, profiles, and social relationships in Postgres, no credential material. Emails are lowercased at every entry point (a CHECK constraint backstops storage). `users.email_verified_at` tracks verification; `UpdateEmail` clears it only when the address actually changes and `MarkEmailVerified` only applies while the supplied email is still current. Profiles carry a globally unique lowercase handle (`user_profiles.username`, `^[a-z0-9_]{2,32}$`, unique index plus CHECK backstop) chosen at registration and resolved through `GetUserProfileByUsername` (public `LookupUser`).
- `services/mailer/v1`: go-zero `zrpc` on `:3007`; stateless transactional mail delivery behind a `SendEmail(to, template, variables)` RPC. Template names and variable keys live in `pkg/mail`; the only provider today is `noop` (redacted logging, no delivery). Template variables may contain secrets and must never be logged verbatim.
- `services/media/v1`: go-zero `zrpc` on `:3008`; owns upload sessions and immutable binary assets across three logical S3-compatible buckets. Avatar and Guild icon sources upload to the private staging bucket, are decoded and validated, then published under `avatars/{user_id}/{asset_id}` or `icons/{guild_id}/{asset_id}` in the public bucket. Attachments upload directly under `attachments/{channel_id}/{asset_id}/{random_token}/{safe_filename}` in the attachment bucket; the whole bucket uses one deployment-wide `public` or `presigned` access mode. Message responses hydrate complete attachment URLs through Media's batch URL RPC, while avatar/icon URLs remain client-constructed from their public base URL and IDs.
- `services/message/v1`: go-zero `zrpc` on `:3002`; messages, mentions, per-channel read watermarks, and 1:1 DM channels in Postgres, publishes message events directly to Kafka when Kafka is configured, and calls User gRPC for relationship checks on DM flows. Its internal READY RPC combines all DM channels with read states for Session-provided visible Guild text channels; scoped reconciliation reloads either one Guild or all DMs without accepting caller-provided channel IDs. Read-state queries are split into capacity-sized batches, and every batch acquires its exact channel count from the process-local weighted limiter. Message create/update enforce configuration-driven attachment and unique-mention limits (defaults 10 and 100).
- `services/guild/v1`: go-zero `zrpc` on `:3005`; guilds/members/bans/roles/channels in Postgres, calls User gRPC when directly adding or banning users, and publishes guild events to its own Kafka topic. Configuration-driven hard quotas bound owned/joined guilds, roles, channels, active invites, and permission overwrites; count-and-create paths use transaction-scoped PostgreSQL advisory locks. An unpaginated READY RPC returns each user's complete Guild/role/visible-channel bootstrap, including overwrites for visible channels, with persistent monotonic access revisions.

## Proto

- Protos use edition 2023. Both public and internal Go generation use the opaque API (`default_api_level=API_OPAQUE`), so use generated getters, setters, and builders instead of field access or message struct literals throughout `gen/`.
- External `proto/api` generation additionally produces Connect-Go code under `gen/api/v1/apiv1connect`.
- `buf.gen.external.yaml` only includes `proto/api`; `buf.gen.internal.yaml` includes `proto/authenticator`, `proto/user`, `proto/message`, `proto/guild`, `proto/presence`, `proto/session`, and `proto/mailer`.

## Resource Updates

- Resource `Update` RPCs use partial-update semantics by default. Only fields explicitly present in the request may change; omitted fields preserve their stored values. Do not read a resource, compose a complete replacement, and write every field back.
- Use edition 2023 scalar presence (`HasFoo`) to distinguish omission from an explicit default value. An explicitly present empty string, zero, false, or empty wrapper/list applies that value when the field supports it.
- Public API adapters must preserve field presence when forwarding to internal services: call a generated setter only when the incoming field is present.
- Store update parameters represent mutable fields with pointers (or an equivalent presence-aware type), and SQL updates only the fields marked present. Reject an `Update` request that contains no mutable fields.
- A present collection-valued field replaces that collection unless the API defines dedicated add/remove operations. Single-field commands such as email, username, and nickname changes are not required to wrap their sole value in a general patch shape.

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
- Message event values use the lightweight `{"t":"message.created","d":{...}}` envelope. Guild message records carry `guild_id`; DM message records carry `user_id` and emit one record per participant. `message.created`, `message.updated`, and `message.deleted` records are keyed by decimal `channel_id`; user-scoped `message.read.updated` records are keyed by decimal `user_id`.
- Realtime domain event names use dot-separated hierarchy only. Shared names live in `pkg/realtime`; do not introduce underscore variants such as `message_created`.
- Kafka publication is best-effort: publication failures are logged but do not turn an already-committed mutation into an RPC failure.
- `message.created` and `message.updated` payloads include `mention_user_ids` and an embedded public `author` profile. Message response objects also embed `author`; the author ID is available as `author.user_id` rather than a duplicate top-level `author_id`. Message loads one profile from User before writes and single-message reads, while list reads deduplicate author IDs and use User's internal `BatchGetUserProfiles`; User owns any future profile caching.
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
- `guilds.access_revision` is advanced by PostgreSQL triggers when membership, role permissions/assignments, channels, channel overwrites, ownership, or deletion changes. `GetUserReadyState` loads all active memberships within the configured joined-Guild hard limit, batch-loads roles/channels/overwrites, and returns server-authoritative visible-channel snapshots labeled with that revision. Guild events include the committed `access_revision` when the Guild still exists; legacy events without it invalidate Session snapshots conservatively.
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
- `channel_read_states` stores `(user_id, channel_id, last_read_message_id)` per channel; `AckMessage` moves the cursor forward only when the requested message is newer. READY and scoped reconciliation read states contain `channel_id`, the current `last_message_id`, `last_read_message_id`, and the real-time unread @-mention count. A channel is unread when `last_message_id > last_read_message_id`; no unread-message count is computed or stored. Message creation reads the author's resulting state back inside the write transaction. Only an actual watermark advance publishes a user-routed `message.read.updated` event, allowing the user's other devices to converge without no-op events.
- Opening a DM requires an active friendship (`CreateDmChannel` is idempotent per pair and races resolve through insert-if-absent). Sending in a DM requires that neither side blocks the other, checked through one snapshot `CheckRelationships(include_reverse)` call; reading history stays available while blocked. DMs have no moderators: non-authors can never edit or delete.
- `dm.channel.created` and DM `message.*` are published to the Message topic as one user-routed record per participant. `dm.channel.created` is keyed by decimal recipient `user_id`, while DM `message.*` uses decimal `channel_id`; Dispatcher routes both record types and `relationship.*` by the payload `user_id`.
- Permission-changing Guild events immediately invalidate the affected Session visibility snapshots.

## Gateway, Session, Dispatcher, And Presence

- Gateway websocket protocol opcodes are in `services/gateway/v1/internal/server/protocol.go`; the first client message after `HELLO` may be `IDENTIFY` (`op=2`) or `RESUME` (`op=6`).
- Snowflake IDs in WebSocket JSON are decimal strings. This includes `READY` IDs and domain event payload IDs; sequences, revisions, and timestamps remain JSON numbers.
- Gateway is a transport adapter. It forwards `connection_id`, Gateway ID/generation, and client operations to Session, then writes Session's `op/s/t/d` frames to the websocket. Heartbeats are the exception: Gateway validates and ACKs them locally, rejects heartbeats arriving more than 10% early without extending liveness, expires silent sockets after two advertised intervals, and batches changed acknowledged sequences back to the owning Session node every five seconds (up to 500 checkpoints per RPC by default).
- API and Gateway normalize IP safety-limit scopes to IPv4 `/32` and IPv6 `/64`; IPv4 policies use looser CGNAT-aware thresholds.
- Gateway keeps connection capacity entirely process-local: defaults are 50,000 total sockets, 5,000 pending handshakes, and pending per-source limits of 100 for IPv4 and 20 for IPv6 `/64`. Successful IDENTIFY/RESUME releases the pending slot, and each connection may send 120 Gateway events per minute by default. Redis is used only for discrete upgrade/IDENTIFY/RESUME rate limits. IDENTIFY is limited by source scope, while RESUME is limited by source scope plus logical Session ID.
- IDENTIFY selects a ready Session node from the etcd `/cordis/session/nodes` directory. RESUME resolves `session:owners:{session_id}` in Redis and the exact node ID/generation in etcd, then connects directly to the owning Session node.
- Session stores state in process memory. A disconnected Session remains resumable for two minutes; a Session-node crash loses its Sessions and clients must IDENTIFY again. IDENTIFY loads one complete Guild READY response plus one Message READY response; immutable visibility snapshots are reference-counted and shared by the user's logical Sessions on the node, and the final Session removal releases the user snapshot set.
- After token validation, Session limits IDENTIFY by user ID and authenticator Session ID. One authenticator Session may create multiple live logical Sessions for concurrent clients. Each logical Session has an independent Redis owner lease, replay window, Presence lease, and transport binding.
- Each Session has an independent monotonically increasing sequence and a 2048-entry sliding replay window. Heartbeat `d` is the acknowledged sequence; Gateway coalesces advances and Session applies the resulting checkpoints to remove acknowledged replay entries. Binding epochs reject delayed checkpoints from replaced connections.
- Session owns `user/guild -> local sessions` indexes and checks Guild `VIEW_CHANNEL` when distributing visibility-sensitive Channel metadata events.
- Session visibility loading is bounded to 100 Guilds and 500 visible channels per Guild by default. Guild access events invalidate affected snapshots by revision; on-demand rebuilds are singleflighted per user/Guild, bounded to 16 concurrent loads per node, and time out after two seconds by default. Malformed, oversized, stale, missing, or explicitly invalid snapshots fail closed. A reload failure emits one sequenced `session.reconcile` hint per invalid snapshot generation so the client can synchronize over the HTTP APIs.
- Dispatcher resolves Guild `message.*` through aggregate Guild routes and calls the dedicated Guild-message dispatch RPC. Session filters these records through server-owned visibility snapshots and delivers them to every local logical Session for each visible user. DM `message.*` uses aggregate user routes. Records without exactly one aggregate Guild/user route are rejected.
- Session nodes register with leases under etcd `/cordis/session/nodes/{node_id}`. Redis owners use `session:owners:{session_id}`; aggregate ZSET routes use `gateway:routes:users:{id}:nodes` and `gateway:routes:guilds:{id}:nodes`. The braces around each Redis ID are literal Cluster hash tags.
- etcd stores only the low-cardinality live Session-node directory and is configured with multiple endpoints in production. Redis stores resume ownership and aggregate routing metadata and must remain Redis Cluster compatible. Neither stores replay payloads.
- Dispatcher instances share consumer group `cordis.dispatcher.v1` and consume the guild, message, user, and presence topics. Dispatcher requires a User gRPC endpoint for friend fan-out. Aggregate routes are deduplicated per dispatch attempt, but retrying a record can call a previously successful Session node again; delivery is at least once and has no general event-ID deduplication.
- Session graceful drain marks the node `draining`, rejects new attachments, and spreads `INVALID_SESSION(false)` notifications across the configured drain window. It does not transfer in-memory Session state.
- Presence is updated by Session and continues to aggregate per-device user status. Invisible presence resolves as offline and hides sessions; switching to or from invisible produces a normal `presence.updated` transition.
- Session passes its in-memory guild membership snapshot (`guild_ids`) on every presence RPC so Presence can stamp transition events with fan-out targets. Detached sessions keep their presence TTL refreshed by batched `refreshSessionLeases`; logical Session owners are renewed with Redis pipelines using the resume timeout as TTL, Presence leases use a batch RPC, and aggregate route renewal runs independently. The maintenance interval is one quarter of the resume timeout with ±20% cycle jitter; 500-session batches are assigned jittered slots within a bounded five-second spread window. The offline transition fires when the two-minute resume window expires (`removeSession` -> `RemoveUserSession`), not at disconnect. A Session-node crash still loses this and the TTL expires silently.
- READY includes complete visible Guild metadata, roles, the current member's explicit role IDs, visible channels and their permission overwrites, all DM channels, and four-field read states. Snowflake IDs are decimal strings. Events received while READY is assembled are buffered and sequenced after READY. The pending buffer is bounded by count and total event bytes, with its effective count kept below replay and binding queue capacity; overflow clears the buffer and fails IDENTIFY so a reconnect rebuilds authoritative state.

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
- Dispatcher integration (`services/dispatcher/v1/internal/server`) uses a harness with run-scoped Kafka topics, consumer groups, and etcd prefixes. It covers Guild/user routes, `guild.created`/`guild.member.joined` user-route merge/dedupe, retry with offset-uncommitted assertions, and poison-pill drop-and-commit. Committed offsets are read with `kmsg.OffsetFetchRequest` (both legacy and group-style fields); do not add `kadm`.
- `sessionregistry.EtcdDirectory` binds one instance to one node ID and one lease by design. Tests that simulate multiple Session nodes must create one registry instance per node (shared prefix); closing an instance revokes its lease and simulates a node crash. Read-side directories (`Ready`/`Resolve`) never register.
- Gateway discovery integration covers IDENTIFY (no ready node, draining excluded) and RESUME failures (missing owner, expired owner, node crash via lease revoke, stale generation, draining node).

### Cross-Service Composition Tests

- Go `internal/` boundaries prevent one test package from importing two services' internal servers. Composition tests therefore run the caller in-process and dependencies as real service binaries: `testkit.BuildService` compiles the main package, `testkit.StartService` runs it with a temporary YAML config (real `conf.LoadConfig` + zrpc wiring) and SIGTERM cleanup; `testkit.FreeAddress` and `testkit.WaitServiceReady` handle ports and readiness probes.
- Multiple services' embedded migrations may be applied to one shared PostgreSQL container; table names do not collide.
- `services/message/v1/internal/server/composition_integration_test.go`: Message in-process against real User + Guild binaries; covers Guild→User member verification and Message→Guild channel authorization (allow, non-member NotFound, category InvalidArgument, MANAGE_MESSAGES, `@everyone` permission revocation, owner bypass).
- `services/authenticator/v1/server/composition_integration_test.go`: Authenticator in-process against a real User binary; covers unverified registration and automatic verification delivery, duplicate-email `AlreadyExists` with `rpcerror` domain/reason propagation, verified-only Argon2id login, refresh-token rotation, the password reset flow (session revocation, single-use tokens), and email verification including stale-token rejection after an email change.

## Runtime Env

- Authenticator config requires `CORDIS_ACCESS_TOKEN_SECRET` and `CORDIS_REFRESH_TOKEN_SECRET` for real token manager startup.
- Optional tracing endpoint is read from `CORDIS_OTEL_ENDPOINT` in service configs.
- Message config requires a Guild gRPC endpoint for channel authorization. Message and Guild Kafka producer configs are optional; Dispatcher Kafka seeds are required.
- Snowflake IDs use a custom node derived from non-loopback IP hash, epoch `2025-01-01`, 16 node bits, and 8 step bits.
