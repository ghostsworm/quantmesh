<div align="center">
  <img src="../../logo/qm_thick_tail_white.svg" alt="QuantMesh Logo" width="200"/>
  
  # QuantMesh Market Maker
  
  **Hochfrequenz-Krypto-Markt-Maker**

  [![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org/dl/)
  [![License](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](../../LICENSE)
  [![GitHub Stars](https://img.shields.io/github/stars/ghostsworm/quantmesh.svg?style=social&label=Stars)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Forks](https://img.shields.io/github/forks/ghostsworm/quantmesh.svg?style=social&label=Forks)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Issues](https://img.shields.io/github/issues/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/issues)
  [![GitHub Release](https://img.shields.io/github/release/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/releases)
  [![Website](https://img.shields.io/badge/Website-quantmesh.io-green.svg)](https://quantmesh.io)
  
  [简体中文](../../README.md) | [繁體中文](README.zh-TW.md) | [English](README.en.md) | [Español](README.es.md) | [Français](README.fr.md) | [Português](README.pt.md) | [Deutsch](README.de.md)
</div>

---

## 🎯 Warum QuantMesh wählen?

| Funktion | QuantMesh | Andere Lösungen |
|---------|-----------|----------------|
| **Börsenunterstützung** | 20+ Börsen | Normalerweise 3-5 |
| **Antwortlatenz** | Millisekunden-Ebene | Sekunden-Ebene |
| **Risikokontrolle** | Mehrschichtige aktive Kontrolle | Grundlegende Kontrolle |
| **Produktion getestet** | $100M+ Handelsvolumen | Nicht getestet |
| **Web-Interface** | ✅ Vollständige React UI | ❌ Keines/Grundlegend |
| **Open Source** | AGPL-3.0 | Geschlossene Quelle/Eingeschränkt |
| **Echtzeit-Daten** | Nur WebSocket | REST Polling |
| **Nebenläufigkeit** | 1000+ Orders/Sekunde | Begrenzt |

**Hauptvorteile:**
- ✅ **Erprobt**: Bewiesen mit $100M+ Handelsvolumen
- ✅ **Hohe Leistung**: Sub-10ms Latenz mit WebSocket-Architektur
- ✅ **Umfassend**: Vollständige Lösung vom Handel bis zur Überwachung
- ✅ **Transparent**: Vollständig Open Source, überprüfbarer Code
- ✅ **Erweiterbar**: Plugin-System zur Anpassung

---

## 📊 Telemetry

**Third-party analytics (PostHog) has been removed.** The Go backend and install script no longer send usage events. Symbols in `utils/telemetry.go` are no-ops for compatibility.

The Web UI may still load a **1×1 pixel** on startup (`webui/src/services/telemetry.ts`). Disable with `VITE_DISABLE_TELEMETRY=1` in `webui/.env.local` or `localStorage` key `QUANTMESH_DISABLE_TELEMETRY` = `1`.

See [TELEMETRY_GUIDE.md](../../docs/TELEMETRY_GUIDE.md).
---

## ⚠️ Haftungsausschluss

Diese Software dient nur zu Bildungs- und Forschungszwecken. Der Handel mit Kryptowährungen birgt hohe Risiken und kann zu Kapitalverlusten führen.
- Benutzer sind allein für Gewinne oder Verluste aus der Nutzung dieser Software verantwortlich.
- Testen Sie immer gründlich auf Testnet, bevor Sie echte Gelder verwenden.
- Die Entwickler sind nicht haftbar für Verluste aufgrund von Softwarefehlern, Netzwerklatenz oder Börsenausfällen.

## 🪙 Krypto-Zahlungsunterstützung

QuantMesh unterstützt Kryptowährungszahlungen für Abonnements und Lizenzen:

### Unterstützte Kryptowährungen
- **BTC** (Bitcoin)
- **ETH** (Ethereum)
- **USDT** (Tether, ERC20)
- **USDC** (USD Coin, ERC20)

### Zahlungsmethoden
1. **Coinbase Commerce** (Empfohlen)
   - Automatische Bestätigung
   - Mehrere Kryptowährungen unterstützt
   - Einfache Zahlungsseite

2. **Direkte Wallet-Zahlung**
   - Keine Beteiligung Dritter
   - Mehr Datenschutz
   - Manuelle Bestätigung (1-24 Stunden)

### Schnellstart
```bash
# Methode A: Coinbase Commerce (15 Minuten)
# 1. Registrieren Sie sich unter https://commerce.coinbase.com
# 2. Konfigurieren Sie API-Schlüssel in .env.crypto
# 3. Dienst starten

# Methode B: Direktes Wallet (5 Minuten)
# 1. Wallet-Adressen konfigurieren
# 2. Dienst starten
# 3. Manuelle Bestätigung
```

### Dokumentation
- 📖 [Benutzerzahlungsleitfaden](../CRYPTO_PAYMENT_GUIDE.md)
- 🚀 [Schnellstartleitfaden](../CRYPTO_PAYMENT_QUICKSTART.md)
- 🔧 [Einrichtungsleitfaden](../CRYPTO_PAYMENT_SETUP.md)
- 📊 [Implementierungszusammenfassung](../reports/CRYPTO_PAYMENT_SUMMARY.md)

### Warum Krypto-Zahlungen?
✅ Keine Kreditkarte oder Bankkonto erforderlich  
✅ Globale Zugänglichkeit, keine regionalen Beschränkungen  
✅ Niedrigere Transaktionsgebühren (1% vs 2.9%)  
✅ Besserer Datenschutz  
✅ Schnelle Bestätigung (10-30 Minuten)  
✅ Perfekt für Krypto-Handelssoftware  

## 📜 Lizenz

Dieses Projekt verwendet ein **Dual-Lizenz-Modell**:

### AGPL-3.0 Open-Source-Lizenz
- ✅ Kostenlos zu verwenden, zu ändern und zu verteilen
- ⚠️ **Alle abgeleiteten Werke müssen Open Source sein** und unter AGPL-3.0 veröffentlicht werden
- ⚠️ Quellcode muss auch für Netzwerkdienste bereitgestellt werden
- ⚠️ Geänderter Code muss an die Community zurückgegeben werden

### Kommerzielle Lizenz
Wenn Sie diese Software in proprietären Anwendungen oder Diensten verwenden müssen oder Ihre Änderungen nicht Open Source machen möchten, müssen Sie eine kommerzielle Lizenz erwerben.

**Umfang der kommerziellen Lizenz:**
- Verwendung in proprietären Anwendungen
- Keine Verpflichtung, Änderungen Open Source zu machen
- Integration in proprietäre Produkte zur Verteilung
- Prioritätstechnischer Support und Updates

**Anfragen zur kommerziellen Lizenz:**
- 📧 E-Mail: contact@quantmesh.io
- 🌐 Website: https://quantmesh.io/commercial

---

### Lizenzdetails

Dieses Projekt ist unter doppelter Lizenz:

1. **AGPL-3.0 (GNU Affero General Public License v3.0)**
   - Kostenlos für Verwendung, Änderung und Verteilung
   - Alle abgeleiteten Werke müssen unter AGPL-3.0 Open Source sein
   - Quellcode muss allen Benutzern bereitgestellt werden, auch für Netzwerkdienste
   - Änderungen müssen an die Community zurückgegeben werden

2. **Kommerzielle Lizenz**
   - Erforderlich für proprietäre Nutzung
   - Keine Verpflichtung, Änderungen Open Source zu machen
   - Enthält Prioritätssupport und Updates

Für Anfragen zur kommerziellen Lizenzierung kontaktieren Sie bitte:
- 📧 E-Mail: contact@quantmesh.io
- 🌐 Website: https://quantmesh.io/commercial

## 🤝 Beitragen

Wir freuen uns über Beiträge! So können Sie helfen:

- ⭐ **Dieses Repo mit einem Stern versehen**, wenn Sie es hilfreich finden
- 🍴 **Forken und verwenden** Sie das Projekt
- 🐛 **Fehler melden** über [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💡 **Funktionen vorschlagen** über [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📝 **PRs einreichen** für Verbesserungen
- 📖 **Dokumentation verbessern**

**Hinweis:** Gemäß der AGPL-3.0-Lizenz werden alle Beiträge zu diesem Projekt unter derselben AGPL-3.0-Lizenz veröffentlicht.

Siehe [CONTRIBUTING.md](../CONTRIBUTING.md) für detaillierte Richtlinien.

## 🙏 Danksagungen

Danke an das ursprüngliche Projekt [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker) von [dennisyang1986](https://github.com/dennisyang1986) für ihren Open-Source-Beitrag, der eine solide Grundlage für dieses Projekt geschaffen hat. Weitere Informationen finden Sie in der [NOTICE](../../NOTICE)-Datei.

---

## 📞 Kontakt & Support

- 🌐 **Website**: https://quantmesh.io
- 📧 **E-Mail**: contact@quantmesh.io
- 💬 **Discord**: [Treten Sie unserer Community bei](https://discord.gg/YOUR_INVITE_LINK)
- 🐛 **Issues**: [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💬 **Diskussionen**: [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📖 **Dokumentation**: [Vollständige Dokumentation](../)

---

<div align="center">
  <strong>Mit ❤️ vom QuantMesh Team erstellt</strong><br/>
  <sub>Wenn Sie dieses Projekt hilfreich finden, erwägen Sie bitte, ihm einen ⭐ zu geben</sub>
</div>

Copyright © 2025 QuantMesh Team. Alle Rechte vorbehalten.

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
