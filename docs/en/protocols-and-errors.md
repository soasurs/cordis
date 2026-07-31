# APIs, Protocols, and Errors

Public protobuf files under `proto/api` generate opaque Go APIs and Connect-Go.
Internal protobuf files also use edition 2023 opaque Go APIs, so code uses
generated getters, setters, and builders throughout. After proto changes, run:

```bash
make generate
make lint
```

## Resource updates

Resource `Update` RPCs use partial-update semantics. Only fields explicitly
present in a request are changed; omitted fields keep their stored values. An
explicitly present default value is still an update—for example, an explicitly
present empty `bio` clears the bio, and an explicitly present `avatar_asset_id`
of `0` clears the avatar. Requests with no mutable fields are rejected.

Edition 2023 scalar presence is carried through both the public and internal
protobuf APIs. API adapters therefore forward a field only when its generated
`HasFoo` method reports that it is present. Service and store code retain that
presence through pointer-like update parameters and update only the selected
columns; callers must not fetch a resource, compose a complete replacement,
and write unrelated fields back. When present, collection-valued update fields
replace the complete collection unless dedicated add/remove operations are
defined.

Channel creation, deletion, parent moves, and reordering use a Guild-level
`channel_layout_revision` as an optimistic concurrency token. `ListGuildChannels`
returns the token, and structural requests must send the revision from the
client's snapshot. The Guild service checks it after acquiring the transaction
advisory lock; a stale token aborts the transaction with `Aborted`, and the
public Connect API exposes this as HTTP `409 Conflict`. The server does not
refresh or replay a stale client operation; clients must explicitly refresh and
ask the user to retry. Name/topic-only channel updates do not change or require
the layout revision. Internal READY Guild entries and the public `ready` event
carry the layout revision for each channel snapshot; Resume continues the
existing event replay protocol and does not add a separate layout token.

## Create request idempotency

`CreateMessageRequest.idempotency_key` is optional and opaque. When present, it
must be non-empty, contain no leading or trailing whitespace, and be no longer
than 255 UTF-8 bytes. The key is scoped by authenticated actor and operation;
for messages the operation is `message.create`. Clients should generate a new
key for each business intent and reuse it only when retrying that intent.

Message stores a SHA-256 fingerprint of the normalized request. It includes the
channel, content, normalized type, flags, references, attachment asset IDs in
their submitted order, and normalized mention IDs. The same key and
fingerprint returns the original message; the same key with different
parameters returns `InvalidArgument` with public code
`request.idempotency_key_reused`. The record is retained for 30 minutes by
default for CreateMessage, and requests without a key remain compatible with
older clients. Retries still pass through normal authentication,
authorization, and request validation; those checks may return their usual
error without creating another message. Idempotency retention is configured
per operation because different creation RPCs have different retry windows.

## Availability checks and avatar constraints

`CheckUsernameAvailability` reuses the same username normalization and format
rules as `UpdateUsername`. Invalid usernames return `InvalidArgument`; a valid
but taken handle returns `available=false`. Final renames still depend on the
database unique constraint. Media represents avatar validation failures with
stable internal `media.cordis` reasons; the API maps them to the public codes
`profile.avatar_file_too_large`, `profile.avatar_content_type_invalid`,
`profile.avatar_dimensions_exceeded`, and `profile.avatar_pixels_exceeded`.
`GetAvatarUploadConstraints` returns Media's current `userAvatar` image limits
clients should enforce before requesting a presigned PUT.

WebSocket envelopes contain `op`, optional `s`, optional `t`, and `d`. Important
opcodes are dispatch `0`, heartbeat `1`, identify `2`, presence `3`, resume `6`,
invalid session `9`, hello `10`, and heartbeat ACK `11`.
The `identify` and `resume` data objects contain exactly one credential:
native clients use `token` with an access token, while browsers use the
short-lived `gateway_ticket`. Supplying both or neither is rejected.
All event types are lowercase dot-separated names. Gateway event types and
directions are:

| `t` | Direction | `op` |
| --- | --- | ---: |
| `hello` | Server to client | `10` |
| `identify` | Client to server | `2` |
| `ready` | Server to client | `0` |
| `resume` | Client to server | `6` |
| `resumed` | Server to client | `0` |
| `heartbeat` | Client to server | `1` |
| `heartbeat.ack` | Server to client | `11` |
| `error` | Server to client | `4000` |

Snowflake IDs in `ready` and domain event payloads are decimal strings.
Sequences, revisions, and timestamps remain JSON numbers.

Domain services create gRPC statuses through `pkg/rpcerror.New` and attach
`google.rpc.ErrorInfo` with stable domain and reason values. Public API calls
use `apierror.FromRPC` to produce public codes and `api.v1.PublicErrorInfo`
without exposing unknown internal errors. Some Gateway and Presence validation
still uses plain gRPC status errors.

Authenticator owns credential verification and token issuance. Session calls
Authenticator to verify access tokens or atomically redeem Gateway tickets for
IDENTIFY/RESUME. Browser API calls use HttpOnly cookies and transparent refresh;
native clients use Bearer access tokens and explicit refresh. The complete
rotation and client rules are in [Authentication and token rotation](authentication.md).
Guild is the authority for membership, roles,
and channel permissions; Message and Session call Guild instead of duplicating
its permission algorithm.
