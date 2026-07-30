---
title: 当前平台目标合同
created: 2026-07-31
updated: 2026-07-31
status: frozen-for-implementation
freeze_revision: 2026-07-31.1
---

# 当前平台目标合同

本文回答：基于当前共享单车智能锁真实项目，设备平台做到什么才达到当前目标。本文从属于[平台边界合同](./platform-boundary-contract.md)，实施步骤只表示交付顺序，不构成产品版本或平台边界。

## 当前目标

建设一个可实际运行、可持久化、状态可信的 IoT 设备平台，使共享单车应用能够通过 Project 机器身份可靠接入和使用真实智能锁。

目标不是建设功能齐全的通用 IoT 平台，也不是完成一个模拟器里程碑。通用性只来自已确认的平台边界、统一接入方式，以及真实链路中被证明需要的抽象。

## 必须成立的真实链路

### 北向应用接入

- 共享单车应用作为一个 Project，通过受保护的机器凭据调用 Open API。
- Project 数据必须隔离；任何查询、命令和事件都不能越过 Project 边界。
- 每个 Device 只能属于一个 Project。
- API 幂等、错误和结果语义必须允许业务应用安全重试并作出确定判断。

### 真实 WWTIOT 智能锁接入

- 当前真实 Device Type 是 [smart-lock](./device-types/smart-lock.md)，当前真实 Provider 是 [WWTIOT](./providers/wwtiot.md)。两份从属合同分别承载规范化设备能力和厂商适配事实，本文不展开 action 清单或厂商字段。
- 平台通过统一 Provider 边界调用 WWTIOT，而不是把厂商细节扩散到核心命令域；Provider 接入必须服从本合同与通用 [API 合同](./api-contract.md)的 confirmation level 和生命周期语义。
- Platform Core 只处理通用 Command envelope 和生命周期；Device Type Capability profile 定义具体 action 的语义、payload 与经证据确认的安全 metadata；Provider adapter 负责厂商映射；共享单车应用负责动作授权和订单含义。
- 厂商 HTTP 请求被接受、厂商完成下发、设备收到命令、设备最终执行成功是不同事实。
- `success` 必须有可信的设备最终结果依据；Provider acceptance 不得表达为设备执行成功。
- 必须先通过厂商合同与受控设备测试确认 WWTIOT 实际可观测到的最高 confirmation level，并在 API、Event 和后台中如实展示该层级。
- 若 WWTIOT 不提供可信 Device final result，Provider acceptance 只进入 Attempt 的 `confirmation_level=provider_accepted`，Command 保持 `sent`，到观察期限后进入 `timeout`；请求是否送达无法判断时进入 `unknown`。不得自行推断 `acked` 或 `success`。
- 超时、厂商拒绝、网络失败、设备无回执和设备执行失败必须保留可诊断、可恢复的不同技术事实。

Core 不得用 Device Type 或 Provider 专用表、方法、API 路径或具体 action 分支实现设备语义。当前目标只要求支撑真实接入的最小 capability profile，不要求动态规则引擎或通用低代码配置。

### 可恢复的一致状态

- Project、Device、Command、Command Attempt、Device State、Event、Webhook Delivery 和 Audit 必须持久化。
- 状态迁移、事件产生和对外投递记录必须保持一致，进程重启后可以恢复处理。
- 幂等约束、条件状态迁移、重复回执和重复投递不能产生重复的最终效果。
- 后台和 Open API 读取的状态必须来自同一套实际运行模型，不能由多套平行 DTO 或内存状态各自解释。

### 模拟器

模拟器用于重复验证与真实 Provider 相同的核心命令链路和状态语义。它不能拥有一套与业务 Command 无关的平行命令域，也不能把组件内测试通过表述为平台主链闭环。

### 管理后台

后台是单管理员技术控制台，用于管理和诊断 Project、Device、Provider、Command、Attempt、Event、Webhook 和 Audit。它不是共享单车运营后台，不建设组织、员工、业务订单或复杂人类权限功能。

## 当前不进入完成定义

- 设备直连、地图、通用位置、电子围栏、OTA、固件管理、规则引擎、批量操作、告警和长期遥测保留策略不在当前目标中，也不作为本轮实现缺口跟踪。只有产品所有者基于真实需求修订目标合同后才进入实施。
- 自行车、景区、用户、订单、计费、停车和还车业务始终不进入平台完成定义。

## 达到目标的验收原则

只有同时满足以下条件，才可声明当前目标达到：

1. 受控环境中的共享单车应用能用 Project 机器身份完成设备查询和命令调用，且隔离、鉴权和幂等可验证。
2. 使用至少一台受控真实 WWTIOT 智能锁验证从平台请求到厂商实际可观测最高结果层级的端到端链路；Provider acceptance、Device ACK、Device final result 和 Unknown 必须在状态、事件与 API 中被准确区分。只有存在可信 final evidence 时才可进入 `success`。
3. 核心对象及处理进度持久化，服务重启后可恢复，重复请求、重复回执和并发处理不会产生重复最终效果。
4. 模拟器通过同一 Gateway/Provider 边界复现真实 WWTIOT 链路中经证据确认的结果层级与失败条件，不建立平行命令域，也不预设尚未确认的模式清单。
5. Event、Webhook Delivery 和 Audit 与命令事实一致；签名、重试、dead 状态和受控重发可验证。
6. 管理后台能据实展示上述技术状态与失败原因，不通过前端推测或掩盖后端事实。
7. Core、Device Type Capability、Provider adapter 与共享单车应用的责任可在代码和合同中清楚辨认，具体设备能力不固化为 Core 全局枚举。
8. 关键目标语义有自动化测试，并有受控真实设备验收证据；未验证条件明确标为 Unknown。

领域对象、事务边界、worker 所有权与崩溃恢复以[领域模型与一致性合同](./domain-model-contract.md)为准；接口路径、DTO、状态和错误语义以 [API 合同](./api-contract.md)为准。两者均不得降低本文的真实结果门槛。

WWTIOT 当前可观测层级是否足以支撑共享单车正式业务，必须由产品所有者在厂商合同和受控设备证据到齐后作出验收决定。本文不预先假定厂商一定提供 final result，也不预先判定较低 confirmation level 可以满足正式业务。

## 未达到目标的判定

出现下列任一情况，都不能声明完成：

- 核心业务对象仅保存在进程内存，或 schema 存在但运行时未接入。
- 将 Provider acceptance 直接记为 Device `success`，或用推断伪造高于实际证据的 confirmation level。
- 模拟器命令与 Open API/后台创建的 Command 不在同一状态机。
- 命令、事件、Webhook 或审计存在相互独立且会漂移的状态来源。
- API Key、IP 白名单、Project 隔离、幂等或条件状态迁移只有字段/文档，没有运行时执行。
- 只通过组件测试、伪厂商 HTTP 测试或页面展示推断真实设备闭环。

## 目标变更规则

新能力必须来自真实项目证据，并同时满足平台边界。目标变化时先修订本文，再修订 API 合同和实施；不得用数据库字段、模板菜单、历史文档或当前代码静默改变目标。
