#!/bin/bash
#
# QuantMesh 安装脚本
# 用法: sudo ./install.sh
#
# 功能:
#   - 自动安装 QuantMesh 二进制文件到 /opt/quantmesh
#   - 配置 systemd 服务
#   - 处理配置文件（备份/保留/覆盖）
#   - 创建必要的用户和目录
#

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 配置
INSTALL_DIR="/opt/quantmesh"
SERVICE_NAME="quantmesh"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
CONFIG_FILE="${INSTALL_DIR}/config.yaml"
BACKUP_DIR="${INSTALL_DIR}/backups"
DATA_DIR="${INSTALL_DIR}/data"
LOGS_DIR="${INSTALL_DIR}/logs"

# 获取脚本所在目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

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

# 检查是否以 root 运行
check_root() {
    if [ "$EUID" -ne 0 ]; then
        log_error "请以 root 权限运行此脚本: sudo ./install.sh"
        exit 1
    fi
}

# 检测操作系统
detect_os() {
    if [ -f /etc/os-release ]; then
        . /etc/os-release
        OS=$NAME
        OS_VERSION=$VERSION_ID
    else
        OS=$(uname -s)
        OS_VERSION=$(uname -r)
    fi
    log_info "检测到操作系统: $OS $OS_VERSION"
}

# 检查 systemd 是否可用
check_systemd() {
    if ! command -v systemctl &> /dev/null; then
        log_error "systemd 不可用。此安装脚本仅支持使用 systemd 的 Linux 系统。"
        exit 1
    fi
    log_info "systemd 可用"
}

# 查找二进制文件
find_binary() {
    local arch=$(uname -m)
    local os=$(uname -s | tr '[:upper:]' '[:lower:]')
    
    # 转换架构名称
    case $arch in
        x86_64)
            arch="amd64"
            ;;
        aarch64|arm64)
            arch="arm64"
            ;;
        *)
            log_error "不支持的架构: $arch"
            exit 1
            ;;
    esac
    
    # 查找二进制文件
    local binary_name="quantmesh-${os}-${arch}"
    
    # 首先在当前目录查找
    if [ -f "./${binary_name}" ]; then
        BINARY_PATH="./${binary_name}"
    elif [ -f "${SCRIPT_DIR}/${binary_name}" ]; then
        BINARY_PATH="${SCRIPT_DIR}/${binary_name}"
    elif [ -f "${SCRIPT_DIR}/../${binary_name}" ]; then
        BINARY_PATH="${SCRIPT_DIR}/../${binary_name}"
    elif [ -f "./quantmesh" ]; then
        BINARY_PATH="./quantmesh"
    elif [ -f "${SCRIPT_DIR}/quantmesh" ]; then
        BINARY_PATH="${SCRIPT_DIR}/quantmesh"
    elif [ -f "${SCRIPT_DIR}/../quantmesh" ]; then
        BINARY_PATH="${SCRIPT_DIR}/../quantmesh"
    else
        log_error "找不到 QuantMesh 二进制文件。请确保在解压后的目录中运行此脚本。"
        log_error "期望找到: ${binary_name} 或 quantmesh"
        exit 1
    fi
    
    log_info "找到二进制文件: ${BINARY_PATH}"
}

# 查找配置示例文件
find_config_example() {
    if [ -f "./config.example.yaml" ]; then
        CONFIG_EXAMPLE="./config.example.yaml"
    elif [ -f "${SCRIPT_DIR}/config.example.yaml" ]; then
        CONFIG_EXAMPLE="${SCRIPT_DIR}/config.example.yaml"
    elif [ -f "${SCRIPT_DIR}/../config.example.yaml" ]; then
        CONFIG_EXAMPLE="${SCRIPT_DIR}/../config.example.yaml"
    else
        log_warn "找不到 config.example.yaml，将跳过配置文件安装"
        CONFIG_EXAMPLE=""
    fi
    
    if [ -n "$CONFIG_EXAMPLE" ]; then
        log_info "找到配置示例: ${CONFIG_EXAMPLE}"
    fi
}

# 查找 systemd 服务文件
find_service_file() {
    if [ -f "./quantmesh.service" ]; then
        SERVICE_TEMPLATE="./quantmesh.service"
    elif [ -f "${SCRIPT_DIR}/quantmesh.service" ]; then
        SERVICE_TEMPLATE="${SCRIPT_DIR}/quantmesh.service"
    elif [ -f "${SCRIPT_DIR}/../quantmesh.service" ]; then
        SERVICE_TEMPLATE="${SCRIPT_DIR}/../quantmesh.service"
    elif [ -f "${SCRIPT_DIR}/../scripts/quantmesh.service" ]; then
        SERVICE_TEMPLATE="${SCRIPT_DIR}/../scripts/quantmesh.service"
    else
        log_warn "找不到 quantmesh.service 模板，将使用内置模板"
        SERVICE_TEMPLATE=""
    fi
    
    if [ -n "$SERVICE_TEMPLATE" ]; then
        log_info "找到服务文件模板: ${SERVICE_TEMPLATE}"
    fi
}

# 创建 quantmesh 用户和组
create_user() {
    if ! id -u quantmesh &>/dev/null; then
        log_step "创建 quantmesh 用户..."
        useradd -r -s /bin/false -d ${INSTALL_DIR} quantmesh
        log_info "用户 quantmesh 已创建"
    else
        log_info "用户 quantmesh 已存在"
    fi
}

# 创建必要的目录
create_directories() {
    log_step "创建安装目录..."
    
    mkdir -p ${INSTALL_DIR}
    mkdir -p ${BACKUP_DIR}   # config.yaml 同級備份目錄，用於配置備份
    mkdir -p ${DATA_DIR}
    mkdir -p ${LOGS_DIR}
    mkdir -p ${INSTALL_DIR}/scripts
    # 回测结果、报告、缓存、优化结果目录（服务需可写）
    mkdir -p ${INSTALL_DIR}/backtest/results
    mkdir -p ${INSTALL_DIR}/backtest/reports
    mkdir -p ${INSTALL_DIR}/backtest/cache
    mkdir -p ${INSTALL_DIR}/backtest/optim_results
    
    log_info "目录已创建: ${INSTALL_DIR}"
}

# 停止现有服务
stop_existing_service() {
    if systemctl is-active --quiet ${SERVICE_NAME} 2>/dev/null; then
        log_step "停止现有 ${SERVICE_NAME} 服务..."
        systemctl stop ${SERVICE_NAME}
        log_info "服务已停止"
    fi
}


# 安装二进制文件
install_binary() {
    log_step "安装二进制文件..."
    
    cp "${BINARY_PATH}" "${INSTALL_DIR}/quantmesh"
    chmod +x "${INSTALL_DIR}/quantmesh"
    
    # 获取版本信息
    local version=$("${INSTALL_DIR}/quantmesh" --version 2>/dev/null || echo "unknown")
    log_info "二进制文件已安装: ${INSTALL_DIR}/quantmesh (版本: ${version})"
}

# 处理配置文件
handle_config() {
    if [ -z "$CONFIG_EXAMPLE" ]; then
        return
    fi
    
    log_step "处理配置文件..."
    
    if [ -f "$CONFIG_FILE" ]; then
        echo ""
        echo -e "${YELLOW}检测到已存在的配置文件: $CONFIG_FILE${NC}"
        echo ""
        echo "请选择如何处理："
        echo "  [1] 保留现有配置（默认，推荐）"
        echo "  [2] 使用新的示例配置覆盖（会备份旧配置）"
        echo "  [3] 合并配置（保留旧配置，将示例配置复制为 config.example.yaml）"
        echo ""
        
        read -p "请选择 [1/2/3] (默认: 1): " config_choice
        config_choice=${config_choice:-1}
        
        case $config_choice in
            1)
                log_info "保留现有配置文件"
                # 同时复制示例配置供参考
                cp "${CONFIG_EXAMPLE}" "${INSTALL_DIR}/config.example.yaml"
                log_info "示例配置已复制到: ${INSTALL_DIR}/config.example.yaml"
                ;;
            2)
                # 备份旧配置
                local backup_name="config.yaml.backup.$(date +%Y%m%d_%H%M%S)"
                local backup_path="${BACKUP_DIR}/${backup_name}"
                cp "$CONFIG_FILE" "$backup_path"
                log_info "旧配置已备份到: ${backup_path}"
                
                # 复制新配置
                cp "${CONFIG_EXAMPLE}" "$CONFIG_FILE"
                log_warn "配置文件已更新，请编辑 $CONFIG_FILE 配置您的 API 密钥等信息"
                ;;
            3)
                log_info "保留现有配置，复制示例配置供参考"
                cp "${CONFIG_EXAMPLE}" "${INSTALL_DIR}/config.example.yaml"
                log_info "示例配置已复制到: ${INSTALL_DIR}/config.example.yaml"
                ;;
            *)
                log_info "无效选择，保留现有配置文件"
                ;;
        esac
    else
        # 没有现有配置，复制示例配置
        cp "${CONFIG_EXAMPLE}" "$CONFIG_FILE"
        cp "${CONFIG_EXAMPLE}" "${INSTALL_DIR}/config.example.yaml"
        log_warn "已创建配置文件: $CONFIG_FILE"
        log_warn "请编辑配置文件，填入您的 API 密钥和交易参数！"
    fi
}

# 生成内置 systemd 服务文件
generate_service_file() {
    cat > "${SERVICE_FILE}" << 'EOF'
[Unit]
Description=QuantMesh Market Maker Service
Documentation=https://quantmesh.io
After=network.target

[Service]
Type=simple
User=quantmesh
Group=quantmesh
WorkingDirectory=/opt/quantmesh
ExecStart=/opt/quantmesh/quantmesh
ExecStop=/bin/kill -s TERM $MAINPID

# 重启策略
Restart=on-failure
RestartSec=10s
StartLimitInterval=5min
StartLimitBurst=3

# 资源限制
LimitNOFILE=65536
LimitNPROC=4096

# 安全加固
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/opt/quantmesh/data /opt/quantmesh/logs /opt/quantmesh/backups /opt/quantmesh/config.yaml /opt/quantmesh/config_backups /opt/quantmesh/backtest

# 日志
StandardOutput=journal
StandardError=journal
SyslogIdentifier=quantmesh

[Install]
WantedBy=multi-user.target
EOF
}

# 安装 systemd 服务
install_service() {
    log_step "安装 systemd 服务..."
    
    if [ -n "$SERVICE_TEMPLATE" ]; then
        cp "${SERVICE_TEMPLATE}" "${SERVICE_FILE}"
    else
        generate_service_file
    fi
    
    # 重新加载 systemd
    systemctl daemon-reload
    
    # 启用服务
    systemctl enable ${SERVICE_NAME}
    
    log_info "systemd 服务已安装并启用"
}

# 设置权限
set_permissions() {
    log_step "设置文件权限..."
    
    chown -R quantmesh:quantmesh ${INSTALL_DIR}
    chmod 755 ${INSTALL_DIR}
    chmod 700 ${DATA_DIR}
    chmod 0776 ${BACKUP_DIR}   # 0776 確保 quantmesh 用戶可寫入新建配置備份
    chmod 700 ${LOGS_DIR}
    # 回测目录：quantmesh 需可写 results/reports/cache/optim_results
    chmod -R 775 ${INSTALL_DIR}/backtest
    
    # 配置文件权限（包含敏感信息）
    if [ -f "$CONFIG_FILE" ]; then
        chmod 600 "$CONFIG_FILE"
    fi
    
    log_info "权限已设置"
}

# 复制辅助脚本
copy_scripts() {
    log_step "复制辅助脚本..."
    
    # 复制 backup.sh 和 restore.sh（如果存在）
    for script in backup.sh restore.sh; do
        if [ -f "${SCRIPT_DIR}/${script}" ]; then
            cp "${SCRIPT_DIR}/${script}" "${INSTALL_DIR}/scripts/"
            chmod +x "${INSTALL_DIR}/scripts/${script}"
        elif [ -f "${SCRIPT_DIR}/../scripts/${script}" ]; then
            cp "${SCRIPT_DIR}/../scripts/${script}" "${INSTALL_DIR}/scripts/"
            chmod +x "${INSTALL_DIR}/scripts/${script}"
        fi
    done
    
    log_info "辅助脚本已复制"
}

# 启动服务
start_service() {
    echo ""
    read -p "是否现在启动 QuantMesh 服务？[y/N] (默认: N): " start_now
    start_now=${start_now:-N}
    
    if [[ "$start_now" =~ ^[Yy]$ ]]; then
        log_step "启动服务..."
        systemctl start ${SERVICE_NAME}
        
        # 等待几秒检查状态
        sleep 3
        
        if systemctl is-active --quiet ${SERVICE_NAME}; then
            log_info "服务已启动成功"
        else
            log_error "服务启动失败，请检查日志: journalctl -u ${SERVICE_NAME} -f"
        fi
    else
        log_info "服务未启动。您可以稍后使用以下命令启动："
        echo "  sudo systemctl start ${SERVICE_NAME}"
    fi
}

# 打印完成信息
print_completion() {
    echo ""
    echo "=============================================="
    echo -e "${GREEN}QuantMesh 安装完成！${NC}"
    echo "=============================================="
    echo ""
    echo "安装目录: ${INSTALL_DIR}"
    echo "配置文件: ${CONFIG_FILE}"
    echo "数据目录: ${DATA_DIR}"
    echo "备份目录: ${BACKUP_DIR}"
    echo "日志目录: ${LOGS_DIR}"
    echo ""
    echo "常用命令："
    echo "  启动服务:   sudo systemctl start ${SERVICE_NAME}"
    echo "  停止服务:   sudo systemctl stop ${SERVICE_NAME}"
    echo "  重启服务:   sudo systemctl restart ${SERVICE_NAME}"
    echo "  查看状态:   sudo systemctl status ${SERVICE_NAME}"
    echo "  查看日志:   journalctl -u ${SERVICE_NAME} -f"
    echo ""
    echo "Web 界面:    http://localhost:28888"
    echo ""
    
    if [ ! -f "$CONFIG_FILE" ] || grep -q "YOUR_API_KEY" "$CONFIG_FILE" 2>/dev/null; then
        echo -e "${YELLOW}重要提示：${NC}"
        echo "  请先编辑配置文件，填入您的交易所 API 密钥："
        echo "  sudo nano ${CONFIG_FILE}"
        echo ""
    fi
}

# 主函数
main() {
    echo ""
    echo "=============================================="
    echo "     QuantMesh 安装程序"
    echo "=============================================="
    echo ""
    
    check_root
    detect_os
    check_systemd
    find_binary
    find_config_example
    find_service_file
    
    echo ""
    log_step "开始安装..."
    echo ""
    
    create_user
    create_directories
    stop_existing_service
    install_binary
    handle_config
    install_service
    copy_scripts
    set_permissions
    start_service
    print_completion
}

# 运行主函数
main "$@"
