---
title: 当前平台目标合同
updated: 2026-08-02
status: product-decision-required
contract_revision: 2026-08-02
---

# 当前平台目标合同

本文从属于[平台边界合同](./platform-boundary-contract.md)，唯一负责定义当前后端重建必须交付的真实链路、完成门槛和产品所有者裁决项。它不定义领域状态机、HTTP 字段或技术栈。

## 当前目标

建设一个可实际运行、可持久化、状态可信的 IoT 设备平台：共享单车应用通过 Project 机器身份使用统一主链接入 WWTIOT 厂商云智能锁与 Omni 直连智能锁；平台人类 User 通过 Web 在授权 Project 范围内管理同一批技术资源。

目标不是通用 IoT 产品愿景，也不是模拟器里程碑。只有本合同列出的真实链路和为其可靠运行必需的工程能力进入本次完成定义。

## 必须完成的真实链路

### 北向业务应用

共享单车应用作为一个 Project，使用 Project 机器凭据查询所属 Device、创建和查询 Command，并接收签名 Webhook。平台必须执行 Project 隔离、认证、幂等和稳定错误语义；共享单车用户、订单、计费和运营授权仍由业务应用负责。

Project 机器身份不代表任何人类 User，人类 User token 也不能作为 Open API 机器凭据。两类入口可以操作同一 Project 技术资源，但必须分别认证、授权和记录实际 actor。

### 南向真实设备

当前唯一 Device Type 为 [smart-lock](./device-types/smart-lock.md)。[WWTIOT](./providers/wwtiot.md) 与 [Omni](./providers/omni.md) 都是当前必须接入的 Provider，不是候选项、未来项或互相替代关系。所有命令经过统一 Platform Core、Device Type profile 与 Provider adapter：

```text
Shared-bicycle Project
  -> Open API
  -> durable Command / Attempt
  -> smart-lock capability
  -> WWTIOT cloud_api adapter | Omni direct-device adapter
  -> observable Provider or Device evidence
  -> durable Result / State / Event
  -> signed Webhook and technical Audit
```

厂商接受请求、socket 写入、厂商完成下发、设备收到命令和设备最终执行是不同事实。平台必须展示实际可证明的最高 confirmation level；Provider acceptance 或 socket 写入不能表示 Device ACK 或执行成功。两家 Provider 可以具有不同证据上限，但都必须服从[领域模型合同](./domain-model-contract.md)定义的同一状态机、事务、恢复和证据单调性。

### 一致性与恢复

User、Project 管理关系、Device、Command、Attempt、Result、DeviceState、RawMessage、Event、Webhook Delivery 和 Audit 的权威事实必须持久化。进程或消息基础设施重启后，未完成工作可恢复；重复请求、重复消息、乱序或迟到结果不能产生无管理人的 Project、越权访问、重复物理效果或不可变历史覆盖。

### 模拟器与人类管理端

模拟器必须通过同一持久命令主链，只替换 Provider adapter 的受控输出。它用于验证 Core，不提供真实 Device ACK/final evidence，也不替代真实设备验收。

Web 管理后台支持唯一超级管理员与多个 Project 范围普通 User，读取与 Open API、worker 相同的权威事实。超级管理员创建普通 User 与 Project、指定或转交 Project 管理关系并治理平台级安全能力；普通 User 维护分配给自己的 Project 基本信息、Device、Command 和技术记录。它们都不承担共享单车运营业务。

后续小程序的产品定位已确认为 Project 范围移动技术运维端：与 Web 共用 User 身份和 Project 授权，可面向已有 Device 提供搜索、扫码查找、可信连接/遥测查看、`active|disabled` 生命周期维护、Device Type profile 已支持的技术命令、命令证据与审计。技术 `online|offline|unknown` 只来自可信 Device/Provider 证据，不能由人手工修改；电量、位置、响铃、定位等能力只有从属合同确认后才能承诺。小程序不是共享单车业务端，不承载投放回收、租赁、订单、行程、计费或停车区。本次不创建小程序工程、不选择框架，也不增加小程序专用 API。

## 不进入本次完成定义

- Omni 资料所含但未进入 smart-lock 当前三项 action 的 BLE 控制、地图、通用位置、电子围栏、OTA、固件管理、规则引擎、批量操作、告警和长期遥测保留；
- 自行车、景区、用户、订单、行程、计费、停车、还车和业务权限；
- 多租户 SaaS、组织员工体系、Project 多成员/多负责人、复杂人类 RBAC、低代码平台或通用业务工作流；
- 已确认但延期的移动技术运维小程序实现。

这些能力只有在产品所有者基于真实需求修订本合同后才能进入实施。技术栈具备某种能力不构成产品承诺。

## 两个独立完成门槛

### Platform Core 技术实现完成

必须同时满足：

1. 唯一超级管理员、普通 User、Project 单一管理关系与集中授权按核心合同实现；Project 机器认证继续独立，普通 User 不能越权访问其他 Project，敏感平台操作必须在服务端拒绝。
2. `Command.status`、`CommandAttempt.outcome`、`CommandResult` 和 confirmation/evidence 维度没有混用；没有可信 final evidence 时不得产生 `success`。
3. 崩溃恢复、重复请求、重复/乱序消息和迟到结果不覆盖历史，不重复执行不可安全重放的物理动作。
4. WWTIOT 与 Omni adapter 合同测试分别覆盖各自可证明的较低证据等级、失败和恢复分支；模拟器通过同一主链覆盖通用分支，且所有模拟展示明确标记 `simulator`。
5. API、后台、worker 和持久化层读取同一权威模型；User 停用、Project 创建/转交、Audit actor、越权拒绝和既有核心合同均有自动化一致性与并发测试。

WWTIOT callback、Omni 上行真实性、final result 和实机行为仍为 Unknown 时，可以完成 WWTIOT 截至 `provider_accepted -> timeout`、Omni 截至 `transport_sent -> timeout` 的诚实 Core 实现；不能启用未经验证的结果推进、提升证据等级或声明真实业务通过。Omni 的两个明确协议 profile 可以各自实现和测试，但具体 Device 未明确绑定 profile 时必须失败关闭。

### 当前真实业务验收通过

必须同时满足：

1. 受控共享单车 Project 通过机器身份完成查询和命令调用，隔离、认证、幂等和 Webhook 可验证。
2. 至少一台受控 WWTIOT 智能锁和至少一台已证明协议 profile 的受控 Omni 智能锁，分别完成 [smart-lock 真实设备验收矩阵](./device-types/smart-lock.md#真实设备验收矩阵)，并记录 Provider、连接与设备侧可观测证据。
3. 平台状态、Event、Webhook、Audit 与实际最高证据等级一致；只有 `device_final + verified` 可产生 `success`。
4. 两家 Provider 的重启、断线、超时、拒绝、重复消息、不可关联消息、迟到结果和外部依赖故障按合同恢复，无重复物理效果、跨线归属或历史改写。
5. 目标部署通过 [Reliability and Capacity](./operations/reliability-capacity.md) 要求的容量、恢复、监控和故障演练。
6. 下表所有阻塞真实业务验收的 Unknown 已关闭，并完成所需产品裁决。

技术实现完成和真实业务验收通过不得互相替代；模拟器、伪 Provider、单元测试、页面展示或 HTTP 2xx 都不能替代真实设备证据。

## 已裁决的当前口径

- 安装只创建一个不可停用、不可降级的超级管理员；普通 User 只能由超级管理员创建。不存在公开注册、邀请注册、第二个超级管理员或超级管理员身份变更入口。
- Project 只能由超级管理员创建且必须有一个有效 `manager_user_id`；普通 User 可管理多个 Project。超级管理员的全局权限不依赖该字段，且可把 Project 转交给另一个有效 User。
- 普通 User 只能管理分配给自己的 Project、Device、Command 和技术记录。API Key、IP whitelist、Webhook endpoint/secret、凭据轮换、dead delivery 重发、模拟器和全局诊断只允许超级管理员操作。
- 停用普通 User 前必须先转交其管理的全部 Project；Device、Command、Result、Event 和 Audit 继续只归属 Project。所有人类写入 Audit 必须记录实际 `actor_user_id`。
- Device 进入 `deleted` 后仍永久占用 `(provider_code, provider_device_id)`；当前不允许任何 Provider 复用 identity。未来只有在引入可信 identity incarnation、迟到/重放隔离和显式数据迁移后，才能重新提出复用方案。
- Command 幂等重放优先查询 `(project_id, idempotency_key)` 对应的历史 Command。调用方的 `device_id`、规范化 action 和 payload 与历史冻结输入一致时返回原 Command，不重新执行当前 Device/Profile/Provider preflight，不创建 Attempt 或新物理动作；任一调用方输入不一致固定返回 `409 idempotency_key_conflict`。

## 需要产品所有者裁决

| 问题                                                                                                           | 候选口径                                                                                                                                                                            | 影响                                                                                           | 阻塞                                                         | 不阻塞                                                                              | 裁决所需证据                                                                                        |
| -------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- | ------------------------------------------------------------ | ----------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- |
| 当前受控 Omni 设备对应 `omni-bike-tcp-v2.0.7` 还是 `omni-iot-tcp-v1.3.5`，或两者都不匹配                       | 只能由设备型号/固件、厂商确认和受控握手报文证明；不得按品牌名、IMEI 外观或试错写命令猜测                                                                                            | 决定该 Device 的 frame、action 映射和关联字段；马蹄锁 TCP profile 不具备主动 `lock` 下行       | 具体 Omni Device 注册后的真实命令派发与真实业务验收          | 两个显式 profile 的 parser/encoder、adapter 合同测试、缺失 profile 的失败关闭       | 厂商可追溯设备/profile 对照、只读连接报文、固件/型号证据和受控设备矩阵                              |
| Omni TCP 仅以 IMEI/连接声明身份且资料未定义 TLS、签名、防重放时，什么证据可验证消息真实性                      | A. 厂商补充设备认证；B. 受控网络层提供可验证设备身份；C. 只保留 `unverified` 原始事实且不推进 Device ACK/final/State                                                                | 决定上行能否产生可信 DeviceState、CommandResult、`success` 或 `failed/device_reported_failure` | Omni 真实结果推进、真实设备成功/失败验收和生产安全验收       | 单次下行 transport 证据、脱敏 RawMessage、解析/关联候选和模拟主链                   | 正式安全合同、目标网络与设备身份方案、重放/冒充测试和产品风险批准                                   |
| WWTIOT 实际可提供的最高可信 confirmation level 是否足以支撑共享单车正式业务                                    | A. 必须有可信 `device_final`；B. 接受较低层级并由业务应用承担结果不确定性；C. 更换或补充 Provider/设备侧证据通道                                                                    | 决定 `timeout` 的业务处置、风险承担方及是否需要补充证据通道                                    | 当前真实业务验收                                             | Core 对证据分层、`provider_accepted -> timeout` 和不可变历史的实现                  | 厂商正式合同、受控设备矩阵、共享单车风险与处置责任评审                                              |
| `unlock`/`lock` 是否必须坚持 `online_only`                                                                     | A. 保持 `online_only`；B. 在明确风险与授权下允许 `dispatch_once`                                                                                                                    | 改变离线/未知状态下的可用性、误操作与重放风险                                                  | A 在可信在线来源可用前阻塞物理命令；B 需要新的风险验收       | 当前保守 `online_only` profile、查询链路和非物理 Core 能力                          | WWTIOT 在线事实来源、设备离线行为、重放/误操作风险、业务应急流程                                    |
| Command 入队后、派发前 Device 变为 `disabled` 或 `deleted` 时如何处理                                          | A. 继续派发既有 Command；B. 终止为 `cancelled`；C. 终止为 `failed` 并新增稳定 reason；D. 不派发并保持至 `timeout`                                                                   | 改变物理效果、Command/Attempt 终局、Event/Webhook wire 和调用方恢复逻辑                        | 该 lifecycle 竞态分支及对应自动化验收                        | Device 创建/禁用/删除、新 Command 同步拒绝、未发生 lifecycle 竞态的派发链路         | 业务对禁用/删除的撤销意图、已授权物理动作风险、审计和调用方处置评审；裁决后同步 Domain/API 测试矩阵 |
| result deadline 与 Provider response、可信 Device final 并发时如何仲裁，以及相互冲突的 verified final 如何关闭 | A. PostgreSQL 首次终态提交者获胜；B. 以平台可信 `observed_at` 是否早于 deadline 为准；C. deadline 绝对优先，未提交结果一律 late；D. final evidence 优先并定义冲突结果的固定关闭状态 | 改变 Command 终态、late 标记、Event/Webhook 和调用方业务决策                                   | 真实 ACK/final 的 deadline 边界、乱序和冲突结果验收          | 没有 final evidence 的 `provider_accepted -> timeout` 顺序链与不可变结果记录        | 产品截止语义、可信时间来源、冲突证据处置责任和覆盖边界相等/乱序/崩溃的并发测试矩阵                  |
| 首次安装在 migration、管理员、配置和完成事实之间崩溃时采用哪种恢复口径                                         | A. 数据库安装事务提交后只允许 roll-forward；B. 完成事实前补偿到可重新安装；C. 越过不可自动判断点后 fail-stop 并由受审计恢复操作关闭                                                 | 改变 `/setup/status`、`readyz`、重复 install wire 与残留副作用处置                             | setup 跨介质崩溃恢复和安装恢复验收                           | 全新环境无故障安装；已安装环境的普通运行                                            | 获批的部署恢复策略、跨介质 journal 状态机、每个崩溃点的重复安装与恢复测试                           |
| 未逐项冻结的 mutation 成功响应采用哪种 HTTP DTO                                                                | A. PATCH 与命令操作返回完整更新后资源；B. PATCH 返回资源、命令式端点返回固定 result DTO；C. 统一最小 operation result，凭据端点另含一次性 secret                                    | 改变成功响应 JSON、SDK 类型和凭据返回边界                                                      | OpenAPI/SDK 生成、前后端与业务调用方集成、mutation wire 验收 | 已冻结的只读 DTO、Command 创建/幂等重放和失败响应语义                               | 调用方兼容需求、逐端点 response schema/示例与契约测试                                               |
| 失败响应 envelope 的数值 `code` 采用什么兼容规则                                                               | A. 等于 HTTP status；B. 所有失败固定同一非零值；C. 为每个 `error_code` 分配稳定编号；D. 删除失败响应中的数值 `code`                                                                 | 改变完整 JSON wire、生成客户端和快照契约；错误分支仍由 HTTP status 与 `error_code` 承担        | 失败 `code` 的兼容承诺、依赖该字段的 SDK 和 wire 验收        | 现有 HTTP status、稳定 `error_code`、成功 `code=0` 和不依赖失败 `code` 的调用方实现 | 调用方兼容需求、历史消费者证据、逐错误响应 schema 和契约测试                                        |
| 核心数据和审计保留政策                                                                                         | A. 法规/业务期限后清理；B. 长期保留；C. 分对象期限                                                                                                                                  | 改变自动清理、归档、存储容量与合规责任                                                         | 生产保留、归档、容量和合规验收                               | 不自动清理的 Core 实现和逻辑删除                                                    | 法规、业务追溯、成本和安全要求                                                                      |
| 删除请求及备份、归档中历史记录如何处置                                                                         | A. 法定/业务删除后同步清理所有副本；B. 主库逻辑删除、备份按既定周期自然过期；C. 按对象与数据主体分别制定例外                                                                        | 改变删除完成时点、可恢复历史和合规证明                                                         | 面向生产的数据删除流程、备份恢复和合规验收                   | 逻辑删除、不可变历史、权限隔离和不自动清理的 Core                                   | 获批的数据主体/业务删除政策、法规依据、备份生命周期和恢复测试                                       |

在其余裁决完成前，实施必须采用现有保守合同：不自动重放，`unlock`/`lock` 保持 `online_only`，没有可信 final evidence 不产生 `success`，核心历史不自动清理；Omni Device 缺失显式 profile 时不得派发，未认证的 Omni 上行不得产生 Device ACK、device final 或可信 DeviceState。Device lifecycle、deadline/final 仲裁、setup 崩溃恢复、未冻结 mutation 响应和失败数值 `code` 没有可实施的默认分支；对应分支和验收保持阻塞，不得自行选择。以上临时约束防止扩大风险，不代表其余产品裁决已经完成。

## 目标变更

新增能力或改变完成门槛必须来自真实项目证据，先修订本文，再修订下级合同和实现。数据库字段、菜单、旧代码、测试或架构偏好不能静默改变目标。
