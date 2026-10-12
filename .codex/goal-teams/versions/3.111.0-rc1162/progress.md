# RC1162 进度与验证

## 基线

- 起始分支：`main`
- 起始 HEAD：`b8ece4019ec59398322344936659261c9a5c4c0e`
- 既有未跟踪 `--help/` 与多个 RC 历史目录：保留，不纳入提交。
- 起始版本：`3.111.0-rc1161`；本切片版本目标 `3.111.0-rc1162`。

## 团队进展

| 成员 | 任务 | 状态 | 证据/下一步 |
|---|---|---|---|
| 后端-核账迁移并发安全 | SQLite/MySQL 并发 migration | 已实现；本机集成验证通过 | 真实 MySQL 8.0.36 + SQLite 并发/锁取消测试通过；仍需同提交远端 CI |
| 测试-核账生产组合验收 | production wiring → protected API → durable readback | 已实现；race 验证通过 | 生产 service builder、admin route、持久化重启读回、缺 Accounting/Gate fail-closed |
| 评审-核账启动迁移安全复核 | 只读审查锁/DDL/启动调用链 | 复核中 | 已确认启动错误返回与订单路径仍需独立运营风险跟进；等待针对最新 diff 的最终复核 |

## 验证结果

验证均针对当前未提交工作树；没有可供远端 CI 对照的提交 SHA。

- `GOCACHE=/tmp/quantmesh-go-cache go test ./...`（启用本机网络权限后重跑）：exit 0；全仓 Go 包通过，耗时约 201 秒。首次受限沙箱运行因 loopback `httptest` bind 被系统拒绝，不作为代码失败或通过证据。
- `QUANTMESH_MYSQL_TEST_DSN='root@tcp(127.0.0.1:33306)/quantmesh_test?parseTime=true' QUANTMESH_MYSQL_TEST_ALLOW_DESTRUCTIVE_SCHEMA=1 GOCACHE=/tmp/quantmesh-go-cache go test -race ./storage -run '^TestOrderReconciliation' -count=1 -v`：exit 0；MySQL 8.0.36 disposable schema、SQLite/MySQL 并发升级、兼容性拒绝、锁超时取消/释放及重试通过。
- `GOCACHE=/tmp/quantmesh-go-cache go test ./storage -run '^TestOrderReconciliationSQLiteMigrationWaitIsCancelableAndRetryable$' -count=1 -v`：exit 0；150ms context 锁等待在约 0.26 秒内取消，释放锁后迁移重试通过。
- `GOCACHE=/tmp/quantmesh-go-cache go test -race . ./web -run '^(TestProductionOrderReconciliationCompositionFailsClosedAcrossRestart|TestOrderReconciliation.*)$' -count=1`：exit 0；生产组合、admin middleware、case/audit 重启读回与缺 Accounting/Gate fail-closed 通过。
- `GOCACHE=/tmp/quantmesh-go-cache go vet ./...`：exit 0。
- 前端 `yarn --cwd webui typecheck` 与 `yarn --cwd webui test`：exit 0；56 个测试文件、318 个测试通过。仅同步 package 版本，没有前端逻辑变更。
- `git diff --check`：exit 0。

没有运行真实 `main()` 服务进程/真实数据库部署，也没有注入 MySQL DDL 权限拒绝或物理连接断开来验证 `RELEASE_LOCK` 失败分支；因此这些不记为验收通过。完整 Go suite 开始于最后一处 SQLite 重试代码改动之前；改动后补跑了上述 SQLite 锁取消、MySQL 全部 reconciliation 测试、组合 race 测试与 vet。

## 交付状态

尚未提交、推送、打 tag、发布或部署；同一提交的 GitHub MySQL CI 与完整 CI 尚未验证。先前 `--help/` 与 RC 历史目录均保留为未跟踪状态。`main()` 中 `StorageService` 初始化错误仍会导致 storage 依赖缺失；订单安全门会拒绝新建/托管执行，但现有持仓的正常退出能力需另行处理与演练，不因本切片测试通过而宣称无风险。没有连接真实账户或进行真实交易；盈利证据尚未建立。
