<div align="center">
  <img src="logo/qm_thick_tail_white.svg" alt="QuantMesh Logo" width="200"/>
  
  # QuantMesh Market Maker
  
  **毫秒级高频加密货币做市商系统**
  
  <h3>⭐ 如果这个项目对您有帮助，请给个 Star 支持一下！</h3>
  <p>
    <a href="https://github.com/ghostsworm/quantmesh">
      <img src="https://img.shields.io/github/stars/ghostsworm/quantmesh?style=social" alt="GitHub Stars">
    </a>
  </p>

  [![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org/dl/)
  [![License](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](LICENSE)
  [![GitHub Stars](https://img.shields.io/github/stars/ghostsworm/quantmesh.svg?style=social&label=Stars)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Forks](https://img.shields.io/github/forks/ghostsworm/quantmesh.svg?style=social&label=Forks)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Issues](https://img.shields.io/github/issues/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/issues)
  [![GitHub Release](https://img.shields.io/github/release/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/releases)
  [![Website](https://img.shields.io/badge/Website-quantmesh.io-green.svg)](https://quantmesh.io)
  
  [简体中文](README.md) | [繁体中文](docs/i18n/README.zh-TW.md) | [English](docs/i18n/README.en.md) | [Español](docs/i18n/README.es.md) | [Français](docs/i18n/README.fr.md) | [Português](docs/i18n/README.pt.md) | [Deutsch](docs/i18n/README.de.md) | [日本語](docs/i18n/README.ja.md) | [한국어](docs/i18n/README.ko.md) | [Русский](docs/i18n/README.ru.md) | [العربية](docs/i18n/README.ar.md) | [हिन्दी](docs/i18n/README.hi.md) | [Bahasa Indonesia](docs/i18n/README.id.md) | [Tiếng Việt](docs/i18n/README.vi.md) | [ไทย](docs/i18n/README.th.md) | [Türkçe](docs/i18n/README.tr.md) | [Українська](docs/i18n/README.uk.md) | [فارسی](docs/i18n/README.fa.md) | [Nederlands](docs/i18n/README.nl.md) | [Italiano](docs/i18n/README.it.md) | [বাংলা](docs/i18n/README.bn.md) | [اردو](docs/i18n/README.ur.md) | [Polski](docs/i18n/README.pl.md) | [Tagalog](docs/i18n/README.tl.md)
</div>

---

## 这是什么

用 Go 写的做市程序：行情和下单尽量走 WebSocket，逻辑和下单在同一进程里，少一层脚本轮询就少一层延迟和状态不同步。默认大家用得最多的是永续上的单向做多无限网格，但仓库里还有 DCA、马丁、均值回归、动量、趋势、组合策略，可以并行跑；指标和回测也在同一代码库里，改参数之前可以先在历史数据上试。

和「交易所自带网格」或网上那种闭源小工具比，这里多了几样硬东西：多家交易所的统一封装、回测、主动风控和对帐、Prometheus/Grafana、以及一套 React 控制台。代码是 AGPL，能自己改、能查。团队自称实盘累计成交过 **$1 亿+** 量级——听听就好，不构成任何收益承诺；数字举例、风险说明见下文。

## 大致能力（别当采购清单背）

交易所这块覆盖了二十来家，Binance / Bitget / Gate / OKX / Bybit / Deribit 等都在 `exchange/` 里有实现，现货和合约看具体适配情况。策略上网格可以玩得很细：超级槽位、止损止盈、回撤止盈、层数上限、趋势过滤、价格软边界、触发价、等差等比、网格整体平移、终止时全平；还有 P1 资金费率联动、P2 订单簿侧挂单优化之类进阶项。

风控不是只有「设个止损」：会盯 K 线异常、启动前检查余额和杠杆、定期对帐、订单清理；期权对冲（Put/Call、覆盖率、展期）也接了一部分。监控侧有 Prometheus、Grafana、告警、Watchdog。事件中心会记价格波动和成交相关事件；新闻和外部信号可以接，不接也不影响主交易链路。

仓库里还有一块 `ai/`，做的是摘要、参数建议、风险类辅助——**下单不依赖它**，关掉或不用都行。

## 数字与示例（仍不是投资建议）

举例：币安 ETHUSDC、零手续费、间隔约 1 美元、单笔约 300 USDT 时，日成交量可以到数百万美元这个量级——取决于行情和参数。ETH 从 3000 跌到 2700，浮亏可能到几千 USDT 这个数量级；涨回 2850 附近有时接近保本，回到 3000 则要看手续费和网格参数。**上真金白银之前：测试网和小资金先跑熟。**

## 目录结构（扫一眼用）

```
quantmesh_platform/
├── main.go
├── config/                 # YAML、热更新、历史
├── exchange/               # 各所适配
├── strategy/               # 网格、DCA、马丁、均值回归、动量、趋势、组合
├── indicators/
├── ai/                     # 可选：辅助分析，非主路径
├── backtest/
├── position/               # 超级槽位等
├── safety/                 # 安检、风控、对帐、订单清理、资金费率
├── monitor/  event/  metrics/  plugin/
├── web/                    # API 与静态资源
└── webui/                  # React
```

更细的说明见 [ARCHITECTURE.md](ARCHITECTURE.md)、[docs/GRID_STRATEGY_ADVANCED_FEATURES.md](docs/GRID_STRATEGY_ADVANCED_FEATURES.md)、[docs/RISK_CONTROL_GUIDE.md](docs/RISK_CONTROL_GUIDE.md)、[docs/API_REFERENCE.md](docs/API_REFERENCE.md)。

## 跑起来

### Docker

```bash
git clone https://github.com/ghostsworm/quantmesh.git
cd quantmesh
cp config.example.yaml my-config.yaml
# 编辑 my-config.yaml，填 API 与策略
# 首次把配置写入主库：
# ./quantmesh --migrate-app-config my-config.yaml
# 详见 docs/config-database-design.md，主配置权威在 app_config
docker-compose up -d
```

浏览器打开 http://localhost:8080 。停：`docker-compose down`。

### 源码

需要 Go 1.21+，能访问交易所 API。

```bash
git clone https://github.com/ghostsworm/quantmesh.git
cd quantmesh
go mod download
cp config.example.yaml my-config.yaml
# 编辑后：./quantmesh --migrate-app-config my-config.yaml
go run main.go
# 或 go build -o quantmesh && ./quantmesh
```

默认后端在 **28888** 提供前端静态文件。开发时用 `./dev.sh` 或分别起 `go run main.go` 与 `cd webui && yarn dev`（Vite 默认 **15173**）。

配置片段示例：

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

## 架构就四句

- Exchange：统一接口，屏蔽各所 API 差异。  
- Price Monitor：全局 WebSocket 价格源，避免多处各读各的。  
- Super Position Manager：槽位管订单生命周期。  
- Safety：启动检查、运行中监控、该熔断时熔断。

详情见 [ARCHITECTURE.md](ARCHITECTURE.md)。

## 使用统计（可选）

默认会发匿名统计（版本、系统、交易所名、交易对、延迟等），**不收集** IP、余额、API Key、成交金额。关掉：

```bash
export QUANTMESH_DISABLE_TELEMETRY=1
```

或 `webui/.env.local` 里 `VITE_DISABLE_TELEMETRY=1`，或改 `utils/telemetry.go`。长说明见 [docs/TELEMETRY_GUIDE.md](docs/TELEMETRY_GUIDE.md)、[docs/TELEMETRY_PRIVACY.md](docs/TELEMETRY_PRIVACY.md)。

## 免责声明

软件仅供学习与研究。加密货币交易可能亏光本金；盈亏自负；实盘前务必先在测试网充分验证。开发者不对程序 bug、网络延迟、交易所故障等造成的损失负责。

## 加密货币支付（订阅/授权）

支持 BTC、ETH、USDT(ERC20)、USDC(ERC20)。可走 Coinbase Commerce，或直接链上转账（确认慢一些）。  
文档：[docs/CRYPTO_PAYMENT_GUIDE.md](docs/CRYPTO_PAYMENT_GUIDE.md)、[docs/CRYPTO_PAYMENT_QUICKSTART.md](docs/CRYPTO_PAYMENT_QUICKSTART.md)。

## 授权

双授权：**AGPL-3.0**（衍生作品需同样开源；网络服务也要提供源码）或购买**商业授权**（闭源集成、商用等）。  
联系：contact@quantmesh.io · https://quantmesh.io/commercial

## 贡献与联络

Issue / PR 欢迎；贡献同样以 AGPL-3.0 发布。见 [CONTRIBUTING.md](CONTRIBUTING.md)。

- 官网：https://quantmesh.io  
- 讨论： [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)  
- 问题： [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)  
- 文档目录：[docs/](docs/)

---

<div align="center">
  QuantMesh Team · <sub>Version 3.79.12</sub>
</div>

Copyright © 2026 QuantMesh Team. All Rights Reserved.

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
