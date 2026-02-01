# 日志分析工具快速参考

## 🚀 快速开始

### 最常用的命令

```bash
# 1. 查看今天的日志统计
./scripts/analyze_logs.sh --stats

# 2. 查看今天的错误（最后20条）
./scripts/analyze_logs.sh ERROR --tail 20

# 3. 实时监控错误
./scripts/analyze_logs.sh ERROR --follow

# 4. 查看昨天的警告
./scripts/analyze_logs.sh WARN $(date -d "yesterday" +%Y-%m-%d)

# 5. 列出可用的日志日期
python3 scripts/log_analyzer.py --list
```

## 📋 工具对比

| 功能 | Shell 版本 | Python 版本 |
|------|-----------|------------|
| **速度** | ⚡️ 极快 | 🐌 较慢（大文件）|
| **统计分析** | ✅ 基础统计 | ✅ 详细统计 + Top N |
| **实时跟踪** | ✅ 支持 | ❌ 不支持 |
| **关键词搜索** | ❌ 不支持 | ✅ 支持 |
| **时间段报告** | ❌ 不支持 | ✅ 支持 |
| **依赖** | 无 | Python 3.6+ |

**建议**：
- 日常查看 → 使用 Shell 版本
- 深度分析 → 使用 Python 版本

## 🎯 常见场景

### 场景1: 检查程序是否正常运行

```bash
# 查看最近的错误
./scripts/analyze_logs.sh ERROR --tail 20

# 如果没有错误，看看警告
./scripts/analyze_logs.sh WARN --tail 20

# 查看统计，了解整体情况
./scripts/analyze_logs.sh --stats
```

### 场景2: 程序重启后验证

```bash
# 检查持仓恢复
grep "持仓恢复" logs/app-quantmesh-$(date +%Y-%m-%d).log

# 检查订单初始化
grep "订单初始化" logs/app-quantmesh-$(date +%Y-%m-%d).log

# 检查最近的错误
./scripts/analyze_logs.sh ERROR --tail 10
```

### 场景3: 诊断保证金不足

```bash
# 统计保证金不足的错误次数
grep -c "Margin is insufficient" logs/app-quantmesh-$(date +%Y-%m-%d).log

# 查看保证金相关的警告
grep -i "margin\|保证金" logs/app-quantmesh-$(date +%Y-%m-%d).log | tail -20
```

### 场景4: 分析交易活动

```bash
# 查看买单成交
grep "买单成交" logs/app-quantmesh-$(date +%Y-%m-%d).log | tail -20

# 查看卖单成交
grep "卖单成交" logs/app-quantmesh-$(date +%Y-%m-%d).log | tail -20

# 统计今天的成交次数
echo "买单成交: $(grep -c '买单成交' logs/app-quantmesh-$(date +%Y-%m-%d).log)"
echo "卖单成交: $(grep -c '卖单成交' logs/app-quantmesh-$(date +%Y-%m-%d).log)"
```

### 场景5: 监控 WebSocket 连接

```bash
# 实时监控 WebSocket 问题
./scripts/analyze_logs.sh ERROR --follow | grep -i websocket

# 统计重连次数
grep -c "WebSocket.*重连" logs/app-quantmesh-$(date +%Y-%m-%d).log
```

## 💡 使用技巧

### 技巧1: 创建别名

在 `~/.bashrc` 或 `~/.zshrc` 中添加：

```bash
alias qm-logs-error='cd /path/to/quantmesh && ./scripts/analyze_logs.sh ERROR --tail 20'
alias qm-logs-stats='cd /path/to/quantmesh && ./scripts/analyze_logs.sh --stats'
alias qm-logs-watch='cd /path/to/quantmesh && ./scripts/analyze_logs.sh ERROR --follow'
```

### 技巧2: 定时检查脚本

```bash
#!/bin/bash
# save as: check_health.sh

cd /path/to/quantmesh

echo "=== QuantMesh 健康检查 ==="
echo ""

# 1. 统计
echo "📊 日志统计:"
./scripts/analyze_logs.sh --stats | grep -A 5 "app-quantmesh"

echo ""
echo "🚨 最近错误:"
./scripts/analyze_logs.sh ERROR --tail 5

echo ""
echo "⚠️  最近警告:"
./scripts/analyze_logs.sh WARN --tail 5
```

### 技巧3: 结合 grep 进行过滤

```bash
# 查看特定策略的错误
./scripts/analyze_logs.sh ERROR | grep "martingale"

# 查看特定交易对的日志
./scripts/analyze_logs.sh INFO | grep "BTCUSDT"

# 查看特定时间段的日志
grep "2026/01/13 14:" logs/app-quantmesh-2026-01-13.log | grep "\[ERROR\]"
```

## 📁 日志文件说明

| 文件 | 内容 | 大小 (典型) |
|------|------|------------|
| `app-quantmesh-YYYY-MM-DD.log` | 应用主日志 | 50-300MB |
| `web-gin-YYYY-MM-DD.log` | Web API 日志 | 1-10MB |

**日志保留策略**: 默认保留最近 30 天

## 🔧 高级用法

### 生成每日报告

```bash
#!/bin/bash
# daily_report.sh

DATE=$(date +%Y-%m-%d)
REPORT_FILE="report_${DATE}.txt"

{
    echo "========================================="
    echo "QuantMesh 日志报告 - ${DATE}"
    echo "========================================="
    echo ""
    
    ./scripts/analyze_logs.sh --stats
    
    echo ""
    echo "========================================="
    echo "最近的ERROR (Top 20)"
    echo "========================================="
    ./scripts/analyze_logs.sh ERROR --tail 20
    
} > "${REPORT_FILE}"

echo "报告已生成: ${REPORT_FILE}"
```

### 统计错误类型

```bash
#!/bin/bash
# error_stats.sh

echo "错误类型统计:"
echo ""

# 保证金不足
count=$(grep -c "Margin is insufficient" logs/app-quantmesh-$(date +%Y-%m-%d).log)
echo "保证金不足: ${count}"

# 下单金额过小
count=$(grep -c "notional must be no smaller" logs/app-quantmesh-$(date +%Y-%m-%d).log)
echo "下单金额过小: ${count}"

# WebSocket 错误
count=$(grep -c "WebSocket.*错误\|WebSocket.*失败" logs/app-quantmesh-$(date +%Y-%m-% d).log)
echo "WebSocket 错误: ${count}"
```

## 📚 相关文档

- [完整使用指南](../docs/log-analysis-guide.md)
- [故障排查](../docs/troubleshooting.md)
- [监控和告警](../docs/monitoring-guide.md)

## ❓ 常见问题

### Q: 日志文件太大，分析很慢怎么办？

A: 使用 `--tail` 参数只查看最后N行：
```bash
./scripts/analyze_logs.sh ERROR --tail 100
```

### Q: 如何查看多天的日志？

A: 使用循环：
```bash
for date in 2026-01-10 2026-01-11 2026-01-12; do
    echo "=== $date ==="
    ./scripts/analyze_logs.sh ERROR $date --tail 10
done
```

### Q: 如何导出日志？

A: 重定向输出：
```bash
./scripts/analyze_logs.sh ERROR 2026-01-13 > errors.txt
```

## 🆘 获取帮助

```bash
# Shell 版本帮助
./scripts/analyze_logs.sh --help

# Python 版本帮助
python3 scripts/log_analyzer.py --help
```
