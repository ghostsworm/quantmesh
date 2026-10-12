# 需求规格卡：核账生产组合与迁移并发安全

## 目标

证明已接入的核账案/审计服务在真实生产组合路径中可初始化、经授权 API 持久读写并在重启后恢复；多个进程并发启动时不会因核账 schema 迁移竞态导致实例失败或状态半迁移。

## 核心行为

- 生产服务必须使用启动流程已完成迁移的 durable SQL storage 与实际 BotManager evidence provider。
- 经受保护管理员 API 建立的核账案及审计必须能通过真实 repository 读回；重启后数据仍在。
- 本阶段 `Accounting` 和 `Gate` 仍缺逐策略实现。reconcile/release 必须拒绝，状态和审计不能伪造成功。
- SQLite、MySQL 的重复迁移和并发迁移需要分别验证；只通过模拟测试或 SQLite 不代表 MySQL 并发正确。

## 用户/运营路径

管理员授权请求 → 路由身份校验 → 已生产装配的核账 service → SQL case/audit 原子写入 → 查询读回；服务重启后重建依赖并读回相同 case/audit。账务或 Gate 能力为空时，操作以明确 unavailable/fail-closed 结束。

## 范围与排除项

本切片只覆盖生产组合验收、schema 迁移安全、必要的版本与验收文档。排除逐策略 durable cursor/CAS、人工补账执行、解锁桥、费用/借贷总账、真实账户或盈利证明。不得连接实盘、下单、撤单、发布或部署。
