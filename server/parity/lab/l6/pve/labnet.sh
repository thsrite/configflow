#!/bin/bash
# labnet.sh up       add the lab DNS redirect to the lab-owned table inet cfgt:
#                    plain DNS (:53) from cfgt to any non-lab address is answered by
#                    the lab resolver on the infra CT; DoT (:853) from cfgt is dropped.
# labnet.sh down     remove exactly the rules added by "up" (comment l6-labdns)
# labnet.sh egress-lock <id>...   drop traffic from these L6 guests to the outside (comment l6-egress)
# labnet.sh egress-open [<id>...] remove the egress locks (all, or of these guests)
# labnet.sh check    leak and isolation checks (prints PASS/FAIL lines, exit 1 on any FAIL)
# labnet.sh status   show the table
#
# Runtime rules only (lost on a PVE reboot): re-run "up" after a reboot. The
# private-LAN drop rules of inet cfgt are never changed here.
set -euo pipefail
cd "$(dirname "$0")"
. ./lab.conf
T="inet cfgt"
TAG=l6-labdns

base_rules_present() {
  nft list chain $T forward | grep -q 'ip daddr 192.168.0.0/16 drop' &&
    nft list chain $T forward | grep -q 'ip daddr 10.0.0.0/8 drop' &&
    nft list chain $T forward | grep -q 'ip daddr 172.16.0.0/12 drop'
}

delete_tagged() { # delete_tagged <chain> <tag>
  local h
  nft list chain $T "$1" >/dev/null 2>&1 || return 0
  for h in $(nft -a list chain $T "$1" | grep "comment \"$2\"" | sed 's/.*# handle //'); do
    nft delete rule $T "$1" handle "$h"
  done
}

up() {
  base_rules_present || { echo "inet cfgt private-LAN drop rules missing; refusing" >&2; exit 1; }
  down >/dev/null
  nft list chain $T prerouting >/dev/null 2>&1 || nft add chain $T prerouting '{ type nat hook prerouting priority dstnat; }'
  nft add rule $T prerouting iifname cfgt ip daddr != $NET.0/24 udp dport 53 dnat ip to $INFRA_IP comment \"$TAG\"
  nft add rule $T prerouting iifname cfgt ip daddr != $NET.0/24 tcp dport 53 dnat ip to $INFRA_IP comment \"$TAG\"
  # redirected queries come back out of cfgt: accept them and their replies, and
  # masquerade so the resolver answers through the host (keeps conntrack symmetric)
  nft insert rule $T forward iifname cfgt oifname cfgt ip daddr $INFRA_IP meta l4proto '{ tcp, udp }' th dport 53 accept comment \"$TAG\"
  nft insert rule $T forward iifname cfgt oifname cfgt ip saddr $INFRA_IP ct status dnat ct state established accept comment \"$TAG\"
  nft insert rule $T forward iifname cfgt oifname vmbr0 meta l4proto '{ tcp, udp }' th dport 853 drop comment \"$TAG\"
  nft insert rule $T forward iifname cfgt oifname vmbr0 meta l4proto '{ tcp, udp }' th dport 53 drop comment \"$TAG\"
  nft add rule $T postrouting iifname cfgt oifname cfgt ip daddr $INFRA_IP meta l4proto '{ tcp, udp }' th dport 53 masquerade comment \"$TAG\"
  echo "lab DNS redirect up (resolver $INFRA_IP)"
}

down() {
  delete_tagged prerouting $TAG
  delete_tagged forward $TAG
  delete_tagged postrouting $TAG
  echo "lab DNS redirect removed"
}

egress_lock() {
  local id
  for id in "$@"; do
    require_registered "$id"
    nft insert rule $T forward iifname cfgt oifname vmbr0 ip saddr "$(guest_ip "$id")" drop comment \"l6-egress-$id\"
  done
}

egress_open() {
  local id
  if [ $# -eq 0 ]; then set -- $(guest_ids); fi
  for id in "$@"; do delete_tagged forward "l6-egress-$id"; done
}

fail=0
pass() { echo "PASS $*"; }
bad() { echo "FAIL $*"; fail=1; }

check() {
  base_rules_present && pass "private-LAN drop rules present" || bad "private-LAN drop rules missing"
  nft list chain $T prerouting 2>/dev/null | grep -q "dnat ip to $INFRA_IP" && pass "DNS redirect present" || bad "DNS redirect missing"
  nft list chain $T forward | grep "$TAG" | grep -q 'dport 853 drop' && pass "DoT drop present" || bad "DoT drop missing"
  # Probe from a running L6 CT: plain DNS to a public resolver must be answered
  # by the lab resolver (its query log grows), and private LANs must be unreachable.
  local probe
  for probe in 257 252 253 254 255 250; do
    pct status "$probe" 2>/dev/null | grep -q running && break
    probe=
  done
  [ -n "$probe" ] || { echo "SKIP guest probes: no running L6 CT besides infra"; [ $fail = 0 ]; return; }
  local before after
  before=$(pct exec 251 -- sh -c 'wc -l < /var/log/labdns-queries.log 2>/dev/null || echo 0')
  gexec "$probe" sh -c 'command -v dig >/dev/null && dig +time=3 +tries=1 +short @223.5.5.5 l6-leak-check.example.com A >/dev/null 2>&1; true'
  sleep 1
  after=$(pct exec 251 -- sh -c 'wc -l < /var/log/labdns-queries.log 2>/dev/null || echo 0')
  [ "$after" -gt "$before" ] && pass "plain DNS from CT $probe to a public resolver reached the lab resolver" \
    || bad "plain DNS from CT $probe did not reach the lab resolver (before $before after $after)"
  gexec "$probe" sh -c 'timeout 4 nc -z -w 3 192.168.255.254 443 2>/dev/null || timeout 4 curl -s -m 3 -o /dev/null http://192.168.255.254/' \
    && bad "CT $probe reached the home LAN" || pass "home LAN unreachable from CT $probe"
  [ $fail = 0 ]
}

case ${1:-} in
  up) up;;
  down) down;;
  egress-lock) shift; egress_lock "$@";;
  egress-open) shift; egress_open "$@";;
  check) check;;
  status) nft list table $T;;
  *) sed -n '2,13p' "$0"; exit 2;;
esac
