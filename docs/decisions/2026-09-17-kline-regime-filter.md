# ADR 2026-09-17：K 线级市场状态过滤与 ATR 自适应网格间隔

- 状态：已接受（模块已实现；R5 第二段已接入网格，见 `2026-09-17-grid-regime-wiring.md`，其中 Unknown 改为保持原有行为）
- 关联：`docs/audits/2026-09-17-full-audit.md` 第四节第 3、4、5 条，第五节第 4、5、6 条
- 代码：`strategy/regime/`（新包），`indicators/wilder.go`（新增 Wilder RMA/ATR/ADX、PercentileRank）

## 背景

1. `strategy/trend_detector.go` 吃的是 50ms tick，`short=10/long=30` 实际是"最近 1.5 秒均线交叉"，没有趋势识别能力。
2. 网格是单边等差阶梯、没有上沿：下跌趋势里线性累积浮亏，上涨趋势里追高重建仓。
3. `DynamicAdjuster` 的波动率自适应用 1 秒窗口标准差，且改间隔不改锚点，槽位错位。

## 决策

### 1. 独立包 `quantmesh/strategy/regime`

- 只依赖 `exchange`（`Candle` 类型与 `GetHistoricalKlines` 签名）、`indicators`、`logger`。
  `strategy` 包依赖 `position`，因此不放在 `strategy` 包内；`position` 可直接 import 本包而无循环。
- 数据源是一个最小接口 `KlineSource{ GetHistoricalKlines(ctx, symbol, interval, limit) }`，`exchange.Exchange` 天然满足，测试注入假实现。

### 2. 数据：只用已收盘 K 线，默认轮询而非 WebSocket

- 默认 `1h`，仅支持 `m/h/d` 单位（与 Unix 纪元对齐）。
- **默认通过 REST 轮询**：交易所适配器的 `StartKlineStream` 每个适配器只允许一个流（Binance 为单例 manager，重复 Start 报错），多个组件抢一个流不可靠。轮询间隔 = 周期/12，夹在 [15s, 5m]（1h → 5 分钟）。
- 如接入方已有 K 线流，可调用 `Detector.OnCandle(c)` 推送，未收盘（`IsClosed=false`）直接忽略；不连续时标记重同步，由下一次轮询全量重建。
- 时间戳归一：`openTime = ts - ts % interval`，同时兼容"开盘时间"（Binance REST）与"收盘时间 = 下一根开盘 - 1ms"（Binance WS `k.T`）两种口径；小于 1e12 的时间戳视为秒。
- **拉取路径不信任 `IsClosed`**：Binance REST 把最新一根形成中的 K 线也标为 `IsClosed=true`，因此统一以 `openTime + interval <= now` 判定是否收盘。
- 增量拉取 10 根；若拉到的窗口与缓冲不重叠（进程挂起、网络中断），全量重建（`BootstrapBars` 根）。

### 3. 指标（Wilder 口径）

既有 `indicators.ADX/ATR` 用 EMA(2/(n+1)) 平滑，与交易所/TradingView 显示值不一致；为不影响旧调用方，新增而不修改：

| 函数 | 说明 |
|---|---|
| `RMA(values, period)` | Wilder 平滑，alpha=1/period，SMA 起始 |
| `WilderATR(candles, period)` | RMA(TR) |
| `WilderADX(candles, period) (adx, +DI, -DI)` | 标准 Wilder ADX，需 2×period 根 |
| `PercentileRank(values, x)` | 百分位排名（并列取一半） |

派生量：

- `EMASlope = (EMA[t] - EMA[t-L]) / (L × ATR[t])`，单位"每根 K 线移动几个 ATR"，跨品种、跨周期可比。
- `ATRPercentile`：当前 ATR 在最近 `ATRPercentileLookback` 根 ATR 中的百分位。

### 4. 分类状态机（滞回 + 驻留确认）

每根新收盘 K 线计算候选状态：

```
若当前 = TrendUp   且 ADX ≥ exit 且 slope > 0 → TrendUp（保持）
若当前 = TrendDown 且 ADX ≥ exit 且 slope < 0 → TrendDown（保持）
否则若 ADX ≥ enter：slope ≥ +min → TrendUp；slope ≤ -min → TrendDown
否则 → Range
```

- 进入要求 `ADX ≥ enter` 且斜率强度达标；保持只要求 `ADX ≥ exit` 且方向不反转（滞回）。
- 候选与当前不同时，需**连续** `MinDwellBars` 根保持同一候选才确认切换；中途候选变化则重新计数。
- 首次预热完成（Unknown → X）立即确认，不等驻留。
- bootstrap 时按历史逐根回放状态机，所以启动后的滞回/驻留状态与"一直在线运行"一致。

### 5. 过期与降级

- `Snapshot.Stale`：距最后一根 K 线收盘超过 `StaleMultiplier × interval`（默认 2 个周期），读取时计算。
- `Snapshot.Effective()` / `Detector.Regime()`：未就绪或过期时返回 `Unknown`。**消费方必须使用 Effective 值做决策**，原始 `Regime` 仅用于展示。

### 6. ATR 自适应间隔（保持锚点对齐）

```
raw      = k × ATR                     （ATR 用 Snapshot.ATR，即 1h Wilder ATR）
clamped  = clamp(raw, min, max)        （min 默认 = base，max 默认 = 8 × base）
interval = round(clamped / base) × base，且 ≥ 1×base，并保证落在 [min, max]
          （区间内不存在 base 整数倍时取 ≥ min 的最小倍数，宁宽勿窄）
仅当 |new - cur| / cur ≥ ChangeThresholdRatio 时切换
```

因为新间隔永远是 `base_interval` 的整数倍，所有槽位仍满足 `anchor + n × base_interval`，**改间隔无需移动锚点**，已挂订单与已持仓槽位不会错位。

## 默认值

| 配置 | 默认 | 说明 |
|---|---|---|
| `kline_interval` | `1h` | |
| `adx_period` | 14 | |
| `adx_enter_threshold` / `adx_exit_threshold` | 25 / 20 | exit 必须 ≤ enter |
| `ema_period` | 50 | |
| `ema_slope_lookback` | 5 | |
| `ema_slope_min_atr` | 0.05 | 每根 0.05 ATR |
| `min_dwell_bars` | 3 | 1 = 立即切换 |
| `atr_period` | 14 | |
| `atr_percentile_lookback` | 100 | |
| `bootstrap_bars` | 500 | 需 ≥ `RequiredBars()`（默认 114），≤ 1500 |
| `poll_interval_seconds` | 0（自动） | |
| `stale_multiplier` | 2 | |
| `adaptive_interval.atr_multiplier` | 0.5 | |
| `adaptive_interval.min_interval` / `max_interval` | 0 → base / 8×base | 价格单位 |
| `adaptive_interval.change_threshold_ratio` | 0.25 | |

`Enabled` 两者默认均为 `false`。

## 网格如何消费（接入指引）

建议的 YAML（由接入方嵌入 `config.Config` 的交易配置下）：

```yaml
regime_filter:            # regime.RegimeConfig
  enabled: true
  kline_interval: 1h
adaptive_interval:        # regime.AdaptiveIntervalConfig
  enabled: true
  atr_multiplier: 0.5
```

`regime.PolicyFor(regime, direction)` 给出默认调整建议（`GridPolicy`）：

| Effective 状态 | LONG 网格 | SHORT 网格（镜像） |
|---|---|---|
| `Range` | 满铺：窗口 ×1、间隔 ×1 | 同左 |
| `TrendDown` | **逆势**：买窗 ×0.5、间隔 ×1.5（再量化），减缓接飞刀 | **顺势**：冻结下沿，价格创新低时不下移卖窗、不在低位重建空仓 |
| `TrendUp` | **顺势**：冻结上沿——价格创新高时买窗不上移、不在高位重建仓，只保留已有持仓的平仓卖单 | **逆势**：卖窗 ×0.5、间隔 ×1.5 |
| `Unknown`（未就绪/过期） | 保守：窗口 ×0.5、冻结上沿、间隔不变 | 保守：窗口 ×0.5、冻结下沿 |

接入步骤（`position/` 与 `symbol_manager.go` 由接入轮次修改）：

1. `symbol_manager.go`：`cfg.WithDefaults()` 后若 `Enabled`，`regime.NewDetector(symbol, cfg, exchange)`，`Start(ctx)`；退出时 `Stop()`（会等待后台 goroutine 退出）。
2. `SuperPositionManager` 持有只读接口（在 `position` 包内定义，符合"接口定义在使用方"）：
   ```go
   type RegimeProvider interface{ Snapshot() regime.Snapshot }
   ```
3. `AdjustOrders` 每轮取一次 `snap := provider.Snapshot()`，`r := snap.Effective()`，`p := regime.PolicyFor(r, dir)`：
   - `allowedNewBuyOrders = max(1, floor(buyWindowSize × p.EntryWindowScale))`（LONG；SHORT 用卖窗）。
   - 间隔：`iv := adaptiveCfg.Next(currentInterval, snap.ATR, baseInterval)`（未启用时返回 base），
     再 `iv = regime.QuantizeInterval(iv × p.IntervalScale, baseInterval)`；槽位价用 `regime.AlignToAnchor(price, anchor, iv)` 求。
     注意：`adaptiveCfg.Next` 的滞回基于"自适应部分"的当前值，policy 放大不应回写该值，否则会自激。
   - `p.FreezeFavorableBound`：LONG 记录冻结时的上沿（切换到 TrendUp 那一刻的买窗最高槽位），此后买单槽位不得高于该值，直到状态离开 TrendUp；SHORT 镜像冻结下沿。
4. `snap.ATRPercentile` 可作为附加风控输入（例如 ≥ 95 时暂停新开仓），本 ADR 不强制。
5. 可选：`WithOnChange` 回调用于日志/通知/指标，回调内不得执行阻塞的交易逻辑。

## 取舍与后果

- **轮询 vs WebSocket**：轮询最多延迟一个轮询间隔（1h 周期下 ≤5 分钟），换来与现有 K 线流零冲突、无需重连逻辑；对 1h 级别决策可接受。
- **只用已收盘 K 线**：状态切换滞后至少 1 根 + 驻留 `MinDwellBars-1` 根（默认 1h 周期约 3 小时），这是有意的——宁可晚切也不抖动。
- **Unknown 保守**：启动预热和数据中断期间开仓量减半，可能少赚；但避免"数据断了反而满铺"。
- 纯 m/h/d 周期；`1w`/`1M` 未与纪元对齐，暂不支持。
- 旧 `strategy/trend_detector.go` 保持不动，接入完成后应废弃。
