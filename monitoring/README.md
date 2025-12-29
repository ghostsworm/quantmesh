# QuantMesh 监控系统

本目录包含 QuantMesh 做市商系统的完整监控方案，基于 Prometheus + Grafana + AlertManager。

## 快速开始

### 1. 启动监控栈

```bash
# 在项目根目录执行
docker-compose -f docker-compose.monitoring.yml up -d
```

### 2. 访问监控界面

- **Grafana**: http://localhost:3000
  - 默认用户名/密码: `admin/admin`
  - 首次登录后会提示修改密码
  
- **Prometheus**: http://localhost:9090
  - 可以查看原始指标和执行 PromQL 查询
  
- **AlertManager**: http://localhost:9093
  - 查看和管理告警

### 3. 配置 Telegram 告警（可选）

编辑 `alertmanager/alertmanager.yml`，替换以下内容：

```yaml
- bot_token: 'YOUR_BOT_TOKEN'  # 替换为你的 Telegram Bot Token
  chat_id: YOUR_CHAT_ID         # 替换为你的 Chat ID
```

获取 Bot Token 和 Chat ID 的方法：
1. 与 @BotFather 对话创建 Bot，获取 Token
2. 与你的 Bot 对话，发送任意消息
3. 访问 `https://api.telegram.org/bot<YOUR_BOT_TOKEN>/getUpdates` 获取 Chat ID

重启 AlertManager 使配置生效：
```bash
docker-compose -f docker-compose.monitoring.yml restart alertmanager
```

## 监控指标说明

### 订单指标
- `quantmesh_order_total` - 订单总数（按状态分类）
- `quantmesh_order_success_total` - 成功订单数
- `quantmesh_order_failure_total` - 失败订单数（按原因分类）
- `quantmesh_order_duration_seconds` - 订单执行时长（直方图）

### 交易指标
- `quantmesh_trade_volume_total` - 交易量（基础货币）
- `quantmesh_trade_amount_total` - 交易额（计价货币）
- `quantmesh_trade_count_total` - 交易次数

### 盈亏指标
- `quantmesh_pnl_total` - 总盈亏
- `quantmesh_pnl_realized_total` - 已实现盈亏
- `quantmesh_win_rate` - 胜率

### 风控指标
- `quantmesh_risk_control_triggered` - 风控状态（0=正常, 1=触发）
- `quantmesh_risk_control_trigger_count_total` - 风控触发次数
- `quantmesh_margin_usage_ratio` - 保证金使用率
- `quantmesh_position_risk` - 持仓风险评分

### 系统指标
- `quantmesh_goroutine_count` - Goroutine 数量
- `quantmesh_memory_alloc_bytes` - 内存分配
- `quantmesh_gc_pause_duration_seconds` - GC 停顿时间

### 交易所指标
- `quantmesh_websocket_connected` - WebSocket 连接状态
- `quantmesh_websocket_reconnect_count_total` - WebSocket 重连次数
- `quantmesh_api_call_total` - API 调用总数
- `quantmesh_api_call_duration_seconds` - API 调用时长
- `quantmesh_api_rate_limit_hit_total` - API 限流次数

### 价格指标
- `quantmesh_current_price` - 当前价格
- `quantmesh_price_update_count_total` - 价格更新次数

## 告警规则

### P0 级别（立即处理）
- `RiskControlTriggered` - 风控系统触发
- `WebSocketDisconnected` - WebSocket 断开连接
- `NoOrdersPlaced` - 系统停止下单
- `QuantMeshDown` - 服务不可用

### P1 级别（30分钟内处理）
- `LowOrderSuccessRate` - 订单成功率低于 95%
- `HighAPIRateLimitHits` - API 限流频繁触发
- `HighOrderLatency` - 订单延迟过高

### P2 级别（2小时内处理）
- `HighGoroutineCount` - Goroutine 数量过高
- `HighMemoryUsage` - 内存使用过高
- `FrequentWebSocketReconnects` - WebSocket 频繁重连
- `HighReconciliationDiffs` - 对账差异频繁出现

## Grafana Dashboard

系统预配置了以下 Dashboard：

1. **QuantMesh 系统总览** (`quantmesh-overview`)
   - 订单速率和成功率
   - 订单执行延迟（P50/P95/P99）
   - 风控状态
   - Goroutine 数量

更多 Dashboard 可以通过 Grafana UI 创建和导入。

## 常用 PromQL 查询

### 订单成功率（过去 5 分钟）
```promql
sum(rate(quantmesh_order_success_total[5m])) / sum(rate(quantmesh_order_total[5m]))
```

### 订单延迟 P95
```promql
histogram_quantile(0.95, rate(quantmesh_order_duration_seconds_bucket[5m]))
```

### 每秒订单数
```promql
sum(rate(quantmesh_order_total[1m]))
```

### 风控触发次数（过去 1 小时）
```promql
sum(increase(quantmesh_risk_control_trigger_count_total[1h]))
```

### WebSocket 连接状态
```promql
quantmesh_websocket_connected
```

## 故障排查

### 监控服务无法启动
```bash
# 查看日志
docker-compose -f docker-compose.monitoring.yml logs

# 检查端口占用
lsof -i :9090  # Prometheus
lsof -i :3000  # Grafana
lsof -i :9093  # AlertManager
```

### QuantMesh 指标无法抓取
1. 确认 QuantMesh 服务正在运行
2. 访问 http://localhost:28888/metrics 检查指标端点
3. 检查 `prometheus/prometheus.yml` 中的 targets 配置
4. 在 Prometheus UI 中查看 Targets 状态

### 告警未发送
1. 检查 AlertManager 配置是否正确
2. 在 AlertManager UI 中查看告警状态
3. 检查 Telegram Bot Token 和 Chat ID 是否正确
4. 查看 AlertManager 日志：`docker logs quantmesh_alertmanager`

## 停止监控服务

```bash
docker-compose -f docker-compose.monitoring.yml down

# 同时删除数据卷
docker-compose -f docker-compose.monitoring.yml down -v
```

## 数据持久化

监控数据存储在 Docker 卷中：
- `prometheus_data` - Prometheus 时序数据
- `alertmanager_data` - AlertManager 数据
- `grafana_data` - Grafana 配置和 Dashboard

备份数据：
```bash
docker run --rm -v quantmesh_prometheus_data:/data -v $(pwd):/backup alpine tar czf /backup/prometheus-backup.tar.gz /data
```

## 性能调优

### Prometheus 数据保留期
默认保留 15 天，可在 `prometheus.yml` 中修改：
```yaml
command:
  - '--storage.tsdb.retention.time=30d'
```

### 采集间隔
默认 15 秒，可根据需要调整：
```yaml
global:
  scrape_interval: 30s  # 降低频率以减少资源消耗
```

## 扩展阅读

- [Prometheus 官方文档](https://prometheus.io/docs/)
- [Grafana 官方文档](https://grafana.com/docs/)
- [AlertManager 官方文档](https://prometheus.io/docs/alerting/latest/alertmanager/)

