#!/bin/bash

# 统计数据诊断脚本
# 用于快速检查为什么统计数据显示为零

echo "================================"
echo "QuantMesh 统计数据诊断工具"
echo "================================"
echo ""

# 数据库文件路径
DB_FILE="data/quantmesh.db"

# 检查数据库文件是否存在
if [ ! -f "$DB_FILE" ]; then
    echo "❌ 错误: 数据库文件不存在: $DB_FILE"
    echo "   请确保系统已经运行过至少一次"
    exit 1
fi

echo "✅ 数据库文件存在: $DB_FILE"
echo ""

# 1. 检查 trades 表是否存在
echo "📊 检查 trades 表..."
TABLE_EXISTS=$(sqlite3 "$DB_FILE" "SELECT name FROM sqlite_master WHERE type='table' AND name='trades';" 2>/dev/null)
if [ -z "$TABLE_EXISTS" ]; then
    echo "❌ trades 表不存在！"
    echo "   系统可能没有正确初始化数据库"
    exit 1
fi
echo "✅ trades 表存在"
echo ""

# 2. 查询交易记录总数
echo "📈 查询交易记录..."
TOTAL_TRADES=$(sqlite3 "$DB_FILE" "SELECT COUNT(*) FROM trades;" 2>/dev/null)
echo "   总交易数: $TOTAL_TRADES"

if [ "$TOTAL_TRADES" -eq 0 ]; then
    echo ""
    echo "⚠️  没有找到任何交易记录！"
    echo ""
    echo "可能的原因："
    echo "  1. 系统刚启动，还没有完成任何交易"
    echo "  2. 价格波动不够，没有触发交易"
    echo "  3. 交易记录保存失败"
    echo ""
    echo "建议："
    echo "  - 检查系统日志: tail -f logs/quantmesh.log | grep '交易记录'"
    echo "  - 确认交易已启用: cat config.yaml | grep 'enabled'"
    echo "  - 等待价格波动触发交易"
    echo ""
    exit 0
fi

echo ""

# 3. 显示统计汇总
echo "💰 统计汇总:"
sqlite3 "$DB_FILE" "
SELECT 
  '总交易数: ' || COUNT(*) as total_trades,
  '总交易量: ' || ROUND(SUM(quantity), 4) as total_volume,
  '总盈亏: ' || ROUND(SUM(pnl), 2) as total_pnl,
  '胜率: ' || ROUND(CAST(SUM(CASE WHEN pnl > 0 THEN 1 ELSE 0 END) AS FLOAT) / COUNT(*) * 100, 2) || '%' as win_rate
FROM trades;
" | sed 's/|/\n   /g'

echo ""

# 4. 显示最近的交易记录
echo "📋 最近 5 笔交易:"
sqlite3 -header -column "$DB_FILE" "
SELECT 
  datetime(created_at, 'localtime') as time,
  exchange,
  symbol,
  ROUND(buy_price, 2) as buy,
  ROUND(sell_price, 2) as sell,
  ROUND(quantity, 4) as qty,
  ROUND(pnl, 2) as pnl
FROM trades 
ORDER BY created_at DESC 
LIMIT 5;
"

echo ""

# 5. 按日期统计
echo "📅 按日期统计（最近7天）:"
sqlite3 -header -column "$DB_FILE" "
SELECT 
  date(created_at) as date,
  COUNT(*) as trades,
  ROUND(SUM(quantity), 4) as volume,
  ROUND(SUM(pnl), 2) as pnl
FROM trades 
WHERE date(created_at) >= date('now', '-7 days')
GROUP BY date(created_at)
ORDER BY date DESC;
"

echo ""

# 6. 按交易对统计
echo "💱 按交易对统计:"
sqlite3 -header -column "$DB_FILE" "
SELECT 
  symbol,
  COUNT(*) as trades,
  ROUND(SUM(quantity), 4) as volume,
  ROUND(SUM(pnl), 2) as pnl,
  ROUND(CAST(SUM(CASE WHEN pnl > 0 THEN 1 ELSE 0 END) AS FLOAT) / COUNT(*) * 100, 2) || '%' as win_rate
FROM trades 
GROUP BY symbol
ORDER BY trades DESC;
"

echo ""
echo "================================"
echo "✅ 诊断完成"
echo "================================"
echo ""
echo "如果统计数据正常但前端显示为零，可能是："
echo "  1. 账户隔离问题 - 查询的账户与实际交易的账户不匹配"
echo "  2. API 缓存问题 - 尝试刷新页面"
echo "  3. 前端过滤问题 - 检查是否选择了特定的交易所/交易对"
echo ""
