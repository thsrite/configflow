#!/bin/sh
# Offline update for an existing native AIO Agent. Never invoke core lifecycle commands.
set -eu
umask 077

PACKAGE_DIR=$(CDPATH= cd -P "$(dirname "$0")" && pwd)
[ -f "$PACKAGE_DIR/cli-output.sh" ] || { printf '%s\n' '更新包缺少 cli-output.sh，请完整解压。' >&2; exit 1; }
. "$PACKAGE_DIR/cli-output.sh"
cf_verbose_options "$@"
SERVICE=""
for argument do
    case "$argument" in
        --verbose|-v) ;;
        --help|-h) printf '%s\n' '用法：sh update-agent.sh [mihomo|oxidns] [--verbose]'; exit 0 ;;
        mihomo|oxidns) [ -z "$SERVICE" ] || { printf '%s\n' '每次只能更新一个 Agent。' >&2; exit 1; }; SERVICE=$argument ;;
        *) printf '%s\n' "未知参数：$argument" >&2; exit 1 ;;
    esac
done
SERVICE=${SERVICE:-mihomo}
cf_init "更新 $SERVICE Agent" || exit $?
fail() { cf_error "$*"; exit 1; }
[ "$(id -u)" -eq 0 ] || fail "请在 LXC 内使用 root 运行此脚本。"
for command_name in systemctl flock sha256sum sed awk uname cp mv mktemp chmod cat dirname sleep date cmp curl head mkdir rm; do
    command -v "$command_name" >/dev/null 2>&1 || fail "缺少命令：${command_name}；本脚本不会自动安装依赖。"
done

AGENT_DIR="/opt/configflow-agent/$SERVICE"
CONFIG_FILE="$AGENT_DIR/config.json"
AGENT_BINARY="$AGENT_DIR/configflow-agent"
AGENT_UNIT="configflow-agent-$SERVICE.service"
cf_step '校验更新包'
[ -f "$PACKAGE_DIR/VERSION" ] || fail "更新包缺少 VERSION。"
EXPECTED_VERSION=$(cat "$PACKAGE_DIR/VERSION")
printf '%s\n' "$EXPECTED_VERSION" | awk '/^[0-9]+\.[0-9]+\.[0-9]+-go$/ { valid=1 } END { exit !valid }' || fail "更新包 VERSION 格式无效。"
case "$(uname -m)" in
    x86_64|amd64) ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    armv7l|armhf) ARCH=armv7 ;;
    *) fail "当前 CPU 架构不受此更新包支持。" ;;
esac
CANDIDATE="$PACKAGE_DIR/configflow-agent-linux-$ARCH"
[ -f "$PACKAGE_DIR/VERSION" ] && [ "$(cat "$PACKAGE_DIR/VERSION")" = "$EXPECTED_VERSION" ] || fail "更新包 VERSION 无效。"
[ -f "$CANDIDATE" ] && [ ! -L "$CANDIDATE" ] || fail "更新包缺少对应架构的普通二进制文件。"
[ -f "$PACKAGE_DIR/SHA256SUMS" ] || fail "更新包缺少 SHA256SUMS。"
EXPECTED_SHA=$(awk -v name="configflow-agent-linux-$ARCH" '$2 == name || $2 == "*" name { print $1; count++ } END { if (count != 1) exit 1 }' "$PACKAGE_DIR/SHA256SUMS") || fail "SHA256SUMS 中的目标文件记录不唯一。"
case "$EXPECTED_SHA" in *[!0-9a-fA-F]*|'') fail "目标 SHA256 格式无效。" ;; esac
[ "${#EXPECTED_SHA}" -eq 64 ] || fail "目标 SHA256 长度无效。"
ACTUAL_SHA=$(sha256sum "$CANDIDATE" | awk '{print $1}')
[ "$ACTUAL_SHA" = "$EXPECTED_SHA" ] || fail "更新二进制 SHA256 校验失败。"
[ -x "$CANDIDATE" ] || fail "更新二进制没有执行权限；请完整解压更新包。"
[ "$("$CANDIDATE" -version)" = "$EXPECTED_VERSION" ] || fail "更新二进制版本不匹配。"
cf_success "安装包校验通过 · $EXPECTED_VERSION · $ARCH"

cf_step '核对现有服务'
for required_path in /opt/configflow-agent "$AGENT_DIR" "$CONFIG_FILE" "$AGENT_BINARY"; do
    [ ! -L "$required_path" ] || fail "拒绝符号链接部署路径：$required_path"
done
[ -d "$AGENT_DIR" ] && [ -f "$CONFIG_FILE" ] && [ -f "$AGENT_BINARY" ] && [ -x "$AGENT_BINARY" ] || fail "未找到原生同机独立 Agent；不支持旧版共享目录安装。"
json_string() {
    sed -n 's/.*"'"$1"'"[[:space:]]*:[[:space:]]*"\([^"\\]*\)".*/\1/p' "$CONFIG_FILE" | head -n 1
}
[ "$(json_string service_type)" = "$SERVICE" ] || fail "Agent 服务类型与目录不一致。"
[ "$(json_string service_manager)" = systemd ] || fail "本脚本只支持原生 systemd 部署。"
CONFIG_AGENT_UNIT=$(json_string agent_service_unit)
[ "${CONFIG_AGENT_UNIT%.service}.service" = "$AGENT_UNIT" ] || fail "Agent 服务标识不是标准同机布局。"
awk '/"native_aio"[[:space:]]*:[[:space:]]*true([[:space:],}]|$)/ { found=1 } END { exit !found }' "$CONFIG_FILE" || fail "配置不是原生同机 Agent。"
CORE_UNIT=$(json_string service_unit)
CORE_UNIT=${CORE_UNIT%.service}
case "$CORE_UNIT" in ''|*[!A-Za-z0-9_.@-]*|configflow-*) fail "核心服务名无效或与 Agent 管理服务冲突。" ;; esac
CORE_UNIT="$CORE_UNIT.service"
CORE_CONFIG=$(json_string config_path)
case "$CORE_CONFIG" in /*) ;; *) fail "核心配置路径必须是绝对路径。" ;; esac
case "$CORE_CONFIG" in *[!A-Za-z0-9_./@+-]*|*/../*|*/..|/opt/configflow-agent/*) fail "核心配置路径不符合此离线更新器支持的布局。" ;; esac
[ -f "$CORE_CONFIG" ] || fail "核心配置不存在；停止更新。"
AGENT_HOST=$(json_string agent_host)
case "$AGENT_HOST" in ''|0.0.0.0|::|'[::]') AGENT_HOST=127.0.0.1 ;; esac
case "$AGENT_HOST" in *[!A-Za-z0-9.:%_-]*) fail "Agent 监听地址格式不受此更新器支持。" ;; esac
case "$AGENT_HOST" in *:*) HEALTH_HOST="[$AGENT_HOST]" ;; *) HEALTH_HOST=$AGENT_HOST ;; esac
AGENT_PORT=$(sed -n 's/.*"agent_port"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p' "$CONFIG_FILE" | head -n 1)
case "$AGENT_PORT" in ''|*[!0-9]*) fail "Agent 端口无效。" ;; esac
[ "$AGENT_PORT" -ge 1 ] && [ "$AGENT_PORT" -le 65535 ] || fail "Agent 端口超出范围。"
HEALTH_URL="http://$HEALTH_HOST:$AGENT_PORT/health"

unit_property() { systemctl show "$1" "--property=$2" --value; }
[ "$(unit_property "$AGENT_UNIT" LoadState)" = loaded ] || fail "Agent systemd 服务未加载。"
[ "$(unit_property "$CORE_UNIT" LoadState)" = loaded ] || fail "核心 systemd 服务未加载。"
EXEC_START=$(unit_property "$AGENT_UNIT" ExecStart)
case "$EXEC_START" in
    *"{ path=$AGENT_BINARY ; argv[]=$AGENT_BINARY -config $CONFIG_FILE ;"*) ;;
    *) fail "Agent ExecStart 不是预期的独立二进制和配置路径。" ;;
esac
[ "$(printf '%s\n' "$EXEC_START" | awk '{print gsub(/\{ path=/, "&")}')" = 1 ] || fail "Agent 存在多个 ExecStart；请人工检查。"
for property in ExecStop ExecStopPost ExecStartPost ExecReload ExecCondition PartOf ConsistsOf BindsTo BoundBy PropagatesStopTo StopPropagatedFrom; do
    [ -z "$(unit_property "$AGENT_UNIT" "$property")" ] || fail "Agent 存在自定义 ${property}，无法保证与核心独立；本次未停止服务。"
done
EXEC_PRE=$(unit_property "$AGENT_UNIT" ExecStartPre)
if [ -n "$EXEC_PRE" ]; then
    case "$EXEC_PRE" in
        *"{ path=/bin/sh ; argv[]=/bin/sh $AGENT_DIR/enable-ipv4-forwarding.sh ;"*) ;;
        *) fail "Agent 存在未知 ExecStartPre；本次未停止服务。" ;;
    esac
    [ "$(printf '%s\n' "$EXEC_PRE" | awk '{print gsub(/\{ path=/, "&")}')" = 1 ] || fail "Agent 存在多个 ExecStartPre；本次未停止服务。"
fi
for property in Requires RequiredBy Wants Conflicts Upholds UpheldBy; do
    relation=$(unit_property "$AGENT_UNIT" "$property")
    case " $relation " in
        *" $CORE_UNIT "*|*" configflow-recover-"*) fail "Agent 的 $property 含核心联动或旧恢复服务；本次未停止服务。" ;;
    esac
done

STAGE_DIR=""
STOPPED=0
REPLACED=0
COMPLETED=0
BACKUP=""
cleanup() {
    result=$?
    trap - 0
    set +e
    if [ "$result" -ne 0 ] && [ "$STOPPED" -eq 1 ] && [ "$COMPLETED" -eq 0 ]; then
        restore_ok=1
        if [ "$REPLACED" -eq 1 ]; then
            systemctl stop "$AGENT_UNIT"
            if cp -p "$BACKUP" "$STAGE_DIR/restored" && mv -f "$STAGE_DIR/restored" "$AGENT_BINARY"; then
                cf_warn "已恢复旧 Agent 程序：$BACKUP"
            else
                restore_ok=0
                cf_error "旧 Agent 恢复失败，请保留备份人工处理：$BACKUP"
            fi
        fi
        if [ "$restore_ok" -eq 1 ]; then
            systemctl start "$AGENT_UNIT" || cf_error '旧 Agent 未能启动，请检查其日志。'
        fi
        cf_warn '更新未完成；恢复流程只操作 Agent 服务。'
    fi
    if [ -n "$STAGE_DIR" ]; then rm -rf "$STAGE_DIR"; fi
    if [ "$result" -ne 0 ]; then cf_result "Agent 更新未完成"; fi
    cf_finish "$result"
    exit "$result"
}
trap cleanup 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

UPDATE_DIR="$AGENT_DIR/.configflow-updates"
DEPLOY_DIR="$(dirname "$CORE_CONFIG")/.configflow-deployments"
for lock_dir in "$UPDATE_DIR" "$DEPLOY_DIR"; do
    [ ! -L "$lock_dir" ] || fail "锁目录不能是符号链接：$lock_dir"
    mkdir -p "$lock_dir"
done
for lock_file in "$UPDATE_DIR/update.lock" "$DEPLOY_DIR/operation.lock"; do
    [ ! -L "$lock_file" ] || fail "锁文件不能是符号链接：$lock_file"
    [ ! -e "$lock_file" ] || [ -f "$lock_file" ] || fail "锁文件不是普通文件：$lock_file"
done
exec 8>"$UPDATE_DIR/update.lock"
flock -n 8 || fail "另一个 Agent 更新正在运行；本次未停止服务。"
exec 9>"$DEPLOY_DIR/operation.lock"
flock -n 9 || fail "核心正在发布、重启或执行其他操作；本次未停止服务。"
cf_run '核对发布与升级记录' "$CANDIDATE" -config "$CONFIG_FILE" -require-clean-deployments

core_signature() {
    systemctl is-active --quiet "$CORE_UNIT" || return 1
    core_pid=$(unit_property "$CORE_UNIT" MainPID) || return 1
    case "$core_pid" in ''|0|*[!0-9]*) return 1 ;; esac
    core_started=$(awk -v expected="$core_pid" '$1 != expected {exit 1} {sub(/^.*\) /, ""); if (NF < 20 || $20 !~ /^[0-9]+$/) exit 1; print $20}' "/proc/$core_pid/stat") || return 1
    [ -n "$core_started" ] || return 1
    printf '%s:%s\n' "$core_pid" "$core_started"
}
CORE_BEFORE=$(core_signature) || fail "核心未运行或无法读取其进程身份；本次未停止 Agent。"
cf_detail "核心更新前进程签名：$CORE_BEFORE"
assert_core_unchanged() {
    CORE_AFTER=$(core_signature) || fail "核心状态发生变化或进程身份不可读取；停止更新。"
    [ "$CORE_AFTER" = "$CORE_BEFORE" ] || fail "核心进程发生变化（更新前 ${CORE_BEFORE}，当前 ${CORE_AFTER}）；停止更新，不重启核心。"
}
AGENT_GROUP=$(unit_property "$AGENT_UNIT" ControlGroup)
CORE_GROUP=$(unit_property "$CORE_UNIT" ControlGroup)
if [ -n "$AGENT_GROUP" ]; then
    case "$CORE_GROUP" in "$AGENT_GROUP"|"$AGENT_GROUP"/*) fail "核心与 Agent 共用控制组，拒绝停止 Agent。" ;; esac
fi

cf_step '备份并更新 Agent'
STAGE_DIR=$(mktemp -d "$AGENT_DIR/.offline-update.XXXXXX")
BACKUP="$AGENT_DIR/configflow-agent.backup-$(date +%Y%m%d-%H%M%S)-$$"
cp -p "$AGENT_BINARY" "$BACKUP"
cmp -s "$AGENT_BINARY" "$BACKUP" || fail "旧 Agent 备份校验失败。"
cf_success '旧 Agent 已备份'
cf_detail "备份路径：$BACKUP"
cp "$CANDIDATE" "$STAGE_DIR/new"
chmod 755 "$STAGE_DIR/new"
[ "$(sha256sum "$STAGE_DIR/new" | awk '{print $1}')" = "$EXPECTED_SHA" ] || fail "暂存二进制校验失败。"
assert_core_unchanged
STOPPED=1
cf_run '停止 Agent' systemctl stop "$AGENT_UNIT"
assert_core_unchanged
cf_run '复核发布记录' "$CANDIDATE" -config "$CONFIG_FILE" -require-clean-deployments
REPLACED=1
mv -f "$STAGE_DIR/new" "$AGENT_BINARY"
assert_core_unchanged
cf_run '启动 Agent' systemctl start "$AGENT_UNIT"

cf_step '确认 Agent 健康状态'
ready=0
attempt=0
while [ "$attempt" -lt 15 ]; do
    assert_core_unchanged
    if systemctl is-active --quiet "$AGENT_UNIT"; then
        agent_pid=$(unit_property "$AGENT_UNIT" MainPID)
        case "$agent_pid" in ''|0|*[!0-9]*) ;; *)
            if [ "$AGENT_BINARY" -ef "/proc/$agent_pid/exe" ]; then
                if health_body=$(curl --noproxy '*' --connect-timeout 1 --max-time 2 -fsS "$HEALTH_URL") &&
                    printf '%s\n' "$health_body" | awk '/"status"[[:space:]]*:[[:space:]]*"ok"/ { found=1 } END { exit !found }'; then
                    if [ "$(unit_property "$AGENT_UNIT" MainPID)" = "$agent_pid" ] &&
                        [ "$AGENT_BINARY" -ef "/proc/$agent_pid/exe" ]; then
                        ready=1
                        break
                    fi
                fi
            fi
            ;;
        esac
    fi
    attempt=$((attempt + 1))
    sleep 1
done
[ "$ready" -eq 1 ] || fail "新 Agent 在 15 次检查内未能以目标二进制运行并通过健康检查。"
[ "$("$AGENT_BINARY" -version)" = "$EXPECTED_VERSION" ] || fail "已安装 Agent 版本不匹配。"
assert_core_unchanged
COMPLETED=1
cf_success '新 Agent 健康检查通过'
cf_step '只读环境检查'
if "$AGENT_BINARY" -config "$CONFIG_FILE" -check-environment; then
    :
else
    cf_warn 'Agent 更新已完成；环境检查发现的问题需要单独处理。'
fi
assert_core_unchanged
cf_step '更新结果'
cf_success "Agent 已更新 · $EXPECTED_VERSION · 端口 $AGENT_PORT"
cf_success "核心持续运行 · PID ${CORE_BEFORE%%:*} · 启动时间保持不变"
cf_detail "核心进程签名：更新前 ${CORE_BEFORE}，更新后 ${CORE_AFTER}"
cf_success "备份：$BACKUP"
cf_detail "程序：${AGENT_BINARY}；配置：$CONFIG_FILE"
cf_result "Agent 更新完成"
