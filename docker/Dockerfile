# syntax=docker/dockerfile:1
# ConfigFlow Dockerfile

# ==============================================================================
# Stage 1: 构建 Go Agent 二进制文件
# ==============================================================================
FROM golang:1.21-alpine AS go-builder

WORKDIR /build
ENV GOPROXY=https://goproxy.cn,direct

# 先复制依赖文件，利用 Docker 层缓存（go.mod/go.sum 不变则跳过 go mod download）
COPY backend/agents/go-agent/go.mod backend/agents/go-agent/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# 再复制源码并编译（仅 Go 源码变更时才重新编译）
COPY backend/agents/go-agent/ ./
RUN --mount=type=cache,target=/go/pkg/mod \
    mkdir -p /dist && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /dist/configflow-agent-linux-amd64 . && \
    CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o /dist/configflow-agent-linux-arm64 . && \
    CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -ldflags="-s -w" -o /dist/configflow-agent-linux-armv7 .

# ==============================================================================
# Stage 2: 构建前端
# ==============================================================================
# vite-plugin-pwa 依赖的 workbox-build 要求 node >= 20；
# Node 18 已于 2025-04 结束维护，直接升到 20 LTS。
FROM node:20-alpine AS frontend-builder

WORKDIR /app/frontend

# 先复制 package 文件，利用 Docker 层缓存（依赖不变则跳过 npm ci）
COPY frontend/package*.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci

# 再复制源码并构建（仅前端源码变更时才重新 build）
COPY frontend/ ./
RUN npm run build

# ==============================================================================
# Stage 3: 最终镜像（源码部署）
# ==============================================================================
FROM python:3.11-slim

# 版本号参数，从 GitHub Actions 传入
ARG VERSION=dev

WORKDIR /app

# 安装系统依赖、配置权限
RUN apt-get update && \
    apt-get install -y --no-install-recommends nginx supervisor ca-certificates && \
    rm -rf /var/lib/apt/lists/* && \
    (groupadd -r nginx || true) && (useradd -r -g nginx -s /sbin/nologin -d /nonexistent nginx || true) && \
    mkdir -p /data /var/log/supervisor /var/cache/nginx /var/log/nginx /etc/supervisor/conf.d && \
    chown -R nginx:nginx /var/cache/nginx /var/log/nginx /usr/share/nginx/html && \
    rm -rf /etc/nginx/sites-enabled/default /etc/nginx/sites-available/default /etc/nginx/conf.d/default.conf /usr/share/nginx/html/*

# 安装 Python 依赖
COPY backend/requirements.txt /tmp/requirements.txt
RUN --mount=type=cache,target=/root/.cache/pip \
    pip install --upgrade pip && pip install -r /tmp/requirements.txt && \
    rm /tmp/requirements.txt

# 复制后端源码并替换版本号
COPY backend/ /app/backend/
RUN sed -i "s/__version__ = \"dev\"/__version__ = \"${VERSION}\"/" /app/backend/version.py

# 复制 Go Agent 二进制
COPY --from=go-builder /dist/ /opt/configflow/static/agents/

# 复制前端构建产物（在 nginx 安装后，覆盖默认页面）
COPY --from=frontend-builder /app/frontend/dist /usr/share/nginx/html

# 复制配置文件（在安装软件包之后）
COPY docker/nginx.conf /etc/nginx/nginx.conf
COPY docker/supervisord.conf /etc/supervisor/conf.d/supervisord.conf

ENV DATA_DIR=/data TZ=Asia/Shanghai

EXPOSE 80

CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]
