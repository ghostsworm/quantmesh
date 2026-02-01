# OKX 模拟盘使用指南

## 📌 重要说明

**OKX 没有独立的测试网 Web 入口**，模拟盘功能是通过创建**模拟盘 API Key** 来实现的。

## 🔑 如何创建 OKX 模拟盘 API Key

### 方法 1：通过 API 管理页面（推荐）

1. **登录 OKX 账户**
   - 访问：https://www.okx.com/
   - 登录你的账户

2. **进入 API 管理页面**
   - 点击右上角头像/设置图标
   - 选择"API"或"API 管理"
   - 或者直接访问：https://www.okx.com/account/my-api

3. **创建模拟盘 API Key**
   - 点击"创建 API Key"或"Create API Key"
   - **重要**：在创建页面中，找到"交易模式"或"Trading Mode"选项
   - 选择"**模拟盘**"或"**Demo Trading**"（不是"实盘"）
   - 设置 API 权限：
     - ✅ 读取权限（必需）
     - ✅ 交易权限（必需）
     - ❌ 提现权限（不需要）
   - 设置 IP 白名单（可选，但推荐）
   - 创建并保存：
     - API Key
     - Secret Key
     - Passphrase（如果设置了）

### 方法 2：通过模拟交易页面

1. **访问模拟交易页面**
   - 尝试访问：https://www.okx.com/trade-demo
   - 或者：https://www.okx.com/trade-demo/btc-usdt-swap
   - 如果页面不存在，使用方法 1

2. **在模拟交易页面创建 API Key**
   - 登录后，在模拟交易页面找到"API 管理"入口
   - 按照方法 1 的步骤创建模拟盘 API Key

## ⚙️ 配置系统使用模拟盘

创建好模拟盘 API Key 后，在 `config.yaml` 中配置：

```yaml
exchanges:
  okx:
    api_key: "你的模拟盘_API_KEY"
    secret_key: "你的模拟盘_SECRET_KEY"
    passphrase: "你的_PASSPHRASE"
    fee_rate: 0.0002
    testnet: true  # ⚠️ 重要：设置为 true 启用模拟盘
```

## 🔍 如何确认使用的是模拟盘

### 1. 查看日志

启动系统后，查看日志应该显示：

```
[INFO] 🌐 [OKX] 使用模拟盘模式
```

如果显示"使用实盘模式"，说明配置有误。

### 2. 检查 API 请求头

模拟盘模式下，系统会自动在请求头中添加：
```
x-simulated-trading: 1
```

### 3. 测试交易

在模拟盘模式下进行交易：
- ✅ 不会产生真实资金损失
- ✅ 可以使用虚拟资金测试
- ✅ 订单会显示在模拟盘账户中

## 📝 注意事项

1. **模拟盘 API Key 和实盘 API Key 是分开的**
   - 需要分别创建
   - 不能混用

2. **模拟盘 API Key 的特点**
   - 使用虚拟资金
   - 不会影响真实账户
   - 可以无限充值测试币

3. **如果找不到模拟盘选项**
   - 可能你的账户是新账户，需要先完成实名认证
   - 或者联系 OKX 客服咨询

4. **模拟盘 API 地址**
   - REST API: `https://www.okx.com`（与主网相同）
   - WebSocket: `wss://wspap.okx.com:8443/ws/v5/private?brokerId=9999`
   - 区别在于请求头中的 `x-simulated-trading: 1` 标识

## 🆘 常见问题

### Q1: 在 API 管理页面找不到"模拟盘"选项？

**A**: 
- 确认你已经登录账户
- 尝试刷新页面
- 检查账户是否完成实名认证
- 如果仍然找不到，联系 OKX 客服

### Q2: 创建了 API Key，但系统显示"实盘模式"？

**A**: 
- 检查 `config.yaml` 中 `testnet: true` 是否设置正确
- 确认使用的是模拟盘 API Key（不是实盘 API Key）
- 查看日志确认配置是否生效

### Q3: 模拟盘和实盘可以同时使用吗？

**A**: 
- 可以，但需要使用不同的 API Key
- 建议在测试阶段只使用模拟盘
- 实盘交易前充分测试

### Q4: 模拟盘的虚拟资金如何充值？

**A**: 
- 模拟盘通常提供自动充值功能
- 在模拟盘账户页面可以找到充值选项
- 可以随意充值，不影响真实账户

## 🔗 相关链接

- OKX 官网：https://www.okx.com/
- OKX API 文档：https://www.okx.com/docs-v5/zh/
- API 管理页面：https://www.okx.com/account/my-api

## 📞 需要帮助？

如果仍然无法找到模拟盘功能，建议：
1. 联系 OKX 官方客服
2. 查看 OKX 官方文档
3. 在 OKX 社区论坛提问
