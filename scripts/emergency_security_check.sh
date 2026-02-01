#!/bin/bash

# 紧急安全检查和修复脚本
# 用于已部署到公网的系统

set -e

echo "🚨 QuantMesh 紧急安全检查"
echo "================================"
echo "服务器: $(hostname)"
echo "时间: $(date)"
echo "================================"
echo ""

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 工作目录
WORK_DIR="/root/quntmesh"
cd "$WORK_DIR" || {
    echo -e "${RED}❌ 无法进入工作目录: $WORK_DIR${NC}"
    exit 1
}

echo -e "${BLUE}当前工作目录: $(pwd)${NC}"
echo ""

# 1. 立即停止服务（防止进一步被攻击）
echo "1️⃣  停止服务"
echo "-------------------"
if systemctl is-active --quiet quantmesh; then
    echo -e "${YELLOW}⚠️  正在停止 quantmesh 服务...${NC}"
    systemctl stop quantmesh
    echo -e "${GREEN}✅ 服务已停止${NC}"
else
    echo "ℹ️  服务未运行"
fi
echo ""

# 2. 检查认证数据库
echo "2️⃣  检查认证数据库"
echo "-------------------"
if [ -f "data/auth.db" ]; then
    # 文件信息
    echo "📄 认证数据库信息:"
    ls -lh data/auth.db
    echo ""
    
    # 修改时间
    mod_time=$(stat -c "%y" data/auth.db 2>/dev/null | cut -d'.' -f1)
    echo "   最后修改时间: $mod_time"
    
    # 用户数量
    user_count=$(sqlite3 data/auth.db "SELECT COUNT(*) FROM users;" 2>/dev/null || echo "0")
    echo "   用户数量: $user_count"
    
    if [ "$user_count" -gt 1 ]; then
        echo -e "${RED}🚨 警告: 发现多个用户账户！这不正常！${NC}"
        echo "   用户列表:"
        sqlite3 data/auth.db "SELECT username, created_at FROM users;" 2>/dev/null
        echo ""
        echo -e "${RED}⚠️  建议: 立即备份并删除此数据库，重新设置密码${NC}"
    elif [ "$user_count" -eq 1 ]; then
        echo "   用户信息:"
        sqlite3 data/auth.db "SELECT username, created_at FROM users;" 2>/dev/null
    fi
    
    # 创建备份
    backup_file="data/auth.db.backup.$(date +%Y%m%d_%H%M%S)"
    cp data/auth.db "$backup_file"
    echo -e "${GREEN}✅ 已备份到: $backup_file${NC}"
else
    echo -e "${YELLOW}⚠️  认证数据库不存在${NC}"
fi
echo ""

# 3. 检查配置文件
echo "3️⃣  检查配置文件"
echo "-------------------"
if [ -f "config.yaml" ]; then
    echo "📄 配置文件信息:"
    ls -lh config.yaml
    echo ""
    
    mod_time=$(stat -c "%y" config.yaml 2>/dev/null | cut -d'.' -f1)
    echo "   最后修改时间: $mod_time"
    
    # 显示关键配置（隐藏敏感信息）
    echo ""
    echo "   当前配置摘要:"
    echo "   交易所: $(grep 'current_exchange:' config.yaml | awk '{print $2}')"
    echo "   交易对数量: $(grep -c 'symbol:' config.yaml)"
    
    # 创建备份
    backup_file="config.yaml.backup.$(date +%Y%m%d_%H%M%S)"
    cp config.yaml "$backup_file"
    echo -e "${GREEN}✅ 已备份到: $backup_file${NC}"
    
    # 检查是否有系统备份
    if [ -d "backups" ]; then
        backup_count=$(ls -1 backups/config_backup_*.yaml 2>/dev/null | wc -l)
        echo "   系统备份数量: $backup_count"
        if [ "$backup_count" -gt 0 ]; then
            echo "   最新的3个备份:"
            ls -lt backups/config_backup_*.yaml 2>/dev/null | head -3 | awk '{print "   - " $9 " (" $6" "$7" "$8")"}'
        fi
    fi
else
    echo -e "${RED}❌ 配置文件不存在！${NC}"
fi
echo ""

# 4. 检查日志中的可疑活动
echo "4️⃣  检查日志中的可疑活动"
echo "-------------------"
if [ -d "logs" ]; then
    echo "📝 最近的密码设置记录:"
    grep -h "设置密码\|PASSWORD_ALREADY_SET\|AUTH.*密码" logs/*.log 2>/dev/null | tail -10 || echo "   未找到"
    echo ""
    
    echo "📝 最近的配置初始化记录:"
    grep -h "配置初始化\|SECURITY.*配置\|setup/init" logs/*.log 2>/dev/null | tail -10 || echo "   未找到"
    echo ""
    
    echo "📝 失败的登录尝试:"
    grep -h "密码错误\|Unauthorized\|401" logs/*.log 2>/dev/null | tail -10 || echo "   未找到"
    echo ""
    
    echo "📝 访问IP统计（前10）:"
    grep -h "ClientIP" logs/*.log 2>/dev/null | grep -oE '([0-9]{1,3}\.){3}[0-9]{1,3}' | sort | uniq -c | sort -rn | head -10 || echo "   未找到IP记录"
    echo ""
    
    # 保存完整日志用于分析
    log_backup="logs_backup_$(date +%Y%m%d_%H%M%S).tar.gz"
    tar -czf "$log_backup" logs/ 2>/dev/null
    echo -e "${GREEN}✅ 日志已备份到: $log_backup${NC}"
else
    echo -e "${YELLOW}⚠️  日志目录不存在${NC}"
fi
echo ""

# 5. 检查 systemd 服务配置
echo "5️⃣  检查 systemd 服务配置"
echo "-------------------"
if [ -f "/etc/systemd/system/quantmesh.service" ]; then
    echo "📄 服务配置:"
    cat /etc/systemd/system/quantmesh.service
    echo ""
else
    echo -e "${YELLOW}⚠️  未找到 systemd 服务配置${NC}"
fi
echo ""

# 6. 检查网络连接
echo "6️⃣  检查网络连接"
echo "-------------------"
echo "监听的端口:"
netstat -tlnp 2>/dev/null | grep quantmesh || ss -tlnp 2>/dev/null | grep quantmesh || echo "   服务未运行"
echo ""

echo "最近的网络连接:"
netstat -tnp 2>/dev/null | grep quantmesh | head -10 || ss -tnp 2>/dev/null | grep quantmesh | head -10 || echo "   无活动连接"
echo ""

# 7. 检查防火墙规则
echo "7️⃣  检查防火墙规则"
echo "-------------------"
if command -v ufw > /dev/null; then
    echo "UFW 状态:"
    ufw status
elif command -v iptables > /dev/null; then
    echo "iptables 规则（仅显示 INPUT 链）:"
    iptables -L INPUT -n -v | head -20
else
    echo -e "${YELLOW}⚠️  未找到防火墙工具${NC}"
fi
echo ""

# 8. 生成安全报告
echo "8️⃣  生成安全报告"
echo "-------------------"
report_file="security_report_$(date +%Y%m%d_%H%M%S).txt"

{
    echo "QuantMesh 安全检查报告"
    echo "======================"
    echo "服务器: $(hostname)"
    echo "检查时间: $(date)"
    echo "工作目录: $(pwd)"
    echo ""
    
    echo "## 认证数据库"
    if [ -f "data/auth.db" ]; then
        ls -lh data/auth.db
        echo "用户数量: $(sqlite3 data/auth.db 'SELECT COUNT(*) FROM users;' 2>/dev/null || echo '0')"
        sqlite3 data/auth.db "SELECT username, created_at FROM users;" 2>/dev/null || echo "无法读取"
    else
        echo "不存在"
    fi
    echo ""
    
    echo "## 配置文件"
    if [ -f "config.yaml" ]; then
        ls -lh config.yaml
        echo "交易所: $(grep 'current_exchange:' config.yaml | awk '{print $2}')"
    else
        echo "不存在"
    fi
    echo ""
    
    echo "## 可疑日志"
    echo "### 密码设置:"
    grep -h "设置密码\|PASSWORD_ALREADY_SET" logs/*.log 2>/dev/null | tail -20 || echo "无"
    echo ""
    echo "### 配置初始化:"
    grep -h "配置初始化\|SECURITY.*配置" logs/*.log 2>/dev/null | tail -20 || echo "无"
    echo ""
    
    echo "## 访问IP"
    grep -h "ClientIP" logs/*.log 2>/dev/null | grep -oE '([0-9]{1,3}\.){3}[0-9]{1,3}' | sort | uniq -c | sort -rn | head -20 || echo "无"
    
} > "$report_file"

echo -e "${GREEN}✅ 安全报告已生成: $report_file${NC}"
echo ""

# 9. 修复建议
echo "9️⃣  修复建议"
echo "-------------------"
echo ""
echo -e "${YELLOW}⚠️  紧急修复步骤:${NC}"
echo ""
echo "1. 查看安全报告:"
echo "   cat $report_file"
echo ""
echo "2. 如果发现可疑活动，重置密码:"
echo "   rm data/auth.db"
echo ""
echo "3. 如果配置被篡改，从备份恢复:"
echo "   ls backups/"
echo "   cp backups/config_backup_YYYYMMDD_HHMMSS.yaml config.yaml"
echo ""
echo "4. 更新到最新版本（包含安全修复）:"
echo "   git pull origin main"
echo "   go build -o quantmesh"
echo ""
echo "5. 配置防火墙（只允许你的IP访问）:"
echo "   ufw allow from YOUR_IP to any port 8080"
echo "   ufw enable"
echo ""
echo "6. 或者使用 Nginx 反向代理 + IP 白名单:"
echo "   参考: docs/SECURITY_FIX_SETUP_AUTH.md"
echo ""
echo "7. 重启服务:"
echo "   systemctl start quantmesh"
echo ""

# 10. 询问是否立即修复
echo ""
echo -e "${RED}================================${NC}"
echo -e "${RED}是否立即执行修复？${NC}"
echo -e "${RED}================================${NC}"
echo ""
echo "这将执行以下操作:"
echo "1. 删除认证数据库（需要重新设置密码）"
echo "2. 更新到最新版本（包含安全修复）"
echo "3. 重新编译"
echo ""
read -p "是否继续？(yes/no): " confirm

if [ "$confirm" = "yes" ]; then
    echo ""
    echo "开始修复..."
    echo ""
    
    # 删除认证数据库
    if [ -f "data/auth.db" ]; then
        echo "删除认证数据库..."
        rm data/auth.db
        echo -e "${GREEN}✅ 已删除${NC}"
    fi
    
    # 更新代码
    echo "更新代码..."
    git fetch origin
    git pull origin main
    echo -e "${GREEN}✅ 代码已更新${NC}"
    
    # 重新编译
    echo "重新编译..."
    go build -o quantmesh
    echo -e "${GREEN}✅ 编译完成${NC}"
    
    # 重启服务
    echo "重启服务..."
    systemctl start quantmesh
    sleep 2
    
    if systemctl is-active --quiet quantmesh; then
        echo -e "${GREEN}✅ 服务已启动${NC}"
    else
        echo -e "${RED}❌ 服务启动失败，请检查日志${NC}"
        journalctl -u quantmesh -n 50
    fi
    
    echo ""
    echo -e "${GREEN}✅ 修复完成！${NC}"
    echo ""
    echo "下一步:"
    echo "1. 访问 Web UI 重新设置密码"
    echo "2. 配置防火墙或反向代理"
    echo "3. 定期检查日志"
else
    echo ""
    echo "已取消自动修复"
    echo "请手动执行修复步骤"
fi

echo ""
echo -e "${GREEN}================================${NC}"
echo -e "${GREEN}检查完成！${NC}"
echo -e "${GREEN}================================${NC}"
echo ""
echo "生成的文件:"
echo "- 安全报告: $report_file"
echo "- 认证数据库备份: $backup_file (如果存在)"
echo "- 配置文件备份: config.yaml.backup.*"
echo "- 日志备份: $log_backup"
echo ""
