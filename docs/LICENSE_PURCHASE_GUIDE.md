# QuantMesh License 购买指南

本指南介绍如何购买和使用 QuantMesh 商业插件的 License。

## 📋 目录

- [为什么需要 License](#为什么需要-license)
- [License 类型](#license-类型)
- [购买流程](#购买流程)
- [激活 License](#激活-license)
- [License 管理](#license-管理)
- [常见问题](#常见问题)

## 为什么需要 License

QuantMesh 采用双授权模式:

- **开源版 (AGPL-3.0)**: 免费使用,但所有修改必须开源
- **商业版**: 购买 License 后可以:
  - 使用高级 AI 策略
  - 使用多策略组合
  - 使用高级风控功能
  - 不需要开源你的修改
  - 获得技术支持

## License 类型

### 1. 插件 License

按插件购买,适合只需要特定功能的用户。

| 插件 | 价格 | 功能 |
|------|------|------|
| AI 策略插件 | $99/月 | 市场分析、参数优化、风险分析、情绪分析 |
| 多策略插件 | $49/月 | 动量策略、均值回归、趋势跟踪 |
| 高级风控插件 | $79/月 | 机器学习风险模型、投资组合优化 |

### 2. 套餐 License

包含多个插件,更划算。

| 套餐 | 价格 | 包含插件 | 适合人群 |
|------|------|----------|----------|
| 专业版 | $199/月 | AI 策略 + 多策略 | 专业交易者 |
| 企业版 | $499/月 | 所有插件 + 定制支持 | 机构、团队 |

### 3. 永久 License

一次性付费,永久使用。

| 类型 | 价格 | 说明 |
|------|------|------|
| 单插件永久 | $999 | 任选一个插件 |
| 全插件永久 | $2,999 | 所有插件 |

## 购买流程

### 方式 1: 在线购买 (推荐)

1. 访问 https://quantmesh.io/pricing
2. 选择套餐或插件
3. 点击"立即购买"
4. 填写信息并完成支付
5. 立即收到 License Key

### 方式 2: 联系销售

适合企业客户、需要定制的用户。

- 📧 Email: sales@quantmesh.io
- 💬 微信: quantmesh_sales
- 📞 电话: +86 400-xxx-xxxx

### 方式 3: 申请试用

所有插件提供 7 天免费试用。

1. 访问 https://quantmesh.io/trial
2. 填写申请表单
3. 收到试用 License Key
4. 试用期结束后可转为付费

## 激活 License

### 1. 获取 License Key

购买后,你会收到类似这样的 License Key:

```
eyJwbHVnaW5fbmFtZSI6ImFpX3N0cmF0ZWd5IiwiY3VzdG9tZXJfaWQiOiJjdXN0b21lcjEyMyIsInBsYW4iOiJwcm9mZXNzaW9uYWwiLCJleHBpcnlfZGF0ZSI6IjIwMjUtMTItMzFUMjM6NTk6NTlaIiwic2lnbmF0dXJlIjoiLi4uIn0=
```

### 2. 配置 License

编辑 `config.yaml`:

```yaml
plugins:
  enabled: true
  directory: "./plugins"
  
  # 填入你的 License Keys
  licenses:
    ai_strategy: "eyJwbHVnaW5fbmFtZSI6ImFpX3N0cmF0ZWd5Ii..."
    multi_strategy: "eyJwbHVnaW5fbmFtZSI6Im11bHRpX3N0cmF0ZWd5Ii..."
    advanced_risk: ""
  
  config:
    ai_strategy:
      gemini_api_key: "your_gemini_key"
      openai_api_key: "your_openai_key"
```

### 3. 下载插件

```bash
# 从 QuantMesh 下载中心下载插件
wget https://downloads.quantmesh.io/plugins/ai_strategy.so
wget https://downloads.quantmesh.io/plugins/multi_strategy.so

# 放到 plugins 目录
mv *.so plugins/
```

### 4. 启动验证

```bash
./quantmesh
```

查看日志确认 License 验证成功:

```
✅ 插件 ai_strategy License 验证通过
✅ 插件加载成功: ai_strategy (版本: 1.0.0)
```

## License 管理

### 查看 License 信息

```bash
curl http://localhost:8080/api/plugins/licenses
```

响应:

```json
{
  "licenses": [
    {
      "plugin_name": "ai_strategy",
      "plan": "professional",
      "expiry_date": "2025-12-31T23:59:59Z",
      "status": "active"
    }
  ]
}
```

### 更新 License

1. 获取新的 License Key
2. 更新 `config.yaml`
3. 重启 QuantMesh

或者通过 API 更新:

```bash
curl -X POST http://localhost:8080/api/plugins/licenses/update \
  -H "Content-Type: application/json" \
  -d '{
    "plugin_name": "ai_strategy",
    "license_key": "new_license_key"
  }'
```

### 续费

License 到期前 7 天,系统会发送续费提醒。

续费方式:
1. 登录 https://quantmesh.io/account
2. 点击"续费"
3. 完成支付
4. 新的 License Key 会自动更新

### 取消订阅

```bash
curl -X POST http://localhost:8080/api/billing/subscriptions/cancel \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{"immediately": false}'
```

- `immediately: false`: 当前周期结束后取消
- `immediately: true`: 立即取消

## 常见问题

### Q: License 可以在多台机器上使用吗?

A: 取决于你购买的 License 类型:
- 单机 License: 只能在一台机器上使用
- 多机 License: 可以在指定数量的机器上使用
- 企业 License: 可以在公司内部任意机器上使用

### Q: License 过期后会怎样?

A: 
- 插件会停止工作
- 核心功能(基础网格策略)继续可用
- 数据不会丢失
- 续费后立即恢复

### Q: 可以退款吗?

A: 
- 7 天内无理由退款
- 超过 7 天但未使用可申请退款
- 已使用的按比例退款

### Q: License 可以转让吗?

A: 
- 个人 License 不可转让
- 企业 License 可以在公司内部转让
- 需要联系客服办理

### Q: 如何验证 License 是否有效?

A: 

```bash
curl -X POST https://license.quantmesh.io/api/license/verify \
  -H "Content-Type: application/json" \
  -d '{
    "license_key": "your_license_key",
    "machine_id": "your_machine_id"
  }'
```

### Q: License 验证需要联网吗?

A: 
- 首次激活需要联网
- 之后每 24 小时验证一次
- 离线最多可用 7 天

### Q: 忘记 License Key 怎么办?

A: 
1. 登录 https://quantmesh.io/account
2. 查看"我的 License"
3. 或联系客服: support@quantmesh.io

## 价格优惠

### 学生优惠

凭学生证可享受 50% 折扣。

### 年付优惠

年付可享受 2 个月免费 (相当于 83 折)。

### 团队优惠

- 3-5 人: 85 折
- 6-10 人: 75 折
- 11+ 人: 联系销售获取报价

### 推荐奖励

推荐好友购买,双方各获得 1 个月免费使用。

## 联系我们

- 🌐 官网: https://quantmesh.io
- 📧 销售: sales@quantmesh.io
- 💬 支持: support@quantmesh.io
- 📞 电话: +86 400-xxx-xxxx
- 💬 微信: quantmesh_sales

---

Copyright © 2025 QuantMesh Team. All Rights Reserved.

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
