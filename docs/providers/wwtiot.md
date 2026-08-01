---
title: WWTIOT Provider 合同
updated: 2026-08-01
status: frozen-for-implementation
contract_revision: 2026-08-01
---

# WWTIOT Provider 合同

本文只定义 WWTIOT 厂商协议事实、`smart-lock` action 映射、adapter 必须遵守的证据上限和厂商 Unknown。它从属于 [Platform Target](../platform-target-contract.md)、[Domain Model](../domain-model-contract.md)、[API](../api-contract.md)与 [smart-lock Device Type](../device-types/smart-lock.md)，不得重定义 Core 状态机、设备能力或业务验收门槛。

## 证据边界

| 证据层级         | 能证明                                 | 不能证明                               |
| ---------------- | -------------------------------------- | -------------------------------------- |
| 厂商资料         | 文档中写明的字段、签名顺序和示例       | 当前账号配置、线上可用性或真实设备行为 |
| adapter 合同测试 | 平台按本合同构造、校验和分类消息       | WWTIOT 服务接受、设备收到或执行        |
| 受控真实设备验收 | 指定凭据、设备和时间窗口内观测到的行为 | 未覆盖设备、环境或长期稳定性           |

## 厂商资料事实

本合同依据两份外部归档资料：《平台转发协议 V1.1》（2020，SHA-256 `5937e0b4d68961bd07346139381c237050286e5cc8054815c276be8a5a5edcfa`）和《物网通平台转发协议 V2》（2021，SHA-256 `bb88f399c6010be5f1ab9eaa17eb36b7b680e5ef0787755f5e64b58bd689f718`）。V2 页面将相关接口标为“开发中”，因此资料版本和生产可用性仍是 Unknown。

## Provider 注册与配置

| 字段                 | 合同值                                 |
| -------------------- | -------------------------------------- |
| `code`               | `wwtiot`                               |
| `name`               | `WWTIOT`                               |
| `access_type`        | `cloud_api`                            |
| `transport_protocol` | `http`                                 |
| `adapter`            | `wwtiot_cloud_api`                     |
| 调用方式             | 持久 Command Worker 发起单次 HTTP 下行 |

部署提供 `WWTIOT_API_URL`、`WWTIOT_USER_ID`、`WWTIOT_USER_KEY`。URL 必须是无 userinfo、query、fragment 的 absolute HTTP/HTTPS URL；UserID trim 后为 `1..128` UTF-8 byte，UserKey 为 `1..512` byte 且不 trim。任一配置缺失或非法时 Provider 为 `unconfigured`，不得发请求。

资料给出的默认 URL 是 `http://gps.wwtiot.com/api/`。它是厂商资料事实，不是生产安全批准；未确认 HTTPS 或等价响应真实性机制前，响应正文的 evidence status 最高为 `unverified`。

## V2 下行映射

V2 是唯一采用的 adapter 版本；V1.1 仅用于识别历史差异，不能作为 fallback。

| `smart-lock` action | V2 请求                       | 签名字段顺序                                                   |
| ------------------- | ----------------------------- | -------------------------------------------------------------- |
| `unlock`            | `cmd=open`                    | `userid + cmd + deviceid + serialnum + UserKey`                |
| `lock`              | `cmd=close`                   | `userid + cmd + deviceid + serialnum + UserKey`                |
| `query_status`      | `cmd=control&type=23&value=4` | `userid + cmd + type + value + deviceid + serialnum + UserKey` |

签名是上述 UTF-8 字节无分隔拼接后的 MD5。请求还携带 `userid`、`deviceid`、`serialnum` 和 `sign`；API payload 不得覆盖任何 Provider 字段。

`serialnum` 是 Attempt 的持久 Provider request key。同一 Attempt 恢复时必须复用，不得生成第二个外部效果；平台生成正十进制且不超过 9 位，并在 Provider 范围内强制唯一。该范围是平台保守约束，不声称是厂商限制。

UserID、UserKey、完整 sign、Provider endpoint 和原始 transport error 不得进入普通日志、API、Event 或 Audit。请求/响应诊断只保存 adapter allowlist 后的脱敏摘要。

## 下行传输约束

- Provider request timeout 来自 `smart-lock` profile，当前为 10 秒；禁止自动 redirect 和自动重试。
- 响应最多 64 KiB，必须是单个无重复 key、无尾随值的 JSON object；Attempt 摘要最多 4 KiB。
- transport 是否已经写出必须由 `WroteRequest` 或等价可验证信号确定。无法证明未写出时一律按“可能已发送”处理。
- `unlock`/`lock` 的安全策略、在线门槛和观察期限只由 Device Type 合同定义；本合同不能以厂商偏好覆盖。

## 同步响应映射

V2 的 `result=ok` / `info="cmd send ok"` 最多证明 Provider 接受或声称已发送命令，不证明 Device ACK 或 final result。

| 观测                                | Attempt outcome                               | confirmation / evidence          | Command 影响                               |
| ----------------------------------- | --------------------------------------------- | -------------------------------- | ------------------------------------------ |
| 构造请求前发现配置不可用            | `invalid_request`                             | `none / none`                    | `failed/provider_not_configured`           |
| 可以证明请求未写出                  | `transport_error_before_send`                 | `none / none`                    | `failed/provider_transport_error`          |
| 请求可能写出但没有完整响应          | `indeterminate` + `provider_delivery_unknown` | `transport_sent / verified`      | 保持 `sent`，到观察期限 `timeout`          |
| 非 2xx、非法 JSON、字段/echo 不合法 | `indeterminate` + `provider_response_invalid` | `transport_sent / verified`      | 保持 `sent`，到观察期限 `timeout`          |
| 2xx 且结构有效，`result != ok`      | `provider_rejected`                           | `transport_sent / unverified`    | `failed/provider_rejected`                 |
| 2xx 且结构/echo 有效，`result=ok`   | `provider_accepted`                           | `provider_accepted / unverified` | 保持 `sent`，不能进入 `acked` 或 `success` |

有效响应必须包含 string `result`、`userid`、`deviceid`、`cmd` 和非空 `sign`，并包含可规范化为十进制整数的 `serialnum`；echo 必须与请求一致。`query_status` 还必须匹配 `type=23`、`value=4`。`info` 不参与成功判定。

V2 响应包含 `sign`，但已知资料没有无歧义给出响应验签顺序。平台不得猜测；厂商确认并经受控验证前，依赖响应正文的 acceptance/rejection 证据保持 `unverified`。

## 设备信息 callback

V2 描述了包含 `cmd`、`deviceid`、`battery`、`lockstatus`、`time`、`serialnum` 和 `sign` 的设备信息 callback，但示例没有定义与下行 `serialnum` 的关联，也没有将其定义为某条 Command 的终局结果。

在签名顺序、防重放和身份关联 Unknown 关闭前，公开 `/v1/provider-callbacks/wwtiot` 必须按 [API 合同](../api-contract.md)失败关闭：不读取或保存 body，不更新 DeviceState/Command，不产生 Event。

Unknown 关闭后，入口仍必须满足：

- 最大 64 KiB，单个无重复 key、无尾随值的 JSON object；
- 必需字段为 `userid`、`cmd`、`deviceid`、`battery`、`lockstatus`、`time`、`serialnum`、`sign`；
- `userid` 与部署配置一致，Provider identity 全局唯一映射 Device；
- 验签、防重放和 deduplication 在返回成功前持久化；
- 只有签名可信的 callback 可产生 RawMessage 和 [smart-lock DeviceState](../device-types/smart-lock.md#devicestate-规范化)；
- 只有另有可信且无歧义的 Command 关联证据时才可创建 CommandResult；迟到结果服从 Domain Model 的终态不改写规则。

decoder/validator 稳定错误码为 `callback_payload_too_large`、`callback_invalid_json`、`callback_missing_field`、`callback_invalid_field`、`callback_user_mismatch`、`callback_device_not_found`、`callback_device_ambiguous`。这些机器码用于受控诊断，不得回显 body 或 sign。

## 当前代码事实

截至 2026-08-01 的冻结前审计，后端已注册 `wwtiot` cloud adapter，三项 action 通过持久 Command Worker 使用同一 Device、Command、Attempt、Event、Webhook 与 Audit 主链；同步响应分类、发送前/发送后 transport 错误、响应结构/echo 校验、脱敏摘要和 callback 无副作用 decoder/validator 均有本地测试。当前运行时以 PostgreSQL polling/lease 驱动 Command 与 Webhook，尚未实现目标合同中的 NATS JetStream Outbox/Inbox 传播。公开 WWTIOT callback 仍固定失败关闭，因此没有可信 DeviceState 或 CommandResult 上行主链。以上是代码快照，不是厂商或真实设备事实。

## 真实设备验收事实

当前仓库没有可复核的真实 WWTIOT 端到端验收记录。本轮没有使用真实凭据，也没有执行 `query_status`、`unlock` 或 `lock`。厂商资料、签名生成和本地伪 HTTP Provider 测试不能证明设备收到、拒绝、执行、迟到 callback 或最终锁体状态；这些事实全部为 **Unknown**。

## Provider Unknown

| 未知内容                                                           | 阻塞                                                                                                              | 不阻塞                                                                         | 关闭证据                                                                         |
| ------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------ | -------------------------------------------------------------------------------- |
| V2 是否仍为有效正式版本，标注“开发中”的接口是否已生产开放          | 正式 WWTIOT 兼容承诺、真实业务验收                                                                                | 按归档 V2 实现隔离 adapter 和合同测试                                          | 厂商盖章/可追溯正式文档及版本确认                                                |
| HTTPS 或响应签名的正式校验规则                                     | 将 acceptance/rejection evidence 提升为 `verified`、生产传输安全验收                                              | 使用 `unverified` 如实实现下行分类                                             | 厂商 HTTPS endpoint 或明确响应验签规范，加受控联调证据                           |
| `serialnum` 的范围、幂等、防重放和服务端校验                       | 自动重试、超时后重放、request key 长期兼容保证                                                                    | 当前唯一持久 key、单次发送、禁自动重试                                         | 厂商书面语义和重复/超时受控测试                                                  |
| callback 签名顺序、防重放、可靠送达、去重键和 identity incarnation | 启用公开 callback、DeviceState 更新；未来若改变当前 identity 永不复用裁决时还会阻塞复用设计                       | callback 失败关闭、identity tombstone、下行截至 Provider evidence              | 厂商 callback 合同及签名/重复/迟到受控验证；复用另需可信 incarnation 和显式迁移 |
| `online`、`offline` 的可信来源、新鲜度、过期语义、`last_seen_at` 更新来源和过期处理责任 | `online_only` 的 `unlock`、`lock` 创建与派发、连接状态/`last_seen_at` 验收；冻结 TTL 到期后 PostgreSQL 迁移、Event 时点与崩溃恢复 | `query_status`、非物理 Core 能力、adapter 下行合同测试和无新鲜证据时的失败关闭；`last_seen_at` 保持 `null` | 厂商 heartbeat/状态合同、批准的新鲜度窗口、权威时间来源和乱序规则、受控断网/Redis 丢失/恢复及 Event 验证 |
| callback 与 Command 的关联及是否存在独立 ACK/final result/query    | `device_acked`、`device_final`、Command `acked`、`success`、真实业务验收                                          | `provider_accepted -> timeout` Core 实现                                       | 厂商正式关联规则和真实设备端到端矩阵                                             |
| 真实设备的收到、执行、拒绝、延迟和锁体结果                         | smart-lock 真实设备验收                                                                                           | simulator、adapter 合同测试和可靠性建设                                        | 受控凭据、指定设备、明确写授权、设备侧观察记录                                   |
| 限流、配额、错误码、网络 timeout 和重试安全性                      | 生产容量/降级参数和自动重试                                                                                       | 保守单次发送、无自动重试                                                       | 厂商限制说明和目标网络压测                                                       |

WWTIOT 的最高可信 confirmation level 是否满足共享单车业务不由 Provider 合同决定；该产品裁决归 [Platform Target](../platform-target-contract.md)。
