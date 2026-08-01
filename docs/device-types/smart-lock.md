---
title: smart-lock Device Type Capability 合同
updated: 2026-08-01
status: frozen-for-implementation
contract_revision: 2026-08-01
profile_revision: 2
---

# smart-lock Device Type Capability 合同

本文定义当前真实智能锁所需的规范化 capability 语义。它从属于[平台边界合同](../platform-boundary-contract.md)、[当前目标合同](../platform-target-contract.md)、[领域模型合同](../domain-model-contract.md)和通用 [API 合同](../api-contract.md)，不定义任何厂商 HTTP 字段、签名或 Provider 路径。

稳定 profile metadata 为 `code=smart-lock`、`revision=2`、`name=Smart Lock`。revision 2 的物理动作使用 `online_only`。API、后台和数据库的规范 wire identifier 是 `smart-lock`；其他拼写不是允许的别名。

## 当前规范化 action

| action identifier | risk   | 规范化意图                       | 安全 profile                                                                                                                                           |
| ----------------- | ------ | -------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `unlock`          | `high` | 请求智能锁执行解锁物理动作。     | payload 必须是空 object；`online_only`；强制 `idempotency_key`；不自动重试、离线排队、晚到补偿或调用方覆盖策略。                                       |
| `lock`            | `high` | 请求智能锁执行闭锁物理动作。     | payload 必须是空 object；`online_only`；强制 `idempotency_key`；不自动重试、离线排队、晚到补偿或调用方覆盖策略。                                       |
| `query_status`    | `low`  | 请求读取智能锁可提供的技术状态。 | payload 必须是空 object；`dispatch_once`；不自动重试、离线排队或调用方覆盖策略。可信 callback 可更新 DeviceState，但在关联规则未确认前不推进 Command。 |

以上 action 是当前 smart-lock Device Type 的规范化能力，不是 Platform Core 全局枚举，也不证明任何 Provider 已按厂商合同支持它们。

`unlock`/`lock` 使用 `online_only` 是当前保守产品约束，不是厂商事实。只有 `connection_status=online` 时才允许创建并在派发前继续执行物理动作；`offline|unknown` 返回 `409 device_not_online` 且不创建 Command。在线事实必须来自对应 Provider 合同允许的可信上报，不能由 Provider HTTP acceptance、人工填写或 simulator outcome 推断。创建后到派发前连接状态失去 `online` 时，Command 以 `failed/device_not_online` 终止且不发出请求。是否改为允许未知在线状态下的 `dispatch_once` 只由 [Platform Target 的产品裁决](../platform-target-contract.md#需要产品所有者裁决)决定。

`query_status` 继续使用 `dispatch_once`，允许在连接状态未知时发起一次 Provider 请求，以便取得状态证据。三个 action 均禁止自动重试；在 query 状态结果、Provider 幂等和重放安全性经真实证据确认前，`query_status` 也不自动重试。这些是当前保守产品决策，不是厂商保证。

Command 的派发期限为进入 `queued` 后 30 秒，Provider 请求 timeout 为 10 秒，设备结果观察期限为进入 `sent` 后 60 秒。三者是平台拥有的 profile 参数，不是任一厂商的设备响应时限；不能由部署环境或单次 API 调用静默覆盖。请求可能送达而结果不明时，Attempt 记为 `outcome=indeterminate`，Command 保持 `sent`，观察期限到达后进入终态 `timeout`；可信 Provider acceptance 或已验证的本地 socket 完整写入也只使 Command 保持 `sent`。迟到结果只追加 Result/Event，不改写 `timeout`。

三个 action 的 API profile 均固定返回 `payload_schema={"type":"object","maxProperties":0,"additionalProperties":false}`、`dispatch_deadline_ms=30000`、`provider_request_timeout_ms=10000`、`result_observation_timeout_ms=60000`、`retry_allowed=false` 和 `delivery_policy_override_allowed=false`。`unlock`/`lock` 的 `delivery_policy=online_only`，`query_status` 的 `delivery_policy=dispatch_once`。

## 分层约束

- Platform Core 只承载 action identifier、payload、状态、幂等、Attempt 和 confirmation level。
- Provider 合同负责说明具体 Provider 如何映射这些 action、厂商资料支持哪些映射，以及哪些行为仍是 Unknown。
- 共享单车应用负责为什么发起动作、用户是否有权发起，以及结果如何改变订单或计费。
- 本合同不引入动态 capability 编辑器、规则引擎、继承体系或假想设备类型目录。
- API、后台和数据库只使用规范 code `smart-lock`，不得维护第二个 Device Type 标识。

## DeviceState 规范化

只有对应 Provider 合同允许、真实性已验证且能唯一映射 Device 的上行事实，才可以产生以下厂商无关的规范化状态：

| 字段              | 语义                         | 映射限制                                                                                                    |
| ----------------- | ---------------------------- | ----------------------------------------------------------------------------------------------------------- |
| `lock_state`      | `locked`、`unlocked` 或 `unknown` | Provider adapter 只按各自合同映射；未知或越界值不得猜测。                                                    |
| `battery_percent` | 厂商上报的电量百分比             | 规范值只接受整数 `0..100`；电压到百分比的换算必须由 Provider 合同提供可验证规则，否则不写入。               |
| `reported_at`     | 设备消息中的报告时间             | 只有 Provider 能证明格式和时区时才规范化为 UTC；否则为 `null`。                                             |
| `observed_at`     | 平台验证并接收消息的时间         | 由平台生成，不能替代 `reported_at` 或证明设备执行时间。                                                      |

厂商资料中的位置、电压、信号、骑行时间、模式、速度、里程等字段不进入当前 DeviceState；需要诊断时只能按 Provider 合同以受保护、脱敏的 RawMessage 保存。具体厂商字段名、数值映射和消息真实性规则只能出现在 WWTIOT 或 Omni Provider 合同中。

## Capability Unknown

| 未知内容                                      | 阻塞                              | 不阻塞                                  | 关闭证据                                     |
| --------------------------------------------- | --------------------------------- | --------------------------------------- | -------------------------------------------- |
| 可信 Device ACK 与 final result 是否存在      | `acked`、`success` 和真实设备验收 | 截至 Provider evidence 的 Core 实现     | Provider 正式合同与受控真实设备关联证据      |
| 设备失败码与真实执行时间分布                  | 设备侧诊断口径、观察期限调优      | 当前 60 秒平台观察期限和稳定通用 reason | 覆盖成功、拒绝、设备失败、迟到的真实设备样本 |
| `query_status` callback 与发起 Command 的关联 | 由查询结果推进 Command            | callback 仅更新可信 DeviceState         | 厂商关联规则与重复/迟到验证                  |
| 三项 action 的自动重试或补偿安全性            | 启用任何自动重试、离线队列或补偿  | 当前单次发送和禁自动重试策略            | 厂商幂等合同、受控重复测试和产品风险批准     |

平台设置的派发、请求和观察 timeout 都是自身安全参数，不得表述为厂商时限。旧代码、模拟器或单次成功不能关闭上述 Unknown。

## 真实设备验收矩阵

每个 action 都必须在同一受控设备、明确写操作授权和可关联观测条件下覆盖以下矩阵。`unlock`/`lock` 的物理动作需在隔离环境执行；人工物理观察只进入验收记录，不直接推进运行时 Command，除非另有通过签名和关联规则验证的机器证据。

| action         | 下行较低层证据                                                                                                      | 拒绝/发送前失败                                              | 无可信最终结果                                         | 重复/迟到/不可关联上行                                         | 物理或状态观察                                                       |
| -------------- | ------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------ | ------------------------------------------------------ | -------------------------------------------------------------- | -------------------------------------------------------------------- |
| `unlock`       | 记录各 Provider 实际最高证据；`provider_accepted` 或 `transport_sent` 都只能使 Command 保持 `sent`。                | 只按 Provider 可证明事实分类；未发出、明确拒绝和未知必须区分。 | 到 60 秒进入 `timeout`；已有 Attempt/Result 不覆盖。   | 验证身份、关联、去重和迟到追加；不满足时只保留诊断事实。       | 记录锁体是否实际解锁、观测时间和证据来源；与平台 confirmation 对照。 |
| `lock`         | 同上；Provider/profile 没有主动闭锁能力时必须在发送前明确 unsupported。                                             | 同上，不能把传输异常猜成设备拒绝。                           | 到 60 秒进入 `timeout`。                               | 同上，重复消息不得产生重复 Result/Event。                      | 记录锁体是否实际闭锁；不得由 Provider acceptance 或 socket 写入推断。 |
| `query_status` | 同上；查询 response 只有经 Provider 合同验证真实性和关联后才能产生可信 DeviceState 或推进 Command。               | 同上，保持旧 DeviceState 或 `null`。                         | 到 60 秒进入 `timeout`，DeviceState 不由超时自动改变。 | 重复/迟到消息只追加一次允许的事实；不可关联消息不得跨 Device。 | 将规范化锁态/电量与同刻受控观察对照，记录不一致。                    |

WWTIOT 与 Omni 必须分别完成本矩阵，Omni 还必须逐个已证明的 provider profile 记录 capability 差异。每个格子的验收记录至少包含 Project/Device、Provider/profile、Command/Attempt/Provider request key、action、请求与响应时间、脱敏 Provider 证据、消息去重键、平台状态/confirmation、人工或设备侧观察、测试人员和结论。任一格无法执行时必须保留 Unknown 及原因，不能以另一 Provider、本地伪 peer、simulator 或其他 action 的结果代替。
