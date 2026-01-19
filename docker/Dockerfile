# ==============================================================================
# Stage 1: Builder - 构建 Go Agent 二进制文件（仅 amd64，提升速度）
# ==============================================================================
FROM golang:1.21-alpine AS go-builder

WORKDIR /build

# 设置 Go 模块代理
ENV GOPROXY=https://goproxy.cn,direct

# 使用 bind mount 避免 Go 源码保留在镜像层中
# CGO_ENABLED=0: 创建静态链接的二进制文件，不依赖系统的 C 库，可移植性极高
# -ldflags="-s -w": 移除调试信息，减小二进制文件体积
RUN --mount=type=bind,source=backend/agents/go-agent,target=/src \
    --mount=type=cache,target=/go/pkg/mod \
    echo "=== 复制并构建 ConfigFlow Agent ===" && \
    cp /src/go.mod /src/go.sum ./ && \
    go mod download && \
    cp -r /src/* ./ && \
    mkdir -p /dist && \
    echo "Building Go Agent binaries..." && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /dist/configflow-agent-linux-amd64 . && \
    CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o /dist/configflow-agent-linux-arm64 . && \
    CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -ldflags="-s -w" -o /dist/configflow-agent-linux-armv7 . && \
    echo "=== 清理源码 ===" && \
    rm -rf /build/* && \
    echo "✓ 只保留二进制文件" && \
    ls -lh /dist/
# ==============================================================================
# Stage 2: Final Image
# ==============================================================================
FROM python:3.11-slim

WORKDIR /app

# 复制后端依赖文件和版本文件
COPY backend/requirements.txt ./

# 创建静态文件目录并复制 Go Agent 二进制文件
RUN mkdir -p /opt/configflow/static/agents
COPY --from=go-builder /dist/ /opt/configflow/static/agents/

# 安装系统依赖和 Python 包
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    --mount=type=cache,target=/root/.cache/pip \
    apt-get update && \
    apt-get install -y --no-install-recommends nginx supervisor && \
    rm -rf /var/lib/apt/lists/* && \
    pip install --no-cache-dir -r requirements.txt && \
    # 创建 nginx 用户和组
    groupadd -r nginx && \
    useradd -r -g nginx -s /sbin/nologin -d /nonexistent nginx && \
    mkdir -p /data /var/log/supervisor /var/cache/nginx /var/log/nginx && \
    chown -R nginx:nginx /var/cache/nginx /var/log/nginx

# 复制 Nginx 配置
COPY docker/nginx.conf /etc/nginx/nginx.conf

# 创建 Supervisor 配置
RUN echo '[supervisord]\n\
nodaemon=true\n\
logfile=/var/log/supervisor/supervisord.log\n\
pidfile=/var/run/supervisord.pid\n\
\n\
[program:nginx]\n\
command=/usr/sbin/nginx -g "daemon off;"\n\
autostart=true\n\
autorestart=true\n\
stdout_logfile=/dev/stdout\n\
stdout_logfile_maxbytes=0\n\
stderr_logfile=/dev/stderr\n\
stderr_logfile_maxbytes=0\n\
\n\
[program:flask]\n\
command=python app.py\n\
directory=/app\n\
autostart=true\n\
autorestart=true\n\
stdout_logfile=/dev/stdout\n\
stdout_logfile_maxbytes=0\n\
stderr_logfile=/dev/stderr\n\
stderr_logfile_maxbytes=0\n\
environment=FLASK_APP=app.py,PYTHONUNBUFFERED=1,DATA_DIR=/data\n\
' > /etc/supervisor/conf.d/supervisord.conf

# 设置环境变量
ENV FLASK_APP=app.py \
    PYTHONUNBUFFERED=1 \
    DATA_DIR=/data \
    TZ=Asia/Shanghai

# 暴露端口（Nginx 监听 80）
EXPOSE 80

# 使用 Supervisor 启动 Nginx 和 Flask
CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]
