package repo

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"configflow/internal/model"
)

const r10Connection = `{"id":"synthetic","name":"Synthetic","type":"routeros","endpoint":"http://192.0.2.1","username":"api","password":"synthetic-password","verify_tls":true}`
const r10FakeManifest = `{"version":1,"spec":{"cidr":"fd00:face::/64","gateway":"fe80::4%bridge"},"resources":[{"path":"/ipv6/route","id":"*65","config":{"dst-address":"fd00:face::/64","gateway":"fe80::4%bridge"}}]}`
const r10RoutesManifest = `{"version":1,"gateway":"fe80::4%bridge","resources":[{"path":"/ipv6/route","id":"*66","config":{"dst-address":"2001:db8:a::/48","gateway":"fe80::4%bridge"}}]}`

func r10Repository(t *testing.T) (*Repository, *rsJobLocks) {
	t.Helper()
	locks := &rsJobLocks{mu: sync.Mutex{}, held: map[string]bool{}}
	previous := SetHooks(Hooks{RecordHooks: RecordHooks{JobLockRecord: locks.acquire}})
	t.Cleanup(func() { SetHooks(previous) })
	r := newRepo(t)
	must(r.CreateProfileRecord(bg, ProfileCreation{ID: "live", Name: "Synthetic"}, ""))(t)
	_, _, err := r.SaveProfileFieldsJSON(bg, "live", json.RawMessage(`{"routers":{"items":[`+r10Connection+`]}}`), nil)
	noError(t, err)
	return r, locks
}

func r10Publication(field string, value json.RawMessage) RouterPublicationJSON {
	if field == "ipv6_fakeip_state" {
		return RouterPublicationJSON{IPv6FakeIP: JSONUpdate{Set: true, Value: value}}
	}
	return RouterPublicationJSON{IPv6Routes: JSONUpdate{Set: true, Value: value}}
}

func r10Persist(t *testing.T, r *Repository, locks *rsJobLocks, field string) model.Router {
	t.Helper()
	router := rsRouter(t, r)
	release := locks.hold(t, router)
	defer release()
	raw := r10FakeManifest
	if field == "ipv6_routes_state" {
		raw = r10RoutesManifest
	}
	noError(t, r.SaveRouterPublicationJSON(bg, "live", must(model.Marshal(router))(t), r10Publication(field, json.RawMessage(raw))))
	return rsRouter(t, r)
}

func TestR10EveryWriterPreservesIPv6Ownership(t *testing.T) {
	for _, field := range []string{"ipv6_fakeip_state", "ipv6_routes_state"} {
		for _, writer := range rsWriters {
			for _, change := range []string{"remove-router", "remove-manifest", "replace-manifest", "id", "type", "endpoint"} {
				t.Run(field+"/"+writer+"/"+change, func(t *testing.T) {
					r, l := r10Repository(t)
					r10Persist(t, r, l, field)
					before := must(r.ExportAllJSON(bg, false))(t)
					fingerprint := must(r.Store().Fingerprint())(t)
					routers := parseJSONObj(t, before).GetObject("profiles").GetObject("live").GetObject("routers")
					router := routers.GetList("items")[0].(*testObj)
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
					wantError(t, rsReplaceRouters(t, r, writer, mustMarshalJSON(t, routers)), ErrInUse, "")
					if got := must(r.Store().Fingerprint())(t); got != fingerprint {
						t.Fatal("refusal wrote database")
					}
					wantJSONEqual(t, must(r.ExportAllJSON(bg, false))(t), before)
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
			originalRaw := must(model.Marshal(original))(t)
			originalObj := parseJSONObj(t, originalRaw)
			release := l.hold(t, original)
			defer release()
			_, err := r.UpdateProfile(bg, "live", func(_ context.Context, p *model.Profile) error {
				p.Routers.Items[0].Name = "Concurrent metadata"
				return nil
			})
			noError(t, err)
			restored := parseJSONObj(t, mustMarshalJSON(t, originalObj.GetObject(field)))
			restored.GetList("resources")[0].(*testObj).Set("id", "*AA")
			publication := r10Publication(field, mustMarshalJSON(t, restored))
			publication.DNSGuard = JSONUpdate{Set: true, Value: json.RawMessage(`{"resources":[]}`)}
			noError(t, r.SaveRouterPublicationJSON(bg, "live", originalRaw, publication))
			noError(t, r.SaveRouterPublicationJSON(bg, "live", originalRaw, RouterPublicationJSON{
				LastRelease: JSONUpdate{Set: true, Value: json.RawMessage(`{"ok":false}`)},
			}))
			saved := rsRouterObj(t, r)
			wantJSONEqual(t, saved.Value(field), restored)
			wantJSONEqual(t, saved.Value("name"), "Concurrent metadata")
			noError(t, r.SaveRouterPublicationJSON(bg, "live", originalRaw, r10Publication(field, json.RawMessage("null"))))
			saved = rsRouterObj(t, r)
			if _, ok := saved.Get(field); ok {
				t.Fatal("explicit withdrawal did not remove ownership")
			}
			wantJSONEqual(t, saved.Value("dns_guard_state"), json.RawMessage(`{"resources":[]}`))
		})
	}
}

func TestR10IPv6ConnectionRepairCloneAndDeleteGuards(t *testing.T) {
	for _, field := range []string{"ipv6_fakeip_state", "ipv6_routes_state"} {
		for _, change := range []string{"endpoint", "username", "password", "verify_tls"} {
			t.Run(field+"/stale_"+change, func(t *testing.T) {
				r, l := r10Repository(t)
				original := r10Persist(t, r, l, field)
				stale := parseJSONObj(t, must(model.Marshal(original))(t))
				stale.Set(change, rsChanged(change))
				before := must(r.Store().Fingerprint())(t)
				release := l.hold(t, original)
				wantError(t, r.SaveRouterPublicationJSON(bg, "live", mustMarshalJSON(t, stale), r10Publication(field, json.RawMessage("null"))), ErrInUse, "")
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
				originalObj := parseJSONObj(t, must(model.Marshal(original))(t))
				replacement := parseJSONObj(t, must(model.Marshal(original))(t))
				replacement.Set(change, rsChanged(change))
				noError(t, rsReplaceRouters(t, r, "fields", mustMarshalJSON(t, testObjKV("items", []any{replacement}))))
				wantJSONEqual(t, rsRouterObj(t, r).Value(field), originalObj.Value(field))
			})
		}
		for _, operation := range []string{"clone", "delete-profile", "reset", "delete-router"} {
			t.Run(field+"/"+operation, func(t *testing.T) {
				r, l := r10Repository(t)
				original := r10Persist(t, r, l, field)
				originalRaw := must(model.Marshal(original))(t)
				originalObj := parseJSONObj(t, originalRaw)
				switch operation {
				case "clone":
					must(r.CloneProfileRecord(bg, "live", ProfileCreation{ID: "copy", Name: "Copy"}))(t)
					if items := must(r.Profile(bg, "copy"))(t).Routers.Items; len(items) != 0 {
						t.Fatalf("clone routers %+v", items)
					}
					wantJSONEqual(t, rsRouterObj(t, r).Value(field), originalObj.Value(field))
				case "delete-profile":
					wantError(t, r.DeleteProfile(bg, "live", ""), ErrInUse, "")
				case "reset":
					_, err := r.ResetAllJSON(bg)
					wantError(t, err, ErrInUse, "")
				case "delete-router":
					release := l.hold(t, original)
					wantError(t, r.SaveRouterSettingsJSON(bg, "live", originalRaw, nil), ErrInUse, "")
					release()
				}
			})
		}
	}
}

func TestR10MissingJobLockRefusesProtectedTransactions(t *testing.T) {
	for _, field := range []string{"ipv6_fakeip_state", "ipv6_routes_state"} {
		t.Run(field, func(t *testing.T) {
			r, l := r10Repository(t)
			r10Persist(t, r, l, field)
			replacement := rsRouterObj(t, r)
			replacement.Set("username", "replacement-api")
			previous := SetHooks(Hooks{})
			t.Cleanup(func() { SetHooks(previous) })
			before := must(r.Store().Fingerprint())(t)
			wantError(t, rsReplaceRouters(t, r, "fields", mustMarshalJSON(t, testObjKV("items", []any{replacement}))), ErrInUse, "路由器正在发布或同步")
			if got := must(r.Store().Fingerprint())(t); got != before {
				t.Fatal("missing JobLock did not fail closed")
			}
		})
	}
}
