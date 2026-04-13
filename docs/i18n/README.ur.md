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

## 📊 Telemetry

**Third-party analytics (PostHog) has been removed.** The Go backend and install script no longer send usage events. Symbols in `utils/telemetry.go` are no-ops for compatibility.

The Web UI may still load a **1×1 pixel** on startup (`webui/src/services/telemetry.ts`). Disable with `VITE_DISABLE_TELEMETRY=1` in `webui/.env.local` or `localStorage` key `QUANTMESH_DISABLE_TELEMETRY` = `1`.

See [TELEMETRY_GUIDE.md](../../docs/TELEMETRY_GUIDE.md).
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

تفصیلی رہنمائی کے لیے [CONTRIBUTING.md](../CONTRIBUTING.md) دیکھیں۔

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
