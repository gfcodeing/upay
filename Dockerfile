FROM golang:1.24.4-alpine AS builder

WORKDIR /build

# 安装依赖
RUN apk add --no-cache git gcc musl-dev

# 先拷贝 go.mod/go.sum，利用 Docker 缓存层加速后续构建
COPY go.mod go.sum ./
RUN go mod download

# 拷贝源码并编译
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o upay_pro .

# ---- 运行阶段 ----
FROM alpine:latest

WORKDIR /app

# 时区设置为上海
RUN apk add --no-cache tzdata ca-certificates && \
    cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && \
    echo "Asia/Shanghai" > /etc/timezone

COPY --from=builder /build/upay_pro .
COPY --from=builder /build/web ./web
# 前端静态资源与 HTML 模板（程序运行时 LoadHTMLGlob("static/*.html") 依赖，缺失会 panic）
COPY --from=builder /build/static ./static

# 启动脚本
COPY start.sh .
RUN chmod +x start.sh

EXPOSE 8090

CMD ["/app/start.sh"]
