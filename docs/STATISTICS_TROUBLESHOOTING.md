# 统计数据显示为零的问题排查

## 问题现象

收益统计页面显示的所有数据都是零：
- 总交易数：0
- 总交易量：0.0000
- 总盈亏：+0.00
- 胜率：0.00%

## 问题原因

统计数据是从 `trades` 表实时计算的，如果表中没有数据，统计结果就会全是零。

可能的原因：
1. **系统没有进行过交易** - 最常见的原因
2. **交易记录没有保存到数据库** - tradeStorage 未正确初始化
3. **数据库连接问题** - 数据库未正确连接
4. **账户隔离问题** - 查询的账户与实际交易的账户不匹配

## 排查步骤

### 1. 检查系统是否在运行交易

查看系统日志，确认是否有交易活动：

```bash
# 查看最近的日志
tail -f logs/quantmesh.log | grep -E "买单成交|卖单成交|交易记录已保存"
```

如果看到类似这样的日志，说明有交易在进行：
```
✅ [买单成交] 价格: 95000.00, 持仓: 0.0010
✅ [卖单成交] 价格: 95100.00, 剩余持仓: 0.0000
💰 [交易记录已保存] 买入价: 95000.00, 卖出价: 95100.00, 数量: 0.0010, 盈亏: 0.10
```

### 2. 检查数据库中的交易记录

直接查询数据库：

```bash
# 进入项目目录
cd /path/to/quantmesh

# 查询 trades 表
sqlite3 data/quantmesh.db "SELECT COUNT(*) as total_trades FROM trades;"

# 查看最近的交易记录
sqlite3 data/quantmesh.db "SELECT * FROM trades ORDER BY created_at DESC LIMIT 10;"

# 查看统计汇总
sqlite3 data/quantmesh.db "
SELECT 
  COUNT(*) as total_trades,
  SUM(quantity) as total_volume,
  SUM(pnl) as total_pnl,
  CAST(SUM(CASE WHEN pnl > 0 THEN 1 ELSE 0 END) AS FLOAT) / COUNT(*) as win_rate
FROM trades;
"
```

### 3. 检查数据库表结构

确认 trades 表是否存在：

```bash
sqlite3 data/quantmesh.db ".schema trades"
```

应该看到类似这样的输出：
```sql
CREATE TABLE trades (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    buy_order_id INTEGER,
    sell_order_id INTEGER,
    exchange TEXT,
    symbol TEXT,
    buy_price REAL,
    sell_price REAL,
    quantity REAL,
    pnl REAL,
    created_at DATETIME,
    account TEXT
);
```

### 4. 检查系统配置

确认交易是否启用：

```bash
# 查看配置文件
cat config.yaml | grep -A 5 "trading:"
```

确认以下配置：
- `enabled: true` - 交易已启用
- `symbol` 已配置
- `order_quantity` 已配置

### 5. 检查 API 响应

使用浏览器开发者工具或 curl 检查 API 响应：

```bash
# 获取统计数据
curl -X GET "http://localhost:8080/api/statistics" \
  -H "Cookie: session=your_session_cookie" \
  -H "Accept: application/json"
```

## 解决方案

### 方案 1: 启动交易（如果系统未运行）

1. 确保配置文件中交易已启用
2. 启动或重启系统
3. 等待系统开始下单和成交

### 方案 2: 检查 tradeStorage 初始化

如果系统有交易但没有保存记录，检查代码中的 tradeStorage 初始化：

```go
// main.go 中应该有类似的代码
tradeStorage := &tradeStorageAdapter{storage: st}
```

### 方案 3: 手动插入测试数据（仅用于测试）

**警告：仅用于测试环境，不要在生产环境使用！**

```sql
INSERT INTO trades (
  buy_order_id, sell_order_id, exchange, symbol, 
  buy_price, sell_price, quantity, pnl, created_at, account
) VALUES (
  1, 2, 'binance', 'BTCUSDT',
  95000.0, 95100.0, 0.001, 0.1, datetime('now'), ''
);
```

### 方案 4: 等待交易自然发生

如果系统刚启动，可能需要等待：
1. 价格波动触发买单
2. 买单成交建仓
3. 价格上涨触发卖单
4. 卖单成交平仓并记录交易

这个过程可能需要几分钟到几小时，取决于市场波动和配置的网格间距。

## 验证修复

修复后，应该能看到：

1. **日志中有交易记录**：
```
💰 [交易记录已保存] 买入价: 95000.00, 卖出价: 95100.00, 数量: 0.0010, 盈亏: 0.10
```

2. **数据库中有数据**：
```bash
sqlite3 data/quantmesh.db "SELECT COUNT(*) FROM trades;"
# 输出应该 > 0
```

3. **统计页面显示正常数据**：
- 总交易数 > 0
- 总盈亏显示实际数值
- 胜率显示百分比

## 常见问题

### Q: 为什么刚启动系统统计就是零？
A: 系统刚启动时还没有完成任何交易，统计数据为零是正常的。需要等待至少一笔完整的买卖交易完成。

### Q: 系统运行了很久但统计还是零？
A: 可能的原因：
- 价格波动不够，没有触发交易
- 网格间距设置太大，价格没有达到触发条件
- 资金不足，无法下单
- 交易被风控暂停

检查日志中是否有 "买单成交" 或 "卖单成交" 的记录。

### Q: 日志显示有交易但统计还是零？
A: 可能是 tradeStorage 没有正确初始化，或者数据库写入失败。检查日志中是否有 "保存交易记录失败" 的警告。

### Q: 如何快速测试统计功能？
A: 可以：
1. 降低网格间距（如 `price_interval: 10`）
2. 减少订单数量（如 `order_quantity: 0.0001`）
3. 等待价格波动触发交易
4. 或者在测试环境手动插入测试数据

## 相关文件

- 统计 API: `web/api.go` (getStatistics 函数)
- 数据查询: `storage/sqlite.go` (GetStatisticsSummary 函数)
- 交易保存: `position/super_position_manager.go` (SaveTrade 调用)
- 前端组件: `webui/src/components/Statistics.tsx`

## 技术细节

### 统计数据计算逻辑

```sql
SELECT 
  COUNT(*) as total_trades,
  SUM(quantity) as total_volume,
  SUM(pnl) as total_pnl,
  CAST(SUM(CASE WHEN pnl > 0 THEN 1 ELSE 0 END) AS FLOAT) / COUNT(*) as win_rate
FROM trades
WHERE (account = ? OR account IS NULL OR account = '')
```

### 交易记录保存时机

交易记录在以下情况保存：
1. 卖单完全成交（FILLED）时
2. 计算出实际盈亏后
3. 通过 tradeStorage.SaveTrade() 保存

### 数据流程

```
订单成交 → SuperPositionManager.handleOrderUpdate()
         → 计算盈亏
         → tradeStorage.SaveTrade()
         → SQLiteStorage.SaveTrade()
         → INSERT INTO trades
         → 统计 API 查询 trades 表
         → 前端显示统计数据
```
