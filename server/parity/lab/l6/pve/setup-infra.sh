#!/bin/bash
# setup-infra.sh base              CT 251: lab resolver, static HTTP + GitHub mirror, WebDAV, Docker + registry
# setup-infra.sh resolver          CT 251: (re)write and restart the lab resolver only
# setup-infra.sh images <commit>   load the build's images into the lab registry (10.97.0.151:5000)
# setup-infra.sh shim <ct id>...   install the GitHub release shim (curl/wget) in L6 CTs
# setup-infra.sh docker-client <ct id>...   Alpine CTs: Docker + Compose, trust the lab registry
# Runs on the PVE lab host as root. Needs xfer/ from push-to-lab.sh.
set -euo pipefail
cd "$(dirname "$0")"
. ./lab.conf
X=$WORK/xfer
ID=251

openrc_service() { # openrc_service <ct> <name> <command> <args> [user]
  local ct=$1 name=$2 cmd=$3 args=$4
  gexec "$ct" sh -c "cat > /etc/init.d/$name <<EOF
#!/sbin/openrc-run
description=\"L6 lab $name\"
command=$cmd
command_args=\"$args\"
command_background=true
pidfile=/run/$name.pid
output_log=/var/log/$name.log
error_log=/var/log/$name.log
depend() { need net; }
EOF
chmod 755 /etc/init.d/$name; rc-update add $name default >/dev/null; rc-service $name restart >/dev/null"
}

resolver() {
  require_registered $ID
  # resolver: mosdns with DoH-by-IP upstreams (plain DNS would be hijacked by the home router to production DNS)
  local zip=$X/assets/gh/IrineSistiana/mosdns/releases/download/v5.3.3/mosdns-linux-amd64.zip
  rm -rf "$X/mosdns" && mkdir -p "$X/mosdns" && (cd "$X/mosdns" && unzip -q -o "$zip")
  pct push $ID "$X/mosdns/mosdns" /usr/local/bin/mosdns --perms 755
  gexec $ID sh -c "mkdir -p /etc/labdns && cat > /etc/labdns/config.yaml <<'Y'
log: {level: info, file: /var/log/labdns-queries.log}
plugins:
  - tag: fwd
    type: forward
    args:
      concurrent: 2
      upstreams:
        - addr: https://223.5.5.5/dns-query
        - addr: https://120.53.53.53/dns-query
  - tag: main
    type: sequence
    args:
      - exec: debug_print lab-query
      - exec: \$fwd
  - {tag: udp, type: udp_server, args: {entry: main, listen: \"$INFRA_IP:53\"}}
  - {tag: tcp, type: tcp_server, args: {entry: main, listen: \"$INFRA_IP:53\"}}
Y"
  openrc_service $ID labdns /usr/local/bin/mosdns "start -d /etc/labdns -c /etc/labdns/config.yaml"
  sleep 2
  gexec $ID sh -c "dig +time=3 +tries=1 +short @$INFRA_IP example.com A | head -1"
}

base() {
  require_registered $ID
  pct status $ID | grep -q running || pct start $ID
  pin_mirror $ID
  apk_add $ID bash curl ca-certificates bind-tools tzdata busybox-extras unzip rclone docker iproute2 nftables
  resolver
  # static HTTP: GitHub release mirror (gh/), synthetic subscription, probe target
  gexec $ID sh -c "mkdir -p /srv/www/gh /srv/www/sub /srv/webdav /srv/registry"
  tar -C "$X/assets" -cf - gh | pct exec $ID -- tar -C /srv/www -xf -
  pct push $ID "$WORK/lab-sub.yaml" /srv/www/sub/lab.yaml
  gexec $ID sh -c "echo l6-ok > /srv/www/ok.txt"
  openrc_service $ID labhttp /usr/sbin/httpd "-f -p $INFRA_IP:8090 -h /srv/www"
  # WebDAV for the backup tests (credentials only exist in the lab)
  openrc_service $ID labdav /usr/bin/rclone "serve webdav /srv/webdav --addr $INFRA_IP:8091 --user l6dav --pass L6-dav-2611"
  # Docker + registry
  gexec $ID sh -c "mkdir -p /etc/docker && echo '{\"insecure-registries\":[\"$REGISTRY\"]}' > /etc/docker/daemon.json
    rc-update add docker default >/dev/null; rc-service docker restart >/dev/null; sleep 5; docker info --format '{{.ServerVersion}}'"
  gexec $ID sh -c "echo nameserver $INFRA_IP > /etc/resolv.conf"
  sleep 2
  gexec $ID sh -c "dig +time=3 +tries=1 +short @$INFRA_IP example.com A | head -2; wget -q -O- http://$INFRA_IP:8090/ok.txt"
}

images() { # images <commit>
  local commit=${1:?commit} dir=$X/images/$1 stem ref
  (cd "$dir" && sha256sum -c --quiet SHA256SUMS)
  pct push $ID "$dir/registry.tar.gz" /root/registry.tar.gz
  gexec $ID sh -c "gunzip -c /root/registry.tar.gz | docker load -q && rm /root/registry.tar.gz
    docker rm -f registry >/dev/null 2>&1 || true
    docker run -d --restart always --name registry -p $INFRA_IP:5000:5000 -v /srv/registry:/var/lib/registry cf-l6/registry:3 >/dev/null
    sleep 2; wget -q -O- http://$REGISTRY/v2/ && echo registry up"
  tail -n +2 "$dir/manifest.tsv" | while IFS=$'\t' read -r stem ref _; do
    [ "$stem" = registry ] && continue
    echo "== push $ref"
    pct push $ID "$dir/$stem.tar.gz" "/root/$stem.tar.gz"
    gexec $ID sh -c "gunzip -c /root/$stem.tar.gz | docker load -q && rm /root/$stem.tar.gz && docker push -q '$ref'"
  done
  gexec $ID sh -c "wget -q -O- http://$REGISTRY/v2/_catalog; echo; for r in config-flow config-flow-agent; do wget -q -O- http://$REGISTRY/v2/\$r/tags/list; echo; done"
}

shim() { # GitHub release URLs → lab mirror (installers keep checking their pinned SHA256)
  local ct
  for ct in "$@"; do
    gexec "$ct" sh -c "cat > /usr/local/bin/l6-ghshim <<'S'
#!/bin/sh
# L6 lab shim: map https://github.com/<o>/<r>/releases/download/... to the lab mirror.
real=\$(basename \"\$0\")
args=\"\"
for a in \"\$@\"; do
  case \"\$a\" in https://github.com/*/releases/download/*) a=\"$HTTP/gh/\${a#https://github.com/}\";; esac
  args=\"\$args '\$(printf %s \"\$a\" | sed \"s/'/'\\\\\\\\''/g\")'\"
done
for d in /usr/bin /bin; do [ -x \"\$d/\$real\" ] && eval exec \"\$d/\$real\" \$args; done
echo \"l6-ghshim: no real \$real\" >&2; exit 127
S
chmod 755 /usr/local/bin/l6-ghshim; ln -sf l6-ghshim /usr/local/bin/curl; ln -sf l6-ghshim /usr/local/bin/wget"
    echo "shim installed in $ct"
  done
}

docker_client() {
  local ct
  for ct in "$@"; do
    pin_mirror "$ct"
    apk_add "$ct" docker docker-cli-compose
    gexec "$ct" sh -c "mkdir -p /etc/docker &&
      echo '{\"insecure-registries\":[\"$REGISTRY\"]}' > /etc/docker/daemon.json &&
      rc-update add docker default >/dev/null && rc-service docker restart >/dev/null; sleep 5;
      docker info --format '{{.ServerVersion}}' && docker compose version --short"
  done
}

case ${1:-} in
  base) base;;
  resolver) resolver;;
  images) shift; images "$@";;
  shim) shift; shim "$@";;
  docker-client) shift; docker_client "$@";;
  *) sed -n '2,6p' "$0"; exit 2;;
esac
