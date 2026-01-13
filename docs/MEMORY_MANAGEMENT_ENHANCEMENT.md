# 内存管理强化方案

## 概述

本文档描述了为支持长期稳定运行而实施的内存管理强化措施。

## 已实施的强化措施

### 1. 内存管理器（MemoryManager）

**位置**: `monitor/memory_manager.go`

**功能**:
- 定期触发 GC（每5分钟）
- 监控内存使用情况
- 检测内存泄漏迹象（持续增长）
- 监控 Goroutine 数量
- 提供内存统计信息

**使用**:
```go
memoryManager := monitor.NewMemoryManager(cfg, ctx)
memoryManager.Start()
```

### 2. 修复 Goroutine 泄漏

**问题**: 事件处理中使用 `go func()` 但没有限制并发数量

**修复**: 使用 worker pool 模式，限制最多10个并发 worker

**位置**: `main.go` (事件处理循环)

### 3. 修复缓存切片内存泄漏

**问题**: 使用切片截取 `slice[start:]` 不会释放底层数组容量

**修复**: 使用 `copy` 创建新切片

**已修复的位置**:
- `monitor/watchdog.go` - 历史缓存
- `safety/risk_monitor.go` - K线缓存
- `strategy/martingale.go` - 价格历史和K线缓存
- `storage/storage.go` - 存储缓冲区

### 4. 存储服务缓冲区限制

**位置**: `storage/storage.go`

**改进**: 
- 添加缓冲区大小限制（最多10倍批量大小）
- 超过限制时强制刷新

### 5. 数据库查询优化

**位置**: `web/system_metrics_provider.go`

**改进**:
- 限制查询时间范围（最多7天）
- 限制返回数据点数量（最多1万条）
- 超过限制时进行采样

### 6. 订阅者列表管理

**位置**: `storage/log_storage.go`

**改进**:
- 限制订阅者数量（最多100个）
- 超过限制时移除最旧的订阅者（FIFO）

### 7. 槽位清理机制

**位置**: `position/super_position_manager.go`

**新增方法**: `CleanupEmptySlots()`

**功能**: 清理空槽位（空仓、无订单、无订单历史）

**建议**: 定期调用（例如每小时一次）

## 待实施的改进

### 1. 定期清理槽位

在 `main.go` 中添加定期清理槽位的协程：

```go
// 定期清理空槽位（每小时）
go func() {
    ticker := time.NewTicker(1 * time.Hour)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            for _, rt := range symbolManager.List() {
                if rt.SuperPositionManager != nil {
                    deleted := rt.SuperPositionManager.CleanupEmptySlots()
                    if deleted > 0 {
                        logger.Debug("🧹 [%s:%s] 清理了 %d 个空槽位", 
                            rt.Config.Exchange, rt.Config.Symbol, deleted)
                    }
                }
            }
        }
    }
}()
```

### 2. 修复其他策略的缓存问题

以下策略也需要修复缓存切片的内存泄漏：

- `strategy/dca_enhanced.go`
- `strategy/combo_strategy.go`
- `strategy/momentum.go`
- `strategy/mean_reversion.go`
- `strategy/trend_following.go`
- `strategy/trend_detector.go`
- `strategy/dynamic_adjuster.go`

**修复模式**:
```go
// 修复前
if len(s.priceHistory) > maxHistory {
    s.priceHistory = s.priceHistory[len(s.priceHistory)-maxHistory:]
}

// 修复后
if len(s.priceHistory) > maxHistory {
    newHistory := make([]float64, maxHistory)
    copy(newHistory, s.priceHistory[len(s.priceHistory)-maxHistory:])
    s.priceHistory = newHistory
}
```

### 3. 添加内存使用告警

在 `monitor/memory_manager.go` 中增强告警功能：

- 内存使用超过阈值时发送通知
- Goroutine 数量超过阈值时发送通知
- 内存持续增长时发送通知

### 4. 数据库连接池监控

添加数据库连接池状态监控：

- 监控连接池使用情况
- 检测连接泄漏
- 定期检查连接健康状态

### 5. WebSocket 连接管理

确保 WebSocket 连接正确关闭：

- 检查所有 WebSocket 管理器都有 `Stop()` 方法
- 确保 `Stop()` 方法正确等待所有 goroutine 退出
- 添加连接超时检测

## 配置建议

### 内存管理配置

在 `config.yaml` 中添加内存管理配置（可选）：

```yaml
memory:
  gc_interval: 5m          # GC 间隔（默认5分钟）
  cleanup_interval: 30m    # 清理间隔（默认30分钟）
  high_memory_threshold: 500  # 高内存阈值（MB）
  high_goroutine_threshold: 100  # 高 Goroutine 阈值
```

### 槽位清理配置

```yaml
trading:
  slot_cleanup_interval: 3600  # 槽位清理间隔（秒，默认1小时）
```

## 监控指标

内存管理器提供以下监控指标：

- `alloc_mb`: 当前分配的内存（MB）
- `sys_mb`: 系统内存（MB）
- `num_gc`: GC 次数
- `goroutines`: Goroutine 数量
- `heap_alloc_mb`: 堆分配内存（MB）
- `heap_sys_mb`: 堆系统内存（MB）
- `heap_idle_mb`: 堆空闲内存（MB）
- `heap_inuse_mb`: 堆使用内存（MB）
- `next_gc_mb`: 下次 GC 阈值（MB）
- `gc_cpu_fraction`: GC CPU 占用比例

## 最佳实践

1. **定期监控内存使用**
   - 在系统监控页面查看内存趋势
   - 设置内存告警阈值

2. **定期清理**
   - 确保数据保留策略配置合理
   - 定期清理过期数据

3. **避免内存泄漏**
   - 使用 `copy` 而不是切片截取
   - 确保所有 goroutine 都能正确退出
   - 限制缓存和缓冲区大小

4. **监控 Goroutine 数量**
   - 正常情况下应该稳定在某个范围内
   - 如果持续增长，可能存在 goroutine 泄漏

5. **定期重启**
   - 虽然已经做了很多优化，但定期重启（例如每周）仍然是一个好习惯
   - 可以清理一些难以检测的内存碎片

## 验证

修复后，应该观察到：

1. ✅ 内存使用不再持续增长
2. ✅ Goroutine 数量稳定
3. ✅ GC 正常触发
4. ✅ 空槽位被定期清理
5. ✅ 缓存大小受控

## 故障排查

如果内存仍然持续增长：

1. 检查是否有 goroutine 泄漏
   ```bash
   # 在运行时查看 goroutine 数量
   curl http://localhost:15173/api/system/metrics/current
   ```

2. 检查是否有缓存未清理
   - 查看日志中的清理记录
   - 检查数据库大小

3. 使用 pprof 分析内存
   ```go
   import _ "net/http/pprof"
   ```

4. 检查是否有大对象未释放
   - 查看堆内存使用情况
   - 检查是否有大切片或 map

## 总结

通过以上强化措施，系统应该能够长期稳定运行，内存使用保持在合理范围内。如果发现问题，请参考故障排查部分。
