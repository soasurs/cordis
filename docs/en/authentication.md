# Authentication and Token Rotation

## Credential transports

Cordis supports two explicit token transports. Native, CLI, and service clients
use `TOKEN_TRANSPORT_TOKEN`: login responses contain the access and refresh
tokens, API calls send `Authorization: Bearer <access-token>`, and Gateway
`identify` / `resume` JSON sends `token`. Browser clients use
`TOKEN_TRANSPORT_COOKIE`: API sets host-only, `HttpOnly`, `SameSite=Strict`
access and refresh cookies and omits both token strings from the response body.
`Secure` is disabled only by the local HTTP configuration and must be enabled
for production HTTPS.

An explicit Authorization header always wins. An invalid Bearer credential is
never allowed to fall back to cookies. Cookie-authenticated requests must carry
an Origin from `browserAuth.allowedOrigins`; this check, together with strict
SameSite cookies, protects browser API calls from CSRF. The refresh cookie uses
`Path=/` because any protected API procedure may need to refresh before running
its handler. Tokens and complete Cookie headers must never be logged.

`Login` and `CompleteTwoFactorLogin` must use the same requested transport. An
unspecified transport preserves compatibility and behaves as token transport.
Cookie `Refresh` and `Logout` omit `refresh_token` from the request; token
clients continue to put it in the protobuf request.

## Browser transparent refresh

Protected cookie requests are authenticated before domain work starts. A valid
access cookie proceeds normally. When the access cookie is absent or contains a
valid but expired access token, API sends both cookies to Authenticator. A valid
refresh credential is rotated, new cookies are added to the same HTTP response,
and the original handler runs under the renewed identity. A malformed or
incorrectly signed access token does not fall back to refresh, and access and
refresh credentials from different authentication sessions are rejected.

The browser should not run an access-token timer or call Refresh periodically.
It retains the existing page while transient network failures are retried and
only enters an unauthenticated state for an expired, revoked, or replay-revoked
authentication session. Background pages therefore do not keep an idle session
alive merely by running a timer.

## Refresh rotation and sessions

Refresh tokens rotate after every successful use. Authenticator stores only
token hashes plus the current token's non-secret JWT claims. The immediately
previous refresh token has a 30-second recovery window. Reusing it during that
window reconstructs and returns the same current refresh token rather than
creating another generation. This makes concurrent browser requests and a retry
after a lost response idempotent. A valid token from an older generation, or the
previous token after the window, is treated as replay and revokes the session.

Authentication sessions expire after 30 days without a refresh and have an
absolute 180-day lifetime. A refresh advances the idle deadline to the smaller
of `now + 30 days` and the absolute deadline. Rotation never extends the
absolute deadline. `session_expires_at` reports the current idle deadline and
`absolute_session_expires_at` reports the hard deadline.

## Native client practice

Native clients keep refresh tokens in Keychain, Android Keystore, or an
equivalent OS-protected store. Access tokens should normally stay in memory;
persisting one for cold-start latency also requires protected storage. A single
process-wide auth manager owns rotation: concurrent unauthenticated responses
wait for one refresh operation, the new refresh token is persisted before the
new access token is published, and each failed request is retried at most once.
Network failures do not erase credentials or force the login screen. Explicit
session-expired, session-revoked, invalid-refresh, or replay outcomes do.

## Browser Gateway tickets

Browser JavaScript cannot read HttpOnly cookies and cannot add an Authorization
header with the standard WebSocket API. It calls `CreateGatewayTicket` while
opening the WebSocket in parallel, waits for both the ticket and Gateway
`hello`, then sends `gateway_ticket` in `identify` or `resume`. Ticket creation
uses the same transparent cookie authentication and refreshes first when the
remaining access-token lifetime cannot cover the ticket window.

Tickets contain 256 bits of random entropy, live for 30 seconds, are stored in
Redis only under their SHA-256 hash, and are removed atomically when Session
redeems them. Mobile and other native clients do not request tickets and keep
using the `token` JSON field. Supplying both fields, or neither field, is a
protocol error. Gateway still validates the WebSocket Origin allowlist.
