#!/bin/bash
# panel.sh data <data.tar>      put a sanitized data-directory tarball into CT 250 (/srv/cf/data, mode 700);
#                               refuses when the copy has no .l6-sanitize.json or a panel is running
# panel.sh python               start the last Python image on that data (container cf-panel, host network, :80)
# panel.sh go <tag>             switch: stop the running panel, start the Go image <tag>; prints the downtime
#                               (stop issued → /health 200) and the first-start log summary
# panel.sh restart              SIGTERM restart of the running panel (drain check)
# panel.sh kill                 kill -9 the panel process (docker kill -s KILL), then start it again
# panel.sh stop | status | logs [n]
# panel.sh backup <name>        cold copy of /srv/cf/data while stopped (F9 restore rehearsal)
# panel.sh restore <name>       stop, replace /srv/cf/data with that cold copy (the panel stays stopped)
# Runs on the PVE lab host. Login (lab-only credentials) is enabled like production.
set -euo pipefail
cd "$(dirname "$0")"
. ./lab.conf
CT=250
NAME=cf-panel
DATA=/srv/cf/data
ENVFILE=$WORK/panel.env
[ -f "$ENVFILE" ] || cat > "$ENVFILE" <<EOF
TZ=Asia/Shanghai
ADMIN_USERNAME=admin
ADMIN_PASSWORD=L6-panel-2611
JWT_SECRET_KEY=l6-lab-jwt-secret-0123456789abcdef0123456789abcdef
EOF
c() { gexec $CT "$@"; }
now() { date +%s.%N; }
secs() { awk -v a="$1" -v b="$2" 'BEGIN { printf "%.1f", b - a }'; }
health() { c sh -c "wget -q -T 2 -O /dev/null http://127.0.0.1/health" >/dev/null 2>&1; }
running() { c sh -c "docker ps -q --filter name=^$NAME\$" | grep -q .; }

start() { # start <image>
  c sh -c "docker pull -q '$1' >/dev/null && docker rm -f $NAME >/dev/null 2>&1; docker run -d --name $NAME --network host \
    --restart unless-stopped --stop-timeout 30 --env-file /etc/cf-panel.env -v $DATA:/data '$1' >/dev/null"
}

wait_health() {
  local i
  for i in $(seq 300); do health && return 0; sleep 0.5; done
  return 1
}

case ${1:-} in
  data)
    tar=${2:?data tarball}
    running && { echo "a panel is running; stop it first" >&2; exit 1; }
    tar -tf "$tar" | grep -q '\.l6-sanitize\.json$' || { echo "no .l6-sanitize.json in $tar: run sanitize-snapshot.py apply first" >&2; exit 1; }
    c sh -c "rm -rf $DATA && mkdir -p $DATA && chmod 700 /srv/cf $DATA"
    pct push $CT "$tar" /srv/cf/data.tar
    c sh -c "tar -C $DATA --strip-components=1 -xf /srv/cf/data.tar && rm /srv/cf/data.tar && ls $DATA | head -3 && cat $DATA/.l6-sanitize.json | head -3"
    pct push $CT "$ENVFILE" /etc/cf-panel.env --perms 600
    ;;
  python)
    pct push $CT "$ENVFILE" /etc/cf-panel.env --perms 600
    start "$REGISTRY/config-flow:python-final"
    wait_health && echo "python panel healthy" || { echo "python panel not healthy" >&2; exit 1; }
    c sh -c "wget -q -O- http://127.0.0.1/api/version; echo"
    ;;
  go)
    tag=${2:?Go image tag, e.g. 2.0.0-rc.l6-fe126f1ca}
    t0=$(now)
    c sh -c "docker stop -t 30 $NAME >/dev/null 2>&1 || true"
    t1=$(now)
    start "$REGISTRY/config-flow:$tag"
    t2=$(now)
    wait_health || { echo "Go panel not healthy after 150 s" >&2; exit 1; }
    t3=$(now)
    echo "stop $(secs $t0 $t1)s, start issued $(secs $t1 $t2)s, healthy after $(secs $t2 $t3)s; downtime $(secs $t0 $t3)s"
    c sh -c "wget -q -O- http://127.0.0.1/api/version; echo"
    c sh -c "docker logs $NAME 2>&1 | grep -cE 'level=(WARN|ERROR)|WARNING|ERROR' || true" | sed 's/^/warn+error lines: /'
    ;;
  restart)
    t0=$(now); c docker restart -t 30 $NAME >/dev/null; wait_health; t1=$(now)
    echo "SIGTERM restart: healthy after $(secs $t0 $t1)s"
    c sh -c "docker inspect -f '{{.State.ExitCode}} {{.RestartCount}}' $NAME"
    ;;
  kill)
    c docker kill -s KILL $NAME >/dev/null; c docker start $NAME >/dev/null; wait_health && echo "back after kill -9"
    ;;
  stop) c sh -c "docker stop -t 30 $NAME" ;;
  status) c sh -c "docker ps -a --filter name=^$NAME\$ --format '{{.Image}} {{.Status}}'; wget -q -O- http://127.0.0.1/api/version; echo" ;;
  logs) c sh -c "docker logs --tail ${2:-100} $NAME 2>&1" ;;
  backup)
    running && { echo "stop the panel first (cold backup)" >&2; exit 1; }
    c sh -c "mkdir -p /srv/cf/backups && rm -rf /srv/cf/backups/${2:?name} && cp -a $DATA /srv/cf/backups/$2 && du -sh /srv/cf/backups/$2"
    ;;
  restore)
    c sh -c "docker stop -t 30 $NAME >/dev/null 2>&1 || true; test -d /srv/cf/backups/${2:?name} && rm -rf $DATA && cp -a /srv/cf/backups/$2 $DATA && echo restored $2"
    ;;
  *) sed -n '2,13p' "$0"; exit 2;;
esac
