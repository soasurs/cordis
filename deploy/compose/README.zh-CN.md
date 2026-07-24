# 本地完整环境

这套 Compose 编排用于前端联调和本地端到端测试，不替代根目录中只启动基础设施的
`compose.yaml`。

## 准备配置

复制环境变量模板：

```bash
cp deploy/compose/.env.example deploy/compose/.env
```

替换文件中的全部 `REPLACE_ME`。JWT 密钥和 TOTP 密钥可以这样生成：

```bash
openssl rand -base64 48
openssl rand -base64 48
openssl rand -base64 32
```

`CORDIS_POSTGRES_PASSWORD` 与 `CORDIS_DATABASE_DSN` 中的密码必须一致。为避免 URL
转义问题，本地数据库密码建议只使用字母、数字、连字符和下划线。

真实 `.env` 已被 Git 忽略。不要提交或把 `docker compose config` 的完整输出粘贴到日志。

## 启停

检查配置并启动：

```bash
make compose-local-config
make compose-local-up
```

查看状态和日志：

```bash
docker compose --env-file deploy/compose/.env -f deploy/compose/compose.yaml ps
docker compose --env-file deploy/compose/.env -f deploy/compose/compose.yaml logs -f api gateway session dispatcher
```

停止并保留数据：

```bash
make compose-local-down
```

只有确认不再需要本地数据时才使用 `down -v` 删除 PostgreSQL、Redis、Kafka、etcd 和
MinIO 命名卷。

## 访问地址

| 用途 | 地址 |
| --- | --- |
| API | `http://localhost:8080` |
| WebSocket | `ws://localhost:8081/` |
| MinIO S3 API | `http://storage.cordis.localhost:9000` |
| MinIO Console | `http://localhost:9001` |

现代浏览器通常会自动将 `*.localhost` 解析到 `127.0.0.1`。如果本机环境没有这样做，
在 `/etc/hosts` 中加入：

```text
127.0.0.1 storage.cordis.localhost
```

Media 容器也使用 `storage.cordis.localhost:9000`，Compose 为 MinIO 配置了同名网络
别名，因此浏览器和服务端看到的预签名 URL hostname 保持一致。

## 前端开发连接

Vite 开发服务器只需代理 HTTP API：

```ts
server: {
  proxy: {
    "/api.v1": {
      target: "http://localhost:8080",
    },
  },
}
```

WebSocket 不经过 Vite proxy，前端直接连接：

```ts
const gatewayURL = "ws://localhost:8081/"
```

Gateway 会在 WebSocket 握手时校验 `Origin`。默认允许
`CORDIS_GATEWAY_ORIGIN=http://localhost:5173`；如果 Vite 使用其他 host 或端口，需要同步
修改 `.env`。这不是普通 HTTP CORS 响应头，而是 WebSocket 的跨来源访问控制。

生产环境可以分别使用 `https://app.example.com`、`https://api.example.com` 和
`wss://gateway.example.com/`。Gateway 配置中的 `originPatterns` 必须包含前端页面的
Origin，例如 `https://app.example.com`。

公开头像和 Guild 图标的基础地址是：

```text
http://storage.cordis.localhost:9000/cordis-public
```

附件下载使用预签名 URL。MinIO CORS 默认允许 `localhost:5173` 和
`localhost:4173`；前端使用其他端口时需要同步更新 `.env` 中的
`CORDIS_BROWSER_ORIGINS`。

## 当前限制

Mailer 仍使用 `noop` provider。注册、登录和其他业务流程可联调，但邮件验证与密码找回
暂时无法取得真实邮件中的 token。后续可增加 SMTP provider 和 Mailpit。
