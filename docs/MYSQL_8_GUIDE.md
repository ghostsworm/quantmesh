# MySQL 8 配置和迁移指南

本文档提供了 QuantMesh 项目使用 MySQL 8 数据库的完整指南，包括配置、性能优化和迁移建议。

## 目录

- [MySQL 8 特性支持](#mysql-8-特性支持)
- [配置指南](#配置指南)
- [性能优化](#性能优化)
- [索引优化](#索引优化)
- [迁移指南](#迁移指南)
- [备份与恢复](#备份与恢复)
- [故障排查](#故障排查)

## MySQL 8 特性支持

QuantMesh 已针对 MySQL 8.0+ 进行了优化，支持以下特性：

### ✅ 已实现的功能

- **MySQL 8.0+ 连接支持**：使用 `gorm.io/driver/mysql` v1.6.0+
- **时区一致性**：自动设置 UTC 时区，确保时间处理一致
- **性能优化**：禁用 Performance Schema 以减少开销
- **SQL 模式优化**：使用严格模式确保数据完整性
- **跨数据库兼容**：提供统一的接口，支持 SQLite、PostgreSQL 和 MySQL

### 🔧 技术细节

1. **连接池配置**
   ```go
   MaxOpenConns: 100      // 最大打开连接数
   MaxIdleConns: 10       // 最大空闲连接数
   ConnMaxLifetime: 3600  // 连接最大生命周期（秒）
   ```

2. **MySQL 8 会话参数**
   ```sql
   SET SESSION sql_mode = 'STRICT_TRANS_TABLES,NO_ZERO_DATE,NO_ZERO_IN_DATE,ERROR_FOR_DIVISION_BY_ZERO'
   SET SESSION time_zone = '+00:00'
   SET SESSION performance_schema = OFF
   ```

## 配置指南

### 基本配置

在 `config.yaml` 中添加 MySQL 8 配置：

```yaml
database:
  # 数据库类型
  type: "mysql"

  # MySQL 连接字符串 (DSN)
  # 格式: username:password@tcp(host:port)/dbname?param=value
  dsn: "quantmesh:your_password@tcp(localhost:3306)/quantmesh?charset=utf8mb4&parseTime=True&loc=Local"

  # 连接池配置
  max_open_conns: 100      # 最大打开连接数（建议：CPU核心数 × 2）
  max_idle_conns: 10       # 最大空闲连接数（建议：10-20）
  conn_max_lifetime: 3600  # 连接最大生命周期（秒）

  # 日志级别
  log_level: "error"       # silent, error, warn, info
```

### DSN 参数说明

| 参数 | 说明 | 推荐值 |
|------|------|--------|
| `charset` | 字符集 | `utf8mb4` (支持完整 Unicode) |
| `parseTime` | 解析时间 | `True` |
| `loc` | 时区 | `Local` 或 `UTC` |
| `collation` | 排序规则 | `utf8mb4_unicode_ci` |
| `timeout` | 连接超时 | `10s` |
| `readTimeout` | 读超时 | `30s` |
| `writeTimeout` | 写超时 | `30s` |

### 完整 DSN 示例

```yaml
# 生产环境示例
dsn: "quantmesh:strong_password@tcp(mysql-production.example.com:3306)/quantmesh?charset=utf8mb4&parseTime=True&loc=Local&collation=utf8mb4_unicode_ci&timeout=10s&readTimeout=30s&writeTimeout=30s"

# 本地开发示例
dsn: "quantmesh:secret@tcp(localhost:3306)/quantmesh?charset=utf8mb4&parseTime=True&loc=Local"

# 使用 Unix Socket（更快的本地连接）
dsn: "quantmesh:secret@unix(/var/run/mysql/mysql.sock)/quantmesh?charset=utf8mb4&parseTime=True&loc=Local"
```

## 性能优化

### MySQL 8 服务器配置

在 `my.cnf` 或 `my.ini` 中添加以下配置：

```ini
[mysqld]
# 基础配置
default-storage-engine = InnoDB
character-set-server = utf8mb4
collation-server = utf8mb4_unicode_ci

# 连接配置
max_connections = 200
max_connect_errors = 100000

# InnoDB 配置
innodb_buffer_pool_size = 2G         # 建议为系统内存的 50-70%
innodb_log_file_size = 512M
innodb_flush_log_at_trx_commit = 2   # 性能优化：0=最快, 1=最安全, 2=平衡
innodb_flush_method = O_DIRECT

# 查询缓存（MySQL 8.0 已移除，使用其他优化方式）

# 临时表配置
tmp_table_size = 256M
max_heap_table_size = 256M

# 慢查询日志
slow_query_log = 1
slow_query_log_file = /var/log/mysql/slow-query.log
long_query_time = 2

# 二进制日志（用于复制和恢复）
log_bin = /var/log/mysql/mysql-bin.log
expire_logs_days = 7
max_binlog_size = 100M

# 时区设置
default-time-zone = '+00:00'
```

### 应用层优化

QuantMesh 已实现以下优化：

1. **批量写入**：使用 `CreateInBatches` 提高批量插入性能
2. **连接池**：合理配置连接池大小
3. **索引优化**：为常用查询字段添加索引
4. **查询优化**：使用 `Select` 指定字段，避免 `SELECT *`

## 索引优化

### 推荐的索引策略

基于数据模型的查询模式，建议创建以下索引：

```sql
-- trades 表
ALTER TABLE trades ADD INDEX idx_exchange_symbol_created (exchange, symbol, created_at DESC);
ALTER TABLE trades ADD INDEX idx_created_at (created_at DESC);

-- orders 表
ALTER TABLE orders ADD INDEX idx_status_created (status, created_at DESC);
ALTER TABLE orders ADD INDEX idx_client_order_id (client_order_id);

-- async_tasks 表（高频查询）
ALTER TABLE async_tasks ADD INDEX idx_status_created (status, created_at ASC);
ALTER TABLE async_tasks ADD INDEX idx_task_type_status (task_type, status);
ALTER TABLE async_tasks ADD INDEX idx_expires_at (expires_at);

-- events 表
ALTER TABLE events ADD INDEX idx_severity_created (severity, created_at DESC);
ALTER TABLE events ADD INDEX idx_source_created (source, created_at DESC);

-- statistics 表
ALTER TABLE statistics ADD INDEX idx_exchange_date (exchange, date DESC);

-- reconciliation 表
ALTER TABLE reconciliation ADD INDEX idx_resolved_created (resolved, created_at DESC);

-- risk_checks 表
ALTER TABLE risk_checks ADD INDEX idx_healthy_created (is_healthy, created_at DESC);

-- position_plans 表
ALTER TABLE position_plans ADD INDEX idx_status_created (status, created_at DESC);
```

### 索引维护

定期分析和优化索引：

```sql
-- 分析表
ANALYZE TABLE trades, orders, async_tasks, events;

-- 优化表
OPTIMIZE TABLE trades, orders, async_tasks, events;

-- 检查索引使用情况
SELECT
    TABLE_NAME,
    INDEX_NAME,
    SEQ_IN_INDEX,
    COLUMN_NAME,
    CARDINALITY
FROM information_schema.STATISTICS
WHERE TABLE_SCHEMA = 'quantmesh'
ORDER BY TABLE_NAME, INDEX_NAME, SEQ_IN_INDEX;
```

## 迁移指南

### 从 SQLite 迁移到 MySQL 8

#### 步骤 1：准备 MySQL 数据库

```bash
# 创建数据库
mysql -u root -p
```

```sql
CREATE DATABASE quantmesh CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER 'quantmesh'@'localhost' IDENTIFIED BY 'strong_password';
GRANT ALL PRIVILEGES ON quantmesh.* TO 'quantmesh'@'localhost';
FLUSH PRIVILEGES;
```

#### 步骤 2：导出 SQLite 数据

```bash
# 使用 sqlite3 导出数据
sqlite3 data/quantmesh.db <<EOF
.output trades.sql
.dump trades
.output orders.sql
.dump orders
.output events.sql
.dump events
.output async_tasks.sql
.dump async_tasks
.quit
EOF
```

#### 步骤 3：转换 SQL（如有需要）

SQLite 和 MySQL 的 SQL 语法略有不同，需要注意：

- `AUTOINCREMENT` → `AUTO_INCREMENT`
- `INTEGER PRIMARY KEY` → `INT PRIMARY KEY AUTO_INCREMENT`
- `DATETIME` 类型保持一致

#### 步骤 4：导入到 MySQL

```bash
# 使用 GORM 自动创建表结构（推荐）
# 或手动导入转换后的 SQL 文件
mysql -u quantmesh -p quantmesh < trades.sql
mysql -u quantmesh -p quantmesh < orders.sql
# ... 其他表
```

### 使用 GORM 自动迁移

QuantMesh 支持自动迁移，最简单的方法是：

1. 配置 MySQL 连接
2. 启动应用，GORM 会自动创建所需的表和索引

```yaml
database:
  type: "mysql"
  dsn: "quantmesh:password@tcp(localhost:3306)/quantmesh?charset=utf8mb4&parseTime=True&loc=Local"
```

## 备份与恢复

### 备份策略

#### 逻辑备份（mysqldump）

```bash
# 完整备份
mysqldump -u quantmesh -p quantmesh > quantmesh_backup_$(date +%Y%m%d).sql

# 仅备份表结构
mysqldump -u quantmesh -p --no-data quantmesh > quantmesh_schema.sql

# 仅备份数据
mysqldump -u quantmesh -p --no-create-info quantmesh > quantmesh_data.sql

# 压缩备份
mysqldump -u quantmesh -p quantmesh | gzip > quantmesh_backup_$(date +%Y%m%d).sql.gz
```

#### 物理备份（直接复制文件）

```bash
# 备份 InnoDB 表空间
systemctl stop mysql
cp -r /var/lib/mysql/quantmesh /backup/quantmesh_$(date +%Y%m%d)
systemctl start mysql
```

### 自动备份脚本

```bash
#!/bin/bash
# /usr/local/bin/mysql_backup.sh

BACKUP_DIR="/backup/mysql"
DB_NAME="quantmesh"
DB_USER="quantmesh"
DB_PASS="your_password"
RETENTION_DAYS=30

# 创建备份目录
mkdir -p "$BACKUP_DIR"

# 备份文件名
BACKUP_FILE="$BACKUP_DIR/${DB_NAME}_$(date +%Y%m%d_%H%M%S).sql.gz"

# 执行备份
mysqldump -u "$DB_USER" -p"$DB_PASS" "$DB_NAME" | gzip > "$BACKUP_FILE"

# 删除旧备份
find "$BACKUP_DIR" -name "${DB_NAME}_*.sql.gz" -mtime +$RETENTION_DAYS -delete

echo "Backup completed: $BACKUP_FILE"
```

### 恢复数据

```bash
# 从逻辑备份恢复
gunzip < quantmesh_backup_20240101.sql.gz | mysql -u quantmesh -p quantmesh

# 从 SQL 文件恢复
mysql -u quantmesh -p quantmesh < quantmesh_backup.sql
```

## 故障排查

### 常见问题

#### 1. 连接失败

**问题**: `Error 2002: Can't connect to local MySQL server through socket`

**解决方案**:
```bash
# 检查 MySQL 服务状态
systemctl status mysql

# 启动 MySQL
systemctl start mysql

# 检查 Socket 文件
ls -l /var/run/mysql/mysql.sock
```

#### 2. 时区问题

**问题**: 时间显示不正确

**解决方案**:
```yaml
# 确保配置中设置正确的时区
database:
  dsn: "quantmesh:password@tcp(localhost:3306)/quantmesh?charset=utf8mb4&parseTime=True&loc=UTC"
```

#### 3. 性能问题

**问题**: 查询慢

**解决方案**:
```sql
-- 启用慢查询日志
SET GLOBAL slow_query_log = 'ON';
SET GLOBAL long_query_time = 2;

-- 分析慢查询
-- 查看慢查询日志文件
```

#### 4. Error 1449: definer 不存在

**问题**: `Error 1449 (HY000): The user specified as a definer ('qt'@'47.82.4.54:34842') does not exist`

**原因**: 数据库中存在视图、存储过程或触发器，其 definer（创建者）用户已不存在（常见于跨服务器迁移或用户删除后）。

**解决方案**（任选其一）:

1. **创建缺失用户**（若需保留原 definer）:
   ```sql
   CREATE USER 'qt'@'47.82.4.54' IDENTIFIED BY 'your_password';
   GRANT ALL ON quantmesh.* TO 'qt'@'47.82.4.54';
   FLUSH PRIVILEGES;
   ```

2. **修改 definer 为当前用户**（推荐）:
   ```sql
   -- 查看有问题的视图
   SELECT TABLE_NAME, DEFINER FROM information_schema.VIEWS WHERE TABLE_SCHEMA = 'quantmesh';
   -- 修改视图 definer（将 youruser@host 替换为当前用户）
   -- 或删除有问题的视图后重建
   ```

3. **使用 SQLite 回退**: QuantMesh 3.74.0-rc2+ 在 MySQL 连接失败时会自动回退到 SQLite，确保服务可启动。可将 `database.type` 改为 `sqlite` 或修复 MySQL 后重启。

#### 5. 字符集问题

**问题**: 中文乱码

**解决方案**:
```sql
-- 检查字符集
SHOW VARIABLES LIKE 'character%';
SHOW VARIABLES LIKE 'collation%';

-- 修改表字符集
ALTER TABLE trades CONVERT TO CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
```

### 监控查询

```sql
-- 查看连接数
SHOW STATUS LIKE 'Threads_connected';
SHOW STATUS LIKE 'Max_used_connections';

-- 查看表大小
SELECT
    TABLE_NAME,
    ROUND(((DATA_LENGTH + INDEX_LENGTH) / 1024 / 1024), 2) AS 'Size (MB)'
FROM information_schema.TABLES
WHERE TABLE_SCHEMA = 'quantmesh'
ORDER BY (DATA_LENGTH + INDEX_LENGTH) DESC;

-- 查看索引使用情况
SHOW INDEX FROM trades;
SHOW INDEX FROM async_tasks;
```

## 安全建议

1. **使用强密码**：为数据库用户设置复杂密码
2. **限制访问**：仅允许应用服务器访问数据库
3. **定期备份**：设置自动备份任务
4. **监控日志**：定期检查慢查询和错误日志
5. **更新版本**：保持 MySQL 8 更新到最新补丁版本

## 参考资源

- [MySQL 8.0 官方文档](https://dev.mysql.com/doc/refman/8.0/en/)
- [GORM MySQL 驱动](https://github.com/go-gorm/mysql)
- [MySQL 性能优化指南](https://dev.mysql.com/doc/refman/8.0/en/optimization.html)
