package config

import (
	"context"
	"strings"
	"testing"

	"configflow/internal/convert/mihomoenv"
	"configflow/internal/jsonx"
)

func TestLiveConfigProfileAndAuthentication(t *testing.T) {
	mihomoenv.Wire()
	s := exportApp(t)
	_, err := s.Repo.CreateProfile(s.Context(), jsonx.Obj("id", "phone", "name", "手机:/配置"), "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Repo.UpdateSystemTransaction(s.Context(), func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.GetObject("system_config").Update(jsonx.Obj("config_token", "synthetic-config-token", "rule_proxy_token", "synthetic-rule-token", "retired_rule_proxy_tokens", []any{"synthetic-retired-token"}))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"/api/config", "/api/config/phone"} {
		for _, engine := range []string{"mihomo", "surge", "loon", "mosdns", "oxidns"} {
			path := prefix + "/" + engine
			for _, token := range []string{"", "wrong", "synthetic-rule-token", "synthetic-retired-token"} {
				s.Get(path + "?token=" + token).WantStatus(401)
			}
			response := s.Get(path + "?token=synthetic-config-token")
			response.WantStatus(200)
			if len(response.Body) == 0 {
				t.Fatal("empty live config")
			}
			if engine == "loon" && prefix != "/api/config" && !strings.Contains(response.Header.Get("Content-Disposition"), "%E6%89%8B%E6%9C%BA") {
				t.Fatal("Loon filename lacks profile name")
			}
		}
	}
	named := s.Get("/api/config/phone/loon/ignored.lcf?token=synthetic-config-token")
	named.WantStatus(200)
	normal := s.Get("/api/config/phone/loon?token=synthetic-config-token")
	if string(named.Body) != string(normal.Body) {
		t.Fatal("filename alias changes profile output")
	}
}

func TestLoonFilenamePercentEncoding(t *testing.T) {
	mihomoenv.Wire()
	s := exportApp(t)
	if _, err := s.Repo.UpdateProfile(s.Context(), "default", jsonx.Obj("name", "Phone @ Home + Office")); err != nil {
		t.Fatal(err)
	}
	response := s.Get("/api/config/loon")
	response.WantStatus(200)
	if got := response.Header.Get("Content-Disposition"); got != "inline; filename*=UTF-8''Phone%20%40%20Home%20%2B%20Office.lcf" {
		t.Fatalf("Loon filename %q", got)
	}
}
