---
title: WWTIOT Provider 合同
created: 2026-07-31
updated: 2026-07-31
status: frozen-for-implementation
freeze_revision: 2026-07-31.3
---

# WWTIOT Provider 合同

本文记录当前 WWTIOT 厂商资料、适配器代码事实、真实设备验收事实、confirmation level 和 Unknown。它从属于[平台边界合同](../platform-boundary-contract.md)、[当前目标合同](../platform-target-contract.md)、[领域模型合同](../domain-model-contract.md)、通用 [API 合同](../api-contract.md)与 [smart-lock Device Type 合同](../device-types/smart-lock.md)。厂商资料、代码实现和真实设备结果是三个独立证据层级，任一层都不能代替另一层。

## 证据等级与资料依据

本文使用以下证据等级：

- **厂商资料事实**：外部归档的厂商资料直接写明的接口、字段或示例；它不证明当前生产配置或真实设备行为。
- **代码事实**：当前仓库实际实现或本地自动化测试能够证明的行为；它不自动成为厂商合同。
- **真实设备验收事实**：在受控凭据、指定设备和可观测结果下取得的端到端证据。没有验收记录时必须标为 Unknown。

本次核对使用两份只读外部资料，不将原始 PDF 复制到仓库：

| 资料                                    | 厂商资料事实                                                                                                    | 资料局限                                                                               |
| --------------------------------------- | --------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| 《平台转发协议 V1.1》（6 页，2020）     | 第 2 页定义 HTTP、签名和开锁示例：`cmd=control`、`type=1`、`value=0`；第 3-5 页给出设备信息和下发指令应答格式。 | 历史版本，只用于解释版本差异，不作为当前实现映射。                                     |
| 《物网通平台转发协议 V2》（6 页，2021） | 第 1-3 页定义开锁、关锁及同步响应；第 3-4 页定义设备信息回调；第 5-6 页定义获取锁状态请求及同步响应。           | 文档页面将四项接口状态均标为“开发中”；生产可用性、当前账号配置与真实设备行为仍需验收。 |

本次只读核验文件 SHA-256：V1.1 为 `5937e0b4d68961bd07346139381c237050286e5cc8054815c276be8a5a5edcfa`，V2 为 `bb88f399c6010be5f1ab9eaa17eb36b7b680e5ef0787755f5e64b58bd689f718`。hash 只用于确认本次审阅的资料版本，不把外部附件纳入仓库或产品运行依赖。

当前适配器明确以 **V2 映射**为准。V1.1 与 V2 的命令格式不同，不能混用，也不能用 V1.1 的 `control/type=1/value=0` 解释当前开锁实现。

## Provider 元数据与配置

| 项目                 | 冻结值                           |
| -------------------- | -------------------------------- |
| code                 | `wwtiot`                         |
| name                 | `WWTIOT`                         |
| `access_type`        | `cloud_api`                      |
| `transport_protocol` | `http`                           |
| adapter              | `wwtiot_cloud_api`               |
| 调用方式             | 异步 worker 发起的单次 HTTP 下行 |

Provider code/name 固定为 `wwtiot`/`WWTIOT`，Provider request timeout 固定来自 `smart-lock` revision 1 的 10 秒；部署变量不能覆盖这些合同值。部署只提供 `WWTIOT_API_URL`、`WWTIOT_USER_ID` 与 `WWTIOT_USER_KEY`，本文不记录真实值或 secret。URL 必须是无 userinfo、query 或 fragment 的 absolute HTTP/HTTPS URL，UserID trim 后为 `1..128` UTF-8 byte，UserKey 为 `1..512` byte 且不 trim。任一缺失或不合法时 Provider 为 `unconfigured`，dispatcher 不发请求。

默认 API URL 为厂商资料给出的 `http://gps.wwtiot.com/api/`。明文 HTTP 的生产可接受性与厂商是否支持 HTTPS 为 Unknown；部署可以通过 `WWTIOT_API_URL` 选择已确认的 absolute HTTP/HTTPS endpoint，但不能据此推断 endpoint 身份已验证。

## 厂商资料中的版本差异

| smart-lock action | V1.1 厂商资料事实                          | V2 厂商资料事实                                                             | 当前采用版本 |
| ----------------- | ------------------------------------------ | --------------------------------------------------------------------------- | ------------ |
| `unlock`          | `cmd=control`、`type=1`、`value=0`。       | `cmd=open`。                                                                | V2           |
| `lock`            | 未在已核对的下行指令章节给出独立请求定义。 | `cmd=close`。                                                               | V2           |
| `query_status`    | 未在已核对的下行指令章节给出独立请求定义。 | `cmd=control`、`type=23`、`value=4`；资料说明设备随后通过回调上报当前状态。 | V2           |

两版资料都要求按文档字段顺序拼接字段值和 `UserKey` 后计算 UTF-8 字节流的 MD5。V2 的开锁/关锁请求字段顺序为 `userid + cmd + deviceid + serialnum + UserKey`，获取锁状态为 `userid + cmd + type + value + deviceid + serialnum + UserKey`。

V2 同步成功响应示例包含 `result=ok` 和 `info="cmd send ok"`。这直接支持“厂商平台接受或发送命令”的解释，不足以单独证明设备已经执行。V2 还描述了包含 `cmd`、`lockstatus`、电量、位置和时间等字段的设备信息回调；其 callback 示例 `serialnum=0`，资料没有定义它与下行 `serialnum` 的关联规则，也没有把该 callback 定义为某条命令的终局结果。

## V2 adapter action 映射

| smart-lock action | adapter 请求映射                    | 与厂商资料的关系                  |
| ----------------- | ----------------------------------- | --------------------------------- |
| `unlock`          | `cmd=open`                          | 与 V2 一致，不采用 V1.1 映射。    |
| `lock`            | `cmd=close`                         | 与 V2 一致。                      |
| `query_status`    | `cmd=control`、`type=23`、`value=4` | 与 V2 一致；不允许 payload 覆盖。 |

请求还包含 `userid`、`deviceid`、持久化的 `serialnum` 和 `sign`。adapter 按上述 V2 字段顺序无分隔拼接后执行 MD5；`serialnum` 的唯一性、防重放要求、有效范围和真实服务端校验行为仍为 Unknown。

## 响应与 confirmation level

- 非 2xx、空 body、非 JSON object、重复/尾随 JSON、缺少字符串 `result` 或关键 echo 不匹配都必须按下表分类，不能作为成功。
- HTTP Command 创建不得同步调用 adapter，也不得依据同步 Provider 响应执行 `sent -> acked -> success`；只有持久 dispatcher 可以在 Attempt 已进入 `dispatching` 后调用 adapter。
- V2 的 `result=ok` / `info="cmd send ok"` 是厂商资料中的同步响应示例；其协议语义层级最多是 `provider_accepted`，且当前响应真实性仍为 `unverified`。它不能单独推进 Device `acked` 或 `success`。
- V2 设备信息回调可以提供设备状态证据，但回调签名字段顺序、命令关联规则、结果终局性和真实环境送达行为尚未确认。平台只有在这些条件经受控验收后，才能据实提升 confirmation level。

## 冻结目标的 adapter 行为

### 下行请求

- adapter 只接受 `smart-lock` profile 的 `unlock`、`lock` 与 `query_status`，并严格生成上表 V2 字段；API payload 不能覆盖 `cmd`、`type`、`value`、`userid`、`deviceid`、`serialnum` 或 `sign`。
- `serialnum` 在 Attempt 创建时生成并持久化为 Provider request key，同一次 Attempt 重读相同值。当前生成正十进制且不超过 9 位，并以数据库唯一约束处理碰撞；该范围是平台保守选择，不是厂商限制，厂商防重放语义仍为 Unknown。
- UserID、UserKey 与完整 sign 不进入普通日志、API、Event 或 Audit。Attempt 只保留允许诊断的脱敏摘要。
- 物理 action 使用 `dispatch_once`。在厂商幂等与防重放合同未确认前，网络 timeout、连接中断、无效响应和崩溃恢复均不得自动重发。
- adapter 使用 profile 的 10 秒 request timeout，禁止自动 redirect，响应最多读取 64 KiB；Attempt 只保留最多 4 KiB 的字段 allowlist 摘要。超过上限、非 JSON object、重复 JSON key 或尾随 JSON 均为 `invalid_response`。

### 同步响应分类

| 观测                                                       | Attempt outcome               | confirmation level  | evidence status | Command 结果                                                  |
| ---------------------------------------------------------- | ----------------------------- | ------------------- | --------------- | ------------------------------------------------------------- |
| Command 创建后的配置漂移导致请求构造前发现 Provider 不可用 | `invalid_request`             | `none`              | `none`          | `failed`，`provider_not_configured`；正常创建前会同步拦截     |
| 明确未发出请求的本地连接错误                               | `transport_error_before_send` | `none`              | `none`          | `failed`；只有实现能证明未发送时适用                          |
| 请求可能已发出但没有完整响应                               | `transport_error_after_send`  | `transport_sent`    | `verified`      | `unknown`，不自动重发                                         |
| 非 2xx                                                     | `invalid_response`            | `transport_sent`    | `verified`      | `unknown`；厂商未定义 HTTP error 的终局语义，不猜测为明确拒绝 |
| 2xx 但 body 不是 JSON object，或缺少字符串 `result`        | `invalid_response`            | `transport_sent`    | `verified`      | `unknown`                                                     |
| 2xx 且 `result != ok`                                      | `provider_rejected`           | `transport_sent`    | `unverified`    | `failed`，保留厂商 `info`                                     |
| 2xx、`result=ok`，且必需 echo 与请求一致                   | `provider_accepted`           | `provider_accepted` | `unverified`    | 保持 `sent`，不能进入 `acked`/`success`                       |
| 2xx、`result=ok`，但关键 echo 缺失或不一致                 | `invalid_response`            | `transport_sent`    | `verified`      | `unknown`                                                     |

V2 同步响应包含 `sign`，但现有资料没有无歧义写明响应签名的字段顺序；代码不得猜测验证顺序。当前明文 HTTP 下响应真实性存在风险，因此仅凭结构正确的响应只能记录 `evidence_status=unverified`。厂商确认 HTTPS 或响应签名规则并经受控验证后，才能将真实 WWTIOT acceptance evidence 标为 `verified`。

结构正确的成功响应要求 `result`、`userid`、`deviceid`、`cmd`、`serialnum` 和非空 `sign`；`result` 必须严格等于小写 `ok`，前三个 echo 必须是与请求完全一致的 string。`serialnum` 接受 JSON integer 或十进制 integer string 后比较；`query_status` 还要求以同样规则匹配 `type=23`、`value=4`。`info` 和其他扩展字段不参与成功判断，只能进入 4 KiB allowlist 摘要；重复 key 或关键字段类型不符仍是 `invalid_response`。

transport 是否已发送必须由 HTTP transport 的 `WroteRequest`（或可证明等价信号）记录；DNS/dial 等明确未写请求的错误使用 `confirmation_level=none`，一旦已写或无法证明未写就使用 `transport_sent` 并进入 `unknown`。

`evidence_status` 评价本次 outcome 所依赖的证据。由本地 `WroteRequest` 单独证明的 transport/invalid-response 分类为 `verified`，明确未发送且 confirmation 为 `none` 时为 `none`；依赖当前无法验签 HTTP body 的 `provider_accepted` 和 `provider_rejected` 均为 `unverified`。Provider rejection 可以保守地令 Command 失败，但不得把未签名响应提升为更高 confirmation level。

### 设备信息 callback

- 对外入口遵循通用 `/v1/provider-callbacks/{provider_code}`，Provider adapter 负责解析，不在 Core 添加 WWTIOT 专用状态分支。
- 当前资料能支持 callback DTO 解码、字段校验、Device identity 查找与规范化映射的代码和无副作用契约测试。decoder 输入上限 64 KiB，要求单个无重复 key、无尾随值的 JSON object；`userid`、`cmd`、`deviceid`、`battery`、`lockstatus`、`time`、`serialnum` 和 `sign` 必须存在。`userid`、`cmd`、`deviceid`、`time`、`sign` 必须是非空 string；资料的示例值与参数类型标注不一致，因此 `battery`、`lockstatus` 和 `serialnum` 接受 JSON integer 或十进制 integer string 后规范化。`bike`、`lng`、`lat`、`gx`、`gy`、`gz` 是允许但不可信的可选字段，其他字段保留在受控 raw map 供诊断，不进入规范化状态。
- decoder 仅返回结构化候选消息和 validation error；它不验证签名、不写数据库、不产生 Event。`userid` 与部署配置不匹配或 Device identity 不能全局唯一映射时，validator 返回稳定失败，不生成可信 RawMessage/DeviceState。结构正确但超出当前规范化范围的 `lockstatus`、battery 或 time 按 [DeviceState 映射规则](../device-types/smart-lock.md#devicestate-规范化)保留 Unknown/null 与受控 raw 值，不猜测，也不把整条消息误报为已验证设备结果。
- decoder/validator 稳定错误码为 `callback_payload_too_large`、`callback_invalid_json`、`callback_missing_field`、`callback_invalid_field`、`callback_user_mismatch`、`callback_device_not_found` 和 `callback_device_ambiguous`。这些错误当前只用于组件契约测试与安全诊断，不改变公开 callback 的固定 503 响应。
- 当前资料不能无歧义证明 callback sign 字段顺序、防重放规则或命令关联。公开入口在这些条件确认前必须失败关闭：不得更新 DeviceState，不得产生可投递 Event，更不得推进 Command。
- 条件确认后，签名通过的 callback 可以更新 [smart-lock DeviceState](../device-types/smart-lock.md#devicestate-规范化)。只有另有可信且无歧义的命令关联证据时，才可提升 Command confirmation level。

### simulator 对齐

simulator 通过同一 Provider interface 返回 `provider_accepted`、`provider_rejected`、`transport_error_before_send`、`transport_error_after_send`、`invalid_response` 与 `0..60000` ms 可控 delay，并使用与业务 Command 相同的持久状态机。精确 API 和状态映射以 [API 合同的 Simulator 配置](../api-contract.md#simulator-配置)为准。它不提供 `device_acked`、`device_succeeded` 或其他 Device final 模式。

## 已验证与 Unknown

### 代码验收门槛

Current State 只有在自动化检查证明三项 action 的精确请求/签名映射、严格响应矩阵、请求写出前后错误分类、redirect/body 上限、敏感字段 allowlist，以及无副作用 callback decoder/validator 后，才能把 WWTIOT 代码层集成标为已实现。伪 Provider 测试只验证仓库代码，不是真实设备验收。

### 真实设备验收事实

当前仓库没有可复核的真实 WWTIOT 设备端到端验收记录。厂商资料已获得、签名代码可生成请求、本地伪 Provider 测试通过，都不能证明指定设备已收到或执行命令。因此真实设备收到、拒绝、执行、延迟回调以及最终状态的事实均为 **Unknown**。

### Unknown

- 当前使用资料是否为厂商仍有效的正式版本，以及 V2 页面“开发中”状态之后是否有修订版。
- V2 未定义的错误码、限流、幂等、防重放和 `serialnum` 约束。
- 当前凭据、设备与回调 URL 的实际配置状态。
- 设备信息回调的签名字段顺序、可靠送达、去重与命令关联规则。
- `result=ok` 在真实环境中的准确边界，以及设备信息回调能够支持的最高 confirmation level。
- 是否另有独立 Device ACK、final result callback 或结果查询能力。
- 真实设备收到、执行、拒绝和延迟响应的行为。
- timeout、限流、重试安全性、网络与 TLS 条件。
- 验证以上事实需要厂商确认、受控凭据、指定测试设备、回调观测条件和明确的真实设备写操作授权。

WWTIOT 可观测层级是否足以支撑共享单车正式业务，由产品所有者在厂商合同和受控设备证据到齐后决定。
