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

## 📊 Chỉ số hiệu suất

- **Khối lượng giao dịch**: $100M+ đã thử nghiệm sản xuất
- **Độ trễ phản hồi**: <10ms (được điều khiển bởi WebSocket)
- **Sàn giao dịch được hỗ trợ**: 20+
- **Xử lý đồng thời**: 1000+ lệnh/giây
- **Tính khả dụng của hệ thống**: 99.9%+
- **Khả năng giao dịch hàng ngày**: $3M+ mỗi ngày (ví dụ: ETHUSDC)

---

## 📖 Giới thiệu

QuantMesh là hệ thống nhà tạo thị trường tiền điện tử hiệu suất cao, độ trễ thấp tập trung vào các chiến lược giao dịch lưới dài cho thị trường hợp đồng vĩnh viễn. Được phát triển bằng Go và được điều khiển bởi luồng dữ liệu thời gian thực WebSocket, nó nhằm mục đích cung cấp hỗ trợ thanh khoản ổn định cho các sàn giao dịch lớn như Binance, Bitget và Gate.io.

Sau nhiều lần lặp lại, chúng tôi đã sử dụng hệ thống này để giao dịch hơn $100 triệu tiền ảo. Ví dụ, giao dịch Binance ETHUSDC với phí bằng không, khoảng giá $1 và $300 mỗi lệnh, khối lượng giao dịch hàng ngày có thể vượt quá $3 triệu và hơn $50 triệu mỗi tháng. Miễn là thị trường dao động hoặc có xu hướng tăng, nó sẽ tiếp tục tạo ra lợi nhuận. Nếu thị trường giảm một chiều, $30,000 ký quỹ có thể đảm bảo không có thanh lý cho mức giảm 1000 điểm. Thông qua giao dịch liên tục để giảm chi phí, phục hồi 50% là đủ để đạt điểm hòa vốn, và quay trở lại giá mở ban đầu có thể tạo ra lợi nhuận đáng kể. Nếu có sự sụt giảm nhanh một chiều, hệ thống kiểm soát rủi ro chủ động sẽ tự động xác định và ngay lập tức dừng giao dịch, chỉ cho phép các lệnh tiếp tục khi thị trường phục hồi, mà không lo lắng về thanh lý từ các đỉnh giá.

Ví dụ: Bắt đầu giao dịch ETH ở mức 3000 điểm, giá giảm xuống 2700 điểm, mất khoảng $3,000. Khi giá phục hồi trên 2850 điểm, đạt điểm hòa vốn. Quay trở lại 3000 điểm, lợi nhuận dao động từ $1,000 đến $3,000.

## 📜 Nguồn gốc dự án

Dự án này ban đầu được phát triển dựa trên [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker), được xuất bản bởi [dennisyang1986](https://github.com/dennisyang1986) theo Giấy phép MIT.

Dựa trên dự án gốc, chúng tôi đã thực hiện các cải tiến và mở rộng chính sau:

- ✨ **Giao diện Frontend đầy đủ**: Thêm giao diện quản lý web React + TypeScript cung cấp giám sát giao dịch trực quan, quản lý cấu hình và phân tích dữ liệu
- 🏦 **Mở rộng Sàn giao dịch**: Mở rộng từ 3 sàn giao dịch (Binance, Bitget, Gate.io) trong dự án gốc lên **20+ sàn giao dịch lớn**
- 🔒 **Ổn định cấp tài chính**: Cải thiện toàn diện độ tin cậy hệ thống, bao gồm xử lý lỗi toàn diện, cơ chế an toàn đồng thời, đảm bảo tính nhất quán dữ liệu, khôi phục tự động, v.v.
- 📊 **Giám sát nâng cao**: Hệ thống ghi nhật ký được cải thiện, thu thập số liệu (Prometheus), kiểm tra sức khỏe và cảnh báo thời gian thực
- 🛡️ **Kiểm soát rủi ro được tăng cường**: Giám sát rủi ro đa lớp, đối chiếu tự động, ngắt mạch bất thường và bảo vệ an toàn quỹ
- 🔌 **Hệ thống Plugin**: Hỗ trợ cơ chế plugin có thể mở rộng để tùy chỉnh dễ dàng và phát triển thứ cấp
- 📱 **Hỗ trợ Quốc tế hóa**: Giao diện đa ngôn ngữ (Trung/Anh), hỗ trợ i18n
- 🧪 **Hỗ trợ Testnet**: Hỗ trợ môi trường testnet của nhiều sàn giao dịch để phát triển và thử nghiệm

Để mô tả cải tiến chi tiết và thông tin phần mềm bên thứ ba, vui lòng tham khảo tệp [NOTICE](../../NOTICE).

**Lưu ý quan trọng**: Dự án này hiện được phân phối theo **GNU Affero General Public License v3.0 (AGPL-3.0)**. Phù hợp với các yêu cầu Giấy phép MIT của dự án gốc, chúng tôi đã giữ lại sự công nhận của dự án gốc.

## ✨ Tính năng chính

- **Hỗ trợ đa sàn giao dịch**: Tương thích với Binance, Bitget, Gate.io, Bybit, EdgeX và các nền tảng lớn khác.
- **Phản hồi mức mili giây**: Hoàn toàn được điều khiển bởi WebSocket (dữ liệu thị trường và luồng lệnh), loại bỏ độ trễ polling.
- **Chiến lược lưới thông minh**: 
  - **Chế độ số tiền cố định**: Sử dụng vốn có thể kiểm soát hơn.
  - **Hệ thống Super Slot**: Quản lý thông minh trạng thái lệnh và vị thế, ngăn chặn xung đột đồng thời.
- **Hệ thống kiểm soát rủi ro mạnh mẽ**:
  - **Kiểm soát rủi ro chủ động**: Giám sát thời gian thực các bất thường khối lượng K-line, tự động tạm dừng giao dịch.
  - **An toàn quỹ**: Tự động kiểm tra số dư, đòn bẩy và rủi ro vị thế tối đa trước khi khởi động.
  - **Đối chiếu tự động**: Đồng bộ hóa thường xuyên trạng thái cục bộ và sàn giao dịch để đảm bảo tính nhất quán dữ liệu.
- **Kiến trúc đồng thời cao**: Mô hình đồng thời hiệu quả dựa trên Goroutine + Channel + Sync.Map.

## 🏦 Sàn giao dịch được hỗ trợ

| Sàn giao dịch | Trạng thái | Khối lượng giao dịch hàng ngày | Ghi chú |
|----------|--------|------------------------|-------|
| **Binance** | ✅ Stable | $50B+ | Sàn giao dịch lớn nhất thế giới |
| **Bitget** | ✅ Stable | $10B+ | Nền tảng giao dịch hợp đồng tương lai chính |
| **Gate.io** | ✅ Stable | $5B+ | Sàn giao dịch đã thành lập |
| **OKX** | ✅ Stable | $20B+ | Top 3 toàn cầu, cơ sở người dùng Trung Quốc mạnh |
| **Bybit** | ✅ Stable | $15B+ | Nền tảng giao dịch hợp đồng tương lai chính |
| **Huobi (HTX)** | ✅ Stable | $5B+ | Sàn giao dịch đã thành lập, thị trường Trung Quốc mạnh |
| **KuCoin** | ✅ Stable | $3B+ | Nhiều altcoin, hỗ trợ hợp đồng tương lai |
| **Kraken** | ✅ Stable | $2B+ | Tuân thủ mạnh, chính thống ở Châu Âu và Mỹ |
| **Bitfinex** | ✅ Stable | $1B+ | Sàn giao dịch đã thành lập, thanh khoản tốt |
| **MEXC** | ✅ Stable | $8B+ | Khối lượng giao dịch hợp đồng tương lai lớn, nhiều altcoin, hỗ trợ testnet |
| **BingX** | ✅ Stable | $3B+ | Nền tảng giao dịch xã hội, trải nghiệm hợp đồng tương lai tốt, hỗ trợ testnet |
| **Deribit** | ✅ Stable | $2B+ | Sàn giao dịch quyền chọn lớn nhất thế giới, hỗ trợ hợp đồng tương lai + quyền chọn, hỗ trợ testnet |
| **BitMEX** | ✅ Stable | $2B+ | Sàn giao dịch phái sinh đã thành lập, đòn bẩy lên đến 100x, hỗ trợ testnet |
| **Phemex** | ✅ Stable | $2B+ | Giao dịch hợp đồng tương lai không phí, động cơ hiệu suất cao, hỗ trợ testnet |
| **WOO X** | ✅ Stable | $1.5B+ | Sàn giao dịch cấp tổ chức, thanh khoản sâu, hỗ trợ testnet |
| **CoinEx** | ✅ Stable | $1B+ | Sàn giao dịch đã thành lập (2017), nhiều altcoin, hỗ trợ testnet |
| **Bitrue** | ✅ Stable | $1B+ | Sàn giao dịch hệ sinh thái XRP chính, thị trường Đông Nam Á mạnh, hỗ trợ testnet |
| **XT.COM** | ✅ Stable | $800M+ | Sàn giao dịch mới nổi, nhiều altcoin, hỗ trợ testnet |
| **BTCC** | ✅ Stable | $500M+ | Sàn giao dịch đã thành lập (2011), sàn Bitcoin đầu tiên của Trung Quốc, hỗ trợ testnet |
| **AscendEX** | ✅ Stable | $400M+ | Sàn giao dịch cấp tổ chức, thân thiện với DeFi, hỗ trợ testnet |
| **Poloniex** | ✅ Stable | $300M+ | Sàn giao dịch đã thành lập (2014), đa dạng coin phong phú, hỗ trợ testnet |
| **Crypto.com** | ✅ Stable | $500M+ | Thương hiệu nổi tiếng, hàng chục triệu người dùng toàn cầu, hỗ trợ testnet |

## Kiến trúc mô-đun

```
quantmesh_platform/
├── main.go                    # Điểm vào chương trình chính, điều phối thành phần
│
├── config/                    # Quản lý cấu hình
│   └── config.go              # Tải và xác thực cấu hình YAML
│
├── exchange/                  # Lớp trừu tượng sàn giao dịch (lõi)
│   ├── interface.go           # Giao diện thống nhất IExchange
│   ├── factory.go             # Mẫu factory để tạo instance sàn giao dịch
│   ├── types.go               # Cấu trúc dữ liệu chung
│   ├── wrapper_*.go           # Bộ chuyển đổi (bọc sàn giao dịch)
│   ├── binance/               # Triển khai Binance
│   ├── bitget/                # Triển khai Bitget
│   └── gate/                  # Triển khai Gate.io
│
├── logger/                    # Hệ thống ghi nhật ký
│   └── logger.go              # Ghi nhật ký tệp + ghi nhật ký console
│
├── monitor/                   # Giám sát giá
│   └── price_monitor.go       # Luồng giá duy nhất toàn cầu
│
├── order/                     # Lớp thực thi lệnh
│   └── executor_adapter.go    # Trình thực thi lệnh (giới hạn tốc độ + thử lại)
│
├── position/                  # Quản lý vị thế (lõi)
│   └── super_position_manager.go  # Trình quản lý super slot
│
├── safety/                    # An toàn và kiểm soát rủi ro
│   ├── safety.go              # Kiểm tra an toàn trước khi khởi động
│   ├── risk_monitor.go        # Kiểm soát rủi ro chủ động (giám sát K-line)
│   ├── reconciler.go          # Đối chiếu vị thế
│   └── order_cleaner.go       # Dọn dẹp lệnh
│
└── utils/                     # Hàm tiện ích
    └── orderid.go             # Tạo ID lệnh tùy chỉnh
```

## Thực hành tốt nhất

1. **Cho trạng thái VIP Sàn giao dịch**: Hệ thống này là công cụ tạo khối lượng. Nếu biến động giá không lớn, $3,000 ký quỹ có thể tạo ra $10 triệu khối lượng giao dịch trong 2-3 ngày.

2. **Thực hành tốt nhất cho lợi nhuận**: Vào thị trường sau một đợt giảm. Đầu tiên mua một vị thế, sau đó khởi động phần mềm. Nó sẽ tự động bán lưới từng lưới lên trên. Khi vị thế của bạn được bán hết, dừng hệ thống. Nếu bạn không chắc chắn liệu thị trường hiện tại có phải là điểm thấp hay không, bạn có thể bắt đầu mà không có vị thế cơ sở. Nếu nó giảm thêm, thêm vị thế ở điểm thấp và khởi động lại để tiếp tục bán. Điều này tối đa hóa lợi nhuận. Lặp lại chu kỳ này để liên tục có lợi nhuận. Đừng lo lắng về sự sụt giảm - chương trình liên tục giảm chi phí. Miễn là nó phục hồi một nửa, bạn đạt điểm hòa vốn.

## 🚀 Bắt đầu

### Yêu cầu
- Go 1.21 trở lên
- Môi trường mạng có khả năng truy cập API sàn giao dịch

### Cài đặt

1. **Sao chép kho lưu trữ**
   ```bash
   git clone https://github.com/ghostsworm/quantmesh.git
   cd quantmesh
   ```

2. **Cài đặt phụ thuộc**
   ```bash
   go mod download
   ```

### Cấu hình

> **Runtime SSOT:** The primary database table `app_config` holds the authoritative configuration. One-time YAML import: `./quantmesh --migrate-app-config` (with `QUANTMESH_IMPORT_YAML`, or `config.yaml` in the working directory), or run `./quantmesh /path/to/file.yaml` as the first argument. See [`docs/config-database-design.md`](../config-database-design.md).

1. Sao chép tệp cấu hình ví dụ:
   ```bash
   cp config.example.yaml config.yaml
   ```

2. Chỉnh sửa `config.yaml` và điền API Key và tham số chiến lược của bạn:

   ```yaml
   app:
     current_exchange: "binance"  # Chọn sàn giao dịch

   exchanges:
     binance:
       api_key: "YOUR_API_KEY"
       secret_key: "YOUR_SECRET_KEY"
       fee_rate: 0.0002

   trading:
     symbol: "ETHUSDT"       # Cặp giao dịch
     price_interval: 2       # Khoảng cách lưới (giá)
     order_quantity: 30     # Số tiền mỗi lưới (USDT)
     buy_window_size: 10    # Số lệnh mua
     sell_window_size: 10   # Số lệnh bán
   ```

### Sử dụng

#### Chế độ sản xuất

Chạy tệp nhị phân đã biên dịch:

```bash
go run main.go
```

Hoặc build và chạy:

```bash
go build -o quantmesh
./quantmesh
```

Backend sẽ phục vụ các tệp tĩnh frontend trên cổng 28888 (mặc định).

#### Chế độ phát triển

Để phát triển frontend với hot reload và gỡ lỗi mã nguồn:

**Tùy chọn 1: Sử dụng script phát triển (Được khuyến nghị)**

```bash
./dev.sh
```

Script này sẽ:
- Khởi động máy chủ backend Go trên cổng 28888
- Khởi động máy chủ dev Vite trên cổng 15173
- Bật hot reload cho các thay đổi mã frontend
- Cung cấp source maps để gỡ lỗi (không có mã được thu nhỏ)

Sau đó truy cập ứng dụng tại: **http://localhost:15173**

**Tùy chọn 2: Khởi động thủ công**

Terminal 1 - Khởi động backend Go:
```bash
go run main.go
```

Terminal 2 - Khởi động máy chủ dev Vite:
```bash
cd webui
pnpm dev
```

Sau đó truy cập ứng dụng tại: **http://localhost:15173**

**Lợi ích Chế độ phát triển:**
- ✅ Hot reload - Thay đổi mã frontend được phản ánh ngay lập tức
- ✅ Source maps - Gỡ lỗi với mã TypeScript/React gốc (không được thu nhỏ)
- ✅ Fast refresh - Các thành phần React cập nhật mà không mất trạng thái
- ✅ Thông báo lỗi tốt hơn - Xem tên tệp và số dòng thực tế

**Lưu ý:** Trong chế độ phát triển, máy chủ dev Vite proxy các yêu cầu API (`/api/*`) và kết nối WebSocket (`/ws`) đến backend Go đang chạy trên cổng 28888.

## 🏗️ Kiến trúc

Hệ thống áp dụng thiết kế mô-đun với các thành phần cốt lõi bao gồm:

- **Lớp Sàn giao dịch**: Trừu tượng giao diện sàn giao dịch thống nhất, che chắn sự khác biệt API cơ bản.
- **Trình giám sát giá**: Nguồn giá WebSocket duy nhất toàn cầu, đảm bảo tính nhất quán quyết định.
- **Trình quản lý vị thế siêu**: Trình quản lý vị thế cốt lõi, quản lý vòng đời lệnh dựa trên cơ chế Slot.
- **An toàn & Kiểm soát rủi ro**: Kiểm soát rủi ro đa lớp, bao gồm kiểm tra khởi động, giám sát thời gian chạy và ngắt mạch bất thường.

Để tài liệu kiến trúc chi tiết hơn, vui lòng tham khảo [ARCHITECTURE.md](../../ARCHITECTURE.md).

## 📊 Thống kê sử dụng & Bảo vệ quyền riêng tư

QuantMesh bao gồm tính năng thống kê sử dụng tùy chọn để thu thập dữ liệu sử dụng ẩn danh, giúp chúng tôi hiểu việc sử dụng dự án và cải thiện sản phẩm. **Tất cả việc thu thập dữ liệu hoàn toàn minh bạch, mã có thể kiểm tra và có thể tắt bất cứ lúc nào.**

### 🔒 Bảo vệ quyền riêng tư

**Dữ liệu chúng tôi thu thập (Ẩn danh):**
- ✅ **Thông tin cơ bản**: Số phiên bản, hệ điều hành, kiến trúc, ID instance (UUID được tạo ngẫu nhiên)
- ✅ **Thống kê sử dụng**: Tên sàn giao dịch được sử dụng, cặp giao dịch
- ✅ **Chỉ số hiệu suất**: Độ trễ yêu cầu/phản hồi API, độ trễ WebSocket
- ✅ **Hoạt động giao dịch**: Hướng giao dịch (mua/bán), không bao gồm số tiền giao dịch

**Dữ liệu chúng tôi KHÔNG thu thập:**
- ❌ **Địa chỉ IP**: Frontend có tính năng chụp IP bị tắt, backend sử dụng ID instance thay vì IP
- ❌ **Vị trí địa lý**: Không thu thập vĩ độ/kinh độ, thành phố hoặc thông tin vị trí khác
- ❌ **Thông tin cá nhân**: Không thu thập ID người dùng, email, tên hoặc bất kỳ thông tin nhận dạng nào
- ❌ **Dữ liệu nhạy cảm**: Không thu thập khóa API, số tiền giao dịch, số dư tài khoản hoặc thông tin vị thế
- ❌ **Dữ liệu tài chính**: Không thu thập bất kỳ thông tin tài chính hoặc nhạy cảm giao dịch nào

### 🛡️ Biện pháp bảo vệ quyền riêng tư

1. **Cơ chế ID Instance**: Sử dụng UUID được tạo ngẫu nhiên làm định danh duy nhất, được lưu trữ trong tệp `./data/instance_id`, không chứa thông tin cá nhân
2. **IP Frontend bị tắt**: PostHog SDK được cấu hình với `ip_capture: false`, tắt tính năng chụp địa chỉ IP và suy luận vị trí địa lý
3. **Backend không gửi IP**: Mã backend không gửi địa chỉ IP đến dịch vụ thống kê
4. **Hoàn toàn tùy chọn**: Người dùng có thể tắt thống kê bất cứ lúc nào thông qua biến môi trường
5. **Minh bạch mã**: Tất cả mã thống kê có thể kiểm tra, nằm trong `utils/telemetry.go`

### ⚙️ Cách tắt thống kê

**Phương pháp 1: Biến môi trường (Được khuyến nghị)**
```bash
export QUANTMESH_DISABLE_TELEMETRY=1
```

**Phương pháp 2: Tắt Frontend**
Trong tệp `webui/.env.local`:
```bash
VITE_DISABLE_TELEMETRY=1
```

**Phương pháp 3: Sửa đổi mã**
Chỉnh sửa `utils/telemetry.go`, đặt `Enabled` thành `false`

### 📖 Tài liệu chi tiết

Để biết thêm thông tin chi tiết về tính năng thống kê, vui lòng tham khảo:
- 📖 [Hướng dẫn thống kê đầy đủ](../../docs/TELEMETRY_GUIDE.md)
- 🔒 [Hướng dẫn bảo vệ quyền riêng tư](../../docs/TELEMETRY_PRIVACY.md)
- 🚀 [Hướng dẫn thiết lập nhanh](../../docs/TELEMETRY_SIMPLE_GUIDE.md)

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

Xem [CONTRIBUTING.md](../../CONTRIBUTING.md) để biết hướng dẫn chi tiết.

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
