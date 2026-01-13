# Goroutine 泄漏修复说明

## 检查结果

经过全面检查，发现并修复了以下潜在的 goroutine 泄漏问题：

## 已修复的问题

### 1. 日志清理协程泄漏

**位置**: `main.go:444`

**问题**: 日志清理协程没有监听 `ctx.Done()`，导致程序退出时无法正确停止

**修复**: 
- 添加 `ctx.Done()` 监听
- 使用 `time.Timer` 替代 `time.Sleep`，以便能够响应 context 取消

```go
// 修复前
go func() {
    ticker := time.NewTicker(24 * time.Hour)
    defer ticker.Stop()
    time.Sleep(initialDelay)  // 无法响应 context 取消
    for {
        select {
        case <-ticker.C:
            // 清理逻辑
        }
    }
}()

// 修复后
go func() {
    ticker := time.NewTicker(24 * time.Hour)
    defer ticker.Stop()
    
    initialTimer := time.NewTimer(initialDelay)
    defer initialTimer.Stop()
    
    select {
    case <-ctx.Done():
        return
    case <-initialTimer.C:
        // 执行清理
    }
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            // 清理逻辑
        }
    }
}()
```

### 2. 价格变化监听协程泄漏

**位置**: `symbol_manager.go:467`

**问题**: 价格变化监听协程只监听 channel 关闭，没有监听 context 取消

**修复**:
- 添加 `ctx.Done()` 监听
- 使用 `select` 同时监听 context 和 channel
- 添加 panic 恢复机制

```go
// 修复前
go func() {
    priceCh := priceMonitor.Subscribe()
    for priceChange := range priceCh {
        // 处理逻辑
    }
}()

// 修复后
go func() {
    defer func() {
        if r := recover(); r != nil {
            logger.Error("❌ [%s] 价格变化处理协程 panic: %v", symCfg.Symbol, r)
        }
    }()
    
    priceCh := priceMonitor.Subscribe()
    for {
        select {
        case <-ctx.Done():
            return
        case priceChange, ok := <-priceCh:
            if !ok {
                return
            }
            // 处理逻辑
        }
    }
}()
```

## 已验证正确的 Goroutine

以下 goroutine 已经正确实现了退出机制：

### 1. 事件处理协程 (`main.go:649`)
- ✅ 使用 worker pool 限制并发
- ✅ 监听 `ctx.Done()`
- ✅ 正确退出

### 2. 状态更新协程 (`main.go:906`)
- ✅ 监听 `ctx.Done()`
- ✅ 使用 ticker 并正确 defer Stop()

### 3. 定期打印持仓协程 (`symbol_manager.go:529`)
- ✅ 监听 `ctx.Done()`
- ✅ 使用 ticker 并正确 defer Stop()

### 4. 事件中心协程 (`event/center.go`)
- ✅ 使用 WaitGroup 管理生命周期
- ✅ 监听 `ctx.Done()`
- ✅ 正确退出

### 5. Watchdog 协程 (`monitor/watchdog.go`)
- ✅ 监听 `ctx.Done()`
- ✅ 使用 ticker 并正确 defer Stop()

### 6. 内存管理器协程 (`monitor/memory_manager.go`)
- ✅ 监听 `ctx.Done()`
- ✅ 使用 ticker 并正确 defer Stop()

### 7. 存储服务协程 (`storage/storage.go`)
- ✅ 监听 `ctx.Done()`
- ✅ 使用 ticker 并正确 defer Stop()

### 8. 日志存储协程 (`storage/log_storage.go`)
- ✅ 监听 `ctx.Done()` 和 channel 关闭
- ✅ 使用 ticker 并正确 defer Stop()

### 9. 对账协程 (`safety/reconciler.go`)
- ✅ 监听 `ctx.Done()`
- ✅ 使用 ticker 并正确 defer Stop()

### 10. 订单清理协程 (`safety/order_cleaner.go`)
- ✅ 监听 `ctx.Done()`
- ✅ 使用 ticker 并正确 defer Stop()

### 11. 组合策略循环 (`strategy/combo_strategy.go`)
- ✅ `marketDetectionLoop()` 监听 `s.ctx.Done()`
- ✅ `rebalanceLoop()` 监听 `s.ctx.Done()`

### 12. WebSocket 协程 (`exchange/*/websocket.go`)
- ✅ 使用 WaitGroup 管理生命周期
- ✅ 监听 `ctx.Done()`
- ✅ 正确关闭连接

### 13. Web 服务器协程 (`web/server_start.go`)
- ✅ 监听 `ctx.Done()`
- ✅ 正确关闭服务器

## 短期 Goroutine（无需退出机制）

以下 goroutine 是短期的，执行完会自动退出，不需要额外的退出机制：

1. **日志写入协程** (`logger/logger.go:417, 462`)
   - 使用 defer recover 保护
   - 执行完自动退出

2. **通知发送协程** (`notify/notifier.go:107`)
   - 使用 WaitGroup 等待完成
   - 执行完自动退出

3. **订阅者通知协程** (`storage/log_storage.go:257`)
   - 执行完自动退出

4. **风控记录保存协程** (`safety/risk_monitor.go:337`)
   - 执行完自动退出

5. **策略启动协程** (`strategy/strategy.go:164`)
   - 执行完自动退出

6. **策略事件处理协程** (`strategy/strategy.go:210, 226`)
   - 执行完自动退出

## 最佳实践

### 1. 长期运行的 Goroutine

所有长期运行的 goroutine 应该：
- ✅ 监听 `ctx.Done()` 或 `context.Context`
- ✅ 使用 `select` 同时监听多个 channel
- ✅ 使用 `defer ticker.Stop()` 清理资源
- ✅ 在退出时清理资源

### 2. 短期 Goroutine

短期 goroutine 应该：
- ✅ 使用 `defer recover()` 保护
- ✅ 确保执行完会自动退出
- ✅ 避免阻塞主流程

### 3. Worker Pool 模式

对于需要限制并发的场景：
- ✅ 使用 buffered channel 作为 worker pool
- ✅ 使用 `defer` 释放 worker 槽位
- ✅ 限制最大并发数量

### 4. Channel 管理

- ✅ 确保 channel 在适当的时候关闭
- ✅ 使用 `select` 监听多个 channel
- ✅ 检查 channel 是否已关闭 (`ok` 值)

## 验证方法

### 1. 运行时检查

```bash
# 查看 goroutine 数量
curl http://localhost:15173/api/system/metrics/current | jq '.goroutines'
```

### 2. 使用 pprof

```go
import _ "net/http/pprof"

// 访问 http://localhost:6060/debug/pprof/goroutine?debug=1
```

### 3. 日志检查

检查日志中是否有：
- "已停止" 相关的日志
- Goroutine 退出的日志
- Context 取消的日志

## 总结

经过全面检查和修复，所有长期运行的 goroutine 都已经正确实现了退出机制。系统应该能够优雅地关闭所有协程，避免 goroutine 泄漏。

如果发现新的 goroutine 泄漏问题，请参考本文档的最佳实践进行修复。
