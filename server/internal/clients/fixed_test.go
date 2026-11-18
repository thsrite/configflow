package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/repo"
	"configflow/internal/routers"
	"configflow/internal/routers/routerfake"
)

func tokenPython(t *testing.T, input any) struct {
	Token    string          `json:"token"`
	Decoded  json.RawMessage `json:"decoded"`
	Rejected bool            `json:"rejected"`
} {
	t.Helper()
	home, _ := os.UserHomeDir()
	wd, _ := os.Getwd()
	cmd := exec.CommandContext(t.Context(), filepath.Join(home, "cf-test/.venv/bin/python"), filepath.Join(wd, "testdata/r17_token_reference.py"))
	cmd.Stdin = bytes.NewReader(raw(input))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Python token %v %s", err, output)
	}
	var result struct {
		Token    string          `json:"token"`
		Decoded  json.RawMessage `json:"decoded"`
		Rejected bool            `json:"rejected"`
	}
	if err = json.Unmarshal(output, &result); err != nil {
		t.Fatal(err, string(output))
	}
	return result
}
func TestR17TokenBothDirections(t *testing.T) {
	now := time.Unix(1800000000, 0)
	signer := FixedIPToken{Secret: "synthetic-fixed-secret", Now: func() time.Time { return now }}
	for _, payload := range []json.RawMessage{json.RawMessage(`{"a":"x"}`), raw(map[string]string{"name": "中文 🧪", "proof": strings.Repeat("bounded evidence", 50)})} {
		token, err := signer.Sign(payload)
		if err != nil {
			t.Fatal(err)
		}
		python := tokenPython(t, map[string]any{"secret": signer.Secret, "now": now.Unix(), "payload": payload, "token": token})
		if python.Rejected {
			t.Fatal("Python rejected Go token")
		}
		equalJSON(t, "Python verifies Go", payload, python.Decoded)
		decoded, err := signer.Verify(python.Token)
		if err != nil {
			t.Fatal("Go rejected Python", err)
		}
		equalJSON(t, "Go verifies Python", payload, decoded)
		for _, offset := range []int64{-1, 600, 601} {
			verifier := signer
			verifier.Now = func() time.Time { return now.Add(time.Duration(offset) * time.Second) }
			_, err = verifier.Verify(token)
			rejected := err != nil
			oracle := tokenPython(t, map[string]any{"secret": signer.Secret, "now": now.Unix() + offset, "payload": payload, "token": token})
			if rejected != oracle.Rejected || rejected != (offset < 0 || offset > 600) {
				t.Fatalf("expiry %d Go=%v Python=%v", offset, rejected, oracle.Rejected)
			}
		}
	}
}
func fixedState() routerfake.State {
	return routerfake.State{Tables: map[string][]routerfake.Row{"/ip/dhcp-server": {{".id": "*1", "name": "dhcp-example", "interface": "bridge", "disabled": "false"}}, "/ip/dhcp-server/network": {{".id": "*2", "address": "192.0.2.0/24", "gateway": "192.0.2.1"}}, "/ip/address": {{".id": "*3", "address": "192.0.2.1/24", "interface": "bridge"}}, "/ip/dhcp-server/lease": {}, "/ip/arp": {{".id": "*11", "address": "192.0.2.21", "mac-address": "02:00:00:00:00:21", "interface": "bridge"}}}}
}
func fixedService(t *testing.T) (*FixedIPService, *routerfake.Fake, FixedIPIntent) {
	t.Helper()
	f := routerfake.New(nil)
	f.Reset(fixedState())
	httpServer := httptest.NewServer(f)
	t.Cleanup(httpServer.Close)
	db, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(t.Context(), db)
	_, _, err = db.UpdateProfileTransaction(ctx, "default", func(_ context.Context, doc *jsonx.Object) (*jsonx.Object, error) {
		doc.Set("device_notes", jsonx.Obj("192.0.2.21", "Synthetic"))
		doc.Set("routers", jsonx.Obj("items", []any{jsonx.Obj("id", "router-one", "type", "routeros", "endpoint", httpServer.URL, "username", "synthetic-user", "password", "synthetic-pass", "verify_tls", false)}))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	s := &FixedIPService{Repo: db, GuardSetup: func(context.Context, *repo.Repository, string) error { return nil }, Signer: &FixedIPToken{Secret: "synthetic-fixed-secret"}}
	return s, f, FixedIPIntent{Operation: "add", Devices: []FixedIPDevice{{"192.0.2.21", "02:00:00:00:00:21", "router-one", "Synthetic"}}}
}
func TestR17OwnerDisconnectAndShutdown(t *testing.T) {
	for _, mode := range []string{"disconnect", "shutdown"} {
		t.Run(mode, func(t *testing.T) {
			s, f, intent := fixedService(t)
			preview, err := s.Preview(t.Context(), "default", intent)
			if err != nil {
				t.Fatal(err)
			}
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Mode: lifecycle.ModeManual})
			runtime.Start(t.Context())
			defer runtime.Shutdown(t.Context())
			request, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx := runtime.BindRequest(request)
			before, _ := s.Repo.ConfigRevision(t.Context(), "default")
			s.GuardSetup = func(ctx context.Context, _ *repo.Repository, _ string) error {
				if mode == "disconnect" {
					cancel()
				} else {
					runtime.Shutdown(t.Context())
				}
				return ctx.Err()
			}
			err = s.Apply(ctx, "default", preview.Token)
			after, _ := s.Repo.ConfigRevision(t.Context(), "default")
			if mode == "shutdown" {
				if !errors.Is(err, context.Canceled) || after != before || len(f.Snapshot().Tables["/ip/dhcp-server/lease"]) != 0 {
					t.Fatalf("shutdown %v rev %d/%d", err, before, after)
				}
			} else if err != nil || after != before+1 {
				t.Fatalf("disconnect %v rev %d/%d", err, before, after)
			}
		})
	}
}
func TestR17RollbackAfterConcurrentDraftEdit(t *testing.T) {
	s, f, intent := fixedService(t)
	preview, err := s.Preview(t.Context(), "default", intent)
	if err != nil {
		t.Fatal(err)
	}
	original := s.Open
	s.Open = func(r *routers.Router) routers.FixedIPDriver {
		var d routers.FixedIPDriver
		if original != nil {
			d = original(r)
		} else {
			d = routers.NewDriver(r).(routers.FixedIPDriver)
		}
		return &fixedAfterWrite{FixedIPDriver: d, after: func() {
			_, _, err := s.Repo.UpdateProfileTransaction(t.Context(), "default", func(_ context.Context, current *jsonx.Object) (*jsonx.Object, error) {
				current.Set("device_notes", jsonx.Obj("192.0.2.21", "outside edit"))
				return nil, nil
			})
			if err != nil {
				t.Error(err)
			}
		}}
	}
	if err = s.Apply(t.Context(), "default", preview.Token); err == nil {
		t.Fatal("concurrent edit was committed")
	}
	if len(f.Snapshot().Tables["/ip/dhcp-server/lease"]) != 0 {
		t.Fatal("reservation not compensated")
	}
	current, _ := s.Repo.GetProfile(t.Context(), "default")
	if current.GetObject("device_notes").GetString("192.0.2.21") != "outside edit" {
		t.Fatal("outside metadata overwritten")
	}
}

type fixedAfterWrite struct {
	routers.FixedIPDriver
	after func()
}

func (d *fixedAfterWrite) ApplyFixedIP(ctx context.Context, op *routers.FixedIPOperation) (*routers.FixedIPBinding, error) {
	value, err := d.FixedIPDriver.ApplyFixedIP(ctx, op)
	if err == nil {
		d.after()
	}
	return value, err
}
func TestR17BatchLocksAndRollback(t *testing.T) {
	s, f, intent := fixedService(t)
	ctx := repo.WithRepository(t.Context(), s.Repo)
	_, _, err := s.Repo.UpdateProfileTransaction(ctx, "default", func(_ context.Context, current *jsonx.Object) (*jsonx.Object, error) {
		rows := current.GetObject("routers").GetList("items")
		clone := rows[0].(*jsonx.Object).DeepCopy()
		clone.Set("id", "clone")
		current.GetObject("routers").Set("items", append(rows, clone))
		current.GetObject("device_notes").Set("192.0.2.22", "Second")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	seed := f.Snapshot()
	seed.Tables["/ip/arp"] = append(seed.Tables["/ip/arp"], routerfake.Row{".id": "*12", "address": "192.0.2.22", "mac-address": "02:00:00:00:00:22"})
	f.Reset(seed)
	intent.Devices = append(intent.Devices, FixedIPDevice{"192.0.2.22", "02:00:00:00:00:22", "clone", "Second"})
	preview, err := s.Preview(ctx, "default", intent)
	if err != nil {
		t.Fatal(err)
	}
	before := lockx.Violations()
	seed.Faults = []routerfake.Fault{{Method: "PUT", Path: "/ip/dhcp-server/lease", Count: 2, Status: http.StatusForbidden}}
	f.Reset(seed)
	if err = s.Apply(ctx, "default", preview.Token); err == nil {
		t.Fatal("injected second failure not reached")
	}
	if lockx.Violations() != before {
		t.Fatal("nested blocking L2 acquisition")
	}
	if len(f.Snapshot().Tables["/ip/dhcp-server/lease"]) != 0 {
		t.Fatal("first reservation left after batch failure")
	}
}
func FuzzR17FixedRequest(f *testing.F) {
	for _, seed := range []string{`null`, `{}`, `{"profile_id":"default","operation":"add","devices":[{"ip":"192.0.2.21","mac":"02:00:00:00:00:21","router_id":"r","name":"Synthetic"}]}`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 1024*1024 {
			return
		}
		_, _, _ = NormalizeFixedIPRequest(json.RawMessage(input))
		_, _ = (FixedIPToken{Secret: "synthetic-fuzz-secret"}).Verify(input)
	})
}
