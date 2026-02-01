#!/bin/bash

# QuantMesh 远程部署脚本（在服务器上编译）
# 功能：
# 1. 上传代码到服务器
# 2. 在服务器上编译
# 3. 自动备份数据库
# 4. 重启服务
#
# 使用方法：
#   ./scripts/deploy-remote.sh

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# 配置
REMOTE_HOST="facev.app"
REMOTE_USER="root"
REMOTE_PORT="22"
REMOTE_PATH="/root/quntmesh"  # 使用实际运行目录
SERVICE_NAME="quantmesh"      # systemd 服务名
PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# 日志函数
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_step() {
    echo -e "${BLUE}[STEP]${NC} $1"
}

# 执行远程命令
remote_exec() {
    ssh -p ${REMOTE_PORT} -o StrictHostKeyChecking=no ${REMOTE_USER}@${REMOTE_HOST} "$1"
}

# 主流程
main() {
    log_info "=========================================="
    log_info "QuantMesh 远程部署脚本"
    log_info "=========================================="
    log_info ""
    
    # 1. 验证SSH连接
    log_step "验证SSH连接..."
    if ! remote_exec "echo '连接成功'" > /dev/null 2>&1; then
        log_error "无法连接到服务器"
        exit 1
    fi
    log_info "✅ SSH连接正常"
    
    # 2. 创建远程目录
    log_step "创建远程目录..."
    remote_exec "mkdir -p ${REMOTE_PATH} ${REMOTE_PATH}/data ${REMOTE_PATH}/logs ${REMOTE_PATH}/scripts ${REMOTE_PATH}/backups"
    log_info "✅ 目录已创建"
    
    # 3. 上传代码（排除不需要的文件）
    log_step "上传代码到服务器..."
    rsync -avz --progress \
        --exclude '.git' \
        --exclude 'node_modules' \
        --exclude 'webui/dist' \
        --exclude 'webui/node_modules' \
        --exclude 'quantmesh' \
        --exclude 'quantmesh-*' \
        --exclude '*.db' \
        --exclude '*.db-shm' \
        --exclude '*.db-wal' \
        --exclude 'logs/' \
        --exclude 'backups/' \
        -e "ssh -p ${REMOTE_PORT} -o StrictHostKeyChecking=no" \
        "${PROJECT_DIR}/" ${REMOTE_USER}@${REMOTE_HOST}:${REMOTE_PATH}/
    log_info "✅ 代码上传完成"
    
    # 4. 备份数据库（如果存在）
    log_step "备份数据库..."
    remote_exec "cd ${REMOTE_PATH} && if [ -f data/quantmesh.db ]; then mkdir -p backups && cp -r data backups/data_\$(date +%Y%m%d_%H%M%S) 2>/dev/null || true; fi"
    log_info "✅ 数据库备份完成"
    
    # 5. 在服务器上构建前端
    log_step "在服务器上构建前端..."
    remote_exec "cd ${REMOTE_PATH}/webui && if [ -f package.json ]; then if [ ! -d node_modules ]; then npm install || yarn install || pnpm install; fi && npm run build || yarn build || pnpm build; fi && cd .. && if [ -d webui/dist ]; then rm -rf web/dist && cp -r webui/dist web/dist; fi"
    log_info "✅ 前端构建完成"
    
    # 6. 在服务器上构建后端
    log_step "在服务器上构建后端..."
    remote_exec "cd ${REMOTE_PATH} && export CGO_ENABLED=1 && go mod download && go build -buildvcs=false -ldflags='-s -w' -o quantmesh ."
    log_info "✅ 后端构建完成"
    
    # 7. 设置执行权限
    log_step "设置执行权限..."
    remote_exec "chmod +x ${REMOTE_PATH}/quantmesh ${REMOTE_PATH}/scripts/*.sh 2>/dev/null || true"
    log_info "✅ 权限设置完成"
    
    # 8. 使用 systemd 重启服务（若未配置 systemd 则回退为 pkill + nohup）
    log_step "重启服务..."
    if remote_exec "systemctl list-unit-files 2>/dev/null | grep -q '^${SERVICE_NAME}.service'" 2>/dev/null; then
        remote_exec "systemctl stop ${SERVICE_NAME} || true"
        remote_exec "systemctl start ${SERVICE_NAME}"
        sleep 3
        status=$(remote_exec "systemctl is-active ${SERVICE_NAME}" 2>/dev/null | tr -d '\r\n' || true)
        if [ "$status" = "active" ]; then
            log_info "✅ systemd 服务已启动"
        else
            log_error "❌ systemd 服务启动失败"
            remote_exec "systemctl status ${SERVICE_NAME} --no-pager -l" || true
        fi
    else
        log_info "未检测到 systemd 服务，使用进程方式重启..."
        remote_exec "cd ${REMOTE_PATH} && (pkill -f '${REMOTE_PATH}/quantmesh config' 2>/dev/null || true); sleep 2"
        remote_exec "cd ${REMOTE_PATH} && nohup ./quantmesh config.yaml > logs/quantmesh.log 2>&1 &"
        sleep 3
        log_info "✅ 进程已启动"
    fi
    
    # 9. 健康检查
    log_step "执行健康检查..."
    sleep 2
    if remote_exec "curl -f -s http://localhost:28888/api/status > /dev/null 2>&1 && echo 'ok' || echo 'fail'" | grep -q "ok"; then
        log_info "✅ 健康检查通过"
    else
        log_warn "⚠️ 健康检查失败，请查看: ssh ${REMOTE_USER}@${REMOTE_HOST} 'journalctl -u ${SERVICE_NAME} -n 50' 或 tail -f ${REMOTE_PATH}/logs/quantmesh.log"
    fi
    
    log_info ""
    log_info "=========================================="
    log_info "✅ 部署完成！"
    log_info "=========================================="
    log_info ""
    log_info "服务地址: ${REMOTE_HOST}"
    log_info "部署路径: ${REMOTE_PATH}"
    log_info "查看状态: ssh ${REMOTE_USER}@${REMOTE_HOST} 'systemctl status ${SERVICE_NAME}'"
    log_info "查看日志: ssh ${REMOTE_USER}@${REMOTE_HOST} 'journalctl -u ${SERVICE_NAME} -f'"
    log_info ""
}

main "$@"
