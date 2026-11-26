#!/usr/bin/env bash
# DNS HA keepalived lifecycle under real Alpine/OpenRC in a disposable
# container on a private Docker network (lab L3-D1). Run on a test server:
#   rt -- backend/agents/go-agent/integration/oxidns_ha_openrc_lab.sh
set -euo pipefail
cd "$(dirname "$0")/.."
image=${HA_LAB_IMAGE:-alpine:3.24}
network=cf-ha-lab-$$
work=$(mktemp -d)
trap 'rm -rf "$work"; docker network rm "$network" >/dev/null 2>&1 || true' EXIT
CGO_ENABLED=0 go test -c -tags halab -o "$work/routes.test" ./routes
docker network create --subnet 198.51.100.0/24 "$network" >/dev/null
docker run --rm --network "$network" --ip 198.51.100.10 --cap-add NET_ADMIN --cap-add NET_RAW \
	-e CONFIGFLOW_HA_LAB=1 -e HA_LAB_SELF=198.51.100.10 -e HA_LAB_PEER=198.51.100.11 -e HA_LAB_VIP=198.51.100.53 \
	-v "$work:/lab:ro" "$image" sh -euc '
	apk add -q keepalived openrc iproute2 >/dev/null
	mkdir -p /run/openrc && echo default > /run/openrc/softlevel
	cp /lab/routes.test /usr/local/bin/routes.test
	/usr/local/bin/routes.test -test.run "^TestHALab" -test.v -test.count=1
'
