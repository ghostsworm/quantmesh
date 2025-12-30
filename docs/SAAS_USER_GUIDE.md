# QuantMesh SaaS 使用手册

欢迎使用 QuantMesh 云端做市商服务!本手册将帮助你快速上手。

## 📋 目录

- [快速开始](#快速开始)
- [创建实例](#创建实例)
- [管理实例](#管理实例)
- [监控和日志](#监控和日志)
- [套餐管理](#套餐管理)
- [API 文档](#api-文档)
- [常见问题](#常见问题)

## 快速开始

### 1. 注册账号

访问 https://quantmesh.cloud 并注册账号。

### 2. 选择套餐

| 套餐 | 价格 | 配置 | 适合人群 |
|------|------|------|----------|
| 个人版 | $49/月 | 1核1G, 1交易对 | 个人交易者 |
| 专业版 | $199/月 | 2核2G, 5交易对, AI策略 | 专业交易者 |
| 企业版 | $999/月 | 4核8G, 无限交易对, 全功能 | 机构、团队 |

### 3. 创建实例

```bash
# 登录
curl -X POST https://quantmesh.cloud/api/auth/login \
  -d '{"email":"your@email.com","password":"your_password"}'

# 创建实例
curl -X POST https://quantmesh.cloud/api/saas/instances/create \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{"plan":"professional"}'
```

### 4. 访问实例

实例创建后,你会获得一个专属 URL:

```
https://instance-abc123.quantmesh.cloud
```

## 创建实例

### 通过 Web 界面

1. 登录 https://quantmesh.cloud
2. 点击"创建实例"
3. 选择套餐
4. 点击"确认创建"
5. 等待实例启动 (约 30 秒)

### 通过 API

```bash
curl -X POST https://quantmesh.cloud/api/saas/instances/create \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "plan": "professional"
  }'
```

响应:

```json
{
  "instance_id": "qm-abc123-1234567890",
  "status": "running",
  "plan": "professional",
  "url": "https://instance-abc123.quantmesh.cloud",
  "port": 8080,
  "created_at": "2025-01-01T00:00:00Z"
}
```

## 管理实例

### 查看实例列表

```bash
curl https://quantmesh.cloud/api/saas/instances \
  -H "Authorization: Bearer YOUR_TOKEN"
```

### 获取实例详情

```bash
curl https://quantmesh.cloud/api/saas/instances/INSTANCE_ID \
  -H "Authorization: Bearer YOUR_TOKEN"
```

### 停止实例

```bash
curl -X POST https://quantmesh.cloud/api/saas/instances/INSTANCE_ID/stop \
  -H "Authorization: Bearer YOUR_TOKEN"
```

### 启动实例

```bash
curl -X POST https://quantmesh.cloud/api/saas/instances/INSTANCE_ID/start \
  -H "Authorization: Bearer YOUR_TOKEN"
```

### 重启实例

```bash
curl -X POST https://quantmesh.cloud/api/saas/instances/INSTANCE_ID/restart \
  -H "Authorization: Bearer YOUR_TOKEN"
```

### 删除实例

```bash
curl -X DELETE https://quantmesh.cloud/api/saas/instances/INSTANCE_ID \
  -H "Authorization: Bearer YOUR_TOKEN"
```

## 监控和日志

### 查看实例指标

```bash
curl https://quantmesh.cloud/api/saas/instances/INSTANCE_ID/metrics \
  -H "Authorization: Bearer YOUR_TOKEN"
```

响应:

```json
{
  "instance_id": "qm-abc123-1234567890",
  "cpu_usage": 0.45,
  "memory_usage": 0.62,
  "cpu_limit": 2.0,
  "memory_limit": 2048,
  "uptime": 86400,
  "last_active": "2025-01-01T12:00:00Z"
}
```

### 查看实例日志

```bash
# 获取最近 1000 行日志
curl "https://quantmesh.cloud/api/saas/instances/INSTANCE_ID/logs?lines=1000" \
  -H "Authorization: Bearer YOUR_TOKEN"
```

### 实时日志流

```bash
# WebSocket 连接
wscat -c "wss://quantmesh.cloud/api/saas/instances/INSTANCE_ID/logs/stream" \
  -H "Authorization: Bearer YOUR_TOKEN"
```

### Grafana 监控

访问 https://quantmesh.cloud/grafana 查看可视化监控面板。

默认账号:
- 用户名: 你的邮箱
- 密码: 初始密码 (首次登录需修改)

## 套餐管理

### 查看当前套餐

```bash
curl https://quantmesh.cloud/api/billing/subscriptions \
  -H "Authorization: Bearer YOUR_TOKEN"
```

### 升级套餐

```bash
curl -X POST https://quantmesh.cloud/api/billing/subscriptions/update-plan \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{"new_plan":"enterprise"}'
```

### 取消订阅

```bash
# 周期结束后取消
curl -X POST https://quantmesh.cloud/api/billing/subscriptions/cancel \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{"immediately":false}'

# 立即取消
curl -X POST https://quantmesh.cloud/api/billing/subscriptions/cancel \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{"immediately":true}'
```

## API 文档

### 认证

所有 API 请求都需要在 Header 中包含 Bearer Token:

```
Authorization: Bearer YOUR_TOKEN
```

### 端点列表

#### 实例管理

- `POST /api/saas/instances/create` - 创建实例
- `GET /api/saas/instances` - 列出实例
- `GET /api/saas/instances/:id` - 获取实例详情
- `POST /api/saas/instances/:id/stop` - 停止实例
- `POST /api/saas/instances/:id/start` - 启动实例
- `POST /api/saas/instances/:id/restart` - 重启实例
- `DELETE /api/saas/instances/:id` - 删除实例
- `GET /api/saas/instances/:id/logs` - 获取日志
- `GET /api/saas/instances/:id/metrics` - 获取指标

#### 计费管理

- `GET /api/billing/plans` - 获取套餐列表
- `POST /api/billing/subscriptions/create` - 创建订阅
- `GET /api/billing/subscriptions` - 获取订阅信息
- `POST /api/billing/subscriptions/update-plan` - 更新套餐
- `POST /api/billing/subscriptions/cancel` - 取消订阅

### 错误码

| 状态码 | 说明 |
|--------|------|
| 200 | 成功 |
| 400 | 请求参数错误 |
| 401 | 未认证 |
| 403 | 无权限 |
| 404 | 资源不存在 |
| 429 | 请求过于频繁 |
| 500 | 服务器错误 |

## 常见问题

### Q: 实例启动失败?

A: 可能原因:
1. 配置错误 - 检查交易所 API Key
2. 资源不足 - 升级套餐
3. 网络问题 - 检查防火墙设置

查看日志定位问题:
```bash
curl https://quantmesh.cloud/api/saas/instances/INSTANCE_ID/logs
```

### Q: 如何配置交易所 API?

A: 
1. 登录实例: https://instance-abc123.quantmesh.cloud
2. 进入"配置"页面
3. 填入交易所 API Key 和 Secret
4. 保存并重启实例

### Q: 实例可以迁移吗?

A: 
- 同一账号下可以在不同区域间迁移
- 联系客服办理: support@quantmesh.io

### Q: 数据会丢失吗?

A: 
- 所有数据都有自动备份
- 保留最近 30 天的备份
- 可以随时恢复

### Q: 如何扩容?

A: 
- 企业版支持自动扩容
- 其他套餐可以手动升级

### Q: 支持自定义域名吗?

A: 
- 专业版及以上支持
- 在控制面板中配置 CNAME 记录

### Q: 如何导出数据?

A: 
```bash
# 导出交易记录
curl https://instance-abc123.quantmesh.cloud/api/export/trades \
  -H "Authorization: Bearer YOUR_TOKEN" \
  > trades.csv

# 导出配置
curl https://instance-abc123.quantmesh.cloud/api/export/config \
  -H "Authorization: Bearer YOUR_TOKEN" \
  > config.yaml
```

### Q: 支持哪些交易所?

A: 支持 20+ 主流交易所:
- Binance, OKX, Bybit, Bitget, Gate.io
- Huobi, KuCoin, Kraken, Bitfinex
- MEXC, BingX, Deribit, BitMEX
- 等等...

### Q: 可以同时运行多个实例吗?

A: 
- 个人版: 1 个实例
- 专业版: 3 个实例
- 企业版: 无限实例

### Q: 如何获得技术支持?

A: 
- 📧 Email: support@quantmesh.io
- 💬 在线客服: https://quantmesh.cloud/support
- 📚 文档中心: https://docs.quantmesh.io
- 💬 Discord: https://discord.gg/quantmesh

## 最佳实践

### 1. 安全设置

- 使用强密码
- 启用两步验证
- 定期更换 API Key
- 限制 API 权限 (只开启必要的权限)

### 2. 风险控制

- 设置合理的止损
- 控制单笔交易金额
- 分散投资多个交易对
- 定期检查持仓

### 3. 监控告警

- 配置邮件/Telegram 告警
- 监控 CPU/内存使用率
- 关注异常交易
- 定期查看日志

### 4. 成本优化

- 使用年付享受折扣
- 合理选择套餐
- 闲置时停止实例
- 利用推荐奖励

## 更新日志

### 2025-01-01
- 新增企业版套餐
- 支持自动扩容
- 优化实例启动速度

### 2024-12-01
- 新增 Grafana 监控
- 支持实时日志流
- 新增 20+ 交易所

## 联系我们

- 🌐 官网: https://quantmesh.cloud
- 📧 支持: support@quantmesh.io
- 💬 销售: sales@quantmesh.io
- 📱 微信: quantmesh_cloud

---

Copyright © 2025 QuantMesh Team. All Rights Reserved.

