---
title: 本地开发与验证
created: 2026-05-16
updated: 2026-07-31
status: operational-guide
---

# 本地开发与验证

本文说明当前仓库可执行的本地启动和廉价验证方式，不定义产品边界或完成状态。目标验收以[当前平台目标合同](./platform-target-contract.md)为准，当前缺口见[当前实现状态](./current-state.md)。

## 本地依赖

| Service     | 默认地址                                     |
| ----------- | -------------------------------------------- |
| PostgreSQL  | `localhost:5432`                             |
| Redis       | `localhost:6379`                             |
| Backend API | `localhost:8080`                             |
| Frontend    | `localhost:5173`，端口占用时以 Vite 输出为准 |

从仓库根目录准备忽略提交的本地环境文件并检查服务：

```bash
createdb device_platform
make setup-local
make check-services
make check-db
pnpm --dir frontend install
```

`make setup-local` 从示例创建 `backend/.env` 与 `frontend/.env.development`，不会覆盖已有文件。只使用本地测试凭据，不把真实 WWTIOT secret 或业务数据写入仓库、日志或截图。

setup 会生成独立的 32 byte Webhook secret encryption key，并以无 padding base64url 写入忽略提交的 `backend/.env`。已有的已安装本地环境升级后也必须配置 `WEBHOOK_SECRET_ENCRYPTION_KEY`；缺失、解码失败或长度不是 32 byte 时后端会失败关闭，不能复用 `JWT_SECRET`。

持久 Webhook dispatcher 默认每 2 秒扫描，单次 HTTP timeout 为 10 秒、lease 为 15 秒，最多执行 5 次 HTTP Attempt，失败间隔为 `1s,5s,30s,2m`。部署可用 `WEBHOOK_MAX_ATTEMPTS` 降低次数，并同步用 `WEBHOOK_RETRY_SCHEDULE` 提供少一次且不短于对应默认值的间隔；timeout、lease 或 schedule 非法时启动失败关闭。`WEBHOOK_EGRESS_ALLOWLIST` 默认留空，只允许解析为公开地址的目标；即使 Project 允许配置本机 HTTP URL，连接 loopback 或内网目标仍需在部署配置中显式加入受控 IP/CIDR，例如仅本地测试时使用 `127.0.0.1/32`。不要用宽网段替代目标清单，云 metadata 固定地址始终拒绝。

仅对从未写入过加密 Webhook secret 的旧本地环境，可执行以下一次性升级；命令不会把 key 输出到终端。若数据库已经存在 `project_webhook_secrets`，必须恢复原部署 key，生成新 key 会使已有版本无法解密。

```bash
cd backend
umask 077
device_platform_webhook_key="$(openssl rand -base64 32 | tr '+/' '-_' | tr -d '=\n')"
if grep -q '^WEBHOOK_SECRET_ENCRYPTION_KEY=' .env; then
  WEBHOOK_SECRET_ENCRYPTION_KEY="$device_platform_webhook_key" perl -0pi -e 's/^WEBHOOK_SECRET_ENCRYPTION_KEY=.*$/WEBHOOK_SECRET_ENCRYPTION_KEY=$ENV{WEBHOOK_SECRET_ENCRYPTION_KEY}/m' .env
else
  printf '\nWEBHOOK_SECRET_ENCRYPTION_KEY=%s\n' "$device_platform_webhook_key" >> .env
fi
chmod 600 .env
unset device_platform_webhook_key
```

`make check-services` 检查 PostgreSQL 与 Redis 端口；`make check-db` 按 `backend/.env` 的 `DATABASE_URL` 发起只读连接检查。端口可达不等于 schema、业务持久化或目标链路已经完成。

## 启动

分别运行：

```bash
make dev-backend
```

```bash
make dev-frontend
```

存活检查：

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

`/healthz` 只证明进程存活。首次安装前 `/readyz` 会报告需要 setup。

浏览器打开：

```text
http://localhost:5173/setup
http://localhost:5173/auth/login
```

setup 完成后使用刚创建的本地管理员登录。前端开发环境通过 Vite 将相对 `/setup/...` 和 `/v1/...` 请求代理到 `http://localhost:8080`；通常保持 `VITE_API_BASE_URL=''`。

## 直接使用子项目命令

从 `backend/`：

```bash
make build
make test
make test-int
make lint
make migrate-up
make migrate-down
```

`make test-int` 用于带 `integration` tag 且具备外部条件的测试。需要 PostgreSQL 的用例只读取 `MIGRATION_TEST_DATABASE_URL`，并拒绝连接数据库名不是 `device_platform_test` 的 URL；需要真实多进程启动的用例还读取 `MIGRATION_TEST_REDIS_URL`。未设置时对应测试会明确跳过，不能把跳过结果记录为 PostgreSQL/Redis integration 已通过。

只对已经确认的本地测试服务运行，例如：

```bash
MIGRATION_TEST_DATABASE_URL='postgres://user:password@127.0.0.1:5432/device_platform_test?sslmode=disable' \
MIGRATION_TEST_REDIS_URL='redis://127.0.0.1:6379/0' \
make test-int
```

PostgreSQL integration 在 `device_platform_test` 内为每个测试创建随机 schema，并在结束时删除；不要把 URL 指向 `postgres` 管理库、开发业务库或任何生产数据库。`MIGRATION_TEST_REDIS_URL` 只用于连接和多进程运行时依赖检查，不授权清理共享 Redis 数据。`make migrate-up` / `make migrate-down` 会直接修改 `DATABASE_URL` 指向的数据库，只能在已明确识别的本地开发库执行。

从 `frontend/`：

```bash
pnpm dev
pnpm build
pnpm type:check
pnpm lint:fix
pnpm format
pnpm i18n:check
```

从仓库根目录：

```bash
make check-backend
make check-frontend
make check
```

`check-backend` 运行 Go 测试与 lint；`staticcheck` 仅在本机已安装时运行。`check-frontend` 运行 type check、production build 与 i18n key 检查。`make check` 还会检查本地 PostgreSQL/Redis 可达性。

## 当前可以安全验证

- setup 状态、管理员登录和已接入的后台页面。
- Project、Device、Command 的当前 HTTP 行为。
- PostgreSQL 中的 Command/simulator/Webhook worker、重试、恢复、签名和审计行为。
- 使用本地 `httptest` 伪造 Provider 的 WWTIOT HTTP 请求映射。
- 前端类型、构建和双语 key 一致性。

这些检查证明本地持久化、模拟器主链和 Webhook dispatcher 的代码行为，不证明目标部署网络、某个外部 Webhook endpoint、真实 WWTIOT 服务或智能锁执行结果。

## 目标验收路径

目标最终需要验证：

```text
Shared-bicycle Project
  -> Open API
  -> persistent Command and Attempt
  -> unified Gateway/Provider
  -> trustworthy device result
  -> persistent State and Event
  -> signed Webhook delivery
  -> consistent Audit and admin diagnosis
```

模拟器已通过同一持久 Command、Attempt、Event、Delivery 与 dispatcher 链路执行 [API 合同冻结的受控 Provider outcome](./api-contract.md#simulator-配置)，且不扩展为设备 ACK 或 final result 模式。该主链的本地验收不能替代真实设备证据。

真实 WWTIOT 验收需要受控凭据、隔离测试设备、厂商执行结果合同和明确的真实设备写操作授权。缺少任一条件时保持 Unknown，不调用真实 Cloud API 写接口。

## 不由本文承诺

生产部署、Nginx、容器化、CI/CD、设备直连、电子围栏及其他未确认能力，不因出现在本地配置、schema 或代码草稿中而成为当前完成项。
