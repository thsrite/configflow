package model

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestJournalMemberReadsKeepOriginalEvidence(t *testing.T) {
	var event OperationEvent
	if err := json.Unmarshal([]byte(`{"at":7,"status":null,"message":false,"future":{"value":2.00}}`), &event); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ name, want string }{
		{"at", `7`}, {"status", `null`}, {"message", `false`}, {"future", `{"value":2.00}`},
	} {
		raw, present, err := event.MemberJSON(tt.name)
		if err != nil || !present || string(raw) != tt.want {
			t.Fatalf("member=%s raw=%s err=%v", tt.name, raw, err)
		}
		raw[0] = 'x'
		again, _, _ := event.MemberJSON(tt.name)
		if string(again) != tt.want {
			t.Fatal("caller mutated source provenance")
		}
	}
	var deployment Deployment
	if err := json.Unmarshal([]byte(`{"profile_id":7,"Profile_ID":"forged","submitted":"false"}`), &deployment); err != nil {
		t.Fatal(err)
	}
	profile, _, err := deployment.MemberJSON("profile_id")
	if err != nil || !bytes.Equal(profile, []byte(`7`)) {
		t.Fatal("alias replaced original proof", string(profile), err)
	}
	if err := deployment.SetJSON("submitted", []byte(`false`)); err != nil {
		t.Fatal(err)
	}
	if err := deployment.Clear("profile_id"); err != nil {
		t.Fatal(err)
	}
	if raw, present, err := deployment.MemberJSON("profile_id"); err != nil || present {
		t.Fatal(string(raw), present, err)
	}
}

func TestJournalMemberAllocationIgnoresOpaquePayload(t *testing.T) {
	measure := func(size int) testing.BenchmarkResult {
		event := OperationEvent{Status: "ready"}
		if err := event.Fields.Set("captured", []byte(`"`+strings.Repeat("x", size)+`"`)); err != nil {
			t.Fatal(err)
		}
		return testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if raw, present, err := event.MemberJSON("status"); err != nil || !present || !bytes.Equal(raw, []byte(`"ready"`)) {
					b.Fatal(string(raw), present, err)
				}
			}
		})
	}
	small, large := measure(0), measure(2<<20)
	t.Logf("journal member unused=0: %d B/op %d allocs/op; unused=2MiB: %d B/op %d allocs/op", small.AllocedBytesPerOp(), small.AllocsPerOp(), large.AllocedBytesPerOp(), large.AllocsPerOp())
	if large.AllocedBytesPerOp() > small.AllocedBytesPerOp()+1024 || large.AllocedBytesPerOp() > 2048 || large.AllocsPerOp() > small.AllocsPerOp()+2 {
		t.Fatal("member lookup walked unrelated payload")
	}
}
