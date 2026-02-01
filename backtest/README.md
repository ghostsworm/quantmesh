# QuantMesh 回测系统

## 功能概述

QuantMesh 回测系统为商业插件提供完整的历史数据回测功能，支持：

- ✅ **数据获取与缓存**: 从 Binance 获取历史 K 线数据，自动缓存到本地 CSV
- ✅ **三个策略回测**: 动量策略、均值回归策略、趋势跟踪策略
- ✅ **完整指标计算**: 收益率、夏普比率、最大回撤、胜率等 20+ 指标
- ✅ **高级风险指标**: VaR、CVaR（95% 和 99% 置信度）
- ✅ **专业报告生成**: Markdown 格式报告，包含详细的交易明细和分析
- ✅ **RESTful API**: 完整的 HTTP API 接口，支持 Web 集成

## 快速开始

### 1. 命令行使用

```bash
# 运行测试
go test -v ./backtest

# 运行特定策略测试
go test -v ./backtest -run TestMomentumStrategy
go test -v ./backtest -run TestMeanReversionStrategy
go test -v ./backtest -run TestTrendFollowingStrategy
```

### 2. API 使用

启动服务后，通过 HTTP API 运行回测：

```bash
# 运行回测
curl -X POST http://localhost:8080/api/backtest/run \
  -H "Content-Type: application/json" \
  -d '{
    "strategy": "momentum",
    "symbol": "BTCUSDT",
    "interval": "1h",
    "start_time": "2023-01-01T00:00:00Z",
    "end_time": "2023-06-30T23:59:59Z",
    "initial_capital": 10000
  }'

# 查看缓存统计
curl http://localhost:8080/api/backtest/cache/stats

# 列出所有缓存
curl http://localhost:8080/api/backtest/cache/list

# 清理所有缓存
curl -X DELETE http://localhost:8080/api/backtest/cache
```

## 数据缓存

### 缓存结构

```
backtest/
├── cache/
│   ├── BTCUSDT_1h_2023-01-01_2023-06-30.csv
│   ├── BTCUSDT_5m_2023-01-01_2023-06-30.csv
│   └── cache_index.json
└── reports/
    ├── momentum_BTCUSDT_2025-12-31.md
    └── momentum_BTCUSDT_2025-12-31_equity.csv
```

### 缓存优势

- **首次运行**: 从 Binance API 下载数据，自动保存到本地
- **后续运行**: 直接从本地 CSV 读取，速度提升 10-20 倍
- **节省成本**: 避免重复请求 Binance API，防止触发限流

## 支持的策略

### 1. 动量策略 (Momentum)

- **原理**: 基于 RSI 指标，超卖买入，超买卖出
- **参数**: RSI 周期 14，超买线 70，超卖线 30
- **适用行情**: 震荡市

### 2. 均值回归策略 (Mean Reversion)

- **原理**: 基于布林带，价格偏离均值时交易
- **参数**: 周期 20，阈值 2 倍标准差
- **适用行情**: 震荡市

### 3. 趋势跟踪策略 (Trend Following)

- **原理**: 基于双均线，金叉买入，死叉卖出
- **参数**: 快线 10，慢线 30
- **适用行情**: 趋势市

## 回测指标

### 收益指标

- 总收益率
- 年化收益率

### 风险指标

- 最大回撤
- 最大回撤持续时间
- 波动率（年化）

### 风险调整收益

- 夏普比率 (Sharpe Ratio)
- 索提诺比率 (Sortino Ratio)
- 卡玛比率 (Calmar Ratio)

### 交易指标

- 总交易次数
- 胜率
- 利润因子
- 平均盈利/亏损
- 最大单笔盈利/亏损
- 最大连续盈利/亏损

### 高级风险指标

- **VaR (95%)**: 95% 置信度下的最大损失
- **VaR (99%)**: 99% 置信度下的最大损失
- **CVaR (95%)**: 超过 VaR 的平均损失
- **CVaR (99%)**: 超过 VaR 的平均损失

## 报告示例

每次回测会生成：

1. **Markdown 报告**: 包含所有指标和交易明细
2. **权益曲线 CSV**: 可用于绘制图表
3. **结论分析**: 自动评估策略表现

## 架构设计

```
┌─────────────────┐
│  Binance API    │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  Data Fetcher   │◄──────┐
│  (CSV Cache)    │       │
└────────┬────────┘       │
         │                │
         ▼                │
┌─────────────────┐       │
│  Backtester     │       │
│  Engine         │       │
└────────┬────────┘       │
         │                │
         ▼                │
┌─────────────────┐       │
│  Plugin         │       │
│  Adapters       │       │
└────────┬────────┘       │
         │                │
         ▼                │
┌─────────────────┐       │
│  Metrics        │       │
│  Calculator     │       │
└────────┬────────┘       │
         │                │
         ▼                │
┌─────────────────┐       │
│  Report         │       │
│  Generator      │       │
└─────────────────┘       │
                          │
         Cache Hit ───────┘
```

## Web 仪表板

Web 仪表板功能通过 API 接口提供，前端可以使用任何框架（React、Vue 等）来实现可视化：

- 权益曲线图表
- 回撤曲线图表
- 交易分布图
- 指标对比表

## 扩展性

### 添加新策略

1. 在 `plugin_adapter.go` 中实现 `StrategyAdapter` 接口
2. 实现 `OnCandle()` 和 `GetName()` 方法
3. 在 API 路由中注册新策略

### 添加新指标

1. 在 `metrics.go` 中添加计算函数
2. 更新 `Metrics` 结构
3. 在报告模板中添加显示

## 性能优化

- **缓存机制**: 避免重复下载历史数据
- **批量处理**: 分批获取 Binance 数据（每批 1000 根 K 线）
- **限流保护**: 自动延迟避免触发 API 限流
- **并发安全**: 使用互斥锁保护共享数据

## 注意事项

1. **数据准确性**: 历史数据来自 Binance，确保网络连接稳定
2. **交易成本**: 回测已包含 Binance 合约手续费（Taker 0.04%, Maker 0.02%）和滑点（0.03%）
3. **过拟合风险**: 避免过度优化参数以适应历史数据
4. **实盘差异**: 回测结果仅供参考，实盘可能因流动性、网络延迟等因素产生差异

## 商业价值

回测系统为商业插件提供：

- ✅ **可信度证明**: 通过历史数据验证策略有效性
- ✅ **风险评估**: 全面的风险指标帮助用户了解策略风险
- ✅ **购买决策**: 详细的回测报告帮助用户选择合适的策略
- ✅ **参数优化**: 支持多参数回测，找到最优配置

## 未来计划

- [ ] 多交易对组合回测
- [ ] 参数优化器（网格搜索）
- [ ] 实时回测（滚动窗口）
- [ ] 蒙特卡洛模拟
- [ ] 更多策略支持（网格、套利等）

