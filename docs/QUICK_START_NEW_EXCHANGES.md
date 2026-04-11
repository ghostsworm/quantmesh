# 新交易所快速开始指南

本指南帮助您快速开始使用新接入的交易所（OKX 和 Bybit）。

## 📋 前置条件

1. **注册交易所账户**
   - OKX: https://www.okx.com/join/OPENSQT
   - Bybit: https://partner.bybit.com/b/OPENSQT

2. **创建 API 密钥**
   - 登录交易所账户
   - 进入 API 管理页面
   - 创建新的 API 密钥
   - **重要**: 启用合约交易权限

3. **安全设置**
   - 绑定 IP 白名单（推荐）
   - 设置 API 权限（仅需要交易权限）
   - 妥善保管 API 密钥

## 🚀 快速开始

主配置权威在 **`app_config`**；以下片段可粘贴到 **Web** 或 **导入用 YAML**。

### 1. 配置 OKX 交易所

在导入 YAML 或 Web 中配置：

```yaml
app:
  current_exchange: "okx"  # 切换到 OKX

exchanges:
  okx:
    api_key: "your-okx-api-key"
    secret_key: "your-okx-secret-key"
    passphrase: "your-okx-passphrase"  # OKX 需要 passphrase
    fee_rate: 0.0002
    testnet: false  # 生产环境使用 false，测试使用 true

trading:
  symbol: "BTCUSDT"
  price_interval: 10
  order_quantity: 50
  # ... 其他配置
```

### 2. 配置 Bybit 交易所

在导入 YAML 或 Web 中配置：

```yaml
app:
  current_exchange: "bybit"  # 切换到 Bybit

exchanges:
  bybit:
    api_key: "your-bybit-api-key"
    secret_key: "your-bybit-secret-key"
    fee_rate: 0.0002
    testnet: false  # 生产环境使用 false，测试使用 true

trading:
  symbol: "BTCUSDT"
  price_interval: 10
  order_quantity: 50
  # ... 其他配置
```

### 3. 启动程序

```bash
# 方式1: 直接运行
./opensqt_market_maker

# 方式2: 使用启动脚本
./scripts/local/start.sh

# 方式3: 后台运行
nohup ./opensqt_market_maker > logs/opensqt.log 2>&1 &
```

### 4. 验证运行状态

查看日志确认连接成功：

```bash
# 查看实时日志
tail -f logs/opensqt.log

# 查找关键信息
grep "OKX" logs/opensqt.log
grep "Bybit" logs/opensqt.log
```

成功的日志示例：

```
[INFO] 🌐 [OKX] 使用实盘模式
[INFO] ℹ️ [OKX 合约信息] BTC-USDT-SWAP - 数量精度:3, 价格精度:1, 基础币种:BTC, 计价币种:USDT
[INFO] ✅ [OKX WebSocket] 订单流已启动
[INFO] ✅ [OKX WebSocket] 价格流已启动
```

## 🔧 多交易对配置

支持同时运行多个交易对：

```yaml
trading:
  symbols:
    - exchange: "okx"
      symbol: "BTCUSDT"
      price_interval: 10
      order_quantity: 50
      
    - exchange: "okx"
      symbol: "ETHUSDT"
      price_interval: 2
      order_quantity: 30
      
    - exchange: "bybit"
      symbol: "BTCUSDT"
      price_interval: 10
      order_quantity: 50
```

## 🧪 测试网模式

### OKX 模拟盘

1. 访问 OKX 模拟盘: https://www.okx.com/trade-demo
2. 创建模拟盘 API 密钥
3. 配置文件设置 `testnet: true`

```yaml
exchanges:
  okx:
    testnet: true  # 启用模拟盘
```

### Bybit 测试网

1. 访问 Bybit 测试网: https://testnet.bybit.com/
2. 注册测试网账户
3. 创建测试网 API 密钥
4. 配置文件设置 `testnet: true`

```yaml
exchanges:
  bybit:
    testnet: true  # 启用测试网
```

## ⚙️ 常用配置参数

### 交易参数

```yaml
trading:
  symbol: "BTCUSDT"           # 交易对
  price_interval: 10          # 价格间隔（美元）
  order_quantity: 50          # 每单金额（USDT）
  min_order_value: 20         # 最小订单价值
  buy_window_size: 10         # 买单窗口大小
  sell_window_size: 10        # 卖单窗口大小
```

### 风控参数

```yaml
risk:
  max_position: 1000          # 最大持仓（USDT）
  max_leverage: 10            # 最大杠杆倍数
  stop_loss_percent: 5        # 止损百分比
  take_profit_percent: 10     # 止盈百分比
```

### 监控参数

```yaml
monitor:
  price_check_interval: 1     # 价格检查间隔（秒）
  position_check_interval: 5  # 持仓检查间隔（秒）
  reconcile_interval: 60      # 对账间隔（秒）
```

## 📊 Web 管理界面

访问 Web 管理界面：

```
http://localhost:8080
```

功能：
- 📈 实时监控交易状态
- 💰 查看账户余额和持仓
- 📋 订单历史记录
- ⚙️ 在线修改配置
- 🛑 紧急停止交易

## 🔍 常见问题

### 1. API 密钥无效

**错误信息**: `API 错误: Invalid API key`

**解决方案**:
- 检查 API 密钥是否正确复制
- 确认 API 密钥已启用合约交易权限
- OKX 需要确认 passphrase 正确

### 2. 余额不足

**错误信息**: `insufficient balance`

**解决方案**:
- 检查账户余额
- 确认资金已转入合约账户
- 降低 `order_quantity` 参数

### 3. WebSocket 连接失败

**错误信息**: `连接 WebSocket 失败`

**解决方案**:
- 检查网络连接
- 确认防火墙设置
- 尝试使用代理（如果在限制地区）

### 4. 订单被拒绝

**错误信息**: `Order rejected`

**解决方案**:
- 检查价格精度和数量精度
- 确认订单金额满足最小要求
- 检查持仓限制

### 5. 签名错误

**错误信息**: `Invalid signature`

**解决方案**:
- 确认 API 密钥和 Secret 正确
- OKX 确认 passphrase 正确
- 检查系统时间是否同步

## 🛡️ 安全建议

1. **API 密钥安全**
   - 不要在公开场合分享 API 密钥
   - 定期更换 API 密钥
   - 使用 IP 白名单限制访问

2. **资金安全**
   - 从小额开始测试
   - 设置合理的止损止盈
   - 定期检查账户状态

3. **系统安全**
   - 使用强密码
   - 启用双因素认证
   - 保持系统更新

## 📈 性能优化

### 1. 网络优化

```yaml
# 使用代理（如果需要）
export https_proxy=http://127.0.0.1:7890
export http_proxy=http://127.0.0.1:7890
```

### 2. 并发优化

```yaml
# 增加并发数
app:
  max_concurrent_orders: 20
```

### 3. 缓存优化

```yaml
# 启用价格缓存
monitor:
  price_cache_ttl: 1  # 秒
```

## 📞 获取帮助

如遇到问题，请通过以下方式获取帮助：

1. **查看日志**
   ```bash
   tail -f logs/opensqt.log
   ```

2. **查看文档**
   - [交易所接入指南](EXCHANGE_INTEGRATION_GUIDE.md)
   - [实施总结](EXCHANGE_INTEGRATION_SUMMARY.md)
   - [主 README](../README.md)

3. **联系支持**
   - GitHub Issues: https://github.com/your-repo/opensqt_market_maker/issues
   - Telegram: @opensqt
   - Email: support@quantmesh.com

## 🎓 进阶使用

### 自定义策略

```go
// 实现自定义策略
type MyStrategy struct {
    // ...
}

func (s *MyStrategy) Execute(ctx context.Context) error {
    // 自定义逻辑
}
```

### 监控和告警

```yaml
notify:
  telegram:
    enabled: true
    bot_token: "your-bot-token"
    chat_id: "your-chat-id"
    
  email:
    enabled: true
    smtp_host: "smtp.gmail.com"
    smtp_port: 587
    from: "your-email@gmail.com"
    to: "alert@example.com"
```

### 数据分析

```bash
# 导出交易数据
./opensqt_market_maker --export-trades

# 生成报表
./opensqt_market_maker --generate-report
```

## 🚀 下一步

1. **优化策略参数**
   - 根据市场情况调整价格间隔
   - 优化订单金额
   - 设置合理的风控参数

2. **监控性能**
   - 观察订单成交率
   - 监控盈亏情况
   - 分析策略效果

3. **扩展功能**
   - 添加更多交易对
   - 尝试不同策略
   - 接入更多交易所

---

**祝您交易顺利！** 🎉

如有任何问题，欢迎随时联系我们。

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
