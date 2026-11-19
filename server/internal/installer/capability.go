package installer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

// Capability contains the narrowly scoped credentials for one installation.
// ExpiresAt is a JSON number, keeping the persisted numeric token intact.
type Capability struct {
	URL       string      `json:"url"`
	Token     string      `json:"token"`
	ExpiresAt json.Number `json:"expires_at"`
}

// AgentIdentity is the only credential response returned by enrollment.
type AgentIdentity struct {
	AgentID string `json:"agent_id"`
	Token   string `json:"token"`
}

// AgentEditor owns an atomic registry edit and checks shutdown before commit.
type AgentEditor interface {
	EditAgents(context.Context, string, func(context.Context, *[]model.Agent) error) error
}

type installationSession struct {
	fields                                      model.Fields
	id, profileID, name, host, token, tokenHash string
	port                                        json.Number
	expires                                     json.Number
}

func fieldText(fields model.Fields, key string) (string, bool) {
	raw, ok := fields.Get(key)
	if !ok || len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return "", false
	}
	return text, true
}

func fieldNumber(fields model.Fields, key string) (json.Number, bool) {
	raw, ok := fields.Get(key)
	if !ok {
		return "", false
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] == '"' || raw[0] == 'n' {
		return "", false
	}
	var n json.Number
	if json.Unmarshal(raw, &n) != nil {
		return "", false
	}
	if _, err := n.Float64(); err != nil {
		return "", false
	}
	return n, true
}

func setSessionField(fields *model.Fields, name string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return fields.Set(name, raw)
}

func decodeInstallationSession(data []byte) (installationSession, error) {
	var s installationSession
	if err := json.Unmarshal(data, &s.fields); err != nil {
		return s, fmt.Errorf("decode installation: %w", err)
	}
	var ok bool
	for _, field := range []struct {
		key    string
		target *string
	}{
		{"id", &s.id}, {"profile_id", &s.profileID}, {"name", &s.name}, {"host", &s.host},
	} {
		if *field.target, ok = fieldText(s.fields, field.key); !ok {
			return s, errors.New(msgInstallationIdentity)
		}
	}
	if !isSessionID(s.id) {
		return s, errors.New(msgInstallationIdentity)
	}
	if s.port, ok = fieldNumber(s.fields, "port"); !ok {
		return s, errors.New(msgInstallationIdentity)
	}
	if _, err := strconv.ParseUint(s.port.String(), 10, 16); err != nil {
		return s, errors.New(msgInstallationIdentity)
	}
	s.token, _ = fieldText(s.fields, "token")
	s.tokenHash, _ = fieldText(s.fields, "token_hash")
	if s.expires, ok = fieldNumber(s.fields, "expires"); !ok {
		return s, errors.New(msgInstallationExpiry)
	}
	return s, nil
}

func sessionExpired(s installationSession, now float64) bool {
	expiry, err := s.expires.Float64()
	return err != nil || now >= expiry
}

// IssueCapability retains an installation identity across retries. Request keys
// are decoded exactly; unknown members remain part of the historical spec hash.
func IssueCapability(ctx context.Context, dataDir string, request json.RawMessage, profileID string) (*Capability, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := repo.ValidateProfileID(profileID); err != nil {
		return nil, err
	}
	var fields model.Fields
	if err := json.Unmarshal(request, &fields); err != nil {
		return nil, fmt.Errorf("decode installation request: %w", err)
	}
	server, _ := fieldText(fields, "server_url")
	spec := fields
	for _, field := range []struct {
		key   string
		value string
	}{{"mode", "new"}, {"server_url", strings.TrimRight(server, "/")}, {"profile_id", profileID}} {
		if err := setSessionField(&spec, field.key, field.value); err != nil {
			return nil, err
		}
	}
	spec.Delete("action")
	if method, _ := fieldText(spec, "install_method"); method == "compose_compact" {
		for _, key := range []string{"image", "ha_host_id_path", "create_host_identity"} {
			spec.Delete(key)
		}
	}
	digest, err := installationSpecDigest(spec)
	if err != nil {
		return nil, err
	}
	issueMu.Lock()
	defer issueMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root := filepath.Join(dataDir, "docker-ha-installations")
	if err := os.MkdirAll(dataDir, 0777); err != nil {
		return nil, err
	}
	if err := os.Mkdir(root, 0700); err != nil {
		if info, statErr := os.Stat(root); statErr != nil || !info.IsDir() {
			return nil, err
		}
	}
	index := filepath.Join(root, digest+".index")
	indexed, err := os.ReadFile(index)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	var session installationSession
	if exists {
		if !isSessionID(string(indexed)) {
			return nil, enrollmentError(enrollmentUnreadable, 409)
		}
		data, err := os.ReadFile(filepath.Join(root, string(indexed)+".json"))
		if err != nil {
			return nil, enrollmentError(enrollmentUnreadable, 409)
		}
		session, err = decodeInstallationSession(data)
		if err != nil || session.id != string(indexed) {
			return nil, enrollmentError(enrollmentUnreadable, 409)
		}
	} else {
		name, ok := fieldText(fields, "agent_name")
		if !fields.Has("agent_name") {
			name, ok = "agent", true
		}
		if !ok {
			return nil, errors.New(msgInstallationIdentity)
		}
		host, ok := fieldText(fields, "agent_ip")
		if !ok {
			return nil, errors.New(msgInstallationIdentity)
		}
		port, ok := fieldNumber(fields, "oxidns_port")
		if !fields.Has("oxidns_port") {
			port, ok = "8082", true
		}
		n, err := strconv.ParseUint(port.String(), 10, 16)
		if !ok || err != nil || n < 1024 {
			return nil, errors.New(msgInstallationIdentity)
		}
		session = installationSession{id: uuid4Hex(), profileID: profileID, name: name + "-oxidns", host: host, port: port, expires: "0"}
		for _, field := range []struct {
			key   string
			value any
		}{
			{"id", session.id}, {"profile_id", profileID}, {"name", session.name}, {"host", host}, {"port", port},
		} {
			if err := setSessionField(&session.fields, field.key, field.value); err != nil {
				return nil, err
			}
		}
	}
	if sessionExpired(session, enrollmentNow()) {
		session.token = haTokenURLSafe(32)
		sum := sha256.Sum256([]byte(session.token))
		session.tokenHash = hex.EncodeToString(sum[:])
		session.expires = json.Number(strconv.FormatInt(int64(enrollmentNow())+EnrollmentTTL, 10))
		for _, field := range []struct {
			key   string
			value any
		}{
			{"token", session.token}, {"token_hash", session.tokenHash}, {"expires", session.expires},
		} {
			if err := setSessionField(&session.fields, field.key, field.value); err != nil {
				return nil, err
			}
		}
		data, err := installationSessionJSON(session.fields)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path := filepath.Join(root, session.id+".json")
		temp := filepath.Join(root, session.id+".tmp")
		if err := os.WriteFile(temp, data, 0600); err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			_ = os.Remove(temp)
			return nil, err
		}
		if err := os.Rename(temp, path); err != nil {
			return nil, err
		}
	}
	if !isEnrollmentToken(session.token) {
		return nil, errors.New(msgInstallationIdentity)
	}
	if !exists {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(index, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return nil, err
		}
		_, err = f.WriteString(session.id)
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return nil, err
		}
	}
	return &Capability{URL: strings.TrimRight(server, "/") + "/api/agents/docker-oxidns-ha-enroll/" + session.id, Token: session.token, ExpiresAt: session.expires}, nil
}

// EnrollIdentity creates or returns exactly the identity bound to a persisted
// capability. Original string tokens are required for every ownership field.
func EnrollIdentity(ctx context.Context, dataDir string, manager AgentEditor, sessionID, token string, present bool) (AgentIdentity, error) {
	ctx = lifecycle.Detach(ctx)
	result := AgentIdentity{}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !isSessionID(sessionID) || !present || !isEnrollmentToken(token) {
		return result, enrollmentError(enrollmentInvalid, 401)
	}
	data, err := os.ReadFile(filepath.Join(dataDir, "docker-ha-installations", sessionID+".json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return result, enrollmentError(enrollmentInvalid, 401)
		}
		return result, err
	}
	session, err := decodeInstallationSession(data)
	if err != nil || session.id != sessionID {
		return result, enrollmentError(enrollmentInvalid, 401)
	}
	if sessionExpired(session, enrollmentNow()) {
		return result, enrollmentError(enrollmentExpired, 401)
	}
	sum := sha256.Sum256([]byte(token))
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(session.tokenHash)) != 1 {
		return result, enrollmentError(enrollmentExpired, 401)
	}
	if err := repo.ValidateProfileID(session.profileID); err != nil {
		return result, err
	}
	if manager == nil {
		return result, errors.New("installation registry is unavailable")
	}
	err = manager.EditAgents(ctx, session.profileID, func(tx context.Context, records *[]model.Agent) error {
		if err := tx.Err(); err != nil {
			return err
		}
		expected := map[string]string{"name": session.name, "host": session.host, "profile_id": session.profileID, "service_type": "oxidns", "deployment_method": "docker"}
		for i := range *records {
			a := &(*records)[i]
			marker, ok := originalAgentText(a, "docker_ha_installation_id")
			if !ok || marker != sessionID {
				continue
			}
			for key, value := range expected {
				original, ok := originalAgentText(a, key)
				if !ok || original != value {
					return enrollmentError(msgInstallationChanged, 409)
				}
			}
			raw, present, err := a.MemberJSON("port")
			if err != nil || !present || !numericPortEqual(raw, session.port) {
				return enrollmentError(msgInstallationChanged, 409)
			}
			id, idOK := originalAgentText(a, "id")
			credential, tokenOK := originalAgentText(a, "token")
			if !idOK || !tokenOK || id == "" || credential == "" {
				return enrollmentError(msgInstallationChanged, 409)
			}
			result = AgentIdentity{AgentID: id, Token: credential}
			return tx.Err()
		}
		for i := range *records {
			a := &(*records)[i]
			name, nameOK := originalAgentText(a, "name")
			host, hostOK := originalAgentText(a, "host")
			if nameOK && hostOK && name == session.name && host == session.host {
				return enrollmentError(msgInstallationCollision, 409)
			}
		}
		a := model.Agent{}
		id, credential := "agent_"+uuid4Hex(), haTokenURLSafe(24)
		for _, field := range []struct {
			key   string
			value any
		}{
			{"name", session.name}, {"host", session.host}, {"port", session.port}, {"profile_id", session.profileID},
			{"service_type", "oxidns"}, {"deployment_method", "docker"}, {"id", id}, {"token", credential},
			{"docker_ha_installation_id", sessionID}, {"enabled", true}, {"status", "offline"}, {"last_heartbeat", ""},
			{"version", "unknown"}, {"config_version", "0"}, {"created_at", naiveISO(enrollmentLocalNow())},
		} {
			raw, err := json.Marshal(field.value)
			if err != nil {
				return err
			}
			if err := a.SetJSON(field.key, raw); err != nil {
				return err
			}
		}
		*records = append(*records, a)
		result = AgentIdentity{AgentID: id, Token: credential}
		return tx.Err()
	})
	if err != nil {
		return AgentIdentity{}, err
	}
	return result, nil
}

func originalAgentText(a *model.Agent, key string) (string, bool) {
	raw, present, err := a.MemberJSON(key)
	if err != nil || !present || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '"' {
		return "", false
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return "", false
	}
	return text, true
}

func numericPortEqual(raw json.RawMessage, want json.Number) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] == '"' || raw[0] == 't' || raw[0] == 'f' || raw[0] == 'n' {
		return false
	}
	var number json.Number
	if json.Unmarshal(raw, &number) != nil {
		return false
	}
	got, err := strconv.ParseFloat(number.String(), 64)
	expected, wantErr := want.Float64()
	return err == nil && wantErr == nil && got == expected
}
