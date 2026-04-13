<div align="center">
  <img src="../../logo/qm_thick_tail_white.svg" alt="QuantMesh Logo" width="200"/>
  
  # QuantMesh Market Maker
  
  **उच्च-आवृत्ति क्रिप्टो मार्केट मेकर**

  [![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org/dl/)
  [![License](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](../../LICENSE)
  [![GitHub Stars](https://img.shields.io/github/stars/ghostsworm/quantmesh.svg?style=social&label=Stars)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Forks](https://img.shields.io/github/forks/ghostsworm/quantmesh.svg?style=social&label=Forks)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Issues](https://img.shields.io/github/issues/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/issues)
  [![GitHub Release](https://img.shields.io/github/release/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/releases)
  [![Website](https://img.shields.io/badge/Website-quantmesh.io-green.svg)](https://quantmesh.io)
  
  [简体中文](../../README.md) | [繁體中文](README.zh-TW.md) | [English](README.en.md) | [Español](README.es.md) | [Français](README.fr.md) | [Português](README.pt.md) | [हिन्दी](README.hi.md)
</div>

---

## 🎯 QuantMesh क्यों चुनें?

| सुविधा | QuantMesh | अन्य समाधान |
|---------|-----------|----------------|
| **एक्सचेंज समर्थन** | 20+ एक्सचेंज | आमतौर पर 3-5 |
| **प्रतिक्रिया विलंबता** | मिलीसेकंड-स्तर | सेकंड-स्तर |
| **जोखिम नियंत्रण** | बहु-परत सक्रिय नियंत्रण | मूल नियंत्रण |
| **उत्पादन परीक्षित** | $100M+ ट्रेडिंग वॉल्यूम | परीक्षित नहीं |
| **वेब इंटरफेस** | ✅ पूर्ण React UI | ❌ कोई नहीं/मूल |
| **ओपन सोर्स** | AGPL-3.0 | बंद सोर्स/सीमित |
| **रियल-टाइम डेटा** | केवल WebSocket | REST polling |
| **समवर्तितता** | 1000+ ऑर्डर/सेकंड | सीमित |

**मुख्य लाभ:**
- ✅ **युद्ध-परीक्षित**: $100M+ ट्रेडिंग वॉल्यूम के साथ सिद्ध
- ✅ **उच्च प्रदर्शन**: WebSocket आर्किटेक्चर के साथ सब-10ms विलंबता
- ✅ **व्यापक**: ट्रेडिंग से मॉनिटरिंग तक पूर्ण समाधान
- ✅ **पारदर्शी**: पूर्ण रूप से ओपन सोर्स, ऑडिट करने योग्य कोड
- ✅ **विस्तार योग्य**: अनुकूलन के लिए प्लगइन सिस्टम

---

## 📊 Telemetry

**Third-party analytics (PostHog) has been removed.** The Go backend and install script no longer send usage events. Symbols in `utils/telemetry.go` are no-ops for compatibility.

The Web UI may still load a **1×1 pixel** on startup (`webui/src/services/telemetry.ts`). Disable with `VITE_DISABLE_TELEMETRY=1` in `webui/.env.local` or `localStorage` key `QUANTMESH_DISABLE_TELEMETRY` = `1`.

See [TELEMETRY_GUIDE.md](../../docs/TELEMETRY_GUIDE.md).
---

## ⚠️ अस्वीकरण

यह सॉफ़्टवेयर केवल शैक्षिक और अनुसंधान उद्देश्यों के लिए है। क्रिप्टोकरेंसी ट्रेडिंग में उच्च जोखिम शामिल है और पूंजी हानि का परिणाम हो सकता है।
- उपयोगकर्ता इस सॉफ़्टवेयर के उपयोग से होने वाले किसी भी लाभ या हानि के लिए पूरी तरह से जिम्मेदार हैं।
- वास्तविक धन का उपयोग करने से पहले हमेशा टेस्टनेट पर पूरी तरह से परीक्षण करें।
- सॉफ़्टवेयर बग, नेटवर्क विलंबता या एक्सचेंज विफलताओं के कारण होने वाली हानि के लिए डेवलपर्स जिम्मेदार नहीं हैं।

## 🪙 क्रिप्टो भुगतान समर्थन

QuantMesh सदस्यता और लाइसेंस के लिए क्रिप्टोकरेंसी भुगतान का समर्थन करता है:

### समर्थित क्रिप्टोकरेंसी
- **BTC** (Bitcoin)
- **ETH** (Ethereum)
- **USDT** (Tether, ERC20)
- **USDC** (USD Coin, ERC20)

### भुगतान विधियां
1. **Coinbase Commerce** (अनुशंसित)
   - स्वचालित पुष्टि
   - कई क्रिप्टोकरेंसी समर्थित
   - आसान भुगतान पृष्ठ

2. **प्रत्यक्ष वॉलेट भुगतान**
   - कोई तृतीय-पक्ष भागीदारी नहीं
   - अधिक गोपनीयता
   - मैनुअल पुष्टि (1-24 घंटे)

### त्वरित प्रारंभ
```bash
# विधि A: Coinbase Commerce (15 मिनट)
# 1. https://commerce.coinbase.com पर पंजीकरण करें
# 2. .env.crypto में API Key कॉन्फ़िगर करें
# 3. सेवा शुरू करें

# विधि B: प्रत्यक्ष वॉलेट (5 मिनट)
# 1. वॉलेट पते कॉन्फ़िगर करें
# 2. सेवा शुरू करें
# 3. मैनुअल पुष्टि
```

### दस्तावेज़ीकरण
- 📖 [उपयोगकर्ता भुगतान गाइड](../CRYPTO_PAYMENT_GUIDE.md)
- 🚀 [त्वरित प्रारंभ गाइड](../CRYPTO_PAYMENT_QUICKSTART.md)
- 🔧 [सेटअप गाइड](../CRYPTO_PAYMENT_SETUP.md)
- 📊 [कार्यान्वयन सारांश](../reports/CRYPTO_PAYMENT_SUMMARY.md)

### क्रिप्टो भुगतान क्यों?
✅ क्रेडिट कार्ड या बैंक खाते की आवश्यकता नहीं  
✅ वैश्विक पहुंच, कोई क्षेत्रीय प्रतिबंध नहीं  
✅ कम लेनदेन शुल्क (1% बनाम 2.9%)  
✅ बेहतर गोपनीयता सुरक्षा  
✅ त्वरित पुष्टि (10-30 मिनट)  
✅ क्रिप्टो ट्रेडिंग सॉफ़्टवेयर के लिए सही फिट  

## 📜 लाइसेंस

यह प्रोजेक्ट एक **दोहरा लाइसेंस मॉडल** का उपयोग करता है:

### AGPL-3.0 ओपन सोर्स लाइसेंस
- ✅ उपयोग, संशोधन और वितरण के लिए मुफ्त
- ⚠️ **सभी व्युत्पन्न कार्यों को ओपन सोर्स होना चाहिए** और AGPL-3.0 के तहत जारी किया जाना चाहिए
- ⚠️ नेटवर्क सेवाओं के लिए भी सोर्स कोड प्रदान किया जाना चाहिए
- ⚠️ संशोधित कोड को समुदाय में वापस योगदान दिया जाना चाहिए

### वाणिज्यिक लाइसेंस
यदि आपको मालिकाना अनुप्रयोगों या सेवाओं में इस सॉफ़्टवेयर का उपयोग करने की आवश्यकता है, या आप अपने संशोधनों को ओपन सोर्स नहीं करना चाहते हैं, तो आपको एक वाणिज्यिक लाइसेंस खरीदना होगा।

**वाणिज्यिक लाइसेंस दायरा:**
- मालिकाना अनुप्रयोगों में उपयोग
- संशोधनों को ओपन सोर्स करने की कोई बाध्यता नहीं
- वितरण के लिए मालिकाना उत्पादों में एकीकृत करें
- प्राथमिकता तकनीकी समर्थन और अपडेट

**वाणिज्यिक लाइसेंस पूछताछ:**
- 📧 ईमेल: contact@quantmesh.io
- 🌐 वेबसाइट: https://quantmesh.io/commercial

---

### लाइसेंस विवरण

यह प्रोजेक्ट निम्नलिखित दोहरे लाइसेंस के तहत है:

1. **AGPL-3.0 (GNU Affero General Public License v3.0)**
   - उपयोग, संशोधन और वितरण के लिए मुफ्त
   - सभी व्युत्पन्न कार्यों को AGPL-3.0 के तहत ओपन सोर्स होना चाहिए
   - नेटवर्क सेवाओं के लिए भी सभी उपयोगकर्ताओं को सोर्स कोड प्रदान किया जाना चाहिए
   - संशोधनों को समुदाय में वापस योगदान दिया जाना चाहिए

2. **वाणिज्यिक लाइसेंस**
   - मालिकाना उपयोग के लिए आवश्यक
   - संशोधनों को ओपन सोर्स करने की कोई बाध्यता नहीं
   - प्राथमिकता समर्थन और अपडेट शामिल हैं

वाणिज्यिक लाइसेंसिंग पूछताछ के लिए, कृपया संपर्क करें:
- 📧 ईमेल: contact@quantmesh.io
- 🌐 वेबसाइट: https://quantmesh.io/commercial

## 🤝 योगदान

हम योगदान का स्वागत करते हैं! यहां आप कैसे मदद कर सकते हैं:

- ⭐ **इस रिपॉजिटरी को स्टार करें** यदि आप इसे सहायक पाते हैं
- 🍴 **फोर्क करें और उपयोग करें** प्रोजेक्ट
- 🐛 **बग रिपोर्ट करें** [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues) के माध्यम से
- 💡 **सुविधाएं सुझाएं** [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions) के माध्यम से
- 📝 **सुधारों के लिए PR जमा करें**
- 📖 **दस्तावेज़ीकरण में सुधार करें**

**नोट:** AGPL-3.0 लाइसेंस के अनुसार, इस प्रोजेक्ट में सभी योगदान एक ही AGPL-3.0 लाइसेंस के तहत जारी किए जाएंगे।

विस्तृत दिशानिर्देशों के लिए [CONTRIBUTING.md](../CONTRIBUTING.md) देखें।

## 🙏 आभार

मूल प्रोजेक्ट [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker) के लिए [dennisyang1986](https://github.com/dennisyang1986) को उनके ओपन सोर्स योगदान के लिए धन्यवाद, जिसने इस प्रोजेक्ट के लिए एक मजबूत आधार प्रदान किया। अधिक जानकारी के लिए, कृपया [NOTICE](../../NOTICE) फ़ाइल देखें।

---

## 📞 संपर्क और समर्थन

- 🌐 **वेबसाइट**: https://quantmesh.io
- 📧 **ईमेल**: contact@quantmesh.io
- 💬 **Discord**: [हमारे समुदाय में शामिल हों](https://discord.gg/YOUR_INVITE_LINK)
- 🐛 **Issues**: [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💬 **चर्चा**: [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📖 **दस्तावेज़ीकरण**: [पूर्ण दस्तावेज़ीकरण](../)

---

<div align="center">
  <strong>QuantMesh Team द्वारा ❤️ के साथ बनाया गया</strong><br/>
  <sub>यदि आप इस प्रोजेक्ट को सहायक पाते हैं, तो कृपया इसे ⭐ देने पर विचार करें</sub>
</div>

Copyright © 2025 QuantMesh Team. सभी अधिकार सुरक्षित।

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
