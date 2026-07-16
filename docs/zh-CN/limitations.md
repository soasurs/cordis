# 当前限制与演进方向

以下内容描述当前实现边界，不代表已经承诺的交付顺序。

## 可用性

仓库内的本地示例使用单节点 Redis 和 etcd，但生产目标是 Redis Cluster 与 etcd Cluster。Redis 承载 Presence、Resume owner 和实时聚合路由；etcd 承载 Session 节点租约与发现。任一集群失去 quorum 或不可用都会影响对应路径。当前尚未实现路由缓存或广播降级。

Redis key 已按实体使用 hash tag，且没有跨 key Lua 脚本或事务，因此可以由 Redis Cluster 分槽执行。跨 slot pipeline 仅用于批处理，不提供原子性；当前依靠 TTL、generation 和读取时校验清理部分写入或旧记录。

Session 状态与 2048 条回放记录只在内存中。节点故障后无法恢复原 Session；当前只支持优雅 drain 时要求客户端重新 IDENTIFY，不支持状态迁移。

## 事件可靠性

Message 和 Guild 都在数据库事务提交后 best-effort 直写 Kafka，没有事务 Outbox，因此提交成功与事件发布之间存在丢失窗口。Dispatcher 提供重试和手工 offset 提交，但没有死信队列、全局 event ID 或幂等去重。Session RPC 分发按节点串行执行，一个节点失败会使整条 Kafka 记录重试。

## 功能缺口

- 邀请链接和邀请使用；
- 更完整的数量限制与限流；
- 更自动化的角色和频道移动重排；
- Thread 消息与 Thread 生命周期；
- 消息置顶；
- 语音频道仅有类型和元数据，没有语音媒体能力；
- Gateway/Session 协议尚无压缩、分片和明确的版本协商。

## 运维缺口

目前没有统一部署清单、容器编排、跨服务 readiness 策略和端到端运行手册。Dispatcher、Gateway 和 Session 的 Redis、etcd、Kafka 故障场景仍需集成测试与压力测试。配置中的明文开发地址只适用于本地环境，生产环境应使用独立 secret 和配置管理。
