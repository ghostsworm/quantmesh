#!/bin/bash

# 交易所单元测试运行脚本

set -e

# 定义颜色输出
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # 无颜色

echo "🚀 开始运行交易所单元测试..."
echo "=========================================="

# 测试的交易所列表（所有22个交易所）
EXCHANGES=(
    "binance"
    "okx"
    "bybit"
    "huobi"
    "kucoin"
    "kraken"
    "bitfinex"
    "mexc"
    "bingx"
    "deribit"
    "bitmex"
    "phemex"
    "woox"
    "coinex"
    "gate"
    "bitget"
    "bitrue"
    "xtcom"
    "btcc"
    "ascendex"
    "poloniex"
    "cryptocom"
)

PASSED=0
FAILED=0

for exchange in "${EXCHANGES[@]}"; do
    echo ""
    echo "测试 $exchange 交易所..."
    echo "----------------------------------------"
    
    if go test -v ./exchange/$exchange -timeout 30s 2>&1 | tee /tmp/test_${exchange}.log; then
        echo -e "${GREEN}✅ $exchange 测试通过${NC}"
        ((PASSED++))
    else
        echo -e "${RED}❌ $exchange 测试失败${NC}"
        ((FAILED++))
    fi
done

echo ""
echo "=========================================="
echo "测试结果汇总:"
echo -e "${GREEN}通过: $PASSED${NC}"
if [ $FAILED -gt 0 ]; then
    echo -e "${RED}失败: $FAILED${NC}"
else
    echo -e "${GREEN}失败: $FAILED${NC}"
fi
echo "=========================================="

if [ $FAILED -eq 0 ]; then
    echo -e "${GREEN}✅ 所有交易所测试均已通过！${NC}"
    exit 0
else
    echo -e "${RED}❌ 部分测试失败，请检查上方日志。${NC}"
    exit 1
fi

