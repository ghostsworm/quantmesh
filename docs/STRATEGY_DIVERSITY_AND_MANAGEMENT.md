# QuantMesh 策略多样化与管理系统

本文档详细介绍 QuantMesh v3.4.1 版本新增的策略多样化、技术指标库、AI 风险评估以及前端策略管理系统。

---

## 目录

- [概述](#概述)
- [后端功能](#后端功能)
  - [策略多样化](#策略多样化)
    - [增强型 DCA 策略](#增强型-dca-策略)
    - [马丁格尔策略](#马丁格尔策略)
    - [组合策略模块](#组合策略模块)
  - [技术指标库](#技术指标库)
    - [趋势指标](#趋势指标)
    - [波动率指标](#波动率指标)
    - [动量指标](#动量指标)
    - [成交量指标](#成交量指标)
  - [AI 风险评估](#ai-风险评估)
  - [经纪商返佣系统](#经纪商返佣系统)
- [前端功能](#前端功能)
  - [策略市场](#策略市场)
  - [资金管理](#资金管理)
  - [盈利管理](#盈利管理)
- [API 接口](#api-接口)
- [配置示例](#配置示例)
- [最佳实践](#最佳实践)

---

## 概述

QuantMesh v3.4.1 版本带来了重大功能升级，核心目标是：

1. **策略多样化**：从单一网格策略扩展到多种策略类型，满足不同市场环境和风险偏好
2. **专业技术分析**：提供 20+ 专业技术指标，支持策略信号生成
3. **智能风控**：AI 驱动的风险评估系统，帮助用户优化配置
4. **可视化管理**：全新的前端界面，支持策略选购、资金分配和盈利管理

---

## 后端功能

### 策略多样化

#### 增强型 DCA 策略

**文件位置**: `strategy/dca_enhanced.go`

增强型 DCA（Dollar Cost Averaging）策略是对传统定投策略的全面升级，专为加密货币市场的高波动特性设计。

##### 核心特性

| 特性 | 说明 |
|------|------|
| ATR 动态间距 | 根据市场波动率（ATR）自动调整加仓间距，波动大时间距扩大 |
| 三重止盈机制 | 固定止盈 + 追踪止盈 + 时间止盈，多维度锁定利润 |
| 50层仓位管理 | 支持多达50层金字塔加仓，充分利用价格下跌机会 |
| 瀑布保护 | 极端行情下的批量止损机制，保护本金安全 |
| 趋势过滤 | 基于均线判断趋势方向，避免逆势加仓 |

##### 配置参数

```go
type DCAEnhancedConfig struct {
    // 基础参数
    Symbol           string  // 交易对
    Direction        string  // 方向: long/short
    InitialAmount    float64 // 初始开仓金额
    
    // ATR 动态间距
    ATRPeriod        int     // ATR 计算周期（默认14）
    ATRMultiplier    float64 // ATR 倍数（默认1.5）
    MinSpacing       float64 // 最小间距百分比
    MaxSpacing       float64 // 最大间距百分比
    
    // 仓位管理
    MaxLayers        int     // 最大加仓层数（最多50）
    ScaleMultiplier  float64 // 加仓金额倍数
    
    // 三重止盈
    FixedTakeProfit  float64 // 固定止盈百分比
    TrailingStart    float64 // 追踪止盈启动点
    TrailingStep     float64 // 追踪止盈步进
    TimeBasedTP      int     // 时间止盈（小时）
    
    // 瀑布保护
    WaterfallTrigger float64 // 瀑布保护触发点
    WaterfallLayers  int     // 触发时平掉的层数
    
    // 趋势过滤
    TrendMAType      string  // 趋势均线类型: SMA/EMA
    TrendMAPeriod    int     // 趋势均线周期
    EnableTrendFilter bool   // 是否启用趋势过滤
}
```

##### 使用示例

```go
config := &DCAEnhancedConfig{
    Symbol:           "BTCUSDT",
    Direction:        "long",
    InitialAmount:    100,
    ATRPeriod:        14,
    ATRMultiplier:    1.5,
    MaxLayers:        20,
    ScaleMultiplier:  1.2,
    FixedTakeProfit:  2.0,
    TrailingStart:    1.5,
    TrailingStep:     0.3,
    EnableTrendFilter: true,
    TrendMAPeriod:    50,
}

strategy := NewDCAEnhancedStrategy(config, exchange, logger)
strategy.Start(ctx)
```

---

#### 马丁格尔策略

**文件位置**: `strategy/martingale.go`

马丁格尔策略是一种经典的仓位管理策略，通过在亏损时加倍下注来追求回本。本实现增加了多项风控措施。

##### 核心特性

| 特性 | 说明 |
|------|------|
| 正向/反向马丁 | 支持经典马丁（亏损加仓）和反向马丁（盈利加仓） |
| 递减风控 | 随着层数增加降低加仓倍数，控制总体风险 |
| 多方向支持 | 做多/做空/双向三种模式 |
| 最大层数限制 | 防止无限加仓导致爆仓 |
| 冷却时间 | 连续亏损后的强制等待期 |

##### 策略模式

```
经典马丁格尔（Classic）:
第1层: 100 USDT
第2层: 200 USDT (x2)
第3层: 400 USDT (x2)
第4层: 800 USDT (x2)
...

递减马丁格尔（Decreasing）:
第1层: 100 USDT
第2层: 180 USDT (x1.8)
第3层: 306 USDT (x1.7)
第4层: 489 USDT (x1.6)
...

反向马丁格尔（Reverse）:
盈利时加仓，亏损时回到起点
```

##### 配置参数

```go
type MartingaleConfig struct {
    Symbol          string  // 交易对
    Direction       string  // 方向: long/short/both
    Mode            string  // 模式: classic/decreasing/reverse
    
    InitialAmount   float64 // 初始开仓金额
    Multiplier      float64 // 加仓倍数
    DecreaseRate    float64 // 递减模式下的倍数递减率
    
    MaxLayers       int     // 最大层数
    TakeProfit      float64 // 止盈百分比
    StopLoss        float64 // 止损百分比（可选）
    
    CooldownPeriod  int     // 冷却时间（分钟）
    MaxDailyLoss    float64 // 每日最大亏损限制
}
```

##### 风险提示

⚠️ **马丁格尔策略具有高风险性**：
- 理论上需要无限资金才能保证必胜
- 连续亏损会导致仓位指数级增长
- 建议配合严格的止损和最大层数限制使用
- 不建议在高杠杆环境下使用

---

#### 组合策略模块

**文件位置**: `strategy/combo_strategy.go`

组合策略模块允许同时运行多个策略，并根据市场状况动态调整各策略权重。

##### 核心特性

| 特性 | 说明 |
|------|------|
| 多策略组合 | 同时运行多个不同类型的策略 |
| 动态权重 | 根据策略表现动态调整资金分配权重 |
| 市场自适应 | 根据市场状态（趋势/震荡）切换主导策略 |
| 策略间隔离 | 每个策略独立的资金池和风控 |
| 性能追踪 | 独立追踪每个策略的收益表现 |

##### 市场状态识别

```go
type MarketState string

const (
    MarketTrending   MarketState = "trending"    // 趋势市场
    MarketRanging    MarketState = "ranging"     // 震荡市场
    MarketVolatile   MarketState = "volatile"    // 高波动市场
    MarketQuiet      MarketState = "quiet"       // 低波动市场
)
```

##### 策略权重调整逻辑

```
趋势市场 → 增加趋势跟踪策略权重
震荡市场 → 增加网格/均值回归策略权重
高波动   → 降低整体仓位，启用保守策略
低波动   → 增加仓位，使用激进策略
```

##### 配置示例

```go
comboConfig := &ComboStrategyConfig{
    Strategies: []SubStrategyConfig{
        {
            Type:       "grid",
            Weight:     0.4,
            Config:     gridConfig,
            MinWeight:  0.2,
            MaxWeight:  0.6,
        },
        {
            Type:       "dca_enhanced",
            Weight:     0.3,
            Config:     dcaConfig,
            MinWeight:  0.1,
            MaxWeight:  0.5,
        },
        {
            Type:       "trend_following",
            Weight:     0.3,
            Config:     trendConfig,
            MinWeight:  0.1,
            MaxWeight:  0.5,
        },
    },
    RebalanceInterval:  "1h",
    AdaptiveWeights:    true,
    MarketStateSource:  "auto",
}
```

---

### 技术指标库

**文件位置**: `indicators/`

技术指标库提供了完整的技术分析工具集，所有指标实现统一的 `Indicator` 接口。

```go
type Indicator interface {
    Name() string
    Calculate(candles []Candle) (float64, error)
    Period() int
}
```

#### 趋势指标

**文件**: `indicators/trend.go`

| 指标 | 说明 | 用途 |
|------|------|------|
| MACD | 移动平均收敛散度 | 趋势方向和动量 |
| ADX | 平均趋向指数 | 趋势强度 |
| Parabolic SAR | 抛物线转向 | 趋势反转点 |
| Ichimoku Cloud | 一目均衡表 | 综合趋势分析 |
| Aroon | 阿隆指标 | 趋势开始/结束 |
| SuperTrend | 超级趋势 | 趋势方向和止损 |

##### MACD 示例

```go
macd := NewMACD(12, 26, 9)
result, err := macd.Calculate(candles)
// result.MACD     - MACD 线
// result.Signal   - 信号线
// result.Histogram - 柱状图
```

##### SuperTrend 示例

```go
superTrend := NewSuperTrend(10, 3.0) // period=10, multiplier=3.0
result, err := superTrend.Calculate(candles)
// result.Value     - SuperTrend 值
// result.Direction - 1: 上涨, -1: 下跌
```

#### 波动率指标

**文件**: `indicators/volatility.go`

| 指标 | 说明 | 用途 |
|------|------|------|
| ATR | 平均真实波幅 | 波动率测量 |
| Bollinger Bands | 布林带 | 波动区间 |
| Keltner Channel | 肯特纳通道 | 趋势通道 |
| Donchian Channel | 唐奇安通道 | 突破交易 |
| NATR | 标准化 ATR | 跨品种波动比较 |

##### Bollinger Bands 示例

```go
bb := NewBollingerBands(20, 2.0)
result, err := bb.Calculate(candles)
// result.Upper  - 上轨
// result.Middle - 中轨
// result.Lower  - 下轨
// result.Width  - 带宽
```

##### ATR 示例

```go
atr := NewATR(14)
result, err := atr.Calculate(candles)
// result - ATR 值，可用于设置止损距离
```

#### 动量指标

**文件**: `indicators/momentum.go`

| 指标 | 说明 | 用途 |
|------|------|------|
| RSI | 相对强弱指数 | 超买超卖 |
| Stochastic | 随机指标 | 超买超卖 |
| CCI | 商品通道指数 | 趋势/超买超卖 |
| Williams %R | 威廉指标 | 超买超卖 |
| MFI | 资金流量指数 | 带成交量的超买超卖 |
| ROC | 变动率 | 动量 |
| TRIX | 三重指数平滑 | 趋势确认 |
| Ultimate Oscillator | 终极震荡指标 | 多周期动量 |

##### RSI 示例

```go
rsi := NewRSI(14)
result, err := rsi.Calculate(candles)
// result > 70 - 超买
// result < 30 - 超卖
```

##### Stochastic 示例

```go
stoch := NewStochastic(14, 3, 3) // K period, K smooth, D smooth
result, err := stoch.Calculate(candles)
// result.K - %K 线
// result.D - %D 线
```

#### 成交量指标

**文件**: `indicators/volume.go`

| 指标 | 说明 | 用途 |
|------|------|------|
| OBV | 能量潮 | 成交量趋势 |
| VWAP | 成交量加权均价 | 当日公允价格 |
| CMF | 蔡金资金流 | 买卖压力 |
| ADL | 累积/派发线 | 资金流向 |
| Force Index | 力量指数 | 价格/成交量结合 |
| Chaikin Oscillator | 蔡金震荡指标 | ADL 的动量 |

##### OBV 示例

```go
obv := NewOBV()
result, err := obv.Calculate(candles)
// 上升的 OBV 配合上涨价格 = 强势
// 下降的 OBV 配合下跌价格 = 弱势
```

##### VWAP 示例

```go
vwap := NewVWAP()
result, err := vwap.Calculate(candles)
// 价格 > VWAP = 多头倾向
// 价格 < VWAP = 空头倾向
```

---

### AI 风险评估

**文件位置**: `ai/risk_assessor.go`

AI 风险评估器使用多维度分析框架，对用户的交易配置进行全面评估。

##### 四维评估体系

```
┌─────────────────────────────────────────────────────────┐
│                    AI 风险评估                           │
├─────────────┬─────────────┬─────────────┬──────────────┤
│  资金安全   │  风险控制   │  策略适配   │  市场环境    │
│  (25分)     │  (25分)     │  (25分)     │  (25分)      │
├─────────────┼─────────────┼─────────────┼──────────────┤
│ • 仓位大小  │ • 止损设置  │ • 策略匹配  │ • 波动率    │
│ • 杠杆倍数  │ • 最大回撤  │ • 参数合理  │ • 趋势状态  │
│ • 资金分散  │ • 风险敞口  │ • 历史表现  │ • 流动性    │
└─────────────┴─────────────┴─────────────┴──────────────┘
```

##### 评估结果

```go
type RiskAssessmentResult struct {
    TotalScore      int                 // 总分 (0-100)
    RiskLevel       string              // 风险等级: low/medium/high/critical
    Scores          map[string]int      // 各维度得分
    RiskFactors     []RiskFactor        // 识别的风险因素
    Suggestions     []string            // 优化建议
    Explanation     string              // 评估说明
}

type RiskFactor struct {
    Category    string  // 风险类别
    Severity    string  // 严重程度: low/medium/high
    Description string  // 风险描述
    Mitigation  string  // 缓解建议
}
```

##### 使用示例

```go
assessor := NewAIRiskAssessor(geminiClient, logger)

config := &StrategyConfig{
    Symbol:     "BTCUSDT",
    Leverage:   10,
    MaxPosition: 5000,
    // ...
}

result, err := assessor.Assess(ctx, config, marketData)

fmt.Printf("风险评分: %d/100\n", result.TotalScore)
fmt.Printf("风险等级: %s\n", result.RiskLevel)

for _, factor := range result.RiskFactors {
    fmt.Printf("⚠️ %s: %s\n", factor.Category, factor.Description)
}

for _, suggestion := range result.Suggestions {
    fmt.Printf("💡 建议: %s\n", suggestion)
}
```

##### 评分标准

| 分数范围 | 风险等级 | 建议操作 |
|----------|----------|----------|
| 80-100 | 低风险 | 可以放心使用 |
| 60-79 | 中等风险 | 建议调整部分参数 |
| 40-59 | 高风险 | 需要认真审视配置 |
| 0-39 | 极高风险 | 强烈建议修改配置 |

---

### 经纪商返佣系统

**文件位置**: `saas/broker_rebate.go`

经纪商返佣系统支持多交易所的邀请链接生成和返佣追踪。

##### 核心功能

| 功能 | 说明 |
|------|------|
| 邀请链接生成 | 为每个用户生成唯一的邀请链接 |
| 多交易所支持 | 支持 Binance、Bitget、OKX 等主流交易所 |
| 返佣追踪 | 实时追踪被邀请用户的交易和返佣 |
| 报表统计 | 详细的返佣统计报表 |

##### API 接口

```go
// 生成邀请链接
POST /api/broker/invite-link
{
    "exchange": "binance",
    "user_id": "user123"
}

// 查询返佣统计
GET /api/broker/rebate-stats?user_id=user123

// 返佣明细
GET /api/broker/rebate-history?user_id=user123&page=1&limit=20
```

---

## 前端功能

### 策略市场

**文件位置**: `webui/src/components/StrategyMarket.tsx`

策略市场是用户浏览和选购策略的入口页面。

##### 页面功能

- **策略浏览**: 展示所有可用策略卡片
- **分类筛选**: 按策略类型筛选（网格、DCA、马丁格尔、趋势、均值回归、组合）
- **搜索功能**: 根据策略名称或描述搜索
- **风险标识**: 清晰标注每个策略的风险等级
- **付费区分**: 区分免费策略和付费策略

##### 组件结构

```
StrategyMarket/
├── StrategyGrid        # 策略网格布局
├── StrategyCard        # 策略卡片
│   ├── PremiumBadge    # 付费标识
│   └── RiskBadge       # 风险等级标识
└── StrategyDetailModal # 策略详情弹窗
```

##### 策略卡片信息

```typescript
interface StrategyInfo {
  id: string;
  name: string;
  type: 'dca' | 'martingale' | 'grid' | 'trend' | 'combo';
  description: string;
  riskLevel: 'low' | 'medium' | 'high';
  isPremium: boolean;
  isEnabled: boolean;
  features: string[];
  minCapital: number;
  recommendedCapital: number;
  status: 'running' | 'stopped' | 'error';
  currentPnL: number;
  dailyPnL: number;
  totalTrades: number;
}
```

---

### 资金管理

**文件位置**: `webui/src/components/CapitalManagement.tsx`

资金管理页面用于管理账户资金和策略间的资金分配。

##### 页面功能

- **账户总览**: 显示总余额、已分配、可用资金、总盈亏
- **资金分配**: 为每个策略设置资金上限和权重
- **可视化图表**: 饼图展示各策略资金占比
- **再平衡功能**: 一键重新平衡各策略资金

##### 组件结构

```
CapitalManagement/
├── CapitalOverview     # 账户总览卡片
├── AllocationChart     # 资金分配饼图
├── CapitalSlider       # 资金分配滑块
└── RebalanceButton     # 再平衡按钮
```

##### 资金分配配置

```typescript
interface CapitalAllocationConfig {
  strategyId: string;
  maxCapital: number;      // 最大分配资金
  maxPercentage: number;   // 最大占比
  reserveRatio: number;    // 预留比例
  autoRebalance: boolean;  // 自动再平衡
  priority: number;        // 优先级
}
```

---

### 盈利管理

**文件位置**: `webui/src/components/ProfitManagement.tsx`

盈利管理页面用于查看盈利统计和配置利润提取规则。

##### 页面功能

- **盈利趋势图**: 展示按时间的盈利曲线
- **策略盈利统计**: 按策略维度统计盈利
- **手动提取**: 手动将策略盈利提取到账户
- **自动提取规则**: 配置满足条件时自动提取

##### 组件结构

```
ProfitManagement/
├── ProfitSummary       # 盈利总览
├── ProfitChart         # 盈利趋势图
├── StrategyProfitList  # 策略盈利列表
├── WithdrawDialog      # 手动提取对话框
└── WithdrawRuleForm    # 自动提取规则表单
```

##### 自动提取规则

```typescript
interface WithdrawRule {
  strategyId: string;
  enabled: boolean;
  triggerAmount: number;     // 触发金额
  withdrawRatio: number;     // 提取比例
  frequency: 'immediate' | 'daily' | 'weekly';
  destination: 'account' | 'wallet';
  walletAddress?: string;
}
```

---

## API 接口

### 策略管理 API

```
GET    /api/strategies                    # 获取所有策略
GET    /api/strategies/:id                # 获取策略详情
POST   /api/strategies/:id/enable         # 启用策略
POST   /api/strategies/:id/disable        # 禁用策略
POST   /api/strategies/configure          # 配置策略参数
GET    /api/strategies/:id/license        # 检查策略授权
```

### 资金管理 API

```
GET    /api/capital/overview              # 获取资金总览
GET    /api/capital/allocation            # 获取资金分配
PUT    /api/capital/allocation            # 更新资金分配
POST   /api/capital/rebalance             # 触发再平衡
```

### 盈利管理 API

```
GET    /api/profit/summary                # 获取盈利总览
GET    /api/profit/withdraw-rules         # 获取提取规则
PUT    /api/profit/withdraw-rules         # 更新提取规则
POST   /api/profit/withdraw               # 手动提取
GET    /api/profit/history                # 获取提取历史
```

---

## 配置示例

### 多策略组合配置

```yaml
strategies:
  # 网格策略 - 震荡市场主力
  - type: grid
    weight: 0.4
    config:
      symbol: BTCUSDT
      upper_price: 45000
      lower_price: 35000
      grid_count: 20
      amount_per_grid: 50
      
  # DCA 策略 - 下跌市场抄底
  - type: dca_enhanced
    weight: 0.3
    config:
      symbol: ETHUSDT
      direction: long
      initial_amount: 100
      max_layers: 15
      atr_multiplier: 1.5
      
  # 趋势跟踪 - 趋势市场追涨
  - type: trend_following
    weight: 0.3
    config:
      symbol: SOLUSDT
      direction: both
      fast_ma: 10
      slow_ma: 30

capital_management:
  total_capital: 10000
  reserve_ratio: 0.2
  auto_rebalance: true
  rebalance_interval: "24h"

profit_management:
  auto_withdraw:
    enabled: true
    trigger_amount: 500
    withdraw_ratio: 0.5
    frequency: daily
```

---

## 最佳实践

### 策略选择建议

| 市场状态 | 推荐策略 | 权重建议 |
|----------|----------|----------|
| 震荡市场 | 网格策略 + 均值回归 | 60% + 30% |
| 上涨趋势 | 趋势跟踪 + DCA | 50% + 40% |
| 下跌趋势 | DCA (做空) + 网格 | 40% + 30% |
| 高波动 | 组合策略 + 低仓位 | 降低50%仓位 |

### 风险控制建议

1. **资金分散**: 单策略不超过总资金的 40%
2. **杠杆控制**: 建议杠杆不超过 5 倍
3. **止损设置**: 每个策略必须设置止损
4. **定期再平衡**: 每周至少检查一次资金分配
5. **盈利提取**: 建议达到一定盈利后提取部分利润

### 指标使用建议

| 场景 | 推荐指标组合 |
|------|--------------|
| 趋势判断 | MACD + ADX + SuperTrend |
| 入场时机 | RSI + Stochastic + VWAP |
| 止损设置 | ATR + Parabolic SAR |
| 仓位调整 | OBV + CMF |

---

## 版本历史

| 版本 | 日期 | 更新内容 |
|------|------|----------|
| v3.4.1 | 2026-01-11 | 策略多样化、技术指标库、AI风险评估、前端管理系统 |
| v3.3.8 | 2026-01-11 | v3.4.1 之前的稳定版本 |

---

## 相关文档

- [系统架构](../ARCHITECTURE.md)
- [回测系统](./BACKTESTING.md)
- [API 文档](./API_REFERENCE.md)
- [部署指南](./DEPLOYMENT.md)
