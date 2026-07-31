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
Guild metadata includes an optional description of up to 1024 Unicode
characters. Name and description use the presence-aware `UpdateGuild` RPC;
an empty description clears it. Icons use the separate direct-upload flow and
are associated with the Guild only when `CompleteGuildIconUpload` succeeds.

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

Create and update requests allow at most 10 attachments and 100 unique mentioned
user IDs by default. Both limits are configured by the Message service. Image
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
write transaction commits, the service publishes directly to `cordis.message.events.v1`
on a best-effort basis; failures are logged. Guild message records carry
`guild_id` and use the Guild ID as the Kafka key. DM message records carry
`user_id` and emit one user-keyed record per participant.

`CreateMessage` accepts an optional opaque `idempotency_key` for one client
creation intent. The key is scoped to the authenticated user and the
`message.create` operation, is retained for 30 minutes by default, and must be
non-empty, have no leading or trailing whitespace, and be at most 255 UTF-8
bytes. Its fingerprint includes the channel, content, normalized type, flags,
references, attachment asset IDs and order, and normalized mentions. Reusing a
key with the same fingerprint returns the original message without writing
mentions, advancing read state, or publishing another creation event. Reusing
it with different parameters returns `request.idempotency_key_reused`.
Requests without a key retain their existing behavior. A retry still passes
through normal authentication, authorization, and request validation; if
those checks or their dependencies fail, the retry may return that error
without creating another message. The idempotency record and message-side
writes are transactional, while the existing commit-to-Kafka best-effort
publication window remains. TTLs are operation-specific so other creation RPCs
can use different retention periods.

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
Dispatcher resolves aggregate user/Guild routes in Redis and calls the Session
node's dispatch RPC. Profile updates load the subject's Guild, relationship,
and DM audiences before fan-out. Offsets are committed manually. Invalid events
are dropped and committed; transient failures retry with exponential backoff.
Routes are deduplicated within one attempt, but a record retry can call an
already successful node again. Delivery is at least once and there is no
general event-ID deduplication.

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
