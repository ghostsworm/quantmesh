#!/bin/bash

# QuantMesh 远程数据库备份并新建空库
# 用途：在 facev.app 的 /opt/quantmesh 上备份 data/*.db，然后删除原库，便于实盘前清空历史数据
# 使用：./scripts/remote-backup-and-reset-db.sh
# 前提：本机已配置免密 ssh root@facev.app

set -e

REMOTE="root@facev.app"
REMOTE_DIR="/opt/quantmesh"
DATA_DIR="${REMOTE_DIR}/data"
BACKUP_ROOT="${REMOTE_DIR}/backups"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

log_info "目标: ${REMOTE} 目录: ${REMOTE_DIR}"
log_info "将执行: 1) 备份 data 下所有 SQLite 库 2) 停止服务 3) 删除旧库 4) 启动服务（新建空库由应用首次启动时创建）"
echo ""
read -p "确认继续? (yes/no): " confirm
if [ "$confirm" != "yes" ]; then
    log_info "已取消"
    exit 0
fi

ssh "${REMOTE}" "set -e
    cd ${REMOTE_DIR}
    TIMESTAMP=\$(date +%Y%m%d_%H%M%S)
    BACKUP_DIR=\"${BACKUP_ROOT}/db_\${TIMESTAMP}\"
    mkdir -p \"\${BACKUP_DIR}\"

    echo '[远程] 备份数据库...'
    for f in quantmesh.db logs.db auth.db webauthn.db; do
        if [ -f \"${DATA_DIR}/\${f}\" ]; then
            cp \"${DATA_DIR}/\${f}\" \"\${BACKUP_DIR}/\${f}\"
            echo \"  ✓ \${f}\"
        fi
    done
    for base in quantmesh logs auth webauthn; do
        for suf in -wal -shm; do
            f=\"${DATA_DIR}/\${base}.db\${suf}\"
            [ -f \"\${f}\" ] && cp \"\${f}\" \"\${BACKUP_DIR}/\${base}.db\${suf}\" && echo \"  ✓ \${base}.db\${suf}\"
        done
    done

    echo '[远程] 停止 quantmesh 服务...'
    systemctl stop quantmesh 2>/dev/null || true
    sleep 2

    echo '[远程] 删除旧库文件...'
    rm -f ${DATA_DIR}/quantmesh.db ${DATA_DIR}/quantmesh.db-wal ${DATA_DIR}/quantmesh.db-shm
    rm -f ${DATA_DIR}/logs.db ${DATA_DIR}/logs.db-wal ${DATA_DIR}/logs.db-shm
    rm -f ${DATA_DIR}/auth.db ${DATA_DIR}/auth.db-wal ${DATA_DIR}/auth.db-shm
    rm -f ${DATA_DIR}/webauthn.db ${DATA_DIR}/webauthn.db-wal ${DATA_DIR}/webauthn.db-shm
    echo '  ✓ 已删除 data 下所有 .db / .db-wal / .db-shm'

    echo '[远程] 启动 quantmesh 服务...'
    systemctl start quantmesh
    sleep 1
    systemctl is-active --quiet quantmesh && echo '  ✓ 服务已启动' || echo '  ⚠ 请检查: systemctl status quantmesh'

    echo ''
    echo \"[远程] 备份目录: \${BACKUP_DIR}\"
    ls -la \"\${BACKUP_DIR}\"
"

log_info "========================================="
log_info "远程备份并清空数据库已完成。"
log_info "备份保存在服务器: ${BACKUP_ROOT}/db_<时间戳>"
log_info "实盘前请在本机重新配置账户、策略等；首次登录需重新设置密码（auth/webauthn 已清空）。"
log_info "========================================="
