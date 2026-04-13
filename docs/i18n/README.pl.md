<div align="center">
  <img src="../../logo/qm_thick_tail_white.svg" alt="QuantMesh Logo" width="200"/>
  
  # QuantMesh Market Maker
  
  **Wysokoczęstotliwościowy Twórca Rynku Kryptowalut**

  [![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org/dl/)
  [![License](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](../../LICENSE)
  [![GitHub Stars](https://img.shields.io/github/stars/ghostsworm/quantmesh.svg?style=social&label=Stars)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Forks](https://img.shields.io/github/forks/ghostsworm/quantmesh.svg?style=social&label=Forks)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Issues](https://img.shields.io/github/issues/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/issues)
  [![GitHub Release](https://img.shields.io/github/release/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/releases)
  [![Website](https://img.shields.io/badge/Website-quantmesh.io-green.svg)](https://quantmesh.io)
  
  [简体中文](../../README.md) | [繁體中文](README.zh-TW.md) | [English](README.en.md) | [Español](README.es.md) | [Français](README.fr.md) | [Português](README.pt.md) | [Polski](README.pl.md)
</div>

---

## 🎯 Dlaczego wybrać QuantMesh?

| Funkcja | QuantMesh | Inne rozwiązania |
|---------|-----------|----------------|
| **Wsparcie giełd** | 20+ giełd | Zwykle 3-5 |
| **Opóźnienie odpowiedzi** | Poziom milisekund | Poziom sekund |
| **Kontrola ryzyka** | Wielowarstwowa kontrola aktywna | Kontrola podstawowa |
| **Przetestowane w produkcji** | Wolumen handlowy $100M+ | Nieprzetestowane |
| **Interfejs internetowy** | ✅ Pełny interfejs React UI | ❌ Brak/Podstawowy |
| **Open Source** | AGPL-3.0 | Zamknięte źródło/Ograniczone |
| **Dane w czasie rzeczywistym** | Tylko WebSocket | REST polling |
| **Współbieżność** | 1000+ zamówień/sek | Ograniczona |

**Główne zalety:**
- ✅ **Przetestowane w boju**: Udowodnione wolumenem handlowym $100M+
- ✅ **Wysoka wydajność**: Opóźnienie poniżej 10ms z architekturą WebSocket
- ✅ **Kompleksowe**: Kompletne rozwiązanie od handlu do monitorowania
- ✅ **Przejrzyste**: W pełni open source, kod podlegający audytowi
- ✅ **Rozszerzalne**: System wtyczek do dostosowania

---

## 📊 Telemetry

**Third-party analytics (PostHog) has been removed.** The Go backend and install script no longer send usage events. Symbols in `utils/telemetry.go` are no-ops for compatibility.

The Web UI may still load a **1×1 pixel** on startup (`webui/src/services/telemetry.ts`). Disable with `VITE_DISABLE_TELEMETRY=1` in `webui/.env.local` or `localStorage` key `QUANTMESH_DISABLE_TELEMETRY` = `1`.

See [TELEMETRY_GUIDE.md](../../docs/TELEMETRY_GUIDE.md).
---

## ⚠️ Zastrzeżenie

To oprogramowanie jest przeznaczone wyłącznie do celów edukacyjnych i badawczych. Handel kryptowalutami wiąże się z wysokim ryzykiem i może skutkować utratą kapitału.
- Użytkownicy są wyłącznie odpowiedzialni za wszelkie zyski lub straty wynikające z używania tego oprogramowania.
- Zawsze dokładnie testuj na Testnet przed użyciem prawdziwych funduszy.
- Deweloperzy nie ponoszą odpowiedzialności za straty spowodowane błędami oprogramowania, opóźnieniami sieciowymi lub awariami giełd.

## 🪙 Wsparcie płatności krypto

QuantMesh obsługuje płatności kryptowalutowe dla subskrypcji i licencji:

### Obsługiwane kryptowaluty
- **BTC** (Bitcoin)
- **ETH** (Ethereum)
- **USDT** (Tether, ERC20)
- **USDC** (USD Coin, ERC20)

### Metody płatności
1. **Coinbase Commerce** (Zalecane)
   - Automatyczne potwierdzenie
   - Obsługa wielu kryptowalut
   - Łatwa strona płatności

2. **Bezpośrednia płatność portfela**
   - Brak udziału stron trzecich
   - Więcej prywatności
   - Ręczne potwierdzenie (1-24 godziny)

### Szybki start
```bash
# Metoda A: Coinbase Commerce (15 minut)
# 1. Zarejestruj się na https://commerce.coinbase.com
# 2. Skonfiguruj klucz API w .env.crypto
# 3. Uruchom usługę

# Metoda B: Bezpośredni portfel (5 minut)
# 1. Skonfiguruj adresy portfela
# 2. Uruchom usługę
# 3. Ręczne potwierdzenie
```

### Dokumentacja
- 📖 [Przewodnik płatności użytkownika](../CRYPTO_PAYMENT_GUIDE.md)
- 🚀 [Przewodnik szybkiego startu](../CRYPTO_PAYMENT_QUICKSTART.md)
- 🔧 [Przewodnik konfiguracji](../CRYPTO_PAYMENT_SETUP.md)
- 📊 [Podsumowanie implementacji](../reports/CRYPTO_PAYMENT_SUMMARY.md)

### Dlaczego płatności krypto?
✅ Nie wymaga karty kredytowej ani konta bankowego  
✅ Dostępność globalna, brak ograniczeń regionalnych  
✅ Niższe opłaty transakcyjne (1% vs 2.9%)  
✅ Lepsza ochrona prywatności  
✅ Szybkie potwierdzenie (10-30 minut)  
✅ Idealne dopasowanie do oprogramowania handlu krypto  

## 📜 Licencja

Ten projekt używa **modelu podwójnej licencji**:

### Licencja open source AGPL-3.0
- ✅ Darmowe użytkowanie, modyfikacja i dystrybucja
- ⚠️ **Wszystkie dzieła pochodne muszą być open source** i wydane pod AGPL-3.0
- ⚠️ Kod źródłowy musi być udostępniony nawet dla usług sieciowych
- ⚠️ Zmodyfikowany kod musi być zwrócony społeczności

### Licencja komercyjna
Jeśli potrzebujesz użyć tego oprogramowania w aplikacjach lub usługach zastrzeżonych lub nie chcesz udostępnić swoich modyfikacji jako open source, musisz kupić licencję komercyjną.

**Zakres licencji komercyjnej:**
- Użycie w aplikacjach zastrzeżonych
- Brak obowiązku udostępniania modyfikacji jako open source
- Integracja z produktami zastrzeżonymi do dystrybucji
- Priorytetowe wsparcie techniczne i aktualizacje

**Zapytania dotyczące licencji komercyjnej:**
- 📧 Email: contact@quantmesh.io
- 🌐 Strona internetowa: https://quantmesh.io/commercial

---

### Szczegóły licencji

Ten projekt jest objęty podwójną licencją pod:

1. **AGPL-3.0 (GNU Affero General Public License v3.0)**
   - Darmowe użytkowanie, modyfikacja i dystrybucja
   - Wszystkie dzieła pochodne muszą być open source pod AGPL-3.0
   - Kod źródłowy musi być udostępniony wszystkim użytkownikom, nawet dla usług sieciowych
   - Modyfikacje muszą być zwrócone społeczności

2. **Licencja komercyjna**
   - Wymagana do użytku zastrzeżonego
   - Brak obowiązku udostępniania modyfikacji jako open source
   - Obejmuje priorytetowe wsparcie i aktualizacje

W sprawie zapytań dotyczących licencji komercyjnej skontaktuj się:
- 📧 Email: contact@quantmesh.io
- 🌐 Strona internetowa: https://quantmesh.io/commercial

## 🤝 Współtworzenie

Witamy wkład! Oto jak możesz pomóc:

- ⭐ **Oznacz gwiazdką to repozytorium**, jeśli uważasz je za pomocne
- 🍴 **Sforkuj i użyj** projektu
- 🐛 **Zgłoś błędy** przez [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💡 **Zaproponuj funkcje** przez [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📝 **Prześlij PR** dla ulepszeń
- 📖 **Ulepsz dokumentację**

**Uwaga:** Zgodnie z licencją AGPL-3.0, wszystkie wkłady do tego projektu będą wydane pod tą samą licencją AGPL-3.0.

Zobacz [CONTRIBUTING.md](../CONTRIBUTING.md) dla szczegółowych wytycznych.

## 🙏 Podziękowania

Dziękujemy oryginalnemu projektowi [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker) autorstwa [dennisyang1986](https://github.com/dennisyang1986) za ich wkład open source, który zapewnił solidne fundamenty dla tego projektu. Aby uzyskać więcej informacji, zapoznaj się z plikiem [NOTICE](../../NOTICE).

---

## 📞 Kontakt i wsparcie

- 🌐 **Strona internetowa**: https://quantmesh.io
- 📧 **Email**: contact@quantmesh.io
- 💬 **Discord**: [Dołącz do naszej społeczności](https://discord.gg/YOUR_INVITE_LINK)
- 🐛 **Issues**: [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💬 **Dyskusje**: [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📖 **Dokumentacja**: [Pełna dokumentacja](../)

---

<div align="center">
  <strong>Stworzone z ❤️ przez zespół QuantMesh</strong><br/>
  <sub>Jeśli uważasz ten projekt za pomocny, rozważ nadanie mu ⭐</sub>
</div>

Copyright © 2025 QuantMesh Team. Wszelkie prawa zastrzeżone.

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
