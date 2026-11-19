package subs

import (
	"encoding/base64"
	"testing"
)

func TestAuditVMessExactMembers(t *testing.T) {
	raw := `{"ps":"proven","PS":"spoofed","add":"example.com","ADD":"other.example.com","port":443,"PORT":80,"id":"synthetic"}`
	got := ParseList("vmess://" + base64.StdEncoding.EncodeToString([]byte(raw)))
	if len(got) != 1 {
		t.Fatalf("nodes: %#v", got)
	}
	if got[0].Node.Name != "proven" || got[0].Node.Server != "example.com" || got[0].Node.Port != 443 {
		t.Fatalf("case variants consumed: name=%q server=%q port=%d", got[0].Node.Name, got[0].Node.Server, got[0].Node.Port)
	}
}
