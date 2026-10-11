# RC1161 核账生产装配测试计划

## 核心调用链

`main` 启动存储 → 迁移核账表 → 构建生产核账依赖 → `SetOrderReconciliationService` → 受保护路由 → SQL case/audit 读回与可信订单证据读取。

## 必测情形

- SQLite/MySQL 隔离库重复初始化、创建 case 后重启读取，以及审计事件与状态同事务持久化。
- migration/DB 不可用时不注册半初始化服务；API 明确返回不可用，不显示成功态。
- 管理员权限与 owner、Bot、账户 scope、symbol、strategy、client/venue order ID 精确对应。
- venue 非终态、UNKNOWN、成交缺失、费用未知、基础币费用无法表示时不得 reconcile。
- `Accounting == nil` 或 `Gate == nil` 时，reconcile/release 均不可成功；Gate 调用次数为零。
- race/并行双进程启动时 schema 初始化和幂等写入不造成死锁或重复状态转移。

## 完成界限

纯 handler/mock 测试只覆盖 API 契约，不能代替生产启动接线。没有实际逐策略账本 adapter、持久化 owner quarantine 和 owner-specific durable Gate bridge 的策略，不得报告为可人工解锁。
