# Service Catalog

## API

Public Connect-RPC server on `:8080`. It proxies Authenticator, User, Guild, and
Message, converts public/internal protobuf models, and maps domain errors with
`pkg/apierror`. It does not access domain databases.

Public requests use Redis-backed named rate-limit policies with a bounded local
fallback during Redis failures. IP-based buckets use an IPv4 `/32` or IPv6
`/64`; IPv4 policies have looser CGNAT-aware thresholds. Every request first consumes a source-IP guard;
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

Persistent Guild resources have configuration-driven hard limits. The defaults
are 10 owned and 100 joined guilds per user, 250 roles and 500 channels per
guild, 100 active invites per guild, and 100 permission overwrites per channel.
Quota checks and writes are serialized in the same PostgreSQL transaction.

`ListUserGuildChannelVisibilities` pages through a user's active Guild
memberships and returns the complete, ascending set of visible channel IDs for
each Guild. Every snapshot carries a persistent `access_revision`. PostgreSQL
triggers advance this monotonic revision whenever membership, role permissions
or assignments, channels, permission overwrites, ownership, or Guild deletion
can change access. Published Guild events include the committed revision while
the Guild still exists.

## Message

gRPC on `:3002`. Owns messages, attachments, mentions, and replies. Create,
read, update, and delete operations ask Guild for authorization. Listing uses
`before`, `after`, or `around` cursor pagination. Reaction and custom emoji RPCs
are not currently implemented.

Create and update requests allow at most 10 attachments and 100 unique mentioned
user IDs by default. Both limits are configured by the Message service.

`GetReadStates` batches channel read-state, unread-message, and unread-mention
calculation. It resolves DM channels in one store query and authorizes the
remaining Guild channels through one batch RPC. A service-level weighted
semaphore charges the deduplicated channel count and bounds aggregate work on
each Message instance. API also applies a process-local keyed semaphore per
user and the general authenticated-user quota. These concurrency capacities are
per-instance rather than cluster-wide.

Client message types are currently `DEFAULT` and `REPLY`; `THREAD_STARTER` is
reserved. The only client-settable flag is `SUPPRESS_NOTIFICATIONS`. After a
write transaction commits, the service publishes directly to `cordis.message.events.v1`
on a best-effort basis; failures are logged. Guild message records carry
`guild_id` and use the Guild ID as the Kafka key. DM message records carry
`user_id` and emit one user-keyed record per participant.

## Gateway

HTTP/WebSocket on `:8081`, exposing `/ws` and `/health`. It sends `HELLO`,
requires `IDENTIFY` or `RESUME` as the first client message, discovers Session
nodes through etcd, reads resume ownership from Redis, and proxies the WebSocket
over a `SessionService.Connect` bidirectional stream. It owns no logical routing
state and consumes no Kafka events.

Before accepting a WebSocket, Gateway applies trusted-proxy-aware source limits
using an IPv4 `/32` or IPv6 `/64`. Connection capacity is process-local: each
instance defaults to 50,000 total sockets and 5,000 pending handshakes, with
pending per-scope defaults of 100 for IPv4 and 20 for IPv6. A socket leaves the
pending buckets after Session accepts IDENTIFY or RESUME. Client connections may
send at most 120 Gateway events per minute by default. `IDENTIFY` is additionally
limited by source scope, while `RESUME` is limited by both source scope and
logical session ID; only these discrete rate-limit events use Redis.

Gateway owns physical connection liveness. It validates heartbeat sequences,
returns `HEARTBEAT_ACK` locally, and closes a socket after two missed advertised
intervals. Heartbeats arriving more than 10% before the advertised interval are
rejected and do not extend the liveness deadline. Only an advanced acknowledged
sequence becomes dirty state; dirty
checkpoints are coalesced, grouped by the owning Session node, and synchronized
every five seconds in batches of up to 500 by default. Session binding epochs
make delayed checkpoints from replaced connections harmless.

## Session

gRPC on `:3006` and the stateful core of realtime delivery. It validates tokens,
creates or resumes logical sessions, loads Guild visibility, owns local
user/Guild indexes, assigns sequence numbers, and keeps up to 2048 replay
events in memory.

IDENTIFY uses the paginated Guild visibility RPC, whose store reads are batched
per page, to load immutable, sorted
channel snapshots with their access revisions. A snapshot set is shared by all
of the user's logical Sessions on the node and released after the last local
Session is removed. Loading is bounded to 100 Guilds and 500 visible channels
per Guild by default. Guild access events invalidate affected snapshots by
revision. On-demand rebuilds are singleflighted per user and Guild, bounded to
16 concurrent calls per Session node, and time out after two seconds by
default. A stale, missing, malformed, oversized, or otherwise invalid snapshot
fails closed. If rebuilding fails, Session skips the sensitive event and emits
one sequenced `session.reconcile` hint for that invalid snapshot generation.

Session applies Gateway checkpoint batches to advance acknowledged sequences and
trim replay windows. Client heartbeats do not directly refresh Redis ownership
or Presence; logical-session leases are renewed independently of WebSocket
heartbeat traffic through bounded batches, while aggregate route renewal runs
in a separate loop.

After token validation, `IDENTIFY` is limited by user ID and authenticator
session ID. A Redis claim permits only one live logical session per authenticator
session; the claim is renewed while the logical session is retained, including
the detached resume window. Claims contain the owning Session node ID and
generation. A new IDENTIFY may replace an existing claim with Redis CAS only
after etcd confirms that exact generation no longer exists; draining nodes
remain protected from takeover.

Dispatcher resolves Guild messages through aggregate Guild routes and includes
the Guild and channel IDs in a dedicated Guild-message dispatch RPC. Session checks the server-owned
visibility snapshot once per local user and forwards the message to all of that
user's logical sessions. DM message records resolve directly through aggregate
user routes. Message records without exactly one aggregate Guild/user route are
rejected.

No-op Presence updates are discarded. Changed updates are limited to five per
logical session every 20 seconds, then consume a shared per-user quota of ten per
20 seconds across devices before Presence is called.

Detached sessions live for 120 seconds by default. Resume must reach the
original node. Session nodes register through etcd leases. Graceful drain
publishes a draining state, rejects new connections, and gradually instructs
existing clients to identify again.

## Dispatcher

Background Kafka consumer for `cordis.guild.events.v1` and `cordis.message.events.v1`,
using consumer group `cordis.dispatcher.v1`. It resolves aggregate user/Guild
routes in Redis for message delivery and calls the Session node's dispatch RPC. Offsets are committed
manually. Invalid events are dropped and committed; transient failures retry
with exponential backoff. Routes are deduplicated within one attempt, but a
record retry can call an already successful node again. Delivery is at least
once and there is no general event-ID deduplication.

## Presence

gRPC on `:3003`. Redis-backed user-device presence storage. TTL and generation
checks filter stale sessions. Multi-device sessions aggregate into user
presence, while `INVISIBLE` is exposed as offline. Session uses Presence to
register and refresh online state.
