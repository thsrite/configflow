package installer

import (
	"bytes"
	"configflow/internal/fsx"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"configflow/internal/jsondecode"
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
	raw = bytes.TrimSpace(raw)
	if !ok || len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var text string
	if jsondecode.Unmarshal(raw, &text) != nil {
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
	if jsondecode.Unmarshal(raw, &n) != nil {
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
	if err := jsondecode.Unmarshal(data, &s.fields); err != nil {
		return s, fmt.Errorf("decode installation: %w", err)
	}
	var ok bool
	if s.id, ok = fieldText(s.fields, "id"); !ok || !isSessionID(s.id) {
		return s, errors.New(msgInstallationIdentity)
	}
	s.profileID, _ = fieldText(s.fields, "profile_id")
	s.name, _ = fieldText(s.fields, "name")
	s.host, _ = fieldText(s.fields, "host")
	s.port, _ = fieldNumber(s.fields, "port")
	s.token, _ = fieldText(s.fields, "token")
	s.tokenHash, _ = fieldText(s.fields, "token_hash")
	s.expires, _ = fieldNumber(s.fields, "expires")

	return s, nil
}

func sessionExpired(s installationSession, now float64, allowMissing bool) (bool, error) {
	raw, present := s.fields.Get("expires")
	if !present {
		if allowMissing {
			return true, nil
		}
		return false, renderFailed(msgInstallationExpiry)
	}
	text := string(bytes.TrimSpace(raw))
	if text == "true" {
		text = "1"
	} else if text == "false" {
		text = "0"
	}
	if len(text) == 0 || text[0] == '"' {
		return false, renderFailed(msgInstallationExpiry)
	}
	expiry, valid := new(big.Rat).SetString(text)
	if !valid {
		return false, renderFailed(msgInstallationExpiry)
	}
	return expiry.Cmp(new(big.Rat).SetFloat64(now)) <= 0, nil
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
	if err := jsondecode.Unmarshal(request, &fields); err != nil {
		return nil, fmt.Errorf("decode installation request: %w", err)
	}
	name, nameOK := fieldText(fields, "agent_name")
	if !fields.Has("agent_name") {
		name, nameOK = "agent", true
	}
	host, hostOK := fieldText(fields, "agent_ip")
	port, portOK := fieldNumber(fields, "oxidns_port")
	if !fields.Has("oxidns_port") {
		port, portOK = "8082", true
	}
	portValue, portErr := strconv.ParseUint(port.String(), 10, 16)
	if !nameOK || !hostOK || !portOK || portErr != nil || portValue < 1024 {
		return nil, renderFailed(msgInstallationIdentity)
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
	if err := ctx.Err(); err != nil {
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
			if errors.Is(err, os.ErrNotExist) {
				return nil, enrollmentError(enrollmentUnreadable, 409)
			}
			return nil, err
		}
		session, err = decodeInstallationSession(data)
		if err != nil || session.id != string(indexed) {
			return nil, enrollmentError(enrollmentUnreadable, 409)
		}
	} else {
		id, err := uuid4Hex()
		if err != nil {
			return nil, err
		}
		session = installationSession{id: id, profileID: profileID, name: name + "-oxidns", host: host, port: port, expires: "0"}
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
	expired, err := sessionExpired(session, enrollmentNow(), true)
	if err != nil {
		return nil, err
	}
	if expired {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		session.token, err = haTokenURLSafe(32)
		if err != nil {
			return nil, err
		}
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
		if err := fsx.WriteFile(ctx, path, data, fsx.Options{Pattern: session.id + ".*.tmp"}); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}
	}
	if _, ok := fieldText(session.fields, "token"); !ok {
		return nil, renderFailed(msgInstallationIdentity)
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
	expired, err := sessionExpired(session, enrollmentNow(), false)
	if err != nil {
		return result, err
	}
	if expired {
		return result, enrollmentError(enrollmentExpired, 401)
	}
	if _, ok := fieldText(session.fields, "token_hash"); !ok || strings.ContainsFunc(session.tokenHash, func(r rune) bool { return r > 127 }) {
		return result, renderFailed(msgInstallationIdentity)
	}
	sum := sha256.Sum256([]byte(token))
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(session.tokenHash)) != 1 {
		return result, enrollmentError(enrollmentExpired, 401)
	}
	if !session.fields.Has("profile_id") {
		return result, renderFailed(msgInstallationIdentity)
	}
	if _, ok := fieldText(session.fields, "profile_id"); !ok {
		return result, repo.NewValidationError("Invalid profile id")
	}
	if err := repo.ValidateProfileID(session.profileID); err != nil {
		return result, err
	}
	for _, key := range []string{"name", "host", "port"} {
		if !session.fields.Has(key) {
			return result, renderFailed(msgInstallationIdentity)
		}
	}
	for _, key := range []string{"name", "host"} {
		if _, ok := fieldText(session.fields, key); !ok {
			return result, enrollmentError(msgInstallationChanged, 409)
		}
	}
	port, valid := new(big.Rat).SetString(session.port.String())
	if !valid || !port.IsInt() || port.Sign() <= 0 || port.Cmp(big.NewRat(65535, 1)) > 0 {
		return result, enrollmentError(msgInstallationChanged, 409)
	}
	if manager == nil {
		return result, errors.New("installation registry is unavailable")
	}
	err = manager.EditAgents(ctx, session.profileID, func(tx context.Context, records *[]model.Agent) error {
		if err := tx.Err(); err != nil {
			return err
		}
		portNumber := port.Num().Int64()
		for i := range *records {
			a := &(*records)[i]
			if a.DockerHAInstallationID == nil || *a.DockerHAInstallationID != sessionID {
				continue
			}
			if a.Name != session.name || a.Host != session.host || a.ProfileID != session.profileID ||
				a.ServiceType != "oxidns" || a.DeploymentMethod != "docker" || int64(a.Port) != portNumber {
				return enrollmentError(msgInstallationChanged, 409)
			}
			if a.ID == "" || a.Token == "" {
				return enrollmentError(msgInstallationChanged, 409)
			}
			result = AgentIdentity{AgentID: a.ID, Token: a.Token}
			return tx.Err()
		}
		for i := range *records {
			a := &(*records)[i]
			if a.Name == session.name && a.Host == session.host {
				return enrollmentError(msgInstallationCollision, 409)
			}
		}
		suffix, err := uuid4Hex()
		if err != nil {
			return err
		}
		credential, err := haTokenURLSafe(24)
		if err != nil {
			return err
		}
		id := "agent_" + suffix
		a := model.Agent{
			Name: session.name, Host: session.host, Port: model.Int(portNumber), ProfileID: session.profileID,
			ServiceType: "oxidns", DeploymentMethod: "docker", ID: id, Token: credential,
			DockerHAInstallationID: new(sessionID), Enabled: new(true), Status: "offline", LastHeartbeat: "",
			Version: "unknown", ConfigVersion: "0", CreatedAt: naiveISO(enrollmentLocalNow()),
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
