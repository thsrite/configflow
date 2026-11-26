#!/bin/sh
# Healthcheck against ConfigFlow's active web listener port.
set -eu

DATA_DIR="${DATA_DIR:-/data}"
PORT=""

if [ -n "${CONFIGFLOW_WEB_PORT:-}" ]; then
    PORT="$(printf '%s' "$CONFIGFLOW_WEB_PORT" | tr -d ' \t\r\n')"
elif [ -f "$DATA_DIR/web-listener.json" ]; then
    PORT="$(grep -oE '"port"[[:space:]]*:[[:space:]]*[0-9]+' "$DATA_DIR/web-listener.json" 2>/dev/null | head -n 1 | grep -oE '[0-9]+' || true)"
fi

if [ -z "$PORT" ]; then
    PORT="80"
fi

BODY="$(wget -q -T 3 -O - "http://127.0.0.1:${PORT}/health" 2>/dev/null || true)"
[ "$BODY" = "healthy" ]
