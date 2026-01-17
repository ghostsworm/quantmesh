<div align="center">
  <img src="logo/qm_thick_tail_white.svg" alt="QuantMesh Logo" width="200"/>
  
  # QuantMesh Market Maker
  
  **High-Frequency Crypto Market Maker**

  [![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org/dl/)
  [![License](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](LICENSE)
  [![GitHub Stars](https://img.shields.io/github/stars/ghostsworm/quantmesh.svg?style=social&label=Stars)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Forks](https://img.shields.io/github/forks/ghostsworm/quantmesh.svg?style=social&label=Forks)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Issues](https://img.shields.io/github/issues/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/issues)
  [![GitHub Release](https://img.shields.io/github/release/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/releases)
  [![Website](https://img.shields.io/badge/Website-quantmesh.io-green.svg)](https://quantmesh.io)
  
  [English](README.md) | [中文](docs/i18n/README.zh.md) | [Español](docs/i18n/README.es.md) | [Français](docs/i18n/README.fr.md) | [Português](docs/i18n/README.pt.md)
</div>

---

## 🎯 Why Choose QuantMesh?

| Feature | QuantMesh | Other Solutions |
|---------|-----------|----------------|
| **Exchange Support** | 20+ exchanges | Usually 3-5 |
| **Response Latency** | Millisecond-level | Second-level |
| **Risk Control** | Multi-layer active control | Basic control |
| **Production Tested** | $100M+ trading volume | Untested |
| **Web Interface** | ✅ Complete React UI | ❌ None/Basic |
| **Open Source** | AGPL-3.0 | Closed source/Restricted |
| **Real-time Data** | WebSocket-only | REST polling |
| **Concurrency** | 1000+ orders/sec | Limited |

**Key Advantages:**
- ✅ **Battle-tested**: Proven with $100M+ trading volume
- ✅ **High Performance**: Sub-10ms latency with WebSocket architecture
- ✅ **Comprehensive**: Complete solution from trading to monitoring
- ✅ **Transparent**: Fully open source, auditable code
- ✅ **Extensible**: Plugin system for customization

---

## 📊 Performance Metrics

- **Trading Volume**: $100M+ production-tested
- **Response Latency**: <10ms (WebSocket-driven)
- **Supported Exchanges**: 20+
- **Concurrent Processing**: 1000+ orders/second
- **System Availability**: 99.9%+
- **Daily Trading Capacity**: $3M+ per day (example: ETHUSDC)

---

## 📖 Introduction

QuantMesh is a high-performance, low-latency cryptocurrency market maker system focusing on long grid trading strategies for perpetual contract markets. Developed in Go and driven by WebSocket real-time data streams, it aims to provide stable liquidity support for major exchanges like Binance, Bitget, and Gate.io.

After several iterations, we have used this system to trade over $100 million in virtual currency. For example, trading Binance ETHUSDC with zero fees, a price interval of $1, and $300 per order, the daily trading volume can exceed $3 million, and over $50 million per month. As long as the market is oscillating or trending upward, it will continue to generate profits. If the market falls unilaterally, $30,000 in margin can guarantee no liquidation for a drop of 1000 points. Through continuous trading to lower costs, a 50% recovery is enough to break even, and returning to the original opening price can yield substantial profits. If there is a unilateral rapid decline, the active risk control system will automatically identify and immediately stop trading, only allowing continued orders when the market recovers, without worrying about liquidation from price spikes.

Example: Starting trading ETH at 3000 points, the price drops to 2700 points, losing approximately $3,000. When the price recovers to above 2850 points, it breaks even. Returning to 3000 points, profits range from $1,000 to $3,000.

## 📜 Project Origin

This project was originally developed based on [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker), published by [dennisyang1986](https://github.com/dennisyang1986) under the MIT License.

Based on the original project, we have made the following major improvements and extensions:

- ✨ **Complete Frontend Interface**: Added a React + TypeScript web management interface providing visual trading monitoring, configuration management, and data analysis
- 🏦 **Exchange Expansion**: Expanded from 3 exchanges (Binance, Bitget, Gate.io) in the original project to **20+ major exchanges**
- 🔒 **Financial-Grade Stability**: Comprehensively improved system reliability, including comprehensive error handling, concurrency safety mechanisms, data consistency guarantees, automatic recovery, etc.
- 📊 **Enhanced Monitoring**: Improved logging system, metrics collection (Prometheus), health checks, and real-time alerts
- 🛡️ **Strengthened Risk Control**: Multi-layer risk monitoring, automatic reconciliation, anomaly circuit breaking, and fund safety protection
- 🔌 **Plugin System**: Support for extensible plugin mechanisms for easy customization and secondary development
- 📱 **Internationalization Support**: Multi-language interface (Chinese/English), i18n support
- 🧪 **Testnet Support**: Support for testnet environments of multiple exchanges for development and testing

For detailed improvement descriptions and third-party software information, please refer to the [NOTICE](NOTICE) file.

**Important Note**: This project is now distributed under the **GNU Affero General Public License v3.0 (AGPL-3.0)**. In accordance with the MIT License requirements of the original project, we have retained acknowledgment of the original project.

## ✨ Key Features

- **Multi-Exchange Support**: Compatible with Binance, Bitget, Gate.io, Bybit, EdgeX, and other major platforms.
- **Millisecond-Level Response**: Fully WebSocket-driven (market data and order flow), eliminating polling delays.
- **Smart Grid Strategy**: 
  - **Fixed Amount Mode**: More controllable capital utilization.
  - **Super Slot System**: Intelligently manages order and position states, preventing concurrency conflicts.
- **Powerful Risk Control System**:
  - **Active Risk Control**: Real-time monitoring of K-line volume anomalies, automatically pausing trading.
  - **Fund Safety**: Automatically checks balance, leverage, and maximum position risk before startup.
  - **Automatic Reconciliation**: Regularly synchronizes local and exchange states to ensure data consistency.
- **High-Concurrency Architecture**: Efficient concurrency model based on Goroutine + Channel + Sync.Map.

## 🏦 Supported Exchanges

| Exchange | Status | Daily Trading Volume | Notes |
|----------|--------|---------------------|-------|
| **Binance** | ✅ Stable | $50B+ | World's largest exchange |
| **Bitget** | ✅ Stable | $10B+ | Mainstream futures trading platform |
| **Gate.io** | ✅ Stable | $5B+ | Established exchange |
| **OKX** | ✅ Stable | $20B+ | Top 3 globally, strong Chinese user base |
| **Bybit** | ✅ Stable | $15B+ | Mainstream futures trading platform |
| **Huobi (HTX)** | ✅ Stable | $5B+ | Established exchange, strong Chinese market |
| **KuCoin** | ✅ Stable | $3B+ | Rich altcoins, futures contract support |
| **Kraken** | ✅ Stable | $2B+ | Strong compliance, mainstream in Europe and America |
| **Bitfinex** | ✅ Stable | $1B+ | Established exchange, good liquidity |
| **MEXC** | ✅ Stable | $8B+ | Large futures trading volume, rich altcoins, testnet supported |
| **BingX** | ✅ Stable | $3B+ | Social trading platform, good futures experience, testnet supported |
| **Deribit** | ✅ Stable | $2B+ | World's largest options exchange, supports futures + options, testnet supported |
| **BitMEX** | ✅ Stable | $2B+ | Established derivatives exchange, up to 100x leverage, testnet supported |
| **Phemex** | ✅ Stable | $2B+ | Zero-fee futures trading, high-performance engine, testnet supported |
| **WOO X** | ✅ Stable | $1.5B+ | Institutional-grade exchange, deep liquidity, testnet supported |
| **CoinEx** | ✅ Stable | $1B+ | Established exchange (2017), rich altcoins, testnet supported |
| **Bitrue** | ✅ Stable | $1B+ | Main XRP ecosystem exchange, strong Southeast Asian market, testnet supported |
| **XT.COM** | ✅ Stable | $800M+ | Emerging exchange, rich altcoins, testnet supported |
| **BTCC** | ✅ Stable | $500M+ | Established exchange (2011), China's first Bitcoin exchange, testnet supported |
| **AscendEX** | ✅ Stable | $400M+ | Institutional-grade exchange, DeFi-friendly, testnet supported |
| **Poloniex** | ✅ Stable | $300M+ | Established exchange (2014), rich coin variety, testnet supported |
| **Crypto.com** | ✅ Stable | $500M+ | Well-known brand, tens of millions of users globally, testnet supported |

## Module Architecture

```
quantmesh_platform/
├── main.go                    # Main program entry, component orchestration
│
├── config/                    # Configuration management
│   └── config.go              # YAML configuration loading and validation
│
├── exchange/                  # Exchange abstraction layer (core)
│   ├── interface.go           # IExchange unified interface
│   ├── factory.go             # Factory pattern for creating exchange instances
│   ├── types.go               # Common data structures
│   ├── wrapper_*.go           # Adapters (wrapping exchanges)
│   ├── binance/               # Binance implementation
│   ├── bitget/                # Bitget implementation
│   └── gate/                  # Gate.io implementation
│
├── logger/                    # Logging system
│   └── logger.go              # File logging + console logging
│
├── monitor/                   # Price monitoring
│   └── price_monitor.go       # Global unique price stream
│
├── order/                     # Order execution layer
│   └── executor_adapter.go    # Order executor (rate limiting + retry)
│
├── position/                  # Position management (core)
│   └── super_position_manager.go  # Super slot manager
│
├── safety/                    # Safety and risk control
│   ├── safety.go              # Pre-startup safety checks
│   ├── risk_monitor.go        # Active risk control (K-line monitoring)
│   ├── reconciler.go          # Position reconciliation
│   └── order_cleaner.go       # Order cleanup
│
└── utils/                     # Utility functions
    └── orderid.go             # Custom order ID generation
```

## Best Practices

1. **For Exchange VIP Status**: This system is a volume generation tool. If price fluctuations are not large, $3,000 in margin can generate $10 million in trading volume in 2-3 days.

2. **Best Practice for Profit**: Enter the market after a round of decline. First buy a position, then start the software. It will automatically sell grid by grid upward. When your position is sold out, stop the system. If you're unsure whether the current market is a low point, you can start without a base position. If it falls further, add a position at the low point and restart to continue selling. This maximizes profits. Repeat this cycle to continuously profit. Don't worry about declines - the program continuously lowers costs. As long as it recovers by half, you break even.

## 🚀 Getting Started

### Prerequisites
- Go 1.21 or higher
- Network environment capable of accessing exchange APIs

### Installation

1. **Clone the repository**
   ```bash
   git clone https://github.com/dennisyang1986/quantmesh_market_maker.git
   cd quantmesh_market_maker
   ```

2. **Install dependencies**
   ```bash
   go mod download
   ```

### Configuration

1. Copy the example configuration file:
   ```bash
   cp config.example.yaml config.yaml
   ```

2. Edit `config.yaml` and fill in your API Key and strategy parameters:

   ```yaml
   app:
     current_exchange: "binance"  # Select exchange

   exchanges:
     binance:
       api_key: "YOUR_API_KEY"
       secret_key: "YOUR_SECRET_KEY"
       fee_rate: 0.0002

   trading:
     symbol: "ETHUSDT"       # Trading pair
     price_interval: 2       # Grid spacing (price)
     order_quantity: 30     # Amount per grid (USDT)
     buy_window_size: 10    # Number of buy orders
     sell_window_size: 10   # Number of sell orders
   ```

### Usage

#### Production Mode

Run the compiled binary:

```bash
go run main.go
```

Or build and run:

```bash
go build -o quantmesh
./quantmesh
```

The backend will serve the frontend static files on port 28888 (default).

#### Development Mode

For frontend development with hot reload and source code debugging:

**Option 1: Use the development script (Recommended)**

```bash
./dev.sh
```

This script will:
- Start the Go backend server on port 28888
- Start the Vite dev server on port 15173
- Enable hot reload for frontend code changes
- Provide source maps for debugging (no minified code)

Then access the application at: **http://localhost:15173**

**Option 2: Manual startup**

Terminal 1 - Start Go backend:
```bash
go run main.go
```

Terminal 2 - Start Vite dev server:
```bash
cd webui
pnpm dev
```

Then access the application at: **http://localhost:15173**

**Development Mode Benefits:**
- ✅ Hot reload - Frontend code changes are instantly reflected
- ✅ Source maps - Debug with original TypeScript/React code (not minified)
- ✅ Fast refresh - React components update without losing state
- ✅ Better error messages - See actual file names and line numbers

**Note:** In development mode, the Vite dev server proxies API requests (`/api/*`) and WebSocket connections (`/ws`) to the Go backend running on port 28888.

## 🏗️ Architecture

The system adopts a modular design with core components including:

- **Exchange Layer**: Unified exchange interface abstraction, shielding underlying API differences.
- **Price Monitor**: Global unique WebSocket price source, ensuring decision consistency.
- **Super Position Manager**: Core position manager, managing order lifecycle based on Slot mechanism.
- **Safety & Risk Control**: Multi-layer risk control, including startup checks, runtime monitoring, and anomaly circuit breaking.

For more detailed architecture documentation, please refer to [ARCHITECTURE.md](ARCHITECTURE.md).

## ⚠️ Disclaimer

This software is for educational and research purposes only. Cryptocurrency trading involves high risk and may result in capital loss.
- Users are solely responsible for any profits or losses from using this software.
- Always test thoroughly on Testnet before using real funds.
- The developers are not liable for losses due to software bugs, network latency, or exchange failures.

## 🪙 Crypto Payment Support

QuantMesh supports cryptocurrency payments for subscriptions and licenses:

### Supported Cryptocurrencies
- **BTC** (Bitcoin)
- **ETH** (Ethereum)
- **USDT** (Tether, ERC20)
- **USDC** (USD Coin, ERC20)

### Payment Methods
1. **Coinbase Commerce** (Recommended)
   - Automatic confirmation
   - Multiple cryptocurrencies supported
   - Easy payment page

2. **Direct Wallet Payment**
   - No third-party involvement
   - More privacy
   - Manual confirmation (1-24 hours)

### Quick Start
```bash
# Method A: Coinbase Commerce (15 minutes)
# 1. Register at https://commerce.coinbase.com
# 2. Configure API Key in .env.crypto
# 3. Start service

# Method B: Direct Wallet (5 minutes)
# 1. Configure wallet addresses
# 2. Start service
# 3. Manual confirmation
```

### Documentation
- 📖 [User Payment Guide](docs/CRYPTO_PAYMENT_GUIDE.md)
- 🚀 [Quick Start Guide](docs/CRYPTO_PAYMENT_QUICKSTART.md)
- 🔧 [Setup Guide](docs/CRYPTO_PAYMENT_SETUP.md)
- 📊 [Implementation Summary](CRYPTO_PAYMENT_SUMMARY.md)

### Why Crypto Payments?
✅ No credit card or bank account required  
✅ Global accessibility, no regional restrictions  
✅ Lower transaction fees (1% vs 2.9%)  
✅ Better privacy protection  
✅ Fast confirmation (10-30 minutes)  
✅ Perfect fit for crypto trading software  

## 📜 License

This project uses a **Dual License model**:

### AGPL-3.0 Open Source License
- ✅ Free to use, modify, and distribute
- ⚠️ **All derivative works must be open-sourced** and released under AGPL-3.0
- ⚠️ Source code must be provided even for network services
- ⚠️ Modified code must be contributed back to the community

### Commercial License
If you need to use this software in proprietary applications or services, or do not wish to open-source your modifications, you need to purchase a commercial license.

**Commercial License Scope:**
- Use in proprietary applications
- No obligation to open-source modifications
- Integrate into proprietary products for distribution
- Priority technical support and updates

**Commercial License Inquiries:**
- 📧 Email: contact@quantmesh.io
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
- 📧 Email: contact@quantmesh.io
- 🌐 Website: https://quantmesh.io/commercial

## 🤝 Contributing

We welcome contributions! Here's how you can help:

- ⭐ **Star this repo** if you find it helpful
- 🍴 **Fork and use** the project
- 🐛 **Report bugs** via [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💡 **Suggest features** via [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📝 **Submit PRs** for improvements
- 📖 **Improve documentation**

**Note:** According to the AGPL-3.0 license, all contributions to this project will be released under the same AGPL-3.0 license.

See [CONTRIBUTING.md](CONTRIBUTING.md) for detailed guidelines.

## 🙏 Acknowledgments

Thanks to the original project [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker) by [dennisyang1986](https://github.com/dennisyang1986) for their open-source contribution, which provided a solid foundation for this project. For more information, please refer to the [NOTICE](NOTICE) file.

---

## 📞 Contact & Support

- 🌐 **Website**: https://quantmesh.io
- 📧 **Email**: contact@quantmesh.io
- 💬 **Discord**: [Join our community](https://discord.gg/YOUR_INVITE_LINK)
- 🐛 **Issues**: [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💬 **Discussions**: [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📖 **Documentation**: [Full Documentation](docs/)

---

<div align="center">
  <strong>Made with ❤️ by QuantMesh Team</strong><br/>
  <sub>If you find this project helpful, please consider giving it a ⭐</sub>
</div>

Copyright © 2025 QuantMesh Team. All Rights Reserved.
