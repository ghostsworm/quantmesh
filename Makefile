.PHONY: build-frontend build-backend build all clean dev dev-stop
OUTPUT ?= quantmesh

# 前端失败或嵌入产物未核验时，不得继续后端构建
build-frontend:
	@test -d webui/node_modules || yarn --cwd webui install --immutable
	@yarn --cwd webui build
	@ruby scripts/frontend_embed.rb sync

# 构建后端（仅主程序，不编译 tools/ 与 plugin/examples）
# 若需编译全模块检查：go build ./...（会跳过带 //go:build tools 的包）
# 单独编译某工具：go build -tags tools -o set_password ./tools/set_password.go
build-backend: build-frontend
	@echo "Building backend..."
	@ruby scripts/frontend_embed.rb verify
	@VERSION=$$(ruby scripts/frontend_embed.rb version) && \
	echo "Version: $$VERSION" && \
	go build -ldflags="-s -w -X main.Version=$$VERSION" -o "$(OUTPUT)" .

# 完整构建（前端 + 后端）
build: build-backend

all: build

# 清理构建产物
clean:
	@rm -rf quantmesh webui/dist web/dist

# 开发模式启动
dev:
	@./scripts/local/dev.sh

# 停止开发模式
dev-stop:
	@./scripts/local/stop.sh --dev

# 重启（生产模式）
restart:
	@./scripts/local/restart.sh

# 重启（开发模式）
restart-dev:
	@./scripts/local/restart.sh --dev
