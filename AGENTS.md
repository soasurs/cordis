# AGENTS.md

## Documentation

- This file contains repository-specific contribution constraints and execution guidance. Architecture, service behavior, domain rules, protocols, and operational design belong under `docs/`.
- Read the relevant design documents before changing behavior, and update them in the same change when implementation or architecture changes.
- Do not duplicate evolving design details here unless they are required as an implementation constraint.
- Documentation indexes:
  - [English](docs/en/README.md)
  - [简体中文](docs/zh-CN/README.md)

## Git

### Branches

- New working branches must use `<type>/<description>`.
- Use one of these branch types: `feat`, `fix`, `docs`, `refactor`, `test`, `perf`, `ci`, `build`, `chore`, `style`, or `revert`.
- Choose the type from the branch's primary intent:
  - `feat`: add or materially extend user-visible behavior.
  - `fix`: correct defective behavior.
  - `docs`: change documentation only.
  - `refactor`: restructure code without changing intended behavior.
  - `test`: add or correct tests without changing production behavior.
  - `perf`: improve performance without changing intended behavior.
  - `ci`: change CI configuration or automation.
  - `build`: change the build system, code generation, packaging, or dependencies.
  - `chore`: perform repository maintenance not covered by another type.
  - `style`: change formatting only, with no behavior change.
  - `revert`: revert an earlier change.
- Write `<description>` in concise lowercase kebab-case. Describe the outcome, not the implementation process.
- Do not prefix branches with tool names, agent names, usernames, or ownership markers such as `codex/`, `agent/`, or `user/`.
- Examples: `feat/api-guild-member-profiles`, `fix/session-resume-owner`, `test/guild-store-pagination`, `docs/git-conventions`.
- The default branch and explicitly requested release branches are exempt from this working-branch format.

### Commits

- Commits must use Conventional Commits with a required scope:

  ```text
  <type>(<scope>): <subject>
  ```

- Commit types use the same set and meanings as branch types. Choose each commit's type from that commit's contents; it does not have to match the branch type when a branch legitimately contains multiple kinds of commits.
- Use the smallest affected module, package, or subsystem as the scope, such as `api`, `guild`, `message`, `session`, or `proto`.
- Write the subject in the imperative present tense, start it with lowercase unless it begins with an identifier, and do not add a trailing period.
- Keep the subject concise. Add a body when the motivation, prior behavior, or impact is not obvious from the header. Explain why the change is needed rather than narrating the diff.
- Put issue references, deprecation notes, and `BREAKING CHANGE:` details in the footer when applicable.
- Revert commits must identify the reverted commit SHA and explain why the revert is needed.
- Commits in this repo must use `git commit -s` for sign-off. Do not add co-author trailers.
- If GPG signing fails during a commit, return the complete commit message to the user so they can commit it themselves; do not bypass or disable signing.

Examples:

```text
feat(api): embed profiles in guild member list
fix(session): reject stale resume owners
test(guild): cover member pagination cursor
docs(repo): document git conventions
```

These rules follow the type and header semantics of the [Angular commit message guidelines](https://github.com/angular/angular/blob/main/contributing-docs/commit-message-guidelines.md), extended with the repository's `chore`, `style`, and `revert` types and applied to branch naming.

## Commands

```bash
go fix ./...              # modernize Go syntax after each round of Go changes
go build ./...            # build all packages
go vet ./...              # report suspicious constructs and actionable warnings
go tool staticcheck -checks=inherit,-SA5008 ./... # run pinned analysis, excluding go-zero tag false positives
make generate             # buf generate for external and internal protos
make lint                 # buf lint
make test                 # go test ./...
make test-integration     # go test -tags=integration ./... (needs Docker; no pre-existing services)
make compose-up           # fixed-version local Postgres/Redis/Kafka/etcd for manual runs
make compose-down         # stop compose stack (named volumes kept; use `docker compose down -v` to wipe)

# Focused checks
go test ./services/gateway/v1/internal/server/... -v -count=1
go test ./services/session/v1/internal/server/... -v -count=1
go test ./services/dispatcher/v1/internal/server/... -v -count=1
go test ./services/message/v1/internal/server/... -v -count=1
go test ./services/message/v1/internal/store/... -v -count=1
go build ./services/message/v1/...
go test ./services/guild/v1/internal/server/... -v -count=1
go test ./services/guild/v1/internal/store/... -v -count=1
go build ./services/guild/v1/...
```

- Go module is `github.com/soasurs/cordis`; `go.mod` declares Go `1.26`.
- After each round of Go code changes, run `go fix ./...` before the relevant build and test commands so the verified code uses the latest supported syntax.
- Resolve actionable warnings reported by builds, tests, `go vet`, and configured editor or static-analysis tooling for the changed code before considering the work complete.
- `SA5008` is excluded because go-zero configuration extensions such as `default`, `optional`, `options`, and `range` in `json` struct tags are false positives. When changing configuration tags, verify that they use supported go-zero syntax instead of ignoring unrelated malformed tags.
- Code-generation and analysis tools are pinned by the `tool` block in `go.mod` and invoked through `go tool`; do not rely on separately installed copies.

## Generated Code And Protobuf

- Protos use edition 2023 and the opaque Go API. Use generated getters, setters, and builders instead of field access or generated message struct literals.
- Do not edit generated outputs under `gen/` directly.
- After editing any `.proto`, run `make generate` and `make lint`.
- `buf.gen.external.yaml` covers `proto/api`; `buf.gen.internal.yaml` covers the internal service protos.

## Implementation Constraints

### Resource Updates

- Resource `Update` RPCs use partial-update semantics. Only explicitly present fields may change; omitted fields preserve stored values.
- Use edition 2023 scalar presence methods such as `HasFoo` to distinguish omission from an explicit default value.
- Public API adapters must preserve field presence when forwarding to internal services.
- Store update parameters must remain presence-aware, and SQL must update only fields marked present.
- Reject an `Update` request with no mutable fields.
- A present collection-valued field replaces that collection unless dedicated add/remove operations are defined.

### Service Wiring

- Services other than Dispatcher use `Config -> NewDependencies(cfg) -> NewServiceContextWithDependencies(cfg, deps)`; Dispatcher constructs its Kafka consumer and route resolver directly.
- `NewDependencies` creates real dependencies. Tests inject fakes through `NewServiceContextWithDependencies` or direct `ServiceContext` values.
- `NewServiceContextWithDependencies` is fail-fast and panics on missing required dependencies.
- Config is loaded with `conf.LoadConfig(..., conf.UseEnv())`, so `${CORDIS_*}` values in YAML are environment-expanded.

### Storage And Errors

- Postgres services embed SQL migrations with `//go:embed *.sql`; `pkg/migration.Apply()` applies them lexicographically and skips `*.down.sql`.
- Stores define interfaces. SQL stores keep both `*sqlx.DB` and `sqlx.ExtContext`, replacing the executor with `*sqlx.Tx` inside transactions.
- Preserve rollback-on-error and rollback-on-panic behavior in transactional stores.
- Internal domain errors use `pkg/rpcerror.New(code, domain, reason, message)` with `google.rpc.ErrorInfo`.
- Public API adapters map internal errors through `apierror.FromRPC(err)`; do not expose unknown internal errors.
- Realtime domain event names are lowercase and dot-separated. Shared names belong in `pkg/realtime`.

## Tests

- Unit tests use `github.com/stretchr/testify/require`; follow the existing assertion style.
- Integration tests use the `integration` build tag and `make test-integration`. They require Docker but no pre-existing services because `internal/testkit` starts fixed-version dependencies.
- Keep validation, permission, and error-mapping branches in unit tests; cover store interfaces against real backends in integration tests; add cross-service composition coverage for interaction boundaries.
- Go `internal/` boundaries prevent a test package from importing multiple services' internal servers. Composition tests run the caller in-process and dependencies as real service binaries through `internal/testkit`.
