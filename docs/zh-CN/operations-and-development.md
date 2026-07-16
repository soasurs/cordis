# 配置、可观测性与开发

## 默认端口

| 服务 | 端口 |
| --- | --- |
| User | 3000 |
| Authenticator | 3001 |
| Message | 3002 |
| Presence | 3003 |
| Guild | 3005 |
| Session | 3006 |
| API | 8080 |
| Gateway | 8081 |

Dispatcher 没有监听端口。各服务配置位于 `services/<name>/v1/etc/config.yaml`，通过 `conf.LoadConfig(..., conf.UseEnv())` 加载并展开 `${CORDIS_*}`。

## 基础设施

- PostgreSQL：User、Authenticator、Guild、Message。
- Kafka：Guild 与 Message 事件，Dispatcher 必须配置 broker。
- Redis：Presence、Session 节点/owner/路由、Gateway 发现和 Dispatcher 路由。
- OpenTelemetry：RPC 服务可通过 `CORDIS_OTEL_ENDPOINT` 输出 trace。
- Prometheus：go-zero dev server 或 API 自有 observability 配置提供指标。

## 常用命令

```bash
make generate
make lint
make test
go build ./...
go vet ./...
```

单元测试使用 `testify/require`。SQL Store 测试以 `sqlmock` 校验查询。Redis 集成测试需要显式 integration tag 和 Redis 地址。当前仓库不要求 PostgreSQL 集成测试作为日常流程。

## 运行顺序

本地通常先启动 PostgreSQL、Redis、Kafka，再启动 User、Authenticator、Guild、Message、Presence、Session，随后启动 API、Gateway 和 Dispatcher。Session 的 `advertiseAddress` 必须是 Gateway 与 Dispatcher 可访问的地址。

## 生成与提交约定

`gen` 是生成产物，不手改。内部 Proto 变更必须同时生成代码。提交使用带 scope 的 Conventional Commit，并通过 `git commit -s` 添加 sign-off。
