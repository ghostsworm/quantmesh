# 如何添加新的交易对

## 问题描述

如果您发现账户中有其他交易对的持仓（如 BCHUSDT），但系统配置中没有该交易对，您需要将其添加到配置中。

## 方法1：通过配置文件添加（推荐）

### 步骤

1. **打开配置文件**
   - 配置文件路径：`config.yaml`
   - 如果不存在，可以复制 `config.example.yaml` 并重命名

2. **找到交易对配置部分**
   ```yaml
   trading:
     # 多交易对配置（推荐）
     symbols:
       - exchange: "binance"
         symbol: "BTCUSDT"
         price_interval: 10
         order_quantity: 50
         min_order_value: 20
         buy_window_size: 8
         sell_window_size: 8
         reconcile_interval: 60
         order_cleanup_threshold: 50
         cleanup_batch_size: 20
         margin_lock_duration_seconds: 20
         position_safety_check: 100
   ```

3. **添加新的交易对配置**
   在 `symbols` 列表中添加 BCHUSDT：
   ```yaml
   trading:
     symbols:
       - exchange: "binance"
         symbol: "BTCUSDT"
         # ... 其他配置
       - exchange: "binance"  # 如果使用默认交易所，可以留空
         symbol: "BCHUSDT"
         price_interval: 2      # 根据BCH价格波动调整
         order_quantity: 30      # 每单金额（USDT）
         min_order_value: 20     # 最小订单价值
         buy_window_size: 10     # 买单窗口大小
         sell_window_size: 10    # 卖单窗口大小
         reconcile_interval: 60  # 对账间隔（秒）
         order_cleanup_threshold: 50  # 订单清理上限
         cleanup_batch_size: 20  # 清理批次大小
         margin_lock_duration_seconds: 20  # 保证金锁定时间
         position_safety_check: 100  # 持仓安全性检查
   ```

4. **保存配置文件**

5. **重启系统或重新加载配置**
   - 如果系统支持热重载，配置会自动生效
   - 否则需要重启系统

### 配置参数说明

- **exchange**: 交易所名称（如 "binance", "bitget"），如果留空则使用 `app.current_exchange`
- **symbol**: 交易对名称（如 "BCHUSDT"）
- **price_interval**: 价格间隔（USDT），建议根据币种价格波动设置
  - BTC: 10-50 USDT
  - ETH: 2-10 USDT
  - BCH: 2-5 USDT
  - 其他小币种: 0.1-2 USDT
- **order_quantity**: 每单购买金额（USDT）
- **min_order_value**: 最小订单价值（USDT），小于此值不挂单
- **buy_window_size**: 买单窗口大小（网格层数）
- **sell_window_size**: 卖单窗口大小（网格层数）

## 方法2：通过Web UI添加（即将支持）

我们正在开发Web UI的交易对管理功能，届时您可以直接在配置页面添加、编辑和删除交易对。

## 常见问题

### Q1: 如何确定合适的 price_interval？

**A**: 建议根据币种价格和波动性设置：
- 查看币种当前价格
- 设置 price_interval 为价格的 0.1%-1%
- 例如：BCH 价格 300 USDT，可以设置 price_interval = 2-3 USDT

### Q2: 如何确定合适的 order_quantity？

**A**: 建议根据您的资金和风险承受能力：
- 小额资金：20-50 USDT
- 中等资金：50-200 USDT
- 大额资金：200-1000 USDT

### Q3: 添加交易对后需要重启吗？

**A**: 
- 如果系统支持热重载，配置会自动生效
- 否则需要重启系统
- 建议先停止交易，添加配置后再启动

### Q4: 可以同时交易多个交易对吗？

**A**: 是的，系统支持多交易对同时交易。只需在 `symbols` 列表中添加多个交易对配置即可。

### Q5: 如何删除交易对？

**A**: 从 `symbols` 列表中删除对应的配置项即可。注意：
- 如果该交易对正在运行，需要先停止交易
- 建议先平仓所有持仓，再删除配置

## 示例配置

### 示例1：币安多交易对配置

```yaml
trading:
  symbols:
    - exchange: "binance"
      symbol: "BTCUSDT"
      price_interval: 10
      order_quantity: 50
      min_order_value: 20
      buy_window_size: 8
      sell_window_size: 8
      reconcile_interval: 60
      order_cleanup_threshold: 50
      cleanup_batch_size: 20
      margin_lock_duration_seconds: 20
      position_safety_check: 100
    - exchange: "binance"
      symbol: "ETHUSDT"
      price_interval: 2
      order_quantity: 30
      min_order_value: 20
      buy_window_size: 10
      sell_window_size: 10
      reconcile_interval: 60
      order_cleanup_threshold: 50
      cleanup_batch_size: 20
      margin_lock_duration_seconds: 20
      position_safety_check: 100
    - exchange: "binance"
      symbol: "BCHUSDT"
      price_interval: 2
      order_quantity: 30
      min_order_value: 20
      buy_window_size: 10
      sell_window_size: 10
      reconcile_interval: 60
      order_cleanup_threshold: 50
      cleanup_batch_size: 20
      margin_lock_duration_seconds: 20
      position_safety_check: 100
```

### 示例2：多交易所多交易对配置

```yaml
trading:
  symbols:
    - exchange: "binance"
      symbol: "BTCUSDT"
      price_interval: 10
      order_quantity: 50
      # ... 其他配置
    - exchange: "bitget"
      symbol: "ETHUSDT"
      price_interval: 2
      order_quantity: 30
      # ... 其他配置
```

## 注意事项

1. **确保交易所已配置**：添加交易对前，确保对应的交易所API已配置
2. **检查交易对是否存在**：确保交易对在交易所中存在且可交易
3. **资金充足**：确保账户有足够的资金支持多个交易对同时交易
4. **风险控制**：合理设置 `position_safety_check` 和 `min_order_value` 来控制风险
5. **监控运行状态**：添加交易对后，注意监控其运行状态和盈亏情况
