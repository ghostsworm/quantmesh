# 开发模式
./scripts/local/dev.sh                  # 启动开发模式（Go + Vite）
./scripts/local/restart.sh --dev        # 重启开发模式
./scripts/local/stop.sh --dev           # 停止开发模式

# 生产模式
./build.sh                # 构建（前端 + 后端）
./scripts/local/start.sh                # 启动生产模式
./scripts/local/restart.sh              # 重启生产模式
./scripts/local/stop.sh --prod          # 停止生产模式
./scripts/local/stop.sh                 # 停止所有进程

# Makefile 命令
make dev                  # 启动开发模式
make dev-stop             # 停止开发模式
make restart-dev          # 重启开发模式
make build                # 构建
make restart              # 重启生产模式
make clean                # 清理构建产物