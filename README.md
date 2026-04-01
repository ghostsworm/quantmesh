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

## 三句话看懂 QuantMesh

1. **一条链路上跑做市**：行情与订单走 WebSocket，决策与下单在同一套 Go 服务里完成，少一层「脚本轮询」就少一层延迟与翻车点。  
2. **不是只会画网格**：网格 + DCA / 马丁 / 均值回归 / 动量 / 趋势 / 组合策略可并行，50+ 指标与回测在同一仓库里，改参数前能先「在历史里试一遍」。  
3. **能上线才敢写进 README**：多层主动风控、对帐、Prometheus/Grafana、完整 React 控制台——适合既要刷量也要睡得着的人。

---

## 🎯 与「典型网格 / 做市脚本」差在哪？

下表对比的是：**常见闭源小工具、交易所自带网格、或需要自己拼脚本** 的典型短板；QuantMesh 的定位是**可审计的开源做市平台**，而不是单一策略的玩具。

| 维度 | QuantMesh | 常见其他方案 |
|------|-----------|----------------|
| **交易所覆盖** | 20+ 家统一抽象 | 多限于 3–5 家或单所 |
| **延迟形态** | 毫秒级，行情/订单以 WebSocket 为主 | 多为秒级 REST 轮询或半自动 |
| **策略深度** | 网格进阶能力 + 多类策略可并行、可配资金 | 多为单策略或配置项很少 |
| **回测与指标** | 内建 50+ 指标、多策略回测与报告 | 常需外接工具或没有 |
| **风控** | 主动 K 线熔断、启动安检、对帐、期权对冲等 | 多为简单止损或无 |
| **可观测性** | Prometheus、Grafana、告警、Watchdog | 多为日志或简陋面板 |
| **Web 控制台** | 完整 React UI（配置、Bot、监控） | 无、或仅极简页面 |
| **代码与许可** | AGPL-3.0，可审计、可二次开发 | 闭源/黑盒或授权受限 |
| **实战体量** | $1 亿+ 真实成交验证（自述） | 多数未公开或不可核验 |

**一句话总结：**你要的是「能连多家所、能回测、能风控、能监控、还能自己改」——这正是 QuantMesh 与零散脚本的本质差别。

---

## 📊 效能指标

- **交易量**：$1 亿+ 实战验证
- **回应延迟**：<10ms（WebSocket 驱动）
- **支援交易所**：20+
- **并行处理**：1000+ 单/秒
- **系统可用性**：99.9%+
- **每日交易能力**：$300 万+/天（例：ETHUSDC）

---

## 📖 项目简介

**QuantMesh** 是一套用 Go 写的高性能加密货币做市系统：以 **WebSocket 全链路**驱动行情与订单，面向 Binance、Bitget、Gate.io 等 **20+ 交易所** 提供统一抽象，默认场景是永续合约上的 **单向做多无限网格**——同时支持 DCA、马丁、均值回归、动量、趋势与组合策略并行。

团队自述已在实盘累计成交 **$1 亿+ 级别**（仅供参考，不构成收益承诺）。举例：在币安 ETHUSDC 上零手续费、间隔 1 美元、单笔约 300 USDT 时，日交易量可达数百万美元量级；震荡或上行时靠网格与加仓逻辑获利，急跌时 **主动风控** 会尝试暂停交易，待市场缓和后再恢复。

**数字示例（说明用，非投资建议）：** ETH 自 3000 跌至 2700 可能浮亏约 3000 USDT；若回涨至约 2850 附近可接近保本，回到 3000 则盈亏区间依参数与手续费而异。实盘前请务必在 **测试网** 与 **小资金** 验证。


## ✨ 核心特性

> 下面按「交易 → 风控 → 观测 → 扩展」分层列出；若你只想快速扫一眼，优先看 **粗体** 条目。

- **多交易所支援**：适配 Binance、Bitget、Gate.io、Bybit、EdgeX 等主流平台；支援现货与合约
- **毫秒级回应**：全 WebSocket 驱动（行情与订单流），无轮询延迟
- **多策略支援**：
  - **网格策略**：固定金额模式、超级槽位系统；**网格风控**（止损/止盈/回撤止盈/最大层数/趋势过滤）、**价格范围**（软限制）、**触发价格**、**等差/等比模式**、**网格上移/下移**、**终止时全部平仓**；进阶 P1 资金费率趋势联动、P2 订单簿优化挂单
  - **DCA / 马丁格尔 / 均值回归 / 动量 / 趋势跟踪 / 组合策略**：可并行、可分配资金
- **技术指标库**：50+ 专业指标（趋势、动量、波动率、成交量），供策略与回测使用
- **AI 功能**：市场分析、参数优化、风险评估、情绪分析（新闻 / Polymarket 等）
- **回测系统**：历史 K 线回测、多策略回测、20+ 风险指标与报告
- **强大风控系统**：
  - **主动风控**：即时监控 K 线成交量异常，自动暂停交易
  - **资金安全**：启动前自动检查余额、杠杆与最大持仓风险
  - **自动对帐**：定期同步本地与交易所状态，确保资料一致
  - **期权对冲**：支援做多/做空网格与 Put/Call 期权对冲，从 Binance/Deribit 拉取持仓、计算覆盖率、展期建议
- **完整监控体系**：Prometheus 指标、Grafana 仪表板、多层告警、Watchdog 健康检查
- **事件中心与新闻监控**：价格波动与交易事件记录、AI 新闻分析与预测验证
- **使用统计（可选）**：匿名使用数据收集，帮助改进产品；完全透明、可审查、可禁用
- **高并行架构**：基于 Goroutine + Channel + Sync.Map 的高效并行模型

## 🏦 支援的交易所

| 交易所 | 状态 | 日均交易量 | 备注 |
|--------|------|-----------|------|
| **Binance** | ✅ Stable | $50B+ | 全球最大交易所 |
| **Bitget** | ✅ Stable | $10B+ | 合约交易主流平台 |
| **Gate.io** | ✅ Stable | $5B+ | 老牌交易所 |
| **OKX** | ✅ Stable | $20B+ | 全球前三，中文用户多 |
| **Bybit** | ✅ Stable | $15B+ | 合约交易主流平台 |
| **Huobi (HTX)** | ✅ Stable | $5B+ | 老牌交易所，中文市场强 |
| **KuCoin** | ✅ Stable | $3B+ | 山寨币丰富，期货合约支援 |
| **Kraken** | ✅ Stable | $2B+ | 合规性强，欧美主流 |
| **Bitfinex** | ✅ Stable | $1B+ | 老牌交易所，流动性好 |
| **MEXC（抹茶）** | ✅ Stable | $8B+ | 合约交易量大，山寨币丰富，支援测试网 |
| **BingX** | ✅ Stable | $3B+ | 社交交易平台，合约体验佳，支援测试网 |
| **Deribit** | ✅ Stable | $2B+ | 全球最大期权交易所，支援期货+期权，支援测试网 |
| **BitMEX** | ✅ Stable | $2B+ | 老牌衍生品交易所，最高 100x 杠杆，支援测试网 |
| **Phemex** | ✅ Stable | $2B+ | 零手续费合约，高效能引擎，支援测试网 |
| **WOO X** | ✅ Stable | $1.5B+ | 机构级交易所，深度流动性，支援测试网 |
| **CoinEx** | ✅ Stable | $1B+ | 老牌交易所（2017），山寨币丰富，支援测试网 |
| **Bitrue** | ✅ Stable | $1B+ | XRP 生态主要交易所，东南亚市场强，支援测试网 |
| **XT.COM** | ✅ Stable | $800M+ | 新兴交易所，山寨币丰富，支援测试网 |
| **BTCC** | ✅ Stable | $500M+ | 老牌交易所（2011），中国首家比特币交易所，支援测试网 |
| **AscendEX** | ✅ Stable | $400M+ | 机构级，DeFi 友善，支援测试网 |
| **Poloniex** | ✅ Stable | $300M+ | 老牌交易所（2014），币种丰富，支援测试网 |
| **Crypto.com** | ✅ Stable | $500M+ | 知名品牌，全球数千万用户，支援测试网 |

## 功能模组概览

| 模组 | 说明 |
|------|------|
| **交易策略** | 网格、DCA、马丁格尔、均值回归、动量、趋势跟踪、组合策略；支援多交易对与现货/合约 |
| **技术分析** | 50+ 技术指标（趋势、动量、波动率、成交量）；策略信号与回测 |
| **AI** | 市场分析、参数优化、风险评估、情绪分析、Polymarket 信号 |
| **回测** | 历史 K 线回测、多策略、风险指标与 Markdown 报告 |
| **风控与对帐** | 主动 K 线风控、深度监控、持仓对帐、订单清理、启动前安全检查、期权对冲（Put/Call 覆盖率、展期建议） |
| **监控与告警** | Prometheus、Grafana、多层告警、Watchdog、资金费率与价差监控 |
| **事件与新闻** | 事件中心（价格波动/交易事件）、新闻收集与 AI 分析、预测验证 |
| **外挂与扩展** | 外挂载入、授权验证、自订策略与交易所适配 |

详细说明见 [ARCHITECTURE.md](ARCHITECTURE.md)、[docs/GRID_STRATEGY_ADVANCED_FEATURES.md](docs/GRID_STRATEGY_ADVANCED_FEATURES.md)、[docs/RISK_CONTROL_GUIDE.md](docs/RISK_CONTROL_GUIDE.md)、[docs/API_REFERENCE.md](docs/API_REFERENCE.md)。

## 模组架构

```
quantmesh_platform/
├── main.go                    # 主程序入口，元件编排
│
├── config/                    # 配置管理
│   ├── config.go              # YAML 配置载入与验证
│   ├── backup.go              # 配置备份
│   ├── history.go             # 配置历史
│   └── hot_reload.go          # 配置热更新
│
├── exchange/                  # 交易所抽象层（核心）
│   ├── interface.go           # IExchange 统一介面
│   ├── binance/               # 币安（现货/合约）
│   ├── bitget/                # Bitget 实作
│   ├── gate/                  # Gate.io 实作
│   └── [20+ 交易所实作]
│
├── strategy/                  # 策略模组
│   ├── grid_strategy.go       # 网格策略
│   ├── dca_enhanced.go        # DCA 策略
│   ├── martingale.go          # 马丁格尔
│   ├── mean_reversion.go      # 均值回归
│   ├── momentum.go            # 动量策略
│   ├── trend_following.go     # 趋势跟踪
│   └── combo_strategy.go      # 组合策略
│
├── indicators/                # 技术指标库
│   ├── trend.go               # 趋势指标（MACD、ADX 等）
│   ├── momentum.go            # 动量指标（RSI、Stochastic 等）
│   ├── volatility.go          # 波动率指标（ATR、Bollinger 等）
│   └── volume.go              # 成交量指标
│
├── ai/                        # AI 功能
│   ├── service/               # 市场分析、参数优化、风险与情绪分析
│   └── risk_assessor.go       # AI 风险评估
│
├── backtest/                  # 回测系统
│   ├── data_fetcher.go        # 历史 K 线获取与快取
│   ├── backtester.go          # 回测引擎
│   └── metrics.go             # 风险指标
│
├── position/                  # 仓位管理（核心）
│   └── super_position_manager.go  # 超级槽位管理器（P1/P2 整合）
│
├── safety/                    # 安全与风控
│   ├── safety.go              # 启动前安全检查
│   ├── risk_monitor.go        # 主动风控（K 线监控）
│   ├── reconciler.go          # 持仓对帐
│   ├── order_cleaner.go       # 订单清理
│   └── funding_monitor.go     # 资金费率监控
│
├── monitor/                   # 监控（价格、新闻、价差、Watchdog）
├── event/                     # 事件中心
├── metrics/                   # Prometheus 指标
├── plugin/                    # 外挂载入与授权
├── web/                       # Web API 与前端静态资源
└── webui/                     # React 前端原始码
```

## 最佳实践

1. **刷交易所 VIP**：本系统为刷量工具；若涨跌幅度不大，3000 美元保证金约 2–3 天可刷出 1000 万美元交易量。

2. **获利最佳实践**：在一轮下跌后进场，先买一笔持仓再启动软件，会自动向上一格一格卖出；持仓卖完后停止系统。若不确定是否为低点，可不买底仓启动，若再跌在低点补一笔持仓后重启持续卖出，利润最大化。如此循环持续获利；下跌时程序会持续拉低成本，只要涨回一半即可保本。

## 🚀 快速开始

### 方式一：Docker 一键运行（推荐，最简单）

**只需 3 步：**

1. **克隆仓库并准备配置**
   ```bash
   git clone https://github.com/ghostsworm/quantmesh.git
   cd quantmesh
   cp config.example.yaml config.yaml
   ```

2. **编辑配置**：编辑 `config.yaml`，填入 API Key 与策略参数（见下方配置说明）

3. **启动服务**
   ```bash
   docker-compose up -d
   ```

   访问 **http://localhost:8080** 即可使用 Web UI。

   **停止服务：**
   ```bash
   docker-compose down
   ```

---

### 方式二：从源码编译运行

#### 环境需求
- Go 1.21 或更高
- 网路环境可存取交易所 API

#### 安装

1. **克隆仓库**
   ```bash
   git clone https://github.com/ghostsworm/quantmesh.git
   cd quantmesh
   ```

2. **安装依赖**
   ```bash
   go mod download
   ```

#### 配置

1. 复制范例配置：
   ```bash
   cp config.example.yaml config.yaml
   ```

2. 编辑 `config.yaml`，填入 API Key 与策略参数：

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
     price_interval: 2       # 网格间距（价格）
     order_quantity: 30      # 每格投入金额 (USDT)
     buy_window_size: 10     # 买单挂单数量
     sell_window_size: 10    # 卖单挂单数量
   ```

#### 执行

**正式模式：**

```bash
go run main.go
```

或编译后执行：

```bash
go build -o quantmesh
./quantmesh
```

后端将在 port 28888（预设）提供前端静态档案。

#### 开发模式

若需前端热重载与除错：

**方式一：使用开发脚本（建议）**

```bash
./dev.sh
```

脚本会：启动 Go 后端（port 28888）、启动 Vite 开发伺服器（port 15173）、启用热重载与 source map。  
存取 **http://localhost:15173** 即可。

**方式二：手动启动**

终端 1 - 启动 Go 后端：
```bash
go run main.go
```

终端 2 - 启动 Vite：
```bash
cd webui
yarn dev
```

存取 **http://localhost:15173**。

## 🏗️ 架构

系统采模组化设计，核心元件包含：

- **Exchange Layer**：统一交易所介面抽象，屏蔽底层 API 差异
- **Price Monitor**：全域唯一 WebSocket 价格源，确保决策一致
- **Super Position Manager**：核心仓位管理，基于 Slot 机制管理订单生命周期
- **Safety & Risk Control**：多层风控，含启动检查、执行时监控与异常熔断

更多架构说明请参阅 [ARCHITECTURE.md](ARCHITECTURE.md)。

## 📊 使用统计与隐私保护

QuantMesh 包含一个可选的使用统计功能，用于收集匿名的使用数据，帮助我们了解项目使用情况并改进产品。**所有数据收集都是完全透明的，代码可审查，并且可以随时禁用。**

### 🔒 隐私保护

**我们收集的数据（匿名）：**
- ✅ **基础信息**：版本号、操作系统、架构、实例 ID（随机生成的 UUID）
- ✅ **使用情况**：使用的交易所名称、交易币种对
- ✅ **性能指标**：API 请求/响应耗时、WebSocket 延时
- ✅ **交易活动**：交易方向（买入/卖出），不包含交易金额

**我们不收集的数据：**
- ❌ **IP 地址**：前端已禁用 IP 捕获，后端使用实例 ID 而非 IP
- ❌ **地理位置**：不收集经纬度、城市等位置信息
- ❌ **个人信息**：不收集用户 ID、邮箱、姓名等任何身份信息
- ❌ **敏感数据**：不收集 API 密钥、交易金额、账户余额、持仓信息
- ❌ **财务数据**：不收集任何财务或交易敏感信息

### 🛡️ 隐私保护措施

1. **实例 ID 机制**：使用随机生成的 UUID 作为唯一标识符，存储在 `./data/instance_id` 文件中，不包含任何个人信息
2. **前端 IP 禁用**：PostHog SDK 配置了 `ip_capture: false`，禁用 IP 地址捕获和地理位置推断
3. **后端不发送 IP**：后端代码不发送 IP 地址到统计服务
4. **完全可选**：用户可以随时通过环境变量禁用统计功能
5. **代码透明**：所有统计代码都可以审查，位于 `utils/telemetry.go`

### ⚙️ 如何禁用统计

**方法一：环境变量（推荐）**
```bash
export QUANTMESH_DISABLE_TELEMETRY=1
```

**方法二：前端禁用**
在 `webui/.env.local` 文件中：
```bash
VITE_DISABLE_TELEMETRY=1
```

**方法三：修改代码**
编辑 `utils/telemetry.go`，将 `Enabled` 设为 `false`

### 📖 详细说明

更多关于统计功能的详细说明，请参阅：
- 📖 [统计功能完整指南](docs/TELEMETRY_GUIDE.md)
- 🔒 [隐私保护说明](docs/TELEMETRY_PRIVACY.md)
- 🚀 [快速配置指南](docs/TELEMETRY_SIMPLE_GUIDE.md)

---

## ⚠️ 免责声明

本软件仅供教育与研究使用。加密货币交易风险极高，可能导致资金损失。
- 使用本软件产生之盈亏由使用者自行承担。
- 使用真实资金前请务必在测试网 (Testnet) 充分测试。
- 开发者不对软件错误、网路延迟或交易所故障所致损失负责。

## 🪙 加密货币支付支援

QuantMesh 支援以加密货币支付订阅与授权：

### 支援币种
- **BTC** (Bitcoin)、**ETH** (Ethereum)、**USDT** (Tether, ERC20)、**USDC** (USD Coin, ERC20)

### 支付方式
1. **Coinbase Commerce**（建议）：自动确认、多币种、简易付款页
2. **直接钱包**：无第三方、较私密、需手动确认（约 1–24 小时）

### 文件
- 📖 [使用者支付指南](docs/CRYPTO_PAYMENT_GUIDE.md)
- 🚀 [快速开始](docs/CRYPTO_PAYMENT_QUICKSTART.md)
- 🔧 [设定指南](docs/CRYPTO_PAYMENT_SETUP.md)
- 📊 [实作摘要](docs/reports/CRYPTO_PAYMENT_SUMMARY.md)

## 📜 授权

本项目采用**双授权 (Dual License)**：

### AGPL-3.0 开源授权
- ✅ 可免费使用、修改与分发
- ⚠️ **所有衍生作品须开源**并以 AGPL-3.0 发布
- ⚠️ 即使以网路服务提供也须提供原始码
- ⚠️ 修改后代码须回馈社群

### 商业授权
若需在专有应用或服务中使用，或不愿开源修改，须购买商业授权。

**商业授权范围**：于专有应用中使用、修改无须开源、可整合至专有产品分发、优先技术支援与更新。

**商业授权洽询**：📧 contact@quantmesh.io、🌐 https://quantmesh.io/commercial

## 🤝 贡献

欢迎提交 Issue 与 Pull Request。

**注意**：依 AGPL-3.0，对本项目之贡献皆以相同 AGPL-3.0 授权发布。

详见 [CONTRIBUTING.md](CONTRIBUTING.md)。


## 📞 联络与支援

- 🌐 **官网**：https://quantmesh.io
- 📧 **Email**：contact@quantmesh.io
- 💬 **Discord**：欢迎在 [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions) 参与讨论
- 🐛 **Issues**：[GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💬 **讨论**：[GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📖 **文件**：[完整文件](docs/)

---

<div align="center">
  <strong>Made with ❤️ by QuantMesh Team</strong><br/>
  <sub>若本项目对您有帮助，欢迎给予 ⭐</sub><br/>
  <sub>Version 3.79.6-rc19</sub>
</div>

Copyright © 2025 QuantMesh Team. All Rights Reserved.

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
