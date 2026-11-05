# ADR：单账户历史自然日收益与权益对账

- 状态：已实现（2026-11-05，交付版本 `3.112.0-rc1`）；隔离 SQLite 与一次性 MySQL 验收通过，未提交、未推送、未发布，未连接真实交易账户。
- 基线：`7de60665` / `3.111.0-rc1195`；交付版本 `3.112.0-rc1`。这是既有项目的功能迭代，不代表实盘盈利已经验收。
- 范围：一个明确账户范围、USDT 记账、一个完整历史自然日、隔离样本。禁止访问真实交易账户、提交、推送或发布。
- 本文件保留设计与决策记录；前后端版本、CHANGELOG、产品与架构说明已按仓库规则同步到 `3.112.0-rc1`。

## 1. 已核实的 API 约束

| 位置 | 当前事实 | 对日账的约束 |
|---|---|---|
| `web/api_profit.go:1392` | `TotalPnL + funding - marginInterest` 合成现有净收益 | 不得再次减去 `TotalFee`；必须核对成交费用确认时点是否与自然日相同 |
| `web/api_profit.go:1441` | 没有历史完整外部资金流查询，权益对账固定为 false | 不能只把常量改为 true 或用两条权益采样代替证明 |
| `web/api_profit.go:1265` | 今日/周/月使用当前配置时区且截止到 now | 一个历史日通过不代表当前日、周、月通过 |
| `web/api_profit.go:692` | 累计覆盖起点为 1970-01-01 | 日账必须有自己的明确历史起点，不冒充账户全生命周期 |
| `web/api_profit.go:302` | 当前及退役 credential scopes 共同参与现有摘要 | 单账户日账不可自动合并更换前后的账户或跨交易所汇总 |
| `web/api_profit.go:884` | 费用修正未精确到受影响 fill/time，当前会阻断整个对应 stream 的区间 | 首期保持保守门槛，不擅自放宽未知手续费规则 |
| `web/api_config.go:273` | account scope 是 exchange/testnet/APIKey 的哈希 | 不是交易所真实 UID；API Key 更换不能自行推断为同一经济账户 |
| `utils/timezone.go:12` | 默认 Asia/Shanghai | 推荐与现有产品一致，但日账独立持久化时区，不能随配置变化重解释旧账 |

### 1.1 权益、现金流与覆盖的实际底座

- `internal/app/main_equity_state.go:12` 使用固定键 `global-circuit-equity-v1` 保存当前 CAS checkpoint；`risk/equity_wallet.go:526` 会将五分钟重叠窗口以前的逐笔回执归并为累计余额/外部流并删除明细。不能依赖它还原任意一天。该五分钟重叠与利息五分钟安全延迟是不同机制。
- `storage/sql_equity_kline.go:367` 的 `account_equity_records` 只有历史权益标量。`monitor/daily_snapshot.go:304` 使用调度时间标记午夜采样，不能据此证明原子午夜快照；现有保留期也不等于来源历史完整性。
- `internal/app/main_equity_wallet.go` 的账户键/钱包键及现有 `exchange/accounting.Entry/Wallet` 十进制核验、分类和分页可复用。但 `internal/app/main_equity_observation.go:85` 目前只接受 futures 或 Bitget spot；**Binance spot/Cross Margin 尚未接入该权益证据链**。借还本金事件与应计利息存在于独立 margin/recovery 能力，不是完整账户日账。
- `funding_recovery_events` 及 `RecoveryDebtEvidence` 已保留借还本金/利息分拆、源游标与窗口，可引用并去重；其 episode 范围不能证明账户全天无其他借贷。`margin_interest_allocations` 是 Bot 分摊，账户日账只能扣 `margin_interest_payments`，不能重复累计分摊。
- `internal/app/main_helpers.go:501` 的利息区间为首次 `now−90×24h` 到 `now−5m`，增量回看24小时，实际循环六小时一次，并受 margin runtime 接入条件限制。日账不能承诺零点过五分钟就一定完成。
- `internal/app/main_helpers.go:271` 资金费首次从当前毫秒开始，**不回补新 credential scope 之前历史**；与上方旧注释“首次近30天回补”不一致，应以实现和测试为准。Fill 历史首次约30天，增量重叠10分钟（`sync/order_sync.go:141`）。三类历史起点不能互相代替。

### 1.2 复用账本与必要适配

| 事实来源 | 日账采用方式 | 不能原样沿用之处 |
|---|---|---|
| `order_fills`（`storage/sql_order_fills.go`） | 交易所 `trade_time`、原始 realized PnL、手续费及已有换算证据 | `QueryDailyOrderFillsByScope` 已半开，但须增加/组合缺失 PnL 计数，不能把 NULL 当零；先证明账户全部交易流已发现 |
| SQLite `trades` / MySQL `qm_paired_trades` | 用作策略配对与费用交叉核验 | `created_at` 可以是处理时间，费用可含前日开仓成本；不能当交易所自然日主账，不能与 fill PnL 相加 |
| `funding_payments` | 复用源身份与金额，限定范围、市场、`FUNDING_FEE`、币种 | 账户汇总方法 `[start,end]`；须新增 `[start,end)` 查询，不能改现有提现窗口语义 |
| `margin_interest_payments` | 全账户 `*` coverage，全部币种的可信 USDT 估值 | 原币汇总为 `(start,end]`，估值汇总为 `[start,end]`；日账加半开版本；UNVALUED 必须阻断 |
| 现有 fill/funding/interest coverage | 复用已完成连续窗口证据 | 只保留当前连续段，不能表达任意独立旧日回补或已覆盖段失效；需要补区间证据/异常状态 |
| `trade_fee_corrections` | 未解决则阻断，已解决的来源版本纳入输入摘要 | 修正会更新旧配对记录；仅依赖 MAX(id) 不能检测变化 |

`web/api_profit.go:798` 的全历史 raw-fill integrity 要求所有订单配对，正常未平仓订单也可能阻断；不能照搬为新的权益日账门槛。新门槛针对当日原始 fills、期初持仓和期间费用证据，不降低既有摘要/提现门禁。

## 2. 推荐、待确认的账务口径

### 2.1 账户边界

首个隔离正例推荐采用 **一个 Binance 账户作用域，固定纳入现货、Cross Margin 和经典 USDT 本位合约钱包**。三个钱包按同一清单聚合；每个钱包的资产、负债、流水均须覆盖。包括范围内非 Bot 的手工交易和资产，不能仅看当前配置的策略。跨账户、逐仓、币本位、多资产/统一账户保证金模式、多交易所和未列入清单的钱包不在首期核验范围。

这是一项**已实现的隔离证据契约**：现有 Binance spot/Cross Margin 权益来源和 margin 全账户订单历史尚未覆盖该组合。首期由隔离权威样本喂给真实存储/服务/API；缺这些来源的生产账户继续 false，不把 fixture 支持写成交易所连接器已完成。选择这一边界是为了在同一正例验证本金、利息和内部转账；若用户希望只复用当前最完整的实时来源，缩减为经典 USDT 合约单钱包更小，但无法在其范围内验收跨仓借还本金。

这是“明确钱包集合内的账户收益”，不能称为整个交易所账户所有产品收益。是否属于外部资本变动，以资金是否跨越该固定集合为准：

- 集合内两个钱包的同一笔转账，必须双边匹配后抵消；只见一边则不完整。
- 与集合外的钱包或其他账户往来，即使交易所称为 internal transfer，也属于该核验范围的外部资金流。
- 同一账户内的借还本金记录资产与负债同步变化，不算利润，也不作为需要从净权益剔除的外部资本流。
- 范围由 exchange、account scope、testnet、钱包清单及 scope revision 固定。范围中途改变的当日不标已核实；新的完整自然日使用新范围。
- 单纯 Secret/Passphrase 轮换与账户身份切换分开处理；缺少身份证据时不把两个 API Key scope 自动拼接。旧日账保留旧范围和审计版本。

可选但不推荐的首期缩减是只核 USDT 合约钱包；其现货/保证金划转都属于外部资本流，无法在同一正例充分验收借还本金。

### 2.2 时区、历史起点与截止

推荐 **Asia/Shanghai 自然日**，持久化 IANA 时区、local date、UTC start/end。事件归属采用 `[start, end)`；自然日结束用日历加一天，不写死 24 小时，避免未来支持 DST 时出错。

首日从最早拥有以下完整证据的午夜开始：范围快照、资产/本金负债/应计利息/未实现收益/价格证据、逐类连续流水覆盖、成交与费用覆盖。系统启动时刻、最早成交日期、最早一条流水均不能单独作为完整历史起点。

尊重现有利息 90 天回补与 5 分钟安全延迟。日结束至少经过安全延迟且所有源实际水位均跨过 end，才允许核验；等待够 5 分钟本身不能证明覆盖。不能通过截断所请求区间或把回补前历史当零获得完整标志。已持久化的可信旧证据不因时间过去而自动消失；但首次导入超出接口历史限制的日子没有权威补充证据时保持不完整。

现有最新权益 checkpoint 不能作为过去午夜的权益。如果没有精确历史端点或可证明的回放路径，该日不能核实。允许用有证据的期初余额和持仓作为账务切入点，无需假装账户自开户以来历史已齐全。

### 2.3 估值与费用

推荐 **USDT 本位净权益**，而非 USD 法币价值或扣除假设平仓手续费后的可提现/清算金额。USDT 按 1 USDT 记账；合约未实现收益使用同一历史 mark-price 口径；其他持有资产用可追溯的历史 USDT 价格。持有资产清单、数量、价格源、价格时刻和方法版本必须保存。

首期正例与核验支持范围推荐限定：**本金及利息负债均为 USDT；现货/Cross Margin 只持有、划转、借还，不发生需要成本匹配的现货交易**。允许有历史价格证据且数量不变的非交易资产估值。发现非 USDT 借贷/利息、现货交易库存成本未知、赠金/返佣/奖励等暂未建模收入时，明确给出不支持/不完整，不能当零后继续核实。这是为了完成本期同一账户范围的最小完整日，不宣称所有真实账户活动均已覆盖。

未来支持现货交易库存时，推荐**保留有证据的历史成本与期初浮盈**，不在切入日偷偷重置成本。现货/保证金交易库存的未实现变化必须与合约部分分别列示后合并，不能把它们误归“非交易资产估值”：例如期初成本100、市值120，日内130卖出，应为 realized30 + unrealized变化(0−20) = 权益收益10。缺成本基础则不核实。非 USDT 应计利息负债未来也必须单列估值变化：期初欠0.01 BNB、价格200→300，当日无新增利息仍有估值损失1；首期直接阻断该不支持场景。是否将这两类扩入本期，是用户可选择的范围调整。

- 权益 = 钱包资产价值 − 本金负债价值 − 尚未清偿且未已计入资产净额的应计利息 + 尚未计入钱包资产的合约未实现收益。交易所字段若已是净权益或含未实现盈亏，禁止再次加减。
- 日账合约交易净收益推荐按已有 `order_fills` 的交易所 `trade_time` 计算：当日权威 USDT realized PnL 减当日可核验手续费。未平仓的开仓费用也按发生日计入。配对成交只交叉核验，不能再次叠加。现有摘要中的 `TotalPnL` 已扣手续费，其 `TotalFee` 仍只展示一次；两种期间口径不能混用。现货/跨仓交易需要库存成本与费用归属证据，不能把买卖现金流当作 realized PnL；首期样本的这两个钱包只发生资产持有、划转及借还利息，无需假造缺失的现货交易成本。
- 利息在权威计提/发生时确认；后续偿付已计提利息不二次计费。
- 提现本金单列净外部流；**提现手续费及其他有证据的非交易费用列成本**，不藏入外部流。交易费用、资金费、利息已在各专用账本出现的经济事件不得再从通用流水重复扣减。
- 估值变化只能由具体非交易资产/本金负债头寸及历史价格独立算出；合约未实现变化单列，不能再进入估值变化。缺价格、缺成本基础、未知币种、未知收费或其他未支持收入类型都阻断核验。
- 非 USDT 资本进出应按事件时点价格计外部流；与期末价格之差归相应可证明的估值变动。首期正例以 USDT 充值提现避免扩展真实适配范围。

建议核验公式（每项均为 USDT）：

```text
adjusted_equity_change = closing_equity - opening_equity - net_external_capital
explained_profit = trading_net + funding_net - interest_cost
                 + unrealized_change + explicit_valuation_change - other_verified_costs
residual = adjusted_equity_change - explained_profit
```

已实现交易收益、未实现变化、估值变化各自有互斥归属；不能为使 residual=0 新建不明“调整收益”。如跨日费用归属需要适配，从既有 fill/fee 记录导出可逐笔解释的期间费用桥接，缺证据时不核实。

首期推荐 decimal 核验，计算与持久化不使用展示用两位小数；公开明确固定绝对容差（建议 0.00000001 USDT），只用于已知数值精度误差。旧账本 float64 的转换不能恢复原先丢失的精度，超过门槛则返回差额。不得用大额相对容差掩盖费用遗漏。

## 3. 持久化实现

目标是复用原始账本，并新增它们当前不提供的历史证据与可复算结果。

1. **范围与端点证据**：不可变账户/钱包范围 revision、历史期初期末逐资产余额/负债/未实现收益/价格证据、源 cursor/时间、内容 hash。
2. **历史现金流及覆盖证据**：保存现有采集器归一化后的事件及稳定源身份；分页窗口、完整页收据、连续覆盖起止。借还本金与内外部划转保留不同分类；空窗口也需要权威完整收据。未知类型保留为未分类并阻断，不丢弃。
3. **日账版本**：日、范围 revision、估值方法版本、输入来源 IDs/revisions/hash、各分项精确金额、coverage 各项、residual、核验状态/原因、created_at/updated_at。可再次回放获得相同结果。

实际表为 `daily_equity_evidence_versions`、`daily_equity_report_versions`、`daily_equity_current`、`daily_equity_conflicts`；迁移为 `storage/migrations/2026110501_daily_equity_{sqlite,mysql}.{up,down}.sql`，已接入 `storage/storage.go` 的启动自动 up 与失败阻止启动，down 仅显式调用。新增业务表含主键、时间戳、账户/日期索引与稳定源事件唯一身份；不修改已提交迁移。

幂等与修订规则：

- 区分**原始来源记录身份**与**经济事件身份**。来源身份由来源命名空间、scope、钱包、稳定原始ID构成；只在交易所原ID确需原始类型组合才唯一时加入不可变原始类型。unknown→fee 等业务重分类不改变原始身份，也不得生成第二个经济事件。相同身份和内容重复导入只保留一份；同身份不同内容记录冲突，不静默覆盖金额。
- 经济事件按可靠的成交/订单/交易所交易号和已有账本记录建立显式关联。权威损益来源为 fill 的交易PnL/手续费、funding payment、interest payment；通用钱包流水只是对应余额变化的第二份证据，不能再次产生费用。跨来源无法可靠关联时阻断，不按金额时间相近猜测。若一笔手续费对应多份配对记录，分摊总额必须守恒，并保留映射。
- 同一笔内部划转允许有两条钱包 leg；按转账引用匹配。同日双边到齐才可抵消；跨午夜在途转账首期保持相邻日不完整，不能把两天各自单边外部流冲出假收益。未来若引入在途资产，须单独定义端点、确认时点和状态证据。
- 借还的交易所事件及策略恢复 journal 用来交叉核验，不能分别记两笔经济变化。一次还款内本金、已计提利息是同一业务事件的不同会计分项，分别核对资产/负债，不能以重复经济事件形式全额记两次。
- 修订/迟到记录不得维持旧的“已核实”：持久化导入与受影响日账失效必须原子，或在查询时校验输入 revision 后拒绝旧结果。保留旧日账版本供审计，产生新版本后重新核验。
- 计算使用同一输入快照/来源版本，并在发布日账结果前 CAS 核对版本；部分写入、重启与重放不得产生孤立 true。
- 已核实表示完整性检查全部通过且 `abs(residual) <= tolerance`。金额碰巧相等不能弥补漏页、缺钱包、未知费用或范围错误。

## 4. API 实现

只读历史日账接口 `GET /api/profit/daily-reconciliation` 已接入 `web/server.go` 的 protected 盈利路由，要求 `date`、`exchange_id` 与 `account_scope`，返回固化时区/边界。请求的范围必须在当前会话有权读取的当前/历史范围集合内，不能凭任意 opaque scope ID 查询其他账户。日期不合法返回 400，范围未授权返回 403，缺记录返回 404，不返回“已核实零收益”；已有但不完整/源已变化的日账返回明确 false 与原因码。内部错误返回 503 而不是空账。

结果包括 `periodStart`、`periodEnd`、`timezone`、`asset`、范围标识、两端权益、外部净流、交易净收益/展示手续费、资金费、利息、未实现变化、估值变化、其他已核费用、差额、输入/结果版本、逐项 coverage 和 `equityReconciliationComplete`。对用户显示的原因码由本地化资源解释，不暴露 credential 内容或交易所原始错误。

原 `/api/profit/summary` 的 lifetime/today/week/month 只能查询相同区间、相同账户集合的核验证据后独立给出标志；不得用单日结果替换一个共用 bool。首期只有历史单日能力时，其余不具备完整证据的汇总标志继续为 false。现有总收益字段含义不变；新接口分别展示已实现净收益和权益口径总收益，避免把未实现变动误称为已实现利润或可提现金额。

## 5. 隔离样本与验收

`accounting/daily` 的黄金/反例 fixture 使用固定 `2026-11-03`（Asia/Shanghai）时钟与独立算术样本；真实存储/生产组合测试则动态选取“昨天”这个已闭合自然日，以保证利息 90 天窗口与五分钟安全延迟在任何运行日都成立。两者都不连接真实交易所。

- 自然日：`2026-11-03`，Asia/Shanghai。
- UTC 区间：`[2026-11-02T16:00:00Z, 2026-11-03T16:00:00Z)`。
- 核验时间：`2026-11-03T16:10:00Z`；全部源 covered-through 均至少到区间 end；利息回补起点早于 start 且在 90 天支持窗口内。
- 账户与所有事件 ID 为明确的 fixture 标识。与真实 API Key、账户、数据库完全隔离。

| 分项 | 样本值（USDT） | 预期处理 |
|---|---:|---|
| 期初净权益 | 1000 | 来自全部范围钱包的期初证据 |
| 充值本金 | +200 | 外部净流 +200 |
| 提现到外部的本金 | -50 | 外部净流 -50 |
| 提现手续费 | 2 | 其他已核费用 2，实际钱包扣 52 |
| 集合内 spot → futures 划转 | 100 | 双 leg 匹配，外部净流与利润均为 0 |
| Cross Margin 借入 / 归还 USDT 本金 | 500 / 200 | 资产、负债净增 300，权益/利润均不变 |
| 日内完整平仓毛收益 | 30 | 交易毛收益 |
| 同笔成交手续费 | 2 | 交易净收益 28，展示 2，不再另扣 |
| 资金费净收入 | +3 | 资金费 +3 |
| 当日利息 | 1 | 利息成本 1；偿付时不能再扣 |
| 已在期初存在的合约持仓未实现变化 | +8 | 独立历史 mark-price 证据；本日不产生未覆盖的开仓手续费 |
| 非交易持有资产估值变化 | +4 | 例如 0.1 单位资产从 200→240 USDT，独立价格证据 |
| 期末净权益 | 1190 | 与所有实际资产、负债及估值明细勾稽 |

```text
net_external_capital = 200 - 50 = 150
adjusted_equity_change = 1190 - 1000 - 150 = 40
explained_profit = (30 - 2) + 3 - 1 + 8 + 4 - 2 = 40
residual = 0
```

用 Ruby BigDecimal 检查了该恒等式，另检查了本地午夜到 UTC 的映射及日末十分钟后的核验时刻。时间检查命令前两次因当前 Ruby `-e` 源码默认 US-ASCII、输出含中文而报 `invalid multibyte char (US-ASCII)`；改为纯 ASCII 状态输出后通过。以上只验证样本算术和时间，不构成账本/API、SQLite/MySQL 或实盘验收。

### 必须包含的变体

| 场景 | 预期 |
|---|---|
| 充值、提现、非交易费用各只记一次 | 外部本金与成本可逐笔解释，residual=0 |
| 内部划转两 leg 完整 / 缺一 leg / 同金额不同转账 ID | 完整抵消；缺 leg 不完整；不同 ID 不擅自配对 |
| 从被排除的钱包转入 | 对本边界是外部流；不得与范围内转账混淆 |
| 借入500、还本金200；另偿付已计提利息1 | 本金利润0，利息累计只扣1；另拆“仅借款”“仅还本”避免相互抵消掩盖错误 |
| 期初资产1001、已应计利息1；当日仅偿付1 | 期初期末净权益均1000，当日利息成本0 |
| 手续费金额未知、币种未知、费用修正待办 | 即使差额为0，也必须 false |
| fill净收益28已扣fee2，通用流水另ID报告同一fee2 | 只关联证据，净收益仍28；不可变为26 |
| 来源先unknown后重分类fee | 修改分类版本并失效日账，不增加经济事件数量 |
| 完全相同导入重复、同 ID 内容冲突、account展示标签改变后重放 | 相同幂等；冲突阻断且不覆盖原证据；展示标签变化不能造成资金费第二次入账 |
| 范围日内新增/删除钱包，API Key scope 切换 | 该日 false；旧日账不归属新范围 |
| 空成交/空资金费/空利息窗口 | 有完整零事件证据才可认为覆盖；空查询不等于无费用 |
| 覆盖缺中间页、仅首尾记录存在、最后一页写失败 | false，水位不可跨缺口推进 |
| 当日未结束、end+4分59秒、start 超出可信历史 | false；end+5分但任何源未完成也为 false |
| 恰好 start/end 的成交、费用、转账 | start计入本日；end仅计入下一日；SQLite/MySQL一致 |
| 23:59:58成交净赚10，00:00:02才写配对记录 | 交易所发生日收益10，写入日不再次记10；重启重放一致 |
| 历史价格缺失、估值方法混用、净权益重复加未实现 | false，不以 residual 生成“估值调整” |
| 现货成本100/期初市值120，日内130卖出 | 首期返回不支持的交易库存口径；若扩展支持须30−20=10，不得报30 |
| 期初应计利息0.01BNB，价格200→300，无新计息 | 首期非USDT利息负债不支持；若扩展支持应单列估值损失1，利息发生额0 |
| 跨日合约开平仓、期初手续费已扣、当日新增未平仓 | 按fill发生日分别确认PnL/费用，期初费用不重扣；缺权威PnL、费用或期初持仓则 false |
| 迟到资金费/利息/费用修订；已核实后再导入 | 旧 true 失效，新版本完整重放后才恢复 |
| 断电/重启、两 worker 并发导入与日账发布 | 一致快照+CAS；无半账或旧 revision true |
| 历史日已核实、今日/本周/累计不完整 | 只该日 true；汇总/提现权限不得跟随变 true |

### 真实存储到 API 的验收链

实现时用隔离源 adapter → 现有账本及新增历史证据写入 → 关闭/重开存储 → 实际日账 service → 生产 router/API。只模拟外部 venue，不模拟 StorageService 或用预计算金额替代 service。SQLite 与一次性 MySQL 使用同一场景矩阵，必须明确区分 pass/fail/skip。组合测试 `TestSQLiteDailyEquityProductionComposition` / `TestMySQLDailyEquityProductionComposition` 已按此链路执行。

Ruby 验收器输出 JSON 与 Markdown，报告绑定源码指纹、版本、场景、存储后端、测试名称、实际 API 结果与错误原因。缺 MySQL 环境时结果为未验收，不允许成功退出后宣称覆盖齐全。需要验证迁移 up 重复执行、并发启动锁、故障阻止启动、隔离库内 down/up 可逆；仅在可丢弃的一次性库且明确授权该回滚测试时执行 down。

可直接复用的入口及实际限制：

- `internal/app/main_order_reconciliation_composition_integration_test.go:308` 的 `TestProductionFundingIncomeRouteProjectsAndReplaysOriginalCursorAfterAdvance`：真实 StorageService、生产 builder/router、临时认证会话、关闭重开 SQLite。新日账应建独立组合文件，参数化 SQLite/MySQL 运行同一矩阵。
- `internal/app/main_equity_wallet_test.go:482`：权益 SQLite 重启与缺流水时 revision 不推进；`internal/app/main_helpers_funding_sync_test.go:40`：利息101条跨页、页失败、并发去重。复用断言方式，不以这些既有局部通过冒充新日账组合通过。
- `internal/app/funding_carry_constructor_mysql_test.go:74`：校验回环 TCP、一次性测试 schema 命名及显式 opt-in后创建随机独立 schema，再实际 `NewStorageService`。本次验收在本机隔离 MySQL（9.5，`quantmesh_test` 下的随机 `quantmesh_daily_*` schema）上实际执行，未使用宿主业务数据；CI 目标仍为固定 `mysql:8.0.36` 服务容器。不能仅因地址 localhost 就认为可以清空。
- `scripts/verify_profit_readiness.rb` 默认只跑 TestAudit，且其普通全仓成功条件允许 skip；新验收器已采用 `scripts/verify_trading_race.rb:105` 的必需包/父子案例清单与 require-MySQL 方式，结合 `scripts/source_provenance.rb`，任何必需 skip 都不通过。仅模拟 venue 和时钟；Go 原生组合测试驱动生产逻辑，Ruby 编排并核对 JSON 事件、输出 JSON/Markdown。
- 新迁移显式接入 `storage/storage.go` 的启动流程，参考 `storage/order_reconciliation.go` / `storage/funding_recovery.go` 的 SQLite 独占事务与 MySQL 同连接锁。仓库没有通用 migrate up/down/status CLI；本次提供可审计的 Go 方法 `MigrateDailyEquity`、`RollbackDailyEquityMigration`、`DailyEquityMigrationStatus` 及 `TestDailyEquityMigrationLifecycleSQLite` 生命周期测试（up 重复执行、down/up、状态、坏 schema 拒绝），生产路径为启动自动 up；未新增独立 CLI，也未重构其他迁移。

实际实施顺序：确认本ADR → 新增历史证据/日账迁移与服务 → 既有账本半开查询及原始fill日净额 → 范围/覆盖/修订失效与重放 → protected历史日账API → SQLite/MySQL生产组合与Ruby严格报告 → 新功能版本、CHANGELOG、产品/架构最终同步。上列步骤均已落地；不得把 fixture 支持扩大解释为生产交易所连接器、真实账户或盈利验收。

## 6. 交付记录与验收边界

已交付 `accounting/daily`（精确十进制核验、规范化/摘要、服务 CAS 读取）、`storage` 日账迁移与投影/发布、`web` 只读 API、`scripts/verify_account_profit_daily.rb` 及 CI 步骤。主要证据：

- `go test ./accounting/daily ./storage` 通过；黄金日、反例矩阵、缺失转账腿、借还本金、未知费用、跨来源重复、非 USDT 估值/负债、范围修订、半开日界、5 分钟安全延迟、90 天利息历史、重复导入/冲突/重启均有专项用例。
- `TestSQLiteDailyEquityProductionComposition` 与 `TestMySQLDailyEquityProductionComposition` 通过：真实 `storage.NewStorageService` → 生产日账服务发布 → `web.SetupRoutesWithConfig` → 管理员会话 → `GET /api/profit/daily-reconciliation` → 关闭/重开存储 → 追加可变账本事实后读取为 `source_changed`。MySQL 使用一次性随机 schema，库名/回环/无密码/显式 opt-in 均由既有 fixture 校验。
- `ruby scripts/verify_account_profit_daily.rb /tmp/quantmesh-daily-equity` 返回 `accepted:true`：SQLite 与 MySQL 均 pass，12 项必需证据齐全（筛选运行 44 个用例全部 pass），无 missing/skip/fail，Go JSON 解析零错误，测试期间源码指纹稳定，前后端版本 `3.112.0-rc1` 一致。

未覆盖/未验收：生产 Binance spot/Cross Margin 权益连接器、真实账户历史回补、后台自动调度发布、活动状态下的 fail-closed 行为、部署、提现授权与任何盈利结论。单日 `equityReconciliationComplete=true` 不会改变 lifetime/today/week/month 或提现权限。验收样本只覆盖一个明确 credential scope；账户范围变更、API Key 轮换、跨午夜在途转账仍按失败/不完整处理。
