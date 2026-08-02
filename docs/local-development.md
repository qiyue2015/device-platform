---
title: 本地开发与验证
updated: 2026-08-02
status: operational-guide
---

# 本地开发与验证

本文说明当前仓库的本地启动、测试和安全边界，不定义产品合同或真实设备完成状态。完成门槛见 [Platform Target](./platform-target-contract.md)，当前实现与 Unknown 见 [Current State](./current-state.md)。

## 本地依赖

| Service     | 默认地址或用途                                      |
| ----------- | --------------------------------------------------- |
| PostgreSQL  | `localhost:5432`，权威领域事实与 worker lease       |
| Redis       | `localhost:6379`，认证限流和可重建运行依赖          |
| Backend API | `http://localhost:8080`                             |
| Frontend    | `http://localhost:5173`，端口占用时以 Vite 输出为准 |
| NATS        | 当前代码未实现，不是本地启动依赖                    |

从仓库根目录准备忽略提交的环境文件并检查服务：

```bash
createdb device_platform
make setup-local
make check-services
make check-db
pnpm --dir frontend install --frozen-lockfile
```

`make setup-local` 不覆盖已有文件。只使用本地测试凭据；真实 Provider secret、真实 IMEI、raw frame 和未脱敏业务数据不得进入仓库、日志、测试夹具或截图。

## Provider 配置

WWTIOT 使用 `WWTIOT_API_URL`、`WWTIOT_USER_ID` 和 `WWTIOT_USER_KEY`。缺失凭据时 registry 仍可只读展示 Provider，但 `integration_status=unconfigured`，Command 创建/派发失败关闭。普通本地测试使用 `httptest` fake Provider，不需要真实凭据，也不授权调用真实 Cloud API。

Omni 两个 profile listener 必须同时配置或同时留空：

```text
OMNI_BIKE_LISTEN_ADDR=
OMNI_IOT_LISTEN_ADDR=
OMNI_MAX_FRAME_BYTES=4096
OMNI_MAX_CONNECTIONS=256
OMNI_READ_TIMEOUT=5m
```

普通开发默认把两个地址留空，Provider 为 `unconfigured`。需要运行本地 TCP peer 测试时，只绑定明确的 loopback 测试端口；不要暴露到公网，也不要接入真实设备。只配置一个地址、非法 frame/connection limit/read timeout，或任一 listener 运行时非预期退出都会失败关闭双 profile runtime。该 fatal 状态使 `/readyz` 和业务 API 返回 `503 provider_runtime_unavailable`，需要排除故障并重启完整运行时，不能只继续使用剩余 profile。

## 启动

分别运行：

```bash
make dev-backend
```

```bash
make dev-frontend
```

存活与就绪检查：

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

`/healthz` 只证明进程存活。首次安装前 `/readyz` 返回 setup required；Omni 双 profile runtime 降级后即使 HTTP 进程仍存活也必须 unready。

浏览器使用：

```text
http://localhost:5173/setup
http://localhost:5173/auth/login
```

setup 完成后使用本地管理员登录。前端开发环境通过 Vite 代理相对 `/setup/...` 与 `/v1/...` 请求到后端；通常保持 `VITE_API_BASE_URL=''`。

## 后端检查

从 `backend/`：

```bash
make build
make test
make test-int
make lint
```

PostgreSQL integration 只读取 `MIGRATION_TEST_DATABASE_URL`，数据库名保护规则要求 `_test` 后缀。必须使用归属明确、专用且可丢弃的本地测试数据库；不得因为另一个 `_test` 数据库已经存在就复用、清空或迁移它：

```bash
createdb device_platform_local_test
MIGRATION_TEST_DATABASE_URL='postgres://local_user:local_password@127.0.0.1:5432/device_platform_local_test?sslmode=disable' \
MIGRATION_TEST_REDIS_URL='redis://127.0.0.1:6379/0' \
make test-int
```

各用例在该数据库创建随机 schema 并在结束时删除。不要将 URL 指向 `postgres` 管理库、开发业务库、共享测试库或生产数据库。未设置环境变量时 integration test 会明确跳过；跳过不能记为 PostgreSQL/Redis 验收通过。

`make migrate-up` / `make migrate-down` 会修改 `DATABASE_URL` 指向的数据库。只允许在已核对的本地开发库执行；对已有权威历史先按 migration 注释和专项测试执行 fail-stop 预检。历史 WWTIOT RawMessage 缺少可证明真实性字段时，009 只允许保守回填 `unverified`，不得提升为 `verified`。

Omni 关键定向检查：

```bash
go test -race ./internal/directdevice/omni -run 'TestRuntime|TestListener|TestRegistry|TestAdapter|TestParse|TestDecoder' -count=1
go test -race ./cmd/server -run 'TestOmniRuntimeFailureSignalsFatalAndMakesAppUnready' -count=1
```

这些测试使用内存 writer、loopback TCP peer 或故障 listener，不连接真实设备。

## 前端检查

从 `frontend/`：

```bash
pnpm type:check
pnpm i18n:check
pnpm lint:fix
pnpm build
```

可见页面变更还需在本地服务上用 ego-browser 验收 Provider、Device 与 Command 页面。当前仓库没有前端单元测试 runner；类型检查、lint、i18n、生产构建和浏览器 smoke 是本地证据，但不证明真实 Provider。

## 当前允许验证

- setup、唯一超级管理员与普通 User 认证、User 启停、Project manager 分配/转交、Project 范围的 Device/Command/Event/Webhook/Audit 授权，以及停用 token 失效和高风险操作超级管理员门禁。
- WWTIOT V2 请求映射、响应分类、timeout、恢复与 callback 固定失败关闭。
- Omni 两 profile codec、listener/session、RawMessage/Audit、query_status 单次写入、timeout 和 runtime fatal 行为。
- Project、Device、Provider/profile 隔离，以及 Event/Webhook/Audit 的持久事务与恢复。
- simulator 通过同一持久主链产生受控 Provider 层结果。

这些检查不能证明真实 WWTIOT 服务状态、Omni 设备/profile 对应、TCP peer 真实性、设备收到/执行、Device ACK、device final 或真实锁体状态。

## 真实设备禁区

没有受控设备、现场观察、恢复方案和逐次明确授权时，不执行真实 `query_status`、`unlock` 或 `lock`。timeout、网络中断、身份不一致或结果 Unknown 时，不自动重试任何真实写操作。真实验收开始前至少需要：

- WWTIOT 受控凭据、正式响应/callback 规则和隔离智能锁；
- Omni 型号/固件/profile 对照、受控网络身份方案和隔离智能锁；
- 每次动作的明确授权、可观察锁体状态、现场恢复负责人和停止条件；
- 脱敏证据模板，能关联 Project、Device、Provider/profile、Command、Attempt、Result、Event、Webhook 和 Audit。

当前运行时使用 PostgreSQL polling/lease，不需要 NATS；目标 NATS JetStream Outbox/Inbox 未实现，相关消息重复、Ack、redelivery 和 Broker 恢复不能记录为已通过。
