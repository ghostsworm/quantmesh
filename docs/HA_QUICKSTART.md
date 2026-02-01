# 高可用部署快速开始

本指南帮助您快速部署 QuantMesh 的高可用架构。

## 前置要求

- Docker 和 Docker Compose
- 至少 4GB 可用内存
- 至少 20GB 可用磁盘空间

## 快速部署

### 方案 A: Docker Compose 一键部署（推荐）

#### 1. 设置环境变量

```bash
# 创建 .env 文件
cat > .env << EOF
POSTGRES_PASSWORD=your_secure_password_here
HTTPS_PROXY=http://127.0.0.1:7890
HTTP_PROXY=http://127.0.0.1:7890
ALL_PROXY=socks5://127.0.0.1:7890
EOF
```

#### 2. 准备配置文件

```bash
# 复制配置模板
cp config-ha-example.yaml config-instance1.yaml
cp config-ha-example.yaml config-instance2.yaml
cp config-ha-example.yaml config-instance3.yaml

# 编辑实例 1 配置
cat > config-instance1.yaml << 'EOF'
instance:
  id: "instance-1"
  index: 0
  total: 3

database:
  type: "postgres"
  dsn: "host=postgres user=quantmesh password=changeme dbname=quantmesh port=5432 sslmode=disable"
  max_open_conns: 100
  max_idle_conns: 10

distributed_lock:
  enabled: true
  type: "redis"
  redis:
    addr: "redis:6379"

trading:
  symbols:
    - exchange: "binance"
      symbol: "ETHUSDT"
      price_interval: 2
      order_quantity: 30
    - exchange: "binance"
      symbol: "BTCUSDT"
      price_interval: 50
      order_quantity: 0.001
EOF

# 编辑实例 2 配置
cat > config-instance2.yaml << 'EOF'
instance:
  id: "instance-2"
  index: 1
  total: 3

database:
  type: "postgres"
  dsn: "host=postgres user=quantmesh password=changeme dbname=quantmesh port=5432 sslmode=disable"

distributed_lock:
  enabled: true
  type: "redis"
  redis:
    addr: "redis:6379"

trading:
  symbols:
    - exchange: "binance"
      symbol: "BNBUSDT"
      price_interval: 1
      order_quantity: 5
    - exchange: "binance"
      symbol: "SOLUSDT"
      price_interval: 0.5
      order_quantity: 2
EOF

# 实例 3 作为热备
cat > config-instance3.yaml << 'EOF'
instance:
  id: "instance-3"
  index: 2
  total: 3

database:
  type: "postgres"
  dsn: "host=postgres user=quantmesh password=changeme dbname=quantmesh port=5432 sslmode=disable"

distributed_lock:
  enabled: true
  type: "redis"
  redis:
    addr: "redis:6379"

trading:
  symbols: []  # 热备模式，不交易
EOF
```

#### 3. 启动服务

```bash
# 启动所有服务
docker-compose -f docker-compose.ha.yml up -d

# 查看服务状态
docker-compose -f docker-compose.ha.yml ps

# 查看日志
docker-compose -f docker-compose.ha.yml logs -f
```

#### 4. 验证部署

```bash
# 检查 Redis
docker exec quantmesh_redis redis-cli ping
# 应该返回: PONG

# 检查 PostgreSQL
docker exec quantmesh_postgres pg_isready -U quantmesh
# 应该返回: /var/run/postgresql:5432 - accepting connections

# 检查实例 1
curl http://localhost:28881/api/status

# 检查实例 2
curl http://localhost:28882/api/status

# 检查实例 3
curl http://localhost:28883/api/status

# 检查 Nginx 负载均衡
curl http://localhost/api/status
```

### 方案 B: 手动部署

#### 1. 部署 Redis

```bash
docker run -d \
  --name quantmesh-redis \
  --restart unless-stopped \
  -p 6379:6379 \
  -v redis_data:/data \
  redis:7-alpine redis-server --appendonly yes
```

#### 2. 部署 PostgreSQL

```bash
docker run -d \
  --name quantmesh-postgres \
  --restart unless-stopped \
  -e POSTGRES_USER=quantmesh \
  -e POSTGRES_PASSWORD=your_password \
  -e POSTGRES_DB=quantmesh \
  -p 5432:5432 \
  -v postgres_data:/var/lib/postgresql/data \
  postgres:15-alpine
```

#### 3. 编译应用

```bash
# 设置代理
export https_proxy=http://127.0.0.1:7890
export http_proxy=http://127.0.0.1:7890
export all_proxy=socks5://127.0.0.1:7890

# 下载依赖
go mod download

# 编译
go build -o quantmesh .
```

#### 4. 启动实例

```bash
# 启动实例 1
./quantmesh --config=config-instance1.yaml &

# 启动实例 2
./quantmesh --config=config-instance2.yaml &

# 启动实例 3（热备）
./quantmesh --config=config-instance3.yaml &
```

## 架构说明

```
┌─────────────────────────────────────┐
│      Nginx (负载均衡)                │
│      http://localhost:80            │
└──────────────┬──────────────────────┘
               │
    ┌──────────┼──────────┐
    │          │          │
┌───▼────┐ ┌──▼─────┐ ┌──▼─────┐
│实例 1   │ │实例 2   │ │实例 3   │
│:28881   │ │:28882   │ │:28883   │
│ETH/BTC  │ │BNB/SOL  │ │(热备)   │
└───┬────┘ └──┬─────┘ └──┬─────┘
    │         │          │
    └─────────┼──────────┘
              │
    ┌─────────▼──────────┐
    │   Redis :6379      │
    │   (分布式锁)        │
    └─────────┬──────────┘
              │
    ┌─────────▼──────────┐
    │ PostgreSQL :5432   │
    │   (共享数据库)      │
    └────────────────────┘
```

## 交易对分配策略

### 静态分配（当前配置）

- **实例 1**: ETHUSDT, BTCUSDT
- **实例 2**: BNBUSDT, SOLUSDT
- **实例 3**: 热备（不交易）

### 动态分配（未来支持）

基于一致性哈希自动分配交易对，实例故障时自动重新分配。

## 分布式锁机制

### 锁的使用场景

1. **下单前加锁**
   ```
   锁键: quantmesh:lock:order:binance:ETHUSDT:1850.50
   TTL: 5秒
   ```

2. **取消订单前加锁**
   ```
   锁键: quantmesh:lock:cancel:binance:12345678
   TTL: 3秒
   ```

3. **对账时加锁**
   ```
   锁键: quantmesh:lock:reconcile:binance:ETHUSDT
   TTL: 30秒
   ```

### 锁的特性

- **自动过期**: 避免死锁
- **原子操作**: 使用 Lua 脚本保证原子性
- **唯一标识**: 每个锁有唯一 token，只有持有者能释放

## 数据库说明

### 支持的数据库

| 数据库 | 适用场景 | 性能 | 高可用 |
|--------|---------|------|--------|
| SQLite | 单实例、开发环境 | ⭐⭐⭐ | ❌ |
| PostgreSQL | 多实例、生产环境 | ⭐⭐⭐⭐⭐ | ✅ |
| MySQL | 多实例、生产环境 | ⭐⭐⭐⭐ | ✅ |

### 数据库迁移

从 SQLite 迁移到 PostgreSQL：

```bash
# 安装 pgloader
brew install pgloader  # macOS
# 或
apt-get install pgloader  # Ubuntu

# 执行迁移
pgloader \
  data/quantmesh.db \
  postgresql://quantmesh:password@localhost/quantmesh
```

## 监控和运维

### 查看实例状态

```bash
# 查看所有实例
curl http://localhost/api/instances

# 查看实例 1 状态
curl http://localhost:28881/api/status

# 查看实例 1 指标
curl http://localhost:28881/metrics
```

### 查看分布式锁

```bash
# 连接到 Redis
docker exec -it quantmesh_redis redis-cli

# 查看所有锁
KEYS quantmesh:lock:*

# 查看锁的值和 TTL
GET quantmesh:lock:order:binance:ETHUSDT:1850.50
TTL quantmesh:lock:order:binance:ETHUSDT:1850.50
```

### 查看数据库

```bash
# 连接到 PostgreSQL
docker exec -it quantmesh_postgres psql -U quantmesh

# 查看表
\dt

# 查看交易记录
SELECT * FROM trades ORDER BY created_at DESC LIMIT 10;

# 查看订单记录
SELECT * FROM orders ORDER BY created_at DESC LIMIT 10;

# 查看统计数据
SELECT * FROM statistics ORDER BY date DESC LIMIT 10;
```

## 故障处理

### 场景 1: 实例故障

```bash
# 检查实例状态
docker-compose -f docker-compose.ha.yml ps

# 重启故障实例
docker-compose -f docker-compose.ha.yml restart quantmesh-1

# 查看日志
docker-compose -f docker-compose.ha.yml logs quantmesh-1
```

### 场景 2: Redis 故障

```bash
# 检查 Redis 状态
docker exec quantmesh_redis redis-cli ping

# 重启 Redis
docker-compose -f docker-compose.ha.yml restart redis

# 如果数据丢失，重启所有实例
docker-compose -f docker-compose.ha.yml restart quantmesh-1 quantmesh-2 quantmesh-3
```

### 场景 3: PostgreSQL 故障

```bash
# 检查 PostgreSQL 状态
docker exec quantmesh_postgres pg_isready -U quantmesh

# 重启 PostgreSQL
docker-compose -f docker-compose.ha.yml restart postgres

# 恢复备份（如果需要）
docker exec -i quantmesh_postgres psql -U quantmesh < backup.sql
```

### 场景 4: 网络分区

如果发生网络分区，分布式锁会自动过期，避免脑裂问题。

## 性能优化

### Redis 优化

```bash
# 编辑 Redis 配置
cat > redis.conf << EOF
# 内存优化
maxmemory 2gb
maxmemory-policy allkeys-lru

# 持久化优化
save 900 1
save 300 10
save 60 10000
appendonly yes
appendfsync everysec

# 网络优化
tcp-backlog 511
timeout 0
tcp-keepalive 300
EOF

# 使用自定义配置启动
docker run -d \
  --name quantmesh-redis \
  -v $(pwd)/redis.conf:/etc/redis/redis.conf \
  redis:7-alpine redis-server /etc/redis/redis.conf
```

### PostgreSQL 优化

```bash
# 编辑 PostgreSQL 配置
cat > postgresql.conf << EOF
# 内存配置
shared_buffers = 256MB
effective_cache_size = 1GB
work_mem = 16MB
maintenance_work_mem = 64MB

# 连接配置
max_connections = 200

# 性能配置
random_page_cost = 1.1
effective_io_concurrency = 200
EOF
```

### 应用优化

```yaml
# 增加连接池大小
database:
  max_open_conns: 200
  max_idle_conns: 50
  conn_max_lifetime: 1800

# 调整锁超时
distributed_lock:
  default_ttl: 3  # 减少到 3 秒
```

## 扩容和缩容

### 添加新实例

```bash
# 1. 创建配置文件
cp config-ha-example.yaml config-instance4.yaml

# 2. 编辑配置
vim config-instance4.yaml
# 修改 instance.id 和 instance.index

# 3. 启动新实例
docker run -d \
  --name quantmesh-4 \
  --network quantmesh \
  -v $(pwd)/config-instance4.yaml:/app/config.yaml \
  quantmesh/market-maker:latest

# 4. 更新 Nginx 配置
# 添加新实例到 upstream
```

### 移除实例

```bash
# 1. 停止实例
docker stop quantmesh-4

# 2. 等待锁过期（默认 5 秒）
sleep 10

# 3. 删除容器
docker rm quantmesh-4

# 4. 更新 Nginx 配置
```

## 成本估算

### 云服务器配置（阿里云示例）

| 组件 | 规格 | 数量 | 月费用 |
|------|------|------|--------|
| 应用服务器 | 2核4G | 3 | ¥300 |
| Redis | 1G内存 | 1 | ¥100 |
| PostgreSQL | 2核4G | 1 | ¥200 |
| 负载均衡 | 基础版 | 1 | ¥50 |
| **总计** | | | **¥650/月** |

### 自建服务器

| 组件 | 规格 | 数量 | 一次性费用 |
|------|------|------|-----------|
| 服务器 | 8核16G | 2 | ¥8,000 |
| 网络 | 100M带宽 | 1 | ¥500/月 |
| **总计** | | | **¥8,000 + ¥500/月** |

## 安全建议

1. **修改默认密码**
   ```bash
   # PostgreSQL
   ALTER USER quantmesh WITH PASSWORD 'new_secure_password';
   
   # Redis
   CONFIG SET requirepass "your_redis_password"
   ```

2. **启用防火墙**
   ```bash
   # 只允许内网访问数据库
   ufw allow from 10.0.0.0/8 to any port 5432
   ufw allow from 10.0.0.0/8 to any port 6379
   ```

3. **启用 SSL/TLS**
   - PostgreSQL: 配置 SSL 证书
   - Redis: 使用 stunnel 或 Redis 6.0+ 的原生 TLS

4. **定期备份**
   ```bash
   # 添加到 crontab
   0 */6 * * * /opt/quantmesh/scripts/backup.sh
   ```

## 下一步

- 配置监控系统（Prometheus + Grafana）
- 设置告警通知（AlertManager）
- 配置日志聚合（Loki 或 ELK）
- 实施灾难恢复演练

## 参考文档

- [高可用架构设计](HIGH_AVAILABILITY.md)
- [监控系统使用指南](../monitoring/README.md)
- [备份恢复指南](BACKUP_RECOVERY.md)
- [生产部署指南](PRODUCTION_DEPLOYMENT.md)

