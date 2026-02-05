# Bitkub 和 Coins.ph API 调研报告

## Bitkub (泰国) 调研结果

### 市场地位
- 泰国最大交易所，75.4%市场份额
- 2022年交易量：$286亿
- 受泰国SEC监管
- 服务泰国和国际客户

### API文档完整性
- ✅ **官方GitHub仓库**：https://github.com/bitkub/bitkub-official-api-docs
- ✅ **REST API V3**：完整文档
- ✅ **REST API V4**：完整文档
- ✅ **WebSocket API**：完整文档
- ✅ 提供Python V3示例代码
- ✅ 支持第三方SDK（Go、JavaScript、Python）

### 交易产品支持
- ❌ **不支持期货交易**
- ❌ **不支持永续合约**
- ❌ **不支持杠杆交易**
- ✅ **仅支持现货交易**

### API特性
- REST API V4支持完整的交易功能
- WebSocket API支持实时市场数据
- 支持泰铢(THB)交易对
- API密钥管理：最多可创建50个API密钥

### 接入评估
**结论**：**不推荐接入**

**原因**：
1. 项目主要面向期货交易，Bitkub仅支持现货
2. 虽然API文档完整，但功能不符合项目需求
3. 如果未来需要支持现货交易，可以考虑接入

---

## Coins.ph (菲律宾) 调研结果

### 市场地位
- 菲律宾主要交易所之一
- 支持PHP交易对
- 提供企业级交易服务

### API文档完整性
- ✅ **REST API文档**：https://docs.coins.ph/rest-api/
- ✅ **WebSocket Streams文档**：https://docs.coins.ph/web-socket-streams/
- ✅ **User Data Stream文档**：https://docs.coins.ph/user-data-stream/
- ✅ 提供JavaScript和Java SDK
- ✅ 文档定期更新

### API特性

#### REST API
- 支持订单下单和管理
- 账户和余额查询
- 转账和钱包管理
- 子账户管理
- Convert/Trading端点
- 定期更新新端点

#### WebSocket API
- **Base Endpoint**: `wss://wsapi.pro.coins.ph`
- 支持聚合交易流
- 支持订单簿深度（最多200档）
- 每5分钟需要ping保持连接
- 最多支持1024个流
- 支持动态订阅/取消订阅

#### User Data Streams
- 账户更新
- 余额更新（带业务类型分类）
- 订单更新

### 交易产品支持
- ✅ **现货交易**：完整支持
- ❓ **期货/永续合约**：**未找到明确证据**
- 搜索结果未显示期货或永续合约相关信息
- 主要面向现货交易和PHP交易对

### 接入评估
**结论**：**暂不推荐接入（需进一步确认）**

**原因**：
1. 未找到期货/永续合约支持的明确证据
2. 项目主要面向期货交易
3. 如果确认支持期货，可以考虑接入

**建议**：
- 联系Coins.ph业务团队确认：crypto-business@coins.ph
- 如果确认支持期货，可以按照WhiteBIT的模式实现

---

## 总结与建议

### 已完成接入
- ✅ **WhiteBIT** (乌克兰) - 支持期货，已完整实现

### 不推荐接入
- ❌ **Bitkub** (泰国) - 仅支持现货，不符合项目需求
- ❌ **Coins.ph** (菲律宾) - 未确认期货支持，暂不推荐

### 后续建议

1. **寻找其他支持期货的区域交易所**
   - 可以调研其他地区的期货交易所
   - 重点关注API文档完整性和技术成熟度

2. **如果未来需要支持现货交易**
   - Bitkub可以作为候选（API文档完整）
   - Coins.ph也可以考虑（如果确认功能完整）

3. **继续调研其他区域交易所**
   - 可以关注其他未覆盖地区的交易所
   - 优先选择支持期货且API文档完善的交易所

---

## 参考资料

- Bitkub官方API文档：https://github.com/bitkub/bitkub-official-api-docs
- Coins.ph REST API文档：https://docs.coins.ph/rest-api/
- Coins.ph WebSocket文档：https://docs.coins.ph/web-socket-streams/
- Coins.ph User Data Stream文档：https://docs.coins.ph/user-data-stream/
