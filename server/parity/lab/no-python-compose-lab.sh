#!/usr/bin/env bash
# Docker HA Compose bundle lab for Agent 1.9.2-go (no-python-product).
#
# Runs on a test server (never on the Mac, never against production): a temp
# Go panel on loopback issues real bundles, prepare.sh and `docker compose up`
# run for real, and the agent service's first step enrolls with that panel.
# The lab images are alpine + the Agent binary + a stand-in /entrypoint.sh
# (prints its arguments, then sleeps), so the bootstrap layer is exercised
# without starting OxiDNS. Synthetic names and documentation addresses only.
#
#   A  new bundle, image without python3: prepare twice, first enrollment,
#      offline restart (panel stopped), prepare again with running containers.
#   B  bundle from the previous panel (bootstrap.py) on an old image, then the
#      same tag rebuilt with the new Agent (python3 kept, like the real image):
#      old compose keeps working; switching its entrypoint line to
#      `configflow-agent ha-bootstrap` also accepts the existing installation,
#      also on an image without python3.
#
# Usage, from the repository root on the test server:
#   server/parity/lab/no-python-compose-lab.sh OLD_TREE
# OLD_TREE holds `git archive <commit before 1.9.2-go> server backend/agents/go-agent`.
set -euo pipefail

root=$(cd "$(dirname "$0")/../../.." && pwd)
old=$(cd "$1" && pwd)
lab=${LAB_DIR:-$HOME/.cache/cf-nopy-lab}
port=${LAB_PORT:-25811}
docker=(sudo docker)
panel_pid=

say() { printf '\n== %s\n' "$*"; }
fail() { printf 'LAB FAIL: %s\n' "$*" >&2; exit 1; }

stop_panel() {
  if [ -n "$panel_pid" ]; then kill "$panel_pid" 2>/dev/null || true; wait "$panel_pid" 2>/dev/null || true; panel_pid=; fi
}

down_all() {
  local file
  for file in "$lab"/*/bundle/compose.yaml; do
    if [ -f "$file" ]; then (cd "$(dirname "$file")" && "${docker[@]}" compose down --remove-orphans >/dev/null 2>&1) || true; fi
  done
}

cleanup() {
  stop_panel
  down_all
}
trap cleanup EXIT

reset_lab() {
  down_all
  sudo rm -rf "$lab"
  mkdir -p "$lab/bin" "$lab/img"
}

build() {
  say "build panel and Agent binaries (new tree and previous tree)"
  (cd "$root/server" && CGO_ENABLED=0 go build -o "$lab/bin/configflow" ./cmd/configflow)
  (cd "$root/backend/agents/go-agent" && CGO_ENABLED=0 go build -o "$lab/bin/agent-new" .)
  (cd "$old/server" && CGO_ENABLED=0 go build -o "$lab/bin/configflow-old" ./cmd/configflow)
  (cd "$old/backend/agents/go-agent" && CGO_ENABLED=0 go build -o "$lab/bin/agent-old" .)
  "$lab/bin/agent-new" -version
  "$lab/bin/agent-old" -version
}

# image TAG AGENT PYTHON(yes|no)
image() {
  local dir=$lab/img/$1
  mkdir -p "$dir"
  cp "$lab/bin/$2" "$dir/configflow-agent"
  printf '#!/bin/sh\necho "ENTRYPOINT $*"\nexec sleep 3600\n' > "$dir/entrypoint.sh"
  {
    echo 'FROM alpine:3'
    [ "$3" = yes ] && echo 'RUN apk add --no-cache python3'
    echo 'COPY --chmod=755 configflow-agent /usr/local/bin/configflow-agent'
    echo 'COPY --chmod=755 entrypoint.sh /entrypoint.sh'
    echo 'ENTRYPOINT ["/entrypoint.sh"]'
  } > "$dir/Dockerfile"
  "${docker[@]}" build -q -t "cf-nopy-agent:$1" "$dir" >/dev/null
  if [ "$3" = no ] && "${docker[@]}" run --rm --entrypoint sh "cf-nopy-agent:$1" -c 'command -v python3'; then
    fail "image $1 has python3"
  fi
}

# start_panel BINARY: a fresh temp panel on loopback (no login).
start_panel() {
  stop_panel
  mkdir -p "$lab/panel-data"
  DATA_DIR=$lab/panel-data CONFIGFLOW_BACKEND_PORT=$port CONFIGFLOW_WEB_PORT=$((port + 1)) \
    setsid "$lab/bin/$1" >"$lab/panel-$1.log" 2>&1 </dev/null &
  panel_pid=$!
  local i
  for i in $(seq 100); do
    [ "$(curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:$port/api/version" || true)" = 200 ] && return 0
    sleep 0.2
  done
  cat "$lab/panel-$1.log" >&2
  fail "panel $1 did not start"
}

# bundle SCENARIO IMAGE: generate a Compose bundle and unpack it.
bundle() {
  local dir=$lab/$1
  mkdir -p "$dir/bundle"
  [ -f "$lab/host-id" ] || printf '%s\n' "lab-host-identity-0123456789abcdef" > "$lab/host-id"
  jq -n --arg url "http://127.0.0.1:$port" --arg data "$dir/data dir \$x" --arg id "$lab/host-id" --arg image "$2" --arg name "lab$1" \
    '{action: "generate", mode: "new", install_method: "compose", server_url: $url, agent_name: $name, agent_ip: "192.0.2.10",
      oxidns_port: 18082, data_dir: $data, network_mode: "host", ha_host_id_path: $id, image: $image}' > "$dir/request.json"
  curl -fsS -H 'Content-Type: application/json' --data @"$dir/request.json" "http://127.0.0.1:$port/api/agents/docker-oxidns-ha-guide" > "$dir/guide.json" \
    || { cat "$dir/guide.json" >&2; fail "guide $1"; }
  jq -r '.compose_bundle.content_base64' "$dir/guide.json" | base64 -d > "$dir/bundle.zip"
  (cd "$dir/bundle" && bsdtar -xf ../bundle.zip)
  ls -l "$dir/bundle"
}

compose() { (cd "$lab/$1/bundle" && "${docker[@]}" compose "${@:2}"); }

# wait_log SCENARIO TEXT: the agent service printed TEXT and reached the entrypoint.
wait_log() {
  local i logs
  for i in $(seq 100); do
    logs=$(compose "$1" logs --no-log-prefix agent 2>&1 || true)
    if grep -qF "$2" <<<"$logs" && grep -qF 'ENTRYPOINT /usr/bin/supervisord -c /etc/supervisor/supervisord.conf' <<<"$logs"; then
      printf '%s\n' "$logs" | tail -3
      return 0
    fi
    sleep 0.2
  done
  printf '%s\n' "$logs" >&2
  compose "$1" ps -a >&2 || true
  fail "$1: no '$2'"
}

identity_ok() {
  local agent=$lab/$1/data\ dir\ \$x/agent
  sudo test -f "$agent/config-oxidns.json" || fail "$1: no identity"
  [ "$(sudo stat -c %a "$agent/config-oxidns.json")" = 600 ] || fail "$1: identity mode"
  sudo jq -e '(.agent_id | type == "string") and (.token | type == "string")' "$agent/config-oxidns.json" >/dev/null || fail "$1: identity fields"
  sudo jq -e '.identity_digest | test("^[0-9a-f]{64}$")' "$agent/.docker-ha-installation.json" >/dev/null || fail "$1: marker"
}

scenario_a() {
  say "A: new bundle (no bootstrap.py) on an image without python3"
  start_panel configflow
  bundle a cf-nopy-agent:nopy
  [ ! -e "$lab/a/bundle/bootstrap.py" ] || fail "A: bundle ships bootstrap.py"
  grep -A2 'entrypoint:' "$lab/a/bundle/compose.yaml"
  grep -qF -- '- configflow-agent' "$lab/a/bundle/compose.yaml" && grep -qF -- '- ha-bootstrap' "$lab/a/bundle/compose.yaml" || fail "A: entrypoint"
  (cd "$lab/a/bundle" && sudo bash prepare.sh) && (cd "$lab/a/bundle" && sudo bash prepare.sh) || fail "A: prepare"
  sudo test ! -e "$lab/a/data dir \$x/.ha-compose/bootstrap.py" || fail "A: bootstrap.py prepared"
  sudo ls -la "$lab/a/data dir \$x/.ha-compose"
  compose a up -d
  wait_log a '本机 Agent 身份已持久保存；继续启动并等待真实心跳。'
  identity_ok a
  say "A: offline restart (panel stopped, credential no longer needed)"
  stop_panel
  compose a up -d --force-recreate agent
  wait_log a '已核对持久 Agent 身份；无需安装凭据或登记网络，继续启动。'
  say "A: prepare again with the running project (owned containers accepted)"
  (cd "$lab/a/bundle" && sudo bash prepare.sh) || fail "A: re-prepare"
  compose a down
}

scenario_b() {
  say "B: previous panel bundle (bootstrap.py, python3) on the old image"
  image lab agent-old yes
  start_panel configflow-old
  bundle b cf-nopy-agent:lab
  [ -f "$lab/b/bundle/bootstrap.py" ] || fail "B: old bundle has no bootstrap.py"
  (cd "$lab/b/bundle" && sudo bash prepare.sh) || fail "B: prepare"
  compose b up -d
  wait_log b '本机 Agent 身份已持久保存；继续启动并等待真实心跳。'
  identity_ok b
  stop_panel
  say "B: upgrade the image tag to the new Agent (python3 kept); old compose.yaml unchanged"
  image lab agent-new yes
  compose b up -d --force-recreate
  wait_log b '已核对持久 Agent 身份；无需安装凭据或登记网络，继续启动。'
  "${docker[@]}" exec "$(compose b ps -q agent)" configflow-agent -version
  say "B: migration path — entrypoint line switched to configflow-agent ha-bootstrap"
  sed -i 's#- python3$#- configflow-agent#; s#- /run/configflow-compose/bootstrap.py$#- ha-bootstrap#' "$lab/b/bundle/compose.yaml"
  grep -A2 'entrypoint:' "$lab/b/bundle/compose.yaml"
  compose b up -d --force-recreate agent
  wait_log b '已核对持久 Agent 身份；无需安装凭据或登记网络，继续启动。'
  say "B: same installation on an image without python3"
  image lab agent-new no
  compose b up -d --force-recreate
  wait_log b '已核对持久 Agent 身份；无需安装凭据或登记网络，继续启动。'
  compose b down
}

reset_lab
build
image nopy agent-new no
scenario_a
scenario_b
say "LAB PASS: A (new bundle, no python3) and B (upgrade from the bootstrap.py bundle)"
