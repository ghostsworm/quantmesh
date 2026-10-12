# RC1162 验收标准

| ID | 条件 | 证据要求 | 状态 |
|---|---|---|---|
| A1 | 真实生产装配到受保护 API 的组合路径可工作 | 单一测试使用 production wiring 与实际 admin middleware/route | 本机通过；未运行完整 `main()` |
| A2 | case/audit 持久化与重启读回正确 | 通过生产 repository 读回状态、owner、审计事件 | 本机 race 通过 |
| A3 | 缺 `Accounting`/`Gate` 保持 fail-closed | reconcile/release 明确失败；无成功状态与成功审计 | 本机 race 通过；状态仍 prepared，audit 仅含创建事件 |
| A4 | SQLite 迁移并发可重复安全 | 并发升级旧 schema、并发冷启动、反复重入与完整 schema 断言 | SQLite race 定向测试通过；150ms 锁取消后重试通过 |
| A5 | MySQL 8 迁移并发安全 | disposable MySQL 真实集成测试；竞争 DDL 仅对已确认等价的完成结果收敛 | MySQL 8.0.36 disposable schema + race 定向测试通过 |
| A6 | 错误不会被吞掉或永久悬置 | 注入连接/权限/DDL 故障，迁移错误返回且 service 不注册；锁超时有界 | SQLite/MySQL 获取锁超时与 schema 不兼容通过；权限拒绝、MySQL 连接断开/release-lock 失败尚未注入 |
| A7 | 回归门禁与版本记录一致 | 定向、race、Go 全仓、强制 MySQL、vet 结果与 SHA/dirty 状态记录 | 本地 Go 全仓、MySQL 定向、组合 race、vet、前端 typecheck/test 通过；没有提交 SHA 与远端 CI |
| A8 | 安全范围保持不变 | 无真实账户/下单/撤单；不启用策略 Accounting/Gate；untracked 文件保留 | 符合；既有 `--help/` 与 RC 历史目录仍保留 |

即使 A1-A8 全通过，也只证明该工程切片的离线/CI 工程行为；不证明策略级补账可用、不构成实盘或盈利验收。
