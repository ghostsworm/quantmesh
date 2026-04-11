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

## What this is

A Go market-making daemon: market data and orders go over WebSocket where possible; strategy logic and execution stay in one process so you skip an extra “poll with a script” layer—less latency and fewer split-brain bugs. Most people run long-only infinite grid on perpetuals first, but the repo also ships DCA, martingale, mean reversion, momentum, trend, and combo strategies (parallel); indicators and backtests live here too, so you can try parameters on history before leaning on live markets.

Compared to exchange-built grids or random closed-source bots, you get a real multi-exchange wrapper, backtests, active risk controls and reconciliation, Prometheus/Grafana, and a React console. The license is AGPL—you can read and change the code. The team claims **$100M+** cumulative live notional over time; treat that as a story, not a return guarantee. Concrete examples and risk notes are below.

## Rough capabilities (not a feature matrix)

Roughly twenty venues are wired up; Binance, Bitget, Gate, OKX, Bybit, Deribit, and others live under `exchange/`—spot vs perp depends on the adapter. Grids can get quite deep: super slots, stop/take-profit, trailing, max layers, trend filters, soft price bands, trigger price, arithmetic/geometric spacing, shifting the whole ladder, flatten on exit; plus P1 funding-rate hooks and P2 order-book-aware quoting.

Risk is more than a single stop: K-line anomalies, startup balance/leverage checks, periodic reconciliation, order cleanup; options hedging (Put/Call, coverage, rolls) is partially integrated. Monitoring: Prometheus, Grafana, alerts, Watchdog. The event hub records price moves and trade-related events; news and external feeds are optional—your core path does not depend on them.

There is an `ai/` package for summaries, tuning hints, and risk-style helpers—**execution does not depend on it**; leave it off or ignore it.

## Numbers and examples (still not investment advice)

Example: Binance ETHUSDC, zero fees, ~$1 spacing, ~$300 per clip can reach millions of dollars per day in notional—highly dependent on market and settings. ETH from 3000 to 2700 might show a few thousand USDT in paper pain; back toward ~2850 can get near breakeven; at 3000, P&L depends on fees and grid settings. **Before real money: testnet and small size.**

## Layout (quick scan)

```
quantmesh_platform/
├── main.go
├── config/                 # YAML, hot reload, history
├── exchange/               # venue adapters
├── strategy/               # grid, DCA, martingale, mean reversion, momentum, trend, combo
├── indicators/
├── ai/                     # optional helpers, not the main trading path
├── backtest/
├── position/               # super slots, etc.
├── safety/                 # startup checks, risk, reconciliation, order cleanup, funding
├── monitor/  event/  metrics/  plugin/
├── web/                    # API + static assets
└── webui/                  # React
```

More detail: [ARCHITECTURE.md](../../ARCHITECTURE.md), [GRID_STRATEGY_ADVANCED_FEATURES.md](../GRID_STRATEGY_ADVANCED_FEATURES.md), [RISK_CONTROL_GUIDE.md](../RISK_CONTROL_GUIDE.md), [API_REFERENCE.md](../API_REFERENCE.md).

## Run it

### Docker

```bash
git clone https://github.com/ghostsworm/quantmesh.git
cd quantmesh
cp docs/config/examples/config.example.yaml my-config.yaml
# edit my-config.yaml — API keys and strategy
# first import into the primary DB:
# ./quantmesh --migrate-app-config my-config.yaml
# see ../config-database-design.md — authoritative config is app_config
docker-compose up -d
```

Open http://localhost:8080 in the browser. Stop: `docker-compose down`.

(If your compose file maps ports differently, adjust; the app’s default HTTP listen is often **28888** unless overridden.)

### From source

Go 1.21+ and reachable exchange APIs.

```bash
git clone https://github.com/ghostsworm/quantmesh.git
cd quantmesh
go mod download
cp docs/config/examples/config.example.yaml my-config.yaml
# after editing: ./quantmesh --migrate-app-config my-config.yaml
go run main.go
# or: go build -o quantmesh && ./quantmesh
```

By default the backend serves the embedded UI on **28888**. For dev, run `./scripts/local/dev.sh` or `go run main.go` plus `cd webui && yarn dev` (Vite defaults to **15173**).

Sample config snippet:

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

**Config note:** The canonical trading config lives in the primary DB table **`app_config`**. YAML is for import and migration; see [config-database-design.md](../config-database-design.md).

## Architecture in four bullets

- **Exchange**: one interface, venue-specific quirks hidden behind it.  
- **Price monitor**: a single WebSocket price path so decisions do not disagree.  
- **Super position manager**: slots own order lifecycles.  
- **Safety**: checks at startup, monitoring while running, circuit breaking when needed.

Full write-up: [ARCHITECTURE.md](../../ARCHITECTURE.md).

## Telemetry (optional)

Anonymous stats may be sent (version, OS, exchange name, symbols, latency, etc.). **We do not** collect IP, balances, API keys, or trade sizes. Disable:

```bash
export QUANTMESH_DISABLE_TELEMETRY=1
```

Or set `VITE_DISABLE_TELEMETRY=1` in `webui/.env.local`, or edit `utils/telemetry.go`. Long form: [TELEMETRY_GUIDE.md](../TELEMETRY_GUIDE.md), [TELEMETRY_PRIVACY.md](../TELEMETRY_PRIVACY.md).

## Disclaimer

For learning and research. Crypto trading can wipe an account; you own the P&L. Prove things out on testnet before mainnet. The authors are not liable for bugs, network delay, or exchange outages.

## Crypto payments (subscription / license)

BTC, ETH, USDT (ERC20), USDC (ERC20). Coinbase Commerce or direct on-chain (slower confirms).  
Docs: [CRYPTO_PAYMENT_GUIDE.md](../CRYPTO_PAYMENT_GUIDE.md), [CRYPTO_PAYMENT_QUICKSTART.md](../CRYPTO_PAYMENT_QUICKSTART.md).

## License

Dual license: **AGPL-3.0** (derivatives must stay AGPL; network use counts—offer source) or a **commercial license** for proprietary integration.  
Contact: contact@quantmesh.io · https://quantmesh.io/commercial

## Contributing and contact

Issues and PRs welcome; contributions are AGPL-3.0. See [CONTRIBUTING.md](../../CONTRIBUTING.md).

- Website: https://quantmesh.io  
- [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)  
- [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)  
- Docs: [docs/](../)

---

<div align="center">
  QuantMesh Team · <sub>Version 3.79.12</sub>
</div>

Copyright © 2026 QuantMesh Team. All Rights Reserved.

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
