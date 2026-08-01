# 服务端 Mention 解析与展开设计

> 状态：已实现。本文描述当前代码库行为，英文版见 [mention-expansion.md](../en/mention-expansion.md)。

## 1. 背景与目标

当前实现中，mention 由客户端解析 content 后通过 `mention_user_ids` 传给 Message 服务，服务端只做校验和存储：

- 没有内容 markup 协议约定；
- 只支持用户 mention，不支持角色和 `@everyone`；
- 消息对象（内部 proto 与 API proto）不携带 mention 字段，只有请求参数和事件里存在；
- `UpdateMessage` 可以把 mentions 作为独立字段修改，与 content 解耦。

本次重构对齐 Discord 的行为子集：

- 服务端从 content 解析 `<@user>`、`<@&role>`、`@everyone` 三类 mention（`@here`、频道 mention 及其余 markup 不做）；
- 消息对象与实时事件携带结构化 mentions；
- `@role`/`@everyone` 按“频道可见成员”展开并逐用户物化，未读 `mention_count` 包含展开结果；
- 编辑消息时 mentions 随 content 重建，不再独立修改；
- 新增大规模公会的异步展开路径，避免发消息被成员展开阻塞。

非目标（后续单独设计）：`allowed_mentions`、角色 `mentionable` 开关、`@here`、`<#channel>`、用户级抑制设置。

## 2. 内容协议

### 2.1 markup 格式

| 类型 | 格式 | 说明 |
|---|---|---|
| 用户 | `<@USER_ID>` | 兼容已弃用的 `<@!USER_ID>`，二者均归一为同一用户 mention |
| 角色 | `<@&ROLE_ID>` | 仅 Guild 文本频道有效 |
| everyone | `@everyone` | 完整单词匹配，大小写敏感，`@Everyone` 不解析 |

### 2.2 解析规则

- 出现在 content 任意位置；ID 必须是非负十进制整数，溢出或非法则保持原文不解析；
- 反斜杠转义：`\<@123>`、`\@everyone` 不解析（连续反斜杠按常规转义规则处理）；
- `@everyone` 要求单词边界：后一字符不得是字母、数字或下划线；
- 去重并统一按 user ID / role ID 升序输出（存储、响应与事件均不保留内容出现顺序）；
- 上限：user mention 与 role mention 合计不超过 `mentionsPerMessage`（默认 100），`@everyone` 不计入；
- DM 频道：只解析 `<@>`；`<@&>` 和 `@everyone` 保持文本，不触发权限校验、不存储；
- 无效实体（不存在的用户/角色）从解析结果中剔除，不报错（对齐 Discord）。

## 3. 权限

### 3.1 新增权限位

`proto/guild/v1/guild.proto` 新增：

```proto
GUILD_PERMISSION_MENTION_EVERYONE = 2048;
```

同步加入 `services/guild/v1/internal/server/permissions.go` 的 `AllGuildPermissions` 与 `AllChannelPermissions`，否则角色和频道 overwrite 无法授予或设置该位。`MENTION_EVERYONE` 是频道级权限，与 Discord 一致。

### 3.2 消息写入校验

Guild 文本频道中解析出 `@everyone` 时，使用现有 `AuthorizeGuildChannel` 返回的完整有效权限集检查 `MENTION_EVERYONE`；缺失则返回权限错误，整个请求失败。

- role mention：调用 `ListGuildRoles(guild_id, author_id)` 一次获取该 Guild 全部角色，过滤不属于该 Guild 或不存在的角色；本轮不校验 `mentionable`；
- user mention：Message 按每批最多 100 个 ID 调用 Guild 的 `FilterGuildChannelVisibleUsers`，只保留仍是该 Guild 活跃成员且能看到当前频道的用户；随后以相同的批大小调用 User 的 `BatchGetUserProfiles`，过滤不存在的用户；
- 作者自身是 Guild 成员由发送消息的既有授权流程保证。

## 4. 存储

### 4.1 迁移

新增 migration `services/message/v1/db/migrations/000011_add_mention_expansion.sql`：

```sql
ALTER TABLE message_mentions
    ADD COLUMN source SMALLINT NOT NULL DEFAULT 1
    CHECK (source IN (1, 2));

CREATE TABLE IF NOT EXISTS message_role_mentions (
    message_id  BIGINT NOT NULL,
    role_id     BIGINT NOT NULL CHECK (role_id > 0),
    PRIMARY KEY (message_id, role_id)
);

ALTER TABLE messages
    ADD COLUMN mention_everyone BOOLEAN NOT NULL DEFAULT FALSE;
```

`message_mentions.source` 语义：

- `1`：直接 `<@user>`，发消息/编辑事务内同步写入；
- `2`：`@role`/`@everyone` 展开物化，异步写入。

主键仍为 `(message_id, user_id)`：同一用户无论被几个来源提及，一条消息只占一行，现有未读计数 SQL 无需修改。

### 4.2 模型与 Store 接口

- `model.Message` 增加 `Mentions model.MessageMentions`，其中 `UserIDs []int64`、`RoleIDs []int64`、`Everyone bool`；
- Store 接口扩展：
  - `ReplaceMessageMentions(ctx, messageID, params)`：一次替换 users（source=1）、roles、everyone，事务内调用；
  - `ListMessageMentions(ctx, messageID)`：读取完整 mentions（user、role、everyone）；
  - `ListMessagesMentions(ctx, messageIDs)`：ListMessages 批量加载，避免 N+1；
  - `RebuildExpandedMessageMentions(ctx, messageID, expectedRevision, userIDs)`：在同一事务内以 revision 守卫原子替换 source=2 行；
- `messageRow` 增加 `mention_everyone` 列扫描。

## 5. 消息服务处理流程

### 5.1 CreateMessage

1. 现有校验（channel、author、content、attachments、flags、reply 引用）；
2. 解析 content 得到 mention 集合；
3. DM 裁剪：仅保留 user mention；
4. Guild 频道：`@everyone` 权限校验；角色存在性过滤；用户存在性过滤；
5. 上限校验（user + role 合计）；
  6. 幂等指纹（版本 2，见第 10 节）；
  7. 事务：创建消息、`ReplaceMessageMentions`（source=1 + roles + everyone）、作者 `AckMessage`；
  8. 提交后发布消息事件；展开 worker 消费事件完成 `@role`/`@everyone` 物化（第 7 节），消息服务不再发布独立任务。

### 5.2 UpdateMessage

- 删除请求中的 `MentionList` 字段，mentions 完全由 content 决定：
  - `HasContent()`：解析新 content，事务内先读旧完整 mentions（含已展开的 source=2 行）作为 previous，再删除 source=2 行、替换 source=1 行与 role/everyone 定义；提交后发布 `message.updated`（携带 previous 与当前集合），展开 worker 消费该事件按新 revision 重建；
  - 未改 content：mentions 与展开行均不变，不触发展开。
- previous 集合是事务开始时存储中可观测的完整集合；异步展开未完成时 previous 可能不完整，事件中的当前集合才是权威状态，previous 仅用于客户端清理本地高亮（best-effort）。

### 5.3 DeleteMessage

- 事务内删除消息并清理 `message_mentions`、`message_role_mentions` 行；
- 删除事件携带删除时可观测的完整 mention 集合；
- 尚未处理的展开事件因 revision/删除校验被丢弃（第 7 节）。

### 5.4 GetMessage / ListMessages

- `GetMessage` 加载单条完整 mentions；
- `ListMessages` 通过 `ListMessagesMentions` 批量加载；
- 内部 `Message` proto 与 API `Message` proto 均新增字段（第 11 节）。

## 6. 成员可见性展开（Guild 批量接口）

### 6.1 新 RPC

`proto/guild/v1/guild.proto` 新增内部批量接口：

```proto
rpc ListGuildMentionTargets(ListGuildMentionTargetsRequest)
    returns (ListGuildMentionTargetsResponse);

message ListGuildMentionTargetsRequest {
  int64 guild_id = 1;
  int64 actor_user_id = 2;
  int64 channel_id = 3;
  repeated int64 role_ids = 4;
  bool everyone = 5;
  string cursor = 6;
  int32 limit = 7;
}

message ListGuildMentionTargetsResponse {
  repeated int64 user_ids = 1;
  string next_cursor = 2;
}
```

### 6.2 语义

- 返回该频道中“可见”的活跃成员 user ID 集合，分页按 user ID 升序稳定排序，limit 默认 100、最大 1000；
- `everyone=true`：全部频道可见成员；
- `role_ids` 非空：拥有任一指定角色且频道可见的成员；与 `everyone` 同时提供时取并集；
- 结果去重；
- 调用方为 Message 服务，`actor_user_id` 传消息作者（保证是 Guild 成员）；
- 接口防御：校验 channel 属于该 Guild、role_ids 属于该 Guild。

### 6.3 可见性规则

批量接口的可见性判定必须与现有 `channelPermissions`（`services/guild/v1/internal/server/permissions.go`）一致：

1. Guild owner 与 `ADMINISTRATOR` 直接可见；
2. 基础权限 = 默认角色与已分配角色权限并集；
3. overwrite 按固定优先级：`@everyone` 角色 overwrite 最先应用，随后该成员所有已分配角色的 allow/deny 聚合应用（与角色顺序无关），最后应用成员 overwrite；
4. 最终有效权限含 `VIEW_CHANNEL` 才可见；
5. `deleted_at = 0` 的活跃成员才参与。

实现：Guild store 增加按 user ID 升序的成员/角色目标分页查询，server 层复用 `memberAuthorityFromRoles` 与 `channelPermissions` 逐窗口计算可见性，与单用户授权共用同一套规则；集成测试将“批量接口结果”与“对同一批成员逐个执行现有单用户权限计算”的结果做全量对照。

分页语义：每次返回一个候选窗口（窗口为 `limit + 1` 个候选，最多返回 `limit` 个可见成员），游标始终推进到窗口最后一个候选 user ID；窗口内没有可见成员时返回空页，调用方继续翻页直到 `next_cursor` 缺失。

### 6.4 Mention 候选搜索

- API 暴露 `SearchGuildMentionUsers` 和 `SearchGuildMentionRoles`。用户搜索请求带 `guild_id`、当前 `channel_id`、查询字符串和最多 20 条的 limit；客户端输入变化或继续输入时重新发起请求，不使用深分页游标。
- 用户候选不跨 User 服务做全局搜索。Guild 维护 `guild_member_profiles` 投影，包含 `guild_id`、`user_id`、`username`、Guild nickname、profile `name`、头像 ID 以及用于前缀索引的规范化字段。成员创建/加入时写入；`CreateGuild` 可能先写入占位行，再在事务提交后从 User best-effort 补齐；User 的 `user.profile.updated` 事件更新所有相关 Guild，服务启动时做有界重建以补齐历史成员。如果 User 在重建重试预算耗尽后仍不可用，projector 会继续消费事件，并在下一次启动时再次重建。
- 用户搜索匹配 username、Guild nickname 或 profile name 的前缀；username 匹配优先于 nickname/name 匹配，随后按规范化 username 和 user ID 排序。排序发生在频道可见性过滤之前，但服务端会继续读取内部候选窗口（默认 100）直到得到客户端要求的可见结果数或候选耗尽，因此对外 limit 始终是最终可见条数。
- 每个候选窗口都使用与频道授权相同的 `channelPermissions` 规则过滤；搜索调用者本身也必须能看到该频道。投影更新和权限变化之间存在正常的最终一致性窗口，Message 写入时仍通过批量可见性 RPC 做最终校验。
- 角色搜索在 Guild 本地按角色名做前缀匹配，排除特殊的 `@everyone` 角色；角色候选本身不按目标成员的频道可见性过滤，角色展开时再过滤目标成员。第一版使用 PostgreSQL 的规范化前缀匹配和 B-tree `text_pattern_ops` 索引，查询中的 `%`、`_` 和反斜杠会转义；contains/fuzzy 搜索和 `pg_trgm` 留作后续优化。

## 7. 异步展开

### 7.1 Topic 与消费组

不引入独立任务 topic：worker 直接消费现有消息事件 topic `cordis.message.events.v1`。事件 payload 已包含展开所需的全部字段（`message_id`、`channel_id`、`guild_id`、`revision`，以及本次新增的 `mention_role_ids`、`mention_everyone`），无需额外发布任务消息。

- 消费组：`cordis.message.mentions.v1`（沿用 `cordis.<consumer>.<source>.v1` 约定；消费者即 Message 服务自身的 mentions 展开功能）；
- Message 服务 `KafkaConfig` 增加 `MentionsConsumerGroup` 配置（默认值同上），不需要新增 topic 配置；
- worker 作为 Message 服务进程内的后台 goroutine 启动（参考 Dispatcher 的 `kgo.NewClient` + 手动 commit 模式），仅在 Kafka 配置存在时启用；多实例部署由消费组自动分摊，与 Dispatcher 消费组互不影响；
- 事件 key 沿用现有约定（Guild 消息为 `guild_id`），同一 Guild 的消息事件分区内有序，同一消息的 created/updated 事件顺序有保证。

### 7.2 Worker 处理

1. 按事件类型过滤，只处理 `message.created`，以及 mentions 已重建（`rebuild_mentions` 为 true）的 `message.updated`；payload 中 `mention_role_ids` 为空且 `mention_everyone` 为 false 时直接跳过（历史事件缺少这些字段，反序列化后为空，天然不会触发）；
2. 查询消息（id、channel_id、guild_id、revision、deleted_at）；消息不存在、已删除或 `revision` 与事件不一致时直接跳过并提交（防止旧事件覆盖新状态；`message.deleted` 事件无需处理，删除事务已清理展开行）；
3. 分页调用 `ListGuildMentionTargets` 拉取全部目标；
4. 以事件 revision 为守卫，在同一事务内删除旧 source=2 行并按批（建议 ≤ 10000）写入新集合，保证与并发编辑/删除互斥且结果收敛；
5. 全部成功才提交消费位点；失败按指数退避重试（100ms 起步、5s 封顶、最多 8 次），超过上限记录告警并提交跳过。

### 7.3 一致性窗口

消息事件先于展开完成发布：客户端先看到消息，服务端 `mention_count` 稍后（通常毫秒到秒级）就绪。READY / GetReadStates / AckMessage 返回的计数以存储中的物化行为准，天然最终一致。实时高亮由客户端在收到 guild 级消息事件时本地判断（客户端已知自身角色），服务端不按用户 fan-out 展开结果。

事件发布本身是 best-effort（现有语义）：事件丢失时展开同样不会发生，`mention_count` 相应缺失，与消息事件对客户端的影响同源，不引入新的可靠性要求。幂等重试只在真正创建/更新时发布事件，不会重复触发展开；消费组重放历史事件时，旧事件因缺少 mention 字段被跳过，新事件因 `ON CONFLICT DO NOTHING` 与 revision 校验安全重放。

## 8. mention_count 语义

- 计数 = 未读消息中“直接 user mention ∪ 展开物化”的 distinct 用户行数；
- 因主键为 `(message_id, user_id)`，同一消息多个来源只计一次；现有 `listReadyChannelReadStatesQuery` 的 `mention_counts` CTE 无需修改；
- `AckMessage` 推进水位后按新水位重算；
- 编辑后 source=2 行删除并异步重建，删除后行随消息清理；
- 角色成员变动不回溯历史消息（mention 在消息时刻按当时的成员快照物化，与 Discord 一致）。

## 9. 事件

`messagePayload` 新增：

```go
MentionRoleIDs          []string `json:"mention_role_ids"`
MentionEveryone         bool     `json:"mention_everyone"`
RebuildMentions         bool     `json:"rebuild_mentions,omitempty"`
PreviousMentionRoleIDs  []string `json:"previous_mention_role_ids,omitempty"`
PreviousMentionEveryone *bool    `json:"previous_mention_everyone,omitempty"`
```

保留现有 `MentionUserIDs` / `PreviousMentionUserIDs`。`RebuildMentions` 仅在 `message.updated` 且 content 变更（mentions 随 content 重建）时置位，flags/附件-only 更新不会触发展开。删除事件的 `messageDeletedPayload` 同样新增 role/everyone 字段。事件路由不变：Guild 消息单条 guild-keyed，DM 消息逐用户。

## 10. 幂等

- 指纹版本 bump 到 2，字段：channel、content、type、flags、references、attachment asset IDs、解析后的 user IDs、role IDs、everyone；
- 指纹必须包含解析结果而非只依赖 content：解析依赖用户/角色存在性等外部状态，重试时外部状态可能变化，只有显式包含解析结果才能保证“同指纹同结果”的幂等语义；
- 展开物化不进指纹（异步、可重放）；
- 幂等重试仅在 `createdNewMessage` 分支发布事件，重放不会重复展开。

## 11. proto / API 变更

### 11.1 内部 `proto/message/v1/message.proto`

- `CreateMessageRequest`：删除 `mention_user_ids`（字段号 9 `reserved`）；
- `UpdateMessageRequest`：删除 `mentions`（字段号 6 `reserved`）；删除 `MentionList` message；
- `Message` 新增：`mention_user_ids = 15`、`mention_role_ids = 16`、`mention_everyone = 17`。

### 11.2 公共 `proto/api/v1/message.proto`

- `CreateMessageRequest` 删除 `mention_user_ids`（字段号 8 `reserved`）；
- `UpdateMessageRequest` 删除 `mentions`（字段号 5 `reserved`）；删除 `MentionList`；
- `Message` 新增：`mention_user_ids`、`mention_role_ids`、`mention_everyone`；
- API adapter 在 `messageToAPI` 中透传 mention 字段。

返回结构使用 ID 列表而非 Discord 的 user 对象数组（客户端已有 profile 缓存；对象化留作后续）。这是破坏性协议变更，客户端需同步升级；不保留 deprecated 字段，避免“传了列表但被忽略”的双源歧义。

### 11.3 Guild Mention 搜索协议

- 公共 Guild API 新增 `SearchGuildMentionUsers(guild_id, channel_id, query, limit)` 和 `SearchGuildMentionRoles(guild_id, query, limit)`；用户响应包含 `user_id`、`username`、`nickname`、`name` 和头像 ID，角色响应复用 `GuildRole`。
- 内部 Guild API 新增对应的带 `actor_user_id` 用户/角色搜索 RPC，以及 `FilterGuildChannelVisibleUsers` 批量可见性 RPC。前者用于 API 搜索，后者由 Message 分批调用以过滤写入的直接 user mentions。

## 12. 迁移与兼容

- 存量 `message_mentions` 数据保留，迁移后均视为 `source=1`；
- 存量消息 content 无 markup：查询按存储值返回，不回填、不改 content；
- `mention_everyone` 默认 false、`message_role_mentions` 为空，历史消息不产生角色/everyone mention；
- 文档注明历史消息的 mentions 与 content 可能不一致，属于已知遗留。

## 13. 性能与容量

- 同步路径：仅写入直接 user mention（≤100 行）与角色/everyone 定义行，延迟与现状相当；
- 异步路径：`@everyone` 10 万成员的频道约需 100 次批量 RPC（页大小 1000）与 10 次万行级 `unnest` 批量插入，后台秒级完成；
- `message_mentions` 行数随 @everyone 消息增长（每条消息 × 可见成员数），查询走现有 `(user_id, message_id DESC)` 索引，用户视角每消息一行，可接受；
- 后续可优化：Guild 侧把“目标集合计算”下沉为流式 SQL 游标，减少传输与分页次数。

## 14. 测试计划

- 解析器单元测试：markup 变体、`<@!>` 归一、转义、词边界、大小写、非法/溢出 ID、去重与顺序、DM 裁剪、上限；
- 权限测试：`@everyone` 无 `MENTION_EVERYONE` 拒绝、角色/用户存在性过滤、用户频道可见性过滤；
- 搜索测试：username/nickname/name 前缀匹配、排序后过滤仍补足可见 limit、角色搜索和 projection 更新；
- Store 集成测试：source 列迁移、role/everyone 读写、批量加载、编辑删除 source=2、未读计数不受影响；
- Guild 批量接口集成测试：批量结果与逐人单用户权限计算全量对照；
- Worker 测试：事件类型过滤、无展开需求跳过、分页拉取、revision 守卫下的原子重建、过期事件丢弃、消息删除跳过、失败重试；
- 服务端测试：Create/Update/Delete/Get/List 的 mentions 行为、编辑重建与 previous、事件 payload、幂等指纹 v2；
- API 测试：字段透传、请求字段移除；
- 文档同步：`services.md`、`protocols-and-errors.md`、`data-and-events.md`（en + zh-CN）。

## 15. 实施步骤

以上步骤已全部完成并随 PR 链落地：

1. Guild：`MENTION_EVERYONE` 权限位 + `ListGuildMentionTargets` 批量接口（含可见性对照测试）；
2. Message：migration、model/Store、mention 解析器（`services/message/v1/internal/mention`）；
3. Message：Create/Update/Delete/Get/List、事件、幂等指纹 v2；
4. Message：展开 worker（消费 `cordis.message.events.v1`、分批 upsert、重试）；
5. Guild：`guild_member_profiles` 投影、User profile 事件同步、username/nickname/name 前缀搜索和角色搜索；
6. proto 与 API adapter；
7. 全量测试与文档同步。

## 16. 已确认决策

- 权限位取值 `2048`（当前枚举最大 1024）；
- 破坏性协议变更直接移除请求字段（`reserved`），不做 deprecated 兼容层；
- 展开 worker 直接消费消息事件 topic，不引入独立任务 topic；
- 第一版角色 mention 不做 `mentionable` 校验；
- 第一版搜索只做 username/nickname/name/role name 前缀匹配，不引入 `pg_trgm`。
