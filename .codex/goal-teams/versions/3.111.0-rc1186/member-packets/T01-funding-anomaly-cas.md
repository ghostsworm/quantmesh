# T01 成员目标包：Funding anomaly CAS

- display_name：后端-Funding异常CAS
- role/subagent：后端实现 / `goal_backend`
- version：3.111.0-rc1186
- claimed_tasks：T01、T02
- goal：检查并闭合同一 durable source anomaly 被不同 operation 并发人工调整时的 CAS/事务正确性。
- required_doc_load：`plan.md`、`tasklist.md`、`spec/acceptance.md`、`spec/test-plan.md`，以及 `storage/funding_recovery_anomaly.go` 与相关 SQLite 测试。
- allowed_scope：`storage/funding_recovery_anomaly.go` 和当前 SQLite anomaly 测试文件；若实现没有缺陷，仅提交测试，不为测试方便弱化生产校验。
- forbidden_scope：HTTP/API、自动重放、release/permit、Gate/hold 产品逻辑、版本文件、用户已有 `INDEX.md` 改动、`--help/`、旧版本目录。
- locked_scope：不得更改、删除原始 ledger facts；不得让失败方推进 cursor、产生第二份 adjustment 或解除 hold。
- required_tests：独立 SQLite handles；不同 operation ID 和不同精确差额；response-loss 同 operation retry；检查 receipts/outbox/anomaly revision/episode revision/cursor/hold。
- output_contract：说明事务和锁分析、文件改动、测试命令/结果、剩余风险；不得自行提交或推送。
