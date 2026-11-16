// Package auth is the port of backend/common/auth.py and the global
// authentication gate of backend/routes/auth.py (setup_before_request):
// HS256 login tokens interchangeable with PyJWT, strict Bearer parsing,
// require_auth, validate_token_or_jwt for client configuration pulls, and the
// list of public paths and self-authenticating endpoints.
//
// Configuration comes from the environment once at startup (Configure, or
// FromEnv in main); tests call Configure with their own values.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"math/big"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

// Constants of common/auth.py.
const (
	Algorithm           = "HS256"
	ExpirationHours     = 24
	MaxAuthTokenLength  = 8192
	invalidOrMissingMsg = "Invalid or missing authentication"
)

// Config is the authentication setup read from the environment.
type Config struct {
	// AdminUsername and AdminPassword enable login when both are set.
	AdminUsername string
	AdminPassword string
	// SecretKey signs the login tokens: JWT_SECRET_KEY, else random per
	// process (tokens then do not survive a restart).
	SecretKey string
	// Now is the clock (tests); nil means time.Now.
	Now func() time.Time
}

var current atomic.Pointer[Config]

func init() { current.Store(&Config{SecretKey: randomSecret()}) }

// FromEnv reads ADMIN_USERNAME, ADMIN_PASSWORD and JWT_SECRET_KEY.
func FromEnv() Config {
	secret := os.Getenv("JWT_SECRET_KEY")
	if secret == "" {
		secret = randomSecret()
	}
	return Config{
		AdminUsername: os.Getenv("ADMIN_USERNAME"),
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),
		SecretKey:     secret,
	}
}

// Configure installs the configuration and returns the previous one.
func Configure(c Config) Config {
	if c.SecretKey == "" {
		c.SecretKey = randomSecret()
	}
	previous := current.Swap(&c)
	return *previous
}

// Current returns the installed configuration.
func Current() Config { return *current.Load() }

// randomSecret is secrets.token_urlsafe(48).
func randomSecret() string {
	b := make([]byte, 48)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func now() time.Time {
	if c := current.Load(); c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// IsAuthEnabled is is_auth_enabled(): both username and password are set.
func IsAuthEnabled() bool {
	c := current.Load()
	return c.AdminUsername != "" && c.AdminPassword != ""
}

// ParseBearerToken is parse_bearer_token: the token of "Bearer <token>"
// when it is 1–8192 printable ASCII characters without spaces, else "", false.
func ParseBearerToken(header string, present bool) (string, bool) {
	if !present || !strings.HasPrefix(header, "Bearer ") {
		return "", false
	}
	token := header[len("Bearer "):]
	if token == "" || len([]rune(token)) > MaxAuthTokenLength {
		return "", false
	}
	for _, r := range token {
		if r < 0x21 || r > 0x7e {
			return "", false
		}
	}
	return token, true
}

// IsTokenWithinLength is is_token_within_length: a non-empty string of at
// most 8192 characters.
func IsTokenWithinLength(token string) bool {
	n := len([]rune(token))
	return n > 0 && n <= MaxAuthTokenLength
}

// GenerateToken is generate_token(username): {"username", "exp": now+24h,
// "iat": now} as integer UTC seconds.
func GenerateToken(username string) (string, error) {
	t := now().UTC()
	claims := jsonx.Obj(
		"username", username,
		"exp", t.Add(ExpirationHours*time.Hour).Unix(),
		"iat", t.Unix(),
	)
	return EncodeHS256(claims, current.Load().SecretKey)
}

// Verification is the result of verify_token: the claims, or Python's
// {'error': 'expired'} / {'error': 'invalid', 'detail': str(exc)}.
type Verification struct {
	Claims *jsonx.Object
	// Error is "" (decoded), "expired" or "invalid".
	Error string
	// Detail is str(exc) for "invalid".
	Detail string
}

// Failed is `not payload or 'error' in payload`: no claims, empty claims,
// a decoding error, or claims that themselves carry an "error" key (Python
// cannot tell those from its own error dict).
func (v Verification) Failed() bool {
	if v.Error != "" || !jsonx.Truthy(v.Claims) {
		return true
	}
	_, hasError := v.Claims.Get("error")
	return hasError
}

// errorCode and detail are payload['error'] and payload.get('detail', ...).
func (v Verification) errorCode() any {
	if v.Error != "" {
		return v.Error
	}
	value, _ := v.Claims.Get("error")
	return value
}

func (v Verification) detail() string {
	if v.Error != "" {
		if v.Error == "invalid" {
			return v.Detail
		}
		return "unknown error"
	}
	if value, ok := v.Claims.Get("detail"); ok {
		return jsonx.Str(value)
	}
	return "unknown error"
}

// VerifyToken is verify_token(token). A *ClaimTypeError (PyJWT's uncaught
// TypeError) is returned as an error: the caller lets it become a 500.
func VerifyToken(token string) (Verification, error) {
	claims, err := DecodeHS256(token, current.Load().SecretKey, now())
	if err == nil {
		return Verification{Claims: claims}, nil
	}
	if te, ok := err.(*TokenError); ok {
		if te.Expired {
			return Verification{Error: "expired"}, nil
		}
		return Verification{Error: "invalid", Detail: te.Message}, nil
	}
	return Verification{}, err
}

func bearerOf(c *httpx.Ctx) (string, bool) {
	header, present := c.Header("Authorization")
	return ParseBearerToken(header, present)
}

// Require is the require_auth decorator: internal calls and auth-disabled
// mode pass; otherwise a valid Bearer JWT is required.
func Require(next httpx.HandlerFunc) httpx.HandlerFunc {
	return func(c *httpx.Ctx) (*httpx.Response, error) {
		if c.IsInternalCall() || !IsAuthEnabled() {
			return next(c)
		}
		token, ok := bearerOf(c)
		if !ok {
			return httpx.Message(401, false, "缺少有效的登录凭据，请重新登录。"), nil
		}
		result, err := VerifyToken(token)
		if err != nil {
			return nil, err
		}
		if !jsonx.Truthy(result.Claims) && result.Error == "" {
			return httpx.Message(401, false, "登录已失效，请重新登录。"), nil
		}
		if result.Failed() {
			if result.errorCode() == "expired" {
				return httpx.JSONStatus(401, jsonx.Obj("success", false, "message", "登录已过期，请重新登录。", "error", "token_expired")), nil
			}
			return httpx.JSONStatus(401, jsonx.Obj("success", false,
				"message", "登录凭据无效，请重新登录。原始错误："+result.detail(), "error", "token_invalid")), nil
		}
		return next(c)
	}
}

// ValidateResult is the {'valid': bool, 'message': str} of
// validate_token_or_jwt.
type ValidateResult struct {
	Valid   bool
	Message string
}

// ValidateTokenOrJWT is validate_token_or_jwt(request, config): accept an
// internal call, a valid Bearer JWT, or ?token= equal to the configured
// config token; with auth disabled and no config token everything passes.
// The internal rule proxy tokens (current and retired) never authenticate.
// config is the system document (get_system_config()); nil reads it.
func ValidateTokenOrJWT(c *httpx.Ctx, config *jsonx.Object) (ValidateResult, error) {
	if c.IsInternalCall() {
		return ValidateResult{Valid: true}, nil
	}
	if config == nil {
		var err error
		if config, err = repo.GetSystemConfig(c.Context()); err != nil {
			return ValidateResult{}, err
		}
	}
	systemConfig, _ := config.GetOr("system_config", jsonx.NewObject()).(*jsonx.Object)
	if !jsonx.Truthy(systemConfig) {
		systemConfig = jsonx.NewObject()
	}
	configToken := systemConfig.GetOr("config_token", "")
	ruleProxyToken := systemConfig.GetOr("rule_proxy_token", "")
	retired, _ := systemConfig.GetOr("retired_rule_proxy_tokens", []any{}).([]any)
	bearer, hasBearer := bearerOf(c)
	urlToken, _ := c.Arg("token")
	if !IsTokenWithinLength(urlToken) {
		urlToken = ""
	}
	internalMatch := 0
	for _, token := range append([]any{ruleProxyToken}, retired...) {
		if text, ok := token.(string); ok && text != "" {
			// Compare fixed-size digests so credential contents and lengths do not
			// short-circuit secret comparisons. Check every retired capability.
			internalMatch |= equalCredential(bearer, text) | equalCredential(urlToken, text)
		}
	}
	if internalMatch != 0 {
		return ValidateResult{Message: invalidOrMissingMsg}, nil
	}
	if !IsAuthEnabled() && !jsonx.Truthy(configToken) {
		return ValidateResult{Valid: true}, nil
	}
	if hasBearer {
		result, err := VerifyToken(bearer)
		if err != nil {
			return ValidateResult{}, err
		}
		if !result.Failed() {
			return ValidateResult{Valid: true}, nil
		}
	}
	if expected, ok := configToken.(string); ok && urlToken != "" && equalCredential(urlToken, expected) == 1 {
		return ValidateResult{Valid: true}, nil
	}
	return ValidateResult{Message: invalidOrMissingMsg}, nil
}

// equalCredential compares secrets without leaking their matching prefix or length.
func equalCredential(candidate, expected string) int {
	a := sha256.Sum256([]byte(candidate))
	b := sha256.Sum256([]byte(expected))
	return subtle.ConstantTimeCompare(a[:], b[:])
}

var publicExactPaths = map[string]bool{
	"/api/auth/status": true, "/api/auth/login": true, "/api/config/mihomo": true, "/api/config/surge": true,
	"/api/config/loon": true, "/api/config/mosdns": true, "/api/config/oxidns": true,
	"/api/mosdns/rule-proxy": true,
	"/api/agents/register":   true, "/api/agents/install-script": true, "/api/agents/core-install-script": true,
	"/api/agents/docker-compose": true, "/api/agents/docker-run": true,
	"/api/agents/gateway-network-probe.sh": true,
	"/api/agents/docker-mihomo-compose":    true, "/api/agents/docker-mihomo-run": true,
	"/api/agents/docker-mosdns-compose": true, "/api/agents/docker-mosdns-run": true,
	"/api/version": true, "/": true, "/mcp": true,
}

var publicPrefixes = []string{"/api/rule-library/content/", "/api/rules/local/", "/mcp/", "/api/static/agents/", "/assets/", "/static/"}

// Endpoints that never need a panel JWT at the gate: installer downloads,
// short-lived session capabilities, and routes that check Agent credentials
// themselves.
var publicEndpoints = map[string]bool{
	"agents.download_agent_binary": true, "agents.download_install_command": true,
	"agents.gateway_installation_script": true, "agents.gateway_installation_enroll": true,
	"agents.docker_oxidns_ha_enroll": true,
	"agents.agent_heartbeat":         true, "agents.agent_traffic_report": true, "agents.agent_traffic_history_report": true,
	"agents.get_agent_config":  true,
	"agents.get_agent_upgrade": true, "agents.report_agent_upgrade": true,
	"custom_config.linux_gateway_rule_provider": true, "profiles.linux_gateway_rule_provider": true,
	"custom_config.linux_gateway_source_provider": true, "profiles.linux_gateway_source_provider": true,
}

var profilePublicPatterns = []*regexp.Regexp{
	// Python's re.match with "$", which also matches before a final newline.
	regexp.MustCompile(`^/api/config/[A-Za-z0-9][A-Za-z0-9_-]{0,63}/(?:mihomo|surge|loon|mosdns|oxidns)\n?\z`),
	regexp.MustCompile(`^/api/config/[A-Za-z0-9][A-Za-z0-9_-]{0,63}/loon/[^/]+\n?\z`),
	regexp.MustCompile(`^/api/profiles/[A-Za-z0-9][A-Za-z0-9_-]{0,63}/mosdns/rule-proxy\n?\z`),
	regexp.MustCompile(`^/api/profiles/[A-Za-z0-9][A-Za-z0-9_-]{0,63}/(?:subscriptions/[^/]+/proxies|aggregations/[^/]+/provider|rules/local/[^/]+|rule-library/content/[^/]+)\n?\z`),
}

// IsPublicPath reports whether the gate lets a path through without a JWT.
func IsPublicPath(path string) bool {
	if publicExactPaths[path] {
		return true
	}
	for _, prefix := range publicPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	for _, pattern := range profilePublicPatterns {
		if pattern.MatchString(path) {
			return true
		}
	}
	return false
}

// Gate is setup_before_request's before_auth: with authentication enabled,
// every request that is not internal, not to a public path and not to a
// self-authenticating endpoint needs a valid Bearer JWT.
func Gate(c *httpx.Ctx) (*httpx.Response, error) {
	if c.IsInternalCall() || !IsAuthEnabled() {
		return nil, nil
	}
	if publicEndpoints[c.Endpoint] || IsPublicPath(c.Path()) {
		return nil, nil
	}
	token, ok := bearerOf(c)
	if !ok {
		return httpx.Message(401, false, "请先登录后再操作。"), nil
	}
	result, err := VerifyToken(token)
	if err != nil {
		return nil, err
	}
	if result.Failed() {
		return httpx.Message(401, false, "登录已失效，请重新登录。"), nil
	}
	return nil, nil
}

// Install adds the gate to the app, right after the profile context (the
// position of setup_before_request in backend/app.py).
func Install(app *httpx.App) { app.InsertBeforeRequest(1, Gate) }

// pyParseInt and pyTypeName forward to httpx's Python conversions.
func pyParseInt(s string) (*big.Int, bool) { return httpx.PyParseInt(s) }
func pyTypeName(v any) string              { return httpx.PyTypeName(v) }
