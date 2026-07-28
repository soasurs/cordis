# 认证与令牌轮换

## 凭证传输方式

Cordis 明确支持两种 token transport。移动端、CLI 和服务端客户端使用
`TOKEN_TRANSPORT_TOKEN`：登录响应包含 access token 和 refresh token，API 请求发送
`Authorization: Bearer <access-token>`，Gateway 的 `identify` / `resume` JSON 使用
`token`。浏览器使用 `TOKEN_TRANSPORT_COOKIE`：API 写入 host-only、`HttpOnly`、
`SameSite=Strict` 的 access/refresh Cookie，并从响应 body 中省略两个 token 字符串。
本地 HTTP 配置可以关闭 `Secure`，生产 HTTPS 必须启用。

显式 Authorization header 始终优先；无效 Bearer 不能回退到 Cookie。Cookie 认证请求的
Origin 必须精确匹配 `browserAuth.allowedOrigins`，并与严格 SameSite Cookie 一起防御
CSRF。refresh Cookie 使用 `Path=/`，因为任意受保护 API 都可能在进入 handler 前完成刷新。
日志、trace 和错误中不得记录 token 或完整 Cookie header。

`Login` 和 `CompleteTwoFactorLogin` 必须选择相同 transport。未指定 transport 时为兼容
现有客户端按 token transport 处理。Cookie 模式调用 `Refresh` 和 `Logout` 时省略请求中的
`refresh_token`；token 客户端仍通过 protobuf 请求显式传递。

## 浏览器透明刷新

Cookie 模式的受保护请求在领域逻辑执行前完成认证。access Cookie 有效时直接继续；access
Cookie 缺失，或其签名有效但已经过期时，API 把 access/refresh Cookie 交给 Authenticator。
refresh 凭证有效时完成轮换，在同一 HTTP 响应中写入新 Cookie，并以更新后的身份继续原请求。
格式错误或签名错误的 access token 不会回退 refresh；属于不同认证 Session 的 access 与
refresh 凭证也会被拒绝。

浏览器不需要 access token 定时器，也不应周期性主动调用 Refresh。暂时网络错误只进入重试或
离线状态并保留当前页面；只有 Session 过期、撤销或因重放而撤销时才进入未登录状态。后台页面
不会仅因定时器持续运行而无限延长空闲 Session。

## Refresh rotation 与 Session

每次成功使用 refresh token 都会轮换。Authenticator 只存 token hash，以及重建当前 refresh
JWT 所需的非敏感 claims。紧邻的上一个 refresh token 有 30 秒恢复窗口；窗口内再次使用会重建
并返回同一个当前 refresh token，而不是继续产生新一代。这使浏览器并发请求以及响应丢失后的
重试具备幂等性。使用更旧一代 token，或在窗口结束后使用 previous token，会被视为重放并撤销
Session。

认证 Session 空闲 30 天后过期，绝对有效期为 180 天。Refresh 将空闲期限推进到
`min(now + 30 days, absolute deadline)`，但绝不会延长绝对期限。`session_expires_at` 返回当前
空闲期限，`absolute_session_expires_at` 返回硬性期限。

## 原生客户端最佳实践

原生客户端把 refresh token 保存到 Keychain、Android Keystore 或同等级系统安全存储。access
token 通常只放内存；若为了冷启动速度持久化，也必须使用安全存储。进程级统一 AuthManager
负责轮换：并发未认证响应等待同一个 Refresh；先持久化新 refresh token，再发布新 access
token；每个失败业务请求最多重试一次。网络错误不能清除凭证或直接跳转登录；明确的 Session
过期、撤销、refresh 无效或重放结果才结束登录状态。

## 浏览器 Gateway ticket

浏览器 JavaScript 无法读取 HttpOnly Cookie，标准 WebSocket API 也不能添加 Authorization
header。页面在建立 WebSocket 的同时调用 `CreateGatewayTicket`，等待 ticket 和 Gateway `hello`
都到达后，在 `identify` 或 `resume` 中发送 `gateway_ticket`。ticket 创建复用透明 Cookie 认证；
若 access token 剩余时间不足以覆盖 ticket 窗口，会先自动刷新。

Ticket 使用 256 bit 随机熵，有效期 30 秒；Redis key 只包含其 SHA-256 hash，Session 兑换时原子
删除。移动端等原生客户端无需 ticket，继续使用 JSON `token` 字段。两个 credential 字段同时
出现或同时缺失都是协议错误。Gateway 仍必须校验 WebSocket Origin allowlist。
