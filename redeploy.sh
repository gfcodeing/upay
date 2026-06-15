#!/bin/bash
# ======================================================================
# UPAY_PRO 一键重新部署脚本
# 用法：在服务器上执行   bash /www/upay/redeploy.sh
# 作用：拉最新代码 -> 构建镜像 -> 用正确配置重启容器 -> 打印状态与日志
# ======================================================================
set -e

# ======================== 配置区 ========================
DEPLOY_DIR="/www/upay"        # 项目目录
IMAGE_NAME="upay_pro"         # 镜像名
CONTAINER_NAME="upay_pro"     # 容器名
PORT_CHECK="8090"             # 程序监听端口（用于健康检查）
DATA_DIR="$DEPLOY_DIR/data"   # 数据库持久化目录（挂载到容器 /app/DBS）
LOG_DIR="$DEPLOY_DIR/log"     # 日志持久化目录（挂载到容器 /app/log）
# ========================================================

cd "$DEPLOY_DIR"

echo ">>> [1/5] 拉取最新代码..."
git pull origin main

echo ">>> [2/5] 构建镜像 ${IMAGE_NAME}:latest ..."
docker build -t "${IMAGE_NAME}:latest" .

echo ">>> [3/5] 停止并删除旧容器..."
docker rm -f "$CONTAINER_NAME" 2>/dev/null || true

echo ">>> [4/5] 启动新容器（host 网络，连宿主机 Redis）..."
mkdir -p "$DATA_DIR" "$LOG_DIR"
# 用 --network host 让容器能访问宿主机 Redis(127.0.0.1:6379)，因此不需要 -p 端口映射；
# 程序直接监听宿主机的 8090。
docker run -d \
  --name "$CONTAINER_NAME" \
  --restart always \
  --network host \
  -v "$DATA_DIR:/app/DBS" \
  -v "$LOG_DIR:/app/log" \
  "${IMAGE_NAME}:latest"

echo ">>> [5/5] 部署完成，等待启动..."
sleep 3
echo "=================== 容器状态 ==================="
docker ps --filter "name=$CONTAINER_NAME"
echo "=================== 健康检查 ==================="
if curl -sf -o /dev/null -w "HTTP %{http_code}\n" "http://127.0.0.1:${PORT_CHECK}/login"; then
  echo "✅ 后端响应正常（127.0.0.1:${PORT_CHECK}）"
else
  echo "⚠️  后端未响应，请查看下方日志排查"
fi
echo "=================== 最近日志 ==================="
docker logs --tail 20 "$CONTAINER_NAME" 2>&1 | grep -vE "任务开启|没有未支付" || true
echo "================================================"
echo "完成。实时日志请用： docker logs -f $CONTAINER_NAME"
