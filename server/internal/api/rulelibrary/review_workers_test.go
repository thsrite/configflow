package rulelibrary

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/model"
	"configflow/internal/rulefetch"
)

func TestReviewConnectivityWorkerBoundAndRawRequest(t *testing.T) {
	s := fixture(t)
	started, release := make(chan struct{}, 27), make(chan struct{})
	var active, peak, calls atomic.Int32
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil || len(body) != 0 || r.Method != "HEAD" || r.Header.Get("Content-Type") != "" || r.Header.Get("Authorization") != "" {
			t.Errorf("raw probe request: method=%s body=%q headers=%v err=%v", r.Method, body, r.Header, err)
		}
		calls.Add(1)
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.WriteHeader(204)
	}))
	defer origin.Close()
	defer unblock()
	items := make([]map[string]any, 0, 27)
	for i := range 27 {
		items = append(items, map[string]any{"id": fmt.Sprint(i), "name": fmt.Sprint(i), "source_type": "url", "url": fmt.Sprintf("%s/%d", origin.URL, i)})
	}
	itemsRaw, _ := json.Marshal(items)
	err := s.Repo.UpdateShared(s.Context(), func(_ context.Context, p *model.Shared) error {
		return p.SetJSON("rule_library", itemsRaw)
	})
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		value checksResult
		err   error
	}
	done := make(chan result, 1)
	go func() {
		out, err := (&Service{client: rulefetch.DefaultClient}).Test(s.Context(), "http://localhost")
		done <- result{out, err}
	}()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for range 10 {
		select {
		case <-started:
		case <-deadline.C:
			t.Fatal("ten probes did not enter the actual transport")
		}
	}
	unblock()
	out := <-done
	if out.err != nil || out.value.Total != 27 || out.value.Failed != 0 || len(out.value.Results) != 27 || calls.Load() != 27 || peak.Load() != 10 || active.Load() != 0 {
		t.Fatalf("probe fanout: total=%d failed=%d calls=%d peak=%d active=%d err=%v", out.value.Total, out.value.Failed, calls.Load(), peak.Load(), active.Load(), out.err)
	}
	for i, result := range out.value.Results {
		if result.ID != fmt.Sprintf("%d", i) || !result.Available || result.Status == nil || *result.Status != 204 {
			t.Fatalf("probe result lost identity/status at %d: %+v", i, result)
		}
	}
}
