# 服务目录

## API

公开 Connect-RPC HTTP 服务，监听 `:8080`。它代理 Authenticator、User、Guild、Message 和 Presence RPC，将内部 protobuf 转换为公开模型，并通过 `pkg/apierror` 将带 `google.rpc.ErrorInfo` 的内部错误转换为稳定的公开错误。API 本身不访问业务数据库。

公开资源模型会嵌入客户端渲染所需的当前 User profile。API 在组装 Guild 成员、封禁、邀请、关系、消息和 DM 频道时批量加载并校验这些 profile；内部领域模型仍只保存稳定的用户 ID。

公开请求使用 Redis-backed 命名限流 policy，并在 Redis 故障时使用有界本地 fallback。IP 桶按 IPv4 `/32` 或 IPv6 `/64` 归一化，IPv4 阈值会为 CGNAT 放宽。所有请求先消费来源 IP guard；认证成功后再消费用户通用配额，消息创建、关系写入、Guild 资源创建和邀请加入还会消费对应业务桶。认证后的 `GetReadStates` reconcile 还使用进程内 keyed limiter，限制同一用户的并发请求数。

`ResolveUsersPresence` 最多接受 100 个去重后的用户 ID，只返回调用者本人、好友和有有效共同 Guild 的用户。共同 Guild 会独立授予 Presence 可见性，即使任一方 block 对方也仍然可见；block 只取消关系或 DM 可见路径。invisible 或未知聚合状态统一公开为 offline，无关用户直接省略；公开模型仅包含 `user_id`、`status`、`last_seen_at` 和 `version`。

API 入站链在业务限流之前施加服务端 deadline、全局请求并发帽和 CPU 自适应 shedding，并按公开 RPC procedure 使用熔断器隔离持续的服务端失败。只有 `Unknown`、`DeadlineExceeded`、`Internal`、`Unavailable` 和 `DataLoss` 会使熔断器记为失败；校验、鉴权和限流错误不会打开线路。HTTP 总请求体与 Connect 解压后的单消息分别限长，panic 会记录 procedure 和 stack 后转换为不泄露内部信息的 `Internal` 错误。超时请求只有在底层 handler 实际退出后才释放并发槽，避免不响应取消的工作绕过全局并发帽。

## User

监听 `:3000`，拥有用户和资料数据。负责注册所需的用户创建、用户查询、邮箱可用性、用户名可用性、邮箱更新、资料更新、密码校验和修改。密码使用 Argon2id 哈希。User 不签发令牌。

Relationship HTTP 响应嵌入目标用户 profile。`relationship.updated` 事件在关系事务前从 User store 加载目标 profile，使提交后的更新事件可以独立渲染。关系列表使用 opaque `cursor` / `next_cursor` 分页（按 `created_at`、`target_id` 降序；没有下一页时省略 `next_cursor`）。可选的 `type` 过滤属于 cursor 作用域，翻页时必须保持不变。

名称、用户名、简介或头像修改成功后，User 发布携带完整资料快照的 `user.profile.updated`。Dispatcher 将事件投递给用户本人的全部客户端、共同 Guild 成员、非 block 的关系对端以及已有 DM 对端；同一接收者通过多个受众路径命中时由 Session 去重。

`UpdateUserProfile` 对 `name`、`bio` 和 `avatar_asset_id` 采用 presence-aware 部分更新。头像二进制仍走 `CreateAvatarUpload` → 直传 PUT，随后可通过 `CompleteAvatarUpload` 或带 `avatar_asset_id` 的 `UpdateUserProfile` 挂载。保存前可用本地 blob URL 预览。显式传入 `avatar_asset_id = 0` 清除头像；被替换的旧 asset 交由 Media lifecycle 回收。`GetAvatarUploadConstraints` 经 User 读取 Media 的 `userAvatar` 图片限制档，返回最大文件大小、最大宽高、最大像素数和允许 MIME 类型。Media 为头像和公会图标持有强制执行的 `imageConstraints`；消息附件仍按不透明二进制处理，独立的 `attachmentImageInspection` 只限制尽力而为的尺寸与 blurhash 提取。头像创建与完成失败映射为稳定公开 code：`profile.avatar_file_too_large`、`profile.avatar_content_type_invalid`、`profile.avatar_dimensions_exceeded`、`profile.avatar_pixels_exceeded`。`CheckUsernameAvailability` 复用与 `UpdateUsername` 相同的规范化与校验；已占用返回 `available=false`，最终改名仍以数据库唯一约束为准。

## Authenticator

监听 `:3001`，负责注册编排、登录、访问令牌与刷新令牌、令牌校验以及登录 Session 管理。用户身份由 User 提供；密码凭据和认证 Session 存储在 PostgreSQL。access token 默认有效 15 分钟；认证 Session 的空闲期限为 30 天，绝对期限为 180 天。refresh token 每次使用都会轮换，紧邻的 previous token 有 30 秒幂等恢复窗口。API 为原生客户端保留显式 Bearer transport，并为浏览器提供 HttpOnly Cookie 和服务端透明刷新。Authenticator 还在 Redis 中保存 30 秒单次使用的 Gateway ticket。完整客户端约定见[认证与令牌轮换](authentication.md)。真实启动需要访问令牌和刷新令牌密钥环境变量。

注册支持 `open`、`invite_only` 和 `closed` 三种模式。邀请制使用由 Authenticator
保存的一次性邀请码，也可以将邀请码绑定到指定邮箱。邀请码会在 Argon2 和 User RPC
之前被短暂预留，并与密码凭据及邮箱验证 token 在同一事务中核销。注册会发送验证邮件，
但不会创建 Session；只有密码正确且当前邮箱已经验证时，登录才会创建 Session。未知账号、
密码错误和邮箱未验证对外统一返回凭据无效。重新发送验证邮件无需登录，语法有效的邮箱请求
始终返回成功。密码重置只适用于已经拥有 credential 的账号；未完成的注册必须通过
`Register` 继续。

所有 Argon2 哈希和校验都受进程内 weighted semaphore 保护。容量由 `password.maxConcurrency` 配置（默认 4），当前每项 Argon2 工作使用一个权重，因此等同于每个 Authenticator 实例固定数量的并发 slot，而不是全集群共享上限。slot 满时请求等待，context 超时或取消时退出等待；semaphore 本身不提供独立的有界请求队列，外层 API rate limiter 负责限制进入量。

## Guild

监听 `:3005`，拥有 Guild、成员、封禁、角色、成员角色、频道和频道权限覆盖。它实现：

- Guild 创建、查询、修改、删除、离开和所有权转移；
- 成员加入、修改、踢出、封禁、解封和封禁列表；
- 角色 CRUD、成员角色、按角色列出成员和显式排序；
- 文本、分类、语音频道的元数据与排序；
- 角色/成员频道权限覆盖和频道授权检查。
- Mention 用户和角色候选搜索。用户搜索在 Guild 本地的成员 profile 投影上执行 username/nickname/name 前缀匹配，并按当前频道可见性过滤；角色搜索同样在 Guild 本地执行，角色目标成员的可见性留给 Message 展开时处理。

Guild 元数据包含最多 1024 个 Unicode 字符的可选描述。名称和描述通过具备字段 presence 语义的 `UpdateGuild` 修改，显式传入空描述会清除它；图标使用独立的直传流程，仅在 `CompleteGuildIconUpload` 成功时才与 Guild 关联。

Guild 通过 `guild_member_profiles` 维护用于 Mention 搜索的 User profile 投影。User 的完整 profile 仍由 User 服务拥有；Guild 在成员创建/加入时写入投影，`CreateGuild` 可能先写入占位行并在 Guild 事务提交后从 User best-effort 补齐，消费 `user.profile.updated` 更新投影，并在启动时重建历史成员的投影。用户搜索只做规范化 username/nickname/name 前缀匹配，公开 limit 是经过频道可见性过滤后的最终结果数。

该投影 worker 使用 `cordis.guild.user.profiles.v1` 消费组消费
`cordis.user.events.v1`，并复用共享的 partition consumer runtime：每个已分配
partition 一个串行 worker 和有界队列，retry 与 offset 提交状态按 partition
隔离。存储临时失败只阻塞当前 partition；保留现有 retry 次数，耗尽后记录日志、
提交并丢弃该事件。
优雅关闭预算由 `shutdownTimeout` 配置；go-zero 的强制退出时间会在该预算上
加上默认的一秒 wrap-up 阶段，为 worker 停止和最终 offset 提交预留有界时间。

权限使用 `uint64` 位集。Guild owner 和 `ADMINISTRATOR` 获得完整权限；频道权限在 Guild 权限上依次应用默认角色、成员角色以及成员覆盖。失去 `VIEW_CHANNEL` 时相关发送权限也被移除。创建频道时会写入一条空的 `@everyone` overwrite（`applies_to=ROLE`，`applies_to_id=guild_id`，allow/deny 为 0），客户端无需自行补全；该 overwrite 与默认角色均不可删除。Guild 事件直接发布到独立 topic `cordis.guild.events.v1`。

频道创建、删除、parent 移动和 reorder 使用 Guild 单调递增的
`channel_layout_revision`。结构变更事务会先获取 Guild 频道 advisory lock，再拒绝
过期 revision；冲突时不写入，成功的一个逻辑变更只递增一次。结构变更相关的频道
事件携带提交后的 layout revision。

按角色列出成员与 Guild 成员列表使用相同的 opaque `cursor` / `next_cursor` 分页（按 `joined_at`、`user_id` 降序；没有下一页时省略 `next_cursor`）。普通角色返回显式分配且有效的成员，默认角色返回全部有效 Guild 成员。同一角色可对最多 100 个成员批量使用 `AddGuildRoleMembers` / `RemoveGuildRoleMembers`；单成员 RPC 仍保留。公开 Guild member 响应始终嵌入成员 profile；封禁响应同时嵌入被封用户和操作者 profile，邀请响应嵌入创建者 profile。成员与封禁事件不经过 API，因此 Guild 自行加载事件需要的 profile。

持久化 Guild 资源使用配置化硬上限。默认每用户最多拥有 10 个、加入 100 个 Guild；每 Guild 最多 250 个角色、500 个频道和 100 个有效邀请；每频道最多 100 条权限覆盖。配额检查与资源写入在同一 PostgreSQL 事务内串行执行。

`CreateGuild`、`CreateGuildRole`、`CreateGuildChannel` 和 `CreateGuildInvite`
支持可选的 opaque `idempotency_key`，用于标识一次客户端创建意图。key 的作用域
是认证 actor 和各自 RPC 的 operation（`guild.create`、`guild.role.create`、
`guild.channel.create`、`guild.invite.create`），默认保留 24 小时。相同请求
指纹重用 key 时返回第一次创建的资源且不重复任何写入：`CreateGuild` 不会重建
默认角色、频道和 overwrites；`CreateGuildRole` 不会再次占用 position；
`CreateGuildChannel` 不会再次移动其它频道或重复发布频道/overwrite 事件；
`CreateGuildInvite` 返回第一次生成的邀请码和首次计算出的过期时间。不同参数
重用 key 时返回 `request.idempotency_key_reused`。未携带 key 的请求保持原有
行为，不同 actor 之间 key 作用域互不影响。幂等记录与资源写入在同一事务内
完成，每个 operation 的保留时间独立配置。

`CreateAvatarUpload`、`CreateGuildIconUpload` 和 `CreateAttachmentUpload`
支持同样的可选 key，由 API 经所属域服务透传到 Media。key 按 kind 隔离
（`media.create.user_avatar`、`media.create.guild_icon`、
`media.create.message_attachment`），默认保留 24 小时且不低于 upload
session TTL。重试返回同一个 upload ID；asset 仍为 `CREATED` 时还会为同一
object key 重新签发 presigned PUT URL；重试不会再次创建 asset 或消耗上传
配额。响应还会返回上传状态快照以及是否重放了已有幂等记录。完整状态和
恢复语义见协议文档。

内部 `GetUserReadyState` 在一次调用中按用户的有效 Guild 成员关系返回完整 READY 数据，包括 Guild、全部角色、当前成员的显式角色 ID、可见频道、这些频道的 permission overwrites，以及与频道列表对应的 `channel_layout_revision`；Session 会将该字段转发到公开 `ready` event。每份快照携带持久化的 `access_revision`；当成员关系、角色权限或分配、频道、权限覆盖、所有权或 Guild 删除可能改变访问权限时，PostgreSQL 触发器会推进这个单调递增版本。只要 Guild 仍存在，发布的 Guild 事件会携带事务提交后的版本。

## Message

监听 `:3002`，拥有消息、附件、提及和回复关系。创建、读取、更新和删除操作先调用 Guild 授权。列表使用 `before`、`after` 或 `around` 消息 ID 游标分页。DM 频道列表使用 opaque `cursor` / `next_cursor`（按 channel id 降序）。当前没有反应或自定义 emoji RPC。

内部消息对象只携带 `author_id`，不嵌入 User profile。API 在组装公开
`ListMessages` 响应时批量加载去重后的作者资料。单条消息 RPC 将 profile 作为响应附加数据返回，
使创建和更新路径可以复用为实时事件加载的资料。事件不经过 API，因此事件需要的 profile
仍由 Message 服务自行查询。

消息创建和更新默认最多携带 10 个附件和 100 个不重复的用户 + 角色 mention；两项上限均由 Message 服务配置。mention 由服务端从正文解析，使用 `<@user>`、`<@&role>` 和 `@everyone` markup，客户端不再提交 mention ID 列表。role mention 和 `@everyone` 都需要 Guild 的 `MENTION_EVERYONE` 频道权限，角色必须属于该 Guild，用户必须存在；未知实体从解析结果中剔除。Guild 中的直接 user mention 在写入时还会通过 Guild 的批量频道可见性检查，因此不可见频道的用户不会被持久化为 mention。DM 频道只支持用户 mention。角色/everyone 的展开目标由消费消息事件 topic 的后台 worker 异步物化，未读 `mention_count` 包含展开结果。完整设计见 [mention-expansion.md](mention-expansion.md)。图片附件的 `blurhash` 由 Media 在 CompleteUpload 时生成，Message 写入附件元数据一并返回与广播；非图片或未能生成时为空。客户端在 Create/Update 上携带的 `blurhash` 会被忽略并以 Media 元数据覆盖。

内部 READY RPC 一次加载用户的全部 DM，并针对 Session 提供的可见 Guild 文本频道计算 read state。每项包含 `channel_id`、`last_message_id`、`last_read_message_id` 和未读提及数；客户端用 `last_message_id > last_read_message_id` 判断是否未读，不再计算具体未读消息数。`AckMessage` 只有在 watermark 实际前进时才发布 user-routed `message.read.updated`，CreateMessage 也会在写事务内从数据库读回作者的最终 read state。

公开 DM channel 响应嵌入对端用户 profile。Message 为 `dm.channel.created` 事件自行加载对端 profile；Session 在组装 `ready` 事件时独立批量加载 DM 对端 profile。

保留认证后的 HTTP `GetReadStates` 作为 reconcile 路径，但不再接收 `channel_id` 列表。客户端只能按一个 `guild_id` 或全部 DM 两种 scope 同步；Guild scope 由服务端授权结果产生可见文本频道，DM scope 同时返回完整 DM 列表，以修复漏掉的创建事件。API 按用户限制并发，Message 端再用进程内容量限制聚合查询负载。服务端产生的超大 scope 会按 capacity 拆成多个数据库批次，每批获取与频道数完全一致的 weight，不会把一条超大查询 clamp 成 capacity 后直接执行。

允许客户端创建的消息类型仅为 `DEFAULT` 和 `REPLY`；`THREAD_STARTER` 保留给未来 Thread 功能。客户端可设置的 flag 目前只有 `SUPPRESS_NOTIFICATIONS`。写事务提交后，服务 best-effort 直接向 `cordis.message.events.v1` 发布事件；发布失败只记录日志。
写事务会在同一事务内插入 outbox 行，由独立 relay 发布到 `cordis.message.events.v1`。`message.created`、`message.updated`、`message.deleted` 和 `dm.channel.created` 使用 `channel_id` 作为 Kafka key；`message.read.updated` 使用独立的 outbox，stream 和 Kafka key 均为 `user_id:channel_id`。

消息事件携带 `mention_user_ids`、`mention_role_ids` 和 `mention_everyone`；更新事件还会携带 best-effort 的 previous mention 集合，供客户端清理本地高亮。Message 服务内运行一个后台展开消费组（`cordis.message.mentions.v1`）消费同一事件 topic：只处理包含角色或 everyone mention 的 created 事件和 mentions 已重建的 updated 事件，校验存储中的 revision，通过 Guild 分页拉取频道可见成员，并在 revision 守卫下原子重建展开行。展开是最终一致的：消息可能先于其 `mention_count` 贡献可见；best-effort 事件丢失时展开同样丢失。
该 worker 复用共享 partition consumer runtime，每个 partition 保持一个串行
worker、独立有界队列、retry 和 offset 状态；某个 partition 的 retry 不会阻塞
其他 partition。保留现有 retry 次数，耗尽后记录日志、提交并丢弃事件。
优雅关闭预算由 `shutdownTimeout` 配置；go-zero 的强制退出时间会在该预算上
加上默认的一秒 wrap-up 阶段，为 worker 停止和最终 offset 提交预留有界时间。

`CreateMessage` 支持可选的 opaque `idempotency_key`，用于标识一次客户端创建意图。key 的作用域是认证用户和 `message.create` 操作，默认保留 30 分钟；不得为空、首尾不得有空白，长度最多为 255 个 UTF-8 字节。请求指纹（版本 2）包含 channel、正文、规范化后的 type、flags、引用消息、附件 asset ID 及其顺序，以及解析后的 mention 集合（用户 ID、角色 ID 和 everyone 标记）。相同指纹重用 key 时返回第一次创建的消息，不再次写入 mentions、推进 read state 或发布创建事件；不同参数重用 key 时返回 `request.idempotency_key_reused`。未携带 key 的请求保持原有行为。重试仍会执行正常的认证、授权和请求校验；这些检查或其依赖失败时，重试可以返回对应错误，但不会创建新消息。幂等记录与消息侧写入在同一事务内完成，但现有提交后 best-effort 发布 Kafka 的窗口仍然存在。TTL 按 operation 独立配置，其他创建类 RPC 可以使用不同的保留时间。
`CreateMessage` 不再推进作者 read state；客户端发送成功后本地标记已读，再延迟调用 `AckMessage`。相同指纹重用 key 时返回第一次创建的消息，不再次写入 mentions，也不插入新的 outbox 行；不同参数重用 key 时返回 `request.idempotency_key_reused`。未携带 key 的请求保持原有行为。重试仍会执行正常的认证、授权和请求校验；这些检查或其依赖失败时，重试可以返回对应错误，但不会创建新消息。幂等记录与消息侧写入在同一事务内完成。TTL 按 operation 独立配置，其他创建类 RPC 可以使用不同的保留时间。

## Gateway

监听 `:8081`，在根路径 `/` 提供 WebSocket；运维探针由单独的 probe server 提供。连接后发送 `hello`，首个客户端消息必须是 `identify` 或 `resume`。原生客户端在 JSON `token` 中发送 access token；浏览器发送 API 签发的单次 `gateway_ticket`，两个字段必须且只能出现一个。Gateway 从 etcd 发现 Session 节点；Resume owner 仍从 Redis 读取。建立 `SessionService.Connect` 双向 gRPC 流后，它只负责 WebSocket 与 gRPC 消息互转，不保存逻辑路由状态，也不消费 Kafka。WebSocket 握手会按照配置的 `originPatterns` 校验跨来源请求，生产环境应配置前端页面的 Origin。

接受 WebSocket 前，Gateway 会按可信代理解析出的 IPv4 `/32` 或 IPv6 `/64` 来源作用域限速。连接容量完全由进程本地维护：每实例默认最多 50000 条连接和 5000 条 pending handshake，IPv4 与 IPv6 每来源 pending 上限分别为 100 和 20；Session 接受 IDENTIFY 或 RESUME 后立即释放 pending 槽。每条连接默认每分钟最多发送 120 个 Gateway event。`IDENTIFY` 还会按来源作用域限速；`RESUME` 同时按来源作用域和逻辑 Session ID 限速，只有这些离散限流事件使用 Redis。

物理连接活性由 Gateway 本地管理。Gateway 校验 `heartbeat` 的 sequence 并直接返回 `heartbeat.ack`，连续两个约定周期未收到 heartbeat 时关闭连接；比约定周期提前超过 10% 的 heartbeat 会被拒绝，也不会延长活性 deadline。只有确认 sequence 前进时才记录 dirty checkpoint；默认每 5 秒按目标 Session 节点归并，并以每批最多 500 条同步。Session binding epoch 用来拒绝连接被替换后迟到的 checkpoint。

## Session

监听 `:3006`，是实时系统的有状态核心。它负责：

- 校验 IDENTIFY/RESUME 的 access token，或通过 Authenticator 原子兑换浏览器 Gateway ticket；
- 创建逻辑 Session，并加载用户完整的 READY Guild 与 read-state 快照；
- 保存用户和 Guild 的本地反向索引；
- 分配递增 sequence，保存最多 2048 条内存回放记录；
- 应用 Gateway 批量同步的 heartbeat ACK checkpoint、处理 Presence 更新、detach 和 resume；
- 接收 Dispatcher 的 Guild、频道和用户事件并本地 fanout。

IDENTIFY 分别向 Guild 和 Message 拉取完整 READY，再从 User 批量加载 DM 对端 profile，并从 Presence 加载本人和去重后的 DM 对端版本化快照：Guild、角色、成员角色、可见频道及其 permission overwrites、全部 DM 和四字段 read state。READY 不加载全部 Guild 成员的 Presence。READY 组装期间收到的实时事件先缓冲，READY 以 sequence 1 发出后再按接收顺序入队。pending dispatch 同时受事件条数和事件数据总字节数限制，有效条数还会低于 replay 与 binding queue 容量；溢出时清空 pending buffer 并让本次 IDENTIFY 失败，使客户端重连后重新获取权威快照。默认加载上限为每用户 100 个 Guild、每 Guild 500 个可见频道。同一节点上属于同一用户的逻辑 Session 共享授权快照，最后一个本地 Session 移除后释放。Guild access 事件先记录频道的原可见用户，再按 revision 使受影响快照失效并以受控并发重建；事件会投递给原可见与当前可见用户的并集，使新授权客户端添加频道、被撤权客户端移除频道。按用户和 Guild 的重建使用 singleflight 合并，单节点默认最多并发 16 次且每次最多等待 2 秒。缺失、格式错误、超限、版本过旧或已标记失效的快照不能用于授权。重建失败时会跳过敏感事件，并为当前失效代发送一次带 sequence 的 `session.reconcile`。

Access token 校验通过后，`IDENTIFY` 会分别按用户 ID 和认证 Session ID 限速。同一个认证 Session 可以为多个浏览器页面或设备创建并存的逻辑 Session；每个逻辑 Session 拥有独立的 Session ID、回放窗口、Presence 租约和 transport binding。

客户端 heartbeat 不再直接触发 Session 的 Redis owner 或 Presence 续租；逻辑 Session owner 以 resume timeout 为 TTL，通过有界 Redis pipeline 批量续租，Presence 通过批量 RPC 续租。维护周期为 resume timeout 的四分之一，并加入 ±20% cycle jitter 以打散不同 Session 节点；每批 500 个 Session，并分配到最长 5 秒刷新窗口内带 jitter 的 slot。聚合 route 使用单独循环续租，不受 lease sweep 耗时影响。

Dispatcher 通过聚合 Guild route 定位 Guild 消息的候选 Session 节点，并通过专用 Guild-message RPC 携带 Guild 与频道 ID。Session 按本地用户检查服务端可见性快照，将消息投递给该用户的所有本地逻辑 Session。DM 消息为每个参与者各发布一条记录，并通过聚合 user route 投递。没有且仅有一个 Guild/user 聚合 route 的消息记录会被拒绝。

IDENTIFY 中缺失的 Presence status 会保留已有用户偏好，仅在偏好不存在时初始化为 online；client state 缺失时默认为 foreground。显式值会被严格校验。后续 Presence Update 使用部分更新语义：status 更新共享的用户偏好，client state 只更新当前逻辑 Session。空更新会被拒绝，无变化的 client state 更新会直接丢弃。转发的更新每个逻辑 Session 最多 5 次/20 秒，随后还需消耗跨设备共享的每用户 10 次/20 秒配额。

断线 Session 默认保留 120 秒。Resume 必须路由回原 Session 节点；节点进程丢失会同时丢失内存 Session。Session 节点通过 etcd 租约注册；进入 drain 后发布 draining 状态、拒绝新连接，并分批要求现有客户端重新 IDENTIFY。

## Dispatcher

独立后台服务，为 Guild、Message、User 与 Presence event topic 分别创建 consumer client、消费循环和 `cordis.dispatcher.{guild,message,user,presence}.v1` group，使积压、重试和 rebalance 相互隔离，同时共享路由器与 Session gRPC 连接池。每个当前分配的 Kafka partition 都有一个长期存在的串行 worker；poll 得到的记录进入对应 partition 的有界队列，队列满时只暂停该 partition 的拉取。Guild 消息携带 `guild_id`；Message 的 `created`、`updated` 和 `deleted` 记录都按 `channel_id` 作为 Kafka key，包括 payload 携带接收者 `user_id` 的 DM 记录，以保证同一频道的事件顺序。`message.read.updated`、`dm.channel.created` 等 user-keyed Message 记录才按目标 `user_id` 作为 key。Dispatcher 从 Redis 解析聚合 Guild route，再调用频道分发 RPC；DM 消息通过聚合 user route 调用用户分发 RPC。Profile 更新会先从 Guild、User 和 Message 分页加载共同 Guild、关系及 DM 受众，再执行 fanout。

消费完成的 offset 由后台 coordinator 按 partition 合并后提交，正常提交不阻塞消费 worker；rebalance 或停止时会停止对应 worker，并同步 flush 已完成的记录。格式错误或不支持的事件视为永久错误并提交丢弃；发现或 RPC 等暂时错误只阻塞所在 partition 的 worker，按指数退避重试，成功后进入 offset 提交队列；其他 partition 和 topic 可以继续消费。单次尝试会合并重复目标节点，但整条记录重试时可能再次调用已经成功的节点，因此投递是至少一次语义，且当前没有通用 event ID 去重。

Dispatcher、Guild profile projector 和 Message mention expander 共用同一套
partition consumer runtime，统一处理 worker 生命周期、按 partition 隔离的 retry
以及手动 offset 提交。

如果 Kafka commit lag 达到 `maxUncommittedRecords`，Dispatcher 会暂停该 partition 的拉取，直到提交成功后再恢复；这个暂停不会覆盖该 partition 自身的 queue 或 retry 暂停。revoke 期间等待 worker 退出受 `revokeTimeoutSeconds` 限制；超时的 worker 会被标记为 inactive，其正在处理的记录不会被提交，交由 Kafka 重新投递。
优雅关闭预算由 `shutdownTimeout` 配置；go-zero 的强制退出时间会在该预算上加上默认的一秒 wrap-up 阶段。

## Presence

监听 `:3003`，是 Redis 支撑的用户状态偏好与设备活跃度服务。它按用户保存唯一的 status 偏好；Session 只保存 client state、租约和设备元数据，并按 TTL 与 generation 过滤失效记录。没有活动 Session 或偏好为 `INVISIBLE` 时公开状态为离线，其他情况下公开状态等于用户偏好。Session 注册只能初始化缺失偏好，不能覆盖已有偏好；续租不再携带 status。

Presence 在 Redis 中无 TTL 保存用户偏好，并另行持久化公开状态聚合快照，包括 offline tombstone。偏好变化通过 user route 发布私有 `presence.preference.updated`，公开状态变化发布 `presence.updated`，两者分别使用独立的单调递增 version。每次公开状态变化都会获得基于 Snowflake 且跨服务节点钳制为大于旧值的 version；无变化的续租保留版本，`last_seen_at` 可以在不产生公开状态 transition 时推进。内部 Resolve 与对应的 `presence.updated` 携带同一个 version。写入和按需 Resolve 都在按用户的 Redis 锁内完成对账。
