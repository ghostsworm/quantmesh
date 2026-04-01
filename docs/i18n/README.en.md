<div align="center">
  <img src="../../logo/qm_thick_tail_white.svg" alt="QuantMesh Logo" width="200"/>
  
  # QuantMesh Market Maker
  
  **Millisecond-level high-frequency cryptocurrency market maker**
  
  <h3>⭐ If this project helps you, a Star is much appreciated!</h3>
  <p>
    <a href="https://github.com/ghostsworm/quantmesh">
      <img src="https://img.shields.io/github/stars/ghostsworm/quantmesh?style=social" alt="GitHub Stars">
    </a>
  </p>

  [![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org/dl/)
  [![License](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](../../LICENSE)
  [![GitHub Stars](https://img.shields.io/github/stars/ghostsworm/quantmesh.svg?style=social&label=Stars)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Forks](https://img.shields.io/github/forks/ghostsworm/quantmesh.svg?style=social&label=Forks)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Issues](https://img.shields.io/github/issues/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/issues)
  [![GitHub Release](https://img.shields.io/github/release/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/releases)
  [![Website](https://img.shields.io/badge/Website-quantmesh.io-green.svg)](https://quantmesh.io)
  
  [简体中文](../../README.md) | [繁体中文](README.zh-TW.md) | [English](README.en.md) | [Español](README.es.md) | [Français](README.fr.md) | [Português](README.pt.md) | [Deutsch](README.de.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Русский](README.ru.md) | [العربية](README.ar.md) | [हिन्दी](README.hi.md) | [Bahasa Indonesia](README.id.md) | [Tiếng Việt](README.vi.md) | [ไทย](README.th.md) | [Türkçe](README.tr.md) | [Українська](README.uk.md) | [فارسی](README.fa.md) | [Nederlands](README.nl.md) | [Italiano](README.it.md) | [বাংলা](README.bn.md) | [اردو](README.ur.md) | [Polski](README.pl.md) | [Tagalog](README.tl.md)
</div>

---

## QuantMesh in three sentences

1. **One pipeline for market making**: Market data and orders flow over WebSocket; decisions and execution live in one Go service—fewer “script polling” layers means less latency and fewer failure modes.  
2. **More than a grid bot**: Grid plus DCA / martingale / mean reversion / momentum / trend / combo strategies in parallel; 50+ indicators and backtesting in the same repo—tune parameters *after* seeing them in history.  
3. **Production-shaped README**: Multi-layer active risk controls, reconciliation, Prometheus/Grafana, and a full React console—for teams that care about volume *and* sleep.

---

## 🎯 How is this different from “typical grid / MM scripts”?

The table below contrasts **common closed-source tools, exchange-built grids, or DIY glue scripts** with QuantMesh, which targets an **auditable open-source market-making platform**, not a single-strategy toy.

| Dimension | QuantMesh | Typical alternatives |
|-----------|-----------|----------------------|
| **Exchange coverage** | 20+ via one abstraction | Often 3–5 venues or single-exchange |
| **Latency profile** | Millisecond-grade; WebSocket-first for data and orders | Often second-level REST polling or semi-manual |
| **Strategy depth** | Advanced grid features + multiple strategy types, parallel, capital allocation | Often one strategy or very few knobs |
| **Backtest & indicators** | 50+ built-in indicators, multi-strategy backtests and reports | Often external tools or none |
| **Risk control** | Active K-line circuit breakers, startup checks, reconciliation, options hedging, etc. | Often simple stops or none |
| **Observability** | Prometheus, Grafana, alerts, Watchdog | Often logs or a bare panel |
| **Web console** | Full React UI (config, bots, monitoring) | None or minimal |
| **Code & license** | AGPL-3.0, auditable, fork-friendly | Closed box or restricted license |
| **Production scale** | $100M+ reported live volume (self-reported) | Often undisclosed or unverifiable |

**In one line:** If you want **multi-exchange, backtests, risk controls, monitoring, and the freedom to change code**—that is the gap QuantMesh fills versus ad-hoc scripts.

---

## 📊 Performance metrics

- **Trading volume**: $100M+ production-tested (reported)
- **Response latency**: &lt;10ms (WebSocket-driven)
- **Supported exchanges**: 20+
- **Throughput**: 1000+ orders/second
- **Availability**: 99.9%+
- **Daily capacity (example)**: $3M+/day (e.g. ETHUSDC-style setups)

---

## 📖 Introduction

**QuantMesh** is a high-performance cryptocurrency market-making system written in **Go**. It drives market data and orders over **WebSocket end-to-end**, unifies **20+ exchanges** (Binance, Bitget, Gate.io, …), and defaults to **long-only infinite grid** on perpetuals—while supporting DCA, martingale, mean reversion, momentum, trend, and combo strategies in parallel.

The team reports **$100M+ cumulative live notional** (for information only; not a promise of returns). Example: on Binance ETHUSDC with zero fees, $1 spacing, ~$300 per clip, daily notional can reach millions of dollars in favorable conditions; in sharp drops, **active risk controls** aim to pause trading until conditions stabilize.

**Illustrative numbers (not investment advice):** ETH from 3000 to 2700 may show ~$3,000 paper loss; recovery toward ~2850 can approach breakeven; at 3000, P&amp;L depends on fees and parameters. Always validate on **testnet** and **small size** first.

## ✨ Key features

> Grouped as **trading → risk → observability → extension**. Skim the **bold** lines first.

- **Multi-exchange**: Binance, Bitget, Gate.io, Bybit, EdgeX, and more; spot and derivatives
- **Millisecond response**: WebSocket for market data and order flow—no polling tax
- **Multi-strategy**:
  - **Grid**: fixed-notional mode, super-slot system; **grid risk** (stop-loss / take-profit / trailing / max layers / trend filter); **price band** (soft limits); **trigger price**; arithmetic/geometric grids; **grid shift**; **close all on stop**; advanced P1 funding-rate linkage, P2 order-book aware quoting
  - **DCA / martingale / mean reversion / momentum / trend / combo**: parallel execution and capital allocation
- **Indicators**: 50+ (trend, momentum, volatility, volume) for signals and backtests
- **AI**: market analysis, parameter suggestions, risk and sentiment (news / Polymarket, etc.)
- **Backtesting**: historical K-line runs, multi-strategy, 20+ risk metrics and Markdown reports
- **Risk & safety**:
  - **Active risk**: K-line volume anomalies → optional trading pause
  - **Fund safety**: balance, leverage, and max-position checks before start
  - **Reconciliation**: periodic sync between local state and the exchange
  - **Options hedge**: long/short grid with Put/Call hedging; positions from Binance/Deribit, coverage and roll suggestions
- **Monitoring**: Prometheus, Grafana, layered alerts, Watchdog
- **Events & news**: price/trade events, AI-assisted news analysis and prediction checks
- **Telemetry (optional)**: anonymous usage stats to improve the product—transparent, reviewable, **disable anytime**
- **Concurrency**: Goroutine + channel + sync.Map patterns for high throughput

## 🏦 Supported exchanges

| Exchange | Status | Daily volume (approx.) | Notes |
|----------|--------|--------------------------|-------|
| **Binance** | ✅ Stable | $50B+ | Largest global venue |
| **Bitget** | ✅ Stable | $10B+ | Major derivatives |
| **Gate.io** | ✅ Stable | $5B+ | Long-running exchange |
| **OKX** | ✅ Stable | $20B+ | Top-tier global |
| **Bybit** | ✅ Stable | $15B+ | Major derivatives |
| **Huobi (HTX)** | ✅ Stable | $5B+ | Strong in Chinese market |
| **KuCoin** | ✅ Stable | $3B+ | Many altcoins, futures |
| **Kraken** | ✅ Stable | $2B+ | Compliance-focused |
| **Bitfinex** | ✅ Stable | $1B+ | Deep liquidity legacy venue |
| **MEXC** | ✅ Stable | $8B+ | High perp volume, testnet |
| **BingX** | ✅ Stable | $3B+ | Social trading, testnet |
| **Deribit** | ✅ Stable | $2B+ | Options leader, testnet |
| **BitMEX** | ✅ Stable | $2B+ | Legacy derivatives, testnet |
| **Phemex** | ✅ Stable | $2B+ | Zero-fee perps, testnet |
| **WOO X** | ✅ Stable | $1.5B+ | Institutional, testnet |
| **CoinEx** | ✅ Stable | $1B+ | Since 2017, testnet |
| **Bitrue** | ✅ Stable | $1B+ | XRP ecosystem, testnet |
| **XT.COM** | ✅ Stable | $800M+ | Emerging, testnet |
| **BTCC** | ✅ Stable | $500M+ | Since 2011, testnet |
| **AscendEX** | ✅ Stable | $400M+ | DeFi-friendly, testnet |
| **Poloniex** | ✅ Stable | $300M+ | Since 2014, testnet |
| **Crypto.com** | ✅ Stable | $500M+ | Large retail base, testnet |

## Module overview

| Module | What it does |
|--------|----------------|
| **Trading strategies** | Grid, DCA, martingale, mean reversion, momentum, trend, combo; multi-symbol spot/perp |
| **Technical analysis** | 50+ indicators; signals and backtests |
| **AI** | Analysis, tuning hints, risk & sentiment, Polymarket-style signals |
| **Backtesting** | Historical K-line, multi-strategy, risk metrics, Markdown reports |
| **Risk & reconciliation** | Active K-line controls, depth monitoring, reconciliation, order cleanup, startup checks, options hedge (coverage, rolls) |
| **Monitoring & alerts** | Prometheus, Grafana, alerts, Watchdog, funding & spread monitors |
| **Events & news** | Event hub, news ingestion, AI analysis, prediction validation |
| **Plugins** | Loadable plugins, licensing hooks, custom strategies and adapters |

See also [ARCHITECTURE.md](../../ARCHITECTURE.md), [GRID_STRATEGY_ADVANCED_FEATURES.md](../GRID_STRATEGY_ADVANCED_FEATURES.md), [RISK_CONTROL_GUIDE.md](../RISK_CONTROL_GUIDE.md), [API_REFERENCE.md](../API_REFERENCE.md).

## Repository layout (high level)

```
quantmesh_platform/
├── main.go                    # Entry, wiring
├── config/                    # YAML load, backup, history, hot reload
├── exchange/                  # IExchange + 20+ venue implementations
├── strategy/                  # grid, DCA, martingale, mean reversion, momentum, trend, combo
├── indicators/                # trend, momentum, volatility, volume
├── ai/                        # analysis, tuning, risk, sentiment
├── backtest/                  # data, engine, metrics
├── position/                  # super position / slot manager (P1/P2)
├── safety/                    # startup checks, risk monitor, reconciler, order cleaner, funding monitor
├── monitor/                   # prices, news, spread, watchdog
├── event/                     # event hub
├── metrics/                   # Prometheus
├── plugin/                    # plugins & license
├── web/                       # API + static assets
└── webui/                     # React source
```

## Best practices

1. **Exchange VIP / volume**: The system can generate large reported volume; with moderate volatility, ~$3,000 margin may produce on the order of $10M notional in a few days (parameters and venue rules apply).

2. **Profit workflow (conceptual)**: After a dip, you may open a base position, start the bot, and let it scale out grid-by-grid upward; stop when flat. If the bottom is unclear, start without a base leg and add at a lower level if needed. **Always match this to your own risk tolerance.**

## 🚀 Getting started

### Option A: Docker (recommended)

1. Clone and copy config:
   ```bash
   git clone https://github.com/ghostsworm/quantmesh.git
   cd quantmesh
   cp config.example.yaml config.yaml
   ```
2. Edit `config.yaml` (API keys, strategy).
3. Run:
   ```bash
   docker-compose up -d
   ```
4. Open **http://localhost:8080** for the Web UI.  
   Stop: `docker-compose down`

### Option B: Build from source

**Requirements:** Go 1.21+, reachable exchange APIs.

```bash
git clone https://github.com/ghostsworm/quantmesh.git
cd quantmesh
go mod download
cp config.example.yaml config.yaml
# edit config.yaml — example:
```

```yaml
app:
  current_exchange: "binance"

exchanges:
  binance:
    api_key: "YOUR_API_KEY"
    secret_key: "YOUR_SECRET_KEY"
    fee_rate: 0.0002

trading:
  symbol: "ETHUSDT"
  price_interval: 2
  order_quantity: 30
  buy_window_size: 10
  sell_window_size: 10
```

**Production:**

```bash
go run main.go
# or
go build -o quantmesh && ./quantmesh
```

Backend serves the embedded UI on port **28888** by default.

**Development (hot reload):**

```bash
./dev.sh
```

Or manually: terminal 1 `go run main.go`, terminal 2:

```bash
cd webui
yarn dev
```

Open **http://localhost:15173** (Vite proxies `/api` and `/ws` to the Go backend).

## 🏗️ Architecture

- **Exchange layer**: One interface, many venues
- **Price monitor**: Single WebSocket price source for consistent decisions
- **Super position manager**: Slot-based order lifecycle
- **Safety & risk**: Startup checks, runtime monitoring, circuit breaking

Details: [ARCHITECTURE.md](../../ARCHITECTURE.md).

## 📊 Telemetry & privacy

QuantMesh can send **optional anonymous usage statistics** to help improve the product. Collection is **transparent**, **reviewable in code**, and **can be disabled**.

### What we may collect (anonymous)

- Version, OS, arch, random instance UUID
- Exchange name and symbol pairs in use
- API latency, WebSocket timing
- Trade **side** only (buy/sell)—not size or P&amp;L

### What we do **not** collect

- IP (disabled on the client; instance ID only)
- Geo or identity
- API keys, balances, positions, or amounts

### Disable telemetry

```bash
export QUANTMESH_DISABLE_TELEMETRY=1
```

Or in `webui/.env.local`: `VITE_DISABLE_TELEMETRY=1`

See [TELEMETRY_GUIDE.md](../TELEMETRY_GUIDE.md), [TELEMETRY_PRIVACY.md](../TELEMETRY_PRIVACY.md), [TELEMETRY_SIMPLE_GUIDE.md](../TELEMETRY_SIMPLE_GUIDE.md).

---

## ⚠️ Disclaimer

This software is for education and research. Crypto trading is risky.

- You are responsible for your own P&amp;L.
- Test on **testnet** before real funds.
- The authors are not liable for bugs, latency, or exchange outages.

## 🪙 Crypto payments (subscriptions / license)

Supported: **BTC**, **ETH**, **USDT (ERC20)**, **USDC (ERC20)** — Coinbase Commerce or direct wallet.

- [CRYPTO_PAYMENT_GUIDE.md](../CRYPTO_PAYMENT_GUIDE.md)
- [CRYPTO_PAYMENT_QUICKSTART.md](../CRYPTO_PAYMENT_QUICKSTART.md)
- [CRYPTO_PAYMENT_SETUP.md](../CRYPTO_PAYMENT_SETUP.md)
- [reports/CRYPTO_PAYMENT_SUMMARY.md](../reports/CRYPTO_PAYMENT_SUMMARY.md)

## 📜 License (dual)

### AGPL-3.0

- Free to use, modify, and distribute
- Derivatives must be AGPL-3.0 and source must be offered (including network use)

### Commercial license

For proprietary use without AGPL obligations: **contact@quantmesh.io** — https://quantmesh.io/commercial

## 🤝 Contributing

Issues and PRs are welcome. Contributions are licensed under AGPL-3.0. See [CONTRIBUTING.md](../../CONTRIBUTING.md).

## 📞 Contact

- Website: https://quantmesh.io  
- Email: contact@quantmesh.io  
- [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)  
- [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)  
- Docs: [docs/](../)

---

<div align="center">
  <strong>Made with ❤️ by QuantMesh Team</strong><br/>
  <sub>If this project helps you, a ⭐ is welcome</sub><br/>
  <sub>Version 3.79.6-rc20</sub>
</div>

Copyright © 2025 QuantMesh Team. All Rights Reserved.

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
