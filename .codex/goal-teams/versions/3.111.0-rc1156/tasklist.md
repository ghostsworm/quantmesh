# Goal Teams Tasklist

目标：闭合普通 Bot 自有活动委托在启动/运行时终态之后的策略账、成交账及 ExposureBook 核账恢复。状态：实施与定向验证中；策略记账失败后的安全恢复策略待用户选择。

## 成员责任

| Task ID | Member | Skill/Subagent | Claimed By | Status | Locked Scope | Deliverable | Done Criteria | Verification | Docs/SPEC Update |
|---|---|---|---|---|---|---|---|---|---|
| GT-001 | 需求分析-启动订单恢复契约 | `goal_requirements_analyst` | Kant (`01a12167-b892-79f3-bf00-2bd8ec6875dc`) | completed | 只读生产代码；仅可写本版本 `spec/requirement-spec-card.md` | 恢复状态转移契约 | 区分网格/非网格、零/部分/全成交、UNKNOWN、重启；列出解除 gate 所需耐久证据和失败状态 | GT-002 发现纳入规格卡 | 需求规格卡；状态由 Goal Lead 维护 |
| GT-002 | 评审-恢复安全审查 | `goal_reviewer` | Carver (`01a12167-c34a-78f1-a22d-1f153c6f78a6`) | completed | 只读；仅可写本版本 `spec/independent-review.md` | 独立调用链/锁序审查 | 已证实 P1：normal settle 保留内存项，重试因防重绑失败；另有零成交接线缺口 | Goal Lead 已交叉核对源代码 | 独立评审记录；最终 diff 需再次审查 |
| GT-003 | Goal Lead-恢复架构与规格整合 | Goal Lead | 当前会话 | completed_for_implementation | 本版本 SPEC 文档 | 已将稳定 backend identity、同绑定重入、fills/fees-first 顺序与零成交恢复写入架构草案 | 后端按契约实施；若事实与契约冲突则停止并升级 | GT-002 对最终差异复核 | 架构设计、验收 |
| GT-004 | 测试-订单恢复生产接线 | `goal_qa` | Wegener (`01a1216c-d0bb-7991-86ef-9e3309919622`) | completed | 只读现有代码/测试；仅可写本版本 `spec/test-plan.md`；实现阶段只限下方新授权测试文件 | 测试计划与现有夹具映射 | 覆盖订单流到 durable settle、重试、重启与失败封锁的因果链 | 评审-恢复安全审查 | 测试计划、进度 |
| GT-005 | 后端-委托恢复与成交顺序 | `goal_backend` | Lovelace (`01a1217a-220e-7b13-83c0-4796b5b0d4ac`) | implementation_returned_needs_fixes | `execution/intent_journal.go`、`storage/execution_intents.go`、`order/intent_journal.go`、`order/owned_intents.go`、`main_intent_journal.go`、`main_execution_fills.go`、`symbol_manager.go`；不得改策略交易决策或其他文件，越界先停报 | 跨层安全恢复实现 | stable backend identity、同绑定重入、CAS 读回、每单累计 fills/fees durable-first、零成交复查与 settle 接线；仍须闭合策略记账失败恢复和最终 review | order/execution/root callback/storage 定向测试已通过；全仓测试未运行 | 实现、进度、验收 |
| GT-006 | 测试-订单恢复生产接线 | `goal_qa` | Wegener (`01a1216c-d0bb-7991-86ef-9e3309919622`) | implemented_targeted_tests_pass | 仅新增/修改 `main_order_recovery_callback_test.go`、`order/intent_journal_test.go`；不得改生产实现或其他测试文件；测试如要求越界先报 | 生产品质接线测试 | 覆盖正/部分成交 durable 顺序、乱序/瞬时 capture failure、zero-fill 错误身份、journal 重入/CAS 与保护性关门槛 | `./order`、`./execution` 和指定 root callback/journal tests 已通过；全仓未运行 | 测试、测试计划、进度 |
| GT-007 | 评审-委托恢复最终安全复核 | `goal_reviewer` | Carver (`01a12167-c34a-78f1-a22d-1f153c6f78a6`) | re_review_pending | 只读审查同一最终差异与测试；仅追加本版本独立评审文档 | 独立代码/测试评审 | 无未处理 P0/P1；安全门控、归属、账本顺序、CAS/竞态/死锁无回归 | Goal Lead 对照复跑结果 | 独立评审、验收 |
| GT-008 | Goal Lead-版本与交付记录 | Goal Lead | 当前会话 | in_progress | `main.go`、`webui/package.json`、`CHANGELOG.md`、`PRODUCT_OVERVIEW.md`、`product-overview.md`、审计进度文档 | 版本/变更记录/验收状态 | bugfix 使用 rc1156；前后端版本一致；记录本地验证、未验证项与提交/推送状态 | Reviewer 核对记录与事实 | 版本、CHANGELOG、产品概览、审计记录 |

## 执行顺序

| Task ID | 标题 | Owner | Status | Depends On | Stop Condition |
|---|---|---|---|---|---|
| GT-001 | 定义恢复状态与耐久证据 | 需求分析 | completed | - | 关键状态无法从现有数据源证明 |
| GT-002 | 独立复核生产调用链/锁序 | 评审 | completed | - | 发现状态转移或资金所有权假设不成立 |
| GT-003 | 定稿架构、SPEC 与可执行边界 | Goal Lead | completed_for_implementation | GT-001, GT-002 | 实现需要改变确认的经济/安全契约 |
| GT-004 | 设计生产接线测试 | QA | completed | GT-001 | 无法构造无真实账户的确定性生产路径夹具 |
| GT-005 | 实现安全终态恢复与完整核账触发 | 后端 | needs_fixes | GT-003, GT-004, D-004 | 策略记账失败处理待确认，须避免自动重复计账与永久悬置 |
| GT-006 | 独立实施/运行生产接线回归 | QA | targeted_tests_pass | GT-003, GT-004 | 尚缺真实 symbol stream callback wiring/restart/race 证据 |
| GT-007 | 最终独立安全评审 | 评审 | re_review_pending | GT-005, GT-006 | 需复核最新实现，残余 P1 不可带过 |
| GT-008 | 版本、变更记录、验收交接 | Goal Lead | in_progress | GT-007 | 同一最终差异需验证且版本一致 |

## 完成标准

- 每种成功解除恢复封锁的状态，都有确切 owner-scoped intent、venue 状态/成交、策略经济状态及费用账的耐久证据。
- 零成交必须重查 venue 终态及零成交；事件重放幂等。部分成交必须保留已成交部分，并按剩余委托状态分别处置；UNKNOWN、外部单或任何查询/持久化失败都不得解除 gate。
- 恢复后仍运行完整启动 ExposureBook 流程：协调下单快照、核实持仓与所有挂单、恢复完整策略库存及风险预留、成功 seed 后才释放本流程自己的 gate。
- 测试经过实际订单流/策略 accounting/journal/bootstrap 接线，覆盖并发与重启；独立 Reviewer 审查最终代码及测试。
- 仅按实际修复递增版本并同步 CHANGELOG/Product Overview/审计记录；明确区分工作树、已提交、已推送、已发布。禁止访问真实账户、下单或宣称实盘/盈利验收。

## 决策与阻塞

| ID | 类型 | Owner | 状态 | 摘要 | 需决策 |
|---|---|---|---|---|---|
| D-001 | 用户确认 | Goal Lead | 已确认 | 用户于 2026-10-09 确认执行本版本已展示的团队分工计划 | 无 |
| B-001 | 安全停止条件 | 全体 | active | 未知归属、活动外部委托、账本缺失或 venue 数据不完整时保留封锁 | 不可人工绕过 |
| D-004 | 正成交账本顺序 | 用户 | resolved | 用户确认采用 fills/fees durable first，再策略账，再 intent settlement | 否 |
| D-005 | 跨层扩展分工 | 用户 | resolved | 用户确认授权 backend identity/CAS/event coordination 的完整实现及对应测试和独立审查；文件范围见 GT-005/006 | 否 |
| D-006 | 策略账务失败恢复 | 用户 | awaiting_answer | 错误返回无法区分未写入和写入后确认失败；自动重放有重复计账风险，永久 quarantine 会长期封锁 | 是 |
