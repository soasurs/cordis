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

Infrastructure dependencies are PostgreSQL for domain persistence, Kafka for
Guild/Message events, etcd for leased Session-node registration and discovery,
and Redis for Presence, resume ownership, and aggregate realtime routing. RPC
services support OTLP tracing through `CORDIS_OTEL_ENDPOINT`. Metrics are
exposed through go-zero dev servers or API observability settings.
Authenticator encrypts TOTP secrets with AES-256-GCM using the independent
`CORDIS_TOTP_ENCRYPTION_KEY`. It must be a Base64-encoded 32-byte random key
and must not be reused for JWT signing.

Common commands:

```bash
make generate
make lint
make test
go build ./...
go vet ./...
```

Tests use `testify/require`; SQL stores use `sqlmock`. Redis integration tests
require an explicit integration tag and address. PostgreSQL integration tests
are not currently part of the normal workflow.

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
