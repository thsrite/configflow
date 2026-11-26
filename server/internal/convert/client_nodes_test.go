package convert

import (
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/model"
)

func TestTypedClientNodeLines(t *testing.T) {
	tests := []struct{ source, surge, loon string }{
		{`{"name":"N","type":"ss","server":"example.com","port":443,"params":{"cipher":"aes-128-gcm","password":123,"udp":true}}`, "N = ss, example.com, 443, encrypt-method=aes-128-gcm, password=123, udp-relay=true", `N = Shadowsocks,example.com,443,aes-128-gcm,"123",fast-open=false,udp=true`},
		{`{"name":"N","type":"http","server":"example.com","port":443,"params":{"username":"user","password":"p"}}`, "N = http, example.com, 443, user, p", `N = http,example.com,443,user,"p"`},
		{`{"name":"N","type":"trojan","server":"example.com","port":443,"params":{"password":"p","sni":"s.example.com"}}`, "N = trojan, example.com, 443, password=p, sni=s.example.com", `N = Trojan,example.com,443,"p",transport=tcp,sni=s.example.com,skip-cert-verify=false,fast-open=false,udp=false`},
	}
	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			var node model.Node
			if err := json.Unmarshal([]byte(tt.source), &node); err != nil {
				t.Fatal(err)
			}
			before, _ := model.Marshal(node)
			surge, section, err := SurgeNode(node)
			if err != nil || section != "" || surge != tt.surge {
				t.Fatal(surge, section, err)
			}
			loon, err := LoonNode(node)
			if err != nil || loon != tt.loon {
				t.Fatal(loon, err)
			}
			after, _ := model.Marshal(node)
			if string(before) != string(after) {
				t.Fatal("input modified")
			}
		})
	}
}
func TestTypedClientFlagDefaults(t *testing.T) {
	var node model.Node
	if err := json.Unmarshal([]byte(`{"name":"N","type":"http","server":"example.com","port":443,"params":{"tls":"true"}}`), &node); err != nil {
		t.Fatal(err)
	}
	output, err := LoonNode(node)
	if err != nil || strings.Contains(output, "https") || output != "N = http,example.com,443" {
		t.Fatal(output, err)
	}
}
