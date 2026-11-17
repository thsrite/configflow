// Package draft owns recoverable gateway settings and read-only configuration
// previews. It commits only Mihomo/OxiDNS settings, never generated documents.
package draft

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	"configflow/internal/gateway"
	"configflow/internal/gateway/gwbase"
	"configflow/internal/httpx"
	"configflow/internal/model"
	"configflow/internal/redact"
	"configflow/internal/repo"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
	"math/big"
)

// RestoreKey is the persisted first-trial recovery snapshot member.
const RestoreKey = gwbase.RestoreKey

type document map[string]json.RawMessage

func fields(raw json.RawMessage) document { var o document; _ = json.Unmarshal(raw, &o); return o }
func encode(v any) json.RawMessage        { b, _ := json.Marshal(v); return b }
func str(raw json.RawMessage) string      { var s string; _ = json.Unmarshal(raw, &s); return s }
func digest(v any) string                 { b := encode(v); h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func invalid(message string) error        { return &gateway.Error{Message: message} }

// Request preserves exact case, extra keys and request-number forms for guards.
type Request map[string]json.RawMessage

// Operation selects an editor read, a trial, a save or a recovery.
type Operation struct {
	Write     bool
	Preview   bool
	Restore   bool
	ProfileID string
	BaseURL   string
	Body      Request
}

// Summary exposes only recovery identity and time, never stored editor documents.
type Summary struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
}
type restorePoint struct {
	Version   model.Int `json:"version"`
	ID        string    `json:"id"`
	CreatedAt string    `json:"created_at"`
	Mihomo    document  `json:"mihomo"`
	OxiDNS    document  `json:"oxidns"`
}

func point(config document) *restorePoint {
	m := fields(config["mihomo"])
	raw := m[RestoreKey]
	o := fields(raw)
	var version model.Int
	if json.Unmarshal(o["version"], &version) != nil || version != 1 {
		return nil
	}
	// Members are read exact-case from the raw object, as Python's dict.get:
	// encoding/json's struct matching is case-insensitive ("ID", "Mihomo").
	p := restorePoint{Version: version}
	if string(o["id"]) == "null" || json.Unmarshal(o["id"], &p.ID) != nil || p.ID == "" {
		return nil
	}
	if string(o["created_at"]) == "null" || json.Unmarshal(o["created_at"], &p.CreatedAt) != nil {
		return nil
	}
	if json.Unmarshal(o["mihomo"], &p.Mihomo) != nil || json.Unmarshal(o["oxidns"], &p.OxiDNS) != nil {
		return nil
	}
	if p.Mihomo == nil || p.OxiDNS == nil || p.Mihomo[RestoreKey] != nil {
		return nil
	}
	return &p
}
func summary(config document) *Summary {
	p := point(config)
	if p == nil {
		return nil
	}
	return &Summary{p.ID, p.CreatedAt}
}
func clone(config document) document {
	out := make(document, len(config))
	for k, v := range config {
		out[k] = v
	}
	return out
}
func prepare(config document, body Request, restore bool) (document, error) {
	candidate := clone(config)
	if restore {
		p := point(config)
		if p == nil {
			return nil, invalid(msgNoSnapshot)
		}
		candidate["mihomo"] = encode(p.Mihomo)
		candidate["oxidns"] = encode(p.OxiDNS)
		return candidate, nil
	}
	if raw, exists := config["mihomo"]; exists && fields(raw) == nil {
		return nil, invalid(msgBadSection)
	}
	value, ok := body["settings"]
	if !ok {
		value = encode(body)
	}
	settings, err := selection(config, value)
	if err != nil {
		return nil, err
	}
	m := fields(config["mihomo"])
	if m == nil {
		m = document{}
	}
	m["linux_gateway"] = encode(settings)
	candidate["mihomo"] = encode(m)
	return candidate, nil
}
func remember(config, candidate document) error {
	before, err := settings(config)
	if err != nil {
		return err
	}
	after, err := settings(candidate)
	if err != nil {
		return err
	}
	if reflect.DeepEqual(before, after) {
		return nil
	}
	m := fields(config["mihomo"])
	if _, ok := m[RestoreKey]; ok {
		if point(config) == nil {
			return invalid(msgBadSnapshot)
		}
		return nil
	}
	for _, key := range []string{"mihomo", "oxidns"} {
		if raw, exists := config[key]; exists && fields(raw) == nil {
			return invalid(msgBadSection)
		}
	}
	id := make([]byte, 16)
	if _, err = rand.Read(id); err != nil {
		return fmt.Errorf("create gateway restore identity: %w", err)
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000000+00:00")
	p := restorePoint{1, hex.EncodeToString(id), now, m, fields(config["oxidns"])}
	if p.Mihomo == nil {
		p.Mihomo = document{}
	}
	if p.OxiDNS == nil {
		p.OxiDNS = document{}
	}
	out := fields(candidate["mihomo"])
	out[RestoreKey] = encode(p)
	candidate["mihomo"] = encode(out)
	return nil
}

func checkRevision(body Request, revision int64) error {
	var number httpx.Number
	if json.Unmarshal(body["expected_config_revision"], &number) != nil || !number.IsIntegerLiteral() {
		return invalid(msgRevisionMissing)
	}
	n, ok := new(big.Int).SetString(string(body["expected_config_revision"]), 10)
	if !ok || n.Sign() < 0 {
		return invalid(msgRevisionMissing)
	}
	if n.Cmp(big.NewInt(revision)) != 0 {
		return repo.NewInUse(msgRevisionChanged, nil)
	}
	return nil
}

var previewPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func checkPreview(body Request, p Preview) error {
	token := str(body["preview_token"])
	if !previewPattern.MatchString(token) || subtle.ConstantTimeCompare([]byte(token), []byte(p.PreviewToken)) != 1 {
		return repo.NewInUse(msgPreviewChanged, nil)
	}
	return nil
}

// Change is a sanitized leaf-level configuration difference.
type Change struct {
	Path   string `json:"path"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// Preview binds the complete draft inputs, revision, action and public origin.
type Preview struct {
	Success        bool     `json:"success"`
	Action         string   `json:"action"`
	ConfigRevision int64    `json:"config_revision"`
	PreviewToken   string   `json:"preview_token"`
	Changes        []Change `json:"changes"`
	Warnings       []string `json:"warnings"`
	RestorePoint   *Summary `json:"restore_point"`
}

func preview(ctx context.Context, config, candidate document, revision int64, restore bool, baseURL string) (Preview, error) {
	action := "save"
	if restore {
		action = "restore"
	}
	p := Preview{Success: true, Action: action, ConfigRevision: revision, Changes: []Change{}, Warnings: []string{}, RestorePoint: summary(config)}
	seeds, err := sourceCredentials(config, candidate)
	if err != nil {
		return p, err
	}
	after, err := generate(ctx, candidate, baseURL)
	if err != nil {
		return p, invalid(fmt.Sprintf(msgGenerate, failureText(err, seeds)))
	}
	before, beforeErr := generate(ctx, config, baseURL)
	seeds, err = generatedCredentials(before, after, seeds)
	if err != nil {
		return p, err
	}
	if beforeErr != nil {
		p.Warnings = append(p.Warnings, msgCurrentInvalid)
		p.Changes = append(p.Changes, Change{"Mihomo", msgInvalidBefore, msgValidAfter})
	} else {
		p.Changes = changes(before, after, "Mihomo", seeds)
	}
	warnings, err := overlaps(ctx, candidate)
	if err != nil {
		return p, err
	}
	p.Warnings = append(p.Warnings, warnings...)
	if restore {
		for _, service := range []struct{ key, prefix string }{{"mihomo", msgMihomoDraft}, {"oxidns", msgOxiDNSDraft}} {
			left, err := yamlx.FromJSON(config[service.key])
			if err != nil {
				return p, err
			}
			right, err := yamlx.FromJSON(candidate[service.key])
			if err != nil {
				return p, err
			}
			p.Changes = append(p.Changes, changes(left, right, service.prefix, seeds)...)
		}
	}
	// Agents and backup status are runtime observations, excluded from the input identity.
	inputs := []document{clone(config), clone(candidate)}
	for _, v := range inputs {
		delete(v, "agents")
		delete(v, "backup")
	}
	p.PreviewToken = digest([]any{inputs[0], inputs[1], revision, action, baseURL})
	return p, nil
}

func changes(left, right *yamlx.Node, prefix string, seeds redact.Secrets) []Change {
	result := []Change{}
	keys := map[string]bool{}
	for key := range yamlx.Pairs(left) {
		keys[key] = true
	}
	for key := range yamlx.Pairs(right) {
		keys[key] = true
	}
	order := make([]string, 0, len(keys))
	for key := range keys {
		order = append(order, key)
	}
	slices.Sort(order)
	for _, key := range order {
		if key == RestoreKey {
			continue
		}
		a, b := yamlx.Get(left, key), yamlx.Get(right, key)
		if a != nil && b != nil && yamlx.ValueHash(a) == yamlx.ValueHash(b) {
			continue
		}
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		if yamlx.Resolve(a) != nil && yamlx.Resolve(b) != nil && yamlx.Resolve(a).Kind == yaml.MappingNode && yamlx.Resolve(b).Kind == yaml.MappingNode {
			result = append(result, changes(a, b, path, seeds)...)
			continue
		}
		before, after := msgUnset, msgRemoved
		if a != nil {
			before = display(a, key, seeds)
		}
		if b != nil {
			after = display(b, key, seeds)
		}
		publicPath := redact.SanitizeWithSecrets(path, seeds, false).(string)
		result = append(result, Change{publicPath, before, after})
	}
	return result
}
func display(value *yamlx.Node, key string, seeds redact.Secrets) string {
	raw, err := yamlx.ToJSON(value)
	if err != nil {
		return msgHidden
	}
	if slices.Contains([]string{"custom_config", "native_config", "template_base_config", "legacy_native_config", "additional_files"}, key) {
		return msgDigest + digest(json.RawMessage(raw))[:12]
	}
	// Shared redaction handles credential names, multiline spans, URL userinfo,
	// percent-encoded references and registration keys. No private secret parser.
	safe, err := safeValue(key, raw, seeds)
	if err != nil {
		return msgHidden
	}
	node, err := yamlx.FromJSON(safe)
	if err != nil {
		return msgHidden
	}
	data, err := yamlx.Marshal(node)
	if err != nil {
		return msgHidden
	}
	s := strings.TrimSpace(strings.TrimSuffix(string(data), "...\n"))
	r := []rune(s)
	if len(r) > 800 {
		s = string(r[:800]) + "\n…"
	}
	return s
}
