<div align="center">
  <img src="../../logo/qm_thick_tail_white.svg" alt="QuantMesh Logo" width="200"/>
  
  # QuantMesh Market Maker
  
  **Yüksek Frekanslı Kripto Piyasa Yapıcı**

  [![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org/dl/)
  [![License](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](../../LICENSE)
  [![GitHub Stars](https://img.shields.io/github/stars/ghostsworm/quantmesh.svg?style=social&label=Stars)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Forks](https://img.shields.io/github/forks/ghostsworm/quantmesh.svg?style=social&label=Forks)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Issues](https://img.shields.io/github/issues/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/issues)
  [![GitHub Release](https://img.shields.io/github/release/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/releases)
  [![Website](https://img.shields.io/badge/Website-quantmesh.io-green.svg)](https://quantmesh.io)
  
  [简体中文](../../README.md) | [繁體中文](README.zh-TW.md) | [English](README.en.md) | [Español](README.es.md) | [Français](README.fr.md) | [Português](README.pt.md) | [Türkçe](README.tr.md)
</div>

---

## 🎯 Neden QuantMesh'i Seçmelisiniz?

| Özellik | QuantMesh | Diğer Çözümler |
|---------|-----------|----------------|
| **Borsa Desteği** | 20+ borsa | Genellikle 3-5 |
| **Yanıt Gecikmesi** | Milisaniye seviyesi | Saniye seviyesi |
| **Risk Kontrolü** | Çok katmanlı aktif kontrol | Temel kontrol |
| **Üretimde Test Edildi** | $100M+ işlem hacmi | Test edilmedi |
| **Web Arayüzü** | ✅ Tam React UI | ❌ Yok/Temel |
| **Açık Kaynak** | AGPL-3.0 | Kapalı kaynak/Sınırlı |
| **Gerçek Zamanlı Veri** | Sadece WebSocket | REST polling |
| **Eşzamanlılık** | 1000+ sipariş/saniye | Sınırlı |

**Ana Avantajlar:**
- ✅ **Savaşta test edildi**: $100M+ işlem hacmi ile kanıtlandı
- ✅ **Yüksek Performans**: WebSocket mimarisi ile 10ms altı gecikme
- ✅ **Kapsamlı**: İşlemden izlemeye kadar tam çözüm
- ✅ **Şeffaf**: Tamamen açık kaynak, denetlenebilir kod
- ✅ **Genişletilebilir**: Özelleştirme için eklenti sistemi

---

## 📊 Telemetry

**Third-party analytics (PostHog) has been removed.** The Go backend and install script no longer send usage events. Symbols in `utils/telemetry.go` are no-ops for compatibility.

The Web UI may still load a **1×1 pixel** on startup (`webui/src/services/telemetry.ts`). Disable with `VITE_DISABLE_TELEMETRY=1` in `webui/.env.local` or `localStorage` key `QUANTMESH_DISABLE_TELEMETRY` = `1`.

See [TELEMETRY_GUIDE.md](../../docs/TELEMETRY_GUIDE.md).
---

## ⚠️ Sorumluluk Reddi

Bu yazılım yalnızca eğitim ve araştırma amaçlıdır. Kripto para işlemi yüksek risk içerir ve sermaye kaybına neden olabilir.
- Kullanıcılar bu yazılımı kullanmaktan kaynaklanan herhangi bir kâr veya kayıptan tamamen sorumludur.
- Gerçek fonları kullanmadan önce her zaman Testnet'te kapsamlı bir şekilde test edin.
- Geliştiriciler yazılım hataları, ağ gecikmesi veya borsa arızaları nedeniyle oluşan kayıplardan sorumlu değildir.

## 🪙 Kripto Ödeme Desteği

QuantMesh abonelikler ve lisanslar için kripto para ödemelerini destekler:

### Desteklenen Kripto Paralar
- **BTC** (Bitcoin)
- **ETH** (Ethereum)
- **USDT** (Tether, ERC20)
- **USDC** (USD Coin, ERC20)

### Ödeme Yöntemleri
1. **Coinbase Commerce** (Önerilen)
   - Otomatik onay
   - Birden fazla kripto para desteklenir
   - Kolay ödeme sayfası

2. **Doğrudan Cüzdan Ödemesi**
   - Üçüncü taraf müdahalesi yok
   - Daha fazla gizlilik
   - Manuel onay (1-24 saat)

### Hızlı Başlangıç
```bash
# Yöntem A: Coinbase Commerce (15 dakika)
# 1. https://commerce.coinbase.com adresinde kaydolun
# 2. .env.crypto dosyasında API Anahtarını yapılandırın
# 3. Hizmeti başlatın

# Yöntem B: Doğrudan Cüzdan (5 dakika)
# 1. Cüzdan adreslerini yapılandırın
# 2. Hizmeti başlatın
# 3. Manuel onay
```

### Dokümantasyon
- 📖 [Kullanıcı Ödeme Kılavuzu](../CRYPTO_PAYMENT_GUIDE.md)
- 🚀 [Hızlı Başlangıç Kılavuzu](../CRYPTO_PAYMENT_QUICKSTART.md)
- 🔧 [Kurulum Kılavuzu](../CRYPTO_PAYMENT_SETUP.md)
- 📊 [Uygulama Özeti](../reports/CRYPTO_PAYMENT_SUMMARY.md)

### Neden Kripto Ödemeler?
✅ Kredi kartı veya banka hesabı gerekmez  
✅ Küresel erişilebilirlik, bölgesel kısıtlama yok  
✅ Daha düşük işlem ücretleri (%1 vs %2.9)  
✅ Daha iyi gizlilik koruması  
✅ Hızlı onay (10-30 dakika)  
✅ Kripto işlem yazılımı için mükemmel uyum  

## 📜 Lisans

Bu proje **Çift Lisans modeli** kullanır:

### AGPL-3.0 Açık Kaynak Lisansı
- ✅ Kullanım, değiştirme ve dağıtım için ücretsiz
- ⚠️ **Tüm türev eserler açık kaynak olmalıdır** ve AGPL-3.0 altında yayınlanmalıdır
- ⚠️ Ağ hizmetleri için bile kaynak kodu sağlanmalıdır
- ⚠️ Değiştirilmiş kod topluma geri katkıda bulunmalıdır

### Ticari Lisans
Bu yazılımı sahipli uygulamalarda veya hizmetlerde kullanmanız gerekiyorsa veya değişikliklerinizi açık kaynak yapmak istemiyorsanız, ticari bir lisans satın almanız gerekir.

**Ticari Lisans Kapsamı:**
- Sahipli uygulamalarda kullanım
- Değişiklikleri açık kaynak yapma yükümlülüğü yok
- Dağıtım için sahipli ürünlere entegrasyon
- Öncelikli teknik destek ve güncellemeler

**Ticari Lisans Soruları:**
- 📧 E-posta: contact@quantmesh.io
- 🌐 Web sitesi: https://quantmesh.io/commercial

---

### Lisans Detayları

Bu proje aşağıdaki çift lisans altındadır:

1. **AGPL-3.0 (GNU Affero General Public License v3.0)**
   - Kullanım, değiştirme ve dağıtım için ücretsiz
   - Tüm türev eserler AGPL-3.0 altında açık kaynak olmalıdır
   - Ağ hizmetleri için bile tüm kullanıcılara kaynak kodu sağlanmalıdır
   - Değişiklikler topluma geri katkıda bulunmalıdır

2. **Ticari Lisans**
   - Sahipli kullanım için gereklidir
   - Değişiklikleri açık kaynak yapma yükümlülüğü yok
   - Öncelikli destek ve güncellemeler içerir

Ticari lisanslama soruları için lütfen iletişime geçin:
- 📧 E-posta: contact@quantmesh.io
- 🌐 Web sitesi: https://quantmesh.io/commercial

## 🤝 Katkıda Bulunma

Katkıları memnuniyetle karşılıyoruz! İşte nasıl yardımcı olabileceğiniz:

- ⭐ **Bu depoyu yıldızlayın** faydalı bulursanız
- 🍴 **Fork edin ve kullanın** projeyi
- 🐛 **Hata bildirin** [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues) üzerinden
- 💡 **Özellik önerin** [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions) üzerinden
- 📝 **İyileştirmeler için PR gönderin**
- 📖 **Dokümantasyonu iyileştirin**

**Not:** AGPL-3.0 lisansına göre, bu projeye yapılan tüm katkılar aynı AGPL-3.0 lisansı altında yayınlanacaktır.

Ayrıntılı yönergeler için [CONTRIBUTING.md](../CONTRIBUTING.md) dosyasına bakın.

## 🙏 Teşekkürler

Orijinal proje [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker) için [dennisyang1986](https://github.com/dennisyang1986)'a açık kaynak katkıları için teşekkürler, bu proje için sağlam bir temel sağladı. Daha fazla bilgi için lütfen [NOTICE](../../NOTICE) dosyasına bakın.

---

## 📞 İletişim ve Destek

- 🌐 **Web sitesi**: https://quantmesh.io
- 📧 **E-posta**: contact@quantmesh.io
- 💬 **Discord**: [Topluluğumuza katılın](https://discord.gg/YOUR_INVITE_LINK)
- 🐛 **Sorunlar**: [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💬 **Tartışmalar**: [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📖 **Dokümantasyon**: [Tam Dokümantasyon](../)

---

<div align="center">
  <strong>QuantMesh Ekibi tarafından ❤️ ile yapıldı</strong><br/>
  <sub>Bu projeyi faydalı bulursanız, lütfen ⭐ vermeyi düşünün</sub>
</div>

Copyright © 2025 QuantMesh Team. Tüm hakları saklıdır.

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
