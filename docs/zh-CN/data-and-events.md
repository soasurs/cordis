# 数据存储与事件

## PostgreSQL 所有权

- User：`users`、`user_profiles`。
- Authenticator：认证 `sessions`。
- Guild：Guild、成员、封禁、角色、成员角色、频道和权限覆盖。
- Message：消息、提及和附件序列化数据。旧的 reaction、emoji 和 Outbox 表已由最新迁移删除。

迁移以 SQL 文件嵌入服务二进制，通过 `pkg/migration.Apply` 按文件名字典序执行，并跳过 `*.down.sql`。当前表之间不依赖数据库外键，跨实体完整性主要由应用层检查。业务实体普遍以 `deleted_at = 0` 表示未软删除。

## Store 与事务

服务通过 Store 接口隔离业务和 SQL。SQL Store 同时保存数据库连接与 `sqlx.ExtContext` 执行器；进入 `Transact` 后执行器替换为 `*sqlx.Tx`。Postgres 连接由 `pkg/database.NewPostgres` 创建，并通过 otelsql 自动产生 SQL tracing。User、Guild 和 Message 在 error 或 panic 时回滚。依赖通过 `NewDependencies` 创建，测试通过 `NewServiceContextWithDependencies` 注入 fake。

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

事件名常量集中在 `pkg/realtime`，领域事件只使用点分层级，不新增下划线变体。现有事件包括 Guild、成员、角色、频道、权限覆盖、消息、关系、用户资料和 Presence 事件。
Guild 频道列表响应携带 Guild 级 `channel_layout_revision`。创建、删除、parent
移动和 reorder 事件除了频道自身的 `revision` 外，还携带提交后的 layout
revision；过期的结构变更请求会被拒绝，不会自动重放。

消息 created/updated 事件携带解析后的 mention 集合（`mention_user_ids`、
`mention_role_ids`、`mention_everyone`），更新事件还携带 best-effort 的
previous 集合。Message 服务的 mention 展开消费组（`cordis.message.mentions.v1`）
消费同一事件 topic，角色和 `@everyone` 目标与推送给客户端的事件来自同一条
best-effort 数据流；详见 [mention-expansion.md](mention-expansion.md)。

## 直接发布 Kafka

User、Message、Guild 和 Presence 都不使用 Outbox。业务事务成功后，User 将关系和资料事件 best-effort 发布到 `cordis.user.events.v1`，Message 发布到 `cordis.message.events.v1`，Guild 发布到 `cordis.guild.events.v1`，Presence 将公开状态变化和私有偏好变化 best-effort 发布到 `cordis.presence.events.v1`。Presence 在发布前持久化对应的版本化状态，并把同一个 version 用作事件幂等键。发布使用领域聚合 ID 作为 Kafka key：Message 的频道路由事件使用 `channel_id`，用户路由事件使用目标 `user_id`，从而保持同一用户、频道或 Guild 的分区顺序。未配置 Kafka 时不创建 producer；发布失败只记录日志，不改变已经成功的 RPC。数据库提交与 Kafka 发布之间没有原子性。

Guild 的 `guild_member_profiles` 是本地搜索投影，不是 User profile 的权威数据。
它为活跃成员保存 username、Guild nickname、profile name 和头像信息；成员移除时，
投影行会在同一事务中删除。User profile 事件会 best-effort 更新相关行，Guild 启动时
的重建流程可以重新填充投影。`CreateGuild` 会先提交 profile 占位行，再 best-effort
从 User 补齐资料，因此 User 暂时不可用不会导致 Guild 创建失败。

`CreateMessage` 的可选请求幂等记录会与消息、mentions 和作者 read state
一起提交。认证、授权和请求校验正常通过时，相同 key 的重试会返回已有消息，
不再发布创建或 read-state 事件；这些检查仍可能返回原有错误，但不会创建新消息。
这不会消除数据库提交成功后、best-effort Kafka 发布前的崩溃窗口。

Guild 创建类 RPC（`CreateGuild`、`CreateGuildRole`、`CreateGuildChannel`、
`CreateGuildInvite`）的幂等记录与资源写入在同一事务内提交。相同 key 的重试
返回第一次创建的资源，不重复发布创建、频道位移或 overwrite 事件。同样存在
best-effort 发布窗口。

上传创建类 RPC（`CreateAvatarUpload`、`CreateGuildIconUpload`、
`CreateAttachmentUpload`）的幂等记录与 asset 写入在同一事务内提交。相同
key 的重试返回同一个上传，不会再次创建 asset 或消耗配额；Media 自身不发布
创建事件，因此上传幂等不涉及事件抑制。响应会暴露 Media 的状态快照以及
是否重放已有幂等记录；终态 asset 在幂等记录保留期内仍绑定旧 key。
