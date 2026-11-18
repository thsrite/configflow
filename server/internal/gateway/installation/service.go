package installation

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"

	"configflow/internal/agents"
	"configflow/internal/gateway/host"
	"configflow/internal/installer"
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/logx"
	"configflow/internal/redact"
	"configflow/internal/repo"
)

var logger = logx.Get("backend.agents.gateway_installation")

// Service composes the existing installation, host and publication owners.
// Render is an optional immutable collaborator; nil uses the real installer.
// It holds no request context or mutable session state.
type Service struct {
	Repository *repo.Repository
	Manager    *agents.Manager
	Spawner    lockx.Spawner
	Render     func(installer.GatewayOptions) (string, error)
}

func (s *Service) now(ctx context.Context) time.Time { return lifecycle.ClockFrom(ctx).Now() }
func random(n int, hexadecimal bool) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("installation random capability: %w", err)
	}
	if hexadecimal {
		return hex.EncodeToString(b), nil
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func (s *Service) authorize(ctx context.Context, r *Record, token string) error {
	if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(r.Token)) != 1 || float64(s.now(ctx).UnixNano())/1e9 >= r.Expires {
		return failure(msgAuth, 401)
	}
	return nil
}
func serverURL(value string) (string, error) {
	if value == "" || len(value) > 2048 || strings.ContainsFunc(value, func(c rune) bool { return c < 33 }) {
		return "", failure(msgServer, 400)
	}
	u, err := url.Parse(value)
	if err != nil {
		return "", failure(msgServerInvalid, 400)
	}
	if u.Scheme != "http" && u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", failure(msgServerShape, 400)
	}
	if p := u.Port(); p != "" {
		var n int
		if _, err := fmt.Sscan(p, &n); err != nil || n > 65535 {
			return "", failure(msgServerInvalid, 400)
		}
	}
	h := strings.ToLower(u.Hostname())
	if h == "localhost" || h == "0.0.0.0" || h == "::" || h == "::1" || strings.HasPrefix(h, "127.") {
		return "", failure(msgServerLocal, 400)
	}
	return strings.TrimRight(value, "/"), nil
}
func hostIPv4(value string) (string, error) {
	ip, err := netip.ParseAddr(value)
	if err != nil || !ip.Is4() || ip.IsLoopback() || ip.IsMulticast() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.String() == "255.255.255.255" {
		return "", failure(msgHost, 400)
	}
	return ip.String(), nil
}

var validName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,48}$`)

func only(o map[string]json.RawMessage, keys ...string) bool {
	if o == nil {
		return false
	}
	for k := range o {
		found := false
		for _, v := range keys {
			if k == v {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
func boolMember(o map[string]json.RawMessage, k string) (bool, bool) {
	b, ok := o[k]
	if !ok {
		return false, true
	}
	return flag(b), string(b) == "true" || string(b) == "false"
}

// Create validates the saved two-core draft before issuing an idempotent capability.
func (s *Service) Create(ctx context.Context, body json.RawMessage) (json.RawMessage, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	o := object(body)
	if !only(o, "profile_id", "name", "server_url", "network", "config_revision", "mihomo_agent_port", "oxidns_agent_port", "auto_network", "agent_ip", "use_as_server_domain") {
		return nil, failure(msgParams, 400)
	}
	profile := text(o["profile_id"])
	if err := validateProfile(profile); err != nil {
		return nil, err
	}
	var revision int64
	if json.Unmarshal(o["config_revision"], &revision) != nil || string(o["config_revision"]) == "null" || revision < 0 {
		return nil, failure(msgRevision, 400)
	}
	name := text(o["name"])
	if !validName.MatchString(name) {
		return nil, failure(msgName, 400)
	}
	automatic, ok := boolMember(o, "auto_network")
	if !ok {
		return nil, failure(msgAuto, 400)
	}
	var network host.Spec
	requested := ""
	var err error
	if automatic {
		if o["network"] != nil {
			return nil, failure(msgAutoFields, 400)
		}
		if v, present := o["agent_ip"]; present {
			if json.Unmarshal(v, &requested) != nil || string(v) == "null" {
				return nil, failure(msgIPType, 400)
			}
		}
		requested = strings.TrimSpace(requested)
		if requested != "" {
			requested, err = hostIPv4(requested)
			if err != nil {
				return nil, err
			}
		}
		network.Address = requested
	} else {
		if o["agent_ip"] != nil {
			return nil, failure(msgManual, 400)
		}
		network, err = host.ParseSpec(o["network"])
		if err != nil {
			return nil, publicOwnerError(err)
		}
	}
	ports := map[string]int{"mihomo": 8080, "oxidns": 8082}
	for _, service := range services {
		if b, present := o[service+"_agent_port"]; present {
			var portsValue int
			if json.Unmarshal(b, &portsValue) != nil {
				return nil, failure(msgPorts, 400)
			}
			ports[service] = portsValue
		}
	}
	for _, p := range ports {
		if p < 1024 || p > 65535 || p == 1053 {
			return nil, failure(msgPorts, 400)
		}
	}
	if ports["mihomo"] == ports["oxidns"] {
		return nil, failure(msgPorts, 400)
	}
	if err = s.checkPorts(ctx, profile, ports); err != nil {
		return nil, err
	}
	origin, err := serverURL(text(o["server_url"]))
	if err != nil {
		return nil, err
	}
	useDomain, ok := boolMember(o, "use_as_server_domain")
	if !ok {
		return nil, failure(msgUseDomain, 400)
	}
	if useDomain {
		if err = s.setDomain(ctx, origin); err != nil {
			return nil, err
		}
	}
	id, err := random(16, true)
	if err != nil {
		return nil, err
	}
	token, err := random(32, false)
	if err != nil {
		return nil, err
	}
	now := s.now(ctx)
	r := &Record{ID: id, ProfileID: profile, ConfigRevision: revision, Name: name, ServerURL: origin, Network: network, Ports: ports, Token: token, CreatedAt: isoTimestamp(float64(now.UnixNano()) / 1e9), Expires: float64(now.UnixNano())/1e9 + 86400, Phase: "waiting", AgentIDs: map[string]string{}, Registered: map[string]string{}, Deployments: map[string]json.RawMessage{}, Message: msgWaiting, AutoNetwork: automatic, RequestedIP: requested}
	if _, mode, e := s.savedSnapshot(ctx, r); e != nil {
		return nil, e
	} else {
		r.Takeover = mode
	}
	ctx, release, err := creates.Lock(ctx, s.root(), repo.LockTimeout())
	if err != nil {
		return nil, err
	}
	defer release()
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	records, err := s.records(ctx, profile)
	if err != nil {
		return nil, err
	}
	if len(records) > 0 {
		old := records[0]
		view, e := s.public(ctx, old)
		if e != nil {
			return nil, e
		}
		v := object(view)
		if !flag(v["expired"]) && !flag(v["target_changed"]) {
			mode := old.Takeover
			if mode == "" {
				mode = "selective"
			}
			same := old.AutoNetwork == automatic && old.Name == name && old.ServerURL == origin && old.Ports["mihomo"] == ports["mihomo"] && old.Ports["oxidns"] == ports["oxidns"] && mode == r.Takeover
			if automatic {
				same = same && (requested == old.RequestedIP || requested == old.Network.Address)
			} else {
				same = same && old.Network == network
			}
			if same {
				changedRevision := old.ConfigRevision != revision
				err = s.locked(ctx, old.ID, func(ctx context.Context) error {
					current, e := s.read(ctx, old.ID)
					if e != nil {
						return e
					}
					if current.ConfigRevision != revision {
						if current.Phase == "publishing" {
							return failure(msgPublishingRevision, 409)
						}
						current.ConfigRevision = revision
						current.Deployments = map[string]json.RawMessage{}
						current.Phase = "waiting"
						current.Message = msgReuseWaiting
						if len(current.AgentIDs) == 2 {
							current.Phase = "enrolled"
							current.Message = msgReuseEnrolled
						}
					}
					if e = s.ensureLink(ctx, current); e != nil {
						return e
					}
					if e = s.write(ctx, current); e != nil {
						return e
					}
					old = current
					return nil
				})
				if err != nil {
					return nil, err
				}
				if changedRevision {
					logger.Infof(msgReuseLog, s.safeLabel(ctx, old), revision)
				}
				return s.public(ctx, old)
			}
		}
	}
	render := s.Render
	if render == nil {
		render = installer.GenerateGatewayInstallScript
	}
	r.Script, err = render(installer.GatewayOptions{ServerURL: origin, AgentName: name, AgentIP: network.Address, BinaryDownloadURL: origin + "/api/agents/download", MihomoAgentPort: ports["mihomo"], OxidnsAgentPort: ports["oxidns"], EnrollmentURL: origin + "/api/agents/gateway-installations/" + id + "/enroll", EnrollmentToken: token, InstallationID: id, FullTun: r.Takeover == "full_tun"})
	if err != nil {
		return nil, publicOwnerError(err)
	}
	if err = s.ensureLink(ctx, r); err != nil {
		return nil, err
	}
	if err = s.write(ctx, r); err != nil {
		return nil, err
	}
	logger.Infof(msgCreatedLog, s.safeLabel(ctx, r), r.Network.Address)
	return s.public(ctx, r)
}
func (s *Service) ensureLink(ctx context.Context, r *Record) error {
	if r.DownloadPath != "" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	store := installer.Store{DataDir: s.Repository.DataDir()}
	expires := int64(r.Expires)
	token, _, err := store.SaveInstallScript(ctx, installer.WithRootPrivileges(r.Script), &expires)
	if errors.Is(err, installer.ErrStoreFull) {
		return failure(msgLinkFull, 503)
	}
	if err != nil {
		return err
	}
	r.DownloadPath = "/api/agents/i/" + token
	return nil
}

// Script returns only the script authorized by this exact capability.
func (s *Service) Script(ctx context.Context, id, token string) (string, error) {
	r, err := s.read(ctx, id)
	if err != nil {
		return "", err
	}
	if err = s.authorize(ctx, r, token); err != nil {
		return "", err
	}
	return installer.WithRootPrivileges(r.Script), nil
}

// Get projects a private record into an administrator-safe status.
func (s *Service) Get(ctx context.Context, id string) (json.RawMessage, error) {
	r, err := s.read(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.public(ctx, r)
}

// Latest returns the most recent installation for a validated profile.
func (s *Service) Latest(ctx context.Context, profile string) (json.RawMessage, error) {
	if err := validateProfile(profile); err != nil {
		return nil, err
	}
	records, err := s.records(ctx, profile)
	if err != nil {
		return nil, err
	}
	var view json.RawMessage = raw(nil)
	if len(records) > 0 {
		view, err = s.public(ctx, records[0])
	}
	if err != nil {
		return nil, err
	}
	return raw(struct {
		Success bool            `json:"success"`
		Session json.RawMessage `json:"session"`
	}{true, view}), nil
}
func (s *Service) public(ctx context.Context, r *Record) (json.RawMessage, error) {
	revision, err := s.Repository.ConfigRevision(ctx, r.ProfileID)
	if err != nil {
		return nil, err
	}
	agentsView := map[string]json.RawMessage{}
	changed := false
	for service, id := range r.AgentIDs {
		a, e := s.agent(ctx, id)
		if e != nil {
			return nil, e
		}
		if a != nil {
			agentsView[service] = a.public
		}
		if match(r, a, service, true) != nil {
			changed = true
		} else if r.Phase == "published" {
			if taskOf(r.Deployments[service]).DeploymentID != a.Latest.DeploymentID || a.Latest.Status != "succeeded" {
				changed = true
			}
		}
	}
	expired := float64(s.now(ctx).UnixNano())/1e9 >= r.Expires
	v := map[string]json.RawMessage{"success": raw(true), "id": raw(r.ID), "profile_id": raw(r.ProfileID), "config_revision": raw(r.ConfigRevision), "name": raw(r.Name), "network": raw(r.Network), "phase": raw(r.Phase), "created_at": raw(r.CreatedAt), "deployments": raw(r.Deployments), "message": raw(r.Message), "remedy": r.Remedy, "server_url": raw(r.ServerURL), "auto_network": raw(r.AutoNetwork), "takeover": raw(r.Takeover), "expires_at": raw(isoTimestamp(r.Expires)), "expired": raw(expired), "agents": raw(agentsView), "config_changed": raw(revision != r.ConfigRevision), "target_changed": raw(changed)}
	if len(v["remedy"]) == 0 {
		v["remedy"] = raw(nil)
	}
	if r.Takeover == "" {
		v["takeover"] = raw("selective")
	}
	// Metadata receives credential-aware redaction, while the explicitly scoped
	// download command keeps its own installation capability usable.
	secrets, err := s.secrets(ctx)
	if err != nil {
		return nil, err
	}
	safe := v
	safe["message"], err = s.safeMetadataText(ctx, safe["message"])
	if err != nil {
		return nil, err
	}
	publicTasks := map[string]json.RawMessage{}
	for service, task := range r.Deployments {
		publicTasks[service], err = s.safeMetadata(ctx, task)
		if err != nil {
			return nil, err
		}
	}
	safe["deployments"] = raw(publicTasks)
	if !expired {
		scriptURL := r.ServerURL + "/api/agents/gateway-installations/" + r.ID + "/script?" + url.Values{"token": {r.Token}}.Encode()
		download := scriptURL
		if r.DownloadPath != "" {
			download = r.ServerURL + r.DownloadPath
		}
		safe["download_url"] = raw(download)
		cmd := `set -eu; f=$(mktemp); trap 'rm -f "$f"' EXIT; if command -v curl >/dev/null 2>&1; then curl -fSL ` + shellQuote(scriptURL) + ` -o "$f"; else wget -O "$f" ` + shellQuote(scriptURL) + `; fi; if [ "$(id -u)" -eq 0 ]; then sh "$f"; else sudo sh "$f"; fi`
		safe["command"] = raw("sh -c " + shellQuote(cmd))
	}
	return json.Marshal(redact.SanitizeWithSecrets(safe, secrets, true))
}
func shellQuote(v string) string { return "'" + strings.ReplaceAll(v, "'", `'"'"'`) + "'" }

// Stored Unix seconds are rounded to the public timestamp's microsecond
// precision; splitting avoids loss of precision from scaling the whole epoch.
func isoTimestamp(value float64) string {
	seconds, fraction := math.Modf(value)
	at := time.Unix(int64(seconds), int64(math.RoundToEven(fraction*1e6))*1000).UTC()
	if at.Nanosecond() == 0 {
		return at.Format("2006-01-02T15:04:05+00:00")
	}
	return at.Format("2006-01-02T15:04:05.000000+00:00")
}

func (s *Service) safeLabel(ctx context.Context, r *Record) string {
	value, err := s.safeText(ctx, r.Name)
	if err != nil {
		return r.ID[:8]
	}
	return value
}
