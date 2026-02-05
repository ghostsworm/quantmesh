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
  
  [繁體中文](../../README.md) | [简体中文](README.zh-Hans.md) | [English](README.en.md) | [Español](README.es.md) | [Français](README.fr.md) | [Português](README.pt.md) | [Polski](README.pl.md)
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

## 📊 Metryki wydajności

- **Wolumen handlowy**: $100M+ przetestowane w produkcji
- **Opóźnienie odpowiedzi**: <10ms (napędzane WebSocket)
- **Obsługiwane giełdy**: 20+
- **Przetwarzanie współbieżne**: 1000+ zamówień/sekundę
- **Dostępność systemu**: 99.9%+
- **Dzienna zdolność handlowa**: $3M+ dziennie (przykład: ETHUSDC)

---

## 📖 Wprowadzenie

QuantMesh to wysokowydajny, niskiego opóźnienia system twórcy rynku kryptowalut skupiający się na długich strategiach handlu siatką dla rynków kontraktów wieczystych. Opracowany w Go i napędzany strumieniami danych WebSocket w czasie rzeczywistym, ma na celu zapewnienie stabilnego wsparcia płynności dla głównych giełd takich jak Binance, Bitget i Gate.io.

Po kilku iteracjach użyliśmy tego systemu do handlu ponad $100 milionów w walucie wirtualnej. Na przykład, handel Binance ETHUSDC z zerowymi opłatami, interwałem cenowym $1 i $300 na zamówienie, dzienny wolumen handlowy może przekroczyć $3 miliony i ponad $50 milionów miesięcznie. Tak długo, jak rynek oscyluje lub ma tendencję wzrostową, będzie nadal generować zyski. Jeśli rynek spadnie jednostronnie, $30,000 marży może zagwarantować brak likwidacji dla spadku o 1000 punktów. Poprzez ciągły handel w celu obniżenia kosztów, odzyskanie o 50% wystarczy, aby osiągnąć próg rentowności, a powrót do pierwotnej ceny otwarcia może przynieść znaczne zyski. Jeśli nastąpi jednostronny szybki spadek, aktywny system kontroli ryzyka automatycznie zidentyfikuje i natychmiast zatrzyma handel, pozwalając tylko na kontynuację zamówień, gdy rynek się odbije, bez obaw o likwidację z powodu skoków cen.

Przykład: Rozpoczęcie handlu ETH na poziomie 3000 punktów, cena spada do 2700 punktów, tracąc około $3,000. Gdy cena odbije się powyżej 2850 punktów, osiąga próg rentowności. Powrót do 3000 punktów, zyski wahają się od $1,000 do $3,000.

## 📜 Pochodzenie projektu

Ten projekt został pierwotnie opracowany na podstawie [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker), opublikowanego przez [dennisyang1986](https://github.com/dennisyang1986) na licencji MIT.

Na podstawie oryginalnego projektu wprowadziliśmy następujące główne ulepszenia i rozszerzenia:

- ✨ **Kompletny interfejs Frontend**: Dodano interfejs zarządzania internetowego React + TypeScript zapewniający wizualne monitorowanie handlu, zarządzanie konfiguracją i analizę danych
- 🏦 **Rozszerzenie giełd**: Rozszerzono z 3 giełd (Binance, Bitget, Gate.io) w oryginalnym projekcie do **20+ głównych giełd**
- 🔒 **Stabilność na poziomie finansowym**: Kompleksowo poprawiono niezawodność systemu, w tym kompleksową obsługę błędów, mechanizmy bezpieczeństwa współbieżności, gwarancje spójności danych, automatyczne odzyskiwanie itp.
- 📊 **Ulepszone monitorowanie**: Ulepszony system rejestrowania, zbieranie metryk (Prometheus), kontrole zdrowia i alerty w czasie rzeczywistym
- 🛡️ **Wzmocniona kontrola ryzyka**: Wielowarstwowe monitorowanie ryzyka, automatyczne uzgadnianie, wyłącznik anomalii i ochrona bezpieczeństwa funduszy
- 🔌 **System wtyczek**: Wsparcie dla rozszerzalnych mechanizmów wtyczek do łatwego dostosowania i rozwoju wtórnego
- 📱 **Wsparcie internacjonalizacji**: Wielojęzyczny interfejs (chiński/angielski), wsparcie i18n
- 🧪 **Wsparcie Testnet**: Wsparcie dla środowisk testnet wielu giełd do rozwoju i testowania

Szczegółowe opisy ulepszeń i informacje o oprogramowaniu stron trzecich można znaleźć w pliku [NOTICE](../../NOTICE).

**Ważna uwaga**: Ten projekt jest teraz dystrybuowany pod **GNU Affero General Public License v3.0 (AGPL-3.0)**. Zgodnie z wymaganiami licencji MIT oryginalnego projektu zachowaliśmy uznanie oryginalnego projektu.

## ✨ Kluczowe funkcje

- **Wsparcie wielu giełd**: Zgodne z Binance, Bitget, Gate.io, Bybit, EdgeX i innymi głównymi platformami.
- **Odpowiedź na poziomie milisekund**: W pełni napędzane WebSocket (dane rynkowe i przepływ zamówień), eliminując opóźnienia polling.
- **Inteligentna strategia siatki**: 
  - **Tryb stałej kwoty**: Bardziej kontrolowalne wykorzystanie kapitału.
  - **System Super Slot**: Inteligentnie zarządza stanami zamówień i pozycji, zapobiegając konfliktom współbieżności.
- **Potężny system kontroli ryzyka**:
  - **Aktywna kontrola ryzyka**: Monitorowanie w czasie rzeczywistym anomalii wolumenu K-line, automatyczne wstrzymywanie handlu.
  - **Bezpieczeństwo funduszy**: Automatycznie sprawdza saldo, dźwignię i maksymalne ryzyko pozycji przed uruchomieniem.
  - **Automatyczne uzgadnianie**: Regularnie synchronizuje stany lokalne i giełdowe, aby zapewnić spójność danych.
- **Architektura wysokiej współbieżności**: Wydajny model współbieżności oparty na Goroutine + Channel + Sync.Map.

## 🏦 Obsługiwane giełdy

| Giełda | Status | Dzienna objętość handlowa | Uwagi |
|----------|--------|------------------------|-------|
| **Binance** | ✅ Stable | $50B+ | Największa giełda na świecie |
| **Bitget** | ✅ Stable | $10B+ | Główna platforma handlu kontraktami terminowymi |
| **Gate.io** | ✅ Stable | $5B+ | Ustalona giełda |
| **OKX** | ✅ Stable | $20B+ | Top 3 globalnie, silna baza użytkowników chińskich |
| **Bybit** | ✅ Stable | $15B+ | Główna platforma handlu kontraktami terminowymi |
| **Huobi (HTX)** | ✅ Stable | $5B+ | Ustalona giełda, silny rynek chiński |
| **KuCoin** | ✅ Stable | $3B+ | Bogate altcoiny, wsparcie kontraktów terminowych |
| **Kraken** | ✅ Stable | $2B+ | Silne zgodność, główny nurt w Europie i Ameryce |
| **Bitfinex** | ✅ Stable | $1B+ | Ustalona giełda, dobra płynność |
| **MEXC** | ✅ Stable | $8B+ | Duży wolumen handlu kontraktami terminowymi, bogate altcoiny, wsparcie testnet |
| **BingX** | ✅ Stable | $3B+ | Platforma handlu społecznościowego, dobre doświadczenie kontraktów terminowych, wsparcie testnet |
| **Deribit** | ✅ Stable | $2B+ | Największa giełda opcji na świecie, wspiera kontrakty terminowe + opcje, wsparcie testnet |
| **BitMEX** | ✅ Stable | $2B+ | Ustalona giełda instrumentów pochodnych, dźwignia do 100x, wsparcie testnet |
| **Phemex** | ✅ Stable | $2B+ | Handlowanie kontraktami terminowymi bez opłat, silnik wysokiej wydajności, wsparcie testnet |
| **WOO X** | ✅ Stable | $1.5B+ | Giełda na poziomie instytucjonalnym, głęboka płynność, wsparcie testnet |
| **CoinEx** | ✅ Stable | $1B+ | Ustalona giełda (2017), bogate altcoiny, wsparcie testnet |
| **Bitrue** | ✅ Stable | $1B+ | Główna giełda ekosystemu XRP, silny rynek południowo-wschodniej Azji, wsparcie testnet |
| **XT.COM** | ✅ Stable | $800M+ | Rozwijająca się giełda, bogate altcoiny, wsparcie testnet |
| **BTCC** | ✅ Stable | $500M+ | Ustalona giełda (2011), pierwsza giełda Bitcoin w Chinach, wsparcie testnet |
| **AscendEX** | ✅ Stable | $400M+ | Giełda na poziomie instytucjonalnym, przyjazna DeFi, wsparcie testnet |
| **Poloniex** | ✅ Stable | $300M+ | Ustalona giełda (2014), bogata różnorodność monet, wsparcie testnet |
| **Crypto.com** | ✅ Stable | $500M+ | Znana marka, dziesiątki milionów użytkowników globalnie, wsparcie testnet |

## Architektura modułów

```
quantmesh_platform/
├── main.go                    # Punkt wejścia programu głównego, orkiestracja komponentów
│
├── config/                    # Zarządzanie konfiguracją
│   └── config.go              # Ładowanie i walidacja konfiguracji YAML
│
├── exchange/                  # Warstwa abstrakcji giełdy (rdzeń)
│   ├── interface.go           # Ujednolicony interfejs IExchange
│   ├── factory.go             # Wzorzec fabryki do tworzenia instancji giełdy
│   ├── types.go               # Wspólne struktury danych
│   ├── wrapper_*.go           # Adaptery (opakowujące giełdy)
│   ├── binance/               # Implementacja Binance
│   ├── bitget/                # Implementacja Bitget
│   └── gate/                  # Implementacja Gate.io
│
├── logger/                    # System rejestrowania
│   └── logger.go              # Rejestrowanie plików + rejestrowanie konsoli
│
├── monitor/                   # Monitorowanie cen
│   └── price_monitor.go       # Globalny unikalny strumień cen
│
├── order/                     # Warstwa wykonywania zamówień
│   └── executor_adapter.go    # Wykonawca zamówień (ograniczenie szybkości + ponowienie)
│
├── position/                  # Zarządzanie pozycjami (rdzeń)
│   └── super_position_manager.go  # Menedżer super slotów
│
├── safety/                    # Bezpieczeństwo i kontrola ryzyka
│   ├── safety.go              # Kontrole bezpieczeństwa przed uruchomieniem
│   ├── risk_monitor.go        # Aktywna kontrola ryzyka (monitorowanie K-line)
│   ├── reconciler.go          # Uzgadnianie pozycji
│   └── order_cleaner.go       # Czyszczenie zamówień
│
└── utils/                     # Funkcje narzędziowe
    └── orderid.go             # Generowanie niestandardowego ID zamówienia
```

## Najlepsze praktyki

1. **Dla statusu VIP giełdy**: Ten system jest narzędziem generowania wolumenu. Jeśli wahania cen nie są duże, $3,000 marży może wygenerować $10 milionów wolumenu handlowego w 2-3 dni.

2. **Najlepsza praktyka dla zysku**: Wejdź na rynek po rundzie spadku. Najpierw kup pozycję, następnie uruchom oprogramowanie. Automatycznie będzie sprzedawać siatkę po siatce w górę. Gdy Twoja pozycja zostanie wyprzedana, zatrzymaj system. Jeśli nie jesteś pewien, czy obecny rynek jest punktem niskim, możesz zacząć bez pozycji bazowej. Jeśli spadnie dalej, dodaj pozycję w punkcie niskim i uruchom ponownie, aby kontynuować sprzedaż. To maksymalizuje zyski. Powtarzaj ten cykl, aby ciągle osiągać zyski. Nie martw się o spadki - program ciągle obniża koszty. Tak długo, jak odbije się o połowę, osiągasz próg rentowności.

## 🚀 Rozpoczęcie

### Wymagania wstępne
- Go 1.21 lub wyższy
- Środowisko sieciowe zdolne do dostępu do API giełd

### Instalacja

1. **Sklonuj repozytorium**
   ```bash
   git clone https://github.com/ghostsworm/quantmesh.git
   cd quantmesh
   ```

2. **Zainstaluj zależności**
   ```bash
   go mod download
   ```

### Konfiguracja

1. Skopiuj przykładowy plik konfiguracyjny:
   ```bash
   cp config.example.yaml config.yaml
   ```

2. Edytuj `config.yaml` i wypełnij swój klucz API i parametry strategii:

   ```yaml
   app:
     current_exchange: "binance"  # Wybierz giełdę

   exchanges:
     binance:
       api_key: "YOUR_API_KEY"
       secret_key: "YOUR_SECRET_KEY"
       fee_rate: 0.0002

   trading:
     symbol: "ETHUSDT"       # Para handlowa
     price_interval: 2       # Odstęp siatki (cena)
     order_quantity: 30     # Kwota na siatkę (USDT)
     buy_window_size: 10    # Liczba zamówień kupna
     sell_window_size: 10   # Liczba zamówień sprzedaży
   ```

### Użycie

#### Tryb produkcyjny

Uruchom skompilowany plik binarny:

```bash
go run main.go
```

Lub zbuduj i uruchom:

```bash
go build -o quantmesh
./quantmesh
```

Backend będzie obsługiwał statyczne pliki frontend na porcie 28888 (domyślnie).

#### Tryb deweloperski

Dla rozwoju frontend z hot reload i debugowaniem kodu źródłowego:

**Opcja 1: Użyj skryptu deweloperskiego (Zalecane)**

```bash
./dev.sh
```

Ten skrypt:
- Uruchomi serwer backend Go na porcie 28888
- Uruchomi serwer dev Vite na porcie 15173
- Włączy hot reload dla zmian kodu frontend
- Zapewni mapy źródłowe do debugowania (brak zminifikowanego kodu)

Następnie uzyskaj dostęp do aplikacji pod adresem: **http://localhost:15173**

**Opcja 2: Ręczne uruchomienie**

Terminal 1 - Uruchom backend Go:
```bash
go run main.go
```

Terminal 2 - Uruchom serwer dev Vite:
```bash
cd webui
pnpm dev
```

Następnie uzyskaj dostęp do aplikacji pod adresem: **http://localhost:15173**

**Korzyści trybu deweloperskiego:**
- ✅ Hot reload - Zmiany kodu frontend są natychmiast odzwierciedlane
- ✅ Mapy źródłowe - Debuguj z oryginalnym kodem TypeScript/React (nie zminifikowanym)
- ✅ Szybkie odświeżanie - Komponenty React aktualizują się bez utraty stanu
- ✅ Lepsze komunikaty o błędach - Zobacz rzeczywiste nazwy plików i numery linii

**Uwaga:** W trybie deweloperskim serwer dev Vite proxy żądania API (`/api/*`) i połączenia WebSocket (`/ws`) do backendu Go działającego na porcie 28888.

## 🏗️ Architektura

System przyjmuje modułowy projekt z głównymi komponentami, w tym:

- **Warstwa giełdy**: Ujednolicona abstrakcja interfejsu giełdy, osłaniająca różnice API podstawowe.
- **Monitor cen**: Globalne unikalne źródło cen WebSocket, zapewniające spójność decyzji.
- **Menedżer pozycji Super**: Główny menedżer pozycji, zarządzający cyklem życia zamówień opartym na mechanizmie Slot.
- **Bezpieczeństwo i kontrola ryzyka**: Wielowarstwowa kontrola ryzyka, w tym kontrole uruchomienia, monitorowanie czasu wykonania i wyłącznik anomalii.

Aby uzyskać bardziej szczegółową dokumentację architektury, zapoznaj się z [ARCHITECTURE.md](../../ARCHITECTURE.md).

## 📊 Statystyki użycia i ochrona prywatności

QuantMesh zawiera opcjonalną funkcję statystyk użycia do zbierania anonimowych danych użycia, pomagając nam zrozumieć użycie projektu i ulepszyć produkt. **Wszystkie zbieranie danych jest całkowicie przejrzyste, kod podlega audytowi i może być wyłączony w dowolnym momencie.**

### 🔒 Ochrona prywatności

**Dane, które zbieramy (Anonimowe):**
- ✅ **Informacje podstawowe**: Numer wersji, system operacyjny, architektura, ID instancji (losowo wygenerowany UUID)
- ✅ **Statystyki użycia**: Używane nazwy giełd, pary handlowe
- ✅ **Metryki wydajności**: Opóźnienie żądania/odpowiedzi API, opóźnienie WebSocket
- ✅ **Aktywność handlowa**: Kierunek handlu (kupno/sprzedaż), z wyłączeniem kwot handlowych

**Dane, których NIE zbieramy:**
- ❌ **Adres IP**: Frontend ma wyłączone przechwytywanie IP, backend używa ID instancji zamiast IP
- ❌ **Lokalizacja geograficzna**: Brak zbierania szerokości/długości geograficznej, miasta lub innych informacji o lokalizacji
- ❌ **Informacje osobowe**: Brak zbierania ID użytkowników, e-maili, nazwisk lub jakichkolwiek informacji tożsamościowych
- ❌ **Dane wrażliwe**: Brak zbierania kluczy API, kwot handlowych, sald kont lub informacji o pozycjach
- ❌ **Dane finansowe**: Brak zbierania jakichkolwiek informacji finansowych lub wrażliwych handlowych

### 🛡️ Środki ochrony prywatności

1. **Mechanizm ID instancji**: Używa losowo wygenerowanego UUID jako unikalnego identyfikatora, przechowywanego w pliku `./data/instance_id`, nie zawiera informacji osobistych
2. **IP Frontend wyłączony**: PostHog SDK skonfigurowany z `ip_capture: false`, wyłączając przechwytywanie adresu IP i wnioskowanie o lokalizacji geograficznej
3. **Backend nie wysyła IP**: Kod backend nie wysyła adresów IP do usługi statystyk
4. **Całkowicie opcjonalne**: Użytkownicy mogą wyłączyć statystyki w dowolnym momencie poprzez zmienne środowiskowe
5. **Przejrzystość kodu**: Wszystkie kody statystyk podlegają audytowi, znajdują się w `utils/telemetry.go`

### ⚙️ Jak wyłączyć statystyki

**Metoda 1: Zmienna środowiskowa (Zalecane)**
```bash
export QUANTMESH_DISABLE_TELEMETRY=1
```

**Metoda 2: Wyłącz Frontend**
W pliku `webui/.env.local`:
```bash
VITE_DISABLE_TELEMETRY=1
```

**Metoda 3: Zmodyfikuj kod**
Edytuj `utils/telemetry.go`, ustaw `Enabled` na `false`

### 📖 Szczegółowa dokumentacja

Aby uzyskać bardziej szczegółowe informacje o funkcji statystyk, zapoznaj się z:
- 📖 [Kompletny przewodnik statystyk](../../docs/TELEMETRY_GUIDE.md)
- 🔒 [Przewodnik ochrony prywatności](../../docs/TELEMETRY_PRIVACY.md)
- 🚀 [Przewodnik szybkiej konfiguracji](../../docs/TELEMETRY_SIMPLE_GUIDE.md)

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

Zobacz [CONTRIBUTING.md](../../CONTRIBUTING.md) dla szczegółowych wytycznych.

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
