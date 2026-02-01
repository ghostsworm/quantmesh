# 加密货币支付系统部署指南

本指南介绍如何部署和配置 QuantMesh 加密货币支付系统。

## 📋 前置要求

- PostgreSQL 数据库
- (可选) Coinbase Commerce 账号
- 加密货币钱包地址

## 🚀 快速开始

### 1. 注册 Coinbase Commerce

访问 https://commerce.coinbase.com 注册账号。

**步骤**:
1. 注册 Coinbase 账号
2. 启用 Coinbase Commerce
3. 创建 API Key
4. 配置 Webhook URL

### 2. 配置环境变量

复制配置文件:

```bash
cp .env.crypto.example .env.crypto
```

编辑 `.env.crypto`:

```bash
# Coinbase Commerce
COINBASE_COMMERCE_API_KEY=your_api_key_here
COINBASE_WEBHOOK_SECRET=your_webhook_secret

# 钱包地址
WALLET_ADDRESS_BTC=bc1q...
WALLET_ADDRESS_ETH=0x...
WALLET_ADDRESS_USDT=0x...  # ERC20
WALLET_ADDRESS_USDC=0x...  # ERC20
```

### 3. 初始化数据库

```bash
# 连接数据库
psql -U postgres -d quantmesh

# 执行初始化脚本
\i scripts/init_crypto_payments.sql
```

或者在代码中自动初始化:

```go
cryptoPaymentService := saas.NewCryptoPaymentService(db, coinbaseAPIKey)
cryptoPaymentService.InitDatabase()
```

### 4. 配置 Webhook

在 Coinbase Commerce 后台配置 Webhook URL:

```
https://your-domain.com/api/payment/crypto/webhook/coinbase
```

**Webhook 事件**:
- `charge:confirmed` - 支付确认
- `charge:failed` - 支付失败
- `charge:delayed` - 支付延迟
- `charge:pending` - 支付待处理

### 5. 测试支付

```bash
# 测试 Coinbase 支付
curl -X POST http://localhost:8080/api/payment/crypto/coinbase/create \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "plan": "professional",
    "email": "test@example.com"
  }'

# 测试直接支付
curl -X POST http://localhost:8080/api/payment/crypto/direct/create \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "plan": "professional",
    "email": "test@example.com",
    "crypto_currency": "USDT"
  }'
```

## 🔧 配置选项

### Coinbase Commerce 配置

```go
cryptoPaymentService := saas.NewCryptoPaymentService(
    db,
    os.Getenv("COINBASE_COMMERCE_API_KEY"),
)
```

### 钱包地址配置

在 `crypto_payment_service.go` 中更新:

```go
walletAddresses: map[string]string{
    "BTC":  "bc1q...",  // 你的 BTC 地址
    "ETH":  "0x...",    // 你的 ETH 地址
    "USDT": "0x...",    // 你的 USDT (ERC20) 地址
    "USDC": "0x...",    // 你的 USDC 地址
},
```

### 汇率 API 配置

使用 CoinGecko API 获取实时汇率:

```go
func (s *CryptoPaymentService) getExchangeRate(cryptoCurrency string) (float64, error) {
    url := fmt.Sprintf(
        "https://api.coingecko.com/api/v3/simple/price?ids=%s&vs_currencies=usd",
        getCoinGeckoID(cryptoCurrency),
    )
    
    resp, err := http.Get(url)
    if err != nil {
        return 0, err
    }
    defer resp.Body.Close()
    
    var result map[string]map[string]float64
    json.NewDecoder(resp.Body).Decode(&result)
    
    // 解析汇率
    // ...
}
```

## 🔐 安全配置

### 1. Webhook 签名验证

```go
func (s *CryptoPaymentService) verifyCoinbaseSignature(
    payload []byte,
    signature string,
) bool {
    mac := hmac.New(sha256.New, []byte(s.coinbaseWebhookSecret))
    mac.Write(payload)
    expectedSignature := hex.EncodeToString(mac.Sum(nil))
    return signature == expectedSignature
}
```

### 2. 支付金额验证

```go
// 验证支付金额是否匹配
if payment.CryptoAmount < expectedAmount * 0.99 {
    return errors.New("支付金额不足")
}
```

### 3. 防重放攻击

```go
// 检查支付是否已处理
if payment.Status == "completed" {
    return errors.New("支付已完成,请勿重复处理")
}
```

## 📊 监控和日志

### 1. 支付监控

```sql
-- 查看待处理支付
SELECT * FROM crypto_payments 
WHERE status = 'pending' 
AND expires_at > NOW()
ORDER BY created_at DESC;

-- 查看今日完成支付
SELECT COUNT(*), SUM(amount) 
FROM crypto_payments 
WHERE status = 'completed' 
AND DATE(completed_at) = CURRENT_DATE;
```

### 2. 日志记录

```go
logger.Info("✅ 支付完成: ID=%d, 用户=%s, 金额=%.2f %s",
    payment.ID, payment.UserID, payment.CryptoAmount, payment.CryptoCurrency)

logger.Warn("⚠️ 支付过期: ID=%d, 用户=%s", payment.ID, payment.UserID)

logger.Error("❌ 支付失败: ID=%d, 错误=%v", payment.ID, err)
```

### 3. 告警设置

```go
// 支付金额异常告警
if payment.Amount > 10000 {
    notifier.Send(fmt.Sprintf(
        "⚠️ 大额支付: $%.2f, 用户: %s",
        payment.Amount, payment.UserID,
    ))
}

// 支付失败率告警
failureRate := getPaymentFailureRate()
if failureRate > 0.1 {
    notifier.Send(fmt.Sprintf(
        "⚠️ 支付失败率过高: %.1f%%",
        failureRate * 100,
    ))
}
```

## 🔄 自动化任务

### 1. 清理过期支付

```go
func (s *CryptoPaymentService) CleanupExpiredPayments() error {
    _, err := s.db.Exec(`
        UPDATE crypto_payments
        SET status = 'expired', updated_at = NOW()
        WHERE status = 'pending'
        AND expires_at < NOW()
    `)
    return err
}
```

定时任务:

```go
// 每小时清理一次
ticker := time.NewTicker(1 * time.Hour)
go func() {
    for range ticker.C {
        cryptoPaymentService.CleanupExpiredPayments()
    }
}()
```

### 2. 同步 Coinbase 支付状态

```go
func (s *CryptoPaymentService) SyncCoinbasePayments() error {
    // 获取所有待处理的 Coinbase 支付
    payments, _ := s.getPendingCoinbasePayments()
    
    for _, payment := range payments {
        // 查询 Coinbase API
        charge, err := s.getCoinbaseCharge(payment.ChargeID)
        if err != nil {
            continue
        }
        
        // 更新状态
        if charge.Status == "COMPLETED" {
            s.completePayment(payment.ChargeID)
        }
    }
    
    return nil
}
```

## 🧪 测试

### 单元测试

```go
func TestCreateCoinbaseCharge(t *testing.T) {
    service := NewCryptoPaymentService(db, "test_api_key")
    
    payment, err := service.CreateCoinbaseCharge(
        "user123", "test@example.com", "professional", 199.00,
    )
    
    assert.NoError(t, err)
    assert.Equal(t, "pending", payment.Status)
    assert.NotEmpty(t, payment.ChargeID)
}
```

### 集成测试

```bash
#!/bin/bash

# 测试创建支付
response=$(curl -s -X POST http://localhost:8080/api/payment/crypto/coinbase/create \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"plan":"professional","email":"test@example.com"}')

payment_id=$(echo $response | jq -r '.payment_id')

# 测试查询支付
curl -s http://localhost:8080/api/payment/crypto/$payment_id \
  -H "Authorization: Bearer $TOKEN"
```

## 📈 性能优化

### 1. 数据库索引

```sql
CREATE INDEX idx_crypto_payments_user_status 
ON crypto_payments(user_id, status);

CREATE INDEX idx_crypto_payments_charge_id 
ON crypto_payments(charge_id);

CREATE INDEX idx_crypto_payments_expires_at 
ON crypto_payments(expires_at);
```

### 2. 缓存汇率

```go
type RateCache struct {
    rates map[string]float64
    mu    sync.RWMutex
    ttl   time.Duration
}

func (c *RateCache) Get(currency string) (float64, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    rate, exists := c.rates[currency]
    return rate, exists
}
```

### 3. 异步处理

```go
// 异步处理 webhook
go func() {
    if err := s.HandleCoinbaseWebhook(body, signature); err != nil {
        logger.Error("处理 webhook 失败: %v", err)
    }
}()
```

## 🚨 故障排查

### 问题 1: Webhook 未收到

**检查**:
1. Webhook URL 是否正确
2. 服务器是否可以从外网访问
3. 防火墙是否开放端口
4. SSL 证书是否有效

**解决**:
```bash
# 测试 webhook 端点
curl -X POST https://your-domain.com/api/payment/crypto/webhook/coinbase \
  -H "Content-Type: application/json" \
  -d '{"event":{"type":"charge:confirmed"}}'
```

### 问题 2: 支付未自动确认

**检查**:
1. 查看数据库中的支付状态
2. 检查 Coinbase 后台的支付状态
3. 查看应用日志

**解决**:
```sql
-- 手动确认支付
UPDATE crypto_payments
SET status = 'completed', completed_at = NOW()
WHERE charge_id = 'CHARGE_ID';
```

### 问题 3: 汇率计算错误

**检查**:
1. API 是否可访问
2. API Key 是否有效
3. 汇率数据是否最新

**解决**:
```go
// 使用备用汇率源
if rate, err := getPrimaryRate(currency); err != nil {
    rate, err = getBackupRate(currency)
}
```

## 📞 技术支持

如有问题,请联系:
- 📧 Email: tech@quantmesh.io
- 💬 Discord: https://discord.gg/quantmesh
- 📚 文档: https://docs.quantmesh.io

---

Copyright © 2025 QuantMesh Team. All Rights Reserved.

