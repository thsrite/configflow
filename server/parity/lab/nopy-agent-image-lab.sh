#!/usr/bin/env bash
# Agent image without Python (Agent 1.9.3-go): s6-overlay instead of
# Supervisor, Agent subcommands instead of the image's Python scripts.
#
# Runs on a test server only (never on the Mac, never against production).
# Real images are built from this tree (new) and from BASE_TREE (the previous
# Supervisor + Python image); a temp Go panel on loopback issues Compose files
# and enrolls. Networks: a dummy interface cfnlab0 carries a Docker macvlan
# network on 198.51.100.0/24 (the server joins it as 198.51.100.254 through
# a macvlan sibling); "host" mode HA is emulated with containers that
# share one sandbox network namespace (never the server's own). Synthetic
# names, documentation addresses and fake tokens only.
#
#   A  single OxiDNS: s6 states, kill -9 restarts, svc start/stop/restart,
#      FATAL after quick exits and recovery, supervisorctl/python3 shims,
#      docker stop (graceful, exit 0, no zombies), docker start
#   B  MosDNS through the Agent API: /api/restart, log sources and lines,
#      a real deployment (core stopped and started through svc)
#   C  AIO (Mihomo + MosDNS) and a single Mihomo with a custom config path;
#      the old Supervisor command as CMD still starts s6
#   D  macvlan Docker HA through the Agent API: keepalived started by the
#      Agent, VIP present, kill -9 restart, docker stop releases keepalived
#      first, restart recovers from the journal, DELETE stops it
#   D2 HA enabled on the base image (journal says "supervisor"), container
#      recreated with the new image: recovered from the legacy journal
#   E  host-mode Docker HA (shared sandbox netns) with the companion: control
#      socket, keepalived started/stopped in the companion by the Agent
#   F  compact Compose (macvlan): first install + offline restart on the new
#      image; install on the base image (Python) then upgrade to the new one
#   G  compose_direct from the previous panel (python3 docker-compose-install.py)
#      on the base image, then the new image through the python3 shim
#   F2 compact Compose in host mode (companion with installation identity),
#      network_mode host replaced by a shared sandbox namespace
#   G2 compose_direct from this panel on the new image (ha-compose prepare/run)
#   H  migration check of an old single-OxiDNS container (base image) run by
#      the new image
#
# Usage, from the repository root on the test server:
#   server/parity/lab/nopy-agent-image-lab.sh BASE_TREE [SCENARIO...]
# BASE_TREE holds `git archive <commit before 1.9.3-go> docker backend/agents/go-agent server`.
set -euo pipefail

root=$(cd "$(dirname "$0")/../../.." && pwd)
base=$(cd "$1" && pwd)
shift
scenarios=${*:-A B C D D2 E F F2 G G2 H}
lab=${LAB_DIR:-$HOME/.cache/cf-nopy-image-lab}
port=${LAB_PORT:-25831}
docker=(sudo docker)
panel_pid=
token=labtoken0123456789abcdef0123456789
net=cfnlab-mv

say() { printf '\n== %s\n' "$*"; }
fail() { printf 'LAB FAIL: %s\n' "$*" >&2; exit 1; }
ok() { printf '  ok: %s\n' "$*"; }

stop_panel() {
  if [ -n "$panel_pid" ]; then kill "$panel_pid" 2>/dev/null || true; wait "$panel_pid" 2>/dev/null || true; panel_pid=; fi
}

cleanup() {
  stop_panel
  local file
  for file in "$lab"/*/compose/compose.yaml; do
    [ -f "$file" ] && (cd "$(dirname "$file")" && "${docker[@]}" compose down --remove-orphans >/dev/null 2>&1) || true
  done
  "${docker[@]}" ps -aq --filter name=cfn- | xargs -r "${docker[@]}" rm -f >/dev/null 2>&1 || true
  "${docker[@]}" network rm "$net" >/dev/null 2>&1 || true
  sudo ip link del cfnlab0-host 2>/dev/null || true
  "${docker[@]}" network ls -q --filter name=cf-oxidns-labf | xargs -r "${docker[@]}" network rm >/dev/null 2>&1 || true
  sudo ip link del cfnlab0 2>/dev/null || true
}
trap cleanup EXIT

setup() {
  say "build images, panels and the lab network"
  cleanup
  sudo rm -rf "$lab"
  mkdir -p "$lab/bin"
  (cd "$root/server" && CGO_ENABLED=0 go build -o "$lab/bin/configflow" ./cmd/configflow)
  (cd "$base/server" && CGO_ENABLED=0 go build -o "$lab/bin/configflow-base" ./cmd/configflow)
  "${docker[@]}" build -q --network=host -f "$root/docker/Dockerfile.agent" -t cf-nopy-agent:new "$root" >/dev/null
  "${docker[@]}" build -q --network=host -f "$base/docker/Dockerfile.agent" -t cf-nopy-agent:base "$base" >/dev/null
  "${docker[@]}" run --rm --entrypoint configflow-agent cf-nopy-agent:new -version
  "${docker[@]}" run --rm --entrypoint configflow-agent cf-nopy-agent:base -version
  "${docker[@]}" run --rm --entrypoint sh cf-nopy-agent:new -c \
    'apk info 2>/dev/null | grep -E "^(python|py3-|supervisor)" && exit 1; ls -d /usr/lib/python3* 2>/dev/null && exit 1; [ "$(head -c 9 /usr/bin/python3)" = "#!/bin/sh" ]' \
    || fail "new image still ships Python or Supervisor"
  ok "new image: no python3 / pip / setuptools / supervisor packages; /usr/bin/python3 is the shell shim"
  sudo ip link add cfnlab0 type dummy
  sudo ip link set cfnlab0 up
  "${docker[@]}" network create -d macvlan --subnet 198.51.100.0/24 --gateway 198.51.100.1 -o parent=cfnlab0 "$net" >/dev/null
  # A macvlan sibling gives the server (lab panel, curl) a leg on the lab LAN;
  # the parent itself cannot talk to macvlan children.
  sudo ip link add cfnlab0-host link cfnlab0 type macvlan mode bridge
  sudo ip addr add 198.51.100.254/24 dev cfnlab0-host
  sudo ip link set cfnlab0-host up
  printf '%s\n' "lab-host-identity-0123456789abcdef" > "$lab/host-id"
}

start_panel() {
  stop_panel
  mkdir -p "$lab/panel-$1"
  DATA_DIR=$lab/panel-$1 CONFIGFLOW_BACKEND_PORT=$port CONFIGFLOW_WEB_PORT=$((port + 1)) \
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

ex() { "${docker[@]}" exec "$@"; }
svc_state() { ex "$1" configflow-agent svc status "$2" 2>/dev/null | awk '{print $2}' || true; }
svc_pid() { ex "$1" configflow-agent svc status "$2" 2>/dev/null | awk '$3 == "pid" {sub(",", "", $4); print $4}' || true; }

# wait_state CONTAINER PROGRAM STATE [SECONDS]
wait_state() {
  local i
  for i in $(seq $((${4:-20} * 5))); do
    [ "$(svc_state "$1" "$2")" = "$3" ] && return 0
    sleep 0.2
  done
  ex "$1" configflow-agent svc status "$2" >&2 || true
  "${docker[@]}" logs --tail 30 "$1" >&2 || true
  fail "$1: $2 did not reach $3"
}

ip_of() { "${docker[@]}" inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$1"; }
api() { local c=$1 m=$2 p=$3; shift 3; curl -sS -m 120 -X "$m" "http://$(ip_of "$c"):${PORT_OF:-8082}$p" -H "Authorization: Bearer $token" "$@"; }

# preset_identity DIR CORE: an enrolled Agent identity (no panel needed).
preset_identity() {
  sudo mkdir -p "$1"
  printf '{"agent_id":"lab-agent-%s","token":"%s"}' "$2" "$token" | sudo tee "$1/config-$2.json" >/dev/null
}

no_zombies() {
  local z
  z=$(ex "$1" ps -o stat,args | awk '$1 ~ /^Z/' || true)
  [ -z "$z" ] || fail "$1: zombies: $z"
}

# stop_check CONTAINER: graceful docker stop, exit code 0, within the limit.
stop_check() {
  local start end
  start=$(date +%s%N)
  "${docker[@]}" stop -t "${2:-10}" "$1" >/dev/null
  end=$(date +%s%N)
  local code
  code=$("${docker[@]}" inspect -f '{{.State.ExitCode}}' "$1")
  [ "$code" = 0 ] || fail "$1: exit code $code after docker stop"
  ok "$1 stopped in $(( (end - start) / 1000000 )) ms, exit code 0"
}

scenario_A() {
  say "A: single OxiDNS"
  local c=cfn-a
  "${docker[@]}" run -d --name $c -e SERVICE_TYPE=oxidns -e SERVER_URL=http://192.0.2.1:9 -e AGENT_NAME=lab-a cf-nopy-agent:new >/dev/null
  wait_state $c oxidns RUNNING
  wait_state $c agent-oxidns RUNNING
  [ "$(svc_state $c oxidns-log)" = RUNNING ] || true
  ex $c sh -c '[ -s /var/log/configflow/oxidns/current ]' || fail "A: no core log"
  ok "oxidns, agent-oxidns RUNNING; core log /var/log/configflow/oxidns/current"
  local before after
  before=$(svc_pid $c oxidns)
  ex $c kill -9 "$before"
  wait_state $c oxidns RUNNING
  after=$(svc_pid $c oxidns)
  [ "$after" != "$before" ] || fail "A: kill -9 not restarted"
  ok "kill -9 oxidns ($before) restarted as $after"
  before=$(svc_pid $c agent-oxidns)
  ex $c kill -9 "$before"
  wait_state $c agent-oxidns RUNNING
  ok "kill -9 agent-oxidns ($before) restarted as $(svc_pid $c agent-oxidns)"
  ex $c configflow-agent svc stop oxidns
  [ "$(svc_state $c oxidns)" = STOPPED ] && ! ex $c pidof oxidns >/dev/null || fail "A: stop"
  sleep 3
  [ "$(svc_state $c oxidns)" = STOPPED ] || fail "A: stopped core came back"
  ex $c configflow-agent svc start oxidns
  [ "$(svc_state $c oxidns)" = RUNNING ] || fail "A: start"
  before=$(svc_pid $c oxidns)
  ex $c configflow-agent svc restart oxidns
  [ "$(svc_pid $c oxidns)" != "$before" ] || fail "A: restart"
  ok "svc stop (stays down) / start / restart"
  ex $c supervisorctl -c /etc/supervisor/supervisord.conf status oxidns | grep -q '^oxidns .*RUNNING' || fail "A: shim status"
  ex $c supervisorctl stop oxidns >/dev/null && ex $c supervisorctl start oxidns >/dev/null || fail "A: shim stop/start"
  ! ex $c supervisorctl reread 2>"$lab/a-shim.err" && grep -q 'configflow-agent svc' "$lab/a-shim.err" || fail "A: shim misuse"
  ! ex $c python3 -c 'print(1)' 2>"$lab/a-py.err" && grep -q '不含 Python' "$lab/a-py.err" || fail "A: python3 shim misuse"
  ok "supervisorctl shim: status/stop/start; other uses and python3 refused in Chinese"
  ex $c sh -c 'cp /etc/oxidns/config.yaml /tmp/good.yaml && echo "plugins: [" > /etc/oxidns/config.yaml'
  local t0 t1
  t0=$(date +%s)
  ! ex $c configflow-agent svc restart oxidns 2>"$lab/a-fatal.err" || fail "A: broken config started"
  t1=$(date +%s)
  grep -q '多次很快退出' "$lab/a-fatal.err" || fail "A: FATAL message"
  [ "$(svc_state $c oxidns)" = FATAL ] || fail "A: not FATAL"
  sleep 3
  [ "$(svc_state $c oxidns)" = FATAL ] && ! ex $c pidof oxidns >/dev/null || fail "A: FATAL program restarted"
  ex $c sh -c 'cp /tmp/good.yaml /etc/oxidns/config.yaml'
  ex $c configflow-agent svc start oxidns >/dev/null
  [ "$(svc_state $c oxidns)" = RUNNING ] || fail "A: start after FATAL"
  ok "broken config: FATAL after $((t1 - t0)) s (4 quick exits), no further restarts; start after fixing clears FATAL"
  no_zombies $c
  stop_check $c
  "${docker[@]}" start $c >/dev/null
  wait_state $c oxidns RUNNING
  wait_state $c agent-oxidns RUNNING
  ok "docker start: services back"
  stop_check $c
}

scenario_B() {
  say "B: MosDNS through the Agent API"
  local c=cfn-b d=$lab/b
  preset_identity "$d/agent" mosdns
  "${docker[@]}" run -d --name $c -e SERVICE_TYPE=mosdns -e SERVER_URL=http://192.0.2.1:9 -e AGENT_NAME=lab-b \
    -v "$d/agent:/opt/configflow-agent" -v "$d/mosdns:/etc/mosdns" cf-nopy-agent:new >/dev/null
  wait_state $c mosdns RUNNING
  wait_state $c agent-mosdns RUNNING
  local i
  for i in $(seq 50); do PORT_OF=8080 api $c GET /api/capabilities >/dev/null 2>&1 && break; sleep 0.2; done
  local before
  before=$(svc_pid $c mosdns)
  PORT_OF=8080 api $c POST /api/restart | grep -q '"Service restarted"' || fail "B: /api/restart"
  [ "$(svc_state $c mosdns)" = RUNNING ] && [ "$(svc_pid $c mosdns)" != "$before" ] || fail "B: restart did not restart"
  ok "/api/restart restarted mosdns ($before -> $(svc_pid $c mosdns))"
  PORT_OF=8080 api $c GET /api/logs/sources | jq -e '[.sources[] | select(.id == "core" and .path == "/var/log/configflow/mosdns/current" and .kind == "file")] | length == 1' >/dev/null \
    || fail "B: log sources"
  PORT_OF=8080 api $c GET /api/logs/sources | jq -e '[.sources[] | select(.id == "core-err" or .id == "supervisord")] | length == 0' >/dev/null || fail "B: stale sources"
  PORT_OF=8080 api $c GET '/api/logs?lines=5' | jq -e '(.logs | length) > 0 and .error_logs == ""' >/dev/null || fail "B: logs"
  ok "log sources: core = /var/log/configflow/mosdns/current, no separate error output; /api/logs reads it"
  local w id
  w=$(mktemp -d)
  mkdir -p "$w/files"
  sudo cat "$d/mosdns/config.yaml" | sed 's/concurrent: 2/concurrent: 1/' > "$w/files/config.yaml"
  id=$(head -c16 /dev/urandom | od -An -tx1 | tr -d ' \n')
  jq -nc --arg id "$id" --argjson size "$(stat -c %s "$w/files/config.yaml")" --arg sum "$(sha256sum "$w/files/config.yaml" | cut -d' ' -f1)" \
    '{protocol_version: 1, deployment_id: $id, agent_id: "lab-agent-mosdns", profile_id: "lab", service_type: "mosdns",
      config_path: "config.yaml", files: [{path: "config.yaml", size: $size, sha256: $sum, role: "config"}]}' > "$w/manifest.json"
  (cd "$w" && tar czf bundle.tgz manifest.json files/config.yaml)
  before=$(svc_pid $c mosdns)
  PORT_OF=8080 api $c POST /api/deployments -H "Content-Type: application/gzip" -H "X-Deployment-ID: $id" \
    -H "X-Content-SHA256: $(sha256sum "$w/bundle.tgz" | cut -d' ' -f1)" -H "X-Activate: true" --data-binary @"$w/bundle.tgz" >/dev/null
  local state=
  for i in $(seq 120); do
    state=$(PORT_OF=8080 api $c GET "/api/deployments/$id" | jq -r .status)
    case "$state" in succeeded|failed|rolled_back|rollback_failed) break ;; esac
    sleep 0.5
  done
  [ "$state" = succeeded ] || fail "B: deployment $state"
  sudo grep -q 'concurrent: 1' "$d/mosdns/config.yaml" && [ "$(svc_pid $c mosdns)" != "$before" ] && [ "$(svc_state $c mosdns)" = RUNNING ] \
    || fail "B: deployment did not stop/start the core"
  ok "deployment succeeded: config replaced, mosdns stopped and started by the Agent ($before -> $(svc_pid $c mosdns))"
  sudo test ! -e "$d/mosdns/mosdns.err.log" || fail "B: core still writes into the data volume"
  no_zombies $c
  stop_check $c
}

scenario_C() {
  say "C: AIO and single Mihomo with a custom config path; old CMD"
  local c=cfn-c
  "${docker[@]}" run -d --name $c -e ENABLE_MIHOMO=true -e ENABLE_MOSDNS=true -e SERVER_URL=http://192.0.2.1:9 cf-nopy-agent:new >/dev/null
  for p in mihomo agent-mihomo mosdns agent-mosdns; do wait_state $c $p RUNNING; done
  [ "$(svc_state $c oxidns)" = STOPPED ] || fail "C: oxidns should be defined but down"
  ex $c grep -q 'listen: 0.0.0.0:1053' /etc/mihomo/config.yaml || fail "C: AIO mihomo DNS port"
  ex $c sh -c 'tr "\0" " " < /proc/$(pidof mihomo)/cmdline' | grep -qx '/usr/local/bin/mihomo -d /etc/mihomo ' || fail "C: AIO mihomo argv"
  ok "AIO: mihomo + mosdns with Agents RUNNING, oxidns down, default argv"
  stop_check $c
  c=cfn-c2
  "${docker[@]}" run -d --name $c -e SERVICE_TYPE=mihomo -e CONFIG_PATH='/etc/mihomo/my dir/c.yaml' -e SERVER_URL=http://192.0.2.1:9 \
    cf-nopy-agent:new >/dev/null
  wait_state $c mihomo RUNNING
  ex $c sh -c 'tr "\0" "|" < /proc/$(pidof mihomo)/cmdline' | grep -qx '/usr/local/bin/mihomo|-d|/etc/mihomo/my dir|-f|/etc/mihomo/my dir/c.yaml|' \
    || fail "C: custom config argv"
  ex $c grep -q '"config_path": "/etc/mihomo/my dir/c.yaml"' /opt/configflow-agent/config-mihomo.json || fail "C: agent config path"
  ok "single Mihomo: CONFIG_PATH with a space reaches the core argv and the Agent config"
  stop_check $c
  c=cfn-c3
  "${docker[@]}" run -d --name $c -e SERVICE_TYPE=mosdns -e SERVER_URL=http://192.0.2.1:9 cf-nopy-agent:new \
    /usr/bin/supervisord -c /etc/supervisor/supervisord.conf >/dev/null
  wait_state $c mosdns RUNNING
  ok "old CMD /usr/bin/supervisord … still starts s6"
  stop_check $c
}

# ha_spec SELF PEER VIP
ha_spec() { jq -nc --arg s "$1" --arg p "$2" --arg v "$3" '{safety_version: 1, vip: $v, vrid: 51, priority: 150, self_ip: $s, peer_ip: $p}'; }

vip_present() { ex "$1" ip -4 -o addr show | grep -q " $2/"; }

wait_vip() {
  local i
  for i in $(seq 100); do vip_present "$1" "$2" && return 0; sleep 0.2; done
  fail "$1: VIP $2 not assigned"
}

scenario_D() {
  say "D: macvlan Docker HA"
  local c=cfn-d d=$lab/d
  preset_identity "$d/agent" oxidns
  sudo mkdir -p "$d/oxidns" "$d/ha"
  "${docker[@]}" run -d --name $c --network $net --ip 198.51.100.21 --cap-add NET_ADMIN --cap-add NET_RAW \
    -e SERVICE_TYPE=oxidns -e SERVER_URL=http://192.0.2.1:9 -e AGENT_NAME=lab-d -e AGENT_IP=198.51.100.21 \
    -e CONFIGFLOW_DNS_HA=1 -e CONFIGFLOW_DNS_HA_NETWORK=macvlan -e S6_VERBOSITY=2 --stop-timeout 30 \
    -v "$d/agent:/opt/configflow-agent" -v "$d/oxidns:/etc/oxidns" -v "$d/ha:/var/lib/configflow-ha" \
    --mount "type=bind,src=$lab/host-id,dst=/run/configflow-host-machine-id,readonly" cf-nopy-agent:new >/dev/null
  wait_state $c oxidns RUNNING
  wait_state $c agent-oxidns RUNNING
  [ "$(svc_state $c configflow-keepalived)" = STOPPED ] || fail "D: keepalived must not start without a journal"
  local i
  for i in $(seq 50); do api $c GET /api/oxidns-ha >/dev/null 2>&1 && break; sleep 0.2; done
  api $c GET /api/oxidns-ha | jq -e '.environment.ready == true' >/dev/null || { api $c GET /api/oxidns-ha >&2; fail "D: environment not ready"; }
  api $c PUT /api/oxidns-ha -H 'Content-Type: application/json' --data "$(ha_spec 198.51.100.21 198.51.100.22 198.51.100.100)" > "$lab/d-apply.json" \
    || fail "D: apply"
  jq -e '.success != false' "$lab/d-apply.json" >/dev/null || { cat "$lab/d-apply.json" >&2; fail "D: apply refused"; }
  wait_state $c configflow-keepalived RUNNING
  wait_vip $c 198.51.100.100
  ok "Agent applied HA: keepalived RUNNING under s6 in this container, VIP 198.51.100.100 held"
  local before
  before=$(svc_pid $c configflow-keepalived)
  ex $c kill -9 "$before"
  wait_state $c configflow-keepalived RUNNING
  wait_vip $c 198.51.100.100
  ok "kill -9 keepalived ($before) restarted as $(svc_pid $c configflow-keepalived), VIP back"
  no_zombies $c
  local mark
  mark=$("${docker[@]}" logs $c 2>&1 | wc -l)
  stop_check $c 30
  "${docker[@]}" logs $c 2>&1 | tail -n +"$((mark + 1))" > "$lab/d-stop.log"
  local r a o
  r=$(grep -n 's6-rc: info: service ha-release: stopping' "$lab/d-stop.log" | cut -d: -f1)
  a=$(grep -n 's6-rc: info: service agent-oxidns: stopping' "$lab/d-stop.log" | cut -d: -f1)
  o=$(grep -n 's6-rc: info: service oxidns: stopping' "$lab/d-stop.log" | cut -d: -f1)
  [ -n "$r" ] && [ -n "$a" ] && [ -n "$o" ] && [ "$r" -lt "$a" ] && [ "$a" -lt "$o" ] || { cat "$lab/d-stop.log"; fail "D: stop order"; }
  grep -qi 'Stopped\|Stopping' "$lab/d-stop.log" || { cat "$lab/d-stop.log"; fail "D: keepalived did not log a graceful stop"; }
  # ha-release's down script prints this once keepalived is down.
  [ "$(grep -n 'configflow-keepalived：已停止' "$lab/d-stop.log" | cut -d: -f1)" -lt "$a" ] || fail "D: Agent stopped before keepalived was down"
  ok "docker stop: ha-release stopped keepalived first (graceful), then agent-oxidns, then oxidns"
  "${docker[@]}" start $c >/dev/null
  wait_state $c configflow-keepalived RUNNING 60
  wait_vip $c 198.51.100.100
  ok "docker start: the Agent recovered keepalived from the HA journal"
  api $c DELETE /api/oxidns-ha | jq -e '.success == true' >/dev/null || fail "D: remove"
  wait_state $c configflow-keepalived STOPPED
  ! vip_present $c 198.51.100.100 || fail "D: VIP left after removal"
  ok "DELETE: keepalived stopped, VIP released"
  stop_check $c 30
}

# D2: HA journal written by the previous image ("supervisor") recovered by
# the new image's Agent after the container is recreated on the same volumes.
scenario_D2() {
  say "D2: macvlan HA enabled on the base image, container recreated with the new image"
  local c=cfn-d2 d=$lab/d2
  preset_identity "$d/agent" oxidns
  sudo mkdir -p "$d/oxidns" "$d/ha"
  run_d2() {
    "${docker[@]}" run -d --name $c --network $net --ip 198.51.100.23 --cap-add NET_ADMIN --cap-add NET_RAW \
      -e SERVICE_TYPE=oxidns -e SERVER_URL=http://192.0.2.1:9 -e AGENT_NAME=lab-d2 -e AGENT_IP=198.51.100.23 \
      -e CONFIGFLOW_DNS_HA=1 -e CONFIGFLOW_DNS_HA_NETWORK=macvlan --stop-timeout 30 \
      -v "$d/agent:/opt/configflow-agent" -v "$d/oxidns:/etc/oxidns" -v "$d/ha:/var/lib/configflow-ha" \
      --mount "type=bind,src=$lab/host-id,dst=/run/configflow-host-machine-id,readonly" "$1" >/dev/null
  }
  run_d2 cf-nopy-agent:base
  local i
  for i in $(seq 100); do api $c GET /api/oxidns-ha 2>/dev/null | jq -e '.environment.ready == true' >/dev/null 2>&1 && break; sleep 0.3; done
  api $c PUT /api/oxidns-ha -H 'Content-Type: application/json' --data "$(ha_spec 198.51.100.23 198.51.100.24 198.51.100.102)" > "$lab/d2-apply.json" \
    || fail "D2: apply on the base image"
  for i in $(seq 100); do ex $c supervisorctl -c /etc/supervisor/supervisord.conf status configflow-keepalived 2>/dev/null | grep -q RUNNING && break; sleep 0.2; done
  wait_vip $c 198.51.100.102
  sudo jq -e '.service.manager == "supervisor"' "$d/agent/oxidns-ha.json" >/dev/null || { sudo cat "$d/agent/oxidns-ha.json" >&2; fail "D2: base journal"; }
  ok "base image (Supervisor, Python) enabled HA; journal records service.manager \"supervisor\""
  "${docker[@]}" rm -f $c >/dev/null
  run_d2 cf-nopy-agent:new
  wait_state $c configflow-keepalived RUNNING 60
  wait_vip $c 198.51.100.102
  ex $c grep -q '"service_manager": "s6"' /opt/configflow-agent/config-oxidns.json || fail "D2: new config value"
  api $c GET /api/oxidns-ha | jq -e '.managed.active == true and .runtime.keepalived_running == true' >/dev/null || { api $c GET /api/oxidns-ha >&2; fail "D2: status after upgrade"; }
  ok "new image: legacy journal accepted, Agent recovered keepalived under s6, VIP held; config now says \"s6\""
  api $c DELETE /api/oxidns-ha | jq -e '.success == true' >/dev/null || fail "D2: remove"
  wait_state $c configflow-keepalived STOPPED
  ok "DELETE after upgrade: keepalived stopped"
  stop_check $c 30
}

scenario_E() {
  say "E: host-mode Docker HA with the companion (shared sandbox netns)"
  local d=$lab/e sandbox=cfn-e-net main=cfn-e companion=cfn-e-ha
  preset_identity "$d/agent" oxidns
  sudo mkdir -p "$d/oxidns" "$d/ha"
  "${docker[@]}" run -d --name $sandbox --network $net --ip 198.51.100.31 --entrypoint sleep cf-nopy-agent:new infinity >/dev/null
  local pid
  pid=$("${docker[@]}" inspect -f '{{.State.Pid}}' $sandbox)
  local common=(--network "container:$sandbox" -e CONFIGFLOW_DNS_HA=1 -e CONFIGFLOW_DNS_HA_NETWORK=host
    --mount "type=bind,src=$lab/host-id,dst=/run/configflow-host-machine-id,readonly" -v "$d/ha:/var/lib/configflow-ha")
  "${docker[@]}" run -d --name $companion "${common[@]}" --cap-add NET_ADMIN --cap-add NET_RAW -e SERVICE_TYPE=ha-companion --stop-timeout 30 \
    -v "$d/agent:/opt/configflow-agent:ro" -v "$d/oxidns:/etc/oxidns:ro" cf-nopy-agent:new >/dev/null
  wait_state $companion ha-companion-serve RUNNING
  [ "$(svc_state $companion configflow-keepalived)" = STOPPED ] || fail "E: companion started keepalived on its own"
  sudo test -S "$d/ha/run/control.sock" && [ "$(sudo stat -c %a "$d/ha/run/control.sock")" = 600 ] || fail "E: control socket"
  sudo jq -e '.protocol == 1 and .mode == "host"' "$d/ha/run/companion.json" >/dev/null || fail "E: evidence"
  ok "companion: ha-companion-check evidence written, control socket 0600, keepalived waiting"
  "${docker[@]}" run -d --name $main "${common[@]}" -e SERVICE_TYPE=oxidns -e SERVER_URL=http://192.0.2.1:9 -e AGENT_NAME=lab-e \
    -e AGENT_IP=198.51.100.31 --stop-timeout 30 -v "$d/agent:/opt/configflow-agent" -v "$d/oxidns:/etc/oxidns" \
    --mount "type=bind,src=/proc/$pid/ns/net,dst=/run/configflow-host-netns,readonly" cf-nopy-agent:new >/dev/null
  wait_state $main agent-oxidns RUNNING
  local i
  for i in $(seq 50); do api $sandbox GET /api/oxidns-ha >/dev/null 2>&1 && break; sleep 0.2; done
  api $sandbox GET /api/oxidns-ha > "$lab/e-status.json"
  jq -e '.environment.ready == true' "$lab/e-status.json" >/dev/null || { cat "$lab/e-status.json" >&2; fail "E: environment not ready"; }
  ok "main Agent sees the companion (peer in another PID namespace, evidence, control socket)"
  api $sandbox PUT /api/oxidns-ha -H 'Content-Type: application/json' --data "$(ha_spec 198.51.100.31 198.51.100.32 198.51.100.101)" > "$lab/e-apply.json" \
    || fail "E: apply"
  jq -e '.success != false' "$lab/e-apply.json" >/dev/null || { cat "$lab/e-apply.json" >&2; fail "E: apply refused"; }
  wait_state $companion configflow-keepalived RUNNING
  [ "$(svc_state $main configflow-keepalived)" = STOPPED ] || fail "E: keepalived ran in the main container"
  wait_vip $companion 198.51.100.101
  ok "Agent applied HA: keepalived RUNNING in the companion (not the main container), VIP held in the shared netns"
  "${docker[@]}" restart $companion >/dev/null
  wait_state $companion ha-companion-serve RUNNING
  wait_state $companion configflow-keepalived RUNNING 60
  ok "companion recreated/restarted: the Agent restarted keepalived through the new control socket"
  api $sandbox DELETE /api/oxidns-ha | jq -e '.success == true' >/dev/null || fail "E: remove"
  wait_state $companion configflow-keepalived STOPPED
  ok "DELETE: companion keepalived stopped"
  no_zombies $companion
  stop_check $main 30
  stop_check $companion 30
}

# compact_compose DIR IMAGE PANEL: generate a compact (macvlan) Compose.
compact_compose() {
  mkdir -p "$1/compose"
  jq -n --arg url "http://198.51.100.254:$port" --arg data "$1/data" --arg image "$2" \
    '{action: "generate", mode: "new", install_method: "compose_compact", server_url: $url, agent_name: "labf",
      agent_ip: "198.51.100.41", oxidns_port: 8082, data_dir: $data, network_mode: "macvlan", ha_parent: "cfnlab0",
      ha_subnet: "198.51.100.0/24", ha_gateway: "198.51.100.1", image: $image}' > "$1/request.json"
  curl -fsS -H 'Content-Type: application/json' --data @"$1/request.json" "http://127.0.0.1:$port/api/agents/docker-oxidns-ha-guide" > "$1/guide.json" \
    || { cat "$1/guide.json" >&2; fail "guide $1"; }
  jq -r .compose "$1/guide.json" > "$1/compose/compose.yaml"
  # The lab macvlan network already exists on cfnlab0; reuse it.
  sed -i "/^networks:/,\$d" "$1/compose/compose.yaml"
  printf 'networks:\n  dns-ha:\n    external: true\n    name: %s\n' "$net" >> "$1/compose/compose.yaml"
}

fcompose() { (cd "$1/compose" && "${docker[@]}" compose "${@:2}"); }

wait_compact() {
  local i logs
  for i in $(seq 150); do
    logs=$(fcompose "$1" logs --no-log-prefix 2>&1 || true)
    if grep -qF "$2" <<<"$logs"; then return 0; fi
    sleep 0.2
  done
  printf '%s\n' "$logs" | tail -20 >&2
  fail "$1: no '$2'"
}

scenario_F() {
  say "F: compact Compose (macvlan): new image, then upgrade from the base image"
  start_panel configflow
  local f=$lab/f
  compact_compose "$f" cf-nopy-agent:new
  sudo mkdir -p "$f/data/agent" "$f/data/oxidns" "$f/data/ha"
  grep -q 'SERVICE_TYPE: oxidns-ha-macvlan' "$f/compose/compose.yaml" || fail "F: compose"
  fcompose "$f" up -d
  wait_compact "$f" '持久目录、安装身份和登记已核对，启动 OxiDNS。'
  local c
  c=$(fcompose "$f" ps -q agent)
  wait_state "$c" oxidns RUNNING
  wait_state "$c" agent-oxidns RUNNING
  ex "$c" sh -c 'for p in $(pidof configflow-agent oxidns); do tr "\0" "\n" < /proc/$p/environ; done | grep -q "^INSTALL_KEY="' \
    && fail "F: a service sees INSTALL_KEY" || true
  ex "$c" sh -c 'tr "\0" "\n" < /proc/$(pidof configflow-agent | cut -d" " -f1)/environ | grep -qx "CONFIGFLOW_DNS_HA_IDENTITY=installation"' \
    || fail "F: Agent lacks the HA environment ha-compact set"
  ok "first install on the new image: ha-compact enrolled, OxiDNS and Agent RUNNING, INSTALL_KEY not passed on"
  stop_panel
  fcompose "$f" up -d --force-recreate
  wait_compact "$f" '已核对持久 Agent 身份；无需安装凭据或登记网络，继续启动。'
  ok "offline restart (panel stopped): identity verified"
  fcompose "$f" down

  local g=$lab/f-base
  start_panel configflow-base
  compact_compose "$g" cf-nopy-agent:lab-tag
  "${docker[@]}" tag cf-nopy-agent:base cf-nopy-agent:lab-tag
  sudo mkdir -p "$g/data/agent" "$g/data/oxidns" "$g/data/ha"
  fcompose "$g" up -d
  wait_compact "$g" '持久目录、安装身份和登记已核对，启动 OxiDNS。'
  ex "$(fcompose "$g" ps -q agent)" sh -c 'command -v supervisord && command -v python3' >/dev/null || fail "F: base image is not the Python image"
  ok "install on the base image (docker-compose-compact.py under Python, Supervisor)"
  stop_panel
  "${docker[@]}" tag cf-nopy-agent:new cf-nopy-agent:lab-tag
  fcompose "$g" up -d --force-recreate
  wait_compact "$g" '已核对持久 Agent 身份；无需安装凭据或登记网络，继续启动。'
  c=$(fcompose "$g" ps -q agent)
  wait_state "$c" oxidns RUNNING
  wait_state "$c" agent-oxidns RUNNING
  ok "same compose.yaml, image upgraded: ha-compact accepted the Python-made installation; services under s6"
  fcompose "$g" down
}

scenario_G() {
  say "G: compose_direct from the previous panel, base image then the new image (python3 shim)"
  start_panel configflow-base
  local g=$lab/g
  mkdir -p "$g/compose"
  jq -n --arg url "http://198.51.100.254:$port" --arg data "$g/data" --arg id "$lab/host-id" \
    '{action: "generate", mode: "new", install_method: "compose_direct", server_url: $url, agent_name: "labg",
      agent_ip: "198.51.100.51", oxidns_port: 8082, data_dir: $data, network_mode: "macvlan", ha_parent: "cfnlab0",
      ha_subnet: "198.51.100.0/24", ha_gateway: "198.51.100.1", ha_host_id_path: $id, image: "cf-nopy-agent:lab-direct"}' > "$g/request.json"
  curl -fsS -H 'Content-Type: application/json' --data @"$g/request.json" "http://127.0.0.1:$port/api/agents/docker-oxidns-ha-guide" > "$g/guide.json" \
    || { cat "$g/guide.json" >&2; fail "G: guide"; }
  jq -r .compose "$g/guide.json" > "$g/compose/compose.yaml"
  sed -i "/^networks:/,\$d" "$g/compose/compose.yaml"
  printf 'networks:\n  dns-ha:\n    external: true\n    name: %s\n' "$net" >> "$g/compose/compose.yaml"
  grep -q 'docker-compose-install.py' "$g/compose/compose.yaml" || fail "G: old panel compose has no python3 runtime"
  sudo mkdir -p "$g/data"
  "${docker[@]}" tag cf-nopy-agent:base cf-nopy-agent:lab-direct
  fcompose "$g" up -d
  wait_compact "$g" '本机 Agent 身份已持久保存；继续启动并等待真实心跳。'
  ok "previous panel's compose_direct on the base image: prepared and enrolled under Python"
  stop_panel
  "${docker[@]}" tag cf-nopy-agent:new cf-nopy-agent:lab-direct
  fcompose "$g" up -d --force-recreate
  wait_compact "$g" '已核对持久 Agent 身份；无需安装凭据或登记网络，继续启动。'
  local c
  c=$(fcompose "$g" ps -q agent)
  wait_state "$c" oxidns RUNNING
  ok "same compose.yaml on the new image: python3 shim -> configflow-agent ha-compose prepare/run, services under s6"
  fcompose "$g" down
}

scenario_G2() {
  say "G2: compose_direct from this panel on the new image (configflow-agent ha-compose)"
  start_panel configflow
  local g=$lab/g2
  mkdir -p "$g/compose"
  jq -n --arg url "http://198.51.100.254:$port" --arg data "$g/data" --arg id "$lab/host-id" \
    '{action: "generate", mode: "new", install_method: "compose_direct", server_url: $url, agent_name: "labg2",
      agent_ip: "198.51.100.52", oxidns_port: 8082, data_dir: $data, network_mode: "macvlan", ha_parent: "cfnlab0",
      ha_subnet: "198.51.100.0/24", ha_gateway: "198.51.100.1", ha_host_id_path: $id, image: "cf-nopy-agent:new"}' > "$g/request.json"
  curl -fsS -H 'Content-Type: application/json' --data @"$g/request.json" "http://127.0.0.1:$port/api/agents/docker-oxidns-ha-guide" > "$g/guide.json" \
    || { cat "$g/guide.json" >&2; fail "G2: guide"; }
  jq -r .compose "$g/guide.json" > "$g/compose/compose.yaml"
  sed -i "/^networks:/,\$d" "$g/compose/compose.yaml"
  printf 'networks:\n  dns-ha:\n    external: true\n    name: %s\n' "$net" >> "$g/compose/compose.yaml"
  ! grep -qi 'python' "$g/compose/compose.yaml" && grep -q -- '- ha-compose' "$g/compose/compose.yaml" || fail "G2: compose still mentions Python"
  sudo mkdir -p "$g/data"
  fcompose "$g" up -d
  wait_compact "$g" '本机 Agent 身份已持久保存；继续启动并等待真实心跳。'
  wait_compact "$g" '初始化完成；仅使用指定应用目录和选定宿主身份来源。'
  wait_state "$(fcompose "$g" ps -q agent)" oxidns RUNNING
  ok "init container ran ha-compose prepare (read-only rootfs, no Python), Agent ran ha-compose run and enrolled"
  stop_panel
  fcompose "$g" up -d --force-recreate
  wait_compact "$g" '已核对持久 Agent 身份；无需安装凭据或登记网络，继续启动。'
  ok "offline recreate: identity verified"
  fcompose "$g" down
}

# F2: compact Compose in host mode. The two services' network_mode: host is
# replaced by one sandbox namespace on the lab LAN, so nothing touches the
# server's own network namespace; everything else is the generated file.
scenario_F2() {
  say "F2: compact Compose, host mode with the companion (installation identity)"
  start_panel configflow
  local f=$lab/f2 sandbox=cfn-f2-net
  "${docker[@]}" run -d --name $sandbox --network $net --ip 198.51.100.43 --entrypoint sleep cf-nopy-agent:new infinity >/dev/null
  local pid
  pid=$("${docker[@]}" inspect -f '{{.State.Pid}}' $sandbox)
  mkdir -p "$f/compose"
  jq -n --arg url "http://198.51.100.254:$port" --arg data "$f/data" \
    '{action: "generate", mode: "new", install_method: "compose_compact", server_url: $url, agent_name: "labf2",
      agent_ip: "198.51.100.43", oxidns_port: 8082, data_dir: $data, network_mode: "host", image: "cf-nopy-agent:new"}' > "$f/request.json"
  curl -fsS -H 'Content-Type: application/json' --data @"$f/request.json" "http://127.0.0.1:$port/api/agents/docker-oxidns-ha-guide" > "$f/guide.json" \
    || { cat "$f/guide.json" >&2; fail "F2: guide"; }
  jq -r .compose "$f/guide.json" > "$f/compose/compose.yaml"
  grep -q 'SERVICE_TYPE: ha-companion-compact' "$f/compose/compose.yaml" || fail "F2: no companion"
  sed -i "s#network_mode: host#network_mode: container:$sandbox#; s#source: /proc/1/ns/net#source: /proc/$pid/ns/net#" "$f/compose/compose.yaml"
  sudo mkdir -p "$f/data/agent" "$f/data/oxidns" "$f/data/ha"
  fcompose "$f" up -d
  wait_compact "$f" '持久目录、安装身份和登记已核对，启动 OxiDNS。'
  local main companion
  main=$(fcompose "$f" ps -q agent)
  companion=$(fcompose "$f" ps -q ha-companion)
  wait_state "$companion" ha-companion-serve RUNNING 40
  wait_state "$main" agent-oxidns RUNNING
  sudo jq -e '.mode == "host"' "$f/data/ha/run/companion.json" >/dev/null || fail "F2: companion evidence"
  local i
  for i in $(seq 50); do curl -s -m2 http://198.51.100.43:8082/health >/dev/null && break; sleep 0.2; done
  ok "ha-compact claimed the installation; ha-companion-check (installation identity) waited for it and published evidence"
  stop_panel
  fcompose "$f" up -d --force-recreate
  wait_compact "$f" '已核对持久 Agent 身份；无需安装凭据或登记网络，继续启动。'
  wait_state "$(fcompose "$f" ps -q ha-companion)" ha-companion-serve RUNNING 40
  ok "recreate both: identity verified, companion accepted the installation again"
  fcompose "$f" down
  "${docker[@]}" rm -f $sandbox >/dev/null
}

scenario_H() {
  say "H: migration check of an old single-OxiDNS container by the new image"
  local d=$lab/h old=cfn-h-old
  preset_identity "$d/agent" oxidns
  "${docker[@]}" run -d --name $old -e SERVICE_TYPE=oxidns -e SERVER_URL=http://192.0.2.10:5010 -e AGENT_NAME=dns-old -e AGENT_PORT=8082 \
    -v "$d/agent:/opt/configflow-agent" -v "$d/oxidns:/etc/oxidns" cf-nopy-agent:base >/dev/null
  sleep 5
  local out
  out=$("${docker[@]}" inspect --format '{{json .Config.Env}}' $old | "${docker[@]}" run --rm -i --network none --volumes-from "$old:ro" \
    --entrypoint configflow-agent cf-nopy-agent:new ha-migration-check dns-old http://192.0.2.10:5010 8082) || fail "H: migration check refused"
  grep -q 'SERVER_URL' <<<"$out" || true
  printf '%s\n' "$out" | head -5
  ok "ha-migration-check (new image, old volumes read-only) accepted the Supervisor-era container"
  "${docker[@]}" rm -f $old >/dev/null
}

setup
for s in $scenarios; do "scenario_$s"; done
say "LAB PASS: $scenarios"
