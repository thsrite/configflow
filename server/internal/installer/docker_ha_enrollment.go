package installer

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
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
var randomBytes = func(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generate installation entropy: %w", err)
	}
	return b, nil
}

// haTokenURLSafe is secrets.token_urlsafe(n).
func haTokenURLSafe(n int) (string, error) {
	data, err := randomBytes(n)
	if err != nil {
		return "", err
	}
	if len(data) != n {
		return "", fmt.Errorf("installation entropy: expected %d bytes", n)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
func uuid4Hex() (string, error) {
	b, err := randomBytes(16)
	if err != nil {
		return "", err
	}
	if len(b) != 16 {
		return "", fmt.Errorf("installation entropy: expected 16 bytes")
	}
	b[6] = b[6]&15 | 64
	b[8] = b[8]&63 | 128
	return hex.EncodeToString(b), nil
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

var issueMu sync.Mutex

// naiveISO is datetime.now().isoformat() of a local wall-clock time.
func naiveISO(t time.Time) string {
	text := fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02d", t.Year(), int(t.Month()), t.Day(), t.Hour(), t.Minute(), t.Second())
	if micro := t.Nanosecond() / 1000; micro != 0 {
		text += fmt.Sprintf(".%06d", micro)
	}
	return text
}

func bootstrapNative(enrollment *Capability, request GuideRequest) (string, error) {
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
	return "# 安装凭据仅用于本机 OxiDNS，有效期 24 小时；不包含管理账号或全局注册密钥。\n" +
		"docker run --rm -i " + joinQuoted(network) + " --mount " + q("type=bind,src="+dataDir+",dst=/opt/configflow-agent") + " --entrypoint configflow-agent " + q(image) + " ha-enroll <<'CF_DOCKER_HA_ENROLL'\n" + string(values) + "\nCF_DOCKER_HA_ENROLL\n", nil
}
