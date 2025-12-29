# Go pprof 性能分析指南

QuantMesh 已集成 Go 原生的 pprof 性能分析工具，可以帮助诊断性能问题、内存泄漏和并发问题。

## 可用的 pprof 端点

所有 pprof 端点都在 `/debug/pprof/` 路径下：

- `http://localhost:28888/debug/pprof/` - pprof 主页，显示所有可用的 profile
- `http://localhost:28888/debug/pprof/heap` - 堆内存分配
- `http://localhost:28888/debug/pprof/goroutine` - Goroutine 堆栈
- `http://localhost:28888/debug/pprof/allocs` - 所有内存分配
- `http://localhost:28888/debug/pprof/block` - 阻塞分析
- `http://localhost:28888/debug/pprof/mutex` - 互斥锁竞争
- `http://localhost:28888/debug/pprof/profile` - CPU profile（需要采样 30 秒）
- `http://localhost:28888/debug/pprof/trace` - 执行追踪

## 常用分析场景

### 1. CPU 性能分析

#### 采集 CPU profile（30 秒）
```bash
# 方法 1：使用 go tool pprof（推荐）
go tool pprof http://localhost:28888/debug/pprof/profile?seconds=30

# 方法 2：下载 profile 文件
curl -o cpu.prof http://localhost:28888/debug/pprof/profile?seconds=30

# 分析 profile 文件
go tool pprof cpu.prof
```

#### 在 pprof 交互模式中常用命令
```
(pprof) top10          # 显示 CPU 占用最高的 10 个函数
(pprof) list funcName  # 显示函数的源代码和 CPU 占用
(pprof) web            # 在浏览器中显示调用图（需要安装 graphviz）
(pprof) pdf            # 生成 PDF 调用图
(pprof) svg            # 生成 SVG 调用图
```

#### 生成火焰图
```bash
# 安装 FlameGraph 工具
git clone https://github.com/brendangregg/FlameGraph.git

# 生成火焰图
go tool pprof -raw -output=cpu.txt http://localhost:28888/debug/pprof/profile?seconds=30
FlameGraph/stackcollapse-go.pl cpu.txt | FlameGraph/flamegraph.pl > cpu-flamegraph.svg

# 在浏览器中打开 cpu-flamegraph.svg
```

### 2. 内存分析

#### 分析堆内存使用
```bash
# 实时分析
go tool pprof http://localhost:28888/debug/pprof/heap

# 下载并分析
curl -o heap.prof http://localhost:28888/debug/pprof/heap
go tool pprof heap.prof
```

#### 在 pprof 交互模式中
```
(pprof) top10              # 显示内存占用最高的 10 个函数
(pprof) list funcName      # 显示函数的内存分配详情
(pprof) inuse_space        # 按当前使用的内存排序（默认）
(pprof) alloc_space        # 按累计分配的内存排序
(pprof) inuse_objects      # 按当前对象数量排序
(pprof) alloc_objects      # 按累计对象数量排序
```

#### 对比两个时间点的内存使用
```bash
# 采集第一个快照
curl -o heap1.prof http://localhost:28888/debug/pprof/heap

# 等待一段时间（如运行一些交易）

# 采集第二个快照
curl -o heap2.prof http://localhost:28888/debug/pprof/heap

# 对比差异
go tool pprof -base=heap1.prof heap2.prof
```

### 3. Goroutine 分析

#### 查看所有 Goroutine 的堆栈
```bash
# 在浏览器中查看
open http://localhost:28888/debug/pprof/goroutine?debug=1

# 或使用 curl
curl http://localhost:28888/debug/pprof/goroutine?debug=1

# 使用 pprof 分析
go tool pprof http://localhost:28888/debug/pprof/goroutine
```

#### 检测 Goroutine 泄漏
```bash
# 采集第一个快照
curl -o goroutine1.prof http://localhost:28888/debug/pprof/goroutine

# 等待一段时间

# 采集第二个快照
curl -o goroutine2.prof http://localhost:28888/debug/pprof/goroutine

# 对比差异
go tool pprof -base=goroutine1.prof goroutine2.prof
```

### 4. 阻塞分析

阻塞分析可以帮助发现程序中的同步瓶颈（如 channel 操作、锁竞争等）。

```bash
# 注意：需要在程序启动时设置阻塞分析采样率
# 在 main.go 中添加：runtime.SetBlockProfileRate(1)

go tool pprof http://localhost:28888/debug/pprof/block
```

### 5. 互斥锁竞争分析

```bash
# 注意：需要在程序启动时启用互斥锁分析
# 在 main.go 中添加：runtime.SetMutexProfileFraction(1)

go tool pprof http://localhost:28888/debug/pprof/mutex
```

## 使用 pprof Web UI

pprof 提供了一个强大的 Web UI，可以交互式地分析 profile：

```bash
# 启动 Web UI（会自动打开浏览器）
go tool pprof -http=:8080 http://localhost:28888/debug/pprof/profile?seconds=30

# 或者分析已下载的 profile 文件
go tool pprof -http=:8080 cpu.prof
```

Web UI 功能：
- **Top** - 显示最耗时的函数
- **Graph** - 调用图
- **Flame Graph** - 火焰图
- **Peek** - 查看函数调用关系
- **Source** - 查看源代码

## 实际案例

### 案例 1：诊断订单延迟问题

```bash
# 1. 采集 30 秒的 CPU profile
go tool pprof -http=:8080 http://localhost:28888/debug/pprof/profile?seconds=30

# 2. 在 Web UI 中查看 Flame Graph
# 3. 查找 order/executor_adapter.go 相关的函数
# 4. 分析哪些函数占用 CPU 最多
```

### 案例 2：诊断内存泄漏

```bash
# 1. 系统运行一段时间后，采集堆内存快照
curl -o heap1.prof http://localhost:28888/debug/pprof/heap

# 2. 继续运行一段时间（如 1 小时）
sleep 3600

# 3. 再次采集堆内存快照
curl -o heap2.prof http://localhost:28888/debug/pprof/heap

# 4. 对比差异，查看哪些对象增长最快
go tool pprof -http=:8080 -base=heap1.prof heap2.prof

# 5. 在 Web UI 中查看 "inuse_space" 或 "alloc_space"
```

### 案例 3：诊断 Goroutine 泄漏

```bash
# 1. 查看当前 Goroutine 数量
curl http://localhost:28888/debug/pprof/goroutine?debug=1 | grep "goroutine profile:" 

# 2. 如果数量异常高，查看堆栈
curl http://localhost:28888/debug/pprof/goroutine?debug=2 > goroutines.txt

# 3. 分析 goroutines.txt，查找重复的堆栈
# 常见泄漏模式：
# - 阻塞在 channel 接收/发送
# - 阻塞在 select 语句
# - 等待锁释放
```

### 案例 4：诊断锁竞争

```bash
# 1. 确保已启用 mutex profiling（在 main.go 中）
# runtime.SetMutexProfileFraction(1)

# 2. 采集 mutex profile
go tool pprof -http=:8080 http://localhost:28888/debug/pprof/mutex

# 3. 查看哪些锁竞争最严重
# 4. 优化锁的粒度或使用无锁数据结构
```

## 性能优化建议

基于 pprof 分析结果，常见的优化方向：

### 1. CPU 优化
- 减少不必要的计算
- 使用更高效的算法和数据结构
- 减少内存分配（使用对象池）
- 避免反射和类型断言

### 2. 内存优化
- 使用 sync.Pool 复用对象
- 减少字符串拼接（使用 strings.Builder）
- 避免在循环中分配内存
- 及时释放不再使用的资源

### 3. 并发优化
- 减少锁的持有时间
- 使用读写锁替代互斥锁
- 使用 channel 或原子操作替代锁
- 避免在锁内调用外部函数

### 4. Goroutine 优化
- 使用 Goroutine 池限制并发数
- 确保 Goroutine 能够正常退出
- 避免创建过多的短生命周期 Goroutine

## 安全注意事项

⚠️ **生产环境警告**：

1. pprof 端点会暴露敏感信息（堆栈、内存内容等）
2. CPU profiling 会增加系统负载（约 5-10%）
3. 建议通过以下方式保护 pprof 端点：
   - 使用防火墙限制访问
   - 添加认证中间件
   - 仅在需要时临时启用
   - 使用 VPN 或堡垒机访问

### 添加认证保护（可选）

在 `web/server.go` 中为 pprof 端点添加认证：

```go
// pprof 性能分析端点（需要认证）
pprofGroup := r.Group("/debug/pprof")
pprofGroup.Use(authMiddleware()) // 添加认证中间件
{
    // ... pprof 路由
}
```

## 参考资源

- [Go pprof 官方文档](https://golang.org/pkg/net/http/pprof/)
- [Go 性能分析实战](https://github.com/google/pprof/blob/master/doc/README.md)
- [Profiling Go Programs](https://blog.golang.org/pprof)

