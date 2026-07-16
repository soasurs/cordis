# Data Storage and Events

PostgreSQL ownership is divided by service: User owns users/profiles,
Authenticator owns authentication sessions, Guild owns guild domain tables,
and Message owns messages, mentions, and serialized attachment data. The latest
migration removes the old reaction, emoji, and outbox tables.

SQL migrations are embedded into service binaries and applied lexicographically
by `pkg/migration.Apply`; `*.down.sql` is skipped. Cross-table integrity is
mostly enforced by application logic rather than foreign keys. Active soft
deleted entities generally use `deleted_at = 0`.

Stores are interfaces. SQL implementations keep both a database handle and an
`sqlx.ExtContext`; transactions replace the executor with `*sqlx.Tx`. User,
Guild, and Message roll back on errors and panics. Tests inject fake stores and
other dependencies.

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

Message and Guild do not use an outbox. After the business transaction commits,
Message publishes best-effort to `message.events`, while Guild publishes
best-effort to `cordis.guild.events.v1`. The business ID is used as the Kafka
key to preserve per-channel or per-guild partition order. With Kafka disabled,
no producer is created. Publish failure is logged and does not fail the already
committed RPC, so database and Kafka delivery are not atomic.
