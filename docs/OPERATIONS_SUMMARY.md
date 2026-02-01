# 运维体系升级总结

本文档总结了 QuantMesh 做市商系统的运维体系升级内容。

## 升级概览

本次升级建立了完整的生产级运维体系，涵盖监控、日志、告警、备份、安全和自动化部署等方面。

## 已完成的改进

### ✅ 1. 可观测性体系

#### 1.1 Prometheus + Grafana 监控

**实现内容：**
- 集成 Prometheus 客户端库
- 暴露 `/metrics` 端点
- 实现核心业务指标采集
- 配置 Grafana Dashboard
- 部署 AlertManager

**核心指标：**
- 订单指标：成功率、延迟、失败原因
- 交易指标：成交量、成交额、买卖比例
- 盈亏指标：实时 PnL、胜率、最大回撤
- 风控指标：风控状态、触发次数、保证金使用率
- 系统指标：Goroutine、内存、GC 停顿
- 交易所指标：WebSocket 状态、API 延迟、限流次数

**文件位置：**
- `metrics/prometheus.go` - Prometheus 指标定义
- `metrics/system_collector.go` - 系统指标采集器
- `monitoring/docker-compose.monitoring.yml` - 监控栈部署
- `monitoring/prometheus/` - Prometheus 配置
- `monitoring/grafana/` - Grafana 配置和 Dashboard
- `monitoring/README.md` - 监控系统使用文档

#### 1.2 性能分析工具

**实现内容：**
- 暴露 pprof 端点（`/debug/pprof/`）
- 支持 CPU、内存、Goroutine、阻塞、互斥锁分析
- 提供详细的性能分析指南

**文件位置：**
- `web/server.go` - pprof 路由配置
- `docs/PPROF_GUIDE.md` - pprof 使用指南

### ✅ 2. 告警系统

**实现内容：**
- 多层次告警体系（P0/P1/P2/P3）
- Prometheus 告警规则
- AlertManager 配置
- 告警聚合和降噪
- Telegram 通知集成

**告警级别：**
- **P0（立即处理）**: 风控触发、WebSocket 断连、系统停止下单
- **P1（30分钟内）**: 订单成功率低、API 限流、订单延迟高
- **P2（2小时内）**: Goroutine 泄漏、内存过高、频繁重连

**文件位置：**
- `monitoring/prometheus/alerts.yml` - 告警规则
- `monitoring/alertmanager/alertmanager.yml` - AlertManager 配置

### ✅ 3. 数据备份与灾难恢复

**实现内容：**
- 自动化备份脚本
- 数据恢复脚本
- 备份完整性校验
- 云存储上传支持
- 定时备份配置

**备份内容：**
- 数据库文件（quantmesh.db, auth.db, webauthn.db, logs.db）
- 配置文件（config.yaml, config_backups/）
- 日志文件（最近 7 天）

**文件位置：**
- `scripts/backup.sh` - 备份脚本
- `scripts/restore.sh` - 恢复脚本
- `docs/BACKUP_RECOVERY.md` - 备份恢复文档

### ✅ 4. 安全加固

**实现内容：**
- Nginx 反向代理配置
- SSL/TLS 配置（Let's Encrypt）
- Rate Limiting（速率限制）
- IP 白名单
- 安全响应头
- systemd 服务配置
- 生产环境部署指南

**安全措施：**
- HTTPS 强制跳转
- HSTS 配置
- pprof/metrics 端点访问限制
- 防暴力破解（登录接口限流）
- 最小权限原则

**文件位置：**
- `docs/NGINX_CONFIG.md` - Nginx 配置指南
- `scripts/quantmesh.service` - systemd 服务文件
- `docs/PRODUCTION_DEPLOYMENT.md` - 生产部署指南

### ✅ 5. CI/CD 流水线

**实现内容：**
- GitHub Actions CI 工作流
- GitHub Actions CD 工作流
- Docker 镜像构建
- 多平台构建支持
- 自动化测试和代码检查
- 自动发布和部署

**CI 流程：**
1. 代码检查（go vet, go fmt, golangci-lint）
2. 安全扫描（gosec）
3. 单元测试（覆盖率报告）
4. 多平台构建
5. Docker 镜像构建

**CD 流程：**
1. 创建 GitHub Release
2. 构建发布资产
3. 部署到 Staging
4. 部署到 Production
5. 健康检查和自动回滚

**文件位置：**
- `.github/workflows/ci.yml` - CI 工作流
- `.github/workflows/cd.yml` - CD 工作流
- `Dockerfile` - Docker 镜像构建
- `.dockerignore` - Docker 构建排除文件
- `docs/CICD_GUIDE.md` - CI/CD 使用指南

## 快速开始

### 1. 启动监控系统

```bash
# 启动 Prometheus + Grafana + AlertManager
docker-compose -f monitoring/docker-compose.monitoring.yml up -d

# 访问 Grafana
open http://localhost:3000
# 默认用户名/密码: admin/admin
```

### 2. 配置自动备份

```bash
# 编辑 crontab
crontab -e

# 添加每小时备份任务
0 * * * * cd /path/to/quantmesh && ./scripts/backup.sh >> /var/log/quantmesh-backup.log 2>&1
```

### 3. 配置 Nginx 反向代理

```bash
# 复制配置文件
sudo cp docs/nginx-example.conf /etc/nginx/sites-available/quantmesh

# 创建符号链接
sudo ln -s /etc/nginx/sites-available/quantmesh /etc/nginx/sites-enabled/

# 测试配置
sudo nginx -t

# 重载 Nginx
sudo nginx -s reload
```

### 4. 配置 systemd 服务

```bash
# 复制服务文件
sudo cp scripts/quantmesh.service /etc/systemd/system/

# 重载 systemd
sudo systemctl daemon-reload

# 启用并启动服务
sudo systemctl enable quantmesh
sudo systemctl start quantmesh
```

## 监控指标访问

- **Prometheus**: http://localhost:9090
- **Grafana**: http://localhost:3000
- **AlertManager**: http://localhost:9093
- **QuantMesh Metrics**: http://localhost:28888/metrics
- **pprof**: http://localhost:28888/debug/pprof/

## 告警通知配置

编辑 `monitoring/alertmanager/alertmanager.yml`，配置 Telegram Bot：

```yaml
telegram_configs:
  - bot_token: 'YOUR_BOT_TOKEN'
    chat_id: YOUR_CHAT_ID
```

## 性能分析

### CPU 分析

```bash
# 采集 30 秒 CPU profile
go tool pprof http://localhost:28888/debug/pprof/profile?seconds=30

# 在 pprof 交互模式中
(pprof) top10
(pprof) web
```

### 内存分析

```bash
# 采集堆内存快照
go tool pprof http://localhost:28888/debug/pprof/heap

# 对比两个时间点
curl -o heap1.prof http://localhost:28888/debug/pprof/heap
# 等待一段时间
curl -o heap2.prof http://localhost:28888/debug/pprof/heap
go tool pprof -base=heap1.prof heap2.prof
```

## 备份和恢复

### 手动备份

```bash
# 执行备份
./scripts/backup.sh

# 查看备份
ls -lh backups/
```

### 恢复数据

```bash
# 列出可用备份
ls -lh backups/*.tar.gz

# 恢复指定备份
./scripts/restore.sh backups/20250129_143022.tar.gz
```

## 部署流程

### 开发环境

```bash
# 克隆代码
git clone <repository>
cd quantmesh

# 安装依赖
go mod download

# 运行
go run .
```

### 生产环境

```bash
# 1. 构建
go build -ldflags="-s -w" -o quantmesh .

# 2. 部署到服务器
scp quantmesh user@server:/opt/quantmesh/

# 3. 配置 systemd
sudo systemctl restart quantmesh

# 4. 验证
curl http://localhost:28888/api/status
```

### Docker 部署

```bash
# 构建镜像
docker build -t quantmesh/market-maker:latest .

# 运行容器
docker run -d \
  --name quantmesh \
  -p 28888:28888 \
  -v $(pwd)/config.yaml:/app/config.yaml \
  -v $(pwd)/data:/app/data \
  quantmesh/market-maker:latest
```

## 故障排查

### 服务无法启动

```bash
# 查看日志
sudo journalctl -u quantmesh -n 100

# 检查配置
./quantmesh --check-config

# 检查端口占用
sudo lsof -i :28888
```

### 监控数据缺失

```bash
# 检查 Prometheus targets
open http://localhost:9090/targets

# 检查 metrics 端点
curl http://localhost:28888/metrics

# 查看 Prometheus 日志
docker logs quantmesh_prometheus
```

### 告警未发送

```bash
# 检查 AlertManager 状态
open http://localhost:9093

# 查看 AlertManager 日志
docker logs quantmesh_alertmanager

# 测试 Telegram 通知
curl -X POST "https://api.telegram.org/bot<TOKEN>/sendMessage" \
  -d "chat_id=<CHAT_ID>&text=Test"
```

## 性能优化建议

### 系统层面

1. **增加文件描述符限制**
   ```bash
   ulimit -n 65536
   ```

2. **优化 TCP 参数**
   ```bash
   sudo sysctl -w net.core.somaxconn=1024
   sudo sysctl -w net.ipv4.tcp_max_syn_backlog=2048
   ```

### 应用层面

1. **使用对象池减少内存分配**
2. **优化数据库查询**
3. **启用 HTTP/2**
4. **使用连接池**

### 监控层面

1. **调整 Prometheus 采集间隔**
2. **配置数据保留期**
3. **使用 Recording Rules 预聚合**

## 安全检查清单

- [ ] 启用 HTTPS
- [ ] 配置防火墙
- [ ] 设置 IP 白名单
- [ ] 限制 pprof 访问
- [ ] 配置 Rate Limiting
- [ ] 定期更新系统
- [ ] 启用自动备份
- [ ] 配置告警通知
- [ ] 定期安全审计
- [ ] 密钥定期轮换

## 维护计划

### 每日

- 检查服务状态
- 查看告警
- 检查交易统计

### 每周

- 查看备份状态
- 检查磁盘空间
- 查看性能指标

### 每月

- 系统安全更新
- 备份恢复演练
- 性能分析优化
- 清理旧数据

## 文档索引

### 监控和告警
- [监控系统使用指南](../monitoring/README.md)
- [Prometheus 指标说明](../monitoring/README.md#监控指标说明)
- [Grafana Dashboard](../monitoring/grafana/dashboards/)
- [告警规则配置](../monitoring/prometheus/alerts.yml)

### 性能分析
- [pprof 使用指南](PPROF_GUIDE.md)

### 备份恢复
- [备份恢复指南](BACKUP_RECOVERY.md)

### 安全和部署
- [Nginx 配置指南](NGINX_CONFIG.md)
- [生产环境部署](PRODUCTION_DEPLOYMENT.md)

### CI/CD
- [CI/CD 指南](CICD_GUIDE.md)

## 预期收益

### 可量化指标

- **故障发现时间**: 从分钟级降低到秒级
- **故障恢复时间**: 从小时级降低到分钟级
- **系统可用性**: 从 99% 提升到 99.9%
- **问题定位效率**: 提升 10 倍
- **部署时间**: 从 30 分钟降低到 5 分钟

### 业务价值

- 减少因系统故障导致的交易损失
- 提升运维团队效率
- 增强系统可扩展性
- 满足合规审计要求
- 提高系统稳定性和可靠性

## 后续规划

### Phase 6: 结构化日志（可选）

- 引入 zap 或 zerolog
- 部署 Loki 或 ELK Stack
- 统一日志格式
- 日志聚合和检索

### Phase 7: 分布式追踪（可选）

- 集成 OpenTelemetry
- 部署 Jaeger 或 Tempo
- 追踪订单全链路
- 性能瓶颈分析

### Phase 8: 高可用架构（可选）

- 多实例部署
- 配置中心（etcd/Consul）
- 分布式锁
- 负载均衡

## 联系和支持

如有问题或建议，请：

1. 查阅相关文档
2. 查看 GitHub Issues
3. 联系技术支持

---

**文档版本**: 1.0  
**最后更新**: 2025-01-29  
**维护者**: QuantMesh Team

