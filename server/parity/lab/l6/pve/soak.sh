#!/bin/bash
# soak.sh start <run> [proxy-port]   start the collectors (background, survive this shell) into $WORK/soak/<run>/
# soak.sh stop <run>                 stop them
# soak.sh summary <run>              min / median / max / last per metric, error and probe counts
#
# Collected (C.4 of o14-switch-plan.md + the O10 24 h soak):
#   panel.tsv    every 60 s: panel container CPU% and memory (docker stats), main process RSS kB,
#                threads, open fds, CPU ticks; Sub-Store (node) RSS when running
#   devices.tsv  every 300 s: RSS of the Agent and core processes on the gateway / DNS CTs,
#                docker stats of the Docker HA VMs
#   logs.tsv     every 600 s: panel log lines per level since the previous sample; errors.log keeps
#                the WARN/ERROR lines themselves (lab data only; never paste them into reports)
#   agents.tsv   every 600 s: per Agent status, latest deployment status, config version (panel API)
#   probe-dns-*.log / probe-http.log (client CT 257): failures with ms timestamps + per-minute totals
#                for dig on both VIPs (10/s) and curl through the pilot gateway's proxy port (1/s)
# The Go release image has no pprof: goroutines are not sampled here; threads and fds are.
set -euo pipefail
cd "$(dirname "$0")"
. ./lab.conf
ENVFILE=$WORK/panel.env
run=${2:-}
dir=$WORK/soak/$run

panel_sample() {
  local line
  line=$(gexec 250 sh -c '
    s=$(docker stats --no-stream --format "{{.CPUPerc}}\t{{.MemUsage}}" cf-panel 2>/dev/null | sed "s# / [^\t]*##")
    pid=$(docker inspect -f "{{.State.Pid}}" cf-panel 2>/dev/null)
    main=""
    for p in $(cat /proc/$pid/task/*/children 2>/dev/null) $pid; do
      c=$(cat /proc/$p/comm 2>/dev/null)
      case $c in configflow|gunicorn|python*) main=$p;; esac
    done
    [ -n "$main" ] || main=$(pgrep -x configflow | head -1)
    rss=$(awk "/VmRSS/ {print \$2}" /proc/$main/status 2>/dev/null)
    thr=$(awk "/Threads/ {print \$2}" /proc/$main/status 2>/dev/null)
    fds=$(ls /proc/$main/fd 2>/dev/null | wc -l)
    ticks=$(awk "{print \$14+\$15}" /proc/$main/stat 2>/dev/null)
    node=$(for n in $(pgrep -x node); do awk "/VmRSS/ {print \$2}" /proc/$n/status; done | head -1)
    printf "%s\t%s\t%s\t%s\t%s\t%s\t%s\n" "$s" "$(cat /proc/$main/comm 2>/dev/null)" "$rss" "$thr" "$fds" "$ticks" "${node:--}"
  ' 2>/dev/null || true)
  printf '%s\t%s\n' "$(date +%s)" "$line" >> "$dir/panel.tsv"
}

devices_sample() {
  local id out
  for id in 252 253 254 255; do
    pct status $id 2>/dev/null | grep -q running || continue
    out=$(gexec $id sh -c 'for c in configflow-agent mihomo oxidns keepalived; do for p in $(pgrep -x $c); do printf "%s=%s " $c $(awk "/VmRSS/ {print \$2}" /proc/$p/status); done; done' 2>/dev/null || true)
    printf '%s\t%s\t%s\n' "$(date +%s)" "$id" "$out" >> "$dir/devices.tsv"
  done
  for id in 258 259; do
    qm status $id 2>/dev/null | grep -q running || continue
    out=$(ssh -i "$SSH_KEY" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -o ConnectTimeout=5 \
      "root@$(guest_ip $id)" 'docker stats --no-stream --format "{{.Name}}={{.MemUsage}}" | sed "s# / [^ ]*##" | tr "\n" " "' 2>/dev/null || true)
    printf '%s\t%s\t%s\n' "$(date +%s)" "$id" "$out" >> "$dir/devices.tsv"
  done
}

logs_sample() { # logs_sample <since-epoch>
  local since=$1
  gexec 250 sh -c "docker logs --since $since cf-panel 2>&1" > "$dir/.logs.tmp" || true
  printf '%s\t%s\t%s\t%s\n' "$(date +%s)" \
    "$(grep -cE 'level=ERROR| ERROR |\[ERROR\]|Traceback' "$dir/.logs.tmp" || true)" \
    "$(grep -cE 'level=WARN| WARNING |\[WARN' "$dir/.logs.tmp" || true)" \
    "$(wc -l < "$dir/.logs.tmp")" >> "$dir/logs.tsv"
  grep -E 'level=(WARN|ERROR)| (WARNING|ERROR) |Traceback|panic' "$dir/.logs.tmp" >> "$dir/errors.log" || true
}

agents_sample() {
  local user pass
  user=$(sed -n 's/^ADMIN_USERNAME=//p' "$ENVFILE"); pass=$(sed -n 's/^ADMIN_PASSWORD=//p' "$ENVFILE")
  python3 - "$PANEL_IP" "$user" "$pass" >> "$dir/agents.tsv" <<'P' || true
import json, sys, time, urllib.request
host, user, password = sys.argv[1:4]
def call(path, body=None, token=None):
    req = urllib.request.Request(f"http://{host}{path}", data=None if body is None else json.dumps(body).encode(),
                                 headers={"Content-Type": "application/json", **({"Authorization": f"Bearer {token}"} if token else {})})
    return json.load(urllib.request.urlopen(req, timeout=20))
now = int(time.time())
try:
    token = call("/api/auth/login", {"username": user, "password": password}).get("token")
    data = call("/api/agents", token=token)
    agents = data.get("agents", data.get("data", data)) if isinstance(data, dict) else data
    for a in agents or []:
        dep = a.get("latest_deployment") or {}
        print(f"{now}\t{a.get('id')}\t{a.get('status')}\t{dep.get('status', '-')}\t{a.get('config_version', '-')}\t{a.get('version', '-')}")
except Exception as e:
    print(f"{now}\t-\terror\t{type(e).__name__}\t-\t-")
P
}

probes_start() { # dig loops on both VIPs, curl through the pilot gateway's proxy port
  local port=$1
  gexec 257 bash -c "mkdir -p /root/soak && cat > /root/soak/probe.sh <<'S'
#!/bin/bash
# probe.sh dns <server> <name> | http <proxy host:port> <url> <name>
mode=\$1
if [ \"\$mode\" = dns ]; then server=\$2 name=\$3 interval=0.1
else proxy=\$2 url=\$3 name=\$4 interval=1; fi
log=/root/soak/probe-\$name.log
min=\$(date +%Y-%m-%dT%H:%M); ok=0; bad=0
while :; do
  if [ \"\$mode\" = dns ]; then dig +time=1 +tries=1 +short @\$server example.com A >/dev/null 2>&1 && r=0 || r=1
  else curl -s -m 3 -o /dev/null -x http://\$proxy \$url && r=0 || r=1; fi
  if [ \$r = 0 ]; then ok=\$((ok+1)); else bad=\$((bad+1)); echo \"\$(date +%Y-%m-%dT%H:%M:%S.%3N) FAIL\" >> \$log; fi
  m=\$(date +%Y-%m-%dT%H:%M)
  if [ \"\$m\" != \"\$min\" ]; then echo \"\$min ok=\$ok fail=\$bad\" >> \$log; min=\$m; ok=0; bad=0; fi
  sleep \$interval
done
S
chmod 755 /root/soak/probe.sh
pkill -f /root/soak/probe[.]sh || true
nohup /root/soak/probe.sh dns $VIP_NATIVE dns-native >/dev/null 2>&1 &
nohup /root/soak/probe.sh dns $VIP_DOCKER dns-docker >/dev/null 2>&1 &
nohup /root/soak/probe.sh http $(guest_ip 252):$port $HTTP/ok.txt http >/dev/null 2>&1 &
echo probes started"
}

collector() { # the loop itself (run under nohup)
  local t=0 last
  last=$(date +%s)
  while [ -f "$dir/.running" ]; do
    panel_sample
    if [ $((t % 300)) = 0 ]; then devices_sample; fi
    if [ $((t % 600)) = 0 ]; then logs_sample "$last"; last=$(date +%s); agents_sample; fi
    sleep 60; t=$((t + 60))
  done
}

summary() {
  python3 - "$dir" <<'P'
import os, statistics, sys
d = sys.argv[1]
rows = [l.rstrip("\n").split("\t") for l in open(os.path.join(d, "panel.tsv")) if l.strip()]
def col(i, conv=float):
    out = []
    for r in rows:
        try: out.append(conv(r[i].rstrip("%").replace("MiB", "").strip()))
        except (ValueError, IndexError): pass
    return out
span = (int(rows[-1][0]) - int(rows[0][0])) / 3600 if len(rows) > 1 else 0
print(f"samples {len(rows)} over {span:.1f} h")
for name, i in (("cpu%", 1), ("rss_kB", 4), ("threads", 5), ("fds", 6)):
    v = col(i)
    if v: print(f"{name:8s} min {min(v):.1f}  median {statistics.median(v):.1f}  max {max(v):.1f}  last {v[-1]:.1f}")
t = col(7)
if len(t) > 1 and span: print(f"cpu avg  {(t[-1]-t[0]) / os.sysconf('SC_CLK_TCK') / (span*3600) * 100:.2f}% of one core")
lp = os.path.join(d, "logs.tsv")
if os.path.exists(lp):
    L = [l.split("\t") for l in open(lp) if l.strip()]
    print(f"log lines: error {sum(int(x[1]) for x in L)}  warn {sum(int(x[2]) for x in L)}  total {sum(int(x[3]) for x in L)}")
P
  for f in "$dir"/probe-*.log; do
    [ -f "$f" ] || continue
    echo "$(basename "$f"): $(awk '/ok=/ {split($2,a,"=");split($3,b,"="); ok+=a[2]; bad+=b[2]} END {print ok+0 " ok, " bad+0 " failed"}' "$f")"
  done
}

case ${1:-} in
  start)
    [ -n "$run" ] || { echo "usage: soak.sh start <run> [proxy-port]" >&2; exit 2; }
    mkdir -p "$dir"; touch "$dir/.running"
    printf 'epoch\tcpu%%\tmem\tproc\trss_kB\tthreads\tfds\tticks\tnode_rss_kB\n' > "$dir/panel.tsv"
    printf 'epoch\tid\tstatus\tdeployment\tconfig_version\tagent_version\n' > "$dir/agents.tsv"
    probes_start "${3:-7890}"
    nohup bash "$0" _collect "$run" > "$dir/collector.log" 2>&1 < /dev/null &
    echo "soak $run started: $dir"
    ;;
  _collect) collector ;;
  stop)
    rm -f "$dir/.running"
    gexec 257 bash -c 'pkill -f /root/soak/probe[.]sh; true'
    pct pull 257 /root/soak/probe-dns-native.log "$dir/probe-dns-native.log" 2>/dev/null || true
    pct pull 257 /root/soak/probe-dns-docker.log "$dir/probe-dns-docker.log" 2>/dev/null || true
    pct pull 257 /root/soak/probe-http.log "$dir/probe-http.log" 2>/dev/null || true
    echo "stopped; collector exits within 60 s"
    ;;
  summary) summary ;;
  *) sed -n '2,17p' "$0"; exit 2;;
esac
