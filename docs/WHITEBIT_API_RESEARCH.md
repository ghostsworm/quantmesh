# WhiteBIT API 调研报告

> **调研日期**: 2025-02-05  
> **调研目的**: 评估WhiteBIT交易所API接入的技术可行性和实施复杂度

## 1. 交易所基本信息

### 1.1 市场地位
- **欧洲最大加密货币交易所之一**，乌克兰市场领导者
- 服务超过**200万用户**
- 全球安全性排名**前3**
- 日均交易量：未明确，但市场地位高

### 1.2 交易产品
- ✅ **现货交易**：支持多种交易对
- ✅ **期货交易**：支持永续合约（Perpetual Futures）
  - 最高杠杆：**100x**
  - 交易对格式：`BTC_PERP`, `ETH_PERP` 等
  - 手续费：maker 0.01%, taker 0.035%
- ✅ **保证金交易**：支持杠杆交易

## 2. API 架构概览

### 2.1 API版本
- **Public API V4**: 公开市场数据
- **Private API V4**: 私有交易API
- **WebSocket API**: 实时数据流

### 2.2 基础URL
- **REST API**: `https://whitebit.com/api/v4/`
- **WebSocket**: `wss://api.whitebit.com/ws`
- **EU节点**: 支持（文档中提到Global和EU两个节点）

## 3. REST API 认证机制

### 3.1 签名算法
- **算法**: `HMAC-SHA512`
- **编码**: Hex编码
- **签名公式**: `hex(HMAC_SHA512(payload, key=api_secret))`

### 3.2 请求格式
**HTTP方法**: 所有私有API请求必须使用 **POST** 方法

**请求头 (Headers)**:
```
Content-Type: application/json
X-TXC-APIKEY: your_api_key
X-TXC-PAYLOAD: base64_encoded_payload
X-TXC-SIGNATURE: hex_encoded_signature
```

**请求体 (Body)**:
```json
{
  "request": "/api/v4/trade-account/balance",  // 请求路径（不含域名）
  "nonce": 1594297865,                        // 递增数字（推荐使用Unix时间戳毫秒）
  "nonceWindow": true,                        // 可选：启用时间验证（±5秒）
  // ... 其他端点特定参数
}
```

### 3.3 签名生成步骤
1. 构建请求体JSON对象
2. 将请求体序列化为JSON字符串
3. Base64编码JSON字符串 → `X-TXC-PAYLOAD`
4. 使用HMAC-SHA512签名：`hex(HMAC_SHA512(payload, api_secret))` → `X-TXC-SIGNATURE`

### 3.4 Nonce管理
- **推荐**: 使用Unix时间戳（毫秒）
- **要求**: 每个nonce必须大于前一个请求的nonce
- **nonceWindow模式**: 
  - 启用时，nonce必须在服务器时间的±5秒内
  - 适用于高频交易系统

## 4. WebSocket API

### 4.1 连接信息
- **端点**: `wss://api.whitebit.com/ws`
- **协议**: JSON RPC 2.0
- **连接超时**: 60秒无活动自动断开
- **心跳间隔**: 每50秒发送ping消息

### 4.2 认证流程
1. **获取WebSocket Token**:
   ```
   POST /api/v4/profile/websocket_token
   ```
   返回：`{"websocket_token": "your_token"}`

2. **WebSocket认证**:
   ```json
   {
     "id": 0,
     "method": "authorize",
     "params": ["websocket_token", "public"]
   }
   ```

### 4.3 支持的订阅
- `balanceSpot_subscribe` - 现货余额更新
- `balanceMargin_subscribe` - 保证金余额更新
- `ordersPending_subscribe` - 活跃订单更新（实时）
- `ordersExecuted_subscribe` - 已执行订单更新（实时）
- `deals_subscribe` - 成交记录更新（实时）
- `positionsMargin_subscribe` - 持仓更新（1秒间隔）

### 4.4 限频规则
- **连接数**: 1000 WebSocket连接/分钟
- **请求数**: 200请求/分钟/连接

## 5. 核心API端点

### 5.1 市场数据（Public API）
| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/v4/public/markets` | GET | 获取所有市场信息（现货+期货） |
| `/api/v4/public/futures` | GET | 获取期货市场列表 |
| `/api/v4/public/orderbook/{market}` | GET | 获取订单簿 |
| `/api/v4/public/ticker` | GET | 获取24小时价格和成交量摘要 |
| `/api/v4/public/funding-history/{market}` | GET | 获取资金费率历史 |

### 5.2 交易操作（Private API V4）
| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/v4/trade-account/balance` | POST | 获取交易账户余额 |
| `/api/v4/order/new` | POST | 创建限价订单 |
| `/api/v4/order/bulk` | POST | 批量创建限价订单（最多20个） |
| `/api/v4/order/market` | POST | 创建市价订单 |
| `/api/v4/order/cancel` | POST | 取消订单 |
| `/api/v4/order/cancel/all` | POST | 取消所有订单 |
| `/api/v4/orders` | POST | 查询活跃订单 |
| `/api/v4/trade-account/executed-history` | POST | 查询已执行订单历史 |
| `/api/v4/trade-account/order` | POST | 查询订单成交记录 |
| `/api/v4/order/modify` | POST | 修改订单 |

### 5.3 期货/保证金操作
| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/v4/collateral-account/balance` | POST | 获取保证金账户余额 |
| `/api/v4/order/collateral/limit` | POST | 创建保证金限价订单 |
| `/api/v4/order/collateral/bulk` | POST | 批量创建保证金订单 |
| `/api/v4/collateral-account/positions` | POST | 获取持仓信息 |
| `/api/v4/collateral-account/positions/history` | POST | 获取持仓历史 |
| `/api/v4/collateral-account/funding-history` | POST | 获取资金费用历史 |

## 6. 限频规则

### 6.1 REST API限频
| 端点类型 | 限频 |
|---------|------|
| 公开市场数据 | 2000请求/10秒 |
| 订单簿 | 600请求/10秒 |
| 交易账户余额 | 12000请求/10秒 |
| 下单/撤单 | 10000请求/10秒 |
| 查询订单 | 1000请求/10秒 |
| WebSocket Token | 10请求/60秒 |

### 6.2 WebSocket限频
- 1000连接/分钟
- 200请求/分钟/连接

## 7. 期货交易对格式

### 7.1 交易对命名
- **永续合约**: `BTC_PERP`, `ETH_PERP`, `SOL_PERP` 等
- **现货**: `BTC_USDT`, `ETH_USDT` 等

### 7.2 获取期货市场列表
```http
GET /api/v4/public/futures
```

响应示例：
```json
{
  "success": true,
  "result": [{
    "ticker_id": "BTC_PERP",
    "stock_currency": "BTC",
    "money_currency": "USDT",
    "last_price": "24005.5",
    "funding_rate": "0.000044889033693137",
    "next_funding_rate_timestamp": "1660665600000",
    "max_leverage": 100,
    "funding_interval_minutes": 300
  }]
}
```

## 8. 订单类型

### 8.1 支持的订单类型
| 类型ID | 名称 | 说明 |
|--------|------|------|
| 1 | Limit | 限价订单 |
| 2 | Market | 市价订单 |
| 3 | Stop Limit | 止损限价订单 |
| 4 | Stop Market | 止损市价订单 |
| 7 | Margin Limit | 保证金限价订单 |
| 8 | Margin Market | 保证金市价订单 |
| 9 | Margin Stop Limit | 保证金止损限价订单 |
| 10 | Margin Trigger Market | 保证金触发市价订单 |

### 8.2 订单状态
- `OPEN` - 活跃订单
- `FILLED` - 已成交
- `CANCELED` - 已取消
- `PARTIALLY_FILLED` - 部分成交
- `AUTO_CANCELED_USER_MARGIN` - 自动取消（保证金不足）
- `AUTO_CANCELED_LIQUIDATION` - 自动取消（清算）
- `AUTO_CANCELED_REDUCE_ONLY` - 自动取消（仅减仓）

## 9. 错误处理

### 9.1 错误响应格式
```json
{
  "code": 30,
  "message": "Validation failed",
  "errors": {
    "amount": ["Amount field is required."]
  }
}
```

### 9.2 常见错误码
| 错误码 | 说明 |
|--------|------|
| 1 | Invalid argument |
| 2 | Order not found |
| 10 | Not enough balance |
| 11 | Amount too small |
| 30 | Validation failed |
| 31 | Market validation failed |
| 32 | Amount validation failed |
| 33 | Price validation failed |
| 36 | ClientOrderId validation failed |

## 10. 测试网支持

### 10.1 测试环境
❌ **WhiteBIT没有提供测试网或沙箱环境**

**影响**:
- 需要在主网进行小额测试
- 建议使用最小金额进行验证
- 需要谨慎处理API密钥权限

### 10.2 建议
- 创建专门的测试API密钥
- 设置IP限制和端点限制
- 使用最小交易金额进行测试

## 11. 实施复杂度评估

### 11.1 技术复杂度：中等

**优势**:
- ✅ API文档完整清晰
- ✅ 支持批量操作（最多20个订单）
- ✅ WebSocket实时数据完善
- ✅ 支持期货交易（永续合约）
- ✅ 错误信息详细

**挑战**:
- ⚠️ 签名算法使用SHA512（与项目中其他交易所的SHA256不同）
- ⚠️ 需要Base64编码payload
- ⚠️ 没有测试网，需要在主网测试
- ⚠️ WebSocket需要先获取token再认证

### 11.2 与现有交易所对比

| 特性 | WhiteBIT | OKX | Bybit |
|------|----------|-----|-------|
| 签名算法 | HMAC-SHA512 | HMAC-SHA256 | HMAC-SHA256 |
| 测试网 | ❌ | ✅ | ✅ |
| 批量下单 | ✅ (20个) | ✅ | ✅ |
| WebSocket认证 | Token | 签名 | 签名 |
| 期货支持 | ✅ | ✅ | ✅ |
| 文档质量 | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ |

## 12. 实施建议

### 12.1 优先级：P0（高优先级）

**理由**:
1. API文档完整，技术成熟
2. 支持期货交易，符合项目需求
3. 市场地位高（欧洲最大交易所之一）
4. 乌克兰地区有明确需求

### 12.2 实施步骤

1. **REST API客户端实现**（5-7天）
   - 实现HMAC-SHA512签名
   - 实现Base64编码payload
   - 实现核心API方法

2. **WebSocket实现**（5-7天）
   - 实现token获取
   - 实现WebSocket认证
   - 实现订单流和K线流订阅

3. **适配器实现**（5-7天）
   - 实现IExchange接口
   - 处理类型转换
   - 错误处理

4. **测试验证**（3-5天）
   - 主网小额测试
   - 功能完整性验证
   - 稳定性测试

### 12.3 注意事项

1. **签名算法差异**: WhiteBIT使用SHA512，需要单独实现
2. **测试环境**: 没有测试网，需要谨慎测试
3. **WebSocket心跳**: 必须每50秒发送ping，否则60秒后断开
4. **Nonce管理**: 需要确保nonce递增，推荐使用时间戳
5. **限频控制**: 注意不同端点的限频规则

## 13. 参考资源

- **API文档**: https://docs.whitebit.com/
- **认证文档**: https://docs.whitebit.com/private/http-auth/
- **WebSocket文档**: https://docs.whitebit.com/private/websocket/
- **API Quick Start**: https://github.com/whitebit-exchange/api-quickstart
- **API设置**: https://whitebit.com/settings/api

## 14. 结论

WhiteBIT交易所API技术成熟，文档完善，支持期货交易，适合接入。主要挑战在于：
1. 签名算法使用SHA512（需要单独实现）
2. 没有测试网环境（需要主网小额测试）
3. WebSocket认证流程相对复杂（需要先获取token）

总体评估：**推荐接入**，预计实施周期2-3周。
