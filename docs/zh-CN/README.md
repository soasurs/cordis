# Cordis 架构与设计

本文档描述当前代码库中已经实现的架构与设计。尚未实现或仍需加强的部分集中记录在“限制与演进”中。架构、服务行为、领域规则、协议和运维设计以此处为准；`AGENTS.md` 仅保留仓库贡献约束、执行说明及文档入口。

- [系统总览](overview.md)
- [服务目录](services.md)
- [实时系统](realtime.md)
- [数据存储与事件](data-and-events.md)
- [API、协议与错误](protocols-and-errors.md)
- [认证与令牌轮换](authentication.md)
- [配置、可观测性与开发](operations-and-development.md)
- [当前限制与演进方向](limitations.md)

## 阅读建议

初次了解项目时依次阅读“系统总览”“服务目录”和“实时系统”。开发业务 RPC 时重点阅读“数据存储与事件”及“API、协议与错误”；部署或排障时阅读“配置、可观测性与开发”及“当前限制与演进方向”。
