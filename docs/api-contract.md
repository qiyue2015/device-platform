---
title: API 与生命周期合同
created: 2026-05-16
updated: 2026-07-31
status: frozen-for-implementation
freeze_revision: 2026-07-31.2
---

# API 与生命周期合同

本文定义服务当前真实目标所需的接口、认证和技术生命周期语义，从属于[平台边界合同](./platform-boundary-contract.md)、[当前目标合同](./platform-target-contract.md)与[领域模型合同](./domain-model-contract.md)。[当前实现状态](./current-state.md)记录代码是否已经满足本文；实现缺口不能降低合同语义。

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
- `code` 保留兼容性的数值结果码；成功为 `0`。
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

Device 的 `connection_status` 只能是 `unknown`、`online`、`offline`；`lifecycle_status` 只能是 `active`、`disabled`、`deleted`。只有对应 Provider 合同允许并通过验证的上报才能改变 connection status，普通 Project、Device 更新请求和当前 simulator outcome 配置都不能直接写它。WWTIOT callback 仍失败关闭期间，WWTIOT Device 的 connection status 保持 `unknown`。

## 首次安装

安装 namespace 只有以下端点，且必须由部署网络限制为本机或受控管理网访问：

```http
GET  /setup/status
POST /setup/test-db
POST /setup/test-redis
POST /setup/install
```

`GET /setup/status` 只返回 `needs_setup`、`installed` 和 `step="system"`，不返回路径、连接串或 secret。其余三个端点只在未安装时可用；安装完成后统一返回 `409 setup_completed`。服务端以进程锁加持久完成标记串行化 install，两个并发请求至多一个成功。

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

所有字段必需；拒绝未知字段。email trim 后转小写并按 email address 校验，最大 254 字符；display name trim 后长度 `2..80`；password UTF-8 byte length `8..128` 且必须与 confirm 相同；server address 是 `:port` 或 `host:port`，log level 只能是 `debug|info|warn|error`。安装只允许空 users 表，创建唯一管理员，生成至少 32 byte 的 JWT secret 和 32 byte 的 Webhook secret encryption key，并以原子替换、权限 `0600` 的运行配置保存；响应只返回 `installed=true`。

数据库 migration 必须可重复执行。完成标记最后写入；在此之前任一步失败时，不写完成标记，回滚本次管理员和运行配置变更，已提交 migration 可由下一次 install 安全重入。安装请求中的连接串、密码和生成的 secret 不进入普通日志、Event 或 Audit。

Setup 失败码固定为：参数/schema 不合法 `400 invalid_install_request`；数据库或 Redis 连接失败分别为 `400 database_unavailable`、`400 redis_unavailable`；users 表不为空或管理员创建冲突为 `409 admin_creation_failed`；migration、运行配置、完成标记或随机源失败分别为 `500 migration_failed`、`500 config_write_failed`、`500 install_lock_failed`、`500 secret_generation_failed`。安装目标不可写为 `500 install_target_not_writable`。响应 message 必须脱敏，不包含连接串、SQL、文件内容或 secret。

## 单管理员认证

- `POST /v1/auth/login` 接受 `email` 与 `password`，只认证 setup 创建的唯一管理员。密码使用 bcrypt hash 持久化，不进入环境文件或日志。
- 成功返回 `access_token`、`token_type="Bearer"`、整数 `expires_in=86400`。token 使用 HS256、至少 32 byte 的部署 `JWT_SECRET`、24 小时期限，并固定包含 `iss="device-platform"`、`aud="device-platform-admin"`、管理员 ID `sub`、`session_generation`、`iat`、`exp` 和随机 `jti`；验证必须同时检查算法、issuer、audience、时间和 generation。
- `POST /v1/auth/refresh` 只接受尚未过期且 generation 有效的 Bearer token，并签发新的 24 小时 token；它不是无期限 refresh credential。
- `POST /v1/auth/logout` 需要 Bearer token，并原子递增管理员 `session_generation`，使此前签发的全部 token 失效。单管理员模型不提供按设备选择 session 的产品能力。
- 每次受保护请求必须从数据库确认管理员仍有效且 generation 匹配；仅验证 JWT 签名不足以支持 logout 失效语义。
- login 成功/失败、refresh 和 logout 写入安全审计。对外错误统一为 `invalid_credentials` 或 `unauthorized`，不泄露账号是否存在。
- login 失败按规范化 email + 认证层确认的 client IP 限制为 15 分钟内 5 次，并按 client IP 限制为 15 分钟内 20 次；超过后返回 `429 rate_limited` 与整数 `Retry-After`。计数必须跨进程重启保存，成功登录只清除对应 email + IP 计数，不清除 IP 总量计数。
- 限流状态存储不可用时 login 返回 `503 auth_dependency_unavailable`，不能绕过限流继续认证；已登录 API 的 JWT/session generation 数据库校验也失败关闭。

首次安装完成后，除 `GET /setup/status` 外的 `/setup/...` endpoint 返回 `409 setup_completed`。安装事务只创建一个管理员；当前不提供管理员、员工、角色或权限 CRUD。

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

当前实现是否满足 API Key、IP whitelist 与 Project 隔离要求，以 [Current State](./current-state.md) 的逐项证据为准；任何未满足项都不是本合同允许的行为。

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

| List resource             | 排序                                      |
| ------------------------- | ----------------------------------------- |
| Project                   | `created_at DESC, id DESC`                |
| Device Type               | `code ASC`                                |
| Provider                  | `code ASC`                                |
| Device                    | `created_at DESC, id DESC`                |
| Command                   | `created_at DESC, id DESC`                |
| Event                     | `occurred_at DESC, event_id DESC`         |
| Webhook Delivery          | `created_at DESC, id DESC`                |
| Audit                     | `occurred_at DESC, id DESC`               |

Admin 与 Open API 的同类资源使用相同排序。Device Type 与 Provider registry 也接受通用 `page`、`page_size`，除此之外不接受过滤或排序 query；调用方不能改变排序字段或方向。

Open API 的 Project scope 始终来自认证，不接受 `project_id` query。`GET /v1/open/projects/{project_id}` 的 path ID 必须等于认证得到的 Project ID；不相等时按不可见资源返回 `404 not_found`。枚举过滤值必须属于对应稳定枚举；UUID 过滤值必须合法，否则返回 `400`，不能静默产生空列表。

Provider callback namespace 是 `POST /v1/provider-callbacks/{provider_code}`，不使用管理员或 Project 凭据，而由对应 Provider adapter 按厂商合同验证 UserID、签名、schema 与防重放条件。当前 `wwtiot` 路径固定返回 `503 provider_callback_unverified`，不读取或保存 body，也不更新 Device、Command、RawMessage 或 Event；其他未注册 provider code 返回 `404 not_found`。WWTIOT 只实现不挂接公开入口的无副作用 decoder/validator 组件与契约测试，签名顺序确认并重新冻结前不得改变 503 行为。

### Simulator 配置

`GET /v1/simulator` 返回当前部署级 simulator Provider 配置；`PATCH /v1/simulator` 接受且只接受：

```text
outcome:  required; provider_accepted | provider_rejected |
          transport_error_before_send | transport_error_after_send |
          invalid_response
delay_ms: required integer; 0..60000
```

配置变更影响变更提交后新领取的 simulator Attempt，不改写已创建或已领取 Attempt。simulator Device 创建的 Command 必须经过与 WWTIOT 相同的 dispatcher、持久 CommandAttempt、Command 状态机、Event、Outbox/Webhook 和 Audit 链路；simulator 只替换 Provider adapter 的受控结果，不拥有平行 Command map 或状态机。

| simulator `outcome`           | Attempt confirmation/evidence    | Command 结果                                  |
| ----------------------------- | -------------------------------- | --------------------------------------------- |
| `provider_accepted`           | `provider_accepted` / `verified` | 保持 `sent`，观察期限后 `timeout`             |
| `provider_rejected`           | `transport_sent` / `verified`    | `failed`，reason `provider_rejected`          |
| `transport_error_before_send` | `none` / `none`                  | `failed`，reason `provider_transport_error`   |
| `transport_error_after_send`  | `transport_sent` / `verified`    | `unknown`，reason `provider_delivery_unknown` |
| `invalid_response`            | `transport_sent` / `verified`    | `unknown`，reason `provider_response_invalid` |

`delay_ms` 不延迟 durable Attempt 的创建。`transport_error_before_send` 在 delay 后返回且不记录模拟写出；其他模式先记录受控模拟写出，再等待 delay。若 dispatcher 的 Provider request timeout 在 delay 期间到达，结果按 `transport_error_after_send` 处理，不再应用配置的 outcome。simulator 的 `verified` 只证明平台按受控配置观察到模拟结果；Provider code 与 Event source 必须保留 `simulator`，它不证明真实 WWTIOT 或设备行为。simulator 不提供 `device_acked`、`device_succeeded` 或其他 Device final 模式；因此模拟结果不能证明真实设备收到或执行命令。

## 资源 DTO

### Project

创建请求允许 `name`、可选 `webhook_url` 和 `ip_whitelist`；名称 trim 后长度 `1..120`。IP whitelist 每项必须是合法单 IP 或 CIDR，规范化并去重。非本地 Webhook URL 必须是 HTTPS，不允许 embedded credential 或 fragment。

Project 常规读取返回 `id`、`name`、`webhook_url`、`webhook_configured`、`ip_whitelist`、`created_at`、`updated_at`，绝不返回 API key hash、Webhook secret 或 secret hash。创建响应额外一次性返回 `api_key`；设置首个 Webhook endpoint 或显式轮换时额外一次性返回 `webhook_secret`。Webhook secret 固定为 `whsec_` 加 32 个随机 byte 的无 padding base64url 编码，完整 UTF-8 字符串作为 HMAC key。PATCH 只允许更新 `name`、`webhook_url` 与完整替换的 `ip_whitelist`。

`webhook_url` 首次从 `null` 设为 URL 时生成 secret；改变非空 URL 继续使用当前 secret，除非调用 rotate。设为 `null` 只停止为后续 Event 创建 Delivery，既有 Delivery 继续使用其配置 snapshot；重新启用时复用当前 secret 且不返回明文。`webhook-secret/rotate` 只在 endpoint 非空时允许，生成新 version；新 Event 使用新 version，既有 Delivery 保持旧 version。API key rotate 与 Webhook secret rotate 均在新凭据提交后立即使旧凭据不再用于新请求/新 Delivery。

### Device Type 与 Provider

Device Type 读取返回 `code`、`revision`、`name` 和 actions；每个 action 至少包含 identifier、payload schema、risk level、delivery policy、dispatch deadline、provider request timeout、result observation timeout 与 retry/override flags。

Provider 读取返回 `code`、`name`、`access_type`、`transport_protocol`、`adapter` 和 `integration_status=unconfigured|configured_unverified|verified`，不返回 endpoint、UserID、UserKey 或签名材料。当前 registry 固定为：

| code        | name        | access_type | transport_protocol | adapter            |
| ----------- | ----------- | ----------- | ------------------ | ------------------ |
| `wwtiot`    | `WWTIOT`    | `cloud_api` | `http`             | `wwtiot_cloud_api` |
| `simulator` | `Simulator` | `simulator` | `internal`         | `simulator`        |

WWTIOT 必需配置缺失或格式无效时为 `unconfigured`；配置完整但尚未取得已验证响应真实性的受控真实环境证据时为 `configured_unverified`。当前冻结合同没有定义保存或提升 WWTIOT `verified` 的权威状态，因此运行时在重新冻结该证据机制前最多返回 `configured_unverified`；真实联调记录本身不能通过隐式进程状态提升它。Simulator 是进程内受控 adapter，注册且配置可读写时为 `verified`。Provider `verified` 只评价 adapter 接入，不代表任何 Device 已执行命令或达到 Device final result。

### Device

创建请求允许：

```text
project_id:         required for admin API
name:               required, trimmed length 1..120
device_type_code:   required; current value smart-lock
provider_code:      required; current value wwtiot or simulator
provider_device_id: required for wwtiot; forbidden for simulator
```

WWTIOT `provider_device_id` trim 后必须匹配平台约束 `[A-Za-z0-9._:-]{1,128}`；这是输入安全边界，不是对厂商 ID 格式的推断。simulator Device 的 `provider_device_id` 由平台固定为新建 Device UUID；它不是厂商标识。当前没有允许由调用方写入的 Device metadata key，因此创建和更新请求都拒绝 `metadata`。`access_type`、`transport_protocol` 和 `adapter` 由 Provider registry 推导；调用方不能写。新建 Device 固定以 `lifecycle_status=active`、`connection_status=unknown`、`current_state=null`、`last_seen_at=null` 开始；simulator 也不能仅因注册成功伪造连接或上报事实。PATCH 只允许更新 `name` 和 `lifecycle_status=active|disabled|deleted`；不允许改变 Project、Device Type 或 Provider identity。

Device lifecycle 只允许 `active -> disabled|deleted` 与 `disabled -> active|deleted`。`deleted` 不可恢复、不可更新 name、不可接收新 Command，但 Admin 与所属 Project 的历史读取和审计关联仍保留，列表默认也不隐藏它。active/disabled Device 才能改名；同一 PATCH 同时提供 `name` 与目标 `lifecycle_status=deleted` 返回 `400 invalid_request`。每次 lifecycle 变化写 Audit 与 `device.lifecycle_changed` Event，其固定 `reason_code=admin_requested`；当前没有调用方可写的 lifecycle reason 字段。名称变化只写 `device.updated` Audit，不生成未冻结的 Event 或 Outbox；未发生值变化的 name-only PATCH 仍成功返回当前资源，但不写 Audit。

Device 注册允许选择当前 `integration_status=unconfigured` 的已注册 Provider；这只建立技术资源，不允许创建 Command，后者仍返回 `409 provider_not_configured`。未知 `project_id`、Device Type 或 Provider code 返回 `404 not_found`；非法/缺失/被禁止的字段及非法 Provider device ID 返回 `400 invalid_request`；非 `deleted` Provider identity 冲突返回 `409 provider_device_conflict`，identity 只在 Device 进入 `deleted` 后释放。

Device 读取至少返回上述稳定 identity、派生接入字段、connection/lifecycle status、只读 `current_state`、`last_seen_at` 与时间戳。`current_state` 没有可信上报时为 `null`，不能用创建请求 metadata 伪造。

### Command

Open API 创建请求字段见下文；管理员 API 额外要求 `project_id`。Command 读取至少返回：

```text
id, project_id, device_id, command_type, payload,
device_type_revision, delivery_policy, status, reason_code, reason_detail,
confirmation_level, evidence_status, idempotency_key, queued_at,
dispatch_deadline_at, sent_at, result_deadline_at,
finished_at, created_at, updated_at
```

详情额外返回按 `attempt_no ASC` 排序的 Attempts 和按 `occurred_at ASC, event_id ASC` 排序的相关 Events，使单条 Command 的历史按时间正序且同时间结果稳定。Attempt 至少返回 `attempt_no`、`phase`、provider/adapter、provider request key、开始/完成时间、outcome、confirmation level、evidence status、错误与脱敏摘要；`phase` 遵循领域合同的 `claimed|dispatching|completed`。列表默认不嵌入完整 request/response 摘要。Attempt 的请求/响应摘要必须经过 adapter 字段 allowlist 与脱敏，不可只靠通用 key 名黑名单。

### Event、Webhook 与 Audit

Event 是只读资源，v1 envelope 固定包含：

```text
event_id, schema_version=1, event_type, project_id,
device_id|null, command_id|null, occurred_at, source, payload
```

当前稳定 Event 类型与 v1 payload 为：

| `event_type`                | 必需关联        | `payload` v1 必需字段                                                |
| --------------------------- | --------------- | -------------------------------------------------------------------- |
| `device.created`            | Device          | `device_type_code`、`provider_code`、`lifecycle_status`              |
| `device.lifecycle_changed`  | Device          | `from`、`to`、`reason_code`                                          |
| `device.connection_changed` | Device          | `from`、`to`、`evidence_status`                                      |
| `device.state_updated`      | Device          | `state`、`observed_at`、`evidence_status`                            |
| `command.created`           | Device、Command | `command_type`、`delivery_policy`、`status`                          |
| `command.status_changed`    | Device、Command | `from`、`to`、`reason_code`、`confirmation_level`、`evidence_status` |

`source` 只能是 `admin`、`open_api`、`provider_callback`、`simulator` 或 `system`。不适用的 `reason_code` 使用 `null`，不能用空字符串代替。Event payload 可以在同一 schema version 内增加调用方必须忽略的可选字段，但不能删除、改名或改变上述字段语义；破坏性变化必须提升 schema version。

Webhook Delivery 详情返回 `id`、`event_id`、`project_id`、target snapshot、配置 version、`status`、attempt count、next attempt time、`replay_of_delivery_id`、时间戳和按 attempt number 排序的独立 DeliveryAttempts；不返回 secret 或可重放签名。每个 DeliveryAttempt 返回 attempt number、开始/结束时间、HTTP status、截断脱敏 response 摘要和 error。

Audit 读取固定返回 `id`、`actor_type`、`actor_id`、`project_id`、`action`、`result`、`resource_type`、`resource_id`、`ip_address`、`request_id`、脱敏 `metadata` 和 `occurred_at`。`actor_type` 只能是 `admin`、`project`、`provider` 或 `system`；`result` 只能是 `success` 或 `failure`。不适用的关联字段使用 `null`。

当前稳定 Audit action 为 `setup.completed`、`auth.login`、`auth.refresh`、`auth.logout`、`project.created`、`project.updated`、`project.api_key_rotated`、`project.webhook_secret_rotated`、`project.webhook_secret_decryption_failed`、`device.created`、`device.updated`、`device.lifecycle_changed`、`command.created`、`command.cancelled`、`provider.callback_rejected`、`webhook.delivery_replayed` 和 `simulator.updated`。`project.webhook_secret_decryption_failed` 固定使用 `actor_type=system`、`result=failure`，关联 Project，metadata 只含 `webhook_secret_version`、`encryption_key_version` 与固定 `error_code=secret_decryption_failed`；不得保存异常原文、ciphertext、nonce 或 key。`provider.callback_rejected` 固定使用 `actor_type=provider`、`actor_id=provider_code`、`result=failure`，metadata 只含 `provider_code` 与 decoder/validator 稳定 `error_code`，不能保存 callback body 或 sign；无法映射时 Project/Device 关联为 `null`。当前公开 WWTIOT callback 固定 503 且不读取 body，因此在该入口重新冻结并启用前不产生此审计。worker 的普通状态迁移只写 Attempt/Event，不重复写 Audit；新增人工或安全操作必须先增加稳定 action。

## 四层 API 责任

本合同落实[平台边界合同](./platform-boundary-contract.md)的四层分工：

| 层                       | API 与生命周期责任                                                                                                                                         |
| ------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Platform Core            | 接收通用 Command envelope，校验 Project/Device 归属，执行幂等、通用投递、状态迁移、Attempt、timeout、cancel 和 Event；不解释具体 action 的设备或业务含义。 |
| Device Type Capability   | 为 action identifier 提供语义、payload schema、风险等级，以及经真实证据确认的在线要求、重放/补偿规则、timeout 和 delivery policy。                         |
| Gateway/Provider Adapter | 把 action 与 payload 映射为厂商或设备协议请求，并将 Provider acceptance、Device ACK、Device final result 分层映射为通用 Attempt/Command 事实。             |
| Business Application     | 决定为何发起 action、业务用户是否有权发起，以及最终技术结果如何改变订单、计费等业务状态；这些判断不进入本 API 的设备核心模型。                             |

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

当前 revision 1 的三个 action 均把缺失 payload 规范化为空 object，并拒绝调用方提供 `delivery_policy`、`expires_at`、deadline、retry 或 Provider 字段。Command 持久保存创建时采用的 `device_type_revision` 和派生 `delivery_policy`；后续 profile 发布不能静默改变已创建 Command。

创建前同步校验 Project、Device 归属、Device lifecycle、Device Type profile、action/payload、Provider 注册与配置。`device_disabled` 或 `provider_not_configured` 返回 `409`，不创建 Command；不支持的 capability 或 payload 返回 `422`，不创建 Command。Command 创建提交后才由持久 dispatcher 异步投递，因此创建响应不等待 Provider HTTP。

当前 wire field 继续使用 `command_type`，但它的合同含义是设备 capability/action identifier，不是 Core 全局固定命令枚举。具体允许值及 payload 由 Device 的最小 capability profile 决定。未来若重命名字段，应作为显式 API 变更处理，不能同时维护两套语义来源。

### 幂等

幂等范围：

```text
project_id + idempotency_key
```

请求 hash：

```text
sha256(canonical_json(device_id, command_type, normalized_payload))
```

服务器完成 capability payload 规范化后计算 hash。相同 key 和相同 hash 返回既有 Command；相同 key 但任何影响执行的规范化字段不同，返回 `409 idempotency_key_conflict`。幂等记录必须持久化并受唯一约束保护，不能只依赖单进程内存。

canonical JSON 使用 UTF-8、对象 key 词典序、无无意义空白，并在 capability 校验后使用规范化 scalar 与 UTC RFC3339 时间。新建 Command 返回 `201`；幂等重放返回 `200` 并携带 `meta.idempotent_replay=true`。两种响应都只表示平台接受了 API 请求，不表示 Provider 或设备成功。

## Command 与 Attempt

Command 状态：

```text
queued
sent
acked
success
failed
timeout
cancelled
unknown
```

- Command 创建事务直接提交为 `queued`；不存在可被 API、worker 或数据库提交后观察到的 `created` 状态。
- `sent` 表示已向 Gateway/Provider 发起投递。
- `acked` 专指已有可信的 Device ACK，表示设备已收到并确认命令，但尚不等于最终执行成功。
- `success` 表示已有可信的 Device 最终执行成功证据。
- transport 或 Provider 返回的“请求已接受/命令已提交”只能记录为 Command Attempt 的 Provider acceptance；它既不能把 Command 推进到 `acked`，也不能直接推导 Device `success`。在设备 ACK 或最终结果到达前，Command 保持 `sent` 并受 timeout 管理。
- `unknown` 表示投递结果存在不可消除的歧义，例如请求可能已到达 Provider 但响应在持久化前丢失。它不是 `failed`，也不得自动重发物理动作。
- `failed`、`timeout`、`cancelled` 和 `unknown` 必须保留 `reason_code`；诊断细节放入脱敏 `reason_detail`。

每次实际投递形成 Command Attempt，至少记录 phase、provider/gateway、开始与结束时间、`outcome`、脱敏请求/响应摘要、Provider request key、错误、`confirmation_level` 和 `evidence_status`。confirmation level 只能为 `none`、`transport_sent`、`provider_accepted`、`device_acked`、`device_final`，并只能单调提升。Attempt 的 evidence status 为 `none|verified|unverified`，评价该 outcome 所依赖证据；Command 的同名字段保守继承支撑当前 status 与最高 confirmation 的证据。只有 `device_final + verified` 才能支持 Command `success`。敏感凭据不得进入 Attempt、日志、Event 或 API 响应。

当前稳定 Command `reason_code` 为：

| `reason_code`                | 允许的 Command status | 条件                                                            |
| ---------------------------- | --------------------- | --------------------------------------------------------------- |
| `cancelled_by_request`       | `cancelled`           | 调用方在允许状态取消                                            |
| `provider_not_configured`    | `failed`              | 创建后配置漂移，请求构造前失败                                  |
| `provider_transport_error`   | `failed`              | 能证明请求未发送                                                |
| `provider_rejected`          | `failed`              | Provider 明确拒绝；当前 WWTIOT 响应证据仍标 `unverified`        |
| `device_reported_failure`    | `failed`              | 可信 Device final failure                                       |
| `provider_response_invalid`  | `unknown`             | 请求已发送但 HTTP status、body 或关键 echo 不符合 Provider 合同 |
| `provider_delivery_unknown`  | `unknown`             | 请求可能送达但无完整结果，或外部调用后落库前崩溃                |
| `dispatch_deadline_exceeded` | `timeout`             | Command 在进入 `sent` 前超过服务端派发期限                      |
| `result_observation_timeout` | `timeout`             | `sent`/`acked` 后未在 profile 观察期限内取得可信 final result   |

非终止状态的 `reason_code` 为 `null`。终止状态必须使用上表中与状态匹配的值；新增 reason 必须先修订合同，`reason_detail` 不能代替机器码。

### 状态迁移

当前通用生命周期只允许以下迁移：

```text
queued -> sent
queued -> cancelled
queued -> failed
queued -> timeout
sent -> acked
sent -> success
sent -> failed
sent -> timeout
sent -> unknown
acked -> success
acked -> failed
acked -> timeout
```

状态更新必须使用数据库条件更新或事务内等价的原子数据库语句。重复 worker、重复回执、晚到回执和重复事件不能产生重复最终效果。状态、Attempt、Event，以及配置 endpoint 时的初始 Delivery 必须在同一数据库事务中保存；不得用提交后的补偿流程代替该事务边界。

状态终止语义：

- `success`、`failed`、`cancelled`、`timeout` 和 `unknown` 是自动处理的终止状态，不再接受普通状态迁移。
- `queued`、`sent`、`acked` 是处理中的非终止状态。
- `timeout` 或 `unknown` 默认不得推导或补偿为 `success`。只有厂商能力、设备行为和受控验收共同证明某个 action 存在可信、无歧义的晚到最终结果，且 capability profile 明确规定处理方式时，才可执行受条件更新保护的纠正迁移，并保留原状态历史与 correction Event。
- Provider acceptance 是 Command Attempt 事实，不是独立 Command 状态，也不改变上述终止语义。
- 只有没有有效 dispatcher lease 的 `queued` Command 可以由调用方取消；取消时若已有过期 claimed Attempt，将其完成为 `not_dispatched`。有有效 lease 或已经进入 `sent` 时返回 `409 command_not_cancellable`，不得以取消响应暗示设备动作被撤回。

## Capability Profile 与 Delivery Policy

Platform Core 只执行 capability profile 提供的通用 metadata，不固定某个设备 action 的 timeout、离线策略、重试次数、补偿窗口或默认 delivery policy。Core 的判断依据是 metadata，而不是具体 action identifier。当前 profile 使用的保守策略见 [smart-lock Device Type 合同](./device-types/smart-lock.md)。

- 物理动作及其他高风险 action 没有可信 Device final result 时，不得重放或补偿为 `success`。
- 厂商或设备自身的 timeout、离线行为、重试安全性和补偿语义在证据到齐前保持 Unknown。平台为防止永久悬挂采用的保守观察期限与禁重放策略由具体 Device Type profile 明确定义，不得表述为厂商事实。
- 具体 Device Type 的 action 与规范化语义只在 Device Type Capability 合同中定义；Provider 当前映射与厂商事实只在 Provider 合同中定义。二者都不得扩大本合同的 Core 语义。

## 派发期限与结果观察

- timeout 从 Command 进入 `sent` 开始计算。
- 当前 Command 创建后 30 秒内未进入 `sent` 时转为 `timeout`，reason 为 `dispatch_deadline_exceeded`；进入 `sent` 后按 profile 的结果观察期限处理。当前 profile 不提供离线排队、恢复投递、自动重试或晚到补偿。
- 无论采用何种实现，Command 都不能永久悬挂；处理进度必须持久化并能在进程重启后恢复，状态变化产生的 Event 不能丢失。
- capability profile 标记的物理动作没有可信最终证据时不得在离线恢复或 timeout 后重放，也不得补偿为 `success`。
- 若未来 profile 允许处理晚到结果，必须保留原状态历史、记录新的技术 Event，并准确标注 confirmation level；不得覆盖或伪造原结果。

自动网络重试不是默认合同。只有 Provider 证明请求可安全重放、幂等边界明确，并经过受控验收后，才能为具体 action 配置重试。

dispatcher、deadline scanner、callback handler 与 webhook dispatcher 的领取、事务和崩溃恢复责任以[领域模型合同](./domain-model-contract.md)为准。外部 HTTP 不得发生在持有领域数据库事务期间。

## Event、Outbox 与 Webhook

规范化 Event 与 Outbox/Delivery 记录必须可靠持久化：

1. 状态变化产生 Event。
2. Event、相关状态，以及 Project 当时配置了 endpoint 时的初始 Delivery，必须在同一数据库事务中提交；未配置 endpoint 时只提交 Event。不得提交后补建初始 Delivery。
3. worker 领取待投递记录，发送签名 Webhook，并为每次实际 HTTP 请求追加独立的 Webhook Delivery Attempt。
4. 成功转为 `delivered`；失败在 Delivery 与 Attempt 中记录次数、错误与下次重试时间。
5. 首次投递立即执行，失败后按 `1s, 5s, 30s, 2m` 调度，默认最多 5 次实际 HTTP Attempt；该值可由部署配置降低或延长间隔，但不能超过 5 次而无需重新修订合同。耗尽后转为 `dead`。
6. 管理员可对 `dead` 记录执行受审计的手动重发；重发要求 Project 当前有启用的 endpoint，使用重发时的当前 target/secret version 创建新 Delivery，并通过 `replay_of_delivery_id` 指向原记录。原 Delivery 的 snapshot、状态和 Attempt 历史保持不变；当前 endpoint 未配置时返回 `409 webhook_not_configured`。

Webhook Delivery 状态：

```text
pending
sending
delivered
failed
dead
```

状态迁移固定为 `pending -> sending -> delivered|failed`、`failed -> sending|dead`；`sending` lease 过期时先完成本次 DeliveryAttempt 为失败，再进入 `failed` 或在第 5 次后进入 `dead`。`delivered` 与 `dead` 是终止状态，manual resend 只创建新的 `pending` Delivery，不改变原状态。Attempt count 在进入 `sending` 的领取事务中递增，因此崩溃窗口也占用一次上限并允许 at-least-once 重试。

Webhook raw body 是对应 Event 的完整 v1 JSON envelope，不另造第二套 payload；每个 Delivery 创建时序列化并持久保存一次，同一 Delivery 的自动重试逐 byte 复用该 body。每次实际 Attempt 生成当时的 Unix 秒 `timestamp`，签名固定为 `sha256=<hex(HMAC-SHA256(webhook_secret, timestamp + "." + raw_body))>`，请求携带 `X-Device-Platform-Timestamp`、`X-Device-Platform-Signature` 和 `X-Device-Platform-Event-ID`；接收方按部署约定校验时间偏差并按 `event_id` 幂等消费。任意 2xx 为 delivered；其他 status、网络错误或 timeout 均进入失败调度。平台承诺至少一次投递，不承诺恰好一次。

非本地 Webhook endpoint 必须使用 HTTPS。dispatcher 不自动跟随 redirect，响应 body 最多保留 4 KiB 且按敏感字段规则脱敏；连接和响应 timeout 默认 10 秒。目标允许内部 HTTPS endpoint，但 dispatcher 必须按部署级 egress allowlist 校验解析后的每个目标地址并在连接时防止 DNS rebinding；默认拒绝 loopback、link-local、multicast 和云 metadata 地址，只有明确受控的部署 allowlist 才能开放所需内部网段。Project 请求参数不能绕过该策略。

Command Attempt 与 Webhook Delivery Attempt 是两类独立技术记录，不能共享状态、计数或术语归属。前者描述设备命令派发，后者描述事件回调的单次 HTTP 投递。

## 技术审计

以下行为至少应写入不可由普通业务调用覆盖的技术审计：管理员登录与安全变更、Project 凭据创建/轮换、Device 创建与生命周期变更、Command 创建/取消、Webhook 端点与 secret 变更、dead delivery 重发、Provider 配置或模拟模式变更。

审计记录应包含 actor 类型与标识、Project/资源、动作、结果、来源 IP、request_id、发生时间和脱敏 metadata。技术审计不承载共享单车业务审计。

## 实现一致性要求

- API DTO、数据库约束、领域状态机、Provider 映射和前端展示必须共享同一组合同语义。
- 字段、枚举或路径漂移应显式修复，不得由前端推测转换后当作一致。
- 列表数据增长后必须提供服务端分页，并在 `meta` 返回分页信息。
- 当前未满足项统一记录在 [Current State](./current-state.md)，不在本文中把错误现状改写为允许行为。

## 稳定错误码

| HTTP | `error_code`                                                                                                                                                                                                                                            |
| ---- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 400  | `invalid_request`、`invalid_install_request`、`database_unavailable`、`redis_unavailable`                                                                                                                                                               |
| 401  | `invalid_credentials`、`unauthorized`                                                                                                                                                                                                                   |
| 403  | `forbidden`                                                                                                                                                                                                                                             |
| 404  | `not_found`                                                                                                                                                                                                                                             |
| 405  | `method_not_allowed`                                                                                                                                                                                                                                    |
| 409  | `setup_completed`、`admin_creation_failed`、`idempotency_key_conflict`、`invalid_state_transition`、`provider_device_conflict`、`device_disabled`、`device_deleted`、`provider_not_configured`、`command_not_cancellable`、`webhook_delivery_not_dead`、`webhook_not_configured` |
| 422  | `unsupported_capability`、`invalid_capability_payload`                                                                                                                                                                                                  |
| 429  | `rate_limited`                                                                                                                                                                                                                                          |
| 503  | `auth_dependency_unavailable`、`provider_callback_unverified`                                                                                                                                                                                           |
| 500  | `internal_error`、`migration_failed`、`config_write_failed`、`install_lock_failed`、`secret_generation_failed`、`install_target_not_writable`                                                                                                           |

安装的外部依赖、migration、配置写入和 secret 生成失败分别使用前文定义的稳定 setup error code；未分类服务端错误只返回 `internal_error` 和 request ID，不泄露内部细节。HTTP status 表示 API 处理结果；异步 Provider/设备结果只通过 Command status、reason code、Attempt 和 Event 表达，不能把 `provider_rejected` 或 `provider_response_invalid` 混作创建请求的同步 API error，也不能用 HTTP 2xx 暗示设备成功。
