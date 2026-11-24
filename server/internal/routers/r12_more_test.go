package routers

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/ipv6"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/lifecycle"
)

func TestR12FingerprintAcrossProcesses(t *testing.T) {
	if os.Getenv("R12_FINGERPRINT_CHILD") == "1" {
		fp, e := Fingerprint([]device.Stage{}, json.RawMessage(`{"unicode":"合成","a":9007199254740993,"nested":{"z":null,"a":[]}}`), nil, nil)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(os.Getenv("R12_FINGERPRINT_OUT"), []byte(fp), 0600); e != nil {
			t.Fatal(e)
		}
		return
	}
	file := t.TempDir() + "/fingerprint"
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestR12FingerprintAcrossProcesses$")
	cmd.Env = append(os.Environ(), "R12_FINGERPRINT_CHILD=1", "R12_FINGERPRINT_OUT="+file)
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("child %v %s", e, b)
	}
	child, e := os.ReadFile(file)
	if e != nil {
		t.Fatal(e)
	}
	here, e := Fingerprint([]device.Stage{}, json.RawMessage(`{"nested":{"a":[],"z":null},"a":9007199254740993,"unicode":"合成"}`), nil, nil)
	if e != nil || string(child) != here {
		t.Fatalf("unstable fingerprint %s %s %v", child, here, e)
	}
}
func TestR12StartPublishReleasesBeforeDeliveryAndRefusal(t *testing.T) {
	if os.Getenv("R12_STARTPUBLISH_CHILD") != "1" {
		cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestR12StartPublishReleasesBeforeDeliveryAndRefusal$", "-test.count=1")
		cmd.Env = append(os.Environ(), "R12_STARTPUBLISH_CHILD=1")
		if raw, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("isolated runtime: %v %s", err, raw)
		}
		return
	}
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	rt.Start(t.Context())
	defer rt.Stop(time.Second)
	ctx := rt.BindRequest(t.Context())
	var released atomic.Int32
	ch, e := StartPublish(ctx, PublishRequest{RouterID: "synthetic"}, func() { released.Add(1) })
	if e != nil {
		t.Fatal(e)
	}
	select {
	case result := <-ch:
		if released.Load() != 1 || result.Err == nil {
			t.Fatalf("delivery before cleanup %+v released=%d", result, released.Load())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("owner did not finish")
	}
	rt.Stop(time.Second)
	_, e = StartPublish(ctx, PublishRequest{RouterID: "synthetic"}, func() { released.Add(1) })
	if !errors.Is(e, lifecycle.ErrStopping) || released.Load() != 2 {
		t.Fatalf("refusal leaked L1: %v %d", e, released.Load())
	}
}
func TestR12IPv6HostBindingAndFailure(t *testing.T) {
	calls := 0
	fetcher := func(context.Context, *GatewayAgent) (ipv6.IPv6Host, error) {
		calls++
		return ipv6.IPv6Host{}, errors.New("synthetic host unavailable")
	}
	for _, mode := range []string{"missing", "profile", "service", "disabled", "valid"} {
		a := r12Gateway()
		switch mode {
		case "missing":
			a = nil
		case "profile":
			a.ProfileID = "other"
		case "service":
			a.ServiceType = "oxidns"
		case "disabled":
			a.Enabled = pointer(false)
		}
		h := inspectIPv6Host(t.Context(), "default", json.RawMessage(`{}`), a, fetcher)
		if h.Error == "" {
			t.Fatal(mode, "missing fail-closed reason")
		}
	}
	if calls != 1 {
		t.Fatal("contacted invalid Agent", calls)
	}
}
func FuzzR12ReturnHandshake(f *testing.F) {
	for _, seed := range []string{`{}`, `null`, string(device.JSON(r12SafeStatus())), `{"safety":{"version":1,"ok":true,"blockers":[null],"allocation":{}}}`, `{"managed":{"spec":{"proxy_dst":null}},"fwmark_rules":[]}`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 65536 {
			return
		}
		var s ReturnStatus
		if json.Unmarshal([]byte(raw), &s) != nil {
			return
		}
		_ = ReturnAllocationOf(&s)
		_ = ReturnProtocol(&s)
		_ = ReturnSafetyBlockers(&s, true)
		_ = ReturnSafetyBlockers(&s, false)
		b, e := json.Marshal(s)
		if e != nil {
			t.Fatal(e)
		}
		if !json.Valid(b) || strings.Contains(string(b), "\x00") {
			t.Fatal("invalid status output")
		}
	})
}
