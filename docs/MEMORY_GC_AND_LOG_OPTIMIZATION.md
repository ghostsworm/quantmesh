# 内存、GC 和日志优化建议

## 一、GC 优化方向

### 1. 调整 GOGC 参数

**当前状态**: 使用默认 GOGC=100（当堆内存增长到原来的2倍时触发GC）

**优化建议**:
- 对于内存敏感的应用，可以设置 `GOGC=50` 或 `GOGC=75`，更频繁地触发 GC
- 对于性能优先的应用，可以设置 `GOGC=200`，减少 GC 频率

**实现方式**:
```go
// 在 main.go 启动时设置
import "runtime"

func init() {
    // 从环境变量读取，如果没有则使用默认值
    if goGC := os.Getenv("GOGC"); goGC != "" {
        if val, err := strconv.Atoi(goGC); err == nil && val > 0 {
            runtime.SetGCPercent(val)
            logger.Info("✅ GOGC 设置为: %d", val)
        }
    } else {
        // 默认设置为 100（标准值）
        runtime.SetGCPercent(100)
    }
}
```

**或者通过环境变量**:
```bash
export GOGC=75  # 更频繁的 GC
./quantmesh
```

**预期收益**: 
- GOGC=50: 内存使用降低 20-30%，但 GC 频率增加，CPU 使用略增
- GOGC=200: GC 频率降低 50%，但内存使用增加

### 2. 设置内存限制（Go 1.19+）

**当前状态**: 没有设置内存限制

**优化建议**: 使用 `GOMEMLIMIT` 或 `runtime/debug.SetMemoryLimit`

**实现方式**:
```go
import "runtime/debug"

func init() {
    // 从环境变量读取，例如 "512MiB" 或 "1GiB"
    if memLimit := os.Getenv("GOMEMLIMIT"); memLimit != "" {
        // 解析内存限制（需要自己实现解析函数）
        limit := parseMemoryLimit(memLimit)
        debug.SetMemoryLimit(limit)
        logger.Info("✅ 内存限制设置为: %s", memLimit)
    } else {
        // 默认不设置限制，让 Go 自动管理
    }
}
```

**或者通过环境变量**:
```bash
export GOMEMLIMIT=512MiB
./quantmesh
```

**预期收益**: 防止内存无限增长，系统更稳定

### 3. 优化 GC 触发时机

**当前状态**: MemoryManager 每5分钟强制触发一次 GC

**优化建议**: 根据内存使用情况动态调整 GC 频率

**实现方式**:
```go
// 在 monitor/memory_manager.go 中优化
func (mm *MemoryManager) gcLoop() {
    ticker := time.NewTicker(mm.gcInterval)
    defer ticker.Stop()
    
    mm.forceGC()
    
    for {
        select {
        case <-mm.ctx.Done():
            return
        case <-ticker.C:
            var m runtime.MemStats
            runtime.ReadMemStats(&m)
            
            // 如果内存使用超过阈值，立即触发 GC
            allocMB := float64(m.Alloc) / 1024 / 1024
            if allocMB > 300 { // 300MB 阈值
                mm.forceGC()
            } else {
                // 否则按正常间隔触发
                mm.forceGC()
            }
        }
    }
}
```

### 4. 监控 GC 性能指标

**当前状态**: 只记录基本的 GC 信息

**优化建议**: 详细监控 GC 停顿时间、频率、CPU 占用等

**实现方式**:
```go
// 在 monitor/memory_manager.go 中添加
func (mm *MemoryManager) getGCStats() map[string]interface{} {
    var m runtime.MemStats
    runtime.ReadMemStats(&m)
    
    // 计算平均 GC 停顿时间
    var totalPause time.Duration
    pauseCount := 0
    for i := 0; i < 256 && i < int(m.NumGC); i++ {
        idx := (m.NumGC + uint64(255-i)) % 256
        if m.PauseNs[idx] > 0 {
            totalPause += time.Duration(m.PauseNs[idx])
            pauseCount++
        }
    }
    
    avgPause := time.Duration(0)
    if pauseCount > 0 {
        avgPause = totalPause / time.Duration(pauseCount)
    }
    
    return map[string]interface{}{
        "num_gc":           m.NumGC,
        "gc_cpu_fraction":  m.GCCPUFraction,
        "avg_pause_ns":     avgPause.Nanoseconds(),
        "last_pause_ns":    m.PauseNs[(m.NumGC+255)%256],
        "next_gc":          m.NextGC,
        "heap_alloc_mb":    float64(m.Alloc) / 1024 / 1024,
        "heap_sys_mb":      float64(m.Sys) / 1024 / 1024,
    }
}
```

## 二、内存使用优化方向

### 1. Channel 缓冲区优化

**当前状态**:
- 事件总线: 1000
- 日志存储: 1000
- 价格变化: 10

**优化建议**: 根据实际使用情况调整缓冲区大小

**实现方式**:
```go
// 在 config.yaml 中添加配置
memory:
  channel_buffers:
    event_bus: 1000      # 事件总线缓冲区
    log_storage: 500     # 日志存储缓冲区（可以减少）
    price_change: 20     # 价格变化缓冲区（可以增加）
    storage_event: 100   # 存储事件缓冲区
```

**预期收益**: 减少不必要的内存占用，同时保持性能

### 2. 减少不必要的内存分配

**优化方向**:
- 复用 slice（使用 `[:0]` 重置长度）
- 使用对象池复用临时对象
- 避免在循环中创建大对象

**示例**:
```go
// 优化前
for i := 0; i < 1000; i++ {
    data := make([]byte, 1024) // 每次都分配
    // 使用 data
}

// 优化后
var dataPool = sync.Pool{
    New: func() interface{} {
        return make([]byte, 1024)
    },
}

for i := 0; i < 1000; i++ {
    data := dataPool.Get().([]byte)
    defer dataPool.Put(data)
    // 使用 data
}
```

### 3. 优化大对象分配

**优化方向**: 对于频繁分配的大对象（>32KB），考虑使用对象池

**适用场景**:
- HTTP 响应缓冲区
- JSON 序列化缓冲区
- 数据库查询结果缓冲区

## 三、日志系统优化方向

### 1. 优化 logln 函数（未完成）

**当前状态**: `logln` 函数仍使用 `fmt.Sprintf` 和 `fmt.Sprintln`

**优化建议**: 使用 `strings.Builder` + `sync.Pool`，与 `logf` 保持一致

**实现方式**:
```go
// logln 内部日志输出函数（无格式）
func logln(level LogLevel, args ...interface{}) {
    if !shouldLog(level) {
        return
    }
    
    // 使用对象池复用 Builder
    builder := builderPool.Get().(*strings.Builder)
    defer func() {
        builder.Reset()
        builderPool.Put(builder)
    }()
    
    // 构建前缀
    builder.WriteString("[")
    builder.WriteString(level.String())
    builder.WriteString("] ")
    
    // 构建消息
    for i, arg := range args {
        if i > 0 {
            builder.WriteString(" ")
        }
        builder.WriteString(fmt.Sprint(arg))
    }
    message := builder.String()
    
    // 为了兼容性，也构建 prefix
    prefix := fmt.Sprintf("[%s] ", level.String())
    
    // 输出到控制台
    log.Println(append([]interface{}{prefix}, args...)...)
    
    // ... 其余代码保持不变
}
```

### 2. 日志消息长度限制

**当前状态**: 没有限制日志消息长度

**优化建议**: 限制单条日志消息的最大长度，防止异常情况下的内存问题

**实现方式**:
```go
const maxLogMessageLength = 10000 // 最大10KB

func truncateMessage(message string) string {
    if len(message) > maxLogMessageLength {
        return message[:maxLogMessageLength] + "... [truncated]"
    }
    return message
}

// 在 logf 和 logln 中使用
message := truncateMessage(builder.String())
```

### 3. 日志文件大小限制和轮转

**当前状态**: 只有日期轮转，没有大小限制

**优化建议**: 添加文件大小限制，当日志文件超过限制时轮转

**实现方式**:
```go
const maxLogFileSize = 100 * 1024 * 1024 // 100MB

func checkAndRotateLog() {
    // ... 现有代码 ...
    
    // 检查文件大小
    if logFile != nil {
        if info, err := logFile.Stat(); err == nil {
            if info.Size() > maxLogFileSize {
                // 轮转日志文件
                rotateLogFile()
            }
        }
    }
}

func rotateLogFile() {
    // 关闭当前文件
    if logFile != nil {
        logFile.Close()
    }
    
    // 重命名旧文件（添加时间戳）
    oldName := filepath.Join(logDir, fmt.Sprintf("app-quantmesh-%s.log", currentDate))
    newName := filepath.Join(logDir, fmt.Sprintf("app-quantmesh-%s-%s.log", 
        currentDate, time.Now().Format("150405")))
    os.Rename(oldName, newName)
    
    // 创建新文件
    // ... 创建逻辑 ...
}
```

### 4. 日志文件压缩和归档

**当前状态**: 日志文件不压缩，可能占用大量磁盘空间

**优化建议**: 定期压缩旧日志文件，删除过旧的日志

**实现方式**:
```go
// 定期压缩和清理日志文件
go func() {
    ticker := time.NewTicker(24 * time.Hour) // 每天执行一次
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            compressOldLogs()
            deleteOldLogs(30) // 删除30天前的日志
        }
    }
}()

func compressOldLogs() {
    // 压缩昨天的日志文件
    yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
    logFile := filepath.Join(logDir, fmt.Sprintf("app-quantmesh-%s.log", yesterday))
    
    if info, err := os.Stat(logFile); err == nil && !info.IsDir() {
        // 使用 gzip 压缩
        compressFile(logFile)
    }
}

func compressFile(filename string) error {
    file, err := os.Open(filename)
    if err != nil {
        return err
    }
    defer file.Close()
    
    gzFile, err := os.Create(filename + ".gz")
    if err != nil {
        return err
    }
    defer gzFile.Close()
    
    gzWriter := gzip.NewWriter(gzFile)
    defer gzWriter.Close()
    
    _, err = io.Copy(gzWriter, file)
    if err == nil {
        // 压缩成功后删除原文件
        os.Remove(filename)
    }
    return err
}
```

### 5. 日志存储 Channel 缓冲区优化

**当前状态**: 日志存储 channel 缓冲区为 1000

**优化建议**: 
- 减少缓冲区大小（500-800）
- 添加丢弃统计，监控日志丢失情况

**实现方式**:
```go
type LogStorage struct {
    // ... 现有字段 ...
    droppedCount int64 // 丢弃的日志数量（原子操作）
}

func (ls *LogStorage) WriteLog(level, message string) {
    if ls.closed {
        return
    }
    
    entry := &logEntry{
        level:     level,
        message:   message,
        timestamp: utils.NowUTC(),
    }
    
    select {
    case ls.logCh <- entry:
        // 成功加入队列
    default:
        // Channel 满了，丢弃消息
        atomic.AddInt64(&ls.droppedCount, 1)
        // 可选：记录警告（但要避免循环日志）
    }
}

// 获取丢弃统计
func (ls *LogStorage) GetDroppedCount() int64 {
    return atomic.LoadInt64(&ls.droppedCount)
}
```

### 6. 日志级别过滤优化

**当前状态**: 在写入前检查日志级别

**优化建议**: 对于高频日志（如 DEBUG），可以提前过滤，减少不必要的字符串分配

**实现方式**:
```go
// 在 logf 和 logln 的最开始就检查
func logf(level LogLevel, format string, args ...interface{}) {
    // 快速路径：如果日志级别不匹配，直接返回
    if !shouldLog(level) {
        return
    }
    
    // 慢速路径：构建日志消息
    // ... 现有代码 ...
}
```

### 7. 批量日志写入优化

**当前状态**: 日志存储每秒刷新一次，批量大小为 100

**优化建议**: 
- 根据日志量动态调整批量大小
- 添加最大等待时间，避免日志延迟

**实现方式**:
```go
func (ls *LogStorage) processLogs() {
    buffer := make([]*logEntry, 0, 100)
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()
    
    maxWaitTime := 5 * time.Second // 最大等待5秒
    lastFlush := time.Now()
    
    flush := func() {
        // ... 现有刷新逻辑 ...
        lastFlush = time.Now()
    }
    
    for {
        select {
        case entry, ok := <-ls.logCh:
            if !ok {
                flush()
                return
            }
            buffer = append(buffer, entry)
            
            // 达到批量大小或超过最大等待时间，立即刷新
            if len(buffer) >= 100 || time.Since(lastFlush) > maxWaitTime {
                flush()
            }
            
        case <-ticker.C:
            // 定期刷新
            if len(buffer) > 0 {
                flush()
            }
        }
    }
}
```

### 8. 日志采样（高频日志）

**当前状态**: 所有日志都记录

**优化建议**: 对于高频日志（如价格更新），可以采样记录

**实现方式**:
```go
// 采样率配置
type LogSamplingConfig struct {
    Enabled   bool
    Rate      int // 采样率，例如 10 表示每10条记录1条
    Level     LogLevel // 只对特定级别采样
}

var logSampling = LogSamplingConfig{
    Enabled: true,
    Rate:    10, // 每10条记录1条
    Level:   DEBUG,
}

func shouldSample(level LogLevel) bool {
    if !logSampling.Enabled || level != logSampling.Level {
        return true // 不采样
    }
    
    // 简单的采样逻辑
    return time.Now().UnixNano()%int64(logSampling.Rate) == 0
}

// 在 logf 中使用
func logf(level LogLevel, format string, args ...interface{}) {
    if !shouldLog(level) {
        return
    }
    
    // 采样检查
    if !shouldSample(level) {
        return
    }
    
    // ... 记录日志 ...
}
```

## 四、实施优先级

### 高优先级（立即实施）
1. ✅ 优化 logln 函数（使用 Builder + Pool）
2. ✅ 添加日志消息长度限制
3. ✅ 设置 GOGC 参数（通过环境变量或配置）
4. ✅ 优化日志存储 channel 缓冲区

### 中优先级（近期实施）
5. 添加日志文件大小限制和轮转
6. 实现日志文件压缩和归档
7. 优化 GC 触发时机（根据内存使用动态调整）
8. 添加 GC 性能监控

### 低优先级（长期优化）
9. 实现日志采样功能
10. 优化批量日志写入（动态批量大小）
11. 设置内存限制（GOMEMLIMIT）

## 五、配置建议

在 `config.yaml` 中添加以下配置：

```yaml
memory:
  # GC 配置
  go_gc_percent: 100        # GOGC 值（默认100）
  memory_limit: ""          # 内存限制，例如 "512MiB"（可选）
  
  # Channel 缓冲区大小
  channel_buffers:
    event_bus: 1000
    log_storage: 500
    price_change: 20
    storage_event: 100

logging:
  # 日志消息限制
  max_message_length: 10000  # 最大10KB
  
  # 日志文件配置
  max_file_size: 104857600  # 100MB
  compress_after_days: 1     # 1天后压缩
  delete_after_days: 30      # 30天后删除
  
  # 日志采样（可选）
  sampling:
    enabled: false
    rate: 10                  # 采样率
    level: "DEBUG"            # 采样级别
```

## 六、预期收益

### GC 优化
- 调整 GOGC: 内存使用降低 20-30% 或 GC 频率降低 50%
- 设置内存限制: 防止内存无限增长
- 动态 GC 触发: 更智能的内存管理

### 日志优化
- logln 优化: 减少 10-20% 的字符串分配
- 消息长度限制: 防止异常情况下的内存问题
- 文件大小限制: 防止单个日志文件过大
- 压缩归档: 节省 70-90% 的磁盘空间
- Channel 优化: 减少内存占用

## 总结

通过以上优化，可以：
1. **降低内存使用** 20-40%
2. **减少 GC 频率** 或 **降低 GC 停顿时间**
3. **优化日志系统**，减少内存分配和磁盘占用
4. **提高系统稳定性**，防止内存和磁盘问题

建议按照优先级逐步实施，每次优化后进行验证和监控。
