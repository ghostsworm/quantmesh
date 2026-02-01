# 日志分析工具使用指南

## 概述

我们提供了两个日志分析工具，帮助你快速按级别、时间、关键词过滤和分析日志：

1. **Shell 版本** (`scripts/analyze_logs.sh`) - 轻量级，快速查看
2. **Python 版本** (`scripts/log_analyzer.py`) - 功能强大，支持统计分析

## 工具 1: Shell 版本 (analyze_logs.sh)

### 特点
- ✅ 快速启动，无需 Python
- ✅ 彩色输出，易于阅读
- ✅ 支持实时跟踪 (tail -f)
- ✅ 按级别过滤

### 基本用法

```bash
# 查看今天的所有错误
./scripts/analyze_logs.sh ERROR

# 查看指定日期的警告
./scripts/analyze_logs.sh WARN 2026-01-20

# 查看最后 50 条错误
./scripts/analyze_logs.sh ERROR --tail 50

# 实时跟踪错误日志 (类似 tail -f)
./scripts/analyze_logs.sh ERROR --follow

# 显示今天的日志统计
./scripts/analyze_logs.sh --stats

# 显示帮助
./scripts/analyze_logs.sh --help
```

### 参数说明

| 参数 | 说明 | 示例 |
|------|------|------|
| `level` | 日志级别: ERROR, WARN, INFO, DEBUG | `ERROR` |
| `date` | 日期 (YYYY-MM-DD) | `2026-01-20` |
| `--tail N` | 显示最后 N 行 | `--tail 50` |
| `--follow` 或 `-f` | 实时跟踪日志 | `-f` |
| `--stats` 或 `-s` | 显示统计信息 | `-s` |

## 工具 2: Python 版本 (log_analyzer.py)

### 特点
- ✅ 强大的统计分析
- ✅ 关键词搜索
- ✅ 时间段报告
- ✅ Top N 错误/警告统计
- ✅ 支持导出

### 基本用法

```bash
# 列出所有可用的日志日期
python3 scripts/log_analyzer.py --list

# 查看今天的统计信息
python3 scripts/log_analyzer.py --stats

# 查看指定日期的统计
python3 scripts/log_analyzer.py --stats --date 2026-01-20

# 查看今天的所有错误
python3 scripts/log_analyzer.py --level ERROR

# 查看指定日期的警告
python3 scripts/log_analyzer.py --level WARN --date 2026-01-20

# 搜索包含"保证金"的日志
python3 scripts/log_analyzer.py --keyword "保证金"

# 搜索包含"订单"的错误日志
python3 scripts/log_analyzer.py --level ERROR --keyword "订单"

# 显示最后 20 条错误
python3 scripts/log_analyzer.py --level ERROR --tail 20

# 生成时间段报告 (过去7天)
python3 scripts/log_analyzer.py --report --from 2026-01-15 --to 2026-01-21
```

### 高级功能

#### 1. 统计报告

```bash
python3 scripts/log_analyzer.py --stats --date 2026-01-20
```

输出示例：
```
============================================================
日志统计报告 - 2026-01-20
============================================================

📊 总日志数: 15234

按级别统计:
  ERROR   :    125 ( 0.82%)
  WARN    :    456 ( 2.99%)
  INFO    :  12453 (81.75%)
  DEBUG   :   2200 (14.44%)

按文件统计:
  📄 app-quantmesh-2026-01-20.log
    ERROR   : 98
    WARN    : 345
    INFO    : 10234
    DEBUG   : 1890

  📄 web-gin-2026-01-20.log
    ERROR   : 27
    WARN    : 111
    INFO    : 2219
    DEBUG   : 310

🔴 最常见的错误 (Top 10):
  1. [45次] WebSocket 连接断开，准备重连...
  2. [23次] 获取持仓信息失败: context deadline exceeded
  3. [12次] 订单提交失败: insufficient margin
  ...

⚠️  最常见的警告 (Top 10):
  1. [89次] [价格解析异常] ClientOrderID=xxx, 解析价格=xxx
  2. [67次] [保证金不足] 检测到保证金不足错误
  ...
```

#### 2. 关键词搜索

```bash
# 搜索所有包含"重启"的日志
python3 scripts/log_analyzer.py --keyword "重启"

# 搜索包含"WebSocket"的错误
python3 scripts/log_analyzer.py --level ERROR --keyword "WebSocket"

# 搜索包含"持仓恢复"的INFO日志
python3 scripts/log_analyzer.py --level INFO --keyword "持仓恢复"
```

#### 3. 时间段分析

```bash
# 分析过去一周的日志
python3 scripts/log_analyzer.py --report \
  --from 2026-01-15 \
  --to 2026-01-21
```

输出示例：
```
============================================================
时间段报告: 2026-01-15 至 2026-01-21
============================================================

📅 覆盖日期: 2026-01-15, 2026-01-16, ..., 2026-01-21
📊 总日志数: 89234

按级别汇总:
  ERROR   :    567
  WARN    :   2345
  INFO    :  78234
  DEBUG   :   8088
============================================================
```

## 实战场景

### 场景 1: 检查程序重启后的订单恢复

```bash
# 1. 查看最近的INFO日志，确认持仓恢复
python3 scripts/log_analyzer.py --level INFO --keyword "持仓恢复" --tail 20

# 2. 查看是否有订单初始化
python3 scripts/log_analyzer.py --level INFO --keyword "订单初始化" --tail 10

# 3. 检查是否有错误
python3 scripts/log_analyzer.py --level ERROR --tail 50
```

### 场景 2: 分析保证金不足问题

```bash
# 1. 搜索所有保证金相关的警告
python3 scripts/log_analyzer.py --level WARN --keyword "保证金"

# 2. 查看保证金不足的错误
python3 scripts/log_analyzer.py --level ERROR --keyword "margin"

# 3. 查看统计，了解问题频率
python3 scripts/log_analyzer.py --stats
```

### 场景 3: 监控 WebSocket 连接稳定性

```bash
# 1. 搜索所有 WebSocket 相关的错误
python3 scripts/log_analyzer.py --level ERROR --keyword "WebSocket"

# 2. 搜索重连日志
python3 scripts/log_analyzer.py --keyword "重连"

# 3. 实时监控 WebSocket 问题 (Shell版本)
./scripts/analyze_logs.sh ERROR --follow | grep -i websocket
```

### 场景 4: 检查订单成交和价差

```bash
# 1. 查看买单成交
python3 scripts/log_analyzer.py --keyword "买单成交" --tail 20

# 2. 查看卖单成交
python3 scripts/log_analyzer.py --keyword "卖单成交" --tail 20

# 3. 查看价差监控
python3 scripts/log_analyzer.py --keyword "价差" --tail 30
```

### 场景 5: 每日运维检查

```bash
#!/bin/bash
# 每日运维检查脚本

echo "=== 每日日志检查 ==="
echo ""

# 1. 显示统计
echo "1. 日志统计:"
python3 scripts/log_analyzer.py --stats

echo ""
echo "2. 最近的错误 (Top 10):"
python3 scripts/log_analyzer.py --level ERROR --tail 10

echo ""
echo "3. 保证金相关警告:"
python3 scripts/log_analyzer.py --level WARN --keyword "保证金" --tail 5

echo ""
echo "=== 检查完成 ==="
```

## 日志级别说明

| 级别 | 用途 | 示例 |
|------|------|------|
| **ERROR** | 严重错误，需要立即关注 | 订单提交失败、WebSocket 断开、数据库错误 |
| **WARN** | 警告信息，可能影响功能 | 保证金不足、价格解析异常、对账不一致 |
| **INFO** | 正常运行信息 | 订单成交、持仓恢复、系统启动 |
| **DEBUG** | 调试信息，详细日志 | 槽位状态变化、订单状态更新 |

## 日志文件位置

```
./logs/
├── app-quantmesh-2026-01-21.log    # 应用主日志
├── web-gin-2026-01-21.log          # Web API 日志
├── app-quantmesh-2026-01-20.log
├── web-gin-2026-01-20.log
└── ...
```

## 最佳实践

### 1. 定期检查错误日志

```bash
# 添加到 crontab，每小时检查一次错误
0 * * * * cd /path/to/quantmesh && python3 scripts/log_analyzer.py --level ERROR --tail 10 >> /tmp/error_check.log
```

### 2. 重启后验证

```bash
# 程序重启后，运行这个脚本验证
cat > check_after_restart.sh << 'EOF'
#!/bin/bash
echo "检查程序重启后的状态..."
python3 scripts/log_analyzer.py --level INFO --keyword "持仓恢复" --tail 5
python3 scripts/log_analyzer.py --level INFO --keyword "订单初始化" --tail 5
python3 scripts/log_analyzer.py --level ERROR --tail 10
EOF

chmod +x check_after_restart.sh
./check_after_restart.sh
```

### 3. 生成每日报告

```bash
# 每日生成报告
cat > daily_report.sh << 'EOF'
#!/bin/bash
DATE=$(date -d "yesterday" +%Y-%m-%d)
python3 scripts/log_analyzer.py --stats --date $DATE > daily_report_$DATE.txt
echo "报告已生成: daily_report_$DATE.txt"
EOF

chmod +x daily_report.sh
```

## 性能考虑

- **Shell 版本**: 适合快速查看，处理小文件（< 100MB）
- **Python 版本**: 适合统计分析，处理大文件（> 100MB）

## 故障排查

### 问题 1: 找不到日志文件

```bash
# 检查日志目录
ls -lh ./logs/

# 使用 --list 查看可用日期
python3 scripts/log_analyzer.py --list
```

### 问题 2: Python 版本不兼容

```bash
# 检查 Python 版本 (需要 3.6+)
python3 --version

# 如果版本过低，使用 Shell 版本
./scripts/analyze_logs.sh --stats
```

### 问题 3: 权限问题

```bash
# 设置脚本为可执行
chmod +x scripts/analyze_logs.sh
chmod +x scripts/log_analyzer.py
```

## 扩展功能

### 导出为 JSON

```bash
# TODO: 将来可以添加 JSON 导出功能
python3 scripts/log_analyzer.py --stats --output-json stats.json
```

### 集成到监控系统

```bash
# TODO: 可以集成到 Prometheus/Grafana
# 定期导出错误计数到 metrics
```

## 相关文档

- [日志系统架构](./logging-architecture.md)
- [监控和告警](./monitoring-guide.md)
- [故障排查指南](./troubleshooting.md)
