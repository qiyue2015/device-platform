---
title: 当前实现状态
snapshot_date: 2026-07-31
status: implementation-snapshot
contract_freeze_revision: 2026-07-31.3
verified_against_code_revision: contract-freeze-2026-07-31.3-pre-implementation
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

| 能力                     | 状态               | 已验证事实                                                                                                                                                                                                                                                                                                   |
| ------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 安装与单管理员认证       | 部分实现           | 安装使用 PostgreSQL migration，认证、session generation、持久限流和安全审计已实现；但 `.3` 要求的独立跨进程锁文件、数据库 advisory lock 与 users 单例约束、持久 recovery journal、唯一临时文件、marker 前完整运行时预构造和 HTTP readiness 失败关闭尚未实现。                                                |
| Project、Device HTTP API | 已实现             | 已安装运行时的 Project 与 Device 管理/Open 读取已接入 PostgreSQL，并执行冻结的字段、隔离、分页、lifecycle 与凭据边界。                                                                                                                                                                                       |
| Command HTTP API         | 已实现             | Admin/Open 创建、列表、详情和 queued cancel 已接入持久 Command aggregate；创建执行 profile/payload、Provider 配置、Project scope 与持久幂等校验，只提交 `queued`，不在 HTTP 请求内调用 Provider。                                                                                                            |
| 业务数据持久化           | 部分实现           | Project、凭据版本、Device、Command/Attempt、Simulator 配置、Event、Webhook Delivery/Attempt、Audit 与 worker 进度已接入 PostgreSQL 运行时；公开 callback 仍按冻结合同关闭，因此 RawMessage 尚未进入运行时主链。                                                                                              |
| Project 机器 API         | 已实现             | 已安装运行时按 `X-API-Key` 的 SHA-256 digest 查找 Project，只信任 direct peer 执行 IPv4/IPv6 whitelist；普通 DTO 不返回 key、digest、Webhook secret、ciphertext 或 nonce，轮换提交后旧 key 立即失效。                                                                                                        |
| Project 数据隔离         | 已实现             | Project 认证、配置以及 Admin/Open Device 和 Command 查询/写入均使用 Project-scoped PostgreSQL 校验；Open API 不能用 body、header 或 query 切换 Project，跨 Project 资源按不可见处理。                                                                                                                        |
| WWTIOT 下行              | 部分实现           | 持久 dispatcher 已按冻结 profile 调用 V2 adapter；固定 10 秒 client、严格请求/响应、echo、失败分类、证据等级与脱敏摘要已有组件和集成测试。仅证明代码层投递链，不代表真实服务接受、设备 ACK 或设备成功。                                                                                                      |
| WWTIOT callback          | 部分实现 / Unknown | 已有 64 KiB 严格 decoder、字段校验、identity 映射和规范化候选组件；公开 `POST /v1/provider-callbacks/wwtiot` 固定 503 且不读取 body。签名顺序、防重放与命令关联仍为 Unknown。                                                                                                                                |
| Capability 分层          | 已实现             | `smart-lock` revision 1 profile 与 Device Type 读取已接入持久运行时；Command 创建以及 WWTIOT/simulator dispatcher 消费同一持久 profile，派生 action、payload、revision、delivery policy、deadline、Provider timeout 与结果观察期限。                                                                         |
| 设备最终执行语义         | 未实现             | HTTP 创建 Command 不同步调用 WWTIOT；dispatcher 对 Provider HTTP acceptance 只保存 `sent/provider_accepted/unverified`，结果观察超时会结束悬挂状态，但可信 Device ACK/final evidence 尚未接入，真实设备最终执行仍无证据。                                                                                    |
| 模拟器核心链路           | 已实现             | 已安装运行时使用持久 `GET/PATCH /v1/simulator`，并让 simulator Device 的 Command 经过统一 dispatcher、Attempt、Command 状态机、Event、初始 Delivery 与 timeout scanner；只产生受控 Provider 层结果，不产生 Device ACK/final 或 `success`。                                                                   |
| Event、Webhook、Audit    | 部分实现           | 持久 Event、Delivery/Attempt、Audit、dispatcher 和 dead replay 主链已接入；但 `.3` 新冻结的 `command.evidence_updated` 尚未进入 domain、schema、worker 和后台筛选，Provider acceptance 仍使用同状态 `command.status_changed`。                                                                               |
| Webhook 配置             | 已实现             | `/v1/projects` 是唯一配置入口；URL、AES-256-GCM secret 版本与 Audit 已持久化，初始与 replay Delivery 保存配置 snapshot，worker 按 snapshot 解析历史 secret，不使用 fallback。                                                                                                                                |
| 命令超时与崩溃恢复       | 已实现             | 持久 worker 周期处理 dispatch deadline、result observation deadline 与过期 dispatching Attempt；dispatching 崩溃保守转 `unknown/provider_delivery_unknown`，原 Attempt 和 request key 不重放，晚到 worker 结果被 fencing。真实独立 server 进程测试也覆盖共享幂等、worker ownership 与进程终止后 lease 恢复。 |
| 管理后台                 | 部分实现           | Project、Device、Provider/Device Type、Command、Event、Webhook、Audit 和 simulator 页面已接入；除 `.3` 新增的 `command.evidence_updated` 筛选值尚未对齐外，现有 DTO、分页 envelope、状态展示、一次性凭据、lifecycle、profile action、cancel、详情、dead replay 与 Simulator outcome 均按后端事实工作。       |
| 请求关联与分页           | 已实现             | envelope 固定输出全部字段；request ID 已贯穿 header、响应、日志和适用的持久 Audit。Project、Device Type、Provider、Admin/Open Device、Command、Event、Webhook Delivery 与 Audit 已实现严格分页和冻结排序。                                                                                                   |

## 关键实现漂移

### 持久化与模型

- PostgreSQL 当前承载 migration、管理员认证、Project、Device、Command aggregate、Simulator 配置、Event、Webhook Delivery/Attempt、Audit，以及 Command/Webhook worker 进度。已安装运行时的 HTTP、dispatcher、scanner、重试与 lease 恢复均读取持久事实；公开 callback 按冻结合同保持 503，因此 RawMessage 仍未进入运行时主链。
- `internal/domain`、`internal/api/v1` 和 `internal/devicecore` 三套模型仍并存；已安装运行时的 Project/Device/Command HTTP 已使用冻结 DTO 与持久领域服务。未安装生产构造器不再创建旧 `devicecore`、simulator 或 Webhook 内存服务，Open 业务 API 在 setup 完成前固定返回 `503 setup_required`；旧路径只保留为隔离测试兼容，不是生产运行模型。
- Project API key 只以 SHA-256 digest 持久化，明文只在创建或轮换成功响应出现；Webhook secret 使用独立部署密钥进行 AES-256-GCM 版本化加密。已安装运行时缺少或错误配置 `WEBHOOK_SECRET_ENCRYPTION_KEY` 时会失败关闭。
- Device HTTP 已执行非 deleted Provider identity 全局唯一、deleted 后释放、稳定 lifecycle、可信 `current_state` 派生读取和 Project scope；创建 Event、初始 Delivery 与 Audit 的任一写入失败都会整体回滚。
- Provider registry 固定按 `code ASC` 暴露 `simulator` 与 `wwtiot`，返回三层 `integration_status` 且不暴露配置或 secret。当前合同下 WWTIOT 最多为 `configured_unverified`，不能仅凭配置提升为 `verified`。
- 对齐后的 schema 与 Repository 已有 Webhook Delivery Attempt、manual replay 关联、worker lease、可降低至 `1..5` 次的部署级 Attempt 上限和过期恢复；`004_webhook_delivery_attempt_limit` 的 down migration 在存在低于 5 次即 `dead` 的数据时拒绝回滚。schema 也已有 confirmation/evidence 和 Command `unknown` 字段与约束；`003_command_transport_failure_timing` 允许 dispatching 后可证明未发送的 transport failure 保留 `sent_at` 并终止为 `failed`，其 down migration 同样在旧合同无法表达当前数据时拒绝回滚。Command HTTP 与持久 worker 已使用创建、读取、取消、Attempt、Event/Delivery/Audit、幂等、lease 和恢复结构；应用启动、安装切换与关闭会启动、替换并等待 worker 退出后再关闭旧数据库。
- 单管理员认证已执行 JWT/session generation、持久登录限流和安全审计合同；setup 完成后 POST 返回 `409 setup_completed`，安装请求不再为必需字段填默认值，连接测试在实际连接前区分 URL schema 错误。HTTP JSON object 统一拒绝未知字段、重复 key、尾随值与超限 body。
- 首次安装当前只使用进程内 `sync.Mutex`，没有独立跨进程锁文件、PostgreSQL advisory lock 或持久 recovery journal；users 空表检查与 INSERT 不在同一显式数据库事务，schema 也未强制 users 全表单例和 admin-only，完成判断仍接受配置中的 `DEVICE_PLATFORM_INSTALLED`，`.env.tmp`、`.env.restore` 与 `.installed.tmp` 还是固定名称。完成标记写入后仍有可失败的运行时构造，服务与两个 worker 也未作为完整快照一次发布；`/readyz` 在未安装时仍返回 HTTP 200，且不区分必须恢复或重启的旧进程。因此 `.3` 的多进程唯一安装、失败恢复、发布顺序和 readiness 口径尚未达到。
- Project、Device Type、Provider、Device、Command、Event、Webhook Delivery 和 Audit list 均已提供严格 `page`、`page_size`、`total`、重复/未知 query 拒绝与冻结排序。

### 命令与 Provider

- WWTIOT client 是由持久 dispatcher 调用的单次下行 adapter；`Prepare` 在外部 I/O 前冻结脱敏 request summary 和确切请求 body，事务提交 `sent` 后才执行 HTTP。它严格限制请求、响应大小、JSON object、`result`、必需 echo、redirect 和超时，并按 `WroteRequest` 区分发送前后 transport failure。当前没有自动重试，也没有接入可信上行结果。
- `devicecore` 测试兼容路径仍硬编码具体 action 及其策略；生产构造器和已安装 Command HTTP 不再使用该路径，而由持久 Device Type profile 提供 action 与策略。测试兼容代码不是第二套产品合同或生产运行模型。
- `devicecore` 仍包含 `created`、`offline`、`online_only`、`queue_until_expire`、`replace_latest` 和离线恢复/补偿分支；当前冻结 profile 只使用 `queued` 起始状态与 `dispatch_once`，旧分支不得被当作当前产品合同。
- Provider HTTP acceptance 组件只产生 `provider_accepted/unverified`，不会生成 Device ACK/final；旧 HTTP Command 创建也不再同步调用 Provider。
- Provider acceptance 当前在 Command 保持 `sent` 时仍写入 `from=sent,to=sent` 的 `command.status_changed`；`.3` 已冻结为独立 `command.evidence_updated`，但代码、schema、迁移测试和前端筛选尚待对齐。
- 已安装运行时的 simulator 配置与 Command 已统一进入 PostgreSQL 主链。新领取 Attempt 在领取事务内锁定并保存当时的 outcome、delay 与 config version；PATCH 也锁定同一配置行，因此提交顺序决定后续 claim 使用的版本，已领取或重新领取的同一 Attempt 保留原 snapshot 和 request key。
- 生产未安装状态不再启动内存 gateway 或旧 Webhook worker，只开放 setup/health 与明确失败关闭的业务入口。`devicecore` 中 `success`、`failure`、`timeout`、`offline_then_online` 等独立 engine/mode 仅由隔离测试兼容路径使用；它们不是生产事实或第二套产品合同，已安装 `/v1/simulator/gateway` 固定为 `404`。
- `/v1/admin/**` 通配占位处理器已移除；后台只响应冻结合同明确列出的管理路由，未知技术路径返回统一 `404 not_found`，不以占位成功响应暗示能力存在。
- 冻结 Command 主链已有持久 dispatcher、deadline scanner 和 dispatching crash recovery 调用方。claim/preflight、`sent` 事务提交、外部 I/O 与结果事务分离；过期 claimed Attempt 只续领同一 Attempt/request key，过期 dispatching Attempt 不重放并保守转 `unknown`。
- 持久 Command 创建已与 Device、Provider registry 和发布 profile 统一，且 disabled/deleted Device、Provider 配置、payload 与 Project scope 均在落库前校验；本地集成测试能证明 `queued` 到 Provider 结果分类的代码主链，但不能证明真实 WWTIOT 服务或设备执行。

### Webhook、审计与前端

- Device 与 Command HTTP 已在持久事务内写 Event、配置存在时的初始 Webhook Delivery 和 Audit；已安装运行时的 Event、Webhook Delivery/Attempt 与 Audit 管理路由读取相同的 PostgreSQL 事实。Event 与 Audit 没有写入 API。
- schema 与持久 Audit 服务当前仍把从未由运行时写入的 `setup.completed` 视为合法 action；`.3` 已删除该不具备跨文件/数据库原子完成时点的 action，代码和 schema 尚待收紧。
- dead resend 只创建新的持久 `pending` Delivery，使用重发时的当前 endpoint/config/secret version snapshot，通过 `replay_of_delivery_id` 保留来源，并与管理员 `webhook.delivery_replayed` Audit 同事务提交；原 Delivery、raw body snapshot、状态和 Attempts 不变。并发重发各自创建独立历史，endpoint 未配置或非 dead 均失败关闭。
- 已安装运行时只启动持久 Webhook dispatcher；它领取 PostgreSQL Delivery、为每次请求创建 Attempt、按 `1s, 5s, 30s, 2m` 有界重试、恢复过期 lease 并在耗尽后进入 `dead`。未安装兼容模式的旧内存 worker 不再作为安装后事实。
- worker 逐字节复用 Delivery raw body，以 Attempt Unix 秒和历史 secret version 生成 HMAC-SHA256，任意 2xx 进入 `delivered`。响应只保存不含正文值的 media type、长度、截断标记与 digest；secret 解密失败与固定安全 Audit 原子提交。
- dispatcher 禁止 redirect 和环境代理，连接时校验 DNS 返回的每个地址以防 rebinding；默认拒绝非公开、loopback、link-local、multicast 与云 metadata 地址，只有部署 `WEBHOOK_EGRESS_ALLOWLIST` 可显式开放所需内部网段，metadata 固定地址不能由宽网段放行。
- `/v1/events` 与 `/v1/audit-logs` 在已安装持久运行时和未安装兼容路径都严格只读；旧内存 service 的写方法仍是未接入路由的代码残留。
- 前端 API 类型与页面已使用后端冻结字段和 `{items}` 分页 envelope；所有资源表格只请求当前服务端页并使用 `meta.total`，Dashboard 也只读取有界第一页，不再隐式拉取全量。Project/Device 引用选择器按 100 条服务端页触底续载，不会静默截断第 101 条后的资源。Audit 展示 `actor_type`、`actor_id`、`ip_address`、`metadata`、资源与 `occurred_at`；Command 详情展示扁平 aggregate、Attempt/Event、confirmation/evidence 与终止原因；Webhook 展示 `target_url`、Attempt 详情并只允许对 `dead` Delivery replay。
- Admin9 模板的用户、角色、菜单、字典、系统日志和用户中心源码、locale 与 `/api/system/**` 客户端均已移除；当前后台只包含冻结目标内的单管理员技术控制台页面。登录页不提供未纳入合同的自助密码找回或重置入口，相关 URL 按未知前端路由处理。菜单固定由前端冻结路由生成，不提供服务端菜单开关，旧 `/v1/auth/menu` 空占位端点已移除。未使用的 Arco/FAQ 外链菜单、通知与 Dashboard mock、编辑器、图片库、QQ 地图、品牌宣传、验证码输入和面包屑源码及其专用依赖或 locale 已从管理后台工程移除，不进入生产 bundle。
- Project 创建、更新和凭据轮换保留一次性明文披露；轮换入口在请求期间全局互斥，凭据弹窗关闭时立即清除前端引用并卸载内容。Device 创建只提交允许字段，Simulator identity 由平台派生，名称/lifecycle 更新受后端状态机约束；Command action 来自 Device Type profile，创建 body 使用 `project_id`，只有 `queued` 可取消。
- 管理后台提供只读 Provider/Device Type 注册与 profile 诊断，以及可按 Project、Device、Command 和既有稳定类型筛选的独立 Event 列表/详情；`.3` 新增的 `command.evidence_updated` 尚未进入 Event 筛选值。Command 与 Webhook Attempt 仍在各自详情诊断，Audit 保持只读。页面没有将 Provider `integration_status`、Simulator 的 `provider_accepted/verified` 或 WWTIOT HTTP acceptance 展示为 Device ACK/final/success。

## 已有测试能证明什么

- Go 测试覆盖持久 Project HTTP 生命周期、重启读取、一次性凭据披露、凭据轮换、IP whitelist、并发与回滚；也覆盖持久 Device 与 Command 的创建/读取/过滤/lifecycle、Project 隔离、Provider/profile gate、持久幂等与并发、取消、稳定排序，以及 Device/Command、Event、Delivery、Audit 原子回滚。PostgreSQL integration tests 另覆盖 Event/Webhook/Audit 严格查询、详情安全 DTO、dead replay 当前配置 snapshot、Webhook 精确签名、逐字节重试、Attempt/dead、secret 解密安全审计、lease 恢复、并发、回滚与重启读取，以及 Command worker 与 simulator 结果矩阵、profile timeout、Provider 公平领取、snapshot 不变性、deadline scanner 和晚结果 fencing。真实独立 server 子进程测试覆盖跨进程 Command 幂等、Webhook 单一领取，以及进程终止后等待 lease 到期并保守恢复为 `unknown/provider_delivery_unknown`；WWTIOT client/callback 仍只有无真实设备的组件合同证据。
- 前端 `type:check`、`i18n:check`、生产 build、ESLint 与 Stylelint 已通过。ego-browser 在桌面与 390px 移动视口验收 Dashboard、Project、Device、Provider/Device Type、Command、Event、Webhook、Audit 和 Simulator；Command、Event、Webhook 与 Device Type Drawer 以及 Project/Device Modal 均受视口宽度约束。Event 翻到第 2 页时实际请求 `page=2&page_size=10`；含 106 个 Project 和单 Project 105 台 Device 的隔离数据验证两个引用选择器均实际请求 `page=2&page_size=100` 并显示末页选项。2 秒网络延迟下，一次 API Key 轮换期间当前页所有 API Key/Webhook secret 轮换入口都被禁用，只有目标入口显示 loading；凭据弹窗关闭后明文不再存在于 DOM。本地隔离数据库中的 Simulator Project/Device/Command 与 outcome 只产生和展示 Provider 层证据，未把它表述为设备成功。
- 这些检查能证明持久 Command/Webhook worker、simulator 主链、并发和数据库恢复的代码行为，但不能证明某个外部业务 Webhook endpoint 可达、公开可信 callback、真实 WWTIOT 服务行为或真实智能锁最终执行；Command 创建的 `201`/幂等重放 `200` 仍只证明平台接受请求并持久化 `queued`，Simulator 与本地 HTTP 测试服务器都不能替代真实设备证据。

## Unknown 与验证条件

| Unknown                                                                               | 验证条件                                                                                                                           |
| ------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| 当前 WWTIOT 凭据与设备配置是否有效，以及生产响应含义                                  | V2 下行请求签名算法已与资料互证；仍需厂商确认、受控凭据和禁止泄密的联调环境验证真实服务行为。                                      |
| 真实设备收到、执行、拒绝和延迟回执的行为                                              | 隔离测试设备、明确写操作授权和可观测的设备侧证据。                                                                                 |
| V2 资料描述的设备信息 callback 在当前账号下是否可用，以及能否作为可关联的最终执行结果 | 厂商确认回调配置与签名规则，并用受控真实设备验证送达、关联、去重和结果终局性；详见 [WWTIOT Provider 合同](./providers/wwtiot.md)。 |
| WWTIOT 实际可观测的最高 confirmation level，以及该层级是否足以支撑设备平台正式验收    | 厂商合同、受控真实设备端到端证据，以及产品所有者基于证据的验收决定。                                                               |
| 生产网络、TLS、限流、配额和重试安全性                                                 | 目标部署网络与厂商限制说明。                                                                                                       |

不在本文记录本机数据库内容、真实密钥或真实业务数据；它们既敏感，也不能作为长期实现事实。
