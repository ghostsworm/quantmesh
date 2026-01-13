# 内存泄漏修复说明

## 问题描述

系统监控显示内存使用量会不断增长，可能存在内存泄漏问题。

## 修复内容

### 1. 修复 Watchdog 历史缓存的内存泄漏

**问题**：`monitor/watchdog.go` 中的 `updateHistoryCache` 方法使用切片截取来限制缓存大小，但这种方式不会释放底层数组的容量，导致内存泄漏。

**修复**：使用 `copy` 创建新的切片，确保旧数组的内存被释放。

```go
// 修复前
w.historyCache = w.historyCache[len(w.historyCache)-w.maxHistory:]

// 修复后
newCache := make([]*SystemMetrics, w.maxHistory)
copy(newCache, w.historyCache[start:])
w.historyCache = newCache
```

### 2. 为存储服务缓冲区添加大小限制

**问题**：存储服务的缓冲区可能无限增长，如果刷新失败或延迟，会导致内存占用不断增长。

**修复**：添加缓冲区大小限制（最多保留10倍批量大小），超过限制时强制刷新。

```go
maxBufferSize := ss.cfg.Storage.BatchSize * 10
if len(ss.buffer) >= maxBufferSize {
    logger.Warn("⚠️ 存储缓冲区过大 (%d)，强制刷新", len(ss.buffer))
    ss.flush()
}
```

### 3. 优化数据库查询，限制返回的数据量

**问题**：查询系统监控数据时，如果时间范围过大，可能返回大量数据，导致内存占用过高。

**修复**：
- 限制查询时间范围：最多查询7天的细粒度数据
- 限制返回数据点数量：最多返回1万条数据，超过时进行采样
- 限制每日汇总查询：最多查询365天

```go
// 限制查询时间范围
maxDuration := 7 * 24 * time.Hour
if actualDuration > maxDuration {
    startTime = endTime.Add(-maxDuration)
}

// 限制返回数据点数量
maxDataPoints := 10000
if len(storageMetrics) > maxDataPoints {
    // 采样处理
}
```

## 配置建议

确保 `config.yaml` 中的清理配置正确：

```yaml
watchdog:
  enabled: true
  sampling:
    interval: 60  # 采样间隔（秒）
  retention:
    detail_days: 7   # 细粒度数据保留7天
    daily_days: 90  # 每日汇总保留90天
  cleanup_interval: 1  # 清理间隔（小时）
```

## 监控建议

1. **定期检查内存使用趋势**：在系统监控页面查看内存使用趋势图
2. **设置内存告警阈值**：在配置中设置内存告警阈值，超过时发送通知
3. **定期重启服务**：如果内存持续增长，可以考虑定期重启服务（例如每天凌晨）

## 验证修复

修复后，内存使用应该：
- 不再持续增长
- 在清理周期后下降
- 保持在合理范围内（根据实际负载）

如果内存仍然持续增长，可能需要进一步检查：
- Goroutine 泄漏
- Channel 未关闭
- 数据库连接未释放
- 其他缓存未清理
