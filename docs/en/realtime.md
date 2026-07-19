# Realtime System

## Connection lifecycle

1. The client opens a Gateway WebSocket.
2. Gateway sends `HELLO` with a 45-second heartbeat interval.
3. The client sends `IDENTIFY`, or `RESUME` with a session ID and sequence.
4. Gateway selects a ready Session node from etcd. For resume, it reads the
   owner from Redis and validates the node generation through etcd.
5. Gateway opens `SessionService.Connect` and forwards the first request.
6. Session returns a sequenced `READY`, or replays missing events followed by
   `RESUMED`.
7. Presence updates, detach, and server events use the same stream; Gateway
   handles heartbeat frames locally and batches sequence checkpoints to Session.

A WebSocket connection and a logical Session are separate objects. Gateway IDs
include a generation so stale instances can be rejected.

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
logical sessions. Access events invalidate affected snapshots. Rebuilds fail
closed; a failed rebuild produces one sequenced `session.reconcile` hint for the
current invalid snapshot generation. Membership removal or ban events are sent
before the user's Guild index is revoked.

## etcd directory and Redis keys

- `/cordis/session/nodes/{node_id}`: leased etcd key containing generation,
  RPC address, and ready/draining state;
- `session:owners:{session_id}`: logical Session owner;
- `gateway:routes:users:{id}:nodes`;
- `gateway:routes:guilds:{id}:nodes`.

Route members contain node ID and generation. Redis TTLs, etcd leases, and
read-time generation validation remove stale processes.

Domain services publish `{t,d}` envelopes to Kafka. Dispatcher resolves routes
and invokes Session. Session filters Guild messages with visibility snapshots,
assigns sequence, stores replay, and writes a response to Gateway. Delivery is
at least once under retry; there is no general event-ID deduplication yet.
