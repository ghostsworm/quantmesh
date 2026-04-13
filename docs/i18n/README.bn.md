<div align="center">
  <img src="../../logo/qm_thick_tail_white.svg" alt="QuantMesh Logo" width="200"/>
  
  # QuantMesh Market Maker
  
  **উচ্চ-ফ্রিকোয়েন্সি ক্রিপ্টো মার্কেট মেকার**

  [![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org/dl/)
  [![License](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](../../LICENSE)
  [![GitHub Stars](https://img.shields.io/github/stars/ghostsworm/quantmesh.svg?style=social&label=Stars)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Forks](https://img.shields.io/github/forks/ghostsworm/quantmesh.svg?style=social&label=Forks)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Issues](https://img.shields.io/github/issues/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/issues)
  [![GitHub Release](https://img.shields.io/github/release/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/releases)
  [![Website](https://img.shields.io/badge/Website-quantmesh.io-green.svg)](https://quantmesh.io)
  
  [简体中文](../../README.md) | [繁體中文](README.zh-TW.md) | [English](README.en.md) | [Español](README.es.md) | [Français](README.fr.md) | [Português](README.pt.md) | [বাংলা](README.bn.md)
</div>

---

## 🎯 কেন QuantMesh বেছে নেবেন?

| বৈশিষ্ট্য | QuantMesh | অন্যান্য সমাধান |
|---------|-----------|----------------|
| **এক্সচেঞ্জ সমর্থন** | ২০+ এক্সচেঞ্জ | সাধারণত ৩-৫টি |
| **প্রতিক্রিয়া বিলম্ব** | মিলিসেকেন্ড-স্তর | সেকেন্ড-স্তর |
| **ঝুঁকি নিয়ন্ত্রণ** | মাল্টি-স্তর সক্রিয় নিয়ন্ত্রণ | মৌলিক নিয়ন্ত্রণ |
| **উৎপাদন পরীক্ষিত** | $১০০M+ ট্রেডিং ভলিউম | পরীক্ষিত নয় |
| **ওয়েব ইন্টারফেস** | ✅ সম্পূর্ণ React UI | ❌ নেই/মৌলিক |
| **ওপেন সোর্স** | AGPL-3.0 | বন্ধ সোর্স/সীমিত |
| **রিয়েল-টাইম ডেটা** | শুধুমাত্র WebSocket | REST polling |
| **সমবর্তিতা** | ১০০০+ অর্ডার/সেকেন্ড | সীমিত |

**মূল সুবিধা:**
- ✅ **যুদ্ধ-পরীক্ষিত**: $১০০M+ ট্রেডিং ভলিউম দিয়ে প্রমাণিত
- ✅ **উচ্চ কর্মক্ষমতা**: WebSocket আর্কিটেকচার সহ সাব-১০ms বিলম্ব
- ✅ **সম্পূর্ণ**: ট্রেডিং থেকে মনিটরিং পর্যন্ত সম্পূর্ণ সমাধান
- ✅ **স্বচ্ছ**: সম্পূর্ণ ওপেন সোর্স, যাচাইযোগ্য কোড
- ✅ **প্রসারযোগ্য**: কাস্টমাইজেশনের জন্য প্লাগইন সিস্টেম

---

## 📊 Telemetry

**Third-party analytics (PostHog) has been removed.** The Go backend and install script no longer send usage events. Symbols in `utils/telemetry.go` are no-ops for compatibility.

The Web UI may still load a **1×1 pixel** on startup (`webui/src/services/telemetry.ts`). Disable with `VITE_DISABLE_TELEMETRY=1` in `webui/.env.local` or `localStorage` key `QUANTMESH_DISABLE_TELEMETRY` = `1`.

See [TELEMETRY_GUIDE.md](../../docs/TELEMETRY_GUIDE.md).
---

## ⚠️ দায়মুক্তি

এই সফ্টওয়্যারটি শুধুমাত্র শিক্ষাগত এবং গবেষণা উদ্দেশ্যে। ক্রিপ্টোকারেন্সি ট্রেডিং উচ্চ ঝুঁকি জড়িত এবং মূলধন ক্ষতির কারণ হতে পারে।
- ব্যবহারকারীরা এই সফ্টওয়্যার ব্যবহার করে যে কোনও লাভ বা ক্ষতির জন্য একমাত্র দায়ী।
- আসল তহবিল ব্যবহার করার আগে সর্বদা Testnet-এ পুঙ্খানুপুঙ্খভাবে পরীক্ষা করুন।
- সফ্টওয়্যার বাগ, নেটওয়ার্ক বিলম্ব বা এক্সচেঞ্জ ব্যর্থতার কারণে ক্ষতির জন্য বিকাশকারীরা দায়ী নয়।

## 🪙 ক্রিপ্টো পেমেন্ট সমর্থন

QuantMesh সাবস্ক্রিপশন এবং লাইসেন্সের জন্য ক্রিপ্টোকারেন্সি পেমেন্ট সমর্থন করে:

### সমর্থিত ক্রিপ্টোকারেন্সি
- **BTC** (Bitcoin)
- **ETH** (Ethereum)
- **USDT** (Tether, ERC20)
- **USDC** (USD Coin, ERC20)

### পেমেন্ট পদ্ধতি
1. **Coinbase Commerce** (সুপারিশকৃত)
   - স্বয়ংক্রিয় নিশ্চিতকরণ
   - একাধিক ক্রিপ্টোকারেন্সি সমর্থিত
   - সহজ পেমেন্ট পৃষ্ঠা

2. **সরাসরি ওয়ালেট পেমেন্ট**
   - কোনও তৃতীয় পক্ষের জড়িত নেই
   - আরও গোপনীয়তা
   - ম্যানুয়াল নিশ্চিতকরণ (১-২৪ ঘন্টা)

### দ্রুত শুরু
```bash
# পদ্ধতি A: Coinbase Commerce (১৫ মিনিট)
# ১. https://commerce.coinbase.com এ নিবন্ধন করুন
# ২. .env.crypto এ API Key কনফিগার করুন
# ৩. পরিষেবা শুরু করুন

# পদ্ধতি B: সরাসরি ওয়ালেট (৫ মিনিট)
# ১. ওয়ালেট ঠিকানা কনফিগার করুন
# ২. পরিষেবা শুরু করুন
# ৩. ম্যানুয়াল নিশ্চিতকরণ
```

### ডকুমেন্টেশন
- 📖 [ব্যবহারকারী পেমেন্ট গাইড](../CRYPTO_PAYMENT_GUIDE.md)
- 🚀 [দ্রুত শুরু গাইড](../CRYPTO_PAYMENT_QUICKSTART.md)
- 🔧 [সেটআপ গাইড](../CRYPTO_PAYMENT_SETUP.md)
- 📊 [বাস্তবায়ন সারাংশ](../reports/CRYPTO_PAYMENT_SUMMARY.md)

### কেন ক্রিপ্টো পেমেন্ট?
✅ ক্রেডিট কার্ড বা ব্যাঙ্ক অ্যাকাউন্টের প্রয়োজন নেই  
✅ বিশ্বব্যাপী অ্যাক্সেসযোগ্যতা, কোনও আঞ্চলিক সীমাবদ্ধতা নেই  
✅ কম লেনদেন ফি (১% বনাম ২.৯%)  
✅ ভাল গোপনীয়তা সুরক্ষা  
✅ দ্রুত নিশ্চিতকরণ (১০-৩০ মিনিট)  
✅ ক্রিপ্টো ট্রেডিং সফ্টওয়্যারের জন্য নিখুঁত ফিট  

## 📜 লাইসেন্স

এই প্রকল্পটি একটি **দ্বৈত লাইসেন্স মডেল** ব্যবহার করে:

### AGPL-3.0 ওপেন সোর্স লাইসেন্স
- ✅ ব্যবহার, পরিবর্তন এবং বিতরণের জন্য বিনামূল্যে
- ⚠️ **সমস্ত ডেরিভেটিভ কাজ অবশ্যই ওপেন সোর্স হতে হবে** এবং AGPL-3.0 এর অধীনে প্রকাশিত হতে হবে
- ⚠️ নেটওয়ার্ক পরিষেবার জন্যও সোর্স কোড প্রদান করতে হবে
- ⚠️ পরিবর্তিত কোড অবশ্যই সম্প্রদায়ে অবদান রাখতে হবে

### বাণিজ্যিক লাইসেন্স
আপনার যদি মালিকানাধীন অ্যাপ্লিকেশন বা পরিষেবায় এই সফ্টওয়্যার ব্যবহার করতে হয়, বা আপনার পরিবর্তনগুলি ওপেন সোর্স করতে না চান, তাহলে আপনাকে একটি বাণিজ্যিক লাইসেন্স কিনতে হবে।

**বাণিজ্যিক লাইসেন্স সুযোগ:**
- মালিকানাধীন অ্যাপ্লিকেশনে ব্যবহার
- পরিবর্তনগুলি ওপেন সোর্স করার কোনও বাধ্যবাধকতা নেই
- বিতরণের জন্য মালিকানাধীন পণ্যগুলিতে একীভূত করুন
- অগ্রাধিকার প্রযুক্তিগত সমর্থন এবং আপডেট

**বাণিজ্যিক লাইসেন্স অনুসন্ধান:**
- 📧 ইমেইল: contact@quantmesh.io
- 🌐 ওয়েবসাইট: https://quantmesh.io/commercial

---

### লাইসেন্স বিবরণ

এই প্রকল্পটি দ্বৈত লাইসেন্সের অধীনে:

1. **AGPL-3.0 (GNU Affero General Public License v3.0)**
   - ব্যবহার, পরিবর্তন এবং বিতরণের জন্য বিনামূল্যে
   - সমস্ত ডেরিভেটিভ কাজ অবশ্যই AGPL-3.0 এর অধীনে ওপেন সোর্স হতে হবে
   - নেটওয়ার্ক পরিষেবার জন্যও সমস্ত ব্যবহারকারীকে সোর্স কোড প্রদান করতে হবে
   - পরিবর্তনগুলি অবশ্যই সম্প্রদায়ে অবদান রাখতে হবে

2. **বাণিজ্যিক লাইসেন্স**
   - মালিকানাধীন ব্যবহারের জন্য প্রয়োজনীয়
   - পরিবর্তনগুলি ওপেন সোর্স করার কোনও বাধ্যবাধকতা নেই
   - অগ্রাধিকার সমর্থন এবং আপডেট অন্তর্ভুক্ত

বাণিজ্যিক লাইসেন্সিং অনুসন্ধানের জন্য, অনুগ্রহ করে যোগাযোগ করুন:
- 📧 ইমেইল: contact@quantmesh.io
- 🌐 ওয়েবসাইট: https://quantmesh.io/commercial

## 🤝 অবদান

আমরা অবদান স্বাগত জানাই! এখানে আপনি কীভাবে সাহায্য করতে পারেন:

- ⭐ **এই repo-তে একটি তারা দিন** যদি আপনি এটি সহায়ক মনে করেন
- 🍴 **Fork করুন এবং ব্যবহার করুন** প্রকল্পটি
- 🐛 **বাগ রিপোর্ট করুন** [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues) এর মাধ্যমে
- 💡 **ফিচার প্রস্তাব করুন** [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions) এর মাধ্যমে
- 📝 **PR জমা দিন** উন্নতির জন্য
- 📖 **ডকুমেন্টেশন উন্নত করুন**

**নোট:** AGPL-3.0 লাইসেন্স অনুসারে, এই প্রকল্পে সমস্ত অবদান একই AGPL-3.0 লাইসেন্সের অধীনে প্রকাশিত হবে।

বিস্তারিত নির্দেশিকাগুলির জন্য [CONTRIBUTING.md](../CONTRIBUTING.md) দেখুন।

## 🙏 স্বীকৃতি

মূল প্রকল্প [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker) এর জন্য [dennisyang1986](https://github.com/dennisyang1986) এর ওপেন সোর্স অবদানের জন্য ধন্যবাদ, যা এই প্রকল্পের জন্য একটি শক্ত ভিত্তি প্রদান করেছে। আরও তথ্যের জন্য, অনুগ্রহ করে [NOTICE](../../NOTICE) ফাইলটি দেখুন।

---

## 📞 যোগাযোগ এবং সমর্থন

- 🌐 **ওয়েবসাইট**: https://quantmesh.io
- 📧 **ইমেইল**: contact@quantmesh.io
- 💬 **Discord**: [আমাদের সম্প্রদায়ে যোগ দিন](https://discord.gg/YOUR_INVITE_LINK)
- 🐛 **Issues**: [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💬 **আলোচনা**: [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📖 **ডকুমেন্টেশন**: [সম্পূর্ণ ডকুমেন্টেশন](../)

---

<div align="center">
  <strong>QuantMesh Team দ্বারা ❤️ দিয়ে তৈরি</strong><br/>
  <sub>আপনি যদি এই প্রকল্পটি সহায়ক মনে করেন, অনুগ্রহ করে এটিকে একটি ⭐ দিন</sub>
</div>

Copyright © ২০২৫ QuantMesh Team. সর্বস্বত্ব সংরক্ষিত।

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
