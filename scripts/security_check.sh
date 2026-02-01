#!/bin/bash

# 安全检查脚本
# 用于检查系统是否被未授权访问或配置被篡改

set -e

echo "🔍 QuantMesh 安全检查"
echo "===================="
echo ""

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 检查函数
check_file_modification() {
    local file=$1
    local description=$2
    
    if [ -f "$file" ]; then
        local mod_time=$(stat -f "%Sm" -t "%Y-%m-%d %H:%M:%S" "$file" 2>/dev/null || stat -c "%y" "$file" 2>/dev/null | cut -d'.' -f1)
        echo "📄 $description"
        echo "   文件: $file"
        echo "   修改时间: $mod_time"
        echo ""
    else
        echo "⚠️  $description"
        echo "   文件不存在: $file"
        echo ""
    fi
}

# 1. 检查认证数据库
echo "1️⃣  检查认证数据库"
echo "-------------------"
if [ -f "data/auth.db" ]; then
    check_file_modification "data/auth.db" "认证数据库"
    
    # 检查用户数量
    user_count=$(sqlite3 data/auth.db "SELECT COUNT(*) FROM users;" 2>/dev/null || echo "无法读取")
    echo "   用户数量: $user_count"
    
    if [ "$user_count" != "无法读取" ] && [ "$user_count" -gt 1 ]; then
        echo -e "   ${RED}⚠️  警告: 发现多个用户账户！${NC}"
        sqlite3 data/auth.db "SELECT username, created_at FROM users;" 2>/dev/null
    fi
    
    # 检查最近创建的用户
    if [ "$user_count" != "无法读取" ] && [ "$user_count" -gt 0 ]; then
        echo "   最近的用户:"
        sqlite3 data/auth.db "SELECT username, created_at FROM users ORDER BY created_at DESC LIMIT 3;" 2>/dev/null || echo "   无法读取用户信息"
    fi
else
    echo -e "${YELLOW}⚠️  认证数据库不存在（系统可能未初始化）${NC}"
fi
echo ""

# 2. 检查配置文件
echo "2️⃣  检查配置文件"
echo "-------------------"
if [ -f "config.yaml" ]; then
    check_file_modification "config.yaml" "主配置文件"
    
    # 检查配置文件权限
    if [ "$(uname)" = "Darwin" ]; then
        perms=$(stat -f "%Lp" config.yaml)
    else
        perms=$(stat -c "%a" config.yaml)
    fi
    
    if [ "$perms" != "600" ] && [ "$perms" != "400" ]; then
        echo -e "   ${YELLOW}⚠️  警告: 配置文件权限不安全 ($perms)${NC}"
        echo "   建议执行: chmod 600 config.yaml"
    else
        echo -e "   ${GREEN}✅ 配置文件权限正常 ($perms)${NC}"
    fi
    
    # 检查是否有备份
    backup_count=$(ls -1 backups/config_backup_*.yaml 2>/dev/null | wc -l | tr -d ' ')
    echo "   配置备份数量: $backup_count"
    
    if [ "$backup_count" -gt 0 ]; then
        echo "   最近的备份:"
        ls -lt backups/config_backup_*.yaml 2>/dev/null | head -3 | awk '{print "   - " $9 " (" $6" "$7" "$8")"}'
    fi
else
    echo -e "${YELLOW}⚠️  配置文件不存在${NC}"
fi
echo ""

# 3. 检查日志中的可疑活动
echo "3️⃣  检查日志中的可疑活动"
echo "-------------------"
if [ -d "logs" ]; then
    # 检查设置密码的日志
    echo "📝 密码设置记录:"
    grep -h "设置密码\|PASSWORD_ALREADY_SET" logs/*.log 2>/dev/null | tail -5 || echo "   未找到相关日志"
    echo ""
    
    # 检查配置初始化的日志
    echo "📝 配置初始化记录:"
    grep -h "配置初始化\|SECURITY.*配置" logs/*.log 2>/dev/null | tail -5 || echo "   未找到相关日志"
    echo ""
    
    # 检查失败的登录尝试
    echo "📝 失败的登录尝试:"
    grep -h "密码错误\|Unauthorized" logs/*.log 2>/dev/null | tail -5 || echo "   未找到相关日志"
    echo ""
    
    # 检查来自不同IP的请求
    echo "📝 最近的访问IP:"
    grep -h "ClientIP\|IP=" logs/*.log 2>/dev/null | grep -oE '([0-9]{1,3}\.){3}[0-9]{1,3}' | sort | uniq -c | sort -rn | head -10 || echo "   未找到IP记录"
else
    echo -e "${YELLOW}⚠️  日志目录不存在${NC}"
fi
echo ""

# 4. 检查系统进程
echo "4️⃣  检查系统进程"
echo "-------------------"
if pgrep -f "quantmesh" > /dev/null; then
    echo -e "${GREEN}✅ QuantMesh 进程正在运行${NC}"
    ps aux | grep "[q]uantmesh" | awk '{print "   PID: " $2 ", CPU: " $3 "%, MEM: " $4 "%"}'
else
    echo -e "${YELLOW}⚠️  QuantMesh 进程未运行${NC}"
fi
echo ""

# 5. 检查网络监听
echo "5️⃣  检查网络监听"
echo "-------------------"
if command -v lsof > /dev/null; then
    echo "监听的端口:"
    lsof -i -P -n | grep LISTEN | grep quantmesh || echo "   未找到监听端口"
elif command -v netstat > /dev/null; then
    echo "监听的端口:"
    netstat -an | grep LISTEN | grep 8080 || echo "   未找到监听端口"
else
    echo "⚠️  无法检查网络监听（需要 lsof 或 netstat）"
fi
echo ""

# 6. 安全建议
echo "6️⃣  安全建议"
echo "-------------------"

# 检查是否监听在 0.0.0.0
if lsof -i -P -n 2>/dev/null | grep LISTEN | grep quantmesh | grep "0.0.0.0" > /dev/null; then
    echo -e "${RED}⚠️  警告: 系统监听在 0.0.0.0，可能暴露到公网！${NC}"
    echo "   建议: 使用防火墙或反向代理限制访问"
    echo ""
fi

# 检查配置文件权限
if [ -f "config.yaml" ]; then
    if [ "$(uname)" = "Darwin" ]; then
        perms=$(stat -f "%Lp" config.yaml)
    else
        perms=$(stat -c "%a" config.yaml)
    fi
    
    if [ "$perms" != "600" ] && [ "$perms" != "400" ]; then
        echo -e "${YELLOW}⚠️  建议: 限制配置文件权限${NC}"
        echo "   执行: chmod 600 config.yaml"
        echo ""
    fi
fi

# 检查认证数据库权限
if [ -f "data/auth.db" ]; then
    if [ "$(uname)" = "Darwin" ]; then
        perms=$(stat -f "%Lp" data/auth.db)
    else
        perms=$(stat -c "%a" data/auth.db)
    fi
    
    if [ "$perms" != "600" ] && [ "$perms" != "400" ]; then
        echo -e "${YELLOW}⚠️  建议: 限制认证数据库权限${NC}"
        echo "   执行: chmod 600 data/auth.db"
        echo ""
    fi
fi

echo -e "${GREEN}✅ 安全检查完成${NC}"
echo ""
echo "如果发现可疑活动，请参考: docs/SECURITY_FIX_SETUP_AUTH.md"
echo ""
