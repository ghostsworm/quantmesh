# 现货-合约价差监控系统

## 概述

价差监控系统是 QuantMesh 的核心功能之一，用于实时监控现货市场与合约市场之间的价格差异，帮助交易者发现套利机会和市场异常。

## 功能特性

### 开源版功能

- ✅ **实时价差监控**: 每分钟自动获取现货和合约价格，计算价差
- ✅ **多交易对支持**: 同时监控多个交易对（BTC, ETH, BNB, SOL等）
- ✅ **历史数据存储**: 自动保存价差历史数据到SQLite数据库
- ✅ **统计分析**: 计算平均价差、最大/最小价差、标准差等统计指标
- ✅ **Web API**: 提供RESTful API接口，方便第三方集成
- ✅ **可视化界面**: 内置WebUI，实时显示价差数据和历史图表

### 商业版功能（插件）

- 💎 **智能异常检测**: 基于Z-score的统计异常检测
- 💎 **多渠道预警**: Telegram、Webhook等多种通知方式
- 💎 **可配置阈值**: 为每个交易对设置独立的预警阈值
- 💎 **预警冷却机制**: 避免频繁通知，提高预警质量
- 💎 **资金费率背离检测**: 识别价差与资金费率的异常背离

## 快速开始

### 1. 配置

编辑 `config.yaml` 文件，启用价差监控：

```yaml
# 价差监控配置
basis_monitor:
  enabled: true  # 启用价差监控
  interval_minutes: 1  # 检查间隔（分钟）
  symbols:
    - BTCUSDT
    - ETHUSDT
    - BNBUSDT
    - SOLUSDT
```

### 2. 启动系统

```bash
./quantmesh
```

系统启动后，价差监控会自动开始运行。

### 3. 访问WebUI

打开浏览器访问：`http://localhost:8080/basis-monitor`

## API 接口

### 获取当前价差

```bash
GET /api/basis/current?symbol=BTCUSDT
```

**响应示例：**

```json
{
  "data": {
    "symbol": "BTCUSDT",
    "exchange": "binance",
    "spot_price": 50000.00,
    "futures_price": 50250.00,
    "basis": 250.00,
    "basis_percent": 0.5000,
    "funding_rate": 0.0001,
    "timestamp": "2026-01-01T16:00:00Z"
  }
}
```

### 获取历史数据

```bash
GET /api/basis/history?symbol=BTCUSDT&limit=100
```

### 获取统计数据

```bash
GET /api/basis/statistics?symbol=BTCUSDT&hours=24
```

**响应示例：**

```json
{
  "data": {
    "symbol": "BTCUSDT",
    "exchange": "binance",
    "avg_basis": 0.3500,
    "max_basis": 0.8000,
    "min_basis": -0.2000,
    "std_dev": 0.2500,
    "data_points": 1440,
    "hours": 24
  }
}
```

## 价差交易策略

### 正向套利（Carry Trade）

**条件**: 合约价格 > 现货价格（正价差）

**操作**:
1. 买入现货
2. 卖出（做空）等量合约
3. 等待价差收敛或到期交割

**收益**: 价差收敛的差价 + 资金费率收入（如果为正）

### 反向套利（Reverse Carry）

**条件**: 合约价格 < 现货价格（负价差）

**操作**:
1. 卖出（做空）现货
2. 买入（做多）等量合约
3. 等待价差收敛或到期交割

**收益**: 价差收敛的差价 + 资金费率收入（如果为负）

### 风险提示

⚠️ **注意事项**:
- 价差套利需要同时持有现货和合约，占用较多资金
- 需要考虑交易手续费、资金费率等成本
- 价差可能持续扩大，导致浮亏
- 现货做空需要借币，可能面临借币成本和强平风险

## 商业插件使用

### 安装价差预警插件

1. 购买 `basis_alert` 插件 License
2. 配置 `config.yaml`:

```yaml
plugins:
  enabled: true
  directory: "../quantmesh-premium/plugins"
  
  licenses:
    basis_alert: "YOUR_LICENSE_KEY"
  
  config:
    basis_alert:
      enabled: true
      check_interval_seconds: 60
      
      thresholds:
        BTCUSDT:
          basis_percent_high: 0.5   # 0.5%
          basis_percent_low: -0.5   # -0.5%
          zscore_threshold: 2.0
      
      notifications:
        cooldown_minutes: 30
        telegram_enabled: true
        telegram_bot_token: "YOUR_BOT_TOKEN"
        telegram_chat_id: "YOUR_CHAT_ID"
```

3. 重启系统

### 预警类型

1. **绝对阈值预警**: 价差超过设定的百分比阈值
2. **统计异常预警**: Z-score 超过阈值（基于历史数据）
3. **资金费率背离预警**: 价差与资金费率方向不一致

## 技术实现

### 架构设计

```
┌─────────────────┐
│  BasisMonitor   │  ← 核心监控服务
└────────┬────────┘
         │
         ├─→ Exchange.GetSpotPrice()      ← 获取现货价格
         ├─→ Exchange.GetLatestPrice()    ← 获取合约价格
         ├─→ Exchange.GetFundingRate()    ← 获取资金费率
         │
         ├─→ Storage.SaveBasisData()      ← 保存历史数据
         │
         └─→ BasisAlertPlugin (可选)      ← 商业预警插件
```

### 数据库表结构

```sql
CREATE TABLE basis_data (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    symbol TEXT NOT NULL,
    exchange TEXT NOT NULL,
    spot_price REAL NOT NULL,
    futures_price REAL NOT NULL,
    basis REAL NOT NULL,
    basis_percent REAL NOT NULL,
    funding_rate REAL,
    timestamp DATETIME NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_basis_symbol_time ON basis_data(symbol, timestamp);
```

### 价差计算公式

```go
basis := futuresPrice - spotPrice
basisPercent := (basis / spotPrice) * 100

// 年化价差（假设资金费率每8小时结算）
annualizedBasis := basisPercent * (365 * 24 / 8)
```

## 常见问题

### Q: 为什么价差数据没有更新？

A: 检查以下几点：
1. 确认 `basis_monitor.enabled` 设置为 `true`
2. 检查交易所API是否正常（现货和合约API都需要可用）
3. 查看日志是否有错误信息

### Q: 如何添加更多交易对？

A: 在 `config.yaml` 的 `basis_monitor.symbols` 列表中添加交易对名称：

```yaml
basis_monitor:
  symbols:
    - BTCUSDT
    - ETHUSDT
    - 新交易对
```

### Q: 价差监控会影响交易性能吗？

A: 不会。价差监控使用独立的goroutine运行，不会阻塞主交易逻辑。

### Q: 支持哪些交易所？

A: 目前支持：
- ✅ Binance
- ✅ OKX
- ✅ Bybit
- ✅ Gate.io
- ✅ Bitget

## 更新日志

### v1.0.0 (2026-01-01)

- ✅ 首次发布
- ✅ 支持5大主流交易所
- ✅ 实时价差监控
- ✅ 历史数据存储
- ✅ WebUI可视化
- ✅ 商业预警插件

## 相关资源

- [快速入门指南](./articles/zh/02-快速入门.md)
- [API文档](./API.md)
- [插件开发指南](./PLUGIN_DEVELOPMENT.md)

## 技术支持

如有问题，请通过以下方式联系我们：
- 📧 Email: support@quantmesh.io
- 💬 Telegram: @quantmesh
- 🌐 Website: https://quantmesh.io

---

*本文档最后更新: 2026-01-01*

