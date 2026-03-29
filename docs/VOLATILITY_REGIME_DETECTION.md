# QuantMesh 波动率区间检测系统

## 📋 概述

波动率区间检测系统是 QuantMesh 的一项新功能，用于实时监控市场波动率变化，并在波动率区间发生转换时自动调整策略参数，以保护资金安全并优化收益。

## 🎯 解决的问题

根据用户反馈的场景：

> "连续 3 天，价格就在上下 1%之内，这时网格容易赚钱的，但是后面某一天可能一天有 10%上下，我们要能更早地检查到这个波动超限的情况"

这个系统正是为了解决这类问题：

1. **低波动识别**：识别适合网格策略的低波动时期
2. **早期预警**：在波动率突然升高时提供早期预警
3. **自动调整**：根据波动率区间自动调整策略参数
4. **风险保护**：在极端波动时自动采取保护措施

## 🏗️ 架构设计

### 核心组件

```
┌─────────────────────────────────────────────────────────────┐
│                     PriceMonitor                             │
│  (实时价格流 - WebSocket)                                     │
└──────────────────────┬──────────────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────────────┐
│              VolatilityRegimeDetector                        │
│  • 多周期波动率计算 (短期/中期/长期)                            │
│  • 价格范围检测                                              │
│  • 区间分类 (LOW/NORMAL/HIGH/EXTREME)                        │
│  • 突变检测                                                  │
└──────────────────────┬──────────────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────────────┐
│               VolatilityAlertService                         │
│  • 预警历史记录                                              │
│  • 订阅/发布机制                                              │
│  • 事件回调                                                  │
└──────────────────────┬──────────────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────────────┐
│                 DynamicAdjuster                              │
│  • 自动调整策略参数                                           │
│  • 紧急保护机制                                              │
│  • 建议生成                                                  │
└─────────────────────────────────────────────────────────────┘
```

### 波动率区间定义

| 区间 | 波动率范围 | 特征 | 策略建议 |
|------|-----------|------|----------|
| **LOW** | < 1% | 价格稳定，适合网格 | ✅ 优化参数，缩小间距 |
| **NORMAL** | 1% - 3% | 正常波动 | ℹ️ 保持默认参数 |
| **HIGH** | 3% - 5% | 波动较大 | ⚠️ 保守策略，扩大间距 |
| **EXTREME** | ≥ 10% | 极端波动 | 🚨 暂停策略，保护资金 |

## 🔧 技术实现

### 1. 波动率计算

系统使用多周期波动率计算：

```go
// 短期波动率 (24小时)
shortVolatility = calculateVolatility(24小时)

// 中期波动率 (72小时 = 3天)
mediumVolatility = calculateVolatility(72小时)

// 长期波动率 (168小时 = 7天)
longVolatility = calculateVolatility(168小时)

// 价格范围检测（3天内的高低价差）
priceRange = (maxHigh - minLow) / minLow * 100
```

波动率计算使用收益率的标准差：

```go
returns[i] = (price[i+1] - price[i]) / price[i]
volatility = stddev(returns) * 100
```

### 2. 区间分类逻辑

```go
func classifyRegime(shortVol, mediumVol, longVol, priceRange) Regime {
    // 优先检查极端情况
    if shortVol > 10.0 {
        return EXTREME
    }

    // 检查低波动（价格在很小范围内）
    if priceRange < 1.5 && mediumVol < 1.0 {
        return LOW
    }

    // 根据短期波动率分类
    if shortVol > 5.0 {
        return HIGH
    }
    if shortVol < 1.0 {
        return LOW
    }

    return NORMAL
}
```

### 3. 自动参数调整

#### 低波动区间调整

```go
• 价格间距：减少 20%
• 买卖窗口：增加 20%
• 目标：提高交易频率和收益
```

#### 高波动区间调整

```go
• 价格间距：增加 50%
• 买卖窗口：减少 30%
• 目标：降低风险和交易频率
```

#### 突然进入高波动（紧急保护）

```go
• 价格间距：翻倍（+100%）
• 买卖窗口：减半（-50%）
• 单笔金额：减半（-50%）
• 目标：紧急保护资金
```

#### 极端波动调整

```go
• 价格间距：设置为最大值
• 买卖窗口：设置为最小值
• 单笔金额：设置为最小值
• 建议：暂停策略
```

## 📊 配置说明

### 启用波动率检测

在配置文件中添加：

```yaml
trading:
  dynamic_adjustment:
    enabled: true

    # 波动率区间检测
    volatility_detection:
      enabled: true

      # 检测周期（小时）
      short_period: 24    # 短期
      medium_period: 72   # 中期（3天）
      long_period: 168    # 长期（7天）

      # 波动率阈值（百分比）
      low_threshold: 1.0      # 低波动阈值
      normal_threshold: 3.0   # 正常波动上限
      high_threshold: 5.0     # 高波动上限
      extreme_threshold: 10.0 # 极端波动阈值

      # 价格范围检测
      price_range_period: 72     # 检测周期（小时）
      price_range_threshold: 1.5 # 价格范围阈值（%）
```

### 自定义阈值

根据不同的交易对和风险偏好，可以调整阈值：

**保守配置**（更敏感）：
```yaml
low_threshold: 0.8
normal_threshold: 2.0
high_threshold: 4.0
extreme_threshold: 8.0
```

**激进配置**（更宽松）：
```yaml
low_threshold: 1.5
normal_threshold: 4.0
high_threshold: 6.0
extreme_threshold: 12.0
```

## 🚨 预警系统

### 预警级别

| 级别 | 描述 | 行为 |
|------|------|------|
| **info** | 信息性通知 | 仅记录日志 |
| **warning** | 警告 | 记录日志 + 调整参数 |
| **critical** | 关键预警 | 记录日志 + 紧急保护 + 通知 |

### 预警示例

```
📊 [波动率] 区间变化: LOW -> HIGH
⚠️ [波动率调整] 高波动区间，采取保守策略
✅ [波动率调整] 扩大价格间距: 500.00 -> 750.00
✅ [波动率调整] 减少窗口大小: 买 10->7, 卖 10->7
```

```
🚨 [波动率调整] 检测到波动率突然升高，启动紧急保护
🛡️ [紧急保护] 大幅扩大价格间距: 500.00 -> 1000.00
🛡️ [紧急保护] 大幅减少窗口大小: 买 10->5, 卖 10->5
🛡️ [紧急保护] 减少单笔金额: 100.00 -> 50.00
```

## 📈 使用场景

### 场景 1：低波动优化

```
市场状态：连续 3 天价格在 1% 范围内波动
系统检测：
  • 中期波动率：0.8%
  • 价格范围：1.2%
  → 识别为 LOW 区间

自动调整：
  • 价格间距：500 → 400 (-20%)
  • 买卖窗口：10 → 12 (+20%)
  • 预期效果：提高交易频率和收益
```

### 场景 2：突然高波动

```
市场状态：前 3 天波动 0.8%，今日突然波动 8%
系统检测：
  • 短期波动率：8.0%
  → 识别为 HIGH 区间
  → 检测到突变（10倍增加）

紧急保护：
  • 价格间距：500 → 1000 (+100%)
  • 买卖窗口：10 → 5 (-50%)
  • 单笔金额：100 → 50 (-50%)
  • 预期效果：保护资金，降低风险
```

### 场景 3：极端波动

```
市场状态：单日波动达到 12%
系统检测：
  • 短期波动率：12.0%
  → 识别为 EXTREME 区间

自动调整：
  • 价格间距：设置为最大值 (2000)
  • 买卖窗口：设置为最小值 (5)
  • 单笔金额：设置为最小值 (50)
  • 建议：暂停策略
```

## 🔍 API 接口

### 获取当前波动率状态

```go
regime := adjuster.GetCurrentVolatilityRegime()
// 返回：RegimeLow | RegimeNormal | RegimeHigh | RegimeExtreme

riskLevel := adjuster.GetVolatilityRiskLevel()
// 返回：0-10 的风险等级
```

### 判断是否适合网格策略

```go
isFriendly := adjuster.IsGridFriendly()
// 返回：true (LOW/NORMAL) | false (HIGH/EXTREME)
```

### 获取波动率统计

```go
stats := adjuster.GetVolatilityStatistics()
// 返回：
// {
//   "current_regime": "LOW",
//   "current_risk_level": 1,
//   "grid_friendly": true,
//   "short_volatility": 0.8,
//   "medium_volatility": 0.9,
//   "long_volatility": 1.2,
//   "price_range": 1.3,
//   "total_alerts": 5,
//   "unacknowledged": 0
// }
```

## 🛡️ 安全机制

### 1. 确认机制

区间变化需要连续检测 2 次才确认，避免假信号：

```go
if newRegime != currentRegime {
    if newRegime == previousRegime {
        consecutiveCount++
        if consecutiveCount >= 2 {
            // 确认变化
            triggerRegimeChange()
        }
    }
}
```

### 2. 参数限制

所有自动调整都在配置的 min/max 范围内：

```go
newInterval = currentInterval * 0.8
if newInterval < minInterval {
    newInterval = minInterval  // 不会低于最小值
}
```

### 3. 可配置性

所有阈值和参数都可以在配置文件中自定义，适应不同场景。

## 📝 总结

波动率区间检测系统为 QuantMesh 提供了：

1. ✅ **早期预警**：在波动率突然变化时及时预警
2. 🔄 **自动调整**：根据市场条件自动优化参数
3. 🛡️ **风险保护**：在极端情况下自动采取保护措施
4. 📊 **可观测性**：提供详细的波动率统计和历史记录
5. ⚙️ **高度可配置**：所有参数都可以自定义

这个系统特别适合网格策略，能够在低波动时优化收益，在高波动时保护资金，正是用户所需要的功能。

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
