package repo

import (
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

const r11PDManifest = `{"version":1,"future":{"a":[false,1.0,null]},"spec":{"lan":"bridge","pool":"pool-example","client":"*C0","client_interface":"wan","hold":3600},"resources":[{"role":"script","path":"/system/script","id":"*65","config":{"name":"cf-pd-watch","source":"synthetic script","policy":"read,write,test"},"future-resource":true}]}`

func TestR11PDPublicationStorage(t *testing.T) {
	r, l := r10Repository(t)
	router := rsRouter(t, r)
	routerRaw := must(model.Marshal(router))(t)
	release := l.hold(t, router)
	defer release()
	noError(t, r.SaveRouterPublicationJSON(bg, "live", routerRaw, RouterPublicationJSON{
		IPv6PDGuard: JSONUpdate{Set: true, Value: json.RawMessage(r11PDManifest)},
	}))
	saved := rsRouterObj(t, r)
	wantJSONEqual(t, saved.Value("ipv6_pd_guard_state"), json.RawMessage(r11PDManifest))
	_, err := r.UpdateProfile(bg, "live", func(_ context.Context, p *model.Profile) error {
		p.Routers.Items[0].Name = "Concurrent metadata"
		return nil
	})
	noError(t, err)
	restored := parseJSONObj(t, []byte(r11PDManifest))
	restored.GetList("resources")[0].(*testObj).Set("id", "*AA")
	noError(t, r.SaveRouterPublicationJSON(bg, "live", routerRaw, RouterPublicationJSON{
		IPv6PDGuard: JSONUpdate{Set: true, Value: mustMarshalJSON(t, restored)},
	}))
	saved = rsRouterObj(t, r)
	wantJSONEqual(t, saved.Value("ipv6_pd_guard_state"), restored)
	wantJSONEqual(t, saved.Value("name"), "Concurrent metadata")
	noError(t, r.SaveRouterPublicationJSON(bg, "live", routerRaw, RouterPublicationJSON{
		LastRelease: JSONUpdate{Set: true, Value: json.RawMessage(`{"ok":false}`)},
	}))
	wantJSONEqual(t, rsRouterObj(t, r).Value("ipv6_pd_guard_state"), restored)
	noError(t, r.SaveRouterPublicationJSON(bg, "live", routerRaw, RouterPublicationJSON{
		IPv6PDGuard: JSONUpdate{Set: true, Value: json.RawMessage("null")},
	}))
	if _, ok := rsRouterObj(t, r).Get("ipv6_pd_guard_state"); ok {
		t.Fatal("explicit null did not withdraw")
	}
}

func TestR11PDAllWritersProtectProof(t *testing.T) {
	for _, writer := range rsWriters {
		for _, change := range []string{"remove-router", "remove-manifest", "replace-manifest", "endpoint"} {
			t.Run(writer+"/"+change, func(t *testing.T) {
				r, l := r10Repository(t)
				router := rsRouter(t, r)
				release := l.hold(t, router)
				noError(t, r.SaveRouterPublicationJSON(bg, "live", must(model.Marshal(router))(t), RouterPublicationJSON{
					IPv6PDGuard: JSONUpdate{Set: true, Value: json.RawMessage(r11PDManifest)},
				}))
				release()
				fingerprint := must(r.Store().Fingerprint())(t)
				routers := parseJSONObj(t, must(r.ExportProfileJSON(bg, "live"))(t)).GetObject("routers")
				row := routers.GetList("items")[0].(*testObj)
				switch change {
				case "remove-router":
					routers.Set("items", []any{})
				case "remove-manifest":
					row.Delete("ipv6_pd_guard_state")
				case "replace-manifest":
					row.GetObject("ipv6_pd_guard_state").Set("resources", []any{})
				default:
					row.Set("endpoint", "http://192.0.2.9")
				}
				wantError(t, rsReplaceRouters(t, r, writer, mustMarshalJSON(t, routers)), ErrInUse, "")
				if must(r.Store().Fingerprint())(t) != fingerprint {
					t.Fatal("refused mutation committed")
				}
			})
		}
	}
}

func TestR11PDAbsentJobLockFailsClosed(t *testing.T) {
	r, l := r10Repository(t)
	router := rsRouter(t, r)
	release := l.hold(t, router)
	noError(t, r.SaveRouterPublicationJSON(bg, "live", must(model.Marshal(router))(t), RouterPublicationJSON{
		IPv6PDGuard: JSONUpdate{Set: true, Value: json.RawMessage(r11PDManifest)},
	}))
	release()
	replacement := rsRouterObj(t, r)
	replacement.Set("username", "replacement-api")
	old := SetHooks(Hooks{})
	defer SetHooks(old)
	before := must(r.Store().Fingerprint())(t)
	wantError(t, rsReplaceRouters(t, r, "fields", mustMarshalJSON(t, testObjKV("items", []any{replacement}))), ErrInUse, "")
	if must(r.Store().Fingerprint())(t) != before {
		t.Fatal("absent JobLock allowed commit")
	}
}
