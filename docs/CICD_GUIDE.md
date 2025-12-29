# CI/CD 指南

本文档介绍 QuantMesh 的持续集成和持续部署流程。

## 概述

QuantMesh 使用 GitHub Actions 实现自动化的 CI/CD 流程：

- **CI（持续集成）**: 自动测试、代码检查、构建
- **CD（持续部署）**: 自动发布、部署到 staging/production

## CI 流程

### 触发条件

- 推送到 `main` 或 `develop` 分支
- 创建 Pull Request

### CI 步骤

1. **代码检查**
   - `go vet` - 静态分析
   - `go fmt` - 代码格式检查
   - `golangci-lint` - 代码质量检查

2. **安全扫描**
   - `gosec` - 安全漏洞扫描
   - 依赖漏洞检查

3. **单元测试**
   - 运行所有测试
   - 生成覆盖率报告
   - 上传到 Codecov

4. **构建**
   - 多平台构建（Linux/macOS, amd64/arm64）
   - Docker 镜像构建

### 本地运行 CI 检查

```bash
# 代码格式检查
gofmt -l .

# 静态分析
go vet ./...

# 运行测试
go test -v -race -coverprofile=coverage.out ./...

# 代码质量检查（需要安装 golangci-lint）
golangci-lint run

# 安全扫描（需要安装 gosec）
gosec ./...
```

## CD 流程

### 发布流程

#### 1. 创建 Release

```bash
# 确保在 main 分支
git checkout main
git pull origin main

# 创建并推送 tag
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
```

#### 2. 自动化流程

推送 tag 后，GitHub Actions 会自动：

1. 运行完整的 CI 流程
2. 构建多平台二进制文件
3. 构建 Docker 镜像
4. 创建 GitHub Release
5. 上传构建产物
6. 部署到生产环境（如果是正式版本）

### 部署流程

#### Staging 部署

```bash
# 通过 GitHub Actions 手动触发
# 在 GitHub 仓库页面：Actions -> CD -> Run workflow
# 选择 environment: staging
```

#### Production 部署

方式 1：自动部署（推荐）
```bash
# 推送正式版本 tag（不包含 -alpha, -beta, -rc 等后缀）
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
```

方式 2：手动触发
```bash
# 在 GitHub Actions 中手动触发
# 选择 environment: production
```

### 部署架构

```
┌─────────────┐
│   GitHub    │
│   Actions   │
└──────┬──────┘
       │
       ├──────────────┐
       │              │
   ┌───▼────┐    ┌───▼────┐
   │Staging │    │  Prod  │
   │ Server │    │ Server │
   └────────┘    └────────┘
```

## Docker 部署

### 构建镜像

```bash
# 本地构建
docker build -t quantmesh/market-maker:latest .

# 多平台构建
docker buildx build --platform linux/amd64,linux/arm64 -t quantmesh/market-maker:latest .
```

### 运行容器

```bash
# 基本运行
docker run -d \
  --name quantmesh \
  -p 28888:28888 \
  -v $(pwd)/config.yaml:/app/config.yaml \
  -v $(pwd)/data:/app/data \
  quantmesh/market-maker:latest

# 使用 Docker Compose
docker-compose up -d
```

### Docker Compose 配置

创建 `docker-compose.yml`：

```yaml
version: '3.8'

services:
  quantmesh:
    image: quantmesh/market-maker:latest
    container_name: quantmesh
    restart: unless-stopped
    ports:
      - "28888:28888"
    volumes:
      - ./config.yaml:/app/config.yaml:ro
      - ./data:/app/data
      - ./logs:/app/logs
      - ./backups:/app/backups
    environment:
      - TZ=Asia/Shanghai
    healthcheck:
      test: ["CMD", "wget", "--spider", "http://localhost:28888/api/status"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 40s
```

## 环境配置

### GitHub Secrets

需要在 GitHub 仓库设置以下 Secrets：

#### Docker Hub
- `DOCKER_USERNAME` - Docker Hub 用户名
- `DOCKER_PASSWORD` - Docker Hub 密码/Token

#### Staging 环境
- `STAGING_HOST` - Staging 服务器地址
- `STAGING_USER` - SSH 用户名
- `STAGING_SSH_KEY` - SSH 私钥

#### Production 环境
- `PROD_HOST` - 生产服务器地址
- `PROD_USER` - SSH 用户名
- `PROD_SSH_KEY` - SSH 私钥

### 设置 Secrets

1. 进入 GitHub 仓库
2. Settings -> Secrets and variables -> Actions
3. 点击 "New repository secret"
4. 添加上述 Secrets

## 版本管理

### 版本号规范（Semantic Versioning）

格式：`v{major}.{minor}.{patch}[-{prerelease}]`

- `major`: 主版本号（不兼容的 API 变更）
- `minor`: 次版本号（向后兼容的功能新增）
- `patch`: 修订号（向后兼容的问题修正）
- `prerelease`: 预发布标识（alpha, beta, rc）

示例：
- `v1.0.0` - 正式版本
- `v1.1.0-beta.1` - Beta 版本
- `v1.0.1` - 补丁版本

### 分支策略

```
main (生产环境)
  ├── develop (开发环境)
  │   ├── feature/new-feature (功能分支)
  │   ├── bugfix/fix-issue (修复分支)
  │   └── hotfix/critical-fix (热修复分支)
  └── release/v1.0.0 (发布分支)
```

#### 分支说明

- `main`: 生产环境代码，只接受来自 `release` 或 `hotfix` 的合并
- `develop`: 开发环境代码，功能开发的主分支
- `feature/*`: 新功能开发分支，从 `develop` 创建，合并回 `develop`
- `bugfix/*`: Bug 修复分支，从 `develop` 创建，合并回 `develop`
- `hotfix/*`: 紧急修复分支，从 `main` 创建，合并回 `main` 和 `develop`
- `release/*`: 发布准备分支，从 `develop` 创建，合并回 `main` 和 `develop`

### 工作流程

#### 开发新功能

```bash
# 1. 从 develop 创建功能分支
git checkout develop
git pull origin develop
git checkout -b feature/new-feature

# 2. 开发并提交
git add .
git commit -m "feat: add new feature"

# 3. 推送并创建 PR
git push origin feature/new-feature
# 在 GitHub 上创建 PR: feature/new-feature -> develop

# 4. CI 通过后合并
# 合并后删除功能分支
git branch -d feature/new-feature
```

#### 发布新版本

```bash
# 1. 从 develop 创建发布分支
git checkout develop
git pull origin develop
git checkout -b release/v1.0.0

# 2. 更新版本号、CHANGELOG 等
# 提交变更
git commit -m "chore: prepare release v1.0.0"

# 3. 合并到 main
git checkout main
git merge --no-ff release/v1.0.0

# 4. 创建 tag
git tag -a v1.0.0 -m "Release v1.0.0"

# 5. 合并回 develop
git checkout develop
git merge --no-ff release/v1.0.0

# 6. 推送
git push origin main develop --tags

# 7. 删除发布分支
git branch -d release/v1.0.0
```

#### 紧急修复

```bash
# 1. 从 main 创建热修复分支
git checkout main
git pull origin main
git checkout -b hotfix/critical-fix

# 2. 修复并提交
git commit -m "fix: critical security issue"

# 3. 合并到 main
git checkout main
git merge --no-ff hotfix/critical-fix

# 4. 创建 tag
git tag -a v1.0.1 -m "Hotfix v1.0.1"

# 5. 合并回 develop
git checkout develop
git merge --no-ff hotfix/critical-fix

# 6. 推送
git push origin main develop --tags

# 7. 删除热修复分支
git branch -d hotfix/critical-fix
```

## 回滚策略

### 自动回滚

CD 流程包含自动回滚机制：
- 健康检查失败时自动回滚
- 部署失败时自动恢复备份

### 手动回滚

#### 方法 1：重新部署旧版本

```bash
# 1. 找到要回滚的版本
git tag -l

# 2. 重新部署
# 在 GitHub Actions 中手动触发 CD，选择旧版本的 tag
```

#### 方法 2：服务器端回滚

```bash
# SSH 到服务器
ssh user@server

# 停止服务
sudo systemctl stop quantmesh

# 恢复备份
cd /opt/quantmesh
sudo ./scripts/restore.sh $(ls -t backups/*.tar.gz | head -n 1)

# 启动服务
sudo systemctl start quantmesh
```

## 监控和告警

### 部署监控

- 部署成功/失败通知
- 健康检查状态
- 服务可用性监控

### 集成通知

可以在 GitHub Actions 中添加通知步骤：

```yaml
- name: Notify Telegram
  if: always()
  uses: appleboy/telegram-action@master
  with:
    to: ${{ secrets.TELEGRAM_CHAT_ID }}
    token: ${{ secrets.TELEGRAM_BOT_TOKEN }}
    message: |
      Deployment to ${{ github.event.inputs.environment }}:
      Status: ${{ job.status }}
      Version: ${{ github.ref_name }}
```

## 最佳实践

### 1. 代码质量

- 保持测试覆盖率 > 80%
- 所有 PR 必须通过 CI 检查
- Code Review 必须由至少一人审核

### 2. 部署安全

- 使用 SSH 密钥而非密码
- 限制部署权限
- 启用 GitHub 环境保护规则

### 3. 版本管理

- 遵循语义化版本规范
- 维护详细的 CHANGELOG
- 为每个 Release 编写 Release Notes

### 4. 回滚准备

- 每次部署前自动备份
- 定期测试回滚流程
- 记录回滚步骤

### 5. 监控和日志

- 部署后进行冒烟测试
- 监控关键指标
- 保留部署日志

## 故障排查

### CI 失败

1. **测试失败**
   ```bash
   # 本地运行测试
   go test -v ./...
   
   # 查看详细输出
   go test -v -race ./...
   ```

2. **构建失败**
   ```bash
   # 检查依赖
   go mod tidy
   go mod verify
   
   # 本地构建
   go build .
   ```

3. **Lint 失败**
   ```bash
   # 运行 lint
   golangci-lint run
   
   # 自动修复部分问题
   golangci-lint run --fix
   ```

### CD 失败

1. **部署失败**
   - 检查服务器连接
   - 查看部署日志
   - 验证 SSH 密钥

2. **健康检查失败**
   - 检查服务是否启动
   - 查看应用日志
   - 验证配置文件

3. **Docker 构建失败**
   - 检查 Dockerfile
   - 验证基础镜像
   - 查看构建日志

## 参考资源

- [GitHub Actions 文档](https://docs.github.com/en/actions)
- [Docker 文档](https://docs.docker.com/)
- [Semantic Versioning](https://semver.org/)
- [Git Flow](https://nvie.com/posts/a-successful-git-branching-model/)

