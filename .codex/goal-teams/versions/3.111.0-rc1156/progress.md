# Goal Teams Progress

## 2026-10-10 执行轮次 1：契约、独立审查与架构草案

| 成员 | 认领任务 | 状态 | 当前步骤 | 证据 | 下一步 |
|---|---|---|---|---|---|
| 需求分析-启动订单恢复契约 | GT-001 | 完成（Kant） | 规格卡交付并纳入评审发现 | `.codex/goal-teams/versions/3.111.0-rc1156/spec/requirement-spec-card.md` | 进入架构与测试计划 |
| 评审-恢复安全审查 | GT-002 | 第一轮完成（Carver） | 只读审查交付，最终实现后须复审 | `.codex/goal-teams/versions/3.111.0-rc1156/spec/independent-review.md`：normal settle 后 settled map 使 retry 被防重绑 guard 拒绝；grid/non-grid zero-fill 接线缺口 | 核对架构，再审最终 diff |
| Goal Lead-架构整合 | GT-003 | 修订中 | 依据独立评审改为同绑定 journal 幂等再入；未结 intent 时不得污染 loaded 状态 | `.codex/goal-teams/versions/3.111.0-rc1156/spec/architecture-design.md` | 与 QA 计划、Reviewer 架构复核及用户 D-004 决策对齐 |
| 测试-订单恢复生产接线 | GT-004 | 完成（Wegener） | 测试计划交付；所有测试名为建议，尚未实现/执行 | `.codex/goal-teams/versions/3.111.0-rc1156/spec/test-plan.md` | 等架构及 D-004 决策后，获准建立测试 |
| 后端-终态意图恢复接线 | GT-005 | 待派发 | 等待契约/架构门槛 | 锁定范围列于 tasklist.md；当前禁止代码编辑 | 收到 Goal Lead 解锁后实施 |
| 测试-订单恢复生产接线 | GT-004/006 | 待派发 | 测试路径盘点 | 已有 coordinator 单测直接调用 bootstrap helper，但非事件生产接线测试 | 先产测试计划；契约稳定后创建独立回归 |

### 当前证据

- 根 `main` HEAD=`ad1993bc`，本地 `origin/main` 跟踪引用相同；该观察不是本次远端实时查询，也不证明新工作已推送。
- 版本候选为 `3.111.0-rc1156`；生产文件未修改。
- 工作树可见未跟踪 `--help/`、`.codex/`，一律保留；本版本团队文件位于 `.codex/goal-teams/versions/3.111.0-rc1156/`。
- 先前已执行的相关测试不覆盖本任务完整生产事件接线；后续必须由 QA 新增/运行更直接的验证。
- 独立审查指出 helper 路径使用 `SettleRecoveredIntent` 会删 map，而生产 normal settle (`SettleIntent`) 留下 `settled=true` 项；`ConfigureIntentJournal` 的 map 非空 guard 因此让 normal-settle 后 bootstrap retry 失败。要求架构测试按正常 settle 重现，不能仅凭现有 recovery helper 测试。
- 复核架构草案后不采用“settle 后删除 map 项”；改为同 scope/backend 的安全幂等再入，保留终态 owner 路由。若尚有未结 intent，bootstrap retry 仅返回可恢复错误，不可将 `journalLoaded` 置 false，确保后续 terminal callback 仍能落盘结算。
- 当前正成交生产接线实际是 strategy accounting 先于异步 fills/fees persistence；与已确认契约文案的 fills-first 顺序不一致。需要用户确认是否将记账顺序重构纳入本切片。未作任何源码修改。

### 阻塞与决策

| 阻塞/决策 | 成员 | 影响 | 需要用户确认 | 建议 |
|---|---|---|---|---|
| 无新的外部决策；团队计划已确认 | Goal Lead | 可进入分析与只读评审 | 否 | 严格遵循现有确认范围 |
| 任何真实账户/账本证据访问 | 全体 | 不在已确认的隔离验证范围 | 是 | 禁止操作；需要时另行请求批准 |
| 共用 executor settled-map 生命周期变更 | 后端/Goal Lead | 当前原任务文件锁定未覆盖 `order/owned_intents.go` | 是（Goal Lead 解锁前禁止后端修改） | QA 先确认可重现及测试；之后逐文件更新任务锁定范围 |

## 2026-10-10 执行轮次 2：扩展授权与实施

用户确认完整实施，不仅产出架构，并确认将正成交/部分成交顺序调整为 fills/fees durable first。扩展文件分工已记录于 `tasklist.md` 与 `goal-packet.md`。

| 成员 | 任务 | 状态 | 已知证据 | 下一步 |
|---|---|---|---|---|
| 后端-委托恢复与成交顺序 | GT-005 | in_progress（Lovelace） | 首轮按原窄范围触发 stop condition，未改生产代码；用户确认扩大至 interface/storage/executor/runtime 七文件 | 在扩展范围内闭合稳定 backend identity、CAS 不确定恢复与每单累计事件串行方案，再编码/测试 |
| 测试-订单恢复生产接线 | GT-006 | in_progress（Wegener） | `order/intent_journal_test.go` 已新增 130 行同绑定重入/未结状态/错绑定无副作用测试；尚未运行 | 在两文件限权内继续补生产 callback harness 与账本顺序测试 |
| 评审-委托恢复最终安全复核 | GT-007 | pending（Carver） | 架构复核指出 binding identity、CAS restore、duplicate-settle 语义仍需实现证明 | 最终实现及 QA 验证后复审同一工作树差异 |

当前生产源码尚未修改；工作区包括上述未提交 QA 测试、版本化 `.codex` 文档和既有未跟踪资料。无真实账户/生产 DB 访问、交易、提交或推送。版本号及 changelog 仅在修复实现稳定后同步更新。

## 2026-10-10 执行轮次 3：实现复核、定向验证与待确认恢复语义

### 已完成的工作树改动（全部未提交）

- `ConfigureIntentJournal` 增加 journal identity 与 scope 的同绑定重入规则；错绑定无副作用；未结项不污染 `journalLoaded`；保留 settled owner 供幂等归属和 duplicate settle。
- CAS 写入失败后只在精确读回匹配目标 revision/payload 时确认成功，或读回确认 base 完全未变时安全重试一次；并发推进/readback 失败维持 fencing。SQL Storage 增加 backend identity 与精确 owner-scoped row readback。
- `symbol_manager` 正成交事件接入每订单串行 coordinator：成交/费用持久化 → 策略/grid 经济账 → terminal intent settle/bootstrap；零成交只对精确 owner/order/symbol/status/qty 复查后走账户账和 settle。重复 settled update 按已结算累计量跳过，超过已结算量则要求核账。每订单使用稳定 gate key，下一次核验成功可释放自己的事件 hold。
- 对无 journal、scope 不匹配的全新 executor 配置失败做 fail-closed；readback 已证实旧 revision 时安全 retry 的既有测试合同同步更新。
- 版本 `main.go`、`webui/package.json` 与产品概览同步为 `3.111.0-rc1156`；Changelog 记为开发中、未发布。

### 验证证据

- 通过：`GOCACHE=/private/tmp/quantmesh-rc1156-gocache-retry go test ./execution ./order -count=1`。
- 通过：完整根包 `GOCACHE=/private/tmp/quantmesh-rc1156-gocache-retry go test . -count=1`，107.269s；首次运行唯一失败是同后端 decorator 被误判为不同 backend，加入 wrapper identity/unwrapping 与同绑定接口刷新后复跑通过。
- 通过：`GOCACHE=/private/tmp/quantmesh-rc1156-gocache-retry go test . -run '^TestRuntimeStrategyCapitalReleaseOwnerLockDeadlinePreservesCapital$' -count=3`，验证同后端 decorator 的锁内阻塞和 deadline 路径。
- 通过：root callback tests（fills/fees-first、coordinator 串行/重试、late-fill hold 不被旧 duplicate 清除、zero-fill venue 身份拒绝）及 journal/recovery 定向集合。
- 通过：storage intent startup/CAS/pagination 及 fill idempotency/coverage 定向测试。
- 通过：针对 callback、late-fill、zero-fill 与 capital lock 用例的 `go test -race . -run ... -count=1`；不是全仓 race。
- 通过：最后一次 `git diff --check`、`./execution ./order` 定向复跑。全仓 `./...`、全仓 race、MySQL 仍未运行。首次全新根包/storage CGO 编译曾被人工中断，不计为通过或失败。

### 未闭合事项 / 用户决策

- Reviewer 发现策略 `ApplyOrderUpdateForStrategyWithAccounting` 报错后 coordinator 置 `quarantined=true`，同一订单的后续事件也无法自动重试；此设计避免不确定副作用被盲目重复，但没有形成可操作恢复通路。用户正在选择“仅经策略级幂等核验后自动重放”或“保持封锁并提供显式人工核账/解锁流程”。未取得回复前不擅自重放或清除此保护。
- 正成交阶段已按用户 D-004 实现 fills/fees-first，但跨策略状态与 fill/fee 账的进程崩溃窗口未具备原子性证明。需确认账务错误恢复策略，并由最终 reviewer 再查具体策略幂等契约；不宣称账本一次且仅一次。
- 新增 callback 测试覆盖生产使用的 coordinator/stage 构造及实际 SQL fill persistence helper，但尚未直接驱动 `StartOrderStream` 注册的完整 symbol callback；缺少重启后的策略账/fill-fee 两账 crash-replay 覆盖。Reviewer 仍将真实注册 callback 接线列为证据缺口。
- `strategyIntentSettlementBlock` 仍在 legacy helper 中使用；新的 symbol-manager coordinator 路径改用 per-order failure gate，但需 reviewer 确认所有生产调用路径不会留下旧全局 hold。

### 状态边界

当前分支 `main`；工作树改动尚未提交，未推送、未发布。未访问真实账户、真实交易所下单或生产数据库；不代表实盘或盈利验收。既有未跟踪 `--help/` 保留；`.codex/` 中本 Goal Team 版本化审计资料按任务维护。
