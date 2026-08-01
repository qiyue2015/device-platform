---
title: Omni Provider 合同
updated: 2026-08-01
status: frozen-for-implementation
contract_revision: 2026-08-01
---

# Omni Provider 合同

本文只定义 Omni 设备直连协议事实、显式协议 profile、`smart-lock` action 映射、adapter 证据上限和 Omni Unknown。它从属于 [Platform Target](../platform-target-contract.md)、[Domain Model](../domain-model-contract.md)、[API](../api-contract.md)与 [smart-lock Device Type](../device-types/smart-lock.md)。Omni 是当前必须接入的 Provider，不是未来候选；但当前受控设备究竟匹配哪个协议 profile 仍为阻塞真实派发与实机验收的 Unknown。

## 证据边界

| 证据层级         | 能证明                                                 | 不能证明                                         |
| ---------------- | ------------------------------------------------------ | ------------------------------------------------ |
| 厂商资料         | 指定版本中的 TCP/BLE frame、字段、指令和示例           | 当前设备型号/固件匹配、消息真实性或真实设备行为 |
| 当前代码         | parser、encoder、session 和持久 Command 主链的实际行为 | Omni 设备收到、执行或返回可信结果                |
| adapter 合同测试 | 平台对受控 TCP peer 的构造、解析、关联和错误分类       | 真实 Omni 设备、目标网络或长期稳定性             |
| 受控真实设备验收 | 已证明 profile 的指定设备在受控窗口内的可观察行为      | 未覆盖设备、固件、网络或部署                     |

本合同依据两份只读外部归档资料：《欧米智能马蹄锁 TCP+BLE 接口协议 V2.0.7》（SHA-256 `36e835214954d9c45d0c35a3b3aed588d47038dfacc37c9e090301d0b5f7aec3`）和《OMNI 物联网设备 TCP 接口协议 V1.3.5》（SHA-256 `2865a3c93b8c9c2c7185c0549c2955aa525cab8482713ece61b57a6cddc742f6`）。两份资料均已逐页读取和渲染核验；它们定义不同 TCP frame 和 action 流程，不能按品牌名合并。

## 厂商资料事实

当前 adapter 只采用两份资料的 TCP 部分。资料中的 BLE、OTA、定位、围栏、告警、RFID、Beacon、固件和其他指令不进入当前目标。

| provider profile           | TCP 下行/上行 header | identity                   | `unlock`                                      | `lock`                                      | `query_status` |
| -------------------------- | -------------------- | -------------------------- | --------------------------------------------- | ------------------------------------------- | -------------- |
| `omni-bike-tcp-v2.0.7`    | `*CMDS` / `*CMDR`    | 通信模组 15 位 IMEI        | `L0`，含重置标志、用户 ID、秒级时间戳         | 无主动下行；`L1` 是设备主动关锁上报         | `S5`           |
| `omni-iot-tcp-v1.3.5`     | `*SCOS` / `*SCOR`    | 通信模组 15 位 IMEI        | 先 `R0(operation=0)` 取得一次性 KEY，再发 `L0` | 先 `R0(operation=1)` 取得一次性 KEY，再发 `L1` | `S6`           |

服务器下行在 ASCII frame 前加两个二进制字节 `0xFF 0xFF`，字段以逗号分隔，以 `#\n` 结束。`omni-bike-tcp-v2.0.7` frame 另含 `yyMMddHHmmss` 设备时间字段；`omni-iot-tcp-v1.3.5` 不含该字段。TCP stream parser 必须处理分包、粘包、空 frame、超长 frame、错误 header、错误厂商 code、非法 IMEI、未知指令与尾随数据，不能把一次 socket read 等同于一个 frame。

两份资料只以连接内自报 IMEI 标识设备，未给出足以冻结 TLS client identity、消息签名、challenge-response、防重放、连接代际或连接劫持防护的规则。它们描述的服务器响应 `Re` 只确认服务器收到设备上报，不能作为下行 Command 的 Device final evidence。

## Provider 注册与配置

| 字段                 | 合同值                                                         |
| -------------------- | -------------------------------------------------------------- |
| `code`               | `omni`                                                         |
| `name`               | `Omni`                                                         |
| `access_type`        | `direct_device`                                                |
| `transport_protocol` | `tcp`                                                          |
| `adapter`            | `omni_direct_tcp`                                              |
| 调用方式             | 持久 Command Worker 向已建立且唯一绑定的 TCP session 单次写入 |

Device 必须保存显式 `provider_profile`，且只能取本合同列出的两个值。不得根据 IMEI、frame 外观、首次指令或品牌名自动猜测 profile；缺失、未知或与连接 parser 不一致时失败关闭，不创建第二套命令系统。`(provider_code, provider_device_id)` 在全部 lifecycle 中保持部署内全局唯一，profile 不参与放宽该唯一性；`deleted` Device 永久保留 identity tombstone，当前不得复用。

Omni `provider_device_id` 必须是 15 位十进制 IMEI。两份资料把物理动作字段分别描述为用户 ID、当前秒级时间戳/操作序列号和一次性 KEY，但没有给出足以唯一冻结共享单车用户映射、技术用户占位值、取值范围、并发、幂等、防重放和恢复语义的规则。现有 WWTIOT Attempt request key 也不能被直接复用为这些字段：其平台约束最多为 9 位，而现代 Unix 秒为 10 位。实现不得自行选择 `user_id=0`、截断时间、进程当前时间或 request-key 变换；在正式规则关闭前，受影响的物理动作必须在任何 socket 写入前以 `provider_mapping_unknown` 失败关闭。`query_status` 不依赖这些字段，不受该 Unknown 阻塞。

监听地址、最大连接数、idle timeout、frame 上限和读取 deadline 是部署配置。没有明确监听配置时 Provider 为 `unconfigured`；配置完整但设备认证和 profile 匹配证据未通过时最多为 `configured_unverified`。配置、IMEI、原始 frame 和 socket error 必须按 allowlist 脱敏，不得暴露凭据或未脱敏厂商数据。

## 下行映射与证据上限

| profile / action                         | adapter 行为                                                                 | 当前最高下行证据                                  |
| ---------------------------------------- | ---------------------------------------------------------------------------- | ------------------------------------------------- |
| bike / `unlock`                          | 用户 ID 与 operation value 映射未唯一冻结；任何 socket 写入前拒绝            | `none / none`，`provider_mapping_unknown`          |
| bike / `lock`                            | 资料没有主动下行；在任何 socket 写入前拒绝                                   | `none / none`，`provider_action_unsupported`      |
| bike / `query_status`                    | 生成一次 `S5` 并完整写入当前唯一 session                                     | `transport_sent / verified`                       |
| iot / `unlock`                           | R0 用户 ID、operation value 与 KEY 生命周期未唯一冻结；任何 socket 写入前拒绝 | `none / none`，`provider_mapping_unknown`          |
| iot / `lock`                             | R0 用户 ID、operation value 与 KEY 生命周期未唯一冻结；任何 socket 写入前拒绝 | `none / none`，`provider_mapping_unknown`          |
| iot / `query_status`                     | 生成一次 `S6` 并完整写入当前唯一 session                                     | `transport_sent / verified`                       |

`transport_sent / verified` 只证明本进程把完整 frame 写入某个当前 session 的 socket；它不证明该 peer 是真实设备、设备已读取 frame、Provider acceptance、Device ACK 或物理执行。Command 保持 `sent`，到观察期限进入 `timeout`。短写必须继续写到 frame 完整或发生错误；零字节写、deadline、断线或写入结果不明统一按是否能够证明“零字节已写”保守分类。

| 观测                                                     | Attempt outcome                               | confirmation / evidence     | Command 影响                                      |
| -------------------------------------------------------- | --------------------------------------------- | --------------------------- | ------------------------------------------------- |
| profile 缺失/非法、action 不支持、映射 Unknown 或没有唯一 session | `invalid_request`                       | `none / none`               | `failed`，使用稳定 pre-send reason                |
| 可以证明 frame 的任何字节均未写入                        | `transport_error_before_send`                 | `none / none`               | `failed/provider_transport_error`                 |
| frame 完整写入                                           | `indeterminate` + `provider_delivery_unknown` | `transport_sent / verified` | 保持 `sent`，到观察期限 `timeout`                 |
| 部分写入、写入后断线或无法证明未写入                     | `indeterminate` + `provider_delivery_unknown` | `transport_sent / verified` | 保持 `sent`，到观察期限 `timeout`；禁止自动重发   |

一个 Command 只使用现有持久 Attempt，Omni adapter 不得创建旁路 command、内存 command ID 或第二个 Result 状态机。进程恢复时，已进入 `dispatching` 但未完成的 Attempt 按通用合同记为 delivery unknown，不得自动再次写入。未来若 R0 映射 Unknown 由新证据关闭，`R0 -> L0/L1` 仍必须是同一 Attempt 内的 profile 协议步骤；任一步发生不确定发送后都不得自动重启整个物理动作。

## 上行、关联与状态

parser 可以将 frame 保存为脱敏、不可变、`evidence_status=unverified` 的 RawMessage，并记录 provider/profile、连接 ID、IMEI、指令、接收时间、解析结果和稳定错误码。无法唯一映射 Device 的消息保持 Project/Device 为空并进入受控诊断，不得按其他 Project、其他 Provider 或相似 IMEI 猜测归属。

在设备认证、防重放和连接代际 Unknown 关闭前，任何真实 Omni TCP 上行都不得：

- 创建 `device_acked`、`device_succeeded` 或 `device_failed` CommandResult；
- 将 Command 推进为 `acked`、`success` 或 `failed/device_reported_failure`；
- 创建可信 DeviceState、改变 `connection_status` 或更新可信 `last_seen_at`；
- 把 `R0`、`L0`、`L1`、`S5`、`S6` response 或 server socket 接收事实提升为 `verified` Device evidence。

adapter 可以输出只读 correlation candidate 供诊断；candidate 必须同时匹配 provider、显式 profile、全局唯一 Device identity、当前连接代际、指令和 profile 定义的关联字段。同一连接代际内的重复 frame 以稳定 message fingerprint 去重；跨连接代际的相同字节是独立观察并创建独立 RawMessage。不可关联、歧义、重复和迟到都保留原始技术事实，不改写历史。

## 当前代码事实

截至 2026-08-02 的工作树审计，当前后端已完成以下本地实现；这些事实来自代码和自动化测试，不是厂商或真实设备事实：

- Provider registry 已注册 `omni`、两个显式 profile 与 `omni_direct_tcp` adapter；Device 和 Command 持久保存 `provider_profile`。Attempt 通过 `command_id`、Provider 和 adapter 绑定到冻结 Command，不另存 profile；Core 只传递该 opaque binding。
- 两个 profile 使用独立 listener。下行 encoder 保留二进制 `0xFF 0xFF`，上行 decoder 从 `*CMDR`/`*SCOR` 开始，按 profile 精确校验 Q0、H0、S5、S6 的字段数与基本类型，并覆盖分包、粘包、超长和非法前缀。listener 只允许合法 Q0 首报建立 session，H0 或普通 response 不能替代握手，也不从 frame 猜测 profile。
- session registry 按 profile + IMEI 隔离身份并记录连接代际；派发还要求 session 的 Project/Device ownership 与冻结 Command 一致。同一身份存在多个 session 时拒绝派发，旧代际或其他 Device 的 session 不能写入。socket 写入按身份串行、完整处理短写，并从 Command worker context 设置写 deadline；一个身份的阻塞写不会持有全局 registry map 锁或阻塞其他身份注册/注销。
- listener 跟踪已接受连接；取消、主动关闭或 Accept 异常时先关闭本 listener 已绑定和未完成握手的连接并注销 session，再等待连接 goroutine 退出。两个 profile 作为一个运行单元：任一 listener 非预期退出会关闭 sibling、清理 session、使 runtime/adapter `Configured()` 变为 false，并向应用传播 fatal 状态；应用随后使 `/readyz` 与业务 API 失败关闭。
- inbound recorder 将允许保留的上行作为 `unverified` RawMessage，并在同一 PostgreSQL 事务写 `provider.message_received` 或 `provider.message_rejected` Audit。同一连接代际的重复 frame 复用 RawMessage但保留新的 duplicate Audit；跨连接代际的观察独立保存。无法关联或 profile/Project/Provider 不匹配时不绑定 Device，不创建 Result/DeviceState/Event。RawMessage 的 `provider_device_id` 保存规范化 IMEI；其诊断 body 与 Audit metadata 只保存 allowlist 摘要，不保存 IMEI 或 raw frame。
- `query_status` 已通过统一持久 Command worker 派发：bike 生成一次 S5，IoT 生成一次 S6；完整或部分写入只形成 `transport_sent`，Command 保持 `sent` 并在观察期限进入 `timeout`，不创建 Device ACK/final Result。bike `unlock`、bike `lock` 与 IoT `unlock`/`lock` 均在任何 socket 写入前按本合同失败关闭。
- 当前异步调度由 PostgreSQL polling/lease worker 驱动；目标架构中的 NATS JetStream Outbox/Inbox 传播尚未实现，不能把数据库轮询测试记为 NATS 验收。

逐项实现和验证证据以 [Current State](../current-state.md) 为准。本节不会把本地 TCP peer、parser 或持久化测试提升为真实 Omni 设备验收。

## 真实设备验收事实

当前仓库没有可复核的 Omni 真实设备、固件/profile 对照、受控网络身份方案或端到端验收记录。没有执行真实 `query_status`、`unlock` 或 `lock`。因此 profile 匹配、连接真实性、收到、拒绝、执行、迟到、断线恢复和最高 confirmation level 全部为 **Unknown**；parser/encoder 或本地 TCP peer 测试不能替代。

## Provider Unknown

| 未知内容                                                   | 阻塞                                                                 | 不阻塞                                                                    | 关闭证据                                                        |
| ---------------------------------------------------------- | -------------------------------------------------------------------- | ------------------------------------------------------------------------- | --------------------------------------------------------------- |
| 当前受控设备对应哪个 profile，或是否都不匹配               | 该 Device 的真实派发、action 可用性与实机验收                         | 两 profile codec/session、显式绑定、缺失/不匹配失败关闭和本地合同测试      | 型号/固件与协议版本的厂商确认、只读握手报文和受控设备记录        |
| TCP peer、IMEI、frame 的真实性及防重放/连接劫持机制        | 可信 Result、DeviceState、online/last_seen、真实成功/失败与生产安全验收 | unverified RawMessage、解析/关联候选、单次下行 transport 事实              | 正式设备认证合同或受控网络身份方案，加冒充、重放、断线恢复测试   |
| operation value、用户 ID、R0 KEY 的映射、唯一性、有效期和重放语义 | bike `unlock`、IoT `unlock/lock` 的任何真实或伪 peer 派发，以及自动重试、并发物理命令和长期兼容保证 | 两 profile `query_status`、codec/parser/session、同一主链与 pre-send 失败关闭 | 厂商书面字段映射与范围、批准的业务身份规则，以及重复、并发、KEY 过期和异常恢复实机矩阵 |
| bike profile 没有主动 `lock` 下行                          | 该 profile 的规范 `lock` 能力与三 action 实机矩阵                    | 明确返回 unsupported；其他已定义 action                                  | 新正式协议、目标设备另一个已证明 profile，或产品接受能力缺口     |
| 真实设备收到、拒绝、执行、迟到、重复和断线后的行为         | Device ACK/final、真实业务验收和观察期限调优                         | `transport_sent -> timeout` 主链与模拟恢复测试                             | 受控设备、隔离现场、逐次写授权、设备侧观察和完整故障矩阵         |
| 目标连接数、重连风暴、frame 速率和网络边界                 | 生产容量、限流、连接治理与告警阈值                                   | 有界 parser、隔离 worker/session、指标接口和本地故障注入                   | 目标负载模型、受控压测、网络拓扑和恢复演练                       |

Omni 的最高可信 confirmation level 是否满足共享单车业务不由本合同决定；该产品裁决归 [Platform Target](../platform-target-contract.md)。
