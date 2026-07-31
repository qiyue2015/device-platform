---
title: 当前实现状态
snapshot_date: 2026-07-31
status: implementation-snapshot
contract_freeze_revision: 2026-07-31.2
verified_against_code_revision: implementation-unit-2026-07-31
---

# 当前实现状态

本文是 `2026-07-31` 对包含本文件的 Git revision 的状态快照，用于区分已经实现的事实、局部实现和缺口。它不是产品合同，不能扩大或缩小[平台边界合同](./platform-boundary-contract.md)或[当前目标合同](./platform-target-contract.md)。

状态含义：

- **已实现**：运行时主链已接入，并有代码或测试证据。
- **部分实现**：存在可运行部分，但核心语义或链路未闭合。
- **仅合同或 schema**：存在类型、接口或表结构，没有运行时接入。
- **未实现**：当前代码未提供目标能力。
- **Unknown**：仅凭仓库与安全本地检查不能确认，需要外部条件。

## 能力快照

| 能力                              | 状态             | 已验证事实                                                                                                                                                                                                      |
| --------------------------------- | ---------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 安装与单管理员认证                | 已实现           | 安装使用 PostgreSQL migration；JWT 固定 issuer/audience/jti/session generation，每次受保护请求回查数据库，logout 原子失效旧 token，login 持久限流且 login/refresh/logout 写安全审计；`backend/cmd/server/auth.go`。 |
| Project、Device、Command HTTP API | 部分实现         | 已安装运行时的 Project 与 Device 管理/Open 读取已接入 PostgreSQL；Command HTTP 仍使用 `devicecore` 内存服务，尚未接入冻结的持久 Command aggregate。                                                                |
| 业务数据持久化                    | 部分实现         | Project、凭据版本、Device、Device 创建/lifecycle Event、初始 Webhook Delivery 与对应 Audit 已注入 HTTP 运行时；Command、RawMessage、通用 Event/Webhook 查询和 worker、simulator config 尚未统一接入。               |
| Project 机器 API                  | 已实现           | 已安装运行时按 `X-API-Key` 的 SHA-256 digest 查找 Project，只信任 direct peer 执行 IPv4/IPv6 whitelist；普通 DTO 不返回 key、digest、Webhook secret、ciphertext 或 nonce，轮换提交后旧 key 立即失效。             |
| Project 数据隔离                  | 部分实现         | Project 认证、配置及 Admin/Open Device 查询已使用 Project-scoped PostgreSQL 查询和 schema 归属约束；Open Command 仍属于旧内存主链，尚未完成同一持久事务边界。                                                    |
| WWTIOT 下行                       | 部分实现         | V2 action 映射、固定 10 秒 client、严格请求/响应、echo、失败分类、证据等级与脱敏摘要已有组件测试；持久 dispatcher 尚未调用它，因此不代表命令主链或设备成功。                                                       |
| WWTIOT callback                   | 部分实现 / Unknown | 已有 64 KiB 严格 decoder、字段校验、identity 映射和规范化候选组件；公开 `POST /v1/provider-callbacks/wwtiot` 固定 503 且不读取 body。签名顺序、防重放与命令关联仍为 Unknown。                                    |
| Capability 分层                   | 部分实现         | `smart-lock` revision 1 profile 与 Device Type 读取已接入持久运行时；旧 Command HTTP 仍在 `devicecore` 硬编码 action/策略，尚未使用发布 profile。                                                                 |
| 设备最终执行语义                  | 未实现           | HTTP 创建 Command 不再同步调用 WWTIOT 或伪造 `acked/success`；持久 dispatcher、结果观察与可信设备 final evidence 尚未接入，真实设备最终执行仍无证据。                                                          |
| 模拟器核心链路                    | 部分实现         | 独立 simulator engine 有状态机，但主应用只注册 `gateway.Handler.RegisterSimulator` 提供的配置路由；正常业务 Command 不进入该 engine。                                                                                                                                        |
| Event、Webhook、Audit             | 部分实现         | Auth/Project/Device Audit 已持久化；Device 创建与 lifecycle 在同一事务写 Event、配置存在时写初始 Delivery。通用查询、Delivery worker、Attempt 与 manual replay HTTP 仍使用独立内存服务。                           |
| Webhook 配置                      | 部分实现         | `/v1/projects` 是唯一配置入口，旧 `/v1/projects/webhook-endpoints` 写入口已移除；URL、AES-256-GCM secret 版本与 Audit 已持久化，但 Event/Delivery worker 尚未读取该 Project 配置。                                  |
| 命令超时与离线恢复                | 部分实现         | 领域方法和独立 simulator 逻辑存在；业务 Command 没有周期扫描执行器，状态可能长期悬挂。                                                                                                                          |
| 管理后台                          | 部分实现         | 已有 Project、Device、Command、Webhook、Audit 和 simulator 页面/API 调用；部分字段、动作与后端不一致。                                                                                                          |
| 请求关联与分页                    | 部分实现         | envelope 固定输出全部字段；request ID 已贯穿 header、响应、日志和持久 Auth/Project/Device Audit。Project、Device Type、Provider、Admin/Open Device 已实现严格分页与冻结排序，其余列表仍待接入。                    |

## 关键实现漂移

### 持久化与模型

- PostgreSQL 当前承载 migration、管理员认证、Project aggregate 和 Device aggregate。Project/Device HTTP 已可跨应用实例重建读取；Command、通用 Event/Webhook HTTP 与 worker、simulator 运行态仍由旧内存服务维护并会在重启后丢失。
- `internal/domain`、`internal/api/v1` 和 `internal/devicecore` 三套模型仍并存；Project/Device HTTP 已使用冻结 DTO 与持久领域服务，Command HTTP 仍使用 `devicecore`，不能把 Repository 测试误写为 Command 运行时行为。
- Project API key 只以 SHA-256 digest 持久化，明文只在创建或轮换成功响应出现；Webhook secret 使用独立部署密钥进行 AES-256-GCM 版本化加密。已安装运行时缺少或错误配置 `WEBHOOK_SECRET_ENCRYPTION_KEY` 时会失败关闭。
- Device HTTP 已执行非 deleted Provider identity 全局唯一、deleted 后释放、稳定 lifecycle、可信 `current_state` 派生读取和 Project scope；创建 Event、初始 Delivery 与 Audit 的任一写入失败都会整体回滚。
- Provider registry 固定按 `code ASC` 暴露 `simulator` 与 `wwtiot`，返回三层 `integration_status` 且不暴露配置或 secret。当前合同下 WWTIOT 最多为 `configured_unverified`，不能仅凭配置提升为 `verified`。
- 对齐后的 schema 与 Repository 已有 Webhook Delivery Attempt、manual replay 关联、worker lease、可降低至 `1..5` 次的部署级 Attempt 上限和过期恢复；`004_webhook_delivery_attempt_limit` 的 down migration 在存在低于 5 次即 `dead` 的数据时拒绝回滚。schema 也已有 confirmation/evidence 和 Command `unknown` 字段与约束；`003_command_transport_failure_timing` 允许 dispatching 后可证明未发送的 transport failure 保留 `sent_at` 并终止为 `failed`，其 down migration 同样在旧合同无法表达当前数据时拒绝回滚。当前内存运行时尚未使用这些结构。
- 单管理员认证已执行 JWT/session generation、持久登录限流和安全审计合同；setup 完成后 POST 返回 `409 setup_completed`，安装请求不再为必需字段填默认值，连接测试在实际连接前区分 URL schema 错误。HTTP JSON object 统一拒绝未知字段、重复 key、尾随值与超限 body。
- Project、Device Type、Provider 和 Device list 已提供严格 `page`、`page_size`、`total`、重复/未知 query 拒绝与冻结排序；Command、Event、Webhook Delivery 和 Audit list 尚未全部对齐该合同。

### 命令与 Provider

- WWTIOT client 是可由未来 dispatcher 调用的单次下行组件；它严格限制请求、响应大小、JSON object、`result`、必需 echo、redirect、超时和脱敏摘要，并按 `WroteRequest` 区分发送前后 transport failure。当前没有自动重试，也没有接入可信上行结果。
- `devicecore` 当前硬编码具体 action 及其策略，尚未实现 Core / Device Type Capability 的责任分离；这是实现漂移，不是新的产品合同。
- `devicecore` 仍包含 `created`、`offline`、`online_only`、`queue_until_expire`、`replace_latest` 和离线恢复/补偿分支；当前冻结 profile 只使用 `queued` 起始状态与 `dispatch_once`，旧分支不得被当作当前产品合同。
- Provider HTTP acceptance 组件只产生 `provider_accepted/unverified`，不会生成 Device ACK/final；旧 HTTP Command 创建也不再同步调用 Provider。
- 模拟器与 `devicecore` 使用两套独立 Command map 和状态机；切换 simulator mode 不会使后台或 Open API 创建的模拟设备命令完成。
- 独立 simulator engine 的 `success`、`failure`、`timeout`、`offline_then_online` 等旧模式把组件自建状态机结果当成设备 ACK/final 语义；它们不是冻结 simulator Provider 合同，接入统一主链时必须替换，不能兼容为第二套模式。
- Command 超时有领域/Repository 方法，但没有冻结 Command 主链的 dispatcher、deadline scanner 和 dispatching crash recovery 调用方；因此不能验收命令生命周期闭环。
- 持久 Device 与旧内存 Command 服务尚未统一，不能据旧服务行为证明 disabled/deleted Device、Provider 配置或发布 profile 的 Command 创建约束。

### Webhook、审计与前端

- Device HTTP 已在持久事务内写 Event、初始 Webhook Delivery 和 Audit；Command HTTP 及通用 Event/Webhook/Audit 管理路由仍使用独立内存服务，尚未形成完整统一主链。
- Webhook retry worker 每 500ms 扫描进程内到期记录，仍未使用 PostgreSQL Delivery/Attempt、领取 lease 或恢复接口；因此当前运行时在进程重启后仍会丢失记录与重试进度，dead resend 仍会重置同一内存记录的计数。Repository 中已验证的独立 replay 历史不能被写成当前 HTTP 行为。
- Webhook 当前签名只覆盖 body，不含 timestamp；未配置 secret 时使用固定本地 fallback，且没有独立 DeliveryAttempt 记录。
- Project Webhook 配置已收口到 Project aggregate；旧并行写入口不可用。当前缺口是持久 Event/Delivery 创建与 worker 仍未使用该配置和版本化 secret resolver。
- `/v1/events` 当前允许管理员直接注入任意 Event，与冻结目标的只读事件资源不一致。
- 前端 Audit 类型期望 `actor_id`、`ip`、`summary`，后端返回 `actor_type`、`ip_address`、`metadata`。
- 后端具备部分 cancel/online 操作，但当前页面没有完整暴露；部分命令失败字段也存在前后端命名漂移。

## 已有测试能证明什么

- Go 测试覆盖持久 Project HTTP 生命周期、重启读取、一次性凭据披露、凭据轮换、IP whitelist、并发与回滚；也覆盖持久 Device 创建/读取/过滤/lifecycle、Project 隔离、Provider identity 释放、Device/Event/Delivery/Audit 原子回滚、Provider registry，以及 WWTIOT client/callback 的无真实设备组件合同。PostgreSQL integration tests 另覆盖 Command 与 Webhook Repository 的并发、lease、恢复和证据约束。
- 前端类型检查、i18n 检查和构建可验证静态一致性。
- 这些检查不能证明持久 Command dispatcher、simulator 主链、Webhook worker、公开可信 callback 或真实智能锁最终执行；WWTIOT HTTP 测试服务器也不能替代真实设备证据。

## Unknown 与验证条件

| Unknown                                                                               | 验证条件                                                                                                                           |
| ------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| 当前 WWTIOT 凭据与设备配置是否有效，以及生产响应含义                                  | V2 下行请求签名算法已与资料互证；仍需厂商确认、受控凭据和禁止泄密的联调环境验证真实服务行为。                                      |
| 真实设备收到、执行、拒绝和延迟回执的行为                                              | 隔离测试设备、明确写操作授权和可观测的设备侧证据。                                                                                 |
| V2 资料描述的设备信息 callback 在当前账号下是否可用，以及能否作为可关联的最终执行结果 | 厂商确认回调配置与签名规则，并用受控真实设备验证送达、关联、去重和结果终局性；详见 [WWTIOT Provider 合同](./providers/wwtiot.md)。 |
| WWTIOT 实际可观测的最高 confirmation level，以及该层级是否足以支撑共享单车正式业务    | 厂商合同、受控真实设备端到端证据，以及产品所有者基于证据的验收决定。                                                               |
| 生产网络、TLS、限流、配额和重试安全性                                                 | 目标部署网络与厂商限制说明。                                                                                                       |
| 多实例下完整 HTTP/worker 主链的幂等、顺序和恢复行为                                  | 现有 Repository 并发用例通过后，仍需在 worker 接入阶段进行进程重启与故障注入测试。                                                  |

不在本文记录本机数据库内容、真实密钥或真实业务数据；它们既敏感，也不能作为长期实现事实。
