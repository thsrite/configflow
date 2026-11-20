package config

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/api/linuxgateway"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/repo"
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
	if _, err := s.Repo.UpdateProfileMetadata(s.Context(), "default", jsonx.Obj("name", "Phone @ Home + Office")); err != nil {
		t.Fatal(err)
	}
	response := s.Get("/api/config/loon")
	response.WantStatus(200)
	if got := response.Header.Get("Content-Disposition"); got != "inline; filename*=UTF-8''Phone%20%40%20Home%20%2B%20Office.lcf" {
		t.Fatalf("Loon filename %q", got)
	}
}

// TestLiveAndLinuxGatewayAuthUseRequestSnapshotOnTokenRotation rotates the
// stored token after an earlier repo.GetConfig in the same request. The live
// route authenticates and renders from that request-scoped compat view, so the
// pre-rotation token still wins. The Linux gateway provider makes one typed
// read inside the handler for both its credential check and its render, so it
// sees the rotated token; it never mixes the two generations.
func TestLiveAndLinuxGatewayAuthUseRequestSnapshotOnTokenRotation(t *testing.T) {
	mihomoenv.Wire()
	tests := []struct {
		name               string
		path               string
		expectedStatus     int
		snapshot, rejected string
	}{
		{"live config", "/api/config/loon", 200, "token-before-rotation", "token-after-rotation"},
		{"linux gateway provider", "/api/custom-config/linux-gateway/rules/missing", 200, "token-after-rotation", "token-before-rotation"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, candidate := range []struct {
				label, query string
				wantStatus   int
			}{
				{"request snapshot token succeeds", tt.snapshot, tt.expectedStatus},
				{"token from the other generation rejected", tt.rejected, 401},
			} {
				t.Run(candidate.label, func(t *testing.T) {
					var s *httpxtest.Server
					rotated := false
					s = httpxtest.New(t, httpxtest.Options{
						Register: httpxtest.Routes(Register, linuxgateway.Register),
						Setup: func(app *httpx.App) {
							app.BeforeRequest(func(c *httpx.Ctx) (*httpx.Response, error) {
								if rotated {
									return nil, nil
								}
								if _, err := repo.GetConfig(c.Context(), c.ProfileID); err != nil {
									return nil, err
								}
								rotated = true
								if err := s.Repo.UpdateSystem(context.Background(), func(_ context.Context, sys *model.System) error {
									return sys.SystemConfig.SetJSON("config_token", json.RawMessage(`"token-after-rotation"`))
								}); err != nil {
									return nil, err
								}
								return nil, nil
							})
						},
					})
					if err := s.Repo.UpdateSystem(context.Background(), func(_ context.Context, sys *model.System) error {
						return sys.SystemConfig.SetJSON("config_token", json.RawMessage(`"token-before-rotation"`))
					}); err != nil {
						t.Fatal(err)
					}
					s.Get(tt.path + "?token=" + candidate.query).WantStatus(candidate.wantStatus)
					// Subsequent requests observe the rotated token from the new snapshot.
					s.Get(tt.path + "?token=token-after-rotation").WantStatus(tt.expectedStatus)
					s.Get(tt.path + "?token=token-before-rotation").WantStatus(401)
				})
			}
		})
	}
}
