package discovery

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"configflow/internal/agents/trafficstore"
)

func TestAutoRoundReusesOnlyUnchangedFullTraffic(t *testing.T) {
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
	if first.Days[day].IPOnly != 1 || len(second.dayOrder) != 1 || &first.dayOrder[0] != &second.dayOrder[0] {
		t.Fatal("unchanged full report was not reused")
	}
	if len(load(ctx, false).Days) != 0 {
		t.Fatal("status read returned the full report")
	}
	next := load(context.WithValue(t.Context(), roundTrafficKey{}, make(roundTraffic)), true)
	if &next.dayOrder[0] == &first.dayOrder[0] {
		t.Fatal("two rounds shared decoded data")
	}
	replace(2)
	if got := load(ctx, true).Days[day].IPOnly; got != 2 {
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
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(got.dayOrder, []string{today, yesterday}) || !reflect.DeepEqual(got.Days[today].hostOrder, []string{"z.test", "a.test"}) || !reflect.DeepEqual(got.Days[today].Hosts["z.test"].portOrder, []string{"443", "80"}) {
		t.Fatal("day decoding changed validated records or storage order")
	}
}
