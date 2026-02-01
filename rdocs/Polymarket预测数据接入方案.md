# Polymarket 预测数据接入方案

## 一、方案概述

将 Polymarket 预测市场的数据作为交易信号源，整合到 QuantMesh 量化交易系统中。通过分析预测市场对加密货币相关事件的概率预测，生成交易信号，辅助交易决策。

## 二、架构设计

### 2.1 数据流架构

```
Polymarket API
    ↓
DataSourceManager (扩展)
    ↓
PolymarketSignalAnalyzer (新增)
    ↓
DecisionEngine (集成)
    ↓
交易决策
```

### 2.2 核心模块

1. **DataSourceManager 扩展**
   - 添加 `FetchPolymarketMarkets()`: 获取市场列表
   - 添加 `FetchPolymarketMarketData()`: 获取特定市场数据（订单簿、成交历史等）
   - 支持缓存机制，避免频繁API调用

2. **PolymarketSignalAnalyzer (新增)**
   - 定期获取相关预测市场数据
   - 分析预测概率与加密货币价格的相关性
   - 生成交易信号（buy/sell/hold）
   - 计算信号强度和置信度

3. **DecisionEngine 集成**
   - 将预测市场信号纳入决策流程
   - 与其他AI模块（市场分析、情绪分析、风险分析）综合决策

## 三、技术实现

### 3.1 Polymarket API 接口

Polymarket 使用 GraphQL API，主要端点：
- **公共数据接口**: `https://api.polymarket.com/graphql`
- **无需认证**: 可获取市场列表、订单簿、成交历史等

### 3.2 数据模型

```go
// PolymarketMarket 预测市场
type PolymarketMarket struct {
    ID          string    // 市场ID
    Question    string    // 问题描述
    Description string    // 详细描述
    EndDate     time.Time // 结束时间
    Outcomes    []string  // 可能的结果
    Volume      float64   // 交易量
    Liquidity   float64   // 流动性
}

// PolymarketMarketData 市场数据
type PolymarketMarketData struct {
    MarketID      string
    YesPrice      float64 // YES 价格（0-1，表示概率）
    NoPrice       float64 // NO 价格（0-1）
    Volume24h     float64 // 24小时交易量
    BestBid       float64 // 最佳买价
    BestAsk       float64 // 最佳卖价
    LastPrice     float64 // 最新成交价
    Timestamp     time.Time
}

// PolymarketSignal 预测市场信号
type PolymarketSignal struct {
    MarketID      string
    Question      string
    Probability   float64 // 预测概率（0-1）
    Signal        string  // buy, sell, hold
    Strength      float64 // 信号强度（0-1）
    Confidence    float64 // 置信度（0-1）
    Reasoning     string  // 推理过程
    Relevance     string  // 与加密货币的相关性（high, medium, low）
}
```

### 3.3 信号生成逻辑

1. **市场筛选**
   - 筛选与加密货币相关的市场（如：BTC价格预测、监管政策、重大事件等）
   - 过滤即将到期的市场（避免噪音）
   - 过滤低流动性市场（避免价格操纵）

2. **信号转换**
   - **高概率看涨事件**（如：BTC突破$100k的概率>70%）→ **买入信号**
   - **高概率看跌事件**（如：监管禁令概率>60%）→ **卖出信号**
   - **中性事件**（概率接近50%）→ **持有信号**

3. **信号强度计算**
   ```
   信号强度 = |概率 - 0.5| × 2  // 归一化到0-1
   置信度 = 流动性权重 × 交易量权重 × 相关性权重
   ```

### 3.4 配置结构

```yaml
ai:
  modules:
    polymarket_signal:
      enabled: true
      analysis_interval: 300  # 分析间隔（秒）
      api_url: "https://api.polymarket.com/graphql"
      markets:
        # 关注的市场关键词（用于筛选相关市场）
        keywords:
          - "bitcoin"
          - "btc"
          - "ethereum"
          - "eth"
          - "crypto"
          - "regulation"
        # 最小流动性要求（USDC）
        min_liquidity: 10000
        # 最小交易量要求（24小时，USDC）
        min_volume_24h: 5000
        # 市场到期时间过滤（天）
        min_days_to_expiry: 1
        max_days_to_expiry: 90
      signal_generation:
        # 买入信号阈值（概率>此值生成买入信号）
        buy_threshold: 0.65
        # 卖出信号阈值（概率<此值生成卖出信号）
        sell_threshold: 0.35
        # 最小信号强度
        min_signal_strength: 0.3
        # 最小置信度
        min_confidence: 0.5
```

## 四、集成方案

### 4.1 与现有AI模块的协同

1. **与市场分析器协同**
   - 预测市场信号作为市场分析器的补充输入
   - 当预测市场信号与市场分析一致时，提高置信度

2. **与情绪分析器协同**
   - 预测市场数据反映市场情绪
   - 与新闻情绪分析交叉验证

3. **与风险分析器协同**
   - 高风险事件（如监管政策）的预测概率影响风险评分
   - 当预测市场显示高风险事件概率高时，提高风险等级

### 4.2 决策引擎集成

在 `DecisionEngine.MakeDecision()` 中：
1. 获取预测市场信号
2. 与其他AI模块结果综合
3. 根据信号强度和置信度调整最终决策

## 五、实施步骤

1. ✅ 扩展 `DataSourceManager`，添加 Polymarket API 调用
2. ✅ 创建 `PolymarketSignalAnalyzer` 模块
3. ✅ 扩展 AI 模型定义，添加预测市场相关数据结构
4. ✅ 更新配置结构，添加 Polymarket 配置
5. ✅ 集成到 `DecisionEngine`
6. ✅ 更新 `main.go`，初始化分析器
7. ✅ 更新文档和 CHANGELOG

## 六、注意事项

1. **API限制**
   - Polymarket API 可能有速率限制，需要实现请求限流
   - 使用缓存机制减少API调用

2. **数据延迟**
   - 预测市场数据可能有延迟，需要考虑时效性
   - 设置数据过期时间，避免使用过期数据

3. **相关性判断**
   - 需要准确识别与加密货币相关的市场
   - 可以使用关键词匹配和AI分析相结合

4. **风险控制**
   - 预测市场信号仅作为参考，不应完全依赖
   - 设置信号权重，避免过度依赖单一数据源

5. **合规性**
   - 确保使用 Polymarket 数据符合当地法律法规
   - 注意数据使用条款和限制

## 七、未来扩展

1. **机器学习优化**
   - 训练模型学习预测市场信号与价格的相关性
   - 动态调整信号权重

2. **多市场聚合**
   - 聚合多个相关市场的预测，提高信号可靠性
   - 使用加权平均计算综合概率

3. **实时监控**
   - 监控关键市场的价格变化
   - 当预测概率发生显著变化时，及时生成信号

4. **回测验证**
   - 回测历史预测市场数据，验证信号有效性
   - 优化信号生成参数

