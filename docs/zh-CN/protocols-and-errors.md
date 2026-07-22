# API、协议与错误

## Protobuf 与代码生成

公开协议位于 `proto/api`，生成 opaque Go API 和 Connect-Go；内部协议位于各服务目录，同样使用 edition 2023 和 opaque Go API。所有生成的 protobuf 消息都应通过 getter、setter 和 builder 访问，不能依赖生成 struct 字段。

修改 `.proto` 后运行：

```bash
make generate
make lint
```

## 资源更新语义

资源的 `Update` RPC 默认采用部分更新：只修改请求中明确出现的字段，未出现的字段保持存储值不变。显式提供的默认值仍表示一次更新，例如显式传入空 `avatar_uri` 会清除头像；没有提供任何可变字段的请求会被拒绝。

公开与内部 protobuf API 都使用 edition 2023 的标量字段 presence。API 适配层只有在入站请求的 `HasFoo` 为 true 时才向内部请求调用对应 setter；服务与 Store 使用指针或等价的 presence-aware 参数把该信息一直传到 SQL，只更新被选中的列。调用方不得先读取资源、拼出完整的新状态，再把无关字段一并写回。集合字段一旦出现，默认替换完整集合，除非 API 已定义专门的增删操作。

## WebSocket envelope

WebSocket 消息采用 `op`、可选 `s`、可选 `t` 和 `d`。主要 opcode：

- `0`：服务端 dispatch；
- `1`：heartbeat；
- `2`：identify；
- `3`：Presence 更新；
- `6`：resume；
- `9`：invalid session；
- `10`：hello；
- `11`：heartbeat ACK。

领域事件的 `t` 使用小写点分名称；Gateway 生命周期事件使用 `HELLO`、`READY`、`RESUMED`、`HEARTBEAT_ACK` 和 `ERROR`。

WebSocket JSON 中的 Snowflake ID 使用十进制字符串。`READY` 和领域事件 payload 中的 ID 输出为字符串；sequence、revision 和时间戳仍使用 JSON number。

## 内部错误

领域服务使用 `pkg/rpcerror.New` 创建 gRPC status，并附带 `google.rpc.ErrorInfo`，其中包含稳定的 domain 和 reason。错误消息保持面向开发者，稳定判断应使用 code/domain/reason。

公开 API 通过 `apierror.FromRPC` 映射已知错误，并附加 `api.v1.PublicErrorInfo`。未知错误不会泄露内部实现细节。Gateway 和 Presence 的部分校验目前仍直接使用普通 gRPC status。

## 鉴权边界

公开业务请求携带 actor user ID 到内部服务。Authenticator 校验凭证并签发 token；Gateway 的 IDENTIFY/RESUME 由 Session 调用 Authenticator 验证 token。Guild 是成员、角色和频道权限的权威来源，Message 和 Session 不复制权限算法，而是调用 Guild 授权接口。
