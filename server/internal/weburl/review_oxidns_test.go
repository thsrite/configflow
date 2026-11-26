package weburl

import "testing"

func TestOxidnsUnicodeCredentials(t *testing.T) {
	for _, raw := range []string{"https://user:密码@dns.example/q", "https://u:p%2Fword@dns.example/q", "https://名:pass^word@dns.example/q"} {
		u, err := Parse(raw)
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		again, err := Parse(u.String())
		if err != nil || u.String() != again.String() {
			t.Fatalf("userinfo double escaped: %v %v", u, again)
		}
	}
	u, _ := Parse("https://user:密码@dns.example/q")
	if p, _ := u.User.Password(); p != "密码" {
		t.Fatalf("password changed: %q", p)
	}
}
