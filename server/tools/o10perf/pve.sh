#!/usr/bin/env bash
# O10 formal measurement on the PVE lab CT (default VMID 240, 10.97.0.140 on cfgt).
# Runs on a test server (192.168.0.32 / .64); PVE is reached with `ssh pve`.
#
#   rt -- server/tools/o10perf/pve.sh run --servers go       # Go server of this tree
#   rt -- server/tools/o10perf/pve.sh run --servers python   # Python backend of this tree
#   rt -- server/tools/o10perf/pve.sh run --src DIR --servers go   # another source tree on the server
#   rt -- server/tools/o10perf/pve.sh create | python | check     # one-time setup / isolation check
#
# run: builds the Go server here exactly like docker/Dockerfile (CGO_ENABLED=0, -trimpath, -s -w) plus the
# pprof overlay, pushes binary + backend + o10perf + a private snapshot copy into the CT with `pct push`,
# runs run.sh there (loopback-only namespace), copies the private output back to
# ~/cf-prod-snapshot/runs/pve-<stamp> on this server, deletes snapshot and output inside the CT, stops it.
# All other arguments go to perf.py (default: 30 min idle + all route loads). Only report.json is printed.
#
# The CT never has network: net0 is link_down=1 and table inet o10ct drops its IP on the host.
set -euo pipefail
umask 077
ct=${O10_CT:-240}
ip=${O10_CT_IP:-10.97.0.140}
tools=$(cd "$(dirname "$0")" && pwd)
snapshot=$(realpath "${O10_SNAPSHOT:-$HOME/cf-prod-snapshot/20261116/data}")
opt=/opt/o10 # same path on the test server and inside the CT (Python is not relocatable)
pve() { ssh -o ServerAliveInterval=30 -o BatchMode=yes pve "$@"; }
pve_bash() { pve 'bash -s' -- "$@"; } # PVE root shell is zsh: feed bash scripts on stdin

isolate() { # idempotent: link down + host drop rules for the CT's IP only
  pve_bash "$ct" "$ip" <<'EOF'
set -euo pipefail
ct=$1 ip=$2
net=$(pct config "$ct" | sed -n 's/^net0: //p')
[[ $net == *link_down=1* ]] || pct set "$ct" -net0 "$net,link_down=1"
if ! nft list table inet o10ct >/dev/null 2>&1; then
  nft -f - <<NFT
table inet o10ct {
  chain o10fwd {
    type filter hook forward priority -10; policy accept;
    ip saddr $ip drop
    ip daddr $ip drop
  }
  chain o10in {
    type filter hook input priority -10; policy accept;
    ip saddr $ip drop
  }
}
NFT
fi
EOF
}

case ${1:-} in
create)
  pve_bash "$ct" "$ip" <<'EOF'
set -euo pipefail
ct=$1 ip=$2
if pct status "$ct" >/dev/null 2>&1; then echo "CT $ct exists"; exit 0; fi
pct create "$ct" local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst --hostname cf-o10-perf \
  --unprivileged 1 --features nesting=1 --cores 2 --memory 2048 --swap 512 --cpuunits 50 --onboot 0 \
  --rootfs local-lvm:8 --net0 "name=eth0,bridge=cfgt,ip=$ip/24,gw=10.97.0.1,type=veth,link_down=1" \
  --description "O10 performance measurement CT (Go vs Python). No network: link_down=1 + nft table o10ct."
EOF
  isolate
  ;;
python)
  # Python 3.11 + the exact package set of ~/cf-test/.venv, built here and copied in (the CT has no network).
  sudo install -d -o "$USER" -m 755 "$opt"
  rm -rf "$opt/python" "$opt/venv"
  version=$("$HOME/cf-test/.venv/bin/python" -c 'import platform; print(platform.python_version())')
  UV_PYTHON_INSTALL_DIR=$opt/python uv python install "$version"
  uv venv --python "$(echo "$opt"/python/cpython-"$version"-linux-x86_64-gnu/bin/python3)" "$opt/venv"
  uv pip freeze --python "$HOME/cf-test/.venv/bin/python" >"$opt/requirements.txt"
  uv pip install --python "$opt/venv/bin/python" -r "$opt/requirements.txt"
  tar -C / -czf "$HOME/o10-python.tgz" "${opt#/}"
  pve 'mkdir -p /root/cf-o10 && chmod 700 /root/cf-o10'
  scp -q "$HOME/o10-python.tgz" pve:/root/cf-o10/
  pve_bash "$ct" <<'EOF'
set -euo pipefail
ct=$1
pct status "$ct" | grep -q running || pct start "$ct"
pct push "$ct" /root/cf-o10/o10-python.tgz /root/o10-python.tgz
pct exec "$ct" -- bash -c 'rm -rf /opt/o10 && tar --no-same-owner -C / -xzf /root/o10-python.tgz && rm /root/o10-python.tgz &&
  /opt/o10/venv/bin/python -c "import flask, waitress, sys; print(sys.version)"'
rm /root/cf-o10/o10-python.tgz
EOF
  rm "$HOME/o10-python.tgz"
  ;;
check)
  isolate
  pve_bash "$ct" "$ip" <<'EOF'
set -euo pipefail
ct=$1 ip=$2
pct status "$ct" | grep -q running || pct start "$ct"
echo "host veth: $(ip -br link show "veth${ct}i0" | awk '{print $2}')"
nft list table inet o10ct | grep -c drop | sed 's/^/host drop rules: /'
pct exec "$ct" -- bash -c 'ip -br link; ip route; timeout 3 bash -c "</dev/tcp/10.97.0.1/22" && echo "EGRESS OPEN" || echo "no egress to gateway";
  unshare --user --map-root-user --net --mount --pid --fork --mount-proc bash -c "ip link set lo up; ip -o link | wc -l" | sed "s/^/namespace links: /"'
EOF
  ;;
run)
  shift
  src=$(cd "$tools/../../.." && pwd)
  if [[ ${1:-} == --src ]]; then src=$(cd "$2" && pwd); shift 2; fi
  stamp=$(date +%Y%m%d-%H%M%S)
  private=$HOME/cf-prod-snapshot/runs
  mkdir -p "$private" && chmod 700 "$HOME/cf-prod-snapshot" "$private"
  stage=$(mktemp -d)
  trap 'rm -rf "$stage"; pve "rm -f /root/cf-o10/o10-payload.tgz /root/cf-o10/o10-snapshot.tgz /root/cf-o10/o10-out.tgz" || true' EXIT
  # Same build as docker/Dockerfile, plus the pprof listener overlay.
  printf '{"Replace":{"%s":"%s"}}' "$src/server/cmd/configflow/o10_pprof_overlay.go" "$tools/pprof_overlay.go.txt" >"$stage/overlay.json"
  (cd "$src/server" && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -overlay "$stage/overlay.json" \
    -o "$stage/o10/configflow" ./cmd/configflow)
  mkdir -p "$stage/o10/src/server/tools"
  cp -a "$src/backend" "$stage/o10/src/backend"
  find "$stage/o10/src/backend" -name __pycache__ -prune -exec rm -rf {} +
  cp -a "$tools" "$stage/o10/src/server/tools/o10perf"
  {
    echo 'export HOME=/root O10_GO_BIN=/root/o10/configflow CONFIGFLOW_DIFF_PYTHON=/opt/o10/venv/bin/python'
    echo 'export O10_SNAPSHOT=/root/cf-prod-snapshot/data O10_OUT=/root/cf-prod-snapshot/runs/o10'
    printf 'exec bash /root/o10/src/server/tools/o10perf/run.sh'; printf ' %q' "$@"; echo
  } >"$stage/o10/go.sh"
  tar -C "$stage" -czf "$stage/o10-payload.tgz" o10
  tar -C "$snapshot" -czf "$stage/o10-snapshot.tgz" .
  echo "o10: source $src, go $(cd "$src/server" && go version | awk '{print $3}'), CT $ct, stamp $stamp"
  isolate
  pve 'mkdir -p /root/cf-o10 && chmod 700 /root/cf-o10'
  scp -q "$stage/o10-payload.tgz" "$stage/o10-snapshot.tgz" pve:/root/cf-o10/
  pve_bash "$ct" <<'EOF'
set -euo pipefail
ct=$1
pct status "$ct" | grep -q running || { pct start "$ct"; sleep 60; } # let the CT finish booting
for _ in $(seq 120); do # wait while the host is busy (production guests come first)
  load=$(cut -d' ' -f1 /proc/loadavg)
  awk -v l="$load" 'BEGIN { exit !(l <= 4) }' && break
  echo "o10: host load $load > 4, waiting"; sleep 60
done
pct push "$ct" /root/cf-o10/o10-payload.tgz /root/o10-payload.tgz
pct push "$ct" /root/cf-o10/o10-snapshot.tgz /root/o10-snapshot.tgz
rm /root/cf-o10/o10-payload.tgz /root/cf-o10/o10-snapshot.tgz
pct exec "$ct" -- bash -c 'set -e; umask 077; rm -rf /root/o10 /root/cf-prod-snapshot
  mkdir -p /root/cf-prod-snapshot/data; tar --no-same-owner -C /root -xzf /root/o10-payload.tgz
  tar --no-same-owner -C /root/cf-prod-snapshot/data -xzf /root/o10-snapshot.tgz; rm /root/o10-payload.tgz /root/o10-snapshot.tgz
  setsid bash /root/o10/go.sh >/root/o10/run.out 2>&1 </dev/null & echo $! >/root/o10/run.pid'
EOF
  # Wait for the detached run (survives ssh drops); one blocking loop.
  while :; do
    state=$(pve "pct exec $ct -- bash -c 'kill -0 \$(cat /root/o10/run.pid) 2>/dev/null && echo running || echo done'" || echo ssh-error)
    [[ $state == done ]] && break
    sleep 60
  done
  pve_bash "$ct" <<'EOF'
set -euo pipefail
ct=$1
pct exec "$ct" -- bash -c 'mkdir -p /root/cf-prod-snapshot/runs/o10 && cp /root/o10/run.out /root/cf-prod-snapshot/runs/o10/run.out
  tar -C /root/cf-prod-snapshot/runs -czf /root/o10-out.tgz o10; rm -rf /root/cf-prod-snapshot /root/o10/configflow'
pct pull "$ct" /root/o10-out.tgz /root/cf-o10/o10-out.tgz
pct exec "$ct" -- rm -f /root/o10-out.tgz
pct shutdown "$ct" --timeout 60
EOF
  scp -q pve:/root/cf-o10/o10-out.tgz "$stage/"
  mkdir -p "$private/pve-$stamp"
  tar -C "$private/pve-$stamp" --strip-components=1 -xzf "$stage/o10-out.tgz"
  echo "o10: private output $private/pve-$stamp"
  if [[ -f $private/pve-$stamp/report.json ]]; then cat "$private/pve-$stamp/report.json"
  else tail -n 20 "$private/pve-$stamp/run.out"; exit 1; fi
  ;;
*)
  sed -n '2,17p' "$0"; exit 2
  ;;
esac
