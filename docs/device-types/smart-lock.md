---
title: smart-lock Device Type Capability 合同
created: 2026-07-31
updated: 2026-07-31
status: frozen-for-implementation
freeze_revision: 2026-07-31.4
profile_revision: 2
---

# smart-lock Device Type Capability 合同

本文定义当前真实智能锁所需的规范化 capability 语义。它从属于[平台边界合同](../platform-boundary-contract.md)、[当前目标合同](../platform-target-contract.md)、[领域模型合同](../domain-model-contract.md)和通用 [API 合同](../api-contract.md)，不定义任何厂商 HTTP 字段、签名或 Provider 路径。

稳定 profile metadata 为 `code=smart-lock`、`revision=2`、`name=Smart Lock`。本次 revision 2 将物理动作改为 `online_only`；这是安全参数变化，不是文案调整。API、后台和数据库的规范 wire identifier 是 `smart-lock`；文件路径 slug 也是 `smart-lock.md`，`smart_lock` 仅是需显式迁移的旧实现值。

## 当前规范化 action

| action identifier | risk   | 规范化意图                       | 当前安全 profile                                                                                                                                       | 仍为 Unknown                                              |
| ----------------- | ------ | -------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------- |
| `unlock`          | `high` | 请求智能锁执行解锁物理动作。     | payload 必须是空 object；`online_only`；强制 `idempotency_key`；不自动重试、离线排队、晚到补偿或调用方覆盖策略。                                       | Device ACK、final result、厂商失败码和真实执行时长。      |
| `lock`            | `high` | 请求智能锁执行闭锁物理动作。     | payload 必须是空 object；`online_only`；强制 `idempotency_key`；不自动重试、离线排队、晚到补偿或调用方覆盖策略。                                       | Device ACK、final result、厂商失败码和真实执行时长。      |
| `query_status`    | `low`  | 请求读取智能锁可提供的技术状态。 | payload 必须是空 object；`dispatch_once`；不自动重试、离线排队或调用方覆盖策略。可信 callback 可更新 DeviceState，但在关联规则未确认前不推进 Command。 | callback 与命令的关联、可安全重试条件及完整状态字段集合。 |

以上 action 是当前 smart-lock Device Type 的规范化能力，不是 Platform Core 全局枚举，也不证明任何 Provider 已按厂商合同支持它们。

`unlock`/`lock` 使用 `online_only` 是本次采用的保守产品决策，不是现有代码、厂商资料或真实设备证据证明的既有事实。只有 `connection_status=online` 时才允许创建并在派发前继续执行物理动作；`offline|unknown` 返回 `409 device_not_online` 且不创建 Command。在线事实必须来自对应 Provider 合同允许的可信上报，不能由 Provider HTTP acceptance、人工填写或 simulator outcome 推断。创建后到派发前连接状态失去 `online` 时，Command 以 `failed/device_not_online` 终止且不发出请求。

`query_status` 继续使用 `dispatch_once`，允许在连接状态未知时发起一次 Provider 请求，以便取得状态证据。三个 action 均禁止自动重试；在 query 状态结果、Provider 幂等和重放安全性经真实证据确认前，`query_status` 也不自动重试。这些是当前保守产品决策，不是厂商保证。

Command 的派发期限为进入 `queued` 后 30 秒，Provider 请求 timeout 为 10 秒，设备结果观察期限为进入 `sent` 后 60 秒。三者是平台拥有的 profile 参数，不是对 WWTIOT 设备响应时间的厂商事实；不能由部署环境或单次 API 调用静默覆盖。请求可能送达而结果不明时，Attempt 记为 `outcome=indeterminate`，Command 保持 `sent`，观察期限到达后进入终态 `timeout`；可信 Provider acceptance 同样只使 Command 保持 `sent`。迟到结果只追加 Result/Event，不改写 `timeout`。

三个 action 的 API profile 均固定返回 `payload_schema={"type":"object","maxProperties":0,"additionalProperties":false}`、`dispatch_deadline_ms=30000`、`provider_request_timeout_ms=10000`、`result_observation_timeout_ms=60000`、`retry_allowed=false` 和 `delivery_policy_override_allowed=false`。`unlock`/`lock` 的 `delivery_policy=online_only`，`query_status` 的 `delivery_policy=dispatch_once`。

## 分层约束

- Platform Core 只承载 action identifier、payload、状态、幂等、Attempt 和 confirmation level。
- Provider 合同负责说明具体 Provider 如何映射这些 action，以及映射目前属于代码事实、厂商合同事实还是 Unknown。
- 共享单车应用负责为什么发起动作、用户是否有权发起，以及结果如何改变订单或计费。
- 本合同不引入动态 capability 编辑器、规则引擎、继承体系或假想设备类型目录。
- API、后台和数据库使用规范 code `smart-lock`；`smart_lock` 只能作为迁移期间显式处理的旧实现值，不能成为第二个 Device Type。

## DeviceState 规范化

基于当前 V2 资料，签名可信且能唯一映射 Device 的设备信息 callback 可以产生以下规范化状态：

| 字段              | 语义                         | 映射限制                                                                                                    |
| ----------------- | ---------------------------- | ----------------------------------------------------------------------------------------------------------- |
| `lock_state`      | `locked` 或 `unlocked`       | `lockstatus=0` 映射为 `locked`，`lockstatus=1` 映射为 `unlocked`；其他值保存为 `unknown` 并保留原始消息。   |
| `battery_percent` | 厂商上报的电量百分比         | 只接受 `0..100`；越界消息不写入该规范化字段。                                                               |
| `reported_at`     | 设备消息中的报告时间         | 只接受带明确 offset 的 RFC3339 并规范化为 UTC；其他格式或时区不猜测，使用 `null` 并保留平台 `observed_at`。 |
| `observed_at`     | 平台成功验证并接收消息的时间 | 由平台生成。                                                                                                |

V2 示例还包含位置、电压和信号相关字段，但地图、轨迹和完整遥测 profile 不在当前目标；这些字段可在受保护 RawMessage 中保留，不进入当前规范化 DeviceState 合同。

## 进入 profile 的证据门槛

厂商 ACK、final result、失败码、设备执行时长、callback 关联、自动重试或补偿只有在厂商合同与受控真实设备证据一致后才能从 Unknown 变为已确认。平台为防止永久悬挂而设置的 timeout 不得被表述为厂商时限。代码旧默认值、旧文档或模拟器行为不能单独升级为设备事实。

## 真实设备验收矩阵

每个 action 都必须在同一受控设备、明确写操作授权和可关联观测条件下覆盖以下矩阵。`unlock`/`lock` 的物理动作需在隔离环境执行；人工物理观察只进入验收记录，不直接推进运行时 Command，除非另有通过签名和关联规则验证的机器证据。

| action         | Provider 接受                                                                                       | Provider 拒绝                                        | 无最终结果                                              | 延迟/重复 callback                                              | 物理或状态观察                                                       |
| -------------- | --------------------------------------------------------------------------------------------------- | ---------------------------------------------------- | ------------------------------------------------------- | --------------------------------------------------------------- | -------------------------------------------------------------------- |
| `unlock`       | 记录同步响应原文摘要；`result=ok`/`cmd send ok` 只能得到 `provider_accepted`，Command 保持 `sent`。 | 记录明确拒绝与稳定失败原因，不把 HTTP 异常猜成拒绝。 | 到 60 秒进入 `timeout`；已有 Attempt/Result 不覆盖。    | 验证签名、命令关联、去重和迟到追加；终态不改写。                | 记录锁体是否实际解锁、观测时间和证据来源；与平台 confirmation 对照。 |
| `lock`         | 同上，只证明 Provider 接受闭锁请求。                                                                | 同上，记录闭锁拒绝。                                 | 到 60 秒进入 `timeout`。                                | 同上，重复消息不得产生重复 Result/Event。                       | 记录锁体是否实际闭锁及其证据；不得由 HTTP acceptance 推断。          |
| `query_status` | 同上，只证明 Provider 接受查询。                                                                    | 同上，记录查询拒绝。                                 | 到 60 秒进入 `timeout`，DeviceState 保持原值或 `null`。 | 验证回调是否能关联本次查询；重复/迟到消息只追加一次规范化事实。 | 将回调的 `lock_state`/电量/时间与同刻受控观察对照，记录不一致。      |

每个格子的验收记录至少包含 Project/Device、Command/Attempt/Provider request key、action、请求与响应时间、脱敏 Provider 证据、callback 去重键、平台状态/confirmation、人工或设备侧观察、测试人员和结论。任一格无法执行时必须保留 Unknown 及原因，不能以本地伪 Provider、simulator 或其他 action 的结果代替。
