---
title: 可靠性与容量设计规范
updated: 2026-08-01
status: accepted-baseline
decision: ADR-0001
---

# 可靠性与容量设计规范

本文定义 Device Platform 基础设施在规模增长和局部故障下必须保持的设计门槛。它不把讨论示例当作产品规模承诺，也不替代目标部署的容量数据、SLO、RPO 与 RTO 确认。

## 设计原则

- 不以“出现瓶颈后再更换基础设施”为策略；第一版就按 [ADR-0001](../architecture/technology-stack-adr.md) 建立 PostgreSQL、NATS JetStream 和 Redis 的稳定职责。
- Omni 设备直连已进入当前产品合同，但其目标连接规模仍为 Unknown；不得虚构规模，必须保证 Command、Provider Message、TCP Session、Webhook 和可启用的连接观察彼此隔离并可水平扩展。
- 所有容量结论必须来自目标负载模型、压测和故障演练，不能把`1000台设备`或某次单机测试当作长期上限。
- 正确性优先于吞吐：Command 不能丢失，重复消息不能产生重复物理动作，状态不能因降级被伪造。

## 负载模型

容量评审至少明确以下变量：

| 变量 | 含义                                                    |
| ---- | ------------------------------------------------------- |
| `D`  | 已注册设备数                                            |
| `A`  | 同时活跃设备数                                          |
| `H`  | 每台活跃设备每秒连接观察/心跳数，仅在真实合同启用后存在 |
| `C`  | 每秒新建 Command 数                                     |
| `R`  | 每个 Command 平均 Provider 结果或上行消息数             |
| `W`  | 每个 Event 平均 Webhook 目标数，当前每 Project 最多一个 |
| `B`  | 峰值或重连风暴相对正常负载的放大系数                    |

基础入口负载至少按以下关系估算：

```text
connection_observations_per_second = A * H
command_messages_per_second         = C * command_message_factor
provider_messages_per_second        = C * R
webhook_attempts_per_second          = event_rate * W * retry_amplification
peak_rate                            = normal_rate * B
```

未确认变量不得由开发人员填入乐观默认值；其影响和关闭条件见文末 Unknown 表。

## 相对容量门槛

在绝对业务规模冻结前，每个候选生产配置至少验证：

1. 持续正常目标负载下无无界积压；
2. 持续 3 倍预测峰值时，API、Publisher 和 Worker 不发生数据丢失或错误状态迁移；
3. 模拟全部活跃设备在受控窗口内重新连接时，高频流量不能耗尽 Command 路径资源；
4. NATS、Redis、单个 Worker 和单个 Provider 短暂故障恢复后，积压能在已声明恢复窗口内清空；
5. 重复投递、乱序到达和消费者崩溃不会产生重复最终效果；
6. 压测结束后 PostgreSQL 连接、锁等待、WAL、表膨胀、JetStream 存储和 Redis 内存仍在受控预算内。

“3 倍”是最低工程余量，不是产品容量上限。实际业务存在更大活动峰值或统一上线冲击时，按更高的已知`B`验收。

## 流量分类与降级

| 流量                   | 丢失策略                     | 降级方式                                                        | 权威事实                             |
| ---------------------- | ---------------------------- | --------------------------------------------------------------- | ------------------------------------ |
| Command 创建与生命周期 | 不允许丢失                   | 达到安全容量时限流或拒绝新请求，不能返回假成功                  | PostgreSQL                           |
| Provider 上行/结果     | 认证通过后不允许静默丢失     | 持久 Inbox 后异步处理；unverified 消息按 Provider 合同隔离诊断 | PostgreSQL                           |
| Omni TCP session/frame | 不允许挤占 Command 资源      | 有界连接、frame、read deadline 和独立并发；超限关闭对应连接     | 进程 session + PostgreSQL 诊断事实  |
| Webhook Delivery       | 不允许丢失持久 Delivery      | 延迟投递、有界重试、dead 和受控重发                             | PostgreSQL                           |
| Audit                  | 不允许因异步故障丢失关键审计 | 与关键领域事务同步持久化，后续传播可延迟                        | PostgreSQL                           |
| 重复连接观察/心跳      | 可按合同合并，不得伪造在线   | Redis TTL、合并相同观察、批量快照；只有状态变化立即持久化       | Redis 近期状态 + PostgreSQL 状态变化 |
| 长期遥测               | 当前不在产品范围             | 未冻结采集和保留合同前不启用                                    | Unknown                              |

高频连接观察不得与 Command 共享同一个 consumer 并发池、数据库连接池预算或无配额 Subject。Redis 故障不得阻塞已持久 Command 的恢复扫描、Result 处理、Webhook/Audit 或不依赖在线门槛的主链；恢复已持久 Command 不等于允许再次发送物理动作。对 `online_only` action，Redis 不可用、TTL 不可读或在线证据已过期都不能降级放行：创建时按 API 合同返回 `409 device_not_online` 且不创建 Command；已入队但尚未派发时按 Device Type profile 和 Domain Model preflight 完成 `not_dispatched/device_not_online`，Command 进入 `failed`，不得调用 Provider。Redis 故障也不得把设备伪造为 `online` 或把命令错误确认为成功。

## 故障验收矩阵

| 故障注入                            | 必须验证                                                                                                                                                                          |
| ----------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| NATS 停止并恢复                     | PostgreSQL 事实和 Outbox 不丢；API 按容量策略响应；恢复后无重复物理动作且积压可清空                                                                                               |
| Outbox Publisher 在 PubAck 后崩溃   | 允许重复发布；消费者 Inbox 去重；最终效果一次                                                                                                                                     |
| Consumer 在数据库提交后、Ack 前崩溃 | 消息重新投递；Inbox 识别已完成并 Ack                                                                                                                                              |
| Redis 停止并恢复                    | 缓存可重建；恢复扫描、Result/Webhook/Audit 和非 `online_only` 主链继续；`online_only` 创建与派发在无可信新鲜 `online` 证据时按 profile 失败关闭；不丢失权威事实、不伪造在线或成功 |
| PostgreSQL 主连接中断               | 所有权威写入失败关闭；不得退化为只写 NATS 或 Redis 后返回成功                                                                                                                     |
| Provider 超时或连接中断             | WWTIOT HTTP 与 Omni TCP Attempt 都按实际发送证据分类；危险动作不因 Broker 重投、TCP 重连或 session 替换被自动重发                                                                  |
| Omni 重复 IMEI/重连/半帧/超长帧     | 不跨 profile/Device 关联；旧 session 不能继续写；半帧有界缓存，超长或非法 frame 隔离并关闭连接；不产生可信 Result/State                                                           |
| Webhook 持续 5xx/超时               | 只积压对应 Delivery；其他 Project 和 Command 路径保持可用；耗尽后进入 dead                                                                                                        |
| 消息重复、乱序和未知版本            | 不违反条件状态迁移；未知版本隔离并告警，不猜测解析                                                                                                                                |

## 生产拓扑基线

- NATS JetStream 默认至少 3 节点，关键 Stream 副本数 3；本地开发可以单节点；
- PostgreSQL 必须有备份、恢复演练和与业务 RPO/RTO 一致的高可用策略；具体方案由部署 ADR 确定；
- Redis 数据按可重建原则设计；是否以高可用拓扑降低 `online_only` 因在线证据不可用而失败关闭的比例，由业务 SLO 决定，不能改变失败关闭语义；
- API、Outbox Publisher、Command Worker、Provider Message Worker、Omni TCP Listener/Session 和 Webhook Worker 必须可独立扩容、限流和暂停；
- Broker、数据库和 Redis 存储水位都必须有预测性告警，不能只在写满后报警。

## 必需指标与告警

最低指标集合：

- API 请求率、错误率、p50/p95/p99 延迟和限流数；
- PostgreSQL 连接池等待、事务延迟、锁等待、deadlock、WAL 和存储水位；
- Outbox pending count、oldest age、publish failure 和重试次数；
- 每个 JetStream consumer 的 pending、Ack latency、redelivery 和存储水位；
- Command 从创建到首次派发、Provider acceptance 和终态的延迟；
- Provider Message 从接收到处理完成的延迟、重复率、拒绝率和不可关联率；
- Omni 当前连接数、重复 identity、重连率、frame 大小/速率、parse failure、session 写入与断线结果；
- Webhook pending、retry、dead 和 oldest age；
- Redis 延迟、内存、水位、淘汰和在线 TTL 更新失败；
- 各类 quarantine/dead-letter 数量。

告警阈值必须由已通过的容量测试和业务恢复目标确定。只有“CPU 高”或“数据库慢”的通用告警不足以证明设备链路可运行。

## 上线与扩容门槛

每个目标部署在上线前必须形成并保存：

- 已确认的设备数、活跃率、命令率、Provider 上行率、Omni 连接/重连率、Webhook 率和峰值系数；
- 数据保留、RPO、RTO 和可接受降级窗口；
- 与目标配置一致的容量测试报告；
- 故障矩阵执行证据；
- 监控面板、告警接收人与处置手册；
- NATS、PostgreSQL 和 Redis 的备份/恢复或可重建说明；
- 达到哪些指标时扩容 Publisher、Worker、NATS 存储、PostgreSQL 或 Redis 的明确阈值。

没有上述证据时，只能声明功能实现，不能声明中长期容量与可靠性验收通过。

## Reliability Unknown

| 未知内容                                                       | 阻塞                                                                  | 不阻塞                                                             | 关闭证据                                                           |
| -------------------------------------------------------------- | --------------------------------------------------------------------- | ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| `D/A/H/C/R/W/B` 的目标值和业务峰值窗口                         | 生产容量验收、连接池/并发/存储最终配置                                | 参数化实现、基准测试和压测工具建设                                 | 产品负载预测、真实流量样本和批准的峰值模型                         |
| 业务 SLO 与可接受降级窗口                                      | 告警阈值、扩容阈值、上线承诺                                          | 指标埋点、故障隔离和无数据丢失设计                                 | 产品/运维共同批准的可用性与延迟目标                                |
| PostgreSQL/NATS 的 RPO、RTO 和备份拓扑                         | 灾难恢复验收、生产拓扑定版                                            | 本地/测试拓扑和可恢复数据模型实现                                  | 部署 ADR、备份恢复演练及实测 RPO/RTO                               |
| Redis 在线判断的可用性 SLO 与高可用拓扑                        | Redis 高可用拓扑、可用性和降级窗口承诺                                | `online_only` 失败关闭语义、Redis 可重建和 PostgreSQL 权威事实原则 | 业务对在线判断可用性的要求、目标拓扑和故障演练                     |
| WWTIOT 的配额、限流和网络边界                                  | WWTIOT 容量配置和降级阈值                                             | 单次发送、隔离 worker、可观测性                                    | 厂商限制、目标网络测试和真实联调数据                               |
| Omni 的连接数、frame/重连率、idle 窗口和网络边界               | Omni listener/session 容量、限流、超时、水平扩展与生产安全验收        | 有界 parser、单次发送、隔离资源和本地故障注入                      | 目标设备规模、真实流量、网络拓扑、容量测试和重连风暴演练           |
| NATS/Outbox 达到目标部署安全容量时的 API admission 阈值与 wire | 过载时是否创建新 Command、`429`/`503`、`Retry-After` 和调用方退避验收 | PostgreSQL 权威事实、阈值以下受控接收、指标和限流能力建设          | 获批容量模型与阈值、API error/status 决策、NATS 长时中断和恢复压测 |
