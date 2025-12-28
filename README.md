<div align="center">
  <img src="https://r2.quantmesh.com/quantmesh_logo.png" alt="QuantMesh Logo" width="600"/>
  
  # QuantMesh Market Maker
  
  **毫秒级高频加密货币做市商系统 | High-Frequency Crypto Market Maker**

  [![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org/dl/)
  [![License](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](LICENSE)
</div>

---

## 📖 项目简介 (Introduction)

QuantMesh Market Maker 是一个高性能、低延迟的加密货币做市商系统，专注于永续合约市场的单向做多无限独立网格交易策略。系统采用 Go 语言开发，基于 WebSocket 实时数据流驱动，旨在为 Binance、Bitget、Gate.io 等主流交易所提供稳定的流动性支持。

经过数个版本迭代，我们已经使用此系统交易超过1亿美元的虚拟货币，例如，交易币安ETHUSDC，0手续，价格间隔1美元，每笔购买300美元，每天的交易量将达到300万美元以上，一个月可以交易5000万美元以上，只要市场是震荡或向上将持续产生盈利，如果市场单边下跌，3万美元保证金可以保证下跌1000个点不爆仓，通过不断交易拉低成本，只要回涨50%即可保本，涨回开仓原价可以赚到丰厚利润，如果出现单边极速下跌，主动风控系统将会自动识别立刻停止交易，当市场恢复后才允许继续下单，不担心插针爆仓。

举例： eth 3000点开始交易，价格下跌到2700点，亏损约3000美元，价格涨回2850点以上已经保本，涨回3000点，盈利在1000-3000美元。

QuantMesh is a high-performance, low-latency cryptocurrency market maker system focusing on long grid trading strategies for perpetual contract markets. Developed in Go and driven by WebSocket real-time data streams, it aims to provide stable liquidity support for major exchanges like Binance, Bitget, and Gate.io.

## 📺 实时演示 (Live Demo)

<video src="https://r2.quantmesh.com/product_review.mp4" controls="controls" width="100%"></video>

[点击观看演示视频 / Watch Demo Video](https://r2.quantmesh.com/product_review.mp4)

## ✨ 核心特性 (Key Features)

- **多交易所支持**: 适配 Binance, Bitget, Gate.io, Bybit, EdgeX 等主流平台。
- **毫秒级响应**: 全 WebSocket 驱动（行情与订单流），拒绝轮询延迟。
- **智能网格策略**: 
  - **固定金额模式**: 资金利用率更可控。
  - **超级槽位系统 (Super Slot)**: 智能管理挂单与持仓状态，防止并发冲突。
- **强大的风控系统**:
  - **主动风控**: 实时监控 K 线成交量异常，自动暂停交易。
  - **资金安全**: 启动前自动检查余额、杠杆倍数与最大持仓风险。
  - **自动对账**: 定期同步本地与交易所状态，确保数据一致性。
- **高并发架构**: 基于 Goroutine + Channel + Sync.Map 的高效并发模型。

## 🏦 支持的交易所 (Supported Exchanges)

| 交易所 (Exchange) | 状态 (Status) | 日均交易量 | 备注
|-------------------|---------------|-----------|-----
| **Binance**       | ✅ Stable     | $50B+     | 全球最大交易所
| **Bitget**        | ✅ Stable     | $10B+     | 合约交易主流平台
| **Gate.io**       | ✅ Stable     | $5B+      | 老牌交易所
| **OKX**           | ✅ Stable     | $20B+     | 全球前三，中文用户多
| **Bybit**         | ✅ Stable     | $15B+     | 合约交易主流平台
| **Huobi (HTX)**   | ✅ Stable     | $5B+      | 老牌交易所，中文市场强
| **KuCoin**        | ✅ Stable     | $3B+      | 山寨币丰富，期货合约支持
| **Kraken**        | 🚧 Beta       | $2B+      | 合规性强（开发中）
| **Bitfinex**      | 🚧 Beta       | $1B+      | 老牌交易所（开发中）


## 模块架构

```
quantmesh_platform/
├── main.go                    # 主程序入口，组件编排
│
├── config/                    # 配置管理
│   └── config.go              # YAML配置加载与验证
│
├── exchange/                  # 交易所抽象层（核心）
│   ├── interface.go           # IExchange 统一接口
│   ├── factory.go             # 工厂模式创建交易所实例
│   ├── types.go               # 通用数据结构
│   ├── wrapper_*.go           # 适配器（包装各交易所）
│   ├── binance/               # 币安实现
│   ├── bitget/                # Bitget实现
│   └── gate/                  # Gate.io实现
│
├── logger/                    # 日志系统
│   └── logger.go              # 文件日志 + 控制台日志
│
├── monitor/                   # 价格监控
│   └── price_monitor.go       # 全局唯一价格流
│
├── order/                     # 订单执行层
│   └── executor_adapter.go    # 订单执行器（限流+重试）
│
├── position/                  # 仓位管理（核心）
│   └── super_position_manager.go  # 超级槽位管理器
│
├── safety/                    # 安全与风控
│   ├── safety.go              # 启动前安全检查
│   ├── risk_monitor.go        # 主动风控（K线监控）
│   ├── reconciler.go          # 持仓对账
│   └── order_cleaner.go       # 订单清理
│
└── utils/                     # 工具函数
    └── orderid.go             # 自定义订单ID生成
```

## 最佳实践
1.用来刷交易所vip，本系统是刷量神器，如果上涨下跌幅度不大，3000美元保证金两三天即可刷出1000万美元交易量。

2.赚钱的最佳实践，在市场经过一轮下跌后介入，先买一笔持仓，然后再启动软件，会自动向上一格格卖出，当你的持仓卖光以后停止系统，或不确定当前市场是否是低点，可以不买底仓启动，如果下跌在低点再补一笔持仓重新启动持续给你卖出，利润将最大化，如此循环往复持续赚钱，下跌也不怕，程序持续拉低成本，只要涨回一半即可保本。

## 🚀 快速开始 (Getting Started)

### 环境要求 (Prerequisites)
- Go 1.21 或更高版本
- 网络环境需能访问交易所 API

### 安装 (Installation)

1. **克隆仓库**
   ```bash
   git clone https://github.com/dennisyang1986/quantmesh_market_maker.git
   cd quantmesh_market_maker
   ```

2. **安装依赖**
   ```bash
   go mod download
   ```

### 配置 (Configuration)

1. 复制示例配置文件：
   ```bash
   cp config.example.yaml config.yaml
   ```

2. 编辑 `config.yaml`，填入你的 API Key 和策略参数：

   ```yaml
   app:
     current_exchange: "binance"  # 选择交易所

   exchanges:
     binance:
       api_key: "YOUR_API_KEY"
       secret_key: "YOUR_SECRET_KEY"
       fee_rate: 0.0002

   trading:
     symbol: "ETHUSDT"       # 交易对
     price_interval: 2       # 网格间距 (价格)
     order_quantity: 30      # 每格投入金额 (USDT)
     buy_window_size: 10     # 买单挂单数量
     sell_window_size: 10    # 卖单挂单数量
   ```

### 运行 (Usage)

```bash
go run main.go
```

或者编译后运行：

```bash
go build -o quantmesh
./quantmesh
```

## 🏗️ 系统架构 (Architecture)

系统采用模块化设计，核心组件包括：

- **Exchange Layer**: 统一的交易所接口抽象，屏蔽底层 API 差异。
- **Price Monitor**: 全局唯一的 WebSocket 价格源，确保决策一致性。
- **Super Position Manager**: 核心仓位管理器，基于槽位 (Slot) 机制管理订单生命周期。
- **Safety & Risk Control**: 多层级风控，包含启动检查、运行时监控和异常熔断。

更多详细架构说明请参阅 [ARCHITECTURE.md](ARCHITECTURE.md)。

## ⚠️ 免责声明 (Disclaimer)

本软件仅供学习和研究使用。加密货币交易具有极高风险，可能导致资金损失。
- 使用本软件产生的任何盈亏由用户自行承担。
- 请务必在实盘前使用测试网 (Testnet) 进行充分测试。
- 开发者不对因软件错误、网络延迟或交易所故障导致的损失负责。

This software is for educational and research purposes only. Cryptocurrency trading involves high risk.
- Users are solely responsible for any profits or losses.
- Always test thoroughly on Testnet before using real funds.
- The developers are not liable for losses due to software bugs, network latency, or exchange failures.

## 📜 许可证 (License)

本项目采用**双许可模式 (Dual License)**：

### AGPL-3.0 开源许可
- ✅ 免费使用、修改和分发
- ⚠️ **所有衍生作品必须开源**，并在 AGPL-3.0 许可下发布
- ⚠️ 即使通过网络服务使用，也必须提供源代码
- ⚠️ 修改后的代码必须回馈给社区

### 商业许可
如果您需要在专有应用或服务中使用本软件，或者不希望开源您的修改，您需要购买商业许可证。

**商业许可授权范围：**
- 在专有应用中使用本软件
- 修改代码无需开源
- 将本软件集成到专有产品中分发
- 优先技术支持和技术更新

**商业许可咨询：**
- 📧 Email: commercial@quantmesh.io
- 🌐 Website: https://quantmesh.io/commercial

---

### License Details

This project is dual-licensed under:

1. **AGPL-3.0 (GNU Affero General Public License v3.0)**
   - Free for use, modification, and distribution
   - All derivative works must be open-sourced under AGPL-3.0
   - Source code must be provided to all users, even for network services
   - Modifications must be contributed back to the community

2. **Commercial License**
   - Required for proprietary use
   - No obligation to open-source modifications
   - Includes priority support and updates

For commercial licensing inquiries, please contact:
- 📧 Email: commercial@quantmesh.com
- 🌐 Website: https://quantmesh.com/commercial

## 🤝 贡献 (Contributing)

欢迎提交 Issue 和 Pull Request！

**注意：** 根据 AGPL-3.0 许可，所有对本项目的贡献都将以相同的 AGPL-3.0 许可发布。

---
Copyright © 2025 QuantMesh Team. All Rights Reserved.
