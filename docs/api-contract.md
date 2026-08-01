---
title: API 合同
updated: 2026-08-01
status: product-decision-required
contract_revision: 2026-08-01
---

# API 合同

本文定义服务当前真实目标所需的 HTTP 路径、认证、请求/响应 DTO、幂等 wire 规则和错误码，从属于[平台边界合同](./platform-boundary-contract.md)、[当前目标合同](./platform-target-contract.md)与[领域模型合同](./domain-model-contract.md)。对象、不变量、状态机、事务和恢复只由 Domain Model 定义；本文只规定它们如何通过 HTTP 表达。

## 命名空间

| Namespace       | 用途                 | 认证                             |
| --------------- | -------------------- | -------------------------------- |
| `/setup/...`    | 首次安装             | 仅安装前可用，并应受部署网络保护 |
| `/v1/auth/...`  | 单管理员登录与会话   | 按具体端点                       |
| `/v1/...`       | 已登录后台技术控制台 | 管理员 Bearer token              |
| `/v1/admin/...` | 仅平台级技术管理     | 管理员 Bearer token              |
| `/v1/open/...`  | 业务应用机器调用     | Project `X-API-Key`              |

后台只存在一名管理员，不建设细粒度人类 RBAC。`/v1/admin` 表示平台级技术操作，不表示组织或员工权限产品。

## 统一响应

JSON 响应使用统一 envelope：

```json
{
  "success": true,
  "status": 200,
  "code": 0,
  "message": "ok",
  "error_code": "",
  "data": {},
  "meta": null,
  "request_id": "request-id"
}
```

- `success` 表示本次 API 处理是否成功。
- `status` 是 HTTP status code。
- `code` 保留兼容性的数值结果码；成功为 `0`。失败时采用 HTTP status、固定非零值还是错误目录编号尚未由上级合同唯一裁决，按 [Platform Target 的产品裁决](./platform-target-contract.md#需要产品所有者裁决)保持阻塞；调用方当前只能以 HTTP status 和 `error_code` 分支，不得依赖失败 `code`，服务端也不得把任一候选冻结为稳定 wire。
- `error_code` 是稳定、可供调用方判断的机器错误码。
- `message` 面向人类诊断，不作为业务分支依据。
- `data` 是资源数据，`meta` 承载分页等元信息。
- `request_id` 应在请求入口生成并贯穿日志、响应和技术审计。

资源状态不能复用 envelope 的 `status` 含义：Command 使用 `status`，Device 使用 `connection_status` 与 `lifecycle_status`，Webhook Delivery 使用自己的 `status`。

## 通用标量与分页

- 资源 ID 是小写 UUID string；时间使用 UTC RFC3339，写入时接受等价 RFC3339 offset 并规范化为 UTC。
- JSON request 必须是单个 object，拒绝未知字段、重复 key、尾随 JSON 和不符合 schema 的 number/string 混用。query parameter 同一 key 只允许出现一次；重复 key 返回 `400 invalid_request`。
- 列表默认 `page=1`、`page_size=20`，`page_size` 范围 `1..100`。`meta` 至少返回 `page`、`page_size`、`total`；每类资源必须使用下文冻结的稳定排序，不能依赖数据库默认顺序。
- 空集合返回 `items: []`，可选对象字段使用 `null` 或省略规则必须在同一 DTO 内保持一致；不得用空字符串替代未知时间、ID 或状态。
- 新建资源返回 `201`，普通读取/更新返回 `200`，无 response body 的成功操作返回 envelope 中的明确结果对象，不返回裸空 body。
- 服务端为每个请求生成 UUID `request_id` 并返回 `X-Request-ID` header 与 envelope 字段；客户端提供的同名 header 只能作为独立 `client_request_id` 经长度与字符校验后记录，不能替代服务端 ID。

Device 的 `connection_status` 只能是 `unknown`、`online`、`offline`；`lifecycle_status` 只能是 `active`、`disabled`、`deleted`。只有对应 Provider 合同允许并通过验证的上报才能改变 connection status，普通 Project、Device 更新请求和当前 simulator outcome 配置都不能直接写它。WWTIOT callback 失败关闭期间，WWTIOT Device 保持 `unknown`；Omni 设备身份/连接代际真实性 Unknown 关闭前，TCP accept、自报 IMEI、心跳或 socket 写入也不能把 Omni Device 提升为 `online`。

## 首次安装

安装 namespace 只有以下端点，且必须由部署网络限制为本机或受控管理网访问：

```http
GET  /setup/status
POST /setup/test-db
POST /setup/test-redis
POST /setup/install
```

在没有待恢复副作用的正常分支，`GET /setup/status` 只返回 `needs_setup`、`installed` 和 `step="system"`，不返回路径、连接串或 secret。其余端点只在未安装时可用；安装完成后统一返回 `409 setup_completed`。同一持久配置或数据库上的并发安装至多一个成功，数据库必须独立强制仅有一个管理员。跨介质失败后的 status/install wire 在产品裁决前不属于已冻结分支。

`test-db` 与 `test-redis` 分别只接受严格 object `{"url":"..."}`，连接 timeout 为 5 秒。Database URL 只接受 `postgres`/`postgresql`，Redis URL 必须能由目标 Redis client 解析。成功只返回 `reachable=true`；响应、日志和审计都不得回显含凭据 URL。

`install` 接受：

```text
database.url
redis.url
admin.email
admin.display_name
admin.password
admin.confirm_password
server.addr
server.log_level
```

所有字段必需；拒绝未知字段。email trim 后转小写并按 email address 校验，最大 254 字符；display name trim 后长度 `2..80`；password UTF-8 byte length `8..128` 且必须与 confirm 相同；server address 是 `:port` 或 `host:port`，log level 只能是 `debug|info|warn|error`。安装只允许空管理员集合，创建唯一管理员，生成至少 32 byte 的 JWT secret 和独立的 32 byte Webhook secret encryption key；响应只返回 `installed=true`。

安装必须具有单一持久完成事实，并以可恢复方式协调运行配置、migration、唯一管理员和 secret。写入完成事实后不得回滚为“未安装”；恢复未闭合时服务保持 unready，且不得承载业务 API。完成事实写入前跨介质失败究竟 roll-forward、补偿还是 fail-stop，已进入 [Platform Target 的产品裁决](./platform-target-contract.md#需要产品所有者裁决)；裁决前不得实现或验收该崩溃分支，也不得从现有错误码猜测恢复策略。具体文件、锁、journal 或热切换机制在裁决后由 Runtime Architecture 细化，但不能降低上述不变量。

`GET /healthz` 只证明进程存活并返回 `200`。`GET /readyz` 只有在本进程已经发布完整可用运行时且无待恢复安装副作用时返回 `200`；未安装、恢复中或需要重启分别返回 `503 setup_required`、`503 setup_recovery_required`、`503 setup_restart_required`。

Setup 失败码固定为：参数/schema 不合法 `400 invalid_install_request`；数据库或 Redis 连接失败分别为 `400 database_unavailable`、`400 redis_unavailable`；users 表不为空或管理员创建冲突为 `409 admin_creation_failed`；migration、运行配置、锁/完成标记、恢复或随机源失败分别为 `500 migration_failed`、`500 config_write_failed`、`500 install_lock_failed`、`500 install_recovery_failed`、`500 secret_generation_failed`。安装目标不可写为 `500 install_target_not_writable`。响应 message 必须脱敏，不包含连接串、SQL、文件内容或 secret。

## 单管理员认证

- `POST /v1/auth/login` 接受 `email` 与 `password`，只认证 setup 创建的唯一管理员。密码使用 bcrypt hash 持久化，不进入环境文件或日志。
- `GET /v1/auth/me` 需要 Bearer token，只返回当前唯一管理员的安全身份字段；后台菜单固定来自前端冻结路由，不存在服务端菜单或菜单权限 API。
- 成功返回 `access_token`、`token_type="Bearer"`、整数 `expires_in=86400`。token 使用 HS256、至少 32 byte 的部署 `JWT_SECRET`、24 小时期限，并固定包含 `iss="device-platform"`、`aud="device-platform-admin"`、管理员 ID `sub`、`session_generation`、`iat`、`exp` 和随机 `jti`；验证必须同时检查算法、issuer、audience、时间和 generation。
- `POST /v1/auth/refresh` 只接受尚未过期且 generation 有效的 Bearer token，并签发新的 24 小时 token；它不是无期限 refresh credential。
- `POST /v1/auth/logout` 需要 Bearer token，并原子递增管理员 `session_generation`，使此前签发的全部 token 失效。单管理员模型不提供按设备选择 session 的产品能力。
- 每次受保护请求必须从数据库确认管理员仍有效且 generation 匹配；仅验证 JWT 签名不足以支持 logout 失效语义。
- login 成功/失败、refresh 和 logout 写入安全审计。对外错误统一为 `invalid_credentials` 或 `unauthorized`，不泄露账号是否存在。
- login 失败按规范化 email + 认证层确认的 client IP 限制为 15 分钟内 5 次，并按 client IP 限制为 15 分钟内 20 次；超过后返回 `429 rate_limited` 与整数 `Retry-After`。计数必须跨进程重启保存，成功登录只清除对应 email + IP 计数，不清除 IP 总量计数。
- 限流状态存储不可用时 login 返回 `503 auth_dependency_unavailable`，不能绕过限流继续认证；已登录 API 的 JWT/session generation 数据库校验也失败关闭。

首次安装完成后，除 `GET /setup/status` 外的 `/setup/...` endpoint 返回 `409 setup_completed`。安装只创建一个管理员；当前不提供管理员、员工、角色或权限 CRUD。

## Project 机器认证与隔离

`/v1/open/...` 使用：

```http
X-API-Key: {project_api_key}
```

- API Key 固定为 `dp_` 加 32 个随机 byte 的无 padding base64url 编码；服务端对完整 UTF-8 key 计算 SHA-256 并以 digest 唯一索引，只在创建或轮换时显示明文。
- 非本地环境必须使用 HTTPS。
- Project 配置 IP whitelist 时，认证层必须执行来源校验；空列表表示允许任意来源。默认只信任连接的 `RemoteAddr`，不得直接信任任意客户端提供的 `X-Forwarded-For`；只有显式配置可信反向代理后才按受控规则解析转发头。
- 认证后得到唯一 `project_id`，查询与写入都必须以它作为隔离条件。
- 每个 Device 只属于一个 Project；传入其他 Project 的 `device_id` 应按不可见资源处理。
- 不使用共享单车用户身份、订单身份或后台管理员 session 代替 Project 机器认证。

## 当前 Open API

```http
GET    /v1/open/projects/{project_id}
GET    /v1/open/devices
GET    /v1/open/devices/{device_id}
GET    /v1/open/device-commands
POST   /v1/open/device-commands
GET    /v1/open/device-commands/{command_id}
POST   /v1/open/device-commands/{command_id}/cancel
```

设备详情应包含可信的当前状态快照 `current_state`。接口只暴露设备技术事实，不添加自行车、用户、订单、停车、还车或计费 API。

## 当前管理 API

冻结实现至少提供以下后台技术接口；列表接口统一支持 `page`、`page_size` 与资源所需过滤条件，并在 `meta` 返回分页信息：

| Resource    | Routes                                                                                                                                          | 语义                                                                                 |
| ----------- | ----------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------ |
| Project     | `GET/POST /v1/projects`、`GET/PATCH /v1/projects/{id}`、`POST /v1/projects/{id}/api-key/rotate`、`POST /v1/projects/{id}/webhook-secret/rotate` | 创建、查看、更新机器接入配置；明文 API key 或 Webhook secret 仅各自创建/轮换时返回。 |
| Device Type | `GET /v1/device-types`、`GET /v1/device-types/{code}`                                                                                           | 只读查看随发布交付的 capability profile。                                            |
| Provider    | `GET /v1/cloud-providers`                                                                                                                       | 只读查看注册与配置状态，不返回或修改 secret。                                        |
| Device      | `GET/POST /v1/devices`、`GET/PATCH /v1/devices/{id}`                                                                                            | 管理技术设备和 lifecycle；不提供业务资产关系。                                       |
| Command     | `GET/POST /v1/device-commands`、`GET /v1/device-commands/{id}`、`POST /v1/device-commands/{id}/cancel`                                          | 后台诊断性创建、查询与允许的取消，使用同一 Command 服务。                            |
| Event       | `GET /v1/events`、`GET /v1/events/{id}`                                                                                                         | 只读技术事件；不开放任意 Event 注入接口。                                            |
| Webhook     | `GET /v1/webhook-deliveries`、`GET /v1/webhook-deliveries/{id}`、`POST /v1/webhook-deliveries/{id}/resend`                                      | 查看投递及 Attempt；只允许 dead delivery 受审计重发。                                |
| Audit       | `GET /v1/audit-logs`、`GET /v1/audit-logs/{id}`                                                                                                 | 只读技术审计。                                                                       |
| Simulator   | `GET/PATCH /v1/simulator`                                                                                                                       | 管理显式 simulator Provider 的可重复行为；不建立平行命令域。                         |

上表只冻结路由和领域效果。Project/Device PATCH、Command cancel、凭据 rotate、Webhook resend 与 simulator PATCH 的成功 `data` DTO 尚未由上级合同唯一裁决，候选口径、阻塞与关闭证据见 [Platform Target](./platform-target-contract.md#需要产品所有者裁决)。裁决前不得为这些 mutation 生成稳定 SDK 或通过 wire 验收；这不影响其只读资源 DTO 和失败响应合同。

列表只接受以下资源过滤参数，均为 exact match；未列出的 query key 返回 `400 invalid_request`：

| List route                 | 允许过滤参数                                                                               |
| -------------------------- | ------------------------------------------------------------------------------------------ |
| `/v1/projects`             | `name`                                                                                     |
| `/v1/devices`              | `project_id`、`device_type_code`、`provider_code`、`connection_status`、`lifecycle_status` |
| `/v1/device-commands`      | `project_id`、`device_id`、`command_type`、`status`                                        |
| `/v1/events`               | `project_id`、`device_id`、`command_id`、`event_type`                                      |
| `/v1/webhook-deliveries`   | `project_id`、`event_id`、`status`                                                         |
| `/v1/audit-logs`           | `project_id`、`actor_type`、`action`、`result`、`resource_type`、`resource_id`             |
| `/v1/open/devices`         | `device_type_code`、`provider_code`、`connection_status`、`lifecycle_status`               |
| `/v1/open/device-commands` | `device_id`、`command_type`、`status`                                                      |

各列表的稳定排序固定为：

| List resource    | 排序                              |
| ---------------- | --------------------------------- |
| Project          | `created_at DESC, id DESC`        |
| Device Type      | `code ASC`                        |
| Provider         | `code ASC`                        |
| Device           | `created_at DESC, id DESC`        |
| Command          | `created_at DESC, id DESC`        |
| Event            | `occurred_at DESC, event_id DESC` |
| Webhook Delivery | `created_at DESC, id DESC`        |
| Audit            | `occurred_at DESC, id DESC`       |

Admin 与 Open API 的同类资源使用相同排序。Device Type 与 Provider registry 也接受通用 `page`、`page_size`，除此之外不接受过滤或排序 query；调用方不能改变排序字段或方向。

Open API 的 Project scope 始终来自认证，不接受 `project_id` query。`GET /v1/open/projects/{project_id}` 的 path ID 必须等于认证得到的 Project ID；不相等时按不可见资源返回 `404 not_found`。枚举过滤值必须属于对应稳定枚举；UUID 过滤值必须合法，否则返回 `400`，不能静默产生空列表。

HTTP Provider callback namespace 是 `POST /v1/provider-callbacks/{provider_code}`，不使用管理员或 Project 凭据，而由对应 Provider adapter 按厂商合同验证身份、schema 与防重放条件。当前 `wwtiot` 路径固定返回 `503 provider_callback_unverified`，不读取或保存 body，也不更新 Device、Command、RawMessage 或 Event；`omni` 是 direct-device TCP Provider，不暴露 HTTP callback，该路径返回 `404 not_found`；其他未注册 code 同样返回 `404`。WWTIOT 签名顺序确认并重新冻结前不得改变固定 503 行为。

### Simulator 配置

`GET /v1/simulator` 返回当前部署级 simulator Provider 配置；`PATCH /v1/simulator` 接受且只接受：

```text
outcome:  required; provider_accepted | provider_rejected |
          transport_error_before_send | transport_error_after_send |
          invalid_response
delay_ms: required integer; 0..60000
```

配置变更影响变更提交后新领取的 simulator Attempt，不改写已创建或已领取 Attempt。simulator Device 创建的 Command 必须经过与 WWTIOT、Omni 相同的 dispatcher、持久 CommandAttempt、Command 状态机、Event、Outbox/Webhook 和 Audit 链路；simulator 只替换 Provider adapter 的受控结果，不拥有平行 Command map 或状态机。

| simulator 配置模式            | 持久 Attempt outcome          | confirmation/evidence            | Command 结果                                                                  |
| ----------------------------- | ----------------------------- | -------------------------------- | ----------------------------------------------------------------------------- |
| `provider_accepted`           | `provider_accepted`           | `provider_accepted` / `verified` | 保持 `sent`，观察期限后 `timeout`                                             |
| `provider_rejected`           | `provider_rejected`           | `transport_sent` / `verified`    | `failed`，reason `provider_rejected`                                          |
| `transport_error_before_send` | `transport_error_before_send` | `none` / `none`                  | `failed`，reason `provider_transport_error`                                   |
| `transport_error_after_send`  | `indeterminate`               | `transport_sent` / `verified`    | 保持 `sent`，Attempt reason `provider_delivery_unknown`，观察期限后 `timeout` |
| `invalid_response`            | `indeterminate`               | `transport_sent` / `verified`    | 保持 `sent`，Attempt reason `provider_response_invalid`，观察期限后 `timeout` |

表中后两项是 simulator 的输入模式名；持久 Attempt `outcome` 统一为 `indeterminate`，具体模式进入稳定 reason，不形成第二套生命周期状态。`delay_ms` 不延迟 durable Attempt 的创建。`transport_error_before_send` 在 delay 后返回且不记录模拟写出；其他模式先记录受控模拟写出，再等待 delay。若 dispatcher 的 Provider request timeout 在 delay 期间到达，结果按 `transport_error_after_send` 模式处理，不再应用配置的 outcome。simulator 的 `verified` 只证明平台按受控配置观察到模拟结果；Provider code 与 Event source 必须保留 `simulator`，它不证明真实 WWTIOT 或设备行为。simulator 不提供 `device_acked`、`device_succeeded` 或其他 Device final 模式；因此模拟结果不能证明真实设备收到或执行命令。

## 资源 DTO

### Project

创建请求允许 `name`、可选 `webhook_url` 和 `ip_whitelist`；名称 trim 后长度 `1..120`。IP whitelist 每项必须是合法单 IP 或 CIDR，规范化并去重。非本地 Webhook URL 必须是 HTTPS，不允许 embedded credential 或 fragment。

Project 常规读取返回 `id`、`name`、`webhook_url`、`webhook_configured`、`ip_whitelist`、`created_at`、`updated_at`，绝不返回 API key hash、Webhook secret 或 secret hash。当前目标每个 Project 同时只能配置一个 `webhook_url`，不提供 endpoint 集合或每事件路由。创建响应额外一次性返回 `api_key`；设置首个 Webhook endpoint 或显式轮换时额外一次性返回 `webhook_secret`。Webhook secret 固定为 `whsec_` 加 32 个随机 byte 的无 padding base64url 编码，完整 UTF-8 字符串作为 HMAC key。PATCH 只允许更新 `name`、`webhook_url` 与完整替换的 `ip_whitelist`。

`webhook_url` 首次从 `null` 设为 URL 时生成 secret；改变非空 URL 继续使用当前 secret，除非调用 rotate。设为 `null` 只停止为后续 Event 创建 Delivery，既有 Delivery 继续使用其配置 snapshot；重新启用时复用当前 secret 且不返回明文。`webhook-secret/rotate` 只在 endpoint 非空时允许，生成新 version；新 Event 使用新 version，既有 Delivery 保持旧 version。API key rotate 与 Webhook secret rotate 均在新凭据提交后立即使旧凭据不再用于新请求/新 Delivery。

### Device Type 与 Provider

Device Type 读取返回 `code`、`revision`、`name` 和 actions；每个 action 至少包含 identifier、payload schema、risk level、delivery policy、dispatch deadline、provider request timeout、result observation timeout 与 retry/override flags。

Provider 读取返回 `code`、`name`、`access_type`、`transport_protocol`、`adapter`、`profiles` 和 `integration_status=unconfigured|configured_unverified|verified`，不返回 endpoint、UserID、UserKey、监听地址或签名材料。`profiles` 是只读 opaque identifier 列表；具体含义只来自 Provider 合同。当前 registry 固定为：

| code        | name        | access_type    | transport_protocol | adapter            | profiles                                                     |
| ----------- | ----------- | -------------- | ------------------ | ------------------ | ------------------------------------------------------------ |
| `wwtiot`    | `WWTIOT`    | `cloud_api`    | `http`             | `wwtiot_cloud_api` | `wwtiot-cloud-api-v2`                                        |
| `omni`      | `Omni`      | `direct_device` | `tcp`             | `omni_direct_tcp`  | `omni-bike-tcp-v2.0.7`, `omni-iot-tcp-v1.3.5`                |
| `simulator` | `Simulator` | `simulator`    | `internal`         | `simulator`        | `simulator-v1`                                               |

WWTIOT 必需配置缺失或格式无效时为 `unconfigured`；配置完整但尚未取得已验证响应真实性的受控真实环境证据时为 `configured_unverified`。Omni listener 配置缺失时为 `unconfigured`；配置完整但设备认证、profile 匹配和连接代际未验证时最多为 `configured_unverified`。当前合同没有定义自动提升 WWTIOT 或 Omni `verified` 的权威状态，真实联调记录不能通过隐式进程状态提升它。Simulator 是进程内受控 adapter，注册且配置可读写时为 `verified`。Provider `verified` 只评价 adapter 接入，不代表任何 Device 已执行命令或达到 Device final result。

### Device

创建请求允许：

```text
project_id:         required for admin API
name:               required, trimmed length 1..120
device_type_code:   required; current value smart-lock
provider_code:      required; current value wwtiot, omni or simulator
provider_profile:   required; must belong to the selected Provider
provider_device_id: required for wwtiot or omni; forbidden for simulator
```

`provider_code` 当前只能是 `wwtiot|omni|simulator`。WWTIOT `provider_device_id` trim 后必须匹配平台约束 `[A-Za-z0-9._:-]{1,128}`；Omni 必须是 15 位十进制 IMEI；simulator Device 的 `provider_device_id` 由平台固定为新建 Device UUID。调用方必须显式提交 registry 中属于所选 Provider 的 `provider_profile`，不得让服务按 IMEI 或品牌猜测；simulator 固定为 `simulator-v1`。当前没有允许写入的 Device metadata key，因此创建和更新请求都拒绝 `metadata`。`access_type`、`transport_protocol` 和 `adapter` 由 Provider registry 推导；调用方不能写。新建 Device 固定以 `lifecycle_status=active`、`connection_status=unknown`、`current_state=null`、`last_seen_at=null` 开始。PATCH 只允许更新 `name` 和 lifecycle；Project、Device Type、Provider identity 与 profile 均不可更新。

Device lifecycle 迁移和终态只以 Domain Model 为准。API PATCH 只接受 `name` 和目标 `lifecycle_status=active|disabled|deleted`；同一请求同时提供 `name` 与目标 `deleted` 返回 `400 invalid_request`。列表默认不隐藏 `deleted`，Admin 与所属 Project 仍可读取历史。每次有效 lifecycle 变化写 Audit 与 `device.lifecycle_changed` Event，其固定 `reason_code=admin_requested`；名称变化只写 `device.updated` Audit。未发生值变化的 name-only PATCH 返回当前资源且不写 Audit。

Device 注册允许选择当前 `integration_status=unconfigured` 的已注册 Provider；这只建立技术资源，不允许创建 Command，后者仍返回 `409 provider_not_configured`。未知 `project_id`、Device Type、Provider code 或 Provider profile 返回 `404 not_found`；profile 不属于所选 Provider、非法/缺失/被禁止字段或非法 Provider device ID 返回 `400 invalid_request`；任何 lifecycle 的 Provider identity 冲突都返回 `409 provider_device_conflict`。profile 不参与放宽 identity 唯一性；`deleted` Device 永久保留 identity tombstone，当前 API 不提供释放或复用入口。

Device 读取至少返回上述稳定 identity、`provider_profile`、派生接入字段、connection/lifecycle status、只读 `current_state`、`last_seen_at` 与时间戳。`current_state` 没有可信上报时为 `null`，不能用创建请求 metadata 伪造。对应 Provider 的在线真实性 Unknown 关闭前，`last_seen_at` 固定为 `null`；关闭时必须由 Provider 合同同时冻结哪些可信事实更新该字段、采用 Provider 时间还是平台观察时间以及重复/乱序消息的单调更新规则，不能由任意 RawMessage、TCP accept 或读取投影推断。

### Command

Open API 创建请求字段见下文；管理员 API 额外要求 `project_id`。Command 读取至少返回：

```text
id, project_id, device_id, provider_code, provider_profile, command_type, payload,
device_type_revision, delivery_policy, status, reason_code, reason_detail,
confirmation_level, evidence_status, idempotency_key, queued_at,
dispatch_deadline_at, sent_at, result_deadline_at,
finished_at, created_at, updated_at
```

详情额外返回按 `attempt_no ASC` 排序的 Attempts、按 `observed_at ASC, result_id ASC` 排序的不可变 Results，以及按 `occurred_at ASC, event_id ASC` 排序的相关 Events，使单条 Command 的历史按时间正序且同时间结果稳定。Attempt 至少返回 `attempt_no`、`phase`、provider/adapter、provider request key、开始/完成时间、outcome、reason code、confirmation level、evidence status、错误与脱敏摘要；`phase` 遵循领域合同的 `claimed|dispatching|completed`。Result 至少返回 `result_id`、可选 `attempt_id`、source、outcome、confirmation level、evidence status、`reported_at|null`、`observed_at` 与 `late`，不返回原始签名或 secret。列表默认不嵌入完整 request/response 摘要。Attempt/Result 的摘要必须经过 adapter 字段 allowlist 与脱敏，不可只靠通用 key 名黑名单。

### Device State、Event、Webhook 与 Audit

Device 的 `current_state` 没有可信上报时为 `null`；存在时使用以下最小 envelope，Device Type 专用字段只进入 `state`：

```text
state_id, schema_version=1, project_id, device_id,
device_type_code, provider_code, provider_profile, reported_at|null, observed_at,
evidence_status, state
```

`evidence_status` 必须为 `verified`；无法验签、无法唯一映射 Device 或 schema 不合法的上报不得产生 DeviceState。`smart-lock` 的 `state` 字段见其从属合同，未知锁态可以是类型内的 `lock_state=unknown`，但这与 Command 生命周期无关。

Event 是只读资源，v1 envelope 固定包含：

```text
event_id, schema_version=1, event_type, project_id,
device_id|null, command_id|null, occurred_at, source, payload
```

Event 类型由 Domain Model 冻结；HTTP 与 Webhook 的 v1 wire payload 必须按下表投影，不得新增另一套事件含义：

| `event_type`                | 必需关联        | `payload` v1 必需字段                                                             |
| --------------------------- | --------------- | --------------------------------------------------------------------------------- |
| `device.created`            | Device          | `device_type_code`、`provider_code`、`provider_profile`、`lifecycle_status`        |
| `device.lifecycle_changed`  | Device          | `from`、`to`、`reason_code`                                                       |
| `device.connection_changed` | Device          | `from`、`to`、`evidence_status`                                                   |
| `device.state_updated`      | Device          | `state`、`observed_at`、`evidence_status`                                         |
| `command.created`           | Device、Command | `command_type`、`delivery_policy`、`status`                                       |
| `command.status_changed`    | Device、Command | `from`、`to`、`reason_code`、`confirmation_level`、`evidence_status`              |
| `command.evidence_updated`  | Device、Command | `status`、`attempt_id`、`outcome`、`confirmation_level`、`evidence_status`        |
| `command.result_recorded`   | Device、Command | `status`、`result_id`、`outcome`、`confirmation_level`、`evidence_status`、`late` |

`command.status_changed` 要求 `from != to`，只表达 Command 状态迁移。`command.evidence_updated` 要求 `status` 仍为当前 Command 状态，且关联 Attempt 已完成并实际改变 Command 的 confirmation level 或 evidence status；它与该聚合更新、Event 及初始 Delivery 在同一事务内按稳定 deduplication key 写入。`command.result_recorded` 对应一个不可变 CommandResult；`late=true` 时 Command 已是终态，Event 仍携带原终态且不得触发状态改写。confirmation level 严格按 `none < transport_sent < provider_accepted < device_acked < device_final` 单调提升。`none` 层的 evidence 必须保持 `none`；`transport_sent|provider_accepted` 的 evidence 可以是 `unverified|verified`；`device_acked|device_final` 必须是 `verified`。confirmation level 不变时只允许 `unverified -> verified`，`verified` 不得回退；confirmation level 提升到 `transport_sent|provider_accepted` 时，evidence 改为保守评价支撑新层级的决定性证据，因新证据未验签可以从较低层的 `verified` 变为新层的 `unverified`，这不是同层证据回退。Provider acceptance 使用 `command.evidence_updated`，不得用它暗示 Device ACK 或 final result。

`source` 只能是 `admin`、`open_api`、`provider_message`、`simulator` 或 `system`。不适用的 `reason_code` 使用 `null`，不能用空字符串代替。Event payload 可以在同一 schema version 内增加调用方必须忽略的可选字段，但不能删除、改名或改变上述字段语义；破坏性变化必须提升 schema version。

Webhook Delivery 详情返回 `id`、`event_id`、`project_id`、target snapshot、配置 version、`status`、attempt count、next attempt time、`replay_of_delivery_id`、时间戳和按 attempt number 排序的独立 DeliveryAttempts；不返回 secret 或可重放签名。每个 DeliveryAttempt 返回 attempt number、开始/结束时间、HTTP status、截断脱敏 response 摘要和 error。

Audit 读取固定返回 `id`、`actor_type`、`actor_id`、`project_id`、`action`、`result`、`resource_type`、`resource_id`、`ip_address`、`request_id`、脱敏 `metadata` 和 `occurred_at`。`actor_type` 只能是 `admin`、`project`、`provider` 或 `system`；`result` 只能是 `success` 或 `failure`。不适用的关联字段使用 `null`。

当前稳定 Audit action 为 `auth.login`、`auth.refresh`、`auth.logout`、`project.created`、`project.updated`、`project.api_key_rotated`、`project.webhook_secret_rotated`、`project.webhook_secret_decryption_failed`、`device.created`、`device.updated`、`device.lifecycle_changed`、`command.created`、`command.cancelled`、`provider.message_received`、`provider.message_rejected`、`webhook.delivery_replayed` 和 `simulator.updated`。首次安装以完成标记作为唯一完成事实，不写无法与文件标记原子提交的 `setup.completed` Audit。`project.webhook_secret_decryption_failed` 的 metadata 规则保持不变。worker 的普通状态迁移只写 Attempt/Event，不重复写 Audit。

Omni 上行 Audit 固定使用 `actor_type=provider`、`actor_id=omni`、`resource_type=device_raw_message` 和对应 RawMessage ID。通过 parser、显式 profile 与唯一当前 Device binding 校验的消息使用 `provider.message_received/result=success`，并关联已解析 Device 的 Project；该 success 只表示平台接收并持久化一条 `unverified` 技术消息，不表示真实性验证、Device ACK 或 final。schema、首报、identity、profile 或 lifecycle 校验失败时使用 `provider.message_rejected/result=failure`，无法映射时 Project/Device 关联为 `null`。两类 metadata 只允许 `provider_code`、`provider_profile`、`adapter`、平台生成的 `connection_id`、`frame_bytes`、`parse_status`、`evidence_status`、`duplicate`、`raw_message_id`，以及按分支适用的 `command`、`field_count`、稳定 `error_code`/`reject_code`；不得保存 IMEI、raw frame、socket error、secret 或认证材料。当前公开 WWTIOT callback 固定 503 且不读取 body，因此在该入口重新冻结并启用前不产生 Provider message Audit。

## 四层 API 责任

本合同落实[平台边界合同](./platform-boundary-contract.md)的四层分工：

| 层                       | API 与生命周期责任                                                                                                                                                             |
| ------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Platform Core            | 接收通用 Command envelope，校验 Project/Device 归属，执行幂等、通用投递、状态迁移、Attempt、Result、timeout、cancel 和 Event；不解释具体 action 的设备或业务含义。             |
| Device Type Capability   | 为 action identifier 提供语义、payload schema、风险等级，以及平台明确采用或经真实证据确认的在线要求、重放/补偿规则、timeout 和 delivery policy；两类依据必须在从属合同中标明。 |
| Gateway/Provider Adapter | 把 action 与 payload 映射为厂商或设备协议请求，并将 Provider acceptance、Device ACK、Device final result 分层映射为通用 Attempt/Command 事实。                                 |
| Business Application     | 决定为何发起 action、业务用户是否有权发起，以及最终技术结果如何改变订单、计费等业务状态；这些判断不进入本 API 的设备核心模型。                                                 |

例如调用方提交 `command_type={capability_action}` 时，Core 只把该值当作 capability/action identifier。其规范化语义来自对应 Device Type Capability 合同，厂商映射来自 Provider 合同，业务授权与业务状态变化由 Business Application 负责。

## 创建 Command

`POST /v1/open/device-commands` 接受：

```text
device_id:        required
command_type:     required; wire field carrying a device capability/action identifier
payload:          optional; required when command type needs parameters
idempotency_key:  required; trimmed UTF-8 length 1..128
```

Project 范围来自认证结果，不接受调用方用 body 或 header 任意切换 Project。

当前 revision 2 的三个 action 均把缺失 payload 规范化为空 object，并拒绝调用方提供 `delivery_policy`、`expires_at`、deadline、retry 或 Provider 字段。`unlock`/`lock` 只在 Device `connection_status=online` 时允许创建，否则返回 `409 device_not_online`；`query_status` 不使用该门槛。Command 持久保存创建时采用的 `device_type_revision` 和全部派生执行参数；后续 profile 发布不能静默改变已创建 Command。

首次使用新 idempotency key 创建前，同步校验 Project、Device 归属、Device lifecycle、Device Type profile、action/payload、`online_only` 门槛、Provider/profile action 映射、Provider 注册与配置。`device_disabled`、`device_not_online`、`provider_not_configured`、`provider_action_unsupported` 或 `provider_mapping_unknown` 返回 `409`，不创建 Command；最后一项表示厂商字段存在但当前证据不足以形成唯一安全映射，不能降级成未配置、设备拒绝或传输失败。Device Type 不支持的 capability 或 payload 返回 `422`，不创建 Command。Command 创建提交后才由持久 dispatcher 异步投递，因此创建响应不等待 Provider I/O。

当前 wire field 继续使用 `command_type`，但它的合同含义是设备 capability/action identifier，不是 Core 全局固定命令枚举。具体允许值及 payload 由 Device 的最小 capability profile 决定。未来若重命名字段，应作为显式 API 变更处理，不能同时维护两套语义来源。

### 幂等

幂等范围：

```text
project_id + idempotency_key
```

请求 hash：

```text
sha256(canonical_json(
  device_id, device_type_code, provider_code, provider_profile, provider_device_id,
  command_type, normalized_payload,
  device_type_revision, delivery_policy, dispatch_deadline_ms,
  provider_request_timeout_ms, result_observation_timeout_ms,
  retry_allowed
))
```

服务器首次创建时完成 capability payload 规范化、冻结所有派生执行参数并计算 hash。Project 已包含在幂等唯一范围内；hash 覆盖其余所有影响执行对象、动作、Provider、payload、时限、投递和重试效果的字段。物理动作 `unlock`/`lock` 强制提供非空 `idempotency_key`；当前 API 对 `query_status` 也保持同一要求。幂等记录必须持久化并受唯一约束保护，不能只依赖单进程内存。

重放必须先查询历史 Command，再执行任何当前 Device/Profile/Provider preflight。调用方 `device_id`、规范化 `command_type` 与 payload 和历史 Command 一致时返回该 Command 及其首次冻结的完整派生语义；不重新计算当前 hash，不创建 Attempt、Event、Audit 或新物理动作。任一调用方字段不一致返回 `409 idempotency_key_conflict`。并发首次请求必须通过持久唯一约束和事务锁收敛为一个 `201` 与其余 `200` replay。

canonical JSON 使用 UTF-8、对象 key 词典序、无无意义空白，并在 capability 校验后使用规范化 scalar 与 UTC RFC3339 时间。新建 Command 返回 `201`；幂等重放返回 `200` 并携带 `meta.idempotent_replay=true`。两种响应都只表示平台接受了 API 请求，不表示 Provider 或设备成功。

## Command、Attempt 与 Result

Command、Attempt、Result 的状态机、证据单调性、终态和 reason 映射以[领域模型合同](./domain-model-contract.md)为唯一权威。HTTP wire 必须使用其枚举全集：

```text
Command.status: queued | sent | acked | success | failed | timeout | cancelled
Attempt.phase: claimed | dispatching | completed
Attempt.outcome: not_dispatched | invalid_request | provider_accepted |
                 provider_rejected | transport_error_before_send | indeterminate
Result.outcome: device_acked | device_succeeded | device_failed
confirmation_level: none | transport_sent | provider_accepted | device_acked | device_final
evidence_status: none | verified | unverified
```

`reason_code` 必须使用 Domain Model 的固定映射；Attempt 的投递歧义只使用 `provider_delivery_unknown|provider_response_invalid`。`reason_detail`、请求/响应摘要和 Result payload 必须经过 Provider allowlist 脱敏，不返回 secret、签名材料或原始 transport 错误。

HTTP 创建或幂等重放只表示平台持久接受请求。Provider acceptance 仍返回 Command `status=sent`；只有可信 Device Result 可以产生 `acked|success`。迟到结果在详情中作为 `late=true` 的不可变 Result 返回，原终态保持不变。

## Capability Profile 与 Delivery Policy

Platform Core 只执行 capability profile 提供的通用 metadata，不固定某个设备 action 的 timeout、离线策略、重试次数、补偿窗口或默认 delivery policy。Core 的判断依据是 metadata，而不是具体 action identifier。当前 profile 见 [smart-lock Device Type 合同](./device-types/smart-lock.md)。

- 物理动作及其他高风险 action 没有可信 Device final result 时，不得重放或补偿为 `success`。
- 厂商或设备自身的 timeout、离线行为、重试安全性和补偿语义在证据到齐前保持 Unknown。平台为防止永久悬挂采用的保守观察期限与禁重放策略由具体 Device Type profile 明确定义，不得表述为厂商事实。
- 具体 Device Type 的 action 与规范化语义只在 Device Type Capability 合同中定义；Provider 当前映射与厂商事实只在 Provider 合同中定义。二者都不得扩大本合同的 Core 语义。

## 派发期限与结果观察

- timeout 从 Command 进入 `sent` 开始计算。
- 当前 Command 创建后 30 秒内未进入 `sent` 时转为 `timeout`，reason 为 `dispatch_deadline_exceeded`；进入 `sent` 后按 profile 的结果观察期限处理。到期时 Command 为终态 `timeout/result_observation_timeout`，最终执行结果按 indeterminate 对待；既有 Attempt outcome 不被覆盖，也不伪造新的 Attempt。当前 profile 不提供离线排队、恢复投递、自动重试或晚到补偿。
- Provider response 或可信 Device final 与 result deadline 并发时，截止判定点、事务优先级及冲突 verified final 的关闭规则按 [Platform Target 的产品裁决](./platform-target-contract.md#需要产品所有者裁决)保持阻塞；上条只冻结没有竞争结果事实时的顺序 timeout。
- 无论采用何种实现，Command 都不能永久悬挂；处理进度必须持久化并能在进程重启后恢复，状态变化产生的 Event 不能丢失。
- capability profile 标记的物理动作没有可信最终证据时不得在离线恢复或 timeout 后重放，也不得补偿为 `success`。
- 晚到结果按当前合同追加 CommandResult/Event，并准确标注 confirmation level 与 `late=true`；它不重放动作，也不覆盖或伪造原结果。

自动网络重试不是默认合同。只有 Provider 证明请求可安全重放、幂等边界明确，并经过受控验收后，才能为具体 action 配置重试。

dispatcher、deadline scanner、Provider message handler 与 webhook dispatcher 的领取、事务和崩溃恢复责任只以[领域模型合同](./domain-model-contract.md)为准。外部 HTTP 或 socket I/O 不得发生在持有领域数据库事务期间。

## Event、Outbox 与 Webhook

规范化 Event 与 Outbox/Delivery 记录必须可靠持久化：

1. 状态变化产生 Event。
2. Event、相关状态，以及 Project 当时配置了 endpoint 时的初始 Delivery，必须在同一数据库事务中提交；未配置 endpoint 时只提交 Event。不得提交后补建初始 Delivery。
3. worker 领取待投递记录时创建独立 Webhook DeliveryAttempt，再尝试发送签名 Webhook；Attempt 必须区分尚未开始 HTTP、已写出和已完成，不能用记录存在推断接收方收到请求。
4. 成功转为 `delivered`；失败在 Delivery 与 Attempt 中记录次数、错误与下次重试时间。
5. 首次投递立即领取，失败后按 `1s, 5s, 30s, 2m` 调度，默认最多 5 个 DeliveryAttempt 预算；每次进入 `sending` 的领取都占用一次预算，即使在 HTTP 写出前崩溃。该值可由部署配置降低或延长间隔，但不能超过 5 次而无需重新修订合同。耗尽后转为 `dead`。Webhook secret 解密失败不发送 HTTP，但其是否创建/消耗 DeliveryAttempt、重试或立即 `dead` 仍由 [Domain Unknown](./domain-model-contract.md#domain-unknown) 阻塞。
6. 管理员可对 `dead` 记录执行受审计的手动重发；重发要求 Project 当前有启用的 endpoint，使用重发时的当前 target/secret version 创建新 Delivery，并通过 `replay_of_delivery_id` 指向原记录。原 Delivery 的 snapshot、状态和 Attempt 历史保持不变；当前 endpoint 未配置时返回 `409 webhook_not_configured`。

Webhook Delivery 状态：

```text
pending
sending
delivered
failed
dead
```

Delivery 状态机、终态和崩溃恢复只以 Domain Model 为准。Attempt count 在进入 `sending` 的领取事务中递增，因此崩溃窗口也占用一次上限并允许 at-least-once 重试；manual resend 只创建新 Delivery。

Webhook raw body 是对应 Event 的完整 v1 JSON envelope，不另造第二套 payload；每个 Delivery 创建时序列化并持久保存一次，同一 Delivery 的自动重试逐 byte 复用该 body。当前 wire version 固定为 `v1`，每次实际 Attempt 生成当时的 Unix 秒 `timestamp`，并使用该 Delivery snapshot 的正整数 `secret_version`。签名输入逐 byte 固定为 ASCII `v1.` + 十进制 `timestamp` + `.` + 十进制 `secret_version` + `.` + `raw_body`，签名值为 `v1=<hex(HMAC-SHA256(webhook_secret, signing_input))>`。

请求必须携带 `X-Device-Platform-Timestamp`、`X-Device-Platform-Signature`、`X-Device-Platform-Event-ID` 和 `X-Device-Platform-Secret-Version`。接收方必须选择该 version 的 secret、使用常量时间比较验签、确认 header event ID 与 body `event_id` 一致，并拒绝与接收时刻绝对偏差超过 300 秒的 timestamp；边界 `<=300` 秒有效。接收方按 `event_id` 幂等消费，secret 轮换期间只保留仍被有效 Delivery 或 300 秒验签窗口引用的版本。任意 2xx 为 delivered；其他 status、网络错误或 timeout 均进入失败调度。平台承诺至少一次投递，不承诺恰好一次。

非本地 Webhook endpoint 必须使用 HTTPS。dispatcher 不自动跟随 redirect，响应 body 最多保留 4 KiB 且按敏感字段规则脱敏；连接和响应 timeout 默认 10 秒。目标允许内部 HTTPS endpoint，但 dispatcher 必须按部署级 egress allowlist 校验解析后的每个目标地址并在连接时防止 DNS rebinding；默认拒绝 loopback、link-local、multicast 和云 metadata 地址，只有明确受控的部署 allowlist 才能开放所需内部网段。Project 请求参数不能绕过该策略。

Command Attempt 与 Webhook Delivery Attempt 是两类独立技术记录，不能共享状态、计数或术语归属。前者描述设备命令派发，后者描述事件回调的单次 HTTP 投递。

## 技术审计

以下平台内可观察行为至少应写入不可由普通业务调用覆盖的技术审计：管理员登录与安全变更、Project 凭据创建/轮换、Device 创建与生命周期变更、Command 创建/取消、Webhook 端点与 secret 变更、dead delivery 重发和模拟模式变更。当前 Provider secret/config 只由部署环境提供，不存在平台内配置 mutation；其变更记录属于部署审计，在形成新的在线配置合同前不得让实现自行新增领域 Audit action。

审计记录应包含 actor 类型与标识、Project/资源、动作、结果、来源 IP、request_id、发生时间和脱敏 metadata。技术审计不承载共享单车业务审计。

## 合同一致性要求

- API DTO、数据库约束、领域状态机、Provider 映射和前端展示必须共享同一组合同语义。
- 字段、枚举或路径漂移应显式修复，不得由前端推测转换后当作一致。
- 列表数据增长后必须提供服务端分页，并在 `meta` 返回分页信息。
- 实现差距应在实现项目的验收记录中跟踪，不能写回本合同成为允许行为。

## 稳定错误码

| HTTP | `error_code`                                                                                                                                                                                                                                                                                          |
| ---- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 400  | `invalid_request`、`invalid_install_request`、`database_unavailable`、`redis_unavailable`                                                                                                                                                                                                             |
| 401  | `invalid_credentials`、`unauthorized`                                                                                                                                                                                                                                                                 |
| 403  | `forbidden`                                                                                                                                                                                                                                                                                           |
| 404  | `not_found`                                                                                                                                                                                                                                                                                           |
| 405  | `method_not_allowed`                                                                                                                                                                                                                                                                                  |
| 409  | `setup_completed`、`admin_creation_failed`、`idempotency_key_conflict`、`invalid_state_transition`、`provider_device_conflict`、`device_disabled`、`device_deleted`、`device_not_online`、`provider_not_configured`、`provider_action_unsupported`、`provider_mapping_unknown`、`command_not_cancellable`、`webhook_delivery_not_dead`、`webhook_not_configured` |
| 422  | `unsupported_capability`、`invalid_capability_payload`                                                                                                                                                                                                                                                |
| 429  | `rate_limited`                                                                                                                                                                                                                                                                                        |
| 503  | `setup_required`、`setup_recovery_required`、`setup_restart_required`、`auth_dependency_unavailable`、`provider_callback_unverified`                                                                                                                                                                  |
| 500  | `internal_error`、`migration_failed`、`config_write_failed`、`install_lock_failed`、`install_recovery_failed`、`secret_generation_failed`、`install_target_not_writable`                                                                                                                              |

安装的外部依赖、migration、配置写入和 secret 生成失败分别使用前文定义的稳定 setup error code；未分类服务端错误只返回 `internal_error` 和 request ID，不泄露内部细节。HTTP status 表示 API 处理结果；异步 Provider/设备结果只通过 Command status、reason code、Attempt 和 Event 表达，不能把 `provider_rejected` 或 `provider_response_invalid` 混作创建请求的同步 API error，也不能用 HTTP 2xx 暗示设备成功。
