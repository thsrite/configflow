package installer

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

// Port of backend/agents/docker_ha_enrollment.py: short-lived, single-device
// enrollment for the Docker installation guide. Generating a command creates
// no Agent. The executed installer can obtain only the credentials of its
// exact new OxiDNS identity, even with registration locked.

// EnrollmentTTL is TTL (seconds).
const EnrollmentTTL = 24 * 60 * 60

// EnrollmentError is docker_ha_enrollment.EnrollmentError (a ValueError with
// an HTTP status).
type EnrollmentError struct {
	Message string
	Status  int
}

func (e *EnrollmentError) Error() string { return e.Message }

func enrollmentError(message string, status int) error {
	return &EnrollmentError{Message: message, Status: status}
}

// enrollmentNow is time.time(), replaceable in tests.
var enrollmentNow = func() float64 { return float64(time.Now().UnixNano()) / 1e9 }

// enrollmentLocalNow is datetime.now() (local wall clock), replaceable in tests.
var enrollmentLocalNow = time.Now

const (
	enrollmentUnreadable = "安装记录无法读取，请保留现场并检查服务端数据。"
	enrollmentInvalid    = "安装凭据无效，请重新生成安装命令。"
	enrollmentExpired    = "安装凭据无效或已过期，请重新生成安装命令。"
)

// randomBytes is os.urandom(n) (uuid4, token_urlsafe), replaceable in tests.
var randomBytes = func(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("installer: no randomness: " + err.Error())
	}
	return b
}

// haTokenURLSafe is secrets.token_urlsafe(n).
func haTokenURLSafe(n int) string { return base64.RawURLEncoding.EncodeToString(randomBytes(n)) }

// uuid4Hex is uuid.uuid4().hex.
func uuid4Hex() string {
	b := randomBytes(16)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return hex.EncodeToString(b)
}

func isSessionID(s string) bool {
	if len(s) != 32 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !(s[i] >= '0' && s[i] <= '9' || s[i] >= 'a' && s[i] <= 'f') {
			return false
		}
	}
	return true
}

func isEnrollmentToken(s string) bool {
	if len(s) != 43 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// pathExists is pathlib.Path.exists() of Python 3.11: stat errors other than
// ENOENT, ENOTDIR, EBADF and ELOOP are raised.
func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	var errno syscall.Errno
	if errors.As(err, &errno) && (errno == syscall.ENOENT || errno == syscall.ENOTDIR || errno == syscall.EBADF || errno == syscall.ELOOP) {
		return false, nil
	}
	return false, err
}

// readText is Path.read_text() (UTF-8, strict): notFound for
// FileNotFoundError, a *ValueError for invalid UTF-8, other errors as they are.
func readText(path string) (text string, notFound bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", errors.Is(err, os.ErrNotExist), err
	}
	if !utf8.Valid(data) {
		return "", false, valueError(msgInstallationEncoding)
	}
	return string(data), false, nil
}

var issueMu sync.Mutex

// naiveISO is datetime.now().isoformat() of a local wall-clock time.
func naiveISO(t time.Time) string {
	text := fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02d", t.Year(), int(t.Month()), t.Day(), t.Hour(), t.Minute(), t.Second())
	if micro := t.Nanosecond() / 1000; micro != 0 {
		text += fmt.Sprintf(".%06d", micro)
	}
	return text
}

const bootstrapSource = `import json, os, sys, urllib.error, urllib.request
from pathlib import Path
settings = json.loads(SETTINGS_LITERAL)
path = Path('/opt/configflow-agent/config-oxidns.json')
marker = path.with_name('.docker-ha-installation.json')
if path.is_symlink() or marker.is_symlink():
    sys.exit('Agent 身份路径是软链接，已停止安装。')
if marker.exists():
    try: same_installation = json.loads(marker.read_text()).get('url') == settings['url']
    except Exception: same_installation = False
    if not same_installation: sys.exit('此目录属于另一份安装命令；请保留原命令或使用迁移。')
elif path.exists():
    sys.exit('Agent 数据目录已有身份文件；请选择旧容器迁移，不能覆盖。')
request = urllib.request.Request(settings['url'], data=b'{}', method='POST',
    headers={'Authorization': 'Bearer ' + settings['token'], 'Content-Type': 'application/json'})
try:
    with urllib.request.urlopen(request, timeout=30) as response:
        identity = json.load(response)
except urllib.error.HTTPError as error:
    try: message = json.load(error).get('message', '安装登记失败')
    except Exception: message = '安装登记失败'
    sys.exit(message + '；请回到引导重新生成或重试。')
except Exception:
    sys.exit('无法连接 ConfigFlow 登记设备，请检查服务地址、证书和网络后重试。')
if not all(isinstance(identity.get(key), str) and identity[key] for key in ('agent_id', 'token')):
    sys.exit('安装登记响应无效，未写入身份文件。')
if path.exists():
    try: existing = json.loads(path.read_text())
    except Exception: sys.exit('现有 Agent 身份文件不可读，已停止安装。')
    if any(existing.get(key) != identity[key] for key in ('agent_id', 'token')):
        sys.exit('现有 Agent 身份与本次安装不一致，已停止安装。')
    print('已核对本次安装的 Agent 身份，保留原文件。')
    sys.exit(0)
if not marker.exists():
    fd = os.open(marker, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as output: json.dump({'url': settings['url']}, output)
fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
with os.fdopen(fd, 'w') as output:
    json.dump({key: identity[key] for key in ('agent_id', 'token')}, output)
print('本机 Agent 身份已登记；启动后等待真实心跳与高可用环境检查。')
`

func bootstrapNative(enrollment *Enrollment, request GuideRequest) (string, error) {
	dataDir := strings.TrimRight(request.Options.DataDir, "/") + "/agent"
	image := request.Image
	network := []string{"--network", "host"}
	if request.Options.NetworkMode == "macvlan" {
		network = []string{"--network", request.Options.AgentName + "-oxidns-ha-net", "--ip", request.Options.AgentIP}
	}
	values, err := inlineSettingsJSON(struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	}{enrollment.URL, enrollment.Token})
	if err != nil {
		return "", err
	}
	literal := "'" + strings.ReplaceAll(strings.ReplaceAll(string(values), "\\", "\\\\"), "'", "\\'") + "'"
	source := strings.ReplaceAll(bootstrapSource, "SETTINGS_LITERAL", literal)
	return "# 安装凭据仅用于本机 OxiDNS，有效期 24 小时；不包含管理账号或全局注册密钥。\n" +
		"docker run --rm -i " + joinQuoted(network) + " --entrypoint python3 --mount " + q("type=bind,src="+dataDir+",dst=/opt/configflow-agent") + " " + q(image) + " - <<'CF_DOCKER_HA_ENROLL'\n" + source + "CF_DOCKER_HA_ENROLL\n", nil
}
