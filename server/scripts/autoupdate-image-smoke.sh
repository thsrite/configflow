#!/usr/bin/env bash
# Smoke test of the autoupdate image against a local git repository (no GitHub,
# no production): run from the repository root on a test server.
# Usage: server/scripts/autoupdate-image-smoke.sh <autoupdate image>
#  1. the source equals the image's: nothing is rebuilt;
#  2. a server + frontend change is committed: both are rebuilt and installed;
#  3. restart with an unchanged source: the recorded build is reused;
#  4. a broken server change: the update fails and the last good build stays.
set -euo pipefail
image=$1
name=cf-au-smoke-$$
volume=cf-au-smoke-$$
port=$((19000 + $$ % 1000))
repo=$(mktemp -d "${TMPDIR:-/tmp}/cf-au-repo.XXXXXX")
fail=0
step() { printf '\n== %s\n' "$*"; }
ok() { printf 'PASS %s\n' "$*"; }
bad() { printf 'FAIL %s\n' "$*"; fail=1; }
cleanup() {
	docker rm -f "$name" >/dev/null 2>&1 || true
	docker volume rm "$volume" >/dev/null 2>&1 || true
	rm -rf "$repo"
}
trap cleanup EXIT
commit() { git -C "$repo" -c user.name=smoke -c user.email=smoke@example.com commit -qam "$1"; git -C "$repo" rev-parse HEAD; }
healthy() {
	for _ in $(seq "$1"); do
		curl -sf "http://127.0.0.1:$port/health" >/dev/null 2>&1 && return 0
		sleep 2
	done
	return 1
}
version() { curl -s "http://127.0.0.1:$port/api/version" | grep -o '"version": *"[^"]*"' | cut -d'"' -f4; }
updates() { docker logs "$name" 2>&1 | grep -E "auto-update" | cut -c1-200; }

step "source repository (this tree)"
rsync -a --exclude .git --exclude node_modules --exclude dist --exclude .venv ./ "$repo"/
git -C "$repo" init -q -b main
git -C "$repo" add -A
base=$(git -C "$repo" -c user.name=smoke -c user.email=smoke@example.com commit -qm base && git -C "$repo" rev-parse HEAD)
echo "base ${base:0:7}"

step "1. start: source equals the image"
docker volume create "$volume" >/dev/null
docker run -d --name "$name" -p "127.0.0.1:$port:80" -v "$volume:/data" -v "$repo:/src:ro" \
	-e CONFIGFLOW_UPDATE_REPO=/src -e CONFIGFLOW_UPDATE_BRANCH=main \
	-e GIT_CONFIG_COUNT=1 -e GIT_CONFIG_KEY_0=safe.directory -e GIT_CONFIG_VALUE_0='*' "$image" >/dev/null
healthy 60 && ok "healthy" || bad "not healthy"
updates
image_version=$(version)
echo "version $image_version"
updates | grep -q "running ${base:0:7}" && ok "recorded ${base:0:7}" || bad "base not recorded"
updates | grep -q "building" && bad "rebuilt an unchanged component" || ok "nothing rebuilt"

step "2. server + frontend change"
echo "// autoupdate smoke $$" >>"$repo/server/internal/version/version.go"
sed -i "s|<head>|<head><meta name=\"cf-smoke\" content=\"$$\">|" "$repo/frontend/index.html"
v2=$(commit "server and frontend change")
docker restart -t 10 "$name" >/dev/null
healthy 600 && ok "healthy after rebuild" || bad "not healthy after rebuild"
updates | tail -8
got=$(version)
echo "version $got"
[ "$got" = "main-${v2:0:7}" ] && ok "server rebuilt from ${v2:0:7}" || bad "version $got"
updates | grep -q "building frontend" && ok "frontend rebuilt" || bad "frontend not rebuilt"
docker exec "$name" grep -q "cf-smoke" /usr/share/configflow/html/index.html && ok "new frontend installed" || bad "frontend not installed"
curl -s "http://127.0.0.1:$port/" | grep -q "cf-smoke" && ok "new frontend served" || bad "old frontend served"
docker exec "$name" sh -c 'ls /usr/share/configflow/html/assets/*.js.gz >/dev/null' && ok "new frontend precompressed" || bad "no .gz assets"
updates | grep -q "building Agent" && bad "Agents rebuilt without a change" || ok "Agents reused from the image"

step "3. restart with the same source"
docker restart -t 10 "$name" >/dev/null
healthy 60 && ok "healthy" || bad "not healthy"
updates | tail -2
updates | tail -2 | grep -q "already built" && ok "recorded build reused" || bad "not reused"
[ "$(version)" = "main-${v2:0:7}" ] && ok "still ${v2:0:7}" || bad "version $(version)"

step "4. broken server change"
echo "this is not go" >"$repo/server/internal/version/broken.go"
git -C "$repo" add -A
v3=$(commit "broken")
docker restart -t 10 "$name" >/dev/null
healthy 600 && ok "healthy after failed update" || bad "not healthy after failed update"
updates | tail -3
updates | grep -q "update failed, keeping ${v2:0:7}" && ok "failure reported" || bad "failure not reported"
[ "$(version)" = "main-${v2:0:7}" ] && ok "kept ${v2:0:7}" || bad "version $(version)"

step "docker stop"
docker stop -t 10 "$name" >/dev/null
code=$(docker inspect -f '{{.State.ExitCode}}' "$name")
[ "$code" = 0 ] && ok "clean exit" || bad "exit code $code"

echo
[ $fail = 0 ] && echo "ALL PASS" || echo "SOME FAILED"
exit $fail
