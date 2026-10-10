# 架构设计：人工核账与精确解锁

## 现状与关键约束

`runtimeOrderUpdateCoordinator` 按 venue order 串行处理更新；策略 accounting 返回 error 后设置 `quarantined`，后续更新拒绝继续处理。生产 callback 使用 `owned_order_update_unverified:<orderID>` gate，策略路径还可能设置 `strategy_accounting_unverified`。OpeningGate 是多来源封锁，任何恢复都必须由来源 owner 精确清理。intent journal 的 `ledgerPending`、`ledgerReason`、`ledgerPayload` 是可持久化的账务恢复线索；未知结果不可被人工入口偷换成“未写入”。

## 建议分层

- **Reconciliation operation record**：SQL Storage 中新增 owner-scoped、append-only 的核账/授权记录（SQLite/MySQL 同迁移），保存目标稳定身份、expected intent revision、账本/venue 证据摘要、提交与确认状态、操作者/时间/原因、幂等键。严禁保存交易所凭据。
- **Runtime registry/command**：runtime 注册准确 quarantine 的 inspect/reconcile/release handler；操作用 order ID + CID + scope + owner + expected revision CAS fence。重启时从未结 intent/持久核账操作恢复 hold，未授权完成的 operation 继续阻断。
- **Administrator API**：复用 `/api/capital` 管理端保护模式；GET 查询待处理项与历史，POST 先记录核账证据，单独 POST 显式确认。数据库写入及 runtime transition 顺序必须可重试、幂等；缺任一能力则不解锁。
- **API 阶段边界**：先 `GET /api/capital/order-reconciliations` 查看状态/证据；再 `POST /api/capital/order-reconciliations` 创建准备案（完整 owner identity、intent revision、理由、证据引用、幂等键）；随后必须有独立操作 `POST /{case_id}/reconcile`（人工授权策略精确补缺口，服务端持久化操作 ID）；只有 reconcile 后服务端重新读回策略、fills/fees 与 intent 均匹配才签发短时确认令牌；最后独立 `POST /{case_id}/release` 显式确认解锁。不得提供单个强制解锁 endpoint。路径以现有 router 前缀实现为准，失败需稳定返回拒绝状态且 gate 不变。
- 客户端提供的 fills/fees/状态只可作为审核线索，不能成为策略补账输入的权威来源；reconcile 服务必须经当前运行时的可信 exchange/account adapter 重读完整成交/手续费，再构建 `accounting.ReconcileRequest`。若本地环境没有 adapter，不展示成功态并返回 503。
- **React/i18next 操作面板**：展示服务端证据与明确待办，理由/证据必填，二次确认显示完整 order identity；不显示无条件强制解锁按钮。

## 已批准扩展：策略账务 cursor 与受控人工 reconcile

- 为每一种生产可达策略实现共用的 `OrderAccountingStateReader` 语义：按稳定 owner/order identity 读回 durable accounted cursor、累计经济结果、持久版本及证据完整性；通用策略快照不得冒充订单级账务证据。
- 策略可提供 `ManualOrderAccountingReconciler` 语义：只接受 operation ID、完整订单身份、expected durable revision、from/to cursor 与已核实 fills/fees；以 CAS/幂等方式只补缺失区间，返回 durable receipt。不得调用原始 order-update handler，不得倒退游标；游标已到目标时只读确认，游标超前或有缺页/冲突则拒绝。
- API 必须先持久化准备案和审计，再向管理员展示并要求独立二次确认；确认时重新读取 intent/venue/fills/strategy evidence。经确认才调用策略 reconcile；调用返回不确定时维持 quarantine，重启后只通过 operation ID 和 durable cursor/receipt 读取与继续，不自动重放回调。
- reconcile 后二次 durable readback 必须证明 owner、cursor、经济结果均精确匹配目标。只有这个证明和审计 durable 后，才进入单订单 gate release；其余 gate source 不变。
- contract 建立前，策略组与后端组只可在不同文件集并行开发适配器/后台流程；共享接口变更由 Goal Lead 合并定义，禁止双方各自造不兼容签名。

## 尚需实现者证明的关键设计点

1. 实现需列出所有生产注册策略，并逐一证明 durable cursor 从哪里读、如何持久写、启动如何恢复；不支持安全 cursor 的策略继续 fail-closed，不得静默降级为“仅有快照”。
2. reconcile 必须严格使用 durable cursor 做 CAS：精确一次处理遗漏区间，且 response lost 后重复 operation ID 只读同一结果，不重复经济记账。
3. 要区分操作状态与经济账状态：提交证据不是 ledger reconciled；审批成功也不能清除 intent `ledgerPending`，除非有明确且幂等的策略/intent durable transition。
4. 如果无法以同一事务/可靠状态机原子化操作审计与 gate transition，先持久化 prepared/confirmed/released 阶段并通过幂等 reconcile 恢复，绝不采用“先开 gate 后写记录”。

## GT-009 生产策略清单与基线证据（只读盘点）

生产可达注册路径至少包含 Grid、Trend、Mean Reversion、Momentum、Martingale、DCA、DCA Enhanced、Combo、Spot Short/Long、Futures Short/Long、Funding Carry、Funding Perp Spread。策略订单 accounting 在 `strategy/strategy.go` 仍只返回 `accounted/error`。

盘点发现：Signal 类只保留当前 active order 游标，终态时会清除；DCA/Martingale 有 pending order 的成交进度但需做订单级留痕与 CAS；Grid 仅保留当前 slot/最后终态成交；Spot Long/Futures Hedge/Spot Short 与 Funding 双腿/借贷策略缺少通用订单成交、费用或终态耐久游标。strategy runtime store 基础接口是 Load/Save，CAS 依赖可选且并非所有状态更新路径一致。证据详见子代理 Rawls 的只读结论与对应源码行。补充：rc1156 工作区 `main_execution_fills.go` 会在策略 callback 前通过 `ordersync.PersistOwnedOrderFills` 将逐笔 trade ID/价格/数量/佣金写入 `order_fills`；这是未来 server-side evidence adapter 的原始成交候选来源，不等于策略经济账 cursor/CAS，也不等于交易所历史覆盖已独立验证。

因此批准范围意味着实现耐久 per-order ledger/cursor 并按策略经济语义接入；不能只暴露现有快照。若某策略本轮无法安全接入，必须返回明确 unsupported/503 并保持其订单 quarantine，最终报告不得称该策略已支持人工解锁。

## 安全不变量

fail-closed、exact owner match、no automatic replay、source-owned unblock、audit-before-release、restart reconstructability、full bootstrap remains required。

## GT-010A Grid/position 适配阻塞发现（2026-10-10）

本切片在写生产适配前停止；现有接口无法证明逐笔游标或安全人工补账，不能以普通快照写入伪装成功：

1. `position.OrderUpdate` 提供累计 `ExecutedQty`、`AvgPrice` 与单次回调费用，但没有 venue `TradeID`。同一累计回调可重复到达，不能据此生成 `accounting.Cursor` 的逐笔序号/交易身份；终态回调也不能恢复已丢失的逐笔序列。
2. `GridRuntimeStateStore` 只有 `LoadRuntimeState` / `SaveRuntimeState`。`PersistGridRuntimeState` 的进程内保存互斥不等于跨实例 durable CAS，也没有按 `OperationID` 唯一约束的收据写入。生产注入的 store 不满足 GT-010A 要求的条件写能力，故 reconcile 必须明确返回 unsupported，不能退化成 Load+Save。
3. Grid schema v5 只保存当前 slot 与 `LastTerminalFill`。slot 被复用后，旧订单终态经济上下文不再完整；该快照不足以构成订单历史或持仓周期归属证明。
4. 当前共用 `accounting.VerifiedFill` 已补充成交方向、position side 与 order role，但尚无 Grid cycle/slot 身份及其订单级耐久账本；仅凭成交价格/数量/手续费仍不能安全重建 SELL 成本基础或归属已轮换 slot。

因此 GT-010A 本轮不得新增可成功解锁的 Grid reconcile 路径。继续实现前需由共享契约/存储切片补齐：可信逐笔成交来源（含唯一 TradeID、方向及费用覆盖）、带订单终态历史的 owner-scoped durable ledger、以 revision 与 OperationID 原子去重的 CAS/receipt，以及足以归属 Grid cycle 并核验实现盈亏的证据字段。完成后再由 Grid adapter 逐笔读回、精确比对并进行 CAS；能力缺失时 gate 保持不变。当前发现不代表真实账户已核账，也不清除任何 opening gate。
