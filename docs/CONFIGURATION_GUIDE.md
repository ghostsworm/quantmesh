# 配置指南

本文档说明如何配置 QuantMesh，包括单机模式和多实例模式。

## 配置模式

### 单机模式（默认）

**特点：**
- ✅ 简单易用，开箱即用
- ✅ 无需额外服务（Redis、PostgreSQL）
- ✅ 使用 SQLite 本地数据库
- ✅ 不启用分布式锁（零开销）
- ✅ 适合开发环境和小规模交易

**配置示例：**

```yaml
# 实例配置（单机模式，可省略）
instance:
    id: "default-instance"
    index: 0
    total: 1

# 数据库配置（单机模式）
database:
    type: "sqlite"                    # 使用 SQLite
    dsn: "./data/quantmesh.db"        # 本地文件
    max_open_conns: 100
    max_idle_conns: 10
    conn_max_lifetime: 3600
    log_level: "error"

# 分布式锁配置（单机模式）
distributed_lock:
    enabled: false                    # 不启用分布式锁
```

### 多实例模式（高可用）

**特点：**
- ✅ 高可用（99.9%+）
- ✅ 水平扩展，性能提升
- ✅ 使用 PostgreSQL/MySQL 共享数据库
- ✅ 使用 Redis 分布式锁
- ✅ 适合生产环境和大规模交易

**配置示例：**

```yaml
# 实例配置（多实例模式）
instance:
    id: "instance-1"                  # 每个实例唯一
    index: 0                          # 实例索引（0, 1, 2...）
    total: 3                          # 总实例数

# 数据库配置（多实例模式）
database:
    type: "postgres"                  # 使用 PostgreSQL
    dsn: "host=localhost user=quantmesh password=secret dbname=quantmesh port=5432 sslmode=disable"
    max_open_conns: 100
    max_idle_conns: 10
    conn_max_lifetime: 3600
    log_level: "error"

# 分布式锁配置（多实例模式）
distributed_lock:
    enabled: true                     # 启用分布式锁
    type: "redis"
    prefix: "quantmesh:lock:"
    default_ttl: 5
    redis:
        addr: "localhost:6379"
        password: ""
        db: 0
        pool_size: 10
```

## 配置项说明

### 实例配置 (instance)

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `id` | string | "default-instance" | 实例唯一标识 |
| `index` | int | 0 | 实例索引，用于交易对分配 |
| `total` | int | 1 | 总实例数 |

### 数据库配置 (database)

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `type` | string | "sqlite" | 数据库类型：sqlite, postgres, mysql |
| `dsn` | string | "./data/quantmesh.db" | 数据源名称 |
| `max_open_conns` | int | 100 | 最大打开连接数 |
| `max_idle_conns` | int | 10 | 最大空闲连接数 |
| `conn_max_lifetime` | int | 3600 | 连接最大生命周期（秒） |
| `log_level` | string | "error" | 日志级别：silent, error, warn, info |

#### DSN 格式

**SQLite:**
```yaml
dsn: "./data/quantmesh.db"
```

**PostgreSQL:**
```yaml
dsn: "host=localhost user=quantmesh password=secret dbname=quantmesh port=5432 sslmode=disable"
```

**MySQL:**
```yaml
dsn: "quantmesh:secret@tcp(localhost:3306)/quantmesh?charset=utf8mb4&parseTime=True&loc=Local"
```

### 分布式锁配置 (distributed_lock)

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `enabled` | bool | false | 是否启用分布式锁 |
| `type` | string | "redis" | 锁类型：redis, etcd, database |
| `prefix` | string | "quantmesh:lock:" | 锁键前缀 |
| `default_ttl` | int | 5 | 默认锁过期时间（秒） |

#### Redis 配置

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `addr` | string | "localhost:6379" | Redis 地址 |
| `password` | string | "" | Redis 密码 |
| `db` | int | 0 | Redis 数据库 |
| `pool_size` | int | 10 | 连接池大小 |

## 配置模板

### 模板 1: 单机开发环境

```yaml
# config-dev.yaml
app:
    current_exchange: binance

exchanges:
    binance:
        api_key: "YOUR_API_KEY"
        secret_key: "YOUR_SECRET_KEY"
        testnet: true

trading:
    symbols:
        - exchange: binance
          symbol: ETHUSDT
          price_interval: 2
          order_quantity: 30

system:
    log_level: "DEBUG"
    timezone: "Asia/Shanghai"
    cancel_on_exit: true

# 单机模式（默认配置）
instance:
    id: "dev-instance"
    index: 0
    total: 1

database:
    type: "sqlite"
    dsn: "./data/quantmesh.db"

distributed_lock:
    enabled: false
```

### 模板 2: 生产环境（单实例）

```yaml
# config-prod-single.yaml
app:
    current_exchange: binance

exchanges:
    binance:
        api_key: "YOUR_API_KEY"
        secret_key: "YOUR_SECRET_KEY"
        testnet: false

trading:
    symbols:
        - exchange: binance
          symbol: ETHUSDT
          price_interval: 2
          order_quantity: 200
        - exchange: binance
          symbol: BTCUSDT
          price_interval: 50
          order_quantity: 0.001

system:
    log_level: "INFO"
    timezone: "Asia/Shanghai"
    cancel_on_exit: true

# 单机模式
instance:
    id: "prod-instance"
    index: 0
    total: 1

database:
    type: "sqlite"
    dsn: "./data/quantmesh.db"

distributed_lock:
    enabled: false
```

### 模板 3: 生产环境（多实例 - 实例 1）

```yaml
# config-prod-instance1.yaml
app:
    current_exchange: binance

exchanges:
    binance:
        api_key: "YOUR_API_KEY"
        secret_key: "YOUR_SECRET_KEY"
        testnet: false

trading:
    symbols:
        - exchange: binance
          symbol: ETHUSDT
          price_interval: 2
          order_quantity: 200
        - exchange: binance
          symbol: BTCUSDT
          price_interval: 50
          order_quantity: 0.001

system:
    log_level: "INFO"
    timezone: "Asia/Shanghai"
    cancel_on_exit: true

# 多实例模式 - 实例 1
instance:
    id: "prod-instance-1"
    index: 0
    total: 3

database:
    type: "postgres"
    dsn: "host=postgres user=quantmesh password=secret dbname=quantmesh port=5432 sslmode=disable"
    max_open_conns: 100
    max_idle_conns: 10

distributed_lock:
    enabled: true
    type: "redis"
    prefix: "quantmesh:lock:"
    default_ttl: 5
    redis:
        addr: "redis:6379"
        password: ""
        db: 0
        pool_size: 10
```

### 模板 4: 生产环境（多实例 - 实例 2）

```yaml
# config-prod-instance2.yaml
app:
    current_exchange: binance

exchanges:
    binance:
        api_key: "YOUR_API_KEY"
        secret_key: "YOUR_SECRET_KEY"
        testnet: false

trading:
    symbols:
        - exchange: binance
          symbol: BNBUSDT
          price_interval: 1
          order_quantity: 100
        - exchange: binance
          symbol: SOLUSDT
          price_interval: 0.5
          order_quantity: 50

system:
    log_level: "INFO"
    timezone: "Asia/Shanghai"
    cancel_on_exit: true

# 多实例模式 - 实例 2
instance:
    id: "prod-instance-2"
    index: 1
    total: 3

database:
    type: "postgres"
    dsn: "host=postgres user=quantmesh password=secret dbname=quantmesh port=5432 sslmode=disable"
    max_open_conns: 100
    max_idle_conns: 10

distributed_lock:
    enabled: true
    type: "redis"
    prefix: "quantmesh:lock:"
    default_ttl: 5
    redis:
        addr: "redis:6379"
        password: ""
        db: 0
        pool_size: 10
```

## 配置验证

### 验证单机模式

```bash
# 启动应用
./quantmesh --config=config-dev.yaml

# 查看日志，应该看到：
# ✅ 分布式锁未启用（单机模式）
# ✅ 数据库已初始化 (类型: sqlite)
```

### 验证多实例模式

```bash
# 启动实例 1
./quantmesh --config=config-prod-instance1.yaml &

# 启动实例 2
./quantmesh --config=config-prod-instance2.yaml &

# 查看日志，应该看到：
# ✅ 分布式锁已启用 (类型: redis, 实例: prod-instance-1)
# ✅ 数据库已初始化 (类型: postgres)
```

## 配置迁移

### 从单机迁移到多实例

**步骤 1: 部署 Redis**
```bash
docker run -d --name redis -p 6379:6379 redis:7-alpine
```

**步骤 2: 部署 PostgreSQL**
```bash
docker run -d --name postgres \
  -e POSTGRES_USER=quantmesh \
  -e POSTGRES_PASSWORD=secret \
  -e POSTGRES_DB=quantmesh \
  -p 5432:5432 postgres:15-alpine
```

**步骤 3: 迁移数据**
```bash
# 使用 pgloader 迁移 SQLite 到 PostgreSQL
pgloader data/quantmesh.db postgresql://quantmesh:secret@localhost/quantmesh
```

**步骤 4: 更新配置**
```yaml
# 启用分布式锁
distributed_lock:
    enabled: true

# 切换到 PostgreSQL
database:
    type: "postgres"
    dsn: "host=localhost user=quantmesh password=secret dbname=quantmesh"
```

**步骤 5: 重启应用**
```bash
# 停止旧实例
pkill quantmesh

# 启动新实例
./quantmesh --config=config-prod-instance1.yaml &
./quantmesh --config=config-prod-instance2.yaml &
```

## 常见问题

### Q1: 单机模式下是否需要配置 distributed_lock？

**A:** 不需要。单机模式下 `distributed_lock.enabled` 默认为 `false`，系统会使用零开销的 `NopLock`。

### Q2: 可以在单机模式下使用 PostgreSQL 吗？

**A:** 可以。数据库类型和分布式锁是独立的。单机模式下也可以使用 PostgreSQL，只是没有必要。

### Q3: 多实例模式下必须使用 Redis 吗？

**A:** 目前是的。未来会支持 etcd 和数据库锁。

### Q4: 如何验证配置是否正确？

**A:** 启动应用后查看日志：
- 单机模式：`ℹ️ 分布式锁未启用（单机模式）`
- 多实例模式：`✅ 分布式锁已启用 (类型: redis, 实例: xxx)`

### Q5: 配置错误会怎样？

**A:** 应用会在启动时检测并报错，不会启动。例如：
- Redis 连接失败：`❌ 初始化分布式锁失败: dial tcp: connection refused`
- 数据库连接失败：`⚠️ 初始化数据库失败: connection refused`

## 性能建议

### 单机模式

```yaml
database:
    type: "sqlite"
    max_open_conns: 1      # SQLite 只支持单连接写入
    max_idle_conns: 1
```

### 多实例模式（3 实例）

```yaml
database:
    type: "postgres"
    max_open_conns: 100    # 3实例 × 30并发 + 10余量
    max_idle_conns: 10     # 10% 的最大连接数
    conn_max_lifetime: 1800

distributed_lock:
    default_ttl: 5         # 5秒足够大部分操作
    redis:
        pool_size: 10      # 每实例10个连接
```

## 安全建议

1. **生产环境使用强密码**
   ```yaml
   database:
       dsn: "...password=STRONG_PASSWORD..."
   
   distributed_lock:
       redis:
           password: "STRONG_PASSWORD"
   ```

2. **限制数据库访问**
   ```bash
   # 只允许内网访问
   ufw allow from 10.0.0.0/8 to any port 5432
   ufw allow from 10.0.0.0/8 to any port 6379
   ```

3. **使用 SSL/TLS**
   ```yaml
   database:
       dsn: "...sslmode=require"
   ```

4. **定期备份**
   ```bash
   # 添加到 crontab
   0 */6 * * * /opt/quantmesh/scripts/backup.sh
   ```

## 参考文档

- [高可用架构设计](HIGH_AVAILABILITY.md)
- [快速开始指南](HA_QUICKSTART.md)
- [多实例解决方案](MULTI_INSTANCE_SOLUTION.md)

