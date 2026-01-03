# 多阶段构建 Dockerfile

# 构建阶段
FROM golang:1.21-alpine AS builder

# 安装构建依赖（包括 Node.js 和 npm 用于构建前端）
RUN apk add --no-cache git make gcc musl-dev nodejs npm

# 设置工作目录
WORKDIR /build

# 复制 go mod 文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 构建前端（如果需要）
RUN if [ -d "webui" ] && [ -f "webui/package.json" ]; then \
      cd webui && \
      npm ci --legacy-peer-deps || npm install --legacy-peer-deps && \
      npm run build && \
      cd .. && \
      mkdir -p web/dist && \
      if [ -d "webui/dist" ] && [ "$$(ls -A webui/dist 2>/dev/null)" ]; then \
        cp -r webui/dist/* web/dist/ || true; \
      fi; \
    else \
      mkdir -p web/dist && \
      echo "# Placeholder" > web/dist/.gitkeep; \
    fi

# 构建应用（排除工具文件，避免 main 函数冲突）
RUN mkdir -p .tools_backup && \
    for file in analyze_market_data.go run_1m_backtest.go run_3m_backtest.go run_eth_backtest.go test_intrabar_backtest.go test_intrabar_quick.go test_license_validation.go test_plugin_detailed.go test_plugin_loading.go test_zero_fee.go; do \
      if [ -f "$$file" ]; then mv "$$file" .tools_backup/ || true; fi \
    done && \
    VERSION=$$(git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || echo "unknown") && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X main.Version=$$VERSION" \
    -o quantmesh . && \
    rm -rf .tools_backup

# 运行阶段
FROM alpine:latest

# 安装运行时依赖
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    sqlite

# 创建非 root 用户
RUN addgroup -g 1000 quantmesh && \
    adduser -D -u 1000 -G quantmesh quantmesh

# 设置工作目录
WORKDIR /app

# 从构建阶段复制二进制文件
COPY --from=builder /build/quantmesh .

# 复制配置文件示例
COPY config.example.yaml ./config.example.yaml

# 创建数据目录
RUN mkdir -p /app/data /app/logs /app/backups && \
    chown -R quantmesh:quantmesh /app

# 切换到非 root 用户
USER quantmesh

# 暴露端口
EXPOSE 28888

# 健康检查
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:28888/api/status || exit 1

# 设置环境变量
ENV TZ=Asia/Shanghai

# 启动应用
ENTRYPOINT ["/app/quantmesh"]

