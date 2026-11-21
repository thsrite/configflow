# syntax=docker/dockerfile:1
# ConfigFlow Dockerfile (Go server + frontend + bundled Sub-Store + Agents + node-probe)

# ==============================================================================
# Stage 1: 构建 Go Server 主程序（按目标平台交叉编译，不经 QEMU 模拟）
# ==============================================================================
# Go 1.27.4 与 server/go.mod 保持一致，纯 Go 构建（modernc.org/sqlite，无需 CGO）。
FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine AS server-builder

WORKDIR /build
ENV GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 GOTOOLCHAIN=local

COPY server/go.mod server/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY server/ ./
ARG VERSION=dev
ARG GIT_COMMIT=
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
# 每次发布注入唯一版本（包含提交号，供 /api/version 与 OxiDNS RulesFingerprint 使用）；
# 镜像内的 Go server 与下方 Agent 二进制均来自同一次构建上下文（同一提交）。
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /dist && \
    FULL_VERSION="${VERSION}" && \
    if [ -n "${GIT_COMMIT}" ]; then \
      case "${FULL_VERSION}" in \
        *"${GIT_COMMIT}"*) ;; \
        *) FULL_VERSION="${FULL_VERSION}+${GIT_COMMIT}" ;; \
      esac; \
    fi && \
    GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM=${TARGETVARIANT#v} \
    go build -trimpath -ldflags="-s -w -X configflow/internal/version.Version=${FULL_VERSION}" \
    -o /dist/configflow ./cmd/configflow

# ==============================================================================
# Stage 2: 构建 Go Agent 二进制文件（全平台供面板下发，构建机原生交叉编译）
# ==============================================================================
FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine AS go-builder

WORKDIR /build
ENV GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 GOOS=linux GOTOOLCHAIN=local

COPY backend/agents/go-agent/go.mod backend/agents/go-agent/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY backend/agents/go-agent/ ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /dist && \
    GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /dist/configflow-agent-linux-amd64 . && \
    GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o /dist/configflow-agent-linux-arm64 . && \
    GOARCH=arm GOARM=7 go build -trimpath -ldflags="-s -w" -o /dist/configflow-agent-linux-armv7 .

# ==============================================================================
# Stage 3: 独立节点测速组件（按目标架构交叉编译，不启用 TUN 或监听服务）
# ==============================================================================
FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine AS node-probe-builder

WORKDIR /build
ENV GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 GOTOOLCHAIN=local

COPY backend/node-probe/go.mod backend/node-probe/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY backend/node-probe/ ./
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /dist && \
    GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM=${TARGETVARIANT#v} \
    go build -trimpath -ldflags="-s -w" -o /dist/configflow-node-probe .

# ==============================================================================
# Stage 4: 构建前端（产物为平台无关静态文件，在构建机原生平台构建一次）
# ==============================================================================
# vite-plugin-pwa 依赖的 workbox-build 要求 node >= 20；与 CI 一致使用 Node 24 LTS。
FROM --platform=$BUILDPLATFORM node:24-alpine AS frontend-builder

WORKDIR /app/frontend

COPY frontend/package*.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci --no-audit --no-fund

COPY frontend/ ./
RUN npm run build

# ==============================================================================
# Stage 5: 内置 Sub-Store bundle（可在线更新到 /data/sub-store/runtime）
# ==============================================================================
FROM --platform=$BUILDPLATFORM alpine:3.24 AS sub-store
ARG SUB_STORE_VERSION=2.42.3
ADD --chmod=644 https://github.com/sub-store-org/Sub-Store/releases/download/${SUB_STORE_VERSION}/sub-store.bundle.js /rootfs/opt/sub-store/sub-store.bundle.js
RUN mkdir -p /rootfs/opt/sub-store && echo "${SUB_STORE_VERSION}" > /rootfs/opt/sub-store/VERSION

# 汇总内置程序与许可证为一层：主服务或前端更新时复用，任一内置组件更新时替换。
FROM scratch AS runtime-assets
COPY --from=sub-store /rootfs/ /rootfs/
COPY --from=go-builder /dist/ /rootfs/opt/configflow/static/agents/
COPY --from=node-probe-builder /dist/ /rootfs/opt/configflow/node-probe/
COPY backend/node-probe/LICENSE backend/node-probe/NOTICE /rootfs/opt/configflow/node-probe/

# ==============================================================================
# Stage 6: 组装应用文件（Go server、前端静态资源、配置与脚本），在最终镜像中占一层
# ==============================================================================
FROM --platform=$BUILDPLATFORM alpine:3.24 AS app

RUN apk add --no-cache python3

COPY --chmod=755 --from=server-builder /dist/configflow /rootfs/usr/local/bin/configflow
COPY --from=frontend-builder /app/frontend/dist /rootfs/usr/share/configflow/html
COPY docker/supervisord.conf /rootfs/etc/supervisor/conf.d/supervisord.conf
COPY --chmod=755 docker/sub-store-start.sh docker/configflow-start.sh docker/configflow-healthcheck.sh /rootfs/usr/local/bin/
COPY --chmod=755 docker/auto_update.py /rootfs/usr/local/bin/configflow-auto-update.py

# 预压缩前端资源供 Go Edge gzip_static 直接发送；记录镜像自带各组件的源码指纹，
# 使 autoupdate 只重建有变动的组件，且缺工具链时不混用不同版本。
RUN --mount=type=bind,source=server,target=/tmp/image-src/server \
    --mount=type=bind,source=frontend,target=/tmp/image-src/frontend \
    --mount=type=bind,source=backend/agents/go-agent,target=/tmp/image-src/go-agent \
    --mount=type=bind,source=backend/node-probe,target=/tmp/image-src/node-probe \
    mkdir -p /app /rootfs/app && \
    python3 /rootfs/usr/local/bin/configflow-auto-update.py --precompress /rootfs/usr/share/configflow/html && \
    python3 /rootfs/usr/local/bin/configflow-auto-update.py --record-image /tmp/image-src && \
    mv /app/.image-trees.json /rootfs/app/.image-trees.json

# ==============================================================================
# Stage 7: 最终运行镜像（target: runtime）
# 无 Flask/gunicorn/nginx 与 backend/ Python 运行依赖；保留 supervisord 仅用于
# 管理 Go server 与按需启停的内置 Sub-Store（Node 24 LTS，支持 node:sqlite）。
# alpine:3.24 在 amd64/arm64 均提供 nodejs 24.22.3+（含 2026-10 安全修复）与 supervisor 4.3.0。
# ==============================================================================
FROM alpine:3.24 AS runtime

RUN apk add --no-cache ca-certificates tzdata supervisor nodejs && \
    mkdir -p /app /data /var/log/supervisor /var/run /etc/supervisor/conf.d

COPY --link --from=runtime-assets /rootfs/ /
COPY --link --from=app /rootfs/ /

WORKDIR /app

ENV DATA_DIR=/data \
    TZ=Asia/Shanghai \
    CONFIGFLOW_FRONTEND_DIR=/usr/share/configflow/html \
    SUB_STORE_BACKEND_API_HOST=127.0.0.1 \
    SUB_STORE_BACKEND_API_PORT=3001 \
    SUB_STORE_DATA_BASE_PATH=/data/sub-store

EXPOSE 80

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD ["/usr/local/bin/configflow-healthcheck.sh"]

CMD ["/usr/local/bin/configflow-start.sh"]

# ==============================================================================
# Stage 8: 自动更新镜像（target: autoupdate）
# 在 runtime 基础上加入 Go、npm 与 git：容器启动时拉取分支最新源码，按源码指纹
# 仅重建有变动的组件（Go server、前端、Agent、node-probe）并安装；任何失败均回退。
# ==============================================================================
FROM golang:1.27.4-alpine AS go-toolchain

FROM runtime AS autoupdate
COPY --from=go-toolchain /usr/local/go /usr/local/go
RUN apk add --no-cache npm git && \
    npm --version && /usr/local/go/bin/go version && git --version
ENV CONFIGFLOW_AUTO_UPDATE=1
