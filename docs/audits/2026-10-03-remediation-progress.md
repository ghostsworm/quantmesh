# 实盘准备度整改进度续篇

历史记录见 [原整改进度](2026-09-24-remediation-progress.md)。此处继续原 R01–R15 范围，不代表范围缩减或真实盈利验收。

## 2026-10-09 R12：网格终态成交耐久结算（rc1152）

- 复核发现生产回调仅在策略管理器确认的子策略成交后调用 `SettleIntent`；网格虽然已在 `OnOrderUpdate` 持久化槽位库存、并由 `runtimeFillCapture` 持久化完整成交明细，却没有结算 grid owner 的执行 intent。该记录在重启加载时会按未结算意图转为 UNKNOWN，令 `ConfigureIntentJournal` 返回恢复未完成并阻断 bootstrap。
- 网格回调现在将“终态网格槽位账本已成功耐久保存”与“完整 venue 成交明细已耐久保存”作为结算前置条件；只按 journal 记录内的 grid owner 调用 `SettleReconciledIntent`，精确重查终态后写入 settled。成交明细核对和 intent 结算期间保留对应开仓 gate；结算后才重试全账户暴露核账。网格会计快照写入失败、费用/账本锁、身份不符或交易所复核失败均不能走成功路径。
- 新回归验证：网格正成交仅在 durable accounting 确认后结算，MySQL/SQLite intent journal 重启读取不再将已结算网格订单恢复为 UNKNOWN；position 测试验证运行态快照持久化失败不会报告 accounting verified。尚需完整验证生产事件流和 race；本修复不闭合活动挂单恢复、SpotLong/Futures hedge 完整手续费/经济账、跨进程 fencing 等 R12/R09 缺口，不代表真实交易或盈利验收。

## 2026-10-09 R12：SpotShort 启动对账故障可自动重试（rc1151）

- `SpotShortStrategy.Start` 在运行态可正常解码、但首次借款历史/订单终态/成交费用/还款核对失败时，原本直接返回错误；`StrategyManager.StartAll` 因此不会保留该策略的重试 worker。即使外部依赖随后恢复，也需重启 Bot 才重试。
- 现仅当生产注入了共享 `OpeningGate` 时，SpotShort 才允许带着 `spot_short_reconciliation_unverified` 专属封锁启动，并启动后台重试；成功核完全部待办后只解除该专属来源，再请求全账户暴露核账。无 gate 的构造方式仍拒绝继续，运行态解码/版本/身份错误仍拒绝启动；通用账本持久化、runtime ownership 与其他风控封锁不会被清理。
- 新增生产策略启动路径回归：首次精确订单查询注入暂时失败，验证 Start 不要求 Bot 重启、gate 持续封锁、worker 后续精确恢复并结算后才解除专属封锁。更广泛 R12/R09（SpotLong 与 Futures hedge 经济账、共享 exposure 成本/费用、活动委托恢复、跨进程 fencing）仍未闭合；完整费用、借贷账本和盈利证据未验收。
- 验证：`go test ./strategy -run '^TestSpotShort(StartupReconciliationFailureRetriesWithoutRestart|StartupRecoversPreparedBorrowFromUniqueConfirmedHistory|TerminalBorrowSettlementOutboxRetries|RuntimeReconciliationRetriesPendingBorrowOrder|StartupKeepsAmbiguousBorrowHistoryBlocked)$' -count=1`、新启动重试用例的 `go test -race`、全仓 `go test ./... -count=1 -timeout=600s` 与 `go vet ./...` 均通过；`yarn verify` 通过（53 个测试文件/311 项测试及生产构建），`ruby scripts/frontend_embed.rb sync/verify/version` 通过并核实 `3.111.0-rc1151`，`git diff --check` 通过。未执行全仓 race 或强制 MySQL 门禁；未访问真实账户、下单或部署。

## 2026-10-09 R12：SpotShort 借贷卖单终态结算 outbox（rc1150）

- SpotShort SELL 回报由同步策略回调明确标记为 deferred，避免通用终态路径在借贷账尚未核实前结算共享执行意图。耐久恢复器在验证借款归属、client order ID、交易对、方向、请求数量、终态及足额成交后，先保存包含终态订单证据的 `settlement_pending` outbox，再向 owner-scoped executor 结算；仅在 journal 成功后确认删除 outbox。结算失败或进程在两步间重启时保留待办并可重试；已结算 journal 记录支持精确所有者范围内幂等确认。
- 运行时全部 SpotShort 借贷/买回/还款待办清空后，只释放 SpotShort 专属对账 gate 并触发串行账户暴露核账重试；运行态持久化失败使用的通用策略账本 gate 不被此回调清理。未核实的部分成交、剩余借贷、身份不符及 journal 错误仍 fail-closed。
- 验证：`go test ./strategy ./order ./position . -run 'TestSpotShort|TestSettleIntent|TestRecoveredIntent|TestApplyOrderUpdateForStrategy' -count=1` 与 deferred accounting/outbox/journal 重启的定向 `go test -race` 通过；最终 `go test ./... -count=1 -timeout=600s` 全包通过，`go vet ./...` 通过；`yarn verify` 通过（53 个测试文件、311 项测试及生产构建），`ruby scripts/frontend_embed.rb sync/verify/version` 通过并核实嵌入版本 `3.111.0-rc1150`，`git diff --check` 通过。未运行全仓 race 或强制 MySQL 门禁；完整成交费用与借贷账本、线上账户权限、部署和可审计盈利证据仍未验收。

## 2026-10-09 R12：策略恢复后的 UNKNOWN intent 受限结算（rc1145）

- 生产恢复调用链在 DCA、趋势/均值回归/动量信号及马丁格尔策略中，现仅于精确 venue 订单核对完成、策略 `OnOrderUpdate` 成功持久化经济状态后，才按 client order ID 通知对应策略 adapter 结算 UNKNOWN intent。物理 executor 再次核对 journal owner、终态订单身份/数量并持久化 settled 状态；`IntentRecoveryBlock` 保留到随后全 Bot 策略库存、venue 持仓和挂单 bootstrap 成功。
- 终态标签与成交数量矛盾时不能结算：FILLED/FULLY_FILLED/CLOSED 必须与请求数量在交易所精度容差内一致；UNKNOWN、部分成交、开放订单、查询/成交费用/账本持久化错误继续阻止启动恢复。
- 实际恢复方法 race 用例覆盖 DCA/信号/马丁格尔正例与 underfilled-FILLED 负例；`go test ./strategy -count=1 -timeout=360s`、根包 `TestRuntimeExposureBootstrapRetriesAfterStrategyRecoverySettlesIntent` race、`go vet ./...` 均通过。最终工作树 `go test ./... -count=1 -timeout=600s` 通过（无 MySQL DSN）；`yarn --cwd webui verify` 的 TypeScript、53 个测试文件/311 项测试及生产构建通过，`ruby scripts/frontend_embed.rb sync/verify` 通过，嵌入版本 `3.111.0-rc1145`。未执行全仓 race / 强制 MySQL 门禁，也未验证交易所线上权限、生产部署或盈利。
- 尚未接入/验证 SpotLong、Combo 子策略、Futures hedge 与网格恢复器的受限 settlement；任何活动挂单仍会令启动 exposure bootstrap fail-closed，且终态到达后的运行时自动重试 bootstrap 尚未证明。因此 R12/R09 整体仍未闭合，本次不得作为全面恢复验收。

## 2026-10-09 R12：保留 Combo 子策略恢复结算能力（rc1146）

- 实际接线：`ComboStrategy` 将同一个 `comboExposureAdmissionExecutor` 传给 DCA、信号与马丁格尔子策略；该 wrapper 之前没有实现 `SettleRecoveredIntent`，使 rc1145 的子策略恢复结算接口在 Combo 路径被隐藏。现在仅将 client order ID 和 context 透传到底层 owner-scoped adapter；底层不支持时返回错误，防止恢复器误认为已结算。
- 针对性测试覆盖 context/client ID 原样透传、底层错误传播及不支持时 fail-closed。其余 R12 缺口（SpotLong、Futures hedge、网格恢复、活动挂单/终态后的运行时重试）仍未闭合；此改动不扩大交易或开仓权限，也不构成实盘或盈利验收。

## 2026-10-09 R12：Futures hedge 不完整 FILLED 保护（rc1147）

- `futuresHedgeOrderTracker.OnOrderUpdate` 与启动 `RestoreAndReconcile` 原先只要求 `FILLED.ExecutedQty > 0`，没有与耐久请求数量比较；underfilled-FILLED 可以清除本地 pending，之后通用终态回调可能进一步结算共享 intent。
- 两条生产接线现在要求累计成交量在数值容差内覆盖请求量，否则返回错误且不清理 tracker pending。定向测试验证实时更新和重启 REST 快照均保留 pending；仍未把 Futures hedge 的完整成交费用/策略库存恢复接入 R12，也未关闭活动订单导致的启动封锁或运行时自动 exposure bootstrap 缺口。
- 验证：`go test ./strategy -run '^TestFuturesHedge(Tracker|Restore)' -count=1`、同范围 `go test -race`、`go test ./strategy -count=1 -timeout=360s`（183.717s）、`go vet ./strategy` 通过；前端嵌入 `build/sync/verify` 通过，版本为 `3.111.0-rc1147`。未运行全仓 Go/race/MySQL 门禁，也未连接真实账户、部署或验收盈利。

## 2026-10-09 R12：策略终态后的运行时暴露核账重试（rc1148）

- 生产链路此前在策略 `StartAll` 后只重试一次 bootstrap；若当时 venue 仍有活动挂单，启动封锁保持有效，但挂单后来进入策略已核账的终态且 intent 耐久结算成功后，不会再触发整体持仓/库存核账。
- 新增串行协调器：策略恢复就绪后才允许 retry；启动后的失败可由后续成功执行 `settleVerifiedStrategyIntent` 的策略终态回调再次触发。任一尝试失败不标记完成，保留 `runtimeExposureBootstrapBlock`；成功时仍须重新加载所有策略库存、核对持仓/挂单、播种共享 exposure 并由原有 bootstrap 流程释放其专属核账阻断。测试覆盖 ready 前不执行、失败后可重试、并发回调仅一次成功播种尝试及成功后幂等不重跑。
- 范围限制：不会自行撤单或恢复活动订单；只在某个策略订单账本和 intent 结算成功后尝试全局核账。未覆盖没有 `strategyAccountingVerified` 的网格正成交、SpotLong、Futures hedge 完整手续费/库存接线；若没有后续成功结算事件仍可能长期 fail-closed，不能视为 R12/R09 全闭环。
- 验证：定向 `go test . -run '^(TestRuntimeExposureBootstrapCoordinatorRetriesAndSerializes|TestRuntimeExposureBootstrapRetriesAfterStrategyRecoverySettlesIntent)$' -count=1` 与同范围 `-race` 通过；全 `go test ./... -count=1 -timeout=600s` 通过（策略包 184.679s）；`go vet ./...`、`yarn verify`（53 文件/311 项测试及生产构建）、`ruby scripts/frontend_embed.rb sync/verify/version` 均通过，嵌入版本为 `3.111.0-rc1148`。`git diff --check` 通过。未运行全仓 race / MySQL 强制门禁；未连接真实账户、下单、部署或验收盈利。

## 2026-10-09 R12：SpotLong 拒绝不足额 FILLED（rc1149）

- SpotLong 的运行态 `OnOrderUpdate` 原先仅拒绝零成交 FILLED；不足额的正成交 FILLED 会进入终态分支、清除耐久 pending，重启 REST 快照也会进入同一个更新方法。由于主订单回调可能继而尝试结算该 owner intent，这是不一致成交数量越过恢复保护的风险。
- 现在 FILLED 累计量必须覆盖耐久请求数量（数值精度容差内）；不足时在修改/持久化 pending 前报错，保留已有累计成交证据与开仓保护。实时和 Start→REST reconcile 两条路径均有回归测试。
- 边界：没有接入 SpotLong 的成交费用/资产归属耐久经济账；此补丁只阻止矛盾的 FILLED 状态清空 pending，不允许据此认定完整 SpotLong 恢复或盈利核算闭环。
- 验证：`go test ./strategy -run '^TestSpotLong(UnderfilledFilledOrderRetainsDurableBlock|RestoreRetainsUnderfilledFilledOrder)$' -count=1` 及同范围 `-race` 通过；完整 `go test ./strategy -count=1 -timeout=360s` 和全仓 `go test ./... -count=1 -timeout=600s` 通过（全仓策略包 185.011s）；`go vet ./...`、`yarn verify`（53 文件/311 项测试及生产构建）、`ruby scripts/frontend_embed.rb sync/verify/version` 均通过，嵌入版本 `3.111.0-rc1149`；`git diff --check` 通过。未运行全仓 race / MySQL 强制门禁；未连接真实账户、下单、部署或验收盈利。

## 2026-10-09 主线风险顺序复核（rc1143 后；仅核对当前程序与定向测试）

- 按原建议顺序复核 R01–R04、R12、R13。R01 的资金费/趋势联动不再清除硬开仓限制；R02 的同一 `OpeningGate` 已接到网格及策略实体执行器；R03 触发价只阻止新增风险，既有持仓保护另有路径；R04 有核实型平仓状态机。定向 race 验证：`go test -race . -run '^TestBotOpeningGateReachesGridAndAllStrategyAdapters$' -count=1`、`go test -race ./position -run '^(TestFundingTrendPreservesEveryHardOpeningConstraint|TestLiquidateAllVerified_UnfilledLimitCancelledThenMarketResidual|TestLiquidateAllVerified_PartialFill|TestManualPauseKeepsProtectivePositionManagementActive)$' -count=1` 均通过。测试支持所列入口/场景，不代表所有策略、交易所或故障时序都已覆盖。
- R13 的开发免登入只对直连回环请求有效，服务启动时拒绝 public bind；首次设置在认证管理器缺失、数据库查询失败、已安装但数据库失败或远端匿名请求时均拒绝变更。`go test -race ./web -run '^(TestDirectLoopbackRejectsProxiesAndSpoofedHeaders|TestInstalledSetupRequiresSessionEvenOnLoopback|TestLocalDevServerRejectsPublicBind|TestSetupMutationAuthenticationFailsClosed)$' -count=1` 通过。仍未做生产反向代理/实际部署配置验证。
- R12 尚未闭合为可恢复流程：`configureRuntimeIntentJournalWithGridRecovery` 对加载后仍未核账的自有意图返回 `ErrOrderUnknown`；`bootstrapRuntimeExposure` 对任何非空启动挂单快照拒绝播种；`symbol_manager.go` 只记录 recovery incomplete 并保留开仓 gate。这能避免未核清时继续加仓，但目前没有把这些自有订单重新接入耐久 exposure reservation 并完成策略经济核账后的恢复闭环。不得清除标记或手动解除 gate 冒充修复。`go test -race ./execution ./order -run 'Exposure|OpeningGate' -count=1` 仅验证现有预留/封锁语义，不证明重启恢复。
- 下一步实现目标应是 R12/R09 的受控 owner-scoped 恢复：精确绑定持久意图、venue 订单/成交、策略仓位与资本占用；只有读取完整、身份一致、成交账本耐久且所有剩余委托状态已核实，才可让新风险重新进入准入。无法完成经济归属时必须维持保护性退出能力及封锁，并提供可操作的核对状态；不把长期封锁写成风险已解决。
- 本次只新增审查记录，没有修改交易实现、版本、标签或 main；没有连接交易所、下单、部署或验收盈利。

## rc1143：退役账户封锁原因可观测性

- 生产周期核验原先把 unsupported 市场、观测器工厂失败、REST 查询失败及显式不完整响应统一持久化成 `incomplete`；管理员 UI 虽返回 `last_evidence_result`，但页面没有展示。Bitget Spot 等受支持范围外账户因此可能长期保持 fail-closed，却不给操作者可区分的修复方向。
- 现在仅保存有限原因码：`unsupported_market`、`observer_unavailable`、`query_failed`、`incomplete`、`open_exposure`、`flat`；不保存交易所原始错误正文/凭据。管理面板新增最近核验结果的三语展示。所有失败/不完整/非零状态继续清空连续 flat 计数，只有完整的 flat 结果可累计；UI 显示不改变服务端 reset 门槛。
- 定向根包回归覆盖四类结果与“错误详情不入库”；旧 flat/非 flat 结果仍可解码。前端回归覆盖原因码到 i18n key 的安全映射。该修复改善故障定位，不自动修复不支持的 Bitget Spot verifier，也不解除其封锁。
- 最终源码验证：3 个退役账户定向 race 用例通过；根包完整 `go test . -count=1` 通过；`go vet .`、`git diff --check`、embed sync/verify 均通过。前端 `yarn verify` 的 TypeScript、53 个测试文件/311 项测试与生产构建通过，前后端/嵌入版本一致为 `3.111.0-rc1143`。未运行全仓 `--require-mysql`，未连接真实交易所。
- 尚缺：前端真实管理员浏览器 E2E、交易所只读权限/端点线上覆盖、Bitget Spot 债务与持仓完整证据、完整发布验证及部署后复核；不连接真实账户、不下单，也不宣称实盘或盈利验收。

## rc1142：退役账户重置 MySQL 基线后审计失败恢复验证

- 新增 `TestMySQLRetiredResetRecoversAfterBaselineBeforeAuditAcrossRestart`，在独立 checkpoint namespace 下跑实际 BotManager/resetter/SQLStorage/PauseCoordinator 接线；注入一次审计 checkpoint 写入失败，覆盖基线 CAS 已耐久、账户归档仍待处理的故障窗口。断言失败时 reset intent/旧账户归档/开仓 hold 均保留，随后关闭并重建 storage/coordinator，确认 hold 从 MySQL 恢复；同一管理员与目标 scope 重试复用 operation ID、幂等读取已经提交的新权益 baseline，完成 audit、清理旧密文并只在完成后移除持久 hold。
- 该用例只使用本机新建的专属空 schema `quantmesh_audit_rc1142_20261009`（MySQL 9.5）和只读观察器桩，不访问真实账户。定向命令 `QUANTMESH_MYSQL_TEST_DSN=... go test . -run '^TestMySQLRetiredResetRecoversAfterBaselineBeforeAuditAcrossRestart$' -count=1 -v` 通过；测试后已删除该专属 schema，并确认其不存在。rc1142 最终源码 `go test ./...` 通过（未设置 MySQL DSN，因此其他 DSN 门控用例未运行），`go vet ./...` 通过；`yarn --cwd webui verify` 通过 TypeScript、53 个测试文件/310 项测试与生产构建，随后 embed sync/verify 通过，后端/前端/嵌入版本均为 `3.111.0-rc1142`、216 个资源。首轮无提升权限的全仓测试因沙箱禁止 `httptest` 绑定 loopback 失败；同命令在本机权限下重跑通过。此轮未重跑 MySQL 8.0 全仓/race gate，rc1141 的 MySQL 8.0 与 race 证据不等于 rc1142 同提交证据；仍不代表交易所 verifier 的线上权限/覆盖、生产部署或盈利验收。

## rc1141：退役账户密文归档 MySQL 重启恢复验证

- 新增 `TestMySQLRetiredEquityArchiveEncryptsAndSurvivesRestart`：要求 MySQL DSN 为 loopback、root 无密码且仅允许测试 schema；使用独立 checkpoint 前缀，直接走 `SQLStorage` 的风险 checkpoint 实现。验证旧凭据不以明文出现在持久 payload、关闭并重新创建 MySQL storage 后凭配置主密钥解密，恢复出的旧账户凭据、market 与 scope 均匹配。清理只删除本测试唯一 checkpoint key。
- 在 `mysql:8.0.36` 随机 loopback 端口 / tmpfs 一次性容器上，定向用例通过。rc1141 账户归档实现全仓 `go test ./...`（含该 MySQL 用例）通过；正式 `scripts/verify_trading_race.rb --require-mysql` 通过，2311 项 race 测试与全部 19 个必需 MySQL 场景通过，报告位于 `/private/tmp/quantmesh-trading-race-rc1141-20261009`。前端 `yarn typecheck`、53 files / 310 tests、`yarn build` 均通过，embed sync/verify 成功，当前资源版本 `3.111.0-rc1141`、216 个资源；`go vet . ./risk ./web` 和 `git diff --check` 通过。race/full-suite 后仅将新增测试的失败诊断改为不打印 fixture 凭据；该最终改动后的定向 MySQL 用例再次通过。测试没有访问真实交易所，不构成生产密钥轮换/部署或盈利验收。

## rc1140：耐久权益 checkpoint 的 MySQL CAS 整合测试

- 新增 `TestMySQLRiskCheckpointDurableCAS`，只在明确提供 `QUANTMESH_MYSQL_TEST_DSN` 时执行；使用唯一 checkpoint key，验证显式建表迁移、缺失/首次状态、首次 CAS、重复初始化冲突、12 个并发旧 revision 写入仅一个成功、重载后 payload/revision，以及非法 JSON 拒绝。CI MySQL 服务会自动执行此测试。
- 本机验证使用 `mysql:8.0.36` 一次性容器，仅映射 `127.0.0.1` 随机埠、数据目录为 tmpfs、无 host volume；为执行仓库要求的 schema DDL 用例，显式设置 destructive-schema opt-in，但目标仅为该容器及其随机独立测试 schema。全仓 `go test ./...` 通过；`scripts/verify_trading_race.rb --require-mysql` 通过，2310 项 race 测试通过，必需 19 个 MySQL 场景均有通过证据，报告位于 `/private/tmp/quantmesh-trading-race-rc1140-20261009`。这验证隔离 MySQL 下的存储、恢复和交易关键调用路径，不等同生产权限/部署后配置验收或真实账户交易/盈利验收。
- rc1140 前端 `yarn typecheck` 通过、`yarn test` 53 files / 310 tests 通过、`yarn build` 通过；embed sync/verify 成功，嵌入版本为 `3.111.0-rc1140`（216 个资源）。`go vet . ./risk ./web`、`git diff --check` 通过，后端 `main.go` 与前端 package 版本一致。未连接真实交易所或真实账户，亦未下单；Bitget Spot 的完整 flatness verifier 仍不支持，故其退役账户继续保持无法重置的 fail-closed 状态。

## rc1139：退役账户重置管理界面

- 全局安全设置页新增退役账户状态与 reset audit 读取；只展示交易所/市场、核验状态、零暴露观察计数和最后观测时间，不显示账号凭据、scope hash 或账户 ID。仅服务器管理员 API 授权成功后显示数据，403 明确提示管理员权限，其他读取失败不伪装为空列表。
- 前端仅在列表非空且每条记录均为 `ready_for_explicit_reset` 并具备两次 flat 证据时启用按钮；另需勾选确认并经二次对话框提交。后端再次执行 fresh 旧账户只读查询、新 scope 完整权益核账、CAS 与耐久审计；前端禁用门槛不是安全边界，失败仍提示保留封锁并展示服务端返回的 pending operation，可由同一管理员重试。状态/历史读取失败或 403 时不再显示“无历史”等空状态。
- 最终源码验证：前端 `yarn typecheck` 通过、全量 `yarn test` 53 files / 310 tests 通过、`yarn build` 通过；`frontend_embed.rb sync` 后 `verify` 成功，当前嵌入版本为 3.111.0-rc1139、包含 216 个资源。新服务层测试确认状态/历史/POST reset 均经 same-origin authenticated transport；组件测试覆盖证据门槛及读取前不伪报空历史。Go 最终版本下 `go test . ./risk ./web` 通过；更早 rc1138 全仓 `go test ./...` 通过，但不是本 rc1139 精确源码指纹的整仓复跑。Bitget Spot 仍不支持完整 flatness verifier，永远不能通过此重置按钮门槛；仍缺浏览器交互 E2E、严格 MySQL、部署后权限/主密钥配置验收。未连接真实账户或下单，不代表盈利或实盘验收。

## rc1138：管理员旧账户显式重置工作流（Spot 仍 fail-closed）

- `POST /api/capital/retired-equity-accounts/reset` 只允许已认证 admin，拒绝 local-dev fallback；待办 intent 在任何账户复核/权益基线操作前耐久写入 checkpoint，并绑定 admin username、随机 operation ID、目标 scope、旧账户 hash 列表。管理员重试复用同 actor/scope 的 pending op；另一个 actor 或 scope 不能接管。
- 工作流在配置 scope 刷新锁内确保独立退休账户开仓 hold、同步重新查询所有 retired accounts、要求每条完整 flat 证据仍达门槛，再调用 feeder 的逐钱包完整证据 CAS baseline 操作；操作 checkpoint CAS、reset audit/旧凭据清理 CAS 完成之后，才用 `ReleaseChecked` 解除该 hold。任意失败留下 durable intent/hold；baseline ACK 不确定可按 operation ID 幂等重试，审计已完成但 release 失败可再 POST 收敛。
- completed audit 记录 actor、目标 scope、旧账户哈希、完成时间及 equity checkpoint revision；`GET /api/capital/retired-equity-accounts/reset-history` 仅 admin 可读。相同账户 scope 的凭据轮换只刷新当前账本证据并保留原 high-water，避免把 API secret 轮换误作新账户重置盈利基线。
- 定向根包/Web/risk 回归覆盖 store intent/retry/complete/secret removal、管理员授权、完整 reset 与持久 pause release、baseline idempotency/credential-only HWM。Bitget Spot 未支持 verifier，始终 incomplete 并拒绝重置。完整 race/Go/MySQL/前端门禁及部署后配置权限验证仍缺；本轮不访问真实交易所/账户或下单。

## rc1137：退役账户持续平仓核验计数饱和

- 生产 verifier 每分钟在两次 flat 样本后继续递增 `FlatEvidenceCount`，第三次写入会保存超过 validator 上限的记录；后续 load 失败会令 retired-account 状态/API/启动恢复进入不可用封锁。现在计数饱和于 `retiredEquityFlatEvidenceRequired`，之后的完整 flat 样本只刷新 `LastFlatAt`，非零/不完整仍清零连续计数。
- 每次证据更新在持久化前重新验证变更后的记录，避免其他状态机回归把不可读状态写入数据库。回归在到达 ready 后连续增加四次 flat 样本并逐次 reload，再验证非零样本仍转回 pending。

## rc1136：权益基线显式 CAS 重置底座（尚未接入账户重置工作流）

- 新增 `MetricsFeeder.ResetEquityBaseline`：要求现有 durable checkpoint、持久化与现金流水核账配置、AccountEquitySource、调用方给定操作 ID 和目标 scope；方法在 feeder 锁内重新读取 checkpoint 并取得 cursor 为空的新范围逐钱包观测，检查 scope、完整度、钱包证据和 freshness 后以旧 revision CAS 写入新基线。
- checkpoint revision 单调递增并持久化 reset operation ID；相同操作在数据库提交成功但 ACK 丢失后可幂等读取成功结果。不同操作遇到已切换的新 scope 会拒绝覆盖；CAS/证据失败不会发布新缓存，CAS 不确定失败会令下一 tick 重载权威状态。
- `go test -race ./risk -run 'Test(ResetEquityBaseline|EquityScopeInvalidationReloadsCheckpointWithoutDroppingPriorState|FailedMetricsTickInvalidatesDurableEquityCache|MetricsFeederLoadsExplicitlyResetScopeAfterOldScopeFailure)' -count=1` 与根包退役账户轮换/重启/暂停定向 race 回归、`go vet . ./risk ./web`、版本一致性和 `git diff --check` 通过；完整 Go/MySQL 门禁未执行。本底座不解除旧账户 hold，不验证 retired-account readiness，不写操作者 audit，不清理密文；这些必须由后续 durable 工作流先后接线。Bitget Spot 仍 incomplete，不能完成其退役记录重置。

## rc1135：权益范围切换后的 feeder checkpoint 重新加载

- `MetricsFeeder.InvalidateEquityScope` 在串行化 scope publish 前使持久化 cache 标记失效，但保留旧 checkpoint 对象，确保持久化 checkpoint 若确实消失仍按错误封锁，不会被误判成首次启动。
- 任何失败 tick 也使 durable cache 在下一个 tick 前失效；因此外部经授权完成 CAS checkpoint 重置后，运行中 feeder 能重新加载新 scope，而不会永久以旧内存状态循环报 scope mismatch。race 回归以内存持久 store 人工推进 CAS revision，证明 scope invalidation 后 feeder 读取新 scope 并从新 state 继续，且失败时保留旧 state 证据。
- `go test -race ./risk -run 'Test(EquityScopeInvalidationReloadsCheckpointWithoutDroppingPriorState|FailedMetricsTickInvalidatesDurableEquityCache|MetricsFeederLoadsExplicitlyResetScopeAfterOldScopeFailure)' -count=1`、根包 retire-account race 定向回归、Web 状态 API race 测试、`go vet . ./risk ./web`、前后端版本一致性和 `git diff --check` 通过；完整 Go/MySQL 门禁仍未执行。本 rc 后续新增底层 reset 原语，rc1135 本身未包含重置实现。

## rc1134：管理员可见退役账户证据状态（仅只读 API）

- 增加 `GET /api/capital/retired-equity-accounts`，要求已认证管理员且拒绝 local-dev fallback。返回 sha256 账户身份、交易所/市场、account scope hash、当前状态、累计次数和观测时间，不返回 API key/secret/passphrase/ciphertext。读取或解密失败回 503，而非伪装空列表；失败同时打服务端日志。
- BotManager 读取归档前会校验主密钥及记录完整性；Web 层通过独立 reader 注册，避免向通用公开 JSON 暴露凭据。测试覆盖未登录/普通用户/local-dev 拒绝、管理员成功、不包含敏感字段、storage failure fail-closed。
- 本轮该项为只读审计可见性，不提供 reset/重置权益基线/释放 hold；未因此改变账户状态，也未连接真实账户。管理员状态 API 仍需完整身份认证及部署后访问控制验收。

## rc1133：Futures 退役账户周期只读核验接线（Spot/reset 仍待闭合）

- Binance/Bitget Futures observer factory 只创建 REST-only account-evidence adapters，单独返回 `complete` 与 `flat` 及观察时间；已完整读取但仍有持仓/挂单会记录 open exposure，API/解码错误会记 incomplete，绝不把错误或缺失数据算作零值。
- BotManager 在启动后按分钟扫描已归档 Futures 凭据，单账户请求限时 30 秒；成功的完整平仓/无挂单观测进入持久化累计，完整非零观测会清零连续平仓计数；失败/不支持的 Spot 观测记为 incomplete 并维持 `pending_verification`。开仓 hold 不由采样任务释放。
- `go test -race . -run 'Test(RetiredEquity|PrepareEquityScope|EquityScopeChangeInvalidatesOpeningAdmissionBeforeNewSnapshot|RuntimeEquityUsesCurrentCredentialsAfterRotation|MetricsFeederRevalidatesAfterCredentialRotationWithConfiguredSource)' -count=1`、Web 配置持久化 race 定向回归、Binance/Bitget/exchange 包 race、`go vet . ./web ./exchange`、`git diff --check` 与前后端版本一致性均通过。适配器包首次在 sandbox 因 IPv6 loopback 监听权限 panic；批准本机 loopback 后同一测试通过。未跑完整 `go test ./...` 或强制 MySQL 门禁。
- 尚未闭合：Bitget Spot wallet 含冻结资产的归零定义待用户确认，Spot liabilities/open orders 与 wallet 需要完整组合；无显式 reset API/UI/审计操作者记录、无归档凭据清除机制。REST Futures 双 endpoint 非原子、只覆盖旧凭据权限可见范围；不可据此宣称账户全面无负债或盈利验收。
- 本轮未进行真实账户请求或下单；代码在应用启动后会使用归档凭据发起限时 REST-only 账户只读请求，不含下单/撤单调用。未提交/推送/发布；schema migrations 仍须发布前批准执行。

## rc1132：退役账户封锁与零暴露证据状态机（只读采样/重置仍未闭合）

- 在 rc1131 加密归档基础上，配置切换成功前为退役账户登记独立开仓暂停；重启时解密归档记录并恢复该暂停。归档或持久化暂停失败均拒绝配置切换；启动无法读取归档则建立 fail-closed 暂停。存储缺失/主密钥丢失不会明文回退。
- 持久化证据状态机要求两次完整零暴露观测相隔至少一分钟才到 `ready_for_explicit_reset`；不完整数据、非零仓位/订单/负债会清空连续零值计数；旧/倒退时间戳和失配状态拒绝。定向根包 race 测试覆盖加密归档、状态累计/复位、切换前暂停、重启恢复；测试只调用存储 API，不代表交易所数据已真实采样。
- 本轮 `go test -race . -run 'TestRetiredEquity|TestPrepareEquityScope' -count=1` 通过。尚未重跑完整 Web/Go/MySQL 门禁、`go vet` 或前端验证；未提交/推送/发布。
- 明确未完成：旧账户凭据尚无生产只读 verifier 构造/周期采样调度，Spot inventory 的“平”定义仍未获得用户确认；`recordEvidence` 暂无生产调用；没有可审计核验证据 API、显式 reset API/UI 或安全删除已退役密文的流程。因此当前有效行为是持续开仓封锁，不会自动认定清零或迁移权益基线。`risk_checkpoints` 与 opening-pause schema 仍须在部署前经批准显式迁移。
- 本轮没有访问真实账户、下单、提交、推送或发布；不代表实盘或盈利验收。

## rc1131：旧账户凭据的加密耐久归档前置门禁（生命周期仍未闭合）

- 用户确认账户范围变化后，旧账户须持续只读跟踪至仓位、负债均有可审计零值证据，再经显式操作重置；并确认跨重启凭据复用现有 `QUANTMESH_MASTER_KEY` / `master.key` 加密，不可用时拒绝轮换，禁止明文降级。
- 配置持久化入口新增 preflight：凭据轮换或移除导致旧账户退出当前范围时，先用现有主密钥加密完整只读账户凭据并经 risk-checkpoint CAS 耐久归档，再写新配置、发布新权益范围。并发 revision 冲突或存储/密钥/加密错误时拒绝配置变更；仅交易对变化不归档重复凭据。归档密文采用现有 checkpoint 表，因此生产仍需先显式部署对应 risk-checkpoint schema migration。
- 归档单测覆盖密文不含明文凭据、重启式重新加载解密、幂等重试、SecretKey 轮换识别、缺主密钥拒绝及 scope 发布前顺序；另有 web 配置保存失败时不会发布配置的回归。最终 `go test -race . -run 'Test(RetiredEquityAccounts|RemovedEquityAccounts|PrepareEquityScopeConfig|EquityScopeChangeInvalidatesOpeningAdmissionBeforeNewSnapshot|RuntimeEquityUsesCurrentCredentialsAfterRotation|MetricsFeederRevalidatesAfterCredentialRotationWithConfiguredSource)' -count=1` 与 `go test -race ./web -run 'Test(ConfigPersistenceNotifiesEquityScopeUpdater|ConfigMutationRequiresRetiredAccountArchiveBeforePersistence)' -count=1` 均通过；`go vet . ./web`、`git diff --check`、后端/前端 `3.111.0-rc1131` 一致性检查通过。`yarn --cwd webui verify` 通过 TypeScript、52 个测试文件/306 项测试及 Vite/PWA 构建。首次 web 测试因新夹具未含有效 Bot 配置而在调用 preflight 前退出；补齐真实合法配置后原测试通过。默认 Go build cache 在沙箱无权限，改用 `/private/tmp/quantmesh-gocache-rc1131` 后相同 race 命令成功。未运行完整仓库/MySQL 门禁；不能据此宣称同提交完整验收。
- 此增量尚未读取/验证旧账户全市场仓位与所有负债、累计重复零值证据、持久化核验结果、提供显式 reset API/UI，亦未解决旧账户资产归属/外部 writer 及非原子快照风险；即使已有只读 futures/margin verifier，也不得据此宣布旧账户已平或允许基线迁移。未连接真实账户、未下单、未提交/推送/发布、未验收真实盈利。

## rc1130：Binance Spot Margin 只读负债/挂单 verifier 工厂

- 新增 REST-only adapter 构造器与统一 factory，绕过交易 metadata/WebSocket 初始化；复用既有跨仓与逐仓负债、全账户普通挂单及 OCO 查询。adapter 设置 stop-evidence-only，回归确认下单、撤单、借币、还款均在触网前拒绝，并隐藏可变更的 MarginClient。
- factory 仅支持 Binance 且要求完整 API key/secret；校验范围严格限制于 Spot Margin 负债/挂单，不覆盖 Futures/普通 Spot 仓位、账户全资产或读取间外部 writer，也不是旧账户 reset 凭证。
- 定向 `go test -race ./exchange/binance ./exchange -count=1` 通过（分别 19.303s / 11.706s），使用 REST-only helper 驱动 cross/isolated 债务、普通挂单、OCO、null、缺字段与 oversized 响应 fixtures，并验证下单/撤单/借款/还款立即拒绝；factory 拒绝不支持 venue 与缺凭据。`go vet ./...`、`yarn --cwd webui verify`（类型检查、52 文件/306 测试、Vite/PWA build）、版本一致性与 `git diff --check` 通过。
- 当前 rc1130 整仓回归报告 `/private/tmp/quantmesh-profit-readiness-rc1130.mOcM5u/results.json`：3242 pass、25 skip、0 fail/解析错误；源码 SHA-256 `73a5e27730479fc5f79c8c41816014c0493c066f956e1cd28e90458c06a26e72` 前后稳定，HEAD `5c048825cb8a7536b0fe479a0fd6b7c47a73a8ea`，后端/前端 `3.111.0-rc1130` 一致。25 项中 19 项需强制隔离 MySQL、6 项明确 opt-in 的真实网络交易所测试；rc1129 的 MySQL race 结果不继承为 rc1130 验收，本版本强制 MySQL 门禁仍待补。
- 未连交易所或真实账户、未下单、未提交/推送/发布，未验收实盘或盈利。旧账户 archive/lifecycle 仍未接入；只读跨重启凭据保管仍待用户选择，普通 Spot inventory 与外部 writer 也不在本 verifier 范围内。

## rc1129：Binance/Bitget 统一 Futures 仓位与订单 verifier

- 增加 `exchange.AccountFuturesFlatnessVerifier` 与 `NewAccountFuturesFlatnessVerifier`，仅构造 Binance/Bitget REST-only adapter。Binance 不沿用浮点 `GetPositions` 或会把 JSON null 化为空切片的 SDK 路径：改用带 Binance server-time 签名的原始 `/fapi/v2/positionRisk` 与 `/fapi/v1/openOrders`，严格拒绝 null/坏字段，并以精确 decimal 检查 positionAmt 和 isolatedWallet。
- fixture 覆盖 Binance 明确空响应、微小非零持仓、残留逐仓钱包、未结订单、null、坏行和 API 失败；factory 限定支持市场及所需凭据。Bitget 同一接口复用前一版本的完整三产品仓位/全账户未结单快照。
- `go test -race ./exchange/binance ./exchange/bitget ./exchange -count=1` 三包全通过；两条主程序复合权益回归、`go vet ./...`、版本一致性及 `git diff --check` 通过。Binance API 时钟/签名、HTTP 数据形状仅由离线 fixture 覆盖，未连真实端点。
- 此接口只核验 Futures positions/orders 的非原子采样，不覆盖现货库存、借贷/保证金负债或第三方 writer 后续交易；不得单独清除旧账户追踪/重置 equity checkpoint。凭据跨重启保存选择仍待用户回复。

## rc1128：Bitget Futures 仓位与订单的只读 flatness 观测

- REST-only 账户证据 adapter 将此前的全产品 position reader 与已有账户级 futures open-order reader 汇合为一个完整度标记快照。任一子查询错误即不返回完成快照；只有精确仓位全为零且全账户未结订单数为零，`IsFlat()` 才返回 true。观测明确标注为非原子快照，并且不包含借贷负债，不足以自行批准账户退役/权益基线重置。
- HTTP fixture 覆盖 flat、跨产品微量仓位、账户级订单、未结订单数据为 null；`go test -race ./exchange/bitget -count=1`（8.622s）、主程序复合权益范围两项根包回归、`go vet ./exchange/bitget`、版本一致性及 `git diff --check` 通过。fixture 首次受沙箱 IPv6 loopback 限制，获准后按原命令重跑通过。
- 旧账户凭据跨重启保管选择仍未收到；未持久化/暴露凭据，也未访问真实账户。

## rc1127：Bitget 全产品期货账户仓位只读证据底座

- 新增 Bitget REST-only account-evidence adapter 的全账户读取接口，查询 `USDT-FUTURES`、`USDC-FUTURES`、`COIN-FUTURES` 仓位，并严格区分明确空数组与缺字段/null/格式错误/任一产品请求失败；身份、margin coin、持仓方向及 decimal 数量必须有效。对仓位数量使用精确 decimal 判零，不设有损容差。
- HTTP fixture 覆盖只读 adapter 接线、三产品空账户、跨产品残余微小非零仓位、缺失/null/坏行与第二产品失败；`go test -race ./exchange/bitget -count=1`、主程序复合权益范围两项根包回归、`go vet ./exchange/bitget`、版本一致性及 `git diff --check` 通过。沙箱首次运行被 `httptest` 的 IPv6 loopback 权限阻止，获准 loopback 后原命令通过。完整旧账户 verifier 接线尚待完成；该接口本身不是已核平结论。
- 本版本仅添加只读读取原语，未保存旧凭据，也未连接账户。旧凭据跨重启保管方式待用户选择；旧账户 flatness 还必须联合账户级未结订单和全部可用借贷负债证据，再做显式重置。

## rc1126：复合策略的权益范围按底层账户核验

- 生产调用链原先把 `funding_carry` / `funding_perp_spread` 当成单一普通账户市场类型，配置范围构建会拒绝这些策略，运行时权益采样也无法将策略运行时映射为真实交易所账户。这会让启用权益风控的复合策略无法取得完整账户核验，保持开仓封锁。
- 现将 Funding Carry 展开为同一交易所的 futures + spot，将 Funding Perp Spread 展开为两腿交易所的 futures；复合 runtime 不作为账户证据来源，缺失的底层来源必须由专用只读账户证据 adapter 读取。Binance Carry 因尚无受支持的现货完整权益证据仍明确失败关闭；不受支持市场/缺失凭据不降级为未经调整的权益。
- 回归：`go test . -run '^(TestBuildEquityScopeSnapshotExpandsCompositeBotAccounts|TestRuntimeEquityUsesConfiguredUnderlyingSourcesForCompositeRuntimes|TestRuntimeEquityUsesCurrentCredentialsAfterRotation|TestMetricsFeederRevalidatesAfterCredentialRotationWithConfiguredSource|TestEquityScopeChangeInvalidatesOpeningAdmissionBeforeNewSnapshot)$' -count=1` 通过；`go test -race . -run 'Equity' -count=1`、完整根包 `go test . -count=1`（112.519s）、`go vet .`、版本一致性检查及 `git diff --check` 通过。真实交易所账单/账户未访问，外部资金流正确性与跨币种合并仍需相应 adapter/人工审计验证。
- 当前实现是未提交的本地改动，服务端/前端版本为 `3.111.0-rc1126`。旧账户在账户范围变更后持续只读追踪，直到仓位与负债均有可审计零值后才显式重置，仍未实现；用户已选该生命周期策略，但旧凭据如何安全跨重启保存/授权尚待决定。未提交、推送、发布或下单，不代表实盘或盈利验收。
- 继续核验发现，当前 `accounting.Snapshot` 只承载 equity、wallet 与流水，不能证明仓位/未结订单/借贷负债归零；已有 `VerifyFlat` 多为单策略/自有资产语义，也不能替代整个旧账户的全品种平仓与负债证明。配置历史虽会加密保留完整旧配置，但现有风险 checkpoint 只存 scope 摘要与钱包/流水，不含可反查的旧账户标识或凭据引用；权益范围变更回调又明确禁止 storage/exchange I/O。因此需要独立的旧账户档案与只读全账户核验通路，且必须在替换旧范围前安全保留旧凭据/档案，不能依靠新配置或策略私有账本推断清零。
- 全账户能力边界继续核验：Binance Futures SDK 在省略 symbol 时请求全账户 positionRisk，且现有 Binance/Bitget 都已有账户级未结订单读取器；但当前 Bitget `GetPositions` 固定使用 adapter 的单 symbol，不能复用作旧账户平仓证明。Bitget 官方提供按 USDT/USDC/COIN productType 查询全仓 position 的 `/api/v2/mix/position/all-position`，可作为独立只读实现入口；借贷负债覆盖与现货资产何种状态算“无仓位”仍须明确验证契约，未证明前禁止清除旧账户追踪状态。

## rc1124：Funding Carry 待核账状态的只读诊断

- 策略可视化及受保护的 Funding Carry 状态 API 现在传递稳定的恢复原因码、耐久策略账本债务/仓位、借款流水身份与时间，并标明债务数值来自策略账本而非交易所实时负债。前端使用 i18n 展示；不暴露原始 exchange/storage 错误，不自动下单、平仓、还款或解除 UNKNOWN/开仓封锁。
- 验证：`go test ./... -count=1 -timeout=30m` 在允许测试启动本机回环 HTTP fixture 的环境全包通过；首次沙箱内运行因 `httptest` 监听 `::1` 被拒而失败，不作为代码失败或通过证据。`go test -race ./strategy` 的诊断用例与 `go test -race ./web` 的状态 HTTP 用例通过，`go vet ./strategy . ./web` 通过。`yarn verify` 通过：类型检查、52 个测试文件/306 项测试及 Vite/PWA 构建；前后端版本 `3.111.0-rc1124`、三套诊断字典键一致，`git diff --check` 通过。
- 强制命令 `QUANTMESH_MYSQL_TEST_DSN='root@tcp(127.0.0.1:32810)/quantmesh_test?parseTime=true' QUANTMESH_MYSQL_TEST_ALLOW_DESTRUCTIVE_SCHEMA=1 ruby scripts/verify_trading_race.rb /private/tmp/quantmesh-trading-race-rc1124-mysql-dbinit --require-mysql` 最终通过：10 包 2,279 pass、0 skip/fail/解析错误，19/19 必需 MySQL 父例及 62 条父/子终态全 pass，无缺包/缺例；源码 SHA-256 `17c1980f8c60d5ae1c7dcdfd10fbdbd7c5387dd6d0225cdc74285963e33b6c0` 测试前后稳定，HEAD `5c048825cb8a7536b0fe479a0fd6b7c47a73a8ea`，前后端 `3.111.0-rc1124` 一致、`source_dirty=true`。通过报告 `/private/tmp/quantmesh-trading-race-rc1124-mysql-dbinit/results.json` 与 `results.md`。首轮报告 `/private/tmp/quantmesh-trading-race-rc1124-mysql/results.json` 保留为失败证据：容器已就绪但预期的 `quantmesh_test` schema 未创建，导致 MySQL 用例无法运行；创建并确认空 schema 后，使用独立目录重跑成功。容器为 MySQL 8.0.36、随机回环端口 `127.0.0.1:32810`、512 MiB tmpfs、无宿主卷；按精确容器 ID 移除并核实不存在。报告读回后只追加了本审计记录；完整树哈希相对报告中的测试快照因此会变，但后续差异仅是文档记录，没有代码变更。
- 诊断仅改善可观察性，不闭合未知借款/资产归属的经济恢复。用户已确认旧账户必须持续只读跟踪到仓位与负债均有可审计零值证据后才可显式重置；如何跨重启保留旧账户只读凭据仍待选择。未做真实账户访问、下单、提交、推送、发布或盈利验收；严格 MySQL/race 只证明本地交易核心测试。

## rc1123：权益账户范围切换撤销旧采样健康凭据

- 调用链确认账户配置更新先替换 `BotManager` 权益快照，而熔断器健康状态原可沿用至周期 feeder 下一轮；期间开仓准入可能误用旧账户/凭据范围的权益证据。现由生产 `startCircuitBreakerFeeder` 接线范围变更处理器：变更与 feeder `Tick` 共用串行锁，先发布不可用健康状态，再发布新范围；旧 tick 不能在切换后重新写回健康状态。新范围重新取得完整健康证据前维持独立开仓封锁。
- 新增生产接线回归，验证凭据旋转会立即持有 `risk_metrics_unavailable`、只改交易对不会误失效、重新核验后仅释放该独立暂停源。目标根包 race 10 轮，权益/开仓相关根包 race 3 轮，完整 `risk` 包 race、健康/到期准入测试 race 5 轮及 `go vet ./risk` 均通过；`git diff --check` 通过。
- 无排除项整仓 `go test ./... -count=1` 报告 `/private/tmp/quantmesh-profit-readiness-rc1123-scope-change/results.json`：3224 pass、25 skip、0 fail、0 JSON 解析错误；其中 19 项 MySQL 案例未在该 runner 执行，另 6 项外部网络用例跳过。测试前后 dirty 源码 SHA-256 均为 `43a98f9d6f73f3aa6127ac34fb1f23c8687865904da6bf608b39301b5ad19507`，服务端/前端 `3.111.0-rc1123` 一致。该无 race 全仓回归不替代强制 MySQL/race、真实账户或发布验证；报告生成后本审计文件仅追加本节，未改代码、测试或版本。
- 后续十包交易核心 race 门禁 `/private/tmp/quantmesh-trading-race-rc1123-equity-scope/results.json` 通过：2258 pass、19 skip，0 JSON 解析错误、无缺失包；19 项均为 MySQL 专项，该次命令 `mysql_required=false`，因此不计作 MySQL 验收。dirty 源码 SHA-256 `dcd2da9daa5bad3fa466637e1387b10dd780023c62b804fb2bd6db2e864b2ce4` 测试前后相同，前后端均为 `3.111.0-rc1123`。报告生成后仅更新本审计记录，未更改受测代码/测试/版本。
- 随后在 MySQL 8.0.36 强制重跑 `/private/tmp/quantmesh-trading-race-rc1123-mysql/results.json`：10 包 2277 pass、0 skip/fail/解析错误；19/19 必需 MySQL 父用例、62 条父/子证据全通过，缺包/缺例均为 0，`mysql_required=true`。源码 SHA-256 `c0131ea9932b8089ba122cbbf958aa44d77e0cc1e24d218be97e039e54280d39` 测试前后相同，前后端 `3.111.0-rc1123` 一致。MySQL 8.0.36 容器仅绑定随机回环端口 `127.0.0.1:32809`，数据目录为 512 MiB tmpfs、无宿主卷；验证后按精确容器名移除并核实 `no such object`。报告生成后仅更新本审计记录，未改受测代码/测试/版本。
- 本轮继续对已知 COMMIT-confirmed-canceled 调用方与单行 receipt 做只读调用链复核；现有生产代码已对借贷事件、订单 ACK/成交、还款意图清除、平仓检查点及 final intent 分别保留 durable 已确认状态或封锁后续流程。本轮未发现足以证明存在新的安全绕过的具体反例，未以推测扩大改动；单行 receipt 被新 owner 写入覆盖仍可能使旧结果无法确认并保守留在 UNKNOWN，需与跨代 financial intent reconciliation 一并处理。
- 仍未闭合：用户已确认旧账户须只读跟踪至仓位与负债有可审计零值证据后再显式重置；旧凭据如何跨重启提供尚未决定，当前 checkpoint 也仍拒绝不同 scope 的自动切换。本修复仅封住旧健康采样的开仓窗口，没有实现旧账户只读核验、负债/未结单清零证明或显式新基线提交。未访问账户、下单、提交、推送或发布，不代表真实实盘或盈利验收。

## rc1117：交易风险 race 门禁超时与全仓验证

- 原 `scripts/verify_trading_race.rb` 对每个 Go 测试包固定 `-timeout=600s`。实际全仓 `go test -race -p=2 ./... -count=1 -timeout=30m` 在允许本机回环测试的环境 exit 0，`web` 包耗时 808.564 秒，证明 600 秒门限会在有效测试完成前将此包判失败。
- race 验证器门限提高到 1,200 秒，并同步更新契约测试；`ruby scripts/tests/trading_race_gate_test.rb` 20 runs/446 assertions、`go vet ./...`、版本一致性与 `git diff --check` 通过。版本 `3.111.0-rc1117` 同步服务端/前端。
- rc1117 强制命令 `QUANTMESH_MYSQL_TEST_DSN='root@tcp(127.0.0.1:32805)/quantmesh_test?parseTime=true' QUANTMESH_MYSQL_TEST_ALLOW_DESTRUCTIVE_SCHEMA=1 ruby scripts/verify_trading_race.rb /private/tmp/quantmesh-trading-race-rc1117-final --require-mysql` exit 0：10 包 2,272 pass、0 fail、0 JSON 解析错误，19/19 必需 MySQL 父例齐全、62 条父/子路径证据，缺包/缺例均为 0。报告 `/private/tmp/quantmesh-trading-race-rc1117-final/results.json` 记录前后端均为 `3.111.0-rc1117`、dirty=true、测试期间源码稳定，SHA-256 前后相同 `9e60f822d4d26fae63bec958b702cfdc3a6b10beafd97e4ccf885e5885001cb5`。
- 使用 MySQL 8.0.36 一次性容器 `aadab83f67a91c2182e0ef043307d29673cdccd0bfd55f679cbf9391e0f74346`，仅绑定 `127.0.0.1:32805`、数据目录 512MiB tmpfs、无宿主卷；测试后依精确 ID 移除，`docker inspect` 回报 no such object，端口无监听。此为本地 dirty 工作树证据，不等同干净提交/发布 CI、部署、真实账户或盈利验收；未访问真实账户或下单。
- 全仓 `go test -race -p=2 ./... -count=1 -timeout=30m` 亦 exit 0（Web 808.564 秒、策略 189.748 秒），但该次全仓命令在 rc1116 的 Go 源码快照执行；rc1117 的强制十包 MySQL race runner 则直接验证本版本源码指纹。

## rc1116：手動平倉路由配置使用原子快照

- `BotRuntime.planClosePosition` 曾在沒有 Bot lifecycle lock 的手動平倉路徑直接讀 `br.Config.Symbol` 與 `MarketType`；熱更新在 `configMu` 下整體替換 `BotConfig`，形成真實資料競爭窗口，也可能讀到不同版本的交易對/市場字段。
- 新增 `closePositionScopeSnapshot`，在短讀鎖內成對讀取 symbol 與規範化市場類型，立即釋鎖後才執行持倉 RPC；不把鎖帶入網絡調用。
- 1,000 次配置整體替換與 1,000 次路由快照的關聯一致性測試，定向 race 連跑 3 輪通過；`go vet .`、`git diff --check` 及版本一致性通過。這是快照/鎖序回歸，未連接交易所，不代表真實平倉流程、賬戶倉位或盈利驗收。
- bugfix 版本 `3.111.0-rc1116` 已同步服務端、前端、CHANGELOG 與產品概覽；未提交/推送/發布。其他 runtime 配置讀路徑、權益賬戶遷移與 Borrow `UNKNOWN` 人工處置仍待審查或確認。

## rc1115：Bot 詳情 API 與熱更新配置快照隔離

- `BotRuntime.applyRuntimeTradingParamsWithContext` 在 `configMu` 寫鎖內發布配置；但 `botManagerProviderAdapter.GetBot` 曾直接讀取 `br.Config`，並把 `&br.Config` 返回給 HTTP 層稍後序列化。熱更新併發時可能觸發 Go 資料競爭，且同一響應可由不同時刻的字段拼成。
- 新增 `snapshotBotRuntimeConfig`：持配置讀鎖完成配置 JSON 深拷貝與 Bot ID 讀取，詳情響應只持有獨立副本；當前全局配置暫不可用時，測試網狀態回退到 Bot 自身快照值，不再解引用 nil。
- 新增快照可變嵌套字段隔離測試，以及 500 次熱配置寫入與 500 次讀快照的一致性回歸；另經實際 `GetBot` adapter 確認無全局配置時安全回退，且調用方修改返回配置不會改動 runtime。定向 `go test -race . -run 'Test(SnapshotBotRuntimeConfig|GetBotReturnsDetachedRuntimeConfigWithoutGlobalConfig)' -count=3`、`go vet .` 與 `git diff --check` 通過。這只證明 Bot 詳情配置快照邊界，未聲稱已解決其他 `BotRuntime.Config` 讀取方的併發問題。
- bugfix 版本 `3.111.0-rc1115` 已同步服務端、前端、CHANGELOG 與產品概覽；未提交/推送/發布，未訪問真實帳戶或下單。舊帳戶只讀追蹤憑據途徑與 Borrow `UNKNOWN` 持有可歸屬倉位/負債時的處置仍待確認。

## rc1114：资金使用视图保留运行时 Bot 归属

- `capitalDataSourceAdapter` 之前只导出交易所与交易对；Web handler 再用 `GenerateBotID(exchange, symbol, "")` 重新构造身份，既丢失配置中的自定义 Bot ID，也隐含默认 futures。现货/合约相同交易对会发生 ID 冲突，自定义 Bot 则可能指向不存在或错误的 Bot。
- 适配器现在从 `BotManager.List()` 读取运行时 Bot ID及其配置市场类型，并在读配置时持有运行时配置读锁；使用视图保留该 ID，只有兼容数据源未提供 ID 时才按其市场类型生成。新回归覆盖同一交易所/交易对的自定义 spot/futures Bot，并验证生产适配器分别导出自定义 ID 与 market-scoped ID。
- 账户与策略资金明细仍未因此证明策略级资金归属：相关分配 API 保持 fail-closed。此修复只纠正 Bot 资金使用视图的运行时身份映射，不推断策略占用、全账户所有权或盈亏。
- bugfix 版本 `3.111.0-rc1114` 已同步服务端、前端、CHANGELOG 与产品概览。`go test ./web ./ -run 'TestCapitalUsagePreservesRuntimeBotIdentity|TestExchangeProviderAndCapitalAdapters' -count=1`、相同选择的 `-race -count=3`、`go vet ./web .` 与 `git diff --check` 通过；未访问真实账户、未提交/推送/发布。账户范围迁移及借款 `UNKNOWN` 处置选择仍按后续段落待闭合。

## rc1113：配置权益范围重入回归与旧账户迁移复核

- 全仓 `go test -race -p=2 ./...` 首轮在 `TestSetSymbolEnabledNotifiesEquityScopeAfterUnlock` 报告 3 秒超时；测试超时后清理替换全局配置管理器，后台调用再从全局变量取锁对象，造成二次解锁崩溃。隔离该用例通过，说明首轮不能据此认定生产通知存在死锁。`SetSymbolEnabled` 现于操作开始时固定管理器引用；重入回归保留有界检测并将窗口调至 30 秒。相关两个回归连续 5 轮 `-race` 通过；修复后相同全仓 race 门禁 exit 0，`web` 包 578.387s；`go vet ./...` 与 `git diff --check` 通过。
- 当前完整准备度报告 `/private/tmp/quantmesh-rc1113-final/results.json` / `results.md` exit 0：3210 pass、25 skip、0 fail/解析错误；25 skip 为 19 项需 disposable MySQL DSN/显式许可及 6 项显式交易所网络测试。服务端/前端 `3.111.0-rc1113` 一致，源码 `d19d3d31075af54d4e5ec41ce436467a862419469e91b0d316098476e02ad644` 测试前后相同，baseline `5c048825cb8a7536b0fe479a0fd6b7c47a73a8ea`，dirty=true。报告明确排除不了 MySQL 实例验证，亦不等同发布同提交或实盘/盈利验收。
- 补齐 19 项数据库路径：镜像 MySQL 8.0.36 (`sha256:a532724022429812ec797c285c1b540a644c15e248579c6bfdf12a8fbaab4964`)，一次性容器 `227dd5f3bdd564776ffc8d35396750e2d7275bc2c898d8745144970cf99ee4d4` 仅映射 `127.0.0.1:32804`、`/var/lib/mysql` 为 512MiB tmpfs、无宿主卷；开始前 `quantmesh_test` 为 0 张表。强制命令 `QUANTMESH_MYSQL_TEST_DSN='root@tcp(127.0.0.1:32804)/quantmesh_test?parseTime=true' QUANTMESH_MYSQL_TEST_ALLOW_DESTRUCTIVE_SCHEMA=1 ruby scripts/verify_trading_race.rb /private/tmp/quantmesh-trading-race-rc1113-mysql --require-mysql` exit 0，2267 pass、62 条必需 MySQL 父/子证据、缺包/缺案例/解析错误为 0，源码 SHA-256 `2350fba450d102a10edd8fe9439f8cebca92bf759cab8e2f5b217619ae85186d` 前后相同，版本 `3.111.0-rc1113`。容器测试后按精确名称移除，并读回 `no such object`。此为当前 dirty 源码本地隔离数据库证据，不替代干净提交/发布 CI、真实账户或盈利验收；报告写入 `/private/tmp`，后续仅审计/版本文档更新，Go 源码与测试未改。
- CI/CD 调用链当前均在 test job 显式注入一次性 `quantmesh_test` MySQL 环境并运行 `verify_trading_race.rb ... --require-mysql`；CD 的 verification 与 build jobs 都 checkout `${{ github.sha }}`，race artifact 名包含同一 SHA，构成“同一候选提交先验再构建”的定义。三项契约测试 `trading_race_gate_test.rb`（20/446）、`frontend_embed_test.rb`（14/27）、`profit_readiness_timeout_test.rb`（14/123）全通过。此为 workflow 静态/契约验证，仍未观察本版本 GitHub Actions 实际运行、已发布 artifact 或部署状态。
- 同轮重新追踪账户范围迁移生产调用链：`BotManager.updateEquityScopeConfig` 会以新配置替换旧账户快照；`runtimeEquitySource.ObserveAccountEquity` 只读取当前快照；耐久权益 checkpoint 仍只有一个 scope，scope 改变时 `nextEquityCheckpoint` / `nextWalletEquityCheckpoint` 先拒绝更新，即使有 reset 标记也不能切换到不同身份。当前策略防止静默遗忘/换基线，但旧账户停止运行或重启后没有被持续只读核验的接线，用户确认的“只读至仓位和负债可审计归零，再显式重置”仍未实现。
- 版本 `3.111.0-rc1113` 已同步后端、前端、CHANGELOG、PRODUCT_OVERVIEW；以上测试未连接真实账户或下单。旧账户只读凭据持久化方案及借款 `UNKNOWN` 发现可归属仓位/负债时的处置选择仍待确认；未提交、未推送、未发布，不构成真实资金或盈利验收。

## rc1111：已确认 COMMIT 的恢复检查点保持内存/持久态一致

- 扩展 rc1110 的调用链审查至 Funding Carry 最终 intent 完成、反向期货平仓检查点、还款意图清除、买回订单 ACK 与成交明细检查点。若 fenced store 已精确确认 durable COMMIT、随后报告调用 context 取消，旧逻辑会回滚上述内存快照，造成同一运行实例与 durable 状态分叉；尤其 ACK/成交证据回滚会令当前关闭流程不能依事实恢复。
- 仅对 `ErrFundingCarryRuntimeStateCommitConfirmedCanceled` 保留该次已确认提交对应的内存快照，同时仍返回取消、不继续金融流程；一般保存错误及 commit outcome unknown 仍保留原回滚/封锁。新加五项针对性回归；最终 `go test -race ./strategy -run 'TestFundingCarry(CoverAcknowledgementConfirmedCanceledKeepsCommittedMemory|CoverFillConfirmedCanceledKeepsCommittedMemory|FinishConfirmedCanceledCommitKeepsCommittedIntentState|ReverseCloseCheckpointConfirmedCanceledKeepsCommittedQuantity|ClearedRepayIntentConfirmedCanceledKeepsCommittedMemory|IntentCancellationAndSaveFailurePreserveRecovery|ConfirmedDebtEventCommitCancellationKeepsCommittedMemory)$' -count=3` 通过；完整 `go test -race ./strategy -count=1`（183.334s）、`go vet ./strategy` 通过。
- 最终源码快照 `GOCACHE=/private/tmp/quantmesh-go-cache-rc1110 go test ./... -count=1` 在获准本机回环的环境退出 0，所有有测试的包均为 `ok`；普通沙箱初次运行有多包因既有 `httptest` 绑定 `[::1]:0` 被拒而失败，不能记作代码断言失败。来源采集完整，基线 `5c048825cb8a7536b0fe479a0fd6b7c47a73a8ea`，后端/前端 `3.111.0-rc1111` 一致，dirty=true，测试时源码树 SHA-256 `1475e2b120ee01798cb59e24672e1d9dd32dfb12d5cb6d3cb76cdcb7ea1c675d`。之后只修订 CHANGELOG、产品概览和审计说明以准确覆盖五条路径；生产源码、测试、版本未变。报告不等同于已提交、推送、发布、真实账户或盈利验收。
- 最终强制 `ruby scripts/verify_trading_race.rb /private/tmp/quantmesh-race-rc1111.AaIHJn --require-mysql` 返回 exit 0：十包 race 2267 pass、19/19 必需 MySQL 案例通过、62 条父/子证据齐全、无缺包/缺例/JSON 解析错误；测试前后 SHA-256 均为 `cca368f60c1ecef56b7fe873e70f45528cd2e466272bd1798d751b5bacf4b477`，版本 `3.111.0-rc1111`、dirty=true。隔离 MySQL 8.0.36 容器 ID `22cdbfa1f36dde82248d0ee2478085aad82c65d5b580af14e59b9e389880f5ef` 仅绑定 `127.0.0.1:32803`、512MiB tmpfs、1GiB 内存、无宿主卷；结束后按精确 ID 移除并确认不存在。报告保留在 `/private/tmp/quantmesh-race-rc1111.AaIHJn/`。此为未提交源码本地验证，不证明发布同提交、生产账户、实盘或盈利验收。
- 用户已确认账户范围改变时旧账户继续只读跟踪，需以证据证明仓位和负债皆为零后才显式重置。跨进程继续只读追踪需明确旧账户凭据的持久化/核验途径；该选项仍待用户选择。本轮没有实现或绕过此依赖，因此权益基线切换仍拒绝自动推进。

## rc1110：确认 COMMIT 后取消不再回滚借贷内存账本（本地强制门禁通过）

- 调用链审查发现，`recordMarginDebtEvent` 与 `returnBorrowedPrincipal` 在 storage 用独立写回执确认 generation-fenced COMMIT 已落盘、但调用 context 同时取消时，仍无条件回滚内存事件/本金并锁存 UNKNOWN。由此 durable 借贷账本与运行实例暂时分叉，当前运行态难以直接重试对账，可能把已确认结果悬置到重启。两条新回归均先在旧逻辑失败，分别观测到 durable 含还款事件/本金已降至 0.003，而内存撤销事件/仍保持 0.005。
- 修复仅识别 `ErrFundingCarryRuntimeStateCommitConfirmedCanceled`：保留与精确已提交 durable snapshot 相符的内存账本，但继续将取消错误返回调用方，阻止当前金融操作继续；普通写失败、CAS 冲突和 `CommitOutcomeUnknown` 仍执行原回滚并 fail-closed。新增两条提交已确认取消回归；两条回归、重启幂等及普通保存失败恢复用例 `go test -race ./strategy ... -count=3` 通过，`go vet ./strategy` exit 0。最终 `go test ./... -count=1` 各包均输出 `ok`（须有本机 loopback 权限，沙箱初次运行会被既有 `httptest` 的 `[::1]:0` 监听拒绝）。
- 最终强制命令 `QUANTMESH_MYSQL_TEST_DSN='root@tcp(127.0.0.1:32802)/quantmesh_test?parseTime=true' QUANTMESH_MYSQL_TEST_ALLOW_DESTRUCTIVE_SCHEMA=1 GOCACHE=/private/tmp/quantmesh-go-cache-rc1109 ruby scripts/verify_trading_race.rb /private/tmp/quantmesh-trading-race-rc1110-final --require-mysql` 返回 exit 0：10 包 2262 pass、0 skip/fail，19/19 必需 MySQL 用例通过，缺包/缺例/JSON 解析错误为 0。报告记录 baseline `5c048825cb8a7536b0fe479a0fd6b7c47a73a8ea`、版本 `3.111.0-rc1110`、dirty=true；验证时源码 SHA-256 `ddb9510ab2780b126fbab3ed67c3e4599c22032e1bad2543268a85f4ab46e834` 前后稳定，前后端版本一致。使用 loopback-only、512MiB tmpfs、无宿主卷的 MySQL 8.0.36，验证后容器已停止并确认移除。报告生成后仅本审计文档追加此段，生产代码、测试、版本和产品概览均未改变；当前工作树指纹因此已变化。此证据仍是未提交工作树的本地自动化验证，不代表发布同提交、真实账户、实盘或盈利验收。

## rc1109：双腿 base asset 生产构造器拒绝路径与强制门禁

- 新增生产构造器回归，以两家离线交易所返回 BTC/ETH 不同 base asset，断言构造器显式拒绝，且两腿 `GetAccount` 均未调用、FundingPerpSpread durable state 未写入。既有策略回归继续断言 `MaxPositionQuantity` 是同一 base asset 下两腿数量合计上限；实际 constructor 在读取任何账户权益/预留资金前用交易所元数据校验同一 base，因此该合计在维度上成立。未改生产交易逻辑或访问真实账户。
- 定向构造器两用例、策略双腿数量限额用例通过；`go vet .` 与版本一致检查通过。完整 `go test ./... -count=1` 在沙箱内因多包 `httptest` 监听 `[::1]:0` 被拒而出现环境性 panic；放行本机 loopback 后整仓输出的全部包均为 `ok`。强制命令 `QUANTMESH_MYSQL_TEST_DSN='root@tcp(127.0.0.1:32800)/quantmesh_test?parseTime=true' QUANTMESH_MYSQL_TEST_ALLOW_DESTRUCTIVE_SCHEMA=1 GOCACHE=/private/tmp/quantmesh-go-cache-rc1109 ruby scripts/verify_trading_race.rb /private/tmp/quantmesh-trading-race-rc1109-current --require-mysql` 返回 exit 0：10 包 2260 pass、0 skip/fail，19/19 必需 MySQL 用例通过，缺包/缺例/JSON 解析错误均为 0。报告 `results.json` 的 baseline `5c048825cb8a7536b0fe479a0fd6b7c47a73a8ea`、版本 `3.111.0-rc1109`、源码 SHA-256 `dfd12735bb622dcd9f5d9df19f1bd3618e8e8adc0544516a70d9cff5909a0fb7` 测试前后稳定，且 dirty=true。隔离 MySQL 8.0.36 仅绑定 `127.0.0.1`、512MiB tmpfs、无宿主卷，测试后容器已停止并确认删除。上述不证明提交/发布同快照、真实交易所账户、实值盈利或实盘验收。

## rc1108：盈利准备度报告器保留 Go 构建诊断（本地核心门禁通过，发布/账户验收未闭合）

- rc1107 原始审查回归首次运行时，报告记录 6 项 PASS 后 `strategy` setup/build failure、`web` setup failure，且退出码为 1；Go `-json` 确实将编译器信息作为 `Action=build-output`、`ImportPath` 事件输出，但报告器只序列化 test output 与 package output，故失败详情丢失，不能据现有报告确认根因。显式指定隔离 `GOCACHE` 后同一 11 项回归通过；首次失败具体底层诊断未保存，原因不推断。
- rc1108 将 build-output 事件加入 JSON 诊断字段与 Markdown 报告，并在 Markdown 明列 package pass/fail/skip 结果；复用凭据脱敏与安全 fenced-code 输出。合成 package setup/build failure 契约验证源码位置、诊断保留、明确 package fail 与 secret 不泄露。契约 `ruby scripts/tests/profit_readiness_timeout_test.rb` 为 14 runs/123 assertions，`ruby -c`、版本一致与 `git diff --check` 通过。原始 11 项审查回归报告 `/private/tmp/quantmesh-profit-readiness-rc1108-final/results.md` 为 11 pass、0 parse error、exit 0；源码 SHA-256 `772f3264e1c967828c08c6d10d00120a6e7dd346f116e7b487116533b05f3e43`，前后端 `3.111.0-rc1108` 一致，测试期间稳定。报告保留 `go-m1cpu` 编译 warning 和每包结果，证明 build-output/package events 均进入报告；该份 11 项报告本身不覆盖整仓或强制 MySQL race，后续当前快照门禁见下条。bugfix 版本 `3.111.0-rc1108` 已同步后端、前端、CHANGELOG 与产品概览；未提交、未推送、未发布，不代表真实账户、实盘或盈利验收。
- 同一当前工作树后续验证：`go test ./... -count=1` 所有报告包通过，`go vet ./...` 退出码 0，`git diff --check` 通过。强制命令 `QUANTMESH_MYSQL_TEST_DSN='root@tcp(127.0.0.1:32799)/quantmesh_test?parseTime=true' QUANTMESH_MYSQL_TEST_ALLOW_DESTRUCTIVE_SCHEMA=1 ruby scripts/verify_trading_race.rb /private/tmp/quantmesh-trading-race-rc1108-current-mysql-optin --require-mysql` 通过：10 包 2259 pass、0 skip/fail，19/19 必需 MySQL 案例通过，缺包/缺案例/JSON 解析错误为 0。报告 `results.json` 记录 baseline `5c048825cb8a7536b0fe479a0fd6b7c47a73a8ea`、前后端 `3.111.0-rc1108`、源码 SHA-256 前后稳定为 `6ad98bf0ade31ec3cb02ade2c87207b514618a59299418acd83a8eec39fd5929`。使用只绑定 `127.0.0.1`、512MiB tmpfs、无宿主卷的 MySQL 8.0.36 一次性容器，测试后按精确名称停止并确认移除。先前两次尝试分别因沙箱 loopback 限制和未启用测试要求的 disposable-schema opt-in 失败；按 CI 等价授权在该隔离实例重跑后通过，不是代码测试失败。报告绑定的是当前 dirty 快照，不等同提交/发布或真实交易所/账户验收。未提交、未推送、未发布；不代表真实账户、实盘或盈利验收。

## 已确认的账户范围迁移准则（实现未闭合）

- 用户确认：账户范围变更时，旧账户继续只读跟踪，直至仓位与负债有证据证明为零；之后才允许显式重置权益基线。不能因配置改动、进程重启或熔断恢复而自动遗忘旧账户/重建基线。
- 当前证据显示仍缺迁移闭环：`risk.EquityCheckpoint` 仅保存一个 `Scope`；`nextEquityCheckpoint` / `nextWalletEquityCheckpoint` 对 scope 变化拒绝更新；`BotManager.updateEquityScopeConfig` 以新快照替换旧快照，`runtimeEquitySource` 只聚合当前配置及仍存活 runtime。因而安全地拒绝基线跳转，但无法在配置覆盖且 runtime 停止/进程重启后继续核验旧账户。
- 清零证据也不能只用权益钱包快照替代：必须覆盖旧账户全部相关仓位、未结订单及所有保证金负债/利息，失败或能力缺失继续封锁。用户已确认“旧账户只读跟踪至仓位、负债均有证据清零后，再显式重置权益基线”。跨重启如何取得只读核验能力仍待决策：独立只读 API Key 加密保存、复用现有交易凭据但仅走只读接口并加密保存，或不保存凭据、清零后由人工提供可审计证明。未触碰真实账户或凭据。

## 借款 UNKNOWN 的重启核账闭环（只读调用链复核，仍未闭合）

- `reconcileSavedMarginBorrowReceipt` 仅接管带精确 transfer ID 的 ACK-only 状态；它查询并持久化已确认本金后，将状态设为 `DirectionReverse`、`IntentInFlight=true`、`ExposureUnknown=true`，然后以“当前债务、资产及中断操作仍需核账”返回错误。后续反向开仓还可能提交保证金现货卖单及期货买单，不能把借款回执当作完整交易结果。
- 共享执行意图 journal 会保留未结算/UNKNOWN 订单，并明确等待策略级经济核账；Funding Carry 启动在 journal 要求经济恢复时封锁策略。普通 runtime-state decoder 仍拒绝 `IntentInFlight`/`ExposureUnknown`，Web/Bot 恢复配置入口只核验配置与状态归属，没有该借款操作的人工核账入口。因此新进程重启后完整的“订单/成交归属—现货/期货仓位—借款本金/利息—余额”接续尚未证明，不能靠清除标记或只看当前债务零值放行。
- 本轮本地离线定向验证：`go test -race ./strategy -run '^TestFundingCarry(StartupRecordsSavedBorrowReceiptWithoutReplaying|BorrowReceiptQueryFailureKeepsUnstartedStopBlocked)$' -count=1`、`go test . -run '^TestFundingCarryFullConstructorBorrowReceiptRetainsCapitalAndOwnership$' -count=1` 均通过；它们证明回执精确导入、失败时保留资本/所有权且不重放借还款，不证明第二个新进程完成核账。`go test -race ./risk -run '^TestEquity(WalletModeRequiresExplicitReset|WalletLoadsLegacySingleCurrencyCheckpoint)$' -count=1` 通过，验证现有检查点守卫，不覆盖跨账户范围迁移。
- 同一调用链复核确认权益迁移也存在不可达路径：`BotManager.updateEquityScopeConfig` 以新配置替换旧范围；`nextEquityCheckpoint` 和 `nextWalletEquityCheckpoint` 在显式 reset 处理前就拒绝不同 scope/currency。即使未来拿到旧账户仓位/负债清零证据，当前实现也不能切换到新范围并建立显式新基线。旧账户凭据保存方式及借款 UNKNOWN 完整核账后的非零仓位操作策略仍待用户确认。
- 上述均为当前 dirty worktree 的源码/离线测试证据；未修改生产代码，未访问真实账户、下单、提交、推送、发布或部署。当前版本仍为 `3.111.0-rc1111`，未因此宣称发布、实盘或盈利验收。

## rc1107：全部启动恢复入口先验可取消读取能力

- 对 rc1106 修复后的相邻调用链继续审查，发现准备阶段意图、覆盖挂单 ACK、已接受还款与剩余借贷资产恢复也存在相同先经旧 `LoadRuntimeState`、后检查 `RuntimeStateContextReader` 的结构；最终完整 runtime-state restore 也保留 legacy fallback。`Start` 顺序调用这些入口，任意一个旧读取都可能绕过共享 15 秒 deadline。新增表驱动回归覆盖准备意图、借款回执、覆盖 ACK/成交、还款、剩余资产及完整状态 restore 七处；无 context reader 时要求零次 legacy 读取。
- rc1107 将能力检查统一前移；同时拒绝 nil/已取消 context。只将合格 context 传入 context-aware 首次/后续读，不改变金融副作用恢复、UNKNOWN 仲裁或放行策略。版本 `3.111.0-rc1107` 前后端、CHANGELOG 与产品概览同步。最终 `go test -race ./strategy -count=1` 通过（184.058s）；构造器/停止/最终核验生产接线 race 子集通过（`go test -race . -run 'TestFundingCarry(FullConstructor|RetentionCannotAdmit|CloseVerificationRejects)' -count=1`，76.424s）；`go vet ./strategy`、原始 11 项 `TestAudit` 回归、Ruby 报告器契约（13 runs/103 assertions）及 `git diff --check` 通过。原始回归首次因 `strategy` setup/build failure 返回 exit 1（6 项已执行用例通过）；同一门禁显式指定隔离 `GOCACHE` 重跑为 11/11 通过、JSON 解析错误 0、源码前后 SHA-256 `5ddde761471840493b49fb4bef3a0f1e5d80afc0a66fbb23c2048565b51456da` 稳定、前后端版本一致。初次 setup failure 的底层错误未被报告器保存，不能声称已定位或修复。未提交、未推送、未发布，未访问真实账户或下单；实盘和盈利验收仍开放。

## rc1106：成交回执恢复的首次读取遵守启动期限

- Funding Carry 的 `Start` 为成交回执恢复设置 15 秒 context，但 `reconcileSavedMarginCoverFills` 此前先经旧 `LoadRuntimeState` 读取，再检查存储是否支持 context-aware reader；无此能力的存储可能在启动恢复超时之外长期阻塞。新增回归先验证旧接口调用数为零，原实现失败并返回 nil；修复后先拒绝 nil/已取消 context 和不支持可取消读取的存储，再执行有期限的首次读取。
- 版本按 bugfix 递增为 `3.111.0-rc1106`，前后端、CHANGELOG、PRODUCT_OVERVIEW 同步。回归先在旧实现上失败（无可取消 reader 却返回 nil），修复后 `go test -race ./strategy -run 'CoverFill|BorrowReceipt' -count=2` 通过；完整 `go test -race ./strategy -count=1`（184.081s）、根包真实构造器恢复/最终核验接线 race（4 项）及 `go vet ./strategy`、gofmt、版本一致、`git diff --check` 均通过。只覆盖本地 fake/SQLite 及构造器调用路径；未完成全仓/严格 MySQL/发布门禁，也不重放或确认未知金融副作用、不自动解除 UNKNOWN 封锁。本轮未连接交易所或账户。

## rc1105：额度撤单核实后的恢复锁存

- 生产 `ExchangeOrderExecutor` 在额度下降/行情标记恶化后异步撤销自有开仓单；若撤单只收到 ACK 或终态查询失败，会保留 `exposureCancellationPending` 和独立门控。此前后续 `CancelOwnedOpeningOrders` 受管重试即使已核实所有自有开仓订单终态，也没有清除前次遗留的 scheduling latch，导致额度风险消失后开仓仍可能永久保持封锁。
- 新增真实受管执行器故障回归：先注入 ACK-only 撤单并确认两个门控与 latch 均保留，再恢复终态核验、重试同一自有订单，要求只在全批核实成功后清除 latch、额度占用归零并由 `reconcileExposureLimitBlock` 重新判断是否可开仓。初始回归失败于 latch 仍为 true；修复在成功核验路径清理 latch，失败/未知路径保持原封锁语义。
- 版本按 bugfix 递增为 `3.111.0-rc1105`，前后端一致，CHANGELOG 与产品概览已同步。`go test -race ./order -run '^TestVerifiedExposureCancellationRetryClearsPendingLatch$' -count=3`、额度/撤单相关 order race 回归（`-count=3`）、`go vet ./order` 均通过；未连接真实交易所、账户或生产数据库，未提交、推送或发布，不代表 R12 全部额度验收或盈利验收。

## rc1104：验证脚本参数解析与报告覆盖防护

- 曾将 `--help` 误解析为输出目录并覆盖该目录的既有报告。验证脚本现使用 `OptionParser`，帮助请求不运行测试，未知参数/缺失输出目录在创建输出或运行测试前以错误退出；完整测试别名保留兼容。新增契约用例预置 `--help/results.json` 与 Markdown 哨兵内容，证明调用 `--help` 不运行 Go 且逐字保留已有报告；未知选项同样不运行 Go、不创建路径。
- Bugfix 版本更新为 `3.111.0-rc1104`，同步后端与前端版本、CHANGELOG 和产品概览。`ruby scripts/tests/profit_readiness_timeout_test.rb` 为 13 runs/103 assertions、0 failure/error/skip；两个 Ruby 文件语法检查通过；真实 `--help` 命令仅输出用法、未启动 Go；版本一致性和 `git diff --check` 通过。该修复仅改善验证工具安全性，不代表盈利审查完成、发布或真实账户验收。
- rc1104 当前快照整仓 Go 回归为 3200 pass、25 skip、0 fail、0 JSON 解析错误；6 项 skip 为明确关闭的公网/交易所网络测试，19 项 MySQL 例由强制 race 门禁覆盖。强制十包 MySQL race 为 2257 pass、0 skip/fail；19 个必需父案例/62 条父子证据全部通过，无缺包、缺例或解析错误。两份报告源码前后 SHA-256 均为 `f1fa6e80512fa878de204ca86cfd4fecc73a15df80f2df6b4fd5b20c2dc27c1e`，源码稳定，前后端版本均为 `3.111.0-rc1104`。仅本地自动化及隔离 MySQL，不等同真实交易所、发布或盈利验收。

## rc1103：MySQL race 门禁时间预算

- rc1102 当前工作树第一次强制 MySQL race 门禁共 2177 pass、19 个顶层 MySQL 用例失败；失败诊断显示测试容器未预建 `quantmesh_test` schema，另有 Web race package 达 370.9s 后超过命令的 360s timeout。缺 schema 已在本轮新建、空的 512 MiB tmpfs 测试容器内创建专用库修正；不访问现有数据库。
- 将 race 门禁超时改为 600s，避免在完整测试仍运行时因固定 360s 上限被中断；门禁测试 20 runs、446 assertions、0 failure/error/skip。修正后强制十包 MySQL race 报告 `/private/tmp/quantmesh-trading-race-rc1103-mysql/results.json` 为 2257 pass、0 fail；19 个必需 MySQL 父用例和 43 个子路径共 62 项均 PASS，无缺包/缺例/解析错误。源码测试前后稳定，SHA-256 `2cce0ec18c000cead98bdb0b0b4e8dadec59b272d090d834bb2585a25d98270c`，前后端版本一致 `3.111.0-rc1103`。仅隔离本地 MySQL/race，不能外推生产数据库或盈利验收。
- `yarn --cwd webui verify` 通过 TypeScript、Vitest 51 files/304 tests、生产嵌入构建及 PWA service worker 生成；构建输出位于 Git 忽略的 `webui/dist`。自动化测试通过不替代实际浏览器渲染及完整用户流程验收。

## rc1102：整仓验证报告器的诊断汇总复杂度

- 当前源码第一次 loopback 受限整仓回归为 2781 pass、25 fail、21 skip；25 项全部在测试构造 `httptest` listener 时被沙箱拒绝绑定 `[::1]:0`，不是断言失败。获准本机 loopback 后，Go 测试阶段完成，但报告器在每个顶层用例上反复扫描全部 JSON 事件，长时间无法生成报告；终止该无子进程会话，不把它记为通过证据。
- `scripts/verify_profit_readiness.rb` 现按 package/test ancestry 单次索引父测试及所有后代的输出，避免相邻测试名称前缀碰撞；契约测试覆盖父/子/深层输出和近似名称隔离。Ruby 契约门禁 11 runs、92 assertions、0 failure/error/skip。
- 修复后对同一工作树运行 `go test -json ./... -count=1 -timeout=10m`，报告 `/private/tmp/quantmesh-current-full-rc1102-loopback/results.json` 显示 3200 pass、25 skip、0 fail，JSON 解析错误 0，源码前后稳定，SHA-256 `f027c1d391876ecf68953dc81164863f9436ddbe7554180d46046d05fea1b2d8`，前后端版本一致 `3.111.0-rc1102`。25 项 skip 为 19 项未配置隔离 MySQL DSN 和 6 项显式公网交易所测试；没有执行强制 MySQL race 门禁。该整仓非 race 检查不证明实库、真实交易账户、发布或盈利。
- 仍需完成强制 MySQL/race 验证、账户范围切换时旧账户只读取证与仓位/负债清零后显式重置、同一提交发布验证及可审计盈利证据；未提交、未推送、未发布、未访问真实账户或下单。

## rc1098：Binance Spot 停流重启 race 夹具确定性

- rc1097 最终十包非强制 race 在 2231 pass、1 fail、19 MySQL skip 后，只剩 `TestBinanceOrderStreamStopWaitsForEveryWorkerAndRetriesSameGeneration/spot_connection`：重启后立即 Stop，在高并行压力下触发 10ms fixture deadline。相同子用例 `-count=10` 通过，完整 `exchange/binance` race 包单独 28.298s 通过；证据指向测试未同步第二代 worker 的调度时序，而非已经确认的生产泄漏。
- 回归夹具等待 generation 2 的离线 `serveUserData` 被实际调用后再停止，保留生产 10ms timeout 及“超时不当作完成”的断言。改进后子用例 `-count=10`、完整 `exchange/binance -race -count=1` 均通过。
- rc1098 最终十包 race 报告 `/private/tmp/quantmesh-trading-race-rc1098-final/results.md` 为 2232 pass、0 fail、19 MySQL skip；源码测试前后稳定，后端/前端版本一致 `3.111.0-rc1098`，源码 SHA-256 为 `f42f024fc0a64261af292c39f00773be0cd56f78357b31cb5604913b389de8ba`。同 SHA 的原始盈利准备度回归集 `/private/tmp/quantmesh-profit-readiness-rc1098-current/results.md` 为 11 pass、0 fail、JSON 解析错误 0。19 个 MySQL wire 父/子案例因隔离 DSN 缺失未运行，不能记为通过；以上均为本地未提交源码验证，不等同发布、生产数据库或真实账户验收。

## rc1100：资金释放与回执恢复及成交账本边界

- 复核 FundingCarry 启动调用链时发现，回执恢复先通过旧 `LoadRuntimeState` 读取，再检查存储是否支持 context-aware reader；因此启动检查可能卡在不可取消的存储调用，`Start` 的 15 秒恢复期限并不能约束该首次读取。
- 修復為在任何讀取前要求 `RuntimeStateContextReader`，並使用調用方 context 執行首次讀取。新增回歸斷言：僅有舊接口的存儲被零次調用。最終 FundingCarry 策略整包 race 通過（183.703s）；後續十包交易核心 `--require-mysql` 門禁讀回 2254 pass、0 fail/skip，19/19 MySQL 必需父例與 62 條父/子終態通過，無缺包或 JSON 解析錯誤。
- 調用鏈審查確認平倉釋放路徑把數量容差用於 `DirectionNone` 的實時合約倉位與策略自有現貨，也對 durable/in-memory owned amounts 採用容差。因該 verifier 被 production capital release callback 使用，低於精度半步的非零殘量可能未被證明歸零就釋放預留。現改為全鏈精確零條件；新增 subprecision futures/owned-spot 測試與完整 constructor 驗證。首次只讀核驗失敗時，資金預留/停止待核仍保留，但不污染金融 ownership ledger；每次停止重試重新取證，交易所變為精確零時可釋放。
- 成交捕獲曾在驗證總成交量與父訂單不符前逐筆寫庫；現在先完整驗證集合、再開始寫入，且 SQLStorage 提供訂單級 transaction；race 回歸覆蓋數量不符零寫入、capture→SQLStorage→日彙總、批次 writer 接線，以及 SQLite 批次中第二筆 execution identity 衝突時回滾第一筆，避免日彙總讀到半張訂單。`StorageService.GetStorage()` 返回此 SQLStorage；兼容舊單行 writer fallback 仍不具批次原子性。SQLite fixture 不替代 MySQL/生產庫驗收。
- 不改變借款回執接受條件、金融 RPC 重放策略或既有 UNKNOWN 失敗關閉語義；純只讀停止核驗失敗不再升格污染乾淨账本。constructor 殘倉保留/恢復測試 `-count=3`、capture→SQLStorage→日彙總與 batch rollback race 測試通過；完整十包強制 MySQL 門禁報告 `/tmp/quantmesh-trading-race-rc1100-mysql-loopback.X5jJ60/results.json`，19/19 MySQL 父例及62條父/子終態通過，源碼 SHA-256 `5ff3e863a1782a679f9dc823d47ed4085d85bed570d02f3625aaad392e92f9cf`。隨後無排除項全倉 `/tmp/quantmesh-profit-readiness-rc1100-full.POFM8G/results.json` 共3220項、3195 pass、25 skip、0 fail；11項 TestAudit 全部通過，19個 MySQL skip由前一強制門禁獨立覆蓋，另6項真實網絡測試依設定關閉。全倉快照SHA-256 `7fefe072c9fa958d0aace6ab69371d7978e6c669e5d29fd9d25c2f9641e5d81c`，版本`3.111.0-rc1100`、dirty=true、測試期間穩定。此前沙箱內回環拒絕造成的失敗診斷不作程式碼失敗；獲准 loopback 後相關門禁均成功。隔離 MySQL 容器已按精確 ID 清理。仍未完成同提交發布驗證、賬戶遷移政策及真實賬戶/盈利驗收；未提交、未推送、未發布、不下單。
- 未提交、未推送、未发布；不涉及真实账户、下单或盈利验收。
- 当前工作树复验（commit `5c048825`，dirty=true，版本 `3.111.0-rc1100`，源码 SHA-256 `2e185ab94c0219a27cfe1f30c9d8b920924735d05124d6926d3718a36ec07b5d`，两项测试前后稳定）：loopback 全仓回归 `/tmp/quantmesh-current-full-loopback.ZvLUhK/results.json` 为 3195 pass、25 skip、0 fail；10 包强制 race/MySQL `/tmp/quantmesh-current-race-mysql-ready.yxNgfJ/results.json` 为 2254 pass、0 skip/fail，19/19 MySQL 必需父例和 62 条父/子事件通过。普通沙箱的首次全仓失败全部是 `httptest` 绑定 IPv6 loopback 被拒；获准 loopback 后原样重跑通过。`go vet ./...`、全量 Go 文件 `gofmt -d`、`git diff --check` 通过；门禁 Ruby 回归共 30 runs/527 assertions、0 failures/errors/skips。测试 MySQL 是仅映射 loopback 随机端口、512 MiB tmpfs、无宿主挂载的一次性 MySQL 8.0.36 容器，已停止并按精确 ID 核实不存在。25 个全仓 skip 中 19 个另由同 SHA 强制 MySQL 门禁覆盖，6 个外部网络测试关闭。独立审查代理因 agent thread slots 已满未能启动；本轮人工检查不计独立审查。仍未完成同提交发布验证、账户迁移政策和可审计真实盈利证据；未提交、未推送、未发布，无真实账户访问或下单。

## rc1099：Spot 重启代 race 夹具仍复用 10ms 负例期限

- 新建一次性 MySQL 8.0.36 测试容器（仅 `127.0.0.1:32791`，`/var/lib/mysql` 为 512MiB tmpfs，无宿主机挂载），运行强制十包 race 门禁；共 2250 pass、1 fail，`missing_verified_mysql_cases=[]`、19 个必需 MySQL 案例均通过、JSON 解析错误 0。
- 唯一失败再次落在 `TestBinanceOrderStreamStopWaitsForEveryWorkerAndRetriesSameGeneration/spot_connection`，但这次位于“第一代已正确验证未确认停止并在释放后确认退出、manager 可重启”之后的第二代正常 stop：fixture 对所有阶段继续使用 10ms close timeout，race 并行负载下误把调度延迟报告成未退出。失败输出为 `restarted generation did not stop ... context deadline exceeded`；这是测试门禁失败，尚无证据表明生产 15s 等待路径存在同一问题。
- 修正仅把已确认可重启的第二代 fixture 关闭期限提升为命名常量 1s；第一代 10ms 超时、风险保留和禁止提前重启的断言不变，生产代码未改。版本同步升至 rc1099。
- 修正后生命周期用例 `go test -race ./exchange/binance -run '^TestBinanceOrderStreamStopWaitsForEveryWorkerAndRetriesSameGeneration$' -count=20` 通过，完整 `go test -race ./exchange/binance -count=1` 通过，`go vet . ./strategy ./web ./exchange/binance`、版本一致及 `git diff --check` 通过。
- 对 rc1099 当前实现/测试/版本执行强制十包 race：`/private/tmp/quantmesh-trading-race-rc1099-mysql/results.md` 显示 2251 pass、0 fail/skip，19 个强制 MySQL 用例及 62 条父/子路径事件全部通过，无缺包、缺 MySQL 案例或 JSON 解析错误；测试期间源码快照稳定，版本 `3.111.0-rc1099` 一致，SHA-256 `05e00cd2526a7e42e56c06d442e91735d93a96d22db00590fa1dbafc44e04d75`。门禁后仅更新审计、索引、进度及产品概览文档，未改实现、测试或版本，因此该报告绑定门禁时实现/测试/版本，但不是当前完整 dirty worktree 的指纹。容器已按精确 ID 停止并自动删除。本门禁仅为离线/隔离环境证据，不证明真实交易所 WebSocket、真实账户或盈利。
- 随后使用另一个一次性 MySQL 8.0.36 loopback/tmpfs fixture 运行整仓 `/private/tmp/quantmesh-profit-readiness-rc1099-full/results.md`：`go test ./... -count=1 -timeout=10m` 共 3209 pass、6 skip、0 fail，JSON 解析错误 0；skip 项逐一确认为显式要求真实网络开关的 Binance 公共行情、Bybit 与 OKX 网络测试；报告中的 23 个 MySQL 用例全部 PASS。测试期间工作树稳定、前后端 `3.111.0-rc1099` 一致，源码 SHA-256 `5cf1d87f3378801e9a52b953e722e3353ad4978cf876a81d06e5a3558d7d4249`。报告门禁后只更新审计、索引、进度及产品概览中的验证记录，未改实现、测试或版本；之后 `yarn --cwd webui verify` 在 rc1099 下重新完成 typecheck、Vitest、Vite build 与 PWA service worker 生成。故整仓报告绑定实现/测试/版本快照，而非最终全部文档的工作树指纹。临时容器已精确停止并自动删除。该离线整仓回归不验证真实交易所服务、账户权益/负债或盈利。

## rc1097：FundingPerpSpread recovery-proof schema 回归契约对齐

- 十包 race 门禁发现 `TestRecoveryConfigProofRejectsAmbiguousEvidence` 仍将 schema 7 当作未来版本拒绝，而生产 `fundingPerpSpreadRuntimeStateVersion` 已为 7，导致策略包失败。现改为使用当前 schema 常量验证有效快照，并用 `current+1` 验证拒绝未来版本；不放宽恢复校验或资金释放条件。
- 本轮第一次十包 race 结果 `/private/tmp/quantmesh-trading-race-rc1096-final/results.json`：2108 pass、3 fail、19 MySQL skip、JSON 解析错误 0、源码测试前后稳定、前后端为 rc1096。一个策略测试是上述陈旧断言；另两项 (`monitor/TestNewsCollectorKeywordsCacheSummaryAndNewsAPI`、`exchange/binance/TestAccountOpenOrdersQueriesAllSymbolsForEachMarket`) 因沙箱拒绝 `httptest` IPv6 loopback bind (`[::1]:0`, operation not permitted) 而失败。隔离 MySQL DSN 未配置，19 个必需 MySQL 父例/子例未执行；这不是全门禁通过。
- 按仓库规则升至 rc1097；本轮修正后的相关策略 race 已通过；非强制门禁随后发现独立 Binance Spot 停止用例在高并发下偶发超时，见 rc1098。即使非强制门禁通过，也不替代 MySQL wire、实盘核账或盈利验收。

## rc1096：FundingPerpSpread 完整 runtime constructor 离线接线验证

- rc1095 的验证只通过双腿 owner-generation adapter builder 测了 fenced Save/CAS，未证明完整 `startFundingPerpSpreadSymbolRuntime` 在调用 `StrategyManager.StartAll` 前确实安装该 adapter。
- 增加只供测试传入的 exchange factory seam；生产入口仍固定委托 `exchange.NewExchange`。新增回归通过完整生产 constructor、真实 SQLite storage、离线 fake exchanges 和明确的空仓/空账户挂单证据启动策略，并从 SQL runtime-state store 读回 constructor/strategy 启动写入。该测试没有连接交易所网络或账户。
- 本轮 `go test -race . -run '^TestFundingPerpSpreadConstructorBindsFencedStateBeforeStrategyStart$' -count=1` 已通过；rc1096 下 FundingPerpSpread 根包 race 和 BotManager durable-stop/Journal 组合 race 通过，FundingCarry strategy race 通过，`go vet . ./strategy ./web`、版本一致性与 `git diff --check` 通过。盈利准备度原始 11 项报告 11 pass、0 fail、解析错误 0、源码前后稳定（详见 `/private/tmp/quantmesh-profit-readiness-rc1096-current/results.json`）。不替代 MySQL wire、整仓测试、生产账户核账或发布验证。
- 当前 branch `codex/verified-capital-release-rc876` 领先 upstream 17 个提交；相关工作树有大量既有未提交/未跟踪内容，未提交、未推送、未发布。本次只报告本切片验证，不把既有 dirty 状态说成同提交发布。
- 跨 Redis/SQL/交易所 RPC 的原子 fencing、UNKNOWN 借款人工恢复契约、权益核账与可审计净盈利仍未闭合。

## rc1095：FundingPerpSpread 持久状态 owner-generation fencing

- FundingPerpSpread 原本在运行策略前把状态接到通用 `strategyRuntimeStateAdapter`，可不经 owner token 写入；Redis 双腿租约切换后，旧实例仍可能覆盖新实例的 intent/recovery checkpoint。
- 现在先取得双腿 Redis leases，再按同一组 futures ownership scopes 原子 claim SQL owner generation；策略 runtime-state 使用 fenced adapter，Save、context Save 和 CAS 都由 storage 事务验证 generation。Claim 后再次核验 Redis leases，再附加 adapter；过期 lease 直接阻止策略启动。generation claim 失败保持初始化失败并释放本次 leases。
- 新增两腿 generation 替换回归：通过运行时实际使用的 adapter builder，先取得两条 Redis lease 并 claim 双腿 SQL generation；旧 adapter 的 Save/CAS 被拒绝且不改变持久快照，新 owner 可继续 Save/CAS。该 SQL fencing 只阻止已被更新 generation 取代的旧写者，不提供 Redis lease 与 SQL claim 的原子交接，也不 fencing 交易所 RPC。
- 验证：FundingPerpSpread 策略 race、FundingCarry prepared 策略/constructor race、根包 generation/租约/市场路由 race 与 Web 精确 stop handler race 均通过；`go vet . ./strategy ./web`、版本一致性与 `git diff --check` 通过。盈利准备度原始 11 项报告 `/private/tmp/quantmesh-profit-readiness-rc1095-current/results.json`：11 pass、0 fail、JSON 解析错误 0，测试前后源码快照稳定、前后端均为 `3.111.0-rc1095`，工作树仍 dirty。此报告只覆盖原始 11 项，不替代整仓/强制 MySQL；完整 FundingPerpSpread exchange constructor、真实账户/发布链未验证。
- 真实子交易所 RPC 的跨系统原子 fencing、UNKNOWN借款人工恢复契约、实值权益核账、同提交发布验证与盈利证据仍未闭合。

## rc1094：FundingCarry prepared 恢复生产接线与停止市场路由

- FundingCarry `Start` 仅允许 schema 8 中明确 `prepared` 且无并存未决金融证据的 intent 进入条件 CAS 清理；旧 schema、`dispatching`、UNKNOWN 与有财务冲突的快照保持封锁。新增完整 runtime constructor 回归，由 SQL owner-generation fenced adapter 读取并 CAS 清理，校验不执行现货/期货金融写 RPC。
- 停止 API 原先虽读取 `market_type`，却调用无市场类型的 stop provider；provider 再从配置首个同币项推断，可能停错同币的 spot/futures Bot，并更新错状态键。现在请求市场类型贯穿 stop 路由；缺省时先用实际运行时，再用配置，唯一匹配才推断，多市场冲突返回 400 而非 500；返回最终市场键更新状态。
- 验证：策略 prepared 恢复两项 race 用例通过；完整 FundingCarry constructor/真实 adapter prepared 恢复用例通过；根包 stop market 解析与 Web handler 精确路由用例通过。初次新增 constructor 夹具遗漏 `GetBalance`，仅补只读零余额实现后重跑通过。`go vet`、整仓与强制 MySQL 门禁尚未重跑，本次未发布或连接真实账户。
- FundingCarry UNKNOWN/借款回执人工审计恢复契约仍待用户确认；跨 SQL/Redis/交易所 RPC 的原子 fencing、实值权益核账、同提交发布验证及可审计净盈利均未闭合。

## rc1093：FundingPerpSpread 双腿每笔开仓的租约/额度复核

- 生产接线审查确认双腿由策略直接调用 `IExchange.PlaceOrder`，因此 `ExchangeOrderExecutor` 的逐次 admission guard 不覆盖此路径；FundingPerpSpread 原 guard 只在 pair 开始前执行一次。第一腿成交并写入 pending fill 后 gate 会封锁新 pair，但已准入 pair 会继续对冲，因此第二腿需要在耐久意图后重新验证 ownership lease 与账户额度，且不能错误地重新获取会被 fill-ledger block 拒绝的 pair gate。
- 两条物理开仓路径现在均在订单意图持久化后再次调用 runtime admission guard；意图先持久化为 `prepared`，准入通过后写为 `dispatching`，并在 RPC 前再核验一次。失败且能成功还原为 `prepared` 时，确定未发送的 intent 可在重启时用精确仓位/活动委托快照核验并清除；如果前腿已成交，则保存应急平仓要求，正常策略 tick 不会继续操作，受管重启恢复会安排平仓。旧 schema 的 intent 仍按可能已发送处理，继续走精确历史订单核验。
- 新增测试覆盖真实策略开仓路径中第二腿 ownership 失效时仅发送第一腿、耐久状态保留第一腿成交/应急平仓要求，以及 prepared 第二腿恢复只读确认未发送并保留已知前腿。`GOCACHE=/private/tmp/quantmesh-go-cache-rc1093 go test -race . ./strategy -run '^TestFundingPerpSpread' -count=1` 两包通过，`go vet . ./strategy`、`git diff --check` 通过。此前一次中间实现测试发现 gate 二次 admission 会错误拦截“先完成对冲、再核成交账”的设计，该回归已修正后整组测试通过。
- 最终盈利准备度基础集报告 `/private/tmp/quantmesh-profit-readiness-rc1093-current/results.json`：11 pass、0 fail，解析错误 0；测试前后源码 SHA-256 均为 `5e2cfe4a32809b9f5901f8988c094114a3f1702058f429113661df4520596299`，前后端版本一致 `3.111.0-rc1093`。该门禁只跑原始 `TestAudit` 集，不替代整仓、强制 MySQL、真实账户或盈利验收。
- 仍未闭合跨系统原子 fencing：最终同步租约续期与交易所 RPC 之间仍有不可原子化窗口，需要交易所侧 fencing/idempotency 或具备 fencing token 的持久提交协议；本地测试不能证明该窗口不存在。完整整仓/强制 MySQL/发布同提交验证及真实盈亏证据未完成。
- 版本同步为后端/前端 `3.111.0-rc1093`；未访问真实账户/生产库、未下单、未提交/推送/发布，不宣称实盘或盈利验收。

## rc1092：FundingPerpSpread 额度接线与 FundingCarry UNKNOWN 人工恢复缺口（开发中，未提交）

- 生产调用链复核发现 FundingPerpSpread 专用运行时此前绕过通用 Bot 风控配置，双腿下单也未按已核验预算限制总名义金额/总数量。现将 Bot 风控及核验预算上限接入专用 runtime，双腿总额在策略开仓处二次约束，数量向下取整，成对敞口按一层计算，并接入热更新；不改变 FundingCarry 的借贷/关闭策略。
- 新增策略真实开仓路径测试验证双腿合计名义金额、总数量、成对层数及热更新；runtime 测试验证 Bot 风控与已核验资本取较严上限、回调热更新及非法限额拒绝。定向 `go test -race ./strategy -run '^TestFundingPerpSpread' -count=1`、根包对应 runtime 测试、`go vet . ./strategy`、`git diff --check` 通过。
- 盈利准备度审查基础集报告 `/private/tmp/quantmesh-profit-readiness-rc1092-current/results.json`：11 pass、0 fail，JSON 解析错误 0；源码快照测试前后 SHA-256 均为 `a5d8328e80bb7e8af0b8ff413fe98d95d25335118f3a891948f8d370b58fb4cb`，前后端版本一致 `3.111.0-rc1092`。整仓报告 `/private/tmp/quantmesh-profit-readiness-rc1092-full/results.json`：2764 pass、25 fail、21 skip、解析错误 0，exit 1。25 项均在 `httptest.NewServer` 创建本机 IPv6 listener 时被沙箱拒绝；同 25 个精确用例在允许 loopback 的环境单独重跑通过，但这不把原整仓门禁改记为通过，也不覆盖 21 项 skip。
- 进一步调用链审查确认 `strategy/funding_carry_borrow_recovery.go` 在精确核验借款回执后，将债务写入耐久状态并保持 UNKNOWN/in-flight；`funding_carry_stop_recovery.go` 的 BotManager 生产恢复只调用已平仓终态复核器，后者只接受 final-close-verification 或已干净 checkpoint。借款回执状态不满足其准入条件，故目前没有从该 UNKNOWN 到审计核账完成的生产恢复入口。Binance 有分页借还历史查询能力，但仅核对当前负债为零不足以证明历史本金、利息和责任归属已闭合。
- 待确认的恢复契约：是否只提供人工触发的只读核账/耐久审计记录；要求精确借还流水、当前负债、现货/合约仓位及活动委托一致；遇到证据缺失或不一致继续封锁；绝不自动撤单、平仓、还款或重开。该选择影响证据模型及最终清账语义，本轮未擅自实现或解除封锁。
- 工作树仍有大量未提交改动；当前分支 `codex/verified-capital-release-rc876` 基于 `5c048825`，本地领先远端 17 个提交。rc1092 代码及文档尚未提交/推送/发布；未访问真实账户或生产库、未下单。上述测试不构成发布同提交验证、真实权益/负债核账或盈利验收。

## rc1091：启动恢复失败与未启动停止的待核锁存

- 沿借款 ACK 恢复链发现：`Start` 在精确查询已确认借款流水时若遇网络/接口错误，会在 CAS 前直接失败；未启动对象仍持空方向/零债务，`StopContext` 只读内存即可返回 nil。这样控制面可能把“尚未核清的借款”报告为停止完成。新增回归先在旧实现复现该错误结论；另一个 COMMIT 回执未知探针验证了已有 CAS 错误分支会锁存，因此没有修改那条已安全的路径。
- 修复在 `FundingCarryStrategy.Start` 的全部恢复、持仓核验与初始状态持久化失败出口锁存 `startupRecoveryErr`。未成功启动前，`StopContext` 与平账验证不能报告完成，Funding Status/Visualization 的 `reconciliation_required` 也显示待核；仅后续完整启动验证通过后清除。该锁存不清账、不自动重试交易所请求，也不代替 durable UNKNOWN 恢复。
- 最终源码下定向 `go test -race ./strategy -run '^(TestFundingCarryBorrowReceiptQueryFailureKeepsUnstartedStopBlocked|TestFundingCarryStartupRecordsSavedBorrowReceiptWithoutReplaying)$' -count=3` 通过；`go test ./strategy -count=1` 通过（181.453s）；`go test . -run '^TestFundingCarry(FullConstructor|CloseVerification)' -count=1` 通过。`go vet . ./strategy`、`git diff --check`、前后端版本一致性检查通过，版本 `3.111.0-rc1091`。
- rc1091 强制 race 报告 `/private/tmp/quantmesh-trading-race-rc1091-mysql-approved/results.md` 与 `results.json`：10 包 2241 pass、0 fail/skip、0 JSON 解析错误；19 个必需 MySQL 父案例及 62 条父/子证据通过，缺包/缺用例均为零。新增 `TestFundingCarryBorrowReceiptQueryFailureKeepsUnstartedStopBlocked` 明确 PASS；测试前后源码 SHA-256 均为 `2def85e69cd8bd1cba371b4b1baf2ea5a4dfea4a686a43b2ecd4f433b6b9abf2`，前后端版本一致 `3.111.0-rc1091`。首次受限沙箱运行的回环拒绝失败报告另行保留，不作为产品失败；相同源码在允许 loopback 的本机环境原样重跑通过。
- MySQL 使用 `mysql:8.0.36` 一次性容器，ID `edbe6de54852beaa211b334a67690442904318c48dd57d590bcc5c05e65e8fe6`，仅发布到 `127.0.0.1:13309`、`/var/lib/mysql` 为 tmpfs、无宿主机挂载。门禁结束后按该完整 ID 删除；端口 13309 已停止监听，原有 127.0.0.1:3306 服务仍在监听且未连接。
- 门禁结束后只更新了 `CHANGELOG.md`、`PRODUCT_OVERVIEW.md`、本审查记录与 Goal Teams 索引；没有改动被测实现、测试、门禁脚本或版本。因此报告 SHA 精确绑定门禁运行时的实现/测试快照，更新后的文档本身不包含在该报告指纹中。
- 本轮没有真实账户/生产库访问、下单、提交、推送或发布；该隔离 MySQL 门禁不证明生产数据库语义、完整生产重启恢复、真实权益/负债核账、同提交发布验证或可审计净盈利。

## rc1086：MySQL 代际 fencing 多 scope 原子性证据补强（完整强制 race 门禁通过）

- 独立评审指出 rc1085 MySQL 专项虽能证明串行旧 owner Save/CAS 被拒绝且原 payload 不变，但没有逐 scope 核对 generation/token、没有验证部分 claim 失败回滚，也未读回确认新 owner 的写入实际落盘。按该缺口扩展 MySQL 8 fixture：检查每个 scope 的 owner generation/token；对排序末尾 scope 注入 generation advance 失败，确保事务失败后所有 scope 仍为旧 owner；新 owner Save/CAS 后读回 payload。
- 定向 `go test -race ./storage -run '^TestMySQLFundingCarryRuntimeGenerationFencesOldOwner$' -count=1 -v` 通过。Ruby race 门禁契约 20 项/396 断言通过，`go vet ./storage` 与 `git diff --check` 通过。最终完整命令 `ruby scripts/verify_trading_race.rb /private/tmp/quantmesh-trading-race-rc1086-mysql-final --require-mysql` 通过：10 包共 2230 pass、0 skip/fail，18 个强制 MySQL 案例及 51 条父/子路径事件全部 PASS，缺包、缺 MySQL 案例、JSON 解析错误均为零。
- 最终 JSON/Markdown 报告：`/private/tmp/quantmesh-trading-race-rc1086-final-source-retry/results.json` 与 `results.md`；baseline `5c048825cb8a7536b0fe479a0fd6b7c47a73a8ea`，dirty 工作树 SHA-256 `2d774d3f99d45c0d08859f514fac44e915fecccb665119662142f15615864982`，测试前后相同，后端/前端 `3.111.0-rc1086` 一致。验证使用仅映射 `127.0.0.1:13307`、tmpfs 数据目录的隔离 MySQL 8.0.36 容器；随后仅更新本审计记录引用最终报告，未改动被测代码/版本。
- 独立评审复核最终测试实现，确认多 scope generation/token 断言、第二 scope 注错后的 InnoDB 整体回滚及 current owner Save/CAS 读回均有效，未发现明显假阳性；同时确认本轮不覆盖并发 claim/write 竞争、deadlock/有限重试、commit 回执不确定、生产启动整链与滚动升级旧 writer 隔离。
- 仍开放：Redis lease/SQL generation 与已发交易所 RPC 不具跨系统原子性；未验证并发 claim-vs-Save/CAS 死锁/重试、提交回执不确定、真实旧 writer 升级隔离；该证据是 dirty 工作树本地测试，不是干净提交 CI 或发布验收。未访问真实账户或生产库、未发布、未实盘或盈利验收。

## rc1087：Generation fenced runtime 写入的 COMMIT 回执不确定恢复（开发中，完整强制 MySQL 门禁通过）

- 独立调用链审查确认 `SetFundingCarryRuntimeState` 与 fenced CAS 原先在 `tx.Commit()` 报错时直接返回，无法区分“服务端已提交但响应丢失”和“未提交”；策略层安全封锁会防止继续金融动作，但可能令内存/耐久快照分叉并滞留 UNKNOWN。审查未发现该错误路径直接重放借款/还款。
- 为每次 fenced runtime-state Save/CAS 在同一事务中写入唯一 operation ID 回执。COMMIT 返回错误后，经与写池物理隔离的连接做一次有界单语句只读核验；仅当所有 generation scope 仍由本次 token 持有、operation ID 完全匹配且 schema/payload 与目标逐字节一致才确认。owner 接管/读失败/状态不匹配仍 fail-closed，不重放事务。CAS 若确认后原 context 已取消则返回专用可识别错误，而非普通成功。
- 独立审查曾发现 `s.db.QueryContext` 可能复用 COMMIT 出错但仍持有未提交事务的物理连接，造成自身可见数据假阳性；已添加独立 `commitProbe` 池。隔离 MySQL wire fixture 现在注入 COMMIT 前断连、真实 COMMIT OK 后 ACK 丢失、ACK 前 owner 接管，以及向客户端返回普通 MySQL ERR 同时保留原未提交 server session；能分别核对写 session 可见与独立 probe session 不可见。最终 `QUANTMESH_MYSQL_TEST_DSN=... go test . -run '^TestMySQLFundingCarryRuntimeGenerationAdapterCommitOutcomeRecovery$' -count=1 -race` 9 条子路径通过。首轮还发现 MySQL 默认禁用 multiStatements，原迁移将两条 CREATE 合并一次 Exec 会在启动时报 1064；已拆分为 0701 generation 和 0702 receipt 单语句迁移。
- 最终专项命令 `QUANTMESH_MYSQL_TEST_DSN='root@tcp(127.0.0.1:13307)/quantmesh_test?parseTime=true' QUANTMESH_MYSQL_TEST_ALLOW_DESTRUCTIVE_SCHEMA=1 GOCACHE=/tmp/quantmesh-go-cache-rc1087 go test . -run '^TestMySQLFundingCarryRuntimeGenerationAdapterCommitOutcomeRecovery$' -count=1 -race` 通过；九条子路径全部 PASS。`go test -race ./storage -run 'TestFundingCarryRuntimeGeneration|TestMySQLFundingCarryRuntimeGeneration' -count=1` 通过，含实际 SQLite/MySQL 0702→0701 down migration 执行与表删除读回；`go test ./storage -count=1`、root adapter 定向测试、`go vet ./storage .`、Ruby race-gate 契约 20 项/446 断言及 `git diff --check` 通过。最终完整命令 `ruby scripts/verify_trading_race.rb /private/tmp/quantmesh-trading-race-rc1087-final --require-mysql` 通过：10 包 2232 pass、0 skip/fail、19 个 MySQL 父例/61 条父子证据 PASS、无缺包/缺案例/JSON 解析错误；JSON/Markdown 报告存于该目录，source commit `5c048825cb8a7536b0fe479a0fd6b7c47a73a8ea`、版本 `3.111.0-rc1087`、dirty=true、源码 SHA-256 `87c0d9dc219277df8e88e717cb5d51fa7a4e7040ab9a1840ee88d5145a1d59cc` 测试前后相同。之后只更新此审计/变更说明，未改生产代码。
- 未闭合风险：commit-confirmed-canceled 能阻止当前调用把取消当成功，但部分策略调用方在该错误后可能保留旧内存快照或恢复 source；必须逐条保证下一金融动作前重读耐久状态、保持风险封锁且能恢复，而非无限悬置 UNKNOWN。此前 receipt 为每个 bot/strategy 单行，下一写会覆盖旧 receipt；这是保守地降级为 UNKNOWN 的窗口，仍需明确覆盖并发场景验收。Redis lease / DB claim / 交易所 RPC 不具跨系统原子性。本轮未访问真实账户/生产库，未提交、未推送、未发布、未实盘或盈利验收。
- 最终独立只读复审未发现 P0/P1，并确认未提交事务自读反例、probe 隔离和 down 次序验证有效；保留两个 P2：已确认提交后 caller 未统一从 durable snapshot 重同步，以及后续写覆盖单行 receipt 会保守产生 UNKNOWN。完整门禁报告 SHA 为 `87c0d9dc219277df8e88e717cb5d51fa7a4e7040ab9a1840ee88d5145a1d59cc`；报告生成后仅追加/更新 `CHANGELOG.md`、`PRODUCT_OVERVIEW.md` 与本审计文档，未改生产代码、测试或门禁文件，因此当前工作树不得表述为与报告快照完全相同。

## rc1085：代际 fencing 的强制 MySQL 门禁与业务调用链测试

- 将 `TestMySQLFundingCarryRuntimeGenerationFencesOldOwner` 纳入交易核心验证器的强制 MySQL 案例；契约测试要求该证据必须成功、不可 skip/fail、不可缺失或错包归属，避免专用 stale-owner 用例未跑而门禁仍报完整 MySQL 证据。CI/CD 使用 MySQL 8.0.36 service；本机尚无对应隔离服务。
- 新增完整 FundingCarry constructor 路径测试：recovery checkpoint 覆盖 current owner、generation takeover 后 stale CAS 拒绝、持久化错误；close verification 在最终只读核验中途被新 generation 接管时，`StopBot`/`StopAll` 均保留待核状态和 futures/spot/margin 三市场资本预留，不重放平仓或还款。错误注入用指定 `verified=true` 的目标检查点过滤；写入失败后允许耐久保留已核实订单 ACK，但继续要求 `intent_in_flight`、`exposure_unknown`、债务/借款身份及未验证成交证据不丢失。
- 独立审查核验了 CAS 前故障注入、持久 reservation 断言和未知/部分成交状态检查；先后发现并修正 Go cleanup 编译错误、游标参数错误及对合法中间 ACK 检查点过严的原 payload 断言。最终定向根包测试通过；`go test -race` 根包 generation/recovery/close 路径通过，storage generation SQLite race 路径通过；Ruby 门禁契约 20 项/396 断言通过。
- 验收：使用仅映射 `127.0.0.1:13307`、数据目录 tmpfs、资源受限的 MySQL 8.0.36 一次性容器，专用 stale-owner race 用例通过；完整 `--require-mysql` 交易核心 race 门禁共 2230 pass、0 skip/fail，18 个必需 MySQL 案例均有终态 PASS，缺包/缺案例/JSON 解析错误均为零。报告 `/private/tmp/quantmesh-trading-race-rc1085-mysql-full/results.json` 与 `results.md` 记录 baseline `5c048825cb8a7536b0fe479a0fd6b7c47a73a8ea`、dirty 工作树 SHA-256 `560f2b4c2e28fa97c413c42d7e2b7d59a6131bb5f3ce91f5fc36a05df2153af4`，测试前后完全一致，版本 `3.111.0-rc1085`。这关闭了该脏工作树下 MySQL generation fencing 的初始运行门槛，但不代表干净提交/CI 或发布验收；reviewer 发现多 scope 原子回滚和 current-owner 读回断言仍弱，列为 rc1086 补强项。未访问真实账户/生产数据库，未发布、未实盘或盈利验收。

## rc1084：FundingCarry 持久 owner-generation fencing（开发中，未发布）

- 新增 FundingCarry 专用持久 generation claim；启动取得 Redis ownership leases 后，按完整排序 scope 集合一次性 claim，失败则不创建交易所连接、不继续启动。普通 runtime-state Save、恢复/关闭条件写使用 fenced storage API；旧 handle 在新 claim 提交后不能覆盖原 payload。handle 字段封装，通用策略 adapter 不回退/改变。
- 独立验证：`GOCACHE=/tmp/quantmesh-go-cache-fcgen go test ./storage -run 'TestFundingCarryRuntimeGeneration|TestMySQLFundingCarryRuntimeGeneration' -count=1 -v` 通过，SQLite首次claim、全部scope失败回滚、跨进程接管、legacy payload 保留等通过；MySQL因缺少 disposable DSN 显式 SKIP。`go test -race ./storage -run 'TestFundingCarryRuntimeGeneration|TestMySQLFundingCarryRuntimeGeneration' -count=1` 通过（MySQL仍SKIP）；`go test ./storage -count=1` 通过（14.274s）。
- 根包 targeted adapter/startup fail-closed 测试通过；初次 `go test . -count=1` 发现3个旧测试传入nil storage，因启动现在必须先claim generation而未达到原测试点。为保持生产 fail-closed，在测试中补隔离 SQLite fixture，原取消清理/配置隔离断言不变；针对性三例及随后根包全量重跑均通过（后者约157s，筛选器无失败输出、Go exit 0）。`go vet ./storage .` 与差异检查通过。
- 未验收：MySQL实现/迁移运行时（生产启动会使用，构成验收门槛）；恢复 checkpoint 与最终 close verification 两个策略业务调用链未各自端到端驱动；Save使用独立30秒背景context、DB commit错误后的结果核实、SQLite busy/MySQL deadlock竞争尚缺专门证据。Redis lease 与SQL claim非原子、已发/在途交易所RPC不受fencing保护；旧版writer不识别generation，启用前须停止/隔离所有旧writer。未访问真实账户/生产库，未实盘或盈利验收，未提交/推送/发布。

## rc1083：拒絕失租後 FundingCarry 現貨成交提交舊庫存（回歸重現與修復）

- 延伸 ownership 調用鏈時確認，現貨買入後的 `recordStrategySpot` 與現貨賣出後的 `releaseStrategySpot` 原先都會直接改寫 FundingCarry runtime state。失租通知已到達的舊實例仍可覆蓋 `intent_in_flight` 檢查點；平倉側也可能把後續取消/結算流程當作安全繼續。兩條庫存回歸均先在舊實作上失敗，再驗證已知失租會回錯誤、不改內存自有庫存及持久快照，且買賣調用方停止後續步驟。另於共同 `persistRuntimeStateLocked` 邊界拒絕所有已知失租後的直接 runtime-state 寫入，並新增繞過 helper 的持久化拒絕測試。
- 將兩個庫存提交 helper 改為回傳錯誤，於任何狀態變更前檢查 runtime ownership 封鎖；前後端版本同步為 `3.111.0-rc1083`，CHANGELOG/PRODUCT_OVERVIEW 已更新。最終源碼下 `GOCACHE=/tmp/quantmesh-go-cache-rc1083 go test ./strategy -count=1` 通過（181.435s）；4 個失租/直接寫入案例的定向 `go test -race` 通過。
- 仍無法封住租約遠端失效到本地通知之間的競態；尚需 runtime ownership generation 原子 fencing 與完整交易核心/MySQL 驗證。本項不代表真实账户核账、发布或盈利验收。

## rc1082：拒絕已知失租後的 FundingCarry 開倉 ACK 狀態覆寫（定向回歸通過）

- 實際調用鏈發現：合約開倉 RPC 返回 ACK 後，`recordFuturesOpening` 會直接覆寫 FundingCarry 持久 runtime state；若舊實例已收到租約丟失通知，可能覆蓋先前已落盤的 `intent_in_flight` 恢復檢查點。新增回歸先證明原路徑會錯誤接受 ACK，再於狀態變更/寫入前檢查本地 ownership 封鎖；已知失租時拒絕 ACK、標記 UNKNOWN，保留 durable intent。
- `go test ./strategy -run '^TestFundingCarryFuturesOpeningAckAfterOwnerLossPreservesRecoveryState$' -count=1` 與 FundingCarry ownership/opening 定向測試通過。版本已同步至後端/前端 `3.111.0-rc1082`，並更新 CHANGELOG/PRODUCT_OVERVIEW。
- 此修復僅處理本地已知失租；遠端租約過期至本地檢測之間仍無原子 fencing。後續仍需 race/MySQL 故障路徑驗證及持久 owner-generation fencing；不代表真实账户核账、发布或盈利验收。

## rc1081：交易核心 race 门禁绑定确切源码快照（完整 MySQL race 门禁通过）

- 发现 CI/CD 实际执行的 `verify_trading_race.rb` 只记录 HEAD 与 dirty 状态，不像盈利准备度报告那样记录测试源码指纹及前后稳定性；本地并发修改可能令报告无法证明实际验证的源码。新增共用 `SourceProvenance.capture`，复用到两种报告；race 门禁 JSON/Markdown 记录测试前后 SHA-256、前后端版本及稳定性，来源采集不完整、源码变化或版本不一致均 fail closed。
- 新增真实门禁报告合约的源码漂移反例。`ruby scripts/tests/trading_race_gate_test.rb` 为 19 项/388 断言通过；原始盈利审查 runner 契约 `ruby scripts/tests/profit_readiness_timeout_test.rb` 为 10 项/81 断言通过；三个 Ruby 文件语法检查及 `git diff --check` 通过。按 rc1081 当前源码重新运行强制命令 `ruby scripts/verify_trading_race.rb /private/tmp/quantmesh-trading-race-rc1081-mysql --require-mysql` 成功：10 包均 pass、2215 pass/0 skip/0 fail、50 条必需 MySQL 父/子路径全 pass、0 解析错误/缺包/缺 MySQL 用例；根包 193.098s、strategy 183.585s。JSON/Markdown 报告记录基线 HEAD `5c048825cb8a7536b0fe479a0fd6b7c47a73a8ea`、dirty 工作树、rc1081 前后端版本一致、源码前后 SHA-256 均为 `9d051ed6ace3bfa235ad2ccd56c116efc1d750c20568f1660590ff595226c59a` 且 `source_stable_during_run=true`，生成于 `2026-10-07T22:20:18+08:00`。报告 `/private/tmp/quantmesh-trading-race-rc1081-mysql/results.json` 与 `results.md`；测试 MySQL 8.0.36 容器仅映射 `127.0.0.1:13308`、tmpfs、无挂载、无密码 root，完成后清理。
- 无真实账户/生产库访问、下单、提交、推送或发布；仍不构成跨进程原子 fencing、真实资金/权益核账或盈利验收。

## rc1080：延长强制交易核心 race 门禁时限（完整 MySQL race 门禁通过）

- rc1079 的首次强制 MySQL 运行因临时容器设置 root 密码，被仓库明确的无密码 loopback fixture 安全前置条件拒绝；改用仅绑定 `127.0.0.1`、tmpfs、资源受限、无密码 root 的一次性 MySQL 后，所需新增 MySQL 用例实际开始执行。
- 第二次运行 1846 项通过、无解析错误，但根包在 `StreamCleanupAndFinalVerification` 子路径时达到既有 180 秒单包 race 时限，部分用例没有终态证据。将门禁上限提高到 360 秒并增加契约断言后，Ruby 门禁 18 项/380 断言通过；最终强制命令 `ruby scripts/verify_trading_race.rb /private/tmp/quantmesh-trading-race-rc1080-mysql --require-mysql` 返回成功：10 个包均 pass，2215 pass、0 skip/fail、0 JSON 解析错误；50 条必需 MySQL 父/子路径事件全部 pass，缺失包与缺失 MySQL 用例均为空。报告 `/private/tmp/quantmesh-trading-race-rc1080-mysql/results.json` 与 `results.md` 记录版本 `3.111.0-rc1080`、基线 HEAD `5c048825cb8a7536b0fe479a0fd6b7c47a73a8ea`、工作树 dirty，生成于 `2026-10-07T22:11:15+08:00`。测试用 MySQL 8.0.36 一次性容器仅映射 `127.0.0.1:13307`、`/var/lib/mysql` 使用 tmpfs、无密码 root；非生产数据库。测试结束后已清理该精确容器。
- 此结论仅涉及本地隔离测试环境；没有生产数据库/账户访问、真实下单、发布或盈利验收。

## rc1079：盈利准备度诊断脱敏与 JSON 完整性门禁

- 复用已覆盖 bearer/API key/token/password/DSN/webhook/private-key 的诊断脱敏器，所有结构化测试输出、未解析行及 stderr 在写入 JSON/Markdown 前脱敏；任何非 JSON 行或非对象记录都计入解析错误并阻断验证通过。该改进保护验证证据本身，不替代交易核账、原子 fencing 或盈利验收。

## rc1078：盈利准备度报告固定工作树来源身份（报告契约测试通过）

- 报告新增后端/前端版本一致性、dirty 状态与源码快照 SHA-256；快照覆盖 `git diff HEAD` 及所有非忽略未跟踪文件内容和可执行位，并在 Go 测试前后各采集一次，期间提交、版本或快照变化即拒绝通过。Markdown 与 JSON 保留来源身份，防止将脏工作树测试误读为基线提交证据。契约测试 9 项/65 断言通过；在获准 loopback 环境中当前 rc1078 工作树整仓 `go test ./... -count=1` 为 78 包、3156 pass/23 skip/0 fail，报告记录测试前后源码哈希一致，基线 HEAD `5c048825`、工作树 dirty，前后端版本一致 `3.111.0-rc1078`。23 项跳过包含 MySQL DSN/schema opt-in 与 live-network 用例；未运行这些外部集成，亦非同一干净提交发布/实盘或盈利验收。
- 此改动只提升证据可追溯性，不闭合 Redis 与 SQL/交易所 RPC 的原子 fencing，不代表 MySQL 集成、同提交发布、真实账户核账或盈利验收。

## rc1077：同步续租失败沿用原 ownership-loss 通知（定向 race、vet 通过）

- `runtimeOwnershipLease.Validate` 发现当前 Redis token 已失效时，立即进入与后台续租相同的单次 ownership-loss 通知；因此策略开仓被拒绝的同时，风险门与持久化 UNKNOWN/归属状态也会收到事件，不再等后台 ticker 迟到。测试断言同步错误能触发 handler，后续重复 Validate 不重复通知；根包/订单包相关用例 race 通过，根包、订单、策略、exchange、Binance `go vet` 和 `git diff --check` 通过。完整根包/订单包测试在 rc1076 同一接线版本已通过。
- 这是检测时延改进，不是原子 fencing；远端 Redis lease 与交易所 RPC 仍没有共同事务，已发送请求仍可能 UNKNOWN。未访问真实账户。

## rc1076：开仓/资本释放前同步续验运行租约（定向 race 与根/订单全测通过；原子 fencing 仍开放）

- 新增 `runtimeOwnershipLease.Validate(ctx)`：在本地丢失标志以外，主动调用分布式锁 `Extend` 验证当前 Redis token 并续租；FundingCarry/FundingPerpSpread 在每次物理开仓 RPC 前执行，因此明确拒绝重试阶段发现的失效租约；FundingCarry 资本 claim SQL 删除前也重新校验所有腿租约。读到任一失败即将本地 lease 永久标记 lost，既有关闭路径保持不变。
- 新增同步续租失效/取消上下文与物理重试中途租约丢失的回归；租约重验仅通过时才允许第一次下单，拒绝第二次物理 RPC。定向 race 用例通过；获准 localhost fixture 后，根包和订单包全套测试通过（217.7s/14.8s），相关 `go vet` 与 `git diff --check` 通过。
- 尚不能称原子 fencing：Redis 验证与随后 SQL/交易所 RPC 仍非同一事务；若租约在验证返回后立即转移，已进入网络中的交易所请求无法被撤销。必须继续评估数据库单调 fence 对全部关键写入的约束，且交易所侧最终 fencing 能力受 API 限制。本轮无真实账户操作。

## rc1075：durable stop 的管理器恢复接线（相关包全测通过，跨进程 fencing 仍开放）

- BotManager 现在仅在读取到未完成 stop journal 后，调用显式安装的 FundingCarry 恢复器；其余策略/交易所、缺少存储或分布式锁、NopLock、身份/claim 不匹配均 fail closed。SymbolManager 恢复器刷新权威配置、获取 futures/spot/margin 三个 runtime ownership lease，通过 REST-only Binance futures/spot adapter 的受限 `IExchange` wrapper 与 margin evidence adapter 重建策略，只读复核持仓/挂单/负债后才释放 journal 中精确 token 的资本 claim，再释放 lease，之后现有流程才持久停用并退役 journal。
- 新增管理器回调失败保留原 enabled 状态及未完成 journal、成功才结束持久化的测试；另修正停机回调执行期重复 Start 幂等语义，并隔离 StopAll 测试 journal 路径。根包 race 定向用例通过；获准 localhost fixture 后，根包/strategy/exchange/Binance 全套测试通过（分别约 123.8s/181.5s/1.4s/5.0s）；`go vet . ./strategy ./exchange ./exchange/binance`、版本同步与 `git diff --check` 通过。
- 这不是跨进程原子 fencing：Redis lease 是独立 TTL token，ownership guard 的内存检查与 SQL 事务提交之间仍有竞态窗口；SQL 只以 reservation token 阻止释放更新代 claim。必须继续设计并验证单调 fencing generation 与关键写操作的原子约束。恢复器也尚无真实账户核账证据；本次未访问真实账户、下单、提交、推送或发布。当前分支仍有大量未提交改动，不能称同提交发布验证或盈利验收。

## rc1074：Binance 停止核帳專用 REST 證據 adapter（隔离 fixture 通过；rc1075 已接入管理器）

- 为重启后只读核账准备交易所来源，新增 Binance futures、spot、cross spot-margin 三类 stop-evidence adapter。它们按上下文加载指定 symbol 的公开 metadata，不创建订单流/行情流 manager，也不触发生产构造器的账户余额采集、资本预留、价格监控或 usage telemetry。stop-evidence 模式下下单、批量下单、撤单、批量/全部撤单、内转及 margin 借还均被拒绝；margin mutation client 不对外暴露。
- 本地 HTTP fixture 只观察到三次精确的公开 exchangeInfo GET；确认精度/币种加载、所有流 manager 为空、写操作均在发 HTTP 请求前被拒绝。专用测试 `-race -count=5` 通过（`ok quantmesh/exchange/binance 1.327s`），`go vet ./exchange/binance` 通过。首轮沙箱内回环监听被权限拒绝，获准提升后同一隔离 fixture 通过；无交易所账户端点调用、真实凭据、下单或资产变更。
- 截至 rc1074，生产 BotManager 尚未消费这些 adapter，且 recovery 仍需结合 durable journal 的精确配置/claim、取得当前 runtime ownership、对 Strategy 层可选查询能力进行安全委托，然后通过完整的 capital claim/租约/journal 流程提交恢复；管理器接线由 rc1075 记录。构造器单测不等于真实账户核账、跨进程原子 fencing、同提交发布或盈利证据验收。

## rc1073：停止清理状态后的崩溃窗口只读复核（策略层定向验证通过，管理器仍待接线）

- 上轮的持久化 final-marker 恢复只覆盖“待末次只读核验”marker 尚在的崩溃窗口；而成功条件写入清除 marker 后、capital claim/ownership lease 释放和 stop journal 退役前仍可能崩溃。新增 `VerifyPersistedStoppedFlat(ctx, ownershipGuard)`：在当前 stop recovery 持有运行归属的前提下，只导入严格解码、账户作用域精确且已完全 resolved 的 schema 7 状态，再经原有 flat 核验重新读取负债、持仓、挂单和 durable state。它不要求或伪造 final marker，不接受 UNKNOWN/in-flight，不启动生产者，不提交 CAS/资金动作，也不释放 claim、租约或 journal。
- 新增真实 marker 完成并 CAS 清理后用第三个全新策略实例复核的 race 回归，以及 marker 未清理时必须拒绝的回归；六项定向策略 race 用例三轮通过（`ok quantmesh/strategy 43.775s`），`go vet ./strategy`、前后端版本一致和 `git diff --check` 通过。编译输出含依赖 `go-m1cpu` 的既有 VLA 扩展 warning。方法仍无 BotManager/API 生产接线，因此不能恢复 durable stop 流程或收尾释放；账户所有权 claim 原子 fencing、十包/MySQL、发布及可审计盈利证据仍未闭合。未提交、未推送、未发布；无真实账户访问或下单。

## rc1072：持久化 FundingCarry 停止末阶段的只读核验器重建（策略层已接入，BotManager恢复流程待接线）

- 现有 `ReconcileStoppedMarginClose` 只接受同进程保存 stopErr/runDone 的原实例；崩溃重启后即使 schema 7 精确写入 `margin_close_verification_pending`，仍不能复用既有只读终验。新增 `ReconcilePersistedStoppedMarginClose(ctx, ownershipGuard)`：要求新实例未启动、有 cancellable state reader、schema 7、精确交易所/市场/符号及 margin account scope、明确 pending marker、实际 liability reader及非空当前 runtime ownership guard；guard 在 checkpoint 读取、交易所证据读取前后、条件 CAS 前后检查。恢复仅导入账本并重用原 verifier 的负债、仓位、账户挂单、钱包协调和 payload CAS 检查。这仍不是 DB CAS 内的 fencing token；跨进程原子租约 fencing 与管理器接线仍开放。
- 从持久状态导入 ExposureUnknown/in-flight 原值，只有完整权威证据通过与 CAS 提交后才由共享验证路径清除；缺失标记、错账户、缺能力、读失败、取消、源版本变化仍封锁。入口不启动 loop/stream，不执行新订单/借款/还款/取消，不释放资本或 ownership lease；这些释放仍必须由管理器级流程在它自身取得 durable journal 和新租约后单独完成。
- 新增真实 donor-stop checkpoint 后创建全新策略实例的恢复正例、无 marker 拒绝例及核验前/核验中租约丢失不得 CAS 的回归。首轮 race 遇到真实夹具表明合法 final marker 同时保留 `ExposureUnknown=true`，新入口初版错误拒绝；已改为保留该标记直到共享只读证明成功，避免将其预先清掉。策略包五父用例 race 三轮通过（`ok quantmesh/strategy 38.991s`），`go vet ./strategy` 与 `git diff --check` 通过。当前还没有 BotManager/API 生产构造器接线，不能称为完整重启金融恢复；十包/MySQL门禁、发布及盈利验收仍未闭合，无真实账户访问或下单。

## rc1071：区分批量停机回调与完成态 journal 持久化（定向回归通过，未提交）

- 本轮在最新工作树实际运行 StopBot/StopAll 排空重试及 journal 重启定向 race 时，发现 StopBot 用例通过而 StopAll 用例失败：排空取消后，实际热更新报告 `bot_stop_persistence_pending`，但准确状态应为 `bot_stop_drain_pending`。journal 在调用金融停止回调前已成功耐久化，错误来源是 StopAll 把“停机回调仍进行中”复用了“完成态 journal 尚待持久化”标记。
- 增加独立的 stop-transition-in-progress 标记供 API/准入状态覆盖回调执行期；只在停止回调成功后设置 persistence pending。失败分类仍按 drain/ownership/verification/cleanup/reconciliation 分流，不清除已有 durable stop journal，不允许启用或热更新，也不放宽开仓安全门。
- 初始七项定向 race 检查暴露上述 StopAll 状态误分类；修复后 StopBot/StopAll 排空分类与重试、停止租约重试及 journal 目录同步重试三组根包父用例 `-count=3` 的 race 执行通过（`ok quantmesh 3.660s`），根 `go vet .` exit 0，`git diff --check` 通过；前后端版本一致 `3.111.0-rc1071`，API 启动仍以 `web.SetVersion(Version)` 接线。此次仅为定向根包验证，不代表全仓/十包/MySQL/发布验收。完整重启金融核账/恢复及盈利证据仍未闭合；无真实账户访问、下单、提交、推送或发布。

## rc1070：停止意图目录同步失败后重用 operation 重试（定向验证通过，未提交）

- 沿 rc1069 的 journal 原子写路径复核失败边界：初始 hard link 已发布但目录 `fsync` 报错时，方法此前未把 operation 保存进 BotRuntime；重试会把同一 runtime 已发布的 intent 当成外部冲突长期悬置。调用方在 fsync 成功前不会执行金融回调，因此可安全只重试同一个 operation 的持久化确认。
- 现在在 initial publish 前将随机 operation/path/mode 绑定到生命周期锁保护的 BotRuntime；同一 runtime 重试时验证已发布 journal 的 operation，补目录同步，或在尚未发布时在原路径以同一 operation 重试 hard link。不同 operation 仍 fail-closed，不做跨 runtime 接管。第一次十项 race 暴露测试中状态文件路径变化时误在新路径复制 journal；固定重试原始路径后，同一命令十项根包生命周期/故障注入用例 `-count=3` 通过，`go vet .`、gofmt 与 `git diff --check` 通过。仅根包，不外推十包/实库/分布式 fencing/完整金融恢复；未提交或发布。

## rc1069：批量 StopAll 崩溃前持久化意图（定向验证通过，未提交）

- 发现 `StopAll` 与 `StopBot` 保护不一致：前者在金融停止回调前不写 journal；回调报错或进程骤停后，新进程可能只读到仍启用的 Bot 状态并自动启动，绕过待核账封锁。
- 新增 `shutdown` journal mode：回调前持久化且显式记录原启用状态；中断日志仍 fail-closed。成功回调后先写完成态再退役，启动/显式启用路径在生命周期与 journal 锁内识别完成态并清理，不重放金融回调、不把干净 StopAll 错改成永久禁用。若仍是 incomplete，一律要求核账，不自动猜完成。
- 增加进程骤停、回调前日志读回、成功 StopAll 保留 DB 启用态、完成态重启续启、资本/租约重试及显式 StopBot 兼容用例。九项根包用例 `go test -race . -run '^(TestStopJournalSurvivesAbruptProcessExit|TestDurableStopReconstructionCanFinishPersistenceWithoutFinancialReplay|TestInterruptedStopCannotBeGuessedCompleteOnReconstruction|TestValidateDurableStopCapitalClaims|TestStopAllDurablyGuardsInterruptedStopWithoutDisablingCleanShutdown|TestCompletedShutdownJournalIsRetiredBeforeEnabledBotStart|TestFundingCarryManagedStopRetriesOwnershipRelease|TestStopPersistenceFailureCanRetryWithoutRepeatingShutdown|TestRuntimeStopPhasesNeverReplayUnknownFinancialResult)$' -count=3` 通过；根 `go vet .`、格式及 `git diff --check` 通过。仅根包定向验证，不外推为十包门禁、实库、跨进程分布式 fencing 或全金融恢复。原有未提交工作区继续保留；本版本未提交或发布，也不构成实盘/盈利验收。

## rc1068：停机日志保留资金预留代际凭据（定向验证通过，未提交）

- 复核 rc1067 控制面修复后，发现不完整停止日志虽然可见且继续封锁，但只持久化 Bot 状态和操作号；精确 `reservation_token` 仅在运行时内存。进程重启后，历史只读预留列表不提供 token，无法安全释放旧代或判断是否已由新代接管。
- 停止意图现于金融停止回调前封存最小 claim（wallet、代际 token、金额及交易归属），完整日志沿用同一证据；解析时拒绝无效 token、金额和重复钱包。日志原子写入及现有私有目录/文件权限保持，API 不暴露 token。旧格式无 claims 日志仍可读，但本改动不提供自动金融恢复/释放入口。
- 新增真实子进程骤停用例，分别检查不完整与完整阶段重启后精确保留代际凭据；还验证会将预置 0755 目录收紧至 0700、日志文件为 0600，并拒绝损坏凭据，同时接受旧日志无 claims。五项根包 race 用例各三轮通过（`go test -race . -run '^(TestStopJournalSurvivesAbruptProcessExit|TestDurableStopReconstructionCanFinishPersistenceWithoutFinancialReplay|TestInterruptedStopCannotBeGuessedCompleteOnReconstruction|TestValidateDurableStopCapitalClaims|TestReconstructedDurableStopActualAPIReportsPending)$' -count=3`），`go vet .`、格式和 `git diff --check` 通过；完整金融核账/恢复仍未实现。后续恢复仍须接入账户/仓位/委托/借贷权威核账、对账结果及代际 CAS 后才能操作资金；未声明真仓或盈利验收。

## rc1067：重建后持久停止待核账的真实控制面状态（开发中，未提交）

- 上轮存储代际验证为进展。本轮追实际recoverDurableStop发现不完整日志仍需金融核账，完整日志可结束持久化，但无内存runtime时实际ListBots/GetBot遗漏stop_pending，无法区分普通停止与仍封锁的待核账。77823实际适配器红测exit1/根8.015s，未完成/完成未退役/读取失败三模式列表及详情均失配，无日志对照通过。
- 共同控制状态填充读取持久日志，存在日志或读取错误均StopPending=true/Running=false，不增加锁、写入、清除日志、金融重放或原始诊断暴露，已有前端i18n待核账展示直接消费原字段；同实例待核及重建后的封锁一致。
- 本轮独立重跑：初始默认GOCACHE写入受沙箱拒绝；改用`/private/tmp/quantmesh-stop-status-rc1067.tWVZSs`后，GOTOOLCHAIN=go1.25.4根包`go test -race`四父例（重建API四状态、进程骤停日志重建、租约待核控制面及未知停止不可猜测完成）`-count=3`通过，输出`ok quantmesh 8.672s`；根包`go vet .`exit0，diff --check通过。临时缓存仍在，仅验证构建缓存，不含用户数据。
- 不完整/不可读日志继续拒绝StopBot自动猜完成并拒绝Enable，仅列表/详情现在可见，尚无金融核账后修复/退役不完整日志的完整恢复入口。此修复防止控制面把仍封锁的Bot显示成普通已停止，但未消除人工/运维恢复阻塞；下一步需实现有权威账户、订单、仓位及借贷证据的受控金融核账与日志CAS退役，不以UI重试代替核账。完整盈利准备度、全套金融重启恢复、MySQL/跨进程fencing、同提交发布仍开放；未提交/推送/发布/部署/真实账户访问。
- 验证（2026-10-06，接手续跑，go1.26.3）：实际四模式适配器回归、骤停日志、租约待核控制面及中断停止不可猜测完成共4例三轮race全部PASS（ok quantmesh 19.987s）；根包vet无告警，根包全量race一次PASS（ok quantmesh 311.225s）。仅根包，非十包门禁，17项MySQL必需证明未重跑，不得继承rc1065结果。完整金融核账恢复/跨进程fencing/同提交发布及R01–R15/盈利仍开放，无提交/推送/发布/部署或真实账户访问。

## rc1066：替代预留提交后的独立进程骤停与旧代原子释放（开发中，未提交）

- 上轮完成真实断连证据为进展。本轮核实既有旧代释放测试仅同store单钱包，补SQLite真实子进程Reserve提交后os.Exit(23)绕过Close/defer，父进程重新开库后重放原token三腿释放；替代发生在排序第一腿或最后腿，重复两次均必须拒绝、所有资本token/金额/归属元数据保留，最终当前代释放成功。仅增加测试，不改生产金融语义，不将测试缺口当已证实生产漏洞。
- 38626首轮三次失败exit1/storage1.481s，夹具遗漏已识别钱包必需的持久余额观察序列，尚未达到子进程退出点；补父/子进程真实BeginAccountWalletBalanceObservation，不放宽生产校验，待最终验证。完整策略/停止日志/金融恢复、真实MySQL、跨进程原子fencing及R01–R15/盈利均未闭合，无提交/推送/发布/部署或真实账户访问。
- 36607最终第一/最后腿三轮race终止exit0/storage3.303s；11158 storage vet及diff通过。30156复用现有报告器只指定storage包/count3/timeout180s，实际终止exit0/success=true/storage62.445s，552顶层PASS与27外部SKIP（均含三轮重复），不是十包门禁，17项MySQL必需证明缺失不得继承rc1065结果。JSON/Markdown `/private/tmp/quantmesh-capital-generation-rc1066.hFd83u/storage-three-round/results.json`及results.md读回rc1066/HEAD5c048825/source_dirty=true，新进程父例各三次PASS、无解析错误或失败诊断。
- 本轮只有测试和前后端版本/记录更新，没有生产修复红绿证明，也未修改生产金融或SQL实现。临时SQLite由测试TempDir清理，报告保留；无新容器、真实账户、提交/推送/发布/部署。已证明替代SQL提交后骤停的存储代际拒绝与多腿原子回滚，但未证明完整构造器接管、停止日志可恢复、COMMIT前后崩溃时序或跨进程原子fencing；下一步应沿完整恢复接线验证，不将存储单层证据扩大为策略验收。

## rc1065：真实MySQL驱动COMMIT连接中断（开发中，未提交）

- 上轮实际修复与验证为进展。本地go-sql-driver/mysql@v1.8.1.transaction.go证实Tx.Commit走真实mc.exec(COMMIT)，不将rc1063/64事后包装错误当连接层证据。本轮仅测试/门禁/版本变化，生产金融行为不变。
- 专用代理只允许空凭据root/127.0.0.1/quantmesh_ctor_合成schema，无TLS或封包/SQL内容日志，逐帧有界转发；在独立真实SQLStorage释放回调武装，只截断COMMIT发送前，或收到真实服务器OK但不转给驱动。不得伪造驱动错误，必须真实释放返回非nil、代理命中一次、预期OK计数和独立原存储SQL三腿/零行结果匹配，当前原归属/停止待核状态/账本不变，重新核验后才正常幂等释放且零金融重放。
- 代理清理关闭监听与活跃连接，等待accept退出后才等待worker及双向转发退出，SQLStorage先关闭；不新增生产锁路径。MySQL StopBot/StopAll发送前/回执后四子路径新父例列入必需门禁，待真实race和完整验证。没有实盘/线上数据库/凭据访问，无提交/推送/发布/部署，R01–R15/未知远端结果/崩溃恢复/跨进程fencing及盈利仍开放。
- 91187真实驱动首轮race已终止exit0/根14.332s；9520门禁契约18项378断言、超时契约8项47断言、根/storage/strategy vet及diff检查通过。68016新断连四路径三轮race已终止exit0/根38.535s；49449原故障指针/所有权六模式三轮race终止exit0/根2.264s。68016命令中的可选marker名称未匹配，六模式证据来自单独49449，不将未匹配正则当已执行。
- 68016随后独立执行原十包count1/timeout180s/--require-mysql，最终exit0/success=true：2198顶层PASS、17项必需MySQL父例及50条父子终态，无缺包/实库证明、解析错误或失败诊断；根164.718s/strategy171.973s/web87.223s。JSON/Markdown `/private/tmp/quantmesh-capital-wire-rc1065.mfzVk6/race/results.json`及results.md读回source_version=rc1065/source_commit=5c048825/source_dirty=true，新断连四子路径各一次PASS。
- 所有本轮验证会话终止后核对容器ef34d5c0f7b34bb6e4432639436e8f46f8fcc8d79c98b5274e6ae4fb7e16d2c4、isolated-audit-rc1065标签、tmpfs/no mounts和quantmesh_ctor_ schema为空，精确回收本轮容器及不可恢复的临时测试数据库，标签查询为空；报告保留，未删除主机文件或访问已有数据库。
- 本轮是受控COMMIT发送前与已收到服务器OK之后两端对照，生产金融语义不变；不得推断发送后但服务器结果未知的全部时序、进程崩溃重启、跨进程原子fencing、同提交发布或真实盈利已验收。版本前后端rc1065一致，无提交/推送/发布/部署、全仓Go/UI渲染/嵌入/Linux或真实账户访问。下一步仍需补跨进程/重启恢复与新代预留不被旧代释放的经济边界，而非继续仅靠同实例内存marker证明。

## rc1064：资本释放未知结果的诚实诊断与控制面接线（开发中，未提交）

- 上轮完成完整构造器/实库验证为进展，本轮修正其直接暴露的诊断矛盾：SQL删除已提交，生产错误及shutdown marker仍称capital reservation retained。84442实际SQLite StopBot提交后回执丢失红测exit1/根3.259s，断言位于共享构造器254行。不得靠错误文案推断SQL预留是否仍存在。
- 生产只改该错误为capital release is unverified; runtime ownership release withheld，保留cause与原指针/所有权守卫，不增加金融/SQL重放。措辞指未主动释放，而不保证租约未失效；guard可能已检测租约丢失，不能泛化声称仍持有所有权。实际构造器新增提交前未执行SQL错误/以取消子ctx调用SQL，以及原提交后错误/子ctx取消对照；前者SQL三腿预留仍在，后者SQL删除确为零，但两者诊断均不猜测资本实际结果，当前归属保留夹具可只读核验恢复。
- 在真实资本失败窗口补实际Enable/Start/热更新拒绝、状态adapter Running=false/StopPending=true，查询故障不改账本或重复释放，恢复后完成原费用/还款/意图账本及控制器释放。SQLite/MySQL StopBot/StopAll四故障共八子路径，MySQL门禁强制八路径，待最终验证；R01–R15/未知SQL实际结果/崩溃及跨进程fencing/发布/盈利不缩减，无提交/推送/部署/真实账户访问。
- 95358首轮SQLite八路径raceexit0/根19.997s；最终措辞进一步限定为runtime ownership release withheld，只承诺没有主动解锁，不保证租约仍有效。96437门禁18项353断言及超时8项47断言、根/storage/strategy vet/diff检查通过。
- 34883最终SQLite/MySQL八停止路径及原marker六模式三轮race已返回根123.386s/exit0，继而启动独立完整十包count1/timeout180s/--require-mysql，目标 `/private/tmp/quantmesh-capital-outcome-rc1064.odtTrx/race`。不把三轮组合当发布门禁，待完整终态及当前版本证据读回；未继承rc1063的实库结果。
- 34883独立十包门禁终止exit0/success=true：2197顶层PASS、16项必需MySQL父例与45条父子终态，无缺包/实库证明或解析错误；根152.559s/strategy171.722s/web86.511s。JSON/Markdown `/private/tmp/quantmesh-capital-outcome-rc1064.odtTrx/race/results.json`与results.md读回source_version=rc1064/source_commit=5c048825/source_dirty=true，八条SQL资本停止对照均PASS，failure_diagnostics为空。
- 隔离mysql:8.0.36容器674ca0735a046c51bdbe322c9a8ded46251b4fb9ade6a6d93e35d3fb20e2d191标签isolated-audit-rc1064、仅回环32789/tmpfs/no mounts；所有会话终止且quantmesh_ctor_ schema为空，精确身份核对后回收本轮容器/临时测试数据，报告保留。没有访问已有数据库、主机挂载或真实账户。
- 前后端rc1064一致，Ruby语法/vet/diff均验证；本版未做全仓Go、UI渲染/嵌入/Linux/同提交发布，未提交/推送/部署。修复诊断事实，不将确定性提交前/后注入当真实TCP回执丢失、未知COMMIT结果、崩溃重启或跨进程fencing验收；R01–R15及实盘盈利仍开放。下一步可在严格隔离MySQL中补真实驱动/连接层COMMIT回执中断的来源证明，不访问真实账户或线上数据库。

## rc1063：资本SQL提交后回执丢失的非零仓位构造器证明（开发中，未提交）

- 上轮告警修复及验证为进展，本轮返回经济恢复主线。既有资本SQL释放按确切token和钱包锁幂等，rc1058/59只在干净零敞口资本查询故障中验证；本轮复用非零反向0.4仓位、0.4008买回/0.0008费用/还款81、实际执行器及SQL持久账本构造器，补资本提交后错误窗口，不将测试证据缺口说成新发现的代码缺陷。
- 私有构造器releaseCapital依赖只包裹金融停止后的实际SQL释放调用，默认仍原store，VerifyStoppedFlat与当前归属守卫始终在前；隔离故障先真正调用ReleaseAccountWalletCapital提交，再注入应答错误或仅取消SQL子请求。19740首次SQLite四停止子路径raceexit0/根11.089s，无行为红测或新增金融语义修复声明。
- 要求提交后SQL资本确为零，三腿归属仍持有，资本阶段新marker不同于先前金融只读marker，禁启用；再次只读查询故障保留该marker/账本且不得再调用SQL释放，恢复查询后才允许第二次幂等释放、租约/停止日志/控制器完成。原平仓/买回/还款各一次、费用/借贷/执行归属断言均保持。新增同夹具MySQL父例及四子路径列为必需门禁，待实库/完整验证。
- R01–R15、真实网络/未知SQL提交状态、崩溃及跨进程原子fencing、其他UNKNOWN处置/权益/发布/盈利仍开放，不因SQL确已提交夹具而认定这些均已完成；未提交/推送/发布/部署或访问真实账户。
- 52018首次Ruby门禁18项322断言失败，仅精确父子计数断言仍为31，新资本父例/四子例令最小完整证明增加到36；修正计数后31230最终18项333断言及超时8项47断言、根/storage/strategy vet和diff通过，不将初次失败算通过。
- 2199六父例(SQLite/MySQL新资本提交、旧CAS提交与混合清理)三轮组合在根181.752s终止exit1，后续完整门禁因set-e未启动；原输出截断，未获得结构化JSON，不能排除其早期其他失败或据此认定死锁。按build-test-acceleration技能区分编译和执行，未清缓存、改生产期限或缩减用例；缓存保持/Users/rocky/Library/Caches/go-build与/Users/rocky/go。
- 55271使用临时诊断包装器仅指定同六父例/三轮，组合预算360s，报告目标 `/private/tmp/quantmesh-capital-commit-rc1063.wjIBG9/targeted-360`，必须读回实际Go exit_code、全部六父例各三次PASS及失败诊断，不能把该局部报告当完整十包/发布门禁。原完整门禁仍保持count1/timeout180s/16项MySQL必需父子条件，待后续独立执行。
- 55271诊断最终实际Go exit_code=0/success=true：六父例各三次PASS、根158.949s，无缺轮或失败诊断；报告JSON/Markdown已读回。实测耗时低于原180s并不反向定位首轮截断输出的唯一原因，也不代表改预算造成提速；无缓存清理/重启/生产期限或金融语义变化。
- 4946独立正式十包count1/timeout180s/--require-mysql终止exit0/success=true：2197顶层PASS、16项必需MySQL父例及41条父子终态，无缺包/实库证据或解析错误；根134.023s/strategy171.968s/web91.532s。`/private/tmp/quantmesh-capital-commit-rc1063.wjIBG9/race/results.json`与results.md读回source_version=rc1063/source_commit=5c048825/source_dirty=true，新SQL资本提交后的StopBot/StopAll应答错误/子请求取消四子例均PASS，failure_diagnostics为空。未将局部包装器当正式门禁。
- 新建隔离mysql:8.0.36容器92fc6bcde1f1e6c8d3990a1b5667d8a7bad0b6ee9a30e4a99c8eec0e7c1c8b41仅回环32788、标签isolated-audit-rc1063、tmpfs/no mounts，所有测试后quantmesh_ctor_ schema查询为空，精确身份核对后回收本轮容器与临时测试数据，保留全部报告。无已有数据库、主机目录或真实账户访问。
- 所有验证会话已终止，前后端rc1063一致；仅补生产故障注入边界、回归与门禁证据，没有修复未知SQL提交/真实断连或分布式原子fencing的声明。未运行本版Go全仓、UI渲染/嵌入/Linux/同提交发布，未提交/推送/部署，R01–R15与真实盈利仍开放。新证据还暴露旧错误文案“capital reservation retained”不能在已提交删除回执丢失时保证SQL预留仍存在，后续需明确结果未知/归属保留的可观测性，不据文案推断实际资本状态。

## rc1062：CPU告警采样与口径兼容（开发中，未提交）

- 上轮完成代码及验证属于进展。本轮核实阈值、watchdog检查/消息/通知payload均直接用CPUPercent，容量归一化后八核100%进程用量变12.5%，旧80%阈值与20点增量阈值均失敏；42312真实检查与通知红测exit1，八/三十二核固定阈值及增量失败，一核正常。
- 新生产CollectSystemMetrics与确定性夹具共用readProcessCPUSample，单次读取同时保留容量百分比及内部原始进程百分比；告警固定阈值、增量、通知文本及cpu_percent使用原始单核100%口径，通知另外标明basis/容量占比，API/持久化容量百分比不重写。旧内存调用无来源标记保留其原单位，两类来源不混算增量；不据此反推旧数据库数据。内存超限原先经混合检查也触发CPU告警，已分开CPU比较。
- 需验证同次读取/无原始字段JSON暴露、实际watchdog固定/增量通知、内存单独超限与混合来源拒绝；待最终race/vet/完整门禁。陈旧缓存/展示与历史口径、真实整机或容器quota/瞬时监控、资本SQL应答丢失/非零仓位实库恢复及R01–R15均未缩减，无提交/推送/发布/部署/真实账户访问。
- 11728首轮三轮race及monitor/web vet通过；99041最终生产采集、同次读取、单/八/三十二核固定阈值与通知、增量通知、双向混合来源拒绝、仅内存超限与API回归三轮raceexit0/monitor2.264s/web2.853s，monitor/web/config vet及diff检查通过。31262门禁18项308断言/超时8项47断言通过。
- 85599完整十包race已终止exit0/success=true：2180顶层PASS/15MySQL SKIP，无缺包/解析错误；source_version=rc1062/source_commit=5c048825/source_dirty=true。`/private/tmp/quantmesh-cpu-alert-units-rc1062.rfTg5U/race/results.json`与Markdown已读回，新增六父例与旧EmptyStorageFallback均PASS，根82.940s/web92.686s/strategy171.027s，failure_diagnostics/unparsed_output为空。原会话等待至终态，无缩减/重启。
- 既有watchdog.notifications.fixed_threshold.cpu_percent和rate_threshold.cpu_increase配置值保持不变，当前生产含义分别为进程生命周期平均单核100%用量阈值及同口径百分点增量，非总容量占比/瞬时CPU/容器quota。原整机fallback已在rc1060移除，不能称完整历史混合来源或整机告警兼容；API卡片仍需更明确的口径说明与新鲜度处理。
- 所有本轮会话已终止，前后端rc1062一致；配置源码无净变更，新增采样逻辑独立46行文件，未在超长配置文件追加实现。未运行当前版Go全仓、隔离MySQL、UI渲染/嵌入/Linux/同提交发布，无提交/推送/部署/真实账户访问。R01–R15仍开放，下一步继续资本SQL提交应答丢失及非零仓位恢复的实际构造器/实库证据，不用监控回归替代经济闭环。

## rc1061：监控失败不得冒充健康零值（开发中，未提交）

- 上轮只读回查改变下一步行动：CPU归一化改变既有告警语义，且提供者采集失败/API提供者失败两层均能生成新时间零值。本轮优先闭合后者实际生产出口，不把阈值迁移或旧缓存有效性当已经解决。
- rc1060遗留78077 vet/diff已终止exit0；29208门禁18项308断言及超时8项47断言通过。rc1060无完整十包结果，不能继承rc1058通过或以rc号数量验收。
- 64055真实提供者红测两子例失败：current返回新时间零值且nil错误，empty_history返回成功单点。16573真实Gin路由三子例均HTTP200零值（无提供者/采集错误/空结果），红测exit1。新增只读采集依赖注入，默认仍实际CollectSystemMetrics，无真实账户访问。
- 提供者无可用读数返回包裹原cause的错误，空历史回退保留错误；实际当前HTTP处理器提取独立文件，对无提供者/错误/nil返回503与固定错误码，不输出原始错误。正常零值保留。既有错误假正常契约改为明确503，并需新真实路由验证；旧缓存/历史来源与新鲜度、告警阈值兼容、资本SQL提交应答丢失/非零仓位实库恢复及R01–R15全部保留开放。
- 4995首轮及6714最终实际提供者/HTTP正反例与CPU采集三轮race均exit0；最终monitor2.066s/web2.735s，包含真实零值HTTP200与不可用503、空历史真实HTTP500。monitor/web vet及diff检查通过。93227门禁18项308断言/超时8项47断言通过。
- 80126完整十包race已终止exit0/success=true：2174顶层PASS/15MySQL SKIP，无缺包/解析失败；source_version=rc1061/source_commit=5c048825/source_dirty=true。`/private/tmp/quantmesh-metrics-unavailable-rc1061.YzUs5G/race/results.json`与results.md逐项读回，新增三父例、CPU父例与旧失败EmptyStorageFallback均PASS；根85.351s/web85.146s/strategy173.555s，failure_diagnostics及unparsed_output为空。
- 前端请求层非2xx抛错，SystemMonitor当前失败分支清空当前数据，仅源代码读回，未做浏览器/渲染验收。历史失败仍可保留上次图表，watchdog缓存/DB旧样本仍可能作为当前值且无新鲜度拒绝，本轮未解决。CPU按逻辑核数归一化后既有固定阈值含义变化，不能以测试通过证明告警敏感度兼容；应优先补指标口径/阈值接线，再继续资本SQL应答丢失与非零仓位实库恢复。
- 本轮所有验证会话已终止，前后端rc1061一致，未运行当前版全仓./...、隔离MySQL、前端构建/嵌入/Linux/同提交发布；无提交/推送/部署/真实账户访问。不得继承旧版实库或以本地十包通过缩减R01–R15、实盘与盈利验收范围。

## rc1060：CPU指标口径与完整门禁失败（开发中，未提交）

- rc1059真实完整门禁捕获CPUPercent=101.370776违反Web测试0–100。本地gopsutil/v3@v3.24.5源码证实CPUPercent=100*累计进程CPU时间/存活秒，多核可超过100且是生命周期平均，不是首次间隔采样零；旧代码又在零/失败时替换系统CPU，混合了来源。
- 新采样按runtime.NumCPU换算进程占系统逻辑CPU容量百分比，与本进程内存/ProcessID及0–100 UI/告警范围保持一致；零值保留，读失败返回上下文错误，非有限/负值/超过总容量拒绝，不钳位或删除原测试。新增十模式及读错误/缺reader契约待验证。
- 历史库/聚合数据不重写、不反推原采样来源；该量非瞬时CPU或容器quota，现有展示未额外新增UI字段，不能声称指标模型全部完成。完整门禁、资本SQL应答丢失/实库恢复、其他R01–R15及发布/盈利仍开放，无提交/推送/发布/部署或真实账户访问。

## rc1059：停止持仓核验查询取消（针对性完成，完整门禁失败，未提交）

- 上轮明确查询前/返回后context终止仍未分类，本轮新增查询未执行/返回后证据拒绝及实际VerifyStoppedFlat取消后账本不变、新请求重核回归；普通VerifyFlat仍保持UNKNOWN。待红测与修复，不将取消返回的空数组当平仓证据。
- 99725红测exit1/strategy1.130s：查询前/后取消缺positionSnapshotQueryError来源分类，实际停止flat取消改写持久UNKNOWN。只把两处context取消包裹为原读取不可用错误，保留errors.Is；无效入参/空快照/错币对等验证错误仍不采用此类型，普通同步仍封锁。
- 4882策略取消、停止flat六模式、普通同步/上下文/归属三轮raceexit0/strategy2.093s。新增完整构造器StopAll_deadline实际等待生产独立核验请求超时，故意返回空数组以证明不被接受；两次失败保留原指针、SQL三腿资本与账本payload/schema，新请求恢复后才释放，不重播金融或成功流清理。47245首轮实际StopBot/StopAll及deadline三子例raceexit0/根32.773s；97016当前三轮同组合仍运行。
- 根/strategy/storage vet、门禁18项308断言及超时契约8项47断言通过。5645完整十包race仍运行，报告目标 `/private/tmp/quantmesh-capital-cancel-rc1059.mQxoeF/race`；无MySQL DSN，不继承旧实库/全仓/发布证据。
- 97016最终构造器三轮race已终止exit0/根98.205s，包含真实StopAll独立核验deadline两次失败到第三次新请求成功；没有缩短生产核验超时来绕过边界。该夹具金融停止前状态为干净零敞口，实际超时不涉及未知金融提交结果，不证明非零仓位/还款链路的全部取消恢复。
- 5645完整十包终止exit1/success=false：2169顶层PASS/15MySQL SKIP/1FAIL，缺web通过；根81.711s及strategy174.304s通过。rc1057补的failure_diagnostics留住本次直接证据：web.TestSystemMetricsProvider_GetMetrics_EmptyStorageFallback，system_metrics_provider_test.go:60，CPUPercent=101.370776违反0–100断言。JSON/Markdown `/private/tmp/quantmesh-capital-cancel-rc1059.mQxoeF/race/results.json`与results.md保留，不覆写失败或用单例重跑算门禁通过。
- 本次证据定位到CPU指标范围；此前rc1056相同父例失败没有断言留存，不能反向断言其必定同因。采集代码使用进程CPUPercent且可回退系统CPU，口径与指标范围的契约需下一步从生产采集/展示/阈值消费者核对修复，不简单钳位或删除断言。
- 所有会话已终止，前后端rc1059一致，diff检查通过；金融取消修复的针对性证据成立，但完整门禁未过，其他R01–R15、SQL提交应答丢失、非零仓位/实库、发布与盈利仍开放，无提交/推送/发布/部署/真实账户访问。
- 完整资本释放构造器取消串联、SQL提交应答丢失、非零仓位及实库恢复、崩溃/跨进程fencing和R01–R15/发布/盈利仍开放，无提交/推送/发布/部署或真实账户访问。

## rc1058：金融停止后的独立资本核验恢复（本地针对性及十包完成，未提交）

- 完整构造器夹具只在订单流已停后故障期货查询，定位金融停止完成后的独立资本核验；实际StopBot/StopAll应保留原故障及资本/租约、重查，恢复后核验释放且不重复金融/流操作。待红测与修复。
- 13619完整构造器红测exit1/根1.468s，两路径没有重查且查询恢复仍被通用故障封锁。资本释放阶段新增私有候选、精确原指针CAS与当前租约检查；普通金融错误/其他同文本新指针/外部清除原指针/核验前或核验中归属丢失不允许借此释放。BotManager复用停止待核验状态，不解锁启用/热应用，不重播金融或已完成清理。
- 10189与71752首轮外层修复仍失败：原VerifyFlat调用syncPositions，纯查询失败把干净策略持久化为UNKNOWN；这不是可通过清除UNKNOWN解决的外层缓存问题。新增VerifyStoppedFlat入口，只在原金融停止完成且无错误（或从未启动且干净）时将纯读取故障保留为未取得证据，不改写原账本；普通VerifyFlat/sync仍走原严格封锁，既有UNKNOWN/未决意图、畸形或不匹配持仓均不放宽。
- 43336初次专用入口因错误使用历史started标记拒绝真实已停止实例，导致纯/混合清理既有路径回归；已修为操作槽下按原stopMu读取stopCompleted/stopErr，不新增相反锁序，不将失败夹具当通过。50334策略六模式及既有flat/context/owner三轮raceexit0/strategy3.404s；71751当前最终构造器/六指针归属负例/策略组合三轮race待终态。
- 37162完整十包race正在运行，报告目标 `/private/tmp/quantmesh-capital-verification-rc1058.sSXU4O/race`；当前无MySQL，不继承rc1055实库或旧版全仓/同提交发布证据。
- 71751最终根/策略组合三轮race已终止exit0：根32.271s、strategy5.722s；完整构造器两停止路径恢复、六类原指针/归属负例、既有UNKNOWN、纯/混合清理和flat/context证明均通过。新增SQL断言要求失败重查前后payload/schema不变，策略六模式要求纯查询恢复不改账本、运行中/已有UNKNOWN拒绝、错误币对/真实残留仍封锁；不继承早期失败输入为通过。
- 根/strategy/storage vet、共享门禁18项308断言、超时契约8项47断言通过。只读构建诊断按build-test-acceleration技能区分编译/链接与测试执行；指定进程存活，磁盘约231GiB可用，未清理缓存、重启或减少验证范围，没有实测提速声明。funding_carry_strategy.go现2918行，已超过考虑拆分阈值，后续新增代码应先拆分相关核验模块，避免超过3000行。
- 37162完整十包已终止exit0/success=true：2169顶层PASS/15MySQL SKIP，无缺包/解析错误；rc1058/HEAD5c048825/source_dirty=true。`/private/tmp/quantmesh-capital-verification-rc1058.sSXU4O/race/results.json`与Markdown已读回，新资本构造器/原指针保护/停止flat三父例PASS，failure_diagnostics为空。所有本轮会话终止，前后端版本及diff检查通过，无提交/推送/发布/部署或真实账户访问。
- 本轮新增故障恢复完整构造器为干净零敞口夹具，既有非零反向金融关闭/费用与还款串联仅作回归，没有在该串联末尾注入新的资本查询/SQL提交后应答丢失故障；隔离MySQL、崩溃恢复和跨进程原子fencing不在已通过范围。readScopedPositionSnapshot在查询前/返回后发现context结束仍是未分类错误，停止flat可能按原严格路径持久封锁，其受限恢复需后续补证；不把这一版当所有取消或未知经济结果均已恢复。R01–R15及盈利验收范围保持开放。
- 原SQL释放按钱包锁及确切reservation_token拒绝新世代，缺行可幂等处理；本项不放宽这些语义，不自动解除策略UNKNOWN。SQL提交应答丢失及完整仓位/实库恢复、跨进程fencing和R01–R15发布/盈利仍开放，无提交/推送/部署或真实账户访问。

## rc1057：验证失败诊断证据留存（本地契约及十包回归完成，未提交）

- rc1056首轮race只有失败父例而无直接断言，原样重跑不能证明失败原因已修复。新增契约要求失败父例保留同包子例输出、包失败保留panic/所有输出、JSON与Markdown留存stderr及解析故障并脱敏；待红测及修复，不改变通过条件、包范围和MySQL必需父子证据。
- 原验证器契约红测16项220断言、1失败1错误，明确缺failure_diagnostics及broken_test.go:42诊断。新增失败父/子与包级输出聚合、stderr和未解析行字段；Markdown按内容选择更长代码围栏，不由日志中的反引号结束证据块。遮蔽常见API/令牌/密码/listenKey/webhook/DSN、URL凭据及私钥格式，不读取或输出环境凭据，不宣称能识别任意无标签敏感字符串。
- 生产run→JSON/Markdown写入路径采用失败输入契约验证，保留断言文件行、编译错误及未解析输出，仍返回失败；成功/失败/解析/MySQL判定未改。最终Ruby门禁17项296断言、超时契约8项47断言通过。新增输出证据是脱敏后的诊断，不是未经处理的完整原始日志，不把失去的rc1056首轮断言当作已经找回。
- 71470当前完整十包实际Go race仍运行，报告目标 `/private/tmp/quantmesh-race-evidence-rc1057.rifA6K/race`；未连接MySQL，不继承旧版本实库/全仓/发布证据。
- 新增testdata中专用故意失败Go夹具，真实go test -race -json终止exit1且父/子断言带文件行与脱敏值，Ruby契约应通过；不纳入普通./...发现，不冒充金融回归。首次82022仅因契约写错预期行10而真实输出为行9失败，改为有效文件行匹配；无生产诊断修改。最终契约18项308断言通过，包含凭据格式、JSON/Markdown写入与反引号围栏；超时8项47断言及根vet通过。
- 71470完整十包已终止exit0/success=true：2166顶层PASS/15MySQL SKIP，source_version=rc1057/source_commit=5c048825/source_dirty=true；JSON/Markdown已读回，failure_diagnostics与unparsed_output字段为空、stderr_present=false、stderr为空，核心没有TestDiagnosticFailure夹具计数。报告 `/private/tmp/quantmesh-race-evidence-rc1057.rifA6K/race/results.json`及results.md留存；未声称本轮触发真实交易核心故障或定位rc1056指标失败。
- 所有会话终止，前后端rc1057一致、diff通过。该版本补的是后续失败可审计性，不是资本核验恢复或普通UNKNOWN经济处置闭环；下一步继续来源受限资本核验恢复，R01–R15及实库/同提交发布/盈利仍开放，无提交/推送/发布/部署/真实账户访问。
- 资本核验失败只读恢复、普通UNKNOWN经济处置、跨进程fencing及R01–R15、同提交发布/盈利仍未闭合，未提交/推送/发布/部署或访问真实账户。

## rc1056：普通金融停止故障控制面接线（本地针对性及十包复跑完成，未提交）

- 实际FundingCarry构造器先持久化UNKNOWN，再经StopBot/StopAll失败；新增启用、重复启动、热更新及实际详情状态断言，检查控制面不得与金融停止故障脱节。待红测和修复，不放宽金融重放、原指针或SQL资本/租约保护。
- 95754行为红测exit1/根1.323s：两路径都允许重复启动请求成功返回、热参数报告Applied且详情仍Running；StopAll还允许Enable写入。重复启动成功返回不证明新金融运行实际启动，红测只证明控制面错误接受。
- BotRuntime新增独立stopReconciliationPending原子标记；StopBot/StopAll的非分类StopWithError失败设置，纳入启动/启用/API stop_pending及热应用拒绝。标记不属于金融核验或清理可重试类型，不清除原故障指针、不改变金融停止缓存，不自动解锁UNKNOWN；停止入口仍可调用原受管StopWithError，实际成功后正常删除控制器，不把待办标记当永久禁用配置。
- 38156修复后实际UNKNOWN与纯/混合清理、所有权恢复关联三轮raceexit0/根19.210s；扩展实际新旧配置ListBots后7580最终UNKNOWN两模式三轮raceexit0/根3.473s。SQL来源不变、原指针保留、三腿资本/租约留存及金融/清理零重放均通过；实库、当前全仓与发布未继承rc1055。
- 根/web/config vet、Ruby门禁14项214断言及超时契约8项47断言通过，Yarn既有botLifecycle四项测试通过（仅纯状态逻辑，非渲染验收）。90691完整十包race终止exit1：2165PASS/15MySQL SKIP/1FAIL，失败父例web.TestSystemMetricsProvider_GetMetrics_EmptyStorageFallback；报告 `/private/tmp/quantmesh-stop-reconciliation-rc1056.X1T73a/race/results.json` 和Markdown保留，不当本版整包通过。正在单例与整Web包复核，未删除/排除失败测试；本轮无MySQL DSN，不继承rc1055实库或全仓证据。
- 96352原样指标单例三轮race终止exit0/web2.159s，89322原样完整Web包race终止exit0/web83.523s；未修改测试或生产指标代码，首轮失败原因未获得直接断言输出，不能据此宣称原因已修复。现有race汇总报告没有保留Go逐例Output，需要后续补失败诊断证据留存；CPU采集混合进程/系统口径与测试0–100约束的关系亦需独立核验，不把推断当首轮故障结论。87185原样第二轮完整十包race-final仍运行，待终态。
- 87185第二轮完整十包race-final已终止exit0/success=true：2166顶层PASS/15MySQL SKIP，无缺包/解析错误；`/private/tmp/quantmesh-stop-reconciliation-rc1056.X1T73a/race-final/results.json`及Markdown已读回，source_version=rc1056/source_commit=5c048825/source_dirty=true。UNKNOWN实际控制器、既有停止成功恢复及指标父例均PASS；首轮race失败报告保留，不排除失败用例或改源码后沿用第二轮证据。未连接MySQL，不是严格实库门禁或本版全仓/前端渲染/同提交发布验收。
- 所有本轮验证会话已结束，前后端rc1056一致，diff检查通过，无提交/推送/发布/部署/真实账户访问；下一步补资本独立核验失败的来源受限只读恢复及验证失败证据留存。R01–R15及普通UNKNOWN经济处置范围保持开放，不把控制面状态修正当盈利恢复闭环。
- 普通UNKNOWN的经济核账/处置、资本独立核验故障恢复、跨进程fencing及R01–R15和同提交发布/盈利仍开放；未提交/推送/发布/部署或访问真实账户。

## rc1055：外层失败订单流清理恢复（本地验证完成，未提交）

- rc1054使真实未退出订单流返回错误，外层stopOnce仍把金融与流错误混合永久缓存；6264完整构造器红测复现失败腿没有被重试。外层分离金融缓存与订单流清理记录，仅在金融已核清或严格末阶段核验候选下重试失败腿，成功腿不重播；要求原故障指针及当前三腿所有权，混合故障的金融核验仍独立完成才释放SQL资本/租约。
- 实际StopBot/StopAll完整构造器覆盖纯清理失败、混合末阶段核验失败，以及普通金融UNKNOWN不转成清理候选。69715最终三轮race已终止exit0/根16.775s；UNKNOWN的SQL来源、故障指针和资本/租约保留、零金融/清理重放均通过。81039此前实际控制器纯/混合路径及七模式指针/所有权负例三轮race通过；最初17529因夹具缺eventBus在成功StopBot发布事件时panic，补真实事件总线后修复，不当生产缺陷或降低生产要求。
- 根/strategy/storage/binance vet通过；Ruby门禁14项214断言、超时契约8项47断言通过。新增强制MySQL纯/混合清理父例及StopBot/StopAll各子路径，缺失、跳过、重复或错包不得通过。57348本轮隔离MySQL8.0.36/127.0.0.1:32787/tmpfs完整十包race已终止exit0/success=true：2181顶层PASS、15项必需MySQL父例及36条MySQL父子终态全部PASS，无缺包/解析错误。报告 `/private/tmp/quantmesh-stream-cleanup-rc1055.aYP2Ag/race/results.json` 已读回rc1055/HEAD5c048825/source_dirty=true，純清理StopBot/StopAll及混合StopBot_cleanup/StopAll_cleanup逐项通过。这是当前未提交源码本地回归，不是同提交发布或实盘验收。46290顺序全仓--full已启动，终态待核验。
- 回查确认普通金融UNKNOWN的StopAll泛化故障没有停止待处理标记，API Running、启用及热应用仍需补同一路径负例和修复；资本释放独立核验失败设置通用故障后可能阻断后续只读复核，亦未闭合。R01–R15、跨进程fencing、其他UNKNOWN经济处置及同提交发布/盈利范围保持开放，未提交/推送/发布/部署/真实账户访问。
- 46290最终Go1.25.4全仓--full/./...已终止exit0：3139顶层PASS/6外部交易所SKIP。`/private/tmp/quantmesh-stream-cleanup-rc1055.aYP2Ag/full/results.json` 与Markdown已读回；实际SQLite纯清理StopBot/StopAll、混合StopBot_cleanup/StopAll_cleanup、普通金融UNKNOWN两子例及MySQL纯/混合四子例终态逐项PASS。源码HEAD5c048825不变，前后端rc1055一致，diff检查通过；没有当前前端/嵌入构建、Linux/同提交发布、真实网络或盈利验收。
- 清理前核对完整容器e7572b3ba926f85d378447fb0c23e5db1b8a8a2b79d530fc1263ba0e12276ea2、isolated-audit-rc1055标签、127.0.0.1:32787及tmpfs，无quantmesh_ctor_/quantmesh_cas_noop_残留schema；停止后读回容器不存在。只移除本轮合成内存数据，夹具可重建；JSON/Markdown报告、镜像及其他容器保留，所有验证会话已终止。

## rc1054：Binance订单流停止确认（本地验证完成，未提交）

- 非金融清理永久缓存回查发现实际接口更前置风险：Futures WebSocketManager.Stop超时只警告并置isRunning=false，且监听函数关闭SDK连接后不等连接done；BinanceAdapter.StopOrderStream无条件nil。Spot管理器也超时返回void且适配器清空指针，旧清理尚未确认便可能当作成功释放。先补真实停止确认，不把离线构造器错误注入当这些实际接口已有正确回执。
- 新增实际Start/监听/适配器调用的离线网络夹具，底层连接收到stop后故意延迟done，要求失败回执及重启拒绝；待红测、修复与最终验证。R01–R15、外层缓存清理恢复、跨进程fencing、发布及盈利仍开放，未提交/推送/发布/部署/真实账户访问。
- 38001实际合约管理器/监听/适配器红测已终止exit1/0.668s：底层连接done未关闭却返回停止成功。改StopWithError保持超时错误，Stop兼容入口只日志告警；不持mu等退出，等待SDK连接/回调done及保活协程共同完成后才清理当前代、关闭完成通道。REST启动前认领当前代及完成通道，使Stop能够取消并等待正在启动的请求；每次确认完成后重新建代，不重复close或注册回调。
- Spot相同生命周期与可取消保活/重连等待，SDK停止改close而非可能无人接收的发送，并等待连接done；适配器保留管理器指针并转发错误。顺带去掉Spot debug日志的原始listenKey，不输出凭据。本项仅订单流，不把价格/K线流或SDK外部资源当全部已退出。
- 47458初次合约/原重连三轮race已终止exit0/1.725s。新增双适配器七模式首次仅因具名回调类型与测试func类型不匹配编译失败，修正测试桥接，非行为红测；3158最终合约/Spot14子场景及已有重连/取消/回调三轮race已终止exit0/2.842s，覆盖connection/callback/keepalive/startup/connect_startup/external_cancel/concurrent_stop及同代重试、确认后重新启动。完整扩大验证待执行，不继承rc1053发布或全仓证据。
- 再接实际StartOrderStream公开入口时20764因夹具缺合约持仓模式REST客户端panic，非生产缺陷；用现有newGuardAdapter本机HTTP服务完整经过持仓模式预检，不设置假verified标志或绕过检查，4949最终公开Start/Stop接线三轮race已终止exit0/2.945s，最终根/strategy/storage/binance vet、13项168断言门禁契约及8项47断言超时契约通过。
- 47954首次完整门禁已终止exit1/success=false，缺quantmesh/exchange/binance通过证据，13项MySQL已核验；保留 `/private/tmp/quantmesh-order-stop-rc1054.QyYEaq/race/results.json` 与 `results.md` 失败报告，不当成本版完整通过。已修正公开入口夹具后启动race-final独立最终报告，待终态。仅本轮MySQL8.0.36回环32786/tmpfs/quantmesh_audit_rc1054，不触生产。
- 86872最终完整十包Go1.25.4 race-final门禁已终止exit0/success=true：2176顶层pass、13项MySQL及全部必需子路径通过；新双适配器生命周期父例及14子场景真实执行。JSON/Markdown最终报告已生成待逐项读回。继续顺序执行本版完整Go全仓--full/./...（不排除审查），结果待读回，不继承旧版全仓证据。
- 最终race-final JSON/Markdown已读回：rc1054/HEAD5c048825/source_dirty=true，全部30条MySQL终态pass；根68.158s/strategy172.588s/storage12.412s/binance5.038s，新生命周期父例0.36s和连接退出红测回归0.01s均pass。随后22807真实完整Go全仓--full已终止exit0：3134顶层pass/6外部交易所skip；全部13项必需MySQL父例pass，新14个公开Start/Stop生命周期子例逐项PASS从JSON输出读回（父0.37s），根78.032s/strategy169.043s/storage16.016s/binance3.569s。最终报告 `/private/tmp/quantmesh-order-stop-rc1054.QyYEaq/race-final/results.json`、`full/results.json`及对应Markdown；旧race失败报告保留。
- 这证明本地注入网络/本机HTTP预检下的订单流生产管理器和公开适配器停止确认，不代表真实SDK网络断连或全部SDK辅助资源/价格/K线流退出验收。上层FundingCarry清理错误仍被stopOnce缓存，虽不再虚假成功释放，但需要继续实现仅失败非金融步骤重试；金融UNKNOWN/混合故障、原失败指针所有权及运行租约检查不能放宽。当前前端/嵌入构建、Linux/同提交发布、跨进程fencing和R01–R15整体盈利准备度仍未验收。
- 清理前核对MySQL8.0.36、127.0.0.1:32786/tmpfs和完整容器33aceb965b70专用标签，确认quantmesh_ctor_/quantmesh_cas_noop_无残留schema，仅移除本轮容器及合成内存数据，不保留数据副本但夹具可重建，报告/镜像/其他容器保留。所有验证会话已结束，前后端rc1054一致/diff通过，无提交/推送/发布/部署/真实账户访问。

## rc1053：SQL已提交应答故障到实际停止恢复（本地验证完成，未提交）

- 扩展共用完整构造器夹具：真实金融停止后才替换转发到生产SQL适配器的单次故障封装，确认SQL已提交，再注入ACK丢失或实际SQL子请求取消；不冒充外层停止context取消。StopBot/StopAll各两故障，要求干净SQL账本但原失败指针、资本和三腿归属仍保留，下一次只读证明后才完整释放，订单/还款/流清理仅一次。
- MySQL只在每个测试新建的quantmesh_ctor_合成schema冻结updated_at，迫使已干净快照再次条件写入进入rc1052无变化路径，不改生产迁移或其他测试表。待契约与真实SQLite/MySQL及完整门禁验证；R01–R15、非金融清理恢复、跨进程fencing、同提交发布及盈利仍开放，无提交/推送/发布/部署/真实账户访问。
- 门禁契约红测13项137断言/1失败，确认原门禁不要求新增父例；补第13项MySQL及四个子路径，缺失/skip/fail/错包/重复拒绝。修复后13项168断言、原超时契约8项47断言零失败/错误/skip，语法及diff通过；使用build-test-acceleration维护完整入口和隔离证据，不宣称提速。
- 99090初次SQLite三轮exit1：纯StopAll不会持久化用户禁用，旧夹具最终调用StopBot掩盖差别。改实际StopAll重试暴露的是夹具断言错误，非生产金融缺陷；分别断言StopBot禁用/StopAll保留原先无启停记录，停止日志为空、资本/租约完整释放及零金融重放共同严格核验，不改生产启停语义。
- 49626共用SQLite/MySQL四故障及两原路径三轮race已终止exit0/根89.840s，20686根/strategy/storage vet通过。最终追加必须且仅一次成功相同来源SQL CAS断言，53746最终三轮仍运行；不把上一输入通过继承为新增断言或完整门禁验收。
- 53746最终共用三轮race已终止exit0/根89.335s，明确先distinct pending-to-clean实际提交后故障，再恰好一次成功same-source/target CAS；冻结MySQL诊断时钟使该后续写入无物理变化，确认rc1052生产分支串联。最终vet与两组Ruby契约再通过。完整十包--require-mysql门禁已启动，报告目录 `/private/tmp/quantmesh-constructor-cas-rc1053.hzDGBA/race`，待读回终态，不将运行中当通过。
- 最终23353实际Go1.25.4完整十包门禁已终止exit0/success=true：2174顶层pass、13项MySQL及12个必需构造器子例核验；另五个存储CAS子例，共30条MySQL终态均pass。新增StopBot_ack_error2.75s/StopBot_cancelled_commit2.66s/StopAll_ack_error2.63s/StopAll_cancelled_commit2.68s及父10.72s均实际quantmesh/pass；根65.913s/strategy170.677s/storage15.752s/web59.290s。JSON/Markdown报告已读回，明确rc1053/HEAD5c048825/source_dirty=true，不把本地dirty源码证据当已提交或已发布。
- 此项补齐完整构造器到实际StopBot/StopAll的已提交SQL应答丢失/子请求取消后同来源核验与资本释放，不扩充为真实网络断连、外层context取消、跨进程原子fencing或重启全金融恢复证明。未执行当前版Go全仓、前端/嵌入程序构建、Linux或同提交GitHub发布；R09/R14/R15及R01–R15整体仍未验收，非金融清理错误的缓存恢复仍待后续。
- 清理前确认MySQL8.0.36、仅127.0.0.1:32785/tmpfs、无quantmesh_ctor_/quantmesh_cas_noop_残留schema，核对专用完整ID/标签后移除本轮fdd3cd0f8d41容器及合成内存数据，数据不可恢复但夹具可重建；报告/镜像/其他容器保留。所有验证会话已终止，前后端rc1053一致、diff通过；仅测试/门禁/版本记录变更，无提交/推送/发布/部署或真实账户访问。

## rc1052：MySQL相同快照条件写入（本地实库验证完成，未提交）

- MySQL CAS仅检查RowsAffected==1，而updated_at为DATETIME(3)，已提交后相同快照再次核验可能来源正确但没有物理变化。新增独立schema/诊断时钟冻结trigger确定性回归；仅合成表，不修改生产迁移或其他fixture表。待红测和修复，不推断真实账户故障。
- R01–R15保留，CAS应答丢失全构造器串联、非金融清理恢复、跨进程fencing、Linux/同提交发布和盈利仍开放，无提交/推送/发布/部署或真实账户访问。
- 29215真实MySQL红测已终止exit1/storage0.840s：独立诊断时钟冻结trigger使同快照没有物理变化，原实现saved=false/err=nil，明确复现错误冲突，不冒充真实账户事件。修复仅MySQL/RowsAffected0/next与expected完全相同分支，用当前SELECT快照精确比较Bot、策略、schema及BINARY payload；来源不匹配/缺失继续false，不插入、不重试金融或其他目标写入。它是相同目标的原子比较，不是新增持久代际fence或ABA防护。
- 93625 SQLite/实际MySQL共用CAS三轮race已终止exit0/storage2.106s，包含八写者一胜、陈旧/大小写/缺失/错误schema/取消拒绝和新同快照回归；33751根包/strategy/storage vet通过。门禁契约12项135断言、原审查超时契约8项47断言均零失败/错误/skip。新增no-op子例嵌在既有必需MySQL CAS父例内，父失败会阻断原完整入口，JSON/Markdown保留真实子例终态。
- 最终24874真实Go1.25.4完整十包race门禁已终止exit0/success=true：2172顶层pass、12项MySQL全部核验；新增identical_snapshot_noop实际quantmesh/storage/pass/0.02s，根45.966s/strategy174.231s/storage14.150s/web59.063s。报告 `/private/tmp/quantmesh-mysql-noop-cas-rc1052.Fgo6Pm/race/results.json` 与 `results.md`，明确rc1052/HEAD5c048825/source_dirty=true；未执行当前Go全仓、前端/嵌入构建、Linux或同提交发布，不继承旧证据。
- 临时MySQL8.0.36容器709d4ad5332d，仅127.0.0.1:32784/tmpfs/quantmesh_audit_rc1052；清理前确认quantmesh_cas_noop_及quantmesh_ctor_无残留schema，核对完整ID/标签后仅移除本轮专用容器与合成内存数据，数据不可恢复但夹具可重建，报告/镜像/其他容器保留。全部会话终止，前后端rc1052一致/diff通过。
- 此处证明存储层同来源no-op恢复语义，尚未注入完整构造器CAS真正提交后ACK丢失/取消并走SQL资本释放；需继续该同路径验证。非金融清理恢复、跨进程fencing与盈利证据仍开放，R09/R14/R15及全R01–R15范围不缩减。

## rc1051：完整停止恢复MySQL与发布门禁（本地实库验证完成，未提交）

- SQLite完整构造器平仓/还款末阶段故障—实际StopBot/StopAll重试—SQL资本/三腿租约释放案例抽出共用存储工厂；MySQL父例预检DSN、每子例独立临时schema，复用所有金融来源/intent经济结清/停止日志断言，不改生产金融语义。
- 原门禁遗漏此MySQL父例：契约红测12 runs/114 assertions/1 failure已终止；新增第12项MySQL与两种Stop路径各唯一正确包pass要求，缺失/skip/fail/错包/重复都拒绝。待实际MySQL及完整门禁；R01–R15、非金融清理恢复、跨进程fencing、Linux/同提交发布与盈利仍开放，无提交/推送/发布/部署或真实账户访问。
- 使用build-test-acceleration技能保持完整测试入口、共享数据库隔离和严格失败传播，不宣称提速。临时MySQL8.0.36容器47205aa88b47，仅127.0.0.1:32783、tmpfs、独立quantmesh_audit_rc1051；73127 SQLite/MySQL同恢复链三轮race已终止exit0/根31.273s。根包/strategy/storage vet、原审查超时契约8项47断言通过，缺DSN不被继承为MySQL通过。
- 47219初版真实十包完整race门禁已终止exit0/success=true，2172顶层pass、12项MySQL全部核验；但初版报告仅保存父例，追加mysql_evidence和Markdown父子终态表，使审核可独立读回子路径。最终门禁契约12 runs/135 assertions零失败/错误/skip，不放宽原判断，旧race报告保留。
- 最终95794真实完整十包race-final门禁已终止exit0/success=true：Go1.25.4、2172顶层pass、12项MySQL父例及八个必需构造器子例全部核验；报告另含四个SQL CAS子例，共24条MySQL终态全部pass。新增StopBot2.72s/StopAll2.83s/父5.55s均实际quantmesh根包pass，根45.453s/strategy171.860s/storage14.657s/web60.444s。JSON/Markdown `/private/tmp/quantmesh-mysql-final-verification-rc1051.Yn55uv/race-final/results.json` 与 `results.md`，明确rc1051/HEAD5c048825/source_dirty=true；未执行当前版Go全仓、前端/嵌入构建、Linux或实际GitHub工作流，不将本地门禁当同提交发布。
- 全部验证会话终止、前后端rc1051一致/diff通过；清理前确认无quantmesh_ctor_残留schema并核对完整容器ID/标签/tmpfs，仅本轮专用容器及合成内存数据已移除，数据不可恢复但夹具可重建，报告/镜像/其他容器保留。交易所/运行租约仍离线模拟，不是跨进程fencing或真实账户证明。
- 当前实库正例覆盖CAS提交前的最终查询失败后恢复。CAS已提交但应答丢失/取消后的干净快照再次核验，仍只有内存夹具，需单独验证真实SQL同来源条件写入及MySQL受影响行语义；非金融流清理锁存仍缺独立恢复。R09/R14/R15与完整R01–R15范围继续开放，不宣称实盘或盈利验收。

## rc1050：完整构造器停止只读恢复串联（SQLite本地验证完成，未提交）

- 新回归从真实SQL已归属Reverse/Futures0.4/借款42本金0.4启动默认构造器，离线venue通过真实executor和共享intent journal平期货、买回净量0.4及还款81；仅最后独立负债查询失败。实际StopBot/StopAll及再次失败只读核验保留SQL三腿资本，修复查询后Bot API重试须清核验指针、SQL资本及运行/钱包锁，订单/还款/流清理不得重放。
- 待最终验证；不调用真实交易所，内存租约不是跨进程fencing。R01–R15保留，其他UNKNOWN经济处置、非金融清理锁存、MySQL/Linux/同提交发布和盈利仍开放，无提交/推送/发布/部署或真实账户访问。
- 23238首轮真实完整构造器两模式exit0/5.258s；23142加强SQL借款42/还款81/净量0.4来源、marker/UNKNOWN/intent、三腿租约保留、最终共享intent经济结清、Bot持久化禁用及停止日志清理后exit0/5.246s。真实executor与SQL adapter完成两订单/一还款，故障重复只读查询不重放，成功释放三个50USDT钱包预留和全部运行/钱包锁；不使用手工资本回调作为本轮正例。
- 最初85596扩大三轮race通过（根27.488s/strategy50.623s）；22317初版full exit0/3118pass/17skip。复核发现测试用journalTestOwner先按旧通用scope注册后改Config，改为SymbolConfigToBotConfig真实carry合同预先登记再停止；初版报告保留，不当作最终注册源码验收。
- 最终66646扩大三轮race已终止exit0：根25.960s/strategy51.206s，包含完整构造器、资本/租约、待办/停止drain及策略恢复/保存故障。最终2050真实Go1.25.4全仓full-final已终止exit0：3118顶层pass/17skip，根33.632s/strategy168.437s；新增父例及StopBot/StopAll子例已从JSON逐项读回pass。报告 `/private/tmp/quantmesh-constructor-final-verification-rc1050.luGXHZ/full-final/results.json` 与 `results.md`，初版full另存。
- 最终根包/strategy/storage vet、前后端rc1050一致/diff通过，全部验证会话终止。本轮仅测试/版本/记录变更，未改生产金融语义。17skip含11项MySQL与6外部交易所，本轮无隔离DSN，不能继承旧版实库/构建/发布证据。下一步应补此同路径隔离MySQL与门禁证据，并审查非金融清理锁存的恢复能力；R09/R14/R15和全R01–R15范围仍开放，不宣称实盘或盈利验收。

## rc1049：外层停止只读重试接线（本地分层验证完成，未提交）

- FundingCarry生产停止closure消费严格候选分类；第一次仅报告待办，后续只调用ReconcileStoppedMarginClose，不重跑stopOnce金融/流清理。核验前后当前租约及本次shutdownCloseUnverified指针必须一致，仅CAS清除自有指针，其他错误/所有权丢失继续拒绝。
- StopBot/StopAll识别独立核验待办，不再覆盖其错误来源，保持stop_pending、禁启用/重复启动/热参数；停止流错误加入合并结果，混合失败不得走只读候选。成功仍经过原独立资本验证及租约释放。待针对性验证，R01–R15不缩减，无提交/推送/发布/部署/真实账户访问或盈利验收。
- 99631初次针对性根包exit0/1.705s，13350补充完整生产构造器清理失败exit0/1.355s。指针/取消/撤权九模式拒绝其他或相同文本的新错误；实际StopBot/StopAll及状态adapter保留待办，封锁启用/启动/热应用，修复查询后停止计数一次/核验两次/资本边界一次。该正例使用手工post-classification边界和模拟资本回调，不是实际金融RPC与SQL资本释放串联证明；私有策略候选本身由rc1048实际Stop回归覆盖，不能拼接为全构造器正向验收。
- 完整默认构造器+SQLite清理拒绝案例使用实际生产停止closure，注入spot StopOrderStream失败，原错误可追溯、非核验待办、三腿资本/所有权保留、流清理一次且零金融RPC；撤销夹具故障后仍返回缓存错误，不误放行。该非金融清理错误尚无单独恢复路径，不将安全锁存视为处置完成；运行租约为内存夹具，非跨进程fencing。
- 最终88672扩大三轮race已终止exit0：根包14.922s/strategy50.190s，包含新三父例、原完整构造器与资本/租约重试、停止drain及策略候选/重启恢复/检查点；75837根包/strategy/storage vet通过。27569真实Go1.25.4全仓脚本已终止exit0：3117顶层pass/17skip，根包27.652s/strategy169.989s；新父例/九模式/StopBot和StopAll子例已从JSON逐项读回pass。报告 `/private/tmp/quantmesh-runtime-final-verification-rc1049.aUMO5J/full/results.json` 与 `results.md`。
- 前后端rc1049一致、diff检查通过，全部验证会话终止；17skip含11项MySQL与6外部交易所，本轮无隔离DSN。下一步优先补完整构造器实际末阶段金融故障到只读恢复/SQL资本释放/三腿租约的同路径证据，并检查单纯非金融清理锁存的可审计恢复；当前嵌入/前端构建、Linux/同提交发布、跨进程fencing与盈利仍未验收，R09/R14/R15继续开放。

## rc1048：只读停止重试错误来源（分类层本地验证完成，未提交）

- StopContext仅在金融请求结束、明确marker与完整本地借款/成交/本金归属及零剩余、无共享执行待办时封装私有候选错误，保留原cause与停止缓存；不据此解除UNKNOWN。全错误树分类要求每个joined分支均为候选，拒绝候选混合prepare/所有权/其他策略失败，避免宽泛errors.As误清。
- 新增实际StopContext候选/缓存/只读核验无重放回归、十种错误树及十七种本地证据缺失测试。待最终验证；外层生产重试接线、资本释放与真实SQL/跨进程fencing、同提交发布及盈利证据仍开放。R01–R15不缩减，无提交/推送/发布/部署或真实账户访问。
- 55746首轮定向exit0/strategy8.903s；最终73583扩大三轮race已终止exit0/strategy122.572s，包含原停止UNKNOWN锁存、候选错误缓存、只读完成、重启恢复及关闭检查点故障；泛化UNKNOWN不生成候选，混合错误拒绝、原cause可追溯。
- 最终60524真实Go1.25.4全仓脚本已终止exit0：3114顶层pass/17skip、strategy168.723s；三个新父例及十种错误树/十七种缺失证据子例已从JSON逐项读回pass。报告 `/private/tmp/quantmesh-stop-error-provenance-rc1048.wenTq3/full/results.json` 与 `results.md`；27976根包/strategy/storage vet、前后端rc1048一致及diff通过，全部验证会话终止。17skip含11项MySQL与6外部交易所，本轮未继承旧实库/前端嵌入构建/发布为当前证据。
- 此候选仅在strategy.StopContext金融阶段返回后产生，外层funding_carry_runtime/BotManager尚未消费；停止缓存阻断未解除。后续生产接线必须保留shutdownCloseUnverified原错误来源、拒绝混合prepare/所有权失败，独立核验当前租约后只重试核账，再走资本及租约释放；实际StopBot/StopAll串联证明仍欠缺，不把分类测试当生产闭环完成。

## rc1047：停止实例显式只读核验（策略层本地验证完成，未提交）

- 策略Stop缓存与外层运行时stopOnce各自持有失败。先新增ReconcileStoppedMarginClose明确只读接口，必须已停止且原实例持有末阶段marker、生产者退出；复用完整账本/当前零负债与期货/账户无挂单/来源CAS/所有权证明，导入确切已提交状态，不调用金融停止、资本释放或租约释放。
- 前次CAS提交后取消的干净快照仅允许原实例的原borrow身份与marker来源重新核验，不推断泛化UNKNOWN；等待测试。外层缓存/资本释放接线、完整SQL构造器/实库/跨进程fencing、其他UNKNOWN经济处置与同提交发布/盈利仍开放，R01–R15不缩减。无提交/推送/发布/部署/真实账户访问。
- 20302初次两父例通过；最终20447扩大三轮race已终止exit0/strategy110.651s，覆盖已停止复核十八模式及相邻停止、重启恢复、关闭和检查点故障。取消/撤权/残债/利息/挂单/来源变化拒绝；CAS已提交后应答丢失、取消及钱包清理失败保留原marker，修复故障后再次只读核验成功，不重放金融请求。夹具生产者退出、交易所与运行租约为离线模拟，不是实际生产并发或跨进程fencing证明。
- 最终6618真实Go1.25.4全仓脚本已终止exit0，3111顶层pass/17skip、strategy164.719s；两新父例与十八命名子例已从JSON逐项读回pass。报告 `/private/tmp/quantmesh-stopped-verification-rc1047.A1cwHa/full/results.json` 与 `results.md`；77235根包/strategy/storage vet、门禁契约11项112断言、审查脚本契约8项47断言及diff检查通过，全部验证会话终止。17skip仍含11项MySQL/6外部交易所，本轮无DSN；没有当前前端/嵌入构建、Linux或同提交发布证据。
- 实际调用方搜索确认新接口仅测试使用；funding_carry_runtime.go:707仍返回缓存stopErr，BotManager通用失败还会写入shutdownCloseUnverified。下一步必须按错误来源分类处理只读核验待办，并精确保护其他准备/所有权/清理失败，走实际StopBot/StopAll资本验证与释放接线；不能泛化清空错误或重跑金融阶段。此处策略验证不代表该生产闭环完成，R09/R14/R15继续开放。

## rc1046：完整平仓末阶段只读核验恢复（本地验证完成，未提交）

- StopContext金融失败锁存且普通restore拒绝UNKNOWN，已确认还款/成交、零剩余资产后仅最后只读核验故障仍无恢复路径。新增仅由实际close末阶段写入的明确marker，保持原schema7 pending/unknown flags，旧二进制不据此解除封锁。
- 仅完整归属与本金/成交账本、确切scope、零剩余/期货/负债、账户级三腿无挂单、共享执行无未决、活跃ctx/运行所有权及原子来源比较通过才条件清理；零金融重放。无marker旧UNKNOWN及不完整记录继续拒绝。待最终验证；初次新增夹具缺少unknownOrders成员编译失败已修复，不当行为红测。
- R01–R15范围保留，跨进程fencing、其他UNKNOWN/剩余资产处置、Linux/同提交发布及盈利仍开放；未提交/推送/发布/部署/真实账户访问。
- 19501最初直接close路径验证通过（strategy14.828s），75655扩大直接路径26.839s通过。改为实际StopContext夹具后35434/85407及13048全仓失败：夹具设置钱包key却缺distributed coordinator，生产在金融阶段前正确拒绝；不是金融算法缺陷红测。full/results.json/MD失败报告保留，已补专用walletCoordinationTestLock，不放宽生产要求。
- 78189补齐协调器后实际停止—重启17模式三轮race通过（strategy7.983s）；停止重复调用保持同一错误/零重放，复核操作槽和钱包锁释放。加入明确marker的完整零剩余/零负债终态历史正例，仍拒绝泛化旧UNKNOWN，不因历史部分成交长期退化。
- 最终52462扩大三轮race已终止exit0/strategy98.533s，包含停止/旧取消与UNKNOWN锁存、残债及新marker/最终保存拒写、已写入后应答错误三类故障，核验原借款身份/账本/未决flags保留；最终22592真实全仓full-final仍运行，待读回终态。不把全仓编译成功或在运行算验收。
- 最后flat状态与清理intent/marker一并写入；失败恢复原方向/borrow身份及marker，防“flat但pending无marker”的崩溃窗口。新marker为schema7可选字段，保持pending flags；实际运行租约为离线夹具，非真实跨进程fencing。现有进程的BotManager/运行时停止缓存与资本释放整体重试、本版完整SQL构造器接线/实库仍待后续，不将策略Start回归当这些范围完成。
- 最终22592真实Go1.25.4全仓full-final已终止exit0：3109顶层pass/17skip，新增停止—重启17子例和提交故障3子例逐项pass。报告 `/private/tmp/quantmesh-close-verification-rc1046.ur1mCq/full-final/results.json` 与 `results.md` 已读回；full旧失败报告保留不覆盖。17skip含11项MySQL/6外部交易所，本轮无DSN，不继承旧版实库/发布证据。92512最终vet/分支HEAD/rc1046版本一致/diff通过，全部会话终止，尚无当前前端/嵌入主程序构建、Linux/同提交发布或实盘/盈利验收。

## rc1045：保护性反向平仓独立负债接线（本地验证完成，未提交）

- syncPositions已独立读取负债，但closeReverse两边界仍使用空仓视图。实际路径十模式红测4705已终止exit1/strategy0.756s，读取独立接口均为0，证明已归属仓位被持币视图拒绝阻断；不冒充真实账户故障。
- 平仓前/还款后改独立本金利息接口，无能力仍严格旧路径；错误不回退、金融精度核验本金、还款后任何正本金或利息拒绝，前后检查取消，不采用账户余额或修改成交/净量/还款来源/剩余资产约束。待实际平仓/旧路径/停止与恢复扩展race和完整相关包验证。
- 当前UNKNOWN全账本/资产归属与受管经济处置、跨进程fencing、Linux/同提交发布和盈利仍开放，R01–R15范围不缩减；无提交/推送/发布/部署或真实账户访问。
- 51530初次修复后owner_lost用例错误要求原开仓门禁拒绝也必须改写UNKNOWN，已校准为门禁保留/零金融请求/不写durable；7848实际十四模式通过。随后正例补完整借款账本、解码验证重启有效性，新增十二模式旧接口严格证明回归，待最终验证。
- 扩展70823已终止exit1/strategy126.643s：残债组件与末次租约故障原先注入GetPositions，继承的独立读取绕过了专用故障钩子。仅专用fundingCarryResidualCloseExchange新增显式独立读取，继续通过原钩子暴露残债/撤权；不改变通用mock能力或放宽生产拒绝。最终52238三轮针对性race和50058真实脚本Go全仓在运行，41368 vet/版本一致/diff通过，待终态读回。
- 最终52238三轮实际平仓/旧接口/既有残债与最终租约/债务刷新race已终止exit0，strategy128.135s。50058真实Go1.25.4全仓脚本已终止exit0：3107顶层pass/17skip，strategy153.057s、根包28.813s、storage11.545s、web14.523s；新增两项父例和26命名子例在报告输出逐项pass。JSON/Markdown报告 `/private/tmp/quantmesh-close-liability-rc1045.MSkCV7/full/results.json` 与 `results.md` 已读回。
- 17skip为11项MySQL（含构造器两项）及6项外部交易所，本轮无DSN，不将rc1044实库报告继承为当前版验收。门禁契约11 runs/112 assertions、原审查超时契约8 runs/47 assertions、最终vet/版本一致/diff均通过，全部会话终止；未重建前端/嵌入主程序或执行Linux/同提交发布/浏览器E2E，不代表实盘或盈利验收。

## rc1044：完整借款/还款构造器MySQL接线（本地验证完成，未提交）

- 复用SQLite六模式的全部断言，通过生产NewStorageService/MySQL初始化与实际策略适配器，逐例独立临时schema，核验收据、查询期新检查点、三腿资本/租约保留、零金融RPC与流清理。只允许明确opt-in的无凭据loopback测试DSN；生成的临时库在服务关闭后清理。
- 待真实隔离MySQL验证。仅测试/版本/记录变更，不改生产金融语义；R09当前经济处置、跨进程fencing、R14同提交发布/Linux及R15盈利仍开放，未提交/推送/发布/部署或访问真实账户。
- 临时MySQL8.0.36容器d74777899369，仅127.0.0.1:32782、tmpfs及独立quantmesh_audit_rc1044库。62410六模式实库exit0/4.784s；46149 SQLite/MySQL同组构造器三轮race已终止exit0/14.448s、56589 vet通过。随后补父例预检，27883无DSN父例明确SKIP且最终vet通过；缺凭据不是实库通过。
- 发现旧共用发布门禁只要求8项storage MySQL证据；新增恢复CAS和两个根包完整构造器，共11项，构造器各三个子例必须准确归属且各通过一次，防父PASS掩盖子SKIP。Ruby契约11 runs/112 assertions、原审查超时契约8 runs/47 assertions全部通过；实际十包完整race门禁92106仍运行，待最终报告。不宣称提速，不缩减完整入口。
- 最终92106真实完整十包race门禁已终止exit0/success=true，Go1.25.4、2158顶层pass、11项必需MySQL及六个构造器子例全部核验。根包45.110s/strategy142.991s/storage19.827s/web81.494s；全部验证会话终止，报告 `/private/tmp/quantmesh-constructor-mysql-rc1044.Heb4yx/race/results.json` 与 `results.md`，明确source_dirty=true/HEAD5c048825/rc1044。本轮未执行Go全仓、Linux、前端/嵌入构建或真实GitHub工作流，不继承旧产物为本版发布证明。
- 清理前读回无残留quantmesh_ctor_临时库，并复核容器完整ID/标签/tmpfs；仅本轮专用容器与合成内存数据已移除，数据不可恢复但夹具可重建，报告与基础镜像/其他容器保留。测试仍使用离线交易所和内存租约夹具，不证明真实交易所HTTP端到端或跨进程fencing，R09/R14/R15仍开放。

## rc1043：原审查脚本全仓预算（本地验证完成，未提交）

- 原full/旧full-with-known-pending别名固定120秒，小于实测strategy141–145秒。契约红测25308已终止exit1：7 runs/33 assertions，两全仓命令预算失败；这是命令契约红测，不冒充实际脚本超时红测。
- 全仓改为Go常规10分钟，原11项审查保持120秒；不排除案例、不容忍失败。新增Ruby契约覆盖原始/两全仓入口、事件fail即使子进程exit0、非零退出、原审查skip与缺案例证据，CI/CD必需步骤均接入；待最终契约和真实脚本全仓验证。
- 仅R14门禁可靠性修复，不改生产金融逻辑，不宣称加速；R01–R15、当前经济处置/跨进程fencing/端到端MySQL、Linux/同提交发布/盈利仍开放，未提交/推送/发布/部署/访问真实账户。
- 最终16046语法/新契约8 runs47 assertions、竞态契约9 runs58 assertions、嵌入契约14 runs27 assertions均零失败/错误/skip；28880真实原始审查11pass/exit0。97253 Go1.25.4真实全仓脚本已终止exit0，3105pass/15skip/0fail，strategy141.936秒，命令完整`./... -count=1 -timeout=10m`；9项MySQL与6项外部交易所skip不算验收，本轮无DSN。
- JSON/Markdown报告 `/private/tmp/quantmesh-profit-runner-timeout-rc1043.grAhXC/`，全仓与原审查明细分别full/audit；全部会话终止，版本一致/diff通过。CI/CD契约仅本地，未实际触发GitHub工作流；未重建本版嵌入前端/主程序，不继承rc1041产物为rc1043发布证明。

## rc1042：隔离MySQL集成与恢复CAS语义（本地验证完成，未提交）

- 专用临时MySQL8.0.36 arm64容器，仅127.0.0.1随机端口32781、独立quantmesh_audit_rc1041库、内存数据目录，无现有容器/数据库操作。98667 Go1.25.4显式MySQL测试已终止exit0/storage1.961s，此前8项集成加初始化单元共9项全pass、无skip，破坏性迁移只在该临时库执行。
- 新增MySQL runtime checkpoint CAS共用SQLite身份/schema/payload/大小写/取消回归，直接证实默认collation相等不能代表字节来源；八个并发旧写者只允许一胜并读回最终payload。仅测试/版本/记录，无生产语义修改，无修复前缺陷红测；待本版实库race/完整storage/适配器验证。
- 当前经济处置、跨进程fencing、完整构造器到MySQL接线、Linux/同提交发布及盈利仍未闭环，rc1041其他验证不自动继承；R01–R15完整范围保留，未提交/推送/发布/部署/访问真实账户。
- 最终70738 Go1.25.4三轮race storage4.226s，每轮9项真实MySQL集成/初始化单元/SQLite CAS全部pass且零skip；64536启用隔离MySQL完整storage13.234s、13862构造器/适配器三轮race根包8.492s及storage vet均exit0。构造器仍SQLite，不能冒充MySQL端到端接线。版本一致/diff通过，全部会话终止；JSON/Markdown报告 `/private/tmp/quantmesh-mysql-integration-rc1041.7KwHxU/`。
- 清理前复核专用容器ID/标签/tmpfs后已停止并自动移除容器；仅合成测试数据销毁不可恢复，用例可重建、报告保留，基础镜像及其他容器未移除。本版未重建嵌入前端/主程序，不将rc1041产物继承为rc1042发布证明。

## rc1041补证：全仓Go/原始审查/CI工具链（未提交，MySQL仍跳过）

- 79436 Go1.26.3全仓 `go test ./... -count=1` 已终止exit0，根包28.530s/strategy145.306s/storage12.597s/web14.212s；原始审查脚本30420全部11项pass/exit0，明细保留，不当作R01–R15验收全部闭合。
- 取得仓库CI指定Go1.25.4后20896六个相关包完整回归已终止exit0，根包29.517s/strategy141.197s/storage12.657s/risk4.540s/order11.410s/execution4.029s。仅macOS arm64相关包，不冒充Go1.25.4全仓/Linux/race/同提交发布。
- 44127显式MySQL读回：包exit0/0.379s，8项集成用例因缺隔离DSN全部SKIP，初始化nil DB单元例可运行；环境无测试DSN或破坏性许可，不把跳过当实库通过。Docker只读检查引擎29.5.2可用，尚未创建/修改/停止容器，后续可独立临时数据库补证。
- 原审查脚本全仓模式硬编码120s，而本轮strategy145.306s/另一工具链141.197s，存在门禁假超时风险；未执行该脚本全仓模式，不把静态发现当超时红测。当前直接全仓命令通过。JSON/Markdown报告 `/private/tmp/quantmesh-whole-repo-check-rc1041.Kw8xfP/`；仅记录变更不递增代码版本。
- 全部派发验证会话终止；当前工作区与完整经济恢复、跨进程fencing、真实MySQL/Linux/浏览器E2E/同提交发布/盈利仍开放。未提交/推送/发布/部署/启动服务或访问真实账户，R01–R15保持完整范围。

## rc1041补证：当前工作区Web/前端/嵌入式主程序构建（未提交，非同提交发布）

- FundingCarry成交确认主要通过waitOrderFill轮询；本轮不凭OnOrderUpdate空实现添加未经证实的接线修改。转向R14近期完整Web与前端/嵌入构建证据缺口，仅记录/构建产物变更，不修改业务代码或递增版本。
- 45206 Yarn4.12.0 typecheck、75216前端51文件304测试、66166生产build均exit0；2651 Ruby frontend_embed14 runs/27 assertions与trading_race_gate9 runs/58 assertions零失败。Browserslist数据过期警告保留未自动更新依赖。
- 先构建/核验再同步嵌入目录，旧bundle可恢复备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261004-89870-jdo48a/dist`；manifest为rc1041/216文件，source_digest=dfd261bd9e7d8bb6f0f824245c8f69943af39bda9995b39c5ff9ed1f116f7d3f。
- 同步前81927完整Web15.545s通过不代表新嵌入；同步后91280带embedded_frontend标签完整Web13.917s/编译嵌入字节摘要与handler文件系统验证exit0。49699隔离darwin/arm64主程序构建exit0，Go1.26.3、HEAD5c048825/vcs.modified=true；SHA256=19670cd425c6af6e695b648d7de6156943aab444768315c34cfc8a5ce5cb2731，未执行二进制。
- 全部本轮验证会话终止，JSON/Markdown报告 `/private/tmp/quantmesh-worktree-release-check-rc1041.idA11T/`。未提交工作区补证不是同一干净提交发布；未跑全仓Go/真实MySQL/浏览器E2E/CI Go1.25.4与Linux目标，不启动服务/访问真实账户/下单/提交/推送/发布/部署；R01–R15及盈利仍未闭环。

## rc1041：资本释放核清证明读取与最终租约（本地验证完成，未提交）

- 耐久剩余补币资产已有解码保护，本轮不重复添加。VerifyFlat旧Load绕过上下文，最终成功前缺读后取消/租约核验；改用必需ContextReader、读后检查ctx及最终持锁verifyDebtCommitLocked。旧能力拒绝，不用后台等待产生过期证明；生产SQL适配器已具备接口。
- 红测2024已终止exit1/strategy0.892s：四模式均误报flat，旧路径绕过reader所以取消/租约钩子未执行，不冒充已真实触发后的旧失败证据。新测试核验等待取消、数据返回同时取消、读后租约丢失和legacy拒绝，原payload/零金融动作及操作槽释放。
- 核清正例、剩余资产/消费及锁序夹具局部声明reader，不扩通用legacy fixture。待最终三轮race/完整相关包/vet；R01–R15/当前经济核账与处置/跨进程fencing/同提交发布/盈利仍开放，未提交/推送/发布/部署或真实账户访问。
- 外层verifyAndReleaseAccountWalletCapitalGuarded已有证明返回后的ctx/所有权检查，未复现实际提前释放；本轮补内部读取等待及证明最终边界，不冒充跨进程原子fencing。最终26349三轮race根包11.640s/strategy3.501s/storage2.411s、51815完整根包27.901s/strategy141.951s/storage11.451s/risk2.624s/order10.839s/execution2.862s、58019 vet均exit0，全部本版验证会话终止；JSON/Markdown报告 `/private/tmp/quantmesh-flat-proof-context-rc1041.QQMOFu/`。版本一致/diff通过；未验证完整Web/前端、构建、真实MySQL或同提交发布，不代表实盘或盈利验收。

## rc1040：负债核账金融精度（本地验证完成，未提交）

- 实际syncPositions两接口八个微额负债模式及实际Start红测55982已终止exit1/strategy0.745s，量化精度0.0005以内的外部本金差额、纯利息及正向负债均被放行。初次夹具编译失败已修正，不将编译失败当缺陷红测。
- 反向本金改金融一致性核验，无仓/正向任何正本金或利息拒绝；旧空仓视图分解亦金融精度核验，不用最小下单量抹掉负债。正常利息及二进位舍入继续兼容；待最终针对性race/相关完整包/vet，不改写本金、不发金融RPC。
- 本次推进R09当前负债核账边界，不证明当前资产归属、完整受管处置或跨进程fencing；R01–R15/同提交发布/盈利仍开放，未提交/推送/发布/部署或真实账户访问。
- 最终77531扩展三轮race strategy116.648s/binance2.481s、67273完整根包28.432s/strategy141.165s/storage11.335s/risk4.852s/order10.712s/execution0.503s、57417 vet均exit0，全部本版验证会话已终止。JSON/Markdown报告 `/private/tmp/quantmesh-sync-debt-precision-rc1040.iUKhB6/`；版本一致/diff通过，未验证完整Web/前端、构建、真实MySQL或同提交发布，不代表实盘或盈利验收。

## rc1039：独立负债读取接入持仓核账（本地验证完成，未提交）

- syncPositions原先依赖保证金空仓视图，Binance买回资产后该视图因无法归属库存而拒绝，已有独立MarginLiabilityReader未接入。优先读取确切base本金/利息，有限非负/合计溢位/取消核验；无能力维持旧持仓路径，不回退掩盖负债查询失败。
- 红测61669已终止exit1/strategy0.817s，八模式独立读取均为0；新增回归要求有效本金核对成功、外部本金/坏金额/失败/取消拒绝，既有UNKNOWN保持，不导入余额或重写策略本金。待最终针对性race/相关包/vet及Binance离线HTTP接口验证。
- 本次只补当前负债读取能力，不解除历史UNKNOWN，不提供当前库存归属、受管处置、跨进程fencing或完整权益核账；R01–R15/同提交发布/盈利仍开放，未提交/推送/发布/部署或真实账户访问。
- 最终88714针对性三轮race strategy44.915s/binance1.758s、47923完整根包28.537s/strategy141.105s/storage11.167s/risk5.564s/order8.970s/execution2.430s、9145 vet均exit0；全部本版验证会话已终止。Binance HTTP与策略核账为分层证明，尚无一条完整构造器到HTTP端到端用例。JSON/Markdown报告 `/private/tmp/quantmesh-sync-liability-rc1039.FrpfQn/`；未重跑完整Web/前端、构建、真实MySQL或同提交发布，不代表实盘或盈利验收。

## rc1038：普通启动检查点读取取消（本地验证完成，未提交）

- 普通Start在金融恢复探测后最终restore仍调用后台读取；改为restoreRuntimeStateContext(checkCtx)，优先ContextReader，并在读后/持锁导入前检查ctx。实际Start两模式要求最末读取等待可取消，或reader返回数据同时取消时不得采用归属/金融状态，原payload/零金融RPC保持。
- 直接restoreRuntimeState()保持既有后台上下文契约，旧store缺reader时仍legacy读取但前后查取消，不能保证其等待有界。待最终普通恢复/全部金融恢复、SQL构造器及资本释放旧接口负例的race/完整包/vet。
- 租约fencing、跨进程来源新鲜度、当前债务/利息/资产核账与受管处置、实库MySQL/同提交发布/盈利仍开放，未提交/推送/发布/部署或真实账户访问，不代表R01–R15已闭合。
- 修复前67789实际Start两模式exit1/strategy0.794s，最终普通恢复绕过可取消读取；最终34245扩展三轮race根包21.056s/strategy22.540s/storage5.704s、71172完整根包30.468s/strategy140.792s/storage11.149s/risk3.784s/order10.637s/execution1.227s、48399 vet均exit0，全部本版验证会话已终止。JSON/Markdown报告 `/private/tmp/quantmesh-ordinary-restore-context-rc1038.Efypus/`；未重跑完整Web/前端、构建、实库MySQL或同提交发布，不代表实盘或盈利验收。

## rc1037：剩余资产历史恢复读取取消（本地验证完成，未提交）

- 实际Start两阶段取消用例覆盖remaining探测/钱包内重读；生产改为启动ctx/operationCtx读取，已识别cover账本后缺能力拒绝，不退回后台读取。取消必须保留原checkpoint、不导入历史资产、不发布managed reconciliation、零金融RPC并释放锁/操作槽。
- 修复前91626两模式已终止exit1/strategy0.766s，remaining读取绕过接口且返回只读接管。中间63742三轮race及17575完整包exit1，剩余资产/manager两组旧夹具缺ContextReader导致提前拒绝；局部声明能力后扩展最终组合重跑，未降低生产要求，失败保留不充当最终通过。
- 剩余资产夹具局部声明ContextReader，钱包内加载钩子仍核验来源改变/丢失、取消和归属，普通legacy store不扩接口。待最终剩余资产/全部借还补币与SQL构造器/race/完整包/vet验证。
- 此路径只恢复历史账本，不读取或采用当前余额；普通restore、旧接口初次探测、实库MySQL与金融RPC fencing/当前经济核账/受管处置仍开放。R01–R15/同提交发布/盈利未闭环，未提交/推送/发布/部署或真实账户访问。
- 最终43309扩展三轮race根包17.802s/strategy19.153s/storage3.799s、74613完整根包27.361s/strategy141.695s/storage10.571s/risk4.079s/order9.476s/execution2.617s、12326 vet均exit0；全部派发会话终止，版本一致/diff通过。中间夹具能力失败已保留，不充当最终通过；JSON/Markdown报告 `/private/tmp/quantmesh-remaining-read-context-rc1037.iSY1dl/`。未重跑完整Web/前端、构建或同提交发布，不代表实盘或盈利验收。

## rc1036：补币请求/成交恢复读取取消（本地验证完成，未提交）

- 补币CID与未核验成交恢复的两次读取改为具备ContextReader时使用启动ctx及钱包operationCtx，识别pending后缺能力拒绝，不退回后台读取。四阶段实际Start回归要求原请求/ACK不变、零交易所查询及金融动作、锁与操作槽释放。
- 修复前82448四模式已终止exit1/strategy0.809s，reader仅调用借款探测1次，补币读取绕过接口；不将失败证据当作修复后通过。
- 恢复内存夹具显式升级局部ContextReader，不改变通用legacy接口；还款取消用例沿实际新探测序列定位，避免误测前置阶段。待最终包括CID/成交、资本释放旧接口拒绝及SQL构造器的race/完整包/vet。
- 旧接口初次探测、剩余资产恢复/普通restore读取、即时金融写入、真实MySQL及完整经济核账尚未闭环；R01–R15/受管处置/同提交发布/盈利仍开放，未提交/推送/发布/部署或真实账户操作。
- 最终93269扩展三轮race根包19.816s/strategy21.430s/storage3.926s、42728完整根包31.062s/strategy143.932s/storage11.373s/risk4.836s/order13.043s/execution3.279s、5271 vet均exit0；全部派发会话终止，版本一致/diff通过。JSON/Markdown报告 `/private/tmp/quantmesh-cover-read-context-rc1036.2aFsZ4/`；未重跑完整Web/前端、构建或同提交发布，不代表实盘或盈利验收。

## rc1035：完整构造器SQLite补币CID冲突（本地验证完成，未提交）

- rc1034内存冲突不足以证明生产适配器接线。新增真实SQLite完整构造器正确ACK/错误CID/查单期间cover-7改cover-new，正例保存原订单7/请求数量0.401/价格50000并清理pending cover intent但保留UNKNOWN/in-flight/未验证成交；负例精确保留原/新payload。三腿资本claim/运行租约保留，零金融RPC，流各清理一次，不查不存在的成交或启动交易/managed reconciliation。
- 仅测试/版本/记录，不修改生产金融语义，无修复前缺陷红测。待最终三轮race/完整包/vet，fake venue/锁与SQLite不是实盘/实库MySQL/跨进程故障证明，保留封锁不代表当前经济核账及受管处置闭环。R01–R15/同提交发布/盈利仍开放，未提交/推送/发布/部署或真实账户访问。
- 最终83479完整构造器组合三轮race根包8.458s、87758完整根包27.225s/strategy141.812s/storage11.295s/risk3.368s/order10.920s/execution3.465s及vet均exit0。全部派发会话终止，版本一致/diff通过；JSON/Markdown报告 `/private/tmp/quantmesh-cover-cid-sql-rc1035.uRuNQj/`。新增实际SQL CID冲突接线证明，不代表完整Web/前端、构建、同提交发布、实盘或盈利验收。

## rc1034：补币CID恢复ACK条件更新（本地验证完成，未提交）

- 实际Start回归在确切CID查询期间变更耐久pending CID，旧查询ACK不得清除新请求。恢复绑定原store/schema/payload，ACK检查点使用现有恢复条件更新链；缺能力不查询/不无条件回退，冲突仍保持未知并保留新durable请求，不重买/还款。
- 修复前64600已终止exit1/strategy0.770s，旧ACK覆盖新durable请求，不当作修复后通过证据。
- 普通实时金融ACK无恢复来源上下文仍沿用原保存契约，保持受理金融请求证据处理语义；等待最终覆盖CID/部分成交/借还SQL构造器及停止回归的race/完整包/vet。
- 读取等待、UNKNOWN订单经济恢复、真实MySQL/金融RPC fencing、当前账户核账及受管处置仍开放。R01–R15/同提交发布/盈利未闭环，未提交/推送/发布/部署或真实账户操作。
- 最终45634组合三轮race根包12.286s/strategy19.118s/storage4.782s、79604完整根包28.519s/strategy140.617s/storage11.274s/risk3.860s/order9.696s/execution3.520s、41810 vet均exit0；全部派发会话终止，版本一致/diff通过。JSON/Markdown报告 `/private/tmp/quantmesh-cover-ack-cas-rc1034.ndLuMf/`；新增CID冲突为内存store/fake venue，未新增实际SQL CID冲突测试，未重跑完整Web/前端、构建或同提交发布，不代表实盘/盈利验收。

## rc1033：补币成交恢复原子检查点（本地验证完成，未提交）

- 全成/取消部分成交实际Start回归在GetOrderFills期间把保存ACK的order ID 7改8，修复前旧费用/净量证明覆盖新payload并可能发布managed reconciliation。恢复操作绑定原store/schema/payload，全成及终态部分成交检查点都通过原子条件更新，冲突回滚本地成交证据并保留未知，零重买/还款。
- 修复前27072两个模式已终止exit1/strategy0.779s；不以旧失败证据代替修复后通过。
- 将既有还款条件更新helper泛化复用；普通实时金融保存没有恢复来源上下文时仍保持旧契约。待最终包含部分成交、完整SQL构造器及还款证据链的race/完整包/vet。
- 其他补币ACK恢复、读取等待、真实MySQL/金融RPC fencing、当前债务/利息/资产及受管经济恢复仍开放；R01–R15/同提交发布/盈利未闭环，未提交/推送/发布/部署或真实账户访问。
- 最终99465组合三轮race根包13.248s/strategy18.598s/storage5.191s、66155完整根包27.040s/strategy141.390s/storage10.663s/risk4.165s/order12.028s/execution3.127s、46083 vet均exit0。全部派发会话终止，版本一致/diff通过；JSON/Markdown报告 `/private/tmp/quantmesh-cover-fill-cas-rc1033.FIJtQa/`。实际SQLite部分成交接管与借还构造器已覆盖，不代表完整Web/前端、构建、同提交发布、实盘或盈利验收。

## rc1032：还款恢复检查点读取取消（本地验证完成，未提交）

- 实际Start两阶段取消回归验证还款探测/钱包内重读是否使用调用者上下文；修复前绕过可取消接口。具备reader时分别用启动ctx和operationCtx，已识别pending却无能力时拒绝恢复，不影响非pending旧存储探测契约。
- 修复前95410已终止exit1/strategy0.872s，两个模式reader均仅调用1次（借款探测），还款读取绕过接口并保存收据；不将此中断前结果当作修复通过。
- 针对性借/还及实际SQL构造器、资本释放legacy负例/race/完整包/vet待最终结果；借款专用ContextReader包装继续局部隔离，不给通用memory fixture新增接口。取消要求原ACK不变、零收据查询/还款RPC、策略锁/操作槽释放。
- 旧接口初次探测、其他金融恢复读取及即时还款写入、实库MySQL/金融RPC fencing/经济核账仍开放。R01–R15/受管恢复/同提交发布/盈利未闭环，未提交/推送/发布/部署或真实账户操作。
- 中间28659三轮race与46969 vet通过未覆盖补币消费/利息的旧恢复夹具；36072完整exit1/strategy25.261s，两个夹具未声明ContextReader导致提前拒绝及后续断言越界。显式升级这两组夹具，不改变通用legacy store，不放宽生产要求，扩展最终组合及完整包重新验证。
- 最终73338扩展三轮race根包15.307s/strategy18.129s/storage2.755s、7845完整根包26.749s/strategy140.844s/storage9.353s/risk2.403s/order10.213s/execution1.555s及最终vet均exit0。全部派发会话终止，版本一致/diff通过，中间失败保留不当作通过；JSON/Markdown报告 `/private/tmp/quantmesh-repay-read-context-rc1032.0SN1Vz/`。未重跑完整Web/前端、构建或同提交发布，未进行实盘或盈利验收。

## rc1031：完整构造器SQLite还款条件更新（本地验证完成，未提交）

- rc1030内存测试不足以证明还款的SQL适配器接线。本版新增完整构造器正确收据/错误ID/查询期间7改8的实际SQLite回归：本金0.4归还为0、事件2条、清理repay intent但UNKNOWN/in-flight保留；负例精确保留原/新payload，均保留三腿资本claim/所有权、零金融操作、流各清理一次且不启动交易或managed reconciliation。
- 仅测试/版本/记录，不修改生产金融语义，无修复前缺陷红测。待最终race/完整包/vet；fake venue/锁与SQLite不是真实交易所/实库MySQL或跨进程租约故障证据，保留封锁不代表完整经济恢复，R01–R15/同提交发布/盈利仍开放。未提交/推送/发布/部署或真实账户访问。
- 最终81339完整构造器借/还收据六模式三轮race根包4.531s、35158完整根包29.695s/strategy143.262s/storage8.663s/risk4.135s/order8.956s/execution1.590s及vet均exit0。全部派发会话终止，版本一致/diff通过；JSON/Markdown报告 `/private/tmp/quantmesh-repay-sql-constructor-rc1031.v7IzgP/`。未重跑完整Web/前端、构建或同提交发布，不代表实盘或盈利验收。

## rc1030：还款恢复条件写入证据链（本地验证完成，未提交）

- 75353真实Start红测已终止exit1/strategy0.786s：查询7号还款时pending身份改为8号，旧收据覆盖了新耐久快照。恢复操作绑定原schema/payload/store，每次本金/cover证据保存及最终意图清理均CAS；成功的新payload成为后续CAS来源，冲突不清理意图、不重发还款。
- 普通即时还款无恢复来源上下文时保持旧持久化契约；恢复缺原子写入能力拒绝，不做无条件回退。待最终回归/race/vet，其他恢复路径、读取等待、真实MySQL及金融RPC fencing仍开放。
- R01–R15、当前债务/利息/资产及受管经济恢复、同提交发布/盈利仍未闭环；未提交/推送/发布/部署或真实账户访问。
- 最终68878组合三轮race根包8.382s/strategy177.416s/storage3.496s、5052完整根包27.769s/strategy141.244s/storage10.038s/risk2.860s/order10.410s/execution1.678s、22973 vet均exit0，覆盖本金写入及意图清理两个CAS边界冲突保留新payload/不重发金融请求。中间52272/97953通过不含最终两例，不替代最终结果；全部派发会话终止，版本一致/diff通过。JSON/Markdown报告 `/private/tmp/quantmesh-repay-cas-rc1030.kE0KsW/`；未重跑完整Web/前端、构建或同提交发布，未新增实际SQL还款构造器，不代表实盘/盈利验收。

## rc1029：借款恢复检查点读取取消（本地验证完成，未提交）

- 13650真实Start三阶段红测已终止exit1/strategy0.805s：可取消reader调用数为0，生产恢复绕过接口并保存收据。生产适配器已有QueryRowContext，但此前三次恢复读取均走Background。
- 具备可取消reader时初次探测使用启动上下文，钱包内复查和持策略锁的最终复查使用operationCtx；已识别pending借款但缺能力时拒绝，不退回无界读取。三阶段取消保持原ACK及零金融动作，并验证锁释放，待最终race/完整包/vet。
- 旧接口未识别借款前仍旧式探测，其他金融恢复链路、实际MySQL、租约fencing和经济核账仍待闭环。R01–R15保持开放，未提交/推送/发布/部署或真实账户访问，不代表盈利验收。
- 中间73152三轮race/63278 vet通过不含legacy负例；75892完整exit1，strategy143.565s发现给通用memory store新增ContextReader污染了其他策略旧接口拒绝测试前提。改为仅借款测试显式包装，不改变通用legacy夹具；最终候选37840扩展资本释放/namespace负例race及30789完整包验证运行中，失败记录保留，不冒充最终通过。
- 最终37840扩展三轮race根包14.418s/strategy10.982s/storage3.077s、30789完整根包26.462s/strategy140.510s/storage9.019s/risk3.339s/order10.181s/execution1.522s及最终vet均exit0。全部派发会话终止，版本一致/diff通过。JSON/Markdown报告 `/private/tmp/quantmesh-borrow-read-context-rc1029.QTNrkL/`；未重跑完整Web/前端、构建或同提交发布，不代表实盘或盈利验收。

## rc1028：借款检查点原子条件写入（本地验证完成，未提交）

- rc1027已确认的复查到写入窗口改为SQL单语句条件UPDATE，按Bot/strategy/schema/raw payload比较；MySQL使用BINARY避免大小写不敏感匹配。无匹配不插入，缺能力不退回无条件Save；SQL报错仍保留未知，冲突不导入旧收据本地账本。
- SQLite新增身份/schema/字节差异/取消/过期写者测试，策略夹具在最终读后、CAS前更新借款身份，验证保留新证据及零金融调用。实际构造器沿适配器接入SQL CAS，待最终组合race/完整包/vet；本版未做修复前红测，不将rc1027红测冒充本版边界红测。
- payload CAS不防ABA，也不是金融RPC fencing；真实MySQL、当前本金/利息/资产及受管经济恢复、R01–R15/同提交发布/盈利仍开放，未提交/推送/发布/部署或真实账户操作。
- 最终7421组合三轮race根包8.872s/storage4.623s/strategy9.250s、69289完整根包29.409s/strategy142.037s/storage8.659s/risk4.920s/order9.099s/execution1.732s、33526 vet均exit0；全部派发会话终止，版本一致及diff检查通过。JSON/Markdown报告 `/private/tmp/quantmesh-borrow-cas-rc1028.rj1QsD/`。未重跑完整Web/前端、构建或同提交发布；实际SQLite加fake venue不是实盘或跨进程租约验收。

## rc1027：借款收据来源快照新鲜度（本地验证完成，未提交）

- 确定性真实Start回归39371已终止exit1/strategy0.760s：查询42号借款期间存储更新为43号，旧收据覆盖了更新的耐久证据。生产恢复在写回前重读并比较schema/raw payload/found，读失败或来源变更直接拒绝导入，再核验上下文及归属；不重放金融请求。
- 新增完整构造器实际SQLite同场景，要求最新payload、三腿资本及运行租约保留、零金融动作、流各清理一次。待最终race/完整包/vet结果；旧rc1026通过不继承为本版验收。
- 本次比较不是跨进程原子CAS，读到写间仍有窗口；钱包协作协议之外的并发写入及完整经济恢复仍待闭环。R01–R15保持开放，未提交/推送/发布/部署或真实账户访问，不能宣称实盘或盈利验收。
- 最终90280组合三轮race根包7.982s/strategy9.424s、78814完整根包30.441s/strategy141.415s/risk3.035s/order11.492s/execution3.540s、53331 vet均exit0。全部派发会话终止，版本一致及diff检查通过；JSON/Markdown报告 `/private/tmp/quantmesh-borrow-freshness-rc1027.LsCuKC/`。未重跑完整Web/前端、构建或同提交发布，SQLite/fake venue不是实盘验收。

## rc1026：完整构造器的SQL借款收据与保留验证（本地验证完成，未提交）

- rc1025策略内存回归不能证明启动失败后的真实资本清理。新增实际SQLite/完整构造器，已保存ACK经spot_margin确切ID查询，正例要求耐久历史本金/UNKNOWN/in-flight与三腿资本claim保留；错误ID必须保留原payload。两例都拒绝交易启动，结构化retention不能冒充managed reconciliation；零金融操作、每条stream清理一次、三腿运行所有权不提前释放。
- 本版仅测试/版本/记录，不改生产金融语义，无缺陷红测。等待最终验证，fake venue/锁不是实盘或跨进程故障验收；保留资本与租约不是可恢复处置闭环，仍需当前债务/利息/资产核账及受管恢复路径。R01–R15、完整经济恢复/同提交发布/盈利保持开放，未提交/推送/发布/部署。
- 首轮完整SQL构造器61767三轮race2.931s通过，但进一步检查发现新检查点仅写SQL未导入本地财务视图：直接StopContext从DirectionNone/unknown=false误报成功。新增真实Start→Stop回归52878红测strategy exit1/0.744s；保存ACK成功后才同步本地方向/历史本金/事件及UNKNOWN/in-flight，不提前写本地核清状态或执行金融动作。此前仅测试范围已扩展为该接线bugfix；等待最终源码验证，不沿用首轮结果。
- 中间40241三轮race根包3.535s通过/strategy0.977s失败：Stop更早的cancel=nil分支绕过财务核验。补上未启动策略的未核清状态拒绝，不尝试自动平仓/还款，干净零财务状态仍允许无操作停止；原87653完整旧源码会话继续观察，不因等待重启，也不作为最终修复通过证据。
- 87653已终止：完整strategy142.719s同一红测失败，其他包通过，不沿用。33695最终候选完整策略141.679s通过但根包27.121s失败，暴露剩余资产/部分成交只读接管退化：manager把失败启动的金融Stop拒绝包装成rollback未核实，无法识别原精确reconciliation结果。修复为仅对FundingCarry精确单因reconciliation结果排空生产者并重新VerifyRemainingReconciliation；其他错误和已启动策略仍金融Stop，未知借款收据不纳入该豁免。两次失败证据保留，继续最终全量验证。
- 最终8650扩展组合三轮race根包7.831s/strategy8.452s，通过实际SQL借款保留、剩余资产/部分成交只读接管、strict retention与启动回滚/停止；50599完整根包28.746s/strategy140.515s/risk3.406s/order10.260s/execution3.037s、18919最终vet均exit0。版本一致/diff通过，全部本版派发验证终止，中间失败均保留不冒充最终门禁。JSON/Markdown报告 `/private/tmp/quantmesh-borrow-constructor-rc1026.n0RhAL/`。实际SQLite加fake venue/锁不代表实盘/跨进程故障，未重跑完整Web/前端、构建或同提交发布；当前账本/资产受管恢复与R01–R15继续开放，未提交/推送/发布/部署或盈利验收。

## rc1025：已保存借款ACK的只读收据检查点（本地验证完成，未提交）

- 实际Start没有借款ACK恢复步骤，普通decode拒绝DirectionNone携带borrow ID。红测90963 strategy exit1/0.795s：实际借款查询中断并保存ACK后，重新Start不能补齐债务事件。
- 专用恢复只接受当前schema的孤立pending ACK，先验证非ACK字段及原账本、确切账户范围，再只读查询保存ID的BORROW收据；币种/状态/时间/金额与本金组件及新账本一致后耐久保存实际本金。保留UNKNOWN/in-flight，普通decoder不放宽，重复启动不重复导入；不借款/还款/下单或恢复交易。
- 原ACK没有原始请求数量，不能声称请求数量吻合；本检查点仅证明收据记录的历史本金，不证明当前余额/负债/利息或资产归属。下一阶段仍需当前账户核账与可恢复处置，R01–R15、完整经济恢复/同提交发布/盈利保持开放。待最终验证，未提交/推送/发布/部署或真实账户操作。
- 最终85337三轮race strategy3.121s、67040完整根包27.223s/strategy141.252s/risk4.122s/order11.400s/execution2.510s、78756 vet均exit0；diff及版本一致通过，全部本版派发验证终止。首轮45169/1.851s不含最终负例，不继承其结果。实际Start回归覆盖错误账户/ID/币种/本金、利息/非有限金额、pending、归属丢失、写入失败和旧schema不改写原快照/不发金融请求；实际已保存ACK正例导入一次，重复Start保持停机。报告 `/private/tmp/quantmesh-borrow-receipt-rc1025.AquZOT/`。内存store/fake venue不是完整构造器SQL或实盘金融恢复；未重跑完整Web/前端、构建或同提交发布门禁，R01–R15仍开放。

## rc1024：FundingCarry借款/开仓抵押划转的健康检查（本地验证完成，未提交）

- 原反向开仓及ensureFuturesMargin只依赖钱包归属与上游tick检查，借款/划转不经订单executor。确定性state-store在首次新意图保存后失效，红测strategy exit1/0.772s：借款路径发生借款/卖出/补偿还款三次金融动作，抵押路径划转一次，证明仅拦订单不够。
- 两个操作在新意图持久化后、首次金融RPC前检查共享gate；确定尚未发送时调用现有finishRuntimeIntent(success=true)完成无金融动作意图，不凭暂停制造UNKNOWN；保存或归属失败继续保留fail-closed恢复语义。还款/保护性平仓不接此新增开仓检查，已有真实未知RPC处理不改。
- 待最终回归；内存store/fake venue不是实际SQL/交易所验收，检查到RPC仍非原子，R01–R15/完整经济恢复/同提交发布/盈利仍开放。未提交/推送/发布/部署或真实账户操作。
- 最终49446组合三轮race strategy75.055s、7485完整根包27.893s/strategy141.466s/risk4.405s/order9.652s/execution2.825s、71151 vet均exit0；版本一致/diff通过，全部本版派发验证终止。新增清理失败回归保留原pending durable payload及本地未知标记，零金融调用；首轮6021/1.884s不含最终失败断言，不继承其结果。报告 `/private/tmp/quantmesh-wallet-health-rc1024.Yal33p/`。未重跑完整Web/前端、构建或同提交发布门禁；正常/关闭路径的完整策略回归不能冒充真实账户金融恢复，R01–R15仍开放。

## rc1023：实际重试循环的健康屏障与UNKNOWN保持（本地验证完成，未提交）

- rc1022专用日志屏障仅覆盖首发，不能从旧回归推断所有重试故障。新增五模式实际ExchangeOrderExecutor循环：限速/PostOnly第一次明确拒绝，第二次提交尝试日志写入后失效；开仓必须只发送一次且耐久拒绝可重启，保护性平仓必须第二次发送成功。模糊首发响应同时健康失效时仍UNKNOWN，不重发，重新构造执行器加载同一journal也必须保持待核账。
- 仅增加夹具/回归及版本记录，不改生产金融语义，无修复前缺陷红测；等待最终结果，不代表真实DB抖动、实际交易所、原子RPC fencing、完整经济恢复或盈利。R01–R15原范围不变，未提交/推送/发布/部署或真实账户操作。
- 最终72338三轮race order2.463s、64230完整根包28.504s/order9.062s/execution2.006s/risk4.508s及独立vet均exit0；版本一致/diff通过。全部派发验证结束，JSON/Markdown报告 `/private/tmp/quantmesh-retry-health-rc1023.6cQNU8/`。此证据补足特定真实循环的五种故障组合，不代表完整经济恢复；未重跑完整策略/Web/前端、构建或同提交发布门禁。

## rc1022：提交日志等待后的权益健康复查（本地验证完成，未提交）

- 实际循环已有明确拒绝后重试的暂停检查，rc1020动态健康已接入该检查，不能把整个重试复查说成缺失。新增确定性journal夹具在第二次写入（submission attempt）后失效，原代码仍下单一次且返回成功；红测order exit1/0.729s，保护性平仓正例保留。
- 每次prepareJournalSubmission成功后、真实PlaceOrder前追加共享gate健康检查，覆盖首发和每次可安全重试；未发送的确定性拒绝由现有finishIntent保存rejected并释放本地意图，不标造UNKNOWN。测试同时重新构造执行器读回同一journal，要求该拒绝不产生虚假待恢复订单；待最终回归。
- 检查和真实RPC之间仍非原子，不能撤回已发送请求或证明跨进程金融fencing；实际数据库延迟/金融经济恢复/同提交发布/盈利仍未验收。R01–R15原范围不变，未提交/推送/发布/部署或真实账户操作。
- 最终31400三轮race order13.634s/execution2.670s、25686完整根包30.161s/order10.554s/execution1.741s/risk2.785s、37822 vet均exit0；diff及版本一致通过，全部本版已派发验证终止。报告 `/private/tmp/quantmesh-submission-health-rc1022.yJ6gDl/`；专用内存journal屏障只验证首次开仓和保护性平仓，组合含既有拒绝重试回归，不宣称每种重试金融故障或真实DB抖动均已验收。未重跑完整Web/策略/前端或构建，不继承旧版同提交发布门禁。

## rc1021：动态准入到撤单封锁的零值安全（本地验证完成，未提交）

- rc1020令零值gate即使sources为空也可被动态检查封锁；生产CancelResidualOpeningOrders使用HoldIfBlocked追加撤单未核实来源，原实现直接写nil map导致panic。新增确定性回归红测execution exit1/0.663s复现assignment to entry in nil map。
- 同一gate锁下初始化map再安装来源；健康恢复仍保留撤单未核实来源，只有确切来源解除后才能重新开仓。待最终回归；R01–R15、逻辑订单重试期间的有效期再检查、在途RPC fencing、完整经济恢复及盈利验收仍开放。未提交/推送/发布/部署或真实账户操作。
- 最终42569三轮race execution1.862s/order1.944s、86040完整根包28.940s/execution2.877s/order10.626s/risk2.794s、19468 vet均exit0，diff/版本一致通过。实际CancelOwnedOpeningOrders+fake venue验证仅ACK未终止时保持封锁、健康恢复不解除、确认终态后才移除来源，不是实盘撤单证据。报告 `/private/tmp/quantmesh-live-cancel-rc1021.8xiF8n/`；全部本版已派发验证终止，未重跑完整策略/Web/前端、构建或同提交发布门禁，R01–R15继续开放。

## rc1020：权益过期的开仓准入检查（本地验证完成，未提交）

- 实际OpeningGate.Begin此前只看已安装sources；健康数据自然过期但后台30秒检查未执行时仍可准入。共享协调器增加独立唯读predicate，管理器经启动context在三个生产构造器策略启动前安装；每次准入读取当前健康/有效期，失效或缺少有效期拒绝，不在入口调用Bot/数据库/交易所或持有transitionMu。背景来源持久化/撤单路径继续保留，不用谓词清除其他来源。
- 确定性风险回归只移动观测有效期，不发布暂停/不调用checker，覆盖回撤、日亏损、连续亏损及仅连接类正例、恢复和手动来源保留。真实物理执行器+网格/多策略adapter回归验证拒绝不开单不泄漏资金，保护性平仓仍执行；实际SQL/完整FundingCarry构造器另检查callback安装与拒绝。等待最终验证，不继承旧版结果。
- 仍不证明已准入的金融RPC fencing、过期时残留挂单即时撤单、多进程原子传播、完整UNKNOWN经济恢复或盈利；R01–R15原范围不变，未提交/推送/发布/部署或访问真实账户。
- 最终三轮race40960根包4.864s/risk8.782s/execution1.774s、完整回归34485根包29.396s/risk2.915s/execution1.379s、vet20434均exit0；版本一致/diff通过。首轮50384通过但不含随后最终断言，不沿用其结果。本版未跑修复前红测，缺口来自原生产调用链检查；JSON/Markdown报告 `/private/tmp/quantmesh-expiry-admission-rc1020.G1mHWX/`。完整FundingCarry构造器使用实际SQLite/fake venue，普通/FundingPerpSpread安装点已检查但不是各自完整过期故障验收；未重跑完整Web/策略/前端、构建或同提交发布门禁。

## rc1019：已知暂停先封锁本地再持久化（本地验证完成，未提交）

- 检查数据过期与执行边界时定位到更直接缺口：OpeningPauseCoordinator.Pause先等待最多5秒SQL Upsert，再装holders/当前Bot gate；已知权益失效和直接风险暂停都可能在慢库期间放行旧Bot。确定性store写入屏障回归要求在Upsert尚未返回时gate已存在，不以计时猜测数据库速度。
- holder/pending及本地来源先安装，再进行既有持久化；仅成功ACK删除pending，失败与后续共享刷新仍保留封锁。串行transition保护启动/释放，未修改来源独立释放或删除其他持有者。验证待终态；纯过期依赖后台轮询的延迟仍开放，不代表RPC金融fencing、多进程原子传播或盈利验收。R01–R15保持原范围，未提交/推送/发布/部署或真实账户操作。
- 数据库写入屏障红测risk exit1/1.243s，直接暂停和权益失效两条路径均失败。修复后最终屏障/指标健康/持久化失败/来源恢复组合三轮race9.000s通过；最终完整根包risk68502及vet93513待终态，版本一致/diff通过。报告 `/private/tmp/quantmesh-pause-publish-rc1019.uhklWs/`；屏障store和source-owned Bot为fixture，实际SQL/完整构造器回归另包含于完整根包，不代表真实数据库抖动或金融RPC故障验收。
- 68502/93513已终止：完整根包36.145s/risk4.889s、独立vet均exit0。JSON/Markdown同步，全部本版验证结束；不继承完整Web/前端、构建或同提交发布门禁，纯过期轮询延迟、跨进程传播与在途RPC保护及R01–R15继续开放。未提交/推送/发布/部署或盈利验收。

## rc1018：首轮权益观测前的启动封锁（本地验证完成，未提交）

- main实际先NewGlobalCircuitBreaker→SetPauseCoordinator→启动feeder goroutine，再自动StartBot；此前绑定不发布尚未观测数据的启动持有者，不能凭goroutine已启动假设首轮核账完成。新增零观测指标回归及真实SQL/完整FundingCarry构造器启动回归；日亏损、连续亏损、回撤启用需封锁，仅连接类不要求指标gate。
- 绑定同步调用既有健康gate，不提前放开交易；depsMu释放后调用，避免健康gate内部deps读锁递归等待。数据恢复仍仅释放自有来源、保留SQL迁移/手动持有者。验证待终态；不代表配置热启用整个生命周期、在途提交fencing、真实权益/借贷经济恢复或盈利验收，R01–R15继续开放。未提交/推送/发布/部署或真实账户访问。
- 实际SQL/完整构造器无观测红测根包2.676s/risk1.063s，exit1：drawdown/daily_loss/loss_streak缺初始holder，连接类不要求指标gate。修复后最终三轮race根包4.756s/risk4.363s通过，含初始化SQL来源、完整构造器启动继承、健康恢复只删除自身、持久化失败封锁及既有协调器回归；独立vet60742、版本一致/diff exit0。完整根包risk21540仍运行，报告 `/private/tmp/quantmesh-equity-initial-rc1018.NSvgwV/`。
- 21540现已终止：完整根包34.089s/risk4.724s，exit0，JSON/Markdown同步。全部本版验证结束，不继承完整Web/前端、构建、真实权益或同提交发布门禁。未提交/推送/发布/部署，R01–R15、配置热启用和在途RPC保护仍开放。

## rc1017：权益数据失效的后续启动封锁（本地验证完成，未提交）

- 回查R10及实际启动接线：已观察到权益/现金流水失效时，只向GetAllBots快照安装本地风险gate；随后StartBot仅继承OpeningPauseCoordinator holders，因此新Bot可能在下一轮风控检查前没有此封锁。新增真实协调器/耐久store/BeginBotStart回归从零现存Bot开始，验证失效持有者传播与恢复只删除自身来源。
- 独立risk_metrics_unavailable来源通过既有Pause/ReleaseChecked同步，保留本地SetRiskDataUnavailable兼容路径；持久化错误不当作已解除，不调用通用ResumeOpening。验证待终态；不覆盖首次风控初始化之前的启动/在途提交原子fencing，借贷市场完整权益/盈利/R01–R15仍开放。未提交/推送/发布/部署或真实账户操作。
- 原启动红测risk exit1/0.952s。中间owner-scoped manual预期错误risk1.116s，修正实际来源后初始完整risk race4.135s通过。新增真实SQLite/完整FundingCarry构造器夹具先出现参数编译错误、缺OpeningPauseHolders迁移及误用RiskCheckpoints迁移，再出现错误单行预期：真实迁移还创建独立opening_pause_legacy_state_unverified来源。修正夹具并保留迁移封锁，不改生产schema或弱化安全来源；中间失败均保留于报告。观察等待不是死锁证据，原会话后续终态表明已失败结束，没有因观察超时重启。
- 最终实际SQL+完整构造器传入BeginBotStart持有者，真实BotProvider健康恢复只删除权益来源，保留迁移gate；故障store覆盖写入/删除失败后启动仍封锁。最终三轮race63677/完整根包risk56294/vet69987待终态，报告 `/private/tmp/quantmesh-equity-startup-rc1017.QIvaT8/`。健康标记由fixture显式提供，不代表真实权益/盈利核验。
- 三会话已终止：最终三轮race根包4.031s/risk4.435s、完整根包34.614s/risk3.268s、独立vet均exit0，diff及版本一致通过。JSON/Markdown同步，全部本版验证结束；没有完整Web/前端、构建、真实账户核账、同提交发布或盈利验收，R01–R15仍开放。

## rc1016：完整FundingCarry停机释放接线（本地验证完成，未提交）

- 从无历史策略状态的真实隔离SQLite构造器启动正常策略；fake venue提供独立空持仓/订单/余额证据且拒绝金融RPC、不给交易信号。实际rt.StopWithError执行排空/停止、fc.VerifyFlat、真实SQL资本释放，随后只对runtime-owner租约注入首次解锁未执行失败。重试所有venue拒绝账户读取，要求零额外读/金融RPC和流清理只一次、三腿Unlock共四次。
- 补齐rc1015金融/资本callback夹具未覆盖的实际生产接线，不改生产恢复语义；验证待终态。真实Redis、解锁已执行但响应丢失、跨进程停机阶段耐久恢复及R01–R15仍开放，未提交/推送/发布/部署或实盘/盈利验收。
- 初始完整构造器三轮race2.818s通过，尚无最后SQL/peer断言；追加直接SQL读回schema7已核实零持仓/零债务、实际peer持有已释放腿及重试金融payload不变。中间组合6.002s和完整根包27.958s失败：fixture误以为失败腿固定spot_margin，生产实际按scope键排序；改为按注入失败确切key验证，不修改生产排序或放宽peer断言。最终组合82253和完整根包54257运行中，vet/diff exit0、版本一致；报告 `/private/tmp/quantmesh-full-stop-rc1016.KcRt15/`。该干净零仓位夹具不代表非零敞口实际平仓已验收。
- 最终82253/54257已终止：针对性三轮race6.729s、完整根包28.019s，exit0，包含实际peer精确归属及SQL断言。全部本版已派发验证结束，JSON/Markdown同步；没有完整策略/Web/前端、构建、真实Redis、同提交发布或盈利验收。R01–R15仍保持原范围，未提交/推送/发布/部署。

## rc1015：FundingCarry多腿租约释放重试（本地验证完成，未提交）

- 真实StopBot/StopAll配合现有多腿释放helper红测exit1/1.509s：纯解锁失败被标成金融未核实，未接入普通运行时已实现的typed受管重试。生产正常停机末尾此前使用Release(false)，失败停止续租。
- 新共用release阶段只在金融停止成功后调用：资本核验/释放成功仅一次，成功腿不重复Unlock，未释放腿release(true)保持续租；归属/金融核验前后及逐腿释放重新检查，丢失归属或资本失败不返回typed重试。初始化回滚仍保持原bounded cleanup，不产生无管理器续租。
- 新实际控制器回归覆盖多腿部分释放、StopBot/StopAll重试、金融/资本回调一次及失去归属/UNKNOWN拒绝。验证待终态；资本回调及锁provider为隔离夹具，不是完整FundingCarry干净仓位构造器或真实Redis故障验收，RPC响应丢失但服务端已删锁、跨进程释放阶段恢复、R01–R15仍开放。未提交/推送/发布/部署/真实账户或盈利验收。
- 中间race2.569s失败，后续诊断1.284s定位到fixture未配置eventBus，StopBot成功尾部panic；较早完整根包21.187s失败同因/锁包1.956s通过。补齐夹具生产所需事件总线，不改生产判定；失败不删除。最终针对性三轮race7.722s通过，精确验证失败租约自身续期、peer取得已释放腿后重试不重复Unlock、实际主库disabled和journal退休；既有普通释放/SQL恢复构造器亦包含。最终独立vet根包/lock和diff exit0、版本一致；完整根包/lock会话45060仍运行。报告 `/private/tmp/quantmesh-funding-release-rc1015.3L2Gbw/`。
- 最终会话45060已终止：完整根包28.220s/lock0.736s，exit0，包含最后peer归属及主库断言；JSON/Markdown同步，所有本版已派发验证完成。本版没有重跑完整策略/Web/前端或构建，不继承rc1014等旧版门禁。未提交/推送/发布/部署，R01–R15及完整金融恢复继续开放。

## rc1014：首次查询成交到SQL接管证据（本地验证完成，未提交）

- 完整生产构造器从schema7未核实补仓记录及真实SQLite共享UNKNOWN开始；精确订单查询/原币费用经策略恢复写入SQL，独立oracle检查CID、scope、请求、0.2 gross/0.1995 net、0.0005 BTC费、0.4债务及未知标记。后续实际停止/失败重试继续要求保留SQL证据、三类claims、UNKNOWN且不执行金融RPC。
- 三轮完整构造器race红测exit1/4.861s：首次查询保存成交后钱包协调helper以errors.Join包装只核账结果，即使Unlock成功也不是单因错误，严格构造器正确拒绝。修复为callback保存pending并返回nil，只有协调/归属/清理成功后才返回精确只核账结果；不放宽多因失败接管规则。测试待终态。未验证跨表崩溃原子性/真实余额/完整债务恢复或同提交发布，不宣称实盘或盈利验收；R01–R15继续开放。
- 修复前完整根包exit1/27.378s，同一构造器缺陷。修复后最终构造器及新增钱包清理失败三轮race根包4.953s/strategy2.400s通过；清理失败即使成交已耐久保存仍不产生只核账接管错误类型。独立vet根包/strategy及diff exit0。较早完整根包/策略会话13508尚运行且不包含最后清理失败断言；最终完整strategy另行派发，不继承旧版门禁或把运行中记为通过。报告 `/private/tmp/quantmesh-first-query-rc1014.rnJ31Q/`。
- 会话13508现已终止，exit0：完整根包27.652s/策略141.796s，但其编译早于最后清理失败断言；最终完整strategy会话37101仍运行，待终态。下一处已确认生产接线缺口：FundingCarry正常停机最后仍调用releaseFundingCarryRuntimeOwnershipLeases→Release(false)，解锁失败停止续租，且不产生受管typed释放重试；普通运行时release(true)的既有修复尚未覆盖此路径。后续需同时覆盖多腿部分释放、控制器重试与资本释放阶段，不可仅替换helper就宣称金融恢复完成。
- 最终会话37101现已终止，完整strategy exit0/141.168s，包含最后清理失败断言；本版所有已派发验证终止，JSON/Markdown同步。完整根包、最终策略、针对性race、独立vet及版本一致/diff通过，不是全仓/严格MySQL/浏览器/真实Redis、构建或同提交发布门禁。未提交/推送/发布/部署；金融核账、上述租约释放接线及R01–R15仍开放。

## rc1013：共享意图UNKNOWN与只核账接管的生产接线（验证中，未提交）

- 临时overlay完整构造器/真实隔离SQLite红测exit1/1.644s：schema7已核实部分成交存在，但Margin共享意图UNKNOWN在策略恢复之前导致ConfigureIntentJournal直接退出，无法形成受管只核账运行时。不是仅组件fake回调。
- order以专用错误区分完整scope校验/加载后的待经济核账，LoadedIntentRecoveryRequired同时核验journalLoaded/绑定/待意图/独立gate，任意UNKNOWN或残缺加载不能通过。FundingCarry仅此类型继续策略恢复，设置不可逆executionRecoveryRequired，若没有严格只核账恢复错误则拒绝普通启动，避免启动生产者或写入初始空经济状态。
- 不settle/delete意图，不清除IntentRecoveryBlock，不释放claims；原严格VerifyRemainingReconciliation仍必须通过，资金、债务、实际钱包与持仓尚未完整核实。新增真实SQL共享意图构造器、owner校验、普通启动拒绝回归待终态；R01–R15、完整UNKNOWN经济恢复、同提交发布及盈利仍开放。未提交/推送/发布/部署。
- 最终完整构造器三轮race3.815s通过，直接SQL读回接管和未核实停止后Unknown=true/Settled=false，三类claims/策略证据保留，无金融RPC。order完整owner加载/2101条分页及腐坏scope三轮race2.952s，策略普通初始化hold/部分恢复三轮race2.105s通过。fixture持久化已核实策略记录与未解共享意图，不代表首次成交查询→SQL提交或跨库原子崩溃全链路。
- 最终完整根包29.206s/order11.349s组合exit0；独立vet根包/order/strategy exit0、前后端版本一致及diff通过。完整strategy87294仍运行，不继承rc1012为本版门禁；报告 `/private/tmp/quantmesh-shared-intent-rc1013.rH1fMV/`。本版未构建/完整Web/前端/严格MySQL/同提交发布或盈利验收。

## rc1012：终态部分补仓成交的耐久恢复（验证中，未提交）

- 实际Start红测exit1/1.128s：原请求0.401 BTC、已撤销成交0.2 BTC、基础币费用0.0005，原恢复拒绝保存成交净证据。新增schema7终态记录，精确CID/OrderID/原请求查询后按逐笔数量和费用核验净额；只对Canceled/Expired有正成交的终态分离净额守恒与全债务覆盖。非终态部分成交、缺失费用、外部归属丢失或持久化失败仍拒绝推进。
- 新schema7保存Verified仅指该记录成交/费用证据核实，不代表债务已覆盖；旧schema1–6的历史full-cover规则不弱化，也不能携带新TerminalStatus。原偿债source依净额上限继续拒绝全额债务，保留marginDebt/IntentInFlight/ExposureUnknown及只核账错误类型；不追加买单、还款、采用钱包余额或恢复交易。
- 回归包含已撤销/过期、费用缺失、owner丢失、save失败、二次Start不重放及旧schema拒绝，待终态。完整经济恢复、实际运行时初始化/账户核账、R01–R15、同提交发布及盈利仍开放；未提交/推送/发布/部署。
- 零净額二次Start红测exit1/1.144s实际丢失只核账错误分类，不是已证明金融重放；剩余数量为零但有已核实终态部分记录和未偿债务时，恢复并允许只核账管理，仍不允许交易。最终实际Start/利息消费/防篡改组合三轮race8.136s通过；实际完整构造器、隔离SQLite、三类claim保留及API只核账状态三轮race4.118s通过，追加部分/零净额两种证据，不执行金融RPC。第一轮真实成交查询恢复使用内存store，SQL构造器验证读取已核实schema7记录，不等于完整查询→SQL提交/崩溃全链路。
- 最终完整根包27.793s通过、独立vet根包/strategy exit0、版本一致及diff通过。首轮完整strategy140.916s失败且具体断言被日志截断；最终源码筛选失败信息会话99858仍运行，不能报告完整策略门禁通过。报告 `/private/tmp/quantmesh-partial-cover-rc1012.U4ZcJB/`；本版未构建/完整Web/前端/严格MySQL/同提交发布或盈利验收。
- 诊断会话99858完整strategy终态exit1/140.846s，定位恢复配置证明只接受当前schema导致schema6被拒；生产证明显式支持6/7，不改变schema1–5拒绝配置清理的旧要求。新增partial配置证明最初1.711s失败因fixture交易所名称为空、独立绑定不完整，补齐名称后实际Start/证明/collection dispatch三轮race2.909s通过。未偿债务/剩余资产返回ErrRecoveryConfigRequired，schema6携带新partial字段拒绝，不以partial净额为配置删除依据。
- 最终完整根包18685 exit0/27.210s，含schema兼容生产修复；最终独立vet根包/strategy exit0。完整strategy45875启动早于最后fixture名称修正且仍运行，不视为最终门禁通过；失败历史保留，未发布/实盘验收。
- 会话45875终态exit1/140.608s，仅复现已修正的fixture独立名称缺失；最终完整strategy会话7545已派发、包含最后修正，仍运行。报告同步，不能将最终完整策略门禁视为通过。
- 会话7545现已终止：最终完整strategy exit0/141.065s，本版验证全部结束；报告同步，先前失败记录保留。不继承该会话为rc1013门禁。

## rc1011：补仓费用原币/折价证据一致性（验证中，未提交）

- 实际closeReverse红测以Commission=0.002 BTC、BaseFeeQty=0.0004、CommissionQuote=20/Rate=50000矛盾证据仍执行还款，exit1/3.477s。更早100原币的夸大夹具同样失败3.145s，不作为最终现实量级红测。净补仓不能只用折价字段覆盖原币费用。
- 已读实际Binance Margin适配器及wrapper：Commission原币、BaseFeeQty基础币数量，现货适配器也保留原币并单独附折价证据。原converted_base_fee测试误填Commission=20折价额，已修正为0.0004原币及Quote=20。现在原币费用始终核对BaseFeeQty，折价已知时额外核验Quote/Rate与原币一致；不改生产adapter单位、不自动迁移矛盾历史证据。
- R05/R09费用与债务验收仍需生产接线/重启回归，完整UNKNOWN/部分成交恢复、R01–R15、同提交发布及盈利仍开放。未提交/推送/发布/部署/真实账户。
- 最终实际closeReverse完整净补仓表三轮race通过86.359s；实际Start重启净cover恢复三轮race通过2.034s，新增一致折价可保存/矛盾折价不得推进净数量，不追加买单或还款；实际Binance Margin成交查询HTTP夹具三轮race1.684s通过，证明原币Commission/BaseFeeQty生产单位。当前Margin producer不输出QuoteKnown，矛盾折价路径是fixture覆盖而非已发生实盘故障证据；历史verified记录共用相同验证函数，但未做完整历史账户迁移或真实DB重启演练。
- 独立go vet ./strategy exit0、版本一致3.111.0-rc1011及diff check通过；完整strategy会话22522仍运行，不继承rc1009为本版门禁。报告 `/private/tmp/quantmesh-cover-fee-rc1011.nSaIau/`。本版未构建、完整根包/Web/前端/严格MySQL、同提交发布或盈利验收。
- 会话22522终态exit0/140.800s，完整策略包通过；报告同步，仅证明rc1011最终策略源码。

## rc1010：排空期间归属丢失的优先级（验证中，未提交）

- rc1009生产接线仅在取得stopMu/排空之前检查归属，排空后会先进入PrepareShutdown金融撤单再检查租约；等待错误也直接typed为纯排空。新增同生产drain helper的ownershipGuard，在开始及失败/成功返回时读回真实ownershipLeases，已知丢失优先返回非retry错误，避免进入撤单阶段。
- 检查不是跨RPC原子金融fencing；不得清除UNKNOWN、重新获取租约token或盲目恢复金融动作。实际策略/执行器fixture覆盖取消等待和成功排空后归属变化，不是完整FundingCarry初始化或真实Redis故障注入。R01–R15及耐久经济核账/同提交发布/盈利继续开放，未提交/推送/发布/部署。
- 初始针对性三轮race3.324s通过；提取与生产共用fundingCarryStopDrainOwnershipGuard并实际读回runtimeOwnershipLease.lost atomic后，最终三轮race3.184s通过，含实际StopBot/StopAll Start/Enable/热更新拒绝及停止重试。原完整根包27.181s通过但早于最后提取，最终根包重跑仍运行。最终独立vet根包exit0、前后端版本一致及diff exit0；报告 `/private/tmp/quantmesh-drain-ownership-rc1010.ib9F38/`。本版未构建/全仓/严格MySQL/浏览器/同提交发布验收。
- 最终根包56807现已终止：完整无缓存根包26.871s/exit0，含最后共用guard接线，报告同步；所有本版验证会话均已结束。再次对照原R01–R15，当前改善R07/R09停止阶段归属核验，不替代完整经济恢复、R14同提交发布或R15盈利证据。

## rc1009：金融停止前纯排空重试（验证中，未提交）

- 实际FundingCarry运行时在stopOnce金融边界前调用drainFundingCarryStop，先BeginShutdown所有执行器，再实际策略QuiesceContext取消生产者、等待runDone和operationGate，最后DrainShutdown。没有撤单/平仓/还款或清除UNKNOWN；仅纯等待阶段产生typed retry错误。失败保留资金续期和观测工作，成功才进入原有缓存金融阶段。
- StopBot/StopAll独立stopDrainPending保留归属，接入共同stop_pending/Start/Enable/退出检查及热应用拒绝；不将纯等待失败写入rt金融UNKNOWN。原有金融UNKNOWN仍缓存，不把已有在途动作结果视为已核实。
- 实际策略/执行器与真实BotManager两条停止接线回归待终态；fixture未启动真实策略循环或提交金融动作，不代表完整FundingCarry初始化/成交核账。撤单UNKNOWN耐久恢复、订单流经济消费者、多主机fencing、R01–R15及同提交发布/盈利仍开放；未提交/推送/发布/部署。
- 首轮管理器fixture缺eventBus导致exit1/1.589s，并非有效生产红测；补齐fixture后三轮race3.202s通过。最后新增Start/热应用拒绝断言后，针对性根包三轮race4.701s通过，含既有租约待释放接线与金融UNKNOWN不重放；实际strategy Stop/Quiesce三轮race2.026s通过。Quiesce回归检查未退出生产者、持有operationGate取消及重试无stopAttempted/stopCompleted变化。
- 根包/strategy vet exit0，版本一致3.111.0-rc1009与diff check通过；首轮完整根包27.586s通过但启动早于最后断言。最终完整根包10765/strategy51533仍运行，不视为已通过。报告 `/private/tmp/quantmesh-funding-drain-rc1009.6mmzMa/`。本版未构建，不继承旧embed或前端完整测试为本版门禁。
- 最终完整根包10765现已终止：exit0/27.428s，包含最后启动/热应用断言；完整strategy51533仍运行，报告同步根包终态。停止排空待处理标记随原控制器保留到完整停止核实/注销，不代表在途金融动作已核实，也不是完整阶段状态机。
- 完整strategy51533现已终止：exit0/138.772s，报告同步。该会话验证rc1009策略改动，不是rc1010完整根包/运行时验证。

## rc1008：实际策略平仓前取消重试（验证中，未提交）

- 实际FundingCarryStrategy.StopContext红测exit1/1.300s：以stopMu屏障等待调用取得operationGate后取消，未进入金融动作，新context却返回旧context canceled。不是假停止回调或金融接口fixture的推测。
- 只将平仓前context错误直接返回，不写stopAttempted/stopErr；UNKNOWN仍永久保留，金融边界前设置stopAttempted后仍缓存失败。新增UNKNOWN清除内存标记也不能重放的回归。
- 外层funding_carry_runtime的sync.Once准备错误缓存、准备失败后StopAll/关闭订单流、耐久金融恢复及R01–R15全范围仍开放。本版未提交/推送/发布/部署，无真实账户或盈利验收；针对性验证待终态，不继承rc1007测试或构建为本版验收。
- 两项实际策略回归十轮race终态exit0/2.573s；独立go vet ./strategy exit0、前后端版本一致3.111.0-rc1008及diff check exit0。完整策略包会话42533仍运行，已只读确认strategy.test存活，不能报告完整门禁通过；报告 `/private/tmp/quantmesh-funding-stop-rc1008.GPMQtG/`。本版未构建，不继承旧embed来源证明。
- 会话42533现已终止：完整strategy包通过138.601s，组合exit0；报告同步终态。进一步确认PrepareShutdown含CancelOwnedShutdownOrders金融撤单与成交查询，不是纯准备：新增成交/已UNKNOWN均保留UNKNOWN直到耐久经济消费者核账。因此后续修复必须拆分纯quiesce/drain与撤单核账边界，不能将全部准备失败标可重试或仅清除runtime错误。当前FundingCarry没有在该runtime内启动订单流的接线证据，策略OnOrderUpdate还是no-op，不能把“保留流不关闭”冒充成交消费者恢复。
- 实际order停止组件三轮race终态exit0/1.975s，覆盖CancellationRequiresValidTerminalEvidence、ClosePermissionsComposeAcrossIndependentExecutors及DrainsOpeningAndClosingSubmissions；新增成交、UNKNOWN等九种证据不足均维持不确定性。此结果支持后续阶段划分，不代表完整FundingCarry生产核账接线或外层停止恢复已修复。

## rc1007：租约待释放的状态与启动/热应用接线（本地验证与构建通过，未提交）

- 原实际StopBot/StopAll红测exit1/根包1.899s：租约释放失败后StartBot返回成功，热应用实际Applied=[owner]/callback一次，实际Web详情/列表Running=true且JSON没有stop_pending。不是仅检查假provider或提交标题。
- stopOwnershipPending独立atomic区别stopPersistencePending；共同stopTransitionPending检查接入新启动/重复启动、Enable、进程退出seal。热应用明确bot_stop_ownership_pending，不执行运行参数回调。StopBot/StopAll只在明确typed金融已完成/租约未释放错误时置位；不设置stopIntent或跳过callback，仍需原租约重试核验、主库disabled及退休记录。
- API详情/列表同一次pending读回产生Running=false和stop_pending=true。React详情/列表待处理badge及独立停止重试不调用额外closePositions；总览按exchange/symbol/market查找所有匹配Bot，待处理时只进入详情/列表，不猜测唯一归属或追加平仓；状态汇总优先已知Bot运行标记，不把同名spot/futures混为同一市场。中文简/繁/英文i18n，其余语言英文回退；待处理筛选独立于已停止。
- 初始针对性三轮race通过5.138s；最后新增Enable拒绝、List StopPending及UI实际Bot API重试最初StopAll的断言后，最终针对性三轮通过3.920s，直接主库disabled读回且金融callback一次、Unlock两次。空金融回调/内存租约/隔离SQLite不是完整运行时金融核账、真实Redis集群或实盘证明。
- 前端中间类型检查因本地SymbolStatus缺market_type失败，已补齐类型及批量/回退状态来源；最终typecheck exit0，51文件/304项完整Vitest通过，生命周期/精确市场四项单测通过。未运行浏览器组件交互/截图，不把状态选择测试当完整渲染验收。
- 使用Go+React embed技能沿现有Makefile先Vite构建、来源/版本摘要核验与同步，再Go临时二进制构建，exit0；metadata version=3.111.0-rc1007/source_digest=2b769cbdf0043c7be94d4803a8a97c33e9252b4227a353bee57a7084bd2571f8/216files。二进制SHA256=4f92f4835d552950172324abce06c048e59f48233d207d0973a9cc9c0306712f，buildinfo HEAD=5c048825、vcs.modified=true；不是干净同提交发布证明。旧embed产物由已有脚本保留在构建输出指明的backup路径，未启动服务/部署。
- 首轮完整根包35.159s/Web97.822s通过、config/lock命中缓存，组合exit0；这一轮启动早于最后新增Bot API重试断言，不能报告最终完整测试集门禁。最终无缓存根包/config/lock另行派发，当前仍运行；vet根包/Web/config及diff exit0。报告 `/private/tmp/quantmesh-stop-pending-rc1007.wxqPzg/`。
- 最终会话92753已终止：无缓存完整根包30.862s/config3.553s/lock2.258s通过，组合exit0，包含最后新增的Bot API重试/主库断言；最终vet根包/Web/config、diff及embed verify会话67048 exit0。JSON/Markdown已同步，所有本版验证会话均已结束，不是全仓/严格MySQL/浏览器或同提交发布验收。
- 完整R01–R15仍开放：金融UNKNOWN耐久恢复、崩溃后阶段核验、独立策略错误缓存、多主机fencing、全仓/严格MySQL、浏览器及同提交发布/盈利证据。stop_pending不是已平仓或盈利证明；当前字段也不是整个执行生命周期状态机。未提交/推送/main/tag/发布/部署或访问真实账户。

## rc1006：金融停机成功后的租约释放重试（本地验证完成，未提交）

- 原辅助函数红测exit1/2.764s：首次未执行删除的解锁RPC失败后误报released=true、停止续租、sync.Once永久缓存错误，恢复provider后仍只调用一次Unlock，peer不能接管。普通symbol_manager Stop还把释放与金融错误一起缓存，控制器重试无法抵达有效释放。
- 普通运行时使用newStandardRuntimeStop共用分阶段组件，金融阶段执行一次且UNKNOWN不重放；只有该阶段成功、运行租约未丢失、rt没有金融未核实标记时，租约释放失败才产生明确typed retry错误。StopBot/StopAll对该类型继续保留归属、开仓封锁和未完成journal，但不将纯释放错误写成金融UNKNOWN；重试仍通过原callback执行释放，确认后继续主库disabled/退休记录。
- 已管理运行时release(true)失败继续续期，成功才停止；续期与Unlock共用mutex，stopRenew的等待在释放mutex之后，onLost与released检查串行化，释放前后丢失所有权均拒绝报告核实完成。默认Release用于初始化回滚仍停止续期，避免无管理器接管时无限孤儿续租；没有重新获取token或删除peer-owned租约。
- 中间针对性3.879s使用空reason，随后发现会掩盖控制器将释放错误写成金融UNKNOWN，已替换为同生产newStandardRuntimeStop/实际rt理由及真实BotManager，不作为最终接线证明。后续中间5.635s未含最后StopAll/丢失租约/启动回滚断言，且同命令lock包过滤后无测试，不报告lock验证通过。
- 最终针对性三轮race通过6.313s，含实际StopBot主库读回/intent退休、StopAll、并发分阶段调用、UNKNOWN不重放、租约丢失不标可重试、续期保留及初始化回滚不孤儿续期。最终完整根包39.161s/锁包2.300s、组合exit0，vet根包/Web/config和diff exit0。空金融回调与内存锁fixture不是完整普通运行时初始化/实际金融核账，锁包包含Redis协议fixture而非真实Redis集群故障注入。本版未完整Web/config、严格MySQL、Windows、同提交发布验证。
- 尚未闭合：金融UNKNOWN及进程崩溃后金融阶段耐久接管；funding_carry等独立策略停机中原sync.Once错误缓存；租约RPC响应丢失但服务端已删锁的完整核验；多主机金融fencing。main_adapters_bot仍仅按stopPersistencePending判断Running，租约待释放时金融已停但没有该标记，Web展示及Start/热应用需继续接线，不能把当前控制器归属视为仍运行。
- 版本前后端同步3.111.0-rc1006，报告 `/private/tmp/quantmesh-lease-release-rc1006.R0wtWl/`。未提交/推送/main/tag/发布/部署、真实账户/下单或盈利验收，完整R01–R15继续开放。

## rc1005：共享回退状态文件并发与原子发布（修复后验证中，未提交）

- 修改前24个实际管理器并发Enable红测exit1/2.357s，出现半截JSON解析错误，最终仅6条而非25条记录，已确认启用也丢失，未知恢复字段被固定struct重编码丢弃。原实现无共享文件级锁且直接os.WriteFile覆写，单Bot guard不能保护其他Bot。
- 全文件读改写持同路径持久文件锁，固定顺序生命周期->Bot journal->共享状态文件；RawMessage保留其他及当前Bot未知字段，CreateTemp私有0600、Write/Sync/Close、Rename并Sync目录。不可读/损坏/null顶层继续拒绝且保留原件。发布后的目录Sync失败回报错误，不冒充耐久确认；所有I/O阶段崩溃及Windows运行尚未覆盖。
- 实际24管理器并发和三个真实子进程/24启用、同owner未知字段保留与权限、损坏保留/停止重试均待最终终态。原热更新取消用例十轮复验首次启动时与源码迁移重叠，build清单未包含新文件，报saveBotStateToFile undefined；这不是有效原样复验或生产缺陷，将在最终静止源码重跑。未提交/推送/发布/部署或访问真实账户，完整R01–R15及盈利验收仍开放。
- 最终针对性三轮race通过14.378s/exit0，覆盖上述真实管理器及子进程回归；原热取消十轮通过5.883s，vet根包/Web/config及diff exit0，未因果解释rc1004完整门禁的200ms失败。最终完整根包会话92516仍运行，本版未重跑完整Web/config，不继承rc1004为本版门禁。JSON/Markdown `/private/tmp/quantmesh-state-file-rc1005.bxvXxK/`。
- 最终完整根包会话92516 exit0/42.909s，已终止，报告同步。不是全仓、严格MySQL、同提交发布或真实盈利验收；未对所有语法有效但已知字段类型错误的外部状态文件、全部I/O故障/崩溃点或Windows运行进行验收。

## rc1004：共享停止记录的控制面串行化（实现后验证中，未提交）

- 各管理器生命周期锁之后、读取停止意图之前取得同Bot持久文件锁，覆盖实际StopBot恢复/停机/主库写入/退休、Enable、新启动及正常StopAll；pending StopAll委托StopBot helper，不重复取锁。锁文件不删除避免inode ABA，非阻塞OS锁轮询等待最多30秒，Start沿调用context取消，持有阶段不缩短金融回调。
- Unix使用flock，Windows使用已有x/sys LockFileEx，其他平台明确拒绝而不是空锁；Windows尚未执行目标环境门禁。同数据路径串行化不是多主机金融租约，也不是整个账户只允许一个运行时的分布式所有权证明。未完成停止金融核验/恢复及完整R01–R15仍开放。
- 新永久回归保留两个真实管理器/隔离SQLite及延迟原inode读取，允许正确代码先阻塞后继启用，最终仍必须读回enabled=true；不弱化旧红测的最终断言。另测取消等待、不同Bot独立、实际子进程持锁互斥及os.Exit后的OS释放。验证尚待终态，未提交/推送/发布/部署/真实账户或盈利验收。
- 最终针对性三轮race通过15.601s/exit0，包含实际StartBot在peer持锁时取消且不进入validator/金融初始化，并确认取消未留生命周期锁。Windows辅助函数单独编译及最终vet根包/Web/config、diff exit0，不代表Windows运行验证。完整根包/Web/config会话41409仍运行，启动早于最后新增StartBot测试；生产源码没有随后改变，不报告完整最终通过。报告 `/private/tmp/quantmesh-stop-guard-rc1004.EteyMC/`。不同Bot共享回退状态文件的原子并发更新仍需另行闭合。
- 会话41409最终exit1：根包FAIL/132.374s，TestActualHotAdapterCancellationWhileLifecycleLocked的200ms返回观察失败，calls=0/最终cancelled且未应用；Web442.261s/config5.346s通过。已保留失败，不报告rc1004完整通过，不把最终cancelled等同已证明及时返回。

## 停止记录恢复覆盖新启用的并发缺口（rc1003实际管理器红测，rc1004修复验证中）

- 两个真实管理器共享隔离SQLite/停止记录，用临时FIFO inode屏障延迟第一管理器实际os.ReadFile的原Complete字节；第二管理器真实StopBot完成disabled并退休记录，再真实EnableBot提交enabled=true，直接主库读回确认。释放原读取后，第一管理器真实recoverDurableStop再次写disabled，主库enabled变false，覆盖更晚的显式启用。
- 独立overlay race红测exit1/根包2.225s，夹具/JSON/Markdown `/private/tmp/quantmesh-stop-peer-race-audit.7xNSqj/`。未mock管理器/存储方法；这是逻辑并发错误，不是Go内存race报告、跨主机证明或真实金融操作。读记录后缺共同串行化/权威条件更新，退休校验在状态写入之后；仅补写前重读仍有TOCTOU，不能冒充闭环。需联合恢复/启用/退休并继续验证金融fencing。

## rc1003：定时人工暂停回归等待真实发布状态（本地验证完成，未提交）

- rc1002最终根包FAIL/108.096s/exit1，TestTimedManualPauseExpiryPreservesCoordinatedRiskHold固定sleep1.1s后manual=true/risk=true。原样十轮复验通过15.183s，未把一次全包失败断言为稳定生产缺陷。源码计时器在后台协程启动后创建，manual gate与配置不是同时发布，有调度/观察窗口，尚未因果证明此前失败原因。
- 两项定时人工暂停测试改为三秒期限内共同等manual gate及配置PauseOpening解除，读配置持configMu；保留独立risk gate/coordinator持有及最终显式解除后开仓门闩清空的所有原要求，不改生产风控。按构建/测试技能保留完整门禁，不以增加sleep隐藏断言，不宣称精确一秒延迟或提速。
- 最终两项各十轮race通过24.960s、完整根包race通过71.394s/exit0，vet根包/Web/config及diff exit0。本版版本前后端/记录同步3.111.0-rc1003，JSON/Markdown `/private/tmp/quantmesh-pause-conditions-rc1003.2E6j71/`。不继承rc1001完整Web结果为本版重跑；未全仓/严格MySQL/浏览器/同提交发布、提交/推送/部署或真实账户/盈利验收。

## rc1002：子进程突然退出后的停止记录读回（针对性三轮通过，完整根包运行中，未提交）

- 新永久测试启动当前race测试二进制的独立子进程，限定隔离临时数据目录和fixture phase；未完成场景在真实StopBot的StopWithError回调内os.Exit(23)，完成场景在真实StopBot回调成功、关闭隔离主库造成最终状态写入失败后os.Exit(23)。两处均不执行test cleanup/defer，不是只在同一进程重建管理器。
- 父进程检查精确退出码/30秒期限，重新打开同一临时SQLite及停止意图，直接核对phase、记录0600权限、旧enabled不能覆盖意图和普通Enable不能清意图。未完成态StopBot不猜测恢复成功；完成态StopBot无运行时可恢复主库disabled并退休意图。金融回调为空夹具，未接交易所、未下单。
- 子进程退出及既有重建/未完成门禁三轮race最终exit0/根包12.375s，包含六次预设phase退出；vet根包/Web/config及diff检查exit0，版本前后端一致3.111.0-rc1002。完整本版根包race仍运行；仅新增Go测试/版本，Web/config生产代码未改，本轮不重新将rc1001完整Web结果报告为rc1002门禁。本版JSON/Markdown `/private/tmp/quantmesh-stop-process-rc1002.5ACKtT/`。
- OS进程突然退出是os.Exit且发生在两个预设点，不是任意I/O时点SIGKILL/断电；未完成态金融恢复、多主机共享停止意图/金融fencing、跨进程并发退休/启用事务及Windows门禁仍开放。未提交/推送、main/tag、发布/部署、真实账户或实盘盈利验收，R01–R15保留原范围。
- 首轮完整根包FAIL/44.790s/exit1，TestBotManagerKeepsSpecializedRuntimeWhenSafeStopFails及TestBotRemovalDoesNotPersistWhenSafeStopFails使用默认数据路径，被已有停止意图拦截，未到达原预期失败回调。按构建/测试资源隔离规则把两夹具改为t.TempDir，保留原错误原因/归属/不持久删除断言，不清理默认目录；联合子进程/重建及原失败用例三轮race通过52.548s/exit0。继续将成功重试夹具也隔离，避免写默认停止状态文件；该最后修改后另派发最终完整根包和vet，运行中。之前已派发两轮完整根包亦等待终态，不因观察等待重启，不冒充包含最后夹具修改或宣称提速。
- 隔离前两夹具后的完整根包两轮最终通过276.311s/exit0，会话30425已结束，但不包含随后成功重试夹具的隔离修改。最终vet/diff通过exit0；包含全部三个夹具隔离的最终完整根包会话37629仍运行，不报告最终完整门禁通过。
- rc1002会话37629最终FAIL/108.096s/exit1，定时人工暂停固定sleep断言未过；因此本版不报告最终完整验证通过。后续rc1003修正测试实际状态等待并单独完整根包验证，不能回写为rc1002通过。

## rc1001：持久停止意图与重建后的停止记录恢复（针对性通过，最终完整门禁运行中，未提交）

- StopBot在金融回调之前写独立停止意图，文件按BotID SHA256命名避免路径注入；临时文件0600/目录0700、文件Sync、初次hard-link独占发布，阶段更新原子Rename并Sync目录。完整记录包含随机operation和停止状态；核实回调完成才标Complete。完成不等于全账户平仓，close_on_stop=false仍遵循原策略。主库disabled持久化成功后才退休记录；普通Enable不清除未处理意图，启动权威读取先检查意图，损坏/不可读也拒绝未知状态。
- 重建管理器且没有runtime时StopBot仅对Complete记录恢复权威disabled/退休记录，不重放金融回调；未完成记录返回明确需金融核验，不冒充已恢复。退出seal识别待处理意图，main退出不重复已由管理器完成的停机回调。初始意图写失败返回错误并限制新增开仓，不伪装已经停止；遗留void Stop存在未核实标记时不记Complete。
- 原退出/重建overlay及新永久恢复/未完成拒绝、既有停止重试/旧适配器三轮race通过10.634s；此结果在最后“写失败开仓限制/void未核实拒绝”补充之前，不冒充最终源码验证。首轮新增记录后既有更换回退目标的重试测试失败exit1/根包2.537s，原因记录路径跟随目标变化；绑定每次stop operation的记录路径保留原回归要求，未放宽断言。
- 最终补充后原overlay/新永久/既有三轮race与完整根包/Web/config以及vet均已派发，仍待终态。JSON/Markdown报告 `/private/tmp/quantmesh-stop-journal-rc1001.v9axAR/`。未实现所有journal阶段I/O错误故障注入、OS进程kill/power loss、跨进程金融fencing/并发启用退休事务或Windows文件系统门禁。未完成阶段仍须完整金融恢复流程，不能把停止意图封锁当恢复能力闭环。前后端/文档同步3.111.0-rc1001，未提交/推送、发布/部署、真实账户/下单或实盘盈利验收，R01–R15仍开放。
- 最终补充源码针对性三轮race通过7.044s/exit0，vet及diff通过exit0；完整根包race通过33.881s，Web/config组合仍运行。本地停止日志只覆盖使用同一数据路径的管理器重建，不是多主机共享停止意图或跨进程金融租约证明，不能据此宣称分布式恢复安全。
- rc1001完整组合最终通过：根包33.881s/Web222.788s/config4.291s，exit0，会话50158已终止，报告已更新。不是随后rc1002新增子进程测试或全仓/严格MySQL/发布验收证据。

## 停止意图退出与重建管理器缺口（rc1000隔离红测已复现，尚未修复）

- 实际StopBot在文件写入失败后保留归属，随后真实sealProcessRuntimes排空admission仍返回nil，没有识别stopPersistencePending；源码main.go退出末尾仍直接遍历rt.Stop，存在再次调用已完成停机的路径。后者为调用链证据，未把它声称为本次独立测试复现结果。
- 隔离主库先Enable成功，关闭SQLite后实际StopBot金融停止成功、主库持久化失败；重新打开同一临时库并构造全新管理器，实际IsBotEnabledInDB读回旧enabled=true。该证据是管理器重建/权威启动状态错误，不是OS崩溃注入或真实StartBot交易启动。
- 两项实际接线overlay race红测exit1/根包5.197s，JSON/Markdown/夹具 `/private/tmp/quantmesh-pending-stop-shutdown-audit.ospTap/`。没有修改生产代码、访问真实账户/下单或写生产数据。必须继续闭合持久停止意图/原子并发写、主库与停止日志协调、明确恢复/启用语义及退出避免重复停机；不能用新增内存封锁或只写低优先级fallback冒充崩溃恢复闭环。

## rc1000：旧交易对停止入口统一管理器核验（实际接线三轮race通过，完整门禁运行中，未提交）

- 真实symbolManagerWebAdapter.StopSymbol独立overlay红测exit1/根包3.373s：StopWithError回UNKNOWN仍返回nil，且直接Stop与Remove内管理器停止两次执行金融回调。仅临时SQLite/无外部操作的运行时，配置主库写入成功，明确越过SetSymbolEnabled到达真实停止接线；归属因原管理器核验拒绝仍保留，未将此项声称为新红测失败。
- 按精确交易所/交易对/市场取得BotRuntime，继续先执行既有配置停用，随后只调用管理器StopBot(botID)，不直接rt.Stop或void Remove。失败返回调用方并发固定停止未核实事件；只有管理器核验及停止状态持久化均成功才发原完成事件。未改宽泛Remove API或所有外部调用。
- 新永久实际适配器测试先注入UNKNOWN，要求返回错误、仅一次回调、归属保留；恢复核验器后从同一公开入口再次停止，要求仅一个新回调、归属释放且隔离主库GetBotState读回disabled。原overlay和新测试及既有管理器/适配器三轮race正在运行，最终根包/Web/config完整race另派发，不能继承rc999通过结果。本版vet及diff检查exit0。
- 前后端版本/变更记录/产品概览同步3.111.0-rc1000，报告 `/private/tmp/quantmesh-legacy-stop-rc1000.bA8Fkb/`；不宣称Web浏览器验收、真实订单终态或完整恢复。配置enabled=false写入与金融停止不是共同事务、启用与并发请求、跨进程/崩溃恢复、文件原子写仍开放。未提交/推送、main/tag、发布/部署、真实账户/下单或实盘盈利验收。
- 后续针对性终态：原overlay、新永久实际适配器及既有管理器/适配器三轮race通过，根包13.951s/exit0，会话60179已结束。进程只读核对确认完整根包/Web测试仍在执行，没有因观察等待重启；最终组合门禁尚未到终态。
- rc1000最终组合终态：根包66.403s/Web396.815s/config13.150s通过，exit0，会话57945已结束，报告已同步。该证明不覆盖随后rc1001持久停止记录源码。

## rc999：停止持久化失败保留归属并可重试（根包/Web/config完整race通过，未提交）

- 以先前真实StopBot红测为依据修复：stopBotWithReason在金融停止成功后保存进程内stopIntent及原子pending状态，在权威停止记录写入成功之前不注销提供者或移除runtime。失败透传错误；重试只持久化，不重新执行已经成功的StopWithError/Stop回调。主库存在时失败不再回退文件冒充主库成功，无主库保留既有文件回退。
- StopAll遇到同一pending stopIntent时走上述持久化重试，保留原停止原因，不执行重复金融停机。StartBot在重复启动快速路径和生命周期内复核pending，EnableBot与停止共用生命周期锁并拒绝pending；热应用明确Failed，实际Web适配器Running=false，不把受管归属冒充仍运行。批量删除沿原StopBotsAndPersistRemoval调用返回错误，不继续删除配置。
- 新永久测试覆盖StopBot/StopAll两种恢复重试、真实管理器启用/启动/热应用拒绝、状态文件直接存在及启动读回disabled、完成后释放归属、金融停止回调只一次；关闭隔离主库失败场景要求返回错误且保留归属、不生成误导性fallback。新测试及既有Enable/停止热应用生命周期共三轮race通过，根包6.122s。最终根包/Web/config完整race与vet另行运行中，不继承rc998的完整Web结果。
- 局限：stopIntent是进程内状态，主库写入失败后进程崩溃仍可能丢失该意图，不能宣称跨进程停止恢复闭合；文件并发写/原子替换、退出排空、遗留void Stop的终态证明、权威核账及完整R01–R15仍需继续验证。已持久化disabled之后管理员可显式Enable；没有自动解除UNKNOWN风险。版本前后端及文档同步3.111.0-rc999，未提交/推送、main/tag、发布/部署、真实账户/下单或实盘盈利验收。
- 本版vet根包/Web/config及diff检查最终exit0；完整根包race通过56.970s，组合命令中的Web/config尚待终态，不报告全套通过。JSON/Markdown报告 `/private/tmp/quantmesh-stop-persistence-rc999.mDInU9/`，原overlay红测也已派发修复后复验，等待终态。
- 原overlay复验最终exit0/根包3.611s：同一修复前失败的三项要求全部通过，确认实际StopBot生产接线改善，不以新写测试替代原红测。组合Web/config门禁仍需确认终态。
- 后续终态：根包56.970s/Web296.411s/config4.125s均通过，完整组合命令exit0，会话48805已结束。报告已同步，严格MySQL/全仓/前端/同提交发布未验证。继续追查生产接线发现旧symbolManagerWebAdapter.StopSymbol在SetSymbolEnabled后直接rt.Stop，再通过无返回值Remove调用管理器停止，可能重复执行并吞掉失败；独立实际适配器故障注入正在验证，不把BotManager修复冒充此入口闭环。

## 真实停止持久化与重试缺口（rc998只读源码与隔离红测，尚未修复）

- 实际stopBotWithReason在金融停止回调成功后，先移除runtimes归属，再调用没有返回值的saveBotStateToDB；文件/主库失败只记录日志，调用方仍nil，Web postBotStop因此可能回200。第二次停止因没有runtime直接nil，无法重试停止记录。
- overlay隔离实际管理器：StopWithError无外部操作且成功；回退目标先为临时目录制造失败，再切换有效临时文件重试。最终race红测exit1/根包1.452s，独立复现写失败仍成功、归属提前释放、重试成功但停止文件未生成三项。不将IsBotEnabled默认值当作持久化证明，直接检查文件存在。临时测试/overlay及JSON/Markdown报告 `/private/tmp/quantmesh-stop-persistence-audit.mxJxIJ/`，未修改生产停止实现。
- 下一修复必须区分金融停机完成与停止记录持久化待重试：传播失败、保留启动互斥/金融归属、允许持久化恢复且不重复金融停机，并协调批量删除/StopAll/热应用。仅在删除归属后加一个error返回不足以闭环；主库失败但文件成功的权威读取、崩溃/跨进程恢复仍需验证。不宣称完整恢复或实盘盈利验收。

## rc998：撤单生产接线测试隔离外网（最终Web完整race通过，未提交）

- 原按需创建Binance测试使用伪造凭据调用真实外网，且仅排除“交易所不存在”文本，HTTP500也可通过。修改测试HTTP边界，保留实际handler、配置创建SDK及服务时间/交易对初始化，未知端点不转送外网；要求DELETE中的symbol/orderId正确、恰好一次撤单及HTTP200。SDK身份参数位于表单体，初始仅检查URL的夹具三轮失败，随后补齐表单解析且没有放宽断言。
- 最终目标测试三轮race明确通过，Web35.310s；更改前完整Web曾通过221.857s，故不能将rc997的超时归因于该外网测试，也不宣称提速比例。夹具修改期间启动的完整Web仍FAIL/Web605.298s，十分钟超时堆栈涉及TestPasswordManagerInstallRecoveryAndSecurityStates及RecoverPasswordWithCode；该运行不作为最终稳定源码的完整验证，原因待独立定位。
- 本轮仅改善测试可靠性，没有修复真实撤单终态、停止持久化或完整金融恢复。替换http.DefaultTransport是全局测试状态，恢复使用t.Cleanup；完整race及全局状态隔离仍需验证。前后端版本及变更记录同步3.111.0-rc998；未提交/推送、main/tag、发布/部署、严格MySQL、真实账户或实盘盈利验收。
- 六小时回查后补齐rc998文档，最终vet根包/Web/config及diff检查exit0。原命令在沙箱中先因已有Go缓存权限setup失败，获许可原样重跑后进入测试，不把权限错误当源码缺陷。独立密码恢复测试三轮race通过/Web160.818s/exit0，没有复现永久阻塞；一次进程采样仅见等待线程，不能据此断定死锁或超时根因。最终稳定rc998完整Web race已另行派发，保留十分钟门禁和测试名/超时诊断，尚待终态；不以独立测试通过替代完整验证。
- 后续实际终态：最终稳定rc998完整Web race package pass/266.903s，pipefail命令exit0，会话60660已结束。该运行包括最终DELETE表单夹具，没有再次复现此前超时，不宣称已查明根因或提速。报告 `/private/tmp/quantmesh-web-test-isolation-rc998.QqNv8v/`；本轮没有全仓、严格MySQL、前端或发布产物验证，完整R01–R15继续开放。

## rc997：启停回退文件损坏时保留原恢复证据（针对性验证完成，Web全包未通过，未提交）

- 上轮rc996为有效进展。本轮实际EnableBot/文件回退红测exit1、根包1.759s：损坏JSON被忽略并覆盖，null文档因nil map赋值panic。数组场景在该次红测因前一panic未运行，不将其声称为独立修复前证据。仅隔离文件与无主库管理器夹具，无交易初始化或生产数据操作。
- saveBotStateToFile在写入前核对os.ReadFile和json.Unmarshal；非文件不存在的读取错误、解析失败以及null map均返回错误，不覆写原文件。确实不存在的文件仍允许初始化，正常合法对象沿用原更新逻辑。EnableBot沿rc996透传此失败给Web受理门禁，停止路径仍仅日志记录失败，不能宣称停止已可靠持久化。
- 两项新永久测试覆盖损坏JSON/null/数组返回失败且原字节保留、正常启用新Bot不丢其他Bot停止记录；联合实际Enable、既有文件启停读取三轮race通过/根包6.279s。最终vet与diff均exit0、Ruby交易门禁9runs58assertions零失败/错误/跳过。未继承rc996验证。
- 首轮完整根包race出现TestRuntimeExposureSharedGridStrategyAndHotLimits失败：观察到PendingQuantity已0但IsOpeningPaused仍true，根包144.473s/exit1。源码order.CancelOwnedOpeningOrders先ObserveOrder释放额度再Unblock，cancelOpeningsForExposureRisk随后清除pending标记并reconcile移除限额hold，故两条件并非同时发布。测试原来仅等待PendingQuantity，再立即断言暂停解除；改为同一原3秒期限共同等待两条件，保留最终归零且不暂停及新限额内重新下单要求，未改生产撤单/风控逻辑。十轮针对性复验通过/根包15.773s，完整根包重跑通过/50.027s。
- 原Web全包最终FAIL/Web609.550s，config通过/13.839s，组合命令exit1。过滤输出含超时堆栈的HTTP2连接协程，但未保留panic头/正在执行的测试名，不能凭该片段确定网络故障、死锁或代码回归，也不能报告全套已通过。已确认旧进程终止，未因观察超时重启；后续需有完整测试名/超时诊断的全包证据。直接相关Web受理/恢复/基线保护回归另行运行exit0/Web57.817s，不替代失败的全包结论；JSON/Markdown报告 `/private/tmp/quantmesh-state-integrity-rc997.Xxqejg/`。本轮所有已派发测试均已终止，无遗留测试句柄。
- 原范围仍开放：文件读取/修改/写入不是跨Bot或跨进程共同事务，未添加原子替换/fsync；未知字段兼容、停止持久化失败回执、任务取消/排空与完整金融恢复仍待闭合。本轮不重跑前端测试或构建本版二进制；版本3.111.0-rc997、未提交/推送、main/tag、发布/部署、严格MySQL、浏览器E2E或真实账户操作，不宣称实盘/盈利验收。

## rc996：Web启动受理与真实启用持久化（本地针对性验证完成，未提交）

- 上轮rc995为有效进展，本轮检查postBotStart受理边界。真实handler三场景独立红测exit1/Web2.151s：EnableBot失败仍HTTP202且派发StartBot；Enable回调替换全局provider后，任务被新provider接收；已取消请求仍启用。仅隔离配置与provider夹具，没有调用真实交易初始化。
- 请求内固定provider用于GetBot/EnableBot及异步StartBot；启用前检查请求取消，Enable错误回503/bot_start_enable_failed/start_accepted=false，不继续派发；Enable后再次检查取消，回408且不派发，不宣称已经持久化的enable被撤回。已受理任务仍用独立Background，不简单绑定Request.Context使202返回后自动取消；重复运行Bot仍200幂等。最后检查与派发之间没有跨线程原子取消栅栏，不能冒充可撤销的启动任务。
- 继续沿实际适配器发现EnableBot原来吞掉saveBotStateToDB/file错误并始终返回nil，若仅修改handler便无法覆盖生产失败。实际管理器红测exit1/根包2.685s：隔离文件目标为目录，写失败后EnableBot仍报告成功。真实EnableBot改为有主库时检查SetBotState错误并直接返回，不用写enabled回退文件冒充主库成功；无主库保留已有文件回退并返回失败。未修改停止状态最佳努力回退逻辑，不声称停止持久化问题已闭合。
- Web六项回归覆盖失败不派发/固定provider/启用前取消/启用期间取消/正常受理后HTTP结束仍可运行/已运行幂等。最初三轮race通过/Web6.446s后，正向后台任务测试增加gate，让ctx.Err读取确定发生在HTTP上下文取消之后；完整门禁以最终测试为准。根包四项实际Enable回归覆盖文件失败、正常文件读回、主库成功读回、关闭隔离主库后失败且不生成误导性文件，三轮race通过/根包4.881s。
- 最终受影响包完整race终态exit0：根包39.591s/Web237.050s/config6.316s；最终vet及diff均exit0，前端typecheck/实际3文件46项回执守卫1.60s通过，Ruby交易门禁9runs58assertions零失败/错误/跳过。中途handler-only完整race根包50.707s/Web295.918s/config7.902s也通过，但不冒充包含真实Enable修复的最终证明。两轮因实现更新而先后派发，均确认终态；较长运行期间只读核对进程活跃，没有因观察超时重启，也没有将耗时当源码缺陷。JSON/Markdown报告 `/private/tmp/quantmesh-start-admission-rc996.V0CWmu/`。
- 限制：Enable/Start不是共同事务或同一生命周期操作，启用后取消可能保留enabled状态；Web后台任务ID/查询/取消/退出排空仍待完成，202不代表交易已运行。停止持久化错误、文件回退并发写/损坏记录保护、完整恢复及R01–R15仍开放。源码3.111.0-rc996，未重建本版二进制、未提交/推送或main/tag、发布/部署、严格MySQL/浏览器E2E/真实账户/生产数据操作，不宣称实盘或盈利验收。

## rc995：实际启动生命周期等待可取消（本地针对性验证完成，未提交）

- 上轮rc994为有效进展，本轮检查实际启动与恢复配置门禁的生产调用链。新实际StartBot回归先失败exit1/根包1.446s：持生命周期锁时30ms期限没有使启动返回，释放锁后仍进入配置验证；验证回调取消后返回nil，也仍进入startBotUnderTransition，得到storage_unavailable而非context.Canceled。只使用实际管理器、隔离配置及无存储夹具，不触及交易所初始化。初始第二夹具使用nil事件总线导致无关panic，补齐事件总线后再完成上述独立红测；不把夹具panic当作生产缺陷。
- startBotWithValidation复用WithBotStrategyConfigurationContext同一生命周期锁/退出排空机制；可取消调用方等待用既有TryLock/timer，不创建超时后可能获得锁的遗留等待协程。取得锁后复核受管实例，重复启动仍幂等；配置验证前后检查ctx.Err，取消不进入后续启动准备。nil上下文明确拒绝。同步验证器一旦进入仍不能强行中断，不宣称已开始外部操作回滚。
- 五项永久回归：锁等待取消无需先解锁且不调用验证、验证期间取消不进入transition、正常解锁保留验证及错误原因、已受管重复启动幂等、nil上下文拒绝。取消后再取得同Bot配置锁证明没有遗留持锁。前两项及既有配置删除/启动互斥三轮race通过/根包2.701s；最终完整race根包25.592s/Web59.357s/config3.642s、vet根包/Web/config及diff均exit0。前端typecheck、实际3文件46项保存回执/运行守卫0.256s通过；Ruby交易门禁9runs58assertions零失败/错误/跳过。
- 生产接线边界：main.go主进程调用symbolManager.StartBot(ctx,botCfg)，main_adapters_bot.StartBot原样透传调用方ctx；但web/api_bots.go的postBotStart先回202并在协程中使用context.Background。这是独立后台任务语义，不能简单接Request.Context导致HTTP回202后初始化自动取消，或给运行实例继承短超时而误停已启动交易。本轮未改后台任务生命周期，不将核心可取消能力冒充HTTP断开取消/启动任务查询的端到端闭环。
- 源码3.111.0-rc995、HEAD5c048825且未提交；JSON/Markdown报告 `/private/tmp/quantmesh-start-context-rc995.Z4Hw2s/`。未重建本版二进制、未执行严格MySQL/全前端/浏览器E2E、未提交/推送或main/tag、发布/部署、真实账户/生产数据操作。后台启动状态/取消、其他配置锁与数据库取消、全局历史owner及安全重启/完整经济状态恢复仍开放，R01–R15不缩减，不宣称实盘或盈利验收。

## rc994：全局保存前保护金融恢复归属（本地针对性验证完成，未提交）

- 上轮只读复现为有效进展，本轮修复全局JSON/YAML保存绕过恢复保护的缺口；下方“已复现，未修复”为历史诊断状态，不代表本轮之后仍未实现。先重跑原overlay两真实handler及名称正例，修复后exit0/Web3.477s。
- 保存前提取独立Bot的删除/新增/冷配置变化；共享Exchange配置、Strategies、BotGroups、冷Trading默认或旧Symbols冷配置变化时保守纳入全部相关配置owner。独立Bots列表与旧Symbols镜像分开比较，镜像不能掩盖主配置归属被删除。冷字段投影抽到config.ColdBotConfiguration与运行态共用；仅排除既有热应用支持字段及Name/CreatedAt，不以允许保存声称专用执行器所有热字段都支持。
- 按ID排序调用实际provider已有的上下文生命周期协调接口，在所有已取得锁仍保持时检查受管状态、钱包预留、完整恢复状态，再进入配置管理器写锁进行既有基线比较及事务保存。受管/预留/pending回409，无法核验回503，均在持久化前明确config_saved=false；取消等锁沿用写前取消sentinel。底层持久化错误仍不宣称数据回滚。全局路径的新闻/权益通知移到生命周期锁释放后，不在持配置锁时反向等待Bot锁。
- 新永久回归覆盖两格式真实handler的删除、方向、策略、账户测试网、Bot组及遗留镜像变更，检查管理器未变化与SQLStorage主库owner保留；支持热/名称更新后pending仍存在。另有受管/预留/损坏记录拒绝、上下文等锁取消、重复身份拒绝、多Bot排序及解锁后通知。多Bot顺序测试为Web协调器探针，生产适配器的生命周期/上下文接线沿用已有根包回归，不冒充真实账户并发启动/停止的端到端验收。
- 初次完整race根包23.126s/Web57.352s/config1.680s与vet通过，但随后修改了旧Symbols边界，不能作为最终版本证据。新增镜像夹具初次三轮回归失败，原因是旧格式funding_carry缺少必填订单金额，在handler校验阶段即400；补足隔离夹具OrderQuantity后关联三轮race通过/Web25.001s，不放宽生产校验或降低409断言。
- 最新完整Web门禁另发现TestUpdateConfig及TestUpdateConfigWithStringNumbers旧夹具没有生命周期协调器，故冷配置保存按新要求回503而非原200；为正常保存夹具补充协调器，并独立增加缺协调器拒绝且不保存的负例，未放宽生产门禁。最终完整race根包24.665s/Web59.884s/config1.436s、vet根包/Web/config与diff均exit0；前端typecheck及实际3文件46项保存回执/运行守卫测试0.244s通过，Ruby交易门禁9runs58assertions零失败/错误/跳过。没有把中途通过或旧构建作为最终版本验收。JSON/Markdown报告 `/private/tmp/quantmesh-global-recovery-rc994.tbRrdV/`；源码3.111.0-rc994、HEAD5c048825且未提交。main.SetVersion与API版本header既有接线保留，未启动本版服务做真实header读回。
- 限制：共享依赖保护目前偏保守，可能要求无关受管Bot先停止；不能将持续拒绝当作安全停止/核账/恢复功能闭环。其他全局配置消费者、仅旧单Symbol配置、没有当前配置的历史owner、兼容AddRuntime/外部资金预留写入、跨进程改写/ABA、提交确认及保存/应用共同事务仍待核验，完整金融恢复及R01–R15仍开放。未构建rc994新二进制、未访问生产库/真实账户、未下单、未提交/推送、main/tag、发布或部署。

## rc993后续核验：全局保存绕过待恢复Bot身份保护（已复现，未修复）

- 上轮实现/验证为进展；本轮回到R09恢复合约及控制面保护，使用Go overlay注入隔离诊断，不把失败探针放入默认测试或修改交易实现。源码仍3.111.0-rc993/HEAD5c048825，保留既有未提交改动，未修改版本或提交。
- 真实JSON/YAML全局handler两种格式独立子场景：seedFinancialRecoveryFixture建立pending金融记录，并先核对verifyBotRecoveryConfiguration确实返回ErrRecoveryConfigRequired；从新提交配置移除该Bot后保存。race命令终态exit1、Web2.180s，两种格式均HTTP200，管理器与SQLStorage主库app_config读回均不再包含待恢复owner。此为真实handler/配置持久化接线及隔离SQLite复现，不是生产账户操作。
- 没有证明历史bot_configs或金融记录已被删除：复现证明的是主配置可以遗忘待恢复Bot，绕过单Bot删除/策略恢复身份保护。rc993锁内DeepEqual仅确保读取后未被其他写入改变，不核验请求自身是否破坏恢复合约；保存后的runtime_restart_required或NotRunning也不能补回丢失的主配置身份。
- 正向边界另行运行exit0、Web2.708s：有pending记录时仅更改Name仍允许YAML保存，owner保留且恢复核验仍要求核账。因此后续修复不能简单禁止所有有待恢复记录的配置保存，应保留正常元数据/已支持热控制更新。
- 下一实现要求：确定删除/账户身份/策略/方向/金融腿等恢复合约变更影响的Bot集合；按与StopBotsAndPersistRemoval一致的排序取得生命周期锁，在保存前核对受管状态、钱包预留及完整金融恢复记录，再在已有配置写锁内比较基线并保存；不能在持配置锁时反向等待Bot锁，也不能只做解锁后的一次性核验。需要覆盖多Bot、并发启动/停止、全局凭据/策略改写，以及正常热/元数据保存，不能缩小为仅删除一个Bot的拒绝。
- 证据/可重跑overlay/JSON/Markdown保存在 `/private/tmp/quantmesh-global-recovery-audit.JOXIV4/`；探针没有写入仓库，diff检查通过。该高风险问题尚未修复，不给出新的发布/实盘放行结论；未访问真实账户/生产库、未下单、未提交/推送或发布/部署，原R01–R15及全局事务/版本栅栏/金融UNKNOWN恢复仍开放。

## rc993：全局JSON/YAML保存的进程内基线比较（针对性验证完成，未提交）

- 上轮为有效进展，本轮继续全局写入绕过并发保护的问题。独立受控FileConfigManager回归先失败exit1（Web2.309s）：包装既有UpdateConfigWithBotHistorySource后，读取旧基线、另一路修改Bot、再保存旧请求会丢掉新修改；已取消请求也仍保存。不是两个真实HTTP请求完整调度复现；使用的是现有真实持久化方法与隔离SQLite夹具。正常保存/请求引用隔离在旧方法已通过，保留正例而非声称新修复。
- JSON/YAML两入口改为updateConfigFromSnapshot：复制请求配置，通过已有UpdateConfigUsingWithBotHistorySource在配置管理器写锁内检查请求取消并DeepEqual比较读取基线，再提交新配置。冲突在任何本次持久化前返回固定configuration_changed/409、config_saved=false，不进入HotReloader或Bot应用。写前取消带专用sentinel，回configuration_save_cancelled/408；底层持久化返回的Canceled/DeadlineExceeded不能等同写前取消，不宣称未保存或回滚，回固定configuration_save_failed/500且不公开原错误。
- 六项新回归覆盖中间Bot修改拒绝且管理器/主库读回保留、已取消不保存、正常写入及请求引用隔离、同一基线并发两请求仅一个成功、JSON/YAML真实handler取消不保存/应用，以及底层普通/取消/超时错误不泄漏详情或宣称回滚。主库读回loadConfigFromPrimaryDB已核对为SQLStorage.GetAppConfigDocument，不是管理器缓存。初期关联三轮race13.867s通过；完整六项在写前取消错误分型完成后再三轮race8.273s通过。
- 初次完整回归通过后追加取消错误分型，未以旧检查冒充最终验证；最新生产实现完整race根包24.376s/Web49.727s/config1.614s、vet与diff均exit0。前端typecheck13.80s、保存回执2文件43项0.237s通过；Ruby交易门禁9runs58assertions零失败/错误/跳过。源码版本3.111.0-rc993，HEAD5c048825未提交；JSON/Markdown报告 `/private/tmp/quantmesh-config-snapshot-rc993.mdpNe7/`。未重建本版嵌入二进制，未重跑前端全部300项，不继承rc989构建或同提交发布验收。
- 存储源码核对：SaveAppConfigSnapshotWithBotSource已有SQL事务统一app_config/history与Bot快照，且既有SQLite触发器故障回滚测试；本轮不声称此前没有任何数据库原子性，也未改造该事务/运行严格MySQL。连接丢失时commit确认、并发外部写入及内存/运行态一致性仍是额外证据要求，不能从普通error推出全部数据库已回滚。
- 限制及下一步：此比较只保护GetConfig之后、写锁内提交之前的本进程窗口，不识别客户端请求前就持有旧文档的意图，也不识别ABA；并非主库expected_revision CAS或跨进程fencing。读写锁及数据库等待仍不能保证及时取消，其他入口仍可直接保存；受管Bot恢复合约全局写入保护、核验后变化/HotReloader与通知旁路、保存/应用共同事务、部分应用补偿、金融UNKNOWN完整恢复和跨账户协调仍开放。未做严格MySQL/浏览器E2E/目标Linux构建、真实账户/生产数据操作或提交/推送、main/tag、发布/部署；R01–R15不缩减，不宣称实盘或盈利验收。

## rc992：策略保存锁等待可取消（针对性验证完成，未提交）

- 上轮为已实施并验证进展，本轮继续策略保存阶段仍不能响应取消的问题。真实botManagerProviderAdapter独立红测exit1（根包1.339s）：30ms超时，200ms观察窗未返回，解锁后模拟保存callback仍执行且返回nil。此为实际适配器锁等待与callback接线复现，不是生产数据库被错误写入的证明；真实策略handler在最终持久化前原本已有请求检查，但仍存在长等待及多余回调工作。
- 适配器补转发WithBotStrategyConfigurationContext，策略HTTP保存入口选择该能力并传递Request.Context；使用已有BotManager上下文锁等待/准入逻辑，不增加waiter协程。进入保存callback前再检查取消；兼容不支持context的协调器保留分派但取得锁后也检查，不保证其等待可立即取消。正常持久化、保存锁释放后应用及已有恢复配置拒绝保留。
- 无成功保存快照且协调取消时返回408固定bot_configuration_cancelled及config_saved=false；若成功保存标记已存在，即使协调器随后返回取消错误，也继续沿已保存的应用回执路径，不将已保存文档说成回滚。数据库部分写入但返回错误仍是独立事务/UNKNOWN风险，不能仅凭标记缺失证明底层完全无副作用；既有handler已写错误响应时不覆盖为未保存408。
- 一项根包回归覆盖实际适配器持锁取消及时返回、不调用callback及后续重新取得锁；两项真实Web策略handler回归核对取消前未保存的配置快照保持不变，以及保存后取消仍回saved=true/409并读回实际管理器配置。关联正常保存/应用和三入口上下文透传三轮race根包2.062s/Web7.341s通过；最终完整race根包23.244s/Web52.627s/config2.022s、vet及diff均exit0。前端typecheck16.34s、保存回执2文件43项0.310s通过；Ruby门禁9runs58assertions零失败/错误/跳过。
- 源码3.111.0-rc992，HEAD5c048825且未提交；JSON/Markdown报告 `/private/tmp/quantmesh-strategy-context-rc992.dsJ6WP/`。未重建嵌入二进制/compiled来源，未重跑前端全部300项，不继承rc989构建验收。本轮关联控制面恢复身份保护与有效控制应用，不缩减R01–R15。
- 其他破坏性配置入口/JSON/YAML的配置管理器锁及SQL保存仍不保证及时取消；HTTP读取body、已有执行回调和configMu等阻塞也未完整改造。保存/应用事务、单调版本及核验后写入、HotReloader旁路、兼容注册/跨Bot账户保护、部分应用补偿、金融UNKNOWN恢复与跨进程fencing仍开放。未做严格MySQL/浏览器E2E/目标Linux构建、真实账户或生产数据操作；未提交/推送、main/tag、发布/部署，不宣称实盘或盈利验收。

## rc991：热应用等待的请求取消接线（针对性验证完成，未提交）

- 上轮为有效实施/验证进展，本轮继续rc988–rc990开放的取消边界。独立实际symbolManagerWebAdapter红测exit1（根包1.353s）：请求30ms超时，但200ms观察窗结束仍未返回，只有释放生命周期锁后返回runtime_configuration_changed。旧路径没有上下文方法，红测用旧guard检查取消，不将未调用回调当作等待已可取消。
- 策略、JSON、YAML保存的报告调用改为携带Request.Context；web可选context能力→真实适配器→SymbolManager→BotManager完整透传，旧直接调用使用Background维持兼容。实际管理器在初始阶段、生命周期锁准入后/guard后及configMu取得后复核取消；生命周期锁等待使用已存在的无遗留waiter协程TryLock/context机制，不再等待锁释放才识别取消。已取消或超时返回固定runtime_application_cancelled，剩余Bot亦不会新进入执行回调；生命周期非取消错误仍为runtime_lifecycle_unavailable。
- 三项根包回归覆盖真实适配器持锁取消可返回、预先取消不应用而后续正常请求仍可应用、锁内freshness检查取消后不调用参数回调。一个Web表驱动真实入口回归覆盖策略/JSON/YAML请求上下文透传与正常应用；关联三轮race根包2.354s/Web5.688s通过。最终完整race根包22.733s/Web48.842s/config1.876s、vet与diff均exit0。前端typecheck13.81s、保存回执2文件43项0.269s通过；Ruby交易门禁9runs58assertions零失败/错误/跳过。
- 源码版本3.111.0-rc991，HEAD5c048825且未提交；JSON/Markdown报告 `/private/tmp/quantmesh-hot-context-rc991.SK9hfw/`。未重跑前端全部300项或本版嵌入构建/provenance，不继承rc989二进制；当前检查仅证明本地受控应用行为，不是同提交发布或实盘验收。
- 边界：HTTP保存入口在持久化前等待的策略保存锁仍走旧无context协调接口；全配置管理器读锁/clone/持久化、br.configMu等待及执行回调本身不提供及时取消。取得configMu后复核保证该等待期间已取消不启动新回调，但不能宣称请求在此阻塞点立即返回。已开始回调/金融操作不会被请求取消自动回滚，回调失败同时发生取消时以取消码报告并保留既有安全门禁，不等于已核清部分应用。兼容不支持context的provider仍可走旧分派，进程退出上下文也尚未统一透传。
- 保存/应用共同事务、单调版本/核验后写入及HotReloader旁路、兼容注册保护、部分应用补偿、UNKNOWN完整恢复、跨Bot账户和跨进程fencing仍开放；R01–R15保持完整范围。未做严格MySQL/浏览器E2E/目标Linux构建或真实账户/生产数据操作；未提交/推送、main/tag、发布/部署，不宣称实盘或盈利验收。

## rc990：YAML保存实际运行态应用接线（针对性验证完成，未提交）

- 上轮实施并验证快照guard为有效进展；本轮检查真实YAML控制面接线，独立两项修复前race红测exit1（Web1.933s）：正常保存HTTP200但guarded应用调用为0；运行态拒绝也未回409/报告。实际主程序main.go创建HotReloader后未注册执行callback，仓库注册点仅见测试，因此原YAML只更新内部配置不能视为Bot执行器已应用。
- YAML保存后沿用热重载器，再调用与JSON相同的applyTradingParamsWithReport；实际适配器已有逐Bot生命周期/快照guard。失败回409固定runtime_configuration_apply_failed、config_saved=true及runtime_update；正常报告Applied/Failed/NotRunning，保留changes_count、requires_restart及兼容runtime_update_verified。缺provider保留saved/unverified，不虚构应用成功。热重载器失败仍返回失败，不吞错误、不泄露原callback错误。
- 四项新真实handler回归覆盖正常guarded应用、已保存后逐Bot拒绝、缺provider未核实，以及热重载callback注入后续保存使旧YAML快照在应用前被拒绝且新保存仍保留。初始两个正/负路径与既有热重载失败两格式三轮race5.548s通过；补齐四项后的三轮YAML回归6.121s通过。最后增加“被新保存替代”用例与完整命令并行，完整结果不单独证明新增用例覆盖，由随后四项三轮终态证明；未修改生产实现后沿用旧检查。
- 完整race根包23.453s/Web48.316s/config3.139s、vet及diff均exit0；前端typecheck14.39s、保存回执2文件43项0.239s通过；Ruby交易门禁9runs58assertions零失败/错误/跳过。本轮仅后端接线和版本/记录变化，未重跑前端全300项、嵌入构建/compiled provenance，不继承rc989二进制/同提交验证。源码版本3.111.0-rc990，HEAD5c048825且源码未提交；JSON/Markdown报告 `/private/tmp/quantmesh-yaml-report-rc990.EsoSvq/`。
- 本轮关联R01/R02的实际控制应用及R09/R13的控制面接线，不将其当作R01–R15闭环：所有全局消费者、HotReloader在guard前的内部快照/回调、核验后全局写入/ABA/跨进程版本栅栏、保存/应用共同事务、可取消等待、兼容注册、部分应用补偿和金融UNKNOWN完整恢复仍开放。未做严格MySQL/浏览器E2E/目标Linux构建或真实账户/生产数据操作；未提交/推送、main/tag、部署，不宣称实盘或盈利验收。

## rc989：持生命周期锁后核验保存快照（本地验证完成，未提交）

- 上轮为已实施并验证的进展，本轮继续rc988明确开放的保存锁释放后旧快照滞后应用窗口。实际Web报告增加guarded能力，由真实symbolManagerWebAdapter→SymbolManager→BotManager透传；guard从GetLatestConfig读取管理器当前配置副本并DeepEqual比较请求快照。不是主库直读或数据库版本CAS。
- 管理器先核验再登记声明equity scope，且取得每个Bot生命周期锁后再核验；已被替代或读取失败返回固定runtime_configuration_changed，不调用旧参数回调、不列Applied。既有直接内部调用与不支持guard的兼容provider保留原行为，不能据此称所有入口均受版本保护。逐Bot应用期间的新变化仍可形成部分报告，不追溯撤销先前已执行回调。
- 两项新根包回归验证初始核验后等待锁期间状态变化会在锁内拒绝、实际Web适配器拒绝旧快照且正常PauseOpening可应用；三项Web回归验证保存后再次替代的旧快照拒绝、最新快照正常应用，以及策略/JSON真实保存入口使用guard。初次Web断言误认为seed仅一Bot，实际夹具含两个Bot导致测试失败，按完整Bots集合修正，没有放宽新鲜度门禁。随后四项关联三轮race根包2.236s/Web4.075s通过（最后新增JSON入口纳入最终完整验证）；没有独立修复前红测，不把夹具断言错误当作原生产缺陷复现。
- 最终完整race根包24.809s/Web53.473s/config2.645s及vet全部exit0，diff通过。前端typecheck14.20s、50文件300项4.59s通过；Ruby交易9runs58assertions/嵌入14runs27assertions零失败/错误/跳过；compiled embed来源1.273s通过。
- Go+React嵌入技能用于本地Make，完整前端/Vite/PWA/同步/Go构建exit0。CLI版本3.111.0-rc989，SHA256 `8f4565dfb0a597c019a9235b7da2b6c584be2daa19e688afdcd1fa4de46f16b6`，源码5c048825 modified=true，非干净同提交发布验收。产物/JSON/Markdown `/private/tmp/quantmesh-hot-freshness-rc989.8EXXok/`；旧嵌入备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-16755-9i7pv6/dist` 保留。
- 边界与下一步：比较值不识别ABA，也不提供单调版本；核验完成后不持全局配置锁，其他全局写入/跨进程修改仍可发生，equity scope登记与核验也非共同事务。JSON全局hotReloader在报告前执行，YAML仍未走此报告路径，不能把本轮拒绝当所有配置消费者都已阻止旧值。全配置比较意味着其他字段变化也会拒绝旧快照，每Bot复制完整配置的成本未基准测量；大配置下需版本化快照代替重复比较。保存/应用共同流程、可取消等待、兼容注册保护、部分应用补偿、UNKNOWN恢复与跨进程fencing仍开放。未做策略全包/严格MySQL/浏览器E2E或真实金融终态验收；未提交/推送、main/tag、部署或真实账户/生产数据操作，R01–R15不缩减，不宣称实盘或盈利验收。

## rc988：热应用与停止生命周期协调（本地验证完成，未提交）

- 上轮六小时回查为有效证据进展：独立实际StopBot红测在停止回调尚未完成时，热回调已执行并被报告Applied。此次新增永久StopBot回归和实际Web策略保存入口回归先独立失败（根包1.125s/Web1.438s）；后者证明保存仍持生命周期锁调用应用，不能直接给报告函数追加同锁。StopAll同样旁路，新增红测0.960s失败；首次执行因Go缓存权限setup失败，获许可按原范围重跑才复现，不算源码失败。
- 实际报告函数逐Bot通过同一生命周期锁与进程runtimeAdmissions；等待后重读注册实例，再调用风控/参数应用，不持runtimesMu执行回调。已移除实例进入NotRunning，生命周期/退出拒绝进入固定runtime_lifecycle_unavailable，不能列Applied。已准入回调会被退出Drain等待。配置声明的equity scope仍按既有逻辑先更新，不声称整体配置已应用。
- 策略保存入口只在锁内检查/持久化并获取成功保存快照，释放后才调用运行态应用并返回原saved/report语义，消除该入口重入同锁及持目标锁遍历其他Bot的锁序循环。StopAll保留整批准入，逐Bot持锁复核List指针仍为注册实例；提取独立文件，不把全批多锁同时持有。停止失败仍保留原未核实标记及受管实例，不冒充关闭成功。
- 新四项根包回归覆盖StopBot→应用、StopAll→应用、应用→StopBot互斥、退出Drain等待/回调后释放与拒绝新应用；一个实际Web入口回归覆盖保存已读回后、保存锁已释放再报告应用。前期关联三轮race根包5.245s/Web15.772s通过（最后补反向互斥断言纳入完整回归）；最终完整race根包24.515s/Web48.417s/config1.621s及vet全部exit0。前端typecheck12.85s、50文件300项3.85s通过；Ruby交易9runs58assertions/嵌入14runs27assertions零失败/错误/跳过，compiled embed1.259s通过，diff检查通过。
- Go+React嵌入技能用于本地Make，前端/Vite/PWA/同步/后端完整exit0。版本3.111.0-rc988，SHA256 `9dc6675d7c5c4ac3fa1e70bc6313573b89b6fbd2a6c2ffa37f82fe78c05a876d`，源码5c048825 modified=true，非干净同提交发布验收。产物/JSON/Markdown `/private/tmp/quantmesh-hot-lifecycle-rc988.w6Vulz/`；旧嵌入备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-15587-xopyhi/dist` 保留。
- 限制及下一步：释放保存锁后存在后续保存抢先写入、旧快照稍后应用的窗口，本轮没有版本栅栏，不能称保存/应用共同事务或按保存顺序线性化。报告接口仍无请求取消上下文，等待停止可长时间阻塞；任意回调同步重入同Bot生命周期仍可能自锁，已知生产更新回调并不这么调用，但不作全回调证明。兼容AddRuntime替换/跨Bot账户保护、其他全局写入、回调内部部分应用补偿、UNKNOWN恢复/跨进程fencing仍开放。未做策略全包/严格MySQL/浏览器E2E或真实金融终态验收；未提交/推送、main/tag、部署或真实账户/生产数据操作，不缩减R01–R15，不宣称实盘或盈利验收。

## rc987：策略快照共享图与循环容器（本地验证完成，未提交）

- 修复前有限共享图独立红测exit1：同一子map被复制成两个副本，递归不记录容器身份，复杂共享子图会重复展开。循环输入无终止条件为源码推断，没有让旧实现运行循环数据致进程栈溢出，也没有真实启动/生产故障复现。
- 快照按reflect.Type/容器地址记录map副本，按类型/地址/长度记录slice副本，先缓存再递归；保留共享容器关系、终止map/slice循环，不混淆同一底层数组的不同长度视图。地址仅用作身份key，不解引用或写入外部内存。本机Go官方reflect.Value.UnsafePointer文档已核对map/slice可用，race/checkptr及vet通过。
- 三项新回归覆盖共享子图只复制一次且不使用外部map、循环map/slice独立复制可终止、不同slice长度正确；原数值类型/nil/空/嵌套隔离与实际注册测试保留。config快照三轮race1.773s通过；最终完整race根包22.779s/config1.469s、vet根包/config/Web及diff通过。前端typecheck12.00s、50文件300项3.16s通过；Ruby交易9runs58assertions/嵌入14runs27assertions零失败/错误/跳过，compiled embed1.318s通过。
- 首次完整race因Go缓存sandbox权限setup失败（根包和config均未运行）；获许可后原完整范围重跑通过，不把setup失败称代码失败或已验收。Go+React嵌入技能用于Make本地前端/同步/后端构建，完整exit0。版本3.111.0-rc987，二进制SHA256 `c66799f157a3f0d8361b02af06ec0d7e68f063938fb20a67d6ef4f4b7385e1a2`，源码5c048825 modified=true，非干净同提交发布验收。产物/JSON/Markdown `/private/tmp/quantmesh-strategy-graph-rc987.MHN2w7/`；旧嵌入备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-14199-p28d4e/dist` 保留。
- 限制：复制完成不表示循环策略可序列化、通过验证或执行；本轮无内存上限/超深链上限或实测资源基准，源map并发修改、任意程序对象/指针及其他冷引用仍未隔离。实际初始化/兼容组件内部旧引用、保存与安全重启共同流程、金融UNKNOWN恢复、跨Bot账户协调及跨进程fencing仍开放。未做策略全包/严格MySQL/浏览器E2E/真实终态验收，未提交/推送、main/tag、部署或真实账户/生产数据操作，R01–R15不缩减，不宣称实盘或盈利验收。

## rc986：初始策略恢复合约数据隔离（本地验证完成，未提交）

- 实际AddRuntime独立红测exit1：注册后原配置修改策略类型和嵌套steps数组，直接改变管理器BotRuntime.Config的策略恢复合约。不是完整金融组件/交易所初始化复现。
- CloneStrategyInstances复制策略列表及声明式map/slice/array/interface容器，保留具体整数/浮点类型、nil与空集合语义；不使用JSON往返改变参数数值类型。实际StartBot在最新配置/范围预留后、费用查询及startSymbolRuntime前复制策略；兼容AddRuntime在configMu内首次复制，之后释放该锁再发布，不增加管理器锁内等待配置锁。首次快照标记避免重复注册重写已发布策略引用。
- 一项实际注册回归覆盖原类型/嵌套参数修改不影响管理器及重复注册不更换策略列表；两项config回归覆盖int64/大uint64/float64类型、typed map/slice、嵌套Combo式children、array内slice及nil/空语义。三轮相关race根包2.087s/config2.151s通过（之后只补重复注册断言，纳入最终完整验证）；最终完整race根包22.741s/config1.446s，vet根包/config/Web和diff通过。前端typecheck10.93s、50文件300项2.82s通过；Ruby交易9runs58assertions/嵌入14runs27assertions零失败/错误/跳过，compiled embed1.099s通过。
- 首次完整命令根包因Go缓存sandbox权限setup失败、config1.673s通过，但整条exit1不称完整成功；获许可后按原范围完整验证。Go+React嵌入技能用于本地Make，前端Vite/PWA/同步成功、后端权限失败exit2；最终获许可的完整race/vet/Go构建链exit0。二进制3.111.0-rc986，SHA256 `d45deb95a56510590cfac264be08e670b48143d38b89b89c6e2846b88f6d3822`，源码5c048825 modified=true，非干净同提交发布验收。产物/JSON/Markdown `/private/tmp/quantmesh-strategy-snapshot-rc986.v9eX1X/`；旧嵌入备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-13183-e46n6c/dist` 保留。
- 限制：复制器前提是稳定、无环的声明式配置，并不隔离任意程序对象/指针/循环图或并发输入修改。实际启动前复制接线有源码证据，未完整启动金融运行时做端到端故障注入；兼容注册仅隔离管理器Config，Inner及已经初始化的策略组件可能仍持旧引用，内部组件之间也可能共享副本。其他冷配置指针、全局保存/安全重启共同流程、跨Bot账户协调、UNKNOWN恢复及跨进程fencing仍开放。未做策略全包/严格MySQL/浏览器E2E/真实终态验收，未提交/推送、main/tag、部署或真实账户/生产数据操作；不缩减R01–R15，不宣称实盘或盈利验收。

## rc985：热更新不接管请求的冷配置引用（本地验证完成，未提交）

- 修复前独立实际applyRuntimeTradingParams回归exit1：旧运行配置与请求配置值相同但引用独立，合法PauseOpening热更新成功后，调用方修改Enabled指针、策略嵌套数组、金融腿、Profiles map及SlotFilter价格数组，会直接改变运行态冷配置，绕过rc984的应用前比较。
- runtimeHotCandidate从运行态previous合并实际应用的热字段/展示元数据，OpenPositionControl沿用现有深复制；不再整份接管请求的冷map/slice/pointer。Inner配置由该合并结果生成。冷比较和原风险应用失败回滚保留，不靠拒绝正常热更新避免引用泄漏。
- 一项新永久回归覆盖五种外部冷引用修改不影响Config/Inner、合法PauseOpening保留、事后再次提交变化后的冷配置被拒绝且回调只执行一次。最终热参数/冲突快照三轮race2.081s、根包完整race23.902s、vet根包/Web和diff通过；前端typecheck13.79s、50文件300项4.72s通过。Ruby交易9runs58assertions/嵌入14runs27assertions零失败/错误/跳过，compiled embed1.097s通过。初次vet受Go缓存sandbox权限拒绝，获许可后原检查通过；没有把权限失败当源码失败。
- Go+React嵌入技能用于Make本地前端构建/同步/Go构建，完整exit0。版本3.111.0-rc985，二进制SHA256 `acead699da4bed8556af60e20852138a51c166808f292bcfceda6189a68c6cbb`，源码5c048825 modified=true，非干净同提交发布验收。产物/JSON/Markdown `/private/tmp/quantmesh-hot-alias-rc985.SzTGyq/`；旧嵌入备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-12549-jdg83j/dist` 保留。
- 限制：运行态previous在初始启动/注册时可能已与全局配置共享引用，本轮没有将所有冷配置深复制或隔离这些旧别名；外部并发修改输入对象本身的快照协议仍未完成。保存前门禁、安全停止/核清/重新初始化共同流程、跨Bot账户协调、初始化失败恢复、旧身份迁移及跨进程fencing仍开放。未做策略全包/严格MySQL/浏览器E2E或真实金融终态验证，未提交/推送、main/tag、部署、真实账户/生产数据操作；完整R01–R15保留，不宣称实盘或盈利验收。

## rc984：运行态热参数与冷配置边界（本地验证完成，未提交）

- 修复前九个实际applyRuntimeTradingParams子场景独立红测exit1：交换交易所/交易对/市场/测试网/方向/策略/库存政策/分配资金/网格价格边界时，函数均返回nil并调用UpdateOpenControl；整份Config/Inner配置改写并没有重建对应金融执行器，不可称完整应用。
- 在configMu内、任何Config发布和风控回调前比对冷配置；只排除已有应用路径的PriceInterval/ProfitSpread/OrderQuantity/买卖窗口/OpenPositionControl/GridRiskControl及展示元数据。新增字段默认冷；冷变化返回sentinel，实际Web适配器报告Failed[botID]=runtime_restart_required，不进入Applied、不回传原错误。默认ID、方向、市场及库存政策按已有默认语义比较，不误封仅显式填写默认值；spot与spot_margin仍不同。
- 专用策略无SPM时只拥有开仓控制回调，不将网格间距/窗口/数量/网格风控变化冒充已应用，额外按冷配置拒绝；正常SPM网格参数/风控、原专用策略开仓控制仍可更新。原并发Config/range回归改为实际支持的PauseOpening热字段，未通过保留不真实的专用策略网格应用来维持测试。
- 新四个测试函数覆盖11个拒绝子场景（其中新增两个专用网格场景没有修复前独立红测）、全Config/Inner保留、实际Web适配器失败报告、正常SPM参数与开仓风控发布、等价默认及现货杠杆区别。最终相关热参数/原网格/专用策略/冲突快照三轮race2.302s、根包完整race23.828s、vet根包/Web与diff通过；前端typecheck12.77s、50文件300项3.02s通过；Ruby交易9runs58assertions/嵌入14runs27assertions零失败/错误/跳过，compiled embed1.090s通过。
- Go+React嵌入技能用于Make本地构建，Make完整exit0。随后默认语义修正后的根包验证因Go缓存sandbox权限setup失败，不作为源码失败；获许可后完整race/vet/Go构建通过，之后专用策略分支补全再完整验证和重建最终产物。二进制版本3.111.0-rc984，SHA256 `fd82c4554aec50409876c4c663cb3863ff1e304cb1e02516fe3ac2e28ecc7544`，源码5c048825 modified=true，非干净同提交发布验收。产物/JSON/Markdown `/private/tmp/quantmesh-hot-contract-rc984.QWXPxo/`；旧嵌入备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-11281-rsnhfa/dist` 保留。
- 限制：后端拒绝冷运行态应用不撤销已保存文档，全球配置对象/其他写入回调可能仍变化；保存前生命周期/恢复门禁及安全停止-核清-重新初始化共同流程没有完成。此前不支持的字段现在报告失败/需重启，不把安全拒绝当作冷变更功能闭环；前端目前消费通用应用失败回执，专用重启引导与浏览器E2E仍待补齐。配置指针别名、回调部分应用补偿、跨Bot账户协调、初始化失败恢复、旧身份迁移与跨进程fencing及R01–R15仍开放。未做策略全包/严格MySQL/真实终态验收，未提交/推送、main/tag、部署或真实账户/生产数据操作，不宣称实盘或盈利验收。

## rc983：运行中金融冲突范围固定快照（本地验证完成，未提交）

- 新真实BotRuntime回归在修复前执行1000次热参数更新并发1000次启动冲突查询，race明确报告applyRuntimeTradingParams整份Config赋值与BotsConflict读取Exchange/Symbol/MarketType竞争，测试exit1。非推断或测试运行中当成功。
- 实际startSymbolRuntime返回后的Bot注册与AddRuntime兼容注册均保存固定金融冲突快照（原始市场字段、UseSpotMargin及复制的双永续腿）。findConflictingRuntime仅在管理器runtimesMu内读取该快照，不读可变Config；缺快照保守拒绝，不把未知当无冲突。启动预留复用同一快照构造。重复注册同实例不重新解释金融腿；新替代实例才更新其快照。
- AddRuntime先持configMu读配置快照、释放该锁，再取runtimesMu发布；冲突读路径不在管理器锁内等待configMu，避免热参数回调持configMu查询管理器的逆序。没有改写所有运行态配置，也没有把热配置修改当成实际重新初始化交易腿。
- 四项永久回归覆盖实际热参数并发冲突查询、实际UpdateOpenControl回调持configMu时查询管理器、spread指针变化及重复注册不释放原腿、绕过注册的未知范围不能称无冲突。最终与rc982六项预留回归共三轮race2.141s；根包完整race24.892s通过。根包/Web vet及diff通过；前端typecheck12.68s、50文件300项3.84s通过；Ruby交易9runs58assertions/嵌入14runs27assertions零失败/错误/跳过，compiled embed1.172s通过。
- Go+React嵌入技能用于本地Make。前端Vite/PWA及同步通过，Make后端和初次vet因Go缓存sandbox权限退出（Make exit2）；许可后vet+Go构建exit0。版本3.111.0-rc983，二进制SHA256 `df840a5c89fa103d6cbc2915ecced6f73ebfce0467afd10a4130dddb3dcfd874`，源码5c048825 modified=true，非干净同提交发布。产物/JSON/Markdown `/private/tmp/quantmesh-conflict-snapshot-rc983.01pp3Z/`；旧嵌入备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-10680-7com8f/dist` 保留。
- 仍开放：全局热更新可写入不同恢复合约，Config/Inner/实际金融路由一致性、其他读Config路径及其竞争未因固定冲突快照自动修复；现货多Bot、AddRuntime冲突准入、平仓与启动共同保护、初始化失败后耐久补偿及跨进程fencing仍待闭合。未验证策略全包/严格MySQL/浏览器E2E或真实终态。未提交/推送、main/tag、部署、真实账户/生产数据操作，不缩减R01–R15，不宣称实盘或盈利验收。

## rc982：冲突Bot初始化前的范围预留（本地验证完成，未提交）

- 源码发现StartBot原首次冲突检查与注册检查之间调用费率接口/startSymbolRuntime；两不同Bot可在都未注册时通过首次检查，注册时才拒绝不能撤销初始化副作用。本轮为源码时序发现，没有修复前真实金融双初始化红测，不宣称该旧路径已通过故障注入复现。
- 在prepareBotStartConfig刷新/解析最新配置后、费率RPC及startSymbolRuntime前，于runtimesMu下检查已注册实例与pendingStarts并预留范围；预留到初始化返回/注册结束才通过defer释放。沿用config.BotsConflict含Funding Carry和双永续两腿政策，不新增现货多Bot政策或猜账户。复制只读冲突字段和spread定义，sync.Once防止旧cleanup删除替代预留；不持runtimesMu跨金融RPC，不新增等待锁序。费率查询前后检查取消。
- 复核发现计算结果spot_margin不能直接存入BotConfig.MarketType（GetMarketType会把它当默认futures）；最终预留保留原MarketType及UseSpotMargin，并补正常现货杠杆/合约并存正例。初版实现缺陷已修正，不用误封正常能力换取测试通过。
- 六项永久测试：同时请求同腿只有一个预留成功；spread指针修改不变更预留及cleanup幂等；已注册所有者拒绝/无关币对正常；实际StartBot遇pending冲突在feeFetcher前退出且不注册；feeFetcher夹具取消后实际StartBot不初始化且释放预留；spot_margin不改变原政策。测试状态文件只写本次TempDir，费率mock无外部RPC，不执行真实交易初始化。实际完整两条金融初始化并发及所有副作用恢复仍无本版证据。
- 最终定向race三轮1.882s、根包完整race22.516s、vet根包/Web及diff通过；类型检查13.64s、前端50文件300项3.25s通过。Ruby交易门禁9runs58assertions及嵌入14runs27assertions零失败/错误/跳过；compiled embed1.441s通过。初次根包race22.848s通过后vet受缓存权限拒绝；Make前端Vite/PWA构建同步成功后Go缓存权限拒绝（exit2）。获许可后重跑最终完整race/vet/Go构建全链exit0，没有把权限失败称代码失败或Make成功。
- Go+React嵌入技能用于本地构建；最终二进制版本3.111.0-rc982，SHA256 `18295aa29dd7252c61aa50e9e5feea7f16bb65fd20ce8010856c4de0e697a12a`，VCS 5c048825 modified=true，非干净同提交发布验收。二进制/JSON/Markdown `/private/tmp/quantmesh-startup-scope-rc982.4eoA7y/`；旧嵌入备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-9839-zqqu4f/dist` 保留。
- 开放：现货/现货杠杆多个Bot原政策不冲突，manual close尚未参与同一预留，AddRuntime兼容入口绕过预留。已注册Config与全局热更新的完整快照协议、初始化部分金融副作用失败后的耐久持有/补偿、旧身份迁移及跨进程fencing仍需完成；预留释放不表示未知金融意图已核清。未做策略全包/严格MySQL/浏览器E2E或真实账户终态验收。未提交/推送、main/tag、部署、真实账户/生产数据操作；R01–R15不缩减，不宣称实盘或盈利验收。

## rc981：平仓生命周期锁等待可取消（本地验证完成，未提交）

- 独立红测在修复前复现：持有目标Bot生命周期锁，25ms请求期限到期后仍不返回，250ms观察窗口超时；释放锁才退出，测试exit1。原30秒期限在取得生命周期锁后才创建，无法约束等待停止流程。
- 抽取原生命周期mutex获取器，context入口使用同一mutex的TryLock/10ms可取消等待，不启动可能遗留的等待协程；无取消的旧配置调用仍使用阻塞Lock。请求/进程取消上下文和30秒期限在目标筛选前创建，锁等待及金融调用共享期限，取得锁后再次检查取消和原shutdown admission。兼容平仓入口nil adapter安全拒绝。
- 永久测试覆盖真实平仓adapter在锁仍被占用时按请求期限退出且零金融RPC、进程已取消拒绝、取消等待后可重新获取同一锁，以及有竞争后释放锁仍执行managed回调。进程取消测试可能在入口前取消，不冒充确定的执行中取消证明。原Bot生命周期/关闭回归保留；已受理订单不会因context取消自动回滚，配置快照锁等其他阻塞点尚未实现可取消等待。
- 定向race三轮2.330s通过；根包完整race24.225s通过（随后仅增加锁释放正例，该正例纳入最终三轮定向验证）。vet根包/Web通过；前端typecheck35.05s、50文件300项5.42s通过；Ruby交易门禁9runs58assertions及嵌入14runs27assertions零失败/错误/跳过，compiled embed1.224s及diff通过。
- Go+React嵌入技能用于本地构建。Make前端Vite/PWA成功并同步嵌入，后端阶段被本机Go缓存sandbox权限拒绝；获许可后以相同版本ldflags完成Go构建，未把第一次Make exit2称成功。二进制版本3.111.0-rc981，SHA256 `2683991fcec877e6ef7d221cfd7552d0240719b3b6b9836950b713a847d88b34`，VCS 5c048825且modified=true，非干净同提交发布验证。产物及JSON/Markdown报告 `/private/tmp/quantmesh-close-wait-rc981.YIwuoj/`；原嵌入备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-9115-74ps8a/dist` 保留。
- 本轮未验证完整策略包/严格MySQL、浏览器E2E或真实金融终态；跨Bot账户共同保护、所有权/UNKNOWN恢复补偿、全局配置共同事务、跨进程fencing及完整R01–R15仍开放。未提交/推送、未变更main/tag、未部署或访问真实账户/生产数据，不宣称实盘或盈利验收。

## rc980：手动平仓明确市场/Bot选择与请求取消、生命周期复核（本地验证完成，未提交）

- 源码追踪发现旧HTTP处理器只派发exchange/symbol，实际adapter调用resolveMarketType从配置猜市场并使用a.ctx，忽略请求market_type/Bot及请求context。此次是源码接线发现，未把新测试或推断称旧版本独立红测。实际公开 `/api/trading/close-positions` 已指向独立scoped处理器；旧web/api.go处理器保留作为内部兼容代码/旧测试对象，不作为新公开路径验收证据。
- 新实际adapter从BotManager注册集合按请求市场/Bot筛选唯一目标；无market/Bot的旧请求只允许唯一实例，不以首个配置猜市场。同市场多个账户/Bot和跨市场多实例的未明确目标请求拒绝。每个候选Config用configMu读快照，进入共享Bot生命周期锁和shutdown admission后重读注册指针/配置身份；替换、停止或错配不执行。持锁期间仍调用原closeLegacyPositions所有权/耐久执行门禁，没有新增直接账户级下单路径。
- 使用HTTP请求context与30秒超时，并附加进程ctx取消；执行前及等到生命周期锁后都核对取消，不能因等锁后已过期继续触及金融调用。并不声称已提交的金融意图可随HTTP取消回滚；已提交未知结果仍需原耐久日志/核账。HTTP仅接受新scoped接口，旧忽略目标接口503；歧义409，缺目标503，执行未核实500，nil/部分结果503；固定码不回传原provider错误或带成功数量的失败。
- 前端closeAllPositions facade带market_type/可选bot_id并保留同源鉴权；配置两条前置路径和全局看板行/确认框保留market参数，看板平仓loading键区分市场，不显示原后端message。看板聚合行未猜测findBotId的第一个Bot；同市场多实例仍要求独立明确选择流程，可能拒绝旧一键操作，尚未完成该UX。
- 新根包五项测试使用实际BotManager/SymbolRuntime及受控exchange夹具：跨市场歧义、Bot/市场错配、准确目标只触及该目标查询、原未归属库存拒绝、请求/进程取消与shutdown admission、同市场多账户拒绝、合法已确认平账路径。HTTP两项probe验证新参数/context派发、旧provider拒绝及nil/部分/错误不冒充成功；probe不是实际金融执行证明。前端一项实际facade验证市场/Bot编码与一次派发。没有完整浏览器→默认HTTP→真实交易所端到端证据。
- 定向race三轮根包2.180s、Web2.245s通过；最终补进程/锁后取消核验后根包完整race22.327s，Web完整race43.947s通过，最终vet/diff通过。前端类型检查13.28s、50文件300项2.48s通过；Ruby嵌入14runs27assertions/交易门禁9runs58assertions零失败/错误/跳过。非策略全包、严格MySQL/远端CI验收。
- Go+React嵌入技能用于本地Make与compiled embed1.045s，通过；补最终取消检查后同前端清单重建最终Go二进制。版本rc980，SHA256 `ad3918f9f99873724609ed20dc4337285f7a2a8805b05d514384d1cd3f258acc`；两清单SHA256 `4e82089b1797e649d0c81920abf9de5546555038226007212eee25ff6b5ff00e`。仍5c048825 dirty源码/vcs.modified=true，不是干净发布构建。产物/JSON/Markdown `/private/tmp/quantmesh-close-scope-rc980.HQSl78/`，旧嵌入备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-8367-77r20s/dist` 保留。
- 限制：单Bot生命周期锁不排斥另一个Bot在同账户作用域并发启动，既有legacyCloseMu仅排斥手动平仓调用；同账户peer注册/金融核账/执行的共同事务尚未证明。全局配置写入仍可能绕过该锁，恢复就绪、旧身份迁移、跨进程fencing、完整金融费用/借贷/归属证据和R01–R15仍开放；scope选择不是账户归属或真实空仓证明。未提交/推送、main/tag、部署、真实账户或生产数据变更，不宣称实盘/盈利验收。

## rc979：待成交接口不制造空订单证据、保留未知订单与准确市场注册（本地验证完成，未提交）

- 基线隔离实际HTTP处理器四红测终态失败（Web1.230s）：缺provider返回200；指定spot交易对落到无归属默认provider；UNKNOWN/撤单中的槽位被筛掉只剩部分成交；已知空本地槽位orders:null。仅隔离provider/槽位，不连接交易所或下单。
- 待成交接口独立选择provider，不改通用PickPositionProvider的旧兼容行为：完整显式交易对按包含市场的精确键查找，再回退匹配Config身份的实际运行时适配器；缺失、typed nil、nil SPM回503固定码及requires_reconciliation，部分作用域400。无作用域旧默认视图仍可读，但不证明完整账户或指定市场。空本地快照明确[]/count0；UNKNOWN/CANCEL_REQUESTED保留，响应source=local_slots。
- 实际生产接线检查发现动态StartSymbol注册Web provider漏传market_type，默认键为futures；已在main.go注册调用显式传rt.Config.GetMarketType()，与启动初始化的带市场注册一致，spot_margin保留。此为生产源码接线修正；完整动态启动到HTTP的生命周期/多账户/多Bot集成验收未在本轮建立，不能将单条源码和fixture当成该闭环。
- 配置页两条前置路径使用pendingOrderIDsForMutation，非数组、未知/撤单中状态、零/非安全整数ID及重复ID直接拒绝，不再filter(Boolean)后把未知订单当不存在；不自动重试、补单或清日志。新增11项、现前置测试26项通过。只核验本地快照与ID可用性，不证明真实全账户无委托、恢复已完成或撤单终态。
- 九项永久Go测试覆盖缺provider、串默认/错市场、未知状态、空数组、部分scope、nil实现、合法默认/futures视图、spot_margin身份及实际SPM/Web adapter运行时回退。实际SPM夹具只构造本地槽位，不初始化/启动交易或金融RPC。最终定向race三轮2.389s、vet通过；完整Web race44.015s通过，修正main动态注册后根包完整race22.652s通过。类型检查11.83s、前端49文件299项2.45s通过；Ruby嵌入14runs27assertions/交易门禁9runs58assertions无失败/错误/跳过，diff通过。没有策略全包、严格MySQL或浏览器端到端本版证据。
- Go+React嵌入技能用于Make本地构建，compiled embed1.049s通过；之后main市场注册修正完成后在相同前端清单上按同ldflags重建最终Go二进制。版本rc979，最终SHA256 `640033c4a56ce9d45c79db2e5003c76eb7c0fb1a604457368401a108b578d6b1`；两清单SHA256 `7e92e23a12e11e57eeab15ad41e79a46b3c79a8e3a91616a86e21e77c6894f98`。仍基于5c048825 dirty源码，vcs.modified=true，不是干净发布构建；API SetVersion(Version)接线保留，未运行真实服务验证header。产物/JSON/Markdown `/private/tmp/quantmesh-pending-evidence-rc979.Uwf6cj/`，旧嵌入备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-7462-x0bnly/dist` 保留。
- 限制：本地槽位不是所有专用策略/账户/手工订单；相同市场/交易对多Bot与账户身份仍未建立完整作用域。运行时反射身份与热更新并发的快照协议、恢复就绪和真实金融终态核验仍需补齐。旧平仓接口缺market_type/Bot身份、全局写入生命周期门禁、共同事务/回调补偿、跨进程fencing、旧身份迁移及R01–R15仍开放。未提交/推送、main/tag、部署、真实账户/生产数据变更，不宣称实盘或盈利验收。

## rc978：全局配置反馈、方向切换启动门禁与展示页拆分（本地验证完成，未提交）

- 两个JSON updateConfig facade及YAML facade共用专用回执解析，不改变通用请求器：只有固定应用失败409且config_saved=true才按已保存/失败消费，其他拒绝仍抛出。JSON成功回执校验报告字段、唯一Bot归属、failed为空、ok/verified与requires_restart类型；YAML保存/未核实不冒充应用成功。显示文案明确报告仅针对交易热参数，不证明全部全局设置。
- 全局JSON/YAML实际保存入口使用i18n提示，未确认保存保留本地修改/确认窗口，只有已保存才记录保存事件和刷新配置。HTTP拒绝与网络未知分别提示，不把provider错误当可见文案或自动重试。成功IDs不再冒充价格范围更新证明。方向切换前查询订单异常不再当作空数组；保存后只有明确目标bot_id出现在有效applied且无需重启才可能调用原启动入口，其余提示核查后人工使用正常启动入口。交易对旧配置通常无bot_id，故一键方向切换可能停在人工启动阶段；这是能力降级，不把拒绝自动启动称完整方向切换验收。
- 撤单回执success/count不匹配及平仓fail_count非零/缺字段时，不继续保存；对应两条实际调用路径已接线。数量回执只代表API报告，不证明交易所撤单终态、真实空仓或归属核账。实际batchCancelOrders后端count为请求ID长度，交易所adapter逐笔终态未在本轮验证；getPendingOrders缺provider返回200空集合（web/api_pending_orders.go），仍不能证明真实无订单。closeAllPositions旧接口缺market_type/明确Bot身份，旧方向流程的金融生命周期协调和实际持仓核验仍开放，本轮未执行这些金融操作。
- Configuration.tsx由3815行拆至2855行：AI/通知/存储/安全/全局风险及共享展示组件独立文件，父页保留状态和字段写入函数。新props类型收敛；提取出的文案/占位符进入i18n资源。五项静态渲染核对实际配置值、密码路径、共享风险段和只读安全字段，不能当浏览器事件/E2E证明。补丁首次因中间截断读入而不匹配，未写入；重读逐段成功后声明边界错误导致typecheck失败，已修正。新测试首次window缺失失败，按实际transport导入时环境需求修正夹具后通过，无隐瞒失败。
- 最终类型检查15.41s；完整前端49文件288项2.56s，通过。新全局回执/启动门禁24项、前置回执15项、展示静态渲染5项；包含rc977的19项。后端回执定向race三轮根包1.993s/Web6.612s；Ruby嵌入14runs27assertions、交易门禁9runs58assertions均零失败/错误/跳过；diff检查通过。非本版后端全包、严格MySQL、浏览器实际交互或真实金融RPC验收。
- Go+React嵌入技能用于Make本地构建及compiled embed1.088s核验。二进制版本3.111.0-rc978，SHA256 `fb2e79edb1edf79e47f62581326174938a064d6b8a69c521ba892d459bd3902b`；两清单SHA256 `1e2708efa9782a84308763a3da9ef8473d6e2d6bd067dcf1af756983a1a09629`。基于5c048825 dirty源码，vcs.modified=true，不是干净发布构建。产物/JSON/Markdown报告 `/private/tmp/quantmesh-config-receipt-rc978.6TcIFq/`；旧嵌入备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-6521-ivdzja/dist` 保留。
- R01–R15保持完整：配置保存/运行应用共同事务、全局写入生命周期/恢复门禁、专用回调补偿、旧身份迁移、跨进程fencing、实际订单/持仓身份与发布同提交证据仍开放。未提交/推送、main/tag、部署、账户访问或生产数据修改；推送目的地授权仍未收到，不宣称实盘和盈利验收。

## rc977：策略保存页消费保存/应用回执（本地验证完成，未提交）

- 实际 updateBotStrategy facade 使用专用回执解析器；同Bot config_saved=true 且固定应用失败409按已保存/失败消费，其他冲突与鉴权拒绝仍抛出。有效、verified=true、ok=true且目标Bot唯一出现在applied才显示成功；未运行、缺报告、旧式成功、重复/矛盾身份等不冒充应用成功。原始provider内容不进入新提示或回执。
- BotDetail 已保存结果清除脏状态并刷新；失败/未核实使用警告，不自动重试、停止或启动。传输中断提示保存结果未知；保存后刷新异常独立提示，不反称保存失败。新增中文简繁/英文资源，其他语言依现有运行时资源模式回退英文；不是全部语言人工翻译证明。
- 新19项API facade/纯解析/反馈资源测试通过；最终完整前端46文件244项（2.47s）、最终typecheck（18.06s）通过。后端定向race三轮根包2.592s、Web7.342s，覆盖实际adapter和策略/JSON/YAML回执；非后端全包、严格MySQL或浏览器实际交互验收。
- 使用Go+React嵌入技能进行Make本地构建、compiled embed1.126s通过。前后端版本rc977一致，临时二进制版本读回及SHA256 `47e7636e96636326bd2e9a225cf31887502c6af31fc660ee4daaf8d092f562b8`；两清单SHA256 `6eaf4f94c2d62c0c76969b2a4dc46e2dd8444794873a0a920335ee20d87b63ce`。二进制基于5c048825的dirty源码，vcs.modified=true，不是最终提交干净发布构建。报告与产物位于 `/private/tmp/quantmesh-strategy-receipt-rc977.QMAwTX/`。旧嵌入备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-5327-6mp8ti/dist` 保留。
- 全局Configuration.tsx的JSON/YAML回执展示和方向切换自动启动仍未修改；该文件3815行，继续修复必须结构拆分而非扩大巨型文件。保存/应用共同事务、全局生命周期门禁、专用回调部分应用补偿、旧身份迁移、跨进程fencing和R01–R15其余问题保持开放。无推送、main/tag、部署、真实账户或生产数据变更，不宣称实盘/盈利验收。

## rc976：逐Bot应用报告与配置已保存/运行态失败回执（后端本地验证完成）

- 新增结构化report接口并贯通实际BotManager→SymbolManager→Web adapter：applied/failed/not_running逐Bot区分；注册但未初始化不能当作未运行，nil配置不能当已核实。失败只发布固定risk_apply_failed/runtime_uninitialized码，不向API传原始provider错误。旧成功ID接口继续兼容；Web对旧更新器仅调用一次且标未核实，不将其IDs当无失败证明。Verified是本次本地管理器应用结果，不证明实时资产、持续运行或跨实例一致性。
- 策略JSON接口及全局JSON配置接口应用失败409、ok=false、config_saved=true，保留部分成功IDs和失败Bot映射，不假称已回滚保存配置。JSON热重载失败同样反馈409；YAML不扩大原应用行为，热重载失败409，保存正常时明确运行态未核实。仅保存或旧/缺失provider返回200兼容，但ok=false/report.verified=false，不称完成应用。全局配置实际写入门禁仍未补齐。
- 永久回归覆盖实际Web adapter混合成功/失败/未运行及原始错误不进入序列化结果；SQLite真实策略处理器保存后部分失败、正常旧dispatch/缺provider未核实；JSON配置逐Bot失败与管理器配置读回（本轮未另从数据库直读）；真实HotReloader回调故障下JSON/YAML持久化调用返回成功后的内存配置读回和固定错误码。没有浏览器或真实金融RPC，不把fixture report当默认HTTP到实际交易实例E2E。
- 初次同名代码块补丁定位错，将局部变量放到其他函数，编译失败后修正；新增配置测试未先创建Bot而数组越界，改成明确Bot ID夹具后复验。记录失败，不用后续vet退出0掩盖先前失败测试。
- 最终生产源码完整race根包22.558s、Web46.859s、配置1.700s终态通过，vet/diff通过；实际adapter/策略/三个反馈入口定向三轮根包2.073s、Web16.929s；之后补充全局JSON逐Bot失败测试及反馈回归三轮Web6.514s通过。Yarn verify整链26.21s、45文件225项；Ruby嵌入14runs27assertions/交易9runs58assertions零失败/错误/跳过。未继承旧策略全包或严格MySQL/远端CI验收。
- Go+React嵌入技能用于本地Make与compiled embed1.138s通过，版本rc976、API版本接线保持；两清单SHA256 `7057a662b7c71f3e81d877d30502758c698c799fcbf57c4a0ddc617abf5c192b`。隔离产物/JSON/Markdown报告 `/private/tmp/quantmesh-runtime-report-rc976.4d16z1/`，提交后重建的实际VCS revision/modified与产物摘要以报告读回为准，不冒充干净发布产物。旧嵌入备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-4252-lwtnb5/dist` 保留，不覆盖运行程序。
- 未改前端状态消费：现有页面可能仍按HTTP200或通用409显示反馈，需补i18n的已保存/部分应用/未核实展示及防误重试并做浏览器验证。保存/应用共同事务、专用回调部分应用补偿、全局写入生命周期/恢复门禁、旧身份迁移和跨进程fencing仍开放。R01–R15范围保持；未改main/tag、推送、部署、真实账户或生产数据，推送目的地批准仍未收到，不宣称实盘/盈利验收。

## rc975：实际运行时热更新不跳过专用回调、不在风控失败前发布参数（本地验证完成）

- 在75532c41基线实际BotManager/SPM隔离overlay复现两红测：无SPM专用实例的UpdateOpenControl调用数0、未触发失败封锁；注入无限verifiedCapitalBudget使网格风控拒绝时，管理器旧interval100仍在，但SPM/内层已变200、order_quantity250且返回已更新ID。该预算是故障注入，不宣称线上实际预算无限；其invalid-budget gate已封锁，不据此声称真实违规订单发生。正常网格控制通过。原根包红测退出1、1.178s。
- 提取实际更新函数至bot_manager_runtime_params.go，避免继续扩大已超过2000行的管理器文件。逐实例在configMu内先调用真实publishRiskControlsLocked；失败恢复原管理器配置并立即退出，不继续更新SPM/内层，也不列入成功IDs。无SPM但有UpdateOpenControl的专用实例实际调用回调，失败保留旧配置/封锁，成功重试发布新控制并仅清除自身失败封锁。内层配置从已限额的最终管理器配置生成，而非未经预算夹紧的输入。
- 新永久回归使用实际管理器、实际SPM及受控专用回调，未连接交易所或提交订单：失败拒绝、各层旧参数、专用成功重试、正常网格均通过。定向原风控/新热更新三轮14.409s；增加重试后新三项三轮1.823s。最终根包完整race24.489s、Web完整race93.415s、vet/diff均终态通过；未将旧版策略全包/严格MySQL/远端CI当本版证据。
- Yarn verify整链终态57.90s，45文件225项；Ruby嵌入14runs27assertions/交易9runs58assertions零失败/错误/跳过。Go+React嵌入技能用于本地Make构建及compiled embed1.086s，通过；临时二进制--version为3.111.0-rc975，API版本接线保持。两清单SHA256 `6f80f5bf2397e559165decde471b0c5bb048cc33757671f5f6a51854be6c8908`，旧嵌入备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-3151-k54bz9/dist` 保留。隔离产物/JSON/Markdown读回目录 `/private/tmp/quantmesh-runtime-apply-rc975.OMqeVn/`，提交后额外二进制重建的实际SHA与摘要以报告为准；不覆盖运行程序。
- 限制：旧接口仅返回成功IDs，失败原因仍未逐Bot回传给HTTP；配置保存、风控回调与多实例应用不是共同事务，专用回调自身部分应用的补偿也未证明。更新成功IDs表示实际应用成功，不限价格字段改变；不是配置有记录就称应用成功。其他全局写入口/热更新的生命周期协调、实时金融核账、旧日志身份迁移与跨进程fencing仍开放。下一步应提供结构化逐Bot结果并将保存/应用状态明确反映到API，不能仅靠日志或空IDs让调用方猜测。
- R01–R15完整目标保持；未改main/tag、推送、部署、生产数据或真实账户，目的地批准仍未收到。没有真实实盘或盈利验收，也没有默认浏览器端到端应用证明。

## rc974：策略写入生命周期保护与快照冲突核验（本地验证完成）

- 实際BotManager/适配器新增策略写入协调：复用同一生命周期锁和shutdown admission，回调得到注册表managed事实，不因受管但不交易而当作已停止。旧整份配置门禁仍拒绝受管实例。策略API在该锁内读最新配置；策略列表（含参数/权重）、方向、库存策略发生变化时，受管实例拒绝409；非受管实例核验钱包预留及全部金融日志，未核清409/不足证据503。不会自动Stop或清除原账本。
- 智能挂单等既有非契约字段仍可写入/派发热更新。永久HTTP/SQLite夹具确认pending账本受管时智能挂单参数200、配置读回与updater派发、原金融payload不变；这是fixture派发证明，不是默认运行时应用成功或收益证明。策略定义/权重原先仅持久化而不完整热重构，本轮要求停止后变更，不能将其统称正常热调整。
- 写入使用配置锁内完整快照比较：核验期间配置变化则409，不覆盖新配置；保留原put_bot_strategy历史source，持久化后才派发热更新。此为单进程CAS，不证明数据库跨进程fencing或配置/金融日志/多文档共同事务。旧核清日志更换身份后的迁移仍未实现，不能把允许修改称作下一次启动恢复必然成功。
- 原SpotShort借款group/symbol两红测均409、身份不变、日志原样、仍Required；永久回归覆盖正常flat/absent、未知scope/旧策略、缺协调器、受管契约拒绝、真实管理器注册状态、并发配置变化，以及取消/存储失败不更新内存或派发。根包原生命周期启动排他回归由同一新底层实现继续覆盖；尚未构成默认HTTP到完整StartBot/浏览器E2E。
- 首轮新增热更新测试误在通用Storage接口调用可选读取方法而编译失败，改用实际SQLStorage；并发测试最初注入会被Validate归一化还原的legacy交易字段，未制造真实配置变化而失败，改为可持久化Name并读回后通过。没有隐藏失败或将原无效并发夹具算证据。
- 最终生产源码完整race根包22.496s、Web75.569s、配置2.809s完成通过，vet/diff通过；前一轮定向含全部原四入口及新增策略/生命周期三轮根包2.303s、Web42.110s通过，后来新增持久化/取消测试再次全部策略写回归三轮11.767s通过。Yarn verify45文件225项、Ruby嵌入14runs27assertions/交易9runs58assertions均通过；没有继承rc973的策略全包结果作本版验收。
- Go+React嵌入技能用于本地Make构建与compiled embed1.049s，之后在不变前端清单上重建最终后端；版本rc974、API版本接线保持。产物 `/private/tmp/quantmesh-strategy-guard-rc974.yntWib/quantmesh` SHA256 `0207722ebbe05af93bc277435a4a7be1330579c78bb2ba573b4e555bf12ef9c9`；两清单SHA256 `3ff47ebd47869d8105d4f3672ddc3117b39c57b67052b7dcb7890d4f0264169a`，214资产，JSON/Markdown报告同目录。构建基于76290c32的dirty源码，非最终提交发布构建；旧嵌入备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-1810-8730uv/dist` 保留，不覆盖运行程序。
- 仍需核清旧日志身份迁移、非Binance资产元数据、缺历史定义恢复、其他配置/热更新门禁、运行时更新失败明确反馈、跨进程原子保护；已持久化后updater接口仅返回IDs，不能证明每个运行时已成功应用。未运行严格MySQL/远端CI/浏览器E2E，不改main/tag、推送、部署、真实账户或生产数据；推送仍待目的地批准。R01–R15完整目标保持，实盘和盈利未验收。

## rc973后续复查：策略写入口仍可改掉待核账恢复身份（红测已复现，未修复）

- 独立SQLite overlay两项安全断言失败：同类型SpotShort策略修改group_id/symbol均HTTP200，主配置实际改变且原schema9待借款payload保留；修改前rc973门禁为Required，修改后Unverified。钱包预留确认不存在，不能依赖预留保护这个路径。
- [专项证据和修复验收要求](2026-10-03-strategy-write-recovery-review.md)。这是四入口之外的真实缺口，不扩大rc973已验证范围；下一步需生命周期内最新配置/完整金融状态核验及原子写入策略，同时保留运行中正常风险调整，避免粗暴封锁带来的能力退化。
- 本轮只有审查文档和临时隔离测试，未改生产源码/版本/main/tag、推送、部署或真实账户。两项失败不作验收通过，完整目标保持。

## rc973：四入口生命周期内金融日志门禁（本地验证完成）

- 配置文件PUT/DELETE、Bot DELETE、组DELETE在既有生命周期锁/有序停止协调回调内，从同一主存储完整读取全部Bot策略记录，以独立配置身份核验每条。有效未完成经济游标409；读取失败/取消、nil、未知策略/schema、缺历史定义、错账户及重复Bot身份503。合法平账与确认无记录正常放行。组内所有成员先核验，再开始配置删除；不释放钱包预留或改写金融payload。
- 普通策略复用生产归一化与重复检查；对冲按实际直接读取Bot实例路径绑定group/symbol，基础币仍依据适配器所用Bot符号。Combo显式Bot symbol覆盖raw参数；自定义旧子定义缺失保持未核实，不从金融payload猜测。账户摘要共享原运行时协议。离线基础币仅支持明确Binance稳定币符号契约，不是实时交易所元数据/库存证明。
- 隔离SQLite永久回归使用真实HTTP处理器和生命周期fixture provider，匹配账户身份的pending/历史剩余资产先单独证明ErrRecoveryConfigRequired，四入口409并读回原主配置、文档及payload；错误/旧证据503，合法平账/无记录200。覆盖组第二成员待核账不能先删第一成员、锁内晚到记录、取消/读失败及重复Bot身份。不冒充默认BotManager全流程或浏览器E2E。
- 首轮失败是通用测试夹具附带非马丁格尔无关Direction，以及legacy环境Bot下标/总数假设；修正为契约和明确ID、前后读回后通过。新增对冲测试首次误用不存在snapshot方法编译失败，改用真实持久化序列化后复验；未隐藏失败。
- 最终策略完整race139.869s、Web完整race40.906s、最终生产源码根包22.532s/配置1.849s通过；绑定/四入口三轮race策略2.149s、Web28.695s通过，随后新增重复身份夹具包含在最终Web完整结果。vet/diff通过；Yarn verify整链通过，独立dot读回45文件225项；Ruby嵌入14runs27assertions/交易9runs58assertions均无失败、错误、跳过。原八项overlay策略1.882s/Web4.389s通过，但其拒绝来自scope不匹配503，不代替匹配身份409证明。
- Go+React嵌入技能用于本地构建：Make通过后在稳定清单上重建最终后端，compiled embed1.105s通过，临时二进制--version为3.111.0-rc973，API版本接线保持。报告 `/private/tmp/quantmesh-config-gate-rc973.Jj4sc0/results.{json,md}`；二进制SHA256 `3e33d5ff6689bd535b5a6c35c2d811186fa78268a66a7bdf03d86bd3b53e5a88`，两前端清单SHA256 `cf048a788232575413d07f517af2ec9ea51f839a0ba18970f58153c157fb65ed`，214资产。构建基于b6dff013的dirty源码，非最终提交发布构建。旧嵌入备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-98245-kj2ere/dist` 保留，无运行程序覆盖。
- 尚缺其他配置/策略参数写入及热更新门禁、非Binance独立资产元数据、移除自定义定义的恢复能力、双文档原子持久化及跨进程fencing。下一步沿实际写入链复现绕过并补恢复能力，不能以长期封锁代替闭环；R01–R15目标保持。未运行本版严格MySQL/远端CI/浏览器E2E，不继承旧证据；不改main/tag、推送、部署、真实账户或生产数据，目的地批准仍未收到，不宣称实盘或盈利验收。

## rc972：对冲日志与完整Bot记录集合核验（生产入口尚未接线）

- 补齐SpotLong、SpotShort、FuturesLong/Short和Combo父记录只读核验。现货待办订单/借款/买回/还款、期货预提交订单不能被日志缺失之外的零值掩盖；已确认正ID的消费还款历史允许保留，不以有历史记录永久阻止正常平账。未知schema、缺字段/null、错group/资产/身份、非法消费还款ID不返回已核清；非空待办只要求保留配置，不依赖此处把其恢复证据验成可交易。
- 新增完整Bot状态集合核验，显式类型/独立绑定分派全部13种受支持类型/别名，所有记录均需核验；不按当前enabled配置过滤，也不根据JSON形状猜旧策略类型。nil集合是不可用，non-nil空集合是调用方已确认无记录；拒绝nil/重复/跨Bot记录及缺失/重复绑定。类型和绑定来自独立配置/身份的契约仍须实际生产解析器接线，函数自身不能证明调用方绑定来源可信或传入没有截断的集合；必须使用rc969完整Bot读取，不能把任意部分列表作证。
- Combo子键复用实际producer的父名SHA256前16字节命名规则，提取为共同纯函数且保持原读取/写入校验与128字节限制。子记录要求父类型/身份/symbol及实际父记录存在，不允许嵌套父关系或不支持的子类型；无论输入顺序父先/子先，父状态正常不能遮蔽子待办。无记录只证明该完整快照缺少耐久日志，父记录单独并非子库存/实时账户证明。
- 全部RecoveryConfig定向三轮race2.332s通过：实际SpotLong/Short及期货tracker保存流程，SpotShort历史还款/已借未完成、期货预提交，13类分派正常及schema拒绝；真实临时SQLite105条旧signal记录末端待办/漏绑定拒绝，全部核清允许，数据库直接读回未改原payload；Combo六类子策略正常、未完成、顺序、缺父/错父保护；nil/重复/跨Bot/未知类型和已取消context拒绝。首次fixture给lazy nil消费映射直接赋值导致panic，初始化测试映射后重跑，不改生产构造行为。中途context取消未独立注入，不将预取消测试扩大为该证据。
- 最终源码完整strategy race139.750s完成通过，JSON/Markdown终态报告位于上述临时目录results.{json,md}；不将运行中测试视为通过，不继承旧版回归记录。
- Yarn verify及独立dot读回45文件225项通过；Go+React嵌入技能用于前端先构建、Make临时二进制及compiled embed1.066s通过，API版本header原接线保持，前后端rc972。vet/diff和Ruby嵌入14runs27assertions/交易9runs58assertions通过。目录 `/private/tmp/quantmesh-state-collection-rc972.oJAYgg/`；二进制SHA256 `d9760251784e29aecdf53b412db66fc6dbb5cad4e3da2a63f0f80ff3b942d99c`，两清单SHA256 `ca8d4831e1b723fa508028baf8134c768127db93cd2aaaa60574aa4d8eda5973`，214资产；旧嵌入备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-94722-dsckbo/dist` 保留，未覆盖运行程序。
- 当前SQLite套利overlay兼容race1.809s通过；四入口八项安全断言仍失败（Web3.063s，退出1）。新增集合函数尚无生产绑定解析器、生命周期内完整读取或四入口调用；不能称真实保护已经生效。下一步必须补独立绑定解析（含旧/禁用记录及缺失绑定保持恢复能力）、默认管理器/四入口同一核验接线、正常核清/失败放行边界与数据库读回、永久回归及E2E；其他配置写路径、旧schema完整核账与跨进程fencing也未闭合。
- 此处订单/借贷日志核清不证明实时现货、保证金资产/负债、当前所有权、资本释放或盈利；不继承旧严格MySQL/远端CI/目标平台。构建基于95176be1上的dirty源码，非发布提交。R01–R15完整目标保持，未改main/tag、推送、部署或访问生产数据/真实账户；推送目的地人工批准仍未收到。

## rc971：DCA/马丁格尔/信号耐久平账的配置保留判断（尚未接入四入口）

- 新增三类只读纯函数，要求独立Bot/策略名/symbol（马丁格尔另要求LONG/SHORT），使用真实私有schema及既有资本释放空仓判断检查条目、平仓意图、成交/费用游标和精确非零数量；不能用空仓标志或策略仍在配置中作证。允许自定义/Combo底层子名称，但记录键与Combo命名空间、类型映射的集合核验尚未实现。
- 实际DCA/马丁格尔serializer将空层/条目保存为null，不再用通用null禁止规则误判合法平账；只在对应root集合允许，嵌套null条目、进度/统计null均拒绝。统计与close_progress要求真实Go序列化的完整字段（Quantity/Notional、TotalTrades/WinRate/TotalPnL/TotalVolume）；缺失、重复键/大小写覆盖、尾随或未知数据不能被零值补成平账。此前套利null拒绝规则保持。
- 该核验证明耐久经济游标没有未处理内容，不证明当前账户库存或资本释放；非空条目/持仓/订单等直接保留配置，不靠这一步完成其有效性核账或下单恢复。无效/不足证据与需保留恢复配置以既有sentinel区分。正常暂停、历史亏损/利润、合法空集合不永久阻止平账；旧schema不能冒充当前证据，后续仍须明确兼容核账能力。
- 定向全部RecoveryConfig测试三轮race1.629s通过：真实三个持久化producer保存并读回合法平账，包含暂停、历史亏损及自定义子名；UNKNOWN条目、未完成订单/平仓/费用游标、负平仓损益、最小正数保持拒绝；错Bot/策略/symbol、未知schema、嵌套缺字段/null、nil条目、重复统计键等不足证据拒绝。纯函数仅收字符串和绑定，不接真实交易、存储写入或金融RPC，原payload不变。
- 最终源码策略完整race139.604s完成通过；前端verify与独立dot读回均45文件225项通过。不将运行中测试算作终态，不继承旧版本通过记录。
- 前端Yarn verify完成、Vite/PWA构建通过；Go+React嵌入技能用于先前端后临时二进制，Make和compiled embed1.130s通过，API原版本header接线保持，前后端rc971一致。vet/diff通过，Ruby嵌入14runs27assertions/交易9runs58assertions通过。临时产物与JSON/Markdown报告目录 `/private/tmp/quantmesh-single-leg-proof-rc971.Nt5BBJ/`，二进制SHA256 `8bee3b898211c0a9492acc1b18d2f6336a08aa1c5c3c29bdefbd30397a842f81`，两清单SHA256 `b84b0c710ad197c05db946a968284f4b5170e584fff473cd807b4e234a9e14db`，214资产；旧嵌入可恢复备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-92932-kmtypw/dist` 保留，不覆盖运行程序。
- 原临时SQLite套利overlay兼容race1.771s通过；四入口八项安全断言仍失败（Web3.449s、退出1），金融payload不变但配置依然可被改掉。现货/期货对冲、Combo父子记录映射、所有Bot记录的独立绑定、旧schema核账与四入口统一接线、其他写路径、正常核清放行、默认adapter/E2E与跨进程fencing均未闭合，不宣称本轮修复开放入口缺陷。
- 不继承旧严格MySQL/远端CI/目标发布证据；本地构建为c8d5d166上的dirty源码，非最终发布提交。R01–R15完整目标保持活跃，未改main/tag、推送、部署或访问生产库/真实账户；推送目的地人工批准仍未收到。

## rc970：套利耐久状态的只读配置保留核验（尚未接入入口）

- 新增 Funding Carry / Funding Perp Spread 纯函数，调用既有实际恢复解码器及借贷、成交费用/确认消耗校验；要求独立非空身份绑定，Carry 明确账户 scope 和基础币。已核清耐久记录返回 nil，需要核账与无效证据通过不同 sentinel 错误及 errors.Is 区分，不输出完整金融 payload。
- Carry 在正常本金/仓位清零后仍精确核算历史余量；0.0008 及 nextafter 微量正余量不得消失。双永续额外核查 execution ledger、emergency close、pending order/executions 和两腿有符号数量，不能仅靠零仓位证明核清。只是耐久账本判断，不证明实时资产、可支用余额、账户所有权或释放资金安全。
- 对当前 schema6 的完整序列化字段核验：缺失基础字段、null、任何层级重复键（含大小写覆盖）、未知字段、尾随数据拒绝；采用深度上限。旧schema、未知schema及缺少独立绑定不返回已核清。旧schema后续须明确兼容核账路径，不能让拒绝永久替代恢复能力；当前函数未投入生产配置准入，不引入旧Bot永久禁止修改的入口行为。
- 最终定向三轮 race 1.812s 通过，包含合法零差额、两类余量、待办标志、错账户/资产、旧schema、缺字段/null/重复键、未知字段，双永续未核实账本、紧急平仓、合法 pending order/execution、负仓位和最小正数。首轮旧夹具交易所名为空导致拒绝，补齐夹具名称后重跑，不放宽生产校验。输入为字符串和绑定，不接交易所、存储写入或金融RPC。
- 策略包完整race139.884s完成通过，使用最终生产实现；全量运行期间仅追加测试覆盖，新增断言已由随后定向三轮验证，不将运行中测试算通过。JSON/Markdown报告在 `/private/tmp/quantmesh-recovery-proof-rc970.yGiuH0/results.{json,md}`。
- Yarn verify 45文件225项及Vite/PWA完成；Go+React嵌入技能用于前端先构建、清单sync/verify及临时二进制构建，编译内嵌测试1.082s通过。首次Go缓存权限阻止构建/vet，原命令允许环境重跑通过；Ruby嵌入14runs/27assertions与交易9runs/58assertions通过，最终vet/diff检查通过。二进制SHA256 `414b91d7677d27e4abdc0827ab445f314677e402e6f43319ce84e65a689febd3`，两清单SHA256 `a5106bf0f1cfe2e9a8c1a700b58fa09c427ab2ef8569de2b44503aff66f75034`，214资产；旧嵌入保留于 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-90994-1npws/dist`。
- 真实SQLite原overlay兼容性1.750s通过，四入口两种状态仍8项安全断言失败（Web3.068s，退出1），金融payload保持但配置依然可变。此轮不得称四入口缺陷已修复；尚须所有策略schema及旧记录身份核验、读取接口/管理器/四入口统一接线、正常核清放行、默认适配器及浏览器E2E、跨进程fencing。R01–R15完整范围保持。
- 没有本版严格MySQL、远端CI、同提交发布或实盘盈利证据；构建验证基于dd9cbc5b上的dirty源码，非发布提交。未改main/tag、推送、部署、真实账户或生产数据；推送目的地批准仍未收到。

## rc969：统一金融状态保护前的完整Bot记录读取（存储步骤本地验证完成）

- 新增可选BotStrategyRuntimeStateContextLister及SQL实现：按参数化Bot身份读取全部策略记录，不依赖现配置策略名，不跨Bot，不设静默分页截断；保留strategy/schema/payload/updated_at并按strategy稳定排序。已知无记录返回非nil空slice；取消、连接等待、查询/扫描/迭代/关闭失败返回错误和nil，不能拿先前部分记录证明全部核清。使用既有表/主键，不新增DDL或改旧读取接口。
- 最终定向三轮race2.368s通过：105记录完整性、旧策略/跨Bot隔离、参数化SQL、原字段保持、等待单连接取消及正常恢复，真实SQLite第二行损坏timestamp证明首行扫描成功后的失败不会暴露部分记录。夹具清理错误不吞掉；最后校准后重新跑定向和全量。关闭驱动错误/中途迭代取消尚未独立注入，不能以逻辑已接入冒充逐分支证据。
- 最终根包/存储完整race24.316s/12.658s，857 pass测试事件、8 MySQL skip、零fail；事件数包含父测试，不作为独立叶子数量。两包vet/diff检查通过。当前源码另重跑既有overlay：两类有效金融夹具通过、八项API安全断言仍失败（go退出1），直接读回确认新接口尚未保护配置入口，不掩盖开放缺陷。
- Yarn verify完整链45文件225项及Vite PWA、Ruby嵌入14runs/27assertions与交易9runs/58assertions完成通过。按Go+React嵌入流程Make、清单verify、编译内嵌字节1.561s通过；临时二进制版本3.111.0-rc969。最终夹具清理校准只改测试，不改生产二进制源码。报告 `/private/tmp/quantmesh-bot-state-reader-rc969.Pd2dk8/results.{json,md}`；二进制SHA256 `9a8d7029922d7877123fe6e681b23523b62044d5145ed082bd4d8a09d01264c7`，两份前端清单SHA256 `133d71862496697762597e22eafa2205129b4316b9a2d79d4c5a7058b46759d9`，214资产。验证于HEADa2ca4b08的dirty源码，非最终发布提交构建；旧嵌入备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-88712-15n2fq/dist` 保留，不覆盖运行程序。
- 这是原统一保护方案的必要存储步骤；未以只读接口替代金融语义核验和四入口接线，八项开放红测仍未修复。各策略正常核清/UNKNOWN/借贷/历史资产schema核验、缺失或未知数据拒绝、配置恢复状态保持、跨实例fencing及R01–R15完整范围均继续保留，版本前后端rc969一致。
- 没有本版严格MySQL、远端CI、目标平台或默认适配器/浏览器E2E证明，不继承旧数据库验收；未改main/tag/推送/发布/部署，目的地推送确认仍未收到，目标保持活跃而非完成/阻塞。

## rc968后续审查：无预留但有金融状态时仍可丢失恢复配置（已复现，未修复）

- 基线精确为7218cb432a03fd6dd85e80327a6ba6cdea0a88eb，无业务源码改动。独立overlay和临时SQLite确认两类有效schema6金融状态：未完成操作与标志已清零但历史剩余0.0008BTC；真实恢复解码器接受恢复模式，严格模式均拒绝，夹具兼容race1.764s通过。初轮成交字段用错JSON格式导致无效，校准后重跑全部证据，未忽略该失败。
- 八项API红测真实读回：确认无预留的两类状态均可通过PUT config-file、DELETE config-file、DELETE Bot、DELETE group返回200，金融payload不变但配置文档变化；PUT/Bot删除/组删除同时改变或移除主身份，配置文档删除单项仍有主配置fallback。八项安全断言失败是开放缺陷而非回归通过，也不是线上已经丢失数据的证明。
- 详细范围、下一步schema核验/全Bot耐久记录读取/四入口一致保护/正常核清放行要求见[专项复查](2026-10-03-orphan-runtime-state-config-review.md)，JSON/Markdown与可重跑overlay在 `/private/tmp/quantmesh-orphan-state-audit.SuqaY2/`。生命周期提供者为隔离夹具，不冒充默认适配器HTTP/浏览器E2E。仅追加审查文档，不递增业务版本；R01–R15目标保持，不标完成/阻塞，未改main/tag/推送/生产账户。

## rc968：配置文件入口不得抹掉持有预留的失败实例恢复配置（本地验证完成）

- 真实临时SQLite红测确认：停止/未受管实例有50USDT钱包预留，PUT整份配置和DELETE配置文档仍200；旧代码明确进入覆盖BTCUSDT为ETHUSDT/删除分支。红测在状态断言处失败，不把成功日志冒充红测数据库变化读回。现有Bot删除保护并未覆盖这两个配置文件入口，不以“启动失败”推定无资产。
- 新增明确生命周期协调接口，实际BotManager和Web适配器接线；同一Bot启动/停止和配置核查/写入共用生命周期锁，进程退出拒绝新修改。存在受管实例（即使非交易循环）直接拒绝，不自动Stop；Web在锁内重新核查实例及持久化预留，核查失败或缺少协调接口503，有预留409，确认无预留时保留原更新/删除行为。
- 最终定向三轮race根包3.092s/Web12.554s，根包和Web完整race28.588s/61.723s、两包vet/diff检查终态通过。修复后真实SQLite读回确认原配置文档Content、主Bot身份及50USDT预留不变；新预留/实例在协调准入期间出现也拒绝，存储查询失败和缺少协调器503，已停止且确认无预留的正常操作200并读回。真实BotManager启动验证被配置持久化锁排除，拒绝受管实例且零Stop调用；退出拒绝及失败回调解锁通过。Web夹具协调器与真实管理器分别验证，不冒充默认适配器完整HTTP/浏览器E2E。
- Yarn verify完整链45文件225测试、Ruby嵌入门禁14runs/27assertions、交易门禁9runs/58assertions通过；按Go+React嵌入流程Make构建、清单verify、编译内嵌字节1.579s完成。报告 `/private/tmp/quantmesh-config-recovery-rc968.PBulMJ/results.{json,md}`；二进制版本3.111.0-rc968，SHA256 `80916e1fc12f1a017a606daec56570bcbfbbff6330c49b5e7161fb5661972b77`，两份前端清单SHA256 `b09c36b6a7605c53dfd071dcffa8107716dcc15f4487cb3035630738bafd61f2`，214资产。验证于HEAD92216f5e上dirty源码，不是最终发布提交构建。旧嵌入备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-87305-jogadv/dist` 保留，无运行程序覆盖。
- 改动限于PUT/DELETE config-file及协调接线；其他策略参数入口、全局配置/热更新、跨实例数据库原子fencing、没有预留但有金融状态的独立保护及配置双文档原子持久化仍需审查，不宣称全配置写路径或R09完成。不继承严格MySQL/远端CI/目标平台证据；未删生产数据/真实账户，未改main/tag/推送，推送目的地确认仍未收到。

## rc967：前置失败不能隐藏历史钱包预留（本地验证完成）

- 真实临时SQLite完整构造器先建立受管核账实例，再停止并读回三钱包各50USDT和原schema6金融payload；重试预检失败/已取消两个红测均复现只有普通原错误，历史预留未出现在返回诊断。
- 有持久化服务且有效构造器输入时，在全部失败清理完成后只读核查同一Bot历史预留。取消路径使用独立五秒context；持有预留、读取失败或接口缺失加入结构化未核实诊断，原失败/读取原因仍可errors.Is追溯。确认无预留原样返回，已有保留诊断不重复查询；成功路径不查询。nil存储及无效构造参数不承诺历史存储核验。
- 不建额外交易连接、不释放/改写预留或金融payload，不将前置失败准入为受管核账运行时；该查询是当时数据库快照，不证明资产已平仓或跨实例原子fencing。完整失败实例管理接管仍未完成。
- 定向三轮race3.416s、根包完整race25.424s、根包vet/diff检查终态通过，覆盖真实构造器重试、取消独立读取、读失败/接口缺失及准入拒绝。Yarn verify整条链及独立dot报告45文件225项通过；Ruby嵌入门禁14runs/27assertions、交易门禁9runs/58assertions全部通过。按Go+React嵌入流程完成Make构建、磁盘清单验证及编译内嵌字节测试1.710s，临时二进制版本3.111.0-rc967。
- JSON/Markdown报告位于 `/private/tmp/quantmesh-startup-audit-rc967.zt24tZ/results.{json,md}`；二进制SHA256 `f3c21b6738cacf33765630bb8eed0dafb1c9e389745f39cd152aa44c8c89189c`，两份前端清单SHA256 `a6d55009abe665417e118f88e4ea051bd51e59ca21ecc2c1fe8b81c5502a1295`、214资产。验证时HEAD4721d247且源码dirty，非最终提交/发布构建；旧嵌入备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-86270-rce16e/dist` 保留。元数据读回脚本首次用错键失败，改用实际files字段后读回/verify完成，不掩盖失败。
- 未运行本版严格MySQL、远端CI或浏览器E2E，不继承rc966/旧严格证据；R01–R15范围保持，不改main/tag/部署/真实账户，推送目的地确认仍未收到。

## rc966：完整构造器的剩余资产接管与清理保留诊断（本地验证完成）

- 不再只测试准入辅助函数：实际生产构造器委托实现接入隔离预检/交易所和t.TempDir SQLite，真实配置、钱包观测序号、三钱包预留、执行意图后端、策略耐久适配器及PriceMonitor均运行。成功的历史剩余资产路径可返回SymbolRuntime并导入0.0008 BTC历史量；不启交易循环，StopWithError拒绝平仓/资金释放，三钱包各50USDT及schema6原始金融payload均保持。
- 将实际构造结果加入真实BotManager的AddRuntime注册表并读出Bot状态映射，确认“受管但不交易且待核账”；这是注册接线，不冒充默认StartBot自动构造入口或浏览器验收。夹具明确拒绝并计数Place/Cancel/Borrow/Repay/Transfer，核验零金融RPC及价格/订单流清理。
- 错账户完整构造器红测读回：三项预留和原始金融payload确实被保留、没有金融变更，但返回错误仅含原启动原因，清理拒绝释放仅在日志。新增fundingCarryStartupRetentionError并与原错误Join，覆盖资金核清失败、策略停止失败及所有权丢失/冻结失败；不准入为正常核账实例，不自动释放资金或接管错账户。
- 诊断措辞为“预留释放未核实”，不把存储提交报错当作已读回预留必然存在；错账户/停止失败夹具另以数据库实际读回证明三项预留仍在。单原因包装的保留诊断同样拒绝准入，不仅依赖外层Join。其他所有权丢失/策略停止失败分支接入同类型返回，但尚缺逐分支完整构造器故障注入证明。
- 最终定向三轮race根包4.031s、根包完整race29.528s终态通过，根包vet、diff检查通过；包含实际临时SQLite构造器与原取消/所有权/受管准入回归。错误账户的原启动原因及结构化释放未核实原因均返回，原payload不变，三个钱包预留真实读回均为50USDT；单原因诊断包装也不能恢复准入。没有以日志代替数据库读回。
- Yarn verify整条链条成功，类型检查/测试/Vite PWA构建通过；独立dot报告读回45文件/225项测试全部通过。Ruby嵌入门禁14runs/27assertions、原交易门禁9runs/58assertions均零失败/错误/跳过。完整Make构建成功；最终诊断措辞校准后，在前端清单不变且核验通过的条件下重新go build最终源码，--version返回3.111.0-rc966，编译内嵌字节测试2.297s通过。
- 最终二进制 `/private/tmp/quantmesh-constructor-recovery-rc966.gOnIBM/quantmesh` SHA256 `3cd3f899add06887a3414d0eaa8ef98dcbd5767c6b2e94608e55743bb1c6d9a9`；两份前端清单均为 `6f17c818a8116777602c84971e67f5b6089e241770485eb81178c9175a64ebf3`（214资产），来源摘要 `3c41d8444198b1c13fc30e22563a4b8a1a8e691d7846c8d1c1be9acdbe7c282f`。构建于HEAD45856458的dirty源码，非发布提交构建。JSON/Markdown在上述专用目录；上一轮嵌入目录保留于 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-85253-3gmc6a/dist`，无现有运行程序覆盖。
- 完整前置失败管理接管、当前库存/资产处置、UNKNOWN/部分成交补偿、原子fencing和R01–R15其余要求仍未完成；本轮SQLite不冒充严格MySQL/跨进程行锁证明，不继承旧数据库/远端CI或盈利证据，不改main/tag/发布/部署，推送仍待目的地确认。

## rc965：套利运行时不能写入其他 Bot 的策略 map（本地验证完成）

- 继续核对 R09 生产初始化/恢复前提时发现两个专用构造器均浅拷贝 baseCfg，再直接写共享 Strategies.Configs。串行红测确认 Funding Carry/Perp Spread 都改变基础配置；Funding Carry 真实构造器入口在注入工厂失败后，仍覆写共享策略权重/启用/参数。未用标题或race未复现臆测问题。
- 两处合并先 maps.Clone 外层 map，再写当前 Bot 的专用条目，保留无关策略及原 Bot 参数选择/默认权重。该路径仅读取条目内Config，没有宣称所有嵌套值深拷贝或同时修改基础配置的热更新已安全；全构造器成功路径、Perp Spread全入口及失败实例接管仍待验证。
- 双入口合并、失败Funding Carry构造器、24个并发本地合并及原取消/受管准入回归连续三轮race终态通过（根包2.349s）；根包完整race23.962s、根包vet和diff检查通过。并发夹具仅并发合并不可变基础配置，不用于证明热更新同时写入基础map的安全性；Perp Spread只覆盖同生产合并函数，不冒充其全构造器成功/恢复。
- Yarn verify整条命令成功，类型检查、前端测试及Vite/PWA构建通过；另以dot报告读回45文件/225项测试全部通过。嵌入门禁14runs/27assertions及原交易门禁9runs/58assertions均零失败/错误/跳过。
- `make build OUTPUT=/private/tmp/quantmesh-funding-config-rc965.6l3uOd/quantmesh` 终态成功，--version返回3.111.0-rc965；最终稳定后编译内嵌字节测试1.551s通过，未与前端重构建重叠。产物SHA256 `fc6d0094e58d6bf778df479ea5eae69ca4c1788c102962ccc4d46ae2f96527c9`，两份前端清单均为 `e1c9125308b9aa8e6b47679cea835aead29a2eb6295e068c47fc52374b2132b1`（214资产），来源摘要 `cffa8b9121f63f5deb3c32ea98f673910aae93fc403f443ebe80204b2cfbb30b`。构建时HEAD45328980且源码dirty，非发布提交构建；JSON/Markdown结果在上述目录。
- 上轮嵌入保留于 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-83938-tqepb0/dist`，不覆盖现有运行程序。未继承旧严格MySQL或远端CI结果。不解除资金预留或交易封锁；R01–R15范围保持，完整资产恢复/处置、目标平台交付与净盈利仍未验收。推送目的地仍待确认，不重试被拒绝操作，不改main/tag/发布/部署或连接真实账户。

## rc964：专用构造器取消边界（本地验证完成）

- 原生产构造器预检返回后未检查调用方取消，实际前置/预检中取消均继续进入所有权初始化；初始价格使用不可取消的Sleep，已取消但有缓存报价时仍返回成功。先保持原语义并注入显式预检/工厂依赖复现四项失败；不替换全局变量、不访问真实交易所。
- 同一生产入口委托显式依赖实现，构造前及预检/工厂/余额读取返回后检查取消；保留取消与RPC/清理错误，失败清理登记提前到首个连接创建，含价格启动失败。报价等待使用可取消timer，保留原10次等待预算，拒绝NaN/Inf；没有放宽资金、所有权、金融意图或账户核验。
- 最终定向三轮race通过（根包2.498s），覆盖6组新增测试与原受管准入/所有权回归：预检前/预检中取消、首个连接构造期间取消及流清理失败、进入报价等待后取消、缓存报价不能覆盖取消、非法报价和两项租约清理失败均可追溯。根包完整race终态25.517s通过，根包vet、diff检查通过；无真实账户/下单。余额及后续连接分支有生产取消检查，但尚无完整构造器贯穿全部分支的端到端夹具，不据此宣称全部初始化成功/失败路径验收。
- `yarn --cwd webui verify` 整条命令终态成功：类型检查、45文件/225测试、Vite/PWA构建通过；Ruby嵌入门禁14runs/27assertions及交易门禁9runs/58assertions，均零失败/错误/跳过。完整 `make build OUTPUT=/private/tmp/quantmesh-funding-startup-rc964.BvaYDj/quantmesh` 成功，`--version` 返回3.111.0-rc964；最终稳定后的编译内嵌字节测试1.419s通过，不与前端构建重叠。
- 产物SHA256 `f1845609accf729c002eb63f3442b4d983a4fce044860f8c67bd863483de7408`，两个前端清单均为 `af76b00beaeca3ce26d1286b7d5254c7e0531620ca9c747cd0267fc8b518bbc3`（214资产）；来源摘要 `65ad97be79a97984c6d24cb3932b944db7056c35f348ff1c1da1982225a04b7d`。构建于dirty源码及HEAD882422ad，非发布提交产物；JSON/Markdown结果在上述专用目录。上轮嵌入产物保留于 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-83177-p9v09s/dist`，不覆盖现有运行程序。
- 本项不冒充完整失败受管实例接管：预检拒绝、资产历史状态注册、当前库存、处置与恢复交易仍未闭合，工厂没有context参数仍不能中止已进入的构造调用。未继承rc962严格MySQL、未核验远端CI/目标平台发布。R01–R15范围保持，推送待用户确认目的地，不重试或切凭据，未改main/tag/发布/部署或连接真实账户。

## rc963：前端嵌入交付来源与失败门禁（本地验证完成）

- 对应 R14：标准 Makefile、scripts/build.sh 与 CI/CD 使用 Yarn 前端构建及 Ruby 门禁，删除构建中的忽略复制失败、缺前端跳过和占位回退；Make 依赖保证并行构建仍先前端后 Go，不从历史 git tag 覆盖源码版本。
- build-meta.json 绑定前后端版本、前端 src/public/配置及门禁脚本摘要、完整产物 SHA256；失败构建先撤销旧标记，源码在构建过程中改变不得发新标记。同步先核验临时副本，保留旧嵌入目录；复制或交换失败保留/恢复原目录，拒绝产物符号链接和越界引用。来源目录仅允许指向同目录内普通文件的链接，并同时核验链接身份与目标内容，兼容仓库已有 PWA 图标链接；越界/目录链接不接受。
- 新增 opt-in embedded_frontend Go 测试，直接读取编译进测试程序的 go:embed 元数据及全部资产字节，与当前核验前端清单比对；CI/CD 标准测试明确执行，不仅检查磁盘复制。
- 最终门禁14 runs/27 assertions，原交易门禁9 runs/58 assertions，均零失败/错误/跳过；覆盖 `make -j4` 前端失败不得进入 Go、内部来源链接及目标变更、越界链接、失败交换恢复。首次构建因现有8个PWA图标链接被过度拒绝而失败，补红测并修正上述来源边界后才接受结果。前端类型检查及225测试通过，后续独立构建和完整 Make 构建通过；没有将最初失败的 yarn verify 整体链条写成成功。
- `make build OUTPUT=/private/tmp/quantmesh-embed-rc963-build.FWYjEh/quantmesh` 成功，`--version` 返回3.111.0-rc963。产物SHA256 `a911e9a0e097484f3ed5048d353fe4b41aa22b2d9bc35a857a30f2341bf94c00`；webui/dist与web/dist清单均为 `a33853ef7675468634187e3b5c3156c6fa5c3ff953d00893e97e67dfd8a07cc8`，包含214个资产，来源摘要 `ece7e39ed9126ce9a31a6ad99fa8ed5ae944ea6d547dbde2cf5d56db88533253`。本地dirty源码构建，Vite内Git短号仍指向构建时父提交，不冒充最终发布提交构建。
- 编译内嵌测试在最终产物稳定后成功（web1.940s），根包/Web全包race终态成功（38.505s/82.696s），根包/Web vet、YAML解析、bash语法及diff检查通过。初次Go检查受缓存权限阻断；获准重跑时与前端重构建重叠导致标记缺失，该失败不作为验收，待构建完成后重新执行成功。此处无严格MySQL fixture/零跳过数据库证明，不继承rc962报告。JSON及Markdown本地记录位于上述专用临时目录。
- 首次同步将原旧嵌入产物保留于 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-81753-pbboil/dist`；Make再次同步保留上轮产物于 `quantmesh-embedded-backup-20261003-81911-9moh1k/dist`（同临时根目录）。未删除旧产物或覆盖现有运行程序。
- 不把来源摘要当作外部环境/依赖安装可重现证明；历史 scripts/build-release.sh、远端CI/实际目标平台发行构建、真实账户资产处置、完整恢复、原子 fencing 和净盈利仍未验收。R01–R15范围保持，未改 main、打 tag、发布、部署或连接真实账户。

## rc962：同提交严格数据库验证与嵌入前端证据边界

- 被测代码提交精确为 `186818f634fe9486b927c04a58f0499db5898045`，版本 `3.111.0-rc962`。测试前后HEAD相同，tracked diff和index diff均为空；报告source_dirty=true仅因用户无关的`?? --help/`，该目录未打开/修改/暂存。后续本次提交仅补文档，不冒充文档子提交另跑过全部测试。
- 新建MySQL8.0.36一次性fixture，镜像`sha256:a532724022429812ec797c285c1b540a644c15e248579c6bfdf12a8fbaab4964`，schema `quantmesh_readiness_rc962` 初始表数0。仅绑定127.0.0.1:32780，无宿主目录挂载，512MiB tmpfs、1GiB内存/2CPU；临时破坏性迁移开关仅指向该空库，不读取生产配置/账户/凭据。
- 执行`ruby scripts/verify_trading_race.rb /private/tmp/quantmesh-trading-race-rc962-same-commit-mysql --require-mysql`，临时DSN仅指向上述fixture。`results.json`及`results.md`均读回1941 pass、零skip/失败，mysql_required=true、无缺包/缺数据库证据/解析错误；strategy141.145s、storage18.723s、Web57.700s。
- 8项强制数据库用例均为pass：独立暂停owner、资金费身份/覆盖、共享钱包资金预留、借币利息账本/覆盖、逐笔成交覆盖迁移、现货快照迁移、模糊Bot归属回填拒绝、收益提现规则。额外nilDB配置测试不用于冒充这8项证据。
- 结束后再次核验容器身份/专用标签/无宿主挂载，只删除精确容器`3e8fd7ed71b3f05c823dd35439b48d320cf731e96dfd35a56e936e5ed2d2f5e5`；按ID和名称查询均无残留。tmpfs测试数据已丢弃，报告与缓存镜像保留，未删除其他容器、仓库文件或生产数据。
- 按Go+React嵌入流程另查R14交付边界：`web/static.go`嵌入`web/dist/*`，本地该目录与最新`webui/dist`不是同一目录；index SHA256分别为`887c3d0eb5c31690504bbff8c36103245955963f092d8c5500fc1f692a5d242d`和`2bea7973b5062c41adb2fa8f3710f7cd3712e7fd58e7d70f1d9bc15e8ab35f15`，当前rc962版本字符串仅在最新webui产物找到。CI标准测试步骤明确复制新产物，但CI/CD其他构建分支存在`cp ... || true`及占位回退，尚需统一失败门禁及同步证明；不将本地差异直接宣称线上已部署旧前端。
- 此严格报告补齐当前提交的数据库回归，不证明最新前端嵌入产物、目标平台发行构建、当前真实资产归属/处置、完整故障恢复、原子fencing或净盈利。未同步/改写嵌入产物以干扰本轮相同输入，未改main、打tag、发布、部署或连接真实账户；R01–R15未完成事项继续保留。

## rc962：启动回滚冻结顺序与多重失败不能丢失

- 两个定向红测分别复现：StartAll只返回原启动错误、丢失失败项及先前项的停止失败；回滚开始时先前成功项的循环context仍活跃，可能在其他清理阻塞期间继续决策。不是把日志中的失败当作代码已返回的证据。
- 启动失败先取消共享子context，再调用失败项和先前成功项Stop；仅发送取消，不能冒充全部在途RPC已终止或资产已平仓。管理器收集所有清理错误，StrategyStartupRollbackError通过多原因Unwrap保留启动、取消与各停止/平仓原因；全部清理仍会尝试，未启动项不被停止。
- 专用Funding Carry受管恢复准入只接受单一结构化剩余资产恢复结果及普通单原因包装；joined/multi-cause错误即使包含恢复诊断也不准入。原核账内容、独立封锁和资金claim证明要求保持不变，不新增自动平仓/处置或释放。
- 最新连续3轮race（根包3.787s/strategy2.803s）覆盖全部错误可追溯、先取消context再进入Stop、取消及旧回滚兼容、正常受管恢复与多原因拒绝。实际Funding Carry策略成功启动后注入UNKNOWN并取消，真实Stop返回自动平仓拒绝，管理器同时保留取消与该拒绝；无新下单/还款、未报告平仓完成。该夹具不等于完整构造器或真实账户验收。
- 根包/strategy最新vet、diff检查、Ruby9runs/58assertions、Yarn类型检查/45文件225项测试/Vite PWA构建均完成通过。初轮十包报告包含冻结步骤前的源码，仅作阶段记录，不将该结果继承为最终源码通过。
- 冻结后最终十包报告 `/private/tmp/quantmesh-trading-race-rc962-frozen-final/results.json` 与 `results.md` 已读取：1933 pass、8 MySQL skip、零失败，无缺包/解析错误，strategy143.336s；全部新增回滚/冻结/真实UNKNOWN关闭拒绝/多原因准入用例通过。source_commit=04ce8896、source_version=3.111.0-rc962、source_dirty=true，是本版提交前源码证据，不是同提交严格MySQL验收；测试期间最终生产实现未再变化。
- R09/R07的失败可观测性和取消顺序有所推进，但失败实例完整接管、资产处置、现时全账户库存归属、原子fencing及R01–R15其余验收仍未闭合。本版没有同提交严格MySQL验收，未合main、打tag、发布、部署或连接真实账户，不宣称实盘/盈利已验收。

## rc961：受管启动不能脱离调用方生命周期

- 实际构造器调用专用受管启动，但旧管理器 StartAll 使用创建管理器时的 background context。先以真实 Funding Carry 耐久读取回调取消调用方，复现旧路径仍导入历史余量，定向用例明确失败；这不是仅靠静态推测或新接口自测。
- 新 StartAllContext 把策略恢复与循环绑定调用方和管理器两个 context，启动前/每项调用前/每项返回后检查取消；未调用的策略不触发 Stop，已调用项和先前成功项走原回滚。旧 StartAll 仍绑定管理器生命周期，专用 Funding Carry 生产调用显式传入所属 runtime context。策略枚举失败保留原错误链，不以泛化错误丢掉取消原因。
- 最新三轮 race（根包4.667s/strategy3.413s）验证 nil/已取消/管理器停止/第一或第二项启动期间取消、先前已启动项全部回滚而尚未启动项不调用、调用方或管理器取消均通知循环、旧接口兼容，以及真实 Funding Carry 循环退出；真实余量恢复用例证明取消后不导入，完整耐久内容保持不变、无金融 RPC 或启动循环。实际Web异步入口使用background context，不把HTTP请求结束视为Bot生命周期结束。
- 此处仅闭合 R09 的调用生命周期缺口，不自动释放claim、不解除交易门禁，也不把退出循环当作平仓证据。构造器前置失败的受管接管、当前库存/资产处置、部分成交补偿、全账户归属、原子fencing、其他专用运行时调用方context绑定及R01–R15其余验收仍待完成。
- 十包race报告 `/private/tmp/quantmesh-trading-race-rc961-final/results.json` 和 `results.md` 已读取终态：1929 pass、8 MySQL skip、零失败，无缺包/解析错误，strategy145.031s、Web159.997s；source_commit=4051fe78、source_version=3.111.0-rc961、source_dirty=true，是提交前工作树证据，不是同提交严格数据库验收。随后补强第二项取消夹具的最新三轮结果如上，生产实现未再变化。
- 根包/strategy vet、diff检查、Ruby9runs/58assertions及Yarn类型检查、45文件/225项测试、Vite/PWA构建均已通过。不继承旧版严格MySQL结果；未改main、打tag、发布、部署或访问真实账户，不宣称完整恢复或盈利已验收。

## rc960：受管核账实例不能冒充正在交易

- 实际追踪发现 Bot List/GetBot 适配器的 running 表示注册表中有受管实例，而 Funding Carry 仪表盘直接把该值显示为运行；active_bots 还无条件累计配置数。新增 funding_carry_runtime 单独报告真实循环及待核账状态，不改变原受管停止/资本claim生命周期，列表和详情生产适配器都接入该读取。
- 仪表盘状态优先待核账，其次真实循环；受管但缺少策略证据返回 unknown，停止、待核账、未知都不计入活跃数。专用 status 接口的 running 改为已确认交易状态，同时显式返回 managed 以保留受管语义。React 使用真实国际化资源显示待核账/未知，缺少语言键沿现有 zh-CN fallback，不在 JSX 写死新增文案。
- 正常化状态不能代替资金证明：历史余量仍不等于现时库存，未增加恢复交易/自动处置/资金释放能力。真实构造器前置失败、全账户资产归属、部分成交处置、原子 fencing 与 R01–R15 其余风险仍待闭合。
- 实际剩余资产 Start 后，将真实策略管理器放入 BotManager 注册表并读回状态，证明保留 managed 且不报告交易循环；原资金释放拒绝断言保留。Web 三轮 race 覆盖状态优先级、专用 HTTP、真实仪表盘 HTTP 四类实例且仅1个计为活跃；根包5.655s、Web4.640s。这是夹具接线证据，不是完整生产构造器或浏览器端到端验收。
- 前端首轮新增服务端渲染夹具因导入 browser-only API 触发 window 未定义，已 mock 请求模块隔离环境后重跑；不修改生产请求模块以迁就测试。最新Yarn类型检查、45文件/225项测试、Vite/PWA构建通过，Ruby9runs/58assertions通过。根包/Web全量race完成（42.569s/77.237s），两包vet及diff检查通过；初次Go缓存访问权限失败的命令原样在允许环境重跑通过。结果是本版提交前工作树证据，不是十包或同提交严格 MySQL 验收。未合main、打tag、发布、部署或访问真实账户。

## rc959：已核清历史余量的受管核账启动分支

- 生产专用构造器调用受管启动准入：仅完整账本已导入、UNKNOWN/in-flight、未开始交易循环、无未保存金融意图/错误且账户/本金/成交证据再核验通过的结构化剩余资产恢复状态可继续构造SymbolRuntime。要求唯一Funding Carry策略及同一OpeningGate绑定，独立funding_carry_reconciliation_required封锁保留其他暂停，并标记平仓未核清；后续资本claim/租约移交走原受管生命周期。
- 结构化错误在钱包协调及释放成功后才返回，钱包释放失败不能因含恢复信息而被errors.As误认成准入；其他初始化/账本/归属错误仍返回失败。受管接管不表示当前余额可用、不重发下单或还款，不解除UNKNOWN，不把资金claim当作可释放。
- 管理器原enabled-only回退可将未启动Funding Carry显示为running；首个夹具因未配置Enabled而没复现此问题，已修正为显式启用并用真实旧接口回退路径对照。新增IsRunning按实际started/context报告未启动/正常循环/停止，不用配置开关冒充成功。
- 根包使用真实FundingCarry策略、管理器、完整schema6金融/成交快照和钱包锁隔离夹具验证生产准入函数；无真实交易所连接。全构造器前置权限/余额检查、注册表保留及Web实际渲染尚未端到端验收，预检失败/其他恢复类型的受管接管也不在本次通过范围；当前库存与剩余资产最终处置仍未闭合。
- 最终受管准入/真实运行报告/新旧恢复/原资金释放用例连续3轮race通过（根包3.511s、strategy3.328s），覆盖普通本金账本错误、错账户、取消、所有权丢失、钱包unlock失败及gate错绑定；根包夹具最初缺margin Borrow接口导致编译失败，改为完整margin接口后才接受业务验证。已确认状态数据含历史余量、reconciliation_required=true且IsRunning=false，原钱包释放真实调用fc.VerifyFlat并拒绝删除claim，未读实盘或发金融RPC。
- 十包 `/private/tmp/quantmesh-trading-race-rc959-final/results.json` 与 `results.md` 已读回1923 pass、8 MySQL skip、0失败、无缺包/解析错误，strategy140.102s；source_commit=778fbd32、source_version=3.111.0-rc959、source_dirty=true，是本版提交前源码回归，非同提交严格数据库验收。根包/strategy vet、diff检查、Ruby9runs/58assertions、Yarn类型检查/测试/Vite PWA构建通过。
- 不继承rc957数据库证据。R01–R15其余要求、全账户归属、当前库存/资产处置、部分成交/完整恢复、原子世代fencing及真实盈利证明仍待闭合，未改main、发布、部署或访问真实账户。

## rc958：重启接管剩余资产的完整历史账本

- 新增实际Start回归先复现rc957：耐久净量0.4008、确认消耗0.4，但普通恢复拒绝后内存没有账本，状态API返回qty=null/known=false，而非已核清的历史差额0.0008。
- 启动在已保存买回/成交/还款恢复之后，使用与其他路径一致的操作门→钱包锁，锁内重新读取完整schema/本金账本/成交费用/确认消耗，核验实际基础币及当前账户；context/运行所有权在内存接管前再次检查。本地未保存的还款ACK/买回CID或保存失败不被较旧耐久记录覆盖。
- 只接管已验证历史账本，深拷贝逐笔成交，保留其他腿、借款身份和金融事件，标记UNKNOWN/in-flight后返回仍需现时库存与处置核验的错误；重复Start可读回同样差额，不改耐久记录、不调用下单/还款、不宣告运行成功。零余量仍交给原普通恢复核验。
- 生产边界：funding_carry_runtime.go 在strategyManager.StartAll失败后直接返回nil，早于SymbolRuntime构造；本版真实启动错误可明确报告余量，策略对象状态读取已接管账本，但不宣称失败实例已挂入Web或可进行受管处置。失败后保留/重新挂入只允许核账的受管运行时仍需实现并验证，不能把内部可见性冒充完整恢复闭环。
- 资金释放接线只读复核：specialized Funding Carry 启动失败清理与停止释放都调用fc.VerifyFlat；不存在已举证的绕过该余量门禁路径。标准MSE资金释放要求私有账本核验和可取消库存能力，缺能力并不把GetPositions=nil当作空仓。此处不替代完整运行时资金释放演练或原子fencing验收。
- 十包 `/private/tmp/quantmesh-trading-race-rc958-final/results.json` 与 `results.md` 已读回1920 pass、8 MySQL skip、0失败，无缺包/解析错误，strategy140.086s，source_commit=53ef37fe、source_version=3.111.0-rc958、source_dirty=true；这是本版提交前生产源码回归，不是同提交严格MySQL验收。
- 随后仅补强测试夹具的UNKNOWN/in-flight、借款身份及启动诊断金额/现时库存边界断言，生产实现未变；最新新旧恢复、余量、启动还款和钱包锁序用例连续3轮 race 通过（3.068s），覆盖锁内快照更新/丢失/错误、错账户/资产/本金账本、取消/所有权丢失、本地未保存ACK/CID/保存失败拒绝覆盖、其他腿保留及真实输入深拷贝。strategy vet 初次缓存权限失败后原样允许环境重跑通过；diff检查、Ruby9runs/58assertions、Yarn类型检查/测试/Vite PWA构建通过。
- rc957同提交严格MySQL证据不继承为本版结果。当前失败实例的受管Web接管、实物库存覆盖、全账户其他所有者归属、剩余资产处置、部分成交/完整恢复、原子世代fencing及R01–R15其余要求仍待闭合；未访问生产库/账户、发布、部署或验收盈利。

## rc957：同提交严格 MySQL 验证检查点

- 测试代码提交：`e6d1260f0b92d42b12a1eb15ea9c158c62b542ce`，版本 `3.111.0-rc957`。测试前后 HEAD 完全相同，tracked diff 与 index diff 均为空；报告 source_dirty=true 仅因既有无关 `?? --help/`，未读取、改动或纳入提交。
- 命令：`ruby scripts/verify_trading_race.rb /private/tmp/quantmesh-trading-race-rc957-same-commit-mysql --require-mysql`。测试 DSN 和 destructive-schema 许可仅指向本轮新建的 `quantmesh_readiness_rc957`，开始前确认业务表数为0，未读取生产配置。
- 环境：MySQL `8.0.36`，镜像 `sha256:a532724022429812ec797c285c1b540a644c15e248579c6bfdf12a8fbaab4964`；容器 `quantmesh-readiness-mysql-rc957`，精确ID `6716596b4145477d6eb92ab753a491527af03558cfa599b27f874be0d4aa0a5f`，标签 isolated-readiness-test。无宿主机数据卷，512MiB tmpfs、1GiB内存/2CPU，仅监听 `127.0.0.1:32779`；清理前再次核对身份及挂载。
- JSON 与 Markdown 已读回1925 pass、0 skip、0 fail，无缺包/解析错误，mysql_required=true、missing_verified_mysql_cases为空；8项强制用例逐一pass：独立暂停所有者、资金费身份/覆盖、账户钱包预留、借贷利息账本、成交覆盖迁移、现货库存快照、歧义Bot归属回填拒绝、利润提取规则。strategy140.776s。
- 容器和tmpfs库已精确删除，并读回无残留；测试数据不可恢复，报告 `/private/tmp/quantmesh-trading-race-rc957-same-commit-mysql/results.json`、`results.md` 和镜像保留。本检查点只更新3份文档，不修改代码或提升版本。
- 本次补齐rc957的本地同提交数据库证据，不替代目标平台交付、生产迁移、真实账户资产覆盖或盈利证明。当前实际Start会保留耐久余量证据并拒绝启动，但没有完整重启接管/处置；全账户库存归属、部分成交恢复、原子世代fencing及R01–R15其余要求仍待闭合。未改main、打发布标签、部署或实盘操作。

## rc957：还款后剩余买回资产独立核账

- 实际 `closeReverse` 回归先复现旧行为：净买回0.4008、确认还款0.4之后返回成功并宣告平仓，遗漏0.0008；不是只从提交标题推断风险。
- 本版使用既有持久化逐笔费用/净量和实际确认还款消耗，按十进制有理数独立计算 margin 剩余量，不用交易精度或4 ULP容差抹掉正差额。还款记录和本金清零保留，但剩余操作继续未解决，不重复RPC，不把margin差额写入spot库存。
- 空仓证明同时核对耐久与内存记录；普通恢复拒绝遗留差额，恢复专用解码仍可读取历史证据。状态 API 返回精确字符串/known/basis，未知为null，未建立所有权的启动失败状态不虚构零余额。
- 有效零差额保持可恢复/可核验；重复订单、错账户/资产、未核清成交、超耗（含很小的负差额）、缺/无效/重复还款证据等不能返回已知零。多周期不同来源精确求和，微量正差额同时覆盖内存、耐久与实际Start。原来的有余量成功预期改为保留未解决资产；补充真实零余量成功用例，并用明确base fee构造零余量夹具，让原微债务/最终所有权门禁仍被真实执行而非提前挡住。
- 最终重点关联 race 两轮111.344s通过；`go vet ./strategy`、`git diff --check`、Ruby门禁9 runs/58 assertions、Yarn typecheck/测试/Vite PWA构建均通过。初轮扩大回归不算验收：首轮有包级/HTTP用例失败，原样具名HTTP重跑通过但原因未完全确认；随后完整回归揭示3处旧有余量成功预期，逐一调整夹具与语义后再跑。
- 最终十包 `/private/tmp/quantmesh-trading-race-rc957-final3/results.json` 与 `results.md` 已读回1917 pass、8 MySQL skip、0失败、无缺包/解析错误，strategy140.019s，source_version=3.111.0-rc957、source_commit=74805846、source_dirty=true。这是当前提交前源码回归，不是本版同提交严格MySQL结果；此前final/final2失败不作为通过证据，rc956数据库证据不继承为rc957验收。
- 这是可审计历史差额核算与空仓漏洞修复，不等于当前资产仍在账户或可支用，也不是自动补偿/出售/转账能力。重启后残余资产的完整接管、全账户库存归属/现时覆盖和最终处置仍待闭合；R01–R15范围不缩减，未发布、部署、访问真实账户或盈利验收。

## rc956：同提交严格 MySQL 验证检查点

- 测试代码提交：`e0e2f2f0cb337796d0dfd14165d3fff260df52fe`，版本 `3.111.0-rc956`；测试前后 HEAD 完全相同，tracked diff 与 index diff 均为空。报告 source_dirty=true 仅因原有无关 `?? --help/`，未读取或改动该目录，不把它纳入提交。
- 命令：`ruby scripts/verify_trading_race.rb /private/tmp/quantmesh-trading-race-rc956-same-commit-mysql --require-mysql`。只对本次新建且初始业务表数为0的一次性 schema `quantmesh_readiness_rc956` 设置测试 DSN 和 destructive-schema 显式许可，没有读取生产配置。
- 环境：MySQL `8.0.36`，镜像 `sha256:a532724022429812ec797c285c1b540a644c15e248579c6bfdf12a8fbaab4964`；容器 `quantmesh-readiness-mysql-rc956`，ID `14a01ace1b2d445a2ea20a7b7f0dd398e969aaad02bfefb076760dc44990441b`。启动/清理前核对 `isolated-readiness-test` 标签和精确身份，无宿主机数据卷，`/var/lib/mysql` 为512MiB tmpfs，内存1GiB/CPU2，仅监听 `127.0.0.1:32778`。
- JSON 与 Markdown 已读回1921 pass、0 skip、0 fail、无缺包/解析错误，mysql_required=true、missing_verified_mysql_cases为空；全部8项强制MySQL用例各自为pass，覆盖暂停所有者、资金费身份/覆盖、账户预留、借贷利息账本、成交覆盖迁移、现货库存快照、歧义Bot归属回填拒绝和提取规则。
- 临时容器和tmpfs库已删除，并读回精确ID无残留；保留镜像及 `/private/tmp/quantmesh-trading-race-rc956-same-commit-mysql/results.json`、`results.md`。本检查点仅更新文档，不修改代码或提升版本。
- 此项补齐当前代码的本地同提交数据库回归，不代表生产迁移、目标平台交付、真实账户归属/恢复或净盈利验收。R01–R15范围不缩减；全账户库存、剩余资产、部分成交/完整恢复、原子世代fencing及真实盈利证据仍待闭合，未合入main、未打发布标签、未部署或实盘操作。

## rc956：空仓核验与恢复统一锁顺序

- 真实 VerifyFlat 隔离回归观察到它在等待已被持有的策略操作门时先获取钱包租约，导致操作持有者无法取得钱包（探测请求超时）；启动恢复、tick 与手动关闭则为操作门后钱包锁，形成反向锁序。
- VerifyFlat 先取得可取消操作门，再进入既有钱包协调租约；不修改全账户资本核验外层钱包租约协议、不放宽空仓/持久化/UNKNOWN门禁。回归核验等待操作期间钱包可由当前操作持有者取得，取消后操作门和钱包仍可正常重试。
- 补充回归复现仅调整锁序后，已失去所有权的调用仍会排队至超时；保留取操作门前的所有权快速拒绝，钱包协调内部仍二次核验。前轮最终测试启动于该补充修正前，不作为当前源码的最终证据。
- 全账户库存归属、剩余资产、完整恢复、严格数据库/发布证据及原子世代 fencing 仍未闭合；此批只消除该实际锁序冲突，不宣称所有并发路径已验收。
- 验证：锁序/取消后重试、同钱包串行化及所有权快速拒绝五轮 race 通过（2.978s）；最终关联策略/解码/关闭路径两轮 race 通过（251.878s）。strategy vet、diff 检查和 Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。此前3.141s定向及250.750s关联结果不包含所有权补充修正，不替代最终验证。
- 最终十包 `/private/tmp/quantmesh-trading-race-rc956-final2/results.json` 与 `results.md` 已读回1913 pass、8 MySQL skip、0失败、无缺包/解析错误，source_version=3.111.0-rc956、source_commit=5739e73a、source_dirty=true；前轮 final 的1912项为补充修正前证据，非最终结果。这是提交前源码回归，不是本版同提交严格 MySQL 验证。未访问真实账户/生产库、未下单或发布。

## rc955：margin 可用余额与还款金额同响应核对

- HTTP 回归先复现 margin GetBalance 因继承 spot 方法访问 /api/v3/account：margin free0.25 BTC、locked0.2 BTC时误返回 spot free999 BTC。覆盖 margin 方法，改从 /sapi/v1/margin/account 精确匹配唯一资产、解析有限非负 free；资产缺失/重复不冒充零余额，locked 不加到 free。
- 新单响应本金/利息/free能力接入真实 closeReverse：买回核账后同时验证当前本金与策略账本、含息金额不超历史净量且 free 足额，再保存精确还款请求。已花费、冻结不足额、非法读数、查询失败、取消/所有权丢失或意图保存失败不还款，历史成交仍保留。
- 可用余额不是策略资产归属：仍缺全账户其他所有者库存/支用约束、剩余资产处置、完整重启/部分成交恢复和原子世代 fencing；账户响应到 RPC 间外部资金变化不宣称原子排除。未访问真实账户/生产库、未下单或发布，不继承旧版严格验收。
- 验证：当前可用资金九种真实 closeReverse 场景与原债务刷新七种场景两轮 race 通过（66.254s），覆盖足额正例、已花费/冻结、NaN/负额、查询失败、取消/所有权及意图保存失败；最终 Binance 余额/债务 HTTP 回归两轮 race 通过（1.754s），确认正确账户、不含 locked、同响应本金/利息/free、资产缺失/重复及非法余额拒绝、明确零余额保留。最终关联策略/解码/关闭路径两轮 race 通过（250.727s）；strategy/exchange/Binance vet、diff 检查和 Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。
- 十包 `/private/tmp/quantmesh-trading-race-rc955-final/results.json` 与 `results.md` 已读回1911 pass、8 MySQL skip、0失败、无缺包/解析错误，source_version=3.111.0-rc955、source_commit=b0029a8d、source_dirty=true；这是提交前源码回归，不是本版同提交严格 MySQL 验证。未访问真实账户/生产库、未下单或发布，不继承旧版严格验收。

## rc954：买回完成后刷新现时本金和利息

- 首轮回归夹具数量精度不匹配，先被既有超请求成交门禁拒绝，不计为债务时间差缺陷；修正到实际四位取整后，复现含息金额变化仍按旧0.4 BTC还款，以及超额利息/本金变化/查询失败或缺失组件仍进入 RPC。
- 正常 closeReverse 在净成交核账和订单结算后，通过独立负债能力重新查询同基础币本金和利息；保存请求前复核 context/owner、有限金额、策略本金一致性及该买回来源净数量。利息0.0002可按实际0.4002还款，超出净量或本金不匹配不还款。
- 生产 Binance margin 的持仓接口要求零基础币库存，不能拿买回后库存当负债证据；新增 margin/account 精确基础币负债读数及 wrapper 接线，允许已有库存但不认领它、不削弱旧持仓门禁。仍需实际可用余额/全账户归属、剩余资产、部分成交与完整重启恢复、原子世代 fencing；刷新到 RPC 间的新增利息也不能宣称原子消除。
- 验证：实际 closeReverse 七种刷新场景两轮 race 通过（30.390s），涵盖含息金额增长、净量不足、本金变化、查询错误/缺失组件、取消和所有权丢失；真实 Binance margin/account HTTP 七类场景两轮 race 通过（2.059s），覆盖买回库存、缺失/重复资产、非法/溢出金额及外币请求，并确认旧库存归属门禁不退化。最终关联策略/解码/关闭路径两轮 race 通过（215.117s）；strategy/exchange/Binance vet、diff 检查和 Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。
- 十包 `/private/tmp/quantmesh-trading-race-rc954-final/results.json` 与 `results.md` 已读回1908 pass、8 MySQL skip、0失败、无缺包/解析错误，source_version=3.111.0-rc954、source_commit=3f8863e8、source_dirty=true；这是提交前源码回归，不是本版同提交严格 MySQL 验证。未访问真实账户/生产库、未下单或发布，不继承旧版严格验收。

## rc953：实际含息还款消耗不再绑定历史买回目标

- 回归先复现历史目标0.4 BTC、净成交0.401 BTC时，含息还款0.4002 BTC虽有足额证据仍在 RPC 前被拒绝。来源按实际请求金额检查净数量上限，确认后消耗严格绑定同账户/币种/ACK 的本金加利息事件，历史目标保持原值；不因目标差异拒绝合法含息核账。
- runtime schema6 接受历史1–5格式，旧程序不能将新消费语义静默读取；不足额、重复消费、事件不匹配继续拒绝。此批不新增自动偿债、不证明当前资产足够，当前负债/实物余额、部分成交补偿、完整恢复及原子世代 fencing 仍未闭合。
- 验证：含息消费、实际 Start ACK 恢复、利息不足额、保存失败原子回滚/重试和 schema5/6 回归两轮 race 通过（2.172s）；既有消费/来源/启动路径两轮 race 通过（14.448s，补充保存失败测试前）；最终关联策略/解码/关闭路径两轮 race 通过（186.638s）。strategy vet、diff 检查和 Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。
- 十包 `/private/tmp/quantmesh-trading-race-rc953-final/results.json` 与 `results.md` 已读回1906 pass、8 MySQL skip、0失败、无缺包/解析错误，source_version=3.111.0-rc953、source_commit=ed061c60、source_dirty=true；这是提交前源码回归，不是本版同提交严格 MySQL 验证。未访问真实账户/生产库、未下单或发布，不继承旧版严格验收。

## rc952：重启继续已确认买回订单的逐笔成交核账

- 原 CID 接管 ACK 后同次启动继续逐笔核账；已有未核清 ACK 也进入恢复。在操作/钱包锁下重读完整账本及账户/资产身份，唯一未核清订单必须具备原 CID、价格和准备时间，通过同 ID 订单查询再次核对原请求及完成状态，再复用逐笔身份/费用/净数量门禁保存证据。
- 不重新买回、不还款、不启动交易；新单/部分成交/撤单暂保留待核账状态，部分成交后自行完成可以继续核账。缺失、重复、错订单、费用导致不足额、查询/保存失败和取消/所有权丢失不推进耐久数量。结束流程不再清除未核清成交的 in-flight，新操作不能覆盖它。
- 历史缺原请求元数据或多个未核清订单不猜测归属。净数量是历史成交证据而非当前可用资产；部分成交撤单/补偿、实际余额覆盖、当前负债/利息、剩余资产处置、自动恢复交易及原子世代 fencing 仍未闭合。
- 验证：真实 Start 净成交恢复十种场景两轮 race 通过（2.189s），覆盖完成、部分成交随后完成、成交缺失/错单/重复、扣费不足额、查询错误、所有权/取消与保存失败；关联策略/解码/关闭路径两轮 race 通过（186.798s）。strategy vet、diff 检查和 Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。
- 十包 `/private/tmp/quantmesh-trading-race-rc952-final/results.json` 与 `results.md` 已读回1905 pass、8 MySQL skip、0失败、无缺包/解析错误，source_version=3.111.0-rc952、source_commit=67a90a1c、source_dirty=true；这是提交前源码回归，不是本版同提交严格 MySQL 验证。未访问真实账户/生产库、未下单或发布，不继承旧版严格验收。

## rc951：启动按原买回 CID 接管订单身份

- Start 在本金核账前识别买回请求，持操作锁与同账户钱包锁重新加载并验证完整账本/资产/账户，再通过 margin 的精确 CID 接口查询原请求；不扫共享订单猜测归属、不重新提交订单或还款，查询未找到保留请求。
- 核对 CID（含既定 broker 前缀）、订单 ID、币对/方向/类型、请求价格/数量、有限执行量、有效订单时间和已知状态，保存 ACK 后仍保持 UNKNOWN/in-flight，订单记录保留原价格及准备时间。新单/部分成交/撤单也只记录身份，不冒充完整成交或资产已核清。
- 查询/保存失败、取消及所有权丢失不推进耐久请求；启动恢复共用15秒 context 预算。新增请求元数据可选，历史记录不补造；完整成交、撤单补偿、余额/剩余资产核账、自动恢复交易与原子世代 fencing 仍未闭合。
- 验证：真实 Start CID 查询的21种正常/异常场景两轮 race 通过（2.205s），包括已成交/新单/部分成交/撤单、broker 前缀、查询未找到/失败、请求身份不匹配、epoch/缺失时间、所有权/取消/保存失败和错误账户/账本。关联策略/解码/关闭路径两轮 race 通过（186.725s）；strategy vet、diff 检查与 Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。
- 十包 `/private/tmp/quantmesh-trading-race-rc951-final/results.json` 与 `results.md` 已读回1904 pass、8 MySQL skip、0失败、无缺包/解析错误，source_version=3.111.0-rc951、source_commit=4a98bb28、source_dirty=true；当前为提交前源码回归，不是本版同提交严格 MySQL 验证。未访问真实账户/生产库、未下单或发布，不继承旧版严格验收。

## rc950：买回提交前精确 CID 意图与生产透传

- 生产适配器回归先复现传入 CID 被替换：fundingCarryOrderExecutor 未透传字段，已补齐；初始隔离夹具缺锁引发的 panic 属于测试夹具问题，不计为产品缺陷，补齐依赖后实际 CID 断言仍失败，修复后两轮 race 通过。
- 买回先保存 CID、币对/资产/账户、数量、价格、目标负债和准备时间，再进入实际 RPC；ACK 与原请求数量/账户/CID（允许交易所既定 broker 前缀）匹配后，原子转换为历史订单记录。无 ACK 或错 CID 保留 in-flight/UNKNOWN，新操作不能覆盖；保存失败不提交。
- schema5 接受历史1–4格式，不补造旧请求；普通恢复拒绝待提交意图，启动本金核账保留此字段而不丢弃它。当前只提供精确恢复入口，按 CID 查询的自动接管、部分成交证据/补偿、完整资产处置及原子世代 fencing 仍未闭合。
- 验证：实际生产适配器 CID 回归两轮 race 通过（2.517s）；提交前请求/ACK/无 ACK/错 CID及保存失败两轮 race 通过（6.204s）；关联策略/解码/关闭路径两轮 race 通过（187.323s）。根包与 strategy vet、diff 检查和 Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。
- 十包 `/private/tmp/quantmesh-trading-race-rc950-final/results.json` 与 `results.md` 已读回1903 pass、8 MySQL skip、0失败、无缺包/解析错误，source_version=3.111.0-rc950、source_commit=48ef9ec3、source_dirty=true；这是提交前源码回归，不是本版同提交严格 MySQL 验证。未访问真实账户/生产库、未下单或发布，不继承旧版严格验收。

## rc949：确认还款与买回消耗绑定

- 实际关闭链将买回订单身份写入还款意图；原有卖出拒绝/零成交/部分成交返还路径不猜测历史买回来源。提交还款前校验同账户/币种、净数量和目标偿债金额，拒绝被其他还款消耗的来源。
- 查询确认后，本金、金融事件、对应买回记录的还款 ID/消耗数量一起保存，消费按实际确认本金加利息金额而非只按本金；失败共同回滚，待还款 ACK 仍在，重试不再提交 RPC。重复确认可幂等保存关联，不重复扣减消耗。
- schema4 接受历史1/2/3格式，但不为历史无关联记录猜测来源；新格式核验消耗有限非负、不超净数量、同还款不重复分配且有匹配金融事件。订单净数量减已确认消耗只能推导账面余量，物理余额覆盖、剩余资产处置、无 ACK/部分成交和完整自动恢复仍未闭合。
- 验证：来源绑定/原子回滚/重复 RPC 拒绝两轮 race 通过（10.452s），另补真实 Start 按已保存买回来源恢复消耗两轮 race 通过（6.191s）；关联策略/解码/关闭路径两轮 race 通过（178.572s）。strategy vet、diff 检查与 Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。
- 包含补充启动用例的最终十包 `/private/tmp/quantmesh-trading-race-rc949-final2/results.json` 与 `results.md` 已读回1900 pass、8 MySQL skip、0失败、无缺包/解析错误，source_version=3.111.0-rc949、source_commit=873f5f76、source_dirty=true；前一份 final 报告1899项未包含补充启动用例，非最终汇总。生产源码在补充测试后未再变更；当前为提交前源码回归，不是本版同提交严格 MySQL 验证。未访问真实账户/生产库、未下单或发布，不继承旧版严格验收。

## rc948：偿债买回订单与成交证据耐久记录

- 买回在成交查询前保存正订单 ACK、CID、币种/账户及请求数量/目标负债；还款前保存经现有逐笔身份、费用和覆盖核验的成交及毛/净数量。取消及丢失所有权保留本地 ACK，已失效所有者不写 durable；存储失败不继续查询或还款。
- schema3 接受历史已核清 schema1/2，新格式核验订单身份不重复、账户、逐笔费用/净数量一致，并在恢复时绑定实际基础币。快照深拷贝成交，启动本金核账不丢弃已保存买回证据。
- 本批是历史证据接线，不是已确认可支用库存；请求提交前 WAL/无 ACK、未足额或部分成交证据、还款消耗归属、剩余资产处置及自动恢复交易仍未闭合。正常关闭保留证据，不补造共享余额归属，不宣称全资产核账完成。
- 额外回归先复现本金清零后无账户标识的买回证据可被当前账户接管；将买回记录纳入账户证据检查后两轮 race 通过（6.492s）。最终关联策略/解码/关闭路径两轮 race 通过（170.547s），包括持久化失败、取消/所有权、深拷贝、净数量篡改与重复订单；strategy vet、diff 检查及 Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。
- 账户边界修复后的十包 `/private/tmp/quantmesh-trading-race-rc948-final2/results.json` 与 `results.md` 已读回1896 pass、8 MySQL skip、0失败、无缺包/解析错误，source_version=3.111.0-rc948、source_commit=3be22a00、source_dirty=true；前一份 final 报告只覆盖账户修复前代码，不作为最终证据。当前为提交前源码回归，不是本版同提交严格 MySQL 验证；未访问真实账户/生产库，未下单或发布，不继承旧版严格验收。

## rc947：启动接入已受理还款的本金核账

- Start 原本直接拒绝待还款快照，现在在普通恢复前识别精确 ACK，持操作锁/钱包协调锁重新加载并校验 schema、账户、原借款、币种和完整本金账本，仅查询同一还款交易并保存确认本金；无 ACK 或无效归属不查询、不还款。
- 保存前复核 context/owner；历史已确认还款幂等重放。成功只清理已核清还款意图，保留 UNKNOWN/in-flight，不启动交易，不补造买回资产或订单证据。该项是本金核账的实际启动接线，不是完整自动接管，订单/资产恢复、无 ACK 归属与原子世代 fencing 仍待闭合。
- 验证：实际 Start 十种场景（正常确认、已记账重放、查询失败、错误账户、无效账本、无 ACK、错误币种、所有权丢失、取消及保存失败）两轮 race 通过；最终关联策略/解码/关闭路径两轮 race 通过（150.370s）。strategy vet、diff 检查及 Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过，并补取紧凑测试汇总。
- 十包 `/private/tmp/quantmesh-trading-race-rc947-final/results.json` 与 `results.md` 已读回1892 pass、8 MySQL skip、0失败、无缺包/解析错误，source_version=3.111.0-rc947、source_commit=58a61e8e、source_dirty=true，为提交前源码回归，不是本版同提交严格 MySQL 验证；未连接生产库/真实账户、未下单或发布，不继承旧版本严格验收。

## rc946：精确还款意图与 ACK 核账重试

- 真实关闭回归先复现还款已受理但确认查询失败时丢失精确身份并清除 in-flight 标记。
- 平仓及卖出拒绝/零成交/部分成交三条返还链均先保存精确请求，再调用还款 RPC；ACK 在查询前保存，请求包含币种、金额、目标剩余本金、账户范围和借款身份。未确认还款不清理全操作意图，不允许新操作覆盖。
- 当前操作的同 ACK 重试只查询原还款并原子保存确认本金/事件，不再次还款；未知 ID 请求拒绝盲目重发。ACK+错误、ACK 保存失败、失去所有权均保留本地身份；失效所有者不覆盖 durable。
- runtime schema2 使旧程序不能将新意图静默当作核清；新程序接受已核清 schema1，不补造旧快照的请求。未核清快照仍拒绝正常启动，自动重启恢复/无 ACK 历史归属/买回资产核账与完整接管仍待接线，不将安全暂停冒充恢复闭环。
- 验证：最终关联策略/解码/关闭路径两轮 race 通过（150.753s）；新增还款查询失败身份保留、同 ACK 不重复还款、ACK/保存/所有权故障及 schema1/2 兼容边界均通过；strategy vet、diff 检查、Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。
- 十包 `/private/tmp/quantmesh-trading-race-rc946-final/results.json` 与 `results.md` 已读回1891 pass、8 MySQL skip、0失败、无缺包/解析错误，source_version=3.111.0-rc946、source_commit=d9e06c41、source_dirty=true，为提交前源码回归，不是本版同提交严格 MySQL 验证；不继承 rc943 严格检查点，未访问生产库/账户、未下单、未发布或验收盈利能力。

## rc945：买回扣费单位与组件一致性

- 三种真实关闭回归先复现基础币费用少报、零费用仍声明基础币扣费、已标记换算却缺失有效汇率仍被当作足额净数量并调用还款。
- 核对现有 Binance margin adapter 与 wrapper：原手续费按费用币种数量直接透传，基础币扣费来自同一原始费用。未标记转换时按原币金额校验；明确标记计价币转换时要求有效金额/正汇率并校验基础币数量，不能混用单位。正常原币扣费、计价币收费、零费用及有效换算正例保留。
- 验证：真实净偿债十二种异常/正常费用路径两轮 race 通过（50.361s），包括少报、零费用矛盾、缺失汇率及正常换算；strategy vet、diff 检查、Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。
- 十包 `/private/tmp/quantmesh-trading-race-rc945-final/results.json` 与 `results.md` 已读回1887 pass、8 MySQL skip、0失败、无缺包/解析错误，source_version=3.111.0-rc945、source_commit=7d6ba3e2、source_dirty=true，为提交前源码回归，不是本版同提交严格 MySQL 验证；顶层用例数未增加，扩展的是实际关闭回归的子场景。
- 完整买回资产持久化/恢复、交易所原始定点精度与全账户金融核账仍待完成，不继承 rc943 严格检查点；未访问生产库/账户、未下单、未发布或验收盈利能力。

## rc944：买回逐笔费用与基础币净数量

- 五种真实反向平仓回归先复现基础币扣费后不足额、缺失成交、订单身份错误、重复成交及累计覆盖不足仍调用还款并宣称成功。
- 买回偿债前查询保证金账户同订单逐笔成交；校验订单/交易/币对/方向/时间、有限数量/费用及基础币费用身份，十进制累计成交与手续费并核对毛成交覆盖。基础币净数量不足本金加利息时不还款，禁止从共享余额补造归属证据。
- 正常夹具明确提供同订单零费用证据，不让 mock 默认 nil 成交列表冒充已核清；正常基础币收费但净额足够、计价币收费及明确零费用仍可平仓。
- 验证：最终关联策略/解码/关闭路径两轮 race 通过（126.688s），覆盖五类不完整/不足额成交及三类正常费用路径，并保留已有本金/最终所有权/关闭检查点回归；strategy vet、diff 检查及 Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。
- 十包 `/private/tmp/quantmesh-trading-race-rc944-final/results.json` 与 `results.md` 已读回1887 pass、8 MySQL skip、0失败、无缺包/解析错误；source_version=3.111.0-rc944、source_commit=d8c7ced3、source_dirty=true，为提交前源码回归，不是本版同提交严格 MySQL 验证。
- 该步骤证明成交账本推导的净数量，不是完整账户余额/资产归属核账；买回资产的耐久记录、失败后恢复与剩余资产处置仍未闭合。不继承 rc943 同提交严格验证，未访问生产库/账户、未下单、未发布或验收盈利能力。

## rc943：同提交严格 MySQL 验证检查点

- 验证源码提交：`e19a71836eb0428d4e7d145ad36c9756e6373774`，版本 `3.111.0-rc943`。测试前后 HEAD 相同，`git diff --exit-code` 与 `git diff --cached --exit-code` 均通过；未修改源码。报告 `source_dirty=true` 仅因原有未跟踪 `--help/types.json`、`--help/types.md`，不宣称全工作树干净，不移动/提交这些无关文件。
- 命令：`ruby scripts/verify_trading_race.rb /private/tmp/quantmesh-trading-race-rc943-same-commit-mysql --require-mysql`，只向本次创建的一次性本地测试库设置 MySQL DSN 和 destructive-schema 显式测试许可，没有使用生产环境配置。
- 环境：MySQL `8.0.36`，镜像 `sha256:a532724022429812ec797c285c1b540a644c15e248579c6bfdf12a8fbaab4964`；容器 `quantmesh-readiness-mysql-rc943`，ID `a9db4df7c8799511a5769e307923ba75072726ad272c80df6540676d7c9955a3`。启动/清理前核对身份与 `isolated-readiness-test` 标签，无宿主机数据卷，`/var/lib/mysql` 为512MiB tmpfs，内存1GiB/CPU2，只监听 `127.0.0.1:32777`。
- `results.json`、`results.md` 已读回：1894 pass、0 skip、0 fail、无缺包/解析错误，`mysql_required=true`、`missing_verified_mysql_cases=[]`；强制八项包括实例开仓暂停、资金费覆盖、钱包预留、借贷利息账本、订单成交覆盖、现货库存迁移、歧义归属回填拒绝、利润提现规则，均有独立 pass 终态。
- 同一源码提交的 `yarn verify` 类型检查、44文件224测试及 Vite/PWA 构建通过；strategy vet、diff 检查、Ruby 门禁9项/58断言通过。没有修改源码后沿用此次验收；后续源码变更需要重新验证。
- 测试完成后删除上述精确容器并读回确认不存在；一次性 tmpfs 测试数据不可恢复，镜像和 JSON/Markdown 报告保留。此检查点仅证明本地交易核心回归及八项 MySQL 验证，不证明完整恢复补偿、交易所净到账/全账户核账、多实例原子 fencing、部署或真实盈利。

## rc943：买回本金与利息的成交核验

- 四种真实关闭回归先复现：只覆盖本金、NaN/无穷大成交、超过委托数量的成交仍调用还款并返回成功；另一个回归复现买回后失去所有权仍调用还款。
- 买回委托取整后不能少于总负债；成交必须有限、非负、不超过委托量且足够覆盖本金与利息，只允许有限机器精度误差，不使用交易数量容差。还款前复核 context/owner。
- 两处旧正例夹具返回超过其委托量的成交，改成实际可成交范围，而非放宽超量门禁。
- 验证：最终关联策略/解码/关闭路径两轮 race 通过（94.326s），覆盖四类非法/不足额成交、失去所有权不还款、正常含息还清与关闭检查点；strategy vet、diff 检查及 Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。
- 十包 `/private/tmp/quantmesh-trading-race-rc943-final/results.json` 与 `results.md` 读回1886 pass、8 MySQL skip、0失败、无缺包/解析错误；source_version=3.111.0-rc943、source_commit=45913e8c、source_dirty=true，为提交前源码回归，不是同提交严格 MySQL 验收，不继承旧版验收。
- 毛成交数量仍不是扣费后净到账，手续费/买回资产归属/余额覆盖、剩余资产与接管补偿仍待闭合；未访问生产库/账户、未下单、未发布或验收盈利能力。

## rc942：期货已关闭的耐久检查点

- 三组真实反向平仓回归先复现：已平期货在后续买回失败时仍保存旧数量、检查点保存失败/所有权丢失后仍调用买回接口。
- 交易所期货快照须确实为零；持策略锁复核 context/owner、方向、原数量与 intent，再保存零期货数量，但保留债务、借款身份及未完成意图。保存失败回滚数量、标记 UNKNOWN，不继续买回/还款。
- 验证：最终关联策略/解码/关闭路径两轮 race 通过（74.682s），覆盖检查点后买回失败、保存失败/数量回滚、失去所有权不覆写 durable、微量期货残余拒绝及完整两腿正常关闭可恢复解码。strategy vet、diff 检查及 Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。
- 十包 `/private/tmp/quantmesh-trading-race-rc942-final/results.json` 和 `results.md` 读回1884 pass、8 MySQL skip、0失败、无缺包/解析错误；source_version=3.111.0-rc942、source_commit=917060c8、source_dirty=true，为提交前源码回归，不是同提交严格 MySQL 验收，不继承旧版验收。
- 已平仓但持久化失败的恢复、微量期货处置、买回资产核账和完整接管补偿仍待闭合；未访问生产库/账户、未下单、未发布或验收盈利能力。

## rc941：反向平仓实际本金与残债核验

- 三种真实关闭回归先复现部分还本、微量本金、微量还款后负债仍返回成功并清空债务/借款身份。
- 平仓使用确认本金扣减；本金/利息核对采用金额一致性，不以交易数量容差跳过微量债务或接受不匹配账目。还款后负债及本金/利息组件必须为零；最终清除身份前复核 context/owner。
- 小于交易数量精度的欠款不能直接清零，暂拒绝零数量买单并保留身份。该项不是完整微量资产处置方案，完整恢复/接管与自动补偿仍未闭合。
- 验证：相关策略/解码/关闭路径两轮 race 通过（62.657s）；最终五种欠款异常、最终所有权丢失及正常完整借还账本恢复另两轮 race 通过（22.188s）。strategy vet、diff 检查及 Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试和 Vite/PWA 构建通过。
- 十包 `/private/tmp/quantmesh-trading-race-rc941-final/results.json` 与 `results.md` 读回1882 pass、8 MySQL skip、0失败、无缺包/解析错误，source_version=3.111.0-rc941、source_commit=c1f2e3b1、source_dirty=true；这是提交前源码回归，不是同提交严格 MySQL 验收，不继承旧版验收。
- 未访问生产账户/数据库、未下单、未发布或验收真实盈利能力。还款 RPC 已受理但确认失败的完整恢复、已买入剩余资产核账与接管补偿仍待完成；安全拒绝不等于恢复闭环。

## rc940：借款调用中断的身份与待恢复意图

- 四种真实反向开仓隔离回归先复现 ACK 身份丢失：调用取消、运行所有权丢失、正 ID 同时返回错误、取消后保存失败。
- 正 ID 先保留本地；当前所有权仍有效时保存恢复元数据，取消后标记 UNKNOWN；已失效所有者不覆盖 durable。带错误 ACK 不查询、卖出、对冲或还款，不冒充已核实本金事件。
- 未核实借款时普通错误返回不再清除 in-flight 意图；只有债务确认并保存后才进入原有结束流程。
- 验证：相关 Funding Carry / 解码 / 关闭路径两轮 race 通过（50.578s）；最终新增断言及缺失身份/意图测试另两轮 race 通过；strategy vet、diff 检查、Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。
- 十包报告 `/private/tmp/quantmesh-trading-race-rc940-final/results.json` 与 `results.md` 读回 1880 pass、8 MySQL skip、0失败、无缺包/解析错误；版本 rc940，source_commit=4817e36b、source_dirty=true。这是提交前源码回归，不是同提交严格 MySQL 验收，不能继承 rc926 验收。
- 原子世代 fencing、ACK 保存失败后的完整耐久接管、自动核账补偿仍未闭合；取消 context 下保存元数据不是重新获得钱包 lease。未连接真实账户或生产库，未下单、发布或验收盈利能力。
