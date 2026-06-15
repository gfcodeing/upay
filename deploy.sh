#!/bin/bash
set -e

# ======================== 配置区 ========================
REPO="git@github.com:gfcodeing/upay.git"
DEPLOY_DIR="/www/upay"
IMAGE_NAME="upay_pro"
CONTAINER_NAME="upay_pro"
# ========================================================

echo ">>> [1/5] 拉取最新代码..."
if [ -d "$DEPLOY_DIR/.git" ]; then
  cd "$DEPLOY_DIR"
  git pull origin main
else
  git clone "$REPO" "$DEPLOY_DIR"
  cd "$DEPLOY_DIR"
fi

echo ">>> [2/5] 构建镜像..."
docker build -t "${IMAGE_NAME}:latest" .

echo ">>> [3/5] 停止并删除旧容器..."
docker stop "$CONTAINER_NAME" 2>/dev/null || true
docker rm "$CONTAINER_NAME" 2>/dev/null || true

echo ">>> [4/5] 启动新容器..."
mkdir -p "$DEPLOY_DIR/data" "$DEPLOY_DIR/log"
docker run -d \
  --name "$CONTAINER_NAME" \
  --restart always \
  -p 127.0.0.1:8090:8090 \
  -v "$DEPLOY_DIR/data:/app/DBS" \
  -v "$DEPLOY_DIR/log:/app/log" \
  -e TZ=Asia/Shanghai \
  "${IMAGE_NAME}:latest"

echo ">>> [5/5] 完成！当前运行状态:"
docker ps | grep "$CONTAINER_NAME"
