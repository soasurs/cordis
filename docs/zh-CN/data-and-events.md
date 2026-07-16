# 数据存储与事件

## PostgreSQL 所有权

- User：`users`、`user_profiles`。
- Authenticator：认证 `sessions`。
- Guild：Guild、成员、封禁、角色、成员角色、频道和权限覆盖。
- Message：消息、提及和附件序列化数据。旧的 reaction、emoji 和 Outbox 表已由最新迁移删除。

迁移以 SQL 文件嵌入服务二进制，通过 `pkg/migration.Apply` 按文件名字典序执行，并跳过 `*.down.sql`。当前表之间不依赖数据库外键，跨实体完整性主要由应用层检查。业务实体普遍以 `deleted_at = 0` 表示未软删除。

## Store 与事务

服务通过 Store 接口隔离业务和 SQL。SQL Store 同时保存数据库连接与 `sqlx.ExtContext` 执行器；进入 `Transact` 后执行器替换为 `*sqlx.Tx`。User、Guild 和 Message 在 error 或 panic 时回滚。依赖通过 `NewDependencies` 创建，测试通过 `NewServiceContextWithDependencies` 注入 fake。

## ID

主要实体使用 Snowflake ID。自定义 epoch 为 2025-01-01，节点号由非 loopback IP 哈希派生，分配 16 位 node 和 8 位 step。事件 JSON 中的 64 位 ID 通常编码为字符串，避免 JavaScript 数字精度损失。

## 事件 envelope

Kafka 事件统一为：

```json
{
  "t": "message.deleted",
  "d": {
    "id": "123",
    "channel_id": "456",
    "revision": 3,
    "deleted_at": 1784190002000
  }
}
```

事件名常量集中在 `pkg/realtime`，领域事件只使用点分层级，不新增下划线变体。现有事件包括 Guild、成员、角色、频道、权限覆盖、消息和反应事件。

## 直接发布 Kafka

Message 和 Guild 都不使用 Outbox。业务事务成功后，Message best-effort 发布到 `cordis.message.events.v1`，Guild best-effort 发布到 `cordis.guild.events.v1`。发布使用业务 ID 作为 Kafka key，以保持同一频道或 Guild 的分区顺序。未配置 Kafka 时不创建 producer；发布失败只记录日志，不改变已经成功的 RPC。数据库提交与 Kafka 发布之间没有原子性。
