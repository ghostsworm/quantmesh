# 分级资金限额功能说明

## 功能概述

分级资金限额是一个智能资金管理功能，可以在市场大幅下跌时自动放宽资金限额，让系统能够继续抄底建仓，而在市场恢复后自动收紧限额，控制风险。

## 核心特性

### 1. 两级限额模式

- **正常限额**：日常交易使用的资金限额（如 5000 USDT）
- **紧急限额**：市场大跌时自动放宽的限额（如 8000 USDT）

### 2. 智能触发机制

系统会实时监控市场情况，满足以下**任一条件**即自动切换到紧急限额：

- **价格下跌**：价格相对于锚点价格（启动时价格）下跌超过阈值（如 10%）
- **持仓层数**：持仓层数达到阈值（如 20 层，说明已经跌了很多层）
- **未实现亏损**：未实现亏损超过阈值（如 500 USDT）

### 3. 自动恢复机制

当市场恢复时，系统会自动恢复到正常限额，需要满足**所有条件**：

- **价格恢复**：价格恢复到下跌阈值以内（如下跌 5% 以内）
- **冷却时间**：距离触发紧急限额已经过了冷却时间（如 5 分钟）

### 4. 智能通知

- 触发紧急限额时发送通知（可配置）
- 恢复正常限额时发送通知（可配置）
- 支持所有通知渠道（钉钉、微信、Telegram、邮件等）

## 配置说明

### 基础配置

```yaml
position_allocation:
  enabled: true
  allocations:
    - exchange: binance
      symbol: BTCUSDT
      max_amount_usdt: 5000  # 正常限额
      max_percentage: 100
```

### 分级限额配置

```yaml
position_allocation:
  enabled: true
  allocations:
    - exchange: binance
      symbol: BTCUSDT
      max_amount_usdt: 5000  # 正常限额
      max_percentage: 100
      
      # 分级限额配置
      tiered_limits:
        enabled: true
        emergency_limit: 8000  # 紧急限额（大跌时自动提升到此值）
        
        triggers:  # 触发条件（满足任一条件即触发）
          price_drop_percent: 10   # 价格下跌10%触发
          position_layers: 20      # 持仓达到20层触发
          unrealized_loss_usd: 500 # 未实现亏损超过500 USDT触发
        
        recovery:  # 恢复条件（满足所有条件才恢复）
          price_recover_percent: 5  # 价格恢复到下跌5%以内
          cooldown_seconds: 300     # 冷却时间5分钟
        
        notification:  # 通知配置
          on_trigger: true   # 触发紧急限额时通知
          on_recovery: true  # 恢复正常限额时通知
```

## 配置参数详解

### tiered_limits.enabled
- 类型：`bool`
- 说明：是否启用分级限额功能
- 默认：`false`

### tiered_limits.emergency_limit
- 类型：`float64`
- 说明：紧急限额（USDT）
- 建议：设置为正常限额的 1.5-2 倍
- 示例：正常限额 5000，紧急限额 8000

### triggers.price_drop_percent
- 类型：`float64`
- 说明：价格下跌百分比触发阈值
- 计算方式：`(锚点价格 - 当前价格) / 锚点价格 * 100`
- 建议值：
  - 保守：5-8%
  - 中等：10-15%
  - 激进：20%+
- 示例：设置为 10，表示价格下跌 10% 时触发

### triggers.position_layers
- 类型：`int`
- 说明：持仓层数达到此值时触发
- 建议值：
  - 价格间隔 150 USDT：15-20 层
  - 价格间隔 100 USDT：20-30 层
  - 价格间隔 50 USDT：30-50 层
- 示例：设置为 20，表示持仓达到 20 层时触发

### triggers.unrealized_loss_usd
- 类型：`float64`
- 说明：未实现亏损超过此值（USDT）时触发
- 建议值：正常限额的 10-20%
- 示例：正常限额 5000，设置为 500（10%）

### recovery.price_recover_percent
- 类型：`float64`
- 说明：价格恢复到下跌此百分比以内时才能恢复正常限额
- 建议值：
  - 保守：2-3%
  - 中等：5%
  - 激进：8-10%
- 示例：设置为 5，表示价格恢复到下跌 5% 以内才恢复

### recovery.cooldown_seconds
- 类型：`int`
- 说明：冷却时间（秒），防止频繁切换
- 建议值：
  - 短期：180-300 秒（3-5 分钟）
  - 中期：600-900 秒（10-15 分钟）
  - 长期：1800+ 秒（30 分钟+）
- 示例：设置为 300（5 分钟）

### notification.on_trigger
- 类型：`bool`
- 说明：触发紧急限额时是否发送通知
- 建议：`true`（重要事件，建议通知）

### notification.on_recovery
- 类型：`bool`
- 说明：恢复正常限额时是否发送通知
- 建议：`true`（便于追踪限额变更）

## 使用场景

### 场景一：BTC 大跌 10%

**初始状态**：
- 锚点价格：90,000 USDT
- 正常限额：5000 USDT
- 已用资金：2920 USDT

**价格跌到 81,000 USDT**（下跌 10%）：
1. 系统检测到价格下跌 10%，触发紧急限额
2. 限额自动提升：5000 → 8000 USDT
3. 发送通知：🚨 资金限额已提升（紧急模式）
4. 系统继续下单，可用资金增加 3000 USDT

**价格恢复到 85,500 USDT**（下跌 5%）：
1. 系统检测到价格恢复到下跌 5% 以内
2. 等待冷却时间（5 分钟）
3. 限额自动恢复：8000 → 5000 USDT
4. 发送通知：✅ 资金限额已恢复（正常模式）

### 场景二：持续下跌，持仓达到 20 层

**初始状态**：
- 持仓层数：15 层
- 正常限额：5000 USDT
- 已用资金：4200 USDT

**持仓增加到 20 层**：
1. 系统检测到持仓层数达到 20 层，触发紧急限额
2. 限额自动提升：5000 → 8000 USDT
3. 发送通知：🚨 资金限额已提升（紧急模式）
4. 系统继续下单，可用资金增加 3000 USDT

### 场景三：未实现亏损超过 500 USDT

**初始状态**：
- 未实现亏损：-450 USDT
- 正常限额：5000 USDT

**亏损增加到 -520 USDT**：
1. 系统检测到未实现亏损超过 500 USDT，触发紧急限额
2. 限额自动提升：5000 → 8000 USDT
3. 发送通知：🚨 资金限额已提升（紧急模式）

## 通知示例

### 触发紧急限额通知

```
🚨 资金限额已提升（紧急模式）

交易所: binance
交易对: BTCUSDT
原限额: 5000.00 USDT
新限额: 8000.00 USDT
触发原因: 价格下跌 10.50% (触发阈值: 10.00%)

当前状态:
- 价格下跌: 10.50%
- 持仓层数: 18
- 未实现盈亏: -450.00 USDT

时间: 2026-01-22 11:30:00
```

### 恢复正常限额通知

```
✅ 资金限额已恢复（正常模式）

交易所: binance
交易对: BTCUSDT
原限额: 8000.00 USDT
新限额: 5000.00 USDT
恢复原因: 价格已恢复，当前下跌 4.50% (恢复阈值: 5.00%)

当前状态:
- 价格下跌: 4.50%
- 持仓层数: 12
- 未实现盈亏: -200.00 USDT

时间: 2026-01-22 12:15:00
```

## 最佳实践

### 1. 保守配置（低风险）

```yaml
tiered_limits:
  enabled: true
  emergency_limit: 6000  # 仅提升 20%
  triggers:
    price_drop_percent: 15   # 价格下跌 15% 才触发
    position_layers: 25      # 持仓 25 层才触发
    unrealized_loss_usd: 1000 # 亏损 1000 USDT 才触发
  recovery:
    price_recover_percent: 3  # 价格恢复到下跌 3% 以内
    cooldown_seconds: 900     # 冷却 15 分钟
```

### 2. 中等配置（平衡风险收益）

```yaml
tiered_limits:
  enabled: true
  emergency_limit: 8000  # 提升 60%
  triggers:
    price_drop_percent: 10   # 价格下跌 10% 触发
    position_layers: 20      # 持仓 20 层触发
    unrealized_loss_usd: 500 # 亏损 500 USDT 触发
  recovery:
    price_recover_percent: 5  # 价格恢复到下跌 5% 以内
    cooldown_seconds: 300     # 冷却 5 分钟
```

### 3. 激进配置（高风险高收益）

```yaml
tiered_limits:
  enabled: true
  emergency_limit: 10000  # 提升 100%
  triggers:
    price_drop_percent: 5    # 价格下跌 5% 即触发
    position_layers: 15      # 持仓 15 层触发
    unrealized_loss_usd: 300 # 亏损 300 USDT 触发
  recovery:
    price_recover_percent: 8  # 价格恢复到下跌 8% 以内
    cooldown_seconds: 600     # 冷却 10 分钟
```

## 风险提示

1. **紧急限额不宜过高**：建议不超过正常限额的 2 倍
2. **触发条件不宜过松**：避免频繁触发
3. **冷却时间要合理**：避免频繁切换，建议至少 5 分钟
4. **始终设置最大限额**：不要超过账户余额的 80%
5. **定期检查通知**：确保及时了解限额变更情况

## 监控和调试

### 查看当前限额状态

通过 Web UI 的"资金管理"页面可以查看：
- 当前限额模式（正常/紧急）
- 正常限额和紧急限额
- 已用资金和可用资金
- 使用百分比

### 查看日志

```bash
# 查看限额变更日志
journalctl -u quantmesh | grep "资金限额"

# 查看触发紧急限额的日志
journalctl -u quantmesh | grep "触发紧急限额"

# 查看恢复正常限额的日志
journalctl -u quantmesh | grep "恢复正常限额"
```

### 手动调整限额

如果需要手动调整限额，可以：
1. 修改 `config.yaml` 中的配置
2. 重启服务：`systemctl restart quantmesh`

## 常见问题

### Q1: 为什么触发了紧急限额但没有收到通知？

A: 检查以下配置：
1. `tiered_limits.notification.on_trigger` 是否设置为 `true`
2. 通知渠道是否正确配置（钉钉/微信/Telegram等）
3. 事件中心是否启用

### Q2: 紧急限额触发后多久会恢复？

A: 需要同时满足两个条件：
1. 价格恢复到恢复阈值以内
2. 距离触发时间已经过了冷却时间

### Q3: 可以设置多级限额吗（如 3 级、4 级）？

A: 当前版本只支持两级限额（正常/紧急）。如果需要更多级别，可以联系开发团队扩展功能。

### Q4: 如何禁用分级限额功能？

A: 设置 `tiered_limits.enabled: false` 即可，系统将始终使用正常限额。

### Q5: 紧急限额会影响已有持仓吗？

A: 不会。分级限额只影响新开仓的资金分配，不会影响已有持仓。

## 技术实现

### 关键代码

1. **配置结构**：`config/config.go` - `SymbolAllocation.TieredLimits`
2. **限额管理**：`position/allocation_manager.go` - `CheckAndAdjustLimit()`
3. **触发检查**：`position/super_position_manager.go` - `AdjustOrders()`
4. **事件通知**：`event/event.go` - `EventTypeAllocationLimitChanged`
5. **通知处理**：`notify/*.go` - 各通知渠道的事件处理

### 工作流程

```
1. AdjustOrders 调用时
   ↓
2. 计算当前市场状态（价格、持仓层数、未实现盈亏）
   ↓
3. 调用 CheckAndAdjustLimit 检查是否需要切换限额
   ↓
4. 如果满足触发条件 → 切换到紧急限额 → 发送通知
   ↓
5. 如果满足恢复条件 → 恢复正常限额 → 发送通知
   ↓
6. 使用当前限额进行资金分配检查
```

## 更新日志

### v1.0.0 (2026-01-22)
- ✅ 实现两级限额（正常/紧急）
- ✅ 支持多条件触发（价格/层数/亏损）
- ✅ 自动恢复机制
- ✅ 智能通知（所有通知渠道）
- ✅ 配置文件支持

## 相关文档

- [资金分配管理](./CAPITAL_ALLOCATION.md)
- [风险控制配置](./RISK_CONTROL.md)
- [通知配置](./NOTIFICATION_CONFIG.md)

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
