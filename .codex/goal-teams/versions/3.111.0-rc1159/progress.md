# RC1159 实施进度

## 当前快照

- 分支：`main`；HEAD：`cc40271a` (`v3.111.0-rc1157`)；`origin/main` 与本地基线相同。
- 工作树有 RC1158 未提交修改：版本文件、Changelog、产品概览、`storage/sql_bot_state.go`，及新建 `storage/sql_bot_state_test.go`。其他未跟踪团队材料及 `--help/` 均保留。
- RC1158/1159/1160 验证：SQLite/MySQL 时间往返、管理员 audit endpoint、缺失案 404、审计读取失败 503、权限与生产 route smoke 均通过。RC1160 修复 UTC/local claim cutoff 不一致后，MySQL 8.0.46 专用 `quantmesh_audit_rc1159` 上 storage 全包与 `go test -p 1 ./... -count=1` 均通过；前端核账/i18n 定向 7 项、完整 318 项与类型检查通过，`go vet ./web ./storage` 通过。仍需 CI MySQL 8.0.36 和同一已提交源码指纹复验。
- 当前确认的主要生产缺口见 RC1157 `tasklist.md`：策略 CAS/幂等账务、owner-scoped durable Gate bridge、专用审计读取 API、权威费用完整性。

## 成员进度

| 成员 | 任务 | 状态 | 证据 | 下一步 |
|---|---|---|---|---|
| 策略-账务核验 | 生产策略能力盘点 | 进行中 | 等待当前只读审查 | 列出策略 registry 与 durable cursor 缺口 |
| 评审-审计盈利证据 | 盈利证据调用链审查 | 进行中 | 等待当前只读审查 | 整理数据/指标契约及真实数据边界 |
| Goal Lead | BotState MySQL 修复 | 本机包级通过 | `go test ./storage -count=1`（强制隔离 DSN），MySQL 8.0.46 整包通过；SQLite/driver parse/round-trip 通过 | 同一已提交源码在 CI MySQL 8.0.36 复验 |
| Goal Lead | 管理端核账审计读取与刷新 | 实现并定向验证通过 | Go 审计/路由用例通过；7 个前端核账/i18n 用例通过；完整前端 318 项及 `yarn typecheck` 通过 | 生产策略与 gate 接线未完成 |
| Goal Lead | MySQL UTC claim 修复 | 本地通过 | 专用 MySQL 8.0.46 上 storage 包和串行整仓 Go 测试通过 | CI MySQL 8.0.36 同提交验证 |
| Goal Lead | Go 静态检查 | passed | `go vet ./web ./storage` | 无 |
| 后端/前端/QA/最终 Reviewer | 全链路人工核账 | 待启动/待契约 | RC1157 文档说明现有接口还不具备生产解锁闭环 | contract 明确后按锁定范围推进 |

## 状态定义

所有记录区分本地未提交、已提交、本地 tag、远端分支、GitHub Actions、发布包与公开 release。测试通过不自动代表发布；人工核账能力也不代表实盘/盈利通过。
