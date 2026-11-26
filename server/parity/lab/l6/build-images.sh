#!/usr/bin/env bash
# L6: build the Go panel image and the s6 Agent image from this tree, fetch the
# last Python-era images and the lab registry image, and save everything as
# gzip tarballs with SHA256 for transfer into the lab (see l6-runbook.md).
#
# Runs on a test server only (never on the Mac), from the repository root of an
# `rt` copy, which has no .git: pass the commit explicitly.
#
#   L6_COMMIT=<12-hex commit> server/parity/lab/l6/build-images.sh
#   rt -e L6_COMMIT=$(git rev-parse --short=12 HEAD) -- server/parity/lab/l6/build-images.sh
#
# Environment:
#   L6_COMMIT       commit the tree was taken from (required; goes into tags and /api/version)
#   L6_VERSION      panel release version to inject (default 2.0.0-rc.l6)
#   L6_REGISTRY     lab registry the images will be pushed to (default 10.97.0.151:5000)
#   L6_OUT          output directory (default ~/.cache/cf-l6/images/<commit>)
#   L6_PY_PANEL     last Python panel image (default thsrite/config-flow:latest)
#   L6_PY_AGENT     Python-era Docker Agent image (default thsrite/config-flow-agent:latest)
#   L6_SKIP_PULL=1  do not pull the Python-era and registry images again
#
# Output: <out>/*.tar.gz, <out>/SHA256SUMS (tarballs) and <out>/manifest.tsv
# (lab reference, local image ID, source reference/digest).
set -euo pipefail

commit=${L6_COMMIT:?set L6_COMMIT to the commit this tree was taken from}
case $commit in *[!0-9a-f]*|'') echo "L6_COMMIT must be lowercase hex" >&2; exit 2;; esac
commit=${commit:0:12}
short=${commit:0:9}
version=${L6_VERSION:-2.0.0-rc.l6}
registry=${L6_REGISTRY:-10.97.0.151:5000}
out=${L6_OUT:-$HOME/.cache/cf-l6/images/$commit}
py_panel=${L6_PY_PANEL:-thsrite/config-flow:latest}
py_agent=${L6_PY_AGENT:-thsrite/config-flow-agent:latest}
docker=(sudo docker)

root=$(cd "$(dirname "$0")/../../../.." && pwd)
cd "$root"
[ -f docker/Dockerfile ] && [ -f docker/Dockerfile.agent ] || { echo "run from a ConfigFlow tree" >&2; exit 2; }

agent_version=$(sed -n 's/^const Version = "\(.*\)"$/\1/p' backend/agents/go-agent/upgrade/migration.go)
[ -n "$agent_version" ] || { echo "cannot read the Agent version" >&2; exit 2; }

# Lab references. The panel is built with AGENT_IMAGE pointing at the lab
# registry, so the Docker commands it generates pull the paired s6 image from
# inside the lab (same mechanism as the release pairing, different registry).
agent_ref="$registry/config-flow-agent:$agent_version-$short"
panel_ref="$registry/config-flow:$version-$short"
py_panel_ref="$registry/config-flow:python-final"
py_agent_ref="$registry/config-flow-agent:python-final"
registry_src=registry:3
registry_ref="cf-l6/registry:3"

mkdir -p "$out"
echo "tree $root commit $commit agent $agent_version"
echo "out $out"

echo "== build Agent image (s6) → $agent_ref"
"${docker[@]}" buildx build --load --platform linux/amd64 \
  -f docker/Dockerfile.agent -t "$agent_ref" .

echo "== build panel image (runtime) → $panel_ref"
"${docker[@]}" buildx build --load --platform linux/amd64 --target runtime \
  --build-arg VERSION="$version" --build-arg GIT_COMMIT="$short" \
  --build-arg AGENT_IMAGE="$agent_ref" \
  -f docker/Dockerfile -t "$panel_ref" .

if [ "${L6_SKIP_PULL:-}" != 1 ]; then
  echo "== pull Python-era images and the registry image"
  for ref in "$py_panel" "$py_agent" "$registry_src"; do
    "${docker[@]}" pull --platform linux/amd64 "$ref"
  done
fi
"${docker[@]}" tag "$py_panel" "$py_panel_ref"
"${docker[@]}" tag "$py_agent" "$py_agent_ref"
"${docker[@]}" tag "$registry_src" "$registry_ref"

echo "== smoke: versions inside the images"
got=$("${docker[@]}" run --rm --entrypoint /usr/local/bin/configflow-agent "$agent_ref" -version 2>&1 | head -1)
echo "agent: $got"
case $got in *"$agent_version"*) ;; *) echo "Agent image reports '$got', want $agent_version" >&2; exit 1;; esac
"${docker[@]}" run --rm --entrypoint sh "$agent_ref" -c '! command -v python3 >/dev/null || head -c 2 "$(command -v python3)" | grep -q "#!"' \
  || { echo "Agent image has a real python3" >&2; exit 1; }
"${docker[@]}" run --rm --entrypoint sh "$panel_ref" -c '! command -v python3 && ! command -v supervisord' >/dev/null \
  || { echo "panel image contains Python or supervisord" >&2; exit 1; }

echo "== save"
: > "$out/manifest.tsv.tmp"
save() { # save <file stem> <lab ref> <source ref>
  local file=$out/$1.tar.gz id digest
  "${docker[@]}" save "$2" | gzip -1 > "$file.tmp"
  mv "$file.tmp" "$file"
  id=$("${docker[@]}" image inspect -f '{{.Id}}' "$2")
  digest=$("${docker[@]}" image inspect -f '{{join .RepoDigests ","}}' "$3" 2>/dev/null || true)
  printf '%s\t%s\t%s\t%s\t%s\n' "$1" "$2" "$id" "$3" "${digest:--}" >> "$out/manifest.tsv.tmp"
}
save agent-s6 "$agent_ref" "$agent_ref"
save panel-go "$panel_ref" "$panel_ref"
save panel-python "$py_panel_ref" "$py_panel"
save agent-python "$py_agent_ref" "$py_agent"
save registry "$registry_ref" "$registry_src"
{
  printf 'stem\tlab_ref\timage_id\tsource\tsource_digest\n'
  cat "$out/manifest.tsv.tmp"
} > "$out/manifest.tsv"
rm -f "$out/manifest.tsv.tmp"
(cd "$out" && sha256sum ./*.tar.gz | sed 's# \./# #' > SHA256SUMS)
printf 'commit\t%s\nagent_version\t%s\npanel_version\t%s+%s\n' "$commit" "$agent_version" "$version" "$short" > "$out/build.txt"
cat "$out/manifest.tsv" "$out/SHA256SUMS" "$out/build.txt"
