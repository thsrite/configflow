// Literal script and README text of the Docker HA Compose bundle, first
// generated from backend/agents/docker_ha_compose.py; since Agent 1.9.2-go the
// bundle has no bootstrap.py (the agent service runs configflow-agent
// ha-bootstrap).

package installer

// composePrepareBlocks are the five raw-string blocks of _prepare, in order.
var composePrepareBlocks = [5]string{
	`
cf_bundle=$(cd -- "$(dirname -- "$0")" && pwd -P)
for cf_file in compose.yaml prepare.sh enrollment.json README.txt; do
  [ ! -L "$cf_bundle/$cf_file" ] && [ -f "$cf_bundle/$cf_file" ] || { printf '%s\n' '安装包缺少普通文件或存在软链接，请重新解压。' >&2; exit 1; }
done
# Inspect context configuration only before contacting the local daemon.
if [ -n "${DOCKER_CONTEXT:-}" ]; then
  cf_endpoint=$(docker context inspect "$DOCKER_CONTEXT" --format '{{.Endpoints.docker.Host}}')
elif [ -n "${DOCKER_HOST:-}" ]; then cf_endpoint=$DOCKER_HOST
else cf_context=$(docker context show); cf_endpoint=$(docker context inspect "$cf_context" --format '{{.Endpoints.docker.Host}}'); fi
case "$cf_endpoint" in unix:///*) ;; *) printf '%s\n' '请在实际 Docker 宿主机使用本地 Unix socket，不能准备远端 daemon。' >&2; exit 1 ;; esac
`,
	`
case "$(docker info --format '{{.OperatingSystem}} {{.KernelVersion}}')" in
  *Docker\ Desktop*|*docker-desktop*|*linuxkit*) printf '%s\n' '此部署不支持 Docker Desktop。' >&2; exit 1 ;;
esac
cf_compose=$(docker compose version --short)
if [[ "$cf_compose" =~ ^v?([0-9]+)\.([0-9]+)\.([0-9]+) ]]; then
  [ "${BASH_REMATCH[1]}" -gt 2 ] || { [ "${BASH_REMATCH[1]}" -eq 2 ] && [ "${BASH_REMATCH[2]}" -ge 20 ]; } || { printf '%s\n' '此安装包需要 Docker Compose 2.20+。' >&2; exit 1; }
else printf '%s\n' '无法识别 Docker Compose，请安装 2.20+。' >&2; exit 1; fi
cf_no_links() {
  local cf_path=$1
  while [ "$cf_path" != / ] && [ "$cf_path" != . ]; do
    [ ! -L "$cf_path" ] || { printf '%s\n' '持久路径或父目录是软链接，拒绝继续。' >&2; return 1; }
    cf_path=$(dirname -- "$cf_path")
  done
}
`,
	`
cf_known=false
if [ -e "$cf_state/setup" ] || [ -L "$cf_state/setup" ]; then
  cf_no_links "$cf_state/setup"
  [ -f "$cf_state/setup" ] && [ "$(cat "$cf_state/setup")" = "$cf_owner" ] || { printf '%s\n' '持久目录属于另一份安装，拒绝接管。' >&2; exit 1; }
  cf_known=true
else
  [ ! -e "$cf_state" ] && [ ! -L "$cf_state" ] || { printf '%s\n' '准备记录不完整，请保留现场检查，不能覆盖。' >&2; exit 1; }
  if [ -e "$cf_data" ]; then
    [ -d "$cf_data" ] || { printf '%s\n' '持久路径不是目录。' >&2; exit 1; }
    shopt -s nullglob dotglob
    cf_contents=("$cf_data"/*)
    for cf_entry in "${cf_contents[@]}"; do
      # A selected existing root-level identity file is safe to reuse, never replace.
      if [ "$cf_entry" = "$cf_identity_source" ] && [ ! -L "$cf_entry" ] && [ -f "$cf_entry" ]; then continue; fi
      [ "$cf_bundle" = "$cf_data" ] || { printf '%s\n' '首次 Compose 安装需要空数据目录；已有数据请使用迁移，不能覆盖。' >&2; exit 1; }
      case "${cf_entry##*/}" in compose.yaml|prepare.sh|enrollment.json|README.txt) ;;
        *) printf '%s\n' '数据目录中含有安装包之外的文件；已有数据请使用迁移，不能覆盖。' >&2; exit 1 ;; esac
      [ ! -L "$cf_entry" ] && [ -f "$cf_entry" ] || { printf '%s\n' '安装包文件异常，拒绝继续。' >&2; exit 1; }
    done
    shopt -u nullglob dotglob
  fi
fi
`,
	`
if [ "$cf_known" != true ]; then
  mkdir -p -- "$cf_data"
  mkdir -- "$cf_state"
  (set -o noclobber; printf '%s\n' "$cf_owner" > "$cf_state/setup")
fi
for cf_dir in agent oxidns ha; do
  cf_no_links "$cf_data/$cf_dir"
  mkdir -p -- "$cf_data/$cf_dir"
done
`,
	`
# Keep startup files in persistent storage, not in a movable download directory.
for cf_file in enrollment.json; do
  [ ! -L "$cf_state/$cf_file" ] && { [ ! -e "$cf_state/$cf_file" ] || [ -f "$cf_state/$cf_file" ]; } || { printf '%s\n' '持久安装文件异常，拒绝覆盖。' >&2; exit 1; }
  cf_tmp="$cf_state/.$cf_file.$$"
  (set -o noclobber; cat "$cf_bundle/$cf_file" > "$cf_tmp")
  chmod 600 "$cf_tmp"
  mv -f -- "$cf_tmp" "$cf_state/$cf_file"
done
chmod 700 "$cf_state"
chmod 600 "$cf_state/setup" "$cf_bundle/enrollment.json"
printf '%s\n' '准备完成；在本安装包目录执行 docker compose up -d。启动会完成首次安全登记，页面真实心跳与 HA 检测通过后再配对。'
`,
}

// composeReadme is README.txt around the project name.
var composeReadme = [3]string{
	`仅用于本机新安装，不能接管已有容器或身份。
1. 将整个安装包解压到本机目录（也可直接解压到表单填写的数据目录），保留原文件名。
2. 在该目录执行：sudo bash prepare.sh
3. 执行：docker compose up -d（Docker 权限不足时加 sudo）。
需要 Linux rootful Docker 与 Compose 2.20+。
首次登记凭据有效期 24 小时，未登记时过期需以相同设置重新生成并再次准备；已保存身份后重启不需要安装凭据或登记 API。
相同安装包可重复准备，已有本项目的容器和网络必须通过归属及挂载核对。不要用 -p 或 COMPOSE_PROJECT_NAME 改项目名。
`,
	`NAS 项目导入时请使用项目名称：`,
	`。
请勿手改设置后接管其他数据；不要删除持久数据。安装包含限定设备的登记凭据，请保护文件，不要公开分享。
Host 模式保留独立 HA companion；Macvlan 固定 IP 属于主容器本身，必须能直接访问 ConfigFlow。
启动不代表 HA 已启用；请回页面验证真实心跳、HA 环境，再配对并检查 LAN/VIP。
`,
}
