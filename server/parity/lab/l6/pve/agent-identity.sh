#!/bin/bash
# agent-identity.sh <ct id> <agent id>
#   Make the Agent on an L6 CT run as a snapshot Agent record (same ID and token, lab panel URL),
#   so repointed records keep their history, HA pairing and DNS-follow bindings.
#   The token is read from the panel CT's data copy (read-only); the CT's Agent config file
#   (/opt/configflow-agent/config-<type>.json, written by the installer) is patched and the
#   Agent restarted. Run after installing the Agent with the panel's normal install command;
#   afterwards delete the extra record that install registered (Agent page) — or, before the
#   install, run with --seed to write the identity first and check whether the installer keeps it.
# agent-identity.sh --seed <ct id> <agent id> <service type>
# Runs on the PVE lab host.
set -euo pipefail
cd "$(dirname "$0")"
. ./lab.conf
seed=
[ "${1:-}" = --seed ] && { seed=1; shift; }
ct=${1:?ct id}; agent=${2:?agent id}; stype=${3:-}
require_registered "$ct"
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
pct pull 250 /srv/cf/data/configflow.sqlite3 "$tmp/db"
token=$(python3 - "$tmp/db" "$agent" <<'P'
import json, sqlite3, sys
c = sqlite3.connect(f"file:{sys.argv[1]}?mode=ro", uri=True)
agents = json.loads(c.execute("SELECT value FROM fields WHERE scope='system' AND field='agents'").fetchone()[0])
for a in agents:
    if a.get("id") == sys.argv[2]:
        print(a.get("token") or ""); break
P
)
[ -n "$token" ] || { echo "agent $agent not in the panel data (or has no token)" >&2; exit 1; }
if [ -n "$seed" ]; then
  [ -n "$stype" ] || { echo "--seed needs the service type (mihomo|oxidns|mosdns)" >&2; exit 2; }
  files="/opt/configflow-agent/config-$stype.json"
  gexec "$ct" sh -c "mkdir -p /opt/configflow-agent; [ -f $files ] || echo '{}' > $files"
else
  files=$(gexec "$ct" sh -c 'ls /opt/configflow-agent/config-*.json 2>/dev/null')
  [ -n "$files" ] || { echo "no Agent config on CT $ct; install first or use --seed" >&2; exit 1; }
fi
for f in $files; do
  pct pull "$ct" "$f" "$tmp/conf.json"
  python3 - "$tmp/conf.json" "$agent" "$token" "http://$PANEL_IP" <<'P'
import json, sys
path, agent, token, url = sys.argv[1:5]
d = json.load(open(path))
d["agent_id"], d["token"], d["server_url"] = agent, token, url
json.dump(d, open(path, "w"), indent=2, ensure_ascii=False)
print(f"{path.rsplit('/', 1)[-1]}: agent_id set, token set, server_url {url}")
P
  pct push "$ct" "$tmp/conf.json" "$f" --perms 600
done
[ -n "$seed" ] && exit 0
gexec "$ct" sh -c 'if command -v systemctl >/dev/null; then systemctl restart configflow-agent && systemctl is-active configflow-agent;
  else rc-service configflow-agent restart >/dev/null && rc-service configflow-agent status; fi'
