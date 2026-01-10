#!/bin/bash

# QuantMesh 数据重置脚本
# 用途：清理所有数据库文件，以便重新初始化系统
# 使用方法：./scripts/reset_data.sh

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 确认操作
log_warn "⚠️  警告：此操作将删除所有数据库文件，包括："
log_warn "   - 认证数据 (auth.db)"
log_warn "   - WebAuthn 数据 (webauthn.db)"
log_warn "   - 主数据库 (quantmesh.db)"
log_warn "   - 日志数据库 (logs.db)"
log_warn "   - 其他数据库文件"
echo ""
read -p "确认要继续吗？(yes/no): " confirm

if [ "$confirm" != "yes" ]; then
    log_info "操作已取消"
    exit 0
fi

# 检查是否有进程正在运行
if pgrep -f "quantmesh" > /dev/null; then
    log_warn "检测到 quantmesh 进程正在运行"
    read -p "是否要停止进程？(yes/no): " stop_process
    if [ "$stop_process" = "yes" ]; then
        log_info "正在停止进程..."
        pkill -f "quantmesh" || true
        sleep 2
        log_info "✓ 进程已停止"
    else
        log_error "请先停止进程后再执行重置"
        exit 1
    fi
fi

# 清理数据库文件
log_info "开始清理数据库文件..."

# 清理 data/ 目录下的数据库文件
if [ -d "./data" ]; then
    log_info "清理 data/ 目录..."
    
    # 删除数据库文件
    for db_file in auth.db webauthn.db quantmesh.db opensqt.db; do
        if [ -f "./data/${db_file}" ]; then
            rm -f "./data/${db_file}"
            log_info "✓ 已删除 data/${db_file}"
        fi
    done
    
    # 删除 SQLite 临时文件
    for temp_file in auth.db-shm auth.db-wal webauthn.db-shm webauthn.db-wal quantmesh.db-shm quantmesh.db-wal opensqt.db-shm opensqt.db-wal; do
        if [ -f "./data/${temp_file}" ]; then
            rm -f "./data/${temp_file}"
            log_info "✓ 已删除 data/${temp_file}"
        fi
    done
fi

# 清理根目录下的日志数据库
if [ -f "./logs.db" ]; then
    rm -f "./logs.db"
    log_info "✓ 已删除 logs.db"
fi

# 清理日志数据库临时文件
if [ -f "./logs.db-shm" ]; then
    rm -f "./logs.db-shm"
    log_info "✓ 已删除 logs.db-shm"
fi

if [ -f "./logs.db-wal" ]; then
    rm -f "./logs.db-wal"
    log_info "✓ 已删除 logs.db-wal"
fi

log_info "========================================="
log_info "数据重置完成！"
log_info "所有数据库文件已删除"
log_info "下次启动系统时将自动创建新的数据库"
log_info "========================================="

exit 0
