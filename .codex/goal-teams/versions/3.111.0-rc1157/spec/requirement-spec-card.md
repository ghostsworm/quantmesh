# 需求规格卡：策略账务错误后的人工核账与解锁

> GT-009 需求分析结论（只读调用链审查，2026-10-10）：当时现有生产接口**不足以安全实现解锁**。随后用户明确批准扩展到所有生产可达策略的订单级耐久游标读回与人工 reconciliation。以下仍是目标契约，不表示代码已经具备能力；没有覆盖到的策略必须继续 fail-closed。

## 目标

当某个自有订单策略账务执行返回错误、无法确定其是否已写入时，系统继续关闭该订单的自动恢复/新开仓路径；管理员可基于权威 venue 与账本证据完成核对，记录责任人和理由，并显式解除该订单自身的隔离。

## 核心流程

1. 策略更新错误/未确认时进入 `QUARANTINED`；不自动再次应用原始策略更新。持久化核账案后为 `RECONCILING`；管理员完成独立二次确认后，服务端可调用策略专属的幂等 reconciliation contract，按 durable cursor 精确补齐遗漏经济记录（不是重放回调）。随后再次读回 cursor/经济状态，验证成功才进入 `READY_TO_CONFIRM`；管理员完成明确解锁确认后，写入耐久审计并仅释放该订单自有 quarantine source，进入 `RELEASED`。任何不确定/失败都停留封锁态。
2. 管理员读取精确 owner-scoped 订单与 intent revision、venue 的精确订单终态和完整成交明细、持久化 fills/fees，以及策略权威状态中能对应本订单/累计更新游标的耐久记录；同时查看 gate source 的 owner 映射。证据必须来自服务端正式读接口，不能由表单勾选或自由文本替代。
3. 第一次请求需提供必填核账理由、证据引用/摘要、目标范围和预期 revision；服务端认证 admin、校验唯一归属及证据后，先耐久写入不可变核账案并返回短时/一次性确认标识。客户端不得提交并让服务端直接信任 fills/fees/策略状态；这些必须由服务端可信 adapter 按精确 owner/order 读取。第二次确认须绑定同一案 ID、revision、证据摘要及操作者，防止 TOCTOU；单名管理员可完成两步，不要求双人审批。
4. 二次确认时重新读取并比较全部权威证据及 revision。策略游标等于目标表示已计入，不重复写；游标落后时仅可使用该策略幂等 reconciliation contract 补齐精确 cursor 区间。游标超前、断档不可界定、游标/身份冲突或结果 UNKNOWN 必须拒绝。reconcile 后需重新读回并证明策略耐久状态精确包含该订单和目标游标，且 fills/fees 与 venue 完整一致；随后才可写 `READY_TO_CONFIRM`。独立解锁确认再耐久写审计并释放 source；跨存储不具事务性时须用 durable outbox/恢复协议，不能先开 gate 后补审计。
5. 解锁不重放策略回调、不推断人工声明等价于账本写入、不触发新开仓。只解除对应订单 source；其他 source（含 manual/risk/startup/trade-ledger/unknown）保持原状。账户级 ExposureBook 如仍未完成核账，继续由其独立 gate 阻止开仓。

## 精确核账证据

- 身份/归属：管理员 session 用户名；exchange、market、account scope、bot、symbol、strategy name/type、clientOrderID、venue orderID 必须唯一匹配持久化 owner intent；不得按 side/价格推断。
- Intent/运行状态：加载后的 durable intent payload + 当前 revision，执行状态、累计 executed quantity/status、`ledgerPending`/`unknown`/`settled` 标志；确认时 revision 未变化。缺记录、读错误、revision 冲突即拒绝。
- Venue：针对精确 orderID 的权威终态查询，并取完整成交游标/明细；订单仍 open、查询 UNKNOWN、成交历史分页/时间范围不完整、数量/均价/状态冲突即拒绝。
- 成交费账：数据库按完整范围读回订单全部 fills，按稳定 trade ID 去重；数量、成交金额及各币种 commission/fee 与 venue 完整历史一致。只有累计数量相等或手工附件不构成完整性证明。
- 策略经济账：必须由策略正式的 durable readback/reconciliation contract 返回已提交的 order identity + accounting cursor/version + 被该订单吸收的经济结果，并与目标 venue cumulative update 对应。`OnOrderUpdateWithAccounting` 的 `bool,error` 仅报告调用结果，不能替代独立读回证据；管理员理由、上传材料、UI 勾选及通用策略快照均不能证明本订单已耐久入账。人工补账操作使用稳定 operation ID + expected strategy revision 做幂等 CAS，不得重放原始回调；操作结果超时/UNKNOWN 时仅读回原 operation/cursor，不能新建操作盲目重试。
- 审计：核账案、证据来源和内容摘要、操作者、理由、创建/确认时间、二次确认绑定值、前后 revision、最终 allow/deny 及拒绝码全部 durable；敏感凭据不得采集。审计写入不可用则不解锁。

## 当前代码调用链与可实现性

- `symbol_manager.go:1041-1115` 是生产 `StartOrderStream` 路由：先按 symbol 过滤，再 `observeOwnedRuntimeOrder`，正成交交由 coordinator；失败闭包持有 `owned_order_update_unverified:<orderID>`，成功闭包会移除此 source。此闭包目前只是进程内 OpeningGate 状态，并无该 source 的持久化 owner registry。
- `main_execution_fills.go:142-189, 343-372` 串行处理每订单更新；account 返回 error 时只将 `runtimeOrderUpdateState.quarantined` 设为内存态，并由上述失败闭包封锁 per-order source。运行时更新阶段没有同步写入 durable quarantine case。重启后 coordinator map 不存在，恢复依赖 intent/其他启动 gate，不能把它当作可解锁审计状态。
- `execution/opening_gate.go:17-28, 52-65, 88-104` 仅提供字符串 source 集合和 Block/Unblock；没有 source 与订单/策略的耐久映射或 compare-and-release API。`symbol_manager.go:1167-1175, 1300-1309, 1498-1555` 的 `strategy_accounting_unverified` 是共享 source，多个订单/策略/运行态错误可共同持有同名 key；清除此 key 会误清其他故障，故人工解锁不得碰它。
- `order/owned_intents.go:18-34, 47-50` 的 `ledgerPending`/`ledgerPayload` 属于 execution/trade-row ledger 恢复；`order/intent_journal.go:484-525` 持久化该 hold，且 `order/intent_journal.go:219-253` 启动时会对带 payload 的 trade ledger 执行配置好的自动幂等 replay。它不是策略账错误 quarantine，不能复用作策略核账状态，也不能由本功能解除或改写。
- 策略层 `strategy/strategy.go:32-36, 460-516` 暴露 `OnOrderUpdateWithAccounting(bool,error)`，未提供与该订单/累计 cursor 对应的权威 durable readback 接口。可选 `RuntimeStateStore`（`strategy/runtime_state.go:13-19`）是通用策略快照存取，不构成订单级核账证明。现有 `/api/capital/retired-equity-accounts` admin 路由（`web/server.go:583-585`；`web/api_retired_equity_accounts.go:66-139`）只可借鉴 admin session 校验，未提供订单账本接口或人工核账协议。
- 因此**仅用现有生产 API 不可安全实现核账完成与解锁**。先须增加耐久 per-order quarantine/audit 案、gate-source owner 的可恢复精确映射，以及策略正式 order/cursor readback/reconcile contract；缺任一项时仅能保持封锁并返回 `503 reconciliation_unavailable`（读证据不可用）或 `409 evidence_incomplete/state_conflict`（状态不符），不能提供绕过入口。

## 必须拒绝

- 未认证/非管理员/local dev 模式、缺少理由或证据、错误确认 token/重复状态转换。
- 不唯一的账户、Bot、策略、symbol/market、client/venue order ID 归属。
- venue UNKNOWN、未终态、成交/费用覆盖不完整、策略账无订单级 durable readback、策略账仍不一致、intent revision 冲突、durable quarantine/source owner 缺失、存储/审计不可用。
- 解除另一订单、`manual` pause、全局风控、启动 ExposureBook 或其他组件拥有的 gate。

## 约束与验收

- 不自动重放可能已成功写入的策略账；不把人工声明伪装成策略层 durable write。
- 证据和操作记录经数据库持久化，重启后仍可查；写入失败时 gate 不变。
- 错误语义稳定可审计：401 未登录；403 非管理员/local-dev；404 无精确 owner intent；409 状态/revision/证据不匹配或非待核账；422 理由/证据格式不合规；503 权威读回、数据库或审计不可用。所有拒绝均保持 quarantine，不返回可误解为已核平的成功状态。
- 仅授权管理员操作；所有新增前端文案经 i18n。
- 测试覆盖真实 handler/runtime wiring、进程重启边界、并发幂等及保护性减仓路径。

## 非目标

不连接真实交易所账户、不下单、不改变策略决策、不自动撤单、不清理未知委托、不宣称真实实盘或盈利验收。
