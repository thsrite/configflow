#!/usr/bin/env bash
# Smoke test of the panel image's process model (tini → configflow → Sub-Store),
# synthetic data only. Usage: panel-image-smoke.sh <image> [--skip-idle]
# Checks: no Python in the image, tini is PID 1, Sub-Store starts on demand for a
# node conversion, restarts after kill -9, stops after 5 idle minutes, orphans
# are reaped, and docker stop ends everything cleanly (exit 0, within grace).
set -euo pipefail
image=$1
skip_idle=${2:-}
name=cf-nopy-smoke-$$
port=$((18000 + $$ % 1000))
fail=0
step() { printf '\n== %s\n' "$*"; }
ok() { printf 'PASS %s\n' "$*"; }
bad() { printf 'FAIL %s\n' "$*"; fail=1; }
cx() { docker exec "$name" "$@"; }
node_pids() { cx sh -c "ps -o pid,pgid,args | awk '/[n]ode .*sub-store.bundle.js/ {print \$1\":\"\$2}'"; }
convert() {
	curl -s -m 60 -X POST "http://127.0.0.1:$port/api/nodes/parse-lines" -H 'Content-Type: application/json' \
		-d "{\"lines\":[\"ss://YWVzLTI1Ni1nY206c3ludGhldGlj@192.0.2.10:${1:-8388}#smoke\"]}"
}
api() { curl -s -m 60 -H 'Content-Type: application/json' "$@"; }
cleanup() { docker rm -f "$name" >/dev/null 2>&1 || true; }
trap cleanup EXIT

step "start $image"
docker run -d --name "$name" -p "127.0.0.1:$port:80" "$image" >/dev/null
for _ in $(seq 60); do curl -sf "http://127.0.0.1:$port/health" >/dev/null && break; sleep 1; done
curl -sf "http://127.0.0.1:$port/health" >/dev/null && ok "healthy" || bad "not healthy"
docker inspect -f '{{.Config.Entrypoint}} {{.Config.Cmd}}' "$name"

step "image contents"
cx sh -c 'command -v python3 python supervisord supervisorctl' && bad "python/supervisord present" || ok "no python3/supervisord binaries"
cx sh -c 'apk info 2>/dev/null | grep -iE "^(python|py3-|supervisor)"' && bad "python packages installed" || ok "no python packages"
pid1=$(cx cat /proc/1/comm)
[ "$pid1" = tini ] && ok "PID 1 is tini" || bad "PID 1 is $pid1"
cx ps -o pid,ppid,pgid,stat,args

step "synthetic subscription"
# A local subscription server (synthetic node) for the template subscription;
# its first refresh then succeeds and the next one is a day away, so the
# scheduled refresh does not keep Sub-Store awake during the idle check.
docker exec -i "$name" sh -c 'cat > /tmp/sub-server.js' <<'JS'
require("http").createServer((q, r) => r.end("ss://YWVzLTI1Ni1nY206c3ludGhldGlj@192.0.2.11:8388#sub-node\n")).listen(8099, "127.0.0.1")
JS
docker exec -d "$name" node /tmp/sub-server.js
for _ in $(seq 50); do cx wget -qO- http://127.0.0.1:8099/sub >/dev/null 2>&1 && break; sleep 0.2; done
sub=$(api "http://127.0.0.1:$port/api/subscriptions" | grep -o '"id":"sub_[^"]*"' | head -1 | cut -d'"' -f4)
api -X PUT "http://127.0.0.1:$port/api/subscriptions/$sub" \
	-d '{"name":"subscribe","url":"http://127.0.0.1:8099/sub","type":"universal","enabled":true,"interval":86400}' >/dev/null
echo "subscription $sub → local synthetic server"

step "on-demand start"
[ -z "$(node_pids)" ] && ok "Sub-Store not running before use" || bad "Sub-Store running at boot: $(node_pids)"
fetched=$(api -X POST "http://127.0.0.1:$port/api/subscriptions/$sub/fetch" -d '{}')
echo "$fetched" | head -c 300; echo
if echo "$fetched" | grep -q '"count": *1'; then ok "subscription conversion through Sub-Store"; else
	bad "subscription fetch failed"; cx sh -c 'grep -E "WARNING|ERROR|下载|拉取" /data/app.log | tail -15 | cut -c1-300'; fi
cx sh -c 'grep -q "Sub-Store 下载" /data/app.log' && ok "fetch went through Sub-Store" || bad "fetch bypassed Sub-Store"
result=$(convert)
echo "$result" | head -c 400; echo
echo "$result" | grep -q '"type": *"ss"' && ok "node conversion through Sub-Store" || bad "conversion failed"
first=$(node_pids)
[ -n "$first" ] && ok "Sub-Store started: pid:pgid=$first" || bad "Sub-Store not running after use"
pid=${first%%:*}; pgid=${first##*:}
[ "$pid" = "$pgid" ] && ok "Sub-Store leads its own process group" || bad "pgid $pgid != pid $pid"
ppid=$(cx sh -c "awk '{print \$4}' /proc/$pid/stat")
[ "$(cx cat /proc/$ppid/comm)" = configflow ] && ok "parent is configflow" || bad "parent is $(cx cat /proc/$ppid/comm)"
cx sh -c 'grep -c " - sub-store - " /data/app.log' | sed 's/^/sub-store lines in app.log: /'
cx sh -c 'grep " - sub-store - " /data/app.log | tail -3'

# Identical lines are answered from the conversion cache; each check below
# converts a different synthetic node so it really reaches Sub-Store.
step "kill -9 Sub-Store"
cx kill -9 "$pid"
second=""
for _ in $(seq 20); do sleep 0.5; second=$(node_pids); [ -n "$second" ] && [ "$second" != "$first" ] && break; done
[ -n "$second" ] && [ "$second" != "$first" ] && ok "restarted: $second" || bad "not restarted (now: '$second')"
cx sh -c 'grep "意外退出" /data/app.log | tail -1'
result=$(convert 8389)
echo "$result" | grep -q '"type": *"ss"' && ok "conversion after restart" || bad "conversion after restart failed"
cx sh -c 'ps -o stat,args | grep -E "^ *Z"' && bad "zombie after crash" || ok "no zombies after crash"

step "orphan reaping by tini"
cx sh -c '(sleep 1 &) ; true'
sleep 3
cx sh -c 'ps -o stat,args | grep -E "^ *Z"' && bad "orphan left as zombie" || ok "orphan reaped"

if [ "$skip_idle" != --skip-idle ]; then
	step "idle stop (5 min)"
	sleep 320
	[ -z "$(node_pids)" ] && ok "Sub-Store stopped after idling" || bad "still running after idle: $(node_pids)"
	cx sh -c 'grep "Sub-Store 已停止" /data/app.log | tail -1'
	cx sh -c 'ps -o stat,args | grep -E "^ *Z"' && bad "zombie after idle stop" || ok "no zombies after idle stop"
	result=$(convert 8390)
	[ -n "$(node_pids)" ] && ok "on-demand start again: $(node_pids)" || bad "no start after idle"
fi

step "docker stop"
[ -n "$(node_pids)" ] || convert 8391 >/dev/null
begin=$(date +%s.%N)
docker stop -t 10 "$name" >/dev/null
elapsed=$(echo "$(date +%s.%N) - $begin" | bc)
code=$(docker inspect -f '{{.State.ExitCode}}' "$name")
echo "docker stop took ${elapsed}s, exit code $code"
[ "$code" = 0 ] && ok "clean exit (not SIGKILLed)" || bad "exit code $code"
awk "BEGIN{exit !($elapsed < 9.5)}" && ok "stopped within grace" || bad "stop took ${elapsed}s"
docker logs "$name" 2>&1 | grep -E "Sub-Store 已停止|Shutting down|关" | tail -5 || true

echo
[ $fail = 0 ] && echo "ALL PASS" || echo "SOME FAILED"
exit $fail
