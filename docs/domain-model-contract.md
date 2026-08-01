---
title: 领域模型与一致性合同
updated: 2026-08-01
status: product-decision-required
contract_revision: 2026-08-01
---

# 领域模型与一致性合同

本文定义当前目标所需的领域对象、稳定标识、归属、约束、生命周期、事务边界与恢复责任。它从属于[平台边界合同](./platform-boundary-contract.md)和[当前平台目标合同](./platform-target-contract.md)，并约束 [API 合同](./api-contract.md)、Device Type 合同、Provider 合同、schema 和运行时代码。

## 模型原则

- `Project` 是机器接入与数据隔离边界，不是租户、组织或人类权限容器。
- `Device` 是平台管理的技术设备；自行车、用户、订单、计费等业务实体不进入本模型。
- Platform Core 保存通用 envelope、状态与一致性事实；具体 action 语义来自只读 Device Type Capability profile，厂商转换来自 Provider adapter。
- 规范化资源使用平台生成的不可变 UUID。Provider identity、幂等 key、外部 request ID 只用于各自明确的关联范围，不能混作平台资源 ID。
- 目标运行模型只有一套。API DTO、后台页面和 worker 都读取同一持久化事实，不维护可独立漂移的内存副本。

## 对象与关系

```text
Project 1 --- n Device n --- 1 DeviceType
Project 1 --- n Command n --- 1 Device
Command 1 --- n CommandAttempt
Command 1 --- n CommandResult
Device 1 --- n DeviceState
Device 0..1 --- n RawMessage              (unverified/unresolved may have no Device)
Project 1 --- n Event
Event 1 --- n WebhookDelivery 1 --- n WebhookDeliveryAttempt
Project 1 --- n AuditLog
Provider 1 --- n Device                 (by provider_code)
DomainTransaction 1 --- n InternalOutbox
Consumer 1 --- n ConsumerInbox          (by consumer_name + message_id)
```

### Project

保存 `id`、`name`、`api_key_hash`、可选 `webhook_url`、版本化加密 `webhook_secret`、`ip_whitelist` 和时间戳。

- API Key 明文只在创建或轮换成功的单次响应中出现；列表、详情、日志、Event、Audit 和数据库均不得保存或返回明文。
- API Key 轮换替换旧 hash，旧 key 立即失效，并写入技术审计。
- 当前目标中每个 Project 同时只能有一个启用的 Webhook endpoint；`webhook_url` 与 `webhook_secret` 属于 Project aggregate 的唯一 Webhook 配置，不建立 endpoint 集合或由另一服务独立维护的端点状态。持久化可以使用版本化子表保存历史 snapshot，但 API 与领域写入只有一个 Project 配置入口。
- Webhook secret 与 API Key 分离，明文只在首次生成或显式轮换的单次响应中出现；数据库使用部署级外部密钥以 AES-256-GCM 保存每个版本的 ciphertext、nonce 和 key version，普通日志与读取 API 不得返回明文、密文或 nonce。移除 endpoint 不得暴露旧 secret；解密失败必须停止本次外部 HTTP、产生安全审计且不能使用 fallback secret。它是否消耗 DeliveryAttempt、重试或立即进入 `dead` 仍按文末 Domain Unknown 阻塞，不能由“失败关闭”推断终局。
- API Key 使用密码学安全随机源生成至少 256 bit secret，并以 SHA-256 digest 持久化和查找；数据库不保存可恢复明文。
- `ip_whitelist` 为空表示允许任意来源，但仍记录认证结果；非空时必须按认证层确认的客户端 IP 执行。

### Provider

Provider 是部署级 adapter 注册项，当前固定为 `wwtiot`、`omni` 与显式 `simulator`。它不是业务可创建资源，也不引入假想的通用 Provider 元模型。

- Provider code 在一个部署内唯一且稳定。
- Provider endpoint、UserID、UserKey、TCP listener 等敏感或部署配置由服务端环境提供；后台只读展示 code、名称、adapter、传输方式和 `integration_status=unconfigured|configured_unverified|verified`，不返回 secret，也不通过普通业务 API 修改 secret。
- `provider_profile` 是 Device 上不可变的通用 Provider binding snapshot，用于选择同一 Provider 下资料明确不同的协议 profile。允许值和含义只由对应 Provider 合同定义；Platform Core 只保存和传递 opaque identifier，不按 `wwtiot`、`omni` 或具体 profile 分支。
- 配置变化在服务启动时生效。需要在线变更或数据库托管 secret 时，必须先形成新的安全合同，不在当前目标中猜测实现。

### DeviceType

Device Type 是代码审查并随发布交付的只读 capability profile。当前唯一 profile code 为 `smart-lock`。

- profile 定义 action identifier、payload schema、风险等级、投递策略、平台观察期限和重放限制。
- 当前目标不提供动态 Device Type CRUD、继承、表达式或低代码规则编辑器。
- 数据库可以保存 profile 的发布快照用于外键和诊断，但运行时不得同时存在另一套语义来源；启动时发现 code 或 profile revision 漂移应失败关闭或执行显式 migration。

### Device

保存 `id`、`project_id`、`device_type_id`、`name`、Provider identity、`provider_profile` binding snapshot、接入元数据、`connection_status`、`lifecycle_status` 和时间戳。

- 一个 Device 必须且只能属于一个 Project。
- 当前不支持跨 Project 转移。
- `(provider_code, provider_device_id)` 在整个平台和全部 lifecycle 中唯一，`provider_profile` 不得用于绕过 identity 唯一性。Device 进入 `deleted` 后保留 identity tombstone，任何后续 Device 都不得复用；这样没有可信设备代际的迟到消息只能解析到原 tombstone 并按非 active 拒绝。未来若引入复用，必须先冻结可信 identity incarnation、迟到/重放隔离并执行显式迁移。
- `provider_profile` 创建后不可更新。需要改变 profile 时必须注册新的 Device identity binding；不能在已有 Command、Attempt、Result 或 RawMessage 下静默换 codec。
- `connection_status` 只表达有证据的技术连接事实。WWTIOT 当前没有已确认 heartbeat 合同；Omni 当前没有已确认的设备认证和连接代际合同，因此不得因 HTTP acceptance、TCP accept、自报 IMEI 或一次 socket 写入推导 `online`。
- `lifecycle_status=disabled` 的 Device 不接受新命令；逻辑删除记录保持审计可追溯。

`connection_status` 允许在 `unknown|online|offline` 间反复变化，但每次变化都必须引用 Provider 合同允许的可信观察。`online` 必须同时具有证据来源和仍有效的新鲜度窗口；窗口到期后的降级值由 Provider 合同确定，未确定时回到 `unknown`，不得永久沿用旧在线事实。

Device lifecycle 的唯一状态机为：

```text
active -> disabled
active -> deleted
disabled -> active
disabled -> deleted
```

`deleted` 是终态。名称更新不改变 lifecycle；Project、Device Type、Provider identity 和 `provider_profile` 不可更新。进入 `deleted` 不得删除或重新归属历史 Command、Event 和 Audit，也不得释放 Provider identity tombstone。

### Command

保存 `id`、`project_id`、`device_id`、action identifier、规范化 payload、状态、delivery policy、幂等字段、原因、观察期限和时间戳。

- Command 的 `project_id` 必须与关联 Device 的 `project_id` 相同；该约束必须由同一事务中的锁定校验或数据库等价约束保证。
- `(project_id, idempotency_key)` 唯一且 key 不为空。历史查询必须先于当前 Device/Profile/Provider 资格校验；调用方 `device_id`、规范化 action 和 payload 与历史 Command 一致时返回原 Command，不重复产生投递、Attempt、Event 或 Audit，也不重新计算当前派生参数。
- 相同 key 的任一调用方输入与历史冻结输入不一致时返回 `idempotency_key_conflict`。历史 Command 保存的 Provider binding、Device Type revision、delivery policy、deadline 和 retry policy 保持首次值；当前 lifecycle、connection、Provider 配置或 profile revision 的变化不改变重放结果，也不能触发新物理动作。
- Command 的 `status` 只表达生命周期位置，不表达单次投递结果或证据等级。`status`、`CommandAttempt.outcome` 和 `confirmation_level` 是三个正交维度：Provider acceptance、Device ACK 和 Device final result 不得合并，也不得把证据不足编码成含义混杂的 Command `unknown` 状态。
- `reason_code` 是稳定机器码，`reason_detail` 是脱敏诊断文本；调用方不得依赖自由文本作分支。

Command lifecycle 的唯一状态机为：

```text
queued -> sent | cancelled | failed | timeout
sent   -> acked | success | failed | timeout
acked  -> success | failed | timeout
```

- `queued`、`sent`、`acked` 是非终态；`success`、`failed`、`cancelled`、`timeout` 是终态。
- `sent` 表示平台已经承诺向 Gateway/Provider 发起本次投递；`acked` 只表示可信 Device ACK；`success` 只表示可信 Device final success。
- `timeout` 不推断设备失败，也不得被普通迁移改写。迟到可信结果只追加 `late=true` Result/Event，并可单调提升证据聚合，不改变原终态、reason 或完成时间。
- result deadline 与 Provider response、可信 Device final 并发，以及相互冲突的 verified final 的仲裁时点与优先级，按 [Platform Target 的产品裁决](./platform-target-contract.md#需要产品所有者裁决)保持阻塞；不能让数据库抢锁时序、`reported_at` 或未冻结的 `observed_at` 规则静默决定外部终态。
- Provider acceptance 不是 Command 状态；投递歧义不创建 `unknown` 状态。
- 仅无有效 dispatcher lease 的 `queued` Command 可取消。取消不表示已经发出的物理动作被撤回。

Command 终态 reason code 的唯一映射为：

| reason code                  | Command status | 事实                                                  |
| ---------------------------- | -------------- | ----------------------------------------------------- |
| `cancelled_by_request`       | `cancelled`    | 调用方在可取消窗口取消                                |
| `provider_not_configured`    | `failed`       | 创建后配置漂移，且请求尚未发出                        |
| `provider_transport_error`   | `failed`       | 可以证明请求未发送                                    |
| `provider_rejected`          | `failed`       | Provider 明确拒绝                                     |
| `device_reported_failure`    | `failed`       | 可信 Device final failure                             |
| `device_not_online`          | `failed`       | `online_only` preflight 失败，且请求未发送            |
| `dispatch_deadline_exceeded` | `timeout`      | `queued` 阶段未在派发期限内承诺发送                   |
| `result_observation_timeout` | `timeout`      | `sent` 或 `acked` 后未在观察期限取得可信 final result |

非终态 reason 为 `null`。新增或改变 reason 必须先修订本合同；自由文本不能代替机器码。

### CommandAttempt

每次 dispatcher 领取 Command 准备向 Gateway/Provider 投递时建立一个独立 Attempt，保存 attempt number、phase、adapter、provider request key、开始/完成时间、outcome、confirmation level、evidence status、脱敏请求/响应摘要和错误。

- `(command_id, attempt_no)` 唯一；Provider request key 在对应 Provider 范围内唯一。
- `phase` 只能按 `claimed -> dispatching -> completed` 或 `claimed -> completed` 单调迁移。`claimed` 尚未承诺外部调用；`dispatching` 表示 worker 已在事务中承诺立即执行外部调用，Command 同时进入 `sent`；`completed` 表示本次 Attempt 的可观测结果已持久化。非 completed Attempt 的 outcome 为 `null`，completed Attempt 必须有稳定 outcome。
- `confirmation_level` 只能是 `none`、`transport_sent`、`provider_accepted`、`device_acked`、`device_final`。
- `evidence_status` 只能是 `none`、`verified` 或 `unverified`。在 Attempt 上，它评价该 Attempt `outcome` 所依赖证据的来源真实性和校验条件；在 Command 上，它保守评价支撑当前 status 与最高 confirmation 的决定性证据。confirmation level 严格按 `none < transport_sent < provider_accepted < device_acked < device_final` 单调提升；`none` 层的 evidence 必须保持 `none`，`transport_sent|provider_accepted` 的 evidence 可以是 `unverified|verified`，`device_acked|device_final` 必须是 `verified`。confirmation level 不变时只允许 `unverified -> verified`，`verified` 不得回退；confirmation level 提升到 `transport_sent|provider_accepted` 时，Command evidence 改为评价支撑新层级的决定性证据，可以因新证据未验签从较低层的 `verified` 变为新层的 `unverified`，不视为同层回退。evidence 不改变 confirmation level 本身；`success` 必须同时具有 `device_final` 与 `verified`。因此 WWTIOT `provider_rejected` 可同时为 `confirmation_level=transport_sent`（本地已证明请求写出）与 `evidence_status=unverified`（决定 rejection 的响应正文尚不可验签），两者不矛盾。
- 当前稳定 Attempt `outcome` 只包括 `not_dispatched`、`invalid_request`、`provider_accepted`、`provider_rejected`、`transport_error_before_send` 和 `indeterminate`。它只回答这一次下行投递观察到了什么：请求可能已发出但没有可判定响应、响应结构/echo 无法形成可信结论，或 dispatching 崩溃无法确定外部结果时统一为 `indeterminate`，具体差异由 Attempt 的稳定 `reason_code=provider_delivery_unknown|provider_response_invalid` 保留。`not_dispatched` 只用于 claimed 阶段被取消、超过派发期限或 `online_only` preflight 失败，confirmation/evidence 均为 `none`。Device ACK/final result 不得覆盖已完成 Attempt 或进入 Attempt outcome。
- Command 结果观察期限到达由 deadline scanner 把 Command 写为终态 `timeout` 并产生 Event；它不伪造新的 Provider Attempt，也不覆盖既有 Attempt outcome。若最后一次 Attempt 已是 `provider_accepted`，该 Attempt 保持原值；Command 的超时表示在观察窗口内未取得可信 final result，最终执行结果仍为 indeterminate。
- confirmation level 单调提升，不能被后来的较低层事实覆盖。

Attempt phase 的唯一状态机为 `claimed -> dispatching -> completed` 或 `claimed -> completed`。`completed` 是终态；一个 Attempt 不能回退、重新打开或被设备结果覆盖。允许的 outcome 及含义以本节列举为全集，Provider 合同只能把厂商事实映射到这些值。

### CommandResult

CommandResult 是设备 ACK、设备 final result 或其他可关联结果证据的不可变记录，至少保存 `result_id`、`command_id`、可选 `attempt_id`、source、稳定 outcome、confirmation level、evidence status、Provider message deduplication key、`reported_at|null`、`observed_at`、`late` 和脱敏 payload。Result outcome 只包括 `device_acked`、`device_succeeded` 和 `device_failed`。它不替代 CommandAttempt：Attempt 描述一次下行投递，Result 描述之后到达的设备结果证据。

- Device ACK 只有在 `confirmation_level=device_acked` 且 `evidence_status=verified` 时才允许 `sent -> acked`；可信 final success 可以直接执行 `sent -> success`，不要求伪造中间 ACK。
- `success` 只接受 `device_succeeded/device_final/verified`；可信 final failure 进入 `failed`。Provider acceptance 不能创建这两种终局结果。
- `timeout` 是终态。终态后到达的可信结果以 `late=true` 追加 CommandResult 和 Event，并可单调提升 Command 的 confirmation/evidence 聚合字段，但不得改变原 `status`、`reason_code`、`finished_at` 或覆盖既有 Attempt/Result。重复结果按稳定 deduplication key 返回既有记录，不重复产生 Event。

### DeviceState 与 RawMessage

- DeviceState 的最小 envelope 为 `state_id`、`schema_version=1`、`project_id`、`device_id`、`device_type_code`、`provider_code`、`provider_profile`、`reported_at|null`、`observed_at`、`evidence_status` 和类型化 `state` object。具体锁态字段由 smart-lock 合同定义；未知字段不能提升为通用 Device 字段。
- 每次允许接收的上行可以创建不可变 `RawMessage`，保存 Provider/profile、方向、接收时间、解析状态、evidence status 和受控原文；secret、认证签名和不必要的敏感字段必须脱敏或分离保护。无法验证或无法唯一关联时，`project_id`/`device_id` 保持 `null`，不能猜测归属。
- 只有真实性验证通过并完成唯一 Device 映射后，才可从 RawMessage 产生规范化 `DeviceState`、CommandResult 与业务可投递 Event。`unverified` RawMessage 只能产生受控诊断/Audit，不得提升 Command confirmation。
- `Device.current_state` 是最新 DeviceState 的派生读取，不建立第二套可独立写入的 JSON 状态。
- 无法验证认证、无法唯一映射设备、profile 不匹配或 schema 不合法的 Provider 消息不更新 DeviceState。已启用的接收入口对通过 parser、显式 profile 与唯一当前 Device binding 校验但真实性仍未验证的消息，以 `provider.message_received` 记录“已接收未验证技术事实”；对 schema、首报、identity、profile 或 lifecycle 失败以 `provider.message_rejected` 记录受控失败。两者均不得回显 secret、IMEI 或 raw frame，也不得因 Audit result 为 success 推进 Command confirmation。Omni RawMessage 与对应 Audit 在同一事务提交；同一连接代际内的相同 frame 可以复用 RawMessage 并保留 duplicate Audit，跨连接代际的相同字节必须创建独立 RawMessage。当前公开 WWTIOT callback 固定 503 且不读取 body，因此在重新冻结并启用前不产生 Provider message Audit。

### Event 与 Outbox

Event 是不可变的规范化技术事实，至少保存 `event_id`、`event_type`、`project_id`、可选 Device/Command 关联、发生时间、source 和 payload。

- 当前 Event envelope 的 `schema_version` 固定为整数 `1`，最小字段为 `event_id`、`schema_version`、`event_type`、`project_id`、`device_id|null`、`command_id|null`、`occurred_at`、`source` 和 `payload`。稳定 `event_type` 只有 `device.created`、`device.lifecycle_changed`、`device.connection_changed`、`device.state_updated`、`command.created`、`command.status_changed`、`command.evidence_updated` 和 `command.result_recorded`；新增类型必须先修订本合同并同步 API wire schema，已有类型的破坏性 payload 变化必须提升相应 schema version。`command.status_changed` 只表达 `from != to` 的 Command 状态迁移；Command 状态不变、仅 Attempt confirmation/evidence 单调提升时使用 `command.evidence_updated`，每个可信 CommandResult 使用 `command.result_recorded`，不制造同状态的 status-changed Event。
- 会改变领域状态的事务同时写入相关 Event 与待投递 Outbox/Delivery；不得在提交后用 best-effort hook 补写。
- Event payload 只含业务应用可消费的规范化技术事实，不泄露 Provider secret 或原始敏感消息。
- 同一领域事实使用稳定 deduplication key，重复 callback 或 worker 重放不能创建重复最终效果。

### WebhookDelivery 与 WebhookDeliveryAttempt

- 每个 Event 按其 Project 在 Event 创建时生效的 Webhook 配置版本形成一个 Delivery；Delivery 保存该配置 version 和 target snapshot。旧 secret version 只保留到引用它的 Delivery 全部终止并超过受控清理期，读取 API 永不返回明文。未配置 endpoint 时 Event 仍保存，但不创建投递。
- 每次进入 `sending` 的领取事务创建不可变 DeliveryAttempt 并消耗一次 attempt count；Attempt 记录 HTTP 是否实际开始、写出和完成，领取后在 HTTP 前崩溃的记录同样保留。Delivery 保存聚合状态、下一次执行时间和当前 attempt count，不替代 Attempt 历史。
- 手动重发使用重发时当前启用的 Project Webhook 配置创建新的 Delivery，并通过 `replay_of_delivery_id` 指向原 dead Delivery；原记录与原配置 snapshot 保持不变。

WebhookDelivery 的唯一状态机为：

```text
pending -> sending
sending -> delivered | failed
failed  -> sending | dead
```

`delivered` 与 `dead` 是终态。`sending` lease 过期时，先终止本次 DeliveryAttempt，再进入 `failed` 或在尝试上限耗尽时进入 `dead`。manual replay 创建新的 `pending` Delivery，绝不重开原 Delivery。

### InternalOutbox 与 ConsumerInbox

- InternalOutbox 是领域事务中创建的内部消息发布意图；它保存稳定 message ID、类型、版本、aggregate 引用、payload、发布进度和恢复时间，不是领域 Event 或 Webhook Delivery。
- ConsumerInbox 以 `(consumer_name, message_id)` 唯一记录消费完成事实。消费者的领域效果与 Inbox 完成标记在同一 PostgreSQL 事务提交。
- Outbox Publisher、Broker 或 Consumer 的投递状态不改变 Command、Result、Event 或 Delivery 语义；消息重复只允许重新读取权威对象并执行条件更新。

### AuditLog

AuditLog 记录管理员、Open API 机器身份、Provider message 或 system worker 的关键技术操作。它保存 actor、Project、资源、action、result、来源 IP、request ID、脱敏 metadata 和时间。

审计记录不是领域 Event，也不承载共享单车业务审计。API 合同要求审计的领域写入应与该写入处于同一事务；登录失败等没有领域事务的安全事件独立持久化。worker 的状态事实由 Attempt 与 Event 记录，除安全或人工操作外不重复制造 Audit。

## 原子事务边界

以下操作各自在一个数据库事务中完成：

1. 创建/更新 Project、API Key 轮换、Webhook 配置变更及对应 Audit。
2. 创建 Device、lifecycle 变更、Device Type 校验及对应 Audit/Event/Outbox；仅名称变更没有稳定领域 Event，只与 `device.updated` Audit 在同一事务完成。
3. 创建 Command：锁定并校验 Project/Device、写 Command 与幂等约束、初始 Event/Outbox 和 Audit。
4. 领取 Command：仅对无有效 lease 的 `queued` Command 条件创建 `phase=claimed` Attempt 和 lease；Command 仍为 `queued`，有有效 lease 时取消与 deadline scanner 都不得越过该所有权。
5. 承诺派发：短事务校验 lease/Attempt 仍有效，并以数据库权威时间重新检查绝对 `dispatch_deadline_at`。当 `now >= dispatch_deadline_at` 时，将 Attempt 完成为 `not_dispatched/dispatch_deadline_exceeded`、Command 改为 `timeout` 并写 Event/Outbox，且不发请求；有效 lease 只保护领取所有权，不能延长派发期限。未到期时再执行已冻结的 Provider 配置和 profile preflight。`online_only` Device 已非可信 `online` 时，将 Attempt 完成为 `not_dispatched/device_not_online`、Command 改为 `failed` 并写 Event/Outbox，且不发请求。全部通过时才将 Attempt 改为 `dispatching`、Command 改为 `sent`，设置 `sent_at` 与 result deadline 并写状态 Event/Outbox，事务提交后立即执行外部调用。Device 在入队后发生 lifecycle 变化的处理尚待产品裁决，不能由实现并入本步骤。
6. 处理 Provider 响应：条件完成 Attempt、更新 Command 的证据聚合，写 Event/Outbox 与所需 Audit；`indeterminate` 不把 Command 写入额外的混合生命周期状态。
7. 接收 Provider 上行：按 Provider 合同完成认证、profile/parser 校验和防重放；可信消息在一个事务中按 Provider 去重键保存不可变 RawMessage 与处理 Outbox，持久化成功后才返回协议成功。合同允许保留的 `unverified` 消息只写 RawMessage/诊断，不创建可信处理 Outbox。
8. 处理已持久化 RawMessage：在另一个事务中锁定未处理消息，按去重键追加 CommandResult、更新 DeviceState 和 Command 证据聚合、写 Event/Outbox，并标记处理完成；只有存在已确认且无歧义的命令关联规则时才关联 Command，迟到结果不得改写终态。
9. Event 事务已经原子创建初始 Webhook Delivery；独立 Webhook 事务只负责领取/完成 DeliveryAttempt、进入 retry/dead，以及创建 manual replay，不能用于提交后补建初始 Delivery。

外部 HTTP 调用不得包在长数据库事务中。`claimed` lease 过期且从未进入 `dispatching` 时，可以由 worker 重新领取同一 Attempt，因为合同保证尚未承诺外部调用。`dispatching` 一旦提交，进程在调用前后或结果落库前崩溃都无法证明未发送；恢复器将 Attempt 完成为 `indeterminate`、reason `provider_delivery_unknown`、confirmation `transport_sent`、evidence `unverified`，Command 保持 `sent` 并由原 result deadline 终止为 `timeout`。不可安全重放的 action 不自动重发。

## Worker 所有权与恢复

| Worker                     | 领取对象                                            | 崩溃恢复                                                                                                                                                          | 重试边界                                      |
| -------------------------- | --------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------- |
| Command dispatcher         | 无有效 lease 的 `queued` Command                    | `claimed` 可安全重新领取并重做已冻结的 config/profile preflight；`dispatching` 过期把 Attempt 记为 `indeterminate`，Command 保持 `sent` 到原 deadline，不自动重放 | 仅 profile 与 Provider 合同共同明确允许时重试 |
| Command deadline scanner   | 无有效 lease 的到期 `queued`，或到期 `sent`/`acked` | queued 的 claimed Attempt 完成为 `not_dispatched`；其余保留既有 outcome，Command 转 `timeout`                                                                     | 不发设备请求                                  |
| Provider message receiver  | 厂商 HTTP callback 或 direct-device TCP frame       | 按 Provider 合同认证、校验并以 message dedupe key 原子保存一次 RawMessage；只有可信消息创建处理 Outbox                                                            | 接收重试不得重复创建 RawMessage               |
| Callback worker            | 已持久化且未完成处理的 RawMessage                   | Inbox/处理状态与领域条件更新保证重复消息不产生重复 Result、State 或 Event                                                                                         | 只重放本地处理，不重放设备动作                |
| Webhook dispatcher         | 到期 `pending`/`failed` Delivery                    | lease 到期可重新领取；每次 HTTP 调用保留独立 Attempt                                                                                                              | 有界重试，耗尽为 `dead`                       |

多实例领取使用数据库条件更新、行锁或 `FOR UPDATE SKIP LOCKED` 等等价机制。Redis 不能成为唯一事实来源。

### 内部消息传播的架构约束

根据已接受的 [ADR-0001](./architecture/technology-stack-adr.md)，目标架构使用 NATS JetStream 传播已持久化事实和唤醒异步 Worker，具体 Envelope、Subject 与消费规则以[内部异步消息合同](./messaging-contract.md)为准。基础设施不能改变本合同的领域对象、状态机、事务边界和恢复语义：

- 领域事务仍先在 PostgreSQL 原子写入业务事实、Event/Audit 与 Outbox；NATS 不参与该数据库事务，也不成为第二套状态来源。
- Outbox Publisher 可以重复发布；Consumer 必须使用 PostgreSQL Inbox/唯一约束和领域条件更新保证重复消息不产生重复最终效果。
- JetStream Ack、redelivery 或 delivery count 只表示消息处理进度，不能表达 Provider acceptance、Device ACK、Device final result 或 Command 状态。
- NATS 负责及时唤醒与解耦，数据库 Recovery Scanner 仍负责发现未发布 Outbox、过期 lease、deadline 和可恢复工作；Broker 通知丢失不能使权威事实永久停滞。
- Redis 继续只承载可重建状态和短期协调，不得用于替代 Outbox、Inbox、Command、Result、Delivery 或 Audit。

## 删除与保留

当前目标采用逻辑删除或禁用关键资源，不级联物理删除已有 Command、Attempt、Result、Event、Delivery 或 Audit。数据保留期限、删除合法性和备份/归档处置由 [Platform Target 的产品所有者裁决](./platform-target-contract.md#需要产品所有者裁决)唯一拥有；本合同只执行裁决前“不自动清理权威历史”的领域不变量，并在裁决后定义不破坏引用、幂等、审计和恢复的实现机制。

## Domain Unknown

厂商行为分别归 [WWTIOT Provider 合同](./providers/wwtiot.md)与 [Omni Provider 合同](./providers/omni.md)，业务可接受性与数据政策归 [Platform Target](./platform-target-contract.md)，容量和灾难恢复指标归 [Reliability and Capacity](./operations/reliability-capacity.md)。本合同只拥有以下领域恢复机制 Unknown：

| 未知内容                                                               | 阻塞                                                                    | 不阻塞                                                                 | 关闭证据                                                                  |
| ---------------------------------------------------------------------- | ----------------------------------------------------------------------- | ---------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| Webhook secret 解密失败是否消耗 DeliveryAttempt、允许重试或立即 `dead` | 该失败分支的 Delivery 终局、attempt count、manual replay 时点和恢复验收 | 停止 HTTP、禁止 fallback secret、写一次去重安全审计及正常 Webhook 链路 | 密钥依赖的可恢复性分类、批准的重试/立即 dead 规则、审计去重规则与故障测试 |
