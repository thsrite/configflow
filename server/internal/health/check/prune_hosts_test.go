package check

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"sort"
	"strconv"
	"testing"
	"time"

	"configflow/internal/model"
)

func makeProbeHosts(t testing.TB, count int, timestamp func(int) string) model.OrderedMap[TimedRecord] {
	t.Helper()
	// An empty fixture stamp omits checked_at. The literal null is stored as-is.
	var hosts model.OrderedMap[TimedRecord]
	hosts.SetAll(func(yield func(string, TimedRecord) bool) {
		for i := range count {
			var entry TimedRecord
			if stamp := timestamp(i); stamp != "" {
				raw := stamp
				if stamp != "null" {
					raw = strconv.Quote(stamp)
				}
				if err := entry.Set("checked_at", []byte(raw)); err != nil {
					t.Fatal(err)
				}
			}
			if !yield(fmt.Sprintf("host-%04d.example.com", i), entry) {
				return
			}
		}
	})
	return hosts
}

func pruneHosts(hosts *model.OrderedMap[TimedRecord], now time.Time) {
	snap := probeSnapshot[struct{}]{file: probeFile{Hosts: *hosts}, retained: indexHosts(*hosts, nil)}
	*hosts = snap.entries(oldest(now, ProbeRetentionDays))
}

func TestPruneHostsRetention(t *testing.T) {
	now := time.Date(2026, time.January, 31, 0, 0, 0, 0, time.UTC)
	const (
		expired = "2025-12-31T23:59:59"
		cutoff  = "2026-01-01T00:00:00"
		recent  = "2026-01-30T00:00:00"
	)
	for _, tt := range []struct {
		name      string
		count     int
		timestamp func(int) string
		keep      func(int) bool
	}{
		{
			"fresh", 2,
			func(i int) string { return []string{recent, cutoff}[i] },
			func(int) bool { return true },
		},
		{
			"partially expired", 5,
			func(i int) string { return []string{"", recent, expired, cutoff, "null"}[i] },
			func(i int) bool { return i == 1 || i == 3 },
		},
		{
			"all expired", 3,
			func(i int) string { return []string{expired, "", "null"}[i] },
			func(int) bool { return false },
		},
		{
			"above limit with ties", MaxProbedHosts + 4,
			func(i int) string {
				switch i {
				case 0:
					return expired
				case 1:
					return cutoff
				case MaxProbedHosts + 3:
					return "2026-01-31T00:00:00"
				default:
					return recent
				}
			},
			func(i int) bool { return i >= 2 && i <= MaxProbedHosts || i == MaxProbedHosts+3 },
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			hosts := makeProbeHosts(t, tt.count, tt.timestamp)
			snapshot := hosts
			originalKeys := snapshot.Keys()
			var want model.OrderedMap[TimedRecord]
			want.SetAll(func(yield func(string, TimedRecord) bool) {
				i := 0
				for host, entry := range snapshot.All() {
					if tt.keep(i) && !yield(host, entry) {
						return
					}
					i++
				}
			})
			pruneHosts(&hosts, now)
			gotJSON, err := hosts.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			wantJSON, err := want.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(gotJSON, wantJSON) {
				t.Fatalf("pruned hosts differ: got %d entries, want %d in insertion order", hosts.Len(), want.Len())
			}
			if !slices.Equal(snapshot.Keys(), originalKeys) {
				t.Fatal("pruning changed the borrowed snapshot")
			}
		})
	}
}

func legacyPruneHosts(hosts *model.OrderedMap[TimedRecord], now time.Time) {
	bound := oldest(now, ProbeRetentionDays)
	var expired []string
	for host, entry := range hosts.All() {
		if entry.CheckedAt() < bound {
			expired = append(expired, host)
		}
	}
	for _, host := range expired {
		hosts.Delete(host)
	}
	if hosts.Len() > MaxProbedHosts {
		ranked := hosts.Keys()
		sort.SliceStable(ranked, func(i, j int) bool {
			left, _ := hosts.Get(ranked[i])
			right, _ := hosts.Get(ranked[j])
			return left.CheckedAt() > right.CheckedAt()
		})
		for _, host := range ranked[MaxProbedHosts:] {
			hosts.Delete(host)
		}
	}
}

func BenchmarkPruneHosts6000(b *testing.B) {
	now := time.Date(2026, time.January, 31, 0, 0, 0, 0, time.UTC)
	hosts := makeProbeHosts(b, 6000, func(i int) string {
		return NaiveISOSeconds(now.AddDate(0, 0, -(i*17)%32))
	})
	for _, impl := range []struct {
		name  string
		prune func(*model.OrderedMap[TimedRecord], time.Time)
	}{
		{"legacy", legacyPruneHosts},
		{"optimized", pruneHosts},
	} {
		b.Run(impl.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				candidate := hosts
				impl.prune(&candidate, now)
			}
		})
	}
}

func TestCachedProbeCutoffMovesBothWays(t *testing.T) {
	store := NewProbeStore(t.TempDir())
	before := time.Now().Truncate(time.Second)
	seedProbeHosts(t, store, "a", []string{"one.example.com"}, before)
	cutoff := before.AddDate(0, 0, ProbeRetentionDays)
	for _, tc := range []struct {
		at    time.Time
		count int
	}{{cutoff, 1}, {cutoff.Add(time.Second), 0}, {cutoff, 1}} {
		if got := store.LoadEntries("a", tc.at).Len(); got != tc.count {
			t.Fatalf("at %v got %d, want %d", tc.at, got, tc.count)
		}
	}
}

func TestProbeStoreCachesReadableEmptyVersions(t *testing.T) {
	store := NewProbeStore(t.TempDir())
	path := store.Path("agent")
	for _, raw := range []string{`{`, `{}`, `{"hosts":[]}`, `{"hosts":{"one.example.com":"invalid"}}`} {
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		first := store.loadSnapshot("agent")
		if len(first.retained) != 0 || store.loadSnapshot("agent") != first {
			t.Fatalf("not cached as empty: %q", raw)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if first := store.loadSnapshot("agent"); store.loadSnapshot("agent") == first {
		t.Fatal("missing file retained")
	}
}
