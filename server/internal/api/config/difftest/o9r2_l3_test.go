package difftest

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
)

// O9 round 2 L3: a legacy stored manual node whose name or params contain line
// breaks injects extra INI lines under Flask's Surge/Loon generators; Go
// neutralizes line-breaking control characters during generation while clean
// nodes stay identical to Flask.
func TestO9R2ClientGenerationLineDefenseAgainstFlask(t *testing.T) {
	seedLegacy := func(injected bool) func(*httpxtest.Server) {
		return func(srv *httpxtest.Server) {
			ctx := srv.Context()
			nodeJSON := `[{"id":"n1","name":"HK 01","type":"ss","server":"192.0.2.1","port":8388,"enabled":true,"params":{"cipher":"aes-256-gcm","password":"clean"}}]`
			if injected {
				nodeJSON = `[{"id":"n1","name":"HK\n[Rule]\nFINAL,DIRECT","type":"ss","server":"192.0.2.1","port":8388,"enabled":true,"params":{"cipher":"aes-256-gcm","password":"pw\r\n[MITM]"}}]`
			}
			if err := srv.Repo.UpdateShared(ctx, func(_ context.Context, shared *model.Shared) error {
				return shared.SetJSON("nodes", json.RawMessage(nodeJSON))
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := srv.Repo.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
				if err := p.SetJSON("proxy_groups", json.RawMessage(`[{"id":"g1","name":"Proxy","type":"select","enabled":true,"manual_nodes":["n1","DIRECT"]}]`)); err != nil {
					return err
				}
				return p.SetJSON("rule_configs", json.RawMessage(`[{"id":"m1","itemType":"rule","rule_type":"MATCH","policy":"Proxy","enabled":true}]`))
			}); err != nil {
				t.Fatal(err)
			}
		}
	}

	t.Run("injected legacy node blocked in Go while Flask emits injected lines", func(t *testing.T) {
		w := newWorld(t, seedLegacy(true))
		for _, engine := range []string{"surge", "loon"} {
			t.Run(engine, func(t *testing.T) {
				req := request{Method: "GET", Path: "/api/config/" + engine + "?token=c34-config-secret"}
				py := w.py.send(t, req)
				if py.Status != 200 {
					t.Fatalf("Flask %s status = %d, want 200", engine, py.Status)
				}
				pyText, _ := py.Body.(string)
				if !strings.Contains(pyText, "\n[MITM]") || strings.Count(pyText, "\n[Rule]\n") <= 1 {
					t.Fatalf("Flask %s baseline did not emit injected lines:\n%s", engine, pyText)
				}

				got := w.srv.Get(req.Path).WantStatus(200)
				goText := string(got.Body)
				if strings.Contains(goText, "\n[MITM]") || strings.Contains(goText, "\nFINAL,DIRECT") {
					t.Fatalf("Go %s emitted injected line:\n%s", engine, goText)
				}
				if count := strings.Count(goText, "\n[Rule]\n"); count != 1 {
					t.Fatalf("Go %s has %d [Rule] headers, want 1:\n%s", engine, count, goText)
				}
			})
		}
	})

	t.Run("negative control: clean node matches Flask strictly", func(t *testing.T) {
		w := newWorld(t, seedLegacy(false))
		for _, engine := range []string{"surge", "loon"} {
			w.check(request{Method: "GET", Path: "/api/config/" + engine + "?token=c34-config-secret"})
		}
	})
}
