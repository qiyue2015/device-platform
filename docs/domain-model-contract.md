---
title: 领域模型与一致性合同
created: 2026-07-31
updated: 2026-07-31
status: frozen-for-implementation
freeze_revision: 2026-07-31.1
---

# 领域模型与一致性合同

本文定义当前目标所需的领域对象、稳定标识、归属、约束、事务边界与恢复责任。它从属于[平台边界合同](./platform-boundary-contract.md)和[当前平台目标合同](./platform-target-contract.md)，并约束 [API 与生命周期合同](./api-contract.md)、Device Type 合同、Provider 合同、schema 和运行时代码。

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
Device 1 --- n DeviceState
Device 1 --- n RawMessage
Project 1 --- n Event
Event 1 --- n WebhookDelivery 1 --- n WebhookDeliveryAttempt
Project 1 --- n AuditLog
Provider 1 --- n Device                 (by provider_code)
```

### Project

保存 `id`、`name`、`api_key_hash`、可选 `webhook_url`、版本化加密 `webhook_secret`、`ip_whitelist` 和时间戳。

- API Key 明文只在创建或轮换成功的单次响应中出现；列表、详情、日志、Event、Audit 和数据库均不得保存或返回明文。
- API Key 轮换替换旧 hash，旧 key 立即失效，并写入技术审计。
- `webhook_url` 与 `webhook_secret` 属于 Project aggregate 的唯一 Webhook 配置，不建立由另一服务独立维护的端点状态。持久化可以使用版本化子表，但 API 与领域写入只有一个 Project 配置入口。
- Webhook secret 与 API Key 分离，明文只在首次生成或显式轮换的单次响应中出现；数据库使用部署级外部密钥以 AES-256-GCM 保存每个版本的 ciphertext、nonce 和 key version，普通日志与读取 API 不得返回明文、密文或 nonce。移除 endpoint 不得暴露旧 secret；解密失败必须让相关 Delivery 失败关闭并产生安全审计，不能使用 fallback secret。
- API Key 使用密码学安全随机源生成至少 256 bit secret，并以 SHA-256 digest 持久化和查找；数据库不保存可恢复明文。
- `ip_whitelist` 为空表示允许任意来源，但仍记录认证结果；非空时必须按认证层确认的客户端 IP 执行。

### Provider

Provider 是部署级 adapter 注册项，当前只有 `wwtiot` 与显式 simulator。它不是业务可创建资源，也不引入假想的通用 Provider 元模型。

- Provider code 在一个部署内唯一且稳定。
- Provider endpoint、UserID、UserKey 等敏感配置由服务端环境配置提供；后台只读展示 code、名称、adapter、传输方式和 `integration_status=unconfigured|configured_unverified|verified`，不返回 secret，也不通过普通业务 API 修改 secret。
- 配置变化在服务启动时生效。需要在线变更或数据库托管 secret 时，必须先形成新的安全合同，不在当前目标中猜测实现。

### DeviceType

Device Type 是代码审查并随发布交付的只读 capability profile。当前唯一 profile code 为 `smart-lock`。

- profile 定义 action identifier、payload schema、风险等级、投递策略、平台观察期限和重放限制。
- 当前目标不提供动态 Device Type CRUD、继承、表达式或低代码规则编辑器。
- 数据库可以保存 profile 的发布快照用于外键和诊断，但运行时不得同时存在另一套语义来源；启动时发现 code 或 profile revision 漂移应失败关闭或执行显式 migration。

### Device

保存 `id`、`project_id`、`device_type_id`、`name`、Provider identity、接入元数据、`connection_status`、`lifecycle_status` 和时间戳。

- 一个 Device 必须且只能属于一个 Project。
- 当前不支持跨 Project 转移。
- 活跃 Device 的 `(provider_code, provider_device_id)` 在整个平台唯一。WWTIOT callback 不携带 `project_id`，只有全局唯一才能无歧义映射。
- `connection_status` 只表达有证据的技术连接事实。WWTIOT 当前没有已确认 heartbeat 合同，因此不得因一次 HTTP acceptance 推导 `online`。
- `lifecycle_status=disabled` 的 Device 不接受新命令；逻辑删除记录保持审计可追溯。

### Command

保存 `id`、`project_id`、`device_id`、action identifier、规范化 payload、状态、delivery policy、幂等字段、原因、观察期限和时间戳。

- Command 的 `project_id` 必须与关联 Device 的 `project_id` 相同；该约束必须由同一事务中的锁定校验或数据库等价约束保证。
- `(project_id, idempotency_key)` 唯一且 key 不为空；相同 key 和相同 canonical request hash 返回原 Command，不重复产生投递或 Event。
- Command 状态只表达平台拥有证据的最高生命周期事实。Provider acceptance、Device ACK 和 Device final result 不得合并。
- `reason_code` 是稳定机器码，`reason_detail` 是脱敏诊断文本；调用方不得依赖自由文本作分支。

### CommandAttempt

每次 dispatcher 领取 Command 准备向 Gateway/Provider 投递时建立一个独立 Attempt，保存 attempt number、phase、adapter、provider request key、开始/完成时间、outcome、confirmation level、evidence status、脱敏请求/响应摘要和错误。

- `(command_id, attempt_no)` 唯一；Provider request key 在对应 Provider 范围内唯一。
- `phase` 只能按 `claimed -> dispatching -> completed` 或 `claimed -> completed` 单调迁移。`claimed` 尚未承诺外部调用；`dispatching` 表示 worker 已在事务中承诺立即执行外部调用，Command 同时进入 `sent`；`completed` 表示本次 Attempt 的可观测结果已持久化。非 completed Attempt 的 outcome 为 `null`，completed Attempt 必须有稳定 outcome。
- `confirmation_level` 只能是 `none`、`transport_sent`、`provider_accepted`、`device_acked`、`device_final`。
- `evidence_status` 只能是 `none`、`verified` 或 `unverified`。它表示该 confirmation evidence 的来源真实性和校验条件是否满足，不改变 confirmation level 本身；`success` 必须同时具有 `device_final` 与 `verified`。
- 当前稳定 `outcome` 为 `not_dispatched`、`invalid_request`、`provider_accepted`、`provider_rejected`、`transport_error_before_send`、`transport_error_after_send`、`invalid_response`、`device_acked`、`device_succeeded` 和 `device_failed`。`not_dispatched` 只用于 claimed 阶段被取消或超过派发期限，confirmation/evidence 均为 `none`。三个设备结果 outcome 只有在对应 Provider 合同具有可信证据来源时才能产生；当前 WWTIOT 和 simulator 均不能产生它们。Command 结果观察期限到达由 deadline scanner 写状态与 Event，不伪造一次新的 Provider Attempt 或覆盖既有 Attempt outcome。
- confirmation level 单调提升，不能被后来的较低层事实覆盖。

### DeviceState 与 RawMessage

- 每次可信上行创建不可变 `RawMessage`，保存 Provider、方向、接收时间和受控原文；secret、认证签名和不必要的敏感字段必须脱敏或分离保护。
- 验证通过并完成 Device 映射后，才可从 RawMessage 产生规范化 `DeviceState` 与 Event。
- `Device.current_state` 是最新 DeviceState 的派生读取，不建立第二套可独立写入的 JSON 状态。
- 无法验证签名、无法唯一映射设备或 schema 不合法的 callback 不更新 DeviceState；记录受控失败审计，不回显 secret。

### Event 与 Outbox

Event 是不可变的规范化技术事实，至少保存 `event_id`、`event_type`、`project_id`、可选 Device/Command 关联、发生时间、source 和 payload。

- 当前 Event envelope 的 `schema_version` 固定为整数 `1`。稳定 `event_type` 只有 `device.created`、`device.lifecycle_changed`、`device.connection_changed`、`device.state_updated`、`command.created` 和 `command.status_changed`；新增类型或改变 payload 语义必须先修订 API 合同并提升相应 schema version。
- 会改变领域状态的事务同时写入相关 Event 与待投递 Outbox/Delivery；不得在提交后用 best-effort hook 补写。
- Event payload 只含业务应用可消费的规范化技术事实，不泄露 Provider secret 或原始敏感消息。
- 同一领域事实使用稳定 deduplication key，重复 callback 或 worker 重放不能创建重复最终效果。

### WebhookDelivery 与 WebhookDeliveryAttempt

- 每个 Event 按其 Project 在 Event 创建时生效的 Webhook 配置版本形成一个 Delivery；Delivery 保存该配置 version 和 target snapshot。旧 secret version 只保留到引用它的 Delivery 全部终止并超过受控清理期，读取 API 永不返回明文。未配置 endpoint 时 Event 仍保存，但不创建投递。
- 每次 HTTP 请求创建不可变 DeliveryAttempt。Delivery 保存聚合状态、下一次执行时间和当前 attempt count，不替代 Attempt 历史。
- 手动重发使用重发时当前启用的 Project Webhook 配置创建新的 Delivery，并通过 `replay_of_delivery_id` 指向原 dead Delivery；原记录与原配置 snapshot 保持不变。

### AuditLog

AuditLog 记录管理员、Open API 机器身份、Provider callback 或 system worker 的关键技术操作。它保存 actor、Project、资源、action、result、来源 IP、request ID、脱敏 metadata 和时间。

审计记录不是领域 Event，也不承载共享单车业务审计。API 合同要求审计的领域写入应与该写入处于同一事务；登录失败等没有领域事务的安全事件独立持久化。worker 的状态事实由 Attempt 与 Event 记录，除安全或人工操作外不重复制造 Audit。

## 原子事务边界

以下操作各自在一个数据库事务中完成：

1. 创建/更新 Project、API Key 轮换、Webhook 配置变更及对应 Audit。
2. 创建/变更 Device、Device Type 校验及对应 Audit/Event/Outbox。
3. 创建 Command：锁定并校验 Project/Device、写 Command 与幂等约束、初始 Event/Outbox 和 Audit。
4. 领取 Command：仅对无有效 lease 的 `queued` Command 条件创建 `phase=claimed` Attempt 和 lease；Command 仍为 `queued`，有有效 lease 时取消与 deadline scanner 都不得越过该所有权。
5. 承诺派发：短事务校验 lease/Attempt 仍有效，将 Attempt 改为 `dispatching`、Command 改为 `sent`，设置 `sent_at` 与 result deadline 并写状态 Event/Outbox；事务提交后立即执行外部调用。
6. 处理 Provider 响应或可信设备结果：条件更新 Attempt 和 Command，写 Event/Outbox 与所需 Audit。
7. 处理可信 callback：保存 RawMessage、更新 DeviceState、写 Event/Outbox；只有存在已确认且无歧义的命令关联规则时才更新 Command。
8. 创建 Webhook Delivery、完成一次 DeliveryAttempt、进入 retry/dead，以及创建 manual replay。

外部 HTTP 调用不得包在长数据库事务中。`claimed` lease 过期且从未进入 `dispatching` 时，可以由 worker 重新领取同一 Attempt，因为合同保证尚未承诺外部调用。`dispatching` 一旦提交，进程在调用前后或结果落库前崩溃都无法证明未发送；恢复器将 Attempt 完成为 `transport_error_after_send`，confirmation 为 `transport_sent`、evidence 为 `unverified`，Command 转 `unknown`。不可安全重放的 action 不自动重发。

## Worker 所有权与恢复

| Worker                    | 领取对象                                            | 崩溃恢复                                                                                      | 重试边界                                      |
| ------------------------- | --------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------- |
| Command dispatcher        | 无有效 lease 的 `queued` Command                    | `claimed` 可安全重新领取；`dispatching` 过期转 `unknown`，不自动重放                          | 仅 profile 与 Provider 合同共同明确允许时重试 |
| Command deadline scanner  | 无有效 lease 的到期 `queued`，或到期 `sent`/`acked` | queued 的 claimed Attempt 完成为 `not_dispatched`；其余保留既有 outcome，Command 转 `timeout` | 不发设备请求                                  |
| Provider callback handler | 签名通过的 RawMessage                               | 依靠 Provider message dedupe key 或受控内容 hash 去重                                         | callback 可安全重复处理，不重复最终效果       |
| Webhook dispatcher        | 到期 `pending`/`failed` Delivery                    | lease 到期可重新领取；每次 HTTP 调用保留独立 Attempt                                          | 有界重试，耗尽为 `dead`                       |

多实例领取使用数据库条件更新、行锁或 `FOR UPDATE SKIP LOCKED` 等等价机制。Redis 不能成为唯一事实来源。

## 删除与保留

当前目标采用逻辑删除或禁用关键资源，不级联物理删除已有 Command、Attempt、Event、Delivery 或 Audit。具体保留期限属于运行政策 Unknown；在政策确认前不得自动清理审计链。

## Unknown

- 生产保留期限、备份恢复点与灾难恢复指标，需要部署要求确认。
- WWTIOT callback 的真实送达、签名校验、重复行为和命令关联能力，需要厂商确认及受控设备验证。
- WWTIOT Provider acceptance 是否足以支持共享单车正式业务，由产品所有者基于真实证据决定。
- 多实例规模与吞吐目标尚未给出；实现必须保证正确性并提供可测量基线，但不猜测容量数字。
