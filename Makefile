.PHONY: build-frontend build-backend build all clean dev dev-stop

# 构建前端（优先使用 pnpm）
build-frontend:
	@echo "Building frontend..."
	@if [ -d "webui" ]; then \
		cd webui && \
		if command -v pnpm >/dev/null 2>&1; then \
			pnpm install && pnpm run build; \
		else \
			npm install && npm run build; \
		fi; \
		if [ -d "dist" ]; then \
			rm -rf ../web/dist && mkdir -p ../web/dist && cp -r dist/* ../web/dist/; \
		fi \
	else \
		echo "Frontend directory not found, skipping..."; \
	fi

# 构建后端
build-backend:
	@echo "Building backend..."
	@VERSION=$$(git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || echo "3.3.2"); \
	echo "Version: $$VERSION"; \
	go build -ldflags="-s -w -X main.Version=$$VERSION" -o quantmesh .

# 完整构建（前端 + 后端）
build: build-frontend build-backend

all: build

# 清理构建产物
clean:
	@rm -rf quantmesh webui/dist web/dist

# 开发模式启动
dev:
	@./dev.sh

# 停止开发模式
dev-stop:
	@./stop.sh --dev

# 重启（生产模式）
restart:
	@./restart.sh

# 重启（开发模式）
restart-dev:
	@./restart.sh --dev

