# ReduceOnly 错误处理说明

## 问题背景

在运行过程中，系统出现了大量 `ReduceOnly Order is rejected` 错误（币安 API 错误码 `-2022`）。这个错误表示：

- 系统尝试下 ReduceOnly（只减仓）订单
- 但实际账户中没有对应的持仓
- 导致订单被交易所拒绝，并持续重试

## 错误原因

1. **本地状态与交易所不同步**：本地槽位管理器认为有持仓，但实际账户已无持仓
2. **持仓已被其他方式平仓**：可能通过手动操作、其他程序、或订单状态更新延迟导致
3. **订单状态回调延迟**：卖单成交的 WebSocket 推送延迟，导致本地状态未及时更新

## 解决方案

### 1. 错误检测（`order/executor_adapter.go`）

添加 `isReduceOnlyError()` 函数，检测 ReduceOnly 错误：

```go
func isReduceOnlyError(err error) bool {
    if err == nil {
        return false
    }
    errStr := err.Error()
    // Binance: code=-2022, msg=ReduceOnly Order is rejected
    return strings.Contains(errStr, "-2022") ||
        strings.Contains(errStr, "ReduceOnly Order is rejected") ||
        strings.Contains(errStr, "reduce only")
}
```

### 2. 停止重试（`order/executor_adapter.go`）

在 `PlaceOrder()` 中，检测到 ReduceOnly 错误时立即返回，不再重试：

```go
} else if isReduceOnlyError(err) {
    // 🔥 ReduceOnly订单被拒绝：无持仓时尝试减仓，不重试
    logger.Warn("⚠️ [%s] ReduceOnly订单被拒绝（无持仓）: %s %.2f",
        oe.exchange.GetName(), req.Side, req.Price)
    return nil, fmt.Errorf("ReduceOnly订单被拒绝（无持仓）: %w", err)
}
```

### 3. 返回详细错误信息（`order/executor_adapter.go`）

新增 `BatchPlaceOrdersResult` 结构体和 `BatchPlaceOrdersWithDetails()` 方法：

```go
type BatchPlaceOrdersResult struct {
    PlacedOrders     []*Order        // 成功下单的订单列表
    HasMarginError   bool            // 是否出现保证金不足错误
    ReduceOnlyErrors map[string]bool // ReduceOnly错误的订单（key为ClientOrderID）
}

func (oe *ExchangeOrderExecutor) BatchPlaceOrdersWithDetails(orders []*OrderRequest) *BatchPlaceOrdersResult
```

### 4. 自动清空槽位（`position/super_position_manager.go`）

在 `AdjustOrders()` 中，检测到 ReduceOnly 错误时，自动清空对应槽位的持仓状态：

```go
// 🔥 处理 ReduceOnly 错误：清空对应槽位的持仓
for clientOID := range result.ReduceOnlyErrors {
    price, side, valid := spm.parseClientOrderID(clientOID)
    if valid {
        if side == "SELL" {
            // SELL ReduceOnly：平多仓失败，清空槽位持仓状态
            slot := spm.getOrCreateSlot(price)
            slot.mu.Lock()
            if slot.PositionStatus == PositionStatusFilled {
                logger.Warn("⚠️ [ReduceOnly错误处理] 清空槽位持仓: 价格=%s, 原持仓=%.4f",
                    formatPrice(price, spm.priceDecimals), slot.PositionQty)
                // 清空持仓状态
                slot.PositionStatus = PositionStatusEmpty
                slot.PositionQty = 0
                slot.SlotStatus = SlotStatusFree
            }
            slot.mu.Unlock()
        } else if side == "BUY" {
            // BUY ReduceOnly：平空仓失败，账户中无空仓（系统不管理空仓状态，仅记录日志）
            logger.Warn("⚠️ [ReduceOnly错误处理] BUY平空仓订单被拒绝: 价格=%s, 账户中无空仓",
                formatPrice(price, spm.priceDecimals))
        }
    }
}
```

### 5. 适配器支持（`main.go`, `strategy/executor_adapter.go`, `strategy/multi_strategy_executor.go`）

在所有适配器中添加 `BatchPlaceOrdersWithDetails()` 方法的实现，确保错误信息能正确传递。

## 效果

1. **停止无效重试**：检测到 ReduceOnly 错误后立即停止，不再持续重试
2. **自动修复状态**：自动清空本地槽位的持仓状态，与交易所实际状态同步
3. **避免资源浪费**：减少无效的 API 调用和日志输出
4. **提高系统稳定性**：防止因状态不同步导致的持续错误

## 日志示例

修改前（持续重试）：
```
2025/12/26 23:44:10 [WARN] ⚠️ [Binance] 下单失败 2927.58 SELL: 下单失败（重试5次）: <APIError> code=-2022, msg=ReduceOnly Order is rejected.
2025/12/26 23:44:13 [WARN] ⚠️ [Binance] 下单失败 2933.58 SELL: 下单失败（重试5次）: <APIError> code=-2022, msg=ReduceOnly Order is rejected.
...（持续重复）
```

修改后（立即处理）：
```
2025/12/26 23:44:10 [WARN] ⚠️ [Binance] ReduceOnly订单被拒绝（无持仓）: SELL 2927.58
2025/12/26 23:44:10 [ERROR] ❌ [ReduceOnly错误] 订单 2927.58 SELL 无持仓，需要清空槽位
2025/12/26 23:44:10 [WARN] ⚠️ [ReduceOnly错误处理] 清空槽位持仓: 价格=2927.58, 原持仓=0.0270

// BUY 方向的 ReduceOnly 错误示例：
2025/12/26 23:45:20 [WARN] ⚠️ [Binance] ReduceOnly订单被拒绝（无持仓）: BUY 3213.62
2025/12/26 23:45:20 [ERROR] ❌ [ReduceOnly错误] 订单 3213.62 BUY 无持仓，需要清空槽位
2025/12/26 23:45:20 [WARN] ⚠️ [ReduceOnly错误处理] BUY平空仓订单被拒绝: 价格=3213.62, 账户中无空仓
```

## 相关文件

- `order/executor_adapter.go`：错误检测和批量下单详细结果
- `position/super_position_manager.go`：槽位状态清空逻辑
- `strategy/multi_strategy_executor.go`：多策略执行器适配
- `strategy/executor_adapter.go`：策略适配器
- `main.go`：主程序适配器

## 注意事项

1. 该修改不影响正常的 ReduceOnly 订单（有持仓时）
2. **SELL 方向的 ReduceOnly 错误**：平多仓失败时，自动清空槽位的持仓状态
3. **BUY 方向的 ReduceOnly 错误**：平空仓失败时，仅记录日志（系统不管理空仓状态）
4. 清空槽位后，该价格位会重新变为可用状态，可以重新下买单
5. 建议定期运行对账功能，确保本地状态与交易所同步

## 测试建议

1. 观察日志中是否还有持续的 ReduceOnly 错误
2. 检查槽位状态是否能正确恢复
3. 验证清空槽位后能否正常下新的买单
4. 运行对账功能，确认本地持仓与交易所一致

