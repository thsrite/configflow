#!/bin/bash
# mkguests.sh register            write $OWNER with every planned guest (before anything is created)
# mkguests.sh create <id>...      create (and start) registered guests that do not exist yet
# mkguests.sh status              owner file + PVE state of every L6 guest
# Runs on the PVE lab host as root, from $WORK (pve/ copied there).
set -euo pipefail
cd "$(dirname "$0")"
. ./lab.conf

register() {
  [ -f "$OWNER" ] && { echo "$OWNER exists; not overwriting"; return; }
  mkdir -p "$WORK"
  printf '%s\n' "$GUESTS" | python3 -c '
import json, sys, datetime
net, owner, vipn, vipd, infra = sys.argv[1:6]
guests = {}
for line in sys.stdin:
    p = line.strip().split("|")
    if len(p) < 8: continue
    gid, kind, name, os_, mem, cores, disk, role = p
    guests[gid] = {"type": kind, "name": name, "ip": f"{net}.{int(gid)-100}", "os": os_,
                   "memory_mb": int(mem), "role": role, "status": "planned"}
doc = {"batch": "L6 (Go rewrite: O10 24 h soak + O14 switch rehearsal), preparation",
       "owner": "L6 prep agent, branch go-l6-prep",
       "created": datetime.date.today().isoformat(),
       "bridge": "cfgt",
       "work_dir": "/root/cf-gwip-lab/l6",
       "vips": {"dns_ha_native": vipn, "dns_ha_docker": vipd},
       "lab_infra": {"resolver_registry_http_webdav": infra},
       "status": "preparing",
       "guests": guests,
       "lab_dns": "inet cfgt: prerouting dnat plain :53 from cfgt to non-lab addresses -> lab resolver; :853 from cfgt dropped (runtime; pve/labnet.sh up|down)"}
json.dump(doc, open(owner, "w"), indent=1, ensure_ascii=False)
print("wrote", owner)
' "$NET" "$OWNER" "$VIP_NATIVE" "$VIP_DOCKER" "$INFRA_IP"
}

set_status() { # set_status <id> <text>
  python3 -c 'import json,sys; f,i,s=sys.argv[1:]; d=json.load(open(f)); d["guests"][i]["status"]=s; json.dump(d,open(f,"w"),indent=1,ensure_ascii=False)' "$OWNER" "$1" "$2"
}

exists() { pct status "$1" >/dev/null 2>&1 || qm status "$1" >/dev/null 2>&1; }

create_ct() { # create_ct <id>
  local id=$1 name os mem cores disk tmpl extra=()
  name=$(guest_field "$id" 3); os=$(guest_field "$id" 4); mem=$(guest_field "$id" 5)
  cores=$(guest_field "$id" 6); disk=$(guest_field "$id" 7)
  case $os in alpine) tmpl=$ALPINE_TMPL;; debian) tmpl=$DEBIAN_TMPL;; *) echo "bad os $os" >&2; exit 1;; esac
  case $id in 252|253) extra=(--dev0 /dev/net/tun);; esac   # Mihomo TUN on the side gateways
  pct create "$id" "$tmpl" --hostname "$name" --memory "$mem" --swap 256 --cores "$cores" \
    --rootfs "local-lvm:$disk" \
    --net0 "name=eth0,bridge=$BRIDGE,ip=$(guest_ip "$id")/24,gw=$GW" --nameserver "$INFRA_IP" --searchdomain lab.invalid \
    --unprivileged 1 --features nesting=1,keyctl=1 --onboot 0 "${extra[@]}" \
    --ssh-public-keys "$SSH_KEY.pub" --description "L6 lab guest (go-l6-prep): $(guest_field "$id" 8)"
  pct start "$id"
  sleep 4
  pin_mirror "$id"   # package mirror reachable before the lab resolver exists (CT 251 is built first)
  if [ "$os" = alpine ]; then
    gexec "$id" sh -c "sed -i 's#https\\?://dl-cdn.alpinelinux.org#$ALPINE_REPO#' /etc/apk/repositories
      echo nameserver $INFRA_IP > /etc/resolv.conf
"
    apk_add "$id" bash curl ca-certificates nftables iproute2 bind-tools tzdata openssh
    gexec "$id" sh -c "rc-update add sshd default >/dev/null 2>&1; rc-service sshd start >/dev/null 2>&1 || true"
  else
    gexec "$id" bash -c "echo nameserver $INFRA_IP > /etc/resolv.conf
      sed -i 's#http://deb.debian.org#$MIRROR#' /etc/apt/sources.list /etc/apt/sources.list.d/*.sources 2>/dev/null || true
      for i in 1 2 3; do timeout 600 apt-get -o Acquire::ForceIPv4=true -o Acquire::http::Timeout=30 update -qq &&
        DEBIAN_FRONTEND=noninteractive timeout 900 apt-get -o Acquire::ForceIPv4=true -o Acquire::http::Timeout=30 install -y -qq curl ca-certificates nftables dnsutils >/dev/null && break; done"
  fi
}

create_chr() { # create_chr <id>
  local id=$1
  [ -f "$CHR_IMG" ] || { echo "missing $CHR_IMG" >&2; exit 1; }
  qm create "$id" --name "$(guest_field "$id" 3)" --memory "$(guest_field "$id" 5)" --cores 1 --ostype l26 --onboot 0 \
    --net0 "virtio,bridge=$BRIDGE" --serial0 socket --vga serial0 --scsihw virtio-scsi-pci \
    --description "L6 lab RouterOS CHR 7.25.1 (go-l6-prep); ether1 $(guest_ip "$id") on cfgt"
  qm importdisk "$id" "$CHR_IMG" local-lvm >/dev/null
  qm set "$id" --scsi0 "local-lvm:vm-$id-disk-0" --boot order=scsi0 >/dev/null
  qm start "$id"
}

create_cloud_vm() { # create_cloud_vm <id>: Debian 12 cloud image + cloud-init (static address, lab key)
  local id=$1
  [ -f "$DEBIAN_CLOUD" ] || { echo "missing $DEBIAN_CLOUD" >&2; exit 1; }
  qm create "$id" --name "$(guest_field "$id" 3)" --memory "$(guest_field "$id" 5)" --balloon 512 \
    --cores "$(guest_field "$id" 6)" --cpu host --ostype l26 --onboot 0 --agent 0 \
    --net0 "virtio,bridge=$BRIDGE" --serial0 socket --vga serial0 --scsihw virtio-scsi-pci \
    --description "L6 lab guest (go-l6-prep): $(guest_field "$id" 8)"
  qm importdisk "$id" "$DEBIAN_CLOUD" local-lvm >/dev/null
  qm set "$id" --scsi0 "local-lvm:vm-$id-disk-0" --boot order=scsi0 --ide2 local-lvm:cloudinit >/dev/null
  qm resize "$id" scsi0 "$(guest_field "$id" 7)G" >/dev/null
  qm set "$id" --ciuser root --sshkeys "$SSH_KEY.pub" \
    --ipconfig0 "ip=$(guest_ip "$id")/24,gw=$GW" --nameserver "$INFRA_IP" --searchdomain lab.invalid >/dev/null
  qm start "$id"
}

create() {
  [ -f "$SSH_KEY" ] || ssh-keygen -q -t ed25519 -N '' -C l6-lab -f "$SSH_KEY"
  local id
  for id in "$@"; do
    require_registered "$id"
    if exists "$id"; then echo "$id exists, skipped"; continue; fi
    echo "== create $id $(guest_field "$id" 3)"
    case $(guest_field "$id" 2)/$(guest_field "$id" 4) in
      lxc/*) create_ct "$id";;
      qemu/chr) create_chr "$id";;
      qemu/debian-cloud) create_cloud_vm "$id";;
    esac
    set_status "$id" "created $(date +%F)"
  done
}

status() {
  local id st
  for id in $(guest_ids); do
    if pct status "$id" >/dev/null 2>&1; then st=$(pct status "$id"); elif qm status "$id" >/dev/null 2>&1; then st=$(qm status "$id"); else st="absent"; fi
    printf '%s\t%-14s\t%-12s\t%s\n' "$id" "$(guest_field "$id" 3)" "$(guest_ip "$id")" "$st"
  done
}

case ${1:-} in
  register) register;;
  create) shift; create "$@";;
  status) status;;
  *) sed -n '2,5p' "$0"; exit 2;;
esac
