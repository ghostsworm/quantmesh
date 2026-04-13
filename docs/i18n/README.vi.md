<div align="center">
  <img src="../../logo/qm_thick_tail_white.svg" alt="QuantMesh Logo" width="200"/>
  
  # QuantMesh Market Maker
  
  **Nhà Tạo Thị Trường Crypto Tần Số Cao**

  [![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org/dl/)
  [![License](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](../../LICENSE)
  [![GitHub Stars](https://img.shields.io/github/stars/ghostsworm/quantmesh.svg?style=social&label=Stars)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Forks](https://img.shields.io/github/forks/ghostsworm/quantmesh.svg?style=social&label=Forks)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Issues](https://img.shields.io/github/issues/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/issues)
  [![GitHub Release](https://img.shields.io/github/release/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/releases)
  [![Website](https://img.shields.io/badge/Website-quantmesh.io-green.svg)](https://quantmesh.io)
  
  [简体中文](../../README.md) | [繁體中文](README.zh-TW.md) | [English](README.en.md) | [Español](README.es.md) | [Français](README.fr.md) | [Português](README.pt.md) | [Tiếng Việt](README.vi.md)
</div>

---

## 🎯 Tại Sao Chọn QuantMesh?

| Tính năng | QuantMesh | Giải pháp khác |
|---------|-----------|----------------|
| **Hỗ trợ Sàn giao dịch** | 20+ sàn | Thường 3-5 |
| **Độ trễ phản hồi** | Mức mili giây | Mức giây |
| **Kiểm soát rủi ro** | Kiểm soát chủ động đa lớp | Kiểm soát cơ bản |
| **Đã thử nghiệm sản xuất** | Khối lượng giao dịch $100M+ | Chưa thử nghiệm |
| **Giao diện Web** | ✅ UI React đầy đủ | ❌ Không có/Cơ bản |
| **Mã nguồn mở** | AGPL-3.0 | Mã nguồn đóng/Hạn chế |
| **Dữ liệu thời gian thực** | Chỉ WebSocket | REST polling |
| **Đồng thời** | 1000+ lệnh/giây | Hạn chế |

**Ưu điểm chính:**
- ✅ **Đã thử nghiệm**: Chứng minh với khối lượng giao dịch $100M+
- ✅ **Hiệu suất cao**: Độ trễ dưới 10ms với kiến trúc WebSocket
- ✅ **Toàn diện**: Giải pháp hoàn chỉnh từ giao dịch đến giám sát
- ✅ **Minh bạch**: Hoàn toàn mã nguồn mở, mã có thể kiểm tra
- ✅ **Có thể mở rộng**: Hệ thống plugin để tùy chỉnh

---

## 📊 Telemetry

**Third-party analytics (PostHog) has been removed.** The Go backend and install script no longer send usage events. Symbols in `utils/telemetry.go` are no-ops for compatibility.

The Web UI may still load a **1×1 pixel** on startup (`webui/src/services/telemetry.ts`). Disable with `VITE_DISABLE_TELEMETRY=1` in `webui/.env.local` or `localStorage` key `QUANTMESH_DISABLE_TELEMETRY` = `1`.

See [TELEMETRY_GUIDE.md](../../docs/TELEMETRY_GUIDE.md).
---

## ⚠️ Từ chối trách nhiệm

Phần mềm này chỉ dành cho mục đích giáo dục và nghiên cứu. Giao dịch tiền điện tử liên quan đến rủi ro cao và có thể dẫn đến mất vốn.
- Người dùng hoàn toàn chịu trách nhiệm về bất kỳ lợi nhuận hoặc tổn thất nào từ việc sử dụng phần mềm này.
- Luôn kiểm tra kỹ lưỡng trên Testnet trước khi sử dụng tiền thật.
- Các nhà phát triển không chịu trách nhiệm về tổn thất do lỗi phần mềm, độ trễ mạng hoặc lỗi sàn giao dịch.

## 🪙 Hỗ trợ thanh toán Crypto

QuantMesh hỗ trợ thanh toán tiền điện tử cho đăng ký và giấy phép:

### Tiền điện tử được hỗ trợ
- **BTC** (Bitcoin)
- **ETH** (Ethereum)
- **USDT** (Tether, ERC20)
- **USDC** (USD Coin, ERC20)

### Phương thức thanh toán
1. **Coinbase Commerce** (Được khuyến nghị)
   - Xác nhận tự động
   - Hỗ trợ nhiều tiền điện tử
   - Trang thanh toán dễ dàng

2. **Thanh toán ví trực tiếp**
   - Không có sự tham gia của bên thứ ba
   - Quyền riêng tư hơn
   - Xác nhận thủ công (1-24 giờ)

### Bắt đầu nhanh
```bash
# Phương pháp A: Coinbase Commerce (15 phút)
# 1. Đăng ký tại https://commerce.coinbase.com
# 2. Cấu hình API Key trong .env.crypto
# 3. Khởi động dịch vụ

# Phương pháp B: Ví trực tiếp (5 phút)
# 1. Cấu hình địa chỉ ví
# 2. Khởi động dịch vụ
# 3. Xác nhận thủ công
```

### Tài liệu
- 📖 [Hướng dẫn thanh toán người dùng](../CRYPTO_PAYMENT_GUIDE.md)
- 🚀 [Hướng dẫn bắt đầu nhanh](../CRYPTO_PAYMENT_QUICKSTART.md)
- 🔧 [Hướng dẫn thiết lập](../CRYPTO_PAYMENT_SETUP.md)
- 📊 [Tóm tắt triển khai](../reports/CRYPTO_PAYMENT_SUMMARY.md)

### Tại sao thanh toán Crypto?
✅ Không cần thẻ tín dụng hoặc tài khoản ngân hàng  
✅ Khả năng tiếp cận toàn cầu, không có hạn chế khu vực  
✅ Phí giao dịch thấp hơn (1% vs 2.9%)  
✅ Bảo vệ quyền riêng tư tốt hơn  
✅ Xác nhận nhanh (10-30 phút)  
✅ Phù hợp hoàn hảo cho phần mềm giao dịch crypto  

## 📜 Giấy phép

Dự án này sử dụng **mô hình Giấy phép kép**:

### Giấy phép mã nguồn mở AGPL-3.0
- ✅ Miễn phí sử dụng, sửa đổi và phân phối
- ⚠️ **Tất cả các tác phẩm phái sinh phải được mở mã nguồn** và phát hành theo AGPL-3.0
- ⚠️ Mã nguồn phải được cung cấp ngay cả cho dịch vụ mạng
- ⚠️ Mã đã sửa đổi phải được đóng góp trở lại cộng đồng

### Giấy phép thương mại
Nếu bạn cần sử dụng phần mềm này trong ứng dụng hoặc dịch vụ độc quyền, hoặc không muốn mở mã nguồn các sửa đổi của mình, bạn cần mua giấy phép thương mại.

**Phạm vi Giấy phép thương mại:**
- Sử dụng trong ứng dụng độc quyền
- Không có nghĩa vụ mở mã nguồn các sửa đổi
- Tích hợp vào sản phẩm độc quyền để phân phối
- Hỗ trợ kỹ thuật ưu tiên và cập nhật

**Yêu cầu Giấy phép thương mại:**
- 📧 Email: contact@quantmesh.io
- 🌐 Trang web: https://quantmesh.io/commercial

---

### Chi tiết giấy phép

Dự án này được cấp phép kép theo:

1. **AGPL-3.0 (GNU Affero General Public License v3.0)**
   - Miễn phí sử dụng, sửa đổi và phân phối
   - Tất cả các tác phẩm phái sinh phải được mở mã nguồn theo AGPL-3.0
   - Mã nguồn phải được cung cấp cho tất cả người dùng, ngay cả cho dịch vụ mạng
   - Các sửa đổi phải được đóng góp trở lại cộng đồng

2. **Giấy phép thương mại**
   - Bắt buộc cho việc sử dụng độc quyền
   - Không có nghĩa vụ mở mã nguồn các sửa đổi
   - Bao gồm hỗ trợ và cập nhật ưu tiên

Để yêu cầu cấp phép thương mại, vui lòng liên hệ:
- 📧 Email: contact@quantmesh.io
- 🌐 Trang web: https://quantmesh.io/commercial

## 🤝 Đóng góp

Chúng tôi hoan nghênh đóng góp! Đây là cách bạn có thể giúp:

- ⭐ **Đánh dấu sao repo này** nếu bạn thấy hữu ích
- 🍴 **Fork và sử dụng** dự án
- 🐛 **Báo cáo lỗi** qua [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💡 **Đề xuất tính năng** qua [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📝 **Gửi PR** để cải thiện
- 📖 **Cải thiện tài liệu**

**Lưu ý:** Theo giấy phép AGPL-3.0, tất cả đóng góp cho dự án này sẽ được phát hành theo cùng giấy phép AGPL-3.0.

Xem [CONTRIBUTING.md](../CONTRIBUTING.md) để biết hướng dẫn chi tiết.

## 🙏 Lời cảm ơn

Cảm ơn dự án gốc [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker) của [dennisyang1986](https://github.com/dennisyang1986) vì đóng góp mã nguồn mở của họ, đã cung cấp nền tảng vững chắc cho dự án này. Để biết thêm thông tin, vui lòng tham khảo tệp [NOTICE](../../NOTICE).

---

## 📞 Liên hệ & Hỗ trợ

- 🌐 **Trang web**: https://quantmesh.io
- 📧 **Email**: contact@quantmesh.io
- 💬 **Discord**: [Tham gia cộng đồng của chúng tôi](https://discord.gg/YOUR_INVITE_LINK)
- 🐛 **Issues**: [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💬 **Thảo luận**: [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📖 **Tài liệu**: [Tài liệu đầy đủ](../)

---

<div align="center">
  <strong>Được tạo với ❤️ bởi Nhóm QuantMesh</strong><br/>
  <sub>Nếu bạn thấy dự án này hữu ích, vui lòng cân nhắc cho nó ⭐</sub>
</div>

Copyright © 2025 QuantMesh Team. Bảo lưu mọi quyền.

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
