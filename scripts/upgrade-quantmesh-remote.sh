#!/bin/bash
# QuantMesh 远程升级脚本
# 用法: 在服务器上执行 ~/upgrade-quantmesh.sh，或本地执行: ssh root@facev.app '~/upgrade-quantmesh.sh'

set -e

REPO="ghostsworm/quantmesh"
QT_DIR="$HOME/qt"

echo "[QuantMesh] 开始升级..."

# 1. 创建并进入 qt 目录，清空
mkdir -p "$QT_DIR"
cd "$QT_DIR"
rm -rf "$QT_DIR"/*

# 2. 获取最新 release 下载地址
echo "[QuantMesh] 获取最新 release..."
RELEASE_URL=$(curl -sL "https://api.github.com/repos/$REPO/releases/latest" | sed -n 's/.*"browser_download_url": "\([^"]*linux-amd64[^"]*\)".*/\1/p' | head -1)

if [ -z "$RELEASE_URL" ]; then
  echo "[QuantMesh] 错误: 无法获取下载地址"
  exit 1
fi

echo "[QuantMesh] 下载: $RELEASE_URL"
curl -fsSL "$RELEASE_URL" -o quantmesh.tar.gz

# 3. 解压
echo "[QuantMesh] 解压..."
tar -xzf quantmesh.tar.gz

# 4. 进入解压目录并执行静默升级
cd quantmesh-*-linux-amd64
echo "[QuantMesh] 执行 install.sh --silent-upgrade..."
./install.sh --silent-upgrade

echo "[QuantMesh] 升级完成!"
