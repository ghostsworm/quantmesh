# 加密货币支付快速开始指南

5 分钟快速接入加密货币支付!

## 🚀 方案选择

### 方案 A: Coinbase Commerce (推荐,自动化)

**优点**: 自动确认,无需手动操作
**时间**: 15 分钟
**难度**: ⭐⭐

### 方案 B: 直接钱包支付 (简单,手动)

**优点**: 无需注册,立即可用
**时间**: 5 分钟
**难度**: ⭐

---

## 方案 A: Coinbase Commerce

### 步骤 1: 注册 Coinbase Commerce (5 分钟)

1. 访问 https://commerce.coinbase.com
2. 使用 Coinbase 账号登录 (没有则注册)
3. 进入 Settings → API Keys
4. 创建新的 API Key
5. 保存 API Key 和 Webhook Secret

### 步骤 2: 配置环境变量 (2 分钟)

```bash
# 复制配置文件
cp .env.crypto.example .env.crypto

# 编辑配置
vim .env.crypto
```

填入:

```bash
COINBASE_COMMERCE_API_KEY=your_api_key_here
COINBASE_WEBHOOK_SECRET=your_webhook_secret_here
```

### 步骤 3: 配置 Webhook (3 分钟)

在 Coinbase Commerce 后台:

1. 进入 Settings → Webhook subscriptions
2. 添加 Webhook URL: `https://your-domain.com/api/payment/crypto/webhook/coinbase`
3. 选择事件: `charge:confirmed`, `charge:failed`
4. 保存

### 步骤 4: 测试 (5 分钟)

```bash
# 启动服务
go run main.go

# 测试支付
./test_crypto_payment.sh
```

✅ **完成!** 用户现在可以使用加密货币支付了。

---

## 方案 B: 直接钱包支付

### 步骤 1: 准备钱包地址 (2 分钟)

准备以下钱包地址:
- BTC 地址 (如 `bc1q...`)
- ETH 地址 (如 `0x...`)
- USDT 地址 (ERC20, 如 `0x...`)
- USDC 地址 (ERC20, 如 `0x...`)

💡 **提示**: 可以使用同一个 ETH 地址接收 ETH, USDT, USDC。

### 步骤 2: 配置钱包地址 (2 分钟)

编辑 `saas/crypto_payment_service.go`:

```go
walletAddresses: map[string]string{
    "BTC":  "bc1q...",  // 你的 BTC 地址
    "ETH":  "0x...",    // 你的 ETH 地址
    "USDT": "0x...",    // 你的 USDT (ERC20) 地址
    "USDC": "0x...",    // 你的 USDC 地址
},
```

或者使用环境变量:

```bash
export WALLET_ADDRESS_BTC="bc1q..."
export WALLET_ADDRESS_ETH="0x..."
export WALLET_ADDRESS_USDT="0x..."
export WALLET_ADDRESS_USDC="0x..."
```

### 步骤 3: 启动服务 (1 分钟)

```bash
go run main.go
```

### 步骤 4: 测试支付 (2 分钟)

```bash
# 创建支付
curl -X POST http://localhost:8080/api/payment/crypto/direct/create \
  -H "Content-Type: application/json" \
  -d '{
    "plan": "professional",
    "email": "test@example.com",
    "crypto_currency": "USDT"
  }'
```

响应:

```json
{
  "payment_id": 1,
  "crypto_currency": "USDT",
  "crypto_amount": 199.0,
  "payment_address": "0x...",
  "amount_usd": 199.00,
  "message": "请向指定地址转账,并保存交易哈希"
}
```

### 步骤 5: 确认支付 (手动)

用户转账后:

```bash
# 确认支付
curl -X POST http://localhost:8080/api/payment/crypto/1/confirm \
  -H "Content-Type: application/json" \
  -d '{
    "transaction_hash": "0xabc..."
  }'
```

✅ **完成!** 支付已确认,订阅已激活。

---

## 🔧 集成到现有系统

### 在 main.go 中初始化

```go
import (
    "quantmesh/saas"
    "quantmesh/web"
)

func main() {
    // ... 其他初始化代码 ...
    
    // 初始化加密货币支付服务
    cryptoPaymentService := saas.NewCryptoPaymentService(
        db,
        os.Getenv("COINBASE_COMMERCE_API_KEY"),
    )
    
    // 初始化数据库
    if err := cryptoPaymentService.InitDatabase(); err != nil {
        log.Fatal(err)
    }
    
    // 设置到 web 服务
    web.SetCryptoPaymentService(cryptoPaymentService)
    
    // ... 启动服务器 ...
}
```

### 在前端集成

```javascript
// 创建支付
async function createCryptoPayment(plan, email, method = 'coinbase') {
    const endpoint = method === 'coinbase' 
        ? '/api/payment/crypto/coinbase/create'
        : '/api/payment/crypto/direct/create';
    
    const data = {
        plan: plan,
        email: email,
    };
    
    if (method === 'direct') {
        data.crypto_currency = 'USDT'; // 或让用户选择
    }
    
    const response = await fetch(endpoint, {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            'Authorization': `Bearer ${token}`,
        },
        body: JSON.stringify(data),
    });
    
    const result = await response.json();
    
    if (method === 'coinbase') {
        // 跳转到 Coinbase 支付页面
        window.location.href = result.payment_url;
    } else {
        // 显示支付信息
        showPaymentInfo(result);
    }
}

// 显示支付信息
function showPaymentInfo(payment) {
    alert(`
        请向以下地址转账:
        地址: ${payment.payment_address}
        金额: ${payment.crypto_amount} ${payment.crypto_currency}
        
        转账后请提交交易哈希
    `);
}

// 查询支付状态
async function checkPaymentStatus(paymentId) {
    const response = await fetch(`/api/payment/crypto/${paymentId}`, {
        headers: {
            'Authorization': `Bearer ${token}`,
        },
    });
    
    const payment = await response.json();
    return payment.status; // pending/completed/expired
}
```

---

## 📊 监控和管理

### 查看所有支付

```sql
SELECT * FROM crypto_payments 
ORDER BY created_at DESC 
LIMIT 20;
```

### 查看待确认支付

```sql
SELECT id, user_id, email, plan, crypto_amount, crypto_currency, 
       payment_address, created_at
FROM crypto_payments 
WHERE status = 'pending' 
AND payment_method = 'direct'
ORDER BY created_at DESC;
```

### 手动确认支付

```sql
UPDATE crypto_payments
SET status = 'completed', 
    transaction_hash = '0xabc...',
    completed_at = NOW()
WHERE id = 123;
```

### 查看收入统计

```sql
SELECT 
    DATE(completed_at) as date,
    COUNT(*) as count,
    SUM(amount) as total_usd,
    SUM(crypto_amount) as total_crypto,
    crypto_currency
FROM crypto_payments
WHERE status = 'completed'
GROUP BY DATE(completed_at), crypto_currency
ORDER BY date DESC;
```

---

## 🎯 最佳实践

### 1. 推荐使用稳定币

向用户推荐 USDT 或 USDC:
- 价格稳定
- 金额精确
- 转账快速

### 2. 设置合理的过期时间

- Coinbase: 1 小时 (自动)
- 直接支付: 24 小时

### 3. 及时确认支付

- 小额支付 (< $1000): 1 个区块确认即可
- 大额支付 (≥ $1000): 等待 6 个以上确认

### 4. 自动化监控

```go
// 定时清理过期支付
ticker := time.NewTicker(1 * time.Hour)
go func() {
    for range ticker.C {
        cryptoPaymentService.CleanupExpiredPayments()
    }
}()
```

### 5. 发送通知

支付完成后发送邮件:

```go
func (s *CryptoPaymentService) completePayment(chargeID string) error {
    // ... 更新数据库 ...
    
    // 发送邮件
    sendEmail(payment.Email, "支付成功", fmt.Sprintf(
        "您的 %s 订阅已激活!",
        payment.Plan,
    ))
    
    return nil
}
```

---

## ❓ 常见问题

### Q: 需要 KYC 吗?

A: 不需要。Coinbase Commerce 和直接支付都不需要 KYC。

### Q: 手续费多少?

A: 
- Coinbase Commerce: 1% 手续费
- 直接支付: 仅网络 Gas 费 (用户承担)

### Q: 支持哪些国家?

A: 全球通用,无地域限制。

### Q: 如何处理退款?

A: 
1. 查询原支付记录
2. 向原地址退款
3. 更新数据库状态

### Q: 汇率如何确定?

A: 
- Coinbase: 使用 Coinbase 实时汇率
- 直接支付: 使用 CoinGecko API

---

## 📞 获取帮助

- 📧 Email: tech@quantmesh.io
- 💬 Discord: https://discord.gg/quantmesh
- 📚 完整文档: `docs/CRYPTO_PAYMENT_GUIDE.md`

---

**恭喜!** 🎉 你已成功接入加密货币支付系统!

