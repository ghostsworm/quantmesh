<div align="center">
  <img src="logo/qm_thick_tail_white.svg" alt="QuantMesh Logo" width="200"/>
  
  # QuantMesh Market Maker
  
  **面向实盘与研究的加密货币做市与网格系统 —— 开源可审计，控制台一站式管理**
  
  <p>
    <a href="https://github.com/ghostsworm/quantmesh"><strong>若对你有用，请点右上角 Star ⭐</strong></a><br/>
    <sub>Star 后可在 GitHub 首页「Following」里收到发版与重要更新动态，也方便我们判断项目是否值得持续投入。</sub>
  </p>
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

## 为什么选择 QuantMesh（你能得到什么）

| 你关心的 | QuantMesh 的做法 |
|----------|------------------|
| **延迟与一致性** | 行情与下单优先走 WebSocket，策略与下单同进程，减少轮询带来的延迟和状态漂移。 |
| **不止「交易所自带网格」** | 多策略（网格、DCA、马丁、均值回归、动量、趋势、组合等）、可并行；同一套代码里带指标与回测，改参数前可先跑历史。 |
| **风控与可观测性** | 启动检查、运行监控、对帐、订单清理、资金费率等；Prometheus / Grafana、告警、事件中心。 |
| **可审计** | **AGPL-3.0** 开源，可自行改逻辑、查实现；可选 AI 辅助模块，**不接管下单主路径**。 |
| **运维面** | React Web 控制台；Docker 一键起服务。 |

团队曾公开提及实盘累计成交约 **$1 亿+** 量级——**仅为背景信息，不构成收益承诺**，详见下文风险与示例。

## 适合谁 · 不适合谁

- **更适合**：希望在自有机房或 VPS 上运行做市/网格、需要可定制策略与风控、愿意阅读配置与日志的交易者与量化小团队。  
- **请谨慎**：期望「安装即躺赚」、不愿承担加密资产波动与杠杆风险、或无法接受 AGPL 开源义务的用户——建议先使用测试网与小资金。

## 快速开始

### Docker（推荐先跑起来）

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

浏览器访问 http://localhost:8080 。停止：`docker-compose down`。

### 源码运行

需要 Go 1.21+，并能访问交易所 API。

```bash
git clone https://github.com/ghostsworm/quantmesh.git
cd quantmesh
go mod download
cp config.example.yaml my-config.yaml
# 编辑后：./quantmesh --migrate-app-config my-config.yaml
go run main.go
# 或 go build -o quantmesh && ./quantmesh
```

默认后端在 **28888** 嵌入前端静态资源。本地开发可执行 `./scripts/local/dev.sh`，或分别运行 `go run main.go` 与 `cd webui && yarn dev`（Vite 默认 **15173**）。  
常用启停脚本已放在 [`scripts/local/`](scripts/local/)，避免仓库根目录杂乱、便于在 GitHub 首页先看到本说明。

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

## 核心能力一览

- **交易所**：二十余家适配（Binance、Bitget、Gate、OKX、Bybit、Deribit 等，见 `exchange/`），现货/合约以具体适配为准。  
- **网格与进阶**：超级槽位、止损止盈、回撤止盈、层数上限、趋势过滤、触发价、等差/等比、整体平移、终止全平等；另有资金费率联动、订单簿侧挂单优化等。  
- **风控**：K 线异常、余额与杠杆检查、定期对帐、订单清理；部分期权对冲能力。  
- **可选项**：`ai/` 提供摘要与建议类辅助，**关闭不影响主交易链路**。

更细的说明见 [ARCHITECTURE.md](ARCHITECTURE.md)、[docs/GRID_STRATEGY_ADVANCED_FEATURES.md](docs/GRID_STRATEGY_ADVANCED_FEATURES.md)、[docs/RISK_CONTROL_GUIDE.md](docs/RISK_CONTROL_GUIDE.md)、[docs/API_REFERENCE.md](docs/API_REFERENCE.md)。

## 数字与示例（仍不是投资建议）

举例：币安 ETHUSDC、零手续费、间隔约 1 美元、单笔约 300 USDT 时，日成交量可到数百万美元量级——取决于行情与参数。ETH 从 3000 跌到 2700，浮亏可能达数千 USDT；涨回 2850 附近有时接近保本，回到 3000 仍取决于手续费与网格参数。**真金白银前务必使用测试网与小资金验证。**

## 架构要点（四句话）

- **Exchange**：统一接口，屏蔽各所 API 差异。  
- **Price Monitor**：全局 WebSocket 价格源。  
- **Super Position Manager**：槽位管理订单生命周期。  
- **Safety**：启动检查、运行监控、该熔断时熔断。

详情见 [ARCHITECTURE.md](ARCHITECTURE.md)。

## 仓库目录（速览）

```
quantmesh_platform/
├── main.go
├── config/          # YAML、热更新、历史
├── exchange/        # 各所适配
├── strategy/        # 多类策略
├── indicators/      backtest/
├── ai/              # 可选辅助
├── position/        safety/  monitor/  web/  webui/
```

## 使用统计（可选）

默认发送匿名统计（版本、系统、交易所名、交易对等），**不收集** IP、余额、API Key、成交金额。关闭方式：

```bash
export QUANTMESH_DISABLE_TELEMETRY=1
```

或在 `webui/.env.local` 中设置 `VITE_DISABLE_TELEMETRY=1`。详见 [docs/TELEMETRY_GUIDE.md](docs/TELEMETRY_GUIDE.md)、[docs/TELEMETRY_PRIVACY.md](docs/TELEMETRY_PRIVACY.md)。

## 免责声明

本软件仅供学习与研究。加密货币交易可能导致本金全部损失；盈亏自负；实盘前请在测试网充分验证。开发者不对程序缺陷、网络延迟、交易所故障等造成的损失承担责任。

## 加密货币支付（订阅/授权）

支持 BTC、ETH、USDT(ERC20)、USDC(ERC20) 等；可走 Coinbase Commerce 或链上转账。  
文档：[docs/CRYPTO_PAYMENT_GUIDE.md](docs/CRYPTO_PAYMENT_GUIDE.md)、[docs/CRYPTO_PAYMENT_QUICKSTART.md](docs/CRYPTO_PAYMENT_QUICKSTART.md)。

## 授权

双授权：**AGPL-3.0**（衍生作品需同样开源；网络服务亦需提供源码）或购买**商业授权**（闭源集成、商用等）。  
联系：contact@quantmesh.io · https://quantmesh.io/commercial

## 贡献与联络

欢迎 Issue / PR；贡献同样以 AGPL-3.0 发布。见 [CONTRIBUTING.md](CONTRIBUTING.md)。

- 官网：https://quantmesh.io  
- 讨论：[GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)  
- 问题：[GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)  
- 文档：[docs/](docs/)

<p align="center">
  <a href="https://github.com/ghostsworm/quantmesh"><strong>觉得有用就 Star ⭐ 一下，感谢支持</strong></a>
</p>

---

<div align="center">
  QuantMesh Team · <sub>Version 3.90.0-rc2</sub>
</div>

Copyright © 2026 QuantMesh Team. All Rights Reserved.

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
