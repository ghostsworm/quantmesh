<div align="center">
  <img src="logo/qm_thick_tail_white.svg" alt="QuantMesh Logo" width="200"/>
  
  # QuantMesh Market Maker
  
  **毫秒級高頻加密貨幣做市商系統**
  
  <h3>⭐ 如果這個項目對您有幫助，請給個 Star 支持一下！</h3>
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
  
  [简体中文](../../README.md) | [繁體中文](README.zh-TW.md) | [English](docs/i18n/README.en.md) | [Español](docs/i18n/README.es.md) | [Français](docs/i18n/README.fr.md) | [Português](docs/i18n/README.pt.md) | [Deutsch](docs/i18n/README.de.md) | [日本語](docs/i18n/README.ja.md) | [한국어](docs/i18n/README.ko.md) | [Русский](docs/i18n/README.ru.md) | [العربية](docs/i18n/README.ar.md) | [हिन्दी](docs/i18n/README.hi.md) | [Bahasa Indonesia](docs/i18n/README.id.md) | [Tiếng Việt](docs/i18n/README.vi.md) | [ไทย](docs/i18n/README.th.md) | [Türkçe](docs/i18n/README.tr.md) | [Українська](docs/i18n/README.uk.md) | [فارسی](docs/i18n/README.fa.md) | [Nederlands](docs/i18n/README.nl.md) | [Italiano](docs/i18n/README.it.md) | [বাংলা](docs/i18n/README.bn.md) | [اردو](docs/i18n/README.ur.md) | [Polski](docs/i18n/README.pl.md) | [Tagalog](docs/i18n/README.tl.md)
</div>

---

## 🎯 為何選擇 QuantMesh？

| 功能 | QuantMesh | 其他方案 |
|---------|-----------|----------------|
| **交易所支援** | 20+ 家 | 通常 3–5 家 |
| **回應延遲** | 毫秒級 | 秒級 |
| **風控** | 多層主動控制 | 基礎控制 |
| **實戰驗證** | $1 億+ 交易量 | 未經驗證 |
| **Web 介面** | ✅ 完整 React UI | ❌ 無/簡陋 |
| **開源** | AGPL-3.0 | 閉源/受限 |
| **即時資料** | 僅 WebSocket | REST 輪詢 |
| **並行** | 1000+ 單/秒 | 有限 |

**核心優勢：**
- ✅ **實戰驗證**：$1 億+ 交易量驗證
- ✅ **高效能**：WebSocket 架構，延遲 <10ms
- ✅ **功能完整**：從交易到監控的完整方案
- ✅ **透明**：完全開源，可審計程式碼
- ✅ **可擴展**：外掛系統可自訂

---

## 📊 效能指標

- **交易量**：$1 億+ 實戰驗證
- **回應延遲**：<10ms（WebSocket 驅動）
- **支援交易所**：20+
- **並行處理**：1000+ 單/秒
- **系統可用性**：99.9%+
- **每日交易能力**：$300 萬+/天（例：ETHUSDC）

---

## 📖 專案簡介

QuantMesh 是高效能、低延遲的加密貨幣做市商系統，專注於永續合約市場的單向做多無限獨立網格策略。以 Go 開發，以 WebSocket 即時資料流驅動，旨在為 Binance、Bitget、Gate.io 等主流交易所提供穩定流動性支援。

經過多個版本迭代，我們已使用此系統交易超過 1 億美元虛擬貨幣。例如：交易幣安 ETHUSDC，零手續費，價格間隔 1 美元，每筆 300 美元，每日交易量可超過 300 萬美元、每月超過 5000 萬美元；只要市場震盪或向上即可持續獲利。若市場單邊下跌，3 萬美元保證金可保證下跌 1000 點不爆倉；透過不斷交易拉低成本，回漲 50% 即可保本，漲回開倉價可獲豐厚利潤。若出現單邊急跌，主動風控會自動識別並立即停止交易，待市場恢復後才允許繼續下單，無須擔心插針爆倉。

舉例：ETH 3000 點開始交易，跌至 2700 點約虧 3000 美元；漲回 2850 點以上保本，漲回 3000 點則獲利約 1000–3000 美元。


## ✨ 核心特性

- **多交易所支援**：適配 Binance、Bitget、Gate.io、Bybit、EdgeX 等主流平台；支援現貨與合約
- **毫秒級回應**：全 WebSocket 驅動（行情與訂單流），無輪詢延遲
- **多策略支援**：
  - **網格策略**：固定金額模式、超級槽位系統；**網格風控**（止損/止盈/回撤止盈/最大層數/趨勢過濾）、**價格範圍**（軟限制）、**觸發價格**、**等差/等比模式**、**網格上移/下移**、**終止時全部平倉**；進階 P1 資金費率趨勢聯動、P2 訂單簿優化掛單
  - **DCA / 馬丁格爾 / 均值回歸 / 動量 / 趨勢跟蹤 / 組合策略**：可並行、可分配資金
- **技術指標庫**：50+ 專業指標（趨勢、動量、波動率、成交量），供策略與回測使用
- **AI 功能**：市場分析、參數優化、風險評估、情緒分析（新聞 / Polymarket 等）
- **回測系統**：歷史 K 線回測、多策略回測、20+ 風險指標與報告
- **強大風控系統**：
  - **主動風控**：即時監控 K 線成交量異常，自動暫停交易
  - **資金安全**：啟動前自動檢查餘額、槓桿與最大持倉風險
  - **自動對帳**：定期同步本地與交易所狀態，確保資料一致
  - **期權對沖**：支援做多/做空網格與 Put/Call 期權對沖，從 Binance/Deribit 拉取持倉、計算覆蓋率、展期建議
- **完整監控體系**：Prometheus 指標、Grafana 儀表板、多層告警、Watchdog 健康檢查
- **事件中心與新聞監控**：價格波動與交易事件記錄、AI 新聞分析與預測驗證
- **使用統計（可選）**：匿名使用數據收集，幫助改進產品；完全透明、可審查、可禁用
- **高並行架構**：基於 Goroutine + Channel + Sync.Map 的高效並行模型

## 🏦 支援的交易所

| 交易所 | 狀態 | 日均交易量 | 備註 |
|--------|------|-----------|------|
| **Binance** | ✅ Stable | $50B+ | 全球最大交易所 |
| **Bitget** | ✅ Stable | $10B+ | 合約交易主流平台 |
| **Gate.io** | ✅ Stable | $5B+ | 老牌交易所 |
| **OKX** | ✅ Stable | $20B+ | 全球前三，中文用戶多 |
| **Bybit** | ✅ Stable | $15B+ | 合約交易主流平台 |
| **Huobi (HTX)** | ✅ Stable | $5B+ | 老牌交易所，中文市場強 |
| **KuCoin** | ✅ Stable | $3B+ | 山寨幣豐富，期貨合約支援 |
| **Kraken** | ✅ Stable | $2B+ | 合規性強，歐美主流 |
| **Bitfinex** | ✅ Stable | $1B+ | 老牌交易所，流動性好 |
| **MEXC（抹茶）** | ✅ Stable | $8B+ | 合約交易量大，山寨幣豐富，支援測試網 |
| **BingX** | ✅ Stable | $3B+ | 社交交易平台，合約體驗佳，支援測試網 |
| **Deribit** | ✅ Stable | $2B+ | 全球最大期權交易所，支援期貨+期權，支援測試網 |
| **BitMEX** | ✅ Stable | $2B+ | 老牌衍生品交易所，最高 100x 槓桿，支援測試網 |
| **Phemex** | ✅ Stable | $2B+ | 零手續費合約，高效能引擎，支援測試網 |
| **WOO X** | ✅ Stable | $1.5B+ | 機構級交易所，深度流動性，支援測試網 |
| **CoinEx** | ✅ Stable | $1B+ | 老牌交易所（2017），山寨幣豐富，支援測試網 |
| **Bitrue** | ✅ Stable | $1B+ | XRP 生態主要交易所，東南亞市場強，支援測試網 |
| **XT.COM** | ✅ Stable | $800M+ | 新興交易所，山寨幣豐富，支援測試網 |
| **BTCC** | ✅ Stable | $500M+ | 老牌交易所（2011），中國首家比特幣交易所，支援測試網 |
| **AscendEX** | ✅ Stable | $400M+ | 機構級，DeFi 友善，支援測試網 |
| **Poloniex** | ✅ Stable | $300M+ | 老牌交易所（2014），幣種豐富，支援測試網 |
| **Crypto.com** | ✅ Stable | $500M+ | 知名品牌，全球數千萬用戶，支援測試網 |

## 功能模組概覽

| 模組 | 說明 |
|------|------|
| **交易策略** | 網格、DCA、馬丁格爾、均值回歸、動量、趨勢跟蹤、組合策略；支援多交易對與現貨/合約 |
| **技術分析** | 50+ 技術指標（趨勢、動量、波動率、成交量）；策略信號與回測 |
| **AI** | 市場分析、參數優化、風險評估、情緒分析、Polymarket 信號 |
| **回測** | 歷史 K 線回測、多策略、風險指標與 Markdown 報告 |
| **風控與對帳** | 主動 K 線風控、深度監控、持倉對帳、訂單清理、啟動前安全檢查、期權對沖（Put/Call 覆蓋率、展期建議） |
| **監控與告警** | Prometheus、Grafana、多層告警、Watchdog、資金費率與價差監控 |
| **事件與新聞** | 事件中心（價格波動/交易事件）、新聞收集與 AI 分析、預測驗證 |
| **外掛與擴展** | 外掛載入、授權驗證、自訂策略與交易所適配 |

詳細說明見 [ARCHITECTURE.md](ARCHITECTURE.md)、[docs/GRID_STRATEGY_ADVANCED_FEATURES.md](docs/GRID_STRATEGY_ADVANCED_FEATURES.md)、[docs/RISK_CONTROL_GUIDE.md](docs/RISK_CONTROL_GUIDE.md)、[docs/API_REFERENCE.md](docs/API_REFERENCE.md)。

## 模組架構

```
quantmesh_platform/
├── main.go                    # 主程式入口，元件編排
│
├── config/                    # 配置管理
│   ├── config.go              # YAML 配置載入與驗證
│   ├── backup.go              # 配置備份
│   ├── history.go             # 配置歷史
│   └── hot_reload.go          # 配置熱更新
│
├── exchange/                  # 交易所抽象層（核心）
│   ├── interface.go           # IExchange 統一介面
│   ├── binance/               # 幣安（現貨/合約）
│   ├── bitget/                # Bitget 實作
│   ├── gate/                  # Gate.io 實作
│   └── [20+ 交易所實作]
│
├── strategy/                  # 策略模組
│   ├── grid_strategy.go       # 網格策略
│   ├── dca_enhanced.go        # DCA 策略
│   ├── martingale.go          # 馬丁格爾
│   ├── mean_reversion.go      # 均值回歸
│   ├── momentum.go            # 動量策略
│   ├── trend_following.go     # 趨勢跟蹤
│   └── combo_strategy.go      # 組合策略
│
├── indicators/                # 技術指標庫
│   ├── trend.go               # 趨勢指標（MACD、ADX 等）
│   ├── momentum.go            # 動量指標（RSI、Stochastic 等）
│   ├── volatility.go          # 波動率指標（ATR、Bollinger 等）
│   └── volume.go              # 成交量指標
│
├── ai/                        # AI 功能
│   ├── service/               # 市場分析、參數優化、風險與情緒分析
│   └── risk_assessor.go       # AI 風險評估
│
├── backtest/                  # 回測系統
│   ├── data_fetcher.go        # 歷史 K 線獲取與快取
│   ├── backtester.go          # 回測引擎
│   └── metrics.go             # 風險指標
│
├── position/                  # 倉位管理（核心）
│   └── super_position_manager.go  # 超級槽位管理器（P1/P2 整合）
│
├── safety/                    # 安全與風控
│   ├── safety.go              # 啟動前安全檢查
│   ├── risk_monitor.go        # 主動風控（K 線監控）
│   ├── reconciler.go          # 持倉對帳
│   ├── order_cleaner.go       # 訂單清理
│   └── funding_monitor.go     # 資金費率監控
│
├── monitor/                   # 監控（價格、新聞、價差、Watchdog）
├── event/                     # 事件中心
├── metrics/                   # Prometheus 指標
├── plugin/                    # 外掛載入與授權
├── web/                       # Web API 與前端靜態資源
└── webui/                     # React 前端原始碼
```

## 最佳實踐

1. **刷交易所 VIP**：本系統為刷量工具；若漲跌幅度不大，3000 美元保證金約 2–3 天可刷出 1000 萬美元交易量。

2. **獲利最佳實踐**：在一輪下跌後進場，先買一筆持倉再啟動軟體，會自動向上一格一格賣出；持倉賣完後停止系統。若不確定是否為低點，可不買底倉啟動，若再跌在低點補一筆持倉後重啟持續賣出，利潤最大化。如此循環持續獲利；下跌時程式會持續拉低成本，只要漲回一半即可保本。

## 🚀 快速開始

### 方式一：Docker 一鍵運行（推薦，最簡單）

**只需 3 步：**

1. **克隆倉庫並準備配置**
   ```bash
   git clone https://github.com/ghostsworm/quantmesh.git
   cd quantmesh
   cp config.example.yaml config.yaml
   ```

2. **編輯配置**：編輯 `config.yaml`，填入 API Key 與策略參數（見下方配置說明）

3. **啟動服務**
   ```bash
   docker-compose up -d
   ```

   訪問 **http://localhost:8080** 即可使用 Web UI。

   **停止服務：**
   ```bash
   docker-compose down
   ```

---

### 方式二：從源碼編譯運行

#### 環境需求
- Go 1.21 或更高
- 網路環境可存取交易所 API

#### 安裝

1. **克隆倉庫**
   ```bash
   git clone https://github.com/ghostsworm/quantmesh.git
   cd quantmesh
   ```

2. **安裝依賴**
   ```bash
   go mod download
   ```

#### 配置

1. 複製範例配置：
   ```bash
   cp config.example.yaml config.yaml
   ```

2. 編輯 `config.yaml`，填入 API Key 與策略參數：

   ```yaml
   app:
     current_exchange: "binance"  # 選擇交易所

   exchanges:
     binance:
       api_key: "YOUR_API_KEY"
       secret_key: "YOUR_SECRET_KEY"
       fee_rate: 0.0002

   trading:
     symbol: "ETHUSDT"       # 交易對
     price_interval: 2       # 網格間距（價格）
     order_quantity: 30      # 每格投入金額 (USDT)
     buy_window_size: 10     # 買單掛單數量
     sell_window_size: 10    # 賣單掛單數量
   ```

#### 執行

**正式模式：**

```bash
go run main.go
```

或編譯後執行：

```bash
go build -o quantmesh
./quantmesh
```

後端將在 port 28888（預設）提供前端靜態檔案。

#### 開發模式

若需前端熱重載與除錯：

**方式一：使用開發腳本（建議）**

```bash
./dev.sh
```

腳本會：啟動 Go 後端（port 28888）、啟動 Vite 開發伺服器（port 15173）、啟用熱重載與 source map。  
存取 **http://localhost:15173** 即可。

**方式二：手動啟動**

終端 1 - 啟動 Go 後端：
```bash
go run main.go
```

終端 2 - 啟動 Vite：
```bash
cd webui
yarn dev
```

存取 **http://localhost:15173**。

## 🏗️ 架構

系統採模組化設計，核心元件包含：

- **Exchange Layer**：統一交易所介面抽象，屏蔽底層 API 差異
- **Price Monitor**：全域唯一 WebSocket 價格源，確保決策一致
- **Super Position Manager**：核心倉位管理，基於 Slot 機制管理訂單生命週期
- **Safety & Risk Control**：多層風控，含啟動檢查、執行時監控與異常熔斷

更多架構說明請參閱 [ARCHITECTURE.md](ARCHITECTURE.md)。

## 📊 使用統計與隱私保護

QuantMesh 包含一個可選的使用統計功能，用於收集匿名的使用數據，幫助我們了解項目使用情況並改進產品。**所有數據收集都是完全透明的，代碼可審查，並且可以隨時禁用。**

### 🔒 隱私保護

**我們收集的數據（匿名）：**
- ✅ **基礎信息**：版本號、操作系統、架構、實例 ID（隨機生成的 UUID）
- ✅ **使用情況**：使用的交易所名稱、交易幣種對
- ✅ **性能指標**：API 請求/響應耗時、WebSocket 延時
- ✅ **交易活動**：交易方向（買入/賣出），不包含交易金額

**我們不收集的數據：**
- ❌ **IP 地址**：前端已禁用 IP 捕獲，後端使用實例 ID 而非 IP
- ❌ **地理位置**：不收集經緯度、城市等位置信息
- ❌ **個人信息**：不收集用戶 ID、郵箱、姓名等任何身份信息
- ❌ **敏感數據**：不收集 API 密鑰、交易金額、賬戶餘額、持倉信息
- ❌ **財務數據**：不收集任何財務或交易敏感信息

### 🛡️ 隱私保護措施

1. **實例 ID 機制**：使用隨機生成的 UUID 作為唯一標識符，存儲在 `./data/instance_id` 文件中，不包含任何個人信息
2. **前端 IP 禁用**：PostHog SDK 配置了 `ip_capture: false`，禁用 IP 地址捕獲和地理位置推斷
3. **後端不發送 IP**：後端代碼不發送 IP 地址到統計服務
4. **完全可選**：用戶可以隨時通過環境變量禁用統計功能
5. **代碼透明**：所有統計代碼都可以審查，位於 `utils/telemetry.go`

### ⚙️ 如何禁用統計

**方法一：環境變量（推薦）**
```bash
export QUANTMESH_DISABLE_TELEMETRY=1
```

**方法二：前端禁用**
在 `webui/.env.local` 文件中：
```bash
VITE_DISABLE_TELEMETRY=1
```

**方法三：修改代碼**
編輯 `utils/telemetry.go`，將 `Enabled` 設為 `false`

### 📖 詳細說明

更多關於統計功能的詳細說明，請參閱：
- 📖 [統計功能完整指南](docs/TELEMETRY_GUIDE.md)
- 🔒 [隱私保護說明](docs/TELEMETRY_PRIVACY.md)
- 🚀 [快速配置指南](docs/TELEMETRY_SIMPLE_GUIDE.md)

---

## ⚠️ 免責聲明

本軟體僅供教育與研究使用。加密貨幣交易風險極高，可能導致資金損失。
- 使用本軟體產生之盈虧由使用者自行承擔。
- 使用真實資金前請務必在測試網 (Testnet) 充分測試。
- 開發者不對軟體錯誤、網路延遲或交易所故障所致損失負責。

## 🪙 加密貨幣支付支援

QuantMesh 支援以加密貨幣支付訂閱與授權：

### 支援幣種
- **BTC** (Bitcoin)、**ETH** (Ethereum)、**USDT** (Tether, ERC20)、**USDC** (USD Coin, ERC20)

### 支付方式
1. **Coinbase Commerce**（建議）：自動確認、多幣種、簡易付款頁
2. **直接錢包**：無第三方、較私密、需手動確認（約 1–24 小時）

### 文件
- 📖 [使用者支付指南](docs/CRYPTO_PAYMENT_GUIDE.md)
- 🚀 [快速開始](docs/CRYPTO_PAYMENT_QUICKSTART.md)
- 🔧 [設定指南](docs/CRYPTO_PAYMENT_SETUP.md)
- 📊 [實作摘要](docs/reports/CRYPTO_PAYMENT_SUMMARY.md)

## 📜 授權

本專案採用**雙授權 (Dual License)**：

### AGPL-3.0 開源授權
- ✅ 可免費使用、修改與分發
- ⚠️ **所有衍生作品須開源**並以 AGPL-3.0 發布
- ⚠️ 即使以網路服務提供也須提供原始碼
- ⚠️ 修改後程式碼須回饋社群

### 商業授權
若需在專有應用或服務中使用，或不願開源修改，須購買商業授權。

**商業授權範圍**：於專有應用中使用、修改無須開源、可整合至專有產品分發、優先技術支援與更新。

**商業授權洽詢**：📧 contact@quantmesh.io、🌐 https://quantmesh.io/commercial

詳情請見上方說明；商業授權洽詢：📧 contact@quantmesh.io、🌐 https://quantmesh.io/commercial

## 🤝 貢獻

歡迎提交 Issue 與 Pull Request。

**注意**：依 AGPL-3.0，對本專案之貢獻皆以相同 AGPL-3.0 授權發布。

詳見 [CONTRIBUTING.md](CONTRIBUTING.md)。


## 📞 聯絡與支援

- 🌐 **官網**：https://quantmesh.io
- 📧 **Email**：contact@quantmesh.io
- 💬 **Discord**：歡迎在 [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions) 參與討論
- 🐛 **Issues**：[GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💬 **討論**：[GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📖 **文件**：[完整文件](docs/)

---

<div align="center">
  <strong>Made with ❤️ by QuantMesh Team</strong><br/>
  <sub>若本專案對您有幫助，歡迎給予 ⭐</sub><br/>
  <sub>Version 3.79.6-rc11</sub>
</div>

Copyright © 2025 QuantMesh Team. All Rights Reserved.
