# QuantMesh Git 部署脚本使用说明

## 概述

`deploy-git.sh` 是一个完整的 Git 部署脚本，使用 Git 方式同步代码，并通过 systemd 管理服务。

## 功能特点

✅ **自动提交推送** - 自动检测本地修改并推送到 GitHub  
✅ **Git 同步** - 从 GitHub 拉取最新代码到服务器  
✅ **自动编译** - 在服务器上自动构建前端和后端  
✅ **数据库保护** - 自动备份数据库，不会覆盖现有数据  
✅ **Systemd 管理** - 使用 systemd 管理服务，支持自动重启  
✅ **健康检查** - 部署后自动验证服务状态  

## 使用方法

### 基本使用

```bash
# 自动提交本地修改并部署
./scripts/deploy-git.sh

# 跳过自动提交（如果代码已经推送）
./scripts/deploy-git.sh --skip-push
```

### 部署流程

1. **本地提交推送**（可选）
   - 自动检测未提交的修改
   - 自动提交并推送到 GitHub

2. **服务器更新代码**
   - 从 GitHub 拉取最新代码
   - 使用 `git pull` 或 `git clone`

3. **构建项目**
   - 构建前端（React）
   - 构建后端（Go）

4. **备份数据库**
   - 自动备份现有数据库文件

5. **重启服务**
   - 使用 systemd 重启服务
   - 自动健康检查

## 配置

脚本中的配置（可在脚本中修改）：

```bash
REMOTE_HOST="facev.app"                    # 服务器地址
REMOTE_USER="root"                         # SSH用户名
REMOTE_PATH="/root/quntmesh"              # 部署路径
GIT_REPO="git@github.com:ghostsworm/quantmesh.git"  # Git仓库
GIT_BRANCH="main"                          # Git分支
SERVICE_NAME="quantmesh"                   # systemd服务名
```

## Systemd 服务管理

部署脚本会自动创建 systemd 服务（如果不存在）。

### 常用命令

```bash
# 查看服务状态
ssh root@facev.app 'systemctl status quantmesh'

# 查看日志
ssh root@facev.app 'journalctl -u quantmesh -f'

# 重启服务
ssh root@facev.app 'systemctl restart quantmesh'

# 停止服务
ssh root@facev.app 'systemctl stop quantmesh'

# 启动服务
ssh root@facev.app 'systemctl start quantmesh'
```

## 注意事项

1. **Git 权限**：确保服务器可以访问 GitHub 仓库（SSH key 或 HTTPS token）
2. **数据库保护**：脚本会自动备份数据库，但建议定期手动备份
3. **配置文件**：`config.yaml` 不会被覆盖，需要手动更新
4. **服务状态**：部署后检查服务状态确保正常运行

## 故障排查

### 1. Git 拉取失败

```bash
# 检查 Git 配置
ssh root@facev.app "cd /root/quntmesh && git remote -v"

# 手动拉取
ssh root@facev.app "cd /root/quntmesh && git pull origin main"
```

### 2. 编译失败

```bash
# 检查 Go 环境
ssh root@facev.app "go version"

# 检查 Node.js 环境
ssh root@facev.app "node --version && yarn --version"
```

### 3. 服务启动失败

```bash
# 查看详细日志
ssh root@facev.app "journalctl -u quantmesh -n 100 --no-pager"

# 检查配置文件
ssh root@facev.app "cd /root/quntmesh && ./quantmesh --help"
```

## 与 rsync 部署的区别

| 特性 | Git 部署 | rsync 部署 |
|------|---------|-----------|
| 代码同步 | Git pull/clone | rsync 上传 |
| 版本控制 | ✅ 有版本历史 | ❌ 无版本历史 |
| 部署速度 | 较快（增量更新） | 较慢（全量上传） |
| 网络要求 | 需要访问 GitHub | 需要 SSH 连接 |
| 适用场景 | 生产环境推荐 | 开发环境或内网 |

## 快速参考

```bash
# 完整部署流程
./scripts/deploy-git.sh

# 查看部署后的服务状态
ssh root@facev.app 'systemctl status quantmesh'

# 查看实时日志
ssh root@facev.app 'journalctl -u quantmesh -f'
```

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
