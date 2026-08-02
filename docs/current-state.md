---
title: 当前实现状态
snapshot_date: 2026-08-02
status: implementation-snapshot
contract_revision: 2026-08-02
verified_against: current-worktree
---

# 当前实现状态

本文记录 `2026-08-02` 当前工作树的实现与可验证范围，不是产品合同。产品边界和完成门槛分别由 [Platform Boundary](./platform-boundary-contract.md) 与 [Platform Target](./platform-target-contract.md) 定义；实现和测试不能反向降低合同或真实设备验收门槛。

状态含义：

- **已实现**：当前运行时主链已接入，并有本地代码或测试证据。
- **部分实现**：安全子集已接入，但受合同 Unknown 阻塞的分支保持失败关闭。
- **目标未实现**：合同已接受，但当前代码没有运行时实现。
- **模拟验证**：由本地 fake Provider、TCP peer、simulator 或故障注入证明，不代表真实厂商/设备。
- **真实设备 Unknown**：仓库和本地测试无法证明，需要外部资料、凭据、受控设备与授权。

## 原始资料核验

实施工作保存了对以下四份只读外部归档资料的完整读取和逐页渲染核对记录；本次最终工作树复审未重新渲染，原件未修改、未迁入仓库：

| 资料                                           | SHA-256                                                            |
| ---------------------------------------------- | ------------------------------------------------------------------ |
| WWTIOT《平台转发协议 V1.1》                    | `5937e0b4d68961bd07346139381c237050286e5cc8054815c276be8a5a5edcfa` |
| WWTIOT《物网通平台转发协议 V2》                | `bb88f399c6010be5f1ab9eaa17eb36b7b680e5ef0787755f5e64b58bd689f718` |
| Omni《欧米智能马蹄锁 TCP+BLE 接口协议 V2.0.7》 | `36e835214954d9c45d0c35a3b3aed588d47038dfacc37c9e090301d0b5f7aec3` |
| Omni《OMNI 物联网设备 TCP 接口协议 V1.3.5》    | `2865a3c93b8c9c2c7185c0549c2955aa525cab8482713ece61b57a6cddc742f6` |

资料只证明对应版本所写协议。它们不证明当前设备型号/profile、目标网络真实性、厂商服务现状或真实锁体行为。

## 能力快照

| 能力                         | 状态                             | 当前代码与本地证据                                                                                                                                                                                                                                                                                                                      |
| ---------------------------- | -------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 当前产品合同                 | 已重新冻结                       | 唯一超级管理员加多个 Project 范围普通 User；一个共享单车业务应用作为 Project；唯一 Device Type 为 `smart-lock`；WWTIOT 与 Omni 均为当前必须接入的 Provider。小程序方向已确认但不在本次实现。                                                                                                                                            |
| User 与 Project 人类授权     | 后端已实现                       | Migration `010` 保留并提升既有管理员为唯一 active 超级管理员，回填既有 Project `manager_user_id`，移除 singleton User 限制并允许普通 User。认证支持所有 active User；后端提供普通 User 创建/列表/详情/启停、Project 指定/转交与集中 scope。普通 User 越权资源返回 `404`，本人 Project 的平台级操作返回 `403`；停用立即使旧 token 失效。 |
| 统一持久主链                 | 已实现                           | Project、Device、Command、Attempt、Result、DeviceState、RawMessage、Event、Webhook Delivery/Attempt 与 Audit 使用同一 PostgreSQL 领域模型；没有 Omni 旁路 Command 系统。                                                                                                                                                                |
| Project/Device/Provider 隔离 | 已实现                           | API、repository、Device identity、Command binding 与 Omni inbound 均校验 Project、Provider、profile 和 Device；同文本 identity 不能跨 Provider/Project 串线。Core 只依赖通用 `ProviderRegistration`/`DeviceIdentityPolicy`，WWTIOT identity、Omni IMEI 与 simulator 自生成规则分别由对应 adapter 提供并在 app 装配。                    |
| `smart-lock` revision 2      | 已实现                           | `unlock`/`lock=online_only`，`query_status=dispatch_once`，统一空 payload、deadline、request timeout、result timeout 和禁止自动重试。                                                                                                                                                                                                   |
| WWTIOT 下行                  | 部分实现                         | 三项 action 通过 V2 adapter 与持久 worker；严格请求/响应、echo、错误映射和脱敏已接入。有效 `result=ok` 最多形成 `provider_accepted/unverified`，随后无可信 final 时 `timeout`。                                                                                                                                                         |
| WWTIOT 上行                  | 受控失败关闭                     | 公开 callback 固定返回 `503 provider_callback_unverified` 且不读取/保存 body；因此不会产生 RawMessage、Result、DeviceState 或 Event。                                                                                                                                                                                                   |
| Omni codec/session/listener  | 已实现                           | 两个显式 profile、方向化 framing、Q0/H0/S5/S6 精确 schema、分包/粘包/错误 frame、Q0-only binding、identity generation、唯一 session、短写与 deadline、主动停机清理均有测试。                                                                                                                                                            |
| Omni 双 profile runtime      | 已实现                           | 两个 listener 必须同时配置；任一 listener 非预期退出会关闭 sibling、清理 session、禁用 adapter，并使应用 fatal/unready，业务 API 返回 `503 provider_runtime_unavailable`。                                                                                                                                                              |
| Omni 下行                    | 部分实现                         | bike S5 与 IoT S6 `query_status` 通过同一持久 worker 单次写入，最高为 `transport_sent/verified`，随后 `timeout`；这只证明本进程 socket 写入。物理 action 因映射/能力 Unknown 在写前失败关闭。                                                                                                                                           |
| Omni 上行                    | 部分实现                         | 合法或拒绝 frame 在 RawMessage + Audit 同一事务保存为 `unverified`；重复、未注册 identity、profile mismatch 与跨 Provider/Project 隔离有测试。不会产生可信 Result、DeviceState、online 或 final。                                                                                                                                       |
| Webhook 与 Audit             | 已符合当前人类授权合同           | Event 事务创建 Delivery，持久 worker 记录 Attempt/重试/dead/replay；人类 Audit 统一使用 `actor_type=user` 和实际 `actor_user_id`，新增 User 创建/状态变化与 Project 转交动作。普通 User 只能读取本人 Project Audit，dead delivery 重发仍只允许超级管理员。Omni 接收/拒绝继续分别使用 `provider.message_received/rejected`。             |
| 管理后台                     | 已实现并完成双角色本地浏览器验收 | Web 已接入 User 创建/查询/启停、Project 创建时指定 manager 与后续转交、普通 User 的 Project 范围页面和操作边界，以及实际 `actor_user_id` Audit 展示。User 管理、Simulator、Project 凭据操作和 dead Webhook 重发只向超级管理员显示；普通 User 编辑 Project 只提交名称。                                                                  |
| NATS JetStream Outbox/Inbox  | 目标未实现                       | ADR、Runtime 和 Messaging 已定义目标；当前代码无 NATS 依赖，Command/Webhook 使用 PostgreSQL polling/lease。不得把当前 worker 测试记为 JetStream 验收。                                                                                                                                                                                  |
| 真实设备端到端               | 真实设备 Unknown                 | 当前没有受控 WWTIOT/Omni 设备、Omni profile 对照、可信上行身份、逐次写授权或设备侧观察记录。没有执行真实 `query_status`、`unlock` 或 `lock`。                                                                                                                                                                                           |

## 双 Provider 行为证据

下表中的“成功”只表示对应 Provider 当前最高可证明的较低层成功，不表示设备执行成功。

| 行为            | WWTIOT 本地证据                                                                      | Omni 本地证据                                                                                                                     | 真实设备事实 |
| --------------- | ------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------- | ------------ |
| 较低层成功      | 有效 V2 response 只进入 `provider_accepted/unverified`                               | S5/S6 完整 socket 写入只进入 `transport_sent/verified`                                                                            | Unknown      |
| 拒绝/发送前失败 | Provider rejection、发送前 transport failure 和 invalid response 分开映射            | 缺失/歧义 session、非法 profile、物理 mapping Unknown 在写前拒绝                                                                  | Unknown      |
| timeout         | acceptance 或不确定发送保持 `sent`，到观察期限 `timeout`                             | query_status 写入后保持 `sent`，到观察期限 `timeout`                                                                              | Unknown      |
| 重复            | Command 幂等/worker lease 不重复下行；callback 入口重复请求同样失败关闭              | 同一连接代际内按 RawMessage fingerprint 去重并保留 duplicate Audit；跨连接代际的相同 frame 是独立观察；终态 Command 不重写 socket | Unknown      |
| 迟到            | worker 恢复后的迟到 adapter 返回被 lease fencing；未启用 callback 不接收设备迟到结果 | 恢复后不重写 socket；未认证上行不能成为迟到 Device Result                                                                         | Unknown      |
| 不可关联消息    | callback 在认证/关联合同关闭前完全不接收                                             | 未注册 IMEI、profile/identity mismatch 保持无 Project/Device 的 `unverified` 诊断                                                 | Unknown      |
| 恢复            | dispatching 崩溃转 delivery unknown，保持原 deadline 且不自动重发                    | 同一通用恢复，并另有 listener 停机、session generation 与双 profile fatal 测试                                                    | Unknown      |

## 本地验证证据

本次最终工作树复审完成以下新鲜、本地、只作用于代码、构建产物和隔离本地测试资源的验证；未访问真实 Provider 或设备：

- 后端短测试 `go test ./... -count=1 -short`、Omni race 测试 `go test -race ./internal/directdevice/omni -count=1` 与 `go vet ./...` 通过；短测试因 `httptest` 本地回环限制在沙箱外执行，但没有启动持久服务或访问真实 Provider/设备。
- 最后修复后的当前工作树使用专用 `device_platform_codex_final_test` 数据库非缓存执行 `go test -tags=integration ./... -count=1 -timeout=120s`，命令 `exit 0`；非 `-v` 输出只报告各 package 为 `ok`，不逐项显示 skip。执行时本机 Redis 未运行且未设置 `MIGRATION_TEST_REDIS_URL`，Redis-dependent 的进程、安装和 WWTIOT runtime 用例会在测试 URL guard 处 skip；保留输出不能证明具体 skip 数，因此数量为 Unknown，本轮结果不计为 Redis 验收。其余用例覆盖 migration 010 回填/唯一超级管理员/回滚 fail-closed、普通 User 登录与停用 token 失效、Project 分配/转交、Project/Device/Command/Event/Webhook/Audit scope、高风险操作 `403`、跨 Project `404`、实际 `actor_user_id`、人类与 Open Project DTO 最小披露，以及 Project 转交与 User 停用并发不产生 disabled manager。测试 schema 均隔离创建并自动删除，未调用真实 Provider 或设备。
- Command recovery fencing 测试重复执行 20 次通过；人类/Open Project HTTP 授权合同测试重复执行 3 次通过。`Scope.Validate` 的人类 scope 非空 `UserID` 约束包含在后端短测试中。并发测试按确定阶段验证 stale worker fencing 与 recovery 后 timeout，没有为测试改变 Command 生产语义。
- 本任务确认专属数据库 `device_platform_dual_provider_test` 不存在且当前角色具备建库能力后创建该库，令 `MIGRATION_TEST_DATABASE_URL` 精确指向它，执行完整 `go test ./... -count=1 -tags=integration -timeout=120s -v`。Migration `009` up/down、repository identity tombstone、Device service tombstone、状态变化后的历史 Command 幂等重放、CommandResult、Omni inbound 持久化/隔离和 worker 主链/恢复均通过；测试使用各自唯一临时 schema。10 个依赖独立 Redis 的进程/安装/WWTIOT runtime 用例因未设置 `MIGRATION_TEST_REDIS_URL` 明确跳过，不计为 Redis 验收。全部测试和连接结束后，该任务专属数据库被删除；既有 `device_platform_test` 未被复用、迁移、清空、改权或删除。
- 前端 `pnpm --dir frontend install --frozen-lockfile`、正式 `pnpm --dir frontend i18n:check`、TypeScript 类型检查、production build、范围 ESLint、变更源码与 `package.json` 的 Prettier，以及变更 Vue 页面的 Stylelint 通过。`tsx@4.20.6` 已作为直接 devDependency 固定；i18n 检查确认 zh-CN/en-US 各 437 个 key 且集合一致，当前引用的 326 个 key 均存在。
- ego-browser 先使用专用 `device_platform_web_acceptance_test` 数据库完成超级管理员与普通 User B 双身份主链。超级管理员可见 User 管理与 Simulator；Project 页首次进入实际请求 `/v1/users?page=1&page_size=50` 并返回 `200`，manager 下拉包含 active User；随后通过 UI 创建两个普通 User、创建指定 User A 的 Project，并以 `POST /v1/projects/{id}/transfer` 把 Project 转交给 User B。普通 User B 登录后只能看到获分配的 Project，User/Simulator 直达均被前端权限 guard 送回工作台，Project 页面不显示创建、转交、API Key 或 Webhook Secret 操作，编辑 modal 只有名称字段且实际 PATCH body 仅为 `{"name":"Web Acceptance Fleet Beta"}`。Audit 页面显示该 User 的实际 `actor_user_id`；Webhook 空列表页的可见按钮只有刷新，没有重发操作。
- 最后 Scope/DTO 修复后，ego-browser 又在连接 `device_platform_codex_final_test` 的本地前后端完成双身份复验：超级管理员通过 UI 创建并分配 Project；普通 User 登录后可见本人 Project，但看不到 User/Simulator 菜单、创建/转交 Project、轮换 API Key 或 Webhook Secret 操作，编辑 modal 只有项目名称。浏览器内实际请求 `/v1/projects` 返回 `200`，Project 对象键为 `created_at/id/manager/manager_user_id/name/updated_at`，不存在 `webhook_url`、`webhook_configured` 或 `ip_whitelist`。页面未出现应用异常或网络加载失败，仅有浏览器扩展自身的 info 日志。按用户要求，本轮本地提交完成后继续保留可通过 `127.0.0.1:5173` 访问的前端、`127.0.0.1:8080` 后端和该隔离测试库供人工查看，不切换到真实 Provider 或设备。
- 普通 User 会话依次打开 Dashboard、Project、Audit 与 Webhook，未观察到 `4xx/5xx`、network loading failure 或浏览器 console error。Project 页在 `390x844` 移动视口下 `scrollWidth=clientWidth=390`，没有页面级横向溢出或被视口裁切的可见输入/按钮。CDP 截图调用两次超时，因此本轮证据不包含持久截图；该限制不改变已回读的 DOM 几何和实际请求证据。
- `git diff --check` 通过。

以下仅为实施阶段保存的历史报告，不作为本次最终文件版本的独立验证：

- 专用隔离 PostgreSQL 数据库曾报告通过 Command worker、Result、Device、Project、Webhook worker、迁移/冻结合同和相关 HTTP 集成测试；这些结果早于最终文件版本。
- Omni fake peer 测试曾报告覆盖 Bike/IoT 的 Q0/H0/S5/S6、分包/粘包、非法前缀、Q0-only binding、generation fencing、并发、清理和双 listener fatal。
- 最后 Scope/DTO/Open Project 修复前，固定 `staticcheck 2024.1.1 (0.5.1)` 曾使用任务临时目录中的 Go `1.23.12` 对默认与 `integration` tags 执行且无 finding；最后修复后当前 PATH 中 `staticcheck` 不可用，因此该历史结果不计为最终文件版本的新鲜验证。
- ego-browser 此前曾在隔离的 `device_platform_acceptance` 数据库验证 Provider、Device、Command、Event 与 Audit 页面，并只对 simulator 下发 `query_status`；该历史记录早于多 User Web 改造，不替代上方使用新专用数据库完成的双身份浏览器证据。
- 该历史 simulator Command 报告为 `provider_accepted/verified` Attempt，Result 为空，只产生 `command.created`、`command.status_changed` 与 `command.evidence_updated`；它不证明 Device ACK、`device_final` 或设备执行成功。
- 所有阶段均未向真实 WWTIOT 或 Omni 设备执行 `query_status`、`unlock` 或 `lock`。

## 当前安全边界

- 未使用或输出真实 Provider 凭据、真实 IMEI、raw frame 或未脱敏 socket error。
- 未执行任何真实 `lock`、`unlock` 或 `query_status`，也没有自动重试真实写操作。
- WWTIOT acceptance、Omni socket 写入和 simulator `verified` 都不会创建 Device ACK、device final 或 `success`。
- Omni inbound 的 Audit `result=success` 只表示技术消息被接收并持久化，RawMessage 仍为 `unverified`。
- Platform Core 不包含 WWTIOT/Omni 专用字段或按 Provider 分支的 action 语义；`provider_profile` 作为 opaque binding 传递，映射留在 adapter。

## 尚未关闭的阻塞

| 阻塞                                                           | 影响                                                | 解除条件                                                                        |
| -------------------------------------------------------------- | --------------------------------------------------- | ------------------------------------------------------------------------------- |
| Omni 目标设备与两个 profile 的对应关系 Unknown                 | 具体设备真实连接、派发和实机验收                    | 厂商可追溯型号/固件/profile 对照，加受控只读握手证据                            |
| Omni TCP peer/IMEI/frame 无已冻结认证与防重放                  | 可信 Result、DeviceState、online、生产安全          | 正式认证合同或受控网络身份方案，加冒充/重放/重连测试                            |
| Omni 物理 action 字段与 KEY 生命周期 Unknown，bike 无主动 lock | Omni `unlock`/`lock` 实施与三 action 实机矩阵       | 厂商书面映射、产品决定及受控故障矩阵；bike lock 需新协议/profile 或接受能力缺口 |
| WWTIOT 响应验签、callback 签名/关联/防重放 Unknown             | verified acceptance、可信 State/Result 和真实 final | 厂商正式规则与受控重复/迟到/关联验证                                            |
| 两 Provider 的可信 online/final 能力与业务可接受性 Unknown     | `online_only` 物理动作和共享单车正式验收            | 受控设备、现场观察/恢复方案、业务风险裁决和逐次明确授权                         |
| NATS Outbox/Inbox 尚未实现                                     | 目标异步架构与 Broker 故障/重投验收                 | 实现 Publisher/Consumer/Recovery，并完成重复、Ack、redelivery 与故障测试        |

本地验证命令和安全运行方式见 [Local Development](./local-development.md)。真实设备验收必须逐 Provider、逐已证明 profile 完成 [smart-lock 验收矩阵](./device-types/smart-lock.md#真实设备验收矩阵)，不能由另一 Provider 或 simulator 代替。
