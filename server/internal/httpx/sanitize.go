package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"configflow/internal/jsonresp"
	"configflow/internal/redact"
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
			body, err := transformResponse(r.body, nil, true, nil, r.Status)
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
	if c.app == nil || c.app.Repo == nil {
		return errors.New("httpx: repository is nil")
	}
	secrets, err := redact.ExternalPayloadSecrets(c.ctx, c.app.Repo, nil)
	if err != nil {
		return err
	}
	if bytes.Equal(bytes.TrimSpace(text), []byte("null")) {
		return nil
	}
	body, err := transformResponse(text, secrets, r.sortValue, c, r.Status)
	if err != nil {
		return fmt.Errorf("redact JSON response: %w", err)
	}
	r.SetBody(body)
	return nil
}

const responseMaxDepth = 128

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
