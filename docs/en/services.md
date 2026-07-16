# Service Catalog

## API

Public Connect-RPC server on `:8080`. It proxies Authenticator, User, Guild, and
Message, converts public/internal protobuf models, and maps domain errors with
`pkg/apierror`. It does not access domain databases.

## User

gRPC on `:3000`. Owns users and profiles, email availability and updates,
profile updates, password verification, and password changes. Passwords use
Argon2id. User does not issue tokens.

## Authenticator

gRPC on `:3001`. Orchestrates registration and login, issues and refreshes
tokens, verifies access tokens, and manages authentication sessions in
PostgreSQL. User remains the authority for user and password data. Real startup
requires access- and refresh-token secrets.

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

Client message types are currently `DEFAULT` and `REPLY`; `THREAD_STARTER` is
reserved. The only client-settable flag is `SUPPRESS_NOTIFICATIONS`. After a
write transaction commits, the service publishes directly to `message.events`
on a best-effort basis; failures are logged.

## Gateway

HTTP/WebSocket on `:8081`, exposing `/ws` and `/health`. It sends `HELLO`,
requires `IDENTIFY` or `RESUME` as the first client message, discovers a Session
node through Redis, and proxies the WebSocket over a `SessionService.Connect`
bidirectional stream. It owns no subscriptions and consumes no Kafka events.

## Session

gRPC on `:3006` and the stateful core of realtime delivery. It validates tokens,
creates or resumes logical sessions, loads guild membership, owns local
user/guild/channel indexes, authorizes channel subscriptions, assigns sequence
numbers, and keeps up to 2048 replay events in memory.

Detached sessions live for 120 seconds by default. Resume must reach the
original node. Graceful drain rejects new connections and gradually instructs
existing clients to identify again.

## Dispatcher

Background Kafka consumer for `cordis.guild.events.v1` and `message.events`,
using one consumer group. It resolves user/guild/channel routes in Redis and
calls the Session node's dispatch RPC. Offsets are committed manually. Invalid
events are dropped and committed; transient failures retry with exponential
backoff. A node is called at most once per event.

## Presence

gRPC on `:3003`. Redis-backed gateway, legacy channel-route, and user-device
presence storage. TTL and generation checks filter stale records. Multi-device
sessions aggregate into user presence, while `INVISIBLE` is exposed as offline.
Session still uses Presence to register and refresh online state.
