# Realtime System

## Connection lifecycle

1. The client opens a Gateway WebSocket.
2. Gateway sends `hello` with a 45-second heartbeat interval.
3. The client sends `identify`, or `resume` with a session ID and sequence.
   Native clients provide an access token in `token`; browsers obtain a
   single-use ticket from API and provide `gateway_ticket`.
4. Gateway selects a ready Session node from etcd. For resume, it reads the
   owner from Redis and validates the node generation through etcd.
5. Gateway opens `SessionService.Connect` and forwards the first request.
6. Session verifies the access token or atomically redeems the ticket through
   Authenticator.
7. Session returns a sequenced `ready`, or replays missing events followed by
   `resumed`.
8. Presence updates, detach, and server events use the same stream; Gateway
   handles `heartbeat` frames locally and batches sequence checkpoints
   to Session.

A WebSocket connection and a logical Session are separate objects. Gateway IDs
include a generation so stale instances can be rejected.

## Presence updates

Clients may set the logical Session's initial Presence in `IDENTIFY` (opcode
`2`):

```json
{
  "op": 2,
  "d": {
    "gateway_ticket": "...",
    "device_type": "desktop",
    "status": "online",
    "client_state": "foreground"
  }
}
```

An omitted `status` defaults to `online`, and an omitted `client_state`
defaults to `foreground`. When present, `status` accepts only `online`, `idle`,
`dnd`, or `invisible`; `client_state` accepts only `foreground` or
`background`. `offline`, `null`, non-string values, empty strings, and unknown
values are invalid. `invisible` is exposed as offline in aggregate results
visible to other users.

After the connection is established, opcode `3` partially updates the current
logical Session. Omitted fields retain their current values, and at least one
field is required:

```json
{"op":3,"d":{"status":"idle"}}
{"op":3,"d":{"client_state":"background"}}
{"op":3,"d":{"status":"invisible","client_state":"foreground"}}
```

`status` normally comes from a user-facing status selector. Clients should
derive `client_state` automatically from page visibility, window focus, or the
application lifecycle. Each device or page has independent values on its
logical Session, which Presence aggregates across all active Sessions for the
user. `RESUME` retains the logical Session's existing values; clients send
opcode `3` afterward when a change is needed.

Gateway reports invalid input with stable `error` event codes:
`presence_update_empty` for an empty update, `presence_status_invalid` for an
invalid status, and `presence_client_state_invalid` for an invalid client
state. Session and the internal Presence service also reject invalid values
that bypass Gateway with `InvalidArgument`.

The `ready` payload includes a `presences` array for the current user and every
unique DM peer, but not all members of the user's Guilds. Each item contains
`user_id`, aggregate `status`, `last_seen_at`, and `version`. IDs and versions
are decimal strings in WebSocket JSON. Clients keep the greatest version seen
per user: apply a later `presence.updated` event only when its version is
greater than the READY or previously applied version. This makes events
buffered during READY assembly safe even when the snapshot and event describe
the same aggregate transition.

## Replay

Replayable dispatches receive monotonically increasing sequence numbers. Each
session retains at most 2048 entries. Heartbeats carry the highest processed
sequence; ACK progress is monotonic and releases the acknowledged prefix.

Resume fails when the requested sequence is below the replay floor, beyond the
server sequence, or the detached session has expired. Replay is memory-only and
cannot move between nodes.

## Routing and permissions

IDENTIFY creates user and Guild routes. Dispatcher routes Guild messages to
candidate Session nodes by Guild, and Session checks its revisioned
per-user visibility snapshot before delivering to all of that user's local
logical sessions. Access events capture prior channel visibility, invalidate and
rebuild affected snapshots with bounded concurrency, and are delivered to the
union of previous and current viewers. Rebuilds fail closed; a failed rebuild
produces one sequenced `session.reconcile` hint for the current invalid snapshot
generation. Membership removal or ban events are sent before the user's Guild
index is revoked.

For `user.profile.updated`, Dispatcher loads shared Guilds, non-blocked
relationships, and existing DM peers, then also includes the profile owner's
user route. Session deduplicates each local recipient by event ID across Guild
and direct-user paths before delivering the full profile snapshot.

## etcd directory and Redis keys

- `/cordis/session/nodes/{node_id}`: leased etcd key containing generation,
  RPC address, and ready/draining state;
- `session:owners:{session_id}`: logical Session owner;
- `gateway:routes:users:{id}:nodes`;
- `gateway:routes:guilds:{id}:nodes`.

Route members contain node ID and generation. Redis TTLs, etcd leases, and
read-time generation validation remove stale processes.

Domain services publish `{t,d}` envelopes to Kafka. Dispatcher uses an
independent consumer group and loop for each domain topic, then resolves routes
and invokes Session through a shared connection pool. Session filters Guild
messages with visibility snapshots, assigns sequence, stores replay, and writes
a response to Gateway. Delivery is at least once under retry; profile fan-out
has recipient-level event deduplication, but there is no general event-ID
deduplication yet.
