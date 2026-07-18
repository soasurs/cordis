# 服务目录

## API

公开 Connect-RPC HTTP 服务，监听 `:8080`。它代理 Authenticator、User、Guild 和 Message RPC，将内部 protobuf 转换为公开模型，并通过 `pkg/apierror` 将带 `google.rpc.ErrorInfo` 的内部错误转换为稳定的公开错误。API 本身不访问业务数据库。

## User

监听 `:3000`，拥有用户和资料数据。负责注册所需的用户创建、用户查询、邮箱可用性、邮箱更新、资料更新、密码校验和修改。密码使用 Argon2id 哈希。User 不签发令牌。

## Authenticator

监听 `:3001`，负责注册编排、登录、访问令牌与刷新令牌、令牌校验以及登录 Session 管理。用户身份由 User 提供；密码凭据和认证 Session 存储在 PostgreSQL。访问令牌默认短期有效，刷新令牌和认证 Session 默认 30 天。真实启动需要访问令牌和刷新令牌密钥环境变量。

所有 Argon2 哈希和校验都受进程内 weighted semaphore 保护。容量由 `password.maxConcurrency` 配置（默认 4），当前每项 Argon2 工作使用一个权重，因此等同于每个 Authenticator 实例固定数量的并发 slot，而不是全集群共享上限。slot 满时请求等待，context 超时或取消时退出等待；semaphore 本身不提供独立的有界请求队列，外层 API rate limiter 负责限制进入量。

## Guild

监听 `:3005`，拥有 Guild、成员、封禁、角色、成员角色、频道和频道权限覆盖。它实现：

- Guild 创建、查询、修改、删除、离开和所有权转移；
- 成员加入、修改、踢出、封禁、解封和封禁列表；
- 角色 CRUD、成员角色和显式排序；
- 文本、分类、语音频道的元数据与排序；
- 角色/成员频道权限覆盖和频道授权检查。

权限使用 `uint64` 位集。Guild owner 和 `ADMINISTRATOR` 获得完整权限；频道权限在 Guild 权限上依次应用默认角色、成员角色以及成员覆盖。失去 `VIEW_CHANNEL` 时相关发送权限也被移除。Guild 事件直接发布到独立 topic `cordis.guild.events.v1`。

## Message

监听 `:3002`，拥有消息、附件、提及和回复关系。创建、读取、更新和删除操作先调用 Guild 授权。列表使用 `before`、`after` 或 `around` 游标分页。当前没有反应或自定义 emoji RPC。

`GetReadStates` 会批量计算频道已读状态、未读消息数和未读提及数。一个请求内的频道授权 fan-out 使用固定 worker 上限并发执行，避免无界跨服务调用；这不是跨请求共享的 weighted semaphore。外部调用量仍由 API 的 authenticated-user 通用配额限制。

允许客户端创建的消息类型仅为 `DEFAULT` 和 `REPLY`；`THREAD_STARTER` 保留给未来 Thread 功能。客户端可设置的 flag 目前只有 `SUPPRESS_NOTIFICATIONS`。写事务提交后，服务 best-effort 直接向 `cordis.message.events.v1` 发布事件；发布失败只记录日志。

## Gateway

监听 `:8081`，提供 `/ws` 和 `/health`。连接后发送 `HELLO`，首个客户端消息必须是 `IDENTIFY` 或 `RESUME`。Gateway 从 etcd 发现 Session 节点；Resume owner 仍从 Redis 读取。建立 `SessionService.Connect` 双向 gRPC 流后，它只负责 WebSocket 与 gRPC 消息互转，不再本地保存订阅，也不消费 Kafka。

## Session

监听 `:3006`，是实时系统的有状态核心。它负责：

- 校验 IDENTIFY/RESUME 的 access token；
- 创建逻辑 Session，并加载用户的 Guild 集合；
- 保存用户、Guild 和频道的本地反向索引；
- 校验频道订阅权限；
- 分配递增 sequence，保存最多 2048 条内存回放记录；
- 处理 heartbeat ACK、Presence 更新、detach 和 resume；
- 接收 Dispatcher 的 Guild、频道和用户事件并本地 fanout。

断线 Session 默认保留 120 秒。Resume 必须路由回原 Session 节点；节点进程丢失会同时丢失内存 Session。Session 节点通过 etcd 租约注册；进入 drain 后发布 draining 状态、拒绝新连接，并分批要求现有客户端重新 IDENTIFY。

## Dispatcher

独立后台服务，使用 consumer group `cordis.dispatcher.v1` 消费 `cordis.guild.events.v1` 和 `cordis.message.events.v1`。它解析统一事件 envelope，根据 Guild、频道或用户路由从 Redis 找到 Session 节点，并调用 `DispatchGuildEvent`、`DispatchChannelEvent` 或 `DispatchUserEvent`。

消费采用手工提交。格式错误或不支持的事件视为永久错误并提交丢弃；发现或 RPC 等暂时错误按指数退避重试，成功后提交。单次尝试会合并重复目标节点，但整条记录重试时可能再次调用已经成功的节点，因此投递是至少一次语义，且当前没有通用 event ID 去重。

## Presence

监听 `:3003`，是 Redis 支撑的在线状态服务。它管理 Gateway 实例、旧版频道路由以及用户设备 Session，按 TTL 过滤失效记录。多个设备状态聚合为用户 Presence；`INVISIBLE` 对外表现为离线。当前 Session 仍调用 Presence 来注册和刷新用户在线状态。
