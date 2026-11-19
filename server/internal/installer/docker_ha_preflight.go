package installer

import (
	"strings"
	"unicode/utf8"

	"configflow/internal/model"
	"configflow/internal/netipx"
	"encoding/json"
	"math/big"
	"unicode"
)

// Port of backend/agents/docker_ha_preflight.py: the bounded, read-only
// Docker HA host collector and the text-only evaluation of its pasted output.

// Preflight limits and markers (MAX_OUTPUT_BYTES, MAX_FIELD_BYTES, BEGIN, END, FIELDS).
const (
	PreflightMaxOutputBytes = 96 * 1024
	PreflightMaxFieldBytes  = 16 * 1024
	PreflightBegin          = "CONFIGFLOW_DOCKER_HA_PREFLIGHT_V1_BEGIN"
	PreflightEnd            = "CONFIGFLOW_DOCKER_HA_PREFLIGHT_V1_END"
)

// PreflightFields is FIELDS.
var PreflightFields = []string{"kernel", "docker_endpoint", "docker_version", "daemon_os", "daemon_platform",
	"daemon_security", "compose", "ipv4", "udp53", "tcp53", "nesting", "machine_fingerprint"}

// CollectorScript is collector_script(). Do not use set -e: an unavailable
// daemon or command must not hide later evidence.
func CollectorScript() string { return collectorScript }

const collectorScript = `#!/bin/bash
(
set +e
set +u
export LC_ALL=C
if ! command -v base64 >/dev/null 2>&1 || ! command -v tr >/dev/null 2>&1; then
  printf '%s\n' '基础检查无法生成结构化输出：缺少 base64 或 tr。请使用具备这些工具的 Linux/NAS 终端后重新采集。'
  exit 0
fi
cf_emit() {
  local cf_key=$1 cf_value cf_rc cf_encoded
  shift
  cf_value=$("$@" 2>/dev/null)
  cf_rc=$?
  if [ "${#cf_value}" -gt 16384 ]; then cf_value=OUTPUT_TRUNCATED; cf_rc=125; fi
  cf_encoded=$(set -o pipefail; printf '%s' "$cf_value" | base64 | tr -d '\r\n')
  if [ $? -ne 0 ]; then printf '%s\n' '采集编码失败，请重新运行检查命令。'; return 1; fi
  [ -n "$cf_encoded" ] || cf_encoded='-'
  printf 'CFHA1\t%s\t%s\t%s\n' "$cf_key" "$cf_rc" "$cf_encoded"
}
cf_docker() {
  if command -v timeout >/dev/null 2>&1; then timeout 8 docker "$@"; else docker "$@"; fi
}
cf_endpoint() {
  local cf_context cf_endpoint
  if [ -n "${DOCKER_CONTEXT:-}" ]; then
    cf_endpoint=$(cf_docker context inspect "$DOCKER_CONTEXT" --format '{{.Endpoints.docker.Host}}' 2>/dev/null) || return 1
  elif [ -n "${DOCKER_HOST:-}" ]; then
    cf_endpoint=$DOCKER_HOST
  else
    cf_context=$(cf_docker context show 2>/dev/null) || return 1
    cf_endpoint=$(cf_docker context inspect "$cf_context" --format '{{.Endpoints.docker.Host}}' 2>/dev/null) || return 1
  fi
  case "$cf_endpoint" in unix:///*) printf '%s' local-unix ;; '') return 1 ;; *) printf '%s' remote ;; esac
}
cf_probe_endpoint() {
  printf '%s' "$cf_endpoint_value"
  return "$cf_endpoint_rc"
}
cf_local_docker() {
  if [ "$cf_endpoint_rc" != 0 ] || [ "$cf_endpoint_value" != local-unix ]; then
    printf '%s' LOCAL_DOCKER_NOT_CHECKED
    return 126
  fi
  cf_docker "$@"
}
cf_nesting() {
  local cf_virt cf_rc
  if [ -f /.dockerenv ] || [ -f /run/.containerenv ]; then printf '%s' container; return 0; fi
  if command -v systemd-detect-virt >/dev/null 2>&1; then
    cf_virt=$(systemd-detect-virt --container 2>/dev/null); cf_rc=$?
    if [ "$cf_rc" = 0 ]; then printf '%s' container; return 0; fi
    if [ "$cf_rc" = 1 ]; then printf '%s' none; return 0; fi
    return 1
  fi
  if [ -r /proc/1/cgroup ] && command -v grep >/dev/null 2>&1; then
    if grep -Eq '(docker|lxc|kubepods|containerd)' /proc/1/cgroup; then printf '%s' container; else printf '%s' none; fi
  else return 1; fi
}
cf_ports() {
  local cf_protocol=$1 cf_ss cf_rc cf_family cf_file cf_rows='' cf_table
  if command -v ss >/dev/null 2>&1; then
    if [ "$cf_protocol" = tcp ]; then cf_ss=$(ss -H -ltn 'sport = :53' 2>/dev/null)
    else cf_ss=$(ss -H -lun 'sport = :53' 2>/dev/null); fi
    cf_rc=$?
    if [ "$cf_rc" = 0 ]; then printf 'source=ss\n%s' "$cf_ss"; return 0; fi
  fi
  command -v awk >/dev/null 2>&1 || return 127
  for cf_family in 4 6; do
    cf_file="/proc/net/$cf_protocol"
    [ "$cf_family" = 4 ] || cf_file="${cf_file}6"
    [ -r "$cf_file" ] || return 1
    cf_table=$(awk -v family="$cf_family" -v protocol="$cf_protocol" '
      function hex(value,width) { return length(value)==width && value !~ /[^0-9A-Fa-f]/ }
      function pair(value,left,right,parts) { return split(value,parts,":")==2 && hex(parts[1],left) && hex(parts[2],right) }
      NR==1 {
        if (NF<12 || $1!="sl" || $2!="local_address" || ($3!="rem_address" && $3!="remote_address") || $4!="st" ||
            $5!="tx_queue" || $6!="rx_queue" || $7!="tr" || $8!="tm->when" || $9!="retrnsmt" ||
            $10!="uid" || $11!="timeout" || $12!="inode" ||
            (NF!=12 && !(NF==15 && $13=="ref" && $14=="pointer" && $15=="drops"))) { bad=1; exit 2 }
        header=1; next
      }
      {
        width=(family==4 ? 8 : 32)
        state=toupper($4)
        if (NF<10 || NF>24 || $1 !~ /^[0-9]+:$/ || !pair($2,width,4) || !pair($3,width,4) || !hex($4,2) ||
            !pair($5,8,8) || !pair($6,2,8) || !hex($7,8) || $8 !~ /^[0-9]+$/ || $9 !~ /^[0-9]+$/ || $10 !~ /^[0-9]+$/ ||
            (protocol=="tcp" && state !~ /^(01|02|03|04|05|06|07|08|09|0A|0B|0C)$/) ||
            (protocol=="udp" && state !~ /^(01|07)$/)) { bad=1; exit 2 }
        split($2,local,":")
        if (toupper(local[2])=="0035" && (protocol=="udp" || state=="0A")) print protocol " " family " " toupper($2) " " state
      }
      END { if (!header || bad) exit 2 }
    ' "$cf_file") || return 2
    if [ -n "$cf_table" ]; then cf_rows="${cf_rows}${cf_table}
"; fi
  done
  printf 'source=proc-net-v1\ntables=%s,%s6\n%s' "$cf_protocol" "$cf_protocol" "$cf_rows"
}
cf_fingerprint() {
  local cf_file cf_identity cf_hash
  command -v sha256sum >/dev/null 2>&1 || return 127
  for cf_file in /etc/machine-id; do
    [ ! -L "$cf_file" ] && [ -f "$cf_file" ] && [ -r "$cf_file" ] || continue
    cf_identity=$(cat "$cf_file" 2>/dev/null) || continue
    [[ "$cf_identity" =~ ^[0-9a-fA-F]{32}$ ]] || continue
    cf_hash=$(printf '%s' "$cf_identity" | sha256sum) || return 1
    printf '%s' "${cf_hash%% *}"
    return 0
  done
  return 1
}
cf_endpoint_value=$(cf_endpoint)
cf_endpoint_rc=$?
printf '%s\n' 'CONFIGFLOW_DOCKER_HA_PREFLIGHT_V1_BEGIN'
cf_emit kernel uname -s
cf_emit docker_endpoint cf_probe_endpoint
cf_emit docker_version cf_local_docker version --format '{"client":{{json .Client.Version}},"server":{{json .Server.Version}}}'
cf_emit daemon_os cf_local_docker info --format '{{.OSType}}'
cf_emit daemon_platform cf_local_docker info --format '{"os":{{json .OperatingSystem}},"kernel":{{json .KernelVersion}}}'
cf_emit daemon_security cf_local_docker info --format '{{json .SecurityOptions}}'
cf_emit compose cf_docker compose version --short
cf_emit ipv4 ip -o -4 address show
cf_emit udp53 cf_ports udp
cf_emit tcp53 cf_ports tcp
cf_emit nesting cf_nesting
cf_emit machine_fingerprint cf_fingerprint
printf '%s\n' 'CONFIGFLOW_DOCKER_HA_PREFLIGHT_V1_END'
)
`

// preflightCheck is _check(ident, label, status, detail, remedy); remedy ""
// is None.
// PreflightCheck describes one host requirement.
type PreflightCheck struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	Remedy string `json:"remedy,omitempty"`
}

// PreflightReport is the bounded host collector's evaluation.
type PreflightReport struct {
	Status          string           `json:"status"`
	Summary         string           `json:"summary"`
	Checks          []PreflightCheck `json:"checks"`
	HostFingerprint string           `json:"host_fingerprint,omitempty"`
}

func preflightCheck(ident, label, status, detail, remedy string) PreflightCheck {
	return PreflightCheck{ident, label, status, detail, remedy}
}
func preflightUnknown(reason string) *PreflightReport {
	return &PreflightReport{Status: "unknown", Summary: "输出不完整或无法识别，尚不能判断基础环境是否符合。",
		Checks: []PreflightCheck{preflightCheck("output", "采集输出", "unknown", reason,
			"重新运行当前页面的检查命令，粘贴包含 V1_BEGIN 到 V1_END 的完整输出。")}}
}

func isBlankTab(c byte) bool { return c == ' ' || c == '\t' }

// isBase64Text is re.fullmatch(r'[A-Za-z0-9+/]*={0,2}', s).
func isBase64Text(s string) bool {
	body := strings.TrimRight(s, "=")
	if len(s)-len(body) > 2 {
		return false
	}
	for i := 0; i < len(body); i++ {
		c := body[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/') {
			return false
		}
	}
	return true
}

type protocolRow struct {
	key     string
	code    int
	payload string
}

// preflightProtocolRow is _protocol_row(line); ok false for None.
func preflightProtocolRow(line string) (protocolRow, bool) {
	line = strings.TrimLeft(line, " \t")
	// re.fullmatch(r'CFHA1[ \t]+([a-z0-9_]+)[ \t]+(0|[1-9][0-9]{0,2})([ \t].*)?', line)
	rest, found := strings.CutPrefix(line, "CFHA1")
	if !found {
		return protocolRow{}, false
	}
	skip := func(s string) (string, bool) {
		trimmed := strings.TrimLeft(s, " \t")
		return trimmed, len(trimmed) < len(s)
	}
	var ok bool
	if rest, ok = skip(rest); !ok {
		return protocolRow{}, false
	}
	i := 0
	for i < len(rest) && (rest[i] >= 'a' && rest[i] <= 'z' || rest[i] >= '0' && rest[i] <= '9' || rest[i] == '_') {
		i++
	}
	if i == 0 {
		return protocolRow{}, false
	}
	key := rest[:i]
	if rest, ok = skip(rest[i:]); !ok {
		return protocolRow{}, false
	}
	i = 0
	for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
		i++
	}
	digits := rest[:i]
	if !(digits == "0" || len(digits) >= 1 && len(digits) <= 3 && digits[0] != '0') {
		return protocolRow{}, false
	}
	tail := rest[i:]
	if tail != "" && !isBlankTab(tail[0]) {
		return protocolRow{}, false
	}
	code := 0
	for j := 0; j < len(digits); j++ {
		code = code*10 + int(digits[j]-'0')
	}
	if code > 255 {
		return protocolRow{}, false
	}
	payload := strings.Trim(tail, " \t")
	if payload == "" && code == 0 && line != "CFHA1\t"+key+"\t0\t" {
		// A collapsed three-column success cannot prove that its payload was empty.
		return protocolRow{}, false
	}
	if payload != "-" && !isBase64Text(payload) {
		return protocolRow{}, false
	}
	return protocolRow{key, code, payload}, true
}

// preflightMarkdownLine is _markdown_protocol_line(line).
func preflightMarkdownLine(line string) string {
	stripped := strings.TrimRight(line, " \t")
	if candidate, found := strings.CutSuffix(stripped, "\\"); found {
		marker := strings.Trim(candidate, " \t")
		if marker == PreflightBegin || marker == PreflightEnd {
			return marker
		}
		if _, ok := preflightProtocolRow(candidate); ok {
			return candidate
		}
	}
	return line
}

// pySplitLines is str.splitlines() (line breaks removed).
func pySplitLines(text string) []string {
	lines := splitLines(text)
	for i, line := range lines {
		if strings.HasSuffix(line, "\r\n") {
			lines[i] = line[:len(line)-2]
			continue
		}
		_, size := utf8.DecodeLastRuneInString(line)
		r, _ := utf8.DecodeLastRuneInString(line)
		switch r {
		case '\r', '\n', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
			lines[i] = line[:len(line)-size]
		}
	}
	return lines
}

// b64decodeValidate is base64.b64decode(s, validate=True) of a str already
// matching [A-Za-z0-9+/]*={0,2} (CPython 3.11 binascii.a2b_base64, not
// strict mode): leading padding is refused, pads before the third character
// of a quad are ignored, the pad that completes a quad must be the last
// character, leftover bits are dropped.
func b64decodeValidate(s string) ([]byte, bool) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	if strings.HasPrefix(s, "=") {
		return nil, false // "Leading padding not allowed" (CPython 3.11.17)
	}
	out := make([]byte, 0, len(s)*3/4)
	quadPos, pads := 0, 0
	var left byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '=' {
			if quadPos >= 2 {
				pads++
				if quadPos+pads >= 4 {
					// "Excess data after padding" (CPython 3.11.17, also outside strict mode)
					return out, i+1 == len(s)
				}
			}
			continue
		}
		v := byte(strings.IndexByte(alphabet, c))
		pads = 0
		switch quadPos {
		case 0:
			quadPos, left = 1, v
		case 1:
			quadPos = 2
			out = append(out, left<<2|v>>4)
			left = v & 0x0f
		case 2:
			quadPos = 3
			out = append(out, left<<4|v>>2)
			left = v & 0x03
		case 3:
			quadPos = 0
			out = append(out, left<<6|v)
			left = 0
		}
	}
	return out, quadPos == 0
}

type preflightValue struct {
	code int
	text string
}

// preflightDecode is _decode(output): the fields, or the reason the output
// cannot be judged; a *ValueError for a wrong type, encoding or size.
func preflightDecode(output any) (map[string]preflightValue, string, error) {
	text, isString := output.(string)
	if !isString {
		return nil, "", valueError(msgPreflightOutputType)
	}
	// A Go string holds no lone surrogate; invalid UTF-8 stands for the
	// str Python cannot encode.
	if !utf8.ValidString(text) {
		return nil, "", valueError(msgPreflightUTF8)
	}
	if len(text) > PreflightMaxOutputBytes {
		return nil, "", valueError(msgPreflightSize)
	}
	lines := pySplitLines(strings.ReplaceAll(text, "\r\n", "\n"))
	for i, line := range lines {
		lines[i] = preflightMarkdownLine(line)
	}
	var starts, ends []int
	for i, line := range lines {
		switch strings.TrimSpace(line) {
		case PreflightBegin:
			starts = append(starts, i)
		case PreflightEnd:
			ends = append(ends, i)
		}
	}
	if len(starts) != 1 || len(ends) != 1 || starts[0] >= ends[0] {
		return nil, "未找到唯一、完整的新版采集块；旧版输出、截断或拼接多次输出不能判定通过。", nil
	}
	start, end := starts[0], ends[0]
	for i, line := range lines {
		if i != start && i != end && strings.HasPrefix(line, "CONFIGFLOW_DOCKER_HA_PREFLIGHT_") {
			return nil, "输出混有其他版本或重复的采集标记。", nil
		}
		if !(start < i && i < end) {
			// re.match(r'^[ \t]*CFHA1(?:[ \t]|$)', line)
			if rest, found := strings.CutPrefix(strings.TrimLeft(line, " \t"), "CFHA1"); found && (rest == "" || isBlankTab(rest[0])) {
				return nil, "完整块之外还有采集字段，请只保留本次完整输出。", nil
			}
		}
	}
	values := map[string]preflightValue{}
	for _, line := range lines[start+1 : end] {
		row, ok := preflightProtocolRow(line)
		_, known := values[row.key]
		if !ok || !isPreflightField(row.key) || known {
			return nil, "采集字段重复、缺损或格式不受支持。", nil
		}
		var decoded []byte
		if row.payload != "-" {
			if decoded, ok = b64decodeValidate(row.payload); !ok {
				return nil, "采集字段编码无效，请重新采集。", nil
			}
		}
		if len(decoded) > PreflightMaxFieldBytes {
			return nil, "单项输出过大，无法确认采集结果完整。", nil
		}
		if !utf8.Valid(decoded) {
			return nil, "采集字段编码无效，请重新采集。", nil
		}
		if strings.Contains(string(decoded), "\x00") {
			return nil, "采集字段包含无效二进制内容。", nil
		}
		values[row.key] = preflightValue{row.code, string(decoded)}
	}
	if len(values) != len(PreflightFields) {
		return nil, "缺少必需检查项，不能把缺失结果视为通过。", nil
	}
	return values, "", nil
}

func isPreflightField(key string) bool {
	for _, field := range PreflightFields {
		if field == key {
			return true
		}
	}
	return false
}

// preflightJSON is _json_value(item, expected_type): the decoded value when
// the probe succeeded and it is a dict (object) or list, else nil.
func preflightObject(item preflightValue) model.Fields {
	var fields model.Fields
	if item.code != 0 {
		return fields
	}
	if json.Unmarshal([]byte(item.text), &fields) != nil {
		return model.Fields{}
	}
	return fields
}

func probeSecurity(item preflightValue) ([]string, bool) {
	if item.code != 0 {
		return nil, false
	}
	var raw []json.RawMessage
	if json.Unmarshal([]byte(item.text), &raw) != nil || raw == nil || len(raw) > 32 {
		return nil, false
	}
	result := make([]string, 0, len(raw))
	for _, value := range raw {
		if len(value) == 0 || value[0] != '"' {
			return nil, false
		}
		var text string
		if json.Unmarshal(value, &text) != nil {
			return nil, false
		}
		result = append(result, text)
	}
	return result, true
}
func pyFields(s string) []string { return strings.Fields(s) }

func isUpperHex(s string) bool {
	for i := 0; i < len(s); i++ {
		if !(s[i] >= '0' && s[i] <= '9' || s[i] >= 'A' && s[i] <= 'F') {
			return false
		}
	}
	return true
}

// preflightPortCheck is _port_check(values, proto, mode).
func preflightPortCheck(values map[string]preflightValue, proto, mode string) PreflightCheck {
	lower := strings.ToLower(proto)
	ident := lower + "53"
	item := values[ident]
	label := "宿主机 " + proto + " 53 端口"
	if mode == "macvlan" {
		return preflightCheck(ident, label, "pass", "Macvlan 不要求宿主机 53 端口空闲，此项不是安装前置条件；未读取或已有监听均不阻断。容器独立地址和 VIP 的可用性尚未验证。",
			"安装后检查容器的 DNS 监听，并从实际局域网设备验证容器地址与 VIP。")
	}
	if item.code != 0 {
		return preflightCheck(ident, label, "unknown", "未能读取当前网络命名空间的监听状态，不能视为端口空闲。",
			"重新运行新版只读命令；它会先使用 ss，再尝试 /proc/net。若均不可读，请在实际 Docker 宿主机检查，勿直接停止服务。")
	}
	lines := pySplitLines(item.text)
	source := "ss"
	if len(lines) > 0 && strings.HasPrefix(lines[0], "source=") {
		source = strings.TrimPrefix(lines[0], "source=")
		lines = lines[1:]
	}
	switch source {
	case "proc-net-v1":
		if len(lines) == 0 || lines[0] != "tables="+lower+","+lower+"6" {
			return preflightCheck(ident, label, "unknown", "/proc/net 未确认 IPv4/IPv6 两张表均完整读取，不能视为端口空闲。", "")
		}
		lines = lines[1:]
		for _, line := range lines {
			columns := strings.Split(line, " ")
			bad := len(columns) != 4 || columns[0] != lower || (columns[1] != "4" && columns[1] != "6")
			if !bad {
				width := 32
				if columns[1] == "4" {
					width = 8
				}
				address, port, found := strings.Cut(columns[2], ":")
				bad = !found || port != "0035" || len(address) != width || !isUpperHex(address)
				if proto == "TCP" {
					bad = bad || columns[3] != "0A"
				} else {
					bad = bad || (columns[3] != "01" && columns[3] != "07")
				}
			}
			if bad {
				return preflightCheck(ident, label, "unknown", "/proc/net 端口记录格式无效，不能视为端口空闲。", "")
			}
		}
	case "ss":
		var kept []string
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				kept = append(kept, line)
			}
		}
		lines = kept
		state := "LISTEN"
		if proto == "UDP" {
			state = "UNCONN"
		}
		for _, line := range lines {
			columns := pyFields(line)
			if len(columns) > 0 && (columns[0] == "udp" || columns[0] == "tcp") {
				columns = columns[1:]
			}
			if len(columns) != 5 || columns[0] != state || !asciiDigits(columns[1]) || !asciiDigits(columns[2]) || !strings.HasSuffix(columns[3], ":53") {
				return preflightCheck(ident, label, "unknown", "无法识别 ss 的监听输出，尚未确认端口状态。", "重新运行页面提供的命令，不要手工改写检查项。")
			}
		}
	default:
		return preflightCheck(ident, label, "unknown", "端口检查来源未知，不能视为端口空闲。", "")
	}
	sourceLabel := "ss"
	if source == "proc-net-v1" {
		sourceLabel = "/proc/net"
	}
	if len(lines) > 0 && mode == "host" {
		return preflightCheck(ident, label, "fail", "通过 "+sourceLabel+" 检测到 "+itoa(len(lines))+" 项监听；默认新装会同时绑定 TCP/UDP 53，包含回环地址的占用也需处理。",
			"核对现有 DNS 服务；若为待迁移的原 OxiDNS，请按迁移步骤操作。不要直接停止正在服务的主机；自定义仅单协议配置需另行确认。")
	}
	return preflightCheck(ident, label, "pass", "本次通过 "+sourceLabel+" 未发现当前网络命名空间的 53 端口监听；不代表其他命名空间或后续启动时仍空闲。", "")
}

// isVersionText is re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+[A-Za-z0-9_.+-]*', s)
// (with prefix "v?" when allowV), returning the three numbers' digits.
func isVersionText(s string, allowV bool) ([3]string, bool) {
	var parts [3]string
	if allowV {
		s = strings.TrimPrefix(s, "v")
	}
	// [0-9]+ runs are greedy, but the suffix class also holds digits and
	// dots: "1.2.3.4" is 1, 2, 3 + ".4"; the third run takes every digit.
	for n := 0; n < 3; n++ {
		i := 0
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == 0 {
			return parts, false
		}
		parts[n], s = s[:i], s[i:]
		if n < 2 {
			if s == "" || s[0] != '.' {
				return parts, false
			}
			s = s[1:]
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == '+' || c == '-') {
			return parts, false
		}
	}
	return parts, true
}

// interfaceName is re.sub(r'[^A-Za-z0-9_.:@-]', "", raw)[:48].
func interfaceName(raw string) string {
	var out []rune
	for _, r := range raw {
		if r < 128 && (r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune("_.:@-", r)) {
			out = append(out, r)
		}
	}
	if len(out) > 48 {
		out = out[:48]
	}
	return string(out)
}

// matchAddressLine is re.match(r'^\d+:\s+(\S+)\s+inet\s+(\S+)\s+', line)
// (str pattern: Unicode digits and white space).
func matchAddressLine(line string) (name, address string, ok bool) {
	runes := []rune(line)
	i := 0
	for i < len(runes) && runes[i] >= '0' && runes[i] <= '9' {
		i++
	}
	if i == 0 || i >= len(runes) || runes[i] != ':' {
		return "", "", false
	}
	i++
	spaces := func() bool {
		j := i
		for i < len(runes) && unicode.IsSpace(runes[i]) {
			i++
		}
		return i > j
	}
	word := func() string {
		j := i
		for i < len(runes) && !unicode.IsSpace(runes[i]) {
			i++
		}
		return string(runes[j:i])
	}
	if !spaces() {
		return "", "", false
	}
	name = word()
	if name == "" || !spaces() {
		return "", "", false
	}
	// \S+ is greedy and backtracks only into shorter runs, which can never be
	// followed by white space, so the next word must be exactly "inet".
	if word() != "inet" || !spaces() {
		return "", "", false
	}
	address = word()
	if address == "" || !spaces() {
		return "", "", false
	}
	return name, address, true
}

// parseIPv4Interface is ipaddress.IPv4Interface(text) for a str: the address
// and the prefix length (decimal, netmask or hostmask; /32 when absent).
func parseIPv4Interface(text string) (uint32, int, bool) {
	addressText, _, _ := strings.Cut(text, "/")
	address, err := parseIPv4(addressText)
	prefix, ok := netipx.ParseNetwork4(text, false)
	return address, prefix.Bits(), err == nil && ok
}

// EvaluatePreflight evaluates bounded collector text without contacting a host.
// Incomplete or malformed machine evidence produces an unknown check.
func EvaluatePreflight(output, mode string) (*PreflightReport, error) {
	if mode != "host" && mode != "macvlan" {
		return nil, valueError(msgPreflightNetwork)
	}
	values, reason, err := preflightDecode(output)
	if err != nil {
		return nil, err
	}
	if reason != "" {
		return preflightUnknown(reason), nil
	}
	var checks []PreflightCheck
	add := func(c PreflightCheck) { checks = append(checks, c) }
	pick := func(cond bool, yes, no string) string {
		if cond {
			return yes
		}
		return no
	}

	kernel := values["kernel"]
	linux := kernel.code == 0 && strings.TrimSpace(kernel.text) == "Linux"
	add(preflightCheck("linux", "Linux 宿主环境", pick(kernel.code != 0, "unknown", pick(linux, "pass", "fail")),
		pick(linux, "当前内核为 Linux。", "未确认 Linux 宿主；macOS/Windows 上的 Docker Desktop 不支持此 HA 部署。"),
		pick(linux, "", "请在实际 Linux/NAS Docker 宿主机的终端重新采集。")))

	endpointItem := values["docker_endpoint"]
	endpoint := strings.TrimSpace(endpointItem.text)
	local := endpointItem.code == 0 && endpoint == "local-unix"
	add(preflightCheck("docker_endpoint", "Docker 检查目标",
		pick(endpointItem.code != 0 || (endpoint != "local-unix" && endpoint != "remote"), "unknown", pick(endpoint == "local-unix", "pass", "fail")),
		pick(local, "Docker 使用本机 Unix socket；以下网卡与端口结果来自当前终端主机。", "当前 Docker 目标为远端或无法确认；本机网卡、身份和端口结果不能代表 daemon 宿主。"),
		pick(local, "", "切换到实际宿主机的本地 Docker context；检查 DOCKER_CONTEXT/DOCKER_HOST 后重新采集。")))

	versionItem := values["docker_version"]
	validVersion := false
	if version := preflightObject(versionItem); version.Len() > 0 {
		validVersion = true
		for _, k := range []string{"client", "server"} {
			text, isString := fieldText(version, k)
			if _, ok := isVersionText(text, false); !isString || !ok {
				validVersion = false
				break
			}
		}
	}
	add(preflightCheck("docker_daemon", "Docker CLI 与 daemon",
		pick(validVersion, "pass", pick(versionItem.code == 126, "unknown", pick(versionItem.code != 0, "fail", "unknown"))),
		pick(validVersion, "已同时读取 Docker 客户端和服务端版本。", pick(versionItem.code == 126, "检查目标不是已确认的本机 Unix socket，已跳过 daemon 读取；这不代表远端服务未启动。", "Docker 命令或 daemon 未就绪，尚不能安装。")),
		pick(validVersion, "", "先确认 Docker 服务已启动、当前用户有访问权限，并能运行 docker version 读取 Server 信息。")))

	osItem := values["daemon_os"]
	linuxOS := osItem.code == 0 && strings.TrimSpace(osItem.text) == "linux"
	add(preflightCheck("docker_os", "Docker 容器系统", pick(osItem.code != 0, "unknown", pick(linuxOS, "pass", "fail")),
		pick(linuxOS, "Docker daemon 使用 Linux 容器。", "未确认 daemon 为 Linux 容器模式。"),
		pick(linuxOS, "", "使用实际 Linux/NAS 上的 rootful Docker Engine。")))

	platformValid, desktop := false, false
	if platform := preflightObject(values["daemon_platform"]); platform.Len() > 0 {
		osText, osOK := fieldText(platform, "os")
		kernelText, kernelOK := fieldText(platform, "kernel")
		platformValid = osOK && strings.TrimSpace(osText) != "" && kernelOK && strings.TrimSpace(kernelText) != ""
		if platformValid {
			combined := strings.ToLower(osText + " " + kernelText)
			for _, term := range []string{"docker desktop", "docker-desktop", "linuxkit"} {
				if strings.Contains(combined, term) {
					desktop = true
				}
			}
		}
	}
	add(preflightCheck("docker_platform", "原生 Docker Engine", pick(!platformValid, "unknown", pick(desktop, "fail", "pass")),
		pick(desktop, "检测到 Docker Desktop/其虚拟机内核，不支持此网络部署。", pick(platformValid, "未发现 Docker Desktop 标记。", "无法确认 Docker 运行平台。")),
		pick(desktop || !platformValid, "请在 Linux/NAS 原生 Docker Engine 上部署。", "")))

	validSecurity, rootless := false, false
	if security, valid := probeSecurity(values["daemon_security"]); valid {
		validSecurity = true
		for _, text := range security {
			if strings.Contains(strings.ToLower(text), "rootless") {
				rootless = true
			}
		}
	}

	add(preflightCheck("docker_rootful", "Rootful Docker", pick(!validSecurity, "unknown", pick(rootless, "fail", "pass")),
		pick(rootless, "检测到 rootless Docker，无法提供所需网络能力。", pick(validSecurity, "服务端未启用 rootless 模式。", "无法读取 daemon 的安全模式。")),
		pick(rootless || !validSecurity, "使用宿主机的 rootful Docker 服务；不要用 privileged 代替环境检查。", "")))

	composeItem := values["compose"]
	modernCompose := false
	if composeItem.code == 0 {
		if parts, ok := isVersionText(strings.TrimSpace(composeItem.text), true); ok {
			// Compare machine version components without an interpreter digit limit.
			var numbers [3]int64
			for i, part := range parts {
				n, ok := new(big.Int).SetString(part, 10)
				if !ok {
					return nil, valueError("Invalid Compose version")
				}
				if !n.IsInt64() || n.Int64() > 1<<40 {
					numbers[i] = 1 << 40
				} else {
					numbers[i] = n.Int64()
				}
			}
			modernCompose = numbers[0] > 2 || numbers[0] == 2 && (numbers[1] > 30 || numbers[1] == 30 && numbers[2] >= 0)
		}
	}
	add(preflightCheck("compose", "Compose 维护工具", pick(modernCompose, "pass", "warning"),
		pick(modernCompose, "Compose 支持迁移模板的 raw 环境文件格式。", "未检测到 Compose 2.30+；自动安装使用 Docker Run，可继续准备，Compose 维护模板暂不能直接使用。"),
		pick(modernCompose, "", "若需使用 Compose 维护模板，再安装或升级到 2.30+；安装脚本不要求 Compose。")))

	ipv4Item := values["ipv4"]
	type candidate struct{ name, address string }
	var candidates []candidate
	malformed := false
	if ipv4Item.code == 0 {
		for _, line := range pySplitLines(ipv4Item.text) {
			if strings.TrimSpace(line) == "" {
				continue
			}
			rawName, address, matched := matchAddressLine(line)
			var ip uint32
			var prefix int
			valid := false
			if matched {
				ip, prefix, valid = parseIPv4Interface(address)
			}
			if !valid {
				malformed = true
			} else if !(isLoopback(ip) || isLinkLocal(ip) || isUnspecified(ip) || isMulticast(ip) || isReserved(ip)) {
				safeName := interfaceName(rawName)
				if safeName == "" {
					safeName = "未识别接口"
				}
				if safeName != rawName {
					safeName += "（名称已截短或清理）"
				}
				candidates = append(candidates, candidate{safeName, ipv4Text(ip) + "/" + itoa(prefix)})
			}
		}
	}
	shown := make([]string, 0, 8)
	for i, c := range candidates {
		if i == 8 {
			break
		}
		shown = append(shown, c.name+" · "+c.address)
	}
	candidateDetail := "读取到 " + itoa(len(candidates)) + " 个候选 IPv4：" + strings.Join(shown, "；") +
		pick(len(candidates) > 8, "；仅显示前 8 条。", "。") + "其中可能有 Docker bridge，尚未验证局域网可达性或固定地址；不会自动选择网卡。"
	add(preflightCheck("ipv4", "网卡 IPv4 信息", pick(ipv4Item.code != 0 || malformed, "unknown", pick(len(candidates) > 0, "pass", "warning")),
		pick(ipv4Item.code == 0 && !malformed && len(candidates) > 0, candidateDetail, "无法可靠读取非回环 IPv4 候选地址。"),
		"请核对实际局域网网卡、静态地址及网关；Macvlan 还需确认 parent 接口和网段。"))

	add(preflightPortCheck(values, "UDP", mode))
	add(preflightPortCheck(values, "TCP", mode))

	nesting := values["nesting"]
	native := nesting.code == 0 && nesting.text == "none"
	add(preflightCheck("nesting", "虚拟化与嵌套容器", pick(native, "pass", "warning"),
		pick(native, "未发现常见容器嵌套标记；仍需后续网络验证。", "当前可能位于 LXC/Docker 等容器内，或无法确认；这不直接判定不支持。"),
		pick(native, "", "确认上层允许所需网络能力，安装后以 Agent 环境检测和真实 LAN/VIP 测试为准。")))

	fingerprintItem := values["machine_fingerprint"]
	fingerprintOK := fingerprintItem.code == 0 && len(fingerprintItem.text) == 64 && isLowerHex(fingerprintItem.text)
	add(preflightCheck("host_identity", "宿主身份信息", pick(fingerprintOK, "pass", "warning"),
		pick(fingerprintOK, "已获取默认 /etc/machine-id 的 SHA-256 指纹，不包含原始身份值。", "默认 /etc/machine-id 缺失、不可读、为软链接或未获取有效指纹；不能直接假定默认身份挂载可用，也尚不能对比两份输出。"),
		pick(fingerprintOK, "", "精简 Compose 会自动准备安装身份，无需此文件，独立主机以安装后的实时检查为准；使用脚本或旧 Compose 时仍需提供稳定的普通身份文件。")))

	statuses := map[string]bool{}
	for _, c := range checks {
		statuses[c.Status] = true
	}
	status := "pass"
	for _, s := range []string{"fail", "unknown", "warning"} {
		if statuses[s] {
			status = s
			break
		}
	}
	summary := map[string]string{
		"pass":    "基础检查通过；仍需安装后的环境检测及真实局域网/VIP 验证。",
		"warning": "基础信息已读取，有注意事项需确认；尚不能据此保证安装和故障接管成功。",
		"fail":    "发现需要处理的基础环境问题；处理后请重新采集。",
		"unknown": "部分必要检查未完成，尚不能判断基础环境是否符合。",
	}[status]
	result := &PreflightReport{Status: status, Summary: summary, Checks: checks}
	if fingerprintOK {
		result.HostFingerprint = fingerprintItem.text
	}
	return result, nil
}

func isLowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		if !(s[i] >= '0' && s[i] <= '9' || s[i] >= 'a' && s[i] <= 'f') {
			return false
		}
	}
	return true
}
