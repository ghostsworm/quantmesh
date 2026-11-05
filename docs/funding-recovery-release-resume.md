# FundingCarry 管理员解锁中断续办

版本：`3.111.0-rc1196`；日期：2026-11-05；开发中，未发布。

## 问题与复现

原释放流程依次调用 `PrepareRecoveryRelease`、`ReleaseRecoveryHold` 和 `ReleaseFundingRecoveryGate`。第一步已提交 `release_pending`，第二步尚未释放 hold 时进程退出，新账户 generation 无法通过普通 takeover，也不能使用要求当前 owner 的 release resolver。原启动装配还会拒绝保留该状态的只读核账 runtime。

修复前执行 `TestProductionFundingRecoveryReleaseInterruptedAcrossInstances`：复用生产 service/router 的真实账务投影 fixture，将准备释放事务提交后关闭 SQLite StorageService，再通过新连接与新 FundingCarry runtime 请求普通接管、原释放。两者分别返回状态限制和 owner 不匹配；hold/Gate 仍在。该复现使用隔离 SQLite 文件和内存 venue，没有访问真实交易账户。

## 接口与授权

两个端点均位于现有受保护的 capital API，要求管理员 session：

- `GET /api/capital/funding-recovery/:episode_id/release/resume/preview?release_operation_id=...`
- `POST /api/capital/funding-recovery/:episode_id/release/resume`

预览只读返回当前 episode、hold、原 release receipt、续办证据摘要、目标 owner、精确 recovery Gate 与当前 Gate 来源。续办请求包括独立 `operation_id`、原 `release_operation_id`/`release_request_hash`、`audit_ref`、预览 `evidence_hash`、`expected_owner_generation`/`owner_generation`、完整 `account_scopes`、`expected_episode_revision`/`expected_hold_revision`/`recovery_generation` 和 `confirmed=true`。`episode_id` 取路径，actor 取已验证的管理员 session；请求体拒绝 actor、未知字段、尾随 JSON 和超过 16 KiB 的内容。

原释放 `EvidenceHash` 与 `ExposureClearanceHash` 保留。外部仓位/借贷清零证明仍是原管理员审计声明，续办不会把它升级为交易所权威验证，也不接受客户端账务事实替换它。

## 事务与恢复边界

| 当前持久状态 | 允许行为 | 结果 |
|---|---|---|
| `release_pending` + `held` | 显式续办原释放操作 | 同一事务转移 owner、追加续办审计、释放原 hold、推进原 release receipt |
| `released` + `released` | 已保留的停止 runtime 接续清理责任 | 更新 owner 与续办审计，原 hold revision 和释放 operation 保持 |
| 同一续办 operation 精确重试 | 核对 generation、请求和提交后证据 | 返回已有结果，不重复 CAS；可重试精确 Gate 清理 |
| 普通 takeover、状态/revision/scope/receipt/hash 不符、旧 capability | 拒绝 | 不解除 hold/Gate，不执行交易 |

存储事务先按账户范围获取 generation fence，再读取完整耐久证据并核对 CAS。续办 receipt 引用原释放 operation/request hash；episode 使用独立 `ReleaseResumptions` 审计链，不修改普通 `OwnerTakeovers` 的 blocked 语义。原 release receipt 的 actor、audit、owner 与请求哈希保留。提交后证据摘要阻止 Gate 失败重试绕过后来出现的异常或变更。

迟到事件重开 episode 后，历史续办审计继续保留；owner 校验按 revision 合并普通接管与续办记录，当前释放链只处理当前 recovery generation。合法重开事件通过独立 receipt 校验，不假定它一定来自 outbox。任何经历过续办的 episode 继续只允许核账，不能重新颁发交易执行许可。旧释放授权也不能在新 owner 上借普通释放的幂等路径绕过续办证据核验。

事务成功后，生产 service 仅调用现有精确 Gate 清理：移除该 episode/recovery generation 的来源，保留人工、风险及其他 episode 来源。Gate 清理失败时 HTTP 409 返回 `hold_released=true` 和未核验状态；同请求重试不会再释放一次。`execution_replayed` 始终为 false，操作不启动策略。

`release_pending` 的真实 constructor 只在 active hold 与已投影证据匹配时保留停止 runtime，不自动接管、释放或重放。若 durable hold 已 `released` 后进程退出，原内存 Gate 随进程消失；本变更没有增加持久化 Gate 完成标志，也不改变应用下一次普通启动的既有策略。`released` 续办分支只在已经保留的停止 runtime 上提供 Gate 清理，不声称完整 constructor 会为已释放 hold 创建新的核账 runtime。

无需 DDL 迁移：授权审计扩展现有 episode/receipt JSON。降级旧二进制会忽略新增审计字段，因此应在管理员操作完成后评估降级，不把降级作为修改恢复记录的方式。

## 验证

修复前复现：`TestProductionFundingRecoveryReleaseInterruptedAcrossInstances` 复用生产 `StorageService` 和路由，在 `PrepareRecoveryRelease` 提交 `release_pending` 后关闭 SQLite，并以新连接、新 runtime generation 重开。普通 takeover 与原 release 均被拒绝，hold 与精确 Gate 保留；接入续办后同一用例继续验证显式恢复路径。

修复后验收（2026-11-05；基线 `7de60665`；Go 1.26.3 darwin/arm64；版本 `3.111.0-rc1196`；隔离 SQLite、内存 venue 与本机临时 MySQL schema，未访问生产账户）：

- SQLite 存储：`TestSQLiteFundingRecoveryReleaseResumeAtomicAndReplay`（`release_pending`/`released`）、`AuthorizationBoundaries`、`EvidenceBoundaries`（提交前后）、`ConcurrentPools`（同/不同 operation）、`LateCASRollsBack`、`AuditOnlyEvidenceChangeRejectsReplay`、`HistoricalReceiptTampering`、`ReopenPreservesOwnerHistory`、`PreviewJSONContract` 全部通过；同一组用例在 `-race` 下重跑通过。
- MySQL 9.5.0：`TestMySQLFundingRecoveryReleaseResumeAtomicAndReplay` 与 `ConcurrentPools` 全部通过；用例创建唯一临时 schema 并在结束后删除，必须显式设置 `QUANTMESH_MYSQL_TEST_ALLOW_DESTRUCTIVE_SCHEMA=1` 才会连接。既有 MySQL 8 专用 fixture 未在本机运行，不声称 MySQL 8 已验证。
- 生产装配/API：`TestProductionFundingRecoveryReleaseInterruptedAcrossInstances` 覆盖新连接新 runtime 的普通路径拒绝、预览与续办、重复请求、旧 owner、证据/release receipt/scope/revision 不符、Gate 清理失败返回 409 `hold_released=true` 且精确 Gate 保留、同请求重试幂等、人工 Gate 保留、二次重启只转移清理责任；`TestFundingCarryFullConstructorRetainsReleasePendingAfterRestart`、startup 保留用例与 web 权限/绑定/持久错误状态用例全部通过。
- 汇总入口：`ruby scripts/verify_funding_release_resume.rb docs/reports/funding-release-resume-rc1196 --mysql`，本次 163 个用例全部通过，MySQL 专项完成，测试期间源码摘要稳定，前后端版本一致；`results.json`/`results.md` 记录基线、源码树摘要、用例与诊断。

未执行/限制：本机 Docker daemon 不可用，未运行 MySQL 8 专用旧 fixture、真实多进程编排、远端 CI 或生产账户验证；`web` 包依赖 `go:embed dist/*`，干净 checkout 需先执行 `ruby scripts/frontend_embed.rb sync`（CI 已如此）或构建 webui。该验收只证明恢复协议正确性，不证明实盘或盈利。

本次不执行远端 CI、提交、推送、tag、部署或生产账户验证。
