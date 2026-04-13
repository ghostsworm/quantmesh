<div align="center">
  <img src="../../logo/qm_thick_tail_white.svg" alt="QuantMesh Logo" width="200"/>
  
  # QuantMesh Market Maker
  
  **High-Frequency Crypto Market Maker**

  [![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org/dl/)
  [![License](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](../../LICENSE)
  [![GitHub Stars](https://img.shields.io/github/stars/ghostsworm/quantmesh.svg?style=social&label=Stars)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Forks](https://img.shields.io/github/forks/ghostsworm/quantmesh.svg?style=social&label=Forks)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Issues](https://img.shields.io/github/issues/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/issues)
  [![GitHub Release](https://img.shields.io/github/release/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/releases)
  [![Website](https://img.shields.io/badge/Website-quantmesh.io-green.svg)](https://quantmesh.io)
  
  [简体中文](../../README.md) | [繁體中文](README.zh-TW.md) | [English](README.en.md) | [Español](README.es.md) | [Français](README.fr.md) | [Português](README.pt.md) | [Nederlands](README.nl.md)
</div>

---

## 🎯 Waarom QuantMesh Kiezen?

| Functie | QuantMesh | Andere Oplossingen |
|---------|-----------|----------------|
| **Exchange Ondersteuning** | 20+ exchanges | Meestal 3-5 |
| **Reactietijd** | Milliseconde-niveau | Seconde-niveau |
| **Risicobeheer** | Multi-laag actieve controle | Basiscontrole |
| **Productie Getest** | $100M+ handelsvolume | Niet getest |
| **Web Interface** | ✅ Volledige React UI | ❌ Geen/Basis |
| **Open Source** | AGPL-3.0 | Gesloten bron/Beperkt |
| **Real-time Data** | Alleen WebSocket | REST polling |
| **Gelijktijdigheid** | 1000+ orders/sec | Beperkt |

**Belangrijkste Voordelen:**
- ✅ **Bewezen**: Getest met $100M+ handelsvolume
- ✅ **Hoge Prestaties**: Sub-10ms latentie met WebSocket architectuur
- ✅ **Uitgebreid**: Complete oplossing van handel tot monitoring
- ✅ **Transparant**: Volledig open source, controleerbare code
- ✅ **Uitbreidbaar**: Plugin systeem voor aanpassing

---

## 📊 Telemetry

**Third-party analytics (PostHog) has been removed.** The Go backend and install script no longer send usage events. Symbols in `utils/telemetry.go` are no-ops for compatibility.

The Web UI may still load a **1×1 pixel** on startup (`webui/src/services/telemetry.ts`). Disable with `VITE_DISABLE_TELEMETRY=1` in `webui/.env.local` or `localStorage` key `QUANTMESH_DISABLE_TELEMETRY` = `1`.

See [TELEMETRY_GUIDE.md](../../docs/TELEMETRY_GUIDE.md).
---

## ⚠️ Disclaimer

Deze software is alleen voor educatieve en onderzoeksdoeleinden. Cryptocurrency handel brengt hoog risico met zich mee en kan leiden tot kapitaalverlies.
- Gebruikers zijn uitsluitend verantwoordelijk voor eventuele winsten of verliezen door het gebruik van deze software.
- Test altijd grondig op Testnet voordat u echte fondsen gebruikt.
- De ontwikkelaars zijn niet aansprakelijk voor verliezen als gevolg van softwarebugs, netwerklatentie of exchange storingen.

## 🪙 Crypto Betalingsondersteuning

QuantMesh ondersteunt cryptocurrency betalingen voor abonnementen en licenties:

### Ondersteunde Cryptocurrencies
- **BTC** (Bitcoin)
- **ETH** (Ethereum)
- **USDT** (Tether, ERC20)
- **USDC** (USD Coin, ERC20)

### Betalingsmethoden
1. **Coinbase Commerce** (Aanbevolen)
   - Automatische bevestiging
   - Meerdere cryptocurrencies ondersteund
   - Eenvoudige betalingspagina

2. **Directe Portemonnee Betaling**
   - Geen betrokkenheid van derden
   - Meer privacy
   - Handmatige bevestiging (1-24 uur)

### Snel Starten
```bash
# Methode A: Coinbase Commerce (15 minuten)
# 1. Registreer op https://commerce.coinbase.com
# 2. Configureer API Key in .env.crypto
# 3. Start service

# Methode B: Directe Portemonnee (5 minuten)
# 1. Configureer portemonnee adressen
# 2. Start service
# 3. Handmatige bevestiging
```

### Documentatie
- 📖 [Gebruikersbetalingsgids](../CRYPTO_PAYMENT_GUIDE.md)
- 🚀 [Snel Startgids](../CRYPTO_PAYMENT_QUICKSTART.md)
- 🔧 [Setup Gids](../CRYPTO_PAYMENT_SETUP.md)
- 📊 [Implementatie Samenvatting](../reports/CRYPTO_PAYMENT_SUMMARY.md)

### Waarom Crypto Betalingen?
✅ Geen creditcard of bankrekening vereist  
✅ Wereldwijde toegankelijkheid, geen regionale beperkingen  
✅ Lagere transactiekosten (1% vs 2.9%)  
✅ Betere privacybescherming  
✅ Snelle bevestiging (10-30 minuten)  
✅ Perfect passend bij crypto handelssoftware  

## 📜 Licentie

Dit project gebruikt een **Dubbele Licentie model**:

### AGPL-3.0 Open Source Licentie
- ✅ Gratis te gebruiken, te wijzigen en te distribueren
- ⚠️ **Alle afgeleide werken moeten open source zijn** en vrijgegeven onder AGPL-3.0
- ⚠️ Broncode moet worden verstrekt, zelfs voor netwerkservices
- ⚠️ Gewijzigde code moet worden bijgedragen aan de gemeenschap

### Commerciële Licentie
Als u deze software nodig heeft in propriëtaire applicaties of services, of uw wijzigingen niet open source wilt maken, moet u een commerciële licentie kopen.

**Commerciële Licentie Bereik:**
- Gebruik in propriëtaire applicaties
- Geen verplichting om wijzigingen open source te maken
- Integreer in propriëtaire producten voor distributie
- Prioritaire technische ondersteuning en updates

**Commerciële Licentie Inquiries:**
- 📧 E-mail: contact@quantmesh.io
- 🌐 Website: https://quantmesh.io/commercial

---

### Licentie Details

Dit project heeft dubbele licentie onder:

1. **AGPL-3.0 (GNU Affero General Public License v3.0)**
   - Gratis voor gebruik, wijziging en distributie
   - Alle afgeleide werken moeten open source zijn onder AGPL-3.0
   - Broncode moet worden verstrekt aan alle gebruikers, zelfs voor netwerkservices
   - Wijzigingen moeten worden bijgedragen aan de gemeenschap

2. **Commerciële Licentie**
   - Vereist voor propriëtair gebruik
   - Geen verplichting om wijzigingen open source te maken
   - Inclusief prioritaire ondersteuning en updates

Voor commerciële licentie inquiries, neem contact op:
- 📧 E-mail: contact@quantmesh.io
- 🌐 Website: https://quantmesh.io/commercial

## 🤝 Bijdragen

We verwelkomen bijdragen! Hier is hoe u kunt helpen:

- ⭐ **Star deze repo** als u het nuttig vindt
- 🍴 **Fork en gebruik** het project
- 🐛 **Rapporteer bugs** via [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💡 **Stel functies voor** via [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📝 **Dien PR's in** voor verbeteringen
- 📖 **Verbeter documentatie**

**Opmerking:** Volgens de AGPL-3.0 licentie zullen alle bijdragen aan dit project worden vrijgegeven onder dezelfde AGPL-3.0 licentie.

Zie [CONTRIBUTING.md](../CONTRIBUTING.md) voor gedetailleerde richtlijnen.

## 🙏 Erkenningen

Bedankt aan het oorspronkelijke project [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker) door [dennisyang1986](https://github.com/dennisyang1986) voor hun open source bijdrage, die een solide basis heeft gelegd voor dit project. Voor meer informatie, verwijzen wij naar het [NOTICE](../../NOTICE) bestand.

---

## 📞 Contact & Ondersteuning

- 🌐 **Website**: https://quantmesh.io
- 📧 **E-mail**: contact@quantmesh.io
- 💬 **Discord**: [Word lid van onze gemeenschap](https://discord.gg/YOUR_INVITE_LINK)
- 🐛 **Issues**: [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💬 **Discussies**: [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📖 **Documentatie**: [Volledige Documentatie](../)

---

<div align="center">
  <strong>Gemaakt met ❤️ door QuantMesh Team</strong><br/>
  <sub>Als u dit project nuttig vindt, overweeg dan om het een ⭐ te geven</sub>
</div>

Copyright © 2025 QuantMesh Team. All Rights Reserved.

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
