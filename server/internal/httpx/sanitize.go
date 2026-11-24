package httpx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"configflow/internal/jsonresp"
	"configflow/internal/redact"
	"configflow/internal/repo"
)

// External returns metadata through the same global JSON exit as JSON. Policy
// runs after after-request hooks, using the final committed credential snapshot.
func External(_ *Ctx, v any) (*Response, error) { return JSON(v), nil }

var renderDraftEndpoints = map[string]bool{
	"oxidns.render_oxidns_draft": true, "profiles.profile_render_oxidns_draft": true,
}

var oxidnsSettingsEndpoints = map[string]bool{
	"oxidns.oxidns_settings": true, "profiles.profile_oxidns_settings": true,
	"oxidns.prepare_oxidns_editor": true, "profiles.profile_prepare_oxidns_editor": true,
	"oxidns.preview_oxidns_template": true, "profiles.profile_preview_oxidns_template": true,
	"oxidns.apply_oxidns_common_settings": true, "profiles.profile_apply_oxidns_common_settings": true,
	"oxidns.apply_oxidns_native_settings": true, "profiles.profile_apply_oxidns_native_settings": true,
}

var oxidnsNativeKeys = []string{"native_config", "additional_files", "template_settings",
	"template_base_config", "legacy_native_config", "_common_disabled", "gateway_onboarding_native_backup"}

var oxidnsValuesEndpoints = map[string]bool{
	"oxidns.read_oxidns_common_settings": true, "profiles.profile_read_oxidns_common_settings": true,
	"oxidns.apply_oxidns_common_settings": true, "profiles.profile_apply_oxidns_common_settings": true,
	"oxidns.apply_oxidns_native_settings": true, "profiles.profile_apply_oxidns_native_settings": true,
}

var tokenEndpoints = map[string]bool{"agents.register_agent": true, "agents.docker_oxidns_ha_enroll": true}

var artifactEndpoints = map[string]bool{
	"agents.get_agent_config": true, "agents.generate_final_agent_config": true,
	"generate.generate_artifact": true, "profiles.profile_generate_artifact": true,
}

// maxRawJSONDepth retains the existing protection for deeply nested raw bodies.
const maxRawJSONDepth = 990

// writeJSON is the sole JSON response exit: encode, redact decoded tokens,
// resolve key collisions in the original provider order, then sort output keys.
// A nil context is used only to materialize Body before request finalization;
// the finalizer still applies the policy to those encoded bytes.
func writeJSON(c *Ctx, r *Response) error {
	if r.hasValue {
		body, err := jsonresp.Encode(r.value)
		if err != nil {
			native, changed, normalizeErr := responseValue(r.value)
			if normalizeErr != nil {
				return normalizeErr
			}
			if !changed {
				return err
			}
			body, err = jsonresp.Encode(native)
			if err != nil {
				return err
			}
		}
		r.body, r.value, r.hasValue = body, nil, false
	}
	if c == nil {
		// Body may be materialized and fed back through SetBody by a hook.
		// Preserve the provider's original-key sort before that becomes raw JSON.
		if r.sortValue {
			d := json.NewDecoder(bytes.NewReader(r.body))
			d.UseNumber()
			var value any
			if err := d.Decode(&value); err != nil {
				return fmt.Errorf("decode materialized JSON response: %w", err)
			}
			body, err := jsonresp.Encode(value)
			if err != nil {
				return err
			}
			r.body = body
		}
		return nil
	}
	if !r.IsJSON() || r.passthrough {
		return nil
	}
	if !r.sortValue && jsonDepth(r.body) > maxRawJSONDepth {
		r.SetBody([]byte(`"[REDACTED]"`))
		return nil
	}
	text, err := decodeJSONBytes(r.body)
	if err != nil {
		return nil
	} // Python's silent get_json leaves invalid bodies alone.
	// Repair legacy non-finite literals before native validation. Invalid syntax
	// otherwise keeps Python's silent get_json behaviour.
	text, nonFinite := finiteJSON(text)
	if !json.Valid(text) {
		return nil
	}
	if nonFinite {
		r.body = text
	}
	rRepo := c.app.Repo
	if rRepo == nil {
		var err error
		rRepo, err = repo.GetRepository(c.ctx)
		if err != nil {
			return err
		}
	}
	secrets, err := redact.ExternalPayloadSecrets(c.ctx, rRepo, nil)
	if err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(text))
	d.UseNumber()
	pass := responseRedactor{decoder: d, secrets: secrets, sortKeys: r.sortValue, cache: make(map[string]string)}
	payload, err := pass.value(0)
	if err != nil {
		return fmt.Errorf("redact JSON response: %w", err)
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("redact JSON response: trailing data")
	}
	if payload == nil {
		return nil
	} // Flask leaves a JSON null body untouched.
	if target, ok := payload.(map[string]any); ok && hasExemptions(c.Endpoint) {
		originalDecoder := json.NewDecoder(bytes.NewReader(text))
		originalDecoder.UseNumber()
		var original map[string]any
		if err := originalDecoder.Decode(&original); err != nil {
			return fmt.Errorf("decode JSON exemption: %w", err)
		}
		applyWhitelists(c, r.Status, original, target)
	}
	body, err := jsonresp.Encode(payload) // encoding/json sorts every object map.
	if err != nil {
		return err
	}
	r.SetBody(body)
	return nil
}

const responseMaxDepth = 128

type responseRedactor struct {
	decoder  *json.Decoder
	secrets  redact.Secrets
	sortKeys bool
	cache    map[string]string
}

type responseMember struct {
	key   string
	value any
}

func (s *responseRedactor) value(depth int) (any, error) {
	token, err := s.decoder.Token()
	if err != nil {
		return nil, err
	}
	if token == json.Delim('{') || token == json.Delim('[') {
		if depth >= responseMaxDepth {
			if err := s.skipContainer(); err != nil {
				return nil, err
			}
			return redact.Redacted, nil
		}
	}
	switch token {
	case json.Delim('{'):
		members := []responseMember{}
		positions := map[string]int{}
		for s.decoder.More() {
			keyToken, err := s.decoder.Token()
			if err != nil {
				return nil, err
			}
			key := keyToken.(string)
			value, err := s.value(depth + 1)
			if err != nil {
				return nil, err
			}
			if redact.SensitiveSystemConfigFields[key] {
				continue
			}
			if i, ok := positions[key]; ok {
				members[i].value = value
			} else {
				positions[key] = len(members)
				members = append(members, responseMember{key, value})
			}
		}
		if _, err := s.decoder.Token(); err != nil {
			return nil, err
		}
		if s.sortKeys {
			sort.SliceStable(members, func(i, j int) bool { return members[i].key < members[j].key })
		}
		out := make(map[string]any, len(members))
		for _, m := range members {
			out[s.str(m.key)] = m.value
		}
		return out, nil
	case json.Delim('['):
		out := []any{}
		for s.decoder.More() {
			value, err := s.value(depth + 1)
			if err != nil {
				return nil, err
			}
			out = append(out, value)
		}
		_, err := s.decoder.Token()
		return out, err
	}
	if value, ok := token.(string); ok {
		return s.str(value), nil
	}
	return token, nil
}

func (s *responseRedactor) str(value string) string {
	if result, ok := s.cache[value]; ok {
		return result
	}
	result := value
	if redact.ContainsEncodedSecret(value, s.secrets) {
		result = redact.Redacted
	}
	if len(value) <= 1024 && len(s.cache) < 4096 {
		s.cache[value] = result
	}
	return result
}

// skipContainer consumes a subtree iteratively, keeping the call stack bounded
// even for hostile raw bodies. The opening delimiter has already been read.
func (s *responseRedactor) skipContainer() error {
	depth := 1
	for depth > 0 {
		token, err := s.decoder.Token()
		if err != nil {
			return err
		}
		switch token {
		case json.Delim('{'), json.Delim('['):
			depth++
		case json.Delim('}'), json.Delim(']'):
			depth--
		}
	}
	return nil
}

func hasExemptions(endpoint string) bool {
	return renderDraftEndpoints[endpoint] || oxidnsSettingsEndpoints[endpoint] || oxidnsValuesEndpoints[endpoint] ||
		tokenEndpoints[endpoint] || artifactEndpoints[endpoint] || endpoint == "settings.handle_rule_fetch_proxy"
}

func applyWhitelists(c *Ctx, status int, payload, sanitized map[string]any) {
	endpoint := c.Endpoint
	if status == 200 {
		if renderDraftEndpoints[endpoint] {
			if config, ok := payload["config"].(string); ok {
				sanitized["config"] = config
			}
		} else if oxidnsSettingsEndpoints[endpoint] {
			original, target := payload, sanitized
			if c.Request.Method == "POST" {
				original, _ = payload["settings"].(map[string]any)
				target, _ = sanitized["settings"].(map[string]any)
			}
			if original != nil && target != nil {
				for _, key := range oxidnsNativeKeys {
					if value, ok := original[key]; ok {
						target[key] = value
					}
				}
			}
		}
		if oxidnsValuesEndpoints[endpoint] {
			if values, ok := payload["values"].(map[string]any); ok {
				sanitized["values"] = values
			}
		}
	}
	if endpoint == "settings.handle_rule_fetch_proxy" && status == 200 {
		if proxy, ok := payload["rule_fetch_proxy"].(string); ok {
			sanitized["rule_fetch_proxy"] = proxy
		}
	}
	if tokenEndpoints[endpoint] && status >= 200 && status < 300 && payload["success"] == true {
		if token, ok := payload["token"].(string); ok {
			sanitized["token"] = token
		}
	}
	if artifactEndpoints[endpoint] && status == 200 && payload["success"] == true {
		if content, ok := payload["content"].(string); ok {
			sanitized["content"] = content
		}
	}
}

// jsonDepth is the maximum container nesting of a JSON text (strings
// skipped), without decoding it.
func jsonDepth(body []byte) int {
	depth, deepest := 0, 0
	inString, escaped := false, false
	for _, b := range body {
		if inString {
			switch {
			case escaped:
				escaped = false
			case b == '\\':
				escaped = true
			case b == '"':
				inString = false
			}
			continue
		}
		switch b {
		case '"':
			inString = true
		case '[', '{':
			depth++
			if depth > deepest {
				deepest = depth
			}
		case ']', '}':
			depth--
		}
	}
	return deepest
}
