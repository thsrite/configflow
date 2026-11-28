package discovery

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents/trafficstore"
)

func TestRequestsAndAutoRoundsReuseOnlyCurrentTraffic(t *testing.T) {
	svc := &Service{Traffic: trafficstore.NewService(t.TempDir())}
	a := Agent{ID: "one"}
	path := svc.Traffic.Store().Path(a.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	day := time.Now().Format("2006-01-02")
	replace := func(n int) {
		t.Helper()
		raw := fmt.Sprintf(`{"status":null,"days":{"%s":{"ip_only_conns":%d,"hosts":{}}}}`, day, n)
		if err := os.WriteFile(path+".tmp", []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(path+".tmp", path); err != nil {
			t.Fatal(err)
		}
	}
	load := func(ctx context.Context, full bool) traffic {
		t.Helper()
		r, err := svc.loadTraffic(ctx, a, full)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	replace(1)
	ctx := context.WithValue(t.Context(), roundTrafficKey{}, make(roundTraffic))
	first := load(ctx, true)
	second := load(ctx, true)
	if len(first.Days) != 1 || first.Days[0].value.IPOnly != 1 || len(second.Days) != 1 || &first.Days[0] != &second.Days[0] {
		t.Fatal("unchanged full report was not reused")
	}
	if len(load(ctx, false).Days) != 0 {
		t.Fatal("status read returned the full report")
	}
	next := load(context.WithValue(t.Context(), roundTrafficKey{}, make(roundTraffic)), true)
	if &next.Days[0] != &first.Days[0] || &load(t.Context(), true).Days[0] != &first.Days[0] {
		t.Fatal("full reads decoded the same revision again")
	}
	replace(2)
	if got := load(ctx, true).Days[0].value.IPOnly; got != 2 {
		t.Fatalf("after replacement got %d", got)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if len(load(ctx, true).Days) != 0 {
		t.Fatal("deleted report was reused")
	}
}

func TestDayRecordsPreserveDiscoveryRankingOrder(t *testing.T) {
	svc := &Service{Traffic: trafficstore.NewService(t.TempDir())}
	a := Agent{ID: "ordered"}
	path := svc.Traffic.Store().Path(a.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	today, yesterday := now.Format("2006-01-02"), now.AddDate(0, 0, -1).Format("2006-01-02")
	raw := fmt.Sprintf(`{"status":{"value":"running"},"days":{"%s":{"hosts":{"z.test":{"ports":{"443":2,"bad":[],"80":1},"by_route":{}},"broken":1,"a.test":{"by_route":{}}}},"%s":{"hosts":{}}}}`, today, yesterday)
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := svc.loadTraffic(t.Context(), a, true)
	if err != nil {
		t.Fatal(err)
	}
	var want traffic
	doc := svc.Traffic.Store().LoadDocument(a.ID, &now)
	if err := project(doc, &want); err != nil {
		t.Fatal(err)
	}
	want.storeState = string(doc.StoreState())
	if !reflect.DeepEqual(got, want) || len(got.Days) != 2 || got.Days[0].key != today || got.Days[1].key != yesterday {
		t.Fatal("changed validated days or storage order")
	}
	hosts := got.Days[0].value.Hosts
	if len(hosts) != 2 || hosts[0].key != "z.test" || hosts[1].key != "a.test" || !reflect.DeepEqual(hosts[0].value.Ports, trafficRows[int64]{{"443", 2}, {"80", 1}}) {
		t.Fatal("day decoding changed validated records or storage order")
	}
}

func TestTrafficCacheAdmissionAndEviction(t *testing.T) {
	store := trafficstore.NewNative(t.TempDir())
	path := store.Path("agent")
	if err := os.WriteFile(path, []byte(`{"status":null,"days":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	current := store.LoadDocument("agent", nil).Revision()
	yesterday := time.Now().AddDate(0, 0, -1)
	old := store.LoadDocument("agent", &yesterday).Revision()
	tests := []struct {
		name              string
		size, count, want int
		revision          trafficstore.DocumentRevision
	}{
		{"entry quota", 0, trafficCacheEntries + 1, trafficCacheEntries, current},
		{"byte quota", trafficCacheBytes / 3, 3, 2, current},
		{"oversize", trafficCacheBytes, 1, 0, current},
		{"previous day", 0, 1, 0, old},
		{"missing or damaged", 0, 1, 0, trafficstore.DocumentRevision{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var c trafficCache
			text := strings.Repeat("x", tc.size)
			snap := trafficSnapshot{traffic{Status: collectionStatus{Value: text}}, tc.revision}
			for i := range tc.count {
				c.put(fmt.Sprint(i), snap)
				if i == tc.count-2 {
					c.get("0")
				}
			}
			if c.order.Len() != tc.want || c.bytes > trafficCacheBytes {
				t.Fatalf("retained %d entries, %d bytes", c.order.Len(), c.bytes)
			}
			if tc.want > 1 {
				if _, ok := c.get("0"); !ok {
					t.Fatal("evicted recently read report")
				}
				if _, ok := c.get("1"); ok {
					t.Fatal("kept older report")
				}
			}
		})
	}
}

func TestCachedTrafficStatusIsPrivate(t *testing.T) {
	r := traffic{Status: collectionStatus{At: new("2026-01-01")}}
	s := statusOf(Agent{}, r)
	*s.LastReport = "changed"
	if *r.Status.At != "2026-01-01" {
		t.Fatal("caller changed shared status")
	}
}
