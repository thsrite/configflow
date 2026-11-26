#!/usr/bin/env bash
# Return-routing lifecycle against a real nft in a disposable container
# (lab L5-D1). Default image debian:12 = nftables 1.0.6, which omits table and
# chain comments from "nft -j list". Run on a test server, never on a host
# whose network matters:  rt -- backend/agents/go-agent/integration/return_routing_nft_lab.sh
set -euo pipefail
cd "$(dirname "$0")/.."
image=${NFT_LAB_IMAGE:-debian:12}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
CGO_ENABLED=0 go test -c -tags nftlab -o "$work/routes.test" ./routes
docker run --rm --cap-add NET_ADMIN -e CONFIGFLOW_NFT_LAB=1 -v "$work:/lab:ro" "$image" bash -euc '
	export DEBIAN_FRONTEND=noninteractive
	apt-get update -qq >/dev/null
	apt-get install -y -qq nftables iproute2 iputils-ping >/dev/null
	/lab/routes.test -test.run "^TestNFTLab" -test.v -test.count=1
'
