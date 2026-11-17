package repo

import (
	"context"
	"sync"
	"testing"

	"configflow/internal/jsonx"
)

const r10Connection = `{"id":"synthetic","name":"Synthetic","type":"routeros","endpoint":"http://192.0.2.1","username":"api","password":"synthetic-password","verify_tls":true}`
const r10FakeManifest = `{"version":1,"spec":{"cidr":"fd00:face::/64","gateway":"fe80::4%bridge"},"resources":[{"path":"/ipv6/route","id":"*65","config":{"dst-address":"fd00:face::/64","gateway":"fe80::4%bridge"}}]}`
const r10RoutesManifest = `{"version":1,"gateway":"fe80::4%bridge","resources":[{"path":"/ipv6/route","id":"*66","config":{"dst-address":"2001:db8:a::/48","gateway":"fe80::4%bridge"}}]}`

func r10Repository(t *testing.T) (*Repository, *rsJobLocks) {
	t.Helper()
	locks := &rsJobLocks{mu: sync.Mutex{}, held: map[string]bool{}}
	previous := SetHooks(Hooks{JobLock: locks.acquire})
	t.Cleanup(func() { SetHooks(previous) })
	r := newRepo(t)
	must(r.CreateProfile(bg, jsonx.Obj("id", "live", "name", "Synthetic"), ""))(t)
	_, _, err := r.SaveProfile(bg, "live", jsonx.Obj("routers", jsonx.Obj("items", []any{parse(t, r10Connection)})), nil)
	noError(t, err)
	return r, locks
}
func r10Publication(field string, value any) RouterPublication {
	if field == "ipv6_fakeip_state" {
		return RouterPublication{IPv6FakeIP: Some(value)}
	}
	return RouterPublication{IPv6Routes: Some(value)}
}
func r10Persist(t *testing.T, r *Repository, locks *rsJobLocks, field string) *jsonx.Object {
	t.Helper()
	router := rsRouter(t, r)
	release := locks.hold(t, router)
	defer release()
	raw := r10FakeManifest
	if field == "ipv6_routes_state" {
		raw = r10RoutesManifest
	}
	noError(t, r.SaveRouterPublication(bg, "live", router, r10Publication(field, parse(t, raw))))
	return rsRouter(t, r)
}

func TestR10EveryWriterPreservesIPv6Ownership(t *testing.T) {
	for _, field := range []string{"ipv6_fakeip_state", "ipv6_routes_state"} {
		for _, writer := range rsWriters {
			for _, change := range []string{"remove-router", "remove-manifest", "replace-manifest", "id", "type", "endpoint"} {
				t.Run(field+"/"+writer+"/"+change, func(t *testing.T) {
					r, l := r10Repository(t)
					r10Persist(t, r, l, field)
					before := must(r.ExportAll(bg, false))(t)
					fingerprint := must(r.Store().Fingerprint())(t)
					routers := before.GetObject("profiles").GetObject("live").GetObject("routers").DeepCopy()
					router := routers.GetList("items")[0].(*jsonx.Object)
					switch change {
					case "remove-router":
						routers.Set("items", []any{})
					case "remove-manifest":
						router.Delete(field)
					case "replace-manifest":
						router.GetObject(field).Set("resources", []any{})
					default:
						router.Set(change, "changed")
					}
					wantError(t, rsReplaceRouters(t, r, writer, routers), ErrInUse, "")
					if got := must(r.Store().Fingerprint())(t); got != fingerprint {
						t.Fatal("refusal wrote database")
					}
					wantEqual(t, must(r.ExportAll(bg, false))(t), before)
				})
			}
		}
	}
}
func TestR10IPv6PublicationPersistenceAndWithdrawal(t *testing.T) {
	for _, field := range []string{"ipv6_fakeip_state", "ipv6_routes_state"} {
		t.Run(field, func(t *testing.T) {
			r, l := r10Repository(t)
			original := r10Persist(t, r, l, field)
			release := l.hold(t, original)
			defer release()
			_, _, err := r.UpdateProfileTransaction(bg, "live", func(_ context.Context, c *jsonx.Object) (*jsonx.Object, error) {
				c.GetObject("routers").GetList("items")[0].(*jsonx.Object).Set("name", "Concurrent metadata")
				return nil, nil
			})
			noError(t, err)
			restored := original.GetObject(field).DeepCopy()
			restored.GetList("resources")[0].(*jsonx.Object).Set("id", "*AA")
			publication := r10Publication(field, restored)
			publication.DNSGuard = Some(jsonx.Obj("resources", []any{}))
			noError(t, r.SaveRouterPublication(bg, "live", original, publication))
			noError(t, r.SaveRouterPublication(bg, "live", original, RouterPublication{LastRelease: Some(jsonx.Obj("ok", false))}))
			saved := rsRouter(t, r)
			wantEqual(t, saved.Value(field), restored)
			wantEqual(t, saved.Value("name"), "Concurrent metadata")
			noError(t, r.SaveRouterPublication(bg, "live", original, r10Publication(field, nil)))
			saved = rsRouter(t, r)
			if _, ok := saved.Get(field); ok {
				t.Fatal("explicit withdrawal did not remove ownership")
			}
			wantEqual(t, saved.Value("dns_guard_state"), jsonx.Obj("resources", []any{}))
		})
	}
}
func TestR10IPv6ConnectionRepairCloneAndDeleteGuards(t *testing.T) {
	for _, field := range []string{"ipv6_fakeip_state", "ipv6_routes_state"} {
		for _, change := range []string{"endpoint", "username", "password", "verify_tls"} {
			t.Run(field+"/stale_"+change, func(t *testing.T) {
				r, l := r10Repository(t)
				original := r10Persist(t, r, l, field)
				stale := original.DeepCopy()
				stale.Set(change, rsChanged(change))
				before := must(r.Store().Fingerprint())(t)
				release := l.hold(t, original)
				wantError(t, r.SaveRouterPublication(bg, "live", stale, r10Publication(field, nil)), ErrInUse, "")
				release()
				if got := must(r.Store().Fingerprint())(t); got != before {
					t.Fatal("refused stale publication changed database")
				}
			})
		}
		for _, change := range []string{"username", "password", "verify_tls"} {
			t.Run(field+"/repair_"+change, func(t *testing.T) {
				r, l := r10Repository(t)
				original := r10Persist(t, r, l, field)
				replacement := original.DeepCopy()
				replacement.Set(change, rsChanged(change))
				noError(t, rsReplaceRouters(t, r, "fields", jsonx.Obj("items", []any{replacement})))
				wantEqual(t, rsRouter(t, r).Value(field), original.Value(field))
			})
		}
		for _, operation := range []string{"clone", "delete-profile", "reset", "delete-router"} {
			t.Run(field+"/"+operation, func(t *testing.T) {
				r, l := r10Repository(t)
				original := r10Persist(t, r, l, field)
				switch operation {
				case "clone":
					must(r.CloneProfile(bg, "live", jsonx.Obj("id", "copy", "name", "Copy")))(t)
					wantEqual(t, must(r.GetProfile(bg, "copy"))(t).GetObject("routers").Value("items"), []any{})
					wantEqual(t, rsRouter(t, r).Value(field), original.Value(field))
				case "delete-profile":
					wantError(t, r.DeleteProfile(bg, "live", ""), ErrInUse, "")
				case "reset":
					_, err := r.ResetAll(bg)
					wantError(t, err, ErrInUse, "")
				case "delete-router":
					release := l.hold(t, original)
					wantError(t, r.SaveRouterSettings(bg, "live", original, nil), ErrInUse, "")
					release()
				}
			})
		}
	}
}
