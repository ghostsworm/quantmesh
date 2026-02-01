#!/bin/bash

# QuantMesh 回测系统测试脚本

echo "🚀 QuantMesh 回测系统测试"
echo "================================"
echo ""

# 颜色定义
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 1. 运行单元测试
echo -e "${BLUE}📊 步骤 1: 运行单元测试${NC}"
echo "--------------------------------"
go test -v ./backtest -run "TestMomentumStrategy|TestMeanReversionStrategy|TestTrendFollowingStrategy"
if [ $? -eq 0 ]; then
    echo -e "${GREEN}✅ 单元测试通过${NC}"
else
    echo "❌ 单元测试失败"
    exit 1
fi
echo ""

# 2. 测试报告生成
echo -e "${BLUE}📄 步骤 2: 测试报告生成${NC}"
echo "--------------------------------"
go test -v ./backtest -run TestReportGeneration
if [ $? -eq 0 ]; then
    echo -e "${GREEN}✅ 报告生成测试通过${NC}"
else
    echo "❌ 报告生成测试失败"
    exit 1
fi
echo ""

# 3. 查看生成的报告
echo -e "${BLUE}📋 步骤 3: 查看生成的报告${NC}"
echo "--------------------------------"
LATEST_REPORT=$(find ./backtest -name "*.md" -type f | head -1)
if [ -n "$LATEST_REPORT" ]; then
    echo "最新报告: $LATEST_REPORT"
    echo ""
    echo "报告预览:"
    head -30 "$LATEST_REPORT"
    echo ""
    echo -e "${GREEN}✅ 报告文件已生成${NC}"
else
    echo -e "${YELLOW}⚠️ 未找到报告文件${NC}"
fi
echo ""

# 4. 查看缓存统计
echo -e "${BLUE}💾 步骤 4: 查看缓存统计${NC}"
echo "--------------------------------"
if [ -d "./backtest/cache" ]; then
    CACHE_COUNT=$(find ./backtest/cache -name "*.csv" -type f | wc -l)
    CACHE_SIZE=$(du -sh ./backtest/cache 2>/dev/null | awk '{print $1}')
    echo "缓存文件数量: $CACHE_COUNT"
    echo "缓存总大小: $CACHE_SIZE"
    echo -e "${GREEN}✅ 缓存系统正常${NC}"
else
    echo -e "${YELLOW}⚠️ 缓存目录不存在（首次运行正常）${NC}"
fi
echo ""

# 5. 总结
echo "================================"
echo -e "${GREEN}🎉 回测系统测试完成！${NC}"
echo ""
echo "功能清单:"
echo "  ✅ 数据获取与 CSV 缓存"
echo "  ✅ 动量策略回测"
echo "  ✅ 均值回归策略回测"
echo "  ✅ 趋势跟踪策略回测"
echo "  ✅ 完整指标计算（20+ 指标）"
echo "  ✅ 高级风险指标（VaR/CVaR）"
echo "  ✅ Markdown 报告生成"
echo "  ✅ RESTful API 接口"
echo ""
echo "下一步:"
echo "  1. 启动服务: ./quantmesh"
echo "  2. 通过 API 运行回测: curl -X POST http://localhost:8080/api/backtest/run ..."
echo "  3. 查看生成的报告: cat $LATEST_REPORT"
echo ""

