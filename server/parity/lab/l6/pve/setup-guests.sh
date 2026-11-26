#!/bin/bash
# setup-guests.sh chr [resume]        VM 256: first login, address, REST over TLS (self-signed), user cf
#                                     (resume: the console already sits at the password-change prompt)
# setup-guests.sh dockervm <id>...    VM 258/259: Docker CE (mirror), trust the lab registry, pre-pull Agent images
# setup-guests.sh cts                 CTs 252-255, 257: GitHub shim (core downloads from the lab mirror)
# setup-guests.sh client              CT 257: dig / curl probe tools
# setup-guests.sh panel               CT 250: Docker + Compose, pre-pull both panel images
# setup-guests.sh check               one line per guest: reachable, DNS through the lab resolver
# Runs on the PVE lab host after setup-infra.sh base/images. Credentials are lab-only.
set -euo pipefail
cd "$(dirname "$0")"
. ./lab.conf
CHR_ADMIN_PASS=L6Chr-2611
CHR_CF_PASS=L6Cf-2611
ssh_vm() { # ssh_vm <id> <command>: cloud VMs, lab key
  ssh -i "$SSH_KEY" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -o ConnectTimeout=5 \
    "root@$(guest_ip "$1")" "$2"
}

chr() {
  local id=256 ip
  require_registered $id
  ip=$(guest_ip $id)
  # First boot: login admin with an empty password, answer "n" to the license
  # question, leave the pager with q if it shows, skip the forced password
  # change with Ctrl-C and set the password with a command instead (the prompt
  # echoes stars and is hard to drive reliably).
  cat > "$WORK/chr-init.txt" <<EOF
admin

#wait 3
n
#wait 2
q!
#wait 2
^C
#wait 2
/user set admin password=$CHR_ADMIN_PASS
/system identity set name=cf-l6-chr
/ip address add address=$ip/24 interface=ether1
/ip route add gateway=$GW
/ip dns set servers=$INFRA_IP
/system ntp client set enabled=no
/certificate add name=l6-ca common-name=l6-ca key-usage=key-cert-sign,crl-sign
/certificate sign l6-ca
#wait 15
/certificate add name=l6-srv common-name=$ip subject-alt-name=IP:$ip
/certificate sign l6-srv ca=l6-ca
#wait 15
/ip service set www-ssl certificate=l6-srv disabled=no
/ip service set www disabled=yes
/user add name=cf group=full password=$CHR_CF_PASS
/ip address print
/ip service print where name~"www"
EOF
  # "resume" skips the login lines when the console is already past them
  if [ "${1:-}" = resume ]; then sed -n '/^\^C$/,$p' "$WORK/chr-init.txt" > "$WORK/chr-run.txt"; else cp "$WORK/chr-init.txt" "$WORK/chr-run.txt"; fi
  python3 chrcon.py $id "$WORK/chr-run.txt" | tail -25
  curl -sk -m 10 -u "cf:$CHR_CF_PASS" "https://$ip/rest/system/resource" | head -c 300; echo
}

dockervm() {
  local id ip
  for id in "$@"; do
    require_registered "$id"
    ip=$(guest_ip "$id")
    for _ in $(seq 60); do ssh_vm "$id" true 2>/dev/null && break; sleep 5; done
    ssh_vm "$id" "set -e
      host=\$(echo $MIRROR | sed 's#https://##')
      grep -q \" \$host\" /etc/hosts || echo '$(getent ahostsv4 "${MIRROR#https://}" | awk 'NR==1{print $1}') '\$host >> /etc/hosts
      sed -i 's#http://deb.debian.org#$MIRROR#; s#https://deb.debian.org#$MIRROR#' /etc/apt/sources.list.d/*.sources /etc/apt/sources.list 2>/dev/null || true
      apt-get -o Acquire::ForceIPv4=true update -qq
      DEBIAN_FRONTEND=noninteractive apt-get -o Acquire::ForceIPv4=true install -y -qq ca-certificates curl gnupg dnsutils >/dev/null
      install -m 0755 -d /etc/apt/keyrings
      curl -fsSL $MIRROR/docker-ce/linux/debian/gpg -o /etc/apt/keyrings/docker.asc
      echo \"deb [arch=amd64 signed-by=/etc/apt/keyrings/docker.asc] $MIRROR/docker-ce/linux/debian bookworm stable\" > /etc/apt/sources.list.d/docker.list
      apt-get -o Acquire::ForceIPv4=true update -qq
      DEBIAN_FRONTEND=noninteractive apt-get -o Acquire::ForceIPv4=true install -y -qq docker-ce docker-ce-cli containerd.io docker-compose-plugin >/dev/null
      mkdir -p /etc/docker
      echo '{\"insecure-registries\":[\"$REGISTRY\"]}' > /etc/docker/daemon.json
      systemctl restart docker
      docker version --format '{{.Server.Version}}'; docker compose version --short
      for ref in \$(curl -fsS http://$REGISTRY/v2/config-flow-agent/tags/list | sed 's/.*\\[//; s/\\].*//; s/\"//g; s/,/ /g'); do
        docker pull -q $REGISTRY/config-flow-agent:\$ref
      done
      docker tag $REGISTRY/config-flow-agent:python-final thsrite/config-flow-agent:latest
      docker images --format '{{.Repository}}:{{.Tag}} {{.ID}}' | grep config-flow-agent"
    echo "docker VM $id ready ($ip)"
  done
}

cts() {
  bash setup-infra.sh shim 252 253 254 255 257
}

client() {
  require_registered 257
  gexec 257 bash -c "DEBIAN_FRONTEND=noninteractive apt-get -o Acquire::ForceIPv4=true install -y -qq dnsutils curl procps >/dev/null && dig -v 2>&1 | head -1"
}

panel() {
  bash setup-infra.sh docker-client 250
  gexec 250 sh -c "docker pull -q $REGISTRY/config-flow:python-final && for t in \$(wget -q -O- http://$REGISTRY/v2/config-flow/tags/list | sed 's/.*\\[//; s/\\].*//; s/\"//g; s/,/ /g'); do docker pull -q $REGISTRY/config-flow:\$t; done; docker images | grep config-flow"
}

check() {
  local id ip kind
  for id in $(guest_ids); do
    ip=$(guest_ip "$id"); kind=$(guest_field "$id" 2)
    if [ "$kind" = lxc ]; then
      pct status "$id" 2>/dev/null | grep -q running || { echo "$id not running"; continue; }
      printf '%s %-13s ' "$id" "$(guest_field "$id" 3)"
      gexec "$id" sh -c "ping -c1 -W2 $INFRA_IP >/dev/null && printf 'infra-ok ' || printf 'infra-FAIL '; (command -v dig >/dev/null && dig +time=3 +tries=1 +short example.com A | head -1 | grep -q . && echo dns-ok) || echo dns-skip-or-fail"
    else
      qm status "$id" 2>/dev/null | grep -q running || { echo "$id not running"; continue; }
      printf '%s %-13s ' "$id" "$(guest_field "$id" 3)"
      ping -c1 -W2 "$ip" >/dev/null && echo reachable || echo unreachable
    fi
  done
}

case ${1:-} in
  chr) shift; chr "$@";;
  dockervm) shift; dockervm "$@";;
  cts) cts;;
  client) client;;
  panel) panel;;
  check) check;;
  *) sed -n '2,8p' "$0"; exit 2;;
esac
