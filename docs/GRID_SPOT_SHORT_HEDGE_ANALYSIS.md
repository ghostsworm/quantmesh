# 网格 + 现货做空对冲策略分析

## 一、策略构想概述

您的设想是：

1. **合约网格（单边做多）**：在期货市场运行单向做多网格，震荡时赚取价差
2. **现货做空**：在现货市场持有空单，对冲网格的单边下跌风险
3. **仓位关系**：现货做空仓位 ≈ 网格投入的 **25%**
4. **杠杆差异**：网格约 5–7 倍杠杆，现货无杠杆；做空规模需随网格规模自动调整

## 二、当前系统能力

### 2.1 现有对冲类型

| 类型 | 说明 | 合约腿 | 现货腿 | 是否支持「做空」 |
|------|------|--------|--------|------------------|
| `futures_spot_hedge` | 合约+现货对冲 | 合约（策略可配） | 现货（策略可配） | 否，现货默认也是网格（做多） |
| `long_short_hedge` | 多空对冲 | 多腿 | 空腿 | 是，但**两腿均为合约** |
| 回测 `hedge_group` | 对冲组回测 | leg_a（长） | leg_b（短） | 是，但 leg_b 为**另一合约交易对** |

### 2.2 现货「做空」限制

根据 `docs/i18n/zh-TW/SPOT_TRADING_GUIDE.md` 与 `docs/BASIS_MONITOR.md`：

- **现货普通交易**：现货无杠杆，**不支持做空**，只能卖出已有持仓
- **现货做空**：需借币卖出（现货杠杆/现货 margin），存在借币成本、强平风险

因此，若要在「现货」做空，必须依赖交易所的**现货杠杆/现货 margin** 能力，否则无法实现。

### 2.3 HedgeCoordinator 行为

`strategy/hedge_coordinator.go` 中：

```go
// GetTargetSpotPosition 根據主 Bot 持倉計算 spot 對沖目標倉位
func (hc *HedgeCoordinator) GetTargetSpotPosition(futuresPosition float64) float64 {
    ratio := hc.group.HedgeConfig.HedgeRatio  // 默认 0.5
    return futuresPosition * ratio
}
```

- 当前逻辑：`spot_position = futures_position * hedge_ratio`
- 含义：现货目标仓位 = 合约目标仓位 × 对冲比例
- 现状：`onEvent` 中 TODO 未实现，实际对 spot 的下单/调仓逻辑尚未与 position manager 打通

### 2.4 创建对冲 Bot 的流程

`POST /api/bot-groups` 创建 `futures_spot_hedge` 时：

- 合约：`market_type: futures`，策略可选 grid/dca 等
- 现货：`market_type: spot`，策略可选 grid/dca 等
- 现货腿默认也是 `grid` 或 `dca`，**没有「做空」策略选项**

因此，当前**无法**通过「现货 + 做空」直接实现您想要的组合。

## 三、仓位与杠杆计算

### 3.1 假设

- 网格：5 倍杠杆，投入资金 `G_cap = 1000 USDT`
- 网格名义敞口：`G_notional = 1000 × 5 = 5000 USDT`
- 现货做空：无杠杆，目标为网格名义敞口的 25%

### 3.2 两种理解

**理解 A：做空仓位 = 网格投入的 25%**

- `spot_cap = 0.25 × G_cap = 250 USDT`
- 现货名义敞口：`250 × 1 = 250 USDT`
- 占网格名义敞口：`250 / 5000 = 5%`

**理解 B：做空名义敞口 = 网格名义敞口的 25%**

- `spot_notional = 0.25 × G_notional = 1250 USDT`
- 现货无杠杆：`spot_cap = 1250 USDT`
- 占网格投入：`1250 / 1000 = 125%`

### 3.3 推荐公式（以 25% 对冲网格名义敞口为例）

若希望做空名义敞口 = 网格名义敞口的 25%：

```
spot_capital = grid_capital × grid_leverage × 0.25
```

例如：`grid_cap = 1000`，`leverage = 5`：

- `spot_capital = 1000 × 5 × 0.25 = 1250 USDT`

即：现货投入需略大于网格投入，才能实现 25% 名义敞口对冲。

## 四、实现路径评估

### 4.1 方案 A：现货做空（需交易所支持）

前提：交易所支持现货杠杆/借币做空。

**实现要点：**

1. 新增 spot margin 或 spot short 相关 API 封装
2. 新增「做空策略」或「持有空仓」策略，供现货腿使用
3. 扩展 `HedgeCoordinator`：根据合约网格持仓动态计算 `spot_short_target = futures_position × hedge_ratio`
4. 实现与 position manager 的联动，自动下单/调仓

**当前缺口：**

- 现货做空 API 支持情况
- 做空策略的实盘逻辑
- HedgeCoordinator 的完整实盘对接

### 4.2 方案 B：合约做空（推荐，可立即落地）

不依赖现货做空，改用**合约做空**对冲网格：

- 合约腿 1：网格（单边做多）
- 合约腿 2：做空（单边做空或简单持有空仓）

**实现要点：**

1. 使用 `long_short_hedge` 或扩展 `futures_spot_hedge` 为「合约网格 + 合约做空」
2. 合约腿 2：`direction: SHORT` 或专门的 short-only 策略
3. 仓位比例：`short_notional = grid_notional × 0.25
4. 杠杆：网格 5–7x，做空腿 1x 或低杠杆，以控制风险

**优势：**

- 不依赖现货杠杆
- 交易所 API 成熟
- 现有合约多空逻辑可复用

### 4.3 方案 C：组合模板（合约网格 + 合约做空）

在现有模板基础上新增：

- 模板 ID：`grid_short_hedge`
- 合约腿 1：网格（LONG）
- 合约腿 2：做空（SHORT）
- 默认 `hedge_ratio = 0.25`（名义敞口对冲比例）

## 五、当前系统支持情况汇总

| 能力 | 是否支持 | 说明 |
|------|----------|------|
| 合约网格 + 现货网格 | ✅ | `futures_spot_hedge`，两腿均为做多 |
| 合约网格 + 现货做空 | ❌ | 现货无做空策略，且需现货杠杆支持 |
| 合约网格 + 合约做空 | ⚠️ | `long_short_hedge` 存在，但需确认是否支持同 symbol 双腿 |
| 做空规模随网格自动调整 | ⚠️ | HedgeCoordinator 有公式，但未与下单/调仓打通 |
| 25% 对冲比例配置 | ✅ | `HedgeConfig.HedgeRatio` 可配置 |
| 杠杆差异考虑 | ⚠️ | 需在配置中显式设置 grid_leverage、short_leverage |

## 六、建议与下一步

1. **短期**：若交易所不支持现货做空，优先采用**方案 B**（合约网格 + 合约做空）
2. **配置**：在 `HedgeConfig` 中增加 `spot_hedge_ratio` 或 `short_notional_ratio`，明确「做空名义敞口 / 网格名义敞口」的比例
3. **实现**：补全 `HedgeCoordinator.onEvent`，实现与 position manager 的联动，使做空规模随网格持仓自动调整
4. **文档**：在创建向导中说明「现货做空」与「合约做空」的区别及适用场景

## 七、附录：相关代码位置

| 文件 | 说明 |
|------|------|
| `strategy/hedge_coordinator.go` | 对冲协调器，`GetTargetSpotPosition` |
| `config/config.go` | `HedgeConfig`、`BotGroup` |
| `web/api_bots.go` | `postBotGroupCreate`，创建对冲组 |
| `webui/src/components/bot-create/StrategyPicker.tsx` | 对冲模式下的策略选择 |
| `backtest/hedge_pair_engine.go` | 对冲回测（leg_a 长、leg_b 短） |
| `docs/i18n/zh-TW/SPOT_TRADING_GUIDE.md` | 现货交易说明（无做空） |
