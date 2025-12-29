# 多阶段构建 Dockerfile

# 构建阶段
FROM golang:1.21-alpine AS builder

# 安装构建依赖
RUN apk add --no-cache git make gcc musl-dev

# 设置工作目录
WORKDIR /build

# 复制 go mod 文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 构建应用
RUN VERSION=$$(git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || echo "unknown") && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X main.Version=$$VERSION" \
    -o quantmesh .

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

