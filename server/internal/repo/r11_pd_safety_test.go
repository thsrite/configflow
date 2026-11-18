package repo

import (
	"context"
	"testing"

	"configflow/internal/jsonx"
)

const r11PDManifest = `{"version":1,"future":{"a":[false,1.0,null]},"spec":{"lan":"bridge","pool":"pool-example","client":"*C0","client_interface":"wan","hold":3600},"resources":[{"role":"script","path":"/system/script","id":"*65","config":{"name":"cf-pd-watch","source":"synthetic script","policy":"read,write,test"},"future-resource":true}]}`

func TestR11PDPublicationStorage(t *testing.T) {
	r, l := r10Repository(t)
	router := rsRouter(t, r)
	release := l.hold(t, router)
	defer release()
	original := parse(t, r11PDManifest)
	noError(t, r.SaveRouterPublication(bg, "live", router, RouterPublication{IPv6PDGuard: Some(original)}))
	saved := rsRouter(t, r)
	wantEqual(t, saved.Value("ipv6_pd_guard_state"), original)
	_, _, e := r.UpdateProfileTransaction(bg, "live", func(_ context.Context, c *jsonx.Object) (*jsonx.Object, error) {
		c.GetObject("routers").GetList("items")[0].(*jsonx.Object).Set("name", "Concurrent metadata")
		return nil, nil
	})
	noError(t, e)
	restored := original.DeepCopy()
	restored.GetList("resources")[0].(*jsonx.Object).Set("id", "*AA")
	noError(t, r.SaveRouterPublication(bg, "live", router, RouterPublication{IPv6PDGuard: Some(restored)}))
	saved = rsRouter(t, r)
	wantEqual(t, saved.Value("ipv6_pd_guard_state"), restored)
	wantEqual(t, saved.Value("name"), "Concurrent metadata")
	noError(t, r.SaveRouterPublication(bg, "live", router, RouterPublication{LastRelease: Some(jsonx.Obj("ok", false))}))
	wantEqual(t, rsRouter(t, r).Value("ipv6_pd_guard_state"), restored)
	noError(t, r.SaveRouterPublication(bg, "live", router, RouterPublication{IPv6PDGuard: Some(nil)}))
	if rsRouter(t, r).Has("ipv6_pd_guard_state") {
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
				noError(t, r.SaveRouterPublication(bg, "live", router, RouterPublication{IPv6PDGuard: Some(parse(t, r11PDManifest))}))
				release()
				fingerprint := must(r.Store().Fingerprint())(t)
				config := must(r.GetProfile(bg, "live"))(t)
				routers := config.GetObject("routers").DeepCopy()
				row := routers.GetList("items")[0].(*jsonx.Object)
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
				wantError(t, rsReplaceRouters(t, r, writer, routers), ErrInUse, "")
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
	noError(t, r.SaveRouterPublication(bg, "live", router, RouterPublication{IPv6PDGuard: Some(parse(t, r11PDManifest))}))
	release()
	replacement := rsRouter(t, r)
	replacement.Set("username", "replacement-api")
	old := SetHooks(Hooks{})
	defer SetHooks(old)
	before := must(r.Store().Fingerprint())(t)
	wantError(t, rsReplaceRouters(t, r, "fields", jsonx.Obj("items", []any{replacement})), ErrInUse, "")
	if must(r.Store().Fingerprint())(t) != before {
		t.Fatal("absent JobLock allowed commit")
	}
}
