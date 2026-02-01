# 内存优化路线图

## 概述

本文档列出了进一步优化内存使用的潜在方向和具体建议。

## 已完成的优化

✅ 修复切片容量泄漏  
✅ 限制数据库查询返回数量  
✅ 优化 CSV 文件读取（流式读取）  
✅ 限制槽位查询数量  
✅ 修复 Goroutine 泄漏  
✅ 添加内存管理器  

## 待优化的方向

### 1. 字符串拼接优化

**问题**: 频繁使用 `fmt.Sprintf` 进行字符串拼接会产生大量临时字符串对象，增加 GC 压力。

**影响位置**:
- `logger/logger.go`: `logf()` 和 `logln()` 函数中多次使用 `fmt.Sprintf`
- `ai/gemini_client.go`: `buildPrompt()` 函数中大量字符串拼接
- `event/center.go`: 事件消息构建中的字符串拼接
- `web/api.go`: API 响应构建中的字符串拼接

**优化方案**:
```go
// 优化前
prefix := fmt.Sprintf("[%s] ", level.String())
message := fmt.Sprintf(prefix+format, args...)

// 优化后 - 使用 strings.Builder
var builder strings.Builder
builder.WriteString("[")
builder.WriteString(level.String())
builder.WriteString("] ")
builder.WriteString(format)
message := fmt.Sprintf(builder.String(), args...)

// 或者使用 sync.Pool 复用 Builder
var builderPool = sync.Pool{
    New: func() interface{} {
        return &strings.Builder{}
    },
}

builder := builderPool.Get().(*strings.Builder)
defer func() {
    builder.Reset()
    builderPool.Put(builder)
}()
builder.WriteString("[")
// ... 使用 builder
message := builder.String()
```

**预期收益**: 减少 10-30% 的字符串分配，降低 GC 压力

### 2. 其他策略的缓存切片内存泄漏修复

**问题**: 其他策略文件可能也存在类似的切片容量泄漏问题。

**需要检查的策略**:
- `strategy/dca_enhanced.go` - `priceHistory` 和 `candles`
- `strategy/momentum.go` - `priceHistory`
- `strategy/mean_reversion.go` - `priceHistory`
- `strategy/trend_following.go` - `priceHistory`
- `strategy/trend_detector.go` - `priceHistory`
- `strategy/dynamic_adjuster.go` - `priceHistory`
- `strategy/combo_strategy.go` - 子策略的缓存

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

**预期收益**: 防止策略缓存的内存泄漏，长期运行更稳定

### 3. sync.Pool 对象池复用

**问题**: 频繁分配和释放的对象（如 `bytes.Buffer`、`strings.Builder`、`[]byte`）会增加 GC 压力。

**适用场景**:
- HTTP 响应体读取缓冲区
- JSON 序列化/反序列化缓冲区
- 日志消息构建
- 字符串拼接
- 数据库查询结果缓冲区

**实现示例**:
```go
// 创建对象池
var bufferPool = sync.Pool{
    New: func() interface{} {
        return make([]byte, 0, 4096) // 预分配 4KB
    },
}

// 使用对象池
func readResponseBody(resp *http.Response) ([]byte, error) {
    buf := bufferPool.Get().([]byte)
    defer func() {
        buf = buf[:0] // 重置长度，保留容量
        bufferPool.Put(buf)
    }()
    
    _, err := io.CopyBuffer(&bytes.Buffer{}, resp.Body, buf)
    // ... 处理
    return buf, nil
}
```

**预期收益**: 减少 20-40% 的对象分配，显著降低 GC 频率

### 4. Map 预分配容量

**问题**: 如果知道 map 的大致大小，预分配容量可以避免多次扩容。

**优化位置**:
- `position/allocation_manager.go`: `allocations` map
- `monitor/watchdog.go`: 各种缓存 map
- `storage/sqlite.go`: 查询结果 map

**优化示例**:
```go
// 优化前
historyMap := make(map[time.Time]*RiskCheckHistory)

// 优化后 - 如果知道大概数量
historyMap := make(map[time.Time]*RiskCheckHistory, limit)
```

**预期收益**: 减少 map 扩容时的内存分配和复制

### 5. 减少不必要的内存分配

**问题**: 在循环或高频调用的函数中创建临时对象会增加 GC 压力。

**优化方向**:
- 将临时对象提升到函数外部或使用对象池
- 复用 slice 和 map
- 避免在循环中创建闭包（如果可能）

**示例**:
```go
// 优化前 - 每次调用都创建新的 slice
func processItems(items []Item) {
    for _, item := range items {
        result := make([]string, 0) // 每次都分配
        // ... 处理
    }
}

// 优化后 - 复用 slice
var resultPool = sync.Pool{
    New: func() interface{} {
        return make([]string, 0, 100)
    },
}

func processItems(items []Item) {
    result := resultPool.Get().([]string)
    defer func() {
        result = result[:0]
        resultPool.Put(result)
    }()
    // ... 处理
}
```

### 6. JSON 序列化优化

**问题**: 频繁的 JSON 序列化/反序列化会产生大量临时对象。

**优化方案**:
- 使用流式 JSON 编码器（`json.Encoder`）而不是 `json.Marshal`
- 复用 `json.Encoder` 和 `json.Decoder`
- 对于大对象，考虑分块序列化

**示例**:
```go
// 优化前
data, err := json.Marshal(obj)
if err != nil {
    return err
}
w.Write(data)

// 优化后 - 流式编码
encoder := json.NewEncoder(w)
return encoder.Encode(obj)
```

### 7. 数据库连接和查询优化

**问题**: 数据库连接和查询结果可能占用大量内存。

**优化方向**:
- 使用连接池并限制最大连接数
- 使用流式查询（如果数据库支持）
- 分批处理大量数据
- 及时关闭 `rows` 和 `statements`

**已实施**: ✅ 查询 limit 限制  
**待优化**: 连接池监控和优化

### 8. WebSocket 消息缓冲区优化

**问题**: WebSocket 消息缓冲区可能积累大量数据。

**优化方向**:
- 限制消息队列大小
- 使用有界 channel
- 丢弃旧消息（如果允许）

**检查位置**:
- `exchange/*/websocket.go`: 消息处理缓冲区
- `web/websocket.go`: WebSocket hub 的消息队列

### 9. 事件总线优化

**问题**: 事件总线的 channel 缓冲区可能积累大量事件。

**优化方向**:
- 限制事件队列大小
- 使用有界 channel
- 对不重要的事件进行采样或丢弃

**检查位置**:
- `event/center.go`: 事件中心
- `event/event.go`: 事件总线

### 10. 缓存大小限制和清理策略

**问题**: 各种缓存可能无限增长。

**优化方向**:
- 为所有缓存设置最大大小限制
- 实现 LRU 或 FIFO 淘汰策略
- 定期清理过期缓存

**已实施**: ✅ Watchdog 缓存限制  
**待优化**: 其他策略缓存、价格历史缓存等

### 11. 日志优化

**问题**: 日志系统可能产生大量字符串分配。

**优化方向**:
- 使用对象池复用字符串构建器
- 限制日志消息长度
- 对高频日志进行采样

**检查位置**:
- `logger/logger.go`: 日志格式化
- `storage/log_storage.go`: 日志存储

### 12. 配置对象优化

**问题**: 配置对象可能被多次复制。

**优化方向**:
- 使用指针传递配置对象
- 避免不必要的配置复制
- 使用只读配置接口

**检查位置**:
- `config/config.go`: 配置结构
- `symbol_manager.go`: 配置传递

## 优先级建议

### 高优先级（立即实施）
1. ✅ 修复其他策略的缓存切片内存泄漏
2. ✅ 字符串拼接优化（高频调用路径）
3. ✅ sync.Pool 对象池（HTTP 响应、JSON 缓冲区）

### 中优先级（近期实施）
4. Map 预分配容量
5. WebSocket 消息缓冲区限制
6. 事件总线优化
7. 日志优化

### 低优先级（长期优化）
8. 配置对象优化
9. 数据库连接池优化
10. 其他细节优化

## 实施建议

### 1. 渐进式优化
- 先优化高频调用的路径
- 使用性能分析工具（pprof）识别热点
- 逐步实施，每次优化后验证效果

### 2. 监控和验证
- 使用内存监控工具跟踪优化效果
- 对比优化前后的内存使用情况
- 确保优化不影响功能正确性

### 3. 测试策略
- 压力测试验证内存稳定性
- 长期运行测试验证内存泄漏
- 性能基准测试验证优化效果

## 工具和资源

### 性能分析工具
- `go tool pprof`: CPU 和内存分析
- `runtime.MemStats`: 内存统计
- `go test -bench`: 基准测试

### 监控工具
- 系统监控 API: `/api/system/metrics/current`
- 内存管理器: `monitor/memory_manager.go`
- 日志监控: 检查内存相关警告

## 总结

通过系统性的内存优化，可以：
- 减少内存占用 20-40%
- 降低 GC 频率 30-50%
- 提高系统长期运行的稳定性
- 改善系统响应性能

建议按照优先级逐步实施，每次优化后进行验证和监控。
