# RC1162 任务清单

| ID | 负责人 | 状态 | 锁定范围 | 完成条件 | 验证与文档 |
|---|---|---|---|---|---|
| T01 | 后端-核账迁移并发安全 | 本机通过；远端待验 | `storage/order_reconciliation.go`、`storage/order_reconciliation_test.go` | SQLite/MySQL 并发/重复 schema 初始化安全；旧 schema 补列可恢复；没有吞掉非竞态 DDL 错误 | MySQL 8.0.36 + SQLite race 定向测试通过；最终只读复核与同提交 CI 待完成 |
| T02 | 测试-核账生产组合验收 | 本机通过；完整 main 进程未覆盖 | `main_order_reconciliation_composition_integration_test.go` | 一个组合测试贯穿 StorageService → production builder → 受保护路由 → durable case/audit；restart readback；无 Accounting/Gate 时拒绝补账/解锁 | Go race 组合测试通过；未启动真实 `main()` 或连接真实账户 |
| T03 | 评审-核账启动迁移安全复核 | 复核中 | 只读 | 评审迁移锁/DDL行为、启动顺序、测试真实性及 fail-closed | 最新 diff 的只读复核未完成；启动后故障时持仓退出能力列为后续风险 |
| T04 | Goal Lead | 本机阶段验证通过；未交付 | 版本、CHANGELOG、产品概览及最终集成 | 版本/文档一致；所有原有未跟踪文件保留；无真实账户/交易 | 本地测试结果记录于 progress；远端 CI、提交、推送、发布均未执行 |

后续逐策略 durable cursor/CAS、费用账本与恢复验证属于下一切片，不是本任务完成条件，也不因本切片通过而自动允许人工解锁。
