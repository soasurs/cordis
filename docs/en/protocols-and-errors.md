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
explicitly present default value is still an update—for example, an empty
`avatar_uri` clears the avatar. Requests with no mutable fields are rejected.

Edition 2023 scalar presence is carried through both the public and internal
protobuf APIs. API adapters therefore forward a field only when its generated
`HasFoo` method reports that it is present. Service and store code retain that
presence through pointer-like update parameters and update only the selected
columns; callers must not fetch a resource, compose a complete replacement,
and write unrelated fields back. When present, collection-valued update fields
replace the complete collection unless dedicated add/remove operations are
defined.

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
