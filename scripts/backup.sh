#!/bin/bash

# QuantMesh 数据备份脚本
# 用途：自动备份数据库、配置文件和日志
# 使用方法：./scripts/backup.sh [backup_dir]

set -e

# 配置
BACKUP_ROOT="${1:-./backups}"
TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
BACKUP_DIR="${BACKUP_ROOT}/${TIMESTAMP}"
RETENTION_DAYS=30

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

# 创建备份目录
mkdir -p "${BACKUP_DIR}"
log_info "创建备份目录: ${BACKUP_DIR}"

# 1. 备份数据库
log_info "备份数据库..."
if [ -f "./data/quantmesh.db" ]; then
    cp "./data/quantmesh.db" "${BACKUP_DIR}/quantmesh.db"
    log_info "✓ 已备份 quantmesh.db"
else
    log_warn "quantmesh.db 不存在，跳过"
fi

if [ -f "./data/auth.db" ]; then
    cp "./data/auth.db" "${BACKUP_DIR}/auth.db"
    log_info "✓ 已备份 auth.db"
fi

if [ -f "./data/webauthn.db" ]; then
    cp "./data/webauthn.db" "${BACKUP_DIR}/webauthn.db"
    log_info "✓ 已备份 webauthn.db"
fi

if [ -f "./logs.db" ]; then
    cp "./logs.db" "${BACKUP_DIR}/logs.db"
    log_info "✓ 已备份 logs.db"
fi

# 2. 备份配置文件
log_info "备份配置文件..."
if [ -f "./config.yaml" ]; then
    cp "./config.yaml" "${BACKUP_DIR}/config.yaml"
    log_info "✓ 已备份 config.yaml"
else
    log_warn "config.yaml 不存在，跳过"
fi

# 备份配置备份目录
if [ -d "./config_backups" ]; then
    cp -r "./config_backups" "${BACKUP_DIR}/config_backups"
    log_info "✓ 已备份 config_backups 目录"
fi

# 3. 备份日志文件（可选，日志文件可能很大）
log_info "备份日志文件..."
if [ -d "./logs" ]; then
    # 只备份最近7天的日志
    mkdir -p "${BACKUP_DIR}/logs"
    find ./logs -name "*.log" -mtime -7 -exec cp {} "${BACKUP_DIR}/logs/" \;
    log_info "✓ 已备份最近7天的日志文件"
fi

# 4. 创建备份元数据
cat > "${BACKUP_DIR}/backup_info.txt" << EOF
备份时间: $(date)
备份类型: 完整备份
主机名: $(hostname)
系统: $(uname -s)
备份内容:
- 数据库文件
- 配置文件
- 日志文件（最近7天）
EOF

log_info "✓ 已创建备份元数据"

# 5. 压缩备份
log_info "压缩备份文件..."
cd "${BACKUP_ROOT}"
tar -czf "${TIMESTAMP}.tar.gz" "${TIMESTAMP}"
rm -rf "${TIMESTAMP}"
log_info "✓ 备份已压缩: ${BACKUP_ROOT}/${TIMESTAMP}.tar.gz"

# 6. 计算校验和
cd - > /dev/null
CHECKSUM=$(sha256sum "${BACKUP_ROOT}/${TIMESTAMP}.tar.gz" | awk '{print $1}')
echo "${CHECKSUM}  ${TIMESTAMP}.tar.gz" > "${BACKUP_ROOT}/${TIMESTAMP}.sha256"
log_info "✓ 校验和: ${CHECKSUM}"

# 7. 清理旧备份
log_info "清理超过 ${RETENTION_DAYS} 天的旧备份..."
find "${BACKUP_ROOT}" -name "*.tar.gz" -mtime +${RETENTION_DAYS} -delete
find "${BACKUP_ROOT}" -name "*.sha256" -mtime +${RETENTION_DAYS} -delete
log_info "✓ 旧备份已清理"

# 8. 显示备份统计
BACKUP_SIZE=$(du -h "${BACKUP_ROOT}/${TIMESTAMP}.tar.gz" | awk '{print $1}')
BACKUP_COUNT=$(ls -1 "${BACKUP_ROOT}"/*.tar.gz 2>/dev/null | wc -l)

log_info "========================================="
log_info "备份完成！"
log_info "备份文件: ${BACKUP_ROOT}/${TIMESTAMP}.tar.gz"
log_info "备份大小: ${BACKUP_SIZE}"
log_info "当前备份数量: ${BACKUP_COUNT}"
log_info "========================================="

# 9. 可选：上传到云存储（需要配置）
if [ -n "${BACKUP_UPLOAD_ENABLED}" ] && [ "${BACKUP_UPLOAD_ENABLED}" = "true" ]; then
    log_info "上传备份到云存储..."
    # 示例：上传到 AWS S3
    # aws s3 cp "${BACKUP_ROOT}/${TIMESTAMP}.tar.gz" "s3://your-bucket/quantmesh-backups/"
    # 示例：上传到阿里云 OSS
    # ossutil cp "${BACKUP_ROOT}/${TIMESTAMP}.tar.gz" "oss://your-bucket/quantmesh-backups/"
    log_warn "云存储上传未配置，跳过"
fi

exit 0

