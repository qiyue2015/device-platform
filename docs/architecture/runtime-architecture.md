---
title: 后端目标运行架构
updated: 2026-08-01
status: accepted-target
decision: ADR-0001
---

# 后端目标运行架构

本文将 [ADR-0001](./technology-stack-adr.md) 落为长期运行拓扑和组件责任。它从属于平台、目标与领域合同；发生冲突时不能用基础设施实现降低领域一致性、真实设备证据或安全门槛。

本文只描述后端重建的目标运行架构，不记录旧实现或迁移步骤。

## 运行单元

后端保持模块化单体代码库，允许按运行责任编译和部署以下进程：

| 运行单元         | 责任                                                                               |
| ---------------- | ---------------------------------------------------------------------------------- |
| API              | 管理/Open API、认证、校验、事务写入和查询，不同步等待异步 Provider 或 Webhook 完成 |
| Outbox Publisher | 从 PostgreSQL 领取未发布 Outbox，发布到 JetStream，并记录发布进度                  |
| Command Worker   | 消费 Command 唤醒消息，按领域 lease 和状态机调用 Provider adapter                  |
| Provider Message Worker | 处理已认证并持久化的 Provider RawMessage，去重并追加 Result/State/Event     |
| Omni TCP Listener       | 接受有界 TCP 连接和 frame，按显式 profile 解析、管理 session 并持久化允许的 RawMessage |
| Webhook Worker   | 消费 Delivery 唤醒消息，执行签名 HTTP 投递、重试和 dead 管理                       |
| Recovery Scanner | 扫描到期 lease、deadline、未发布 Outbox 和待投递记录，保证通知丢失后仍可恢复       |

这些进程共享领域模块和数据库合同，但分别配置并发、连接池、资源配额和扩容策略。Handler 不直接跨模块查询其他模块拥有的表；Provider SDK 类型不得进入领域层。

首次安装跨运行配置、migration、管理员、secret 与完成事实的崩溃恢复口径尚由 [Platform Target](../platform-target-contract.md#需要产品所有者裁决)阻塞。裁决前 Runtime 不得在 roll-forward、补偿或 fail-stop 之间选择实现默认值；裁决后必须在本文补充 setup coordinator/journal 的状态、责任、崩溃点和可观察恢复结果，再进入安装恢复验收。

## 目标拓扑

```text
Admin / Project Application / WWTIOT Callback / Omni TCP Device
                       |
                       v
                    Go API
                       |
          PostgreSQL transaction
        business fact + Event + Outbox
                       |
                       v
                Outbox Publisher
                       |
                       v
                NATS JetStream
           /-----------+------------\
          v            v             v
	   Command Worker Provider Message Worker Webhook Worker
          |            |             |
          v            v             v
   Provider Adapter PostgreSQL   External Webhook

Redis: online TTL, rebuildable state, cache, rate limit
OpenTelemetry: API -> Outbox -> NATS -> Worker -> Provider/Webhook
```

## 写入路径

### Command

1. API 校验 Project、Device、Capability 和幂等键。
2. 同一 PostgreSQL 事务写入 Command、Audit、领域 Event 和 Outbox。
3. API 返回已受理的持久 Command，不等待 Provider。
4. Outbox Publisher 发布 `command.dispatch.requested`。
5. Command Worker 消费消息后，仍以数据库状态、lease 和 profile preflight 为准；消息本身不授权设备动作。
6. Provider 调用结果按领域合同更新 Attempt、Command、Result、Event 和后续 Outbox。

### Provider 上行

可信 Result/State 路径只有在对应 Provider 合同要求的认证、防重放和身份关联 Unknown 关闭后才能启用。WWTIOT 在此前由 API 合同要求 HTTP 入口失败关闭；Omni 可以保存 Provider 合同允许的 `unverified` RawMessage/诊断，但不能进入可信结果处理。

1. WWTIOT HTTP 入口执行大小限制、来源策略、签名与防重放校验；Omni TCP Listener 执行有界 stream parsing、显式 profile、session 和 identity 候选校验。
2. 可信消息在同一 PostgreSQL 事务保存 RawMessage、Provider message dedupe key/RawMessage 唯一约束和处理 Outbox；这里不写 `ConsumerInbox` 完成记录，后者只由 Provider Message Worker 在消费事务中写入。
3. 持久化成功后才按对应协议返回成功或继续连接。`unverified` Omni frame 只写隔离诊断，不创建可信处理 Outbox。
4. Provider Message Worker 消费可信消息并追加规范化 Result/DeviceState/Event；重复消费不产生重复最终效果。

### Webhook

1. 领域事务已经创建 Event 和对应 Webhook Delivery snapshot。
2. Outbox 只发布 Delivery ID 和稳定关联信息，不重新生成 Event 或 Delivery。
3. Webhook Worker 从 PostgreSQL 领取 Delivery lease，读取持久 raw body 与 secret version，执行一次 Attempt。
4. 失败重试、dead 和受控重发继续由 PostgreSQL 记录权威历史。

## 读取与状态职责

- 后台和 Open API 读取 PostgreSQL 中的权威业务状态；
- Redis 中的在线 TTL 只表示可重建的近期观察，不能覆盖 Provider 合同和 Device 持久事实；`online_only` 门槛无法读取 TTL 或证据已过期时，判定为没有可信新鲜 `online` 证据并失败关闭，不能使用缓存旧值或持久旧状态降级放行；
- JetStream 消息不作为查询模型，管理后台不能直接从 Broker 推导 Command 状态；
- 高频连接观察只有在真实 Provider 合同确认后才能启用。重复观察可以在 Redis 合并；TTL 到期由谁触发 PostgreSQL `online -> unknown|offline`、何时产生 Event 及如何崩溃恢复，必须随 Provider 在线语义 Unknown 一并冻结，Runtime 不得只做读取时动态投影或自行增加 scanner；
- Omni 设备直连已是当前必须实现的 Provider；长期遥测保留仍不在当前产品合同。Runtime 必须隔离 TCP session/frame 流量，且不能把连接存在提升为可信在线或结果。

## 故障隔离

| 故障             | 必须成立的行为                                                                                                            |
| -------------- | ------------------------------------------------------------------------------------------------------------------ |
| NATS 不可用       | API 仍可在受控容量内提交权威事实与 Outbox；Publisher 积压并告警，恢复后续发；不得丢失已受理 Command                                                   |
| PostgreSQL 不可用 | 需要权威写入的 API 和 Worker 失败关闭；不得只写 NATS 后返回成功                                                                          |
| Redis 不可用      | 缓存可重建；恢复扫描、Result/Webhook/Audit 和非 `online_only` 主链继续；`online_only` 创建与派发在无可信新鲜 `online` 证据时按 profile 失败关闭，权威事实不丢失 |
| Worker 崩溃      | JetStream 重新投递，数据库 lease 到期后恢复；重复消费不重复最终效果                                                                         |
| Provider 超时    | 按 Attempt 证据与`indeterminate`合同处理，不因消息重投自动重放危险动作                                                                    |
| Omni TCP 重连/冲突 | 新旧 session 按稳定代际隔离；重复 IMEI、profile 不匹配、半帧或超长帧不跨 Device 关联，不产生可信 Result/State，也不自动重发 Command       |
| Webhook 端点故障   | 只积压对应 Delivery，不阻塞 Command 和其他 Project；有界重试后进入 dead                                                                |

## 部署边界

- 本地开发允许单节点 NATS 和单进程组合运行；
- 生产 NATS JetStream 默认采用至少 3 节点、Stream 副本数 3，除非已接受的部署 ADR 基于明确可用性目标另有决定；
- API、Publisher、各类 Worker 与 Omni TCP Listener/Session 必须有独立并发、连接池和内存预算；高频状态流量不能耗尽 Command 资源；
- 恢复 Scanner 是消息通知之外的安全网，不能因接入 NATS 而删除；
- 所有运行单元使用统一`correlation_id`、`message_id`和领域资源 ID 贯穿日志、Trace 与指标。

## 模块边界

目标模块保持：`identity-access`、`projects`、`devices`、`providers`、`commands`、`provider-inbox`、`webhook-outbox`、`audit`和`platform`。`providers` 内部分离 WWTIOT cloud API 与 Omni direct TCP adapter/profile；Platform Core 不依赖厂商 package。`platform`提供 PostgreSQL、NATS、Redis 和可观测性适配器，不拥有 Command 状态机或 Provider 业务语义。

未来若拆分独立服务，先保持消息合同、数据库事实所有权和 API 语义不变，再迁移部署边界；不能以“微服务化”为由复制状态源。
