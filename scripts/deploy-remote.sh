#!/bin/bash
# 远程部署 QuantMesh 到指定服务器
# 用法: ./deploy-remote.sh root@facev.app
# 或:   bash deploy-remote.sh root@facev.app
#
# 公开仓库无需 GitHub Token，直接 wget 下载

set -e

TARGET="${1:-root@facev.app}"
VERSION="3.56.2-rc1"
DOWNLOAD_URL="https://github.com/ghostsworm/quantmesh/releases/download/v${VERSION}/quantmesh-${VERSION}-linux-amd64.tar.gz"
INSTALL_DIR="/opt/quantmesh"
SERVICE_NAME="quantmesh"

echo "=== QuantMesh 远程部署 ==="
echo "目标: $TARGET"
echo "版本: $VERSION"
echo ""

ssh "$TARGET" bash -s << REMOTE_SCRIPT
set -e
echo "[1/6] 停止服务..."
systemctl stop $SERVICE_NAME 2>/dev/null || true

echo "[2/6] 下载 release..."
cd /tmp
rm -f quantmesh-${VERSION}-linux-amd64.tar.gz
wget -q "$DOWNLOAD_URL" -O quantmesh-${VERSION}-linux-amd64.tar.gz

echo "[3/6] 解压..."
rm -rf quantmesh-${VERSION}-linux-amd64
tar -xzf quantmesh-${VERSION}-linux-amd64.tar.gz

echo "[4/6] 备份并替换二进制..."
mkdir -p $INSTALL_DIR
[ -f $INSTALL_DIR/quantmesh ] && cp $INSTALL_DIR/quantmesh $INSTALL_DIR/quantmesh.bak
cp quantmesh-${VERSION}-linux-amd64/quantmesh-linux-amd64 $INSTALL_DIR/quantmesh
chmod +x $INSTALL_DIR/quantmesh
id quantmesh &>/dev/null && chown quantmesh:quantmesh $INSTALL_DIR/quantmesh || true

echo "[5/6] 清理临时文件..."
rm -rf quantmesh-${VERSION}-linux-amd64 quantmesh-${VERSION}-linux-amd64.tar.gz

echo "[6/6] 启动服务..."
systemctl daemon-reload 2>/dev/null || true
systemctl start $SERVICE_NAME
systemctl status $SERVICE_NAME --no-pager

echo ""
echo "=== 部署完成 ==="
$INSTALL_DIR/quantmesh --version 2>/dev/null || true
REMOTE_SCRIPT

echo ""
echo "部署成功！访问 http://facev.app:28888 或 https://facev.app (若已配置反向代理)"
