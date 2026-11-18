// Package redact holds the response-redaction pieces of
// backend/common/config_export.py: the stored credentials that must never
// leave the server (current and retired rule proxy tokens, Agent tokens,
// sensitive system_config fields) and the walk that replaces every string
// containing one of them, in any URL-encoded form, with "[REDACTED]".
//
// The global JSON response hook of internal/httpx uses it for every JSON
// response; route packages use it for text they show (Agent logs) and for
// input checks (a config token must not contain an internal token). The rest
// of config_export.py (sanitize_config_for_output, redact_oxidns_settings)
// belongs to package #17.
package redact

import (
	"context"
	"encoding/json"
	"math"
	"net/url"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"
	"unsafe"

	"configflow/internal/agents/diag"
	"configflow/internal/repo"
	"configflow/internal/weburl"
)

// Redacted is the replacement text.
const Redacted = "[REDACTED]"

// SensitiveSystemConfigFields are dropped from every response wherever they
// appear as an object key (SENSITIVE_SYSTEM_CONFIG_FIELDS).
var SensitiveSystemConfigFields = map[string]bool{
	"rule_proxy_token": true, "retired_rule_proxy_tokens": true, "rule_fetch_proxy": true,
}

const (
	maxDepth             = 128
	stringCacheMaxLength = 1024
	stringCacheSize      = 4096
)

// Secrets is one snapshot of the credentials to hide, sorted and without
// duplicates or empty strings (Python: a frozenset). Compare with Equal.
type Secrets []string

// NewSecrets builds a snapshot from any number of token lists.
func NewSecrets(lists ...[]string) Secrets {
	var all []string
	for _, list := range lists {
		for _, token := range list {
			if token != "" {
				all = append(all, token)
			}
		}
	}
	sort.Strings(all)
	return slices.Compact(all)
}

// Equal is frozenset equality.
func (s Secrets) Equal(other Secrets) bool { return slices.Equal(s, other) }

// ExternalPayloadSecrets is _external_payload_secrets(system_config): the
// rule proxy and Agent tokens stored in the repository, plus the sensitive
// string fields of systemConfig when given (nil: none).
func ExternalPayloadSecrets(ctx context.Context, systemConfig any) (Secrets, error) {
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	ruleProxy, err := repository.RuleProxyTokensForSanitization(ctx)
	if err != nil {
		return nil, err
	}
	agents, err := repository.AgentTokensForSanitization(ctx)
	if err != nil {
		return nil, err
	}
	var tokens []string
	for token := range ruleProxy {
		tokens = append(tokens, token)
	}
	for token := range agents {
		tokens = append(tokens, token)
	}
	for field := range SensitiveSystemConfigFields {
		if text, ok := fieldValue(systemConfig, field).(string); ok && text != "" {
			tokens = append(tokens, text)
		}
	}
	return NewSecrets(tokens), nil
}

// RuleProxyTokens is _repository_rule_proxy_tokens() as a snapshot.
func RuleProxyTokens(ctx context.Context) (Secrets, error) {
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	ruleProxy, err := repository.RuleProxyTokensForSanitization(ctx)
	if err != nil {
		return nil, err
	}
	var tokens []string
	for token := range ruleProxy {
		tokens = append(tokens, token)
	}
	return NewSecrets(tokens), nil
}

// ContainsEncodedSecret is _contains_encoded_secret: whether value contains
// one of the secrets literally or after any sequence of URL decodings
// (unquote / unquote_plus), exploring a bounded number of decoding states.
func ContainsEncodedSecret(value string, secrets Secrets) bool {
	if len(secrets) == 0 {
		return false
	}
	if !strings.ContainsAny(value, "%+") {
		return containsAny(value, secrets)
	}
	pending := []string{value}
	seen := map[string]bool{value: true}
	maxStates := max(1, 2*pyLen(value)+1)
	maxSteps := 2 * maxStates
	states, steps := 0, 0
	for len(pending) > 0 && states < maxStates && steps < maxSteps {
		current := pending[0]
		pending = pending[1:]
		states++
		if containsAny(current, secrets) {
			return true
		}
		for _, decode := range [...]func(string) string{weburl.Unquote, weburl.UnquotePlus} {
			if steps >= maxSteps {
				break
			}
			steps++
			decoded := decode(current)
			if pyLen(decoded) <= pyLen(current) && !seen[decoded] {
				seen[decoded] = true
				pending = append(pending, decoded)
			}
		}
	}
	return false
}

func containsAny(value string, secrets Secrets) bool {
	for _, secret := range secrets {
		if strings.Contains(value, secret) {
			return true
		}
	}
	return false
}

// pyLen is len(str) in code points.
func pyLen(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

// ContainsInternalRuleProxyToken is contains_internal_rule_proxy_token.
func ContainsInternalRuleProxyToken(ctx context.Context, value string) (bool, error) {
	secrets, err := RuleProxyTokens(ctx)
	if err != nil {
		return false, err
	}
	return ContainsEncodedSecret(value, secrets), nil
}

// SanitizeExternalPayload is sanitize_external_payload(payload, system_config).
func SanitizeExternalPayload(ctx context.Context, payload any, systemConfig any) (any, error) {
	secrets, err := ExternalPayloadSecrets(ctx, systemConfig)
	if err != nil {
		return nil, err
	}
	return SanitizeWithSecrets(payload, secrets, false), nil
}

// SanitizeWithSecrets is _sanitize_with_secrets(payload, secrets,
// sort_dict_keys=...): a copy of payload in which the sensitive system_config
// keys are dropped, every string (key or value) containing a secret becomes
// "[REDACTED]", and a container nested 128 levels deep (the root is level 0)
// or containing itself becomes "[REDACTED]". When two keys redact to the same
// text the later value wins at the earlier position, as in a Python dict;
// sortKeys visits keys in sorted order first (what an encode/decode pass with
// sort_keys=True would have produced). payload is not modified.
func SanitizeWithSecrets(payload any, secrets Secrets, sortKeys bool) any {
	s := &sanitizer{secrets: secrets, sortKeys: sortKeys, active: map[containerID]bool{}}
	return s.visit(payload, 0)
}

// CollectCredentials snapshots known and explicitly named credentials from raw
// metadata before public projection can discard their defining fields. It uses
// the same depth/cycle-bounded visitor as SanitizeCredentials, without mutating
// the payload or caller seeds, constructing a history copy, or logging values.
func CollectCredentials(payload any, seeds Secrets) Secrets {
	seeds = NewSecrets(seeds, []string{os.Getenv("AGENT_REGISTRATION_KEY")})
	s := &sanitizer{secrets: seeds, credentials: true, collectOnly: true, active: map[containerID]bool{}}
	s.visit(payload, 0)
	return NewSecrets(seeds, s.discovered)
}

// SanitizeCredentials copies public metadata while masking credential fields,
// credential-bearing text and known tokens. Unlike the global response hook it
// also protects legacy/imported credentials not present in the token registry,
// including references to the server registration key without a field label.
// Data-keyed configuration documents should use their schema-specific redactor.
func SanitizeCredentials(payload any, secrets Secrets) json.RawMessage {
	if payload == nil {
		return nil
	}
	secrets = NewSecrets(secrets, []string{os.Getenv("AGENT_REGISTRATION_KEY")})
	s := &sanitizer{secrets: secrets, credentials: true, active: map[containerID]bool{}}
	public := s.visit(payload, 0)
	// A legacy credential may also occur in a sibling diagnostic, including one
	// visited before its credential field. A fresh pass covers those references.
	return encodedPublic(SanitizeWithSecrets(public, NewSecrets(secrets, s.discovered), false))
}

type containerID struct {
	pointer unsafe.Pointer
	length  int
}

type sanitizer struct {
	secrets     Secrets
	credentials bool
	collectOnly bool
	discovered  []string
	sortKeys    bool
	active      map[containerID]bool
	cache       map[string]bool
}

func (s *sanitizer) visit(value any, depth int) any {
	switch v := value.(type) {
	case string:
		return s.str(v)
	case objectFields:
		ptr := reflect.ValueOf(v)
		if ptr.Kind() == reflect.Pointer && ptr.IsNil() {
			return nil
		}
		identity := unsafe.Pointer(&v)
		if ptr.Kind() == reflect.Pointer {
			identity = ptr.UnsafePointer()
		}
		id := containerID{identity, -1}
		if depth >= maxDepth || s.active[id] {
			return Redacted
		}
		s.active[id] = true
		defer delete(s.active, id)
		keys := v.Keys()
		if s.sortKeys {
			keys = slices.Clone(keys)
			sort.Strings(keys)
		}
		var output *orderedFields
		if !s.collectOnly {
			output = &orderedFields{}
		}
		for _, key := range keys {
			if SensitiveSystemConfigFields[key] {
				continue
			}
			item, _ := v.Get(key)
			if raw, ok := item.(json.RawMessage); ok {
				if decoded, err := decodePublic(raw); err == nil {
					item = decoded
				}
			}
			// This schema flag reports presence, never credential material.
			// A string under the same key still follows the credential rule.
			if key == "has_password" {
				if _, ok := item.(bool); ok {
					if output != nil {
						output.Set(s.str(key), item)
					}
					continue
				}
			}
			if s.credentials && diag.IsCredentialKey(key) {
				if text, ok := item.(string); ok && text != Redacted {
					s.discovered = append(s.discovered, text)
				} else if s.collectOnly {
					s.visit(item, depth+1)
				}
				if output != nil {
					output.Set(s.str(key), Redacted)
				}
				continue
			}
			if s.collectOnly {
				s.visit(item, depth+1)
			} else {
				output.Set(s.str(key), s.visit(item, depth+1))
			}
		}
		return output
	case json.RawMessage:
		if v == nil {
			return nil
		}
		parsed, err := decodePublic(v)
		if err != nil {
			return Redacted
		}
		return s.visit(parsed, depth)
	case map[string]any:
		if v == nil {
			return nil
		}
		id := containerID{reflect.ValueOf(v).UnsafePointer(), -1}
		if depth >= maxDepth || s.active[id] {
			return Redacted
		}
		s.active[id] = true
		defer delete(s.active, id)
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		fields := &orderedFields{}
		for _, key := range keys {
			fields.Set(key, v[key])
		}
		// The wrapper adds no logical depth. Map identity above protects self-links.
		return s.visit(fields, depth)
	case []any:
		if v == nil {
			return v
		}
		id := containerID{unsafe.Pointer(unsafe.SliceData(v)), len(v)}
		if depth >= maxDepth || (len(v) > 0 && s.active[id]) {
			return Redacted
		}
		if len(v) > 0 {
			s.active[id] = true
			defer delete(s.active, id)
		}
		if s.collectOnly {
			for _, item := range v {
				s.visit(item, depth+1)
			}
			return nil
		}
		output := make([]any, len(v))
		for i, item := range v {
			output[i] = s.visit(item, depth+1)
		}
		return output
	}
	switch n := value.(type) {
	case interface{ Float64() float64 }:
		// Legacy numeric wrappers have string storage. Read that raw storage:
		// String() may normalize or round, and Float64() may overflow a valid JSON
		// number. Only actual non-JSON numeric literals are replaced with null.
		raw := reflect.ValueOf(value)
		if raw.Kind() == reflect.String {
			switch raw.String() {
			case "NaN", "Infinity", "-Infinity":
				return nil
			}
		}
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return nil
		}
	case float32:
		if math.IsNaN(float64(n)) || math.IsInf(float64(n), 0) {
			return nil
		}
	}
	return value
}

func (s *sanitizer) str(value string) string {
	if s.collectOnly {
		return value
	}
	if s.credentials {
		if len(diag.CredentialSpans(value)) > 0 {
			return Redacted
		}
		if diag.HasCredentialText(value) {
			value = diag.FailureText(value, nil, len(value)+128)
		}
	}
	if len(s.secrets) == 0 {
		return value
	}
	var contains bool
	if pyLen(value) <= stringCacheMaxLength {
		if s.cache == nil {
			s.cache = map[string]bool{}
		}
		cached, ok := s.cache[value]
		if !ok {
			cached = ContainsEncodedSecret(value, s.secrets)
			if len(s.cache) < stringCacheSize {
				s.cache[value] = cached
			}
		}
		contains = cached
	} else {
		contains = ContainsEncodedSecret(value, s.secrets)
	}
	if contains {
		return Redacted
	}
	return value
}

func variants(secret string) []string {
	result := []string{secret}
	for _, variant := range []string{weburl.Quote(secret), url.QueryEscape(secret)} {
		if !slices.Contains(result, variant) {
			result = append(result, variant)
		}
	}
	return result
}

// RedactSecretsInText is redact_secrets_in_text: replace only the
// credentials (and their URL-encoded forms) inside multi-line text; a line
// that still decodes to a credential afterwards is replaced as a whole.
func RedactSecretsInText(ctx context.Context, text string) (string, error) {
	if text == "" {
		return text, nil
	}
	secrets, err := ExternalPayloadSecrets(ctx, nil)
	if err != nil {
		return "", err
	}
	return redactText(text, secrets), nil
}

func redactText(text string, secrets Secrets) string {
	if !ContainsEncodedSecret(text, secrets) {
		return text
	}
	ordered := slices.Clone([]string(secrets))
	sort.SliceStable(ordered, func(i, j int) bool { return pyLen(ordered[i]) > pyLen(ordered[j]) })
	var out strings.Builder
	for _, line := range splitLinesKeepEnds(text) {
		for _, secret := range ordered {
			for _, variant := range variants(secret) {
				line = strings.ReplaceAll(line, variant, Redacted)
			}
		}
		if ContainsEncodedSecret(line, secrets) {
			end := ""
			if strings.HasSuffix(line, "\n") {
				end = "\n"
			}
			line = Redacted + end
		}
		out.WriteString(line)
	}
	return out.String()
}

// splitLinesKeepEnds is str.splitlines(keepends=True).
func splitLinesKeepEnds(text string) []string {
	var lines []string
	start := 0
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		switch runes[i] {
		case '\r':
			if i+1 < len(runes) && runes[i+1] == '\n' {
				i++
			}
		case '\n', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
		default:
			continue
		}
		lines = append(lines, string(runes[start:i+1]))
		start = i + 1
	}
	if start < len(runes) {
		lines = append(lines, string(runes[start:]))
	}
	return lines
}

// Range is a [Start, End) span in code points.
type Range struct{ Start, End int }

// StoredSecretFinder is stored_secret_finder(): a function giving the ranges
// (in code points) of stored credentials inside a text, from one snapshot of
// them; a text that still decodes to a credential once those are masked is
// returned as a single range covering it.
func StoredSecretFinder(ctx context.Context) (func(text string) []Range, error) {
	secrets, err := ExternalPayloadSecrets(ctx, nil)
	if err != nil {
		return nil, err
	}
	// Registration is a server credential too. It must be irreversibly masked
	// in configuration diffs and excluded from search, rather than merely
	// marked as a revealable user-supplied configuration credential.
	secrets = NewSecrets(secrets, []string{os.Getenv("AGENT_REGISTRATION_KEY")})
	var all []string
	for _, secret := range secrets {
		for _, variant := range variants(secret) {
			if !slices.Contains(all, variant) {
				all = append(all, variant)
			}
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return pyLen(all[i]) > pyLen(all[j]) })
	return func(text string) []Range {
		if len(secrets) == 0 || text == "" || !ContainsEncodedSecret(text, secrets) {
			return nil
		}
		runes := []rune(text)
		var ranges []Range
		for _, variant := range all {
			v := []rune(variant)
			for start := indexRunes(runes, v, 0); start >= 0; start = indexRunes(runes, v, start+1) {
				ranges = append(ranges, Range{start, start + len(v)})
			}
		}
		rest := slices.Clone(runes)
		for _, r := range ranges {
			for i := r.Start; i < r.End; i++ {
				rest[i] = 0
			}
		}
		if ContainsEncodedSecret(string(rest), secrets) {
			return []Range{{0, len(runes)}}
		}
		sort.Slice(ranges, func(i, j int) bool {
			if ranges[i].Start != ranges[j].Start {
				return ranges[i].Start < ranges[j].Start
			}
			return ranges[i].End < ranges[j].End
		})
		return ranges
	}, nil
}

func indexRunes(haystack, needle []rune, from int) int {
	for i := from; i+len(needle) <= len(haystack); i++ {
		if slices.Equal(haystack[i:i+len(needle)], needle) {
			return i
		}
	}
	return -1
}
