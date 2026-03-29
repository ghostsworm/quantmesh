# QuantMesh 加密货币支付指南

本指南介绍如何使用加密货币购买 QuantMesh 订阅和 License。

## 📋 目录

- [为什么选择加密货币支付](#为什么选择加密货币支付)
- [支持的加密货币](#支持的加密货币)
- [支付方式](#支付方式)
- [Coinbase Commerce 支付](#coinbase-commerce-支付)
- [直接钱包支付](#直接钱包支付)
- [常见问题](#常见问题)

## 为什么选择加密货币支付

✅ **优势**:
- 无需银行账户或信用卡
- 全球通用,无地域限制
- 交易费用低
- 隐私保护更好
- 到账快速 (通常 10-30 分钟)

## 支持的加密货币

| 币种 | 网络 | 最小金额 | 推荐 |
|------|------|----------|------|
| **BTC** | Bitcoin | 0.0001 BTC | ⭐⭐⭐⭐⭐ |
| **ETH** | Ethereum | 0.001 ETH | ⭐⭐⭐⭐⭐ |
| **USDT** | Ethereum (ERC20) | 10 USDT | ⭐⭐⭐⭐⭐ |
| **USDC** | Ethereum (ERC20) | 10 USDC | ⭐⭐⭐⭐⭐ |

💡 **推荐使用 USDT 或 USDC**: 稳定币,价格波动小,支付金额精确。

## 支付方式

### 方式 1: Coinbase Commerce (推荐)

**优点**:
- ✅ 自动确认,无需等待
- ✅ 支持多种加密货币
- ✅ 安全可靠
- ✅ 有支付页面,操作简单

**适合**: 所有用户

### 方式 2: 直接钱包支付

**优点**:
- ✅ 无第三方,更私密
- ✅ 无额外手续费
- ✅ 支持所有主流钱包

**缺点**:
- ⚠️ 需要手动确认 (1-24 小时)
- ⚠️ 需要提交交易哈希

**适合**: 注重隐私的用户

## Coinbase Commerce 支付

### 步骤 1: 创建支付

```bash
curl -X POST https://quantmesh.cloud/api/payment/crypto/coinbase/create \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "plan": "professional",
    "email": "your@email.com"
  }'
```

响应:

```json
{
  "payment_id": 123,
  "charge_id": "ABCD1234",
  "payment_url": "https://commerce.coinbase.com/charges/ABCD1234",
  "amount": 199.00,
  "currency": "USD",
  "expires_at": "2025-01-01T01:00:00Z",
  "status": "pending",
  "message": "请在支付页面完成付款"
}
```

### 步骤 2: 访问支付页面

打开 `payment_url`,选择加密货币并完成支付。

### 步骤 3: 等待确认

- 区块链确认通常需要 10-30 分钟
- 确认后自动激活订阅
- 你会收到邮件通知

### 步骤 4: 查看支付状态

```bash
curl https://quantmesh.cloud/api/payment/crypto/123 \
  -H "Authorization: Bearer YOUR_TOKEN"
```

## 直接钱包支付

### 步骤 1: 创建支付

```bash
curl -X POST https://quantmesh.cloud/api/payment/crypto/direct/create \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "plan": "professional",
    "email": "your@email.com",
    "crypto_currency": "USDT"
  }'
```

响应:

```json
{
  "payment_id": 124,
  "crypto_currency": "USDT",
  "crypto_amount": 199.0,
  "payment_address": "0x1234567890abcdef...",
  "amount_usd": 199.00,
  "expires_at": "2025-01-02T00:00:00Z",
  "status": "pending",
  "message": "请向指定地址转账,并保存交易哈希",
  "instructions": {
    "step1": "复制支付地址",
    "step2": "使用钱包转账指定金额",
    "step3": "提交交易哈希等待确认"
  }
}
```

### 步骤 2: 转账

使用你的钱包 (如 MetaMask, Trust Wallet) 向 `payment_address` 转账 `crypto_amount`。

⚠️ **重要提示**:
- 请转账**精确金额**
- 确认网络正确 (USDT 使用 ERC20)
- 保存交易哈希 (TxHash)

### 步骤 3: 提交交易哈希

```bash
curl -X POST https://quantmesh.cloud/api/payment/crypto/124/submit-tx \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "transaction_hash": "0xabcdef1234567890..."
  }'
```

### 步骤 4: 等待确认

- 我们会在 1-24 小时内确认你的支付
- 确认后立即激活订阅
- 你会收到邮件通知

## 通过 Web 界面支付

### 1. 登录账号

访问 https://quantmesh.cloud/login

### 2. 选择套餐

进入"定价"页面,选择套餐。

### 3. 选择支付方式

- **Coinbase Commerce**: 点击"使用加密货币支付"
- **直接支付**: 点击"直接钱包支付"

### 4. 完成支付

按照页面提示完成支付。

## 查看支付历史

```bash
curl https://quantmesh.cloud/api/payment/crypto/list \
  -H "Authorization: Bearer YOUR_TOKEN"
```

响应:

```json
{
  "payments": [
    {
      "id": 123,
      "plan": "professional",
      "amount": 199.00,
      "crypto_currency": "USDT",
      "crypto_amount": 199.0,
      "payment_method": "direct",
      "status": "completed",
      "transaction_hash": "0xabc...",
      "created_at": "2025-01-01T00:00:00Z",
      "completed_at": "2025-01-01T01:00:00Z"
    }
  ],
  "total": 1
}
```

## 常见问题

### Q: 支持哪些钱包?

A: 支持所有主流钱包:
- **桌面**: MetaMask, Trust Wallet, Coinbase Wallet
- **硬件**: Ledger, Trezor
- **交易所**: Binance, OKX, Bybit (提现到钱包后支付)

### Q: 转账金额不对怎么办?

A: 
- **多转了**: 联系客服退款或延长订阅
- **少转了**: 补足差额或联系客服
- 📧 Email: payment@quantmesh.io

### Q: 交易一直未确认?

A: 可能原因:
1. 网络拥堵 - 等待更多区块确认
2. Gas 费太低 - 加速交易
3. 网络错误 - 检查是否选对网络 (ERC20/TRC20)

### Q: 可以退款吗?

A: 
- 支付后 7 天内未使用可退款
- 退款到原支付地址
- 扣除网络手续费

### Q: 支持其他币种吗?

A: 目前支持 BTC, ETH, USDT, USDC。
如需其他币种,请联系客服。

### Q: 汇率如何计算?

A: 
- Coinbase Commerce: 使用 Coinbase 实时汇率
- 直接支付: 使用 CoinGecko API 汇率
- 汇率在创建支付时锁定

### Q: 支付有效期多久?

A: 
- Coinbase Commerce: 1 小时
- 直接支付: 24 小时
- 过期后需要重新创建支付

### Q: 如何获取发票?

A: 支付完成后:
1. 登录账号
2. 进入"账单"页面
3. 下载发票 PDF

### Q: 企业客户如何支付?

A: 企业客户请联系销售:
- 📧 Email: sales@quantmesh.io
- 💬 微信: quantmesh_sales
- 可开具增值税发票

## 安全提示

⚠️ **请注意**:
1. 仔细核对支付地址
2. 确认网络正确 (ERC20/TRC20)
3. 保存交易哈希
4. 不要向陌生地址转账
5. 警惕钓鱼网站

✅ **官方网站**: https://quantmesh.cloud
✅ **官方邮箱**: @quantmesh.io

## 技术支持

如有问题,请联系:
- 📧 Email: payment@quantmesh.io
- 💬 在线客服: https://quantmesh.cloud/support
- 📱 Telegram: @quantmesh_support

---

Copyright © 2025 QuantMesh Team. All Rights Reserved.

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
