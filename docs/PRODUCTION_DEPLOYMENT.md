# 生产环境部署指南

本文档介绍如何在生产环境中部署 QuantMesh 做市商系统。

## 系统要求

### 硬件要求

- **CPU**: 4核心以上（推荐 8核心）
- **内存**: 8GB 以上（推荐 16GB）
- **硬盘**: 100GB 以上 SSD
- **网络**: 稳定的网络连接，低延迟

### 软件要求

- **操作系统**: Ubuntu 20.04 LTS / CentOS 8 / macOS 12+
- **Go**: 1.21 或更高版本
- **数据库**: SQLite 3（内置）
- **反向代理**: Nginx 1.18+ 或 Caddy 2+
- **监控**: Prometheus + Grafana（可选）

## 部署步骤

### 1. 创建系统用户

```bash
# 创建专用用户（不允许登录）
sudo useradd -r -s /bin/false quantmesh

# 创建目录
sudo mkdir -p /opt/quantmesh
sudo mkdir -p /opt/quantmesh/data
sudo mkdir -p /opt/quantmesh/logs
sudo mkdir -p /opt/quantmesh/backups

# 设置权限
sudo chown -R quantmesh:quantmesh /opt/quantmesh
```

### 2. 编译程序

```bash
# 在开发机器上编译
cd /path/to/quantmesh
go build -ldflags="-s -w" -o quantmesh .

# 或使用 Makefile
make build
```

### 3. 部署文件

```bash
# 复制二进制文件
sudo cp quantmesh /opt/quantmesh/

# 复制配置文件
sudo cp config.example.yaml /opt/quantmesh/config.yaml

# 复制脚本
sudo cp -r scripts /opt/quantmesh/

# 设置权限
sudo chown -R quantmesh:quantmesh /opt/quantmesh
sudo chmod +x /opt/quantmesh/quantmesh
sudo chmod +x /opt/quantmesh/scripts/*.sh
```

### 4. 配置文件

编辑 `/opt/quantmesh/config.yaml`：

```yaml
app:
  current_exchange: "binance"

exchanges:
  binance:
    api_key: "YOUR_API_KEY"
    secret_key: "YOUR_SECRET_KEY"
    fee_rate: 0.0002

trading:
  symbols:
    - exchange: "binance"
      symbol: "ETHUSDT"
      price_interval: 2
      order_quantity: 30
      # ... 其他配置

system:
  log_level: "INFO"
  timezone: "Asia/Shanghai"
  cancel_on_exit: true

web:
  enabled: true
  host: "127.0.0.1"  # 只监听本地，通过 Nginx 代理
  port: 28888

# ... 其他配置
```

### 5. 配置 systemd 服务

```bash
# 复制服务文件
sudo cp scripts/quantmesh.service /etc/systemd/system/

# 重载 systemd
sudo systemctl daemon-reload

# 启用服务（开机自启）
sudo systemctl enable quantmesh

# 启动服务
sudo systemctl start quantmesh

# 查看状态
sudo systemctl status quantmesh

# 查看日志
sudo journalctl -u quantmesh -f
```

### 6. 配置 Nginx 反向代理

参考 [NGINX_CONFIG.md](NGINX_CONFIG.md) 配置 Nginx。

关键配置：

```nginx
upstream quantmesh_backend {
    server 127.0.0.1:28888;
}

server {
    listen 443 ssl http2;
    server_name your-domain.com;
    
    # SSL 配置
    ssl_certificate /etc/letsencrypt/live/your-domain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/your-domain.com/privkey.pem;
    
    # Rate Limiting
    limit_req zone=api_limit burst=20 nodelay;
    
    location / {
        proxy_pass http://quantmesh_backend;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

### 7. 配置防火墙

```bash
# Ubuntu/Debian (ufw)
sudo ufw allow 22/tcp    # SSH
sudo ufw allow 80/tcp    # HTTP
sudo ufw allow 443/tcp   # HTTPS
sudo ufw enable

# CentOS/RHEL (firewalld)
sudo firewall-cmd --permanent --add-service=ssh
sudo firewall-cmd --permanent --add-service=http
sudo firewall-cmd --permanent --add-service=https
sudo firewall-cmd --reload
```

### 8. 配置自动备份

```bash
# 编辑 crontab
sudo crontab -e -u quantmesh

# 添加定时任务
# 每小时备份
0 * * * * cd /opt/quantmesh && ./scripts/backup.sh >> /var/log/quantmesh-backup.log 2>&1

# 每周上传到云存储
0 3 * * 0 cd /opt/quantmesh && BACKUP_UPLOAD_ENABLED=true ./scripts/backup.sh >> /var/log/quantmesh-backup.log 2>&1
```

### 9. 配置监控

```bash
# 启动监控栈
cd /opt/quantmesh
docker-compose -f monitoring/docker-compose.monitoring.yml up -d

# 配置 Prometheus 抓取
# 编辑 monitoring/prometheus/prometheus.yml
# 添加 QuantMesh 目标
```

### 10. 验证部署

```bash
# 检查服务状态
sudo systemctl status quantmesh

# 检查日志
sudo journalctl -u quantmesh -n 100

# 检查 Web 界面
curl -k https://your-domain.com/api/status

# 检查 Prometheus metrics
curl http://localhost:28888/metrics
```

## 安全加固

### 1. 最小权限原则

```bash
# 确保文件权限正确
sudo chmod 600 /opt/quantmesh/config.yaml
sudo chmod 700 /opt/quantmesh/data
sudo chmod 700 /opt/quantmesh/backups
```

### 2. 配置 SELinux（CentOS/RHEL）

```bash
# 设置 SELinux 上下文
sudo semanage fcontext -a -t bin_t "/opt/quantmesh/quantmesh"
sudo restorecon -v /opt/quantmesh/quantmesh
```

### 3. 限制 SSH 访问

编辑 `/etc/ssh/sshd_config`：

```
# 禁用 root 登录
PermitRootLogin no

# 使用密钥认证
PasswordAuthentication no
PubkeyAuthentication yes

# 限制用户
AllowUsers your_username
```

重启 SSH：

```bash
sudo systemctl restart sshd
```

### 4. 配置 fail2ban

```bash
# 安装 fail2ban
sudo apt install fail2ban

# 配置
sudo cp /etc/fail2ban/jail.conf /etc/fail2ban/jail.local
sudo nano /etc/fail2ban/jail.local

# 启用
sudo systemctl enable fail2ban
sudo systemctl start fail2ban
```

### 5. 定期更新系统

```bash
# Ubuntu/Debian
sudo apt update && sudo apt upgrade -y

# CentOS/RHEL
sudo yum update -y

# 配置自动安全更新
sudo apt install unattended-upgrades
sudo dpkg-reconfigure -plow unattended-upgrades
```

## 高可用部署

### 多实例部署

```bash
# 实例 1
/opt/quantmesh/quantmesh --config=/opt/quantmesh/config1.yaml --port=28888

# 实例 2
/opt/quantmesh/quantmesh --config=/opt/quantmesh/config2.yaml --port=28889

# 实例 3（备用）
/opt/quantmesh/quantmesh --config=/opt/quantmesh/config3.yaml --port=28890
```

### Nginx 负载均衡

```nginx
upstream quantmesh_backend {
    server 127.0.0.1:28888 weight=1;
    server 127.0.0.1:28889 weight=1;
    server 127.0.0.1:28890 weight=1 backup;
}
```

### 数据库复制（可选）

如果使用 PostgreSQL/MySQL 替代 SQLite：

```bash
# 配置主从复制
# 参考数据库官方文档
```

## 监控和告警

### 1. 系统监控

- CPU、内存、磁盘使用率
- 网络流量
- 进程状态

### 2. 应用监控

- 订单成功率
- 订单延迟
- WebSocket 连接状态
- 风控触发次数

### 3. 告警配置

参考 [monitoring/prometheus/alerts.yml](../monitoring/prometheus/alerts.yml)

### 4. 日志聚合

使用 Loki 或 ELK Stack 聚合日志：

```bash
# 使用 Loki
docker run -d -p 3100:3100 grafana/loki:latest

# 使用 Promtail 收集日志
docker run -d \
  -v /opt/quantmesh/logs:/var/log/quantmesh \
  -v /path/to/promtail-config.yaml:/etc/promtail/config.yaml \
  grafana/promtail:latest
```

## 性能优化

### 1. 系统参数调优

编辑 `/etc/sysctl.conf`：

```ini
# 增加文件描述符限制
fs.file-max = 65536

# TCP 优化
net.core.somaxconn = 1024
net.ipv4.tcp_max_syn_backlog = 2048
net.ipv4.tcp_fin_timeout = 30

# 内存优化
vm.swappiness = 10
```

应用配置：

```bash
sudo sysctl -p
```

### 2. 应用优化

- 使用对象池减少内存分配
- 优化数据库查询
- 使用连接池
- 启用 HTTP/2

### 3. 数据库优化

```sql
-- 定期清理旧数据
DELETE FROM trades WHERE created_at < datetime('now', '-90 days');

-- 重建索引
REINDEX;

-- 优化数据库
VACUUM;
```

## 故障排查

### 常见问题

1. **服务无法启动**
   ```bash
   # 查看日志
   sudo journalctl -u quantmesh -n 100
   
   # 检查配置
   /opt/quantmesh/quantmesh --config=/opt/quantmesh/config.yaml --check-config
   
   # 检查端口占用
   sudo lsof -i :28888
   ```

2. **WebSocket 连接失败**
   ```bash
   # 检查交易所 API 状态
   curl https://api.binance.com/api/v3/ping
   
   # 检查网络连接
   ping api.binance.com
   
   # 检查代理设置
   echo $HTTPS_PROXY
   ```

3. **订单失败率高**
   ```bash
   # 查看 API 限流情况
   curl http://localhost:28888/metrics | grep rate_limit
   
   # 检查账户余额
   # 通过 Web 界面或 API 查看
   ```

### 日志分析

```bash
# 查看错误日志
sudo journalctl -u quantmesh -p err

# 查看最近的日志
sudo journalctl -u quantmesh --since "1 hour ago"

# 实时查看日志
sudo journalctl -u quantmesh -f

# 导出日志
sudo journalctl -u quantmesh --since "2025-01-01" > quantmesh.log
```

## 升级和回滚

### 升级流程

```bash
# 1. 备份当前版本
sudo cp /opt/quantmesh/quantmesh /opt/quantmesh/quantmesh.backup
sudo ./scripts/backup.sh

# 2. 停止服务
sudo systemctl stop quantmesh

# 3. 替换二进制文件
sudo cp quantmesh /opt/quantmesh/

# 4. 检查配置兼容性
/opt/quantmesh/quantmesh --config=/opt/quantmesh/config.yaml --check-config

# 5. 启动服务
sudo systemctl start quantmesh

# 6. 验证
sudo systemctl status quantmesh
curl http://localhost:28888/api/status
```

### 回滚流程

```bash
# 1. 停止服务
sudo systemctl stop quantmesh

# 2. 恢复旧版本
sudo cp /opt/quantmesh/quantmesh.backup /opt/quantmesh/quantmesh

# 3. 恢复配置（如果需要）
sudo ./scripts/restore.sh ./backups/latest.tar.gz

# 4. 启动服务
sudo systemctl start quantmesh
```

## 灾难恢复

参考 [BACKUP_RECOVERY.md](BACKUP_RECOVERY.md)

### 快速恢复步骤

1. 准备新服务器
2. 安装依赖
3. 恢复备份
4. 配置服务
5. 启动系统
6. 验证功能

预期 RTO（恢复时间目标）：< 30 分钟

## 维护清单

### 每日检查

- [ ] 检查服务状态
- [ ] 查看告警
- [ ] 检查交易统计
- [ ] 查看系统资源使用

### 每周检查

- [ ] 查看备份状态
- [ ] 检查磁盘空间
- [ ] 查看日志异常
- [ ] 更新监控 Dashboard

### 每月检查

- [ ] 系统安全更新
- [ ] 证书有效期
- [ ] 备份恢复演练
- [ ] 性能分析和优化
- [ ] 清理旧数据

## 联系支持

如遇到问题，请联系：

- 技术支持: support@quantmesh.com
- 文档: https://docs.quantmesh.com
- GitHub Issues: https://github.com/quantmesh/quantmesh/issues

