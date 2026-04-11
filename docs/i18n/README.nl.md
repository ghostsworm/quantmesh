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

## 📊 Prestatie Metrieken

- **Handelsvolume**: $100M+ productie getest
- **Reactietijd**: <10ms (WebSocket-gedreven)
- **Ondersteunde Exchanges**: 20+
- **Gelijktijdige Verwerking**: 1000+ orders/seconde
- **Systeembeschikbaarheid**: 99.9%+
- **Dagelijks Handelsvermogen**: $3M+ per dag (voorbeeld: ETHUSDC)

---

## 📖 Introductie

QuantMesh is een high-performance, lage latentie cryptocurrency market maker systeem gericht op long grid trading strategieën voor perpetual contract markten. Ontwikkeld in Go en aangedreven door WebSocket real-time data streams, het doel is om stabiele liquiditeitsondersteuning te bieden voor grote exchanges zoals Binance, Bitget en Gate.io.

Na verschillende iteraties hebben we dit systeem gebruikt om meer dan $100 miljoen in virtuele valuta te verhandelen. Bijvoorbeeld, het verhandelen van Binance ETHUSDC met nul kosten, een prijsinterval van $1, en $300 per order, kan het dagelijkse handelsvolume $3 miljoen overschrijden, en meer dan $50 miljoen per maand. Zolang de markt oscilleert of opwaarts trend, zal het blijven winst genereren. Als de markt eenzijdig daalt, kan $30.000 marge garanderen dat er geen liquidatie is voor een daling van 1000 punten. Door continu handelen om kosten te verlagen, is een herstel van 50% genoeg om break-even te bereiken, en terugkeren naar de oorspronkelijke openingsprijs kan aanzienlijke winsten opleveren. Als er een eenzijdige snelle daling is, zal het actieve risicobeheersysteem automatisch identificeren en onmiddellijk stoppen met handelen, alleen voortgezette orders toestaan wanneer de markt herstelt, zonder zorgen over liquidatie door prijspieken.

Voorbeeld: Beginnen met handelen ETH op 3000 punten, de prijs daalt naar 2700 punten, verliest ongeveer $3.000. Wanneer de prijs herstelt naar boven 2850 punten, bereikt het break-even. Terugkeren naar 3000 punten, winsten variëren van $1.000 tot $3.000.

## 📜 Project Oorsprong

Dit project is oorspronkelijk ontwikkeld op basis van [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker), gepubliceerd door [dennisyang1986](https://github.com/dennisyang1986) onder de MIT Licentie.

Gebaseerd op het oorspronkelijke project hebben we de volgende belangrijke verbeteringen en uitbreidingen gemaakt:

- ✨ **Volledige Frontend Interface**: Toegevoegd een React + TypeScript webbeheerinterface die visuele handelsmonitoring, configuratiebeheer en data-analyse biedt
- 🏦 **Exchange Uitbreiding**: Uitgebreid van 3 exchanges (Binance, Bitget, Gate.io) in het oorspronkelijke project naar **20+ grote exchanges**
- 🔒 **Financiële Stabiliteit**: Uitgebreid verbeterde systeembetrouwbaarheid, inclusief uitgebreide foutafhandeling, gelijktijdigheidsveiligheidsmechanismen, gegevensconsistentiegaranties, automatisch herstel, enz.
- 📊 **Verbeterde Monitoring**: Verbeterd loggingsysteem, metriekverzameling (Prometheus), gezondheidscontroles en real-time waarschuwingen
- 🛡️ **Versterkt Risicobeheer**: Multi-laag risicomonitoring, automatische reconciliatie, anomalie circuitbreker en fondsveiligheidsbescherming
- 🔌 **Plugin Systeem**: Ondersteuning voor uitbreidbare plugin mechanismen voor eenvoudige aanpassing en secundaire ontwikkeling
- 📱 **Internationalisatie Ondersteuning**: Meertalige interface (Chinees/Engels), i18n ondersteuning
- 🧪 **Testnet Ondersteuning**: Ondersteuning voor testnet omgevingen van meerdere exchanges voor ontwikkeling en testen

Voor gedetailleerde verbeteringsbeschrijvingen en informatie over software van derden, verwijzen wij naar het [NOTICE](../../NOTICE) bestand.

**Belangrijke Opmerking**: Dit project wordt nu gedistribueerd onder de **GNU Affero General Public License v3.0 (AGPL-3.0)**. In overeenstemming met de MIT Licentievereisten van het oorspronkelijke project hebben we erkenning van het oorspronkelijke project behouden.

## ✨ Belangrijkste Functies

- **Multi-Exchange Ondersteuning**: Compatibel met Binance, Bitget, Gate.io, Bybit, EdgeX en andere grote platforms.
- **Milliseconde-Niveau Reactie**: Volledig WebSocket-gedreven (marktdata en orderflow), elimineert polling vertragingen.
- **Slimme Grid Strategie**: 
  - **Vast Bedrag Modus**: Meer controleerbare kapitaalbenutting.
  - **Super Slot Systeem**: Beheert intelligent order- en positietoestanden, voorkomt gelijktijdigheidsconflicten.
- **Krachtig Risicobeheersysteem**:
  - **Actief Risicobeheer**: Real-time monitoring van K-line volume anomalieën, automatisch pauzeren van handel.
  - **Fondsveiligheid**: Controleert automatisch saldo, hefboomwerking en maximaal positierisico voor opstart.
  - **Automatische Reconciliatie**: Synchroniseert regelmatig lokale en exchange staten om gegevensconsistentie te waarborgen.
- **Hoge Gelijktijdigheidsarchitectuur**: Efficiënt gelijktijdigheidsmodel gebaseerd op Goroutine + Channel + Sync.Map.

## 🏦 Ondersteunde Exchanges

| Exchange | Status | Dagelijks Handelsvolume | Notities |
|----------|--------|------------------------|----------|
| **Binance** | ✅ Stable | $50B+ | Werelds grootste exchange |
| **Bitget** | ✅ Stable | $10B+ | Mainstream futures handelsplatform |
| **Gate.io** | ✅ Stable | $5B+ | Gevestigde exchange |
| **OKX** | ✅ Stable | $20B+ | Top 3 wereldwijd, sterke Chinese gebruikersbasis |
| **Bybit** | ✅ Stable | $15B+ | Mainstream futures handelsplatform |
| **Huobi (HTX)** | ✅ Stable | $5B+ | Gevestigde exchange, sterke Chinese markt |
| **KuCoin** | ✅ Stable | $3B+ | Rijke altcoins, futures contract ondersteuning |
| **Kraken** | ✅ Stable | $2B+ | Sterke naleving, mainstream in Europa en Amerika |
| **Bitfinex** | ✅ Stable | $1B+ | Gevestigde exchange, goede liquiditeit |
| **MEXC** | ✅ Stable | $8B+ | Groot futures handelsvolume, rijke altcoins, testnet ondersteund |
| **BingX** | ✅ Stable | $3B+ | Sociaal handelsplatform, goede futures ervaring, testnet ondersteund |
| **Deribit** | ✅ Stable | $2B+ | Werelds grootste opties exchange, ondersteunt futures + opties, testnet ondersteund |
| **BitMEX** | ✅ Stable | $2B+ | Gevestigde derivaten exchange, tot 100x hefboomwerking, testnet ondersteund |
| **Phemex** | ✅ Stable | $2B+ | Nul-kosten futures handel, high-performance engine, testnet ondersteund |
| **WOO X** | ✅ Stable | $1.5B+ | Institutioneel niveau exchange, diepe liquiditeit, testnet ondersteund |
| **CoinEx** | ✅ Stable | $1B+ | Gevestigde exchange (2017), rijke altcoins, testnet ondersteund |
| **Bitrue** | ✅ Stable | $1B+ | Belangrijkste XRP ecosysteem exchange, sterke Zuidoost-Aziatische markt, testnet ondersteund |
| **XT.COM** | ✅ Stable | $800M+ | Opkomende exchange, rijke altcoins, testnet ondersteund |
| **BTCC** | ✅ Stable | $500M+ | Gevestigde exchange (2011), China's eerste Bitcoin exchange, testnet ondersteund |
| **AscendEX** | ✅ Stable | $400M+ | Institutioneel niveau exchange, DeFi-vriendelijk, testnet ondersteund |
| **Poloniex** | ✅ Stable | $300M+ | Gevestigde exchange (2014), rijke muntvariëteit, testnet ondersteund |
| **Crypto.com** | ✅ Stable | $500M+ | Bekend merk, tientallen miljoenen gebruikers wereldwijd, testnet ondersteund |

## Module Architectuur

```
quantmesh_platform/
├── main.go                    # Hoofdprogramma ingang, component orkestratie
│
├── config/                    # Configuratiebeheer
│   └── config.go              # YAML configuratie laden en valideren
│
├── exchange/                  # Exchange abstractielaag (kern)
│   ├── interface.go           # IExchange verenigde interface
│   ├── factory.go             # Factory patroon voor het maken van exchange instanties
│   ├── types.go               # Algemene gegevensstructuren
│   ├── wrapper_*.go           # Adapters (wrapping exchanges)
│   ├── binance/               # Binance implementatie
│   ├── bitget/                # Bitget implementatie
│   └── gate/                  # Gate.io implementatie
│
├── logger/                    # Loggingsysteem
│   └── logger.go              # Bestandslogging + console logging
│
├── monitor/                   # Prijsmonitoring
│   └── price_monitor.go       # Globale unieke prijsstroom
│
├── order/                     # Order uitvoeringslaag
│   └── executor_adapter.go    # Order executor (snelheidsbeperking + retry)
│
├── position/                  # Positiebeheer (kern)
│   └── super_position_manager.go  # Super slot manager
│
├── safety/                    # Veiligheid en risicobeheer
│   ├── safety.go              # Pre-opstart veiligheidscontroles
│   ├── risk_monitor.go        # Actief risicobeheer (K-line monitoring)
│   ├── reconciler.go          # Positie reconciliatie
│   └── order_cleaner.go       # Order opruiming
│
└── utils/                     # Utility functies
    └── orderid.go             # Aangepaste order ID generatie
```

## Beste Praktijken

1. **Voor Exchange VIP Status**: Dit systeem is een volume generatie tool. Als prijsfluctuaties niet groot zijn, kan $3.000 marge $10 miljoen handelsvolume genereren in 2-3 dagen.

2. **Beste Praktijk voor Winst**: Betreed de markt na een ronde van daling. Koop eerst een positie, start dan de software. Het zal automatisch grid voor grid omhoog verkopen. Wanneer uw positie is uitverkocht, stop het systeem. Als u niet zeker weet of de huidige markt een laag punt is, kunt u beginnen zonder basispositie. Als het verder daalt, voeg een positie toe op het lage punt en herstart om door te gaan met verkopen. Dit maximaliseert winsten. Herhaal deze cyclus om continu winst te maken. Maak u geen zorgen over dalingen - het programma verlaagt continu de kosten. Zolang het met de helft herstelt, bereikt u break-even.

## 🚀 Aan de Slag

### Vereisten
- Go 1.21 of hoger
- Netwerkomgeving die toegang heeft tot exchange API's

### Installatie

1. **Kloon de repository**
   ```bash
   git clone https://github.com/ghostsworm/quantmesh.git
   cd quantmesh
   ```

2. **Installeer afhankelijkheden**
   ```bash
   go mod download
   ```

### Configuratie

> **Runtime SSOT:** The primary database table `app_config` holds the authoritative configuration. One-time YAML import: `./quantmesh --migrate-app-config` (with `QUANTMESH_IMPORT_YAML`, or `config.yaml` in the working directory), or run `./quantmesh /path/to/file.yaml` as the first argument. See [`docs/config-database-design.md`](../config-database-design.md).

1. Kopieer het voorbeeldconfiguratiebestand:
   ```bash
   cp docs/config/examples/config.example.yaml config.yaml
   ```

2. Bewerk `config.yaml` en vul uw API Key en strategieparameters in:

   ```yaml
   app:
     current_exchange: "binance"  # Selecteer exchange

   exchanges:
     binance:
       api_key: "YOUR_API_KEY"
       secret_key: "YOUR_SECRET_KEY"
       fee_rate: 0.0002

   trading:
     symbol: "ETHUSDT"       # Handelspaar
     price_interval: 2       # Grid spacing (prijs)
     order_quantity: 30     # Bedrag per grid (USDT)
     buy_window_size: 10    # Aantal kooporders
     sell_window_size: 10   # Aantal verkooporders
   ```

### Gebruik

#### Productie Modus

Voer de gecompileerde binary uit:

```bash
go run main.go
```

Of bouw en voer uit:

```bash
go build -o quantmesh
./quantmesh
```

De backend zal de frontend statische bestanden serveren op poort 28888 (standaard).

#### Ontwikkelingsmodus

Voor frontend ontwikkeling met hot reload en source code debugging:

**Optie 1: Gebruik het ontwikkelingsscript (Aanbevolen)**

```bash
./scripts/local/dev.sh
```

Dit script zal:
- Start de Go backend server op poort 28888
- Start de Vite dev server op poort 15173
- Activeer hot reload voor frontend code wijzigingen
- Bied source maps voor debugging (geen geminificeerde code)

Toegang tot de applicatie op: **http://localhost:15173**

**Optie 2: Handmatige opstart**

Terminal 1 - Start Go backend:
```bash
go run main.go
```

Terminal 2 - Start Vite dev server:
```bash
cd webui
pnpm dev
```

Toegang tot de applicatie op: **http://localhost:15173**

**Ontwikkelingsmodus Voordelen:**
- ✅ Hot reload - Frontend code wijzigingen worden direct weerspiegeld
- ✅ Source maps - Debug met originele TypeScript/React code (niet geminificeerd)
- ✅ Fast refresh - React componenten updaten zonder staat te verliezen
- ✅ Betere foutmeldingen - Zie werkelijke bestandsnamen en regelnummers

**Opmerking:** In ontwikkelingsmodus proxy de Vite dev server API verzoeken (`/api/*`) en WebSocket verbindingen (`/ws`) naar de Go backend die draait op poort 28888.

## 🏗️ Architectuur

Het systeem neemt een modulair ontwerp aan met kerncomponenten inclusief:

- **Exchange Laag**: Verenigde exchange interface abstractie, afscherming van onderliggende API verschillen.
- **Prijsmonitor**: Globale unieke WebSocket prijsbron, waarborgt beslissingsconsistentie.
- **Super Positie Manager**: Kern positie manager, beheert order levenscyclus gebaseerd op Slot mechanisme.
- **Veiligheid & Risicobeheer**: Multi-laag risicobeheer, inclusief opstartcontroles, runtime monitoring en anomalie circuitbreker.

Voor meer gedetailleerde architectuurdocumentatie, verwijzen wij naar [ARCHITECTURE.md](../../ARCHITECTURE.md).

## 📊 Gebruiksstatistieken & Privacybescherming

QuantMesh bevat een optionele gebruikersstatistiek functie om anonieme gebruiksgegevens te verzamelen, wat ons helpt projectgebruik te begrijpen en het product te verbeteren. **Alle gegevensverzameling is volledig transparant, code is controleerbaar en kan op elk moment worden uitgeschakeld.**

### 🔒 Privacybescherming

**Gegevens die We Verzamelen (Anoniem):**
- ✅ **Basisinformatie**: Versienummer, besturingssysteem, architectuur, instantie ID (willekeurig gegenereerde UUID)
- ✅ **Gebruiksstatistieken**: Exchange namen gebruikt, handelsparen
- ✅ **Prestatiemetrieken**: API verzoek/antwoord latentie, WebSocket latentie
- ✅ **Handelsactiviteit**: Handelsrichting (koop/verkoop), exclusief handelsbedragen

**Gegevens die We NIET Verzamelen:**
- ❌ **IP Adres**: Frontend heeft IP-capture uitgeschakeld, backend gebruikt instantie ID in plaats van IP
- ❌ **Geolocatie**: Geen verzameling van breedtegraad/lengtegraad, stad of andere locatie-informatie
- ❌ **Persoonlijke Informatie**: Geen verzameling van gebruikers-ID's, e-mails, namen of enige identiteitsinformatie
- ❌ **Gevoelige Gegevens**: Geen verzameling van API-sleutels, handelsbedragen, rekening saldi of positie-informatie
- ❌ **Financiële Gegevens**: Geen verzameling van financiële of handelsgevoelige informatie

### 🛡️ Privacybeschermingsmaatregelen

1. **Instantie ID Mechanisme**: Gebruikt willekeurig gegenereerde UUID als unieke identifier, opgeslagen in `./data/instance_id` bestand, bevat geen persoonlijke informatie
2. **Frontend IP Uitgeschakeld**: PostHog SDK geconfigureerd met `ip_capture: false`, schakelt IP-adres capture en geolocatie inferentie uit
3. **Backend Verzendt Geen IP**: Backend code verzendt geen IP-adressen naar statistiekservice
4. **Volledig Optioneel**: Gebruikers kunnen statistieken op elk moment uitschakelen via omgevingsvariabelen
5. **Code Transparantie**: Alle statistiekcode is controleerbaar, gelegen in `utils/telemetry.go`

### ⚙️ Hoe Statistieken Uit te Schakelen

**Methode 1: Omgevingsvariabele (Aanbevolen)**
```bash
export QUANTMESH_DISABLE_TELEMETRY=1
```

**Methode 2: Frontend Uitschakelen**
In `webui/.env.local` bestand:
```bash
VITE_DISABLE_TELEMETRY=1
```

**Methode 3: Code Wijzigen**
Bewerk `utils/telemetry.go`, stel `Enabled` in op `false`

### 📖 Gedetailleerde Documentatie

Voor meer gedetailleerde informatie over de statistiekfunctie, verwijzen wij naar:
- 📖 [Volledige Statistiek Gids](../../docs/TELEMETRY_GUIDE.md)
- 🔒 [Privacybescherming Gids](../../docs/TELEMETRY_PRIVACY.md)
- 🚀 [Snelle Setup Gids](../../docs/TELEMETRY_SIMPLE_GUIDE.md)

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

Zie [CONTRIBUTING.md](../../CONTRIBUTING.md) voor gedetailleerde richtlijnen.

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
