<div align="center">
  <img src="../../logo/qm_thick_tail_white.svg" alt="QuantMesh Logo" width="200"/>
  
  # QuantMesh Market Maker
  
  **Market Maker Crypto ad Alta Frequenza**

  [![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org/dl/)
  [![License](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](../../LICENSE)
  [![GitHub Stars](https://img.shields.io/github/stars/ghostsworm/quantmesh.svg?style=social&label=Stars)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Forks](https://img.shields.io/github/forks/ghostsworm/quantmesh.svg?style=social&label=Forks)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Issues](https://img.shields.io/github/issues/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/issues)
  [![GitHub Release](https://img.shields.io/github/release/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/releases)
  [![Website](https://img.shields.io/badge/Website-quantmesh.io-green.svg)](https://quantmesh.io)
  
  [简体中文](../../README.md) | [繁體中文](README.zh-TW.md) | [English](README.en.md) | [Español](README.es.md) | [Français](README.fr.md) | [Português](README.pt.md) | [Italiano](README.it.md)
</div>

---

## 🎯 Perché Scegliere QuantMesh?

| Funzionalità | QuantMesh | Altre Soluzioni |
|---------|-----------|----------------|
| **Supporto Exchange** | 20+ exchange | Di solito 3-5 |
| **Latenza di Risposta** | Livello millisecondo | Livello secondo |
| **Controllo del Rischio** | Controllo attivo multi-livello | Controllo base |
| **Testato in Produzione** | Volume di trading $100M+ | Non testato |
| **Interfaccia Web** | ✅ UI React completa | ❌ Nessuna/Basica |
| **Open Source** | AGPL-3.0 | Codice chiuso/Limitato |
| **Dati in Tempo Reale** | Solo WebSocket | Polling REST |
| **Concorrenza** | 1000+ ordini/sec | Limitata |

**Vantaggi Chiave:**
- ✅ **Testato sul Campo**: Provato con volume di trading $100M+
- ✅ **Alte Prestazioni**: Latenza sub-10ms con architettura WebSocket
- ✅ **Completo**: Soluzione completa dal trading al monitoraggio
- ✅ **Trasparente**: Completamente open source, codice verificabile
- ✅ **Estendibile**: Sistema di plugin per personalizzazione

---

## 📊 Metriche di Prestazione

- **Volume di Trading**: $100M+ testato in produzione
- **Latenza di Risposta**: <10ms (guidato da WebSocket)
- **Exchange Supportati**: 20+
- **Elaborazione Concorrente**: 1000+ ordini/secondo
- **Disponibilità del Sistema**: 99.9%+
- **Capacità di Trading Giornaliera**: $3M+ al giorno (esempio: ETHUSDC)

---

## 📖 Introduzione

QuantMesh è un sistema market maker per criptovalute ad alte prestazioni e bassa latenza che si concentra su strategie di trading grid unidirezionali per mercati di contratti perpetui. Sviluppato in Go e guidato da flussi di dati in tempo reale WebSocket, mira a fornire supporto di liquidità stabile per exchange principali come Binance, Bitget e Gate.io.

Dopo diverse iterazioni, abbiamo utilizzato questo sistema per scambiare oltre $100 milioni in valuta virtuale. Ad esempio, scambiando Binance ETHUSDC con zero commissioni, un intervallo di prezzo di $1 e $300 per ordine, il volume di trading giornaliero può superare $3 milioni e oltre $50 milioni al mese. Finché il mercato oscilla o tende al rialzo, continuerà a generare profitti. Se il mercato scende unilateralmente, $30.000 di margine possono garantire nessuna liquidazione per un calo di 1000 punti. Attraverso il trading continuo per abbassare i costi, un recupero del 50% è sufficiente per raggiungere il pareggio, e tornare al prezzo di apertura originale può produrre profitti sostanziali. Se c'è un calo rapido unilaterale, il sistema di controllo del rischio attivo identificherà automaticamente e fermerà immediatamente il trading, permettendo ordini continui solo quando il mercato si riprende, senza preoccuparsi della liquidazione da picchi di prezzo.

Esempio: Iniziando a scambiare ETH a 3000 punti, il prezzo scende a 2700 punti, perdendo circa $3.000. Quando il prezzo si riprende a oltre 2850 punti, raggiunge il pareggio. Tornando a 3000 punti, i profitti variano da $1.000 a $3.000.

## 📜 Origine del Progetto

Questo progetto è stato originariamente sviluppato basandosi su [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker), pubblicato da [dennisyang1986](https://github.com/dennisyang1986) sotto la Licenza MIT.

Basandoci sul progetto originale, abbiamo apportato i seguenti miglioramenti e estensioni principali:

- ✨ **Interfaccia Frontend Completa**: Aggiunta un'interfaccia di gestione web React + TypeScript che fornisce monitoraggio del trading visivo, gestione della configurazione e analisi dei dati
- 🏦 **Espansione Exchange**: Espanso da 3 exchange (Binance, Bitget, Gate.io) nel progetto originale a **20+ exchange principali**
- 🔒 **Stabilità di Livello Finanziario**: Migliorata in modo completo l'affidabilità del sistema, inclusa gestione completa degli errori, meccanismi di sicurezza della concorrenza, garanzie di coerenza dei dati, recupero automatico, ecc.
- 📊 **Monitoraggio Migliorato**: Sistema di registrazione migliorato, raccolta di metriche (Prometheus), controlli di salute e avvisi in tempo reale
- 🛡️ **Controllo del Rischio Rafforzato**: Monitoraggio del rischio multi-livello, riconciliazione automatica, interruttore di anomalie e protezione della sicurezza dei fondi
- 🔌 **Sistema di Plugin**: Supporto per meccanismi di plugin estendibili per personalizzazione facile e sviluppo secondario
- 📱 **Supporto Internazionalizzazione**: Interfaccia multilingue (Cinese/Inglese), supporto i18n
- 🧪 **Supporto Testnet**: Supporto per ambienti testnet di più exchange per sviluppo e test

Per descrizioni dettagliate dei miglioramenti e informazioni sul software di terze parti, si prega di fare riferimento al file [NOTICE](../../NOTICE).

**Nota Importante**: Questo progetto è ora distribuito sotto la **GNU Affero General Public License v3.0 (AGPL-3.0)**. In conformità con i requisiti della Licenza MIT del progetto originale, abbiamo mantenuto il riconoscimento del progetto originale.

## ✨ Caratteristiche Principali

- **Supporto Multi-Exchange**: Compatibile con Binance, Bitget, Gate.io, Bybit, EdgeX e altre piattaforme principali.
- **Risposta a Livello di Millisecondo**: Completamente guidato da WebSocket (dati di mercato e flusso di ordini), eliminando i ritardi di polling.
- **Strategia Grid Intelligente**: 
  - **Modalità Importo Fisso**: Utilizzo del capitale più controllabile.
  - **Sistema Super Slot**: Gestisce intelligentemente gli stati degli ordini e delle posizioni, prevenendo conflitti di concorrenza.
- **Sistema Potente di Controllo del Rischio**:
  - **Controllo del Rischio Attivo**: Monitoraggio in tempo reale delle anomalie del volume K-line, pausando automaticamente il trading.
  - **Sicurezza dei Fondi**: Controlla automaticamente il saldo, la leva finanziaria e il rischio massimo della posizione prima dell'avvio.
  - **Riconciliazione Automatica**: Sincronizza regolarmente gli stati locali e dell'exchange per garantire la coerenza dei dati.
- **Architettura ad Alta Concorrenza**: Modello di concorrenza efficiente basato su Goroutine + Channel + Sync.Map.

## 🏦 Exchange Supportati

| Exchange | Stato | Volume di Trading Giornaliero | Note |
|----------|--------|----------------------------|-------|
| **Binance** | ✅ Stable | $50B+ | Exchange più grande al mondo |
| **Bitget** | ✅ Stable | $10B+ | Piattaforma principale di trading futures |
| **Gate.io** | ✅ Stable | $5B+ | Exchange consolidato |
| **OKX** | ✅ Stable | $20B+ | Top 3 globalmente, forte base di utenti cinesi |
| **Bybit** | ✅ Stable | $15B+ | Piattaforma principale di trading futures |
| **Huobi (HTX)** | ✅ Stable | $5B+ | Exchange consolidato, forte mercato cinese |
| **KuCoin** | ✅ Stable | $3B+ | Ricco di altcoin, supporto contratti futures |
| **Kraken** | ✅ Stable | $2B+ | Forte conformità, mainstream in Europa e America |
| **Bitfinex** | ✅ Stable | $1B+ | Exchange consolidato, buona liquidità |
| **MEXC** | ✅ Stable | $8B+ | Grande volume di trading futures, ricco di altcoin, testnet supportato |
| **BingX** | ✅ Stable | $3B+ | Piattaforma di trading sociale, buona esperienza futures, testnet supportato |
| **Deribit** | ✅ Stable | $2B+ | Exchange di opzioni più grande al mondo, supporta futures + opzioni, testnet supportato |
| **BitMEX** | ✅ Stable | $2B+ | Exchange di derivati consolidato, fino a 100x leva finanziaria, testnet supportato |
| **Phemex** | ✅ Stable | $2B+ | Trading futures senza commissioni, motore ad alte prestazioni, testnet supportato |
| **WOO X** | ✅ Stable | $1.5B+ | Exchange di livello istituzionale, liquidità profonda, testnet supportato |
| **CoinEx** | ✅ Stable | $1B+ | Exchange consolidato (2017), ricco di altcoin, testnet supportato |
| **Bitrue** | ✅ Stable | $1B+ | Exchange principale dell'ecosistema XRP, forte mercato del Sud-Est asiatico, testnet supportato |
| **XT.COM** | ✅ Stable | $800M+ | Exchange emergente, ricco di altcoin, testnet supportato |
| **BTCC** | ✅ Stable | $500M+ | Exchange consolidato (2011), primo exchange Bitcoin della Cina, testnet supportato |
| **AscendEX** | ✅ Stable | $400M+ | Exchange di livello istituzionale, amichevole DeFi, testnet supportato |
| **Poloniex** | ✅ Stable | $300M+ | Exchange consolidato (2014), ricca varietà di monete, testnet supportato |
| **Crypto.com** | ✅ Stable | $500M+ | Marca ben nota, decine di milioni di utenti globalmente, testnet supportato |

## Architettura dei Moduli

```
quantmesh_platform/
├── main.go                    # Punto di ingresso del programma principale, orchestrazione dei componenti
│
├── config/                    # Gestione della configurazione
│   └── config.go              # Caricamento e validazione della configurazione YAML
│
├── exchange/                  # Livello di astrazione exchange (core)
│   ├── interface.go           # Interfaccia unificata IExchange
│   ├── factory.go             # Pattern factory per creare istanze di exchange
│   ├── types.go               # Strutture dati comuni
│   ├── wrapper_*.go           # Adattatori (wrapping exchanges)
│   ├── binance/               # Implementazione Binance
│   ├── bitget/                # Implementazione Bitget
│   └── gate/                  # Implementazione Gate.io
│
├── logger/                    # Sistema di registrazione
│   └── logger.go              # Registrazione file + registrazione console
│
├── monitor/                   # Monitoraggio prezzi
│   └── price_monitor.go       # Flusso prezzi unico globale
│
├── order/                     # Livello di esecuzione ordini
│   └── executor_adapter.go    # Esecutore ordini (limitazione velocità + retry)
│
├── position/                  # Gestione posizioni (core)
│   └── super_position_manager.go  # Gestore super slot
│
├── safety/                    # Sicurezza e controllo del rischio
│   ├── safety.go              # Controlli di sicurezza pre-avvio
│   ├── risk_monitor.go        # Controllo del rischio attivo (monitoraggio K-line)
│   ├── reconciler.go          # Riconciliazione posizioni
│   └── order_cleaner.go       # Pulizia ordini
│
└── utils/                     # Funzioni di utilità
    └── orderid.go             # Generazione ID ordine personalizzato
```

## Migliori Pratiche

1. **Per Status VIP Exchange**: Questo sistema è uno strumento di generazione di volume. Se le fluttuazioni di prezzo non sono grandi, $3.000 di margine possono generare $10 milioni di volume di trading in 2-3 giorni.

2. **Migliore Pratica per il Profitto**: Entra nel mercato dopo un round di declino. Prima compra una posizione, poi avvia il software. Venderà automaticamente grid per grid verso l'alto. Quando la tua posizione è esaurita, ferma il sistema. Se non sei sicuro se il mercato attuale è un punto basso, puoi iniziare senza una posizione base. Se scende ulteriormente, aggiungi una posizione al punto basso e riavvia per continuare a vendere. Questo massimizza i profitti. Ripeti questo ciclo per profitti continui. Non preoccuparti dei declini - il programma abbassa continuamente i costi. Finché si riprende della metà, raggiungi il pareggio.

## 🚀 Inizio Rapido

### Prerequisiti
- Go 1.21 o superiore
- Ambiente di rete in grado di accedere alle API degli exchange

### Installazione

1. **Clona il repository**
   ```bash
   git clone https://github.com/ghostsworm/quantmesh.git
   cd quantmesh
   ```

2. **Installa le dipendenze**
   ```bash
   go mod download
   ```

### Configurazione

> **Runtime SSOT:** The primary database table `app_config` holds the authoritative configuration. One-time YAML import: `./quantmesh --migrate-app-config` (with `QUANTMESH_IMPORT_YAML`, or `config.yaml` in the working directory), or run `./quantmesh /path/to/file.yaml` as the first argument. See [`docs/config-database-design.md`](../config-database-design.md).

1. Copia il file di configurazione di esempio:
   ```bash
   cp config.example.yaml config.yaml
   ```

2. Modifica `config.yaml` e inserisci la tua API Key e i parametri della strategia:

   ```yaml
   app:
     current_exchange: "binance"  # Seleziona exchange

   exchanges:
     binance:
       api_key: "YOUR_API_KEY"
       secret_key: "YOUR_SECRET_KEY"
       fee_rate: 0.0002

   trading:
     symbol: "ETHUSDT"       # Coppia di trading
     price_interval: 2       # Spaziatura grid (prezzo)
     order_quantity: 30     # Importo per grid (USDT)
     buy_window_size: 10    # Numero di ordini di acquisto
     sell_window_size: 10   # Numero di ordini di vendita
   ```

### Utilizzo

#### Modalità Produzione

Esegui il binario compilato:

```bash
go run main.go
```

Oppure compila ed esegui:

```bash
go build -o quantmesh
./quantmesh
```

Il backend servirà i file statici del frontend sulla porta 28888 (predefinita).

#### Modalità Sviluppo

Per lo sviluppo frontend con hot reload e debug del codice sorgente:

**Opzione 1: Usa lo script di sviluppo (Consigliato)**

```bash
./scripts/local/dev.sh
```

Questo script:
- Avvia il server backend Go sulla porta 28888
- Avvia il server dev Vite sulla porta 15173
- Abilita hot reload per le modifiche del codice frontend
- Fornisce source maps per il debug (nessun codice minificato)

Quindi accedi all'applicazione su: **http://localhost:15173**

**Opzione 2: Avvio manuale**

Terminale 1 - Avvia backend Go:
```bash
go run main.go
```

Terminale 2 - Avvia server dev Vite:
```bash
cd webui
pnpm dev
```

Quindi accedi all'applicazione su: **http://localhost:15173**

**Vantaggi Modalità Sviluppo:**
- ✅ Hot reload - Le modifiche del codice frontend sono istantaneamente riflesse
- ✅ Source maps - Debug con codice TypeScript/React originale (non minificato)
- ✅ Fast refresh - I componenti React si aggiornano senza perdere lo stato
- ✅ Messaggi di errore migliori - Vedi nomi file e numeri di riga effettivi

**Nota:** In modalità sviluppo, il server dev Vite fa da proxy alle richieste API (`/api/*`) e alle connessioni WebSocket (`/ws`) al backend Go in esecuzione sulla porta 28888.

## 🏗️ Architettura

Il sistema adotta un design modulare con componenti principali inclusi:

- **Livello Exchange**: Astrazione interfaccia exchange unificata, schermando le differenze API sottostanti.
- **Monitor Prezzi**: Sorgente prezzi WebSocket unica globale, garantendo coerenza delle decisioni.
- **Gestore Posizione Super**: Gestore posizioni principale, gestendo il ciclo di vita degli ordini basato sul meccanismo Slot.
- **Sicurezza & Controllo del Rischio**: Controllo del rischio multi-livello, inclusi controlli di avvio, monitoraggio runtime e interruttore di anomalie.

Per documentazione architetturale più dettagliata, si prega di fare riferimento a [ARCHITECTURE.md](../../ARCHITECTURE.md).

## 📊 Statistiche di Utilizzo e Protezione della Privacy

QuantMesh include una funzionalità opzionale di statistiche di utilizzo per raccogliere dati di utilizzo anonimi, aiutandoci a comprendere l'utilizzo del progetto e migliorare il prodotto. **Tutta la raccolta di dati è completamente trasparente, il codice è verificabile e può essere disabilitato in qualsiasi momento.**

### 🔒 Protezione della Privacy

**Dati che Raccogliamo (Anonimi):**
- ✅ **Informazioni di Base**: Numero di versione, sistema operativo, architettura, ID istanza (UUID generato casualmente)
- ✅ **Statistiche di Utilizzo**: Nomi degli exchange utilizzati, coppie di trading
- ✅ **Metriche di Prestazione**: Latenza richiesta/risposta API, latenza WebSocket
- ✅ **Attività di Trading**: Direzione di trading (acquisto/vendita), escludendo importi di trading

**Dati che NON Raccogliamo:**
- ❌ **Indirizzo IP**: Il frontend ha la cattura IP disabilitata, il backend usa ID istanza invece di IP
- ❌ **Geolocalizzazione**: Nessuna raccolta di latitudine/longitudine, città o altre informazioni di posizione
- ❌ **Informazioni Personali**: Nessuna raccolta di ID utente, email, nomi o qualsiasi informazione di identità
- ❌ **Dati Sensibili**: Nessuna raccolta di chiavi API, importi di trading, saldi account o informazioni di posizione
- ❌ **Dati Finanziari**: Nessuna raccolta di informazioni finanziarie o sensibili di trading

### 🛡️ Misure di Protezione della Privacy

1. **Meccanismo ID Istanza**: Utilizza UUID generato casualmente come identificatore unico, memorizzato nel file `./data/instance_id`, non contiene informazioni personali
2. **IP Frontend Disabilitato**: PostHog SDK configurato con `ip_capture: false`, disabilitando la cattura dell'indirizzo IP e l'inferenza di geolocalizzazione
3. **Backend Non Invia IP**: Il codice backend non invia indirizzi IP al servizio statistiche
4. **Completamente Opzionale**: Gli utenti possono disabilitare le statistiche in qualsiasi momento tramite variabili d'ambiente
5. **Trasparenza del Codice**: Tutto il codice statistiche è verificabile, situato in `utils/telemetry.go`

### ⚙️ Come Disabilitare le Statistiche

**Metodo 1: Variabile d'Ambiente (Consigliato)**
```bash
export QUANTMESH_DISABLE_TELEMETRY=1
```

**Metodo 2: Disabilita Frontend**
Nel file `webui/.env.local`:
```bash
VITE_DISABLE_TELEMETRY=1
```

**Metodo 3: Modifica Codice**
Modifica `utils/telemetry.go`, imposta `Enabled` su `false`

### 📖 Documentazione Dettagliata

Per informazioni più dettagliate sulla funzionalità statistiche, si prega di fare riferimento a:
- 📖 [Guida Completa Statistiche](../../docs/TELEMETRY_GUIDE.md)
- 🔒 [Guida Protezione Privacy](../../docs/TELEMETRY_PRIVACY.md)
- 🚀 [Guida Setup Rapido](../../docs/TELEMETRY_SIMPLE_GUIDE.md)

---

## ⚠️ Disclaimer

Questo software è solo a scopo educativo e di ricerca. Il trading di criptovalute comporta un alto rischio e può comportare perdite di capitale.
- Gli utenti sono i soli responsabili di eventuali profitti o perdite derivanti dall'uso di questo software.
- Testa sempre accuratamente su Testnet prima di utilizzare fondi reali.
- Gli sviluppatori non sono responsabili delle perdite dovute a bug software, latenza di rete o guasti degli exchange.

## 🪙 Supporto Pagamenti Crypto

QuantMesh supporta pagamenti in criptovaluta per abbonamenti e licenze:

### Criptovalute Supportate
- **BTC** (Bitcoin)
- **ETH** (Ethereum)
- **USDT** (Tether, ERC20)
- **USDC** (USD Coin, ERC20)

### Metodi di Pagamento
1. **Coinbase Commerce** (Consigliato)
   - Conferma automatica
   - Supporto per più criptovalute
   - Pagina di pagamento facile

2. **Pagamento Portafoglio Diretto**
   - Nessun coinvolgimento di terze parti
   - Maggiore privacy
   - Conferma manuale (1-24 ore)

### Inizio Rapido
```bash
# Metodo A: Coinbase Commerce (15 minuti)
# 1. Registrati su https://commerce.coinbase.com
# 2. Configura API Key in .env.crypto
# 3. Avvia servizio

# Metodo B: Portafoglio Diretto (5 minuti)
# 1. Configura indirizzi portafoglio
# 2. Avvia servizio
# 3. Conferma manuale
```

### Documentazione
- 📖 [Guida Pagamento Utente](../CRYPTO_PAYMENT_GUIDE.md)
- 🚀 [Guida Inizio Rapido](../CRYPTO_PAYMENT_QUICKSTART.md)
- 🔧 [Guida Setup](../CRYPTO_PAYMENT_SETUP.md)
- 📊 [Riepilogo Implementazione](../reports/CRYPTO_PAYMENT_SUMMARY.md)

### Perché Pagamenti Crypto?
✅ Nessuna carta di credito o conto bancario richiesto  
✅ Accessibilità globale, nessuna restrizione regionale  
✅ Commissioni di transazione più basse (1% vs 2.9%)  
✅ Migliore protezione della privacy  
✅ Conferma rapida (10-30 minuti)  
✅ Perfetto per software di trading crypto  

## 📜 Licenza

Questo progetto utilizza un **modello di Licenza Duale**:

### Licenza Open Source AGPL-3.0
- ✅ Gratis da usare, modificare e distribuire
- ⚠️ **Tutte le opere derivate devono essere open source** e rilasciate sotto AGPL-3.0
- ⚠️ Il codice sorgente deve essere fornito anche per servizi di rete
- ⚠️ Il codice modificato deve essere contribuito alla comunità

### Licenza Commerciale
Se hai bisogno di utilizzare questo software in applicazioni o servizi proprietari, o non desideri rendere open source le tue modifiche, devi acquistare una licenza commerciale.

**Ambito Licenza Commerciale:**
- Utilizzo in applicazioni proprietarie
- Nessun obbligo di rendere open source le modifiche
- Integrare in prodotti proprietari per la distribuzione
- Supporto tecnico prioritario e aggiornamenti

**Inquiry Licenza Commerciale:**
- 📧 Email: contact@quantmesh.io
- 🌐 Website: https://quantmesh.io/commercial

---

### Dettagli Licenza

Questo progetto è sotto doppia licenza:

1. **AGPL-3.0 (GNU Affero General Public License v3.0)**
   - Gratis per uso, modifica e distribuzione
   - Tutte le opere derivate devono essere open source sotto AGPL-3.0
   - Il codice sorgente deve essere fornito a tutti gli utenti, anche per servizi di rete
   - Le modifiche devono essere contribuite alla comunità

2. **Licenza Commerciale**
   - Richiesta per uso proprietario
   - Nessun obbligo di rendere open source le modifiche
   - Include supporto prioritario e aggiornamenti

Per inquiry di licenza commerciale, contatta:
- 📧 Email: contact@quantmesh.io
- 🌐 Website: https://quantmesh.io/commercial

## 🤝 Contribuire

Accogliamo i contributi! Ecco come puoi aiutare:

- ⭐ **Metti una stella a questo repo** se lo trovi utile
- 🍴 **Fai fork e usa** il progetto
- 🐛 **Segnala bug** tramite [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💡 **Suggerisci funzionalità** tramite [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📝 **Invia PR** per miglioramenti
- 📖 **Migliora la documentazione**

**Nota:** Secondo la licenza AGPL-3.0, tutti i contributi a questo progetto saranno rilasciati sotto la stessa licenza AGPL-3.0.

Vedi [CONTRIBUTING.md](../../CONTRIBUTING.md) per linee guida dettagliate.

## 🙏 Ringraziamenti

Grazie al progetto originale [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker) di [dennisyang1986](https://github.com/dennisyang1986) per il loro contributo open source, che ha fornito una solida base per questo progetto. Per maggiori informazioni, si prega di fare riferimento al file [NOTICE](../../NOTICE).

---

## 📞 Contatto & Supporto

- 🌐 **Sito Web**: https://quantmesh.io
- 📧 **Email**: contact@quantmesh.io
- 💬 **Discord**: [Unisciti alla nostra comunità](https://discord.gg/YOUR_INVITE_LINK)
- 🐛 **Issues**: [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💬 **Discussioni**: [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📖 **Documentazione**: [Documentazione Completa](../)

---

<div align="center">
  <strong>Fatto con ❤️ dal Team QuantMesh</strong><br/>
  <sub>Se trovi questo progetto utile, considera di dargli una ⭐</sub>
</div>

Copyright © 2025 QuantMesh Team. All Rights Reserved.

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
