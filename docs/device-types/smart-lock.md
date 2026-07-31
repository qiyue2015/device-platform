---
title: smart-lock Device Type Capability 合同
created: 2026-07-31
updated: 2026-07-31
status: frozen-for-implementation
freeze_revision: 2026-07-31.3
profile_revision: 1
---

# smart-lock Device Type Capability 合同

本文定义当前真实智能锁所需的规范化 capability 语义。它从属于[平台边界合同](../platform-boundary-contract.md)、[当前目标合同](../platform-target-contract.md)、[领域模型合同](../domain-model-contract.md)和通用 [API 合同](../api-contract.md)，不定义任何厂商 HTTP 字段、签名或 Provider 路径。

稳定 profile metadata 为 `code=smart-lock`、`revision=1`、`name=Smart Lock`。revision 只在 capability 语义或安全参数变化时递增；文案调整不产生第二套运行 profile。

## 当前规范化 action

| action identifier | risk   | 规范化意图                       | 当前安全 profile                                                                                                                                       | 仍为 Unknown                                              |
| ----------------- | ------ | -------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------- |
| `unlock`          | `high` | 请求智能锁执行解锁物理动作。     | payload 必须是空 object；`dispatch_once`；不因 `connection_status=unknown` 阻止 Cloud API 投递；不自动重试、离线排队、晚到补偿或调用方覆盖策略。       | Device ACK、final result、厂商失败码和真实执行时长。      |
| `lock`            | `high` | 请求智能锁执行闭锁物理动作。     | payload 必须是空 object；`dispatch_once`；不因 `connection_status=unknown` 阻止 Cloud API 投递；不自动重试、离线排队、晚到补偿或调用方覆盖策略。       | Device ACK、final result、厂商失败码和真实执行时长。      |
| `query_status`    | `low`  | 请求读取智能锁可提供的技术状态。 | payload 必须是空 object；`dispatch_once`；不自动重试、离线排队或调用方覆盖策略。可信 callback 可更新 DeviceState，但在关联规则未确认前不推进 Command。 | callback 与命令的关联、可安全重试条件及完整状态字段集合。 |

以上 action 是当前 smart-lock Device Type 的规范化能力，不是 Platform Core 全局枚举，也不证明任何 Provider 已按厂商合同支持它们。

`dispatch_once` 是当前保守 delivery policy：创建后只允许 dispatcher 发起一次 Provider 请求。Command 的派发期限为进入 `queued` 后 30 秒，Provider 请求 timeout 为 10 秒，设备结果观察期限为进入 `sent` 后 60 秒。三者是平台拥有的 profile 参数，不是对 WWTIOT 设备响应时间的厂商事实；可以通过新发布的 profile revision 调整，不能由部署环境或单次 API 调用静默覆盖。请求可能送达而结果不明时进入 `unknown`；收到可信 Provider acceptance 但没有 Device final result 时保持 `sent`，观察期限到达后进入 `timeout`。

三个 action 的 API profile 均固定返回 `payload_schema={"type":"object","maxProperties":0,"additionalProperties":false}`、`dispatch_deadline_ms=30000`、`provider_request_timeout_ms=10000`、`result_observation_timeout_ms=60000`、`retry_allowed=false` 和 `delivery_policy_override_allowed=false`。

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
