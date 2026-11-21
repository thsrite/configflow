package nodes

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/httpx/httpxtest"
)

// O9 round 2 L3: manual node fields that splice into Surge/Loon INI configs
// must reject line breaks and control characters on save (400 with Chinese
// message naming the field) without mutating storage, while normal nodes and
// valid YAML/URI proxy_string inputs continue to save.
func TestO9R2NodeControlCharValidation(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(Register)})
	// Seed one clean node for PUT tests.
	srv.Post("/api/nodes", httpxtest.JSON(map[string]any{
		"id":     "existing",
		"name":   "Clean Node",
		"type":   "ss",
		"server": "192.0.2.1",
		"port":   8388,
		"params": map[string]any{"cipher": "aes-256-gcm", "password": "clean-secret"},
	})).WantStatus(200)

	rejects := []struct {
		name      string
		payload   map[string]any
		wantField string
	}{
		{
			name:      "LF in name",
			payload:   map[string]any{"name": "HK\n[Rule]\nFINAL,DIRECT", "type": "ss", "server": "192.0.2.1", "port": 8388},
			wantField: "name",
		},
		{
			name:      "CR in name",
			payload:   map[string]any{"name": "HK\rFINAL,DIRECT", "type": "ss", "server": "192.0.2.1", "port": 8388},
			wantField: "name",
		},
		{
			name:      "CRLF in server",
			payload:   map[string]any{"name": "HK", "type": "ss", "server": "192.0.2.1\r\nFINAL,DIRECT", "port": 8388},
			wantField: "server",
		},
		{
			name:      "NUL in type",
			payload:   map[string]any{"name": "HK", "type": "ss\x00evil", "server": "192.0.2.1", "port": 8388},
			wantField: "type",
		},
		{
			name:      "NEL in string port",
			payload:   map[string]any{"name": "HK", "type": "ss", "server": "192.0.2.1", "port": "443\u0085evil"},
			wantField: "port",
		},
		{
			name: "NUL in params.password",
			payload: map[string]any{
				"name": "HK", "type": "trojan", "server": "192.0.2.1", "port": 443,
				"params": map[string]any{"password": "pw\x00evil"},
			},
			wantField: "params.password",
		},
		{
			name: "ESC in nested params.reality-opts.public-key",
			payload: map[string]any{
				"name": "HK", "type": "vless", "server": "192.0.2.1", "port": 443,
				"params": map[string]any{"reality-opts": map[string]any{"public-key": "key\x1b[2J"}},
			},
			wantField: "params.reality-opts.public-key",
		},
		{
			name: "U+2028 in nested params.ws-opts.headers.Host",
			payload: map[string]any{
				"name": "HK", "type": "vmess", "server": "192.0.2.1", "port": 443,
				"params": map[string]any{"ws-opts": map[string]any{"headers": map[string]any{"Host": "a.example.com\u2028evil"}}},
			},
			wantField: "params.ws-opts.headers.Host",
		},
		{
			name: "U+2029 in params.dns list",
			payload: map[string]any{
				"name": "WG", "type": "wireguard", "server": "192.0.2.1", "port": 51820,
				"params": map[string]any{"dns": []any{"192.0.2.53\u2029evil"}},
			},
			wantField: "params.dns[0]",
		},
		{
			name:      "percent-encoded LF in URI proxy_string name",
			payload:   map[string]any{"name": "HK", "proxy_string": "ss://YWVzLTI1Ni1nY206cHc@192.0.2.1:8388#HK%0A%5BRule%5D"},
			wantField: "proxy_string.name",
		},
		{
			name:      "percent-encoded LF in URI proxy_string password",
			payload:   map[string]any{"name": "HK", "proxy_string": "trojan://pw%0A%5BRule%5D@192.0.2.1:443#HK"},
			wantField: "proxy_string.params.password",
		},
		{
			name:      "raw newline in URI proxy_string",
			payload:   map[string]any{"name": "HK", "proxy_string": "ss://YWVzLTI1Ni1nY206cHc@192.0.2.1:8388#HK\nFINAL,DIRECT"},
			wantField: "proxy_string",
		},
		{
			name:      "NEL inside YAML scalar in proxy_string",
			payload:   map[string]any{"name": "HK", "proxy_string": "type: trojan\nserver: 192.0.2.1\nport: 443\npassword: \"a\\Nb\"\n"},
			wantField: "proxy_string.password",
		},
		{
			name:      "newline inside YAML name in proxy_string",
			payload:   map[string]any{"name": "HK", "proxy_string": "name: \"a\\nb\"\ntype: trojan\nserver: 192.0.2.1\nport: 443\npassword: pw\n"},
			wantField: "proxy_string.name",
		},
	}

	for _, tc := range rejects {
		t.Run("POST/"+tc.name, func(t *testing.T) {
			before := srv.Get("/api/nodes").WantStatus(200).Body
			res := srv.Post("/api/nodes", httpxtest.JSON(tc.payload)).WantStatus(400)
			var body struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(res.Body, &body); err != nil {
				t.Fatal(err)
			}
			wantMsg := fmt.Sprintf(msgNodeControlChar, tc.wantField)
			if body.Success || body.Message != wantMsg {
				t.Fatalf("got %+v, want success=false message=%q", body, wantMsg)
			}
			if after := srv.Get("/api/nodes").WantStatus(200).Body; string(after) != string(before) {
				t.Fatalf("rejected POST mutated nodes: %s", after)
			}
		})
		t.Run("PUT/"+tc.name, func(t *testing.T) {
			before := srv.Get("/api/nodes").WantStatus(200).Body
			res := srv.Put("/api/nodes/existing", httpxtest.JSON(tc.payload)).WantStatus(400)
			var body struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(res.Body, &body); err != nil {
				t.Fatal(err)
			}
			wantMsg := fmt.Sprintf(msgNodeControlChar, tc.wantField)
			if body.Success || body.Message != wantMsg {
				t.Fatalf("got %+v, want success=false message=%q", body, wantMsg)
			}
			if after := srv.Get("/api/nodes").WantStatus(200).Body; string(after) != string(before) {
				t.Fatalf("rejected PUT mutated nodes: %s", after)
			}
		})
	}

	// Negative controls: valid nodes, valid multi-line YAML proxy_string, and
	// valid URI / opaque proxy_string inputs are accepted.
	accepts := []struct {
		name    string
		payload map[string]any
	}{
		{
			name: "clean structured node with Chinese and punctuation",
			payload: map[string]any{
				"id": "ok-1", "name": "香港 01 (IPLC) - 测试", "type": "trojan",
				"server": "hk.example.com", "port": 443,
				"params": map[string]any{"password": "p@ss!w0rd#123", "sni": "hk.example.com"},
			},
		},
		{
			name: "valid multi-line YAML proxy_string",
			payload: map[string]any{
				"id":           "ok-yaml",
				"name":         "YAML Node",
				"proxy_string": "type: trojan\nserver: 192.0.2.2\nport: 443\npassword: synthetic-secret\n",
			},
		},
		{
			// Mihomo parameter values may span lines (ca-str, inline PEM);
			// Surge/Loon generation folds them, so saving must not refuse them.
			name: "multi-line params.ca-str PEM and newline in params.password",
			payload: map[string]any{
				"id": "ok-pem", "name": "PEM Node", "type": "trojan",
				"server": "192.0.2.4", "port": 443,
				"params": map[string]any{"password": "pw\nline2", "ca-str": testPEM, "sni": "pem.example.com"},
			},
		},
		{
			name: "YAML proxy_string with block-scalar ca-str and quoted newline",
			payload: map[string]any{
				"id":           "ok-yaml-pem",
				"name":         "YAML PEM Node",
				"proxy_string": "type: trojan\nserver: 192.0.2.5\nport: 443\npassword: \"a\\nb\"\nca-str: |\n  " + strings.ReplaceAll(strings.TrimSuffix(testPEM, "\n"), "\n", "\n  ") + "\n",
			},
		},
		{
			name: "valid URI proxy_string",
			payload: map[string]any{
				"id":           "ok-uri",
				"name":         "URI Node",
				"proxy_string": "ss://YWVzLTI1Ni1nY206cHc@192.0.2.3:8388#HK-01",
			},
		},
		{
			name: "valid opaque proxy_string",
			payload: map[string]any{
				"id":           "ok-opaque",
				"name":         "Opaque Node",
				"proxy_string": "opaque://exit",
			},
		},
	}
	for _, tc := range accepts {
		t.Run("accept/"+tc.name, func(t *testing.T) {
			srv.Post("/api/nodes", httpxtest.JSON(tc.payload)).WantStatus(200)
		})
	}

	// The multi-line PEM is stored byte for byte (Mihomo output reads it as is).
	t.Run("accept/PEM round trip", func(t *testing.T) {
		body := srv.Get("/api/nodes").WantStatus(200).Body
		var list []struct {
			ID     string         `json:"id"`
			Params map[string]any `json:"params"`
		}
		if err := json.Unmarshal(body, &list); err != nil {
			t.Fatalf("decode nodes: %v\n%s", err, body)
		}
		for _, n := range list {
			if n.ID == "ok-pem" {
				if got := n.Params["ca-str"]; got != testPEM {
					t.Fatalf("ca-str = %q, want %q", got, testPEM)
				}
				return
			}
		}
		t.Fatalf("node ok-pem not listed:\n%s", body)
	})
}

// testPEM is a synthetic, syntactically PEM-shaped multi-line value.
const testPEM = "-----BEGIN CERTIFICATE-----\nU1lOVEhFVElDLVRFU1QtQ0VSVElGSUNBVEUtREFUQQ==\nQUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVo=\n-----END CERTIFICATE-----\n"
