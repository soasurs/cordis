# 实时系统

## 连接生命周期

1. 客户端连接 Gateway WebSocket。
2. Gateway 返回 `op=10` 的 `HELLO`，包含 45 秒心跳间隔。
3. 客户端发送 `IDENTIFY`，或携带 `session_id` 与最后 sequence 发送 `RESUME`。
4. Gateway 从 etcd 选择可用 Session 节点；Resume 先从 Redis 查找 Session owner，再用 etcd 校验节点 generation。
5. Gateway 建立 `SessionService.Connect` 双向流，并发送首条 `ConnectRequest`。
6. IDENTIFY 成功后 Session 返回 sequence 化的 `READY`；RESUME 成功后重放缺失事件并返回 `RESUMED`。
7. 后续 Presence、detach 和服务端事件经同一条流传递；Gateway 本地处理 heartbeat，并批量向 Session 同步 sequence checkpoint。

Gateway 实例身份包含 ID 与 generation，可区分同名进程重启。逻辑 Session 与 WebSocket connection 分离，因此一次断线重连可以绑定到原 Session。

## Sequence、ACK 与回放

只有需要恢复的 dispatch 事件进入回放缓冲区并获得递增 sequence。每个逻辑 Session 最多保存 2048 条；溢出时移动 replay floor。客户端 heartbeat 携带已处理 sequence，Session 单调更新 ACK，并清理不再需要的前缀。

Resume sequence 低于 replay floor、超过服务端 sequence，或 Session 已过期时，Resume 无效，客户端必须重新 IDENTIFY。缓冲区只在内存中，不跨 Session 节点迁移。

## 路由与权限

IDENTIFY 自动建立用户和 Guild 路由。Dispatcher 按 Guild 将消息路由到候选 Session 节点；Session 通过带 revision 的用户可见性快照过滤，然后投递给该用户的全部本地逻辑 Session。权限事件会使受影响快照失效；重建失败时保持 fail closed，并为当前失效代发送一次带 sequence 的 `session.reconcile`。

成员被踢出或封禁时，事件先投递给当前 Guild 会话，再撤销其 Guild 索引。这样客户端能够收到导致访问失效的最终状态事件。

## etcd 节点目录与 Redis 索引

- `/cordis/session/nodes/{node_id}`：etcd 租约 key，保存节点 generation、RPC 地址和 ready/draining 状态。
- `session:owners:{session_id}`：逻辑 Session 所属节点。
- `session:auth_sessions:{auth_session_id}`：逻辑 Session ID 及所属 Session 节点 ID、generation。
- `gateway:routes:users:{id}:nodes`：用户所在 Session 节点。
- `gateway:routes:guilds:{id}:nodes`：Guild 成员所在 Session 节点。

路由成员包含 node ID 与 generation。Redis TTL 与 etcd 租约、读取时 generation 校验共同排除旧进程记录。

## 事件路径

```mermaid
sequenceDiagram
    participant Domain as Guild/Message
    participant Kafka
    participant Dispatcher
    participant Redis
    participant etcd
    participant Session
    participant Gateway
    participant Client
    Domain->>Kafka: {t, d}
    Dispatcher->>Kafka: poll
    Dispatcher->>Redis: resolve route
    Dispatcher->>etcd: resolve node generation
    Dispatcher->>Session: Dispatch*Event
    Session->>Session: filter + sequence + replay
    Session->>Gateway: ConnectResponse
    Gateway->>Client: WebSocket envelope
```

事件在 Dispatcher 重试下是至少一次语义。当前协议没有通用 event ID 去重，因此消费者应能够容忍重复事件。
