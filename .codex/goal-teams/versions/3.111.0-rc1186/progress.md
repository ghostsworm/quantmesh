# RC1186 进度

## 已完成

- 用户已确认 RC1186 分工计划。
- 创建隔离工作分支 `codex/funding-recovery-anomaly-race-rc1186`；生产实现无需改动，新增并发验收测试。
- 读取并核对 RC1185 anomaly adjustment 写入事务、现有跨句柄测试及 MySQL 集成 fixture。
- 最新远端状态：`main`=`91efbf657c59a0adaa6d7fe2e7de676dba82f1d1`；RC1185 发布验证、Linux amd64 构建与 Release 资产验证成功。main CI race step 仍在运行。

## 当前分工状态

| 成员 | 任务 | 状态 | 证据 | 下一步 |
|---|---|---|---|---|
| 后端-Funding异常CAS | T01/T02 | 本地通过 | SQLite 双 handles、不同 operation；Lead race `-count=20` 与 storage 全套均通过；注入底层 commit 已成功但调用结果报错，生产 `commitProbe` 成功读回 receipt | 等最终独立复审 |
| 测试-MySQL异常竞争 | T03 | 代码/编译通过；MySQL 运行待验 | 独立连接测试、审计字段及 loser retry 后状态读回已补；storage 全套通过 | 本地无 DSN，待 RC1186 强制 MySQL CI 实跑 |
| 评审-恢复并发安全 | T04 | 最终复审完成 | reviewer 未发现 P1/P2；指出原始 anomaly 对象比较可更完整，Lead 已改为完整 Facts 对象断言 | 最终差异复审完成；目标测试重新运行 |

## RC1186 首轮实现证据

- SQLite：新增 `TestSQLiteFundingRecoverySourceAnomalyDifferentAdjustmentsAcrossHandlesHaveSingleCommit`。Lead 已独立通过专项、`-race -count=20` 和 storage 全套；新增驱动注入使 SQLite 先提交事务再返回不确定错误，覆盖生产 `commitProbe` 独立连接读回 receipt 分支。
- MySQL：新增 `TestMySQLFundingRecoveryAnomalyDifferentAdjustmentsHaveSingleWinner`，覆盖两个连接、CAS loser、幂等读回、完整 audit evidence 字段和输家重试后持久状态。首轮拼写错误已由 QA 修复。
- MySQL 环境：本地无 `QUANTMESH_MYSQL_TEST_DSN`，不能执行本地 MySQL 竞争验收。RC1185 CI/CD 的 MySQL 结果不包括本次新增代码，因此不替代 RC1186 专项测试。

## 独立复审后修订

- reviewer 指出普通成功后 retry 不等同于 commit response loss。Lead 加入真实 commit 后注入 commit error 的 SQLite driver，验证生产 `commitProbe` 用独立连接确认并返回同一 receipt。
- reviewer 指出 MySQL 持久 adjustment 审计字段和输家 retry 后状态断言不完整；Lead 已补齐金额/hash/actor/audit ref 读回及 outbox/anomaly/cursor/hold 检查。
- 最终独立复审建议完整比较 MySQL anomaly Facts 对象；Lead 已加入完整对象断言，MySQL 专项复跑仍因本机 DSN 缺失而 skip。
- 带本机回环监听权限的全量 `go test -p=2 ./... -count=1 -timeout=30m` 通过；`go vet ./...` 与 `yarn --cwd webui verify`（typecheck、318 项测试、production build）通过。无权限初次尝试的失败仅因沙箱禁止 `httptest` 绑定 localhost，不作为代码失败。
- MySQL 专项本机因未配置 `QUANTMESH_MYSQL_TEST_DSN` 而 skip；RC1186 同 SHA 远端 CI、强制 MySQL 8 专项与 release verification 尚未触发。
