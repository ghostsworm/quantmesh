<div align="center">
  <img src="../../logo/qm_thick_tail_white.svg" alt="QuantMesh Logo" width="200"/>
  
  # QuantMesh Market Maker
  
  **High-Frequency Crypto Market Maker**

  [![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org/dl/)
  [![License](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](../../LICENSE)
  [![GitHub Stars](https://img.shields.io/github/stars/ghostsworm/quantmesh.svg?style=social&label=Stars)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Forks](https://img.shields.io/github/forks/ghostsworm/quantmesh.svg?style=social&label=Forks)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Issues](https://img.shields.io/github/issues/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/issues)
  [![GitHub Release](https://img.shields.io/badge/Release-GitHub-blue.svg)](https://github.com/ghostsworm/quantmesh/releases)
  [![Website](https://img.shields.io/badge/Website-quantmesh.io-green.svg)](https://quantmesh.io)
  
  [简体中文](../../README.md) | [繁體中文](README.zh-TW.md) | [English](README.en.md) | [Español](README.es.md) | [Français](README.fr.md) | [Português](README.pt.md) | [Tagalog](README.tl.md)
</div>

---

## 🎯 Bakit Piliin ang QuantMesh?

| Feature | QuantMesh | Iba pang Solusyon |
|---------|-----------|----------------|
| **Exchange Support** | 20+ exchanges | Karaniwang 3-5 |
| **Response Latency** | Millisecond-level | Second-level |
| **Risk Control** | Multi-layer active control | Basic control |
| **Production Tested** | $100M+ trading volume | Hindi pa nasubukan |
| **Web Interface** | ✅ Kumpletong React UI | ❌ Wala/Basic |
| **Open Source** | AGPL-3.0 | Closed source/Restricted |
| **Real-time Data** | WebSocket-only | REST polling |
| **Concurrency** | 1000+ orders/sec | Limitado |

**Pangunahing Kalamangan:**
- ✅ **Napatunayan sa labanan**: Napatunayan na may $100M+ trading volume
- ✅ **Mataas na Performance**: Sub-10ms latency na may WebSocket architecture
- ✅ **Komprehensibo**: Kumpletong solusyon mula sa trading hanggang monitoring
- ✅ **Transparent**: Buong open source, naa-audit na code
- ✅ **Napapalawak**: Plugin system para sa customization

---

## 📊 Performance Metrics

- **Trading Volume**: $100M+ na nasubukan sa production
- **Response Latency**: <10ms (WebSocket-driven)
- **Supported Exchanges**: 20+
- **Concurrent Processing**: 1000+ orders/second
- **System Availability**: 99.9%+
- **Daily Trading Capacity**: $3M+ kada araw (halimbawa: ETHUSDC)

---

## 📖 Introduksyon

Ang QuantMesh ay isang high-performance, low-latency cryptocurrency market maker system na nakatuon sa long grid trading strategies para sa perpetual contract markets. Binuo sa Go at pinapatakbo ng WebSocket real-time data streams, layunin nitong magbigay ng stable liquidity support para sa major exchanges tulad ng Binance, Bitget, at Gate.io.

Pagkatapos ng ilang iterations, ginamit namin ang sistemang ito para mag-trade ng mahigit $100 milyon sa virtual currency. Halimbawa, ang pag-trade ng Binance ETHUSDC na may zero fees, price interval na $1, at $300 kada order, ang daily trading volume ay maaaring lumampas sa $3 milyon, at mahigit $50 milyon kada buwan. Hangga't ang market ay nag-o-oscillate o trending pataas, patuloy itong magge-generate ng profit. Kung ang market ay bumagsak nang one-sided, ang $30,000 na margin ay maaaring mag-guarantee ng walang liquidation para sa pagbaba ng 1000 points. Sa pamamagitan ng continuous trading para mapababa ang costs, ang 50% recovery ay sapat na para makabreak-even, at ang pagbalik sa original opening price ay maaaring magyield ng substantial profits. Kung may one-sided rapid decline, ang active risk control system ay awtomatikong makikilala at agad na titigil ang trading, pinapayagan lang ang patuloy na orders kapag ang market ay nag-recover, nang walang pag-aalala tungkol sa liquidation mula sa price spikes.

Halimbawa: Nagsimula ng trading ng ETH sa 3000 points, bumaba ang presyo sa 2700 points, nawalan ng humigit-kumulang $3,000. Kapag ang presyo ay nag-recover sa itaas ng 2850 points, nakabreak-even na. Pagbalik sa 3000 points, ang profits ay mula $1,000 hanggang $3,000.

## 📜 Pinagmulan ng Proyekto

Ang proyektong ito ay orihinal na binuo batay sa [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker), na inilathala ni [dennisyang1986](https://github.com/dennisyang1986) sa ilalim ng MIT License.

Batay sa orihinal na proyekto, gumawa kami ng sumusunod na major improvements at extensions:

- ✨ **Kumpletong Frontend Interface**: Nagdagdag ng React + TypeScript web management interface na nagbibigay ng visual trading monitoring, configuration management, at data analysis
- 🏦 **Exchange Expansion**: Pinalawak mula sa 3 exchanges (Binance, Bitget, Gate.io) sa orihinal na proyekto patungo sa **20+ major exchanges**
- 🔒 **Financial-Grade Stability**: Komprehensibong pinabuti ang system reliability, kasama ang comprehensive error handling, concurrency safety mechanisms, data consistency guarantees, automatic recovery, atbp.
- 📊 **Enhanced Monitoring**: Pinabuting logging system, metrics collection (Prometheus), health checks, at real-time alerts
- 🛡️ **Strengthened Risk Control**: Multi-layer risk monitoring, automatic reconciliation, anomaly circuit breaking, at fund safety protection
- 🔌 **Plugin System**: Suporta para sa extensible plugin mechanisms para sa madaling customization at secondary development
- 📱 **Internationalization Support**: Multi-language interface (Chinese/English), i18n support
- 🧪 **Testnet Support**: Suporta para sa testnet environments ng multiple exchanges para sa development at testing

Para sa detailed improvement descriptions at third-party software information, pakitingnan ang [NOTICE](../../NOTICE) file.

**Mahalagang Paalala**: Ang proyektong ito ay ngayon ipinamahagi sa ilalim ng **GNU Affero General Public License v3.0 (AGPL-3.0)**. Alinsunod sa MIT License requirements ng orihinal na proyekto, pinanatili namin ang pagkilala sa orihinal na proyekto.

## ✨ Key Features

- **Multi-Exchange Support**: Compatible sa Binance, Bitget, Gate.io, Bybit, EdgeX, at iba pang major platforms.
- **Millisecond-Level Response**: Buong WebSocket-driven (market data at order flow), tinatanggal ang polling delays.
- **Smart Grid Strategy**: 
  - **Fixed Amount Mode**: Mas kontroladong capital utilization.
  - **Super Slot System**: Matalinong namamahala sa order at position states, pumipigil sa concurrency conflicts.
- **Powerful Risk Control System**:
  - **Active Risk Control**: Real-time monitoring ng K-line volume anomalies, awtomatikong nagpapause ng trading.
  - **Fund Safety**: Awtomatikong nagsusuri ng balance, leverage, at maximum position risk bago mag-startup.
  - **Automatic Reconciliation**: Regular na nag-synchronize ng local at exchange states para masiguro ang data consistency.
- **High-Concurrency Architecture**: Mahusay na concurrency model batay sa Goroutine + Channel + Sync.Map.

## 🏦 Supported Exchanges

| Exchange | Status | Daily Trading Volume | Notes |
|----------|--------|---------------------|-------|
| **Binance** | ✅ Stable | $50B+ | Pinakamalaking exchange sa mundo |
| **Bitget** | ✅ Stable | $10B+ | Mainstream futures trading platform |
| **Gate.io** | ✅ Stable | $5B+ | Established exchange |
| **OKX** | ✅ Stable | $20B+ | Top 3 globally, malakas na Chinese user base |
| **Bybit** | ✅ Stable | $15B+ | Mainstream futures trading platform |
| **Huobi (HTX)** | ✅ Stable | $5B+ | Established exchange, malakas na Chinese market |
| **KuCoin** | ✅ Stable | $3B+ | Maraming altcoins, futures contract support |
| **Kraken** | ✅ Stable | $2B+ | Malakas na compliance, mainstream sa Europe at America |
| **Bitfinex** | ✅ Stable | $1B+ | Established exchange, magandang liquidity |
| **MEXC** | ✅ Stable | $8B+ | Malaking futures trading volume, maraming altcoins, may testnet support |
| **BingX** | ✅ Stable | $3B+ | Social trading platform, magandang futures experience, may testnet support |
| **Deribit** | ✅ Stable | $2B+ | Pinakamalaking options exchange sa mundo, suporta sa futures + options, may testnet support |
| **BitMEX** | ✅ Stable | $2B+ | Established derivatives exchange, hanggang 100x leverage, may testnet support |
| **Phemex** | ✅ Stable | $2B+ | Zero-fee futures trading, high-performance engine, may testnet support |
| **WOO X** | ✅ Stable | $1.5B+ | Institutional-grade exchange, malalim na liquidity, may testnet support |
| **CoinEx** | ✅ Stable | $1B+ | Established exchange (2017), maraming altcoins, may testnet support |
| **Bitrue** | ✅ Stable | $1B+ | Main XRP ecosystem exchange, malakas na Southeast Asian market, may testnet support |
| **XT.COM** | ✅ Stable | $800M+ | Emerging exchange, maraming altcoins, may testnet support |
| **BTCC** | ✅ Stable | $500M+ | Established exchange (2011), unang Bitcoin exchange sa China, may testnet support |
| **AscendEX** | ✅ Stable | $400M+ | Institutional-grade exchange, DeFi-friendly, may testnet support |
| **Poloniex** | ✅ Stable | $300M+ | Established exchange (2014), maraming coin variety, may testnet support |
| **Crypto.com** | ✅ Stable | $500M+ | Kilalang brand, tens of millions ng users globally, may testnet support |

## Module Architecture

```
quantmesh_platform/
├── main.go                    # Main program entry, component orchestration
│
├── config/                    # Configuration management
│   └── config.go              # YAML configuration loading at validation
│
├── exchange/                  # Exchange abstraction layer (core)
│   ├── interface.go           # IExchange unified interface
│   ├── factory.go             # Factory pattern para sa paggawa ng exchange instances
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
├── safety/                    # Safety at risk control
│   ├── safety.go              # Pre-startup safety checks
│   ├── risk_monitor.go        # Active risk control (K-line monitoring)
│   ├── reconciler.go          # Position reconciliation
│   └── order_cleaner.go       # Order cleanup
│
└── utils/                     # Utility functions
    └── orderid.go             # Custom order ID generation
```

## Best Practices

1. **Para sa Exchange VIP Status**: Ang sistemang ito ay volume generation tool. Kung ang price fluctuations ay hindi malaki, ang $3,000 na margin ay maaaring mag-generate ng $10 milyon na trading volume sa loob ng 2-3 araw.

2. **Best Practice para sa Profit**: Pumasok sa market pagkatapos ng isang round ng pagbaba. Una, bumili ng position, pagkatapos simulan ang software. Awtomatiko itong magbebenta ng grid by grid pataas. Kapag naubos na ang iyong position, itigil ang system. Kung hindi ka sigurado kung ang current market ay low point, maaari kang magsimula nang walang base position. Kung bumaba pa, magdagdag ng position sa low point at i-restart para magpatuloy sa pagbebenta. Pinapamaximize nito ang profits. Ulitin ang cycle na ito para sa patuloy na profit. Huwag mag-alala tungkol sa pagbaba - patuloy na binababa ng program ang costs. Hangga't nag-recover ito ng kalahati, nakabreak-even na.

## 🚀 Getting Started

### Prerequisites
- Go 1.21 o mas mataas
- Network environment na kayang ma-access ang exchange APIs

### Installation

1. **I-clone ang repository**
   ```bash
   git clone https://github.com/ghostsworm/quantmesh.git
   cd quantmesh
   ```

2. **I-install ang dependencies**
   ```bash
   go mod download
   ```

### Configuration

> **Runtime SSOT:** The primary database table `app_config` holds the authoritative configuration. One-time YAML import: `./quantmesh --migrate-app-config` (with `QUANTMESH_IMPORT_YAML`, or `config.yaml` in the working directory), or run `./quantmesh /path/to/file.yaml` as the first argument. See [`docs/config-database-design.md`](../config-database-design.md).

1. Kopyahin ang example configuration file:
   ```bash
   cp docs/config/examples/config.example.yaml config.yaml
   ```

2. I-edit ang `config.yaml` at punan ang iyong API Key at strategy parameters:

   ```yaml
   app:
     current_exchange: "binance"  # Pumili ng exchange

   exchanges:
     binance:
       api_key: "YOUR_API_KEY"
       secret_key: "YOUR_SECRET_KEY"
       fee_rate: 0.0002

   trading:
     symbol: "ETHUSDT"       # Trading pair
     price_interval: 2       # Grid spacing (presyo)
     order_quantity: 30     # Halaga kada grid (USDT)
     buy_window_size: 10    # Bilang ng buy orders
     sell_window_size: 10   # Bilang ng sell orders
   ```

### Usage

#### Production Mode

Patakbuhin ang compiled binary:

```bash
go run main.go
```

O i-build at patakbuhin:

```bash
go build -o quantmesh
./quantmesh
```

Ang backend ay magse-serve ng frontend static files sa port 28888 (default).

#### Development Mode

Para sa frontend development na may hot reload at source code debugging:

**Option 1: Gamitin ang development script (Inirerekomenda)**

```bash
./scripts/local/dev.sh
```

Ang script na ito ay:
- Magse-start ng Go backend server sa port 28888
- Magse-start ng Vite dev server sa port 15173
- Mag-e-enable ng hot reload para sa frontend code changes
- Magpo-provide ng source maps para sa debugging (walang minified code)

Pagkatapos, ma-access ang application sa: **http://localhost:15173**

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

Pagkatapos, ma-access ang application sa: **http://localhost:15173**

**Development Mode Benefits:**
- ✅ Hot reload - Ang frontend code changes ay agad na na-re-reflect
- ✅ Source maps - I-debug gamit ang original TypeScript/React code (hindi minified)
- ✅ Fast refresh - Ang React components ay na-update nang hindi nawawala ang state
- ✅ Mas magandang error messages - Makita ang actual file names at line numbers

**Note:** Sa development mode, ang Vite dev server ay nagpo-proxy ng API requests (`/api/*`) at WebSocket connections (`/ws`) sa Go backend na tumatakbo sa port 28888.

## 🏗️ Architecture

Ang system ay gumagamit ng modular design na may core components kasama ang:

- **Exchange Layer**: Unified exchange interface abstraction, nagtatago sa underlying API differences.
- **Price Monitor**: Global unique WebSocket price source, sinisiguro ang decision consistency.
- **Super Position Manager**: Core position manager, namamahala sa order lifecycle batay sa Slot mechanism.
- **Safety & Risk Control**: Multi-layer risk control, kasama ang startup checks, runtime monitoring, at anomaly circuit breaking.

Para sa mas detalyadong architecture documentation, pakitingnan ang [ARCHITECTURE.md](../ARCHITECTURE.md).

## 📊 Usage Statistics & Privacy Protection

Ang QuantMesh ay may optional usage statistics feature para makolekta ng anonymous usage data, tumutulong sa amin na maintindihan ang project usage at mapabuti ang product. **Lahat ng data collection ay completely transparent, naa-audit ang code, at maaaring i-disable anumang oras.**

### 🔒 Privacy Protection

**Data na Kinokolekta Namin (Anonymous):**
- ✅ **Basic Information**: Version number, operating system, architecture, instance ID (randomly generated UUID)
- ✅ **Usage Statistics**: Exchange names na ginagamit, trading pairs
- ✅ **Performance Metrics**: API request/response latency, WebSocket latency
- ✅ **Trading Activity**: Trading direction (buy/sell), hindi kasama ang trading amounts

**Data na HINDI Namin Kinokolekta:**
- ❌ **IP Address**: Ang frontend ay may disabled IP capture, ang backend ay gumagamit ng instance ID sa halip na IP
- ❌ **Geolocation**: Walang koleksyon ng latitude/longitude, city, o iba pang location information
- ❌ **Personal Information**: Walang koleksyon ng user IDs, emails, names, o anumang identity information
- ❌ **Sensitive Data**: Walang koleksyon ng API keys, trading amounts, account balances, o position information
- ❌ **Financial Data**: Walang koleksyon ng anumang financial o trading sensitive information

### 🛡️ Privacy Protection Measures

1. **Instance ID Mechanism**: Gumagamit ng randomly generated UUID bilang unique identifier, naka-store sa `./data/instance_id` file, walang personal information
2. **Frontend IP Disabled**: Ang PostHog SDK ay configured na may `ip_capture: false`, nagdi-disable ng IP address capture at geolocation inference
3. **Backend Hindi Nagse-send ng IP**: Ang backend code ay hindi nagse-send ng IP addresses sa statistics service
4. **Completely Optional**: Ang mga users ay maaaring i-disable ang statistics anumang oras sa pamamagitan ng environment variables
5. **Code Transparency**: Lahat ng statistics code ay naa-audit, matatagpuan sa `utils/telemetry.go`

### ⚙️ Paano I-disable ang Statistics

**Method 1: Environment Variable (Inirerekomenda)**
```bash
export QUANTMESH_DISABLE_TELEMETRY=1
```

**Method 2: Frontend Disable**
Sa `webui/.env.local` file:
```bash
VITE_DISABLE_TELEMETRY=1
```

**Method 3: I-modify ang Code**
I-edit ang `utils/telemetry.go`, i-set ang `Enabled` sa `false`

### 📖 Detailed Documentation

Para sa mas detalyadong impormasyon tungkol sa statistics feature, pakitingnan ang:
- 📖 [Kumpletong Statistics Guide](../../docs/TELEMETRY_GUIDE.md)
- 🔒 [Privacy Protection Guide](../../docs/TELEMETRY_PRIVACY.md)
- 🚀 [Quick Setup Guide](../../docs/TELEMETRY_SIMPLE_GUIDE.md)

---

## ⚠️ Disclaimer

Ang software na ito ay para lamang sa educational at research purposes. Ang cryptocurrency trading ay may mataas na risk at maaaring magresulta sa capital loss.
- Ang mga users ay solely responsible para sa anumang profits o losses mula sa paggamit ng software na ito.
- Laging i-test nang mabuti sa Testnet bago gamitin ang real funds.
- Ang mga developers ay hindi liable para sa losses dahil sa software bugs, network latency, o exchange failures.

## 🪙 Crypto Payment Support

Ang QuantMesh ay sumusuporta sa cryptocurrency payments para sa subscriptions at licenses:

### Supported Cryptocurrencies
- **BTC** (Bitcoin)
- **ETH** (Ethereum)
- **USDT** (Tether, ERC20)
- **USDC** (USD Coin, ERC20)

### Payment Methods
1. **Coinbase Commerce** (Inirerekomenda)
   - Awtomatikong confirmation
   - Maraming cryptocurrencies na suportado
   - Madaling payment page

2. **Direct Wallet Payment**
   - Walang third-party involvement
   - Mas maraming privacy
   - Manual confirmation (1-24 oras)

### Quick Start
```bash
# Method A: Coinbase Commerce (15 minuto)
# 1. Mag-register sa https://commerce.coinbase.com
# 2. I-configure ang API Key sa .env.crypto
# 3. Simulan ang service

# Method B: Direct Wallet (5 minuto)
# 1. I-configure ang wallet addresses
# 2. Simulan ang service
# 3. Manual confirmation
```

### Documentation
- 📖 [User Payment Guide](../CRYPTO_PAYMENT_GUIDE.md)
- 🚀 [Quick Start Guide](../CRYPTO_PAYMENT_QUICKSTART.md)
- 🔧 [Setup Guide](../CRYPTO_PAYMENT_SETUP.md)
- 📊 [Implementation Summary](../reports/CRYPTO_PAYMENT_SUMMARY.md)

### Bakit Crypto Payments?
✅ Hindi kailangan ng credit card o bank account  
✅ Global accessibility, walang regional restrictions  
✅ Mas mababang transaction fees (1% vs 2.9%)  
✅ Mas magandang privacy protection  
✅ Mabilis na confirmation (10-30 minuto)  
✅ Perpektong fit para sa crypto trading software  

## 📜 License

Ang proyektong ito ay gumagamit ng **Dual License model**:

### AGPL-3.0 Open Source License
- ✅ Libreng gamitin, i-modify, at i-distribute
- ⚠️ **Lahat ng derivative works ay dapat open-sourced** at i-release sa ilalim ng AGPL-3.0
- ⚠️ Ang source code ay dapat ibigay kahit para sa network services
- ⚠️ Ang modified code ay dapat i-contribute pabalik sa community

### Commercial License
Kung kailangan mong gamitin ang software na ito sa proprietary applications o services, o ayaw mong i-open-source ang iyong modifications, kailangan mong bumili ng commercial license.

**Commercial License Scope:**
- Gamitin sa proprietary applications
- Walang obligasyon na i-open-source ang modifications
- I-integrate sa proprietary products para sa distribution
- Priority technical support at updates

**Commercial License Inquiries:**
- 📧 Email: contact@quantmesh.io
- 🌐 Website: https://quantmesh.io/commercial

---

### License Details

Ang proyektong ito ay dual-licensed sa ilalim ng:

1. **AGPL-3.0 (GNU Affero General Public License v3.0)**
   - Libre para sa paggamit, modification, at distribution
   - Lahat ng derivative works ay dapat open-sourced sa ilalim ng AGPL-3.0
   - Ang source code ay dapat ibigay sa lahat ng users, kahit para sa network services
   - Ang modifications ay dapat i-contribute pabalik sa community

2. **Commercial License**
   - Kailangan para sa proprietary use
   - Walang obligasyon na i-open-source ang modifications
   - Kasama ang priority support at updates

Para sa commercial licensing inquiries, pakikontak:
- 📧 Email: contact@quantmesh.io
- 🌐 Website: https://quantmesh.io/commercial

## 🤝 Contributing

Tinatanggap namin ang contributions! Narito kung paano ka makakatulong:

- ⭐ **I-star ang repo na ito** kung nakakatulong ito
- 🍴 **I-fork at gamitin** ang proyekto
- 🐛 **I-report ang bugs** sa pamamagitan ng [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💡 **Mag-suggest ng features** sa pamamagitan ng [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📝 **Mag-submit ng PRs** para sa improvements
- 📖 **Mapabuti ang documentation**

**Note:** Ayon sa AGPL-3.0 license, lahat ng contributions sa proyektong ito ay i-release sa ilalim ng parehong AGPL-3.0 license.

Tingnan ang [CONTRIBUTING.md](../CONTRIBUTING.md) para sa detailed guidelines.

## 🙏 Acknowledgments

Salamat sa orihinal na proyekto [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker) ni [dennisyang1986](https://github.com/dennisyang1986) para sa kanilang open-source contribution, na nagbigay ng solid foundation para sa proyektong ito. Para sa mas maraming impormasyon, pakitingnan ang [NOTICE](../../NOTICE) file.

---

## 📞 Contact & Support

- 🌐 **Website**: https://quantmesh.io
- 📧 **Email**: contact@quantmesh.io
- 💬 **Discord**: [Sumali sa aming community](https://discord.gg/YOUR_INVITE_LINK)
- 🐛 **Issues**: [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💬 **Discussions**: [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📖 **Documentation**: [Full Documentation](../)

---

<div align="center">
  <strong>Ginawa nang may ❤️ ng QuantMesh Team</strong><br/>
  <sub>Kung nakakatulong ang proyektong ito, pakiconsider na bigyan ito ng ⭐</sub>
</div>

Copyright © 2025 QuantMesh Team. Lahat ng Karapatan ay Nakalaan.

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
