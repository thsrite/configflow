# syntax=docker/dockerfile:1
# ConfigFlow Dockerfile

# ==============================================================================
# Stage 1: 构建 Go Agent 二进制文件
# 产物与目标平台无关，固定在构建机原生平台运行：多架构构建时不再经 QEMU 模拟重复编译。
# ==============================================================================
# Go 1.26.11: GO-2026-6859 (net/http MaxBytesReader server crash); 1.26.10 distrusts
# compromised intermediate CA keys in crypto/x509 (CVE-2026-111943);
# Agent and node-probe verify TLS, so security releases need this pin and a new image.
FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine AS go-builder

WORKDIR /build
ENV GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 GOOS=linux

# 先复制依赖文件，利用 Docker 层缓存（go.mod/go.sum 不变则跳过 go mod download）
COPY backend/agents/go-agent/go.mod backend/agents/go-agent/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# 再复制源码并编译（仅 Go 源码变更时才重新编译；编译缓存跨构建复用）
COPY backend/agents/go-agent/ ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /dist && \
    GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /dist/configflow-agent-linux-amd64 . && \
    GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o /dist/configflow-agent-linux-arm64 . && \
    GOARCH=arm GOARM=7 go build -trimpath -ldflags="-s -w" -o /dist/configflow-agent-linux-armv7 .

# ==============================================================================
# 独立节点测速组件：仅编译目标架构，运行时不启用 TUN 或监听服务
# ==============================================================================
FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine AS node-probe-builder
WORKDIR /build
ENV CGO_ENABLED=0
COPY backend/node-probe/go.mod backend/node-probe/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY backend/node-probe/ ./
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM=${TARGETVARIANT#v} \
    go build -trimpath -ldflags="-s -w" -o /dist/configflow-node-probe .

# ==============================================================================
# Stage 2: 构建前端（产物为静态文件，同样只在构建机原生平台构建一次）
# ==============================================================================
# vite-plugin-pwa 依赖的 workbox-build 要求 node >= 20；Node 20 已结束维护、
# 不再获得安全修复，与 CI 一致使用 24 LTS。只在构建时使用，不进入运行镜像。
FROM --platform=$BUILDPLATFORM node:24-alpine AS frontend-builder

WORKDIR /app/frontend

# 先复制 package 文件，利用 Docker 层缓存（依赖不变则跳过 npm ci）
COPY frontend/package*.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci --no-audit --no-fund

# 再复制源码并构建（仅前端源码变更时才重新 build）
COPY frontend/ ./
RUN npm run build

# ==============================================================================
# Stage 3: 内置 Sub-Store 运行时（Node.js 二进制，Sub-Store 要求 node >= 22.5 的 sqlite 内建模块）
# ==============================================================================
# 固定到包含安全修复的版本：24.22.1 修复 Brotli 解码越界写入、TLS 证书 IP 校验与
# dns.resolve 空字节等问题（https://nodejs.org/en/blog/vulnerability/october-2026-security-releases）。
# Sub-Store 会主动拉取外部订阅，Node 安全更新需要更新这里并重新发布镜像。
FROM node:24.22.1-bookworm-slim AS node-runtime

# 准备 Node 与 Sub-Store bundle，随后与其他内置程序合并为一个运行组件层。
FROM --platform=$BUILDPLATFORM python:3.11-slim AS sub-store
# 内置 Sub-Store 后端版本（https://github.com/sub-store-org/Sub-Store/releases），可在线更新
ARG SUB_STORE_VERSION=2.42.3
COPY --from=node-runtime /usr/local/bin/node /rootfs/usr/local/bin/node
ADD --chmod=644 https://github.com/sub-store-org/Sub-Store/releases/download/${SUB_STORE_VERSION}/sub-store.bundle.js /rootfs/opt/sub-store/sub-store.bundle.js
RUN echo "${SUB_STORE_VERSION}" > /rootfs/opt/sub-store/VERSION

# 内置程序及许可证合为一层；应用代码更新时复用，任一程序更新时整体替换。
FROM scratch AS runtime-assets
COPY --from=sub-store /rootfs/ /rootfs/
COPY --from=go-builder /dist/ /rootfs/opt/configflow/static/agents/
COPY --from=node-probe-builder /dist/ /rootfs/opt/configflow/node-probe/
COPY backend/node-probe/LICENSE backend/node-probe/NOTICE /rootfs/opt/configflow/node-probe/

# ==============================================================================
# Stage 4: 组装应用文件（后端运行源码、前端、配置与脚本），在最终镜像中只占一层
# ==============================================================================
FROM --platform=$BUILDPLATFORM python:3.11-slim AS app

# 版本号参数，从 GitHub Actions 传入
ARG VERSION=dev

COPY backend/ /rootfs/app/backend/
# Keep the launcher outside auto-update's replaceable backend source directory.
# Older cached sources still start even when they do not include startup.py.
COPY backend/startup.py /rootfs/usr/local/bin/configflow-python-start.py
COPY backend/utils/web_listener.py /rootfs/usr/local/bin/configflow-web-listener.py
COPY docker/nginx.conf /rootfs/etc/nginx/configflow-nginx.conf.template
COPY docker/supervisord.conf /rootfs/etc/supervisor/conf.d/supervisord.conf
COPY --chmod=755 docker/sub-store-start.sh docker/configflow-start.sh /rootfs/usr/local/bin/
COPY --chmod=755 docker/auto_update.py /rootfs/usr/local/bin/configflow-auto-update.py
COPY --from=frontend-builder /app/frontend/dist /rootfs/usr/share/nginx/html

# 移除测试及 Go 构建源码后再编译字节码，避免将开发文件及其缓存带入运行镜像。
# 写入版本号；预压缩前端资源供 nginx gzip_static 直接发送。
# Fingerprint the sources the bundled frontend/Agent came from, so auto-update
# without a toolchain refuses commits that would mix versions. These bind mounts
# retain the complete build inputs even though runtime source copies are pruned.
RUN --mount=type=bind,source=frontend,target=/tmp/image-src/frontend \
    --mount=type=bind,source=backend/agents/go-agent,target=/tmp/image-src/go-agent \
    --mount=type=bind,source=backend/node-probe,target=/tmp/image-src/node-probe \
    find /rootfs/app/backend -type f \( -name 'test_*.py' -o -name 'conftest.py' \) -delete && \
    rm -rf /rootfs/app/backend/agents/go-agent /rootfs/app/backend/node-probe && \
    sed -i "s/__version__ = \"dev\"/__version__ = \"${VERSION}\"/" /rootfs/app/backend/version.py && \
    python3 -m compileall -q /rootfs/app/backend && \
    find /rootfs/usr/share/nginx/html -type f -size +1k \
        \( -name '*.js' -o -name '*.mjs' -o -name '*.css' -o -name '*.html' -o -name '*.svg' -o -name '*.json' -o -name '*.webmanifest' \) \
        -exec gzip -9 -k -n {} + && \
    mkdir -p /app && \
    python3 /rootfs/usr/local/bin/configflow-auto-update.py --record-image /tmp/image-src && \
    mv /app/.image-trees.json /rootfs/app/.image-trees.json

# ==============================================================================
# Stage 5: 最终镜像（源码部署）
# 运行依赖、内置程序、应用各占一层；应用更新时复用依赖与程序层。
# ==============================================================================
FROM python:3.11-slim AS runtime

# Supervisor 复用镜像内的 Python，避免 apt 再安装一套系统 Python。
# 依赖清单只挂载不复制，pip 缓存保留在构建器中，不写入镜像。
RUN --mount=type=bind,source=backend/requirements.txt,target=/tmp/requirements.txt \
    --mount=type=cache,target=/root/.cache/pip \
    apt-get update && \
    apt-get install -y --no-install-recommends nginx ca-certificates libstdc++6 && \
    rm -rf /var/lib/apt/lists/* && \
    (groupadd -r nginx || true) && (useradd -r -g nginx -s /sbin/nologin -d /nonexistent nginx || true) && \
    mkdir -p /app /data /var/log/supervisor /var/cache/nginx /var/log/nginx /etc/supervisor/conf.d && \
    chown -R nginx:nginx /var/cache/nginx && \
    rm -rf /etc/nginx/sites-enabled/default /etc/nginx/sites-available/default /etc/nginx/conf.d/default.conf /usr/share/nginx/html/* && \
    pip install --disable-pip-version-check -r /tmp/requirements.txt supervisor==4.3.0 && \
    ln -s /usr/local/bin/supervisord /usr/bin/supervisord && \
    ln -s /usr/local/bin/supervisorctl /usr/bin/supervisorctl && \
    sha256sum /tmp/requirements.txt | cut -d' ' -f1 > /app/.image-requirements.sha256

COPY --link --from=runtime-assets /rootfs/ /
COPY --link --from=app /rootfs/ /

# 保持既有运行工作目录，兼容容器内命令及部署方式。
WORKDIR /app

ENV DATA_DIR=/data TZ=Asia/Shanghai \
    SUB_STORE_BACKEND_API_HOST=127.0.0.1 \
    SUB_STORE_BACKEND_API_PORT=3001 \
    SUB_STORE_DATA_BASE_PATH=/data/sub-store

EXPOSE 80

CMD ["/usr/local/bin/configflow-start.sh"]

# ==============================================================================
# Stage 6: 自动更新镜像（标签 autoupdate）
# 在 runtime 基础上加入 Go 与 npm：容器启动时拉取分支最新源码，按需重建前端与
# Agent 后运行（见 docker/auto_update.py）。默认镜像构建请指定 target: runtime。
# ==============================================================================
FROM golang:1.27.4-bookworm AS go-toolchain

FROM runtime AS autoupdate
COPY --from=go-toolchain /usr/local/go /usr/local/go
COPY --from=node-runtime /usr/local/lib/node_modules/npm /usr/local/lib/node_modules/npm
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm && \
    npm --version && /usr/local/go/bin/go version
ENV CONFIGFLOW_AUTO_UPDATE=1
