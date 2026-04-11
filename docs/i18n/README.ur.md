<div align="center">
  <img src="../../logo/qm_thick_tail_white.svg" alt="QuantMesh Logo" width="200"/>
  
  # QuantMesh Market Maker
  
  **ہائی فریکوئنسی کریپٹو مارکیٹ میکر**

  [![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org/dl/)
  [![License](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](../../LICENSE)
  [![GitHub Stars](https://img.shields.io/github/stars/ghostsworm/quantmesh.svg?style=social&label=Stars)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Forks](https://img.shields.io/github/forks/ghostsworm/quantmesh.svg?style=social&label=Forks)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Issues](https://img.shields.io/github/issues/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/issues)
  [![GitHub Release](https://img.shields.io/github/release/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/releases)
  [![Website](https://img.shields.io/badge/Website-quantmesh.io-green.svg)](https://quantmesh.io)
  
  [简体中文](../../README.md) | [繁體中文](README.zh-TW.md) | [English](README.en.md) | [Español](README.es.md) | [Français](README.fr.md) | [Português](README.pt.md) | [اردو](README.ur.md)
</div>

---

## 🎯 QuantMesh کیوں منتخب کریں؟

| خصوصیت | QuantMesh | دیگر حل |
|---------|-----------|----------------|
| **ایکسچینج سپورٹ** | 20+ ایکسچینجز | عام طور پر 3-5 |
| **ردعمل کی تاخیر** | ملی سیکنڈ کی سطح | سیکنڈ کی سطح |
| **خطرے کا کنٹرول** | کثیر-تہہ فعال کنٹرول | بنیادی کنٹرول |
| **پروڈکشن ٹیسٹ شدہ** | $100M+ ٹریڈنگ والیوم | غیر ٹیسٹ شدہ |
| **ویب انٹرفیس** | ✅ مکمل React UI | ❌ کوئی نہیں/بنیادی |
| **اوپن سورس** | AGPL-3.0 | بند سورس/محدود |
| **ریئل-ٹائم ڈیٹا** | صرف WebSocket | REST polling |
| **سمورتی** | 1000+ آرڈرز/سیکنڈ | محدود |

**اہم فوائد:**
- ✅ **جنگ-ٹیسٹ شدہ**: $100M+ ٹریڈنگ والیوم کے ساتھ ثابت شدہ
- ✅ **اعلی کارکردگی**: WebSocket آرکیٹیکچر کے ساتھ سب-10ms تاخیر
- ✅ **جامع**: ٹریڈنگ سے مانیٹرنگ تک مکمل حل
- ✅ **شفاف**: مکمل طور پر اوپن سورس، قابل آڈٹ کوڈ
- ✅ **توسیع پذیر**: حسب ضرورت کے لیے پلگ ان سسٹم

---

## 📊 کارکردگی کے میٹرکس

- **ٹریڈنگ والیوم**: $100M+ پروڈکشن ٹیسٹ شدہ
- **ردعمل کی تاخیر**: <10ms (WebSocket-چلایا گیا)
- **سپورٹ شدہ ایکسچینجز**: 20+
- **سمورتی پروسیسنگ**: 1000+ آرڈرز/سیکنڈ
- **سسٹم کی دستیابی**: 99.9%+
- **یومیہ ٹریڈنگ کی صلاحیت**: $3M+ فی دن (مثال: ETHUSDT)

---

## 📖 تعارف

QuantMesh ایک اعلی کارکردگی، کم تاخیر والا کریپٹو کرنسی مارکیٹ میکر سسٹم ہے جو perpetual contract مارکیٹس کے لیے لمبی گرڈ ٹریڈنگ کی حکمت عملیوں پر توجہ مرکوز کرتا ہے۔ Go میں تیار کیا گیا اور WebSocket ریئل-ٹائم ڈیٹا سٹریمز کے ذریعے چلایا گیا، اس کا مقصد Binance، Bitget اور Gate.io جیسے بڑے ایکسچینجز کے لیے مستحکم لیکویڈیٹی سپورٹ فراہم کرنا ہے۔

کئی iterations کے بعد، ہم نے اس سسٹم کا استعمال کرتے ہوئے $100 ملین سے زیادہ ورچوئل کرنسی میں ٹریڈ کیا ہے۔ مثال کے طور پر، Binance ETHUSDT کو صفر فیس کے ساتھ ٹریڈ کرنا، $1 کی قیمت کا وقفہ، اور فی آرڈر $300، یومیہ ٹریڈنگ والیوم $3 ملین سے تجاوز کر سکتا ہے، اور ماہانہ $50 ملین سے زیادہ۔ جب تک مارکیٹ oscillating یا اوپر کی طرف trending ہے، یہ منافع پیدا کرتا رہے گا۔ اگر مارکیٹ یکطرفہ طور پر گرتی ہے، تو $30,000 مارجن 1000 پوائنٹس کی گراوٹ کے لیے کوئی liquidation کی ضمانت دے سکتا ہے۔ لاگت کم کرنے کے لیے مسلسل ٹریڈنگ کے ذریعے، 50% کی بحالی break-even پہنچنے کے لیے کافی ہے، اور اصل opening price پر واپس آنا کافی منافع پیدا کر سکتا ہے۔ اگر یکطرفہ تیز گراوٹ ہے، تو فعال خطرے کا کنٹرول سسٹم خودکار طور پر شناخت کرے گا اور فوری طور پر ٹریڈنگ روک دے گا، صرف اس وقت مسلسل آرڈرز کی اجازت دے گا جب مارکیٹ بحال ہو، قیمت کے spikes سے liquidation کے بارے میں فکر کیے بغیر۔

مثال: 3000 پوائنٹس پر ETH ٹریڈنگ شروع کرنا، قیمت 2700 پوائنٹس پر گرتی ہے، تقریباً $3,000 کا نقصان۔ جب قیمت 2850 پوائنٹس سے اوپر بحال ہوتی ہے، تو یہ break-even پہنچ جاتی ہے۔ 3000 پوائنٹس پر واپس آنا، منافع $1,000 سے $3,000 تک مختلف ہوتا ہے۔

## 📜 پروجیکٹ کی اصل

یہ پروجیکٹ اصل میں [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker) کی بنیاد پر تیار کیا گیا تھا، [dennisyang1986](https://github.com/dennisyang1986) کے ذریعے MIT لائسنس کے تحت شائع کیا گیا۔

اصل پروجیکٹ کی بنیاد پر، ہم نے درج ذیل اہم بہتریوں اور توسیعات کی ہیں:

- ✨ **مکمل فرنٹ اینڈ انٹرفیس**: ایک React + TypeScript ویب مینجمنٹ انٹرفیس شامل کیا گیا جو بصری ٹریڈنگ مانیٹرنگ، کنفیگریشن مینجمنٹ اور ڈیٹا تجزیہ فراہم کرتا ہے
- 🏦 **ایکسچینج توسیع**: اصل پروجیکٹ میں 3 ایکسچینجز (Binance، Bitget، Gate.io) سے **20+ بڑے ایکسچینجز** تک توسیع
- 🔒 **مالی-درجے کی استحکام**: سسٹم کی بھروسے مندی کو مکمل طور پر بہتر بنایا گیا، جس میں جامع خرابی کا انتظام، concurrency حفاظتی میکانزم، ڈیٹا مستقلت کی ضمانتیں، خودکار بحالی وغیرہ شامل ہیں
- 📊 **بہتر مانیٹرنگ**: بہتر لاگنگ سسٹم، میٹرکس جمع (Prometheus)، صحت کی جانچ اور ریئل-ٹائم انتباہات
- 🛡️ **مضبوط خطرے کا کنٹرول**: کثیر-تہہ خطرے کی مانیٹرنگ، خودکار reconciliation، anomaly circuit breaker اور فنڈ کی حفاظت
- 🔌 **پلگ ان سسٹم**: آسان حسب ضرورت اور ثانوی ترقی کے لیے توسیع پذیر پلگ ان میکانزم کے لیے سپورٹ
- 📱 **بین الاقوامی کاری کی سپورٹ**: کثیر-زبان انٹرفیس (چینی/انگریزی)، i18n سپورٹ
- 🧪 **ٹیسٹ نیٹ سپورٹ**: ترقی اور ٹیسٹنگ کے لیے متعدد ایکسچینجز کے ٹیسٹ نیٹ ماحول کے لیے سپورٹ

تفصیلی بہتری کی تفصیلات اور تیسری پارٹی سافٹ ویئر کی معلومات کے لیے، براہ کرم [NOTICE](../../NOTICE) فائل دیکھیں۔

**اہم نوٹ**: یہ پروجیکٹ اب **GNU Affero General Public License v3.0 (AGPL-3.0)** کے تحت تقسیم کیا جاتا ہے۔ اصل پروجیکٹ کی MIT لائسنس کی ضروریات کے مطابق، ہم نے اصل پروجیکٹ کی تسلیم کو برقرار رکھا ہے۔

## ✨ اہم خصوصیات

- **کثیر-ایکسچینج سپورٹ**: Binance، Bitget، Gate.io، Bybit، EdgeX اور دیگر بڑے پلیٹ فارمز کے ساتھ مطابقت پذیر۔
- **ملی سیکنڈ-سطح کا ردعمل**: مکمل طور پر WebSocket-چلایا گیا (مارکیٹ ڈیٹا اور آرڈر فلو)، polling تاخیر کو ختم کرتا ہے۔
- **ذہین گرڈ حکمت عملی**: 
  - **فکسڈ رقم موڈ**: زیادہ قابل کنٹرول سرمایہ کا استعمال۔
  - **سپر سلاٹ سسٹم**: ذہینی سے آرڈر اور پوزیشن کی حالتوں کا انتظام کرتا ہے، concurrency conflicts کو روکتا ہے۔
- **طاقتور خطرے کا کنٹرول سسٹم**:
  - **فعال خطرے کا کنٹرول**: K-line والیوم anomalies کی ریئل-ٹائم مانیٹرنگ، خودکار طور پر ٹریڈنگ کو روکتا ہے۔
  - **فنڈ کی حفاظت**: شروع ہونے سے پہلے خودکار طور پر بیلنس، leverage اور زیادہ سے زیادہ پوزیشن کے خطرے کو چیک کرتا ہے۔
  - **خودکار reconciliation**: ڈیٹا مستقلت کو یقینی بنانے کے لیے مقامی اور ایکسچینج کی حالتوں کو باقاعدگی سے sync کرتا ہے۔
- **اعلی-سمورتی آرکیٹیکچر**: Goroutine + Channel + Sync.Map پر مبنی موثر سمورتی ماڈل۔

## 🏦 سپورٹ شدہ ایکسچینجز

| ایکسچینج | حالت | یومیہ ٹریڈنگ والیوم | نوٹس |
|----------|--------|------------------------|-------|
| **Binance** | ✅ Stable | $50B+ | دنیا کا سب سے بڑا ایکسچینج |
| **Bitget** | ✅ Stable | $10B+ | مین اسٹریم فیوچرز ٹریڈنگ پلیٹ فارم |
| **Gate.io** | ✅ Stable | $5B+ | قائم شدہ ایکسچینج |
| **OKX** | ✅ Stable | $20B+ | عالمی سطح پر ٹاپ 3، مضبوط چینی صارفین کا بیس |
| **Bybit** | ✅ Stable | $15B+ | مین اسٹریم فیوچرز ٹریڈنگ پلیٹ فارم |
| **Huobi (HTX)** | ✅ Stable | $5B+ | قائم شدہ ایکسچینج، مضبوط چینی مارکیٹ |
| **KuCoin** | ✅ Stable | $3B+ | بھرپور altcoins، فیوچرز کنٹریکٹ سپورٹ |
| **Kraken** | ✅ Stable | $2B+ | مضبوط تعمیل، یورپ اور امریکہ میں مین اسٹریم |
| **Bitfinex** | ✅ Stable | $1B+ | قائم شدہ ایکسچینج، اچھی liquidity |
| **MEXC** | ✅ Stable | $8B+ | بڑا فیوچرز ٹریڈنگ والیوم، بھرپور altcoins، testnet سپورٹ شدہ |
| **BingX** | ✅ Stable | $3B+ | سوشل ٹریڈنگ پلیٹ فارم، اچھا فیوچرز تجربہ، testnet سپورٹ شدہ |
| **Deribit** | ✅ Stable | $2B+ | دنیا کا سب سے بڑا آپشنز ایکسچینج، فیوچرز + آپشنز سپورٹ کرتا ہے، testnet سپورٹ شدہ |
| **BitMEX** | ✅ Stable | $2B+ | قائم شدہ derivatives ایکسچینج، 100x تک leverage، testnet سپورٹ شدہ |
| **Phemex** | ✅ Stable | $2B+ | صفر-فی فیوچرز ٹریڈنگ، اعلی کارکردگی انجن، testnet سپورٹ شدہ |
| **WOO X** | ✅ Stable | $1.5B+ | ادارتی-درجے کا ایکسچینج، گہری liquidity، testnet سپورٹ شدہ |
| **CoinEx** | ✅ Stable | $1B+ | قائم شدہ ایکسچینج (2017)، بھرپور altcoins، testnet سپورٹ شدہ |
| **Bitrue** | ✅ Stable | $1B+ | XRP ecosystem کا اہم ایکسچینج، مضبوط جنوب مشرقی ایشیائی مارکیٹ، testnet سپورٹ شدہ |
| **XT.COM** | ✅ Stable | $800M+ | ابھرتا ہوا ایکسچینج، بھرپور altcoins، testnet سپورٹ شدہ |
| **BTCC** | ✅ Stable | $500M+ | قائم شدہ ایکسچینج (2011)، چین کا پہلا Bitcoin ایکسچینج، testnet سپورٹ شدہ |
| **AscendEX** | ✅ Stable | $400M+ | ادارتی-درجے کا ایکسچینج، DeFi دوستانہ، testnet سپورٹ شدہ |
| **Poloniex** | ✅ Stable | $300M+ | قائم شدہ ایکسچینج (2014)، بھرپور سکے کی قسم، testnet سپورٹ شدہ |
| **Crypto.com** | ✅ Stable | $500M+ | معروف برانڈ، دنیا بھر میں کروڑوں صارفین، testnet سپورٹ شدہ |

## ماڈیول آرکیٹیکچر

```
quantmesh_platform/
├── main.go                    # مین پروگرام انٹری، جزو orchestration
│
├── config/                    # کنفیگریشن مینجمنٹ
│   └── config.go              # YAML کنفیگریشن لوڈنگ اور توثیق
│
├── exchange/                  # ایکسچینج abstraction layer (کور)
│   ├── interface.go           # IExchange متحد انٹرفیس
│   ├── factory.go             # ایکسچینج instances بنانے کے لیے factory pattern
│   ├── types.go               # عام ڈیٹا structures
│   ├── wrapper_*.go           # adapters (exchanges کو wrap کرنا)
│   ├── binance/               # Binance implementation
│   ├── bitget/                # Bitget implementation
│   └── gate/                  # Gate.io implementation
│
├── logger/                    # لاگنگ سسٹم
│   └── logger.go              # فائل لاگنگ + console لاگنگ
│
├── monitor/                   # قیمت مانیٹرنگ
│   └── price_monitor.go       # عالمی منفرد قیمت سٹریم
│
├── order/                     # آرڈر execution layer
│   └── executor_adapter.go    # آرڈر executor (rate limiting + retry)
│
├── position/                  # پوزیشن مینجمنٹ (کور)
│   └── super_position_manager.go  # super slot manager
│
├── safety/                    # حفاظت اور خطرے کا کنٹرول
│   ├── safety.go              # pre-startup حفاظتی چیکس
│   ├── risk_monitor.go        # فعال خطرے کا کنٹرول (K-line مانیٹرنگ)
│   ├── reconciler.go          # پوزیشن reconciliation
│   └── order_cleaner.go       # آرڈر صفائی
│
└── utils/                     # utility functions
    └── orderid.go             # حسب ضرورت آرڈر ID generation
```

## بہترین طریقے

1. **ایکسچینج VIP سٹیٹس کے لیے**: یہ سسٹم ایک والیوم جنریشن ٹول ہے۔ اگر قیمت کی تبدیلیاں بڑی نہیں ہیں، تو $3,000 مارجن 2-3 دنوں میں $10 ملین ٹریڈنگ والیوم پیدا کر سکتا ہے۔

2. **منافع کے لیے بہترین طریقہ**: گراوٹ کے ایک دور کے بعد مارکیٹ میں داخل ہوں۔ پہلے ایک پوزیشن خریدیں، پھر سافٹ ویئر شروع کریں۔ یہ خودکار طور پر grid by grid اوپر بیچے گا۔ جب آپ کی پوزیشن ختم ہو جائے، تو سسٹم روک دیں۔ اگر آپ یقین نہیں ہیں کہ موجودہ مارکیٹ ایک کم نقطہ ہے، تو آپ بغیر base position کے شروع کر سکتے ہیں۔ اگر یہ مزید گرتا ہے، تو کم نقطہ پر ایک پوزیشن شامل کریں اور بیچنا جاری رکھنے کے لیے دوبارہ شروع کریں۔ یہ منافع کو زیادہ سے زیادہ کرتا ہے۔ مسلسل منافع کے لیے اس سائیکل کو دہرائیں۔ گراوٹ کے بارے میں فکر مت کریں - پروگرام مسلسل لاگت کم کرتا ہے۔ جب تک یہ آدھا بحال ہوتا ہے، آپ break-even پہنچ جاتے ہیں۔

## 🚀 شروع کرنا

### ضروریات
- Go 1.21 یا اس سے زیادہ
- ایکسچینج APIs تک رسائی حاصل کرنے کے قابل نیٹ ورک ماحول

### انسٹالیشن

1. **repository clone کریں**
   ```bash
   git clone https://github.com/ghostsworm/quantmesh.git
   cd quantmesh
   ```

2. **dependencies انسٹال کریں**
   ```bash
   go mod download
   ```

### کنفیگریشن

> **Runtime SSOT:** The primary database table `app_config` holds the authoritative configuration. One-time YAML import: `./quantmesh --migrate-app-config` (with `QUANTMESH_IMPORT_YAML`, or `config.yaml` in the working directory), or run `./quantmesh /path/to/file.yaml` as the first argument. See [`docs/config-database-design.md`](../config-database-design.md).

1. مثال کنفیگریشن فائل کاپی کریں:
   ```bash
   cp docs/config/examples/config.example.yaml config.yaml
   ```

2. `config.yaml` میں ترمیم کریں اور اپنا API Key اور حکمت عملی parameters بھریں:

   ```yaml
   app:
     current_exchange: "binance"  # ایکسچینج منتخب کریں

   exchanges:
     binance:
       api_key: "YOUR_API_KEY"
       secret_key: "YOUR_SECRET_KEY"
       fee_rate: 0.0002

   trading:
     symbol: "ETHUSDT"       # ٹریڈنگ جوڑا
     price_interval: 2       # گرڈ spacing (قیمت)
     order_quantity: 30     # فی گرڈ رقم (USDT)
     buy_window_size: 10    # خریدنے کے آرڈرز کی تعداد
     sell_window_size: 10   # بیچنے کے آرڈرز کی تعداد
   ```

### استعمال

#### پروڈکشن موڈ

compiled binary چلائیں:

```bash
go run main.go
```

یا build کریں اور چلائیں:

```bash
go build -o quantmesh
./quantmesh
```

بیک اینڈ پورٹ 28888 (ڈیفالٹ) پر فرنٹ اینڈ static files serve کرے گا۔

#### ڈویلپمنٹ موڈ

hot reload اور source code debugging کے ساتھ فرنٹ اینڈ ڈویلپمنٹ کے لیے:

**آپشن 1: ڈویلپمنٹ اسکرپٹ استعمال کریں (تجویز کردہ)**

```bash
./scripts/local/dev.sh
```

یہ اسکرپٹ:
- پورٹ 28888 پر Go بیک اینڈ سرور شروع کرے گا
- پورٹ 15173 پر Vite dev سرور شروع کرے گا
- فرنٹ اینڈ کوڈ تبدیلیوں کے لیے hot reload فعال کرے گا
- debugging کے لیے source maps فراہم کرے گا (کوئی minified کوڈ نہیں)

پھر ایپلیکیشن تک رسائی حاصل کریں: **http://localhost:15173**

**آپشن 2: دستی start-up**

Terminal 1 - Go بیک اینڈ شروع کریں:
```bash
go run main.go
```

Terminal 2 - Vite dev سرور شروع کریں:
```bash
cd webui
pnpm dev
```

پھر ایپلیکیشن تک رسائی حاصل کریں: **http://localhost:15173**

**ڈویلپمنٹ موڈ فوائد:**
- ✅ Hot reload - فرنٹ اینڈ کوڈ تبدیلیاں فوری طور پر عکس بندی ہوتی ہیں
- ✅ Source maps - اصل TypeScript/React کوڈ کے ساتھ debug کریں (minified نہیں)
- ✅ Fast refresh - React components حالت کھوئے بغیر اپڈیٹ ہوتے ہیں
- ✅ بہتر خرابی کے پیغامات - اصل فائل نام اور لائن نمبر دیکھیں

**نوٹ:** ڈویلپمنٹ موڈ میں، Vite dev سرور API requests (`/api/*`) اور WebSocket connections (`/ws`) کو پورٹ 28888 پر چلنے والے Go بیک اینڈ پر proxy کرتا ہے۔

## 🏗️ آرکیٹیکچر

سسٹم ایک modular design اپناتا ہے جس میں اہم اجزاء شامل ہیں:

- **ایکسچینج لیئر**: متحد ایکسچینج انٹرفیس abstraction، بنیادی API differences کو چھپاتا ہے۔
- **قیمت مانیٹر**: عالمی منفرد WebSocket قیمت کا ذریعہ، فیصلے کی مستقلت کو یقینی بناتا ہے۔
- **سپر پوزیشن مینیجر**: کور پوزیشن مینیجر، Slot mechanism کی بنیاد پر آرڈر lifecycle کا انتظام کرتا ہے۔
- **حفاظت اور خطرے کا کنٹرول**: کثیر-تہہ خطرے کا کنٹرول، start-up checks، runtime monitoring اور anomaly circuit breaker شامل ہیں۔

مزید تفصیلی آرکیٹیکچر دستاویزات کے لیے، براہ کرم [ARCHITECTURE.md](../../ARCHITECTURE.md) دیکھیں۔

## 📊 استعمال کے اعداد و شمار اور رازداری کی حفاظت

QuantMesh میں استعمال کے اعداد و شمار کی ایک اختیاری خصوصیت شامل ہے جو گمنام استعمال کے ڈیٹا کو جمع کرنے کے لیے ہے، ہمیں پروجیکٹ کے استعمال کو سمجھنے اور مصنوعات کو بہتر بنانے میں مدد کرتی ہے۔ **تمام ڈیٹا جمع کرنا مکمل طور پر شفاف ہے، کوڈ قابل آڈٹ ہے، اور کسی بھی وقت غیر فعال کیا جا سکتا ہے۔**

### 🔒 رازداری کی حفاظت

**ہم جو ڈیٹا جمع کرتے ہیں (گمنام):**
- ✅ **بنیادی معلومات**: ورژن نمبر، آپریٹنگ سسٹم، آرکیٹیکچر، instance ID (بے ترتیب طور پر پیدا شدہ UUID)
- ✅ **استعمال کے اعداد و شمار**: استعمال شدہ ایکسچینج کے نام، ٹریڈنگ جوڑے
- ✅ **کارکردگی کے میٹرکس**: API request/response latency، WebSocket latency
- ✅ **ٹریڈنگ سرگرمی**: ٹریڈنگ کی سمت (خریدنے/بیچنے)، ٹریڈنگ کی مقداروں کو خارج کرتے ہوئے

**ہم جو ڈیٹا جمع نہیں کرتے:**
- ❌ **IP پتہ**: فرنٹ اینڈ میں IP capture غیر فعال ہے، بیک اینڈ IP کے بجائے instance ID استعمال کرتا ہے
- ❌ **جغرافیائی مقام**: عرض البلد/طول البلد، شہر یا دیگر مقام کی معلومات کی کوئی جمع نہیں
- ❌ **ذاتی معلومات**: صارف ID، ای میلز، نام یا کوئی شناختی معلومات کی کوئی جمع نہیں
- ❌ **حساس ڈیٹا**: API keys، ٹریڈنگ کی مقدار، اکاؤنٹ بیلنس یا پوزیشن کی معلومات کی کوئی جمع نہیں
- ❌ **مالی ڈیٹا**: مالی یا ٹریڈنگ حساس معلومات کی کوئی جمع نہیں

### 🛡️ رازداری کی حفاظت کے اقدامات

1. **Instance ID میکانزم**: بے ترتیب طور پر پیدا شدہ UUID کو منفرد identifier کے طور پر استعمال کرتا ہے، `./data/instance_id` فائل میں محفوظ، کوئی ذاتی معلومات نہیں رکھتا
2. **فرنٹ اینڈ IP غیر فعال**: PostHog SDK `ip_capture: false` کے ساتھ کنفیگر کیا گیا، IP پتے کی capture اور جغرافیائی مقام inference کو غیر فعال کرتا ہے
3. **بیک اینڈ IP نہیں بھیجتا**: بیک اینڈ کوڈ اعداد و شمار کی سروس پر IP addresses نہیں بھیجتا
4. **مکمل طور پر اختیاری**: صارفین کسی بھی وقت environment variables کے ذریعے اعداد و شمار کو غیر فعال کر سکتے ہیں
5. **کوڈ کی شفافیت**: تمام اعداد و شمار کا کوڈ قابل آڈٹ ہے، `utils/telemetry.go` میں واقع

### ⚙️ اعداد و شمار کو کیسے غیر فعال کریں

**طریقہ 1: Environment Variable (تجویز کردہ)**
```bash
export QUANTMESH_DISABLE_TELEMETRY=1
```

**طریقہ 2: فرنٹ اینڈ غیر فعال کریں**
`webui/.env.local` فائل میں:
```bash
VITE_DISABLE_TELEMETRY=1
```

**طریقہ 3: کوڈ میں ترمیم کریں**
`utils/telemetry.go` میں ترمیم کریں، `Enabled` کو `false` پر سیٹ کریں

### 📖 تفصیلی دستاویزات

اعداد و شمار کی خصوصیت کے بارے میں مزید تفصیلی معلومات کے لیے، براہ کرم دیکھیں:
- 📖 [مکمل اعداد و شمار گائیڈ](../../docs/TELEMETRY_GUIDE.md)
- 🔒 [رازداری کی حفاظت گائیڈ](../../docs/TELEMETRY_PRIVACY.md)
- 🚀 [تیز سیٹ اپ گائیڈ](../../docs/TELEMETRY_SIMPLE_GUIDE.md)

---

## ⚠️ Disclaimer

یہ سافٹ ویئر صرف تعلیمی اور تحقیقی مقاصد کے لیے ہے۔ کریپٹو کرنسی ٹریڈنگ میں اعلی خطرہ شامل ہے اور اس کے نتیجے میں سرمایہ کا نقصان ہو سکتا ہے۔
- صارفین اس سافٹ ویئر کے استعمال سے پیدا ہونے والے کسی بھی منافع یا نقصان کے لیے مکمل طور پر ذمہ دار ہیں۔
- حقیقی فنڈز استعمال کرنے سے پہلے ہمیشہ Testnet پر مکمل طور پر ٹیسٹ کریں۔
- سافٹ ویئر bugs، نیٹ ورک latency یا ایکسچینج کی ناکامیوں کی وجہ سے ہونے والے نقصانات کے لیے ڈویلپرز ذمہ دار نہیں ہیں۔

## 🪙 کریپٹو ادائیگی کی سپورٹ

QuantMesh subscriptions اور licenses کے لیے کریپٹو کرنسی ادائیگیوں کی سپورٹ کرتا ہے:

### سپورٹ شدہ کریپٹو کرنسیاں
- **BTC** (Bitcoin)
- **ETH** (Ethereum)
- **USDT** (Tether, ERC20)
- **USDC** (USD Coin, ERC20)

### ادائیگی کے طریقے
1. **Coinbase Commerce** (تجویز کردہ)
   - خودکار تصدیق
   - متعدد کریپٹو کرنسیاں سپورٹ شدہ
   - آسان ادائیگی کا صفحہ

2. **براہ راست والیٹ ادائیگی**
   - کوئی تیسری پارٹی کی شمولیت نہیں
   - زیادہ رازداری
   - دستی تصدیق (1-24 گھنٹے)

### تیز شروع
```bash
# طریقہ A: Coinbase Commerce (15 منٹ)
# 1. https://commerce.coinbase.com پر رجسٹر کریں
# 2. .env.crypto میں API Key کنفیگر کریں
# 3. سروس شروع کریں

# طریقہ B: براہ راست والیٹ (5 منٹ)
# 1. والیٹ addresses کنفیگر کریں
# 2. سروس شروع کریں
# 3. دستی تصدیق
```

### دستاویزات
- 📖 [صارف ادائیگی گائیڈ](../CRYPTO_PAYMENT_GUIDE.md)
- 🚀 [تیز شروع گائیڈ](../CRYPTO_PAYMENT_QUICKSTART.md)
- 🔧 [سیٹ اپ گائیڈ](../CRYPTO_PAYMENT_SETUP.md)
- 📊 [عملدرآمد کا خلاصہ](../reports/CRYPTO_PAYMENT_SUMMARY.md)

### کریپٹو ادائیگیاں کیوں؟
✅ کریڈٹ کارڈ یا بینک اکاؤنٹ کی ضرورت نہیں  
✅ عالمی رسائی، کوئی علاقائی پابندیاں نہیں  
✅ کم لین دین کی فیس (1% بمقابلہ 2.9%)  
✅ بہتر رازداری کی حفاظت  
✅ تیز تصدیق (10-30 منٹ)  
✅ کریپٹو ٹریڈنگ سافٹ ویئر کے لیے کامل فٹ  

## 📜 لائسنس

یہ پروجیکٹ ایک **دوہرا لائسنس ماڈل** استعمال کرتا ہے:

### AGPL-3.0 اوپن سورس لائسنس
- ✅ استعمال، ترمیم اور تقسیم کے لیے مفت
- ⚠️ **تمام derivative کاموں کو اوپن سورس ہونا چاہیے** اور AGPL-3.0 کے تحت جاری کیا جانا چاہیے
- ⚠️ نیٹ ورک سروسز کے لیے بھی source code فراہم کرنا چاہیے
- ⚠️ ترمیم شدہ کوڈ کو کمیونٹی میں واپس contribute کرنا چاہیے

### Commercial لائسنس
اگر آپ کو proprietary applications یا services میں اس سافٹ ویئر کا استعمال کرنے کی ضرورت ہے، یا آپ اپنی ترمیمات کو اوپن سورس نہیں کرنا چاہتے، تو آپ کو commercial لائسنس خریدنا ہوگا۔

**Commercial لائسنس کا دائرہ:**
- proprietary applications میں استعمال
- ترمیمات کو اوپن سورس کرنے کی کوئی ذمہ داری نہیں
- تقسیم کے لیے proprietary products میں ضم کریں
- ترجیحی تکنیکی سپورٹ اور اپڈیٹس

**Commercial لائسنس کی استفسارات:**
- 📧 ای میل: contact@quantmesh.io
- 🌐 ویب سائٹ: https://quantmesh.io/commercial

---

### لائسنس کی تفصیلات

یہ پروجیکٹ دوہرے لائسنس کے تحت ہے:

1. **AGPL-3.0 (GNU Affero General Public License v3.0)**
   - استعمال، ترمیم اور تقسیم کے لیے مفت
   - تمام derivative کاموں کو AGPL-3.0 کے تحت اوپن سورس ہونا چاہیے
   - نیٹ ورک سروسز کے لیے بھی تمام صارفین کو source code فراہم کرنا چاہیے
   - ترمیمات کو کمیونٹی میں contribute کرنا چاہیے

2. **Commercial لائسنس**
   - proprietary استعمال کے لیے ضروری
   - ترمیمات کو اوپن سورس کرنے کی کوئی ذمہ داری نہیں
   - ترجیحی سپورٹ اور اپڈیٹس شامل ہیں

Commercial لائسنسنگ کی استفسارات کے لیے، براہ کرم رابطہ کریں:
- 📧 ای میل: contact@quantmesh.io
- 🌐 ویب سائٹ: https://quantmesh.io/commercial

## 🤝 تعاون

ہم تعاون کا خیرمقدم کرتے ہیں! یہاں آپ کیسے مدد کر سکتے ہیں:

- ⭐ **اس repo کو star دیں** اگر آپ کو یہ مددگار لگتا ہے
- 🍴 **Fork کریں اور استعمال کریں** پروجیکٹ
- 🐛 **bugs رپورٹ کریں** [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues) کے ذریعے
- 💡 **خصوصیات تجویز کریں** [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions) کے ذریعے
- 📝 **بہتریوں کے لیے PRs جمع کروائیں**
- 📖 **دستاویزات بہتر بنائیں**

**نوٹ:** AGPL-3.0 لائسنس کے مطابق، اس پروجیکٹ میں تمام تعاون اسی AGPL-3.0 لائسنس کے تحت جاری کیے جائیں گے۔

تفصیلی رہنمائی کے لیے [CONTRIBUTING.md](../../CONTRIBUTING.md) دیکھیں۔

## 🙏 اعتراف

اصل پروجیکٹ [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker) کے لیے [dennisyang1986](https://github.com/dennisyang1986) کا شکریہ ان کے اوپن سورس تعاون کے لیے، جس نے اس پروجیکٹ کے لیے ایک مضبوط بنیاد فراہم کی۔ مزید معلومات کے لیے، براہ کرم [NOTICE](../../NOTICE) فائل دیکھیں۔

---

## 📞 رابطہ اور سپورٹ

- 🌐 **ویب سائٹ**: https://quantmesh.io
- 📧 **ای میل**: contact@quantmesh.io
- 💬 **Discord**: [ہماری کمیونٹی میں شامل ہوں](https://discord.gg/YOUR_INVITE_LINK)
- 🐛 **Issues**: [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💬 **بحث**: [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📖 **دستاویزات**: [مکمل دستاویزات](../)

---

<div align="center">
  <strong>QuantMesh Team کی طرف سے ❤️ کے ساتھ بنایا گیا</strong><br/>
  <sub>اگر آپ کو یہ پروجیکٹ مددگار لگتا ہے، تو براہ کرم اسے ⭐ دیں</sub>
</div>

Copyright © 2025 QuantMesh Team. All Rights Reserved.

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
