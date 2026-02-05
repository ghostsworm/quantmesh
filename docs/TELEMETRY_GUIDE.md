# 统计功能说明

## 📊 概述

QuantMesh 包含一个可选的统计功能，用于收集基本的安装和使用数据。这个功能是完全透明的，代码可以审查，并且用户可以随时禁用。

## 🔒 隐私保护

### 收集的信息

#### 基础信息
- **事件类型**：`install`（安装）、`startup`（启动）等
- **时间戳**：事件发生的时间
- **版本号**：QuantMesh 版本
- **操作系统**：如 `linux`、`darwin`、`windows`
- **架构**：如 `amd64`、`arm64`
- **实例 ID**：随机生成的唯一标识符（UUID 格式），用于区分不同的部署实例

#### 使用情况统计（匿名）
- **交易所使用**：使用的交易所名称（如 `binance`、`okx` 等）
- **交易币种**：交易的币种对（如 `BTCUSDT`、`ETHUSDT` 等）
- **交易方向**：买入或卖出（`buy`/`sell`）
- **API 耗时**：API 请求/响应的耗时（毫秒）
- **WebSocket 延时**：WebSocket 消息的延时（毫秒）

**注意**：所有数据都是匿名的，不包含任何个人信息或敏感信息。

### 不收集的信息
- ❌ IP 地址（前端已禁用 IP 捕获，后端仅用于区分实例）
- ❌ 地理位置信息（经纬度、城市等，已禁用）
- ❌ 用户 ID 或任何身份信息
- ❌ API 密钥或配置信息
- ❌ 交易数据或财务信息
- ❌ 任何敏感信息

### 关于 IP 地址和地理位置

**重要说明**：
- **前端代码**：已通过 `ip_capture: false` 配置禁用 IP 地址捕获和地理位置推断
- **后端代码**：**不再使用 IP 地址**，改用实例 ID（UUID）作为唯一标识符
- **实例 ID**：随机生成的 UUID，存储在 `./data/instance_id` 文件中，不包含任何个人信息
- **PostHog 服务端**：即使 PostHog 服务端可能从 HTTP 请求中获取 IP 地址，但由于前端已禁用 IP 捕获，PostHog 不会主动推断地理位置

**隐私保护措施**：
1. ✅ **前端 PostHog SDK 配置了 `ip_capture: false`**，禁用 IP 地址捕获
2. ✅ **后端使用实例 ID 而不是 IP 地址**，实例 ID 是随机生成的 UUID，不包含任何个人信息
3. ✅ **不收集地理位置信息**（经纬度、城市等）
4. ✅ **不收集敏感信息**（API 密钥、交易金额、账户余额等）
5. ✅ 如果仍担心隐私，可以通过环境变量 `QUANTMESH_DISABLE_TELEMETRY=1` 完全禁用统计功能

## 🛠️ 使用的服务

### PostHog（推荐）

**PostHog** 是一个知名的开源产品分析平台，被许多开源项目使用：
- ✅ 完全开源（MIT 许可证）
- ✅ 支持自托管
- ✅ GDPR 合规
- ✅ 隐私友好
- ✅ 被 Vercel、Supabase 等知名项目使用

**官方网站**：https://posthog.com/
**GitHub**：https://github.com/PostHog/posthog

### 其他可选服务

如果需要使用其他服务，可以修改 `utils/telemetry.go` 中的配置：

1. **Plausible Analytics**
   - 开源、隐私友好
   - 主要用于网站分析

2. **Umami**
   - 开源、自托管
   - 轻量级分析工具

3. **自托管服务**
   - 可以部署自己的统计服务
   - 完全控制数据

## ⚙️ 配置

### 启用统计（默认）

统计功能默认启用，无需配置。

### 禁用统计

#### 方法一：环境变量（推荐）

```bash
export QUANTMESH_DISABLE_TELEMETRY=1
```

#### 方法二：修改代码

编辑 `utils/telemetry.go`：

```go
DefaultTelemetryConfig = TelemetryConfig{
    Enabled: false,  // 改为 false
    // ...
}
```

#### 方法三：编译时禁用

在编译时设置构建标签：

```bash
go build -tags no_telemetry
```

## 🔍 代码审查

统计功能的代码完全透明，位于：
- `utils/telemetry.go` - 统计发送逻辑
- `scripts/install.sh` - 安装脚本中的统计调用
- `main.go` - 程序启动时的统计调用

所有代码都可以审查，确保没有后门或恶意行为。

## 📝 实现细节

### 实例 ID 生成

首次运行时，系统会在 `./data/instance_id` 文件中生成一个唯一的实例 ID（UUID 格式）。这个 ID 用于：
- 区分不同的部署实例
- 作为 PostHog 的 `distinct_id`，用于用户识别
- **不包含任何个人信息**，只是一个随机生成的标识符

### 数据收集点

#### 1. 安装和启动统计

**安装时**（`scripts/install.sh`）：
```bash
# 发送安装统计（异步，不阻塞安装）
utils.SendInstallTelemetry(Version)
```

**启动时**（`main.go`）：
```go
// 发送启动统计（异步，不阻塞启动）
utils.SendStartupTelemetry(Version)
```

#### 2. 交易所使用统计

**创建交易所实例时**（`exchange/factory.go`）：
```go
// 追踪交易所使用情况（异步，不阻塞）
utils.TrackExchangeUsage(Version, exchangeName, symbol)
```

收集的数据：
- `exchange`: 交易所名称（如 `binance`、`okx`）
- `symbol`: 交易币种对（如 `BTCUSDT`）

#### 3. API 耗时统计

**API 调用时**（`exchange/binance/adapter.go`）：
```go
// 追踪 API 耗时
startTime := time.Now()
resp, err := orderService.Do(ctx)
latencyMs := time.Since(startTime).Milliseconds()
utils.TrackAPILatency(Version, "binance", "PlaceOrder", latencyMs, err == nil)
```

收集的数据：
- `exchange`: 交易所名称
- `api_method`: API 方法名称（如 `PlaceOrder`、`GetAccount`）
- `latency_ms`: API 耗时（毫秒）
- `success`: 是否成功

#### 4. WebSocket 延时统计

**WebSocket 消息接收时**（`exchange/binance/websocket.go`）：
```go
// 计算 WebSocket 延时（消息从服务器发送到客户端接收的时间差）
serverTime := time.Unix(event.EventTime/1000, ...)
latencyMs := messageReceivedTime.Sub(serverTime).Milliseconds()
utils.TrackWebSocketLatency(Version, "binance", latencyMs, "price_update")
```

收集的数据：
- `exchange`: 交易所名称
- `latency_ms`: WebSocket 延时（毫秒）
- `message_type`: 消息类型（如 `price_update`）

#### 5. 交易活动统计

**下单成功时**（`order/executor_adapter.go`）：
```go
// 追踪交易活动（异步，不阻塞）
utils.TrackTradingActivity(Version, exchangeName, req.Symbol, strings.ToLower(req.Side))
```

收集的数据：
- `exchange`: 交易所名称
- `symbol`: 交易币种对
- `side`: 交易方向（`buy` 或 `sell`）

### 数据发送方式

所有统计数据都是**异步发送**的，不会阻塞主程序运行：
- 使用 `go func()` 在后台 goroutine 中发送
- 如果发送失败，不会影响主程序运行
- 总耗时不超过 3 秒（超时保护）

## 🚀 设置自己的统计服务

### 使用 PostHog Cloud（免费）

1. 注册 PostHog 账户：https://posthog.com/signup
2. 创建项目，获取 Project API Key
3. 修改 `utils/telemetry.go` 中的 `ProjectID`

### 自托管 PostHog

1. 部署 PostHog：https://posthog.com/docs/self-host
2. 获取 API Key
3. 修改 `utils/telemetry.go` 中的 `Endpoint` 和 `ProjectID`

### 使用其他服务

修改 `utils/telemetry.go` 中的 `SendTelemetry` 函数，适配其他服务的 API。

## 📊 查看统计

### PostHog

1. 登录 PostHog 控制台
2. 查看 Events 页面
3. 可以筛选以下事件类型：
   - `install` - 安装事件
   - `startup` - 启动事件
   - `exchange_usage` - 交易所使用情况
   - `api_latency` - API 耗时统计
   - `websocket_latency` - WebSocket 延时统计
   - `trading_activity` - 交易活动统计

4. 使用 PostHog 的分析功能：
   - **趋势分析**：查看不同交易所的使用趋势
   - **性能分析**：分析 API 耗时和 WebSocket 延时的分布
   - **用户分析**：通过实例 ID 区分不同的部署实例

### 自定义服务

根据你使用的服务，查看相应的统计面板。

### 数据示例

**交易所使用事件**：
```json
{
  "event": "exchange_usage",
  "properties": {
    "exchange": "binance",
    "symbol": "BTCUSDT",
    "instance_id": "550e8400-e29b-41d4-a716-446655440000",
    "version": "3.41.0",
    "os": "linux",
    "arch": "amd64"
  }
}
```

**API 耗时事件**：
```json
{
  "event": "api_latency",
  "properties": {
    "exchange": "binance",
    "api_method": "PlaceOrder",
    "latency_ms": 150,
    "success": true,
    "instance_id": "550e8400-e29b-41d4-a716-446655440000"
  }
}
```

## ⚠️ 注意事项

1. **完全可选**：用户可以随时禁用统计功能
2. **透明性**：所有代码都可以审查
3. **隐私保护**：只收集最少的信息
4. **不阻塞**：统计发送是异步的，不会影响安装或启动速度
5. **失败处理**：如果统计服务不可用，不会影响主程序运行

## 🤝 贡献

如果你有更好的统计服务建议，或者想改进统计功能，欢迎提交 Issue 或 PR。

## 📚 相关资源

- [PostHog 文档](https://posthog.com/docs)
- [PostHog GitHub](https://github.com/PostHog/posthog)
- [隐私友好的分析工具](https://plausible.io/)
- [Umami Analytics](https://umami.is/)

---

**记住**：统计功能是为了了解项目的使用情况，帮助我们改进项目。如果你不希望发送统计，可以随时禁用。
