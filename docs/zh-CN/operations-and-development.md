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
- Authenticator 的 TOTP secret 使用独立的 `CORDIS_TOTP_ENCRYPTION_KEY` 以 AES-256-GCM 加密；该值是 Base64 编码的 32 字节随机密钥，不能与 JWT 密钥复用。
- Prometheus：go-zero dev server 或 API 自有 observability 配置提供指标。

## 常用命令

```bash
make generate
make lint
make test
go build ./...
go vet ./...
```

单元测试使用 `testify/require`。SQL Store 测试以 `sqlmock` 校验查询；日常开发不要求 Docker：

```bash
make test
```

真实依赖测试显式使用 `integration` tag。它通过 Testcontainers 启动并清理固定版本的 PostgreSQL、Redis、Kafka（KRaft）与 etcd，不依赖开发机上已有的服务：

```bash
make test-integration
```

集成测试会执行 User、Authenticator、Guild 与 Message 的 SQL migration/事务；验证 Guild、Message 的真实 Kafka 发布；覆盖 Presence 与 Session 的 Redis Store、Gateway 的 Redis + etcd 解析，以及 Kafka → Dispatcher → Redis 路由 → etcd Session 节点目录 → gRPC Session 投递链路。Kafka topic、consumer group 与 etcd prefix 均按测试运行随机命名，避免并行运行互相污染。跨服务组合测试以调用方进程内、被依赖方真实二进制的方式，验证 Message → Guild(+User) 的频道授权和 Authenticator → User 的注册/登录。

需要手动调试整套依赖时，使用仓库根目录的固定版本 Compose 编排：

```bash
make compose-up
# 按 README 的顺序运行 migration 和各服务
make compose-down
```

Compose 保存命名卷；如需删除本地开发数据，显式执行 `docker compose down -v`。

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
