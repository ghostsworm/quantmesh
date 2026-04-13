<div align="center" dir="rtl">
  <img src="../../logo/qm_thick_tail_white.svg" alt="QuantMesh Logo" width="200"/>
  
  # QuantMesh Market Maker
  
  **صانع سوق العملات المشفرة عالي التردد**

  [![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org/dl/)
  [![License](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](../../LICENSE)
  [![GitHub Stars](https://img.shields.io/github/stars/ghostsworm/quantmesh.svg?style=social&label=Stars)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Forks](https://img.shields.io/github/forks/ghostsworm/quantmesh.svg?style=social&label=Forks)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Issues](https://img.shields.io/github/issues/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/issues)
  [![GitHub Release](https://img.shields.io/github/release/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/releases)
  [![Website](https://img.shields.io/badge/Website-quantmesh.io-green.svg)](https://quantmesh.io)
  
  [简体中文](../../README.md) | [繁體中文](README.zh-TW.md) | [English](README.en.md) | [Español](README.es.md) | [Français](README.fr.md) | [Português](README.pt.md) | [العربية](README.ar.md)
</div>

---

<div dir="rtl">

## 🎯 لماذا تختار QuantMesh؟

| الميزة | QuantMesh | الحلول الأخرى |
|---------|-----------|----------------|
| **دعم البورصات** | 20+ بورصة | عادة 3-5 |
| **زمن الاستجابة** | مستوى الميلي ثانية | مستوى الثانية |
| **التحكم في المخاطر** | تحكم نشط متعدد الطبقات | تحكم أساسي |
| **مختبر في الإنتاج** | حجم تداول $100M+ | غير مختبر |
| **واجهة الويب** | ✅ واجهة React كاملة | ❌ لا شيء/أساسي |
| **مفتوح المصدر** | AGPL-3.0 | مغلق المصدر/مقيد |
| **البيانات في الوقت الفعلي** | WebSocket فقط | REST polling |
| **التزامن** | 1000+ طلب/ثانية | محدود |

**المزايا الرئيسية:**
- ✅ **مختبر في المعركة**: مثبت بحجم تداول $100M+
- ✅ **أداء عالي**: زمن استجابة أقل من 10ms مع بنية WebSocket
- ✅ **شامل**: حل كامل من التداول إلى المراقبة
- ✅ **شفاف**: مفتوح المصدر بالكامل، كود قابل للمراجعة
- ✅ **قابل للتوسيع**: نظام إضافات للتخصيص

---

## 📊 Telemetry

**Third-party analytics (PostHog) has been removed.** The Go backend and install script no longer send usage events. Symbols in `utils/telemetry.go` are no-ops for compatibility.

The Web UI may still load a **1×1 pixel** on startup (`webui/src/services/telemetry.ts`). Disable with `VITE_DISABLE_TELEMETRY=1` in `webui/.env.local` or `localStorage` key `QUANTMESH_DISABLE_TELEMETRY` = `1`.

See [TELEMETRY_GUIDE.md](../../docs/TELEMETRY_GUIDE.md).
---

## ⚠️ إخلاء المسؤولية

هذا البرنامج للأغراض التعليمية والبحثية فقط. يتضمن تداول العملات المشفرة مخاطر عالية وقد يؤدي إلى خسارة رأس المال.
- المستخدمون وحدهم مسؤولون عن أي أرباح أو خسائر من استخدام هذا البرنامج.
- اختبر دائمًا بدقة على شبكة الاختبار قبل استخدام الأموال الحقيقية.
- المطورون غير مسؤولين عن الخسائر بسبب أخطاء البرنامج أو زمن استجابة الشبكة أو فشل البورصة.

## 🪙 دعم الدفع بالعملات المشفرة

يدعم QuantMesh المدفوعات بالعملات المشفرة للاشتراكات والتراخيص:

### العملات المشفرة المدعومة
- **BTC** (Bitcoin)
- **ETH** (Ethereum)
- **USDT** (Tether, ERC20)
- **USDC** (USD Coin, ERC20)

### طرق الدفع
1. **Coinbase Commerce** (موصى به)
   - تأكيد تلقائي
   - دعم عملات مشفرة متعددة
   - صفحة دفع سهلة

2. **دفع المحفظة المباشر**
   - لا يوجد تدخل من طرف ثالث
   - المزيد من الخصوصية
   - تأكيد يدوي (1-24 ساعة)

### البدء السريع
```bash
# الطريقة A: Coinbase Commerce (15 دقيقة)
# 1. التسجيل على https://commerce.coinbase.com
# 2. تكوين مفتاح API في .env.crypto
# 3. بدء الخدمة

# الطريقة B: المحفظة المباشرة (5 دقائق)
# 1. تكوين عناوين المحفظة
# 2. بدء الخدمة
# 3. تأكيد يدوي
```

### الوثائق
- 📖 [دليل دفع المستخدم](../CRYPTO_PAYMENT_GUIDE.md)
- 🚀 [دليل البدء السريع](../CRYPTO_PAYMENT_QUICKSTART.md)
- 🔧 [دليل الإعداد](../CRYPTO_PAYMENT_SETUP.md)
- 📊 [ملخص التنفيذ](../reports/CRYPTO_PAYMENT_SUMMARY.md)

### لماذا المدفوعات بالعملات المشفرة؟
✅ لا حاجة لبطاقة ائتمان أو حساب بنكي  
✅ إمكانية الوصول العالمية، لا قيود إقليمية  
✅ رسوم معاملات أقل (1% مقابل 2.9%)  
✅ حماية خصوصية أفضل  
✅ تأكيد سريع (10-30 دقيقة)  
✅ مناسب تمامًا لبرنامج تداول العملات المشفرة  

## 📜 الترخيص

يستخدم هذا المشروع **نموذج ترخيص مزدوج**:

### ترخيص مفتوح المصدر AGPL-3.0
- ✅ مجاني للاستخدام والتعديل والتوزيع
- ⚠️ **يجب أن تكون جميع الأعمال المشتقة مفتوحة المصدر** وتُصدر تحت AGPL-3.0
- ⚠️ يجب توفير الكود المصدري حتى لخدمات الشبكة
- ⚠️ يجب إرجاع الكود المعدل إلى المجتمع

### الترخيص التجاري
إذا كنت بحاجة إلى استخدام هذا البرنامج في تطبيقات أو خدمات احتكارية، أو لا ترغب في جعل تعديلاتك مفتوحة المصدر، فأنت بحاجة إلى شراء ترخيص تجاري.

**نطاق الترخيص التجاري:**
- الاستخدام في التطبيقات الاحتكارية
- لا التزام بجعل التعديلات مفتوحة المصدر
- التكامل في المنتجات الاحتكارية للتوزيع
- الدعم الفني ذو الأولوية والتحديثات

**استفسارات الترخيص التجاري:**
- 📧 البريد الإلكتروني: contact@quantmesh.io
- 🌐 الموقع: https://quantmesh.io/commercial

---

### تفاصيل الترخيص

هذا المشروع مرخص بترخيص مزدوج تحت:

1. **AGPL-3.0 (GNU Affero General Public License v3.0)**
   - مجاني للاستخدام والتعديل والتوزيع
   - يجب أن تكون جميع الأعمال المشتقة مفتوحة المصدر تحت AGPL-3.0
   - يجب توفير الكود المصدري لجميع المستخدمين، حتى لخدمات الشبكة
   - يجب إرجاع التعديلات إلى المجتمع

2. **الترخيص التجاري**
   - مطلوب للاستخدام الاحتكاري
   - لا التزام بجعل التعديلات مفتوحة المصدر
   - يتضمن دعمًا وتحديثات ذات أولوية

للاستفسارات حول الترخيص التجاري، يرجى الاتصال:
- 📧 البريد الإلكتروني: contact@quantmesh.io
- 🌐 الموقع: https://quantmesh.io/commercial

## 🤝 المساهمة

نرحب بالمساهمات! إليك كيف يمكنك المساعدة:

- ⭐ **ضع نجمة على هذا المستودع** إذا وجدته مفيدًا
- 🍴 **انشئ نسخة واستخدم** المشروع
- 🐛 **أبلغ عن الأخطاء** عبر [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💡 **اقترح ميزات** عبر [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📝 **أرسل PR** للتحسينات
- 📖 **حسّن الوثائق**

**ملاحظة:** وفقًا لترخيص AGPL-3.0، سيتم إصدار جميع المساهمات في هذا المشروع تحت نفس ترخيص AGPL-3.0.

راجع [CONTRIBUTING.md](../CONTRIBUTING.md) للحصول على إرشادات مفصلة.

## 🙏 شكر وتقدير

شكرًا للمشروع الأصلي [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker) من [dennisyang1986](https://github.com/dennisyang1986) لمساهمتهم مفتوحة المصدر، والتي وفرت أساسًا قويًا لهذا المشروع. لمزيد من المعلومات، يرجى الرجوع إلى ملف [NOTICE](../../NOTICE).

---

## 📞 الاتصال والدعم

- 🌐 **الموقع**: https://quantmesh.io
- 📧 **البريد الإلكتروني**: contact@quantmesh.io
- 💬 **Discord**: [انضم إلى مجتمعنا](https://discord.gg/YOUR_INVITE_LINK)
- 🐛 **المشاكل**: [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💬 **المناقشات**: [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📖 **الوثائق**: [الوثائق الكاملة](../)

---

<div align="center">
  <strong>صُنع بـ ❤️ بواسطة فريق QuantMesh</strong><br/>
  <sub>إذا وجدت هذا المشروع مفيدًا، يرجى التفكير في إعطائه ⭐</sub>
</div>

Copyright © 2025 QuantMesh Team. جميع الحقوق محفوظة.

</div>

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
