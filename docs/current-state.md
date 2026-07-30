---
title: 当前实现状态
snapshot_date: 2026-07-31
status: implementation-snapshot
contract_freeze_revision: 2026-07-31.1
verified_against_code_revision: 1938e41630a8e0c88db906bcf0f111a261dd3ef4
---

# 当前实现状态

本文是 `2026-07-31` 对代码 revision `1938e41630a8e0c88db906bcf0f111a261dd3ef4` 的状态快照，用于区分已经实现的事实、局部实现和缺口。它不是产品合同，不能扩大或缩小[平台边界合同](./platform-boundary-contract.md)或[当前目标合同](./platform-target-contract.md)。

状态含义：

- **已实现**：运行时主链已接入，并有代码或测试证据。
- **部分实现**：存在可运行部分，但核心语义或链路未闭合。
- **仅合同或 schema**：存在类型、接口或表结构，没有运行时接入。
- **未实现**：当前代码未提供目标能力。
- **Unknown**：仅凭仓库与安全本地检查不能确认，需要外部条件。

## 能力快照

| 能力                              | 状态             | 已验证事实                                                                                                                                                                                                      |
| --------------------------------- | ---------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 安装与管理员登录                  | 已实现           | 安装流程使用 PostgreSQL migration，并从数据库校验管理员；`backend/cmd/server/app.go:32`、`backend/cmd/server/auth.go:200`。                                                                                     |
| Project、Device、Command HTTP API | 部分实现         | 路由与内存领域服务已接入；业务对象保存在 `map`，见 `backend/cmd/server/app.go:47`、`backend/internal/devicecore/service.go:39`。                                                                                |
| 业务数据持久化                    | 仅合同或 schema  | 核心表和 Repository 接口存在，但未发现 Repository 实现或运行时注入；`backend/internal/storage/repository/contracts.go:9`。                                                                                      |
| Project 机器 API                  | 部分实现         | `/v1/open/*` 校验 `X-API-Key`，但运行时保存并遍历明文 key，Project 对象持续返回明文，已配置的 IP whitelist 未在认证中执行；`backend/internal/devicecore/types.go:5`、`backend/internal/httpapi/router.go:311`。 |
| Project 数据隔离                  | 部分实现         | 运行时查询使用 Project 上下文；数据库外键没有保证 Command 的 `project_id` 与 Device 的 Project 一致。                                                                                                           |
| WWTIOT 下行                       | 部分实现         | 当前适配器事实与 action 映射见 [WWTIOT Provider 合同](./providers/wwtiot.md)；运行时入口为 `backend/internal/cloudapi/wwtiot/client.go:76`。                                                                    |
| WWTIOT callback                   | 未实现 / Unknown | V2 原始资料存在，但仓库没有 callback 路由、签名验证、RawMessage/DeviceState 接入或命令关联实现；签名顺序与命令关联仍需外部确认。                                                                                |
| Capability 分层                   | 部分实现         | 运行时把具体 action 作为全局 `command_type` 校验，并在核心服务中硬编码默认策略；`backend/internal/devicecore/service.go:627`。Provider 映射位于 adapter，但最小 Device Type Capability profile 尚未接入运行时。 |
| 设备最终执行语义                  | 未实现           | WWTIOT 2xx 且 `result=ok` 后立即 `sent -> acked -> success`，没有设备回执或状态同步；`backend/cmd/server/command_dispatch.go:89`。                                                                              |
| 模拟器核心链路                    | 部分实现         | 独立 simulator engine 有状态机，但主应用只注册配置路由；`backend/cmd/server/app.go:109`、`backend/internal/gateway/http.go:21`。正常业务 Command 不进入该 engine。                                              |
| Event、Webhook、Audit             | 部分实现         | 存在 HTTP/UI 能力和进程内 retry worker，但服务使用另一组内存 map；`backend/internal/webhookaudit/service.go:31`、`backend/cmd/server/app.go:167`。没有持久 Outbox、多实例领取锁或重启恢复保证。                 |
| Webhook 配置                      | 部分实现         | Project CRUD 的 `webhook_url` 与 `/v1/projects/webhook-endpoints` 属于两套状态；`backend/internal/devicecore/service.go:65`、`backend/internal/webhookaudit/service.go:65`。                                    |
| 命令超时与离线恢复                | 部分实现         | 领域方法和独立 simulator 逻辑存在；业务 Command 没有周期扫描执行器，状态可能长期悬挂。                                                                                                                          |
| 管理后台                          | 部分实现         | 已有 Project、Device、Command、Webhook、Audit 和 simulator 页面/API 调用；部分字段、动作与后端不一致。                                                                                                          |
| 请求关联与分页                    | 未实现           | envelope 定义 `request_id`，但响应写入没有生成或贯穿 request ID；所有列表仍返回完整集合，`meta` 为空。                                                                                                          |

## 关键实现漂移

### 持久化与模型

- PostgreSQL 当前承载 migration 与管理员认证，但 Project、Device、Command、Attempt、Event、Webhook 和 Audit 运行态仍在进程内存，重启会丢失业务状态。
- `internal/domain`、`internal/api/v1` 和 `internal/devicecore` 三套模型并存；实际 HTTP 主链使用 `devicecore`，其余模型不能证明运行时行为。
- schema 定义 `projects.api_key_hash`，运行时对象却保存并返回明文 `api_key`。
- schema 只按 Project 限制 Provider device identity；WWTIOT callback 不含 Project，当前约束无法保证无歧义映射。
- Provider registry 的 WWTIOT code/name/timeout 可被环境变量改变，且运行时 Device Type 使用 `smart_lock`；这些行为分别偏离冻结的 `wwtiot`、`WWTIOT`、10 秒与 `smart-lock` 稳定发布元数据。
- Provider 读取当前只返回布尔 `configured`，尚未实现 `integration_status` 的三层证据语义；simulator 也未作为统一 Provider registry 项接入业务 Device/Command 主链。
- schema 没有 Webhook Delivery Attempt、manual replay 关联、worker lease、confirmation level 或 Command `unknown` 状态所需字段。
- 管理员 JWT 当前只校验签名和到期时间；refresh 用仍有效 token 续签，logout 不做服务端失效，schema 也没有 `session_generation`。
- login 当前没有持久失败计数或限流；setup 的 POST 完成后返回 `403 setup_forbidden`，请求 decoder 也没有执行冻结合同的严格未知字段、重复 key 与尾随 JSON 检查。
- 所有列表接口返回完整集合，当前没有服务端分页。

### 命令与 Provider

- WWTIOT 适配器是同步、单次、仅下行调用，没有自动重试、回调、上行消息或设备状态同步；具体实现事实见 [WWTIOT Provider 合同](./providers/wwtiot.md)。
- WWTIOT client 对空 body、非 JSON 和缺少字符串 `result` 的 2xx 可能返回成功；没有校验 response echo 或响应签名。
- `devicecore` 当前硬编码具体 action 及其策略，尚未实现 Core / Device Type Capability 的责任分离；这是实现漂移，不是新的产品合同。
- `devicecore` 仍包含 `created`、`offline`、`online_only`、`queue_until_expire`、`replace_latest` 和离线恢复/补偿分支；当前冻结 profile 只使用 `queued` 起始状态与 `dispatch_once`，旧分支不得被当作当前产品合同。
- Provider HTTP 接受目前被等同于设备执行成功，这是当前最高优先级的状态可信性缺口。
- 模拟器与 `devicecore` 使用两套独立 Command map 和状态机；切换 simulator mode 不会使后台或 Open API 创建的模拟设备命令完成。
- 独立 simulator engine 的 `success`、`failure`、`timeout`、`offline_then_online` 等旧模式把组件自建状态机结果当成设备 ACK/final 语义；它们不是冻结 simulator Provider 合同，接入统一主链时必须替换，不能兼容为第二套模式。
- Command 超时有领域方法，但没有业务主链的定时扫描调用方；Cloud 调用失败也没有恢复任务。
- Device `lifecycle_status=disabled` 当前没有阻止新 Command。

### Webhook、审计与前端

- Event、Webhook Delivery 和 Audit 由独立内存服务维护，不与 Command 持久事务绑定。
- Webhook retry worker 每 500ms 扫描进程内到期记录，但进程重启后记录与重试进度都会丢失；当前 dead resend 会重置同一记录的计数，不能保留独立重发历史。
- Webhook 当前签名只覆盖 body，不含 timestamp；未配置 secret 时使用固定本地 fallback，且没有独立 DeliveryAttempt 记录。
- Project 表单只写 `/v1/projects` 的 `webhook_url`，不会配置 `/v1/projects/webhook-endpoints` 使用的另一套端点。
- `/v1/events` 当前允许管理员直接注入任意 Event，与冻结目标的只读事件资源不一致。
- 前端 Audit 类型期望 `actor_id`、`ip`、`summary`，后端返回 `actor_type`、`ip_address`、`metadata`。
- 后端具备部分 cancel/online 操作，但当前页面没有完整暴露；部分命令失败字段也存在前后端命名漂移。

## 已有测试能证明什么

- Go 测试覆盖部分 Project/Device/Command 规则、幂等、离线策略、simulator engine，以及用本地 `httptest` 伪造的 WWTIOT HTTP 接受路径。
- 前端类型检查、i18n 检查和构建可验证静态一致性。
- 这些检查不能证明业务数据持久化、模拟器主链闭环、Webhook 可靠投递或真实智能锁最终执行。

## Unknown 与验证条件

| Unknown                                                                               | 验证条件                                                                                                                           |
| ------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| 当前 WWTIOT 凭据与设备配置是否有效，以及生产响应含义                                  | V2 下行请求签名算法已与资料互证；仍需厂商确认、受控凭据和禁止泄密的联调环境验证真实服务行为。                                      |
| 真实设备收到、执行、拒绝和延迟回执的行为                                              | 隔离测试设备、明确写操作授权和可观测的设备侧证据。                                                                                 |
| V2 资料描述的设备信息 callback 在当前账号下是否可用，以及能否作为可关联的最终执行结果 | 厂商确认回调配置与签名规则，并用受控真实设备验证送达、关联、去重和结果终局性；详见 [WWTIOT Provider 合同](./providers/wwtiot.md)。 |
| WWTIOT 实际可观测的最高 confirmation level，以及该层级是否足以支撑共享单车正式业务    | 厂商合同、受控真实设备端到端证据，以及产品所有者基于证据的验收决定。                                                               |
| 生产网络、TLS、限流、配额和重试安全性                                                 | 目标部署网络与厂商限制说明。                                                                                                       |
| 多实例下的幂等、顺序和恢复行为                                                        | 持久 Repository/worker 接入后进行并发、重启和故障注入测试。                                                                        |

不在本文记录本机数据库内容、真实密钥或真实业务数据；它们既敏感，也不能作为长期实现事实。
