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

## 📊 Telemetry

**Third-party analytics (PostHog) has been removed.** The Go backend and install script no longer send usage events. Symbols in `utils/telemetry.go` are no-ops for compatibility.

The Web UI may still load a **1×1 pixel** on startup (`webui/src/services/telemetry.ts`). Disable with `VITE_DISABLE_TELEMETRY=1` in `webui/.env.local` or `localStorage` key `QUANTMESH_DISABLE_TELEMETRY` = `1`.

See [TELEMETRY_GUIDE.md](../../docs/TELEMETRY_GUIDE.md).
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

Lihat [CONTRIBUTING.md](../CONTRIBUTING.md) untuk pedoman detail.

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

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
