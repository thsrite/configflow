package convert_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/model"
)

// O9 round 2 L3: generation-time defense ensures legacy or imported manual
// nodes containing line breaks or control characters can never inject extra
// INI sections or rules into Surge or Loon configs, while clean nodes stay
// byte-identical.
func TestO9R2ClientNodeLineInjectionDefense(t *testing.T) {
	withFakeSubStore(t)

	mustNode := func(t *testing.T, raw string) model.Node {
		t.Helper()
		var n model.Node
		if err := json.Unmarshal([]byte(raw), &n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "LF injection in name",
			raw:  `{"id":"n1","name":"HK\n[Rule]\nFINAL,DIRECT","type":"ss","server":"192.0.2.1","port":8388,"params":{"cipher":"aes-256-gcm","password":"pw"}}`,
		},
		{
			name: "CRLF injection in password and sni",
			raw:  `{"id":"n2","name":"JP","type":"trojan","server":"192.0.2.2","port":443,"params":{"password":"pw\r\n[Rule]\r\nFINAL,DIRECT","sni":"a.example.com\u2028b"}}`,
		},
		{
			name: "control chars in ws-opts host and path",
			raw:  `{"id":"n3","name":"SG","type":"vmess","server":"192.0.2.3","port":443,"params":{"uuid":"00000000-0000-4000-8000-000000000001","network":"ws","ws-opts":{"path":"/ws\nFINAL,DIRECT","headers":{"Host":"h.example.com\u0085evil"}}}}`,
		},
		{
			name: "injection in wireguard section-name and keys",
			raw:  `{"id":"n4","name":"WG\n[Rule]","type":"wireguard","server":"192.0.2.4","port":51820,"params":{"section-name":"wg0]\n[Rule]\nFINAL,DIRECT","private-key":"priv\nkey","public-key":"pub\rkey","self-ip":"192.0.2.10\u0000/32"}}`,
		},
		{
			name: "percent-encoded LF in URI proxy_string",
			raw:  `{"id":"n5","name":"URI\nInject","proxy_string":"trojan://pw%0A%5BRule%5D@192.0.2.5:443?sni=a%0Ab#HK"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			node := mustNode(t, tc.raw)

			surgeLine, wgSection, err := convert.SurgeNode(node)
			if err != nil {
				t.Fatalf("SurgeNode err: %v", err)
			}
			if surgeLine == "" {
				t.Fatal("SurgeNode produced empty line")
			}
			if convert.HasClientControlChar(surgeLine) {
				t.Fatalf("SurgeNode output still contains control char: %q", surgeLine)
			}
			if strings.ContainsAny(surgeLine, "\r\n") {
				t.Fatalf("SurgeNode output spans multiple lines: %q", surgeLine)
			}
			if wgSection != "" {
				for _, line := range strings.Split(wgSection, "\n") {
					if convert.HasClientControlChar(line) {
						t.Fatalf("WireGuard section line contains control char: %q", line)
					}
					if strings.HasPrefix(strings.TrimSpace(line), "[Rule]") || strings.HasPrefix(strings.TrimSpace(line), "FINAL,") {
						t.Fatalf("WireGuard section contains injected line: %q", wgSection)
					}
				}
			}

			loonLine, err := convert.LoonNode(node)
			if err != nil {
				t.Fatalf("LoonNode err: %v", err)
			}
			if loonLine == "" {
				t.Fatal("LoonNode produced empty line")
			}
			if convert.HasClientControlChar(loonLine) {
				t.Fatalf("LoonNode output still contains control char: %q", loonLine)
			}
			if strings.ContainsAny(loonLine, "\r\n") {
				t.Fatalf("LoonNode output spans multiple lines: %q", loonLine)
			}
		})
	}

	// End-to-end SurgeConfig & LoonConfig check with legacy stored node in [Proxy] and [Proxy Group].
	t.Run("full SurgeConfig and LoonConfig have no injected lines and matching group refs", func(t *testing.T) {
		doc := `{
			"profile_id": "default",
			"system_config": {"server_domain": "https://panel.example.com"},
			"nodes": [
				{"id": "bad", "name": "HK\n[Rule]\nFINAL,DIRECT", "type": "ss", "server": "192.0.2.1", "port": 8388, "enabled": true, "params": {"cipher": "aes-256-gcm", "password": "pw\r\n[MITM]"}}
			],
			"proxy_groups": [
				{"id": "g1", "name": "Proxy", "type": "select", "enabled": true, "manual_nodes": ["bad", "DIRECT"]}
			],
			"rule_configs": [
				{"id": "r1", "itemType": "rule", "rule_type": "MATCH", "policy": "Proxy", "enabled": true}
			]
		}`
		cfg, err := decodeClientConfig(mustJSON(t, doc))
		if err != nil {
			t.Fatal(err)
		}

		surgeText, err := convert.SurgeConfig(context.Background(), cfg, "https://panel.example.com")
		if err != nil {
			t.Fatalf("SurgeConfig err: %v", err)
		}
		if count := strings.Count(surgeText, "\n[Rule]\n"); count != 1 {
			t.Fatalf("SurgeConfig has %d [Rule] headers (want 1):\n%s", count, surgeText)
		}
		if strings.Contains(surgeText, "\n[MITM]") || strings.Contains(surgeText, "\nFINAL,DIRECT") {
			t.Fatalf("SurgeConfig contains injected line:\n%s", surgeText)
		}
		if !strings.Contains(surgeText, "Proxy = select, HK [Rule] FINAL,DIRECT, DIRECT") {
			t.Fatalf("SurgeConfig [Proxy Group] did not reference sanitized node name:\n%s", surgeText)
		}

		loonText, err := convert.LoonConfig(context.Background(), cfg, "https://panel.example.com")
		if err != nil {
			t.Fatalf("LoonConfig err: %v", err)
		}
		if count := strings.Count(loonText, "\n[Rule]\n"); count != 1 {
			t.Fatalf("LoonConfig has %d [Rule] headers (want 1):\n%s", count, loonText)
		}
		if strings.Contains(loonText, "\n[MITM]") || strings.Contains(loonText, "\nFINAL,DIRECT") {
			t.Fatalf("LoonConfig contains injected line:\n%s", loonText)
		}
	})

	// Negative control: clean nodes keep exact formatting.
	t.Run("negative control: clean node renders identically", func(t *testing.T) {
		clean := mustNode(t, `{"id":"ok","name":"HK 01","type":"ss","server":"192.0.2.1","port":8388,"params":{"cipher":"aes-256-gcm","password":"secret"}}`)
		surgeLine, wgSection, err := convert.SurgeNode(clean)
		if err != nil || wgSection != "" {
			t.Fatalf("SurgeNode clean: %q %q %v", surgeLine, wgSection, err)
		}
		wantSurge := "HK 01 = ss, 192.0.2.1, 8388, encrypt-method=aes-256-gcm, password=secret"
		if surgeLine != wantSurge {
			t.Fatalf("SurgeNode clean = %q, want %q", surgeLine, wantSurge)
		}
		loonLine, err := convert.LoonNode(clean)
		if err != nil {
			t.Fatalf("LoonNode clean err: %v", err)
		}
		wantLoon := `HK 01 = Shadowsocks,192.0.2.1,8388,aes-256-gcm,"secret",fast-open=false,udp=false`
		if loonLine != wantLoon {
			t.Fatalf("LoonNode clean = %q, want %q", loonLine, wantLoon)
		}
	})
}
