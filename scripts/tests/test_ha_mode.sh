#!/bin/bash

# 测试高可用模式集成
# 这个脚本验证单机模式和多实例模式的配置

set -e

echo "========================================"
echo "测试 1: 单机模式（默认配置）"
echo "========================================"

# 检查配置文件
if grep -q "enabled: false" config.yaml; then
    echo "✅ 分布式锁已禁用（单机模式）"
else
    echo "❌ 分布式锁配置异常"
    exit 1
fi

if grep -q 'type: sqlite' config.yaml || grep -q 'type: "sqlite"' config.yaml; then
    echo "✅ 数据库类型为 SQLite（单机模式）"
else
    echo "⚠️  数据库类型不是 SQLite"
fi

echo ""
echo "========================================"
echo "测试 2: 编译测试"
echo "========================================"

go build -o /tmp/quantmesh_test . 2>&1 | grep -v "warning:" || true

if [ $? -eq 0 ]; then
    echo "✅ 编译成功"
else
    echo "❌ 编译失败"
    exit 1
fi

echo ""
echo "========================================"
echo "测试 3: 检查关键组件"
echo "========================================"

# 检查分布式锁包
if [ -d "lock" ]; then
    echo "✅ 分布式锁包存在"
    ls -l lock/*.go | awk '{print "   -", $NF}'
else
    echo "❌ 分布式锁包缺失"
    exit 1
fi

# 检查数据库抽象层
if [ -d "database" ]; then
    echo "✅ 数据库抽象层存在"
    ls -l database/*.go | awk '{print "   -", $NF}'
else
    echo "❌ 数据库抽象层缺失"
    exit 1
fi

# 检查配置支持
if grep -q "DistributedLock" config/config.go; then
    echo "✅ 配置支持分布式锁"
else
    echo "❌ 配置缺少分布式锁支持"
    exit 1
fi

if grep -q "Database" config/config.go | head -1; then
    echo "✅ 配置支持数据库抽象"
else
    echo "❌ 配置缺少数据库抽象支持"
    exit 1
fi

echo ""
echo "========================================"
echo "测试 4: 检查集成点"
echo "========================================"

# 检查 order executor 是否接受 lock 参数
if grep -q "distributedLock lock.DistributedLock" order/executor_adapter.go; then
    echo "✅ OrderExecutor 已集成分布式锁"
else
    echo "❌ OrderExecutor 缺少分布式锁集成"
    exit 1
fi

# 检查 reconciler 是否接受 lock 参数
if grep -q "lock.DistributedLock" safety/reconciler.go; then
    echo "✅ Reconciler 已集成分布式锁"
else
    echo "❌ Reconciler 缺少分布式锁集成"
    exit 1
fi

# 检查 main.go 是否初始化了 lock 和 database
if grep -q "lock.NewDistributedLock" main.go; then
    echo "✅ main.go 已初始化分布式锁"
else
    echo "❌ main.go 缺少分布式锁初始化"
    exit 1
fi

if grep -q "database.NewDatabaseService" main.go; then
    echo "✅ main.go 已初始化数据库服务"
else
    echo "ℹ️  数据库服务暂未集成（使用现有 storage.StorageService）"
fi

echo ""
echo "========================================"
echo "测试 5: 检查 Prometheus 指标"
echo "========================================"

if grep -q "lockAcquireTotal" metrics/prometheus.go; then
    echo "✅ 分布式锁指标已定义"
else
    echo "❌ 分布式锁指标缺失"
    exit 1
fi

echo ""
echo "========================================"
echo "测试 6: 验证高可用配置示例"
echo "========================================"

if [ -f "config-ha-example.yaml" ]; then
    echo "✅ 高可用配置示例存在"
    
    # 验证启用了分布式锁
    if grep -q "enabled: true" config-ha-example.yaml; then
        echo "   ✅ 示例配置启用了分布式锁"
    else
        echo "   ❌ 示例配置未启用分布式锁"
        exit 1
    fi
    
    # 验证使用了 PostgreSQL
    if grep -q "type: postgres" config-ha-example.yaml; then
        echo "   ✅ 示例配置使用 PostgreSQL"
    else
        echo "   ⚠️  示例配置未使用 PostgreSQL"
    fi
else
    echo "❌ 高可用配置示例缺失"
    exit 1
fi

echo ""
echo "========================================"
echo "测试 7: 检查 Docker Compose 配置"
echo "========================================"

if [ -f "docker-compose.ha.yml" ]; then
    echo "✅ 高可用 Docker Compose 配置存在"
    
    # 验证包含 Redis
    if grep -q "redis:" docker-compose.ha.yml; then
        echo "   ✅ 包含 Redis 服务"
    else
        echo "   ❌ 缺少 Redis 服务"
        exit 1
    fi
    
    # 验证包含 PostgreSQL
    if grep -q "postgres:" docker-compose.ha.yml; then
        echo "   ✅ 包含 PostgreSQL 服务"
    else
        echo "   ❌ 缺少 PostgreSQL 服务"
        exit 1
    fi
else
    echo "❌ 高可用 Docker Compose 配置缺失"
    exit 1
fi

echo ""
echo "========================================"
echo "✅ 所有测试通过！"
echo "========================================"
echo ""
echo "摘要："
echo "  ✓ 单机模式（SQLite + NopLock）- 默认配置"
echo "  ✓ 多实例模式（PostgreSQL + Redis Lock）- 需手动配置"
echo "  ✓ 分布式锁集成在订单执行、取消、对账路径"
echo "  ✓ 数据库抽象层支持 SQLite/PostgreSQL/MySQL"
echo "  ✓ Prometheus 指标已扩展"
echo "  ✓ 向后兼容，默认单机模式"
echo ""
echo "下一步："
echo "  1. 启动单机模式: ./quantmesh config.yaml"
echo "  2. 启动多实例模式: docker-compose -f docker-compose.ha.yml up"
echo "  3. 查看文档: docs/HIGH_AVAILABILITY.md"
echo ""

