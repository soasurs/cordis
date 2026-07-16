# Realtime System

## Connection lifecycle

1. The client opens a Gateway WebSocket.
2. Gateway sends `HELLO` with a 45-second heartbeat interval.
3. The client sends `IDENTIFY`, or `RESUME` with a session ID and sequence.
4. Gateway discovers a ready Session node or resolves the session owner.
5. Gateway opens `SessionService.Connect` and forwards the first request.
6. Session returns a sequenced `READY`, or replays missing events followed by
   `RESUMED`.
7. Heartbeats, presence updates, subscriptions, detach, and server events use
   the same stream.

A WebSocket connection and a logical Session are separate objects. Gateway IDs
include a generation so stale instances can be rejected.

## Replay

Replayable dispatches receive monotonically increasing sequence numbers. Each
session retains at most 2048 entries. Heartbeats carry the highest processed
sequence; ACK progress is monotonic and releases the acknowledged prefix.

Resume fails when the requested sequence is below the replay floor, beyond the
server sequence, or the detached session has expired. Replay is memory-only and
cannot move between nodes.

## Subscriptions and permissions

IDENTIFY creates user and guild routes. Channels require explicit subscription,
authorized by Guild's `VIEW_CHANNEL` check. Channel and permission events cause
Session to reauthorize affected local sessions. Removal or ban events are sent
before the user's guild and channel indexes are revoked.

## Redis keys

- `session:nodes`: live Session node index;
- `session:nodes:{node_id}`: node generation, RPC address, state, and expiry;
- `session:owners:{session_id}`: logical Session owner;
- `gateway:routes:users:{id}:nodes`;
- `gateway:routes:guilds:{id}:nodes`;
- `gateway:routes:channels:{id}:nodes`.

Route members contain node ID and generation. TTLs plus read-time validation
remove stale processes.

Domain services publish `{t,d}` envelopes to Kafka. Dispatcher resolves routes
and invokes Session. Session filters local subscriptions, assigns sequence,
stores replay, and writes a response to Gateway. Delivery is at least once
under retry; there is no general event-ID deduplication yet.
