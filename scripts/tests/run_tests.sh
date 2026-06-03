#!/bin/bash

# 设置代理（根据用户规则）
export https_proxy=http://127.0.0.1:7890
export http_proxy=http://127.0.0.1:7890
export all_proxy=socks5://127.0.0.1:7890

# 定义颜色输出
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m' # 无颜色

echo "🚀 开始运行 QuantMesh Market Maker 自动化单元测试..."
echo "--------------------------------------------------"

# 清理可能残留的测试数据库文件
rm -f ./test_quantmesh.db*
rm -f ./storage/test_quantmesh.db*

echo "运行 Go 全仓单元测试..."
go test ./... -coverprofile=/tmp/quantmesh-go-cover.out
if [ $? -ne 0 ]; then
    echo "--------------------------------------------------"
    echo -e "${RED}❌ Go 单元测试失败，请检查上方日志。${NC}"
    exit 1
fi

echo "Go 覆盖率摘要："
go tool cover -func=/tmp/quantmesh-go-cover.out | tail -n 1

echo "运行 WebUI 单元测试..."
cd webui
yarn test
if [ $? -ne 0 ]; then
    echo "--------------------------------------------------"
    echo -e "${RED}❌ WebUI 单元测试失败，请检查上方日志。${NC}"
    exit 1
fi

echo "--------------------------------------------------"
echo -e "${GREEN}✅ 所有单元测试均已通过！${NC}"
