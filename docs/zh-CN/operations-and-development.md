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
- etcd：Session 节点租约注册与发现，Gateway、Session、Dispatcher 必须配置 endpoint。
- Redis：Presence、Session owner 和用户/Guild/频道聚合路由。
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

本地通常先启动 PostgreSQL、Redis、etcd、Kafka，再启动 User、Authenticator、Guild、Message、Presence、Session，随后启动 API、Gateway 和 Dispatcher。Session 的 `advertiseAddress` 必须是 Gateway 与 Dispatcher 可访问的地址。

本地配置中的单地址只用于开发。生产环境应给 `sessionRegistry.hosts` 配置多个 etcd endpoint，并将 Redis 配置设为：

```yaml
redis:
  host: redis-0:6379,redis-1:6379,redis-2:6379
  type: cluster
```

Redis Cluster 的 pipeline 可以跨 slot 分发命令，但不保证跨 key 原子性。当前 owner 写入只操作单 key；聚合路由和 Presence 索引使用 TTL、generation 与读时校验容忍部分更新。

## 生成与提交约定

`gen` 是生成产物，不手改。内部 Proto 变更必须同时生成代码。提交使用带 scope 的 Conventional Commit，并通过 `git commit -s` 添加 sign-off。
