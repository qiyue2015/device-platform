---
title: Device Platform 文档
created: 2026-05-16
updated: 2026-07-31
status: frozen-for-implementation
freeze_revision: 2026-07-31.3
---

# Device Platform 文档

本目录保存当前有效的产品与工程合同。产品所有者以中文直接审阅；API 路径、字段、状态值和代码标识保留英文。

## 权威顺序

1. [Platform Boundary Contract](./platform-boundary-contract.md)：平台与业务应用的归属边界、长期运行模型和非目标，是最高产品权威。
2. [Platform Target Contract](./platform-target-contract.md)：基于当前共享单车真实接入的唯一当前目标和完成定义。
3. [Domain Model Contract](./domain-model-contract.md)：当前目标内的对象、关系、不变量、事务边界和恢复责任。
4. [API Contract](./api-contract.md)：边界内、服务当前目标的接口、认证和生命周期语义。
5. Device Type 与 Provider 从属合同：分别定义规范化设备能力和厂商适配事实，不能互相越权。
6. [Current State](./current-state.md)：带日期的实现状态快照，只陈述事实与缺口，不定义合同。
7. [Local Development](./local-development.md)：当前可执行的本地运行与验证方式。

上级合同约束下级文档。Device Type 与 Provider 合同分属规范化能力和厂商映射，冲突时不能彼此覆盖，应回到 Domain/API 合同消除歧义。Current State 与 Local Development 是事实和操作说明，不具备产品合同权威。当前代码、数据库 schema、Admin9 模板菜单和历史文档只能作为实现证据，不能静默扩大或缩小平台边界，也不能替代当前目标合同。

## 从属接入合同

- Device Type Contracts：定义规范化设备能力、payload 和已有证据支持的安全属性；当前为 [smart-lock](./device-types/smart-lock.md)。
- Provider 合同：区分厂商资料事实、适配器代码事实和真实设备验收事实，并定义配置、action 映射、confirmation level 与 Unknown；当前为 [WWTIOT](./providers/wwtiot.md)。

Device Type 合同和 Provider 合同都从属于 Platform Boundary、Platform Target、Domain Model 与通用 API Contract。它们描述当前真实接入，不能扩展 Platform Core 的全局 action、产品范围或生命周期语义。

## 冻结规则

文档冻结只表示实现合同已经无阻塞歧义，不表示代码已实现或真实设备已验收。冻结范围包括上述四份主合同、`smart-lock` Device Type 合同和 WWTIOT Provider 合同；Current State 保持带日期的可变事实快照。

冻结前必须完成链接、术语、状态机、字段、责任、事务边界、恢复策略、厂商证据和 Unknown 反向复核。通过后将合同 front matter 的 `status` 统一改为 `frozen-for-implementation`，并记录同一 `freeze_revision`。冻结后任何语义变更必须先修订合同、说明原因并重新执行冻结复审，不能由实现静默改变。

## 维护规则

- 产品归属或明确非目标改变时，先修订 Platform Boundary Contract。
- 当前共享单车真实目标或完成定义改变时，修订 Platform Target Contract。
- 领域对象、不变量、事务边界或恢复所有权改变时，修订 Domain Model Contract。
- 代码事实改变时，更新 Current State 的日期、证据与分类。
- 接口、状态机、幂等、投递或审计语义改变时，修订 API Contract，并检查其仍符合上级合同。
- 规范化设备能力变化时，修订对应 Device Type Contract；厂商字段、配置、映射或已验证事实变化时，修订对应 Provider 合同。
- 启动命令、依赖或安全本地验证方式改变时，修订 Local Development。

仓库 Git 历史保存过去的交付记录、分支复核记录和代理流程。当前 `docs/` 不承担历史档案库职责，也不继续保留会误导当前实现或产品方向的旧版本叙事。
