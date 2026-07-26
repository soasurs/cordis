# Configuration, Observability, and Development

Default ports:

| Service | Port |
| --- | --- |
| User | 3000 |
| Authenticator | 3001 |
| Message | 3002 |
| Presence | 3003 |
| Guild | 3005 |
| Session | 3006 |
| API | 8080 |
| Gateway | 8081 |

Dispatcher has no listening port. Config files live under each service's
`etc/config.yaml` and are loaded with environment expansion.

The API `inbound` section controls HTTP timeouts and header limits, the total
HTTP body limit, the decompressed Connect message limit, the default RPC
deadline, the per-instance global concurrency cap, the CPU shedding threshold,
the graceful shutdown budget, and the inbound breaker.
`procedureTimeouts` map can override the default deadline by full Connect
procedure, while `serviceMaxMessageBytes` can override the decompressed message
limit for the `authenticator`, `user`, `message`, and `guild` handlers.
Unlisted procedures and services retain the global defaults. The write timeout
must exceed the sum of the read timeout and largest RPC deadline, preserving
the full handler and Connect response encoding budget after a request body is
read. The graceful shutdown budget must in turn exceed the write timeout.
Downstream zrpc calls from API to each domain service have an explicit
two-second default and remain bounded by the default three-second inbound
parent deadline. `cpuThreshold` uses millicpu-style units, so the default `900`
means 90%; setting it to `0` disables adaptive shedding. `maxConcurrency`
counts HTTP/1 and HTTP/2 requests shared by all four public services, not TCP
connections.

Infrastructure dependencies are PostgreSQL for domain persistence, Kafka for
Guild/Message events, etcd for leased Session-node registration and discovery,
and Redis for Presence, resume ownership, and aggregate realtime routing. RPC
services support OTLP tracing through `CORDIS_OTEL_ENDPOINT`. Postgres
connections opened by `pkg/database.NewPostgres` are instrumented with
`github.com/XSAM/otelsql`, so store calls that pass request context become
child spans of the active RPC trace. Metrics are exposed through go-zero
dev servers or API observability settings.
Authenticator encrypts TOTP secrets with AES-256-GCM using the independent
`CORDIS_TOTP_ENCRYPTION_KEY`. It must be a Base64-encoded 32-byte random key
and must not be reused for JWT signing. Guild, User, and Message sign opaque
list cursors with `CORDIS_CURSOR_SECRET` (at least 32 bytes); the three services
must share the same secret.

Common commands:

```bash
go fix ./...
go build ./...
go vet ./...
go tool staticcheck -checks=inherit,-SA5008 ./...
make generate
make lint
make test
```

Run `go fix ./...` after each round of Go edits and before the relevant build
and test commands. Staticcheck is pinned in `go.mod` and is invoked through
`go tool`; no separately installed binary is required. `SA5008` is excluded
because go-zero's extended `json` configuration tags (`default`, `optional`,
`options`, and `range`) are false positives and must retain their extensions.
Configuration-tag changes still require a manual syntax review. Other
actionable diagnostics in changed code should be resolved.

Tests use `testify/require`. Day-to-day development does not require Docker:

```bash
make test
```

SQL stores are covered by Postgres integration tests (`make test-integration`),
not by mocked database/sql drivers.

Real-backend integration tests use the `integration` tag and start fixed-version
PostgreSQL, Redis, Kafka (KRaft), and etcd via Testcontainers without requiring
already-running services:

```bash
make test-integration
```

Integration tests cover every Store interface method against real backends;
Guild and Message Kafka publishing; Redis Store methods for Presence and
Session; Gateway Redis + etcd resolution; and the full Kafka → Dispatcher →
Redis routes → etcd Session-node directory → gRPC Session dispatch chain.
Kafka topics, consumer groups, and etcd prefixes use run-scoped random names to
avoid cross-test pollution. Cross-service composition tests run the caller
in-process against real service binaries (User, Guild) for Message channel
authorization and Authenticator registration/login.

For manual multi-service debugging use the fixed-version Compose stack:

```bash
make compose-up
# run migrations and services in the documented startup order
make compose-down
```

Compose keeps named volumes; run `docker compose down -v` to wipe local
development data.

For local startup, bring up PostgreSQL, Redis, etcd, and Kafka first; then start
User, Authenticator, Guild, Message, Presence, and Session; finally start API,
Gateway, and Dispatcher. Session's advertised address must be reachable by both
Gateway and Dispatcher.

Single addresses in repository configs are for local development. Production
should configure multiple `sessionRegistry.hosts` endpoints and Redis Cluster:

```yaml
redis:
  host: redis-0:6379,redis-1:6379,redis-2:6379
  type: cluster
```

Redis Cluster pipelines can dispatch commands across slots but do not make
cross-key updates atomic. Owner writes are single-key operations; aggregate
routes and Presence indexes tolerate partial updates through TTLs, generations,
and read-time validation.

Generated files under `gen` are not edited manually. Commits use scoped
Conventional Commits and must be signed off with `git commit -s`.
