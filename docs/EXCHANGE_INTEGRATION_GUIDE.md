# 交易所接入指南

本文档说明如何为 QuantMesh 做市商系统接入新的交易所。

## 📋 目录

- [架构概览](#架构概览)
- [接入步骤](#接入步骤)
- [已完成的交易所](#已完成的交易所)
- [开发中的交易所](#开发中的交易所)
- [API 差异对比](#api-差异对比)
- [测试指南](#测试指南)

## 🏗️ 架构概览

QuantMesh 使用统一的接口层来抽象不同交易所的 API 差异：

```
IExchange 接口 (exchange/interface.go)
    ↓
Wrapper 包装器 (exchange/wrapper_*.go)
    ↓
Adapter 适配器 (exchange/*/adapter.go)
    ↓
REST Client + WebSocket (exchange/*/client.go, websocket.go)
```

### 核心组件

1. **IExchange 接口**: 定义所有交易所必须实现的方法
2. **Adapter**: 实现具体交易所的业务逻辑
3. **Client**: 封装 REST API 调用
4. **WebSocket Manager**: 管理实时数据流（订单、价格、K线）
5. **Wrapper**: 将 Adapter 的类型转换为通用类型

## 🔧 接入步骤

### Step 1: 创建目录结构

```bash
exchange/
├── your_exchange/
│   ├── adapter.go          # 核心适配器
│   ├── client.go           # REST API 客户端
│   ├── websocket.go        # 订单流 WebSocket
│   └── kline_websocket.go  # K线流 WebSocket
└── wrapper_your_exchange.go # 包装器
```

### Step 2: 实现 REST API 客户端

参考 `exchange/okx/client.go` 或 `exchange/bybit/client.go`：

```go
type YourExchangeClient struct {
    apiKey     string
    secretKey  string
    baseURL    string
    httpClient *http.Client
}

func NewYourExchangeClient(apiKey, secretKey string, useTestnet bool) *YourExchangeClient {
    // 初始化客户端
}

func (c *YourExchangeClient) sign(params string) string {
    // 实现签名算法
}

func (c *YourExchangeClient) request(ctx context.Context, method, path string, params interface{}) ([]byte, error) {
    // 实现 HTTP 请求
}
```

**必须实现的方法**:
- `GetInstruments()` - 获取合约信息
- `PlaceOrder()` - 下单
- `CancelOrder()` - 取消订单
- `GetOrder()` - 查询订单
- `GetOpenOrders()` - 查询未完成订单
- `GetBalance()` - 获取余额
- `GetPositions()` - 获取持仓
- `GetKlines()` - 获取K线数据
- `GetFundingRate()` - 获取资金费率

### Step 3: 实现 WebSocket 管理器

参考 `exchange/okx/websocket.go` 或 `exchange/bybit/websocket.go`：

```go
type WebSocketManager struct {
    apiKey     string
    secretKey  string
    conn       *websocket.Conn
    mu         sync.RWMutex
    stopChan   chan struct{}
    isRunning  atomic.Bool
    lastPrice  atomic.Value
}

func (w *WebSocketManager) Start(ctx context.Context, symbol string, callback func(OrderUpdate)) error {
    // 1. 连接 WebSocket
    // 2. 认证（如果需要）
    // 3. 订阅订单频道
    // 4. 启动消息处理
}

func (w *WebSocketManager) StartPriceStream(ctx context.Context, symbol string, callback func(float64)) error {
    // 订阅价格流
}
```

### Step 4: 实现适配器

参考 `exchange/okx/adapter.go` 或 `exchange/bybit/adapter.go`：

```go
type YourExchangeAdapter struct {
    client           *YourExchangeClient
    symbol           string
    wsManager        *WebSocketManager
    klineWSManager   *KlineWebSocketManager
    priceDecimals    int
    quantityDecimals int
    baseAsset        string
    quoteAsset       string
    useTestnet       bool
}

func NewYourExchangeAdapter(cfg map[string]string, symbol string) (*YourExchangeAdapter, error) {
    // 1. 解析配置
    // 2. 创建客户端
    // 3. 获取合约信息
}
```

**必须实现 IExchange 接口的所有方法**（参见 `exchange/interface.go`）

### Step 5: 创建包装器

参考 `exchange/wrapper_okx.go` 或 `exchange/wrapper_bybit.go`：

```go
type yourExchangeWrapper struct {
    adapter *yourexchange.YourExchangeAdapter
}

// 实现 IExchange 接口，将类型转换为通用类型
func (w *yourExchangeWrapper) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
    // 转换请求类型
    // 调用 adapter
    // 转换响应类型
}
```

### Step 6: 更新工厂模式

在 `exchange/factory.go` 中添加：

```go
import (
    "quantmesh/exchange/yourexchange"
)

func NewExchange(cfg *config.Config, exchangeName, symbol string) (IExchange, error) {
    // ...
    case "yourexchange":
        exchangeCfg, exists := cfg.Exchanges["yourexchange"]
        if !exists {
            return nil, fmt.Errorf("yourexchange 配置不存在")
        }
        cfgMap := map[string]string{
            "api_key":    exchangeCfg.APIKey,
            "secret_key": exchangeCfg.SecretKey,
            // 其他配置...
        }
        adapter, err := yourexchange.NewYourExchangeAdapter(cfgMap, symbol)
        if err != nil {
            return nil, err
        }
        return &yourExchangeWrapper{adapter: adapter}, nil
}
```

### Step 7: 更新配置文件

在 `config.example.yaml` 中添加：

```yaml
exchanges:
  yourexchange:
    api_key: "YOUR_API_KEY"
    secret_key: "YOUR_API_SECRET"
    fee_rate: 0.0002
    testnet: false
```

## ✅ 已完成的交易所

### 1. OKX (欧易)

**状态**: ✅ 完成  
**优先级**: P0  
**特点**:
- 全球前三交易所
- 支持 USDT 永续合约
- 完整的 REST API 和 WebSocket 支持
- 支持模拟盘测试

**关键实现**:
- 签名算法: HMAC-SHA256
- 需要 passphrase
- 合约标识格式: `BTC-USDT-SWAP`
- WebSocket 认证: 需要签名

**文件**:
- `exchange/okx/adapter.go`
- `exchange/okx/client.go`
- `exchange/okx/websocket.go`
- `exchange/okx/kline_websocket.go`
- `exchange/wrapper_okx.go`

### 2. Bybit

**状态**: ✅ 完成  
**优先级**: P0  
**特点**:
- 合约交易主流平台
- 支持 USDT 永续合约
- V5 API 统一接口
- 支持测试网

**关键实现**:
- 签名算法: HMAC-SHA256
- 需要 recv_window 参数
- 统一账户模式
- WebSocket 认证: 需要签名和过期时间

**文件**:
- `exchange/bybit/adapter.go`
- `exchange/bybit/client.go`
- `exchange/bybit/websocket.go`
- `exchange/bybit/kline_websocket.go`
- `exchange/wrapper_bybit.go`

### 3. Binance (币安)

**状态**: ✅ 稳定  
**特点**:
- 全球最大交易所
- 完善的 API 文档
- 支持测试网

### 4. Bitget

**状态**: ✅ 稳定  
**特点**:
- 合约交易主流平台
- 支持批量操作

### 5. Gate.io

**状态**: ✅ 稳定  
**特点**:
- 老牌交易所
- 支持多种合约类型

## 🚧 开发中的交易所

### 1. Huobi (HTX)

**状态**: 🚧 待开发  
**优先级**: P1  
**预计完成**: 第3周

**特殊注意**:
- WebSocket 使用 gzip 压缩
- 签名需要按字母序排序参数
- 需要处理 Signature 参数的 URL 编码

### 2. KuCoin

**状态**: 🚧 待开发  
**优先级**: P1  
**预计完成**: 第4周

**特殊注意**:
- WebSocket 连接需要先获取 token
- 需要 passphrase + API-KEY-VERSION
- 支持公共和私有两种 WebSocket 端点

### 3. Kraken

**状态**: 🚧 待开发  
**优先级**: P2  
**预计完成**: 第5周

**特殊注意**:
- 使用 SHA512 签名算法（与其他交易所不同）
- WebSocket 需要通过 REST API 获取 token
- 合约交易对命名规则特殊（如 `PI_XBTUSD`）

### 4. Bitfinex

**状态**: 🚧 待开发  
**优先级**: P2  
**预计完成**: 第6周

**特殊注意**:
- 使用 SHA384 签名算法
- WebSocket 使用频道订阅模式
- 需要处理 nonce 时间戳

## 📊 API 差异对比

### 签名算法

| 交易所 | 算法 | 特殊要求 |
|--------|------|---------|
| OKX | HMAC-SHA256 | 需要 passphrase |
| Bybit | HMAC-SHA256 | 需要 recv_window |
| Binance | HMAC-SHA256 | 需要 timestamp |
| Bitget | HMAC-SHA256 | 需要 passphrase |
| Gate.io | HMAC-SHA512 | 需要 timestamp |
| Huobi | HMAC-SHA256 | 参数需排序 |
| KuCoin | HMAC-SHA256 | 需要 passphrase + version |
| Kraken | HMAC-SHA512 | 需要 nonce |
| Bitfinex | HMAC-SHA384 | 需要 nonce |

### REST API 限频

| 交易所 | 限频规则 | 解决方案 |
|--------|---------|---------|
| OKX | 20次/2秒 | 令牌桶算法 |
| Bybit | 120次/分钟 | 批量操作优先 |
| Binance | 1200次/分钟 | 权重管理 |
| Huobi | 10次/秒 | 请求队列 |
| KuCoin | 30次/3秒 | WebSocket 优先 |
| Kraken | 1次/秒 | 严格限速 |
| Bitfinex | 90次/分钟 | 请求合并 |

### WebSocket 特性

| 交易所 | 认证方式 | 心跳机制 | 断线重连 |
|--------|---------|---------|---------|
| OKX | 签名认证 | ping/pong | ✅ 支持 |
| Bybit | 签名认证 | ping/pong | ✅ 支持 |
| Binance | listenKey | 定期续期 | ✅ 支持 |
| Bitget | 签名认证 | ping/pong | ✅ 支持 |
| Gate.io | 签名认证 | ping/pong | ✅ 支持 |

### 测试网支持

| 交易所 | 测试网 | 测试网地址 |
|--------|--------|-----------|
| OKX | ✅ | `https://www.okx.com/priapi/v5/simulate/` |
| Bybit | ✅ | `https://api-testnet.bybit.com/` |
| Binance | ✅ | `https://testnet.binancefuture.com/` |
| Bitget | ✅ | 模拟盘 |
| Gate.io | ❌ | 无 |
| Huobi | ❌ | 无 |
| KuCoin | ✅ | `https://api-sandbox-futures.kucoin.com/` |
| Kraken | ✅ | `https://demo-futures.kraken.com/` |
| Bitfinex | ❌ | 无 |

## 🧪 测试指南

### 单元测试

为每个交易所创建测试文件：

```go
// exchange/yourexchange/adapter_test.go
func TestYourExchangeAdapter_PlaceOrder(t *testing.T) {
    // 测试下单
}

func TestYourExchangeAdapter_GetAccount(t *testing.T) {
    // 测试获取账户
}

func TestYourExchangeAdapter_WebSocket(t *testing.T) {
    // 测试 WebSocket
}
```

### 集成测试

在测试网环境验证：

1. **下单流程**: 下单 → 查询 → 取消
2. **WebSocket**: 订单更新实时性
3. **价格流**: 价格推送稳定性
4. **K线流**: 多交易对订阅
5. **断线重连**: 模拟网络中断

### 压力测试

模拟高频交易场景：

- 每秒 10+ 订单下单
- 批量撤单（50+ 订单）
- WebSocket 长时间稳定性（24小时+）

## 🎯 成功标准

每个交易所接入完成后，需满足：

### 功能完整性
- ✅ 所有 IExchange 接口方法均已实现
- ✅ REST API 和 WebSocket 均正常工作
- ✅ 支持批量操作（下单、撤单）

### 稳定性
- ✅ WebSocket 断线重连成功率 > 99%
- ✅ 订单成功率 > 95%（排除余额不足等正常错误）
- ✅ 24小时持续运行无崩溃

### 性能
- ✅ 下单延迟 < 100ms（P99）
- ✅ WebSocket 消息延迟 < 50ms（P99）
- ✅ 批量撤单支持 50+ 订单

### 测试覆盖
- ✅ 单元测试覆盖率 > 70%
- ✅ 集成测试通过率 100%
- ✅ 在测试网完成完整流程验证

## 📚 参考资料

### 官方文档

- [OKX API 文档](https://www.okx.com/docs-v5/zh/)
- [Bybit API 文档](https://bybit-exchange.github.io/docs/v5/intro)
- [Binance API 文档](https://binance-docs.github.io/apidocs/futures/cn/)
- [Bitget API 文档](https://bitgetlimited.github.io/apidoc/zh/mix/)
- [Gate.io API 文档](https://www.gate.io/docs/developers/apiv4/)

### 社区资源

- [CCXT](https://github.com/ccxt/ccxt) - 统一的加密货币交易 API
- [Go Binance](https://github.com/adshao/go-binance) - 币安 Go SDK
- [Bybit Go API](https://github.com/bybit-exchange/bybit.go.api) - Bybit 官方 Go SDK

## 🤝 贡献指南

欢迎贡献新的交易所接入！请遵循以下步骤：

1. Fork 本项目
2. 创建功能分支 (`git checkout -b feature/add-exchange-xxx`)
3. 按照本指南实现交易所接入
4. 编写测试并确保通过
5. 提交 Pull Request

**注意事项**:
- 代码风格遵循项目规范
- 添加必要的注释和文档
- 确保 linter 检查通过
- 在测试网完成验证

## 📞 联系方式

如有问题或建议，请通过以下方式联系：

- GitHub Issues: [opensqt_market_maker/issues](https://github.com/your-repo/opensqt_market_maker/issues)
- Telegram: @opensqt
- Email: support@quantmesh.com

---

**最后更新**: 2025-12-28  
**版本**: v1.0.0

