# RC1186 团队计划

## 目标

针对 RC1185 留下的高风险验收缺口，证明两个独立 storage owner 同时对同一 durable funding source anomaly 提交不同人工 adjustment operation 时，只有一个精确账务调整能提交；原始事实保持不可变，失败方不能推进 cursor、解除 hold 或覆盖获胜方审计记录。使用 SQLite 独立句柄和 CI MySQL 8.0.36 独立连接验证真实数据库语义。

## 已确认决定与边界

- 用户确认版本按 bugfix 顺延为 `3.111.0-rc1186`。
- 用户确认 funding 历史异常保留原事实，只追加有审计依据的精确调整事件。
- 仅调整本任务涉及的 CAS/事务/测试；不修改订单重放、自动解锁或 permit 来源策略。
- 不接触真实交易账户，不下单，不发布盈利结论。
- 保留 `--help/`、旧 RC 历史目录、当前 `doc-capsules.jsonl` 和其他未跟踪文件；不把它们纳入提交。
- 当前 `.codex/goal-teams/INDEX.md` 已有用户未提交改动，本计划只做新增行，不重排或清理既有内容。

## 团队分工

| 成员 | 角色/子代理 | 任务 | 锁定范围 | 交付与完成标准 | 文档/测试责任 | 独立验证 |
|---|---|---|---|---|---|---|
| 后端-Funding异常CAS | `goal_backend` | 检查并在确有缺陷时修复异常调整事务/CAS；补 SQLite 两独立句柄、不同 operation 并发竞争 | `storage/funding_recovery_anomaly.go` 及 SQLite anomaly 测试；不得修改管理员 API 或自动释放逻辑 | 仅一个 operation 可写入 adjustment；另一个得到可识别冲突或安全可重试结果；receipt/outbox/anomaly revision/episode revision 一致 | 更新 RC1186 tasklist/progress 证据；负责 SQLite 并发测试 | 测试成员验证行为；评审成员复核不变量 |
| 测试-MySQL异常竞争 | `goal_qa` | 为 MySQL 8 编写独立连接并发集成测试，覆盖事务竞争、CAS loser、结果读回及清理 | 新增独立 MySQL 测试文件和测试 fixture；不得修改生产存储逻辑 | 强制 DSN 下通过；未配置本地 DSN 时清楚报告 skip，不将其算通过 | 更新 test-plan/acceptance；负责真实 MySQL 验证证据 | 评审成员检查 fixture 安全和覆盖面 |
| 评审-恢复并发安全 | `goal_reviewer` | 独立审查 CAS、owner fencing、原始事实不可变、cursor/hold 隔离及重复请求行为 | 只读源代码、SQL 与测试，不修改实现 | 报告 P1/P2 或确认无此级别问题；指出测试未覆盖场景 | 提供审查结论供 Goal Lead 整合 | Goal Lead 复核结论和代码差异 |

## 前置状态与证据

- `storage/funding_recovery_anomaly.go` 的调整写入通过 `updateRecoverySourceAnomaliesTx` 校验 anomaly revision/state、episode revision、generation、actor/audit/evidence，并在事务中以 revision 条件更新 anomaly。
- 已有 `TestSQLiteFundingRecoveryConcurrentOperationAcrossHandlesHasSingleCommit` 只覆盖相同 operation 幂等，不覆盖同一 anomaly 上两个不同 adjustment operation 的竞争。
- `storage/funding_recovery_anomaly_test.go` 覆盖单次 append-only adjustment 与持久化异常状态，但当前没有对应 MySQL anomaly adjustment 并发测试。
- GitHub `main` 已指向 RC1185 commit `91efbf657c59a0adaa6d7fe2e7de676dba82f1d1`；发布工作流已通过并发布 Linux amd64 资产。重复 main CI 的 race step 在本计划开始时仍显示运行中，后续单独读回。

## 验收顺序与停止条件

1. 后端先构造竞争夹具与 SQLite 独立句柄测试，确认实际结果类型与存储原子边界。
2. 测试成员独立加入 MySQL 8 双连接竞争测试，使用 workflow 已有隔离 schema/DSN 防护。
3. Goal Lead 审核差异后运行目标用例、race、`go test ./storage`、`go vet ./...`、完整 Go/Web 门禁。
4. 评审成员独立检查实现与测试；如发现可能重复入账、绕过 hold/cursor 或跨账户归属的问题，停止提交并修复/重规划。
5. 完成后更新版本号、CHANGELOG、PRODUCT_OVERVIEW（如产品行为说明受影响）、验收文档；明确区分本地、提交、推送、CI、构建、发布状态。

阻止条件：缺少可隔离 MySQL schema/DSN 时不得宣称 MySQL 并发验收通过；竞态中结果不确定、出现重复事件、失败方改变游标或解除 hold、或测试仅验证同 operation replay 时不得进入发布。
