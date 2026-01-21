#!/bin/bash

# QuantMesh 部署配置文件
# 服务器: facev.app

# 远程服务器配置
REMOTE_HOST="facev.app"                    # 服务器地址
REMOTE_USER="root"                         # SSH用户名
REMOTE_PORT="22"                           # SSH端口
REMOTE_PATH="/opt/quantmesh"               # 远程部署路径

# SSH配置
SSH_KEY=""                                 # SSH私钥路径（留空使用默认）

# 服务配置
SERVICE_NAME="quantmesh"                   # systemd服务名称
HEALTH_CHECK_URL="http://localhost:28888/api/status"  # 健康检查URL

# 构建配置
USE_DOCKER_BUILD=false                     # 是否使用Docker构建（Mac环境推荐true）
