# 并发安全修复报告

## 问题描述

在 `web/api.go` 中发现严重的并发安全问题:

### 受影响的全局变量
- `statusBySymbol` - 存储多交易对状态的 map
- `priceProviders` - 价格提供者 map
- `exchangeProviders` - 交易所提供者 map
- `positionProviders` - 持仓管理提供者 map
- `riskProviders` - 风控提供者 map
- `storageProviders` - 存储服务提供者 map
- `fundingProviders` - 资金费率提供者 map

### 并发访问场景

**写入操作** (无同步保护):
1. `RegisterSymbolProviders()` - 从 main.go 的多个 goroutine 中调用,为每个交易对注册 providers
2. `RegisterFundingProvider()` - 从 main.go 中调用,注册资金费率监控

**读取操作** (无同步保护):
1. `pickStatus()`, `pickPriceProvider()`, `pickExchangeProvider()` 等 - 从 HTTP handler 并发调用
2. `getSymbols()` - 遍历 `statusBySymbol` map
3. `getExchanges()` - 遍历 `statusBySymbol` map

### 潜在问题
- **Go runtime panic**: 并发读写 map 会导致 "concurrent map read and write" panic
- **数据损坏**: 可能导致数据不一致
- **竞态条件**: 在高并发场景下会频繁触发

## 修复方案

### 1. 添加读写锁

```go
var (
    // 保护 statusBySymbol 的读写锁
    statusMu sync.RWMutex
    
    // 保护所有 provider 映射的读写锁
    providersMu sync.RWMutex
)
```

### 2. 保护写入操作

**RegisterSymbolProviders():**
```go
func RegisterSymbolProviders(exchange, symbol string, providers *SymbolScopedProviders) {
    if providers == nil {
        return
    }
    key := makeSymbolKey(exchange, symbol)
    
    // 使用写锁保护并发写入
    statusMu.Lock()
    statusBySymbol[key] = providers.Status
    statusMu.Unlock()
    
    providersMu.Lock()
    if providers.Price != nil {
        priceProviders[key] = providers.Price
    }
    // ... 其他 providers
    providersMu.Unlock()
}
```

**RegisterFundingProvider():**
```go
func RegisterFundingProvider(exchange, symbol string, provider FundingMonitorProvider) {
    if provider == nil {
        return
    }
    key := makeSymbolKey(exchange, symbol)
    
    // 使用写锁保护并发写入
    providersMu.Lock()
    fundingProviders[key] = provider
    providersMu.Unlock()
}
```

### 3. 保护读取操作

**单点查询 (例如 pickStatus):**
```go
func pickStatus(c *gin.Context) *SystemStatus {
    if key := resolveSymbolKey(c); key != "" {
        statusMu.RLock()
        st, ok := statusBySymbol[key]
        statusMu.RUnlock()
        if ok && st != nil {
            return st
        }
    }
    return currentStatus
}
```

**遍历操作 (例如 getSymbols):**
```go
func getSymbols(c *gin.Context) {
    // ...
    
    // 使用读锁保护遍历操作
    statusMu.RLock()
    for _, st := range statusBySymbol {
        // ... 处理逻辑
    }
    statusMu.RUnlock()
    
    // ...
}
```

### 4. 所有受保护的函数

**写入函数:**
- `RegisterSymbolProviders()`
- `RegisterFundingProvider()`

**读取函数:**
- `pickStatus()`
- `pickPriceProvider()`
- `pickExchangeProvider()`
- `pickPositionProvider()`
- `pickRiskProvider()`
- `pickStorageProvider()`
- `pickFundingProvider()`
- `getSymbols()`
- `getExchanges()`

## 验证测试

创建了 `web/api_concurrency_test.go` 包含以下测试:

1. **TestConcurrentProviderAccess** - 测试并发读写 provider maps
   - 10 个 goroutine 并发写入
   - 10 个 goroutine 并发读取
   - 10 个 goroutine 并发遍历
   - 每个 goroutine 执行 100 次操作

2. **TestConcurrentFundingProviderRegistration** - 测试并发注册资金费率提供者
   - 5 个 goroutine 并发注册
   - 5 个 goroutine 并发读取

3. **TestConcurrentSymbolIteration** - 测试并发遍历交易对列表
   - 10 个 goroutine 并发遍历
   - 3 个 goroutine 并发写入

### 测试结果

```bash
$ go test -race -run TestConcurrent ./web/... -v

=== RUN   TestConcurrentProviderAccess
    api_concurrency_test.go:100: 并发测试完成，没有发生数据竞争
--- PASS: TestConcurrentProviderAccess (0.00s)
=== RUN   TestConcurrentFundingProviderRegistration
    api_concurrency_test.go:141: 资金费率提供者并发注册测试完成
--- PASS: TestConcurrentFundingProviderRegistration (0.00s)
=== RUN   TestConcurrentSymbolIteration
    api_concurrency_test.go:208: 并发遍历测试完成
--- PASS: TestConcurrentSymbolIteration (0.00s)
PASS
ok      quantmesh/web   1.794s
```

✅ **所有测试通过,Go race detector 没有检测到任何数据竞争!**

## 性能影响

使用 `sync.RWMutex` 的性能影响:
- **读操作**: 多个 goroutine 可以同时持有读锁,性能影响极小
- **写操作**: 写锁是独占的,但写操作频率很低(仅在启动时注册 providers)
- **整体影响**: 可忽略不计,因为读操作远多于写操作

## 修改文件

1. `web/api.go` - 添加锁保护
2. `web/api_concurrency_test.go` - 新增并发测试

## 建议

1. ✅ **已修复**: 所有全局 map 的并发访问都已添加适当的锁保护
2. ✅ **已验证**: 通过 race detector 和并发测试验证修复有效
3. 📝 **建议**: 在代码审查时注意检查新增的全局变量是否需要并发保护
4. 📝 **建议**: 考虑使用 `sync.Map` 替代 `map + sync.RWMutex` (如果性能成为瓶颈)

## 总结

此修复解决了 `web/api.go` 中严重的并发安全问题,防止了:
- Go runtime panic (concurrent map read and write)
- 数据竞争和数据损坏
- 在高并发场景下的不稳定性

修复后的代码已通过 Go race detector 验证,可以安全地在生产环境中使用。

