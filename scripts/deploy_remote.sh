#!/bin/bash
# 遠程編譯部署腳本
# 在 facev.app 伺服器上編譯 quantmesh 項目

set -e

SERVER="root@facev.app"
VERSION="${1:-latest}"

echo "========================================="
echo "遠程編譯部署腳本"
echo "版本: $VERSION"
echo "========================================="

# 1. 清理遠程環境並複製代碼
echo ">>> 步驟 1: 清理遠程環境並複製代碼..."
ssh $SERVER "rm -rf /root/quantmesh-build /root/.yarn/cache/*"
git archive HEAD | gzip | ssh $SERVER "mkdir -p /root/quantmesh-build/quantmesh && tar xzf - -C /root/quantmesh-build/quantmesh"

# 2. 驗證代碼已正確複製
echo ">>> 步驟 2: 驗證代碼..."
ssh $SERVER "head -60 /root/quantmesh-build/quantmesh/webui/src/components/BotBacktestDialog.tsx | grep -q '@chakra-ui/react' && echo '✓ BotBacktestDialog.tsx 使用 Chakra UI' || echo '✗ BotBacktestDialog.tsx 未使用 Chakra UI'"

# 3. 在遠程伺服器上編譯
echo ">>> 步驟 3: 在遠程伺服器上編譯..."
ssh $SERVER bash << 'END_OF_SCRIPT'
set -e

REPO_DIR="/root/quantmesh-build/quantmesh"
cd $REPO_DIR

echo "環境信息:"
echo "Go 版本: $(go version)"
echo "Node 版本: $(node --version)"
echo "Yarn 版本: $(yarn --version)"

# 先編譯前端（Go 編譯需要 dist/ 目錄）
echo ">>> 編譯前端..."
cd webui
rm -rf node_modules dist .yarn/cache
yarn install --frozen-lockfile
yarn build
cd ..

# 編譯 Go 後端
echo ">>> 編譯 Go 後端..."
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w -X main.Version=$VERSION" -o quantmesh .

echo "編譯完成!"
END_OF_SCRIPT

# 4. 下載編譯好的二進制文件
echo ">>> 步驟 4: 下載編譯結果..."
mkdir -p ./dist
scp $SERVER:/root/quantmesh-build/quantmesh/quantmesh ./dist/
scp -r $SERVER:/root/quantmesh-build/quantmesh/webui/dist ./dist/webui-dist

echo "========================================="
echo "編譯完成!"
echo "二進制文件: ./dist/quantmesh"
echo "前端文件: ./dist/webui-dist"
echo "========================================="