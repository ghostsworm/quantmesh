# 新闻监控系统集成指南

## 概述

新闻监控系统是一个基于实时新闻分析的风险预警模块，通过监控和分析与比特币相关的新闻，预测可能的价格大跌风险，为交易系统提供额外的风控保护。

## 功能特性

1. **多新闻源支持**：支持NewsAPI、RSS等新闻源
2. **智能风险评分**：基于关键词、时间衰减、来源权重等因素计算风险评分（0-100）
3. **大跌概率预测**：基于风险评分和历史数据计算比特币大跌概率（0-1）
4. **实时监控**：定期检查新闻并更新风险评估
5. **Prometheus指标**：提供风险评分、大跌概率等指标供监控

## 配置说明

在 `config.yaml` 中添加以下配置：

```yaml
news_monitor:
  enabled: true                    # 是否启用新闻监控
  check_interval: 5m               # 检查间隔（5分钟）
  sources:                         # 新闻源列表
    - newsapi
    - rss
  news_api_key: "your-api-key"     # NewsAPI密钥（可选）
  rss_feeds:                       # RSS源列表（可选）
    - "https://coindesk.com/feed"
    - "https://www.cointelegraph.com/rss"
  keywords:                        # 监控关键词
    - "bitcoin"
    - "crypto"
    - "regulation"
    - "SEC"
    - "伊朗"
    - "战争"
    - "关税"
  risk_threshold: 70               # 风险阈值，超过此值触发警告
```

## 集成到现有系统

### 1. 在main.go中初始化

```go
import (
    "quantmesh/monitor"
    "quantmesh/storage"
)

// 创建新闻监控器
newsMonitor := monitor.NewNewsMonitor(cfg, storageService)

// 启动监控
if err := newsMonitor.Start(); err != nil {
    logger.Error("启动新闻监控失败: %v", err)
}

// 在程序退出时停止
defer newsMonitor.Stop()
```

### 2. 与RiskMonitor集成

在 `safety/risk_monitor.go` 中集成新闻监控：

```go
import "quantmesh/monitor"

type RiskMonitor struct {
    // ... 现有字段
    newsMonitor *monitor.NewsMonitor
}

// 在检查风控时考虑新闻风险
func (r *RiskMonitor) checkSymbol(symbol string) (bool, string) {
    // 原有的K线检查逻辑
    isPanic, reason := r.checkKlineAnomaly(symbol)
    
    // 如果新闻监控显示高风险，也触发风控
    if r.newsMonitor != nil && r.newsMonitor.IsHighRisk() {
        assessment := r.newsMonitor.GetRiskAssessment()
        if assessment.OverallRiskScore >= 80 {
            return true, fmt.Sprintf("新闻风险过高(%.2f)", assessment.OverallRiskScore)
        }
    }
    
    return isPanic, reason
}
```

### 3. 获取风险评估

```go
// 获取当前风险评估
assessment := newsMonitor.GetRiskAssessment()

fmt.Printf("综合风险评分: %.2f\n", assessment.OverallRiskScore)
fmt.Printf("大跌概率: %.2f%%\n", assessment.CrashProbability * 100)
fmt.Printf("建议: %s\n", assessment.Recommendation)
fmt.Printf("风险因素: %v\n", assessment.RiskFactors)

// 检查是否高风险
if newsMonitor.IsHighRisk() {
    logger.Warn("⚠️ 检测到高风险新闻，建议减少仓位或停止交易")
}
```

## 风险评估说明

### 风险评分计算

风险评分（0-100）基于以下因素：

1. **关键词匹配**：匹配高风险关键词（如"战争"、"禁令"、"黑客"等）
2. **关键词密度**：匹配的关键词数量
3. **时间衰减**：越新的新闻权重越高
4. **来源权重**：权威媒体（如Reuters、Bloomberg、CoinDesk）权重更高
5. **类别权重**：地缘政治、监管政策、交易所安全等类别权重不同

### 大跌概率计算

大跌概率（0-1）基于：
- 综合风险评分
- 高风险新闻数量
- 历史数据模式

### 建议级别

- `normal`: 风险评分 < 40，正常交易
- `caution`: 风险评分 40-60，谨慎交易
- `reduce_position`: 风险评分 60-80，减少仓位
- `stop_trading`: 风险评分 >= 80，停止交易

## Prometheus指标

系统提供以下Prometheus指标：

- `quantmesh_news_risk_score`: 新闻风险评分（0-100）
- `quantmesh_bitcoin_crash_probability`: 比特币大跌概率（0-1）
- `quantmesh_high_risk_news_count`: 最近24小时高风险新闻数量

## 新闻源配置

### NewsAPI

1. 访问 https://newsapi.org/ 注册账号
2. 获取API密钥
3. 在配置文件中设置 `news_api_key`

### RSS源

支持任何标准的RSS/Atom feed，例如：
- CoinDesk: https://coindesk.com/feed
- CoinTelegraph: https://www.cointelegraph.com/rss
- 自定义RSS源

## 注意事项

1. **API限制**：NewsAPI免费版有请求限制，建议检查间隔设置为5-10分钟
2. **代理配置**：如果在中国大陆，可能需要配置代理（通过环境变量 `HTTPS_PROXY`）
3. **误报处理**：新闻监控可能产生误报，建议结合技术指标（K线、成交量等）综合判断
4. **成本考虑**：NewsAPI付费版有更高的请求限制，根据需求选择

## 扩展开发

### 添加新的新闻源

在 `news_monitor.go` 的 `fetchFromSource` 方法中添加新的case：

```go
case "custom_source":
    newsItems, err = nm.fetchFromCustomSource()
    // ...
```

### 自定义风险关键词

修改 `initRiskKeywords` 方法中的关键词库：

```go
nm.highRiskKeywords = map[string]float64{
    "你的关键词": 0.9,  // 权重0-1
    // ...
}
```

### 调整风险评分算法

修改 `calculateRiskScore` 方法中的计算逻辑，调整各因素的权重。

## 示例输出

```
📰 启动新闻监控系统 (检查间隔: 5m0s)
⚠️ 新闻风险评分较高: 75.50, 大跌概率: 65.00%, 建议: reduce_position
风险因素: [地缘政治, 监管政策]
📰 保存高风险新闻: 伊朗爆炸事件导致比特币跌破81,000美元 (风险评分: 85.20)
```

## 故障排查

1. **无法获取新闻**：检查网络连接、API密钥、代理配置
2. **风险评分始终为0**：检查关键词配置、新闻源是否正常工作
3. **误报过多**：调整风险阈值、优化关键词库、增加时间衰减权重
