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
Guild/Message events, and Redis for Presence plus realtime discovery and
routing. RPC services support OTLP tracing through `CORDIS_OTEL_ENDPOINT`.
Metrics are exposed through go-zero dev servers or API observability settings.

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

For local startup, bring up PostgreSQL, Redis, and Kafka first; then start User,
Authenticator, Guild, Message, Presence, and Session; finally start API,
Gateway, and Dispatcher. Session's advertised address must be reachable by both
Gateway and Dispatcher.

Generated files under `gen` are not edited manually. Commits use scoped
Conventional Commits and must be signed off with `git commit -s`.
