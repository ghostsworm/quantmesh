# RC1185 进度

- 用户确认对 late/changed/deleted funding facts 不覆盖原账；人工结案须追加有审计依据的精确差额事件。
- 独立只读评审指出当前仅 fail-closed，未持久化异常差异，也没有 Funding history adjustment API；异常捕获须 owner-fenced 且 held Gate 不能丢失。
- 已接入 SQLite/MySQL schema、owner-fenced durable capture、完整历史差异归类、管理员 append-only adjustment 事件和异常状态 API；策略 operation 将 adjustment、receipt/outbox 与异常 CAS 原子提交。
- adjustment 不改写原始事件、不改变 cursor/hold；新的完整历史扫描匹配账本后才标记 anomaly verified 并推进 cursor。
- 通过：`go test -p=2 ./... -count=1 -timeout=30m`、`go vet ./...`、WebUI typecheck、Vitest（56 files / 318 tests）与生产前端 build；全仓 Go 测试需可绑定 loopback 的执行环境，沙箱受限失败后已原命令获准重跑通过。
- 通过：生产 HTTP 捕获异常→管理员精确调整→首次投影失败→同 operation retry（账务事件仅一条）→完整重扫 verified 并推进 cursor；release prepare/commit 两个存储入口均验证拒绝 open / adjustment_applied anomaly。独立 reviewer 修复 episode revision 卡死与 release guard 缺口，复审未发现新的 P1/P2。
- race 通过：`go test -race . -run '^TestProductionFundingIncomeRouteProjectsAndReplaysOriginalCursorAfterAdvance$' -count=1` 与 `go test -race ./storage -run '^TestSQLiteRecoverySourceAnomalyCaptureIsFencedDurableAndIdempotent$' -count=1`。
- MySQL 真实迁移、多 storage process / handle 冲突与同 SHA 远端 CI 仍待验收。当前工作树未提交、未推送、未发布。
