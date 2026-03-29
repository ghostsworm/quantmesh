# QuantMesh 部署指南

## 本地重新部署

如果只是修复了代码，需要重新编译并重启服务：

```bash
# 强制重新编译并启动
./start.sh -f

# 或者只重启（如果代码没有变化）
./restart.sh
```

## 部署到远程服务器

### 方式一：使用完整部署脚本（推荐）

#### 1. 创建配置文件

```bash
# 复制配置示例
cp scripts/deploy-config.sh.example scripts/deploy-config.sh

# 编辑配置文件
vim scripts/deploy-config.sh
```

配置内容：
```bash
#!/bin/bash

# 远程服务器配置
REMOTE_HOST="your-server.com"           # 服务器地址
REMOTE_USER="deploy"                     # SSH用户名
REMOTE_PORT="22"                         # SSH端口
REMOTE_PATH="/opt/quantmesh"             # 远程部署路径

# SSH配置
SSH_KEY="~/.ssh/id_rsa"                 # SSH私钥路径

# 服务配置
SERVICE_NAME="quantmesh"                 # systemd服务名称
HEALTH_CHECK_URL="http://localhost:28888/api/status"
```

#### 2. 执行部署

```bash
# 使用配置文件部署
./scripts/deploy-to-server.sh --config scripts/deploy-config.sh

# 或者直接指定参数
./scripts/deploy-to-server.sh \
  --host your-server.com \
  --user deploy \
  --key ~/.ssh/id_rsa \
  --path /opt/quantmesh
```

#### 3. Mac环境使用Docker编译

如果您的Mac无法直接编译Linux版本，可以使用Docker：

```bash
./scripts/deploy-to-server.sh \
  --host your-server.com \
  --user deploy \
  --docker
```

### 方式二：手动部署

#### 1. 本地编译（Mac需要Docker）

**使用Docker编译（推荐）：**
```bash
# 构建前端
cd webui
yarn install
yarn build
cd ..
cp -r webui/dist web/dist

# 使用Docker编译后端
docker run --rm \
  -v "$PWD":/app \
  -w /app \
  -e CGO_ENABLED=1 \
  golang:1.21 \
  bash -c "go mod download && go build -ldflags='-s -w' -o quantmesh ."
```

**本地编译（如果目标也是Mac）：**
```bash
# 构建前端
cd webui
yarn install
yarn build
cd ..
cp -r webui/dist web/dist

# 构建后端
export CGO_ENABLED=1
go build -ldflags="-s -w" -o quantmesh .
```

#### 2. 上传到服务器

```bash
# 上传二进制文件
scp -i ~/.ssh/id_rsa quantmesh user@server:/opt/quantmesh/

# 上传配置文件（如果需要更新）
scp -i ~/.ssh/id_rsa config.yaml user@server:/opt/quantmesh/
```

#### 3. 在服务器上部署

```bash
# SSH连接到服务器
ssh user@server

# 进入部署目录
cd /opt/quantmesh

# 备份数据库
./scripts/backup.sh

# 停止服务
sudo systemctl stop quantmesh
# 或
pkill -f quantmesh

# 设置执行权限
chmod +x quantmesh

# 启动服务
sudo systemctl start quantmesh
# 或
nohup ./quantmesh config.yaml > logs/quantmesh.log 2>&1 &

# 检查服务状态
sudo systemctl status quantmesh
# 或
ps aux | grep quantmesh

# 健康检查
curl http://localhost:28888/api/status
```

## 部署脚本功能说明

`deploy-to-server.sh` 脚本会自动完成以下步骤：

1. ✅ **验证SSH连接** - 确保可以连接到服务器
2. ✅ **构建前端** - 自动构建React前端
3. ✅ **构建后端** - 本地编译或使用Docker编译
4. ✅ **备份数据库** - 自动备份远程数据库
5. ✅ **上传文件** - 通过SCP上传二进制文件
6. ✅ **重启服务** - 自动重启systemd服务或进程
7. ✅ **健康检查** - 验证服务是否正常启动

## 常见问题

### 1. SSH连接失败

**问题：** 无法连接到服务器

**解决：**
- 检查SSH密钥是否正确
- 检查服务器地址和端口
- 检查防火墙设置
- 测试SSH连接：`ssh -i ~/.ssh/id_rsa user@server`

### 2. 权限问题

**问题：** 无法写入文件或启动服务

**解决：**
- 确保SSH用户有写入权限
- 如果使用systemd，确保用户有sudo权限
- 检查文件权限：`chmod +x quantmesh`

### 3. 编译失败

**问题：** Docker编译或本地编译失败

**解决：**
- Mac环境推荐使用 `--docker` 选项
- 确保Docker已安装并运行
- 检查Go版本和依赖：`go mod download`

### 4. 服务启动失败

**问题：** 部署后服务无法启动

**解决：**
- 查看日志：`journalctl -u quantmesh -n 100`
- 检查配置文件：`./quantmesh --check-config`
- 检查端口占用：`lsof -i :28888`
- 检查数据库文件权限

### 5. 健康检查失败

**问题：** 部署后健康检查失败

**解决：**
- 等待几秒后重试（服务可能需要时间启动）
- 检查服务日志
- 检查防火墙和端口配置
- 验证配置文件是否正确

## 回滚

如果部署后出现问题，可以回滚到之前的版本：

```bash
# 在服务器上执行
cd /opt/quantmesh

# 停止服务
sudo systemctl stop quantmesh

# 恢复备份
./scripts/restore.sh backups/db_backup_YYYYMMDD_HHMMSS

# 恢复旧版本二进制文件（如果有备份）
cp backups/quantmesh_old quantmesh

# 启动服务
sudo systemctl start quantmesh
```

## 注意事项

1. **数据库保护**：部署脚本会自动备份数据库，但建议在部署前手动备份重要数据
2. **配置文件**：部署不会覆盖 `config.yaml`，需要手动更新
3. **服务管理**：如果使用systemd，确保服务配置正确
4. **端口冲突**：确保部署时端口未被占用
5. **依赖检查**：确保服务器上已安装所有必需的依赖

## 快速参考

```bash
# 本地重新部署
./start.sh -f

# 部署到服务器（使用配置文件）
./scripts/deploy-to-server.sh --config scripts/deploy-config.sh

# 部署到服务器（直接指定参数）
./scripts/deploy-to-server.sh --host server.com --user deploy --docker

# 查看部署帮助
./scripts/deploy-to-server.sh --help
```

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
