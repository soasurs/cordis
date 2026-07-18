# Service Catalog

## API

Public Connect-RPC server on `:8080`. It proxies Authenticator, User, Guild, and
Message, converts public/internal protobuf models, and maps domain errors with
`pkg/apierror`. It does not access domain databases.

Public requests use Redis-backed named rate-limit policies with a bounded local
fallback during Redis failures. Every request first consumes a source-IP guard;
successful authentication then consumes the general user quota. Message
creation, relationship writes, Guild resource creation, and invite joins also
consume business-specific buckets. `GetReadStates` concurrency is bounded per
user within each API process, and waiting follows the request context.

## User

gRPC on `:3000`. Owns users and profiles, email availability and updates,
profile updates, password verification, and password changes. Passwords use
Argon2id. User does not issue tokens.

## Authenticator

gRPC on `:3001`. Orchestrates registration and login, issues and refreshes
tokens, verifies access tokens, and manages authentication sessions in
PostgreSQL. User remains the authority for user identity, while Authenticator
owns password credentials. Real startup requires access- and refresh-token
secrets.

All Argon2 hashing and verification is protected by a process-local weighted
semaphore. Its capacity comes from `password.maxConcurrency` (default 4), and
each Argon2 operation currently consumes weight 1. The configured capacity is
therefore a fixed concurrency slot count per Authenticator instance, not a
cluster-wide limit. Requests wait when all slots are occupied and stop waiting
when their context expires or is canceled. The semaphore does not provide a
separate bounded request queue; the outer API rate limiter bounds admission.

## Guild

gRPC on `:3005`. Owns guilds, members, bans, roles, member-role assignments,
channels, and channel permission overwrites. It supports guild lifecycle,
membership and bans, role management and ordering, text/category/voice channel
metadata and ordering, and channel authorization.

Permissions are a `uint64` bit set. Owners and administrators receive all
permissions. Channel evaluation applies the default role, member roles, and
member overwrites. Guild publishes dot-separated events directly to
`cordis.guild.events.v1`.

## Message

gRPC on `:3002`. Owns messages, attachments, mentions, and replies. Create,
read, update, and delete operations ask Guild for authorization. Listing uses
`before`, `after`, or `around` cursor pagination. Reaction and custom emoji RPCs
are not currently implemented.

`GetReadStates` batches channel read-state, unread-message, and unread-mention
calculation. Channel authorization fan-out within one request uses a configured
worker bound to avoid unbounded cross-service calls. A service-level weighted
semaphore charges the deduplicated channel count and bounds aggregate work on
each Message instance. API also applies a process-local keyed semaphore per
user and the general authenticated-user quota. These concurrency capacities are
per-instance rather than cluster-wide.

Client message types are currently `DEFAULT` and `REPLY`; `THREAD_STARTER` is
reserved. The only client-settable flag is `SUPPRESS_NOTIFICATIONS`. After a
write transaction commits, the service publishes directly to `cordis.message.events.v1`
on a best-effort basis; failures are logged.

## Gateway

HTTP/WebSocket on `:8081`, exposing `/ws` and `/health`. It sends `HELLO`,
requires `IDENTIFY` or `RESUME` as the first client message, discovers Session
nodes through etcd, reads resume ownership from Redis, and proxies the WebSocket
over a `SessionService.Connect` bidirectional stream. It owns no subscriptions
and consumes no Kafka events.

## Session

gRPC on `:3006` and the stateful core of realtime delivery. It validates tokens,
creates or resumes logical sessions, loads guild membership, owns local
user/guild/channel indexes, authorizes channel subscriptions, assigns sequence
numbers, and keeps up to 2048 replay events in memory.

Detached sessions live for 120 seconds by default. Resume must reach the
original node. Session nodes register through etcd leases. Graceful drain
publishes a draining state, rejects new connections, and gradually instructs
existing clients to identify again.

## Dispatcher

Background Kafka consumer for `cordis.guild.events.v1` and `cordis.message.events.v1`,
using consumer group `cordis.dispatcher.v1`. It resolves user/guild/channel
routes in Redis and calls the Session node's dispatch RPC. Offsets are committed
manually. Invalid events are dropped and committed; transient failures retry
with exponential backoff. Routes are deduplicated within one attempt, but a
record retry can call an already successful node again. Delivery is at least
once and there is no general event-ID deduplication.

## Presence

gRPC on `:3003`. Redis-backed gateway, legacy channel-route, and user-device
presence storage. TTL and generation checks filter stale records. Multi-device
sessions aggregate into user presence, while `INVISIBLE` is exposed as offline.
Session still uses Presence to register and refresh online state.
