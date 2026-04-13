<div align="center">
  <img src="../../logo/qm_thick_tail_white.svg" alt="QuantMesh Logo" width="200"/>
  
  # QuantMesh Market Maker
  
  **高頻度暗号通貨マーケットメーカー**

  [![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org/dl/)
  [![License](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](../../LICENSE)
  [![GitHub Stars](https://img.shields.io/github/stars/ghostsworm/quantmesh.svg?style=social&label=Stars)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Forks](https://img.shields.io/github/forks/ghostsworm/quantmesh.svg?style=social&label=Forks)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Issues](https://img.shields.io/github/issues/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/issues)
  [![GitHub Release](https://img.shields.io/github/release/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/releases)
  [![Website](https://img.shields.io/badge/Website-quantmesh.io-green.svg)](https://quantmesh.io)
  
  [简体中文](../../README.md) | [繁體中文](README.zh-TW.md) | [English](README.en.md) | [Español](README.es.md) | [Français](README.fr.md) | [Português](README.pt.md) | [日本語](README.ja.md)
</div>

---

## 🎯 QuantMeshを選ぶ理由

| 機能 | QuantMesh | 他のソリューション |
|---------|-----------|----------------|
| **取引所サポート** | 20+取引所 | 通常3-5 |
| **応答レイテンシ** | ミリ秒レベル | 秒レベル |
| **リスク管理** | 多層アクティブ制御 | 基本制御 |
| **本番テスト済み** | $100M+取引量 | 未テスト |
| **Webインターフェース** | ✅ 完全なReact UI | ❌ なし/基本 |
| **オープンソース** | AGPL-3.0 | クローズドソース/制限あり |
| **リアルタイムデータ** | WebSocketのみ | REST polling |
| **並行処理** | 1000+注文/秒 | 制限あり |

**主な利点:**
- ✅ **実戦テスト済み**: $100M+の取引量で実証済み
- ✅ **高性能**: WebSocketアーキテクチャでサブ10msレイテンシ
- ✅ **包括的**: 取引から監視まで完全なソリューション
- ✅ **透明性**: 完全オープンソース、監査可能なコード
- ✅ **拡張可能**: カスタマイズ用のプラグインシステム

---

## 📊 Telemetry

**Third-party analytics (PostHog) has been removed.** The Go backend and install script no longer send usage events. Symbols in `utils/telemetry.go` are no-ops for compatibility.

The Web UI may still load a **1×1 pixel** on startup (`webui/src/services/telemetry.ts`). Disable with `VITE_DISABLE_TELEMETRY=1` in `webui/.env.local` or `localStorage` key `QUANTMESH_DISABLE_TELEMETRY` = `1`.

See [TELEMETRY_GUIDE.md](../../docs/TELEMETRY_GUIDE.md).
---

## ⚠️ 免責事項

このソフトウェアは教育および研究目的のみです。暗号通貨取引には高いリスクが伴い、資本損失が発生する可能性があります。
- ユーザーはこのソフトウェアの使用による利益または損失について単独で責任を負います。
- 実際の資金を使用する前に、常にTestnetで徹底的にテストしてください。
- 開発者は、ソフトウェアのバグ、ネットワークレイテンシ、または取引所の障害による損失について責任を負いません。

## 🪙 暗号通貨決済サポート

QuantMeshは、サブスクリプションとライセンスのための暗号通貨決済をサポートしています:

### サポート暗号通貨
- **BTC** (Bitcoin)
- **ETH** (Ethereum)
- **USDT** (Tether, ERC20)
- **USDC** (USD Coin, ERC20)

### 決済方法
1. **Coinbase Commerce** (推奨)
   - 自動確認
   - 複数の暗号通貨サポート
   - 簡単な決済ページ

2. **直接ウォレット決済**
   - 第三者関与なし
   - より多くのプライバシー
   - 手動確認（1-24時間）

### クイックスタート
```bash
# 方法A: Coinbase Commerce (15分)
# 1. https://commerce.coinbase.com で登録
# 2. .env.cryptoでAPIキーを設定
# 3. サービスを開始

# 方法B: 直接ウォレット (5分)
# 1. ウォレットアドレスを設定
# 2. サービスを開始
# 3. 手動確認
```

### ドキュメント
- 📖 [ユーザー決済ガイド](../CRYPTO_PAYMENT_GUIDE.md)
- 🚀 [クイックスタートガイド](../CRYPTO_PAYMENT_QUICKSTART.md)
- 🔧 [セットアップガイド](../CRYPTO_PAYMENT_SETUP.md)
- 📊 [実装サマリー](../reports/CRYPTO_PAYMENT_SUMMARY.md)

### なぜ暗号通貨決済？
✅ クレジットカードや銀行口座不要  
✅ グローバルアクセス、地域制限なし  
✅ 低い取引手数料（1% vs 2.9%）  
✅ より良いプライバシー保護  
✅ 高速確認（10-30分）  
✅ 暗号通貨取引ソフトウェアに完璧に適合  

## 📜 ライセンス

このプロジェクトは**デュアルライセンスモデル**を使用しています:

### AGPL-3.0オープンソースライセンス
- ✅ 使用、変更、配布が無料
- ⚠️ **すべての派生作品はオープンソース化**され、AGPL-3.0の下でリリースされる必要があります
- ⚠️ ネットワークサービスでもソースコードを提供する必要があります
- ⚠️ 変更されたコードはコミュニティに還元される必要があります

### 商用ライセンス
このソフトウェアをプロプライエタリアプリケーションやサービスで使用する必要がある場合、または変更をオープンソース化したくない場合は、商用ライセンスを購入する必要があります。

**商用ライセンス範囲:**
- プロプライエタリアプリケーションでの使用
- 変更をオープンソース化する義務なし
- 配布用のプロプライエタリ製品への統合
- 優先技術サポートとアップデート

**商用ライセンスのお問い合わせ:**
- 📧 メール: contact@quantmesh.io
- 🌐 ウェブサイト: https://quantmesh.io/commercial

---

### ライセンス詳細

このプロジェクトは以下のデュアルライセンスの下にあります:

1. **AGPL-3.0 (GNU Affero General Public License v3.0)**
   - 使用、変更、配布が無料
   - すべての派生作品はAGPL-3.0の下でオープンソース化される必要があります
   - ネットワークサービスでもすべてのユーザーにソースコードを提供する必要があります
   - 変更はコミュニティに還元される必要があります

2. **商用ライセンス**
   - プロプライエタリ使用に必要
   - 変更をオープンソース化する義務なし
   - 優先サポートとアップデートを含む

商用ライセンスのお問い合わせについては、以下にお問い合わせください:
- 📧 メール: contact@quantmesh.io
- 🌐 ウェブサイト: https://quantmesh.io/commercial

## 🤝 貢献

貢献を歓迎します！以下の方法でお手伝いできます:

- ⭐ **このリポジトリにスターを付ける** 役に立つと思ったら
- 🍴 **フォークして使用** プロジェクト
- 🐛 **バグを報告** [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)経由
- 💡 **機能を提案** [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)経由
- 📝 **改善のためのPRを提出**
- 📖 **ドキュメントを改善**

**注:** AGPL-3.0ライセンスに従い、このプロジェクトへのすべての貢献は同じAGPL-3.0ライセンスの下でリリースされます。

詳細なガイドラインについては、[CONTRIBUTING.md](../CONTRIBUTING.md)を参照してください。

## 🙏 謝辞

元のプロジェクト[OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker)の[dennisyang1986](https://github.com/dennisyang1986)に、このプロジェクトの強固な基盤を提供したオープンソース貢献に感謝します。詳細については、[NOTICE](../../NOTICE)ファイルを参照してください。

---

## 📞 連絡先とサポート

- 🌐 **ウェブサイト**: https://quantmesh.io
- 📧 **メール**: contact@quantmesh.io
- 💬 **Discord**: [コミュニティに参加](https://discord.gg/YOUR_INVITE_LINK)
- 🐛 **Issues**: [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💬 **ディスカッション**: [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📖 **ドキュメント**: [完全なドキュメント](../)

---

<div align="center">
  <strong>QuantMesh Teamによって❤️で作成</strong><br/>
  <sub>このプロジェクトが役に立つと思ったら、⭐を付けることを検討してください</sub>
</div>

Copyright © 2025 QuantMesh Team. All Rights Reserved.

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
