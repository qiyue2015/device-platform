---
title: 内部异步消息合同
updated: 2026-08-01
status: implementation-unknown
decision: ADR-0001
contract_revision: 2026-08-01
schema_version: 1
---

# 内部异步消息合同

本文定义 Device Platform 使用 NATS JetStream 传播内部异步工作的稳定消息边界。它从属于平台、目标、领域模型和 API 合同，并执行 [ADR-0001](./architecture/technology-stack-adr.md)。消息只能传播已持久化事实或唤醒待处理工作，不能创造第二套领域状态。

## 基本语义

- 投递保证固定为 **at-least-once**；发布者和消费者都必须容忍重复。
- 不承诺全局顺序。需要顺序的领域迁移由 PostgreSQL 条件更新、版本或 lease 保证。
- `Ack` 只表示消费者已经完成该消息对应的可恢复处理，不表示真实设备执行成功。
- 消息到达不授权设备动作；Command Worker 必须重新读取并锁定 PostgreSQL 中的 Command、Attempt、Device 和 capability policy。
- JetStream 消息保留、`Nats-Msg-Id` 去重窗口和 consumer pending 状态都不是领域正确性的唯一依据。
- 不允许业务代码绕过 Transactional Outbox 直接发布需要可靠传输的消息。

## 消息 Envelope

所有消息使用 UTF-8 JSON object，拒绝未知顶层字段。版本 1 固定字段如下：

```json
{
  "message_id": "01900000-0000-7000-8000-000000000001",
  "schema_version": 1,
  "message_type": "command.dispatch.requested",
  "occurred_at": "2026-08-01T00:00:00Z",
  "producer": "command-service",
  "project_id": "01900000-0000-7000-8000-000000000002",
  "aggregate_type": "command",
  "aggregate_id": "01900000-0000-7000-8000-000000000003",
  "correlation_id": "01900000-0000-7000-8000-000000000004",
  "causation_id": null,
  "payload": {
    "command_id": "01900000-0000-7000-8000-000000000003"
  }
}
```

字段约束：

- `message_id`：平台生成的不可变唯一 ID，同时作为 Outbox ID 和默认消费去重键；
- `schema_version`：正整数，版本 1 固定为`1`；
- `message_type`：稳定业务类型，不含版本后缀；
- `occurred_at`：权威事实形成时间，不是 Broker 接收时间；
- `producer`：稳定模块标识；
- `project_id`：Project 范围消息必须存在，部署级消息才允许`null`；
- `aggregate_type`、`aggregate_id`：主要领域对象；
- `correlation_id`：贯穿原始 HTTP 请求、消息、Worker、Provider 与 Webhook；
- `causation_id`：直接导致本消息的 request ID 或上游 message ID，没有时为`null`；
- `payload`：只携带消费者定位事实所需的稳定 ID 和不可变小型元数据，不复制完整领域 aggregate。

消息不得包含 API key、Webhook secret、Provider secret、Authorization header、未脱敏 Provider 报文、数据库连接串或其他凭据。原始报文需要保留时，先按领域合同加密或脱敏写入 PostgreSQL，消息只携带其 ID。

## Subject 与所有权

Subject 格式固定为：

```text
dp.<domain>.<action>.v<schema_version>
```

当前合同类型：

| Subject                            | message_type                 | 发布者                          | 主消费者        | 用途                          |
| ---------------------------------- | ---------------------------- | ------------------------------- | --------------- | ----------------------------- |
| `dp.command.dispatch.requested.v1` | `command.dispatch.requested` | commands Outbox Publisher       | Command Worker  | 唤醒已持久化 Command 的派发   |
| `dp.provider.message.received.v1`  | `provider.message.received`  | provider-inbox Outbox Publisher | Provider Message Worker | 处理已认证和持久化 RawMessage |
| `dp.webhook.delivery.requested.v1` | `webhook.delivery.requested` | webhook-outbox Outbox Publisher | Webhook Worker  | 唤醒已持久化 Delivery 的投递  |

新增消息类型必须先定义发布者、消费者、权威来源、幂等效果、失败恢复与 schema version，再进入实现。临时 Subject 和消费者私有 payload 不得成为生产合同。

## Transactional Outbox

1. 领域事务同时写入业务事实、Event/Audit 和 Outbox row。
2. Outbox row 至少保存`message_id`、类型、版本、aggregate、payload、创建时间、发布状态、attempt 和下次可发布时间。
3. Publisher 使用短数据库 lease 并发领取，不在数据库事务内等待 NATS 网络 I/O。
4. 发布时设置`Nats-Msg-Id=message_id`；收到 JetStream PubAck 后条件标记已发布。
5. Publisher 在 PubAck 返回后、数据库标记前崩溃时允许重复发布。
6. 未发布 Outbox 由恢复 Scanner 继续发现；不能只依赖进程内通知。
7. Outbox 进入不可自动恢复状态时必须告警并保留诊断，不得静默删除。

Event、Webhook Delivery 和消息 Outbox 可以共享同一领域事务，但各自责任不同：Event 是不可变领域事实，Delivery 是外部 Webhook 投递事实，消息 Outbox 是内部传播进度。不得混用状态和重试计数。

## 消费幂等与 Ack

消费者处理顺序固定为：

1. 解析并严格校验 Envelope、类型与版本；
2. 开启 PostgreSQL 事务并尝试写入`consumer_name + message_id`唯一 Inbox 记录；
3. 已存在完成记录时直接 Ack，不重复领域效果；
4. 不存在时读取权威领域对象，执行条件状态迁移并写 Event/Outbox/Audit；
5. 同一事务标记 Inbox 完成；
6. 事务提交成功后 Ack 消息。

数据库事务失败、进程崩溃或 Ack 失败都允许 JetStream 重新投递。消费者不得使用仅存在进程内存或 Redis 的去重状态保证物理动作幂等。

对于不可安全重放的物理动作，消息重复只允许重新检查现有 Command/Attempt 状态，不得创建新的 Provider request key 或再次调用 Provider。是否允许产生新的 Attempt 由 Device Type profile 与 Provider 合同共同决定。

## 重试与死信

- 传输级临时错误由 JetStream 按 consumer 策略重新投递；
- 领域重试必须落入 PostgreSQL 的 CommandAttempt、WebhookDeliveryAttempt 等专属记录，不能只依赖 Broker delivery count；
- 非法 schema、未知版本、永久缺失关联对象或违反合同的消息进入受控 quarantine/dead-letter 流并告警；
- dead-letter 消息不得由运维直接改 payload 后重放。修复后通过受审计工具重新驱动原权威事实；
- Command、Callback 和 Webhook 使用独立 consumer、并发和积压告警，彼此不能共享一个无隔离的工作队列。

具体 Ack 等待、最大传输重投和退避值属于部署配置，但必须满足 [Reliability and Capacity](./operations/reliability-capacity.md) 的恢复门槛。配置变化不能改变领域层最大 Attempt、危险动作重放或 Webhook 有界重试合同。

## Messaging Unknown

| 未知内容 | 阻塞 | 不阻塞 | 关闭证据 |
| -------- | ---- | ------ | -------- |
| poison message 的 durable quarantine/dead-letter 落点、Ack/NAK 顺序、稳定 Subject/记录标识和受审计重驱入口；候选机制包括 JetStream MaxDeliver 后独立 DLQ、另发 DLQ 后 Ack，或 PostgreSQL quarantine 后 Ack | 非法 schema、未知版本、永久缺失关联对象及违约消息的无丢失恢复、无限重投防护和运维重驱验收 | 合法版本消息的 Outbox/Inbox、重复投递和消费者崩溃恢复主链；poison message 只允许保留并告警，不得改 payload 后直接重放 | 架构负责人批准的单一落点与状态机、Ack/NAK 崩溃点表、稳定标识/告警/审计字段以及注入 poison message 的恢复测试 |

## 版本兼容

- 新增可选 payload 字段且旧消费者可忽略时，可以保持 schema version；
- 删除字段、改变含义、改变类型或收紧旧消息有效范围时，必须提升 schema version 并使用新 Subject；
- 升级期间 Publisher 可以在明确时限内双发新旧版本，但每个消费者的领域效果必须共享同一幂等键；
- 旧版本停止发布前必须证明所有目标消费者已支持新版本，旧 Stream 中的可处理消息已清空或有显式迁移方案；
- 不允许消费者根据部署时间猜测消息版本。

## 可观测性

至少记录以下指标，并按 Subject 和 consumer 区分：

- publish success/failure/latency；
- Outbox oldest age、pending count 和 attempt；
- consumer pending、redelivery、Ack latency 和 processing failure；
- Inbox duplicate count；
- quarantine/dead-letter count；
- 从领域事务提交到消费者完成的端到端延迟。

日志和 Trace 必须携带`message_id`、`correlation_id`、`aggregate_type`、`aggregate_id`、Subject 和 consumer；禁止记录敏感 payload。
