# Goal Teams Progress

## 2026-10-10：人工核账/解锁方案 2

| 成员 | 任务 | 状态 | 当前步骤 | 证据 | 下一步 |
|---|---|---|---|---|---|
| 需求分析-人工核账契约 | GT-009 | 完成（Wegener） | 只读调用链与策略账能力盘点 | 已记录基线缺少 per-order durable cursor；用户批准扩展为所有生产策略 durable cursor/readback 与显式人工精确 reconcile | 规格已扩大；未宣称实现完成 |
| 策略-账务游标与人工 reconciliation | GT-010 | blocked_on_strategy_durable_accounting | 仅 `strategy/`、`position/` | 多数生产策略仍无 per-order durable accounting readback/CAS；Grid 与 Signal 明确未接入，不得发 receipt 或放行 | 必须补策略实现和重启/幂等测试 |
| 后端-核账审计与解锁 API | GT-011 | 部分实现；生产解锁未接线 | 新增 `accounting/`、`storage/order_reconciliation*`、`web/api_order_reconciliation*`、runtime evidence adapter | 双阶段管理员 API、持久化 case/audit、可信订单/成交只读适配器雏形已存在；生产未注册 OrderAccounting 或 owner-scoped durable Gate bridge | 先拆 intent 与 strategy revision；修正全量成交/后缀游标语义；实现精确 gate bridge 后才可谈生产解锁 |
| 前端-人工核账操作台 | GT-012 | implemented_scaffold | `webui/src` 核账面板、service、测试、locale | 手工建案、单独核账确认与单独解锁确认、服务端证据展示/i18n；错误和不可用状态 fail-closed | 子集测试通过；需与最终后端 DTO 对齐并 QA |
| 独立 QA-跨层恢复验收 | GT-015 | pending | 独立测试文件 | 当前缺全策略 durable cursor、生产 wiring、进程重启/并发/精确 gate 集成验收 | 依赖 blocker 清除；当前包测试不能替代全链路 |
| 评审-核账安全最终复核 | GT-013 | interim_review_complete; final_pending | 只读当前 diff 与测试 | 无 P0；生产策略账 CAS/幂等、精确 durable gate bridge、费用账本与原始数值精度仍是 P1；审计读取 API/UI 缺失 | blocker 清除后最终复核 |

## 初始状态

- 分支 `main`；原 `3.111.0-rc1156` 源码、QA 测试与 `.codex` 文档均为既有未提交资料；不重置、不清理、不覆盖。
- 方案 2 已由用户选择；后续团队范围/API/UI/审计约束已获确认。
- 未访问真实账户或生产 DB，未下单；未提交、推送、发布。
- 旧版本测试记录只作为历史证据；所有最终结论必须绑定本任务最终源码指纹和当次测试结果。

## GT-009 发现与停止条件

- 策略回调 `OnOrderUpdateWithAccounting(bool, error)` 没有 durable per-order/accounting-cursor readback；通用 `GetOrders()` 或运行时快照不构成该证明。
- `ledgerPending/ledgerPayload` 是 execution trade-row 的独立恢复机制，不是策略账状态，不能证明 strategy accounted。
- quarantine 与共享 `strategy_accounting_unverified` source 包含进程内状态；单订单操作不得清除其他订单、manual、global risk 或 bootstrap hold。
- 依已确认的停止条件，当前不能提供会实际开放开仓的 release endpoint。缺少策略级 durable readback 与 gate recovery protocol 时，只可只读展示/建案，不能称解锁流程已实现。
- 用户已批准扩展范围。下一步冻结跨层 accounting cursor/reconcile 合约，梳理生产注册策略全清单；未实现 durable cursor/readback 的策略只能继续保持封锁，不得降级或绕过。

## GT-010A/010B 策略适配结果（2026-10-10）

- Grid/position 子代理与 Signal（trend/mean_reversion/momentum）子代理都按安全停止条件未新增生产代码：当前 Grid 仅存 slot/最后终态而且 state store 无 CAS；Signal 只存 active order fill progress，终态清除且缺账户 scope。两者均不得产生 reconciliation receipt 或开放 release。
- 子代理最初指出订单回调自身不含 trade ID；进一步检查发现 rc1156 工作区 `main_execution_fills.go` 在策略 accounting 前已通过 `ordersync.PersistOwnedOrderFills` 写入 `order_fills`。它有逐笔 trade ID/side/qty/price/fee，但还需实现并验证 exact-order 历史读取、venue 覆盖、strategy owner 与 Grid position cycle 的映射；这并不消除策略账 durable cursor/CAS 缺口。
- 这两组定向测试分别通过 `go test ./position` 与 `go test ./strategy`，只是现有基线通过，不代表新人工核账能力已实现。禁止据此报告全策略支持。

## 本轮实现与验证（2026-10-10）

- 已新增 accounting contract、SQLite/MySQL 核账案与审计迁移、受保护 API、管理面板和本地化；运行时订单证据 provider 能按 Bot + intent 归属读取交易所终态订单和成交明细，但它当前没有注册进 API 服务，也不构成账本或 gate 接线。
- `go test ./accounting ./order ./storage ./web` 通过；Web UI `yarn typecheck` 与 `yarn test` 通过（56 files / 317 tests）；`git diff --check` 通过。
- 初始实现曾把策略账与 intent revision 混用，并将全量 fills 直接交给要求后缀的 accounting API；后端实现者已将 revision 拆分，并加入已处理前缀 TradeID 核验及仅提交缺口 fills/fees 的逻辑。
- 目前没有任何生产代码注册 `OrderReconciliationService`；策略 `OrderAccounting` 和 durable owner-scoped `Gate` bridge 也未实现。即使 API/UI 单测全绿，实际生产仍返回不可用且不能解锁，这是预期 fail-closed，不得改成 permissive fallback。
- 全仓 `GOCACHE=/tmp/quantmesh-go-cache go test ./...` 已在允许 loopback 的环境通过；详细结果见下方复核记录。

## Interim review 与修正（2026-10-10）

- GT-013 首轮独立审查在共享工作区尚处于跨文件编辑期间执行，报告的旧 DTO/编译问题随后已由后端实现者修正；当前前后端都使用独立的 intent/strategy revision，后端定向 `go test ./storage ./web` 与 race 子集通过，前端 typecheck 与 3 个核账 Vitest 文件通过。
- 复核发现 runtime evidence provider 把已知 `UNKNOWN` intent 一并拒绝，导致本功能不能接住它要处理的“策略账结果不确定”订单。现已允许仅在精确 owner intent 加载成功、独立交易所订单为终态、完整 fills/fees 与请求量核验通过时继续人工核账；`LedgerPending` 和 `Rejected` 仍拒绝。新增对应 race 测试通过。该修正不清除 intent UNKNOWN 或任何 gate。
- `GOCACHE=/tmp/quantmesh-go-cache go test -race ./accounting ./storage ./web .` 已通过；`web` 约 266 秒、根包约 120 秒。`go test -race . -run 'TestRuntimeTrustedOrderEvidenceProvider' -count=1` 也通过。
- 完整 `GOCACHE=/tmp/quantmesh-go-cache go test ./...` 在允许 loopback 的环境通过；前端 `yarn typecheck && yarn test` 通过（56 个测试文件 / 317 项）；`git diff --check` 通过。
- GT-013 interim 独立审查无 P0，但确认生产策略账/Gate wiring 是 P1 阻断；此外共享 accounting result 尚无费用账本，runtime amounts 来源为 `float64` 适配值而非交易所原始十进制，审计尚无专用读取 API。零手续费和零成交终态目前保守拒绝，避免把证据缺失误判成零。
