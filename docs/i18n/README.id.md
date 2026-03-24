<div align="center">
  <img src="../../logo/qm_thick_tail_white.svg" alt="QuantMesh Logo" width="200"/>
  
  # QuantMesh Market Maker
  
  **Pembuat Pasar Crypto Frekuensi Tinggi**

  [![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org/dl/)
  [![License](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](../../LICENSE)
  [![GitHub Stars](https://img.shields.io/github/stars/ghostsworm/quantmesh.svg?style=social&label=Stars)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Forks](https://img.shields.io/github/forks/ghostsworm/quantmesh.svg?style=social&label=Forks)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Issues](https://img.shields.io/github/issues/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/issues)
  [![GitHub Release](https://img.shields.io/github/release/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/releases)
  [![Website](https://img.shields.io/badge/Website-quantmesh.io-green.svg)](https://quantmesh.io)
  
  [简体中文](../../README.md) | [繁體中文](README.zh-TW.md) | [English](README.en.md) | [Español](README.es.md) | [Français](README.fr.md) | [Português](README.pt.md) | [Bahasa Indonesia](README.id.md)
</div>

---

## 🎯 Mengapa Memilih QuantMesh?

| Fitur | QuantMesh | Solusi Lain |
|---------|-----------|----------------|
| **Dukungan Exchange** | 20+ exchange | Biasanya 3-5 |
| **Latensi Respons** | Tingkat milidetik | Tingkat detik |
| **Kontrol Risiko** | Kontrol aktif multi-layer | Kontrol dasar |
| **Diuji Produksi** | Volume perdagangan $100M+ | Tidak diuji |
| **Antarmuka Web** | ✅ UI React lengkap | ❌ Tidak ada/Dasar |
| **Open Source** | AGPL-3.0 | Sumber tertutup/Terbatas |
| **Data Real-time** | Hanya WebSocket | REST polling |
| **Konkurensi** | 1000+ pesanan/detik | Terbatas |

**Keuntungan Utama:**
- ✅ **Teruji di medan**: Terbukti dengan volume perdagangan $100M+
- ✅ **Performa Tinggi**: Latensi sub-10ms dengan arsitektur WebSocket
- ✅ **Komprehensif**: Solusi lengkap dari perdagangan hingga pemantauan
- ✅ **Transparan**: Sepenuhnya open source, kode yang dapat diaudit
- ✅ **Dapat Diperluas**: Sistem plugin untuk kustomisasi

---

## 📊 Metrik Performa

- **Volume Perdagangan**: $100M+ diuji produksi
- **Latensi Respons**: <10ms (didorong WebSocket)
- **Exchange yang Didukung**: 20+
- **Pemrosesan Konkuren**: 1000+ pesanan/detik
- **Ketersediaan Sistem**: 99.9%+
- **Kapasitas Perdagangan Harian**: $3M+ per hari (contoh: ETHUSDC)

---

## 📖 Pengenalan

QuantMesh adalah sistem pembuat pasar cryptocurrency berperforma tinggi dan latensi rendah yang berfokus pada strategi perdagangan grid panjang untuk pasar kontrak perpetual. Dikembangkan dalam Go dan didorong oleh aliran data real-time WebSocket, ini bertujuan untuk memberikan dukungan likuiditas yang stabil untuk exchange utama seperti Binance, Bitget, dan Gate.io.

Setelah beberapa iterasi, kami telah menggunakan sistem ini untuk memperdagangkan lebih dari $100 juta dalam mata uang virtual. Misalnya, memperdagangkan Binance ETHUSDC dengan biaya nol, interval harga $1, dan $300 per pesanan, volume perdagangan harian dapat melebihi $3 juta, dan lebih dari $50 juta per bulan. Selama pasar berosilasi atau cenderung naik, ini akan terus menghasilkan keuntungan. Jika pasar jatuh sepihak, $30,000 dalam margin dapat menjamin tidak ada likuidasi untuk penurunan 1000 poin. Melalui perdagangan berkelanjutan untuk menurunkan biaya, pemulihan 50% cukup untuk mencapai titik impas, dan kembali ke harga pembukaan asli dapat menghasilkan keuntungan yang cukup besar. Jika ada penurunan cepat sepihak, sistem kontrol risiko aktif akan secara otomatis mengidentifikasi dan segera menghentikan perdagangan, hanya mengizinkan pesanan berkelanjutan ketika pasar pulih, tanpa khawatir tentang likuidasi dari lonjakan harga.

Contoh: Memulai perdagangan ETH pada 3000 poin, harga turun ke 2700 poin, kehilangan sekitar $3,000. Ketika harga pulih di atas 2850 poin, mencapai titik impas. Kembali ke 3000 poin, keuntungan berkisar dari $1,000 hingga $3,000.

## 📜 Asal Proyek

Proyek ini awalnya dikembangkan berdasarkan [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker), diterbitkan oleh [dennisyang1986](https://github.com/dennisyang1986) di bawah Lisensi MIT.

Berdasarkan proyek asli, kami telah membuat perbaikan dan ekstensi utama berikut:

- ✨ **Antarmuka Frontend Lengkap**: Menambahkan antarmuka manajemen web React + TypeScript yang menyediakan pemantauan perdagangan visual, manajemen konfigurasi, dan analisis data
- 🏦 **Ekspansi Exchange**: Diperluas dari 3 exchange (Binance, Bitget, Gate.io) dalam proyek asli menjadi **20+ exchange utama**
- 🔒 **Stabilitas Tingkat Keuangan**: Secara komprehensif meningkatkan keandalan sistem, termasuk penanganan kesalahan komprehensif, mekanisme keamanan konkurensi, jaminan konsistensi data, pemulihan otomatis, dll.
- 📊 **Pemantauan yang Ditingkatkan**: Sistem logging yang ditingkatkan, pengumpulan metrik (Prometheus), pemeriksaan kesehatan, dan peringatan real-time
- 🛡️ **Kontrol Risiko yang Diperkuat**: Pemantauan risiko multi-layer, rekonsiliasi otomatis, pemutus sirkuit anomali, dan perlindungan keamanan dana
- 🔌 **Sistem Plugin**: Dukungan untuk mekanisme plugin yang dapat diperluas untuk kustomisasi mudah dan pengembangan sekunder
- 📱 **Dukungan Internasionalisasi**: Antarmuka multi-bahasa (Cina/Inggris), dukungan i18n
- 🧪 **Dukungan Testnet**: Dukungan untuk lingkungan testnet dari beberapa exchange untuk pengembangan dan pengujian

Untuk deskripsi perbaikan terperinci dan informasi perangkat lunak pihak ketiga, silakan lihat file [NOTICE](../../NOTICE).

**Catatan Penting**: Proyek ini sekarang didistribusikan di bawah **GNU Affero General Public License v3.0 (AGPL-3.0)**. Sesuai dengan persyaratan Lisensi MIT dari proyek asli, kami telah mempertahankan pengakuan proyek asli.

## ✨ Fitur Utama

- **Dukungan Multi-Exchange**: Kompatibel dengan Binance, Bitget, Gate.io, Bybit, EdgeX, dan platform utama lainnya.
- **Respons Tingkat Milidetik**: Sepenuhnya didorong WebSocket (data pasar dan aliran pesanan), menghilangkan penundaan polling.
- **Strategi Grid Cerdas**: 
  - **Mode Jumlah Tetap**: Penggunaan modal yang lebih terkontrol.
  - **Sistem Super Slot**: Secara cerdas mengelola status pesanan dan posisi, mencegah konflik konkurensi.
- **Sistem Kontrol Risiko yang Kuat**:
  - **Kontrol Risiko Aktif**: Pemantauan real-time anomali volume K-line, secara otomatis menjeda perdagangan.
  - **Keamanan Dana**: Secara otomatis memeriksa saldo, leverage, dan risiko posisi maksimum sebelum startup.
  - **Rekonsiliasi Otomatis**: Secara teratur menyinkronkan status lokal dan exchange untuk memastikan konsistensi data.
- **Arsitektur Konkuren Tinggi**: Model konkuren yang efisien berdasarkan Goroutine + Channel + Sync.Map.

## 🏦 Exchange yang Didukung

| Exchange | Status | Volume Perdagangan Harian | Catatan |
|----------|--------|------------------------|-------|
| **Binance** | ✅ Stable | $50B+ | Exchange terbesar di dunia |
| **Bitget** | ✅ Stable | $10B+ | Platform perdagangan futures mainstream |
| **Gate.io** | ✅ Stable | $5B+ | Exchange mapan |
| **OKX** | ✅ Stable | $20B+ | Top 3 global, basis pengguna Cina yang kuat |
| **Bybit** | ✅ Stable | $15B+ | Platform perdagangan futures mainstream |
| **Huobi (HTX)** | ✅ Stable | $5B+ | Exchange mapan, pasar Cina yang kuat |
| **KuCoin** | ✅ Stable | $3B+ | Altcoin kaya, dukungan kontrak futures |
| **Kraken** | ✅ Stable | $2B+ | Kepatuhan kuat, mainstream di Eropa dan Amerika |
| **Bitfinex** | ✅ Stable | $1B+ | Exchange mapan, likuiditas baik |
| **MEXC** | ✅ Stable | $8B+ | Volume perdagangan futures besar, altcoin kaya, testnet didukung |
| **BingX** | ✅ Stable | $3B+ | Platform perdagangan sosial, pengalaman futures yang baik, testnet didukung |
| **Deribit** | ✅ Stable | $2B+ | Exchange opsi terbesar di dunia, mendukung futures + opsi, testnet didukung |
| **BitMEX** | ✅ Stable | $2B+ | Exchange derivatif mapan, hingga 100x leverage, testnet didukung |
| **Phemex** | ✅ Stable | $2B+ | Perdagangan futures tanpa biaya, mesin berperforma tinggi, testnet didukung |
| **WOO X** | ✅ Stable | $1.5B+ | Exchange tingkat institusional, likuiditas dalam, testnet didukung |
| **CoinEx** | ✅ Stable | $1B+ | Exchange mapan (2017), altcoin kaya, testnet didukung |
| **Bitrue** | ✅ Stable | $1B+ | Exchange ekosistem XRP utama, pasar Asia Tenggara yang kuat, testnet didukung |
| **XT.COM** | ✅ Stable | $800M+ | Exchange yang muncul, altcoin kaya, testnet didukung |
| **BTCC** | ✅ Stable | $500M+ | Exchange mapan (2011), exchange Bitcoin pertama Cina, testnet didukung |
| **AscendEX** | ✅ Stable | $400M+ | Exchange tingkat institusional, ramah DeFi, testnet didukung |
| **Poloniex** | ✅ Stable | $300M+ | Exchange mapan (2014), variasi koin kaya, testnet didukung |
| **Crypto.com** | ✅ Stable | $500M+ | Merek terkenal, puluhan juta pengguna secara global, testnet didukung |

## Arsitektur Modul

```
quantmesh_platform/
├── main.go                    # Titik masuk program utama, orkestrasi komponen
│
├── config/                    # Manajemen konfigurasi
│   └── config.go              # Memuat dan memvalidasi konfigurasi YAML
│
├── exchange/                  # Lapisan abstraksi exchange (inti)
│   ├── interface.go           # Antarmuka terpadu IExchange
│   ├── factory.go             # Pola factory untuk membuat instance exchange
│   ├── types.go               # Struktur data umum
│   ├── wrapper_*.go           # Adaptor (membungkus exchange)
│   ├── binance/               # Implementasi Binance
│   ├── bitget/                # Implementasi Bitget
│   └── gate/                  # Implementasi Gate.io
│
├── logger/                    # Sistem logging
│   └── logger.go              # Logging file + logging konsol
│
├── monitor/                   # Pemantauan harga
│   └── price_monitor.go       # Aliran harga unik global
│
├── order/                     # Lapisan eksekusi pesanan
│   └── executor_adapter.go    # Eksekutor pesanan (pembatasan laju + retry)
│
├── position/                  # Manajemen posisi (inti)
│   └── super_position_manager.go  # Manajer super slot
│
├── safety/                    # Keamanan dan kontrol risiko
│   ├── safety.go              # Pemeriksaan keamanan pra-startup
│   ├── risk_monitor.go        # Kontrol risiko aktif (pemantauan K-line)
│   ├── reconciler.go          # Rekonsiliasi posisi
│   └── order_cleaner.go       # Pembersihan pesanan
│
└── utils/                     # Fungsi utilitas
    └── orderid.go             # Generasi ID pesanan kustom
```

## Praktik Terbaik

1. **Untuk Status VIP Exchange**: Sistem ini adalah alat pembuatan volume. Jika fluktuasi harga tidak besar, $3,000 dalam margin dapat menghasilkan $10 juta volume perdagangan dalam 2-3 hari.

2. **Praktik Terbaik untuk Keuntungan**: Masuk pasar setelah putaran penurunan. Pertama beli posisi, lalu mulai perangkat lunak. Ini akan secara otomatis menjual grid demi grid ke atas. Ketika posisi Anda habis terjual, hentikan sistem. Jika Anda tidak yakin apakah pasar saat ini adalah titik rendah, Anda dapat mulai tanpa posisi dasar. Jika jatuh lebih jauh, tambahkan posisi pada titik rendah dan restart untuk melanjutkan penjualan. Ini memaksimalkan keuntungan. Ulangi siklus ini untuk keuntungan berkelanjutan. Jangan khawatir tentang penurunan - program terus menurunkan biaya. Selama pulih setengah, Anda mencapai titik impas.

## 🚀 Memulai

### Prasyarat
- Go 1.21 atau lebih tinggi
- Lingkungan jaringan yang mampu mengakses API exchange

### Instalasi

1. **Clone repository**
   ```bash
   git clone https://github.com/ghostsworm/quantmesh.git
   cd quantmesh
   ```

2. **Instal dependensi**
   ```bash
   go mod download
   ```

### Konfigurasi

1. Salin file konfigurasi contoh:
   ```bash
   cp config.example.yaml config.yaml
   ```

2. Edit `config.yaml` dan isi API Key dan parameter strategi Anda:

   ```yaml
   app:
     current_exchange: "binance"  # Pilih exchange

   exchanges:
     binance:
       api_key: "YOUR_API_KEY"
       secret_key: "YOUR_SECRET_KEY"
       fee_rate: 0.0002

   trading:
     symbol: "ETHUSDT"       # Pasangan perdagangan
     price_interval: 2       # Spasi grid (harga)
     order_quantity: 30     # Jumlah per grid (USDT)
     buy_window_size: 10    # Jumlah pesanan beli
     sell_window_size: 10   # Jumlah pesanan jual
   ```

### Penggunaan

#### Mode Produksi

Jalankan biner yang dikompilasi:

```bash
go run main.go
```

Atau build dan jalankan:

```bash
go build -o quantmesh
./quantmesh
```

Backend akan melayani file statis frontend pada port 28888 (default).

#### Mode Pengembangan

Untuk pengembangan frontend dengan hot reload dan debugging kode sumber:

**Opsi 1: Gunakan skrip pengembangan (Direkomendasikan)**

```bash
./dev.sh
```

Skrip ini akan:
- Memulai server backend Go pada port 28888
- Memulai server dev Vite pada port 15173
- Mengaktifkan hot reload untuk perubahan kode frontend
- Menyediakan source maps untuk debugging (tidak ada kode yang diminifikasi)

Kemudian akses aplikasi di: **http://localhost:15173**

**Opsi 2: Startup manual**

Terminal 1 - Mulai backend Go:
```bash
go run main.go
```

Terminal 2 - Mulai server dev Vite:
```bash
cd webui
pnpm dev
```

Kemudian akses aplikasi di: **http://localhost:15173**

**Manfaat Mode Pengembangan:**
- ✅ Hot reload - Perubahan kode frontend langsung tercermin
- ✅ Source maps - Debug dengan kode TypeScript/React asli (tidak diminifikasi)
- ✅ Fast refresh - Komponen React diperbarui tanpa kehilangan status
- ✅ Pesan kesalahan yang lebih baik - Lihat nama file dan nomor baris aktual

**Catatan:** Dalam mode pengembangan, server dev Vite memproksi permintaan API (`/api/*`) dan koneksi WebSocket (`/ws`) ke backend Go yang berjalan pada port 28888.

## 🏗️ Arsitektur

Sistem mengadopsi desain modular dengan komponen inti termasuk:

- **Lapisan Exchange**: Abstraksi antarmuka exchange terpadu, melindungi perbedaan API yang mendasar.
- **Monitor Harga**: Sumber harga WebSocket unik global, memastikan konsistensi keputusan.
- **Manajer Posisi Super**: Manajer posisi inti, mengelola siklus hidup pesanan berdasarkan mekanisme Slot.
- **Keamanan & Kontrol Risiko**: Kontrol risiko multi-layer, termasuk pemeriksaan startup, pemantauan runtime, dan pemutus sirkuit anomali.

Untuk dokumentasi arsitektur yang lebih detail, silakan lihat [ARCHITECTURE.md](../../ARCHITECTURE.md).

## 📊 Statistik Penggunaan & Perlindungan Privasi

QuantMesh menyertakan fitur statistik penggunaan opsional untuk mengumpulkan data penggunaan anonim, membantu kami memahami penggunaan proyek dan meningkatkan produk. **Semua pengumpulan data sepenuhnya transparan, kode dapat diaudit, dan dapat dinonaktifkan kapan saja.**

### 🔒 Perlindungan Privasi

**Data yang Kami Kumpulkan (Anonim):**
- ✅ **Informasi Dasar**: Nomor versi, sistem operasi, arsitektur, ID instance (UUID yang dihasilkan secara acak)
- ✅ **Statistik Penggunaan**: Nama exchange yang digunakan, pasangan perdagangan
- ✅ **Metrik Performa**: Latensi permintaan/respons API, latensi WebSocket
- ✅ **Aktivitas Perdagangan**: Arah perdagangan (beli/jual), tidak termasuk jumlah perdagangan

**Data yang TIDAK Kami Kumpulkan:**
- ❌ **Alamat IP**: Frontend memiliki penangkapan IP yang dinonaktifkan, backend menggunakan ID instance alih-alih IP
- ❌ **Geolokasi**: Tidak ada pengumpulan lintang/bujur, kota, atau informasi lokasi lainnya
- ❌ **Informasi Pribadi**: Tidak ada pengumpulan ID pengguna, email, nama, atau informasi identitas apa pun
- ❌ **Data Sensitif**: Tidak ada pengumpulan kunci API, jumlah perdagangan, saldo akun, atau informasi posisi
- ❌ **Data Keuangan**: Tidak ada pengumpulan informasi keuangan atau perdagangan sensitif

### 🛡️ Tindakan Perlindungan Privasi

1. **Mekanisme ID Instance**: Menggunakan UUID yang dihasilkan secara acak sebagai pengidentifikasi unik, disimpan dalam file `./data/instance_id`, tidak berisi informasi pribadi
2. **IP Frontend Dinonaktifkan**: PostHog SDK dikonfigurasi dengan `ip_capture: false`, menonaktifkan penangkapan alamat IP dan inferensi geolokasi
3. **Backend Tidak Mengirim IP**: Kode backend tidak mengirim alamat IP ke layanan statistik
4. **Sepenuhnya Opsional**: Pengguna dapat menonaktifkan statistik kapan saja melalui variabel lingkungan
5. **Transparansi Kode**: Semua kode statistik dapat diaudit, terletak di `utils/telemetry.go`

### ⚙️ Cara Menonaktifkan Statistik

**Metode 1: Variabel Lingkungan (Direkomendasikan)**
```bash
export QUANTMESH_DISABLE_TELEMETRY=1
```

**Metode 2: Nonaktifkan Frontend**
Dalam file `webui/.env.local`:
```bash
VITE_DISABLE_TELEMETRY=1
```

**Metode 3: Ubah Kode**
Edit `utils/telemetry.go`, setel `Enabled` ke `false`

### 📖 Dokumentasi Terperinci

Untuk informasi lebih detail tentang fitur statistik, silakan lihat:
- 📖 [Panduan Statistik Lengkap](../../docs/TELEMETRY_GUIDE.md)
- 🔒 [Panduan Perlindungan Privasi](../../docs/TELEMETRY_PRIVACY.md)
- 🚀 [Panduan Setup Cepat](../../docs/TELEMETRY_SIMPLE_GUIDE.md)

---

## ⚠️ Penolakan

Perangkat lunak ini hanya untuk tujuan pendidikan dan penelitian. Perdagangan cryptocurrency melibatkan risiko tinggi dan dapat mengakibatkan kerugian modal.
- Pengguna sepenuhnya bertanggung jawab atas keuntungan atau kerugian apa pun dari penggunaan perangkat lunak ini.
- Selalu uji secara menyeluruh di Testnet sebelum menggunakan dana nyata.
- Pengembang tidak bertanggung jawab atas kerugian karena bug perangkat lunak, latensi jaringan, atau kegagalan exchange.

## 🪙 Dukungan Pembayaran Crypto

QuantMesh mendukung pembayaran cryptocurrency untuk langganan dan lisensi:

### Cryptocurrency yang Didukung
- **BTC** (Bitcoin)
- **ETH** (Ethereum)
- **USDT** (Tether, ERC20)
- **USDC** (USD Coin, ERC20)

### Metode Pembayaran
1. **Coinbase Commerce** (Direkomendasikan)
   - Konfirmasi otomatis
   - Beberapa cryptocurrency didukung
   - Halaman pembayaran yang mudah

2. **Pembayaran Wallet Langsung**
   - Tidak ada keterlibatan pihak ketiga
   - Lebih banyak privasi
   - Konfirmasi manual (1-24 jam)

### Mulai Cepat
```bash
# Metode A: Coinbase Commerce (15 menit)
# 1. Daftar di https://commerce.coinbase.com
# 2. Konfigurasi API Key di .env.crypto
# 3. Mulai layanan

# Metode B: Wallet Langsung (5 menit)
# 1. Konfigurasi alamat wallet
# 2. Mulai layanan
# 3. Konfirmasi manual
```

### Dokumentasi
- 📖 [Panduan Pembayaran Pengguna](../CRYPTO_PAYMENT_GUIDE.md)
- 🚀 [Panduan Mulai Cepat](../CRYPTO_PAYMENT_QUICKSTART.md)
- 🔧 [Panduan Setup](../CRYPTO_PAYMENT_SETUP.md)
- 📊 [Ringkasan Implementasi](../reports/CRYPTO_PAYMENT_SUMMARY.md)

### Mengapa Pembayaran Crypto?
✅ Tidak perlu kartu kredit atau rekening bank  
✅ Aksesibilitas global, tidak ada pembatasan regional  
✅ Biaya transaksi lebih rendah (1% vs 2.9%)  
✅ Perlindungan privasi yang lebih baik  
✅ Konfirmasi cepat (10-30 menit)  
✅ Cocok sempurna untuk perangkat lunak perdagangan crypto  

## 📜 Lisensi

Proyek ini menggunakan **model Lisensi Ganda**:

### Lisensi Open Source AGPL-3.0
- ✅ Gratis digunakan, dimodifikasi, dan didistribusikan
- ⚠️ **Semua karya turunan harus open source** dan dirilis di bawah AGPL-3.0
- ⚠️ Kode sumber harus disediakan bahkan untuk layanan jaringan
- ⚠️ Kode yang dimodifikasi harus dikembalikan ke komunitas

### Lisensi Komersial
Jika Anda perlu menggunakan perangkat lunak ini dalam aplikasi atau layanan berpemilik, atau tidak ingin membuat modifikasi Anda open source, Anda perlu membeli lisensi komersial.

**Cakupan Lisensi Komersial:**
- Penggunaan dalam aplikasi berpemilik
- Tidak ada kewajiban untuk membuat modifikasi open source
- Integrasikan ke produk berpemilik untuk distribusi
- Dukungan teknis prioritas dan pembaruan

**Pertanyaan Lisensi Komersial:**
- 📧 Email: contact@quantmesh.io
- 🌐 Situs web: https://quantmesh.io/commercial

---

### Detail Lisensi

Proyek ini memiliki lisensi ganda di bawah:

1. **AGPL-3.0 (GNU Affero General Public License v3.0)**
   - Gratis untuk penggunaan, modifikasi, dan distribusi
   - Semua karya turunan harus open source di bawah AGPL-3.0
   - Kode sumber harus disediakan untuk semua pengguna, bahkan untuk layanan jaringan
   - Modifikasi harus dikembalikan ke komunitas

2. **Lisensi Komersial**
   - Diperlukan untuk penggunaan berpemilik
   - Tidak ada kewajiban untuk membuat modifikasi open source
   - Termasuk dukungan dan pembaruan prioritas

Untuk pertanyaan lisensi komersial, silakan hubungi:
- 📧 Email: contact@quantmesh.io
- 🌐 Situs web: https://quantmesh.io/commercial

## 🤝 Berkontribusi

Kami menyambut kontribusi! Berikut cara Anda dapat membantu:

- ⭐ **Beri bintang pada repo ini** jika Anda merasa membantu
- 🍴 **Fork dan gunakan** proyek
- 🐛 **Laporkan bug** melalui [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💡 **Sarankan fitur** melalui [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📝 **Kirim PR** untuk perbaikan
- 📖 **Tingkatkan dokumentasi**

**Catatan:** Menurut lisensi AGPL-3.0, semua kontribusi untuk proyek ini akan dirilis di bawah lisensi AGPL-3.0 yang sama.

Lihat [CONTRIBUTING.md](../../CONTRIBUTING.md) untuk pedoman detail.

## 🙏 Ucapan Terima Kasih

Terima kasih kepada proyek asli [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker) oleh [dennisyang1986](https://github.com/dennisyang1986) atas kontribusi open source mereka, yang memberikan fondasi yang kuat untuk proyek ini. Untuk informasi lebih lanjut, silakan lihat file [NOTICE](../../NOTICE).

---

## 📞 Kontak & Dukungan

- 🌐 **Situs web**: https://quantmesh.io
- 📧 **Email**: contact@quantmesh.io
- 💬 **Discord**: [Bergabunglah dengan komunitas kami](https://discord.gg/YOUR_INVITE_LINK)
- 🐛 **Issues**: [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💬 **Diskusi**: [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📖 **Dokumentasi**: [Dokumentasi Lengkap](../)

---

<div align="center">
  <strong>Dibuat dengan ❤️ oleh Tim QuantMesh</strong><br/>
  <sub>Jika Anda merasa proyek ini membantu, pertimbangkan untuk memberikannya ⭐</sub>
</div>

Copyright © 2025 QuantMesh Team. Hak Cipta Dilindungi.
