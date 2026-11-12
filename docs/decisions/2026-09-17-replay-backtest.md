# ADR 2026-09-17：实盘同构回放回测引擎与 walk-forward 调参

- 状态：已接受（2026-11-05 更新：`3.112.0-rc1` 网页 grid 优化接入 replay walk-forward；2026-11-12 更新：`3.112.0-rc4` 起该入口只读本地已校验行情。普通回测任务默认仍是 legacy）
- 关联：`docs/audits/2026-09-17-full-audit.md` 第四节第 7、8 条，第五节第 9、10 条
- 代码：`backtest/replay/`（新包）、`backtest/engine_select.go`、`backtest/optimizer/walkforward.go`、`backtest/optimizer/score.go`、`backtest/grid_adapter.go`

## 背景

1. `backtest.RunGridBacktest` 用 K 线 low/high 触价即成交，没有排队、部分成交、PostOnly 拒单，maker/taker 不分，与实盘 `SuperPositionManager` 的挂单逻辑（窗口、费率感知利差、PostOnly 重定价、去抖）完全是两套代码。
2. 未填 `price_low/price_high` 时直接取回测期内的实际最低/最高价作为网格区间——未来函数。
3. 优化器只有一次 train/val 切分，评分没有手续费项，天然偏好「间隔小、成交多、利差被手续费吃光」的参数。

## 决策

### 1. 新包 `quantmesh/backtest/replay`，直接驱动实盘 `position.SuperPositionManager`

- `backtest` 包不能 import `position`（已存在 `position → storage → backtest`），所以回放引擎放在子包；
  `TaskManager` 通过 `SetReplayRunner(replay.RunGridTask)` 注入（`web.SetBacktestTaskManager` 中完成）。
- 组成：
  | 文件 | 职责 |
  |---|---|
  | `sim_exchange.go` | 模拟合约交易所：挂单簿、撮合、净持仓/均价/已实现、手续费、资金费；实现 `position.IExchange` |
  | `executor.go` | 实现 `position.OrderExecutorInterface`，下单语义与 `order.ExchangeOrderExecutor` 一致 |
  | `engine.go` | 回放主循环、权益/回撤/敞口统计、结果组装 |
  | `market.go` | aggTrade → Tick；K 线 → K 线内路径 |
  | `task.go` | 回测任务参数 → Bot 配置；`RunGridTask` |
- 驱动方式与实盘相同：
  - 启动：首个 tick 调 `Initialize(price)`（空仓启动）；
  - 价格：每个 tick 距上次调用 ≥ `adjust_interval_ms`（默认 50ms，对应实盘价格循环）时调 `AdjustOrders(price)`；
  - 成交/撤单：以 `position.OrderUpdate`（`OrderID/ClientOrderID/Status/ExecutedQty 累计/AvgPrice/Commission 本次/RealizedPnL`）调用 `OnOrderUpdate`，与 WS 推送同形；
  - 费率：`SetFeeRates(maker, taker)`，与 `symbol_manager` 注入真实费率一致，费率感知最小利差生效；
  - 注入点：`Config.Setup` 在注入模拟时钟与费率之后、`Initialize` 之前调用；`Config.OnTick` 每个 tick 在撮合回报之后调用；
  - 订单清理：`Config.OrderCleaner` 开启时，每经过 `order_cleanup_interval` 模拟时间在 `AdjustOrders` 之前同步执行一轮 `safety.OrderCleaner`（与实盘 `symbol_manager` 启动的清理协程同规则）。
- 回报在 `AdjustOrders`/`Initialize` 返回后才投递（模拟 WS 异步；同时避免 `LiquidateAll` 持槽位锁撤单时同步回调死锁）。

### 2. 撮合模型（`MatchingConfig`）

| 规则 | 说明 |
|---|---|
| 穿价才成交 | BUY：成交价 < 挂单价；SELL：成交价 > 挂单价。成交价为挂单价，收 maker 费 |
| 触价不成交 | 成交价 == 挂单价（半个 tick 容差）默认不成交 |
| 排队模型（可选） | `queue_factor > 0`：触价成交量先消耗 `挂单量 × queue_factor` 的排队量，剩余量按参与率成交 |
| 部分成交 | 每笔成交的可分配量 = 成交量 × `participation_rate`（默认 1.0），按价格优先依次分配给多个挂单；累计 `ExecutedQty` 推 `PARTIALLY_FILLED/FILLED` |
| PostOnly 拒单 | 下单时 BUY ≥ 最新成交价 / SELL ≤ 最新成交价 → `-5022` 拒单；执行器按实盘规则向远离盘口移 1 tick 重挂，最多 `post_only_reprice_max_attempts`（默认 3）次，永不降级 GTC |
| 非 PostOnly 交叉单 | 以最新价 taker 立即全部成交（如 `LiquidateAll` 的止损单） |
| reduce-only | 方向与净持仓相反且数量不超过净持仓，否则 `-2022`，进入 `ReduceOnlyErrors` |
| 手续费 | maker/taker 分开计费与统计；回报 `Commission` 为本次成交手续费 |
| 资金费（可选） | 跨越 UTC 00/08/16 结算点时 `净持仓 × 最新价 × 费率`，多头在正费率时支付；有序列用序列（最后一个生效时间 ≤ 结算点），否则用固定 `funding_rate` |
| 保证金（可选，默认关） | `enforce_margin=true` 时 可用 = 权益 − 持仓保证金 − 挂单保证金，不足则 `-2019` |

数据源优先级：`params.aggtrade_dir` 下的 Binance aggTrades（任务时间范围）> K 线内路径回退。
K 线路径：`auto` 阳线 O→L→H→C、阴线 O→H→L→C（也可固定 `OHLC`/`OLHC`），每段 `intrabar_steps_per_leg` 步线性插值，成交量均分。路径是假设，只用于没有 tick 数据时。

### 3. 输出指标（`replay.Metrics`，写入结果 JSON `replay_metrics`）

净盈亏（已实现 + 未实现 − 手续费 − 资金费）、maker/taker 手续费、资金费、最大回撤（绝对值与百分比，逐 tick）、成交次数/maker 成交/taker 成交/部分成交、maker 占比（按成交量）、下单/撤单/PostOnly 拒单/重定价/最终拒单/reduce-only 拒单、完成格数（减仓成交次数）、每格净利、每格手续费、每格净利/手续费、敞口摘要（最大净持仓与名义价值、时间加权平均名义敞口、持仓时间占比、按 `EquitySampleMs` 采样的时间序列，最多 2000 点）。
同时返回与 legacy 同结构的 `backtest.BacktestResult`（权益曲线、成交、通用指标），报告生成无需改动。

### 4. 去掉未来函数

`RunGridBacktest` 未填区间时改为 `起点开盘价 × (1 ± auto_range_ratio)`（默认 10%），不再读取回测期内任何后续 K 线；回放引擎不推导区间，未填即不限制（与实盘一致）。

### 5. Walk-forward（`optimizer.WalkForwardConfig`）

- 滚动窗口：train `[s, s+60d)`、test `[s+60d, s+75d)`，`s` 每折前移 `step_days`（默认 = test_days=15）；当前要求 `step = test`，保证测试窗口连续且不重叠，避免汇总风险指标把不等时间间隔误当等间隔收益。只保留测试窗口完整落在数据内、且训练/测试 K 线数足够的折。
- 每折在训练窗口上对全部候选参数回测并按 `CalculateScore` 选最优，然后只在测试窗口评估该参数；
  最终 `Score/Metrics` 由各测试窗口的权益曲线按复利拼接后计算，训练结果只出现在逐折明细里。
- 接入 `GridSearchOptimizer`（`OptimConfig.WalkForward.enabled=true`），`OptimResult.WalkForward` 返回逐折结果，`BestParams` 为最后一折选出的参数；贝叶斯/遗传暂不支持（明确报错）。原单次切分 `ValidationRatio` 路径保持不变。
- 评分新增「每格净利 / 手续费」项：`GridNetProfitFeeWeight(=1.0) × clamp(ratio, ±10)`，其中 ratio = (平仓 PnL − 开仓腿手续费) / 总手续费，无手续费时为 0。`backtest.Metrics` 新增 `total_fees`、`grid_net_profit_to_fee_ratio`。

### 6. 入口

- `POST /api/backtest/tasks` 新增可选字段 `engine`（`legacy` 默认 / `replay`），写入 `params.engine`（随 params 持久化）。
- 回放任务参数：`grid_spacing`（必填，或给 `price_low/price_high/grid_count` 推算）、`profit_spread`、`buy_window_size`/`sell_window_size`（默认 `grid_count` 或 10）、`order_quantity`、`direction`、`maker_fee_rate`、`taker_fee_rate`（缺省取 `fee_rate`）、`participation_rate`、`queue_factor`、`fill_on_touch`、`intrabar_path`、`intrabar_steps_per_leg`、`adjust_interval_ms`、`funding_rate`/`funding_enabled`、`price_decimals`、`quantity_decimals`、`aggtrade_dir`、`enforce_margin`、`post_only_reprice_max_attempts`、`order_cleaner`（缺省 true）、`order_cleanup_threshold`、`cleanup_batch_size`、`order_cleanup_interval`（秒，缺省 60）；杠杆取任务 `leverage`。
- Go 调用：`replay.RunAggTrades(cfg, rows)`、`replay.RunCandles(cfg, candles, opt)`、`replay.RunTicks(cfg, ticks)`；`optimizer.RunWalkForward(ctx, symbol, candles, candidates, wfCfg, lambda, capital)`。

## 已知差距（仍不同构的地方）

1. ~~**牆钟时间**~~（已解决，见审计文档第八节「后续：时钟注入」）：`position.Clock`（`position/clock.go`）通过 `SuperPositionManager.SetClock` 注入，回放引擎用 `replay.SimClock`（`backtest/replay/clock.go`）按 tick 时间戳推进。保证金锁、reduce-only 冷却、去抖兜底、账户/杠杆缓存 TTL、撤单等待 `Sleep(2s)`、订单簿优化间隔、成交频率统计、资金分配紧急冷却、开仓控制器定时/周期规则、regime/智能挂单/自动重建后台循环均按模拟时间生效；`Sleep` 不阻塞牆钟，只推进模拟时间（时钟单调，更早的 tick 不回拨）。回测任务 `enforce_margin` 缺省改为 `true`（直接构造 `replay.Config` 时零值仍为关闭）。仍按牆钟的：网络超时（`context.WithTimeout`）、`ClosePositionManager`/`PlanManager`（手动平仓与仓位计划，不在回放中驱动）、构造时 `lastReconcileTime` 初值。
2. ~~**ClientOrderID 唯一性**~~（已解决）：`utils` 订单号生成器改为「逻辑秒单调 + 每秒最多 999 个序号，用尽借用下一秒」，同进程内 (秒, 序号) 不重复，格式与长度不变（旧格式 / OKX 3 位十进制，紧凑格式 2 位 base36 不再截断）。代价：突发 >999 单/秒时 ID 内时间戳可能超前牆钟数秒（不参与交易决策）。
   - 副作用：模拟时钟的 `Sleep` 推进时间后，回放中紧随其后的 tick 在「被阻塞」的时间窗内仍会被撮合，而实盘同期价格循环被阻塞；影响仅限撤单等待的 2 秒窗口。
3. **盘口**：没有买卖价差与深度，PostOnly 是否交叉按最新成交价判断；非 PostOnly 交叉单假设深度无限、按最新价成交（无冲击成本）。`GetOrderBook` 返回最新价 ±1 tick 的合成盘口，订单簿优化（`orderbook_optimization`）在回放里无意义。
4. **队列位置**：排队模型只按「触价成交量 ≥ 挂单量 × 系数」近似，不跟踪挂单时刻的真实队列长度与撤单；穿价成交按参与率分配，不区分主动方向（aggTrade 的 `isBuyerMaker` 未使用）。
5. **延迟**：下单/撤单/回报都是零延迟（下一个 tick 就生效），实盘有网络与限流延迟（`order` 执行器 25 单/秒、PostOnly 重挂间隔 100ms）。
6. **范围**：单交易对、单向净持仓合约；现货预算裁剪、BOTH 双腿记账、强平、ADL、资金划转、对账器（`reconcile`）、旧 tick 级趋势检测器、资金费套利未接入回放（未注入时对应逻辑自然关闭）。
   - ~~regime 检测器 / 资金费监控器无法注入~~（已解决，见审计文档第八节「后续：回放注入点与订单清理器、补跑校准」）：`replay.Config.Setup(spm, clock)` 在 `Initialize` 前调用，可 `ConfigureRegimeControl`、`SetFundingMonitor`；`replay.Config.OnTick(now, price)` 每个 tick 在撮合回报之后、订单清理与 `AdjustOrders` 之前调用（不持锁），用于在模拟时间上同步驱动 `Detector.Refresh`、`RefreshRegimeInterval`。`RunRegimeControlLoop` 在 `SimClock` 下按模拟时间触发（有测试），但协程与主循环的相对顺序不确定，需要可重复结果时应在 `OnTick` 同步调用（`tools/replaycompare/hooks.go` 即如此）。回测任务入口（`RunGridTask` / `task_features.go`）已注入 regime_filter、adaptive_interval、upper_bound_freeze、inventory_skew、fee_aware_spread 与资金费定价/结算，旧“Web 回测仍为关闭”的判断已过时。网页样本外优化复用这些现有功能的参数子集，但主动禁止资金费预测/偏置信号，只允许预先声明的费用结算。
   - 资金费监控器 `replay.SimFundingMonitor` 的「当前费率」是最后一个已结算费率（无未来函数），实盘读的是交易所当期预测费率，费率变化时相差一期；序列结束后沿用最后一个费率。
   - ~~订单清理器未接入~~（已解决）：`replay.Config.OrderCleaner=true` 时按 `timing.order_cleanup_interval` 模拟时间同步执行 `safety.OrderCleaner.CleanupOnce`（任务参数 `order_cleaner` 缺省 true）。此前回放不清理，单边行情中远端挂单累积到 `order_cleanup_threshold` 后网格永久停止开仓，趋势段绝对盈亏失真（见校准报告第三节 3.2）。清理与 regime 控制在 tick 之间同步执行，没有实盘协程之间的并发与延迟。
7. **K 线回退路径**是假设的价格顺序，同一根 K 线内先低后高还是先高后低会显著影响网格成交；有 aggTrades 时应优先使用。
8. **walk-forward**：旧 `GridSearchOptimizer` 的 legacy 分支保留。`3.112.0-rc1` 新增 `UniversalOptimizer` 的 replay 分支，接入网页 `/backtest` 参数优化和持久任务 `/api/backtest/optim/tasks`，协议见下节。样本外只评估训练胜者，不按测试结果再排序；首版限制搜索预算，不能据此宣称适合大规模优化。
9. 普通回测任务默认引擎仍是 legacy；新建网页 grid 优化任务必须显式选择 replay walk-forward，不能静默回落。没有据此切换实盘或任何交易策略。

## 2026-11-05：网页 replay 样本外协议 v1

- 请求：`search_space.replay_walk_forward = {train_days, test_days, step_days, leverage, base_params}`，随原搜索空间 JSON 持久化。无需数据库迁移；持久化不代表进程崩溃后自动断点续算。
- 固定基线由 base_params 预先声明；候选仅搜索已有 replay 的 spacing/spread/quantity/window/layer 等数值参数，最多 32 组、12 折。禁止 legacy 成交量风控轴、未知参数、外部 aggTrade 路径及资金费预测配置，全部在运行前显式拒绝。
- 数据先复制并规范排序、校验完整 OHLCV、固定周期、UTC 周期对齐，窗口长度必须为完整 K 线整数倍。train/test 为相邻半开窗口、step=test；拒绝缺口和重复，末根完整 K 线可进入窗口，跨界未闭合 K 线不能进入。
- 每个候选、每个窗口创建全新 replay/特征状态；warm-up 只允许窗内且在当前市场时间前闭合的 K 线。撤单等待推进的操作时钟不得提前开放 K 线特征。
- 训练评分固定为净收益率减逐 tick 最大回撤（均为百分比），相同分数保留规范枚举顺序；测试窗只运行一次训练胜者。测试结果不能反向改选参数。
- 所有窗口同额本金、空仓开始，期末按市值记账，不强平、不跨窗复利；输出选中参数的训练/测试和固定基线的训练/测试。成本分列 maker/taker 手续费和资金费；未实现盈亏及期末持仓单列，滑点/冲击未建模不应解释为已证实零成本。
- 结果 `replay_walk_forward` 保存协议、切分、候选训练指标、冻结参数以及数据/配置/参数/实验 SHA-256。`best_by_return/best_by_sharpe` 不用于该模式，避免误称样本外最佳参数。
- 固定费率为预声明的成本假设，历史 funding_series 仅结算，不把最终费率当提前已知的预测信号。缺历史预测费率、盘口、真实排队/延迟/冲击仍属明确限制。
- 复现：`ruby scripts/verify_replay_walkforward.rb <新建且被git忽略的输出目录>`，生成三种趋势的确定性合成样本及 JSON/Markdown 报告。合成实验与泄漏负例只证明工程接线，不证明真实市场收益或实盘盈利。报告见 `docs/reports/2026-11-05-replay-walkforward.md`。

## 2026-11-12：网页 replay 只读已校验的本地行情

- `3.112.0-rc4` 起，带 `replay_walk_forward` 的优化任务只读 `backtest_data.binance_vision_dir`（缺省 `data/kline/binance_vision`，位于已忽略的 `data/` 下）里的 data.binance.vision U 本位合约 1 分钟文件。每个数据文件旁必须有 `sha256sum` 格式的 `<文件名>.CHECKSUM`，哈希覆盖实际读取的字节。
- 窗口覆盖到的每个 UTC 日都必须有且只有一个 csv 或 zip；同一天两者都在、缺一天、当天不足 1440 根、时间戳重复或校验和不符，整次失败。不回退到交易所接口，不补造 K 线，月度包不能顶替缺失的一天。请求的更长周期按完整 1 分钟桶聚合，桶内缺一根即失败，不用会吞掉缺口的聚合。
- 资金费目录按同一规则读取。调用方已经开启资金费且序列非空时，才把历史结算序列写入本次运行的 `funding_series`，只计结算成本；目录缺失或为空时沿用调用方的固定费率，不发明零费率。不写 `mark_price`，不改 `funding_enabled`、`funding_rate`、`funding_pricing` 或方向。
- 结果新增 `data_source`、`verified_files`、`funding_files`，并在限制里标明 `binance_vision_um_1m_checksum_verified`。这些字段只在有来源时进入实验摘要，因此没有来源的运行保持原有实验哈希。来源信息不写入 `base_params`，也不回写已持久化的搜索空间。
- 普通回测任务与不带 replay 的优化任务仍走原有未校验缓存和交易所下载，本轮不改。已校验只说明文件与官方清单一致且覆盖完整，不说明盈利，也不代替实盘验收。

## 2026-11-12：单独的行情获取命令

- `3.112.0-rc5` 新增 `tools/visionfetch`，只下载 data.binance.vision 的 U 本位合约日线 1 分钟压缩包、重叠月份的资金费压缩包，以及它们的官方 `.CHECKSUM`。`-from` 含当天，`-to` 不含当天。
- 文件先写到回放不扫描的 `.partial` 目录。官方校验文件的哈希、两个空格和最终文件名都吻合后，才用独占创建放到最终位置。重定向一律拒绝。任一文件失败，本次新建的最终文件全部删除，已经存在的文件不覆盖、不删除。
- 这个命令不启动回放，回放任务也不调用它。退出 0 只表示请求范围内的文件都已校验。
- `3.112.0-rc6` 起，每个合约日文件还要通过 `visionfeed.CompleteUTCDay`：文件名决定 UTC 日，内容必须正好是这一天的 1440 根 1 分钟 K 线。缺一根、重复、越界或无法解析都失败，本次新建的文件全部删除。回放读取使用同一函数。资金费文件不按分钟要求。
