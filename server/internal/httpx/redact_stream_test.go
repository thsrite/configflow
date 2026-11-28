package httpx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"

	"configflow/internal/jsonresp"
	"configflow/internal/redact"
	"configflow/internal/repo"
)

type referenceRedactor struct {
	decoder  *json.Decoder
	secrets  redact.Secrets
	sortKeys bool
	cache    map[string]string
}

type referenceMember struct {
	key   string
	value any
}

func (s *referenceRedactor) value(depth int) (any, error) {
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
		members := []referenceMember{}
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
				members = append(members, referenceMember{key, value})
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

func (s *referenceRedactor) str(value string) string {
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
func (s *referenceRedactor) skipContainer() error {
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

func referenceWhitelists(c *Ctx, status int, payload, sanitized map[string]any) {
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

func referenceBody(text []byte, secrets redact.Secrets, sortKeys bool, c *Ctx, status int) ([]byte, error) {
	d := json.NewDecoder(bytes.NewReader(text))
	d.UseNumber()
	s := referenceRedactor{d, secrets, sortKeys, map[string]string{}}
	v, err := s.value(0)
	if err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing JSON")
	}
	if v == nil {
		return text, nil
	}
	endpoint := c.Endpoint
	exempt := renderDraftEndpoints[endpoint] || oxidnsSettingsEndpoints[endpoint] || oxidnsValuesEndpoints[endpoint] || tokenEndpoints[endpoint] || artifactEndpoints[endpoint] || endpoint == "settings.handle_rule_fetch_proxy"
	if obj, ok := v.(map[string]any); ok && exempt {
		var original map[string]any
		d := json.NewDecoder(bytes.NewReader(text))
		d.UseNumber()
		if err := d.Decode(&original); err != nil {
			return nil, err
		}
		referenceWhitelists(c, status, original, obj)
	}
	return jsonresp.Encode(v)
}

func benchmarkResponse(b *testing.B, run func([]byte, redact.Secrets, bool, *Ctx, int) ([]byte, error)) {
	var data strings.Builder
	data.WriteString(`{"items":[`)
	for i := range 7600 {
		if i != 0 {
			data.WriteByte(',')
		}
		fmt.Fprintf(&data, `{"host":"h-%d.example.com","probe":{"reasons":["synthetic probe"],"direct":{"ok":2,"total":2},"target":"direct"},"routes":[{"rule":"Match","conns":123456789}],"tags":["uncovered"],"first_seen":"2026-11-28T10:00:00"}`, i)
	}
	data.WriteString("]}")
	raw := []byte(data.String())
	c := &Ctx{Request: &http.Request{Method: "GET"}}
	b.ReportAllocs()
	b.SetBytes(int64(len(raw)))
	for b.Loop() {
		if _, err := run(raw, redact.Secrets{"synthetic-secret"}, true, c, 200); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRedactListingReference(b *testing.B) { benchmarkResponse(b, referenceBody) }

func BenchmarkRedactListing(b *testing.B) { benchmarkResponse(b, transformResponse) }

func compareRedaction(t *testing.T, raw []byte, endpoint, method string, status int, sorted bool, secrets redact.Secrets) {
	t.Helper()
	c := &Ctx{Endpoint: endpoint, Request: &http.Request{Method: method}}
	want, err := referenceBody(raw, secrets, sorted, c, status)
	got, gotErr := transformResponse(raw, secrets, sorted, c, status)
	if (err == nil) != (gotErr == nil) || err == nil && !bytes.Equal(got, want) {
		t.Fatalf("input %q: got %q (%v), want %q (%v)", raw, got, gotErr, want, err)
	}
}

func TestTokenRedactionMatchesReference(t *testing.T) {
	inputs := []string{
		`{"z":[[],{},null,true,1.000e+99,184467440737095516161],"a":"\ud800","\u007a":[false,"\f\b\u001f\n\u2028\u2029<&>\\\""]}`,
		`{"key%2bsecret":1,"[REDACTED]":2,"key+secret":"hidden","key%2bsecret":4,"nested":{"rule_proxy_token":true,"safe":"key%252bsecret"}}`,
		`{"success":false,"success":true,"config":"key+secret","token":"key+secret","content":"key+secret","settings":{"native_config":{"b":"key+secret","a":1},"additional_files":[],"token":"key+secret"},"native_config":null,"values":{"rule_proxy_token":1,"value":"key+secret"},"rule_fetch_proxy":"key+secret"}`,
		` {"\ud800":2,"\ufffd":3} `, " null ",
	}
	for _, depth := range []int{127, 128, 129, 990, 991} {
		inputs = append(inputs, strings.Repeat("[", depth)+`{"value":"key+secret"}`+strings.Repeat("]", depth))
	}
	for _, raw := range inputs {
		for _, sorted := range []bool{false, true} {
			for _, endpoint := range []string{"ordinary", "settings.handle_rule_fetch_proxy", "oxidns.render_oxidns_draft", "oxidns.apply_oxidns_common_settings", "agents.register_agent", "generate.generate_artifact"} {
				for _, method := range []string{"GET", "POST"} {
					for _, status := range []int{200, 201, 400} {
						compareRedaction(t, []byte(raw), endpoint, method, status, sorted, redact.Secrets{"key+secret", "token", "config"})
					}
				}
			}
		}
	}
}

func FuzzTokenRedaction(f *testing.F) {
	for _, raw := range []string{`{"a":[{},null,1.020e9,"s%65cret"],"rule_proxy_token":"x"}`, `{"secret":1,"[REDACTED]":2,"\u0073ecret":3}`, `{"success":true,"token":"secret","settings":{"native_config":{"rule_proxy_token":"secret"}}}`, `"\ud800\u2028<&>"`, strings.Repeat("[", 129) + "1" + strings.Repeat("]", 129)} {
		f.Add([]byte(raw), uint8(0))
	}
	f.Fuzz(func(t *testing.T, raw []byte, mode uint8) {
		if !json.Valid(raw) {
			return
		}
		endpoints := []string{"ordinary", "agents.register_agent", "oxidns.apply_oxidns_native_settings", "generate.generate_artifact", "settings.handle_rule_fetch_proxy"}
		compareRedaction(t, raw, endpoints[int(mode)%len(endpoints)], []string{"GET", "POST"}[mode>>1&1], []int{200, 201, 400}[int(mode>>2)%3], mode&1 == 0, redact.Secrets{"secret", "token"})
	})
}

func TestRawResponseDepthAndNullBytes(t *testing.T) {
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	c := &Ctx{ctx: t.Context(), app: &App{Repo: r}, Request: &http.Request{Method: "GET"}}
	for _, depth := range []int{maxRawJSONDepth - 1, maxRawJSONDepth, maxRawJSONDepth + 1} {
		raw := []byte(strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth))
		want := []byte(`"[REDACTED]"`)
		if depth <= maxRawJSONDepth {
			want, err = referenceBody(raw, nil, false, c, 200)
			if err != nil {
				t.Fatal(err)
			}
		}
		resp := RawJSON(200, raw)
		if err := writeJSON(c, resp); err != nil || !bytes.Equal(resp.Body(), want) {
			t.Fatalf("depth %d: %s, %v", depth, resp.Body(), err)
		}
	}
	for _, raw := range [][]byte{[]byte(" null "), {0xff, 0xfe, 'n', 0, 'u', 0, 'l', 0, 'l', 0}, []byte("{")} {
		resp := RawJSON(200, raw)
		if err := writeJSON(c, resp); err != nil || !bytes.Equal(resp.Body(), raw) {
			t.Fatalf("body %q: %q, %v", raw, resp.Body(), err)
		}
	}
}
