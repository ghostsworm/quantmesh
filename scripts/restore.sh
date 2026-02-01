#!/bin/bash

# QuantMesh 数据恢复脚本
# 用途：从备份恢复数据库和配置文件
# 使用方法：./scripts/restore.sh <backup_file.tar.gz>

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

# 检查参数
if [ $# -eq 0 ]; then
    log_error "用法: $0 <backup_file.tar.gz>"
    echo ""
    echo "可用的备份文件:"
    ls -lh ./backups/*.tar.gz 2>/dev/null || echo "  (无备份文件)"
    exit 1
fi

BACKUP_FILE="$1"

# 检查备份文件是否存在
if [ ! -f "${BACKUP_FILE}" ]; then
    log_error "备份文件不存在: ${BACKUP_FILE}"
    exit 1
fi

log_info "准备从备份恢复: ${BACKUP_FILE}"

# 验证校验和（如果存在）
CHECKSUM_FILE="${BACKUP_FILE%.tar.gz}.sha256"
if [ -f "${CHECKSUM_FILE}" ]; then
    log_info "验证备份文件完整性..."
    if sha256sum -c "${CHECKSUM_FILE}" > /dev/null 2>&1; then
        log_info "✓ 校验和验证通过"
    else
        log_error "校验和验证失败！备份文件可能已损坏"
        exit 1
    fi
else
    log_warn "未找到校验和文件，跳过完整性验证"
fi

# 确认恢复操作
log_warn "========================================="
log_warn "警告：此操作将覆盖当前的数据库和配置文件！"
log_warn "建议在恢复前先备份当前数据"
log_warn "========================================="
read -p "是否继续？(yes/no): " confirm

if [ "$confirm" != "yes" ]; then
    log_info "恢复操作已取消"
    exit 0
fi

# 创建临时目录
TEMP_DIR=$(mktemp -d)
log_info "创建临时目录: ${TEMP_DIR}"

# 解压备份
log_info "解压备份文件..."
tar -xzf "${BACKUP_FILE}" -C "${TEMP_DIR}"

# 查找解压后的目录
BACKUP_DIR=$(ls -d ${TEMP_DIR}/*/ | head -n 1)
if [ -z "${BACKUP_DIR}" ]; then
    log_error "无法找到解压后的备份目录"
    rm -rf "${TEMP_DIR}"
    exit 1
fi

log_info "备份目录: ${BACKUP_DIR}"

# 显示备份信息
if [ -f "${BACKUP_DIR}/backup_info.txt" ]; then
    log_info "========================================="
    cat "${BACKUP_DIR}/backup_info.txt"
    log_info "========================================="
fi

# 停止服务（如果正在运行）
log_info "检查服务状态..."
if pgrep -f "quantmesh" > /dev/null; then
    log_warn "检测到 QuantMesh 正在运行"
    read -p "是否停止服务？(yes/no): " stop_service
    if [ "$stop_service" = "yes" ]; then
        log_info "停止 QuantMesh 服务..."
        pkill -f "quantmesh" || true
        sleep 2
        log_info "✓ 服务已停止"
    else
        log_warn "服务仍在运行，恢复可能失败"
    fi
fi

# 备份当前数据（以防万一）
CURRENT_BACKUP_DIR="./backups/before_restore_$(date +%Y%m%d_%H%M%S)"
mkdir -p "${CURRENT_BACKUP_DIR}"
log_info "备份当前数据到: ${CURRENT_BACKUP_DIR}"

if [ -d "./data" ]; then
    cp -r ./data "${CURRENT_BACKUP_DIR}/" 2>/dev/null || true
fi
if [ -f "./config.yaml" ]; then
    cp ./config.yaml "${CURRENT_BACKUP_DIR}/" 2>/dev/null || true
fi

# 恢复数据库
log_info "恢复数据库文件..."
mkdir -p ./data

if [ -f "${BACKUP_DIR}/quantmesh.db" ]; then
    cp "${BACKUP_DIR}/quantmesh.db" "./data/quantmesh.db"
    log_info "✓ 已恢复 quantmesh.db"
fi

if [ -f "${BACKUP_DIR}/auth.db" ]; then
    cp "${BACKUP_DIR}/auth.db" "./data/auth.db"
    log_info "✓ 已恢复 auth.db"
fi

if [ -f "${BACKUP_DIR}/webauthn.db" ]; then
    cp "${BACKUP_DIR}/webauthn.db" "./data/webauthn.db"
    log_info "✓ 已恢复 webauthn.db"
fi

if [ -f "${BACKUP_DIR}/logs.db" ]; then
    cp "${BACKUP_DIR}/logs.db" "./logs.db"
    log_info "✓ 已恢复 logs.db"
fi

# 恢复配置文件
log_info "恢复配置文件..."
if [ -f "${BACKUP_DIR}/config.yaml" ]; then
    cp "${BACKUP_DIR}/config.yaml" "./config.yaml"
    log_info "✓ 已恢复 config.yaml"
fi

if [ -d "${BACKUP_DIR}/config_backups" ]; then
    cp -r "${BACKUP_DIR}/config_backups" "./config_backups"
    log_info "✓ 已恢复 config_backups 目录"
fi

# 恢复日志文件（可选）
if [ -d "${BACKUP_DIR}/logs" ]; then
    log_info "恢复日志文件..."
    mkdir -p ./logs
    cp -r "${BACKUP_DIR}/logs/"* "./logs/" 2>/dev/null || true
    log_info "✓ 已恢复日志文件"
fi

# 清理临时目录
log_info "清理临时文件..."
rm -rf "${TEMP_DIR}"

# 完成
log_info "========================================="
log_info "恢复完成！"
log_info "当前数据已备份到: ${CURRENT_BACKUP_DIR}"
log_info "========================================="
log_warn "请检查配置文件和数据库是否正确"
log_warn "然后重新启动 QuantMesh 服务"

exit 0

