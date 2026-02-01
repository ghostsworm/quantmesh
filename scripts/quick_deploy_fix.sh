#!/bin/bash

# 快速部署安全修复脚本
# 在服务器上运行此脚本以应用安全修复

echo "🔧 QuantMesh 安全修复快速部署"
echo "================================"
echo ""

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# 检查是否在正确的目录
if [ ! -f "main.go" ]; then
    echo -e "${RED}❌ 错误: 请在 quantmesh 项目根目录运行此脚本${NC}"
    exit 1
fi

echo -e "${GREEN}✅ 当前目录: $(pwd)${NC}"
echo ""

# 1. 备份当前文件
echo "1️⃣  备份当前文件"
echo "-------------------"
backup_dir="backup_before_fix_$(date +%Y%m%d_%H%M%S)"
mkdir -p "$backup_dir"

if [ -f "config.yaml" ]; then
    cp config.yaml "$backup_dir/"
    echo "✅ 已备份 config.yaml"
fi

if [ -f "data/auth.db" ]; then
    cp data/auth.db "$backup_dir/"
    echo "✅ 已备份 auth.db"
fi

if [ -d "logs" ]; then
    cp -r logs "$backup_dir/"
    echo "✅ 已备份 logs"
fi

echo -e "${GREEN}✅ 备份完成: $backup_dir${NC}"
echo ""

# 2. 停止服务
echo "2️⃣  停止服务"
echo "-------------------"
if systemctl is-active --quiet quantmesh; then
    echo "停止 quantmesh 服务..."
    systemctl stop quantmesh
    echo -e "${GREEN}✅ 服务已停止${NC}"
else
    echo "ℹ️  服务未运行"
fi
echo ""

# 3. 更新代码
echo "3️⃣  更新代码"
echo "-------------------"
echo "拉取最新代码..."
git fetch origin
git pull origin main

if [ $? -eq 0 ]; then
    echo -e "${GREEN}✅ 代码更新成功${NC}"
else
    echo -e "${RED}❌ 代码更新失败${NC}"
    exit 1
fi
echo ""

# 4. 显示更新内容
echo "4️⃣  更新内容"
echo "-------------------"
echo "最近的提交:"
git log -3 --oneline
echo ""

# 5. 重新编译
echo "5️⃣  重新编译"
echo "-------------------"
echo "编译中..."
go build -o quantmesh

if [ $? -eq 0 ]; then
    echo -e "${GREEN}✅ 编译成功${NC}"
    ls -lh quantmesh
else
    echo -e "${RED}❌ 编译失败${NC}"
    exit 1
fi
echo ""

# 6. 检查安全修复
echo "6️⃣  验证安全修复"
echo "-------------------"
if grep -q "PASSWORD_ALREADY_SET" web/api_auth.go; then
    echo -e "${GREEN}✅ 密码设置接口已修复${NC}"
else
    echo -e "${YELLOW}⚠️  密码设置接口可能未修复${NC}"
fi

if grep -q "系统已初始化，需要登录后才能修改配置" web/api_setup.go; then
    echo -e "${GREEN}✅ 配置初始化接口已修复${NC}"
else
    echo -e "${YELLOW}⚠️  配置初始化接口可能未修复${NC}"
fi
echo ""

# 7. 重启服务
echo "7️⃣  重启服务"
echo "-------------------"
echo "启动 quantmesh 服务..."
systemctl start quantmesh
sleep 3

if systemctl is-active --quiet quantmesh; then
    echo -e "${GREEN}✅ 服务已启动${NC}"
    echo ""
    echo "服务状态:"
    systemctl status quantmesh --no-pager -l
else
    echo -e "${RED}❌ 服务启动失败${NC}"
    echo ""
    echo "错误日志:"
    journalctl -u quantmesh -n 30 --no-pager
    exit 1
fi
echo ""

# 8. 安全建议
echo "8️⃣  安全建议"
echo "-------------------"
echo ""
echo -e "${YELLOW}⚠️  重要: 配置防火墙${NC}"
echo ""
echo "方案1: 使用 UFW（推荐）"
echo "  # 只允许你的IP访问"
echo "  ufw allow from YOUR_IP to any port 8080"
echo "  ufw enable"
echo ""
echo "方案2: 使用 Nginx 反向代理"
echo "  # 安装 nginx"
echo "  apt install nginx"
echo "  # 配置反向代理和IP白名单"
echo "  # 参考: docs/SECURITY_FIX_SETUP_AUTH.md"
echo ""
echo "方案3: 使用 SSH 隧道（最安全）"
echo "  # 在本地运行:"
echo "  ssh -L 8080:localhost:8080 root@facev.app"
echo "  # 然后访问 http://localhost:8080"
echo ""

# 9. 下一步
echo "9️⃣  下一步操作"
echo "-------------------"
echo ""
echo "1. 如果密码被重置，访问 Web UI 重新设置:"
echo "   http://facev.app:8080"
echo ""
echo "2. 检查配置是否正确:"
echo "   cat config.yaml"
echo ""
echo "3. 查看日志确认无异常:"
echo "   tail -f logs/quantmesh.log"
echo ""
echo "4. 运行完整的安全检查:"
echo "   ./scripts/security_check.sh"
echo ""

echo -e "${GREEN}================================${NC}"
echo -e "${GREEN}部署完成！${NC}"
echo -e "${GREEN}================================${NC}"
echo ""
echo "备份位置: $backup_dir"
echo ""
