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

## 📊 Telemetry

**Third-party analytics (PostHog) has been removed.** The Go backend and install script no longer send usage events. Symbols in `utils/telemetry.go` are no-ops for compatibility.

The Web UI may still load a **1×1 pixel** on startup (`webui/src/services/telemetry.ts`). Disable with `VITE_DISABLE_TELEMETRY=1` in `webui/.env.local` or `localStorage` key `QUANTMESH_DISABLE_TELEMETRY` = `1`.

See [TELEMETRY_GUIDE.md](../../docs/TELEMETRY_GUIDE.md).
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
