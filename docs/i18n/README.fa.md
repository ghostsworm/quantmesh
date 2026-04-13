<div align="center" dir="rtl">
  <img src="../../logo/qm_thick_tail_white.svg" alt="QuantMesh Logo" width="200"/>
  
  # QuantMesh Market Maker
  
  **سازنده بازار رمزنگاری با فرکانس بالا**

  [![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org/dl/)
  [![License](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](../../LICENSE)
  [![GitHub Stars](https://img.shields.io/github/stars/ghostsworm/quantmesh.svg?style=social&label=Stars)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Forks](https://img.shields.io/github/forks/ghostsworm/quantmesh.svg?style=social&label=Forks)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Issues](https://img.shields.io/github/issues/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/issues)
  [![GitHub Release](https://img.shields.io/github/release/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/releases)
  [![Website](https://img.shields.io/badge/Website-quantmesh.io-green.svg)](https://quantmesh.io)
  
  [简体中文](../../README.md) | [繁體中文](README.zh-TW.md) | [English](README.en.md) | [Español](README.es.md) | [Français](README.fr.md) | [Português](README.pt.md) | [فارسی](README.fa.md)
</div>

---

<div dir="rtl">

## 🎯 چرا QuantMesh را انتخاب کنیم؟

| ویژگی | QuantMesh | راه‌حل‌های دیگر |
|---------|-----------|----------------|
| **پشتیبانی صرافی** | 20+ صرافی | معمولاً 3-5 |
| **تأخیر پاسخ** | سطح میلی‌ثانیه | سطح ثانیه |
| **کنترل ریسک** | کنترل فعال چندلایه | کنترل پایه |
| **تست شده در تولید** | حجم معاملات $100M+ | تست نشده |
| **رابط وب** | ✅ رابط کاربری React کامل | ❌ هیچ/پایه |
| **متن باز** | AGPL-3.0 | منبع بسته/محدود |
| **داده‌های بلادرنگ** | فقط WebSocket | REST polling |
| **همزمانی** | 1000+ سفارش/ثانیه | محدود |

**مزایای کلیدی:**
- ✅ **تست شده در میدان**: با حجم معاملات $100M+ ثابت شده
- ✅ **عملکرد بالا**: تأخیر زیر 10ms با معماری WebSocket
- ✅ **جامع**: راه‌حل کامل از معاملات تا نظارت
- ✅ **شفاف**: کاملاً متن باز، کد قابل بررسی
- ✅ **قابل گسترش**: سیستم افزونه برای سفارشی‌سازی

---

## 📊 Telemetry

**Third-party analytics (PostHog) has been removed.** The Go backend and install script no longer send usage events. Symbols in `utils/telemetry.go` are no-ops for compatibility.

The Web UI may still load a **1×1 pixel** on startup (`webui/src/services/telemetry.ts`). Disable with `VITE_DISABLE_TELEMETRY=1` in `webui/.env.local` or `localStorage` key `QUANTMESH_DISABLE_TELEMETRY` = `1`.

See [TELEMETRY_GUIDE.md](../../docs/TELEMETRY_GUIDE.md).
---

## ⚠️ سلب مسئولیت

این نرم‌افزار فقط برای اهداف آموزشی و تحقیقاتی است. معاملات رمزنگاری شامل ریسک بالا است و ممکن است منجر به از دست دادن سرمایه شود.
- کاربران به طور کامل مسئول هر سود یا زیان ناشی از استفاده از این نرم‌افزار هستند.
- همیشه قبل از استفاده از وجوه واقعی، به طور کامل در Testnet تست کنید.
- توسعه‌دهندگان مسئول ضررهای ناشی از اشکالات نرم‌افزار، تأخیر شبکه یا خرابی صرافی نیستند.

## 🪙 پشتیبانی پرداخت رمزنگاری

QuantMesh پرداخت‌های رمزنگاری را برای اشتراک‌ها و مجوزها پشتیبانی می‌کند:

### ارزهای رمزنگاری پشتیبانی شده
- **BTC** (Bitcoin)
- **ETH** (Ethereum)
- **USDT** (Tether, ERC20)
- **USDC** (USD Coin, ERC20)

### روش‌های پرداخت
1. **Coinbase Commerce** (توصیه می‌شود)
   - تأیید خودکار
   - چندین ارز رمزنگاری پشتیبانی می‌شود
   - صفحه پرداخت آسان

2. **پرداخت مستقیم کیف پول**
   - بدون دخالت شخص ثالث
   - حریم خصوصی بیشتر
   - تأیید دستی (1-24 ساعت)

### شروع سریع
```bash
# روش A: Coinbase Commerce (15 دقیقه)
# 1. در https://commerce.coinbase.com ثبت نام کنید
# 2. کلید API را در .env.crypto پیکربندی کنید
# 3. سرویس را شروع کنید

# روش B: کیف پول مستقیم (5 دقیقه)
# 1. آدرس‌های کیف پول را پیکربندی کنید
# 2. سرویس را شروع کنید
# 3. تأیید دستی
```

### مستندات
- 📖 [راهنمای پرداخت کاربر](../CRYPTO_PAYMENT_GUIDE.md)
- 🚀 [راهنمای شروع سریع](../CRYPTO_PAYMENT_QUICKSTART.md)
- 🔧 [راهنمای راه‌اندازی](../CRYPTO_PAYMENT_SETUP.md)
- 📊 [خلاصه پیاده‌سازی](../reports/CRYPTO_PAYMENT_SUMMARY.md)

### چرا پرداخت‌های رمزنگاری؟
✅ نیازی به کارت اعتباری یا حساب بانکی نیست  
✅ دسترسی جهانی، بدون محدودیت منطقه‌ای  
✅ هزینه‌های تراکنش کمتر (1% در مقابل 2.9%)  
✅ محافظت بهتر از حریم خصوصی  
✅ تأیید سریع (10-30 دقیقه)  
✅ مناسب کامل برای نرم‌افزار معاملات رمزنگاری  

## 📜 مجوز

این پروژه از **مدل مجوز دوگانه** استفاده می‌کند:

### مجوز منبع باز AGPL-3.0
- ✅ رایگان برای استفاده، تغییر و توزیع
- ⚠️ **همه آثار مشتق باید منبع باز باشند** و تحت AGPL-3.0 منتشر شوند
- ⚠️ کد منبع باید حتی برای سرویس‌های شبکه ارائه شود
- ⚠️ کد تغییر یافته باید به جامعه بازگردانده شود

### مجوز تجاری
اگر نیاز به استفاده از این نرم‌افزار در برنامه‌ها یا سرویس‌های اختصاصی دارید، یا نمی‌خواهید تغییرات خود را منبع باز کنید، باید یک مجوز تجاری خریداری کنید.

**محدوده مجوز تجاری:**
- استفاده در برنامه‌های اختصاصی
- بدون تعهد برای منبع باز کردن تغییرات
- ادغام در محصولات اختصاصی برای توزیع
- پشتیبانی فنی اولویت و به‌روزرسانی‌ها

**سوالات مجوز تجاری:**
- 📧 ایمیل: contact@quantmesh.io
- 🌐 وب‌سایت: https://quantmesh.io/commercial

---

### جزئیات مجوز

این پروژه تحت مجوز دوگانه زیر است:

1. **AGPL-3.0 (GNU Affero General Public License v3.0)**
   - رایگان برای استفاده، تغییر و توزیع
   - همه آثار مشتق باید تحت AGPL-3.0 منبع باز باشند
   - کد منبع باید به همه کاربران ارائه شود، حتی برای سرویس‌های شبکه
   - تغییرات باید به جامعه بازگردانده شود

2. **مجوز تجاری**
   - برای استفاده اختصاصی مورد نیاز است
   - بدون تعهد برای منبع باز کردن تغییرات
   - شامل پشتیبانی و به‌روزرسانی‌های اولویت

برای سوالات مجوز تجاری، لطفاً تماس بگیرید:
- 📧 ایمیل: contact@quantmesh.io
- 🌐 وب‌سایت: https://quantmesh.io/commercial

## 🤝 مشارکت

ما از مشارکت استقبال می‌کنیم! در اینجا نحوه کمک شما آمده است:

- ⭐ **این مخزن را ستاره کنید** اگر مفید می‌یابید
- 🍴 **Fork و استفاده کنید** پروژه
- 🐛 **گزارش باگ** از طریق [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💡 **پیشنهاد ویژگی** از طریق [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📝 **ارسال PR** برای بهبودها
- 📖 **بهبود مستندات**

**یادداشت:** طبق مجوز AGPL-3.0، همه مشارکت‌ها در این پروژه تحت همان مجوز AGPL-3.0 منتشر خواهند شد.

برای دستورالعمل‌های تفصیلی [CONTRIBUTING.md](../CONTRIBUTING.md) را ببینید.

## 🙏 قدردانی

از پروژه اصلی [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker) توسط [dennisyang1986](https://github.com/dennisyang1986) برای مشارکت منبع باز آنها تشکر می‌کنیم که پایه محکمی برای این پروژه فراهم کرد. برای اطلاعات بیشتر، لطفاً به فایل [NOTICE](../../NOTICE) مراجعه کنید.

---

## 📞 تماس و پشتیبانی

- 🌐 **وب‌سایت**: https://quantmesh.io
- 📧 **ایمیل**: contact@quantmesh.io
- 💬 **Discord**: [به جامعه ما بپیوندید](https://discord.gg/YOUR_INVITE_LINK)
- 🐛 **Issues**: [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💬 **بحث‌ها**: [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📖 **مستندات**: [مستندات کامل](../)

---

<div align="center">
  <strong>ساخته شده با ❤️ توسط تیم QuantMesh</strong><br/>
  <sub>اگر این پروژه را مفید می‌یابید، لطفاً در نظر بگیرید که به آن ⭐ بدهید</sub>
</div>

Copyright © 2025 QuantMesh Team. تمامی حقوق محفوظ است.

</div>

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
