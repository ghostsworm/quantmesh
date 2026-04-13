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

## 📊 Telemetry

**Third-party analytics (PostHog) has been removed.** The Go backend and install script no longer send usage events. Symbols in `utils/telemetry.go` are no-ops for compatibility.

The Web UI may still load a **1×1 pixel** on startup (`webui/src/services/telemetry.ts`). Disable with `VITE_DISABLE_TELEMETRY=1` in `webui/.env.local` or `localStorage` key `QUANTMESH_DISABLE_TELEMETRY` = `1`.

See [TELEMETRY_GUIDE.md](../../docs/TELEMETRY_GUIDE.md).
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

Vedi [CONTRIBUTING.md](../CONTRIBUTING.md) per linee guida dettagliate.

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
