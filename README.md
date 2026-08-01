# Device Platform

Device Platform 是由一名管理员维护的 IoT 设备接入与控制平台。当前唯一目标是依据共享单车智能锁的真实接入和使用，形成可实际运行、可持久化、状态可信的核心链路；通用性来自已确认的平台边界和统一 Gateway/Provider 接入方式，不来自对未来业务的预建。

当前产品范围只有一个共享单车 Project 和一个 `smart-lock` Device Type；WWTIOT cloud API 与 Omni direct-device TCP 都是当前必须接入的 Provider。当前代码已经让两者经过同一持久 Device、Command、Attempt、Result、DeviceState、Event、Webhook 和 Audit 模型：WWTIOT 下行最多证明 `provider_accepted`，Omni 当前只安全启用 `query_status` 并最多证明 `transport_sent`。真实 callback/设备身份、Device ACK、final result 与锁体行为仍无验收证据，详见 [Current State](./docs/current-state.md)。

## 仓库结构

```text
.
├── backend/    Go API service
├── frontend/   Vue 3 Admin9 frontend
└── docs/       当前产品与工程合同
```

## 文档入口

按以下顺序阅读：

1. [Platform Boundary Contract](./docs/platform-boundary-contract.md)：什么属于平台，什么属于业务应用。
2. [Platform Target Contract](./docs/platform-target-contract.md)：当前真实目标与完成定义。
3. [Domain Model Contract](./docs/domain-model-contract.md)：对象、关系、不变量、事务边界和恢复责任。
4. [API Contract](./docs/api-contract.md)：接口、认证和生命周期语义。
5. [Current State](./docs/current-state.md)：`2026-08-01` 的双 Provider 实现事实、验证证据与真实验收缺口。
6. [Local Development](./docs/local-development.md)：启动、检查和当前可验证范围。

历史交付过程由 Git 保存，不作为当前权威文档。下级文档、schema、模板菜单和当前代码不能反向扩大产品边界。

当前真实接入的从属合同：

- [smart-lock Device Type Contract](./docs/device-types/smart-lock.md)：规范化智能锁能力与已确认安全属性。
- [WWTIOT Provider 合同](./docs/providers/wwtiot.md)：V1.1/V2 厂商资料、当前 V2 实现映射、confirmation level 与真实设备验收 Unknown。
- [Omni Provider 合同](./docs/providers/omni.md)：两个直连协议 profile、当前安全映射、session/runtime 约束与真实设备验收 Unknown。

两类从属合同均受 Platform Boundary、Platform Target 和通用 API Contract 约束；具体 action 不进入 Platform Core 全局语义。

## 本地开发

从仓库根目录准备本地依赖与环境文件：

```bash
createdb device_platform
make setup-local
make check-services
make check-db
pnpm --dir frontend install
```

分别启动后端与前端：

```bash
make dev-backend
make dev-frontend
```

后端默认地址为 `http://localhost:8080`。前端默认使用 `http://localhost:5173`，并代理相对 `/setup/...` 与 `/v1/...` 请求。

首次运行打开 `http://localhost:5173/setup`，完成后使用 `http://localhost:5173/auth/login`。这些步骤只证明当前本地功能可运行，不代表真实智能锁目标链路已验收。

## 常用检查

从仓库根目录：

```bash
make check-backend
make check-frontend
make check
```

从 `backend/`：

```bash
make build
make test
make test-int
make lint
make migrate-up
make migrate-down
```

从 `frontend/`：

```bash
pnpm dev
pnpm build
pnpm type:check
pnpm lint:fix
pnpm format
pnpm i18n:check
```

具体依赖、命令含义和安全验证边界见 [Local Development](./docs/local-development.md)。

## 代码位置

- 后端入口与 handlers：`backend/cmd/server/`
- 核心设备与命令逻辑：`backend/internal/deviceservice/`、`backend/internal/commandservice/`、`backend/internal/commandworker/`
- Provider adapter：`backend/internal/cloudapi/wwtiot/`、`backend/internal/directdevice/omni/`、`backend/internal/simulator/`
- Webhook/Audit：`backend/internal/webhookaudit/`
- migration 与 storage contracts：`backend/internal/storage/`
- 前端 API 与页面：`frontend/src/api/`、`frontend/src/views/`
- 当前产品与工程合同：`docs/`

行为、接口、数据库语义或完成状态变化时，应同步更新对应文档层级，不用实施顺序或编号版本替代产品边界。
