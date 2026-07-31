# API、协议与错误

## Protobuf 与代码生成

公开协议位于 `proto/api`，生成 opaque Go API 和 Connect-Go；内部协议位于各服务目录，同样使用 edition 2023 和 opaque Go API。所有生成的 protobuf 消息都应通过 getter、setter 和 builder 访问，不能依赖生成 struct 字段。

修改 `.proto` 后运行：

```bash
make generate
make lint
```

## 资源更新语义

资源的 `Update` RPC 默认采用部分更新：只修改请求中明确出现的字段，未出现的字段保持存储值不变。显式提供的默认值仍表示一次更新，例如显式传入空 `bio` 会清除简介，显式传入 `avatar_asset_id = 0` 会清除头像；没有提供任何可变字段的请求会被拒绝。

公开与内部 protobuf API 都使用 edition 2023 的标量字段 presence。API 适配层只有在入站请求的 `HasFoo` 为 true 时才向内部请求调用对应 setter；服务与 Store 使用指针或等价的 presence-aware 参数把该信息一直传到 SQL，只更新被选中的列。调用方不得先读取资源、拼出完整的新状态，再把无关字段一并写回。集合字段一旦出现，默认替换完整集合，除非 API 已定义专门的增删操作。

频道创建、删除、parent 移动和 reorder 使用 Guild 级
`channel_layout_revision` 作为乐观并发 token。`ListGuildChannels` 返回该
token，结构变更请求必须携带客户端快照中的 revision。Guild 服务在获取事务级
advisory lock 后校验它；token 过期时事务以 `Aborted` 终止并回滚，公开 Connect
API 对应 HTTP `409 Conflict`。服务端不会自动刷新列表或重放过期操作；客户端应由
用户主动刷新后重新操作。仅修改频道 name/topic 不改变也不要求 layout revision。

## 可用性检查与头像约束

`CheckUsernameAvailability` 复用与 `UpdateUsername` 相同的用户名规范化与格式规则。非法用户名返回 `InvalidArgument`；合法但已占用返回 `available=false`。最终改名仍以数据库唯一约束为准。Media 使用稳定的内部 `media.cordis` reason 表示头像校验失败；API 将它们映射为公开 code：`profile.avatar_file_too_large`、`profile.avatar_content_type_invalid`、`profile.avatar_dimensions_exceeded`、`profile.avatar_pixels_exceeded`。`GetAvatarUploadConstraints` 返回 Media 当前 `userAvatar` 图片上传限制，供客户端在申请预签名 PUT 前遵守。

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

所有事件的 `t` 都使用小写点分名称。Gateway 事件类型及方向如下：

| `t` | 方向 | `op` |
| --- | --- | ---: |
| `hello` | 服务端到客户端 | `10` |
| `identify` | 客户端到服务端 | `2` |
| `ready` | 服务端到客户端 | `0` |
| `resume` | 客户端到服务端 | `6` |
| `resumed` | 服务端到客户端 | `0` |
| `heartbeat` | 客户端到服务端 | `1` |
| `heartbeat.ack` | 服务端到客户端 | `11` |
| `error` | 服务端到客户端 | `4000` |

WebSocket JSON 中的 Snowflake ID 使用十进制字符串。`ready` 和领域事件 payload 中的 ID 输出为字符串；sequence、revision 和时间戳仍使用 JSON number。

`identify` 和 `resume` 的 `d` 必须且只能包含一种凭证：原生客户端通过 `token` 发送 access token，浏览器通过 `gateway_ticket` 发送短期单次 ticket；两个字段同时出现或同时缺失都会被拒绝。

## 内部错误

领域服务使用 `pkg/rpcerror.New` 创建 gRPC status，并附带 `google.rpc.ErrorInfo`，其中包含稳定的 domain 和 reason。错误消息保持面向开发者，稳定判断应使用 code/domain/reason。

公开 API 通过 `apierror.FromRPC` 映射已知错误，并附加 `api.v1.PublicErrorInfo`。未知错误不会泄露内部实现细节。Gateway 和 Presence 的部分校验目前仍直接使用普通 gRPC status。

## 鉴权边界

公开业务请求携带 actor user ID 到内部服务。Authenticator 校验凭证并签发 token；Gateway 的 IDENTIFY/RESUME 由 Session 调用 Authenticator 验证 access token 或原子兑换 Gateway ticket。浏览器 API 使用 HttpOnly Cookie 和透明刷新，原生客户端使用 Bearer access token 和显式 Refresh；完整轮换与客户端约定见[认证与令牌轮换](authentication.md)。Guild 是成员、角色和频道权限的权威来源，Message 和 Session 不复制权限算法，而是调用 Guild 授权接口。
