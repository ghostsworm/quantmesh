#!/bin/bash

echo "======================================"
echo "测试日志分离功能"
echo "======================================"

# 清理旧的日志文件
echo "1. 清理旧的测试日志..."
rm -f logs/app-quantmesh-*.log logs/web-gin-*.log 2>/dev/null

# 启动服务（后台运行）
echo "2. 启动服务..."
./quantmesh &
SERVER_PID=$!
echo "服务 PID: $SERVER_PID"

# 等待服务启动
echo "3. 等待服务启动（5秒）..."
sleep 5

# 检查日志文件是否创建
echo "4. 检查日志文件..."
echo "应用日志文件:"
ls -lh logs/app-quantmesh-*.log 2>/dev/null || echo "  未找到应用日志文件"
echo "Web 日志文件:"
ls -lh logs/web-gin-*.log 2>/dev/null || echo "  未找到 Web 日志文件"

# 发送一些测试请求（包括成功和失败的请求）
echo ""
echo "5. 发送测试请求..."

# 成功的请求（应该不会记录到 Web 日志）
echo "  - 发送成功请求 (GET /api/version)..."
curl -s http://localhost:28888/api/version > /dev/null 2>&1

# 失败的请求（应该记录到 Web 日志）
echo "  - 发送 404 请求 (GET /api/nonexistent)..."
curl -s http://localhost:28888/api/nonexistent > /dev/null 2>&1

echo "  - 发送 401 请求 (GET /api/slots)..."
curl -s http://localhost:28888/api/slots > /dev/null 2>&1

# 等待日志写入
sleep 2

# 停止服务
echo ""
echo "6. 停止服务..."
kill $SERVER_PID 2>/dev/null
sleep 2

# 显示日志内容
echo ""
echo "======================================"
echo "应用日志内容 (最后 20 行):"
echo "======================================"
tail -20 logs/app-quantmesh-*.log 2>/dev/null || echo "无应用日志"

echo ""
echo "======================================"
echo "Web 日志内容 (全部):"
echo "======================================"
cat logs/web-gin-*.log 2>/dev/null || echo "无 Web 日志"

echo ""
echo "======================================"
echo "测试完成!"
echo "======================================"
echo "预期结果:"
echo "  1. 应用日志文件: logs/app-quantmesh-YYYY-MM-DD.log (包含应用启动、运行日志)"
echo "  2. Web 日志文件: logs/web-gin-YYYY-MM-DD.log (只包含错误请求: 404, 401)"
echo "  3. 两个日志文件分离,互不干扰"

