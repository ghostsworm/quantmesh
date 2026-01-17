#!/bin/bash

# 测试持仓 API

echo "=== 测试 1: 不带参数（应该返回默认交易对 ETHUSDT）==="
curl -s "http://localhost:28888/api/positions" | jq '._debug'

echo ""
echo "=== 测试 2: 指定 BCHUSDT ==="
curl -s "http://localhost:28888/api/positions?exchange=binance&symbol=BCHUSDT" | jq '._debug'

echo ""
echo "=== 测试 3: 指定 ETHUSDT ==="
curl -s "http://localhost:28888/api/positions?exchange=binance&symbol=ETHUSDT" | jq '._debug'

echo ""
echo "=== 测试 4: 指定 BTCUSDT ==="
curl -s "http://localhost:28888/api/positions?exchange=binance&symbol=BTCUSDT" | jq '._debug'

