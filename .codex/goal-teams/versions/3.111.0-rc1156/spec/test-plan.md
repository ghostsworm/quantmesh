# GT-004 测试计划：订单终态恢复生产接线

## 目标与边界

以 `requirement-spec-card.md` 的状态／证据契约及 `independent-review.md` 已成立的 P1 为验收基准，验证生产订单 callback 是否安全完成经济账、intent settlement、ExposureBook 全量 bootstrap，并仅在证据完整时解除本流程 gate。依据仓库 `AGENTS.md` 与 Goal Teams skill，本文件以中文撰写；新增测试名为建议名称，尚未实现或执行。本计划不代表真实实盘或盈利验收。

本 GT-004 仅规划测试：不得连接真实账户／生产 DB、发单或撤单；实现阶段须使用隔离临时存储与确定性 mock/fake。不得以 helper 单测代替生产接线验收。

## 前置依赖与未定架构

1. P1 修复必须让同一 executor/backend/scope 的已载入 journal 可安全幂等再入；普通 settle 后保留 settled intent 的 owner map。若另有未结/UNKNOWN intent，再入只能返回 recovery error、不可把 `journalLoaded` 清为 false，也不可 Seed；不能简单移除不同绑定的 after-submission guard，也不能丢弃 unresolved intent。此语义是相关测试通过的前置条件。
2. Zero-fill 接线需由架构设计确定 grid/non-grid 的 callback、策略账明确确认方式、gate owner 与 retry 时点。本计划不预设具体 API 或业务放行政策。
3. 部分成交、费用／借贷及策略 inventory loader 的权威证据沿用 requirement card 和后续架构决策；callback 返回 nil 不等同经济账完整。必须验证 durable fills、策略持久化、intent journal、venue snapshot 与 ExposureBook Seed 的次序。
4. 活动委托接管不在本切片范围；仅验证活动委托仍 fail-closed，不测试接管预留或自动撤单。

## 只读发现的测试 seams 与确定性风险

| Seam | 当前源码／测试证据 | fixture 可用性与风险 |
|---|---|---|
| 订单流 callback | `symbol_manager.go:1041` 调用 `ex.StartOrderStream(ctx, callback)`，callback 主体约在 `1042-1166` | fake exchange 可保存 callback 并注入 update；但 callback 嵌在大型 SymbolRuntime 初始化流程内，当前未发现可直接调用的 harness。生产接线测试需抽出最小 seam 或构造受控 runtime fixture；不得复制业务逻辑到测试 helper 后称为端到端。 |
| owned intent/journal | `main_exposure_test.go` 有 `runtimeJournalVenue`、`newJournalRuntime`、`runtimeJournalScope`；`order/intent_journal_test.go` 有 journal fixtures | 可隔离验证 owner、venue identity、持久 journal 与 rebind guard。`storage.NewSQLStorage(filepath.Join(t.TempDir(), ...))` + `MigrateExecutionIntents` 可用于临时 SQLite；不可连接外部 DB。 |
| fills 顺序与故障 | `main_execution_fills_test.go` 有 `runtimeFillExchange`、`runtimeGridFillVenue`、`sequencedRuntimeFillVenue`、`runtimeFillWriter`、`blockingRuntimeFillWriter` | 可控 fills、阻塞和写入错误。现有 blocking writer 等待 release 且不响应 context；测试应以 channel/defer 确保释放，防止测试挂死。需断言费用资产、trade ID 与累计量。 |
| 策略 accounting | callback 在 `symbol_manager.go:1115` 调 `ApplyOrderUpdateForStrategyWithAccounting`；fills 测试可直接传 accounting flags | 接线测试需可观察的策略 fake/reporter，记录路由和 durable 更新顺序。直接传 `strategyAccountingVerified=true` 只覆盖 helper，不证明生产路由。 |
| retry/bootstrap | `runtimeExposureBootstrapCoordinator`、`retryRuntimeExposureBootstrapAfterStrategyRecovery`；`main_exposure_test.go` 有 fake venue、临时 SQLStorage 与 book fixture | 可控制 open orders/positions、inventory、mark、retry 次数；必须使用同一 executor/journal 覆盖 P1，不可重建 executor 掩盖问题。 |
| gate/保护性 close | `order/opening_gate_test.go`、`order/exposure_integration_test.go` | 可在 bootstrap gate 保持时验证 opening 被拒而 futures ReduceOnly close 到达 fake venue。Spot close 不能假定 ReduceOnly；须证明 owner inventory/leg。 |

## 现有覆盖基线：不是端到端验收

- `main_exposure_test.go:23`：`TestRuntimeExposureBootstrapCoordinatorRetriesAndSerializes` 验 ready、失败重试与并发串行；不验生产 callback 触发或阻塞 I/O 活性。
- `main_exposure_test.go:409`：`TestRuntimeExposureBootstrapRetriesAfterStrategyRecoverySettlesIntent` 覆盖 recovery settle 与直接 bootstrap/Seed，但使用 `SettleRecoveredIntent`，不复现 normal `SettleIntent` 在 executor map 留项的 P1。
- `main_exposure_test.go:279,507`：`TestRuntimeExposureBootstrapCannotSeedExistingAccount`、`TestRuntimeExposureBootstrapRequiresAuthoritativeEmptyAccount` 覆盖部分 fail-closed 条件，不覆盖 callback 因果链。
- `main_execution_fills_test.go:148,221,288`：`TestCapturedGridFillSettlesOnlyAfterFillHistoryIsDurable`、`TestCapturedStrategyFillSettlesOnlyAfterFillHistoryIsDurable`、`TestRuntimeFillCaptureDoesNotSettleBeforeLatestCumulativeTarget` 覆盖 fill helper 的 durability/累计目标；没有 stream routing → retry → bootstrap 链路。
- `order/intent_journal_test.go`：`TestSettleIntentAcceptsExchangeBrokerPrefix`、`TestSettleZeroFillIntentRejectsLateVenueFill`、`TestSettleZeroFillIntentRejectsFilledStatusWithZeroQuantity`、`TestSettleZeroFillIntentRetriesTransientVenueFailure`、`TestSettledIntentAllowsVerifiedRestart`、`TestSettleReconciledIntentIsIdempotentAfterRestart`、`TestIntentJournalOlderAcknowledgementsCannotEraseFills`、`TestTradeLedgerFailurePersistsIntentReconciliationHoldAcrossRestart` 与 `TestTradeLedgerRecoveryReplaysDurablePayloadBeforeOpening` 覆盖局部契约；尚未连起 normal settle 后同 executor rebind/bootstrap retry 与生产 callback。
- `order/opening_gate_test.go`：`TestOpeningGateAtPhysicalExecutor`、`TestOpeningGateBatchAndResume`、`TestOpeningAdmissionGuardRunsOnlyForOpeningsAndFailsClosed` 覆盖部分 opening/close 区分。仍需确认恢复 gate 的具体 runtime 接线不阻止保护性 close。

## 建议新增测试（名称均待实现）

建议新增主接线测试文件 `main_order_recovery_callback_test.go`（root package，调用可注入的 production callback seam）；executor/journal 局部回归放 `order/intent_journal_test.go`。如果 seam 最终放在别的 package，测试文件随代码归属调整。

| 优先级 | 建议测试名／文件 | 关键断言 |
|---|---|---|
| P1 | `TestNormalSettledIntentAllowsSameExecutorBootstrapRetry` — `order/intent_journal_test.go` | normal `SettleIntent` 后以同 executor、同 backend/scope 重入 configure/bootstrap；settled 项保留 owner map，确认安全 no-op。另建表驱动多 intent 场景：存在一个未结项时 retry 不清 `journalLoaded`/不清 gate，后续事件仍可持久观察并 settle，所有项完成后再成功 Seed。不同 backend/scope 必须拒绝。不得靠放宽不同绑定防护通过。 |
| P1 | `TestOrderCallbackTerminalFillPersistsLedgerSettlesAndRetriesBootstrap` — `main_order_recovery_callback_test.go` | fake stream 保存并触发 production callback；精确 owner/symbol/strategy 路由。按 D-004 决策验证：若纳入顺序重构，fills/fees durable → strategy accounting durable → intent settle durable → 同一 coordinator retry；若暂缓，则如实测试 strategy-first crash window，验证重启重放不重复记账且 fill/fee ledger 最终完整，然后 full bootstrap/Seed。不能只断言调用返回 nil。 |
| P1 | `TestGridZeroFillCallbackSettlesThenRetriesBootstrap` — `main_order_recovery_callback_test.go` | grid owner 零成交终态先经 fresh exact venue/zero-qty 核验与 grid state durable，再 `SettleZeroFillIntent`、完整 retry/Seed；不制造 fill，重复事件不重复结算。依赖 zero-fill 架构定稿。 |
| P1 | `TestNonGridZeroFillCallbackSettlesThenRetriesBootstrap` — `main_order_recovery_callback_test.go` | non-grid durable route 仅在策略明确 durable accounting 成功后 settle/retry；route 缺失、策略未启用或 accounting 不可确认时不 settle、不 Seed、gate 保持。依赖 settlement 契约定稿。 |
| P1 | `TestPartialFillTerminalCallbackPersistsAllFillsAndFeesBeforeSettle` — `main_order_recovery_callback_test.go` | 部分成交后终态的 exact fills、唯一 trade ID、累计量、费用/资产完整；strategy inventory/funds/borrow 与 fill ledger durable 后才 settle；剩余活动单仍按 fail-closed 契约处理。缺证据不得 Seed/解 gate。 |
| P1 | `TestFillLedgerFailureKeepsIntentAndBootstrapGatesBlocked` — `main_order_recovery_callback_test.go` | 注入 fill writer 错误；intent 不 settle、bootstrap 不成功、book 不 Ready，execution/bootstrap gate 留存；未知外部副作用不得降格为未发送。 |
| P1 | `TestStrategyAccountingFailurePreventsIntentSettlementAndBootstrap` — `main_order_recovery_callback_test.go` | 注入策略 durable accounting 失败；callback 暴露错误，不能宣称 intent/经济账完整，不能成功 Seed，相关 gate 留存。 |
| P1 | `TestVenueTerminalOrFillQueryFailureKeepsRecoveryBlocked` — `main_order_recovery_callback_test.go` | exact order/fills/open-order/position 查询分别失败、unknown 或不匹配时不 settle/Seed/解 gate；修复后以 durable cursor 安全重试。 |
| P1 | `TestDuplicateAndOutOfOrderTerminalUpdatesAreIdempotent` — `main_order_recovery_callback_test.go` | terminal 后重复事件及旧 PARTIALLY_FILLED/NEW 不得重复入账、回退累计量、重复策略变更或错误解 gate；覆盖并发重复 callback。 |
| P1 | `TestRestartBetweenAccountingLedgerSettleAndBootstrapRetry` — `main_order_recovery_callback_test.go` | 用临时 SQLStorage 对策略账后、fill ledger 后、intent settle 后、bootstrap 失败后分别模拟重启；依 journal/venue/cursor 幂等续作，不重计费用/交易、不丢 UNKNOWN、不 Seed 不一致 inventory。可 table-driven 切点。 |
| P1 | `TestConcurrentCallbackAndExplicitBootstrapRetriesSerialize` — `main_order_recovery_callback_test.go` | 多个重复终态 callback 与 explicit retry 并发；attempt 不重叠、Seed 至多一次、持久变化具幂等性。用可控阻塞/取消 adapter 检查 lease 释放及后续 retry 可进入，不把尚未证实的死锁当既定事实。 |
| P1 | `TestBootstrapRetryFailureRetainsGateUntilFullSeedSucceeds` — `main_order_recovery_callback_test.go` | snapshot、inventory loader、Seed 前验证失败均保持 gate；完整 Seed 成功只清恢复流程自己的 gate，其他 gate source 仍阻止 opening。 |
| P1 | `TestRecoveryGateBlocksOpeningButAllowsProtectiveReduceOnlyClose` — `main_order_recovery_callback_test.go` 或 `order/exposure_integration_test.go` | 同一 production-configured physical executor、book 未 Ready/recovery gate 存在时，opening 不抵达 venue，futures 明确 leg 的 ReduceOnly close 仍抵达 fake venue。若测 spot close，以精确 owner inventory/leg 证明减仓，不能以错误标签绕过 gate。 |

## 事件与失败矩阵

| 输入／故障 | 预期 |
|---|---|
| owner 正成交 FILLED | 策略账与 fills/fees 均耐久且重启可收敛后才 settle，再 full bootstrap；具体先后按 D-004 决策。 |
| 部分成交后取消／过期 | exact venue 累计 fills 与费用完整，剩余活动量终态已核实，才可 bootstrap；不能只信单个 websocket 状态。 |
| grid 零成交终态 | exact venue 零量、grid 状态 durable 后 zero-fill settle 与 full retry；不生成成交记录。 |
| non-grid 零成交终态 | durable route/accounting 明确确认后 settle/retry；不支持/未知策略 fail-closed。 |
| unknown、外部 owner、错 symbol/CID/order ID/side/qty | 不归属本 bot，不 settle、不 Seed，gate 保持。 |
| 重复、乱序、并发更新 | 以 trade ID、累计量、journal revision、exact venue order 防重复与回退。 |
| venue、storage、strategy、snapshot 任一失败 | 保持 UNKNOWN/对应 gate，不成功 Seed；故障恢复后可安全重试。 |
| Seed 成功但其他 gate 存在 | 仅清恢复流程 owner 的 block；其他来源仍封锁 opening。 |
| recovery gate 存在时保护性 close | opening 被拒；futures ReduceOnly close 可提交；不能放宽其他增险路径。 |

## 执行顺序、建议命令与完成标准

1. 等 GT-003 定稿 same-binding journal 幂等再入、grid/non-grid zero-fill callback 与可注入 production callback seam；正成交记账顺序决策 D-004 待用户回答，QA 不预设接口。
2. 先做同 executor P1 回归，并证明 unresolved/UNKNOWN guard 不退化。
3. 建立 production callback fixture，串起 fills/fees、strategy accounting、settlement、coordinator 与 bootstrap；扩展正成交、部分成交、两种 zero-fill 及故障矩阵。
4. 增加重启、重复/乱序、并发/取消、gate ownership 与 protective close 覆盖。
5. 建议的针对性命令（未执行；实际测试名落地时可调整）：
   - `go test . -run 'Test(OrderCallback|GridZeroFillCallback|NonGridZeroFillCallback|PartialFillTerminalCallback|FillLedgerFailure|StrategyAccountingFailure|VenueTerminalOrFillQueryFailure|DuplicateAndOutOfOrderTerminalUpdates|RestartBetweenAccountingLedgerSettleAndBootstrapRetry|ConcurrentCallbackAndExplicitBootstrapRetries|BootstrapRetryFailure|RecoveryGate)' -count=1`
   - `go test ./order -run 'Test(NormalSettledIntent|SettleIntent|SettleZeroFillIntent|ConfigureIntentJournal|RecoveredIntent)' -count=1`

Done Criteria：真实 callback 因果链可观测；同 executor P1 retry 测试通过且 unresolved fencing 保留；grid/non-grid zero-fill 各自覆盖；正/部分成交 ledger 与费用先于 settle；重复/乱序/重启/并发及失败均 fail-closed 且可安全重试；完整一致 Seed 后 book 才 Ready；仅解除本流程 gate；恢复封锁期间 protective reduce-only close 仍可用。helper 单测、mock nil 返回或测试全绿均不等于实盘／盈利验收。

## 本次文件验证记录

- 只读检查：仓库 `AGENTS.md`、Goal Teams skill/runtime reference、requirement card、independent review，以及订单 callback、settlement、fill capture、bootstrap 与相关测试符号。
- 本轮只新增本 `test-plan.md`；未改生产/测试代码及其他 Goal Team 文件，未启动代理，未连接账户/生产 DB，未发单/撤单。
- 未执行 Go 测试；该交付为计划文档。对新文件执行 `git diff --no-index --check -- /dev/null <file>`，无空白错误（该命令因比较新增文件返回差异状态）。
