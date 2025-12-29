# 高可用架构设计

本文档介绍 QuantMesh 的高可用部署方案，包括分布式锁和数据库抽象层。

## 架构概览

```
┌─────────────────────────────────────────────────────┐
│                   负载均衡 (Nginx)                   │
└──────────────────┬──────────────────────────────────┘
                   │
        ┌──────────┼──────────┐
        │          │          │
   ┌────▼───┐ ┌───▼────┐ ┌───▼────┐
   │实例 1   │ │实例 2   │ │实例 3   │
   │(主动)   │ │(主动)   │ │(热备)   │
   └────┬───┘ └───┬────┘ └───┬────┘
        │         │          │
        └─────────┼──────────┘
                  │
        ┌─────────▼──────────┐
        │   分布式协调层      │
        │  (etcd/Redis)      │
        │  - 分布式锁         │
        │  - 配置中心         │
        │  - 服务发现         │
        └─────────┬──────────┘
                  │
        ┌─────────▼──────────┐
        │   共享数据库        │
        │ (PostgreSQL/MySQL) │
        └────────────────────┘
```

## 核心问题和解决方案

### 问题 1: 避免重复下单

**挑战**: 多个实例同时运行时，可能对同一价格位重复下单。

**解决方案**: 使用分布式锁

#### 方案 A: Redis 分布式锁（推荐）

**优点**:
- 性能高（内存操作）
- 实现简单
- 支持锁过期
- 广泛使用

**缺点**:
- 需要额外的 Redis 服务
- 单点故障（可通过 Redis Sentinel/Cluster 解决）

#### 方案 B: etcd 分布式锁

**优点**:
- 强一致性（Raft 协议）
- 自带服务发现
- 可作为配置中心
- 高可用

**缺点**:
- 性能略低于 Redis
- 部署复杂度较高

#### 方案 C: 数据库分布式锁

**优点**:
- 无需额外服务
- 事务支持

**缺点**:
- 性能较低
- 增加数据库负载

### 问题 2: 数据库统一抽象

**挑战**: 支持 SQLite、PostgreSQL、MySQL 等多种数据库。

**解决方案**: 数据库抽象层 + ORM

#### 技术选型

1. **GORM** (推荐)
   - 功能完善
   - 支持多种数据库
   - 自动迁移
   - 活跃维护

2. **sqlx**
   - 轻量级
   - 接近原生 SQL
   - 性能好

3. **ent**
   - 类型安全
   - 代码生成
   - 功能强大

## 实施方案

### 阶段 1: 分布式锁实现

#### 1.1 定义锁接口

```go
// lock/interface.go
type DistributedLock interface {
    // Lock 获取锁，阻塞直到成功或超时
    Lock(ctx context.Context, key string, ttl time.Duration) error
    
    // TryLock 尝试获取锁，立即返回
    TryLock(ctx context.Context, key string, ttl time.Duration) (bool, error)
    
    // Unlock 释放锁
    Unlock(ctx context.Context, key string) error
    
    // Extend 延长锁的过期时间
    Extend(ctx context.Context, key string, ttl time.Duration) error
}
```

#### 1.2 Redis 实现

```go
// lock/redis.go
type RedisLock struct {
    client *redis.Client
    prefix string
}

func (r *RedisLock) Lock(ctx context.Context, key string, ttl time.Duration) error {
    lockKey := r.prefix + key
    for {
        ok, err := r.client.SetNX(ctx, lockKey, "locked", ttl).Result()
        if err != nil {
            return err
        }
        if ok {
            return nil
        }
        // 等待后重试
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-time.After(100 * time.Millisecond):
        }
    }
}
```

#### 1.3 使用示例

```go
// 在下单前获取锁
lockKey := fmt.Sprintf("order:%s:%s:%.2f", exchange, symbol, price)
if err := lock.TryLock(ctx, lockKey, 5*time.Second); err != nil {
    // 其他实例正在处理，跳过
    return nil
}
defer lock.Unlock(ctx, lockKey)

// 下单逻辑
order, err := executor.PlaceOrder(req)
```

### 阶段 2: 数据库抽象层

#### 2.1 定义数据库接口

```go
// storage/database.go
type Database interface {
    // 交易记录
    SaveTrade(ctx context.Context, trade *Trade) error
    GetTrades(ctx context.Context, filter TradeFilter) ([]*Trade, error)
    
    // 订单记录
    SaveOrder(ctx context.Context, order *Order) error
    GetOrders(ctx context.Context, filter OrderFilter) ([]*Order, error)
    
    // 统计数据
    GetStatistics(ctx context.Context, filter StatFilter) (*Statistics, error)
    
    // 事务支持
    BeginTx(ctx context.Context) (Tx, error)
}

type Tx interface {
    Commit() error
    Rollback() error
    Database // 继承所有数据库操作
}
```

#### 2.2 GORM 实现

```go
// storage/gorm_impl.go
type GormDatabase struct {
    db *gorm.DB
}

func NewGormDatabase(config *DBConfig) (*GormDatabase, error) {
    var dialector gorm.Dialector
    
    switch config.Type {
    case "sqlite":
        dialector = sqlite.Open(config.DSN)
    case "postgres":
        dialector = postgres.Open(config.DSN)
    case "mysql":
        dialector = mysql.Open(config.DSN)
    default:
        return nil, fmt.Errorf("unsupported database type: %s", config.Type)
    }
    
    db, err := gorm.Open(dialector, &gorm.Config{})
    if err != nil {
        return nil, err
    }
    
    // 自动迁移
    if err := db.AutoMigrate(&Trade{}, &Order{}, &Statistics{}); err != nil {
        return nil, err
    }
    
    return &GormDatabase{db: db}, nil
}

func (g *GormDatabase) SaveTrade(ctx context.Context, trade *Trade) error {
    return g.db.WithContext(ctx).Create(trade).Error
}
```

#### 2.3 配置示例

```yaml
database:
  # SQLite (单实例)
  type: "sqlite"
  dsn: "./data/quantmesh.db"
  
  # PostgreSQL (多实例)
  # type: "postgres"
  # dsn: "host=localhost user=quantmesh password=secret dbname=quantmesh port=5432 sslmode=disable"
  
  # MySQL (多实例)
  # type: "mysql"
  # dsn: "quantmesh:secret@tcp(localhost:3306)/quantmesh?charset=utf8mb4&parseTime=True&loc=Local"
  
  # 连接池配置
  max_open_conns: 100
  max_idle_conns: 10
  conn_max_lifetime: 3600
```

### 阶段 3: 实例协调

#### 3.1 交易对分配策略

**策略 A: 静态分配**

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

**策略 B: 动态分配（基于 etcd）**

```go
// 服务注册
func (s *Service) Register(ctx context.Context) error {
    key := fmt.Sprintf("/quantmesh/instances/%s", s.instanceID)
    lease, err := s.etcd.Grant(ctx, 10) // 10秒租约
    if err != nil {
        return err
    }
    
    _, err = s.etcd.Put(ctx, key, s.metadata, clientv3.WithLease(lease.ID))
    if err != nil {
        return err
    }
    
    // 保持心跳
    go s.keepAlive(ctx, lease.ID)
    return nil
}

// 交易对分配
func (s *Service) AllocateSymbols(ctx context.Context) ([]string, error) {
    // 获取所有活跃实例
    resp, err := s.etcd.Get(ctx, "/quantmesh/instances/", clientv3.WithPrefix())
    if err != nil {
        return nil, err
    }
    
    instances := len(resp.Kvs)
    allSymbols := s.config.Trading.Symbols
    
    // 一致性哈希分配
    mySymbols := []string{}
    for _, symbol := range allSymbols {
        hash := hashSymbol(symbol)
        if hash%instances == s.instanceIndex {
            mySymbols = append(mySymbols, symbol)
        }
    }
    
    return mySymbols, nil
}
```

#### 3.2 配置中心集成

```go
// config/center.go
type ConfigCenter interface {
    // 获取配置
    GetConfig(ctx context.Context, key string) (string, error)
    
    // 监听配置变化
    Watch(ctx context.Context, key string) (<-chan *ConfigEvent, error)
    
    // 更新配置
    SetConfig(ctx context.Context, key, value string) error
}

// etcd 实现
type EtcdConfigCenter struct {
    client *clientv3.Client
}

func (e *EtcdConfigCenter) Watch(ctx context.Context, key string) (<-chan *ConfigEvent, error) {
    watchChan := e.client.Watch(ctx, key)
    eventChan := make(chan *ConfigEvent)
    
    go func() {
        for resp := range watchChan {
            for _, ev := range resp.Events {
                eventChan <- &ConfigEvent{
                    Type:  ev.Type,
                    Key:   string(ev.Kv.Key),
                    Value: string(ev.Kv.Value),
                }
            }
        }
    }()
    
    return eventChan, nil
}
```

## 部署架构

### 单实例部署（当前）

```
┌──────────────┐
│   QuantMesh  │
│   Instance   │
└──────┬───────┘
       │
┌──────▼───────┐
│    SQLite    │
└──────────────┘
```

### 多实例部署（推荐）

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
    │   PostgreSQL       │
    │   (共享数据库)      │
    └────────────────────┘
```

## 实施步骤

### Step 1: 部署 Redis

```bash
# Docker 部署
docker run -d \
  --name quantmesh-redis \
  -p 6379:6379 \
  redis:latest redis-server --appendonly yes

# 或使用 Redis Cluster
docker-compose -f docker-compose.redis-cluster.yml up -d
```

### Step 2: 部署 PostgreSQL

```bash
# Docker 部署
docker run -d \
  --name quantmesh-postgres \
  -e POSTGRES_USER=quantmesh \
  -e POSTGRES_PASSWORD=secret \
  -e POSTGRES_DB=quantmesh \
  -p 5432:5432 \
  postgres:15
```

### Step 3: 配置实例

```yaml
# config-instance1.yaml
instance:
  id: "instance-1"
  index: 0

database:
  type: "postgres"
  dsn: "host=postgres user=quantmesh password=secret dbname=quantmesh"

distributed_lock:
  enabled: true
  type: "redis"
  redis:
    addr: "redis:6379"
    password: ""
    db: 0

trading:
  symbols:
    - symbol: "ETHUSDT"
    - symbol: "BTCUSDT"
```

### Step 4: 启动多实例

```bash
# 实例 1
./quantmesh --config=config-instance1.yaml

# 实例 2
./quantmesh --config=config-instance2.yaml

# 实例 3 (热备)
./quantmesh --config=config-instance3.yaml --standby
```

## 数据库迁移

### 从 SQLite 迁移到 PostgreSQL

```bash
# 1. 导出 SQLite 数据
sqlite3 data/quantmesh.db .dump > dump.sql

# 2. 转换 SQL 语法（SQLite -> PostgreSQL）
# 使用工具: pgloader
pgloader data/quantmesh.db postgresql://user:pass@localhost/quantmesh

# 3. 验证数据
psql -U quantmesh -d quantmesh -c "SELECT COUNT(*) FROM trades;"
```

## 监控和运维

### 健康检查

```go
// 检查分布式锁连接
func (s *Service) HealthCheck() error {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    
    // 检查 Redis
    if err := s.lock.Ping(ctx); err != nil {
        return fmt.Errorf("redis unhealthy: %w", err)
    }
    
    // 检查数据库
    if err := s.db.Ping(ctx); err != nil {
        return fmt.Errorf("database unhealthy: %w", err)
    }
    
    return nil
}
```

### Prometheus 指标

```go
// 分布式锁指标
var (
    lockAcquireTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "quantmesh_lock_acquire_total",
            Help: "Total number of lock acquisitions",
        },
        []string{"key", "status"},
    )
    
    lockHoldDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "quantmesh_lock_hold_duration_seconds",
            Help: "Lock hold duration in seconds",
        },
        []string{"key"},
    )
)
```

## 故障处理

### 场景 1: Redis 故障

**影响**: 无法获取分布式锁，可能导致重复下单

**应对**:
1. 自动降级为单实例模式
2. 停止其他实例
3. 修复 Redis 后恢复

### 场景 2: 数据库故障

**影响**: 无法保存交易记录

**应对**:
1. 内存缓冲队列
2. 故障恢复后批量写入
3. 主从切换（如果配置）

### 场景 3: 实例故障

**影响**: 部分交易对停止交易

**应对**:
1. 其他实例自动接管（动态分配模式）
2. 热备实例激活
3. 告警通知

## 性能优化

### 锁粒度优化

```go
// 粗粒度锁（整个交易对）
lockKey := fmt.Sprintf("order:%s:%s", exchange, symbol)

// 细粒度锁（具体价格位）
lockKey := fmt.Sprintf("order:%s:%s:%.8f", exchange, symbol, price)
```

### 数据库连接池

```yaml
database:
  max_open_conns: 100    # 最大连接数
  max_idle_conns: 10     # 最大空闲连接
  conn_max_lifetime: 3600 # 连接最大生命周期（秒）
```

### 批量操作

```go
// 批量插入交易记录
func (g *GormDatabase) BatchSaveTrades(ctx context.Context, trades []*Trade) error {
    return g.db.WithContext(ctx).CreateInBatches(trades, 100).Error
}
```

## 成本分析

### 单实例 vs 多实例

| 项目 | 单实例 | 多实例 (3个) |
|------|--------|-------------|
| 服务器 | 1台 | 3台 |
| Redis | 不需要 | 1台 |
| 数据库 | SQLite | PostgreSQL (1台) |
| 月成本 | $50 | $200 |
| 可用性 | 99% | 99.9% |
| 性能 | 基准 | 3倍 |

## 参考资源

- [Redis 分布式锁最佳实践](https://redis.io/topics/distlock)
- [GORM 文档](https://gorm.io/docs/)
- [etcd 文档](https://etcd.io/docs/)
- [PostgreSQL 高可用](https://www.postgresql.org/docs/current/high-availability.html)



