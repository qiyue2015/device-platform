---
title: Device Platform 文档
updated: 2026-08-01
status: governed-contract-set
contract_revision: 2026-08-01
---

# Device Platform 文档

本目录是 Device Platform 后端重建的项目合同。核心合同不以现有或历史 `backend/` 为依据；删除实现代码后，产品边界、领域语义、交互协议、工程约束和验收门槛仍然有效。

## 权威顺序

| 顺序 | 文档                                                             | 唯一负责定义                                                   |
| ---- | ---------------------------------------------------------------- | -------------------------------------------------------------- |
| 1    | [Platform Boundary](./platform-boundary-contract.md)             | 平台做什么、不做什么，以及平台、业务应用和设备接入方的责任边界 |
| 2    | [Platform Target](./platform-target-contract.md)                 | 当前后端重建必须完成的真实链路、完成门槛和产品所有者裁决项     |
| 3    | [Domain Model](./domain-model-contract.md)                       | 领域对象、关系、不变量、状态机、事务边界和恢复责任             |
| 4    | [API](./api-contract.md)                                         | HTTP 路径、认证、请求/响应 DTO、幂等 wire 规则和错误码         |
| 5    | [Messaging](./messaging-contract.md)                             | 内部消息 Envelope、Subject、Outbox/Inbox、Ack、重投和版本规则  |
| 6    | [Device Type: smart-lock](./device-types/smart-lock.md)          | 智能锁规范化 action、payload 和安全策略                        |
| 6    | [Provider: WWTIOT](./providers/wwtiot.md)                        | WWTIOT 厂商协议事实、action 映射、证据上限和厂商 Unknown       |
| 6    | [Provider: Omni](./providers/omni.md)                            | Omni 直连协议 profile、action 映射、证据上限和厂商 Unknown    |
| 7    | [ADR-0001](./architecture/technology-stack-adr.md)               | 已接受的技术栈、理由、代价和重新开启条件                       |
| 7    | [Runtime Architecture](./architecture/runtime-architecture.md)   | 运行单元、数据流、基础设施职责和故障隔离                       |
| 7    | [Reliability and Capacity](./operations/reliability-capacity.md) | 容量、SLO/RPO/RTO、降级、故障演练和上线证据                    |

同一顺序的文档是并列从属合同，不能互相覆盖。发生冲突时，较低层文档停止扩展该语义并引用较高层权威；无法由上级合同消除的实质冲突进入 Platform Target 的“需要产品所有者裁决”，不得保留竞争口径。

[Current State](./current-state.md) 是实现与验收快照，不是合同来源。它必须分别记录 WWTIOT、Omni、本地模拟和真实设备证据，不能用某一 Provider 或 simulator 的通过结果替代另一条链路。

[Local Development](./local-development.md) 是本地启动、安全边界和检查命令的操作指南。它不改变合同，也不能把伪 Provider、模拟 TCP peer 或本地浏览器结果提升为真实设备证据。

## 核心概念所有权

| 概念                                     | 唯一权威来源            | 其他文档允许做什么                          |
| ---------------------------------------- | ----------------------- | ------------------------------------------- |
| 产品范围、责任主体、非目标               | Platform Boundary       | 引用，不得增加产品能力                      |
| 当前真实链路、完成定义、产品裁决         | Platform Target         | 细化验收证据，不得降低门槛                  |
| 对象、关系、不变量、生命周期、事务、恢复 | Domain Model            | API 只表达 wire 形式，架构只说明如何实现    |
| HTTP 认证、路径、DTO、幂等响应、错误码   | API                     | Device Type/Provider 只细化其从属字段和映射 |
| 内部异步传输                             | Messaging               | Runtime 只分配运行责任                      |
| 规范化设备能力                           | Device Type             | Provider 只做厂商映射                       |
| 厂商协议事实和证据上限                   | Provider                | Core 不得猜测或提升证据等级                 |
| 技术选型与运行约束                       | ADR/Runtime/Reliability | 不得改变产品范围或领域语义                  |

代码、数据库 schema、管理后台、测试、配置和历史 Git 记录都不是合同来源。它们只能作为某次实现验收的证据，不能反向改写本文档集。

## Unknown 规则

每个 Unknown 必须在拥有该问题的文档中记录，并同时写清：未知内容、阻塞的验收、不阻塞的工作、关闭责任或证据。Unknown 不等于允许猜测，也不自动阻塞全部开发。

- 产品范围、业务可接受性或数据政策 Unknown：记录在 Platform Target，并由产品所有者裁决。
- 领域恢复和一致性机制 Unknown：记录在 Domain Model。数据保留期限、删除合法性和业务可接受性由 Platform Target 拥有；Domain Model 只定义裁决前后的领域不变量与执行机制。
- 厂商协议、回调、设备行为和证据能力 Unknown：记录在 Provider。
- 目标负载、SLO、RPO、RTO 和部署条件 Unknown：记录在 Reliability and Capacity。
- 内部消息 wire、隔离和重驱机制 Unknown：记录在 Messaging。

## 冻结与维护

`frozen-for-implementation` 表示语义可直接实施，不表示代码已完成，也不表示真实设备已验收。`product-decision-required` 表示未受裁决影响的条款仍有约束力，但列明的产品分支不得实施或验收。`implementation-unknown` 表示已冻结部分仍可实施，但列明的工程机制尚无唯一实现，受影响分支不得实施或验收。Unknown 可以在不影响其明确非阻塞范围时随合同冻结；受其阻塞的验收不得通过。

- 改变产品归属或非目标，先修订 Platform Boundary。
- 改变当前真实链路、完成门槛或产品裁决，先修订 Platform Target。
- 改变对象、不变量、生命周期、事务或恢复责任，先修订 Domain Model，再同步下级 wire 合同。
- 改变 HTTP 或内部消息 wire 合同，分别修订 API 或 Messaging。
- 改变设备能力或厂商映射，分别修订 Device Type 或 Provider，不得在 Core 复制定义。
- 改变已接受技术组件，使用替代 ADR；改变部署和可靠性基线，修订 Runtime/Reliability 或新增部署 ADR。
- 每次合同变更都必须检查相对链接、术语、状态机闭合、责任主体、Unknown 影响和上下级一致性。

实现状态、迁移步骤、本机命令、阶段复盘和会话记录不进入核心合同。它们应由实现仓库、发布记录、运维手册或 Git 历史按需要维护，不能继续影响后端重建设计。
