# QuantMesh 全项目审查报告（2026-09-17，基于 main @ 04d4bf4 / v3.110.0-rc3）

审查方式：5 路并行代码审查（订单/持仓核心、策略包、风控/安全、交易所适配层、盈利机制分析）+ `go test ./...`（全部通过）+ 对最高危项人工复核源码。
多路审查独立命中同一问题的，视为交叉确认，标注「✔✔」。

---

## 一、必须先修的（会直接亏钱 / 让保护失效）

### A. SHORT 方向整条链路是反的 ✔✔（三路独立命中）
| # | 问题 | 位置 |
|---|---|---|
| A1 | 单向模式浮亏统一算 `(现价 - 开仓价) × 数量`，做空时盈亏符号反。硬止损、回撤止盈、关闭条件、Web 显示全部错：做空真亏不止损，真赚反而被当亏损全平 | `position/super_position_manager.go:1855-1857` → `super_position_manager_adjust.go:64-142` |
| A2 | `LiquidateAll` 一律 `SELL reduceOnly @ last×0.99`，做空时被交易所 -2022 拒绝；且先把槽位标 Pending，失败不回滚 → 槽位永久卡死，止损条件每个 tick 重触发（内含 2s×3 睡眠并持锁） | `position/super_position_manager_reconcile.go:289-345` |
| A3 | 风控触发时撤单只撤 BUY（`CancelAllBuyOrders`），做空时撤的是平仓单，开仓单继续成交 | `symbol_manager.go:899-903`、`super_position_manager_adjust.go:1004` |
| A4 | 资金费率偏向、趋势过滤只按 LONG 语义作用于「买」，做空时方向反 | `safety/funding_monitor.go:393-413`、`super_position_manager_adjust.go:322-445` |
| A5 | 对账器拿带符号净持仓和正数本地量比，SHORT 永不同步；BOTH 模式净 0 时会把本地仓位全清 | `safety/reconciler.go:205, 319-336` |
| A6 | SHORT 的「秒成交」判定按字面 BUY/SELL 写反，已成交槽位被覆盖成 LOCKED，永远挂不出平仓单 | `super_position_manager_adjust.go:1128-1135` |

**结论：当前版本 `direction: SHORT` 不可用于实盘。** 要么先禁掉 SHORT，要么把上面 6 条一起修。

### B. 平仓 / 紧急止损路径本身有 bug ✔✔
| # | 问题 | 位置 |
|---|---|---|
| B1 | Web 平仓接口硬编码 `ClosePositions(ctx, "SELL", 100, cfg)`：不看方向、不看持仓量，直接卖 100 个单位 | `bot_manager.go:773` |
| B2 | `CloseAllPositions` 数量 = 成本价值 ÷ 现价，跌 10% 就多平 11%；限价路径 `GetLatestPrice` 返回 not implemented；`PriceDecimals` 硬编码 2；紧急平仓用 PostOnly 常被拒。熔断器平仓走的就是这条路 | `bot_manager.go:1033-1039`、`position/exchange_wrapper.go:80-84`、`position/close_manager.go:137,150` |
| B3 | 紧急中心 / 熔断平仓逐 Bot 吞错，「已平仓 0/3」也报 completed；`timeout=0` 时 ctx 立即过期，所有单直接失败 | `risk/emergency_center.go:190, 254-264`、`risk/circuit_breaker.go:342-356` |
| B4 | Binance 批量撤单 / 全部撤单吞掉所有错误永远返回 nil，残留挂单随后成交造成意外加仓 | `exchange/binance/adapter.go:614-660` |

### C. 「有风控」其实是纸面风控 ✔✔
| # | 问题 | 位置 |
|---|---|---|
| C1 | 全局熔断器 `UpdateMetrics` 只有 HTTP handler 调用，引擎内部从不喂数据 → 单日亏损/回撤/WS 断线熔断事实上不存在（已人工复核） | `risk/circuit_breaker.go:432-466`、`web/api_circuit_breaker.go:151` |
| C2 | 熔断/紧急中心的 `PauseOpening` 只改 `br.Config` 值拷贝，SPM 读的是自己的拷贝，多数情况下网格照常开仓 | `bot_manager.go:865-887` vs `super_position_manager_adjust.go:365-369` |
| C3 | 熔断 `pause_duration=0` 注释说无限期，代码 30 秒就恢复；恢复不重置计数 → 触发→平仓→恢复→重建仓循环 | `risk/circuit_breaker.go:231-241, 436-446` |
| C4 | 复合风控 `CompositeRisk`、`require_confirmation`、动态止损 `DynamicStopLoss` 全是死代码 / stub，配置开了 Web 显示已启用，实际什么都不做 | `safety/composite_risk.go`、`risk/dynamic_stop_loss.go:200-331`、`config/config.go:429` |
| C5 | 深度风控 `triggered` 是全局单值，多币种下 BTC 触发、ETH 正常就立刻解除；基线含崩盘样本约 50 秒自动解除 | `safety/depth_monitor.go:150-159, 271-280` |
| C6 | 告警 `sendNotification` 均为 TODO；价格 `lastPriceTime` 存了无人读，WS 静默停推时一直用旧价 | `monitor/watchdog.go:307-319`、`monitor/price_monitor.go:123` |

### D. 资金会被莫名其妙抽走 / 卡住
| # | 问题 | 位置 |
|---|---|---|
| D1 | 利润提取 `immediate` 规则每 5 分钟按「全历史累计盈利 × ratio」从合约划到现货，不扣已提取额，一直划到失败为止，可能把保证金抽干（已人工复核） | `profit/withdraw_executor.go:124-183` |
| D2 | daily/weekly 提取任务用每小时 ticker 的相位判断，进程启动分钟 ≥15 就永远不触发 | `profit/withdraw_executor.go:74-102` |
| D3 | 资金分配「只预留不释放」：SELL 平仓单/SHORT 开仓单被撤时 `UsedAmount` 不回收，累积到上限后连平仓单都被拒 | `super_position_manager_adjust.go:942-952`、`order_events.go:361-412` |
| D4 | `MaxAmount` 被可用余额百分比单向压低，永不回升，最终所有开仓被拒 | `position/allocation_manager.go:251-256` |
| D5 | 策略层资金分配器在 FILLED 时就释放预留，DCA/马丁累计敞口可远超分配额度（分配器形同虚设） | `strategy/multi_strategy_executor.go:368-387`、`symbol_manager.go:590-593` |

### E. 会 panic / 死锁 / 静默停摆
| # | 问题 | 位置 |
|---|---|---|
| E1 | Redis 锁 `lockKeys` map 无互斥（已人工复核：文件里没有任何 Mutex），多槽位并发下单 → `concurrent map writes` fatal，整个进程退出；TTL 5s 短于下单重试总时长且无续期；拿锁失败降级继续执行 | `lock/redis.go:18-113`、`order/executor_adapter.go:126-132` |
| E2 | 分布式锁未获取时 `PlaceOrder` 返回 `(nil, nil)`，上层解引用 panic，被 recover 吞掉后该 Bot 价格协程退出，状态仍显示运行中 | `order/executor_adapter.go:288-307`、`main_adapters_position.go:184,260`、`symbol_manager.go:878` |
| E3 | `TrendDetector.DetectTrend` 持 RLock 再调 `calculateMA`（内部再 RLock），与 `addPrice` 的 Lock 形成三方死锁 | `strategy/trend_detector.go:112-165` |
| E4 | WS 回调持 `slot.mu` 时同步做 REST（`GetAccount`+`GetPositions`），限流时成交回报积压 | `order_events.go:171-405` → `super_position_manager.go:784-838` |
| E5 | 下单超时但交易所已受理时，同 ClientOrderID 重试失败 → 槽位释放 → 新 ID 同价再挂一单，交易所两张单本地只认一张 | `order/executor_adapter.go:254-262`、`adjust.go:1084-1096` |
| E6 | ReduceOnly 被 -2022 拒就清空本地持仓，下一轮同价重新开仓，持仓被放大 | `super_position_manager_adjust.go:1059-1073` |

---

## 二、交易所适配层

**Binance（当前在用）**
| # | 问题 | 位置 |
|---|---|---|
| X1 | `futures.UseTestnet` 是进程全局，Web 查 K 线时被翻成 false → 测试网 Bot 的用户数据流重连和 WS 账户查询会连到主网，成交推送丢失 | `exchange/binance/adapter.go:180,218`、`exchange/factory.go:596-602`、`web/api.go:2854` |
| X2 | listenKey 过期后重连永远用旧 key，不重新申请 → 每 5 秒死循环，成交事件全丢 | `exchange/binance/websocket.go:214-262` |
| X3 | 最小名义金额写死 100 USDT，不足时 `Ceil` 静默放大数量（DOGE 配 20U 每层实际 100U，杠杆暴露 5 倍） | `exchange/binance/adapter.go:483-540, 1738` |
| X4 | 非 reduceOnly 市价单必然被本地校验拒绝（price=0 → NaN），目前只是没人走这条路 | `exchange/binance/adapter.go:437-531` |
| X5 | 三家都不传 positionSide/posSide/positionIdx，账户是对冲模式时每单都被拒，且不自检 | `binance/adapter.go:558-583` 等 |
| X6 | 服务器时间只在构造时同步一次，-1021 不重同步 | `exchange/binance/adapter.go:237` |

**OKX / Bybit（基本不可用，别开实盘）**
- OKX 合约面值 `ctVal` 完全没处理，下单量 / 持仓 / 成交量单位全错（`okx/adapter.go:237-289, 544-575`）
- wrapper 把 `"BUY"/"LIMIT"` 原样透传（OKX 要小写、Bybit 要首字母大写），下单被拒；反向状态 `filled/Filled` 不映射成 `FILLED`，成交永远进不了账本（`wrapper_okx.go:26-36`、`wrapper_bybit.go:26-36`）
- OKX 成交流 Symbol 是 `BTC-USDT-SWAP`，消费者按 `BTCUSDT` 比对直接丢弃（`okx/websocket.go:361`）
- 私有 WS 无重连、登录未确认就订阅（`bybit/websocket.go:265-293`、`okx/websocket.go:98-106`）
- OKX 手续费硬编码 0 且 `GetOrderFills` 返回 nil，PnL 系统性高估
- 数量/价格用 `%.*f` 四舍五入而非按方向取整

---

## 三、策略包
| # | 问题 | 位置 |
|---|---|---|
| S1 | DCA 瀑布保护暂停期间跳过止损止盈（最需要止损时不止损） | `strategy/dca_enhanced.go:385-418` |
| S2 | DCA「尾单止盈」实际是全仓清算，大亏时把整仓平掉还写「止盈」 | `strategy/dca_enhanced.go:740-747` |
| S3 | 马丁/DCA 限价单一下单就标 filled；取消不回滚总量；EXPIRED/REJECTED 平仓单让 `isClosing` 永久 true，策略卡死 | `martingale.go:472-841`、`dca_enhanced.go:584-965` |
| S4 | 组合策略按市况门控子策略时连带跳过其止损 | `strategy/combo_strategy.go:463-471` |
| S5 | 趋势跟踪在震荡判定时提前 return，跳过止损 | `strategy/trend_following.go:288-314` |
| S6 | `calculateEMA` 实际就是 SMA，`method: ema` 无效 | `trend_detector.go:141-158`、`trend_following.go:228-246` |
| S7 | 资金费套利平仓卖出现货账户全部余额（含用户自己的币） | `strategy/funding_carry_strategy.go:857-874` |
| S8 | 现货借币做空：借币成功但卖出失败不还币，下次再借 | `strategy/spot_short.go:246-268` |
| S9 | 多策略执行器用「策略名含 short」判断是否开仓，组合策略下空头子策略绕过资金校验 | `strategy/multi_strategy_executor.go:96-129` |
| S10 | 动态调整极端分支把参数设成未校验的 Min/Max，未配置时写 0；`CalculateUtilization` 是 `return 0.5` 占位符；波动率暂停只在 Low 恢复，`CheckAndResumeOpening` 无人调用 | `strategy/dynamic_adjuster.go:333, 570-615, 731-754, 930-980` |
| S11 | 多处无锁写共享状态（`currentTrend`、`cfg.Trading.*`、`GetStatistics` 返回内部指针） | `trend_detector.go:225`、`dynamic_adjuster.go:331,432,513` |

---

## 四、它到底怎么赚钱，为什么会亏

**实际机制（按代码）**：单边（默认 LONG）等差限价阶梯，不是对称做市。每 50ms 在现价下方铺 `buy_window_size` 个固定金额买单，买单成交后在 `max(槽位价, 均价) + profit_spread` 挂 reduceOnly 卖单。**没有上沿**：价格涨上去卖单成交后，买窗整体跟着上移，在更高价重新建仓。唯一止损是 `grid_risk_control.stop_loss_ratio`，默认关闭。

**结构性漏钱点**：
1. **手续费不进定价**。卖价 = 买价 + spread，没减 2×fee。ETH 示例 `price_interval=2 @3000` ≈ 0.067%，扣 maker 0.04% 剩 0.027%；一旦降级 taker（0.1%）每格净亏。`feerate/` 能拉真实费率但没接进任何定价路径。
2. **PostOnly 降级是永久漏费通道**。被拒 3 次自动转 GTC 且该槽位以后都不再 PostOnly，波动大时很常见（`order/executor_adapter.go:125-190`）。
3. **趋势过滤形同虚设**。`TrendDetector` 吃的是 50ms tick，`short=10/long=30` 就是「最近 1.5 秒的均线交叉」，每 60s 更新一次；默认还关着。
4. **无库存偏斜、无上沿**。持仓 0 层和 7 层铺法一样；涨上去追高重建仓，跌下来线性累积浮亏。
5. **波动率自适应用 1 秒窗口的标准差和固定阈值 0.02 比**，几乎永远走「缩小间隔」；且改间隔不改锚点，槽位错位。
6. **回撤 kill-switch 不存在**（C1）。
7. **回测与实盘不同构**：K 线 low/high 触碰即成交、无排队/部分成交/PostOnly 拒单，且默认区间直接取回测期实际最低/最高价（未来函数）。仓库里 `backtest/BACKTEST_VS_LIVE_ANALYSIS.md` 自己也承认回测 -14%~-74% 而实盘盈利。**不要拿回测结果当参数依据。**
8. 优化器只有单次 train/val 切分，没有 walk-forward，评分必然偏好小间隔高频参数。
9. `AdjustOrders` 每 50ms 全量遍历 + 每轮 `GetAccount()` REST，限流延迟等价于额外滑点。

**README 宣称 vs 实际**：趋势过滤 / 资金费联动 / 盘口优化 / 回撤止盈的代码都在，但 `config.yaml` 里全部 `enabled: false`，且开了也因为数据源问题基本无效；动态窗口依赖的资金利用率是硬编码 0.5；熔断最大回撤只能靠外部 POST 喂值。

---

## 五、提高胜率：按性价比排序的改法

| 序 | 改什么 | 效果 | 难度 | 落点 |
|---|---|---|---|---|
| 1 | **费率感知最小利差**：启动拉 maker/taker，`getEffectiveProfitSpread()` 返回 `max(profit_spread, price×(2×fee+安全边际))`，降级槽位按 taker 算；低于阈值拒挂卖单并告警 | 直接消灭每格净亏的交易，最确定 | 低 | `position/super_position_manager.go: getEffectiveProfitSpread`，`symbol_manager.go` 注入费率 |
| 2 | **PostOnly 不降级改重定价**：被拒就往远离盘口方向移一个 tick 重挂（最多 N 次），永不转 GTC | 全部 maker，费用减半，杜绝穿价 | 低 |  `order/executor_adapter.go: PlaceOrder`，`adjust.go` 两处 `usePostOnly` |
| 3 | **先修 C1/C2/B2**：真正的账户级回撤熔断（每分钟用 `GetAccount` 权益算高水位/日内 PnL 内部调 `UpdateMetrics`），`stop_loss_ratio` 分母改账户权益，`LiquidateAll` 用盘口价 | 把「最多亏多少」变成可执行 | 低 | `main.go` 熔断初始化、`risk/circuit_breaker.go`、`adjust.go:55` |
| 4 | **K 线级 regime 过滤**：`TrendDetector` 改吃 1h/4h K 线（`exchange/binance/kline_websocket.go` 已有），ADX/EMA 斜率 + ATR 分位分 trend_up/down/range；down 缩买窗拉间隔，up 冻结上沿不追高，range 才满铺 | 补最大的漏洞：趋势里的累积浮亏和踏空 | 中 | `strategy/trend_detector.go`，`adjust.go:323` |
| 5 | **上沿冻结**：LONG 默认要求或按 ATR 自动给 `price_high`，突破后只留卖单不重建仓 | 消除高位重建仓 | 低 | `adjust.go:196-215`（已有 priceLow/High 软限制） |
| 6 | **ATR 自适应间隔（保持锚点对齐）**：`interval = k×ATR(1h)` 量化到 base_interval 倍数 | 震荡多成交、高波动少被套 | 中 | `dynamic_adjuster.go: AdjustPriceInterval` |
| 7 | **库存偏斜**：`inv = 层数/max_layers`，买窗 `×(1-inv)`、买价再下移 `inv×interval`、卖价利差 `×(1-0.5×inv)`（不低于第 1 条下界） | 持仓多时自动少买快卖 | 中 | `adjust.go` 算 `allowedNewBuyOrders` 和 `closePrice` 处 |
| 8 | **资金费进定价**：正费率买价下移、卖价上移 `rate×price×剩余小时/8`；结算前 N 分钟净多头暂停开仓（`FundingCarryStrategy.isNearSettlement` 可复用） | 长期持仓资金费成本明显下降 | 低-中 | `safety/funding_monitor.go`、`adjust.go:414` |
| 9 | **回测与实盘同构**：新写 replay 直接驱动 `SuperPositionManager`（mock executor），用 aggTrade 数据，价格穿越才成交、PostOnly 触价即拒、按量比例部分成交、区分 maker/taker，禁用未来区间 | 回测才能预测实盘，才谈得上调参 | 高 | `backtest/` 新文件 + `position/exchange_wrapper.go` |
| 10 | **Walk-forward 调参**：滚动 train 60d / test 15d，只用 test 段拼接指标选参；评分加「每格净利/手续费」项 | 消灭对小间隔的过拟合 | 中 | `backtest/optimizer/eval.go`、`score.go` |
| 11 | `AdjustOrders` 去抖：账户/杠杆缓存 5-10s，只在跨网格线或有成交时全量重算 | 减少限流延迟 | 低 | `adjust.go:860-940` |
| 12 | 把 `DynamicAdjuster` 的占位逻辑修掉或干脆拒绝启用 | 消除「默认关、一开就有害」 | 低 | `dynamic_adjuster.go:333` |

---

## 六、建议的动手顺序

1. **止血**（1-2 天）：A1/A2（或直接禁 SHORT）、B1、B2、D1、E1/E2、X2、X1。这些是「现在就可能亏钱或崩」的。
2. **让风控真的起作用**（2-3 天）：C1、C2、C3、B3、B4、D3/D4。
3. **提高胜率**（按第五节 1→2→3→5→4 的顺序，前三个都是低难度高确定性）。
4. **OKX/Bybit**：要么修完 X 节列的整套，要么在文档和 UI 里明确标「实验性，勿实盘」。
5. 回测/调参（9、10）放最后，它现在给的数字没有参考价值。

## 七、顺带提醒
- `config.yaml` 第 5-6 行有明文 API key/secret，文件已在 `.gitignore`（第 45 行）里，没进 git，但注意别复制到别处。
- `webui/package-lock.json` 和 `yarn.lock` 并存，建议删前者。

---

## 八、整改计划与进度（分支 `fix/review-2026-09-remediation`）

每轮完成后跑 `go test ./...`（必要时加 `-race`）与 `cd webui && yarn test`，通过后再进入下一轮。状态：⬜ 未开始 / 🟡 进行中 / ✅ 已完成（附说明）/ ⏭ 不修（附理由）。

| 轮次 | 范围 | 状态 |
|---|---|---|
| R1 止血 | A1–A6（SHORT 链路）、B1、B2、D1、D2、E1、E2、X1、X2 | ✅（另完成 X6、E5 部分） |
| R2 风控生效 | C1–C6、B3、B4、D3–D5、E3–E6 | ✅（另完成 S6、S9、S11 部分；E5 已在 R1） |
| R3 策略包 | S1–S11 | ✅ |
| R4 交易所 | X3–X6、OKX/Bybit 整套 | ✅（X6 已在 R1；OKX/Bybit 现货链路未改） |
| R5 胜率改进 | 第五节 1–8、11、12 | ✅（12 已在 R3 完成） |
| R6 回测与调参 | 第五节 9、10 | ✅（默认引擎仍为 legacy） |

### 逐项记录

（每轮结束时在此追加：条目编号 → 改了什么、落在哪、对应测试）

#### R1（✅ go build / vet / test ./... 通过；position、safety、lock、order、profit、exchange/binance 通过 -race）
- A1 `position/super_position_manager.go` `calculateUnrealizedPnL`：SHORT/BOTH 空腿按 (开仓价-现价)，开仓价优先 AvgBuyPrice。测试 `position/short_direction_test.go`。
- A2 `position/super_position_manager_reconcile.go` `LiquidateAll`：方向感知撤单 + 平空 BUY@last×1.01，失败槽位回滚 FREE。
- A3 `symbol_manager.go`、`super_position_manager_adjust.go`：风控撤单改 `CancelAllOpenOrders`。
- A4 `safety/funding_monitor.go` 新增 `GetSellBias`；`adjust.go` 趋势过滤/费率偏向按方向取值。测试 `safety/funding_monitor_sell_bias_test.go`。
- A5 `safety/reconciler.go` `normalizeExchangePositionForSync`。测试 `TestReconciler_DirectionAwareSync`。
- A6 `adjust.go` `isPlacedOrderAlreadyHandled`：按开/平仓腿判定秒成交与秒撤单。
- B1/B2 `bot_manager.go` `planClosePosition`、`position/close_manager.go` `PlanCloseOrder`、`position/exchange_wrapper.go` `GetLatestPrice`/`GetPriceDecimals`。测试 `position/close_manager_test.go`。
- D1/D2 `profit/withdraw_executor.go` `processRule`/`withdrawnSince`/`runScheduledTask`。测试 `TestImmediateWithdrawNoRepeatedTransfer` 等。
- E1 `lock/redis.go` 加锁、`lock/renew.go` 自动续期、fail-closed。测试 `lock/redis_test.go`（-race）。
- E2/E5 `order/executor_adapter.go` `ErrLockNotAcquired`、`findOrderByClientOrderID`；`main_adapters_position.go` nil 防护。测试 `main_adapters_position_test.go`。
- X1/X2/X6 新增 `exchange/binance/network.go`（网络认领、按实例 REST 地址、-1021 重同步）；`websocket.go` listenKey 重建与退避。测试 `network_test.go`、`websocket_test.go`。
- 遗留转入后续轮：
  - R2：`super_position_manager_order_events.go` 撤单回调按字面 BUY/SELL 判断（做空开仓单被撤会走错分支）；`symbol_manager.go`/`position` 调用方区分 `ErrLockNotAcquired` 与真实失败。
  - R4：E5 需交易所接口支持按 ClientOrderID 查单（已完全成交的单不在挂单列表）；同进程同时支持 Binance 主网+测试网需自建 WS；Binance WS 管理器 Stop/Start 后回调重复注册、外部 ctx 取消后状态不复位。
  - 行为变化：同一进程同时创建 Binance 合约主网与测试网交易适配器会报错。

#### R2（✅ go build / vet / test ./... 通过；position、safety、lock、order、profit、risk、monitor、strategy、exchange/binance、根包通过 -race；webui 144 用例通过）
- C1 `risk/metrics_feeder.go`、`main_helpers.go` `startCircuitBreakerFeeder`、`risk/circuit_breaker.go` `SubscribeConnectivityEvents`。测试 `risk/metrics_feeder_test.go`。
- C2 `bot_manager.go` `PauseOpening/resumeOpening` → spm。测试 `bot_manager_pause_test.go`。
- C3 `risk/circuit_breaker.go` `autoResumeBlockers`/`recover`，`OpeningPauseCoordinator`。测试 `risk/circuit_breaker_recovery_test.go`。
- B3 `risk/bot_actions.go`、`risk/emergency_center.go` `executeOperation`。测试 `risk/emergency_center_ops_test.go`。
- C4 `web/api_emergency_center.go` 确认校验；`risk/composite_guard.go` + `safety/composite_risk.go` `SetResultHandler`；动态止损启动告警不注册。测试 `web/api_emergency_center_test.go`、`safety/composite_risk_handler_test.go`。
- C5 `safety/depth_monitor.go` 按币种状态、冻结基线。测试 `safety/depth_monitor_test.go`。
- C6 通知接入 `notify.NotificationService`；`monitor/price_monitor.go` `IsStale`/`Stop`。测试 `monitor/price_monitor_test.go`、`monitor/watchdog_notify_test.go`。
- B4 `exchange/binance/adapter.go` `BatchCancelOrders` 错误汇总；`wrapper_binance.go` `CancelAllOrders`。测试 `exchange/binance/batch_cancel_test.go`。
- D3 `position/allocation_reservation.go`（按 ClientOrderID 记账）。测试 `position/allocation_reservation_test.go`（30 例）。
- D4 `position/allocation_manager.go` 局部有效限额。测试 `position/allocation_smart_order_test.go`。
- D5/S9 `strategy/multi_strategy_executor.go` `OnOrderUpdate`、`classifyOrder`；`symbol_manager.go` 登记对冲策略方向。测试 `strategy/multi_strategy_executor_test.go`。
- E3/S6/S11 `strategy/trend_detector.go`、`trend_following.go`。测试 `strategy/trend_detector_concurrency_test.go`。
- E4 `position/super_position_manager.go` 杠杆缓存 `leverageCacheRefreshInterval`。
- E6 `position/super_position_manager_adjust.go` `handleReduceOnlyRejection`。
- R1 遗留（撤单回调方向、ErrLockNotAcquired 调用方）已完成。
- 遗留转入后续轮：
  - R3：`position/opening_controller.go` 定时/周期规则调用 `ResumeOpening` 可覆盖熔断暂停（需分暂停来源）；combo 子策略未设置 `PositionSide`；`dynamic_adjuster.go`、`GetStatistics` 的并发问题（S11 余项）。
  - R4：交易所层发布 WS 断线/认证失败事件；`order/executor_adapter.go` 批量撤单总是 return nil；`position/smart_order_manager.go:185` 等处丢弃撤单错误；Binance 现货/杠杆适配器撤单吞错未查。
  - 已知限制：回撤高水位仅内存保存，重启重新起算；复合风控只接全局因子（按交易对因子为做多语义，未接）；配额超限触发器无数据源。

#### R3（✅ go build / vet / test ./... 通过；含 strategy、position 的 -race 通过）
- S1/S2 `strategy/dca_enhanced.go` `onPrice`、`closeLastLayer`。
- S3 `dca_enhanced.go`、`martingale.go` pending→filled 状态机；`signal_trade_helpers.go` `entryFillFromUpdate`。
- S4 `strategy/combo_strategy.go` `OnPriceChangeRiskOnly` 路由、`GetInfo` 去递归锁、`MaxExposure`/`MaxDrawdown`；Hedge 配置告警。
- S5 `strategy/trend_following.go` `OnPriceChange`。
- S7 `strategy/funding_carry_strategy.go` `recordStrategySpot`/`closeStrategySpot`/`roundQty`。
- S8 `strategy/spot_short.go` `repayAfterFailedShort`。
- S10/S11 `strategy/dynamic_adjuster.go` 边界函数、`CalculateUtilization`、`checkVolatilityPause`、`applyTradingParams`。
- 暂停来源 `position/super_position_manager.go` `PauseOpeningUnlessHeld`/`ResumeOpeningIfOwned`，`position/opening_controller.go`。
- 测试：`strategy/dca_martingale_combo_r3_test.go`、`trend_following_stoploss_test.go`、`funding_carry_spot_leg_test.go`、`spot_short_test.go`、`dynamic_adjuster_bounds_test.go`、`position/opening_controller_pause_source_test.go`。
- 已知限制：资金费套利现货记账仅内存（重启保守推导）；熔断器直接恢复开仓时会顺带清掉被覆盖的定时/周期暂停；动态调整器读取 `cfg.Trading.*` 仍未加锁；trend/mean_reversion 子策略订单未带持仓方向；马丁反向加仓与平仓数量无精度截断；平仓交易记录在下单时写入而非成交后。

#### R4（✅ go build / vet / test ./... 通过；exchange、order 的 -race 通过）
- X3/X4/X5 `exchange/binance/order_guards.go`（MIN_NOTIONAL、市价估算、持仓模式自检）、`adapter.go`；`symbol_manager.go` 对冲模式中止启动。
- E5 `exchange/interface.go` `OrderByClientIDQuerier`、`wrapper_binance.go`、`order/executor_adapter.go`。
- 撤单错误 `order/executor_adapter.go`、`exchange/binance/spot_adapter.go` `cancelOrdersSequentially`、`spot_margin_adapter.go` 杠杆批量撤单；`position/smart_order_manager.go` 记录撤单错误。
- 连线事件 `exchange/binance/connectivity.go`、`websocket.go`；`main_helpers.go` `wireBinanceConnectivityEvents`。
- OKX/Bybit `exchange/okx/mapping.go`、`exchange/bybit/mapping.go`、两个 `adapter.go`/`websocket.go`/`client.go`、`wrapper_okx.go`/`wrapper_bybit.go`。
- 测试：`exchange/binance/order_guards_test.go`、`websocket_test.go`、`order/executor_adapter_test.go`、`exchange/okx/adapter_ctval_test.go`、`mapping_test.go`、`websocket_reconnect_test.go`、`exchange/bybit/adapter_order_test.go`、`websocket_reconnect_test.go`、`exchange/wrapper_okx_bybit_mapping_test.go`。
- 已知限制：连线事件仅在启用全局熔断器时接线，多 Bot 共用一个断线计时；现货用户数据流无连线事件；OKX/Bybit 现货推送状态仍原样透传；Bybit 推送手续费为 0（由 `GetOrderFills` 补查）；OKX 手续费按 `Commission = -fillFee` 记（返佣为负数，减少成本）；部分 OKX/Bybit 旧测试仍会发起真实网络请求。

#### R5（✅ go build / vet / test ./... 通过；position、order、config、feerate、strategy、indicators、safety、根包通过 -race）
- 1 费率感知利差 `position/fee_aware_spread.go`，`symbol_manager.go` 费率注入与刷新；配置 `trading.fee_aware_spread`。测试 `position/r5_profitability_test.go`、`symbol_manager_fee_test.go`。
- 2 PostOnly 重定价 `order/executor_adapter.go`，`adjust.go`/`both.go` `makerSafeClosePrice`。测试 `order/executor_postonly_reprice_test.go`。
- 3 止损口径 `position/adjust_cache.go`（权益缓存）、盘口平仓价 `position/liquidation_price.go`；回撤熔断已在 R2 C1。
- 4/6 K 线行情识别 `strategy/regime/`、`indicators/wilder.go`；接线 `symbol_manager_regime.go`、`position/adjust_plan.go`、`position/adjust_regime.go`；配置 `config/grid_regime.go`。ADR `docs/decisions/2026-09-17-kline-regime-filter.md`、`2026-09-17-grid-regime-wiring.md`。测试 `strategy/regime/*_test.go`、`position/r5b_regime_test.go`、`config/grid_regime_test.go`。
- 5 上沿冻结 `position/adjust_plan.go`（`trading.upper_bound_freeze`）。
- 7 库存偏斜 `position/inventory_skew.go`（`trading.inventory_skew`）。
- 8 资金费定价 `position/funding_pricing.go`、`safety/funding_monitor.go` `EstimateNextFundingTime`。测试 `safety/funding_monitor_settlement_test.go`。
- 11 去抖 `position/adjust_cache.go` `shouldSkipAdjust`。
- 12 已在 R3（`dynamic_adjuster.go` `CalculateUtilization`）。
- `config/config.go` 超 3000 行，交易风控/开仓控制类型拆至 `config/trading_controls.go`（无行为变化）。
- 已知限制：新配置只读全局 `trading.*`/`funding_rate.*`，不支持按 Bot 覆盖；非 8 小时结算品种的结算时间估算有偏差；上述参数尚未经实盘校准。

#### R6（✅ go build / vet / test ./... 通过；backtest、web 通过 -race）
- 9 回放引擎 `backtest/replay/`（`sim_exchange.go`、`executor.go`、`engine.go`、`market.go`、`task.go`）；引擎选择 `backtest/engine_select.go`、`task_manager.go`、`web/api_backtest.go`；legacy 未来函数修复 `backtest/grid_adapter.go`。ADR `docs/decisions/2026-09-17-replay-backtest.md`。测试 `backtest/replay/replay_test.go`、`task_test.go`、`backtest/grid_range_nolookahead_test.go`、`engine_select_test.go`。
- 10 Walk-forward `backtest/optimizer/walkforward.go`，评分 `score.go`。测试 `walkforward_test.go`。
- 已知限制：`SuperPositionManager` 内保证金锁、冷却、`Sleep(2s)` 按挂钟时间，回放需注入 Clock 才能完全同构（回放默认关闭保证金检查）；无盘口深度/延迟模型；仅单交易对单向净持仓；walk-forward 未接入 Web 的 `UniversalOptimizer`；默认引擎在用实盘成交校准前保持 legacy。

---

## 九、整改总结

- 六轮全部完成：第一至七节列出的缺陷全部处理，未修项已在各轮「已知限制」中说明原因。
- 最终验证（2026-09-17）：`go build ./...`、`go vet ./...`、`go test -count=1 ./...` 全部通过；position、safety、lock、order、profit、risk、monitor、strategy、exchange、config、backtest、indicators、feerate、web、根包 `-race` 全部通过；`webui` Vitest 33 个文件 / 144 个用例通过。
- 版本：`3.111.0-rc1`。
- 仍需人工处理：
  1. 新功能（行情识别、自适应间隔、上沿冻结、库存偏斜、资金费定价）默认关闭，建议先用回放引擎和测试网小仓位验证再开启。
  2. 费率感知利差默认开启，间隔过小的现有配置会被自动抬高，升级前核对 `price_interval`。
  3. 同一进程不能同时运行 Binance 合约主网与测试网 Bot；账户处于双向持仓模式时 Bot 拒绝启动。
  4. `storage/sql_storage.go`（3049 行）在 main 上已超 3000 行，非本次引入，建议单独拆分。
