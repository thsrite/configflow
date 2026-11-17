package deploy

import (
	"bytes"
	"configflow/internal/jsonx"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// InputKey is an opaque, comparable in-memory input identity. It is never stored.
type InputKey struct{ sum [32]byte }

// InputKey identifies every generation input except Python's top-level metadata.
// A Source is immutable, so its projection is computed once and shared safely.
func (s *Source) InputKey() (InputKey, error) {
	if s == nil || s.legacy == nil {
		return InputKey{}, fmt.Errorf("missing content source")
	}
	s.inputOnce.Do(func() { s.inputKey, s.inputErr = s.projectedInputKey(false) })
	return s.inputKey, s.inputErr
}

// projectedInputKey keeps the two Python projections in one adapter: preview
// additionally ignores profile_name, while content_sync must include it.
func (s *Source) projectedInputKey(preview bool) (InputKey, error) {
	if s == nil || s.legacy == nil {
		return InputKey{}, fmt.Errorf("missing content source")
	}
	cfg := s.config().DeepCopy()
	for _, key := range []string{"agents", "backup", "_revision", "updated_at", "created_at", "name", "description"} {
		cfg.Delete(key)
	}
	if preview {
		cfg.Delete("profile_name")
	}
	raw, err := encodeJSON(cfg)
	if err != nil {
		return InputKey{}, err
	}
	raw, err = nativeCanonical(raw)
	if err != nil {
		return InputKey{}, err
	}
	return InputKey{sha256.Sum256(raw)}, nil
}

// ContentInputKey combines the source projection, complete applied baseline,
// captured Agent identity fields and the original publishing origin.
func ContentInputKey(t Target, s *Source, baseURL string) (InputKey, error) {
	source, err := s.InputKey()
	if err != nil {
		return InputKey{}, err
	}
	identity := map[string]any{}
	for _, k := range []string{"id", "profile_id", "service_type", "config_version", "applied_content"} {
		identity[k] = t.Record.Value(k)
	}
	raw, err := encodeJSON(struct {
		Agent   map[string]any `json:"agent"`
		BaseURL string         `json:"base_url"`
	}{identity, baseURL})
	if err != nil {
		return InputKey{}, fmt.Errorf("content identity: %w", err)
	}
	raw, err = nativeCanonical(raw)
	if err != nil {
		return InputKey{}, err
	}
	sum := sha256.New()
	sum.Write(source.sum[:])
	sum.Write(raw)
	var key InputKey
	copy(key.sum[:], sum.Sum(nil))
	return key, nil
}

// ContentState is the typed comparison projection of a captured Agent record.
type ContentState struct {
	Pending, PreviouslyPushed, HasBaseline, ValidBaseline bool
	ConfigVersion                                         string
	Applied                                               AppliedContent
}

// AppliedContent is the successfully applied content identity and its digests.
type AppliedContent struct {
	Fingerprint   string            `json:"fingerprint"`
	ProfileID     string            `json:"profile_id"`
	ServiceType   string            `json:"service_type"`
	ConfigVersion string            `json:"config_version"`
	DeploymentID  string            `json:"deployment_id"`
	ConfigPath    string            `json:"config_path"`
	BaseURL       string            `json:"base_url"`
	Files         map[string]string `json:"files"`
}

// ContentView reads legacy Agent fields only at the deploy adapter boundary.
func ContentView(t Target) (ContentState, error) {
	var result ContentState
	if t.Record == nil {
		return result, fmt.Errorf("missing content target")
	}
	tasks := []*jsonx.Object{}
	if value := t.Record.Value("deployments"); value != nil {
		deployments, ok := value.(*jsonx.Object)
		if !ok {
			return result, fmt.Errorf("invalid deployment history")
		}
		for _, value := range deployments.Values() {
			if task, ok := value.(*jsonx.Object); ok {
				tasks = append(tasks, task)
			}
		}
	}
	if latest, ok := t.Record.Value("latest_deployment").(*jsonx.Object); ok {
		tasks = append(tasks, latest)
	}
	for _, task := range tasks {
		switch task.GetString("status") {
		case "succeeded":
			result.PreviouslyPushed = true
		case "failed", "rolled_back":
		default:
			result.Pending = true
		}
	}

	if result.Pending {
		return result, nil
	}
	projection := map[string]any{}
	for _, k := range []string{"config_version", "config_revision", "applied_content"} {
		projection[k] = t.Record.Value(k)
	}
	raw, err := encodeJSON(projection)
	if err != nil {
		return result, err
	}
	var record struct {
		ConfigVersion  json.RawMessage `json:"config_version"`
		ConfigRevision json.RawMessage `json:"config_revision"`
		Applied        json.RawMessage `json:"applied_content"`
	}
	if err = json.Unmarshal(raw, &record); err != nil {
		return result, err
	}
	version := string(record.ConfigVersion)
	result.PreviouslyPushed = result.PreviouslyPushed || (version != "" && version != "null" && version != `""` && version != `"0"`) || (len(record.ConfigRevision) > 0 && !bytes.Equal(record.ConfigRevision, []byte("null")))
	_ = json.Unmarshal(record.ConfigVersion, &result.ConfigVersion)
	result.HasBaseline = !emptyJSON(record.Applied)
	if result.HasBaseline {
		result.ValidBaseline = json.Unmarshal(record.Applied, &result.Applied) == nil && len(record.Applied) > 0 && record.Applied[0] == '{'
	}
	return result, nil
}
func emptyJSON(raw []byte) bool {
	switch string(raw) {
	case "", "null", "{}", "[]", `""`, "false", "0":
		return true
	}
	return false
}

// SnapshotTarget isolates a background preparation from later record mutation.
func SnapshotTarget(t Target) Target {
	copy := t
	if t.Record != nil {
		copy.Record = t.Record.DeepCopy()
	}
	return copy
}

func (s *Source) contentDigest() string {
	key, err := s.projectedInputKey(true)
	if err != nil {
		return ""
	}
	return hex.EncodeToString(key.sum[:])
}

// MatchesProfile checks the optional explicit binding on the captured source.
// A missing key inherits the target profile; an explicit null/empty key does not.
func (s *Source) MatchesProfile(profile string) bool {
	if s == nil || s.legacy == nil {
		return false
	}
	value, present := s.legacy.Get("profile_id")
	if !present {
		return true
	}
	id, valid := value.(string)
	return valid && id == profile
}
