# Complete local environment

This Compose stack runs every Cordis service and its infrastructure dependencies
for frontend integration and local end-to-end testing. It does not replace the
root `compose.yaml`, which intentionally starts infrastructure only.

Copy `.env.example` to `.env`, replace every `REPLACE_ME`, then run:

```bash
make compose-local-config
make compose-local-up
```

The main endpoints are:

- API: `http://localhost:8080`
- WebSocket: `ws://localhost:8081/`
- MinIO S3 API: `http://storage.cordis.localhost:9000`
- MinIO console: `http://localhost:9001`

Stop the stack while retaining its named volumes with:

```bash
make compose-local-down
```

See [README.zh-CN.md](README.zh-CN.md) for configuration, Vite proxy, local DNS,
object storage, and current mail-delivery limitations.
