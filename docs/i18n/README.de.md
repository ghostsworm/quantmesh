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

## 📊 Leistungsmetriken

- **Handelsvolumen**: $100M+ produktionsgetestet
- **Antwortlatenz**: <10ms (WebSocket-gesteuert)
- **Unterstützte Börsen**: 20+
- **Nebenläufige Verarbeitung**: 1000+ Orders/Sekunde
- **Systemverfügbarkeit**: 99.9%+
- **Tägliche Handelskapazität**: $3M+ pro Tag (Beispiel: ETHUSDC)

---

## 📖 Einführung

QuantMesh ist ein Hochleistungs-, Niedriglatenz-Kryptowährungs-Markt-Maker-System, das sich auf Long-Grid-Handelsstrategien für Perpetual-Contract-Märkte konzentriert. Entwickelt in Go und angetrieben von WebSocket-Echtzeit-Datenströmen, zielt es darauf ab, stabile Liquiditätsunterstützung für große Börsen wie Binance, Bitget und Gate.io zu bieten.

Nach mehreren Iterationen haben wir dieses System verwendet, um über $100 Millionen in virtueller Währung zu handeln. Zum Beispiel kann beim Handel mit Binance ETHUSDC mit Nullgebühren, einem Preisintervall von $1 und $300 pro Order das tägliche Handelsvolumen $3 Millionen überschreiten und über $50 Millionen pro Monat. Solange der Markt oszilliert oder nach oben tendiert, wird er weiterhin Gewinne generieren. Wenn der Markt einseitig fällt, können $30.000 Marge garantieren, dass es keine Liquidation für einen Rückgang von 1000 Punkten gibt. Durch kontinuierlichen Handel zur Kostensenkung ist eine Erholung von 50% ausreichend, um die Gewinnschwelle zu erreichen, und die Rückkehr zum ursprünglichen Eröffnungspreis kann erhebliche Gewinne erzielen. Wenn es einen einseitigen schnellen Rückgang gibt, wird das aktive Risikokontrollsystem automatisch identifizieren und den Handel sofort stoppen, nur fortgesetzte Orders zulassen, wenn sich der Markt erholt, ohne sich Sorgen über Liquidation durch Preisspitzen zu machen.

Beispiel: Beginn des ETH-Handels bei 3000 Punkten, der Preis fällt auf 2700 Punkte, Verlust von etwa $3.000. Wenn sich der Preis auf über 2850 Punkte erholt, erreicht er die Gewinnschwelle. Rückkehr zu 3000 Punkten, Gewinne reichen von $1.000 bis $3.000.

## 📜 Projektursprung

Dieses Projekt wurde ursprünglich auf der Grundlage von [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker) entwickelt, veröffentlicht von [dennisyang1986](https://github.com/dennisyang1986) unter der MIT-Lizenz.

Basierend auf dem ursprünglichen Projekt haben wir die folgenden wichtigen Verbesserungen und Erweiterungen vorgenommen:

- ✨ **Vollständige Frontend-Oberfläche**: Hinzugefügt eine React + TypeScript Web-Management-Oberfläche, die visuelle Handelsüberwachung, Konfigurationsverwaltung und Datenanalyse bietet
- 🏦 **Börsenerweiterung**: Erweitert von 3 Börsen (Binance, Bitget, Gate.io) im ursprünglichen Projekt auf **20+ große Börsen**
- 🔒 **Finanzstabilität**: Umfassend verbesserte Systemzuverlässigkeit, einschließlich umfassender Fehlerbehandlung, Nebenläufigkeitssicherheitsmechanismen, Datenkonsistenzgarantien, automatische Wiederherstellung usw.
- 📊 **Verbesserte Überwachung**: Verbessertes Protokollierungssystem, Metrikerfassung (Prometheus), Gesundheitsprüfungen und Echtzeit-Warnungen
- 🛡️ **Verstärkte Risikokontrolle**: Mehrschichtige Risikoüberwachung, automatische Abstimmung, Anomalie-Schutzschalter und Fondsicherheitsschutz
- 🔌 **Plugin-System**: Unterstützung für erweiterbare Plugin-Mechanismen zur einfachen Anpassung und sekundären Entwicklung
- 📱 **Internationalisierungsunterstützung**: Mehrsprachige Oberfläche (Chinesisch/Englisch), i18n-Unterstützung
- 🧪 **Testnet-Unterstützung**: Unterstützung für Testnet-Umgebungen mehrerer Börsen für Entwicklung und Tests

Für detaillierte Verbesserungsbeschreibungen und Informationen zu Drittanbieter-Software siehe bitte die [NOTICE](../../NOTICE)-Datei.

**Wichtiger Hinweis**: Dieses Projekt wird nun unter der **GNU Affero General Public License v3.0 (AGPL-3.0)** vertrieben. In Übereinstimmung mit den MIT-Lizenzanforderungen des ursprünglichen Projekts haben wir die Anerkennung des ursprünglichen Projekts beibehalten.

## ✨ Hauptfunktionen

- **Multi-Börsen-Unterstützung**: Kompatibel mit Binance, Bitget, Gate.io, Bybit, EdgeX und anderen großen Plattformen.
- **Millisekunden-Ebene Antwort**: Vollständig WebSocket-gesteuert (Marktdaten und Orderfluss), eliminiert Polling-Verzögerungen.
- **Intelligente Grid-Strategie**: 
  - **Fester Betragsmodus**: Kontrollierbarere Kapitalnutzung.
  - **Super-Slot-System**: Verwaltet intelligent Order- und Positionszustände und verhindert Nebenläufigkeitskonflikte.
- **Leistungsstarkes Risikokontrollsystem**:
  - **Aktive Risikokontrolle**: Echtzeitüberwachung von K-Line-Volumenanomalien, automatisches Anhalten des Handels.
  - **Fondsicherheit**: Prüft automatisch Guthaben, Hebelwirkung und maximales Positionsrisiko vor dem Start.
  - **Automatische Abstimmung**: Synchronisiert regelmäßig lokale und Börsenzustände, um Datenkonsistenz sicherzustellen.
- **Hochnebenläufigkeits-Architektur**: Effizientes Nebenläufigkeitsmodell basierend auf Goroutine + Channel + Sync.Map.

## 🏦 Unterstützte Börsen

| Börse | Status | Tägliches Handelsvolumen | Notizen |
|----------|--------|------------------------|-------|
| **Binance** | ✅ Stable | $50B+ | Größte Börse der Welt |
| **Bitget** | ✅ Stable | $10B+ | Mainstream-Futures-Handelsplattform |
| **Gate.io** | ✅ Stable | $5B+ | Etablierte Börse |
| **OKX** | ✅ Stable | $20B+ | Top 3 weltweit, starke chinesische Nutzerbasis |
| **Bybit** | ✅ Stable | $15B+ | Mainstream-Futures-Handelsplattform |
| **Huobi (HTX)** | ✅ Stable | $5B+ | Etablierte Börse, starker chinesischer Markt |
| **KuCoin** | ✅ Stable | $3B+ | Reiche Altcoins, Futures-Kontraktunterstützung |
| **Kraken** | ✅ Stable | $2B+ | Starke Compliance, Mainstream in Europa und Amerika |
| **Bitfinex** | ✅ Stable | $1B+ | Etablierte Börse, gute Liquidität |
| **MEXC** | ✅ Stable | $8B+ | Großes Futures-Handelsvolumen, reiche Altcoins, Testnet unterstützt |
| **BingX** | ✅ Stable | $3B+ | Social-Trading-Plattform, gute Futures-Erfahrung, Testnet unterstützt |
| **Deribit** | ✅ Stable | $2B+ | Größte Optionsbörse der Welt, unterstützt Futures + Optionen, Testnet unterstützt |
| **BitMEX** | ✅ Stable | $2B+ | Etablierte Derivatebörse, bis zu 100x Hebelwirkung, Testnet unterstützt |
| **Phemex** | ✅ Stable | $2B+ | Nullgebühren-Futures-Handel, Hochleistungsmotor, Testnet unterstützt |
| **WOO X** | ✅ Stable | $1.5B+ | Institutionelle Börse, tiefe Liquidität, Testnet unterstützt |
| **CoinEx** | ✅ Stable | $1B+ | Etablierte Börse (2017), reiche Altcoins, Testnet unterstützt |
| **Bitrue** | ✅ Stable | $1B+ | Haupt-XRP-Ökosystembörse, starker südostasiatischer Markt, Testnet unterstützt |
| **XT.COM** | ✅ Stable | $800M+ | Aufstrebende Börse, reiche Altcoins, Testnet unterstützt |
| **BTCC** | ✅ Stable | $500M+ | Etablierte Börse (2011), Chinas erste Bitcoin-Börse, Testnet unterstützt |
| **AscendEX** | ✅ Stable | $400M+ | Institutionelle Börse, DeFi-freundlich, Testnet unterstützt |
| **Poloniex** | ✅ Stable | $300M+ | Etablierte Börse (2014), reiche Münzvielfalt, Testnet unterstützt |
| **Crypto.com** | ✅ Stable | $500M+ | Bekannte Marke, zig Millionen Nutzer weltweit, Testnet unterstützt |

## Modularchitektur

```
quantmesh_platform/
├── main.go                    # Hauptprogrammeinstieg, Komponentenorchestrierung
│
├── config/                    # Konfigurationsverwaltung
│   └── config.go              # YAML-Konfiguration laden und validieren
│
├── exchange/                  # Börsenabstraktionsebene (Kern)
│   ├── interface.go           # IExchange einheitliche Schnittstelle
│   ├── factory.go             # Factory-Muster zum Erstellen von Börseninstanzen
│   ├── types.go               # Gemeinsame Datenstrukturen
│   ├── wrapper_*.go           # Adapter (Börsen umhüllen)
│   ├── binance/               # Binance-Implementierung
│   ├── bitget/                # Bitget-Implementierung
│   └── gate/                  # Gate.io-Implementierung
│
├── logger/                    # Protokollierungssystem
│   └── logger.go              # Dateiprotokollierung + Konsolenprotokollierung
│
├── monitor/                   # Preisüberwachung
│   └── price_monitor.go       # Globaler eindeutiger Preisdatenstrom
│
├── order/                     # Orderausführungsebene
│   └── executor_adapter.go    # Orderausführer (Ratenbegrenzung + Wiederholung)
│
├── position/                  # Positionsverwaltung (Kern)
│   └── super_position_manager.go  # Super-Slot-Manager
│
├── safety/                    # Sicherheit und Risikokontrolle
│   ├── safety.go              # Vorstart-Sicherheitsprüfungen
│   ├── risk_monitor.go        # Aktive Risikokontrolle (K-Line-Überwachung)
│   ├── reconciler.go          # Positionsabstimmung
│   └── order_cleaner.go       # Orderbereinigung
│
└── utils/                     # Hilfsfunktionen
    └── orderid.go             # Benutzerdefinierte Order-ID-Generierung
```

## Best Practices

1. **Für Börsen-VIP-Status**: Dieses System ist ein Volumengenerierungstool. Wenn Preisfluktuationen nicht groß sind, können $3.000 Marge in 2-3 Tagen $10 Millionen Handelsvolumen generieren.

2. **Beste Praxis für Gewinn**: Betreten Sie den Markt nach einer Runde des Rückgangs. Kaufen Sie zuerst eine Position, dann starten Sie die Software. Es wird automatisch Grid für Grid nach oben verkaufen. Wenn Ihre Position ausverkauft ist, stoppen Sie das System. Wenn Sie sich nicht sicher sind, ob der aktuelle Markt ein Tiefpunkt ist, können Sie ohne Basisposition starten. Wenn es weiter fällt, fügen Sie eine Position am Tiefpunkt hinzu und starten Sie neu, um weiter zu verkaufen. Dies maximiert die Gewinne. Wiederholen Sie diesen Zyklus, um kontinuierlich Gewinne zu erzielen. Machen Sie sich keine Sorgen über Rückgänge - das Programm senkt kontinuierlich die Kosten. Solange es sich um die Hälfte erholt, erreichen Sie die Gewinnschwelle.

## 🚀 Erste Schritte

### Voraussetzungen
- Go 1.21 oder höher
- Netzwerkumgebung, die auf Börsen-APIs zugreifen kann

### Installation

1. **Repository klonen**
   ```bash
   git clone https://github.com/ghostsworm/quantmesh.git
   cd quantmesh
   ```

2. **Abhängigkeiten installieren**
   ```bash
   go mod download
   ```

### Konfiguration

1. Beispielkonfigurationsdatei kopieren:
   ```bash
   cp config.example.yaml config.yaml
   ```

2. Bearbeiten Sie `config.yaml` und füllen Sie Ihren API-Schlüssel und Strategieparameter aus:

   ```yaml
   app:
     current_exchange: "binance"  # Börse auswählen

   exchanges:
     binance:
       api_key: "YOUR_API_KEY"
       secret_key: "YOUR_SECRET_KEY"
       fee_rate: 0.0002

   trading:
     symbol: "ETHUSDT"       # Handelspaar
     price_interval: 2       # Grid-Abstand (Preis)
     order_quantity: 30     # Betrag pro Grid (USDT)
     buy_window_size: 10    # Anzahl der Kauforders
     sell_window_size: 10   # Anzahl der Verkaufsorders
   ```

### Verwendung

#### Produktionsmodus

Kompilierte Binärdatei ausführen:

```bash
go run main.go
```

Oder erstellen und ausführen:

```bash
go build -o quantmesh
./quantmesh
```

Das Backend wird die Frontend-Statikdateien auf Port 28888 (Standard) bereitstellen.

#### Entwicklungsmodus

Für Frontend-Entwicklung mit Hot Reload und Source-Code-Debugging:

**Option 1: Entwicklungsskript verwenden (Empfohlen)**

```bash
./dev.sh
```

Dieses Skript wird:
- Go-Backend-Server auf Port 28888 starten
- Vite-Dev-Server auf Port 15173 starten
- Hot Reload für Frontend-Codeänderungen aktivieren
- Source Maps für Debugging bereitstellen (kein minifizierter Code)

Dann auf die Anwendung zugreifen unter: **http://localhost:15173**

**Option 2: Manueller Start**

Terminal 1 - Go-Backend starten:
```bash
go run main.go
```

Terminal 2 - Vite-Dev-Server starten:
```bash
cd webui
pnpm dev
```

Dann auf die Anwendung zugreifen unter: **http://localhost:15173**

**Entwicklungsmodus-Vorteile:**
- ✅ Hot Reload - Frontend-Codeänderungen werden sofort widergespiegelt
- ✅ Source Maps - Debuggen mit originalem TypeScript/React-Code (nicht minifiziert)
- ✅ Fast Refresh - React-Komponenten aktualisieren ohne Zustandsverlust
- ✅ Bessere Fehlermeldungen - Tatsächliche Dateinamen und Zeilennummern sehen

**Hinweis:** Im Entwicklungsmodus proxiert der Vite-Dev-Server API-Anfragen (`/api/*`) und WebSocket-Verbindungen (`/ws`) an das Go-Backend, das auf Port 28888 läuft.

## 🏗️ Architektur

Das System verwendet ein modulares Design mit Kernkomponenten, einschließlich:

- **Börsenschicht**: Einheitliche Börsenschnittstellenabstraktion, die zugrunde liegende API-Unterschiede abschirmt.
- **Preismonitor**: Globale eindeutige WebSocket-Preisquelle, die Entscheidungskonsistenz gewährleistet.
- **Super-Positions-Manager**: Kernpositionsmanager, der den Orderlebenszyklus basierend auf dem Slot-Mechanismus verwaltet.
- **Sicherheit & Risikokontrolle**: Mehrschichtige Risikokontrolle, einschließlich Startprüfungen, Laufzeitüberwachung und Anomalieschutzschalter.

Für detailliertere Architekturdokumentation siehe bitte [ARCHITECTURE.md](../../ARCHITECTURE.md).

## 📊 Nutzungsstatistiken & Datenschutz

QuantMesh enthält eine optionale Nutzungsstatistikfunktion zum Sammeln anonymer Nutzungsdaten, die uns hilft, die Projektnutzung zu verstehen und das Produkt zu verbessern. **Alle Datensammlung ist vollständig transparent, Code ist überprüfbar und kann jederzeit deaktiviert werden.**

### 🔒 Datenschutz

**Von uns gesammelte Daten (Anonym):**
- ✅ **Grundinformationen**: Versionsnummer, Betriebssystem, Architektur, Instanz-ID (zufällig generierte UUID)
- ✅ **Nutzungsstatistiken**: Verwendete Börsennamen, Handelspaare
- ✅ **Leistungsmetriken**: API-Anfrage-/Antwortlatenz, WebSocket-Latenz
- ✅ **Handelsaktivität**: Handelsrichtung (Kauf/Verkauf), ohne Handelsbeträge

**Von uns nicht gesammelte Daten:**
- ❌ **IP-Adresse**: Frontend hat IP-Erfassung deaktiviert, Backend verwendet Instanz-ID statt IP
- ❌ **Geolokalisierung**: Keine Erfassung von Breiten-/Längengrad, Stadt oder anderen Standortinformationen
- ❌ **Persönliche Informationen**: Keine Erfassung von Benutzer-IDs, E-Mails, Namen oder Identitätsinformationen
- ❌ **Sensible Daten**: Keine Erfassung von API-Schlüsseln, Handelsbeträgen, Kontoständen oder Positionsinformationen
- ❌ **Finanzdaten**: Keine Erfassung von Finanz- oder handelssensiblen Informationen

### 🛡️ Datenschutzmaßnahmen

1. **Instanz-ID-Mechanismus**: Verwendet zufällig generierte UUID als eindeutigen Bezeichner, gespeichert in `./data/instance_id` Datei, enthält keine persönlichen Informationen
2. **Frontend-IP deaktiviert**: PostHog SDK mit `ip_capture: false` konfiguriert, deaktiviert IP-Adressenerfassung und Geolokalisierungsinferenz
3. **Backend sendet keine IP**: Backend-Code sendet keine IP-Adressen an den Statistikdienst
4. **Vollständig optional**: Benutzer können Statistiken jederzeit über Umgebungsvariablen deaktivieren
5. **Code-Transparenz**: Alle Statistikcodes sind überprüfbar, befinden sich in `utils/telemetry.go`

### ⚙️ So deaktivieren Sie Statistiken

**Methode 1: Umgebungsvariable (Empfohlen)**
```bash
export QUANTMESH_DISABLE_TELEMETRY=1
```

**Methode 2: Frontend deaktivieren**
In der Datei `webui/.env.local`:
```bash
VITE_DISABLE_TELEMETRY=1
```

**Methode 3: Code ändern**
Bearbeiten Sie `utils/telemetry.go`, setzen Sie `Enabled` auf `false`

### 📖 Detaillierte Dokumentation

Für detailliertere Informationen zur Statistikfunktion siehe bitte:
- 📖 [Vollständiger Statistikleitfaden](../../docs/TELEMETRY_GUIDE.md)
- 🔒 [Datenschutzleitfaden](../../docs/TELEMETRY_PRIVACY.md)
- 🚀 [Schneller Einrichtungsleitfaden](../../docs/TELEMETRY_SIMPLE_GUIDE.md)

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

Siehe [CONTRIBUTING.md](../../CONTRIBUTING.md) für detaillierte Richtlinien.

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
