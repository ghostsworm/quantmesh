# GT-001 需求规格卡：普通 Bot 启动委托恢复

版本：`3.111.0-rc1156`　状态：需求分析完成，已由独立评审补充核验（实现前待 Goal Lead 架构定稿）

## 目标与边界

让普通 Bot 在启动或运行期间遇到本 Bot 自有的未完成委托时，只能依据可重放、可核验的归属与经济账证据完成恢复；`ExposureBook` 必须在仓位、策略库存及未完成订单风险均可解释后才初始化并释放 `exposure_bootstrap_unverified`。订单终态不是经济结算证明。适用 grid 与非 grid 策略、进程重启、重复/乱序事件。禁止真实账户访问、下单/撤单、猜测 venue 能力、把 UNKNOWN 当成未发送或零成交，或以人工跳过门控作为成功。

## 现有调用链与证据边界（Doc Capsule）

- owner scope 必须完整匹配 `Account/Exchange/Market/Symbol/Bot`；journal 按 scope 加载（`execution/intent_journal.go:14-34`、`order/intent_journal.go:103-125`）。
- 启动在提交快照协调下加载 journal、核 venue 持仓/策略库存/全部委托，最后 Seed ExposureBook；任一 open order（含已知 owner 单）都会拒绝 Seed（`main_exposure.go:34-42,44-83,103-125`；协调：`order/shutdown.go:39-61`）。
- 重启时未结 intent 保留 `IntentRecoveryBlock`；PREPARED、未结成交等成为 UNKNOWN。零成交终态需重新精确查 venue；恢复路由不代表经济账已结（`order/intent_journal.go:100-103,149-173,202-228,387-411`）。
- 活动单通过精确 intent/CID 或已记录 venue ID 验属；`OwnsOpenOrder` 只证明归属，不接纳风险预留（`order/owned_intents.go:57-83`）。运行时 `ObserveOrder` 合并状态，身份冲突、成交回退、持久化失败保持 UNKNOWN/block（`symbol_manager.go:1041-1055,1104-1133`；`order/owned_cancellation.go:117-184`）。
- 正成交终态目前的真实顺序：callback 先执行 grid/routed strategy accounting（`symbol_manager.go:1104-1127`），随后 `captureTerminalOrderAndSettleOwnedIntent` 封锁 admission 并异步补齐权威 fills/费用/盈亏（`main_execution_fills.go:89-143,146-188`）。fill ledger 成功后才 durable settle intent 及触发 retry。此顺序防止 settle/Seed 早于 fill ledger，但存在 crash window：策略状态已落盘、成交/费用 ledger 尚未落盘；各策略能否安全幂等重放尚无本切片端到端证据。GT-002 将其列作待验证风险，不能宣称错账或已安全。策略重启恢复是专用实现（如 DCA、Martingale：`strategy/dca_reconcile.go:157-218`、`strategy/martingale_reconcile.go:440-483`）。
- **补充 P1：**正常运行时 `SettleIntent` / `SettleZeroFillIntent` 将结算结果耐久写入 journal，却把 `settled` intent 留在 executor 内存 map；完整 bootstrap retry 再调用 `ConfigureIntentJournal` 时，被 `len(oe.intents) != 0` 拒绝。恢复专用 `SettleRecoveredIntent` 会删除 map 项，因此既有 helper 测试未复现该生产差异（`order/owned_intents.go:234-250`、`order/intent_journal.go:103-116`、`order/recovered_intent.go:91-101`）。修复需加入同 scope/backend 的安全幂等再入语义；有其他未结项时保留 journal loaded 状态且拒绝 Seed。不得清除 owner map 或放宽不同绑定/UNKNOWN 防护。
- Coordinator 用互斥锁串行 Retry，策略 Ready 后重跑完整库存加载和 bootstrap；已有触发点为策略启动、正成交结算、SpotShort reconcile。现有 bootstrap 测试直接调 helper，未覆盖订单流生产接线（`main_exposure_strategy.go:17-46,110-123`；`symbol_manager.go:1158-1164,1502-1509,1586-1595`；`main_exposure_test.go:409-491`）。

## 状态/证据契约

| 状态 | 可接受证据与成功条件 | 失败/未知时 gate | 重试与幂等 |
|---|---|---|---|
| 活动 owner 单 | intent scope、CID/venue ID、symbol/side/qty 与 venue 状态精确匹配。现状能验属，不能计入 ExposureBook reservation。 | gate 保持；外部/不明订单拒绝；不自动撤单。 | 重读不重复预留；每次全量快照须与 venue 单一致。接管或等待终态待决。 |
| 零成交终态（grid/non-grid） | 精确 owner intent + venue 查询确认 CANCELED/CANCELLED/EXPIRED/REJECTED 且累计量为零；策略槽位/状态先 durable 更新，再 durable settle，随后全量 bootstrap/Seed。无成交不造 fill。 | 查询、身份、策略/intent 落盘任一失败，保留对应 block 与 bootstrap gate。 | 有限退避受 context 控制；重复终态/重启幂等，不误清其他 gate。现有运行时 non-grid 通用接线缺失。 |
| 部分成交后终态 | 精确身份、venue 终态与累计量；fills 唯一且数量和吻合，含费用/资产及可得盈亏；策略库存/资金账和 intent settle 均 durable。剩余未成交量须证明不再活动。 | fills 不全/冲突、费用或策略账未核实、落盘失败均保持 execution/strategy/intent/bootstrap block。 | 按 trade ID 幂等补齐 durable cursor；累计量不回退，不重放已入账成交。 |
| 全部成交终态 | 满足部分成交证据；并验证终态语义/数量，禁止只信 websocket `FILLED`。完整 bootstrap 中 venue 仓位须与 owner inventory 精确相符。 | 任一成交、费用、策略库存或 venue 仓位证据缺失则封锁。 | 重复 FILLED/旧 PARTIALLY_FILLED 不回滚、不重计；重查精确订单/fills。 |
| UNKNOWN、重启未结/乱序 | 仅用原 scope intent 与精确 venue 查询推进；route 不等于结账；本地游标落后须补齐 fills 并由对应策略 durable accounting。 | 精确查询不支持、nil/未知、错 owner/ID/side/symbol/qty、成交回退或写入失败：保留 `IntentRecoveryBlock`/exposure gate，不 Seed。 | 重复事件收敛；旧状态不覆盖新/终态；revision 冲突须重读核对，禁止盲写。 |

## 已支持 / 缺口 / 待确认

**已有保护**：scope journal、精确 owner 路由与 open-order verifier；未知/冲突门控；零成交精确 venue 验证；正成交完整 fills 持久化；若干策略专用启动核账；retry coordinator 串行重跑完整 bootstrap。以上不代表所有策略路径均已闭环。

**待实现并由独立测试证明**：先修正 durable settled intent 的同绑定 journal 幂等再入语义，使 normal settle 后完整 bootstrap 可安全重试；有其他未结/UNKNOWN/ledgerPending intent 时，retry 不得将 journal 变成 unloaded 或妨碍后续事件完成结算。依用户确认，所有正数量的累计成交更新先耐久持久化唯一 fills/费用，再做对应策略经济账；terminal 意图只在完整成交/费用及策略状态均 durable 后结算并重跑完整 bootstrap。实现须控制异步顺序、乱序和较高累计量竞态，不因等待成交查询而阻塞全局订单流或不安全延迟保护性退出。其后补运行时 non-grid 零成交终态的通用策略结算与 bootstrap retry；grid 零成交结算成功后接入完整 bootstrap retry。任何活动委托（包括已确认 owner 的活动单）继续阻止 ExposureBook Seed；本切片不接管/计入尚无统一耐久 reservation 模型的活动订单，也不自动撤单。测试须穿过真实 production callback 接线，并覆盖重启、重复/乱序、venue/ledger/策略持久化失败及并发重试。成功只能在 owner 经济账完整且全量 bootstrap 成功时解除本流程 gate。

**开放业务问题（源码不能替用户决定）**：

1. 自有活动挂单在启动后应由 Bot 接管为持久风险预留，还是维持开仓封锁直至该单终态？若允许接管，是否要求所有策略先提供统一、耐久且可重建的订单风险预留模型？当前 `ExposureBook` 无该证明，不能自行假设可放行。
2. 非零成交部分终态后，若策略账已完整恢复但账户上仍有同 owner 的其他活动委托，是否仍禁止新开仓直至所有活动委托终态？建议默认禁止，除非活动单风险已显式纳入完整预留/额度模型；需产品/风险负责人确认。
3. 某些策略无法证明手续费/借贷负债或 owner 库存时，是否按全 Bot 持续封锁并要求人工对账？本规格建议 fail-closed；任何人工解锁语义需单独审批和审计设计，不属于本切片。

## 验收判据

每条解除路径都要证明：精确 owner scope 与 venue 身份；终态及累计成交权威；成交明细/费用已耐久（若有成交）；对应策略库存、资金/借贷账已耐久；intent settle 已耐久；全量仓位/委托快照在提交协调下完成；恢复 inventory 与 venue 一致；ExposureBook 成功 Seed；只解除本恢复流程拥有的 gate。缺任一证据、UNKNOWN、外部委托或失败均保持封锁。独立 QA 必须验证订单流/启动调用链，不以直接调用 helper 的单测替代。该验收不等同真实实盘或盈利验收。
