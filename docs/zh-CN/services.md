# 服务目录

## API

公开 Connect-RPC HTTP 服务，监听 `:8080`。它代理 Authenticator、User、Guild 和 Message RPC，将内部 protobuf 转换为公开模型，并通过 `pkg/apierror` 将带 `google.rpc.ErrorInfo` 的内部错误转换为稳定的公开错误。API 本身不访问业务数据库。

公开请求使用 Redis-backed 命名限流 policy，并在 Redis 故障时使用有界本地 fallback。IP 桶按 IPv4 `/32` 或 IPv6 `/64` 归一化，IPv4 阈值会为 CGNAT 放宽。所有请求先消费来源 IP guard；认证成功后再消费用户通用配额，消息创建、关系写入、Guild 资源创建和邀请加入还会消费对应业务桶。认证后的 `GetReadStates` reconcile 还使用进程内 keyed limiter，限制同一用户的并发请求数。

## User

监听 `:3000`，拥有用户和资料数据。负责注册所需的用户创建、用户查询、邮箱可用性、邮箱更新、资料更新、密码校验和修改。密码使用 Argon2id 哈希。User 不签发令牌。

## Authenticator

监听 `:3001`，负责注册编排、登录、访问令牌与刷新令牌、令牌校验以及登录 Session 管理。用户身份由 User 提供；密码凭据和认证 Session 存储在 PostgreSQL。访问令牌默认短期有效，刷新令牌和认证 Session 默认 30 天。真实启动需要访问令牌和刷新令牌密钥环境变量。

注册支持 `open`、`invite_only` 和 `closed` 三种模式。邀请制使用由 Authenticator
保存的一次性邀请码，也可以将邀请码绑定到指定邮箱。邀请码会在 Argon2 和 User RPC
之前被短暂预留，并与密码凭据及初始 Session 在同一事务中核销。密码重置只适用于已经
拥有 credential 的账号；未完成的注册必须通过 `Register` 继续。

所有 Argon2 哈希和校验都受进程内 weighted semaphore 保护。容量由 `password.maxConcurrency` 配置（默认 4），当前每项 Argon2 工作使用一个权重，因此等同于每个 Authenticator 实例固定数量的并发 slot，而不是全集群共享上限。slot 满时请求等待，context 超时或取消时退出等待；semaphore 本身不提供独立的有界请求队列，外层 API rate limiter 负责限制进入量。

## Guild

监听 `:3005`，拥有 Guild、成员、封禁、角色、成员角色、频道和频道权限覆盖。它实现：

- Guild 创建、查询、修改、删除、离开和所有权转移；
- 成员加入、修改、踢出、封禁、解封和封禁列表；
- 角色 CRUD、成员角色和显式排序；
- 文本、分类、语音频道的元数据与排序；
- 角色/成员频道权限覆盖和频道授权检查。

权限使用 `uint64` 位集。Guild owner 和 `ADMINISTRATOR` 获得完整权限；频道权限在 Guild 权限上依次应用默认角色、成员角色以及成员覆盖。失去 `VIEW_CHANNEL` 时相关发送权限也被移除。Guild 事件直接发布到独立 topic `cordis.guild.events.v1`。

持久化 Guild 资源使用配置化硬上限。默认每用户最多拥有 10 个、加入 100 个 Guild；每 Guild 最多 250 个角色、500 个频道和 100 个有效邀请；每频道最多 100 条权限覆盖。配额检查与资源写入在同一 PostgreSQL 事务内串行执行。

内部 `GetUserReadyState` 在一次调用中按用户的有效 Guild 成员关系返回完整 READY 数据，包括 Guild、全部角色、当前成员的显式角色 ID、可见频道以及这些频道的 permission overwrites。每份快照携带持久化的 `access_revision`；当成员关系、角色权限或分配、频道、权限覆盖、所有权或 Guild 删除可能改变访问权限时，PostgreSQL 触发器会推进这个单调递增版本。只要 Guild 仍存在，发布的 Guild 事件会携带事务提交后的版本。

## Message

监听 `:3002`，拥有消息、附件、提及和回复关系。创建、读取、更新和删除操作先调用 Guild 授权。列表使用 `before`、`after` 或 `around` 游标分页。当前没有反应或自定义 emoji RPC。

消息创建和更新默认最多携带 10 个附件和 100 个不重复的被提及用户 ID；两项上限均由 Message 服务配置。

内部 READY RPC 一次加载用户的全部 DM，并针对 Session 提供的可见 Guild 文本频道计算 read state。每项包含 `channel_id`、`last_message_id`、`last_read_message_id` 和未读提及数；客户端用 `last_message_id > last_read_message_id` 判断是否未读，不再计算具体未读消息数。`AckMessage` 只有在 watermark 实际前进时才发布 user-routed `message.read.updated`，CreateMessage 也会在写事务内从数据库读回作者的最终 read state。

保留认证后的 HTTP `GetReadStates` 作为 reconcile 路径，但不再接收 `channel_id` 列表。客户端只能按一个 `guild_id` 或全部 DM 两种 scope 同步；Guild scope 由服务端授权结果产生可见文本频道，DM scope 同时返回完整 DM 列表，以修复漏掉的创建事件。API 按用户限制并发，Message 端再用进程内容量限制聚合查询负载。服务端产生的超大 scope 会按 capacity 拆成多个数据库批次，每批获取与频道数完全一致的 weight，不会把一条超大查询 clamp 成 capacity 后直接执行。

允许客户端创建的消息类型仅为 `DEFAULT` 和 `REPLY`；`THREAD_STARTER` 保留给未来 Thread 功能。客户端可设置的 flag 目前只有 `SUPPRESS_NOTIFICATIONS`。写事务提交后，服务 best-effort 直接向 `cordis.message.events.v1` 发布事件；发布失败只记录日志。

## Gateway

监听 `:8081`，提供 `/ws` 和 `/health`。连接后发送 `HELLO`，首个客户端消息必须是 `IDENTIFY` 或 `RESUME`。Gateway 从 etcd 发现 Session 节点；Resume owner 仍从 Redis 读取。建立 `SessionService.Connect` 双向 gRPC 流后，它只负责 WebSocket 与 gRPC 消息互转，不保存逻辑路由状态，也不消费 Kafka。

接受 WebSocket 前，Gateway 会按可信代理解析出的 IPv4 `/32` 或 IPv6 `/64` 来源作用域限速。连接容量完全由进程本地维护：每实例默认最多 50000 条连接和 5000 条 pending handshake，IPv4 与 IPv6 每来源 pending 上限分别为 100 和 20；Session 接受 IDENTIFY 或 RESUME 后立即释放 pending 槽。每条连接默认每分钟最多发送 120 个 Gateway event。`IDENTIFY` 还会按来源作用域限速；`RESUME` 同时按来源作用域和逻辑 Session ID 限速，只有这些离散限流事件使用 Redis。

物理连接活性由 Gateway 本地管理。Gateway 校验 heartbeat sequence 并直接返回 `HEARTBEAT_ACK`，连续两个约定周期未收到 heartbeat 时关闭连接；比约定周期提前超过 10% 的 heartbeat 会被拒绝，也不会延长活性 deadline。只有确认 sequence 前进时才记录 dirty checkpoint；默认每 5 秒按目标 Session 节点归并，并以每批最多 500 条同步。Session binding epoch 用来拒绝连接被替换后迟到的 checkpoint。

## Session

监听 `:3006`，是实时系统的有状态核心。它负责：

- 校验 IDENTIFY/RESUME 的 access token；
- 创建逻辑 Session，并加载用户完整的 READY Guild 与 read-state 快照；
- 保存用户和 Guild 的本地反向索引；
- 分配递增 sequence，保存最多 2048 条内存回放记录；
- 应用 Gateway 批量同步的 heartbeat ACK checkpoint、处理 Presence 更新、detach 和 resume；
- 接收 Dispatcher 的 Guild、频道和用户事件并本地 fanout。

IDENTIFY 分别向 Guild 和 Message 拉取完整 READY：Guild、角色、成员角色、可见频道及其 permission overwrites、全部 DM 和四字段 read state。READY 组装期间收到的实时事件先缓冲，READY 以 sequence 1 发出后再按接收顺序入队。pending dispatch 同时受事件条数和事件数据总字节数限制，有效条数还会低于 replay 与 binding queue 容量；溢出时清空 pending buffer 并让本次 IDENTIFY 失败，使客户端重连后重新获取权威快照。默认加载上限为每用户 100 个 Guild、每 Guild 500 个可见频道。同一节点上属于同一用户的逻辑 Session 共享授权快照，最后一个本地 Session 移除后释放。Guild access 事件按 revision 使受影响的快照失效；按用户和 Guild 的重建使用 singleflight 合并，单节点默认最多并发 16 次且每次最多等待 2 秒。缺失、格式错误、超限、版本过旧或已标记失效的快照不能用于授权。重建失败时会跳过敏感事件，并为当前失效代发送一次带 sequence 的 `session.reconcile`。

Access token 校验通过后，`IDENTIFY` 会分别按用户 ID 和认证 Session ID 限速。同一个认证 Session 可以为多个浏览器页面或设备创建并存的逻辑 Session；每个逻辑 Session 拥有独立的 Session ID、回放窗口、Presence 租约和 transport binding。

客户端 heartbeat 不再直接触发 Session 的 Redis owner 或 Presence 续租；逻辑 Session owner 以 resume timeout 为 TTL，通过有界 Redis pipeline 批量续租，Presence 通过批量 RPC 续租。维护周期为 resume timeout 的四分之一，并加入 ±20% cycle jitter 以打散不同 Session 节点；每批 500 个 Session，并分配到最长 5 秒刷新窗口内带 jitter 的 slot。聚合 route 使用单独循环续租，不受 lease sweep 耗时影响。

Dispatcher 通过聚合 Guild route 定位 Guild 消息的候选 Session 节点，并通过专用 Guild-message RPC 携带 Guild 与频道 ID。Session 按本地用户检查服务端可见性快照，将消息投递给该用户的所有本地逻辑 Session。DM 消息为每个参与者各发布一条记录，并通过聚合 user route 投递。没有且仅有一个 Guild/user 聚合 route 的消息记录会被拒绝。

无变化的 Presence 更新会直接丢弃。实际变化每个逻辑 Session 最多 5 次/20 秒，随后还需消耗跨设备共享的每用户 10 次/20 秒配额，才会调用 Presence。

断线 Session 默认保留 120 秒。Resume 必须路由回原 Session 节点；节点进程丢失会同时丢失内存 Session。Session 节点通过 etcd 租约注册；进入 drain 后发布 draining 状态、拒绝新连接，并分批要求现有客户端重新 IDENTIFY。

## Dispatcher

独立后台服务，使用 consumer group `cordis.dispatcher.v1` 消费 `cordis.guild.events.v1` 和 `cordis.message.events.v1`。Guild 消息携带 `guild_id` 并按 Guild ID 作为 Kafka key；Dispatcher 从 Redis 解析聚合 Guild route，再调用频道分发 RPC。DM 消息携带目标 `user_id`，通过聚合 user route 调用用户分发 RPC。

消费采用手工提交。格式错误或不支持的事件视为永久错误并提交丢弃；发现或 RPC 等暂时错误按指数退避重试，成功后提交。单次尝试会合并重复目标节点，但整条记录重试时可能再次调用已经成功的节点，因此投递是至少一次语义，且当前没有通用 event ID 去重。

## Presence

监听 `:3003`，是 Redis 支撑的在线状态服务。它管理用户设备 Session，并按 TTL 和 generation 过滤失效记录。多个设备状态聚合为用户 Presence；`INVISIBLE` 对外表现为离线。Session 调用 Presence 注册和刷新用户在线状态。
