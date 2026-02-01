# 其他内存和性能优化点

## 一、编译验证

✅ **编译通过**: 项目可以正常编译，只有一些第三方库的警告（不影响功能）

## 二、发现的优化点

### 1. time.Sleep 优化（可优化但影响较小）

**位置**: `safety/reconciler.go:139`

**当前实现**:
```go
r.reconcileMu.Unlock()
logger.Debug("⏳ [对账] 等待 %v 后执行（最小间隔限制）", waitTime)
time.Sleep(waitTime)  // 阻塞，无法响应 context 取消
r.reconcileMu.Lock()
```

**优化建议**: 使用 `time.Timer` + `context`，可以响应取消

**影响**: 较小，因为对账操作本身不频繁，且 waitTime 通常很短（几秒）

**优先级**: 低（可以优化，但不是关键问题）

### 2. 退出时的 time.Sleep 优化

**位置**: `main.go:1154, 1163`

**当前实现**:
```go
// 等待一小段时间，让事件处理协程完成清理
time.Sleep(500 * time.Millisecond)

// 再等待一小段时间，让存储服务完成最后的写入
time.Sleep(200 * time.Millisecond)
```

**优化建议**: 使用 `sync.WaitGroup` 或 `context.WithTimeout` 等待协程完成

**实现方式**:
```go
// 使用 WaitGroup 等待事件处理完成
var eventWg sync.WaitGroup
eventWg.Add(1)
go func() {
    defer eventWg.Done()
    // 事件处理逻辑
}()

// 退出时等待
done := make(chan struct{})
go func() {
    eventWg.Wait()
    close(done)
}()

select {
case <-done:
    // 完成
case <-time.After(2 * time.Second):
    logger.Warn("⚠️ 等待事件处理超时")
}
```

**优先级**: 中（可以改进，但不是关键问题）

### 3. Map 遍历优化

**当前状态**: 使用 `sync.Map.Range()` 遍历，这是高效的

**优化建议**: 如果知道 map 的大致大小，可以考虑预分配 slice 容量

**示例**:
```go
// 优化前
var slots []DetailedSlotData
spm.slots.Range(func(key, value interface{}) bool {
    slots = append(slots, ...)
    return true
})

// 优化后 - 如果知道大概数量
estimatedCount := spm.GetSlotCount()
slots := make([]DetailedSlotData, 0, estimatedCount)
spm.slots.Range(func(key, value interface{}) bool {
    slots = append(slots, ...)
    return true
})
```

**优先级**: 低（影响较小）

### 4. 字符串格式化优化

**位置**: 高频调用的格式化操作

**优化建议**: 对于简单的字符串拼接，使用 `strings.Builder` 而不是 `fmt.Sprintf`

**示例**:
```go
// 优化前
key := fmt.Sprintf("%s:%s", exchange, symbol)

// 优化后 - 对于简单拼接
var builder strings.Builder
builder.WriteString(exchange)
builder.WriteString(":")
builder.WriteString(symbol)
key := builder.String()

// 或者使用 strings.Join（更简洁）
key := strings.Join([]string{exchange, symbol}, ":")
```

**优先级**: 低（影响较小，fmt.Sprintf 对于简单情况已经足够快）

### 5. 减少不必要的内存分配

#### 5.1 复用 slice（已部分实施）

**优化建议**: 对于频繁使用的临时 slice，使用 `[:0]` 重置而不是重新分配

**示例**:
```go
// 优化前
buffer := make([]*logEntry, 0, 100)
// ... 使用后
buffer = make([]*logEntry, 0, 100)  // 重新分配

// 优化后
buffer := make([]*logEntry, 0, 100)
// ... 使用后
buffer = buffer[:0]  // 重置长度，保留容量
```

**当前状态**: ✅ 已在 `storage/log_storage.go` 中使用 `buffer[:0]`

#### 5.2 避免不必要的类型转换

**优化建议**: 减少 `interface{}` 类型断言，使用泛型（Go 1.18+）或具体类型

**优先级**: 低（需要重构，影响较大）

### 6. 数据库连接池优化

**当前状态**: 已设置连接池参数

**优化建议**: 
- 监控连接池使用情况
- 根据实际负载调整 `MaxOpenConns` 和 `MaxIdleConns`
- 定期检查连接泄漏

**优先级**: 中（可以添加监控）

### 7. WebSocket 缓冲区优化

**当前状态**: 各交易所 WebSocket 实现不同

**优化建议**: 
- 统一缓冲区大小配置
- 监控消息队列长度
- 添加背压机制（当队列满时丢弃旧消息）

**优先级**: 中（可以统一配置）

### 8. 事件总线优化

**当前状态**: 缓冲区 1000，满了丢弃事件

**优化建议**:
- 根据实际使用情况调整缓冲区大小
- 添加事件丢弃统计
- 考虑优先级队列（重要事件优先处理）

**优先级**: 低（当前实现已经足够好）

### 9. 锁优化

**当前状态**: 使用 `sync.RWMutex` 和 `sync.Map`

**优化建议**:
- 检查是否有锁竞争热点
- 考虑使用更细粒度的锁
- 使用 `atomic` 操作替代锁（如果适用）

**优先级**: 低（当前锁使用已经比较合理）

### 10. Context 传递优化

**当前状态**: 大部分协程都使用 context

**优化建议**: 
- 确保所有长期运行的协程都接收 context
- 使用 `context.WithTimeout` 设置超时
- 避免创建新的 `context.Background()`（应该传递父 context）

**优先级**: 中（可以提高代码质量）

## 三、性能分析工具使用

### 1. 使用 pprof 分析内存和 CPU

```bash
# 启动时添加 pprof
go run main.go -pprof=:6060

# 分析内存
go tool pprof http://localhost:6060/debug/pprof/heap

# 分析 CPU
go tool pprof http://localhost:6060/debug/pprof/profile

# 分析 Goroutine
go tool pprof http://localhost:6060/debug/pprof/goroutine
```

### 2. 使用 trace 分析运行时

```bash
# 生成 trace
curl http://localhost:6060/debug/pprof/trace?seconds=5 > trace.out

# 查看 trace
go tool trace trace.out
```

### 3. 内存统计 API

```bash
# 查看当前内存使用
curl http://localhost:15173/api/system/metrics/current | jq '.memory'

# 查看 GC 统计
curl http://localhost:15173/api/system/metrics/current | jq '.gc'
```

## 四、监控指标建议

### 1. 内存指标
- 当前分配内存（Alloc）
- 系统内存（Sys）
- GC 次数和频率
- GC 停顿时间
- Goroutine 数量

### 2. Channel 指标
- 各 channel 的缓冲区使用率
- Channel 满的次数（丢弃事件数）
- Channel 等待时间

### 3. 锁指标
- 锁竞争次数
- 锁等待时间
- 死锁检测

### 4. 数据库指标
- 连接池使用率
- 查询耗时
- 连接泄漏检测

## 五、实施建议

### 高优先级（建议实施）
1. ✅ 添加 GC 和内存监控指标到 API
2. ✅ 优化退出流程（使用 WaitGroup 替代 time.Sleep）
3. ✅ 添加 Channel 使用率监控

### 中优先级（可选实施）
4. 优化 time.Sleep（使用 context + timer）
5. 添加数据库连接池监控
6. 统一 WebSocket 缓冲区配置

### 低优先级（长期优化）
7. Map 遍历预分配
8. 字符串格式化优化
9. 锁优化（需要性能分析后决定）

## 六、总结

当前系统已经实施了大部分关键的内存和性能优化：

✅ **已完成**:
- 修复切片容量泄漏
- 限制数据库查询返回数量
- 优化字符串拼接（Builder + Pool）
- 添加 GC 配置支持
- 智能 GC 触发
- 日志系统优化

🔍 **可进一步优化**:
- 退出流程优化（使用 WaitGroup）
- 添加更多监控指标
- 根据实际运行数据调整参数

💡 **建议**:
1. 先运行系统一段时间，收集实际性能数据
2. 使用 pprof 分析热点
3. 根据分析结果有针对性地优化
4. 持续监控内存和 GC 指标

当前系统的内存管理已经相当完善，可以支持长期稳定运行。进一步的优化应该基于实际运行数据和性能分析结果。
