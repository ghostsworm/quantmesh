# RC1161 任务清单

| ID | 负责人 | 状态 | 锁定范围 | 完成条件 | 验证与文档 |
|---|---|---|---|---|---|
| T01 | Goal Lead | completed | RC1160 推送和 CI 证据 | SHA、MySQL 8.0.36、强制 race、全仓测试与 Linux 构建均有同提交证据 | `progress.md` |
| T02 | 策略-账务能力盘点 | completed_audit_only | 只读策略实现和测试 | 已盘点 14 个生产注册策略；均未实现 `OrderAccounting`，全部须保持 unsupported/fail-closed | 结果记录于 `progress.md`；待 Reviewer 复核 |
| T03 | 后端-核账服务生产装配 | completed | `main_order_reconciliation_service.go` 及其单测 | repository/evidence 装配；schema migration 仍由 `StorageService` 管理；Accounting/Gate 留空 | SQLite `StorageService` 重启读回与 case/audit 持久化测试通过 |
| T04 | Goal Lead | completed | `main.go` 启动路径及版本/变更文档 | storage 与 BotManager 创建后调用 helper；失败清空服务；版本/changelog/产品概览同步 | 启动窗口内暂时 503，安全 fail-closed；Web listener 当前先于 BotManager |
| T05 | 测试-核账生产装配验收 | partial | main/service composition 与 Web API 测试 | 已分别覆盖真实 SQLite `StorageService` 初始化/重启与 Admin case/audit API；有效 case 的 reconcile/release 在缺 adapter 时 503 且 case/audit 不变 | 尚缺同一测试中从 main 装配函数注入到受保护路由的单一端到端 seam；本机未配置 MySQL DSN |
| T06 | 评审-核账装配安全复核 | completed_with_followups | 只读最终差异 | 确认 fail-closed、owner scope 和 startup 顺序 | 发现 listener 启动至 service 注册的短暂 503 窗口；旧 schema check-then-ALTER 存在多进程竞态，均需独立跟进 |
| T07 | Goal Lead | pending | 全量检查与版本文档 | 强制 MySQL、全仓 Go、前端 verify、构建及同 SHA CI 状态明确 | 未提交/已提交/已推送/已发布分别记录 |

T03 的服务装配不依赖 T02 对每个策略给出支持结论；但任何 `Accounting`/`Gate` 成功执行路径必须等待策略契约和独立验收。
