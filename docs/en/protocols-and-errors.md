# APIs, Protocols, and Errors

Public protobuf files under `proto/api` generate open Go APIs and Connect-Go.
Internal protobuf files use edition 2023 opaque Go APIs, so code
uses generated getters, setters, and builders. After proto changes, run:

```bash
make generate
make lint
```

WebSocket envelopes contain `op`, optional `s`, optional `t`, and `d`. Important
opcodes are dispatch `0`, heartbeat `1`, identify `2`, presence `3`, subscribe
`4`, resume `6`, invalid session `9`, hello `10`, and heartbeat ACK `11`.
Domain event types are lowercase dot-separated names. Gateway lifecycle types
are uppercase constants such as `READY` and `RESUMED`.

Snowflake IDs in WebSocket JSON are decimal strings. In particular,
`SUBSCRIBE.channel_ids` accepts only strings, and `READY`, `SUBSCRIBED`, and
domain event payloads emit IDs as strings. Sequences, revisions, and timestamps
remain JSON numbers.

Domain services create gRPC statuses through `pkg/rpcerror.New` and attach
`google.rpc.ErrorInfo` with stable domain and reason values. Public API calls
use `apierror.FromRPC` to produce public codes and `api.v1.PublicErrorInfo`
without exposing unknown internal errors. Some Gateway and Presence validation
still uses plain gRPC status errors.

Authenticator owns credential verification and token issuance. Session calls
Authenticator for IDENTIFY/RESUME. Guild is the authority for membership, roles,
and channel permissions; Message and Session call Guild instead of duplicating
its permission algorithm.
