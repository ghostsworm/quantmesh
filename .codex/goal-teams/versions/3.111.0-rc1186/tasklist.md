# RC1186 任务清单

| ID | 负责人 | 状态 | 任务与完成标准 | 验证/文档 |
|---|---|---|---|---|
| T01 | 后端-Funding异常CAS | 完成 | 复核事务顺序与 CAS：generation fencing、episode revision 冲突，ledger/receipt/outbox/anomaly 同事务；未发现需改生产逻辑的缺陷 | 后端成员基线分析；待 Lead 独立复核 |
| T02 | 后端-Funding异常CAS + Goal Lead | 本地通过 | SQLite 两独立 handles、不同 operation 竞争；驱动真实 commit 后注入 commit error，验证 independent `commitProbe` 读回 receipt；全状态读回 | 专项、ambiguous commit、race 重复 20 次、storage 全套通过 |
| T03 | 测试-MySQL异常竞争 | 测试已编写；实际 DB 验收待做 | 新增 MySQL 8 独立连接竞争测试与 disposable schema 防护；两独立连接通过 `CONNECTION_ID()` 验证；持久 audit 字段及 loser retry 后状态读回 | Lead storage suite 通过；本地专项因 DSN 缺失 skip；强制 MySQL CI 尚未运行 RC1186 文件 |
| T04 | 评审-恢复并发安全 | 完成 | 独立检查实现和测试中的 SQL 锁序、死锁/超时、CAS conflict、重放和 hold/cursor 安全边界；补强原始 anomaly Facts 完整对象比较 | 最终复审无 P1/P2；MySQL 专项因无本地 DSN 而 skip |
| T05 | Goal Lead | 本地门禁完成，远端待验 | 集成差异；完成目标测试、全量 Go/vet、前端门禁、独立复核与版本/变更文档；若影响账本不变量则停止交付 | 本地硬门禁通过；MySQL 专项、同 SHA CI 与 release verification 仍待推送后验证 |

约束：未跟踪的 `--help/`、旧 RC 历史目录、`doc-capsules.jsonl` 与既有用户改动必须保留且不纳入提交。MySQL 未配置或被跳过不算通过；不得用发布验证中其他 MySQL 测试替代本任务特定竞争测试。
