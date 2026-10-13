# T03 成员目标包：MySQL anomaly concurrency

- display_name：测试-MySQL异常竞争
- role/subagent：测试 / `goal_qa`
- version：3.111.0-rc1186
- claimed_tasks：T03
- goal：为 MySQL 8 编写同一 durable funding anomaly 上两个独立连接、不同 adjustment operation 的并发集成测试。
- required_doc_load：`plan.md`、`tasklist.md`、`spec/acceptance.md`、`spec/test-plan.md`、`storage/funding_recovery_test.go` 的 MySQL disposable schema fixture、`storage/funding_recovery_anomaly.go`。
- allowed_scope：新建独立测试文件，最多增补测试专用 helper 文件；不要改生产代码或后端负责的 SQLite 测试文件。
- forbidden_scope：DDL 生产改动、迁移破坏、绕过 MySQL loopback/root/disposable schema/destructive opt-in 防护、真实账户/交易。
- locked_scope：必须通过 `QUANTMESH_MYSQL_TEST_DSN` 和 `QUANTMESH_MYSQL_TEST_ALLOW_DESTRUCTIVE_SCHEMA=1` 的安全夹具；本地缺 DSN 要明确 skip，不得宣称通过。
- required_tests：MySQL 8 实际双连接并发胜负；异常/ledger/receipt/outbox/cursor/hold 读回；获胜 operation 重试幂等；输家不得误认为成功。
- output_contract：测试文件、执行环境、实际运行或 skip 证据、命令、结果和限制；不得自行提交或推送。
