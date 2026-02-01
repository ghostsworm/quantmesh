# 数据备份与灾难恢复指南

本文档介绍 QuantMesh 系统的备份和恢复策略。

## 备份策略

### 备份内容

系统会备份以下内容：

1. **数据库文件**
   - `data/quantmesh.db` - 主数据库（交易记录、统计数据等）
   - `data/auth.db` - 认证数据库（用户密码等）
   - `data/webauthn.db` - WebAuthn 凭证数据库
   - `logs.db` - 日志数据库

2. **配置文件**
   - `config.yaml` - 主配置文件
   - `config_backups/` - 配置历史版本

3. **日志文件**（可选）
   - `logs/` 目录下的最近 7 天日志

### 备份频率建议

- **生产环境**：
  - 每小时增量备份（仅数据库）
  - 每天全量备份
  - 每周异地备份
  
- **测试环境**：
  - 每天全量备份

## 手动备份

### 执行备份

```bash
# 基本用法（备份到默认目录 ./backups）
./scripts/backup.sh

# 指定备份目录
./scripts/backup.sh /path/to/backup/directory
```

### 备份输出

备份完成后会生成：
- `YYYYMMDD_HHMMSS.tar.gz` - 压缩的备份文件
- `YYYYMMDD_HHMMSS.sha256` - 校验和文件

示例：
```
backups/
├── 20250129_143022.tar.gz
├── 20250129_143022.sha256
├── 20250129_153045.tar.gz
└── 20250129_153045.sha256
```

## 自动备份

### 使用 cron 定时任务

#### 1. 编辑 crontab

```bash
crontab -e
```

#### 2. 添加定时任务

```cron
# 每小时执行一次备份（整点）
0 * * * * cd /path/to/quantmesh && ./scripts/backup.sh >> /var/log/quantmesh-backup.log 2>&1

# 每天凌晨 2 点执行备份
0 2 * * * cd /path/to/quantmesh && ./scripts/backup.sh >> /var/log/quantmesh-backup.log 2>&1

# 每周日凌晨 3 点执行备份并上传到云存储
0 3 * * 0 cd /path/to/quantmesh && BACKUP_UPLOAD_ENABLED=true ./scripts/backup.sh >> /var/log/quantmesh-backup.log 2>&1
```

#### 3. 查看定时任务

```bash
crontab -l
```

### 使用 systemd timer（推荐）

#### 1. 创建服务文件

```bash
sudo nano /etc/systemd/system/quantmesh-backup.service
```

内容：
```ini
[Unit]
Description=QuantMesh Backup Service
After=network.target

[Service]
Type=oneshot
User=your_username
WorkingDirectory=/path/to/quantmesh
ExecStart=/path/to/quantmesh/scripts/backup.sh
StandardOutput=journal
StandardError=journal
```

#### 2. 创建 timer 文件

```bash
sudo nano /etc/systemd/system/quantmesh-backup.timer
```

内容：
```ini
[Unit]
Description=QuantMesh Backup Timer
Requires=quantmesh-backup.service

[Timer]
# 每小时执行
OnCalendar=hourly
# 系统启动后 10 分钟执行一次
OnBootSec=10min
# 如果错过了执行时间，立即执行
Persistent=true

[Install]
WantedBy=timers.target
```

#### 3. 启用并启动 timer

```bash
sudo systemctl daemon-reload
sudo systemctl enable quantmesh-backup.timer
sudo systemctl start quantmesh-backup.timer

# 查看状态
sudo systemctl status quantmesh-backup.timer

# 查看下次执行时间
sudo systemctl list-timers
```

## 数据恢复

### 恢复前准备

1. **停止服务**（推荐）
   ```bash
   ./stop.sh
   # 或
   pkill -f quantmesh
   ```

2. **备份当前数据**（以防万一）
   ```bash
   ./scripts/backup.sh ./backups/before_restore
   ```

### 执行恢复

```bash
# 查看可用的备份
ls -lh ./backups/*.tar.gz

# 恢复指定备份
./scripts/restore.sh ./backups/20250129_143022.tar.gz
```

恢复脚本会：
1. 验证备份文件完整性（校验和）
2. 显示备份信息
3. 要求确认操作
4. 备份当前数据到 `backups/before_restore_*`
5. 恢复数据库和配置文件
6. 提示重启服务

### 恢复后操作

1. **验证配置文件**
   ```bash
   cat config.yaml
   # 检查 API Key、交易对等配置是否正确
   ```

2. **验证数据库**
   ```bash
   sqlite3 data/quantmesh.db "SELECT COUNT(*) FROM trades;"
   ```

3. **重启服务**
   ```bash
   ./start.sh
   ```

4. **检查日志**
   ```bash
   tail -f logs/quantmesh.log
   ```

## 云存储备份

### AWS S3

#### 1. 安装 AWS CLI

```bash
# macOS
brew install awscli

# Linux
curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip"
unzip awscliv2.zip
sudo ./aws/install
```

#### 2. 配置 AWS 凭证

```bash
aws configure
```

#### 3. 修改备份脚本

编辑 `scripts/backup.sh`，取消注释 S3 上传部分：

```bash
# 在脚本末尾添加
if [ -n "${BACKUP_UPLOAD_ENABLED}" ] && [ "${BACKUP_UPLOAD_ENABLED}" = "true" ]; then
    log_info "上传备份到 AWS S3..."
    aws s3 cp "${BACKUP_ROOT}/${TIMESTAMP}.tar.gz" "s3://your-bucket/quantmesh-backups/"
    aws s3 cp "${BACKUP_ROOT}/${TIMESTAMP}.sha256" "s3://your-bucket/quantmesh-backups/"
    log_info "✓ 备份已上传到 S3"
fi
```

#### 4. 执行上传备份

```bash
BACKUP_UPLOAD_ENABLED=true ./scripts/backup.sh
```

### 阿里云 OSS

#### 1. 安装 ossutil

```bash
wget http://gosspublic.alicdn.com/ossutil/1.7.15/ossutil64
chmod 755 ossutil64
sudo mv ossutil64 /usr/local/bin/ossutil
```

#### 2. 配置 OSS

```bash
ossutil config
```

#### 3. 修改备份脚本

```bash
# 在脚本末尾添加
if [ -n "${BACKUP_UPLOAD_ENABLED}" ] && [ "${BACKUP_UPLOAD_ENABLED}" = "true" ]; then
    log_info "上传备份到阿里云 OSS..."
    ossutil cp "${BACKUP_ROOT}/${TIMESTAMP}.tar.gz" "oss://your-bucket/quantmesh-backups/"
    ossutil cp "${BACKUP_ROOT}/${TIMESTAMP}.sha256" "oss://your-bucket/quantmesh-backups/"
    log_info "✓ 备份已上传到 OSS"
fi
```

## 灾难恢复演练

建议定期（每月）进行灾难恢复演练，确保备份可用。

### 演练步骤

1. **准备测试环境**
   ```bash
   # 在另一台机器或目录中
   git clone <repository>
   cd quantmesh
   ```

2. **下载备份**
   ```bash
   # 从云存储下载
   aws s3 cp s3://your-bucket/quantmesh-backups/latest.tar.gz ./backups/
   ```

3. **执行恢复**
   ```bash
   ./scripts/restore.sh ./backups/latest.tar.gz
   ```

4. **启动服务**
   ```bash
   ./start.sh
   ```

5. **验证功能**
   - 访问 Web 界面
   - 检查交易记录
   - 验证配置正确性
   - 测试下单功能（测试网）

6. **记录结果**
   - RTO（恢复时间目标）：实际恢复用时
   - RPO（恢复点目标）：数据丢失量
   - 遇到的问题和解决方案

### 演练记录模板

```
灾难恢复演练记录

日期：2025-01-29
执行人：张三
备份文件：20250129_020000.tar.gz
备份时间：2025-01-29 02:00:00

恢复步骤：
1. 下载备份：耗时 2 分钟
2. 解压验证：耗时 1 分钟
3. 恢复数据：耗时 3 分钟
4. 启动服务：耗时 1 分钟

总耗时：7 分钟（RTO）
数据丢失：0 条记录（RPO）

遇到的问题：
- 无

改进建议：
- 备份文件可以进一步压缩
- 考虑增加增量备份
```

## 备份监控

### 监控备份是否成功

创建监控脚本 `scripts/check_backup.sh`：

```bash
#!/bin/bash

BACKUP_DIR="./backups"
MAX_AGE_HOURS=2

# 查找最新的备份
LATEST_BACKUP=$(ls -t ${BACKUP_DIR}/*.tar.gz 2>/dev/null | head -n 1)

if [ -z "${LATEST_BACKUP}" ]; then
    echo "ERROR: No backup found"
    exit 1
fi

# 检查备份时间
BACKUP_TIME=$(stat -f %m "${LATEST_BACKUP}")
CURRENT_TIME=$(date +%s)
AGE_HOURS=$(( (CURRENT_TIME - BACKUP_TIME) / 3600 ))

if [ ${AGE_HOURS} -gt ${MAX_AGE_HOURS} ]; then
    echo "WARNING: Latest backup is ${AGE_HOURS} hours old"
    exit 1
fi

echo "OK: Latest backup is ${AGE_HOURS} hours old"
exit 0
```

### 集成到监控系统

在 Prometheus 告警规则中添加：

```yaml
- alert: BackupTooOld
  expr: time() - quantmesh_last_backup_timestamp > 7200
  labels:
    severity: P1
  annotations:
    summary: "备份文件过旧"
    description: "最后一次备份距今超过 2 小时"
```

## 数据保留策略

### 本地备份保留

- 默认保留 30 天
- 可在 `scripts/backup.sh` 中修改 `RETENTION_DAYS` 变量

### 云存储保留

#### AWS S3 生命周期策略

```json
{
  "Rules": [
    {
      "Id": "QuantMeshBackupRetention",
      "Status": "Enabled",
      "Transitions": [
        {
          "Days": 30,
          "StorageClass": "STANDARD_IA"
        },
        {
          "Days": 90,
          "StorageClass": "GLACIER"
        }
      ],
      "Expiration": {
        "Days": 365
      }
    }
  ]
}
```

## 常见问题

### Q: 备份文件太大怎么办？

A: 可以采取以下措施：
1. 不备份日志文件（注释掉脚本中的日志备份部分）
2. 定期清理数据库中的历史数据
3. 使用增量备份策略
4. 提高压缩率（使用 `tar -czf` 改为 `tar -cJf` 使用 xz 压缩）

### Q: 如何验证备份是否可用？

A: 定期执行恢复演练，或使用以下命令快速验证：

```bash
# 验证压缩文件完整性
tar -tzf backup.tar.gz > /dev/null

# 验证校验和
sha256sum -c backup.sha256

# 验证数据库完整性
tar -xzOf backup.tar.gz */quantmesh.db | sqlite3 :memory: "PRAGMA integrity_check;"
```

### Q: 恢复后数据不一致怎么办？

A: 
1. 检查是否使用了正确的备份文件
2. 检查备份时间点，确认是否在预期范围内
3. 使用对账功能同步交易所数据
4. 如有必要，从更早的备份恢复

### Q: 如何实现跨地域备份？

A: 
1. 使用云存储的跨区域复制功能
2. 在多个地域部署备份服务器
3. 使用 rsync 定期同步到远程服务器

```bash
# 示例：rsync 到远程服务器
rsync -avz --delete ./backups/ user@remote-server:/path/to/backups/
```

## 安全建议

1. **加密备份**
   ```bash
   # 加密备份文件
   gpg -c backup.tar.gz
   
   # 解密
   gpg -d backup.tar.gz.gpg > backup.tar.gz
   ```

2. **限制备份文件权限**
   ```bash
   chmod 600 backups/*.tar.gz
   ```

3. **定期测试恢复**
   - 每月至少进行一次恢复演练
   - 记录恢复时间和遇到的问题

4. **监控备份状态**
   - 集成到监控系统
   - 备份失败时发送告警

5. **异地存储**
   - 至少保留一份异地备份
   - 使用不同的云服务商（避免单点故障）

## 参考资源

- [SQLite 备份 API](https://www.sqlite.org/backup.html)
- [AWS S3 备份最佳实践](https://docs.aws.amazon.com/AmazonS3/latest/userguide/backup-for-s3.html)
- [3-2-1 备份策略](https://www.backblaze.com/blog/the-3-2-1-backup-strategy/)

