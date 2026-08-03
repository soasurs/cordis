# Service Catalog

## API

Public Connect-RPC server on `:8080`. It proxies Authenticator, User, Guild,
Message, and Presence, converts public/internal protobuf models, and maps domain errors with
`pkg/apierror`. It does not access domain databases.

Public resource models embed the current User profiles needed to render them.
The API batch-loads and validates those profiles while composing Guild members,
bans, invites, relationships, messages, and DM channels; internal domain models
continue to carry stable user IDs.

Public requests use Redis-backed named rate-limit policies with a bounded local
fallback during Redis failures. IP-based buckets use an IPv4 `/32` or IPv6
`/64`; IPv4 policies have looser CGNAT-aware thresholds. Every request first consumes a source-IP guard;
successful authentication then consumes the general user quota. Message
creation, relationship writes, Guild resource creation, and invite joins also
consume business-specific buckets. Authenticated `GetReadStates` reconciliation
also uses a process-local keyed limiter to bound concurrent requests per user.

`ResolveUsersPresence` accepts at most 100 unique user IDs and returns only the
caller's own snapshot plus friends and users who share an active Guild. A
shared Guild independently grants Presence visibility even when either user
has blocked the other; blocking removes only the relationship/DM visibility
path. Invisible or unknown aggregate states are exposed as offline, unrelated
users are omitted, and the public model contains only `user_id`, `status`,
`last_seen_at`, and `version`.

Before business rate limiting, the API inbound chain applies a server deadline,
a global in-flight request cap, and CPU-adaptive load shedding. A circuit
breaker isolates persistent server failures per public RPC procedure. Only
`Unknown`, `DeadlineExceeded`, `Internal`, `Unavailable`, and `DataLoss` count
as breaker failures; validation, authentication, and rate-limit errors do not
open a circuit. The total HTTP body and each decompressed Connect message have
separate size limits. Panics are logged with the procedure and stack before
being converted to an opaque `Internal` error. A timed-out request retains its
concurrency slot until the underlying handler actually exits, so work that
ignores cancellation cannot bypass the global cap.

## User

gRPC on `:3000`. Owns users and profiles, email availability and updates,
username availability checks, profile updates, password verification, and
password changes. Passwords use Argon2id. User does not issue tokens.

Relationship HTTP responses embed the target profile. `relationship.updated`
events load the target profile from the User store before the relationship
transaction so committed updates and their event payloads stay self-contained.
Relationship listing uses opaque `cursor` / `next_cursor` pagination ordered by
`created_at`, then `target_id` descending (omit `next_cursor` when there is no
next page). Optional `type` filters are part of the cursor scope and must stay
unchanged across pages.

Name, username, bio, and avatar changes publish a full `user.profile.updated`
snapshot after the profile write succeeds. Dispatcher sends it to every client
owned by the user, all shared-Guild members, non-blocked relationship peers,
and existing DM peers. Session deduplicates recipients reached through more
than one audience path.

`UpdateUserProfile` is presence-aware for `name`, `bio`, and `avatar_asset_id`.
Avatar binaries still use `CreateAvatarUpload` → direct PUT → either
`CompleteAvatarUpload` or `UpdateUserProfile` with the upload/asset ID.
Clients may preview unsaved avatars from a local blob URL. An explicit
`avatar_asset_id` of `0` clears the avatar. Replaced assets are left for Media
lifecycle reclaim. `GetAvatarUploadConstraints` proxies Media's
`userAvatar` image-constraint profile (max file size, max width/height, max
pixels, and allowed MIME types). Media owns enforced `imageConstraints` for
avatars and guild icons. Message attachments remain opaque uploads; the
separate `attachmentImageInspection` budget only controls best-effort
dimension and blurhash extraction. Avatar create and complete failures map to
the stable public codes `profile.avatar_file_too_large`,
`profile.avatar_content_type_invalid`, `profile.avatar_dimensions_exceeded`,
and `profile.avatar_pixels_exceeded`.
`CheckUsernameAvailability` reuses the same username normalization and
validation as `UpdateUsername`; taken handles return `available=false`, while
`UpdateUsername` still relies on the database unique constraint as the final
authority.

## Authenticator

gRPC on `:3001`. Orchestrates registration and login, issues and refreshes
tokens, verifies access tokens, and manages authentication sessions in
PostgreSQL. User remains the authority for user identity, while Authenticator
owns password credentials. Real startup requires access- and refresh-token
secrets.

Access tokens default to 15 minutes. Authentication sessions use a 30-day idle
deadline and a 180-day absolute deadline. Every refresh rotates its token; the
immediately previous token has a 30-second idempotent recovery window. API
supports explicit Bearer transport for native clients and HttpOnly Cookie
transport with transparent server-side refresh for browsers. Authenticator also
stores 30-second, single-use Gateway tickets in Redis. See
[Authentication and token rotation](authentication.md).

Registration supports `open`, `invite_only`, and `closed` modes. Invite-only
registration uses one-time, optionally email-bound invitations stored by
Authenticator. An invitation is reserved before Argon2 and the User RPC, then
redeemed atomically with the password credential and email-verification token.
Registration sends the verification email but does not create a session. Login
creates a session only after the password is valid and the current email has
been verified; unknown accounts, wrong passwords, and unverified accounts share
the same public invalid-credentials response. Verification-email resend accepts
an email without authentication and always reports success for syntactically
valid addresses. Password reset only applies to accounts that already have a
credential; incomplete registrations must resume through `Register`.

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
membership and bans, role management, ordering, and member listing by role,
text/category/voice channel metadata and ordering, and channel authorization.
It also exposes local Guild mention candidate search: user prefixes are
matched against a Guild member profile projection and filtered by the current
channel's visibility, while role prefixes stay local to Guild and are filtered
against target members only during Message expansion.
Guild metadata includes an optional description of up to 1024 Unicode
characters. Name and description use the presence-aware `UpdateGuild` RPC;
an empty description clears it. Icons use the separate direct-upload flow and
are associated with the Guild only when `CompleteGuildIconUpload` succeeds.

Guild maintains `guild_member_profiles` as its local projection for mention
search. User remains the source of truth for complete profiles; Guild writes a
projection row when members join, and `CreateGuild` may hydrate its placeholder
row from User after the Guild transaction commits. It consumes
`user.profile.updated` to update all related Guild rows and rebuilds historical
member rows at startup. User search uses normalized username/nickname/name
prefixes, and the public limit is the final count after channel-visibility
filtering.

The projection worker consumes `cordis.user.events.v1` with the
`cordis.guild.user.profiles.v1` group. It uses the shared partition-consumer
runtime: one serial worker and bounded queue per assigned partition, with
retry and offset commit coordination isolated by partition. A transient store
failure retries in that partition; after the existing retry budget is
exhausted, the event is logged, committed, and dropped.
The graceful shutdown budget is configured by `shutdownTimeout`; go-zero's
force-quit deadline is set to that budget plus its one-second wrap-up phase so
the worker can finish its bounded shutdown and final offset commit.

Permissions are a `uint64` bit set. Owners and administrators receive all
permissions. Channel evaluation applies the default role, member roles, and
member overwrites. Creating a channel always inserts an empty `@everyone`
overwrite (`applies_to=ROLE`, `applies_to_id=guild_id`, allow/deny zero) so
clients receive it without synthesizing one; that overwrite and the default
role cannot be deleted. Structural channel mutations use the Guild's
monotonic `channel_layout_revision`; the mutation transaction acquires the
Guild channel advisory lock, rejects stale revisions without writing, and
increments the layout revision once after a successful logical mutation.
Guild publishes dot-separated events directly to `cordis.guild.events.v1`;
structural channel events carry the committed layout revision.

Role member listing uses the same opaque `cursor` / `next_cursor` pagination as
Guild member listing (ordered by `joined_at`, then `user_id` descending; omit
`next_cursor` when there is no next page). Explicit roles return assigned active
members, while the default role returns every active Guild member. Assigning or
removing members for one role can use `AddGuildRoleMembers` /
`RemoveGuildRoleMembers` with up to 100 user IDs; single-member RPCs remain.
Public Guild member responses always embed the member profile. Ban responses
also embed the banned user and moderator profiles, while invite responses embed
the creator profile. Guild loads the profiles required by member and moderation
events itself because those events do not pass through the API.

Persistent Guild resources have configuration-driven hard limits. The defaults
are 10 owned and 100 joined guilds per user, 250 roles and 500 channels per
guild, 100 active invites per guild, and 100 permission overwrites per channel.
Quota checks and writes are serialized in the same PostgreSQL transaction.

`CreateGuild`, `CreateGuildRole`, `CreateGuildChannel`, and `CreateGuildInvite`
accept an optional opaque `idempotency_key` for one client creation intent.
Keys are scoped to the authenticated actor and the per-RPC operation
(`guild.create`, `guild.role.create`, `guild.channel.create`,
`guild.invite.create`) and are retained for 24 hours by default. Replaying a
key with the same request fingerprint returns the originally created resource
without duplicating any writes: `CreateGuild` does not rebuild the default
role, channels, or overwrites, `CreateGuildRole` does not consume another
position, `CreateGuildChannel` does not shift other channels or republish
channel or overwrite events, and `CreateGuildInvite` returns the first invite
code with its originally computed expiration. Reusing a key with different
parameters returns `request.idempotency_key_reused`. Requests without a key
retain their existing behavior, and different actors never share key scope.
The idempotency record and the resource writes are transactional; each
operation's retention period is configurable independently.

`CreateAvatarUpload`, `CreateGuildIconUpload`, and `CreateAttachmentUpload`
accept the same optional key, forwarded from the API through the owning domain
service to Media. Keys are scoped per kind (`media.create.user_avatar`,
`media.create.guild_icon`, `media.create.message_attachment`) and retained for
24 hours by default, never below the upload session TTL. A retry returns the
same upload ID and, while the asset is still `CREATED`, a fresh presigned PUT
URL for the same object key; it never creates another asset or consumes the
upload quota again. The response also reports the upload status snapshot and
whether an existing idempotency record was replayed. See the protocol
documentation for the full state and recovery semantics.

Internal `GetUserReadyState` returns the user's complete READY Guild bootstrap
in one call: Guild metadata, all roles, the member's explicit role IDs, and
visible channels together with their permission overwrites and the
`channel_layout_revision` represented by those channels; Session forwards this
field in the public `ready` event. Every snapshot carries a persistent `access_revision`. PostgreSQL
triggers advance this monotonic revision whenever membership, role permissions
or assignments, channels, permission overwrites, ownership, or Guild deletion
can change access. Published Guild events include the committed revision while
the Guild still exists.

## Message

gRPC on `:3002`. Owns messages, attachments, mentions, and replies. Create,
read, update, and delete operations ask Guild for authorization. Listing uses
`before`, `after`, or `around` message-ID cursor pagination. DM channel listing
uses opaque `cursor` / `next_cursor` (channel id descending). Reaction and
custom emoji RPCs are not currently implemented.

Internal message objects carry `author_id` rather than embedding a User
profile. The API batch-loads distinct profiles when composing public
`ListMessages` responses. Single-message RPC responses return the profile as
response metadata so create and update can reuse the profile already loaded
for their realtime events. Message loads any profile required by an event
itself because events do not pass through the API.

Create and update requests allow at most 10 attachments and 100 unique user
plus role mentions by default. Both limits are configured by the Message
service. Mentions are parsed server-side from content using `<@user>`,
`<@&role>`, and `@everyone` markup; clients no longer submit mention ID
lists. Role and `@everyone` mentions require the Guild `MENTION_EVERYONE`
channel permission, roles must exist in the Guild, and users must exist;
unknown entities are dropped from the parsed set. Guild direct user mentions
also pass through Guild's channel-visible-user batch check at write time. DM
channels only support user mentions.
Expanded role/everyone targets are materialized asynchronously by a worker
consuming the message event topic, and unread `mention_count` includes them.
See [mention-expansion.md](mention-expansion.md) for the full design. Image
attachment `blurhash` values are generated by Media during CompleteUpload and
denormalized into Message attachment metadata for responses and events. The
field is empty for non-image attachments or when generation is skipped. Client
supplied `blurhash` values on create/update are ignored and replaced from Media
metadata.

The internal READY RPC loads all DMs and computes read states for the visible
Guild text channels supplied by Session. Each state contains `channel_id`,
`last_message_id`, `last_read_message_id`, and the unread mention count. A
channel is unread when `last_message_id > last_read_message_id`; no unread
message count is computed. `AckMessage` advances the watermark monotonically
and publishes user-routed `message.read.updated` events for the user's other
devices.

Public DM channel responses embed the other participant's profile. Message
loads recipient profiles for `dm.channel.created` events; Session independently
batch-loads them when composing the `ready` event.

The authenticated HTTP `GetReadStates` endpoint is retained for reconciliation.
It accepts a server-defined scope rather than channel IDs: one Guild, or all
DMs. The Guild scope derives visible text channels from Guild authorization;
the DM scope also returns the complete DM channel set so missed creation events
can be repaired. API requests are bounded per user and Message bounds aggregate
query work with a process-local weighted limiter. Large server-derived scopes
are split into capacity-sized database batches; each batch acquires its exact
channel count rather than clamping one oversized query to the limiter capacity.

Client message types are currently `DEFAULT` and `REPLY`; `THREAD_STARTER` is
reserved. The only client-settable flag is `SUPPRESS_NOTIFICATIONS`. After a
write transaction commits, outbox rows are inserted in the same transaction and
a separate relay publishes them to `cordis.message.events.v1`. Guild message
records carry `guild_id`; Message `created`, `updated`, and `deleted` records
use the channel ID as the Kafka key to preserve order within a channel,
including DM records whose payload carries a recipient `user_id`.
`dm.channel.created` also uses the channel ID key. `message.read.updated` uses
a separate outbox with a `user_id:channel_id` stream and composite Kafka key.
Message events carry
`mention_user_ids`, `mention_role_ids`, and `mention_everyone`; updated events
also carry the best-effort previous mention set for client-side cleanup. The
Message service runs a background expansion consumer
(`cordis.message.mentions.v1`) over the same event topic; it only handles
created events and updated events whose mentions were rebuilt, when they
contain role or everyone mentions; it checks the stored revision, pages
channel-visible members through Guild, and atomically replaces expanded rows
under a revision guard. Expansion is eventually consistent: a message may be
visible before its `mention_count` contribution lands, and a best-effort
event loss also loses the expansion.

The expansion consumer uses the shared partition-consumer runtime, so each
assigned partition has one serial worker and its own bounded queue, retry
backoff, and offset state. A retry blocks only that partition; after the
existing retry budget is exhausted, the event is logged, committed, and
dropped.
The graceful shutdown budget is configured by `shutdownTimeout`; go-zero's
force-quit deadline is set to that budget plus its one-second wrap-up phase so
the worker can finish its bounded shutdown and final offset commit.

`CreateMessage` accepts an optional opaque `idempotency_key` for one client
creation intent. The key is scoped to the authenticated user and the
`message.create` operation, is retained for 30 minutes by default, and must be
non-empty, have no leading or trailing whitespace, and be at most 255 UTF-8
bytes. Its fingerprint includes the channel, content, normalized type, flags,
references, attachment asset IDs and order, and the parsed mention set (user
IDs, role IDs, and the everyone flag). Reusing a key with the same fingerprint
returns the original message without writing mentions or inserting another
outbox row. Reusing it with different parameters
returns `request.idempotency_key_reused`.
Requests without a key retain their existing behavior. A retry still passes
through normal authentication, authorization, and request validation; if
those checks or their dependencies fail, the retry may return that error
without creating another message. The idempotency record and message-side
writes are transactional. `CreateMessage` does not advance the author's read
state; clients mark the channel read locally after sending and call
`AckMessage` later with debounce. TTLs are operation-specific so other creation
RPCs can use different retention periods.

## Gateway

HTTP/WebSocket on `:8081`, exposing the WebSocket at `/`; operational probes are
served by a separate probe server. It sends `hello`, requires `identify` or
`resume` as the first client message, and forwards exactly one credential:
native clients send an access token in JSON `token`, while browsers send a
single-use `gateway_ticket`. It discovers
Session nodes through etcd,
reads resume ownership from Redis, and proxies the WebSocket over a
`SessionService.Connect` bidirectional stream. It owns no logical routing state
and consumes no Kafka events. WebSocket handshakes validate cross-origin
requests against the configured `originPatterns`; production deployments should
list the frontend application's origin.

Before accepting a WebSocket, Gateway applies trusted-proxy-aware source limits
using an IPv4 `/32` or IPv6 `/64`. Connection capacity is process-local: each
instance defaults to 50,000 total sockets and 5,000 pending handshakes, with
pending per-scope defaults of 100 for IPv4 and 20 for IPv6. A socket leaves the
pending buckets after Session accepts IDENTIFY or RESUME. Client connections may
send at most 120 Gateway events per minute by default. `IDENTIFY` is additionally
limited by source scope, while `RESUME` is limited by both source scope and
logical session ID; only these discrete rate-limit events use Redis.

Gateway owns physical connection liveness. It validates `heartbeat` sequences,
returns `heartbeat.ack` locally, and closes a socket after two missed advertised
intervals. Heartbeats arriving more than 10% before the advertised interval are
rejected and do not extend the liveness deadline. Only an advanced acknowledged
sequence becomes dirty state; dirty
checkpoints are coalesced, grouped by the owning Session node, and synchronized
every five seconds in batches of up to 500 by default. Session binding epochs
make delayed checkpoints from replaced connections harmless.

## Session

gRPC on `:3006` and the stateful core of realtime delivery. It validates access
tokens or atomically redeems browser Gateway tickets through Authenticator,
creates or resumes logical sessions, loads Guild visibility, owns local
user/Guild indexes, assigns sequence numbers, and keeps up to 2048 replay
events in memory.

IDENTIFY loads one complete Guild READY response and one Message READY response,
then batch-loads DM recipient profiles from User and versioned Presence
snapshots for the current user and unique DM peers:
Guild metadata, roles, member role IDs, visible channels and permission overwrites, all DMs, and four-field
read states. It does not load Presence for every Guild member. Realtime events received while these responses are assembled are
buffered and sequenced after READY. Visibility snapshots are shared by the
user's logical Sessions on the node and released after the last local Session is
removed. Loading is bounded to 100 Guilds and 500 visible channels per Guild by
default. Guild access events capture prior channel visibility, invalidate
affected snapshots by revision, and rebuild them with bounded concurrency. An
access event is delivered to the union of previous and current viewers so newly
authorized clients can add the channel and revoked clients can remove it.
Events buffered while READY is assembled are bounded by count and total event
bytes, with the effective count also capped below the replay and binding queue
capacities. Overflow discards the pending buffer and fails IDENTIFY so the next
connection rebuilds an authoritative snapshot.
On-demand rebuilds are singleflighted per user and Guild, bounded to 16
concurrent calls per Session node, and time out after two seconds by default. A
stale, missing, malformed, oversized, or otherwise invalid snapshot fails
closed. If rebuilding fails, Session skips the sensitive event and emits one
sequenced `session.reconcile` hint for that invalid snapshot generation.

Session applies Gateway checkpoint batches to advance acknowledged sequences and
trim replay windows. Client heartbeats do not directly refresh Redis ownership
or Presence; logical-session owner leases are renewed with bounded Redis
pipelines and Presence leases with a batch RPC, independently of WebSocket
heartbeat traffic. Owner TTL equals the resume timeout; maintenance runs at one
quarter of that timeout with ±20% cycle jitter to desynchronize Session nodes.
Within a cycle, 500-session batches are assigned jittered slots in a bounded
five-second spread window. Aggregate route renewal runs in a separate loop.

After token validation, `IDENTIFY` is limited by user ID and authenticator
session ID. One authenticator session may create multiple concurrent logical
sessions, such as separate browser tabs or devices. Each has an independent
session ID, replay window, Presence lease, and transport binding.

Dispatcher resolves Guild messages through aggregate Guild routes and includes
the Guild and channel IDs in a dedicated Guild-message dispatch RPC. Session checks the server-owned
visibility snapshot once per local user and forwards the message to all of that
user's logical sessions. DM message records resolve directly through aggregate
user routes. Message records without exactly one aggregate Guild/user route are
rejected.

Missing Presence status on IDENTIFY preserves the existing user preference and
initializes it to online only when none exists; client state defaults to
foreground. Explicit values are validated strictly. Later Presence Updates use
partial-update semantics: status changes the shared user preference, while
client state changes only the current logical Session. Empty updates are
rejected, and no-op client-state updates are discarded. Forwarded updates are
limited to five per logical session every 20 seconds, then consume a shared
per-user quota of ten per 20 seconds across devices.

Detached sessions live for 120 seconds by default. Resume must reach the
original node. Session nodes register through etcd leases. Graceful drain
publishes a draining state, rejects new connections, and gradually instructs
existing clients to identify again.

## Dispatcher

Background Kafka consumer for the Guild, Message, User, and Presence event
topics. Each topic has its own consumer client, loop, and group
(`cordis.dispatcher.{guild,message,user,presence}.v1`) so backlog, retry, and
rebalance are isolated while routing and Session connections remain shared.
Dispatcher, Guild's profile projector, and Message's mention expander share
the same partition-consumer runtime for worker lifecycle, retry isolation, and
manual offset coordination.
Each currently assigned partition has one long-lived serial worker. Polls enqueue
records into a bounded per-partition queue, pausing only that partition when its
queue is full. Dispatcher resolves aggregate user/Guild routes in Redis and calls
the Session node's dispatch RPC. Profile updates load the subject's Guild,
relationship, and DM audiences before fan-out. Completed records are coalesced
and committed by a background offset coordinator; a rebalance or shutdown
synchronously flushes completed records. Invalid events are dropped and
committed; transient failures retry with exponential backoff in that
partition's worker, so other partitions and topics continue. On rebalance or
shutdown, workers stop and only completed records are committed. If Kafka
commit lag reaches `maxUncommittedRecords`, that partition's fetch is paused
until a commit succeeds; this pause does not override an independent queue or
retry pause. Worker shutdown during revoke is bounded by
`revokeTimeoutSeconds`; a worker that does not stop in time is marked inactive
and its in-flight record is left for Kafka to replay rather than committed.
Routes are deduplicated within one attempt, but a record retry can call an already
successful node again. Delivery is at least once and there is no general event-ID
deduplication.
The graceful shutdown budget is configured by `shutdownTimeout`; go-zero's
force-quit deadline is set to that budget plus its one-second wrap-up phase.

## Presence

gRPC on `:3003`. Redis-backed user preference and device-liveness storage. TTL
and generation checks filter stale sessions. Status is stored once per user;
Sessions store client state and liveness only. Public presence is offline when
there are no live Sessions or the preference is `INVISIBLE`, and otherwise
equals the preference. Session registration may initialize a missing
preference but never overwrite an existing one, and lease refreshes do not
carry status.

Presence persists the user preference without a TTL and keeps a separate
versioned aggregate snapshot, including an offline tombstone. Preference
changes emit private `presence.preference.updated` events along the user route;
public transitions emit `presence.updated`. Each stream has its own monotonic
version. Each aggregate status transition receives a monotonic
Snowflake-based `version` (clamped above the previous value across service
nodes); unchanged refreshes retain the version, while
`last_seen_at` may advance without creating a transition. Internal resolve
responses and the corresponding `presence.updated` event carry the same
version. Mutations and on-demand resolution reconcile under a per-user Redis
lock so callers cannot observe a new status paired with the previous version.
