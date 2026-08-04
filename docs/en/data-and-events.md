# Data Storage and Events

PostgreSQL ownership is divided by service: User owns users/profiles,
Authenticator owns authentication sessions, Guild owns guild domain tables,
and Message owns messages, mentions, and serialized attachment data. The latest
migrations removed the old reaction, emoji, and legacy outbox tables; Message
now uses a transactional outbox for event delivery.

SQL migrations are embedded into service binaries and applied lexicographically
by `pkg/migration.Apply`; `*.down.sql` is skipped. Cross-table integrity is
mostly enforced by application logic rather than foreign keys. Active soft
deleted entities generally use `deleted_at = 0`.

Stores are interfaces. Most SQL implementations keep both a database handle
and an `sqlx.ExtContext`; transactions replace the executor with `*sqlx.Tx`.
The Message, User, Authenticator, Media, and Guild stores use the native pgx
API (`*pgxpool.Pool` and `pgx.Tx`); the Message store shares the pgx stack with
its outbox relay, and all native stores get query tracing from `otelpgx`.
Postgres handles for the sqlx stores come from `pkg/database.NewPostgres`,
which wraps `database/sql` with otelsql tracing. User, Guild, and Message roll
back on errors and panics. Tests inject fake stores and other dependencies.

Entity IDs use Snowflake with a 2025-01-01 epoch, a node derived from a
non-loopback IP hash, 16 node bits, and 8 step bits. Event JSON encodes 64-bit
IDs as strings to preserve JavaScript precision.

Kafka events use:

```json
{
  "t": "message.deleted",
  "d": {
    "id": "123",
    "channel_id": "456",
    "revision": 3,
    "deleted_at": 1784190002000
  }
}
```

Stable names live in `pkg/realtime` and use dot-separated hierarchy.
Message created/updated events carry the parsed mention set
(`mention_user_ids`, `mention_role_ids`, `mention_everyone`) and, for updates,
a best-effort previous set. The Message service's mention expansion consumer
(`cordis.message.mentions.v1`) reads the same event topic, so role and
`@everyone` targets are materialized from the same best-effort stream that
delivers events to clients; see [mention-expansion.md](mention-expansion.md).
Guild channel list responses expose a Guild-level `channel_layout_revision`.
Create, delete, parent-move, and reorder events carry the committed layout
revision in addition to each channel's own `revision`; stale structural
requests are rejected rather than replayed.

Guild and Presence do not use an outbox. After the business transaction
commits, Guild publishes best-effort to `cordis.guild.events.v1`, and Presence
publishes public transitions and private preference changes best-effort to
`cordis.presence.events.v1`. Presence persists the relevant versioned state
before publishing and uses that same version as the event idempotency key.
Publish failure is logged and does not fail the already committed RPC, so
database and Kafka delivery are not atomic.

User uses a PostgreSQL transactional outbox. Profile and relationship
transactions insert outbox rows together with the business changes, and a
separate relay publishes them to `cordis.user.events.v1`. All user events use
the recipient user ID as both the stream key and Kafka key; fanout records of
one logical event share `event_id` and are distinguished by `delivery_index`.
The envelope carries `idempotency_key` (the logical `event_id`),
`stream_sequence`, and `delivery_index`.

Message uses a PostgreSQL transactional outbox. The business transaction
inserts outbox rows together with message state changes, and a separate relay
publishes them to `cordis.message.events.v1`. Message events
(`message.created`, `message.updated`, `message.deleted`, and
`dm.channel.created`) use `channel_id` as both the stream key and Kafka key.
Read-state events (`message.read.updated`) use a separate outbox and stream
keyed by `user_id:channel_id`, with the same composite Kafka key; Dispatcher
routing still uses the payload `user_id`. The envelope carries
`idempotency_key` (the logical `event_id`), `stream_sequence`, and
`delivery_index`; fanout records of one logical event share `event_id` and are
distinguished by `delivery_index`.

Guild's `guild_member_profiles` table is a local search projection, not the
source of truth for User profiles. It stores username, Guild nickname, profile
name, and avatar data for active members; membership removal deletes the
projection row in the same transaction. User profile events update related
rows best-effort, and the Guild startup rebuild can repopulate the projection.
`CreateGuild` commits a placeholder projection row before its best-effort User
profile hydration, so a temporary User outage does not fail Guild creation.
The profile projector consumes `cordis.user.events.v1` in the
`cordis.guild.user.profiles.v1` group.

For `CreateMessage`, an optional request idempotency record is committed with
the message and mentions. `CreateMessage` no longer advances the author's read
state; clients locally mark the channel read after sending and call
`AckMessage` later with debounce, following Discord's model. A same-key retry
returns the existing message without inserting another outbox row when normal
authentication, authorization, and request validation succeed. Those checks
may still return their usual error without creating another message.

Guild creation RPCs (`CreateGuild`, `CreateGuildRole`, `CreateGuildChannel`,
and `CreateGuildInvite`) commit their idempotency record in the same
transaction as the resource writes. A same-key retry returns the originally
created resource without republishing creation, channel-shift, or overwrite
events. The same best-effort publication window applies.

Upload creation RPCs (`CreateAvatarUpload`, `CreateGuildIconUpload`,
`CreateAttachmentUpload`) commit their idempotency record in the same
transaction as the asset write. A same-key retry returns the same upload
without creating another asset or consuming the quota again; Media publishes
no creation events itself, so upload idempotency has no event-suppression
concerns. The response exposes the Media status snapshot and whether the
existing idempotency record was replayed; terminal assets remain bound to the
old key until its retention expires.
