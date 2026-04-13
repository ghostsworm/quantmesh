<div align="center">
  <img src="../../logo/qm_thick_tail_white.svg" alt="QuantMesh Logo" width="200"/>
  
  # QuantMesh Market Maker
  
  **毫秒級高頻加密貨幣做市商系統**
  
  <h3>⭐ 如果這個項目對您有幫助，請給個 Star 支持一下！</h3>
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
  
  [简体中文](../../README.md) | [繁體中文](README.zh-TW.md) | [English](README.en.md) | [Español](README.es.md) | [Français](README.fr.md) | [Português](README.pt.md) | [Deutsch](README.de.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Русский](README.ru.md) | [العربية](README.ar.md) | [हिन्दी](README.hi.md) | [Bahasa Indonesia](README.id.md) | [Tiếng Việt](README.vi.md) | [ไทย](README.th.md) | [Türkçe](README.tr.md) | [Українська](README.uk.md) | [فارسی](README.fa.md) | [Nederlands](README.nl.md) | [Italiano](README.it.md) | [বাংলা](README.bn.md) | [اردو](README.ur.md) | [Polski](README.pl.md) | [Tagalog](README.tl.md)
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

## 📊 Telemetry

**Third-party analytics (PostHog) has been removed.** The Go backend and install script no longer send usage events. Symbols in `utils/telemetry.go` are no-ops for compatibility.

The Web UI may still load a **1×1 pixel** on startup (`webui/src/services/telemetry.ts`). Disable with `VITE_DISABLE_TELEMETRY=1` in `webui/.env.local` or `localStorage` key `QUANTMESH_DISABLE_TELEMETRY` = `1`.

See [TELEMETRY_GUIDE.md](../../docs/TELEMETRY_GUIDE.md).
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
- 📖 [使用者支付指南](../../docs/CRYPTO_PAYMENT_GUIDE.md)
- 🚀 [快速開始](../../docs/CRYPTO_PAYMENT_QUICKSTART.md)
- 🔧 [設定指南](../../docs/CRYPTO_PAYMENT_SETUP.md)
- 📊 [實作摘要](../../docs/reports/CRYPTO_PAYMENT_SUMMARY.md)

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

詳見 [CONTRIBUTING.md](../CONTRIBUTING.md)。


## 📞 聯絡與支援

- 🌐 **官網**：https://quantmesh.io
- 📧 **Email**：contact@quantmesh.io
- 💬 **Discord**：歡迎在 [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions) 參與討論
- 🐛 **Issues**：[GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💬 **討論**：[GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📖 **文件**：[完整文件](../../docs/)

---

<div align="center">
  <strong>Made with ❤️ by QuantMesh Team</strong><br/>
  <sub>若本專案對您有幫助，歡迎給予 ⭐</sub><br/>
  <sub>Version 3.79.6-rc11</sub>
</div>

Copyright © 2025 QuantMesh Team. All Rights Reserved.

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
