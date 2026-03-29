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

## 📊 Performans Metrikleri

- **İşlem Hacmi**: $100M+ üretimde test edildi
- **Yanıt Gecikmesi**: <10ms (WebSocket ile çalışır)
- **Desteklenen Borsalar**: 20+
- **Eşzamanlı İşleme**: 1000+ sipariş/saniye
- **Sistem Kullanılabilirliği**: 99.9%+
- **Günlük İşlem Kapasitesi**: $3M+ günlük (örnek: ETHUSDC)

---

## 📖 Giriş

QuantMesh, sürekli sözleşme piyasaları için uzun grid işlem stratejilerine odaklanan yüksek performanslı, düşük gecikmeli bir kripto para piyasa yapıcı sistemidir. Go'da geliştirilmiş ve WebSocket gerçek zamanlı veri akışlarıyla çalışır, Binance, Bitget ve Gate.io gibi büyük borsalar için istikrarlı likidite desteği sağlamayı amaçlar.

Birkaç yinelemeden sonra, bu sistemi kullanarak $100 milyondan fazla sanal para biriminde işlem yaptık. Örneğin, sıfır ücretle Binance ETHUSDC işlemi, $1 fiyat aralığı ve sipariş başına $300 ile günlük işlem hacmi $3 milyonu aşabilir ve aylık $50 milyondan fazla olabilir. Piyasa salınım yaptığı veya yukarı doğru trend gösterdiği sürece, kâr üretmeye devam edecektir. Piyasa tek taraflı düşerse, $30,000 marj 1000 puanlık bir düşüş için likidasyon garantisi verebilir. Maliyetleri düşürmek için sürekli işlem yoluyla, %50'lik bir toparlanma başa baş noktasına ulaşmak için yeterlidir ve orijinal açılış fiyatına dönmek önemli kârlar sağlayabilir. Tek taraflı hızlı bir düşüş varsa, aktif risk kontrol sistemi otomatik olarak tespit edecek ve işlemi hemen durduracak, piyasa toparlandığında sadece devam eden siparişlere izin verecek, fiyat zirvelerinden likidasyon konusunda endişelenmeden.

Örnek: 3000 puanda ETH işlemine başlamak, fiyat 2700 puana düşer, yaklaşık $3,000 kayıp. Fiyat 2850 puanın üzerine çıktığında, başa baş noktasına ulaşır. 3000 puana dönmek, kârlar $1,000 ile $3,000 arasında değişir.

## 📜 Proje Kökeni

Bu proje orijinal olarak [dennisyang1986](https://github.com/dennisyang1986) tarafından MIT Lisansı altında yayınlanan [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker) temel alınarak geliştirilmiştir.

Orijinal projeye dayanarak, aşağıdaki önemli iyileştirmeleri ve genişletmeleri yaptık:

- ✨ **Tam Frontend Arayüzü**: Görsel işlem izleme, yapılandırma yönetimi ve veri analizi sağlayan bir React + TypeScript web yönetim arayüzü eklendi
- 🏦 **Borsa Genişletmesi**: Orijinal projedeki 3 borsadan (Binance, Bitget, Gate.io) **20+ büyük borsaya** genişletildi
- 🔒 **Finansal Seviye Kararlılık**: Kapsamlı hata işleme, eşzamanlılık güvenlik mekanizmaları, veri tutarlılığı garantileri, otomatik kurtarma vb. dahil olmak üzere sistem güvenilirliği kapsamlı olarak iyileştirildi
- 📊 **Geliştirilmiş İzleme**: Geliştirilmiş günlük sistemi, metrik toplama (Prometheus), sağlık kontrolleri ve gerçek zamanlı uyarılar
- 🛡️ **Güçlendirilmiş Risk Kontrolü**: Çok katmanlı risk izleme, otomatik mutabakat, anomali devre kesici ve fon güvenliği koruması
- 🔌 **Eklenti Sistemi**: Kolay özelleştirme ve ikincil geliştirme için genişletilebilir eklenti mekanizmaları desteği
- 📱 **Uluslararasılaştırma Desteği**: Çok dilli arayüz (Çince/İngilizce), i18n desteği
- 🧪 **Testnet Desteği**: Geliştirme ve test için birden fazla borsanın testnet ortamları desteği

Ayrıntılı iyileştirme açıklamaları ve üçüncü taraf yazılım bilgileri için lütfen [NOTICE](../../NOTICE) dosyasına bakın.

**Önemli Not**: Bu proje artık **GNU Affero General Public License v3.0 (AGPL-3.0)** altında dağıtılmaktadır. Orijinal projenin MIT Lisans gereksinimlerine uygun olarak, orijinal projenin kabulünü koruduk.

## ✨ Ana Özellikler

- **Çoklu Borsa Desteği**: Binance, Bitget, Gate.io, Bybit, EdgeX ve diğer büyük platformlarla uyumludur.
- **Milisaniye Seviyesi Yanıt**: Tamamen WebSocket ile çalışır (piyasa verileri ve sipariş akışı), polling gecikmelerini ortadan kaldırır.
- **Akıllı Grid Stratejisi**: 
  - **Sabit Miktar Modu**: Daha kontrollü sermaye kullanımı.
  - **Süper Slot Sistemi**: Sipariş ve pozisyon durumlarını akıllıca yönetir, eşzamanlılık çakışmalarını önler.
- **Güçlü Risk Kontrol Sistemi**:
  - **Aktif Risk Kontrolü**: K-line hacim anomalilerinin gerçek zamanlı izlenmesi, otomatik olarak işlemi duraklatır.
  - **Fon Güvenliği**: Başlatmadan önce otomatik olarak bakiye, kaldıraç ve maksimum pozisyon riskini kontrol eder.
  - **Otomatik Mutabakat**: Veri tutarlılığını sağlamak için yerel ve borsa durumlarını düzenli olarak senkronize eder.
- **Yüksek Eşzamanlılık Mimarisi**: Goroutine + Channel + Sync.Map'e dayalı verimli eşzamanlılık modeli.

## 🏦 Desteklenen Borsalar

| Borsa | Durum | Günlük İşlem Hacmi | Notlar |
|----------|--------|------------------------|-------|
| **Binance** | ✅ Stable | $50B+ | Dünyanın en büyük borsası |
| **Bitget** | ✅ Stable | $10B+ | Ana akım vadeli işlem platformu |
| **Gate.io** | ✅ Stable | $5B+ | Yerleşik borsa |
| **OKX** | ✅ Stable | $20B+ | Küresel olarak ilk 3, güçlü Çin kullanıcı tabanı |
| **Bybit** | ✅ Stable | $15B+ | Ana akım vadeli işlem platformu |
| **Huobi (HTX)** | ✅ Stable | $5B+ | Yerleşik borsa, güçlü Çin pazarı |
| **KuCoin** | ✅ Stable | $3B+ | Zengin altcoinler, vadeli sözleşme desteği |
| **Kraken** | ✅ Stable | $2B+ | Güçlü uyumluluk, Avrupa ve Amerika'da ana akım |
| **Bitfinex** | ✅ Stable | $1B+ | Yerleşik borsa, iyi likidite |
| **MEXC** | ✅ Stable | $8B+ | Büyük vadeli işlem hacmi, zengin altcoinler, testnet desteklenir |
| **BingX** | ✅ Stable | $3B+ | Sosyal işlem platformu, iyi vadeli işlem deneyimi, testnet desteklenir |
| **Deribit** | ✅ Stable | $2B+ | Dünyanın en büyük opsiyon borsası, vadeli işlemler + opsiyonlar destekler, testnet desteklenir |
| **BitMEX** | ✅ Stable | $2B+ | Yerleşik türev borsası, 100x'e kadar kaldıraç, testnet desteklenir |
| **Phemex** | ✅ Stable | $2B+ | Sıfır ücretli vadeli işlem, yüksek performanslı motor, testnet desteklenir |
| **WOO X** | ✅ Stable | $1.5B+ | Kurumsal seviye borsa, derin likidite, testnet desteklenir |
| **CoinEx** | ✅ Stable | $1B+ | Yerleşik borsa (2017), zengin altcoinler, testnet desteklenir |
| **Bitrue** | ✅ Stable | $1B+ | Ana XRP ekosistemi borsası, güçlü Güneydoğu Asya pazarı, testnet desteklenir |
| **XT.COM** | ✅ Stable | $800M+ | Gelişmekte olan borsa, zengin altcoinler, testnet desteklenir |
| **BTCC** | ✅ Stable | $500M+ | Yerleşik borsa (2011), Çin'in ilk Bitcoin borsası, testnet desteklenir |
| **AscendEX** | ✅ Stable | $400M+ | Kurumsal seviye borsa, DeFi dostu, testnet desteklenir |
| **Poloniex** | ✅ Stable | $300M+ | Yerleşik borsa (2014), zengin coin çeşitliliği, testnet desteklenir |
| **Crypto.com** | ✅ Stable | $500M+ | İyi bilinen marka, küresel olarak on milyonlarca kullanıcı, testnet desteklenir |

## Modül Mimarisi

```
quantmesh_platform/
├── main.go                    # Ana program girişi, bileşen orkestrasyonu
│
├── config/                    # Yapılandırma yönetimi
│   └── config.go              # YAML yapılandırma yükleme ve doğrulama
│
├── exchange/                  # Borsa soyutlama katmanı (çekirdek)
│   ├── interface.go           # IExchange birleşik arayüz
│   ├── factory.go             # Borsa örnekleri oluşturmak için fabrika deseni
│   ├── types.go               # Ortak veri yapıları
│   ├── wrapper_*.go           # Adaptörler (borsaları sarmalama)
│   ├── binance/               # Binance uygulaması
│   ├── bitget/                # Bitget uygulaması
│   └── gate/                  # Gate.io uygulaması
│
├── logger/                    # Günlük sistemi
│   └── logger.go              # Dosya günlüğü + konsol günlüğü
│
├── monitor/                   # Fiyat izleme
│   └── price_monitor.go       # Küresel benzersiz fiyat akışı
│
├── order/                     # Sipariş yürütme katmanı
│   └── executor_adapter.go    # Sipariş yürütücü (hız sınırlama + yeniden deneme)
│
├── position/                  # Pozisyon yönetimi (çekirdek)
│   └── super_position_manager.go  # Süper slot yöneticisi
│
├── safety/                    # Güvenlik ve risk kontrolü
│   ├── safety.go              # Başlatma öncesi güvenlik kontrolleri
│   ├── risk_monitor.go        # Aktif risk kontrolü (K-line izleme)
│   ├── reconciler.go          # Pozisyon mutabakatı
│   └── order_cleaner.go       # Sipariş temizleme
│
└── utils/                     # Yardımcı fonksiyonlar
    └── orderid.go             # Özel sipariş ID oluşturma
```

## En İyi Uygulamalar

1. **Borsa VIP Durumu İçin**: Bu sistem bir hacim üretim aracıdır. Fiyat dalgalanmaları büyük değilse, $3,000 marj 2-3 gün içinde $10 milyon işlem hacmi üretebilir.

2. **Kâr İçin En İyi Uygulama**: Bir düşüş turundan sonra piyasaya girin. Önce bir pozisyon satın alın, sonra yazılımı başlatın. Otomatik olarak grid grid yukarı satacaktır. Pozisyonunuz satıldığında, sistemi durdurun. Mevcut piyasanın düşük bir nokta olup olmadığından emin değilseniz, temel pozisyon olmadan başlayabilirsiniz. Daha fazla düşerse, düşük noktada bir pozisyon ekleyin ve satmaya devam etmek için yeniden başlatın. Bu kârı maksimize eder. Sürekli kâr için bu döngüyü tekrarlayın. Düşüşler konusunda endişelenmeyin - program sürekli maliyetleri düşürür. Yarısı kadar toparlandığı sürece başa baş noktasına ulaşırsınız.

## 🚀 Başlangıç

### Önkoşullar
- Go 1.21 veya üzeri
- Borsa API'lerine erişebilen ağ ortamı

### Kurulum

1. **Depoyu klonlayın**
   ```bash
   git clone https://github.com/ghostsworm/quantmesh.git
   cd quantmesh
   ```

2. **Bağımlılıkları yükleyin**
   ```bash
   go mod download
   ```

### Yapılandırma

1. Örnek yapılandırma dosyasını kopyalayın:
   ```bash
   cp config.example.yaml config.yaml
   ```

2. `config.yaml` dosyasını düzenleyin ve API Anahtarınızı ve strateji parametrelerinizi doldurun:

   ```yaml
   app:
     current_exchange: "binance"  # Borsa seçin

   exchanges:
     binance:
       api_key: "YOUR_API_KEY"
       secret_key: "YOUR_SECRET_KEY"
       fee_rate: 0.0002

   trading:
     symbol: "ETHUSDT"       # İşlem çifti
     price_interval: 2       # Grid aralığı (fiyat)
     order_quantity: 30     # Grid başına miktar (USDT)
     buy_window_size: 10    # Satın alma siparişi sayısı
     sell_window_size: 10   # Satış siparişi sayısı
   ```

### Kullanım

#### Üretim Modu

Derlenmiş ikiliyi çalıştırın:

```bash
go run main.go
```

Veya derleyin ve çalıştırın:

```bash
go build -o quantmesh
./quantmesh
```

Backend, varsayılan olarak 28888 portunda frontend statik dosyalarını sunacaktır.

#### Geliştirme Modu

Hot reload ve kaynak kodu hata ayıklama ile frontend geliştirme için:

**Seçenek 1: Geliştirme betiğini kullanın (Önerilen)**

```bash
./dev.sh
```

Bu betik:
- 28888 portunda Go backend sunucusunu başlatacak
- 15173 portunda Vite dev sunucusunu başlatacak
- Frontend kod değişiklikleri için hot reload'u etkinleştirecek
- Hata ayıklama için kaynak haritaları sağlayacak (minify edilmemiş kod)

Ardından uygulamaya şu adresten erişin: **http://localhost:15173**

**Seçenek 2: Manuel başlatma**

Terminal 1 - Go backend'i başlatın:
```bash
go run main.go
```

Terminal 2 - Vite dev sunucusunu başlatın:
```bash
cd webui
pnpm dev
```

Ardından uygulamaya şu adresten erişin: **http://localhost:15173**

**Geliştirme Modu Avantajları:**
- ✅ Hot reload - Frontend kod değişiklikleri anında yansıtılır
- ✅ Kaynak haritaları - Orijinal TypeScript/React koduyla hata ayıklama (minify edilmemiş)
- ✅ Hızlı yenileme - React bileşenleri durum kaybetmeden güncellenir
- ✅ Daha iyi hata mesajları - Gerçek dosya adlarını ve satır numaralarını görün

**Not:** Geliştirme modunda, Vite dev sunucusu API isteklerini (`/api/*`) ve WebSocket bağlantılarını (`/ws`) 28888 portunda çalışan Go backend'ine proxy eder.

## 🏗️ Mimari

Sistem, şunları içeren çekirdek bileşenlerle modüler bir tasarım benimser:

- **Borsa Katmanı**: Birleşik borsa arayüz soyutlaması, temel API farklılıklarını kalkanlar.
- **Fiyat Monitörü**: Küresel benzersiz WebSocket fiyat kaynağı, karar tutarlılığını sağlar.
- **Süper Pozisyon Yöneticisi**: Çekirdek pozisyon yöneticisi, Slot mekanizmasına dayalı sipariş yaşam döngüsünü yönetir.
- **Güvenlik ve Risk Kontrolü**: Başlatma kontrolleri, çalışma zamanı izleme ve anomali devre kesici dahil çok katmanlı risk kontrolü.

Daha ayrıntılı mimari dokümantasyon için lütfen [ARCHITECTURE.md](../../ARCHITECTURE.md) dosyasına bakın.

## 📊 Kullanım İstatistikleri ve Gizlilik Koruması

QuantMesh, proje kullanımını anlamamıza ve ürünü iyileştirmemize yardımcı olan anonim kullanım verilerini toplamak için isteğe bağlı bir kullanım istatistikleri özelliği içerir. **Tüm veri toplama tamamen şeffaftır, kod denetlenebilir ve istendiğinde devre dışı bırakılabilir.**

### 🔒 Gizlilik Koruması

**Topladığımız Veriler (Anonim):**
- ✅ **Temel Bilgiler**: Sürüm numarası, işletim sistemi, mimari, örnek ID (rastgele oluşturulan UUID)
- ✅ **Kullanım İstatistikleri**: Kullanılan borsa adları, işlem çiftleri
- ✅ **Performans Metrikleri**: API istek/yanıt gecikmesi, WebSocket gecikmesi
- ✅ **İşlem Aktivitesi**: İşlem yönü (alış/satış), işlem tutarları hariç

**Toplamadığımız Veriler:**
- ❌ **IP Adresi**: Frontend'de IP yakalama devre dışı, backend IP yerine örnek ID kullanır
- ❌ **Coğrafi Konum**: Enlem/boylam, şehir veya diğer konum bilgilerinin toplanması yok
- ❌ **Kişisel Bilgiler**: Kullanıcı ID'leri, e-postalar, isimler veya herhangi bir kimlik bilgisi toplanması yok
- ❌ **Hassas Veriler**: API anahtarları, işlem tutarları, hesap bakiyeleri veya pozisyon bilgilerinin toplanması yok
- ❌ **Finansal Veriler**: Herhangi bir finansal veya işlem hassas bilgilerinin toplanması yok

### 🛡️ Gizlilik Koruması Önlemleri

1. **Örnek ID Mekanizması**: Benzersiz tanımlayıcı olarak rastgele oluşturulan UUID kullanır, `./data/instance_id` dosyasında saklanır, kişisel bilgi içermez
2. **Frontend IP Devre Dışı**: PostHog SDK `ip_capture: false` ile yapılandırılmış, IP adresi yakalamayı ve coğrafi konum çıkarımını devre dışı bırakır
3. **Backend IP Göndermez**: Backend kodu istatistik hizmetine IP adresleri göndermez
4. **Tamamen İsteğe Bağlı**: Kullanıcılar ortam değişkenleri aracılığıyla istediğinde istatistikleri devre dışı bırakabilir
5. **Kod Şeffaflığı**: Tüm istatistik kodu denetlenebilir, `utils/telemetry.go` konumunda

### ⚙️ İstatistikleri Nasıl Devre Dışı Bırakılır

**Yöntem 1: Ortam Değişkeni (Önerilen)**
```bash
export QUANTMESH_DISABLE_TELEMETRY=1
```

**Yöntem 2: Frontend'i Devre Dışı Bırak**
`webui/.env.local` dosyasında:
```bash
VITE_DISABLE_TELEMETRY=1
```

**Yöntem 3: Kodu Değiştir**
`utils/telemetry.go` dosyasını düzenleyin, `Enabled`'ı `false` olarak ayarlayın

### 📖 Ayrıntılı Dokümantasyon

İstatistik özelliği hakkında daha ayrıntılı bilgi için lütfen bakın:
- 📖 [Tam İstatistik Kılavuzu](../../docs/TELEMETRY_GUIDE.md)
- 🔒 [Gizlilik Koruması Kılavuzu](../../docs/TELEMETRY_PRIVACY.md)
- 🚀 [Hızlı Kurulum Kılavuzu](../../docs/TELEMETRY_SIMPLE_GUIDE.md)

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

Ayrıntılı yönergeler için [CONTRIBUTING.md](../../CONTRIBUTING.md) dosyasına bakın.

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
