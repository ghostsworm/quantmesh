# 多实例部署解决方案

本文档详细说明 QuantMesh 如何解决多实例部署中的两个核心问题：

1. **分布式协调**：避免多台机器重复下单
2. **数据库抽象**：统一支持 SQLite、PostgreSQL、MySQL

## 问题分析

### 问题 1: 重复下单

**场景**：
```
时间 T: 价格到达 1850.50
实例 A: 检测到价格，准备下单
实例 B: 同时检测到价格，也准备下单
结果: 两个实例都下单，导致重复订单
```

**风险**：
- 超出预期的仓位
- 增加交易成本
- 风险敞口翻倍

### 问题 2: 数据库不统一

**场景**：
- 单实例使用 SQLite（简单，无需额外服务）
- 多实例需要共享数据库（PostgreSQL/MySQL）
- 代码耦合，难以切换

## 解决方案

### 方案 1: 分布式锁

#### 架构设计

```go
// 1. 定义锁接口
type DistributedLock interface {
    Lock(ctx context.Context, key string, ttl time.Duration) error
    TryLock(ctx context.Context, key string, ttl time.Duration) (bool, error)
    Unlock(ctx context.Context, key string) error
    Extend(ctx context.Context, key string, ttl time.Duration) error
}

// 2. 单实例模式：空实现（无锁开销）
type NopLock struct{}

// 3. 多实例模式：Redis 实现
type RedisLock struct {
    client *redis.Client
    prefix string
}
```

#### 使用示例

```go
// 在下单前获取锁
lockKey := fmt.Sprintf("order:%s:%s:%.2f", exchange, symbol, price)

// 尝试获取锁（非阻塞）
acquired, err := lock.TryLock(ctx, lockKey, 5*time.Second)
if err != nil {
    return fmt.Errorf("lock error: %w", err)
}
if !acquired {
    // 其他实例正在处理，跳过
    logger.Debug("价格位 %.2f 已被其他实例锁定，跳过", price)
    return nil
}
defer lock.Unlock(ctx, lockKey)

// 执行下单逻辑
order, err := executor.PlaceOrder(req)
```

#### 锁的特性

1. **自动过期**
   - TTL 默认 5 秒
   - 避免死锁
   - 实例崩溃时自动释放

2. **原子操作**
   ```lua
   -- Lua 脚本保证原子性
   if redis.call("get", KEYS[1]) == ARGV[1] then
       return redis.call("del", KEYS[1])
   else
       return 0
   end
   ```

3. **唯一标识**
   - 每个锁有唯一 token
   - 只有持有者能释放
   - 防止误释放

#### 锁粒度选择

```go
// 粗粒度：整个交易对（并发度低）
lockKey := fmt.Sprintf("order:%s:%s", exchange, symbol)

// 中粒度：价格区间（推荐）
priceLevel := math.Floor(price / priceInterval) * priceInterval
lockKey := fmt.Sprintf("order:%s:%s:%.2f", exchange, symbol, priceLevel)

// 细粒度：精确价格（并发度高）
lockKey := fmt.Sprintf("order:%s:%s:%.8f", exchange, symbol, price)
```

#### 性能对比

| 锁实现 | 延迟 | 吞吐量 | 可用性 | 复杂度 |
|--------|------|--------|--------|--------|
| 无锁（单实例） | 0ms | 无限制 | ⭐⭐⭐ | ⭐ |
| Redis | 1-3ms | 10k+ ops/s | ⭐⭐⭐⭐ | ⭐⭐ |
| etcd | 5-10ms | 1k+ ops/s | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ |
| 数据库 | 10-50ms | 100+ ops/s | ⭐⭐⭐⭐ | ⭐⭐ |

### 方案 2: 数据库抽象层

#### 架构设计

```go
// 1. 定义数据库接口
type Database interface {
    SaveTrade(ctx context.Context, trade *Trade) error
    GetTrades(ctx context.Context, filter *TradeFilter) ([]*Trade, error)
    SaveOrder(ctx context.Context, order *Order) error
    // ... 其他方法
}

// 2. GORM 统一实现
type GormDatabase struct {
    db *gorm.DB
}

// 3. 根据配置创建实例
func NewDatabase(config *DBConfig) (Database, error) {
    switch config.Type {
    case "sqlite":
        dialector = sqlite.Open(config.DSN)
    case "postgres":
        dialector = postgres.Open(config.DSN)
    case "mysql":
        dialector = mysql.Open(config.DSN)
    }
    // ...
}
```

#### 配置示例

```yaml
# 单实例：SQLite
database:
  type: "sqlite"
  dsn: "./data/quantmesh.db"

# 多实例：PostgreSQL
database:
  type: "postgres"
  dsn: "host=localhost user=quantmesh password=secret dbname=quantmesh"
  max_open_conns: 100
  max_idle_conns: 10

# 多实例：MySQL
database:
  type: "mysql"
  dsn: "quantmesh:secret@tcp(localhost:3306)/quantmesh?charset=utf8mb4"
```

#### GORM 优势

1. **多数据库支持**
   - SQLite、PostgreSQL、MySQL、SQL Server
   - 统一的 API
   - 自动迁移

2. **性能优化**
   - 预编译语句
   - 批量操作
   - 连接池

3. **开发友好**
   - 链式 API
   - 自动关联
   - Hooks 支持

#### 数据库迁移

```bash
# 方法 1: 使用 pgloader
pgloader data/quantmesh.db postgresql://user:pass@localhost/quantmesh

# 方法 2: 导出导入
sqlite3 data/quantmesh.db .dump > dump.sql
# 编辑 dump.sql 适配 PostgreSQL 语法
psql -U quantmesh -d quantmesh -f dump.sql

# 方法 3: 使用 GORM 迁移
go run migrate.go
```

## 部署架构

### 单实例部署（开发/小规模）

```
┌──────────────┐
│  QuantMesh   │
│  Instance    │
│              │
│  - NopLock   │
│  - SQLite    │
└──────────────┘
```

**特点**：
- ✅ 部署简单
- ✅ 无额外依赖
- ✅ 成本低
- ❌ 无高可用
- ❌ 性能受限

### 多实例部署（生产/大规模）

```
┌─────────────────────────────────────┐
│          Nginx (负载均衡)            │
└──────────────┬──────────────────────┘
               │
    ┌──────────┼──────────┐
    │          │          │
┌───▼────┐ ┌──▼─────┐ ┌──▼─────┐
│实例 1   │ │实例 2   │ │实例 3   │
│ETH/BTC  │ │BNB/SOL  │ │(热备)   │
└───┬────┘ └──┬─────┘ └──┬─────┘
    │         │          │
    └─────────┼──────────┘
              │
    ┌─────────▼──────────┐
    │   Redis Cluster    │
    │   (分布式锁)        │
    └─────────┬──────────┘
              │
    ┌─────────▼──────────┐
    │ PostgreSQL HA      │
    │ (主从复制)          │
    └────────────────────┘
```

**特点**：
- ✅ 高可用（99.9%+）
- ✅ 高性能（水平扩展）
- ✅ 故障自愈
- ❌ 部署复杂
- ❌ 成本较高

## 实例协调策略

### 策略 1: 静态分配（推荐）

**原理**：每个实例负责固定的交易对

**配置**：
```yaml
# 实例 1
trading:
  symbols:
    - symbol: "ETHUSDT"
    - symbol: "BTCUSDT"

# 实例 2
trading:
  symbols:
    - symbol: "BNBUSDT"
    - symbol: "SOLUSDT"
```

**优点**：
- 简单可靠
- 无需协调
- 故障隔离

**缺点**：
- 手动分配
- 负载不均衡
- 扩容需要重新配置

### 策略 2: 动态分配（未来）

**原理**：基于一致性哈希自动分配

```go
func (s *Service) AllocateSymbols() []string {
    // 获取所有活跃实例
    instances := s.etcd.GetInstances()
    
    // 一致性哈希分配
    mySymbols := []string{}
    for _, symbol := range allSymbols {
        hash := hashSymbol(symbol)
        if hash % len(instances) == s.instanceIndex {
            mySymbols = append(mySymbols, symbol)
        }
    }
    return mySymbols
}
```

**优点**：
- 自动分配
- 负载均衡
- 自动故障转移

**缺点**：
- 需要配置中心（etcd）
- 实现复杂
- 可能短暂重复

### 策略 3: 主从模式

**原理**：一个主实例，多个从实例热备

```yaml
# 主实例
instance:
  role: "master"
  
# 从实例
instance:
  role: "standby"
```

**优点**：
- 无重复下单
- 故障快速切换
- 简单可靠

**缺点**：
- 资源利用率低
- 性能无提升

## 监控指标

### 分布式锁指标

```go
// 锁获取次数
quantmesh_lock_acquire_total{key="order:binance:ETHUSDT",status="success"} 1234

// 锁冲突次数
quantmesh_lock_conflict_total{key="order:binance:ETHUSDT"} 56

// 锁持有时长
quantmesh_lock_hold_duration_seconds{key="order:binance:ETHUSDT",quantile="0.99"} 0.123
```

### 数据库指标

```go
// 连接池状态
quantmesh_db_connections{state="open"} 50
quantmesh_db_connections{state="idle"} 10

// 查询延迟
quantmesh_db_query_duration_seconds{operation="insert",quantile="0.99"} 0.05

// 错误率
quantmesh_db_errors_total{operation="insert",error="timeout"} 3
```

## 故障场景分析

### 场景 1: Redis 故障

**影响**：无法获取分布式锁

**应对**：
1. 自动降级为单实例模式
2. 停止其他实例（手动或自动）
3. 修复 Redis 后恢复

**配置**：
```yaml
distributed_lock:
  fallback_mode: "single_instance"  # Redis 故障时降级
  health_check_interval: 10         # 健康检查间隔（秒）
```

### 场景 2: 数据库故障

**影响**：无法保存交易记录

**应对**：
1. 内存缓冲队列（最多 10000 条）
2. 故障恢复后批量写入
3. 主从切换（如果配置）

**配置**：
```yaml
database:
  buffer_enabled: true
  buffer_size: 10000
  retry_interval: 10
```

### 场景 3: 实例故障

**影响**：部分交易对停止交易

**应对**：
1. 其他实例自动接管（动态分配模式）
2. 热备实例激活（主从模式）
3. 告警通知运维人员

### 场景 4: 网络分区

**影响**：实例间无法通信

**应对**：
1. 分布式锁自动过期（5秒）
2. 短暂重复后自动恢复
3. 监控告警

## 成本分析

### 单实例 vs 多实例

| 项目 | 单实例 | 3实例高可用 |
|------|--------|------------|
| 应用服务器 | 1台 (2核4G) | 3台 (2核4G) |
| Redis | 不需要 | 1台 (1G) |
| 数据库 | SQLite | PostgreSQL (2核4G) |
| 负载均衡 | 不需要 | Nginx (1台) |
| **月成本** | ¥100 | ¥650 |
| **可用性** | 99% | 99.9% |
| **性能** | 基准 | 3倍 |

### ROI 分析

假设：
- 每天交易 1000 笔
- 平均每笔盈利 $1
- 单实例故障率 1%，多实例 0.1%

**单实例**：
- 年收入：1000 × $1 × 365 × 0.99 = $361,350
- 年成本：¥100 × 12 = ¥1,200 ≈ $170
- 年净利润：$361,180

**多实例**：
- 年收入：1000 × $1 × 365 × 0.999 = $364,635
- 年成本：¥650 × 12 = ¥7,800 ≈ $1,100
- 年净利润：$363,535

**增量收益**：$363,535 - $361,180 = $2,355
**投资回收期**：($1,100 - $170) / ($2,355 / 12) ≈ 4.7 个月

## 最佳实践

### 1. 锁粒度选择

```go
// ✅ 推荐：价格区间锁
priceLevel := math.Floor(price / 10) * 10  // 每 10 美元一个锁
lockKey := fmt.Sprintf("order:%s:%s:%.0f", exchange, symbol, priceLevel)

// ❌ 避免：全局锁（并发度低）
lockKey := fmt.Sprintf("order:%s", exchange)

// ⚠️ 谨慎：精确价格锁（可能过细）
lockKey := fmt.Sprintf("order:%s:%s:%.8f", exchange, symbol, price)
```

### 2. 锁超时设置

```go
// ✅ 推荐：根据操作时间设置
下单操作: 5秒
取消订单: 3秒
对账操作: 30秒

// ❌ 避免：过长（影响故障恢复）
ttl := 60 * time.Second

// ❌ 避免：过短（操作未完成就过期）
ttl := 1 * time.Second
```

### 3. 数据库连接池

```yaml
# ✅ 推荐：根据实例数和负载调整
database:
  max_open_conns: 100  # 3个实例 × 30 并发 = 90，留 10 余量
  max_idle_conns: 10   # 10% 的最大连接数
  conn_max_lifetime: 1800  # 30分钟

# ❌ 避免：过大（浪费资源）
max_open_conns: 1000

# ❌ 避免：过小（连接不足）
max_open_conns: 10
```

### 4. 故障恢复

```go
// ✅ 推荐：优雅降级
if err := lock.TryLock(ctx, key, ttl); err != nil {
    if errors.Is(err, redis.Nil) {
        // Redis 故障，降级为本地锁
        return localLock.TryLock(ctx, key, ttl)
    }
    return err
}

// ❌ 避免：直接失败
if err := lock.TryLock(ctx, key, ttl); err != nil {
    return err
}
```

## 总结

### 核心要点

1. **分布式锁**
   - 使用 Redis 实现
   - 支持自动过期
   - 原子操作保证正确性
   - 单实例模式零开销

2. **数据库抽象**
   - 使用 GORM 统一接口
   - 支持 SQLite/PostgreSQL/MySQL
   - 自动迁移和连接池
   - 配置驱动切换

3. **部署策略**
   - 单实例：简单快速
   - 多实例：高可用高性能
   - 静态分配：简单可靠
   - 动态分配：自动化

### 实施路径

**阶段 1: 基础架构**（已完成）
- ✅ 分布式锁接口和实现
- ✅ 数据库抽象层
- ✅ 配置支持

**阶段 2: 集成测试**（下一步）
- 集成到现有代码
- 单元测试
- 集成测试

**阶段 3: 生产部署**（未来）
- Docker 镜像构建
- 多实例部署
- 监控和告警

### 参考文档

- [高可用架构设计](HIGH_AVAILABILITY.md)
- [快速开始指南](HA_QUICKSTART.md)
- [配置示例](../config-ha-example.yaml)
- [Docker Compose](../docker-compose.ha.yml)

