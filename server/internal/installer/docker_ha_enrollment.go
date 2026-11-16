package installer

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"configflow/internal/jsonx"
	"configflow/internal/repo"
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

func randomBytes(n int) []byte {
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
		return "", false, valueError(pyUTF8DecodeError(data))
	}
	return string(data), false, nil
}

// pyUTF8DecodeError is str(UnicodeDecodeError) of CPython's strict UTF-8
// decoder for the first invalid sequence of data.
// TODO(port): copy of repo's unexported helper; share it when one is exported.
func pyUTF8DecodeError(data []byte) string {
	continuation := func(b byte) bool { return b >= 0x80 && b < 0xC0 }
	for start := 0; start < len(data); {
		lead := data[start]
		rest := len(data) - start
		code := -1 // -1 valid; 0 truncated; 1 invalid start; n>1: invalid continuation at offset n-1
		size := 1
		switch {
		case lead < 0x80:
		case lead < 0xC2:
			code = 1
		case lead < 0xE0:
			size = 2
			if rest < 2 {
				code = 0
			} else if !continuation(data[start+1]) {
				code = 2
			}
		case lead < 0xF0:
			size = 3
			bad2 := func(b byte) bool {
				return !continuation(b) || (lead == 0xE0 && b < 0xA0) || (lead == 0xED && b >= 0xA0)
			}
			switch {
			case rest < 2:
				code = 0
			case bad2(data[start+1]):
				code = 2
			case rest < 3:
				code = 0
			case !continuation(data[start+2]):
				code = 3
			}
		case lead < 0xF5:
			size = 4
			bad2 := func(b byte) bool {
				return !continuation(b) || (lead == 0xF0 && b < 0x90) || (lead == 0xF4 && b >= 0x90)
			}
			switch {
			case rest < 2:
				code = 0
			case bad2(data[start+1]):
				code = 2
			case rest < 3:
				code = 0
			case !continuation(data[start+2]):
				code = 3
			case rest < 4:
				code = 0
			case !continuation(data[start+3]):
				code = 4
			}
		default:
			code = 1
		}
		if code < 0 {
			start += size
			continue
		}
		end, reason := start+1, "invalid start byte"
		switch {
		case code == 0:
			end, reason = len(data), "unexpected end of data"
		case code > 1:
			end, reason = start+code-1, "invalid continuation byte"
		}
		if end == start+1 {
			return fmt.Sprintf("'utf-8' codec can't decode byte 0x%02x in position %d: %s", data[start], start, reason)
		}
		return fmt.Sprintf("'utf-8' codec can't decode bytes in position %d-%d: %s", start, end-1, reason)
	}
	return ""
}

// compareWithNow compares a Python number with time.time() exactly
// (-1, 0, 1 for value <, ==, > now); a non-number is the TypeError of the
// comparison.
func compareWithNow(value any, now float64, operator string) (int, error) {
	var x *big.Float
	switch v := value.(type) {
	case bool:
		x = big.NewFloat(0)
		if v {
			x = big.NewFloat(1)
		}
	case jsonx.Number:
		if v.IsInt() {
			n, _ := v.BigInt()
			x = new(big.Float).SetPrec(uint(n.BitLen()) + 64).SetInt(n)
		} else {
			f := v.Float64()
			if math.IsNaN(f) {
				return 2, nil // every comparison with NaN is False
			}
			if math.IsInf(f, 0) {
				if f > 0 {
					return 1, nil
				}
				return -1, nil
			}
			x = big.NewFloat(f)
		}
	default:
		return 0, typeError("'" + operator + "' not supported between instances of 'float' and '" + pyTypeName(value) + "'")
	}
	return x.Cmp(big.NewFloat(now)), nil
}

// IssueEnrollment is issue(repository, body, profile_id). Call only after
// guide input validation and administrator authentication.
func IssueEnrollment(dataDir string, body *jsonx.Object, profileID string) (*Enrollment, error) {
	if err := repo.ValidateProfileID(profileID); err != nil {
		return nil, err
	}
	root := filepath.Join(dataDir, "docker-ha-installations")
	if err := os.MkdirAll(dataDir, 0o777); err != nil {
		return nil, err
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		if info, statErr := os.Stat(root); statErr != nil || !info.IsDir() {
			return nil, err
		}
	}
	// Retain the installation identity when a dialog closes or an interrupted
	// install is retried. Different settings never gain access to that identity.
	serverURL, _ := body.GetOr("server_url", nil).(string)
	spec := body.Clone()
	spec.Set("mode", "new")
	spec.Set("server_url", strings.TrimRight(serverURL, "/"))
	spec.Set("profile_id", profileID)
	spec.Delete("action")
	if spec.GetOr("install_method", nil) == "compose_compact" {
		// Updating the image is routine maintenance, not a new Agent identity.
		// The compact runtime does not use legacy host-file/volume choices.
		for _, ignored := range []string{"image", "ha_host_id_path", "create_host_identity"} {
			spec.Delete(ignored)
		}
	}
	specJSON, err := jsonx.Encode(spec, jsonx.Options{EnsureASCII: true, SortKeys: true})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(specJSON)
	index := filepath.Join(root, hex.EncodeToString(sum[:])+".index")
	lock, err := os.OpenFile(filepath.Join(root, ".issue.lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return nil, err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	var session *jsonx.Object
	exists, err := pathExists(index)
	if err != nil {
		return nil, err
	}
	if exists {
		sessionID, _, err := readText(index)
		if err != nil {
			return nil, err
		}
		if !isSessionID(sessionID) {
			return nil, enrollmentError(enrollmentUnreadable, 409)
		}
		text, notFound, err := readText(filepath.Join(root, sessionID+".json"))
		var ve *ValueError
		if notFound || (err != nil && errors.As(err, &ve)) {
			return nil, enrollmentError(enrollmentUnreadable, 409)
		}
		if err != nil {
			return nil, err
		}
		loaded, err := jsonx.Parse([]byte(text))
		if err != nil {
			return nil, enrollmentError(enrollmentUnreadable, 409)
		}
		var isObject bool
		if session, isObject = loaded.(*jsonx.Object); !isObject {
			return nil, typeError("'" + pyTypeName(loaded) + "' object has no attribute 'get'")
		}
	}
	if session == nil {
		name, err := haString(body.GetOr("agent_name", "agent"))
		if err != nil {
			return nil, err
		}
		session = jsonx.Obj("id", uuid4Hex(), "profile_id", profileID,
			"name", name+"-oxidns",
			"host", body.Value("agent_ip"), "port", body.GetOr("oxidns_port", jsonx.Int(8082)))
	}
	cmp, err := compareWithNow(session.GetOr("expires", jsonx.Int(0)), enrollmentNow(), "<=")
	if err != nil {
		return nil, err
	}
	id, isString := session.GetOr("id", nil).(string)
	if cmp <= 0 {
		token := haTokenURLSafe(32)
		tokenSum := sha256.Sum256([]byte(token))
		session.Set("token", token)
		session.Set("token_hash", hex.EncodeToString(tokenSum[:]))
		session.Set("expires", jsonx.Int(int64(enrollmentNow())+EnrollmentTTL))
		if !isString {
			return nil, typeError("unsupported operand type(s) for +: '" + pyTypeName(session.GetOr("id", nil)) + "' and 'str'")
		}
		path := filepath.Join(root, id+".json")
		temp := strings.TrimSuffix(path, ".json") + ".tmp"
		data, err := jsonx.Default(session)
		if err != nil {
			return nil, err
		}
		output, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return nil, err
		}
		_, err = output.Write(data)
		if closeErr := output.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return nil, err
		}
		if err := os.Rename(temp, path); err != nil {
			return nil, err
		}
	}
	if exists, err = pathExists(index); err != nil {
		return nil, err
	} else if !exists {
		output, err := os.OpenFile(index, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return nil, err
		}
		if !isString {
			output.Close()
			return nil, typeError("write() argument must be str, not " + pyTypeName(session.GetOr("id", nil)))
		}
		_, err = output.WriteString(id)
		if closeErr := output.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return nil, err
		}
	}
	if !isString {
		return nil, typeError("can only concatenate str (not \"" + pyTypeName(session.GetOr("id", nil)) + "\") to str")
	}
	token, present := session.Get("token")
	if !present {
		return nil, typeError("KeyError: 'token'")
	}
	tokenText, isString := token.(string)
	if !isString {
		return nil, typeError("unsupported non-str enrollment token")
	}
	expires, present := session.Get("expires")
	if !present {
		return nil, typeError("KeyError: 'expires'")
	}
	return &Enrollment{URL: strings.TrimRight(serverURL, "/") + "/api/agents/docker-oxidns-ha-enroll/" + id, Token: tokenText, ExpiresAt: expires}, nil
}

// AgentUpdater is the part of the Agent manager enroll needs:
// _update_agents(updater, profile_id) (agents.Manager.UpdateAgents).
type AgentUpdater interface {
	UpdateAgents(ctx context.Context, profileID string, updater func(ctx context.Context, agents *[]any) (any, error)) (any, error)
}

// naiveISO is datetime.now().isoformat() of a local wall-clock time.
func naiveISO(t time.Time) string {
	text := fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02d", t.Year(), int(t.Month()), t.Day(), t.Hour(), t.Minute(), t.Second())
	if micro := t.Nanosecond() / 1000; micro != 0 {
		text += fmt.Sprintf(".%06d", micro)
	}
	return text
}

// Enroll is enroll(repository, manager, session_id, token): {'agent_id',
// 'token'} of the installation's Agent, created on the first call. token is
// parse_bearer_token's result (present false for None).
func Enroll(ctx context.Context, dataDir string, manager AgentUpdater, sessionID, token string, present bool) (*jsonx.Object, error) {
	if !isSessionID(sessionID) || !present || !isEnrollmentToken(token) {
		return nil, enrollmentError(enrollmentInvalid, 401)
	}
	text, notFound, err := readText(filepath.Join(dataDir, "docker-ha-installations", sessionID+".json"))
	var ve *ValueError
	if notFound || (err != nil && errors.As(err, &ve)) {
		return nil, enrollmentError(enrollmentInvalid, 401)
	}
	if err != nil {
		return nil, err
	}
	loaded, err := jsonx.Parse([]byte(text))
	if err != nil {
		return nil, enrollmentError(enrollmentInvalid, 401)
	}
	session, isObject := loaded.(*jsonx.Object)
	item := func(key string) (any, error) {
		if !isObject {
			return nil, typeError("'" + pyTypeName(loaded) + "' object is not subscriptable")
		}
		value, ok := session.Get(key)
		if !ok {
			return nil, typeError("KeyError: '" + key + "'")
		}
		return value, nil
	}
	expires, err := item("expires")
	if err != nil {
		return nil, err
	}
	cmp, err := compareWithNow(expires, enrollmentNow(), ">=")
	if err != nil {
		return nil, err
	}
	if cmp != 2 && cmp <= 0 { // time.time() >= expires (NaN: False)
		return nil, enrollmentError(enrollmentExpired, 401)
	}
	tokenHash, err := item("token_hash")
	if err != nil {
		return nil, err
	}
	expected, isString := tokenHash.(string)
	if !isString {
		return nil, typeError("unsupported operand types(s) or combination of types: 'str' and '" + pyTypeName(tokenHash) + "'")
	}
	for i := 0; i < len(expected); i++ {
		if expected[i] >= 0x80 {
			return nil, typeError("comparing strings with non-ASCII characters is not supported")
		}
	}
	tokenSum := sha256.Sum256([]byte(token))
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(tokenSum[:])), []byte(expected)) != 1 {
		return nil, enrollmentError(enrollmentExpired, 401)
	}
	profileValue, err := item("profile_id")
	if err != nil {
		return nil, err
	}
	profileID, isString := profileValue.(string)
	if !isString {
		return nil, repo.NewValidationError("Invalid profile id")
	}
	if err := repo.ValidateProfileID(profileID); err != nil {
		return nil, err
	}
	expectedFields := jsonx.NewObject()
	for _, key := range []string{"name", "host", "port", "profile_id"} {
		value, err := item(key)
		if err != nil {
			return nil, err
		}
		expectedFields.Set(key, value)
	}
	expectedFields.Set("service_type", "oxidns")
	expectedFields.Set("deployment_method", "docker")

	result, err := manager.UpdateAgents(ctx, profileID, func(ctx context.Context, agents *[]any) (any, error) {
		var owned *jsonx.Object
		for _, a := range *agents {
			agent, ok := a.(*jsonx.Object)
			if !ok {
				return nil, typeError("'" + pyTypeName(a) + "' object has no attribute 'get'")
			}
			if agent.GetOr("docker_ha_installation_id", nil) == sessionID {
				owned = agent
				break
			}
		}
		if owned != nil {
			for key, value := range expectedFields.All() {
				if !jsonx.Equal(owned.GetOr(key, nil), value) {
					return nil, enrollmentError("本次安装的设备信息已改变，请重新生成命令。", 409)
				}
			}
			id, idOK := owned.Get("id")
			agentToken, tokenOK := owned.Get("token")
			if !idOK || !tokenOK {
				return nil, typeError("KeyError: 'id' or 'token'")
			}
			return jsonx.Obj("agent_id", id, "token", agentToken), nil
		}
		for _, a := range *agents {
			agent := a.(*jsonx.Object)
			if jsonx.Equal(agent.GetOr("name", nil), expectedFields.Value("name")) && jsonx.Equal(agent.GetOr("host", nil), expectedFields.Value("host")) {
				return nil, enrollmentError("该名称与地址已有 Agent；请使用旧容器迁移，不能创建或覆盖其身份。", 409)
			}
		}
		agent := expectedFields.Clone()
		agent.Set("id", "agent_"+uuid4Hex())
		agent.Set("token", haTokenURLSafe(24))
		agent.Set("docker_ha_installation_id", sessionID)
		agent.Set("enabled", true)
		agent.Set("status", "offline")
		agent.Set("last_heartbeat", "")
		agent.Set("version", "unknown")
		agent.Set("config_version", "0")
		agent.Set("created_at", naiveISO(enrollmentLocalNow()))
		*agents = append(*agents, agent)
		return jsonx.Obj("agent_id", agent.Value("id"), "token", agent.Value("token")), nil
	})
	if err != nil {
		return nil, err
	}
	return result.(*jsonx.Object), nil
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

// BootstrapCommand is bootstrap_command(enrollment, request): it uses the
// shipped image's Python; no Python/curl dependency on a NAS host.
func BootstrapCommand(enrollment *Enrollment, request *jsonx.Object) (string, error) {
	dataDir, err := haString(request.GetOr("data_dir", nil))
	if err != nil {
		return "", err
	}
	dataDir = strings.TrimRight(dataDir, "/") + "/agent"
	image, err := haString(request.GetOr("image", DockerHAImage))
	if err != nil {
		return "", err
	}
	network := []string{"--network", "host"}
	if request.GetOr("network_mode", nil) == "macvlan" {
		name, err := haString(request.GetOr("agent_name", "agent"))
		if err != nil {
			return "", err
		}
		ip, err := haString(request.GetOr("agent_ip", nil))
		if err != nil {
			return "", err
		}
		network = []string{"--network", name + "-oxidns-ha-net", "--ip", ip}
	}
	values, err := jsonx.Encode(jsonx.Obj("url", enrollment.URL, "token", enrollment.Token), jsonx.Options{EnsureASCII: true})
	if err != nil {
		return "", err
	}
	source := strings.ReplaceAll(bootstrapSource, "SETTINGS_LITERAL", jsonx.Repr(string(values)))
	return "# 安装凭据仅用于本机 OxiDNS，有效期 24 小时；不包含管理账号或全局注册密钥。\n" +
		"docker run --rm -i " + joinQuoted(network) + " --entrypoint python3 --mount " +
		q("type=bind,src="+dataDir+",dst=/opt/configflow-agent") + " " +
		q(image) + " - <<'CF_DOCKER_HA_ENROLL'\n" + source + "CF_DOCKER_HA_ENROLL\n", nil
}
