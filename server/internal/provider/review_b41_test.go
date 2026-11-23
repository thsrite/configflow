package provider

import (
	"context"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/httpx"
	"configflow/internal/store"
)

// TestReviewB41FlightKeyFields pins the inputs of a shared render. A render
// reads the committed configuration (head, profile, resource id, kind) and
// the request's format and filters, nothing else from the request; adding an
// input to a provider render must add it here (and to the key).
func TestReviewB41FlightKeyFields(t *testing.T) {
	var names []string
	typ := reflect.TypeFor[flightKey]()
	for i := range typ.NumField() {
		names = append(names, typ.Field(i).Name)
	}
	want := []string{"kind", "profileID", "itemID", "format", "regex", "excludeRegex", "head", "solo"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("flight key fields %v, want %v", names, want)
	}
}

// TestReviewB41FlightNeverJoinsDifferentRenders: for each input of the key,
// a second request that differs in that input alone, sent while the first
// render is still running, renders on its own and gets its own body.
// (Joining equal requests is covered by the existing serve tests; it is not
// asserted here because it depends on scheduling.)
func TestReviewB41FlightNeverJoinsDifferentRenders(t *testing.T) {
	base := "?item=a&gen=1&commit=c1&store=s&profile=alpha&format=surge&regex=x&exclude_regex=y"
	tests := []struct {
		name   string
		second string
	}{
		{"other format", "?item=a&gen=1&commit=c1&store=s&profile=alpha&format=loon&regex=x&exclude_regex=y"},
		{"no format (Mihomo)", "?item=a&gen=1&commit=c1&store=s&profile=alpha&regex=x&exclude_regex=y"},
		{"other regex", "?item=a&gen=1&commit=c1&store=s&profile=alpha&format=surge&regex=z&exclude_regex=y"},
		{"other exclude_regex", "?item=a&gen=1&commit=c1&store=s&profile=alpha&format=surge&regex=x&exclude_regex=z"},
		{"other profile", "?item=a&gen=1&commit=c1&store=s&profile=beta&format=surge&regex=x&exclude_regex=y"},
		{"other resource", "?item=b&gen=1&commit=c1&store=s&profile=alpha&format=surge&regex=x&exclude_regex=y"},
		{"other generation", "?item=a&gen=2&commit=c1&store=s&profile=alpha&format=surge&regex=x&exclude_regex=y"},
		{"other commit", "?item=a&gen=1&commit=c2&store=s&profile=alpha&format=surge&regex=x&exclude_regex=y"},
		{"other store", "?item=a&gen=1&commit=c1&store=t&profile=alpha&format=surge&regex=x&exclude_regex=y"},
		{"working copy", "?item=a&working=1&profile=alpha&format=surge&regex=x&exclude_regex=y"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newServeFixture(t, 5)
			f.keyFor = func(c *httpx.Ctx) (string, *store.Head) {
				item, _ := c.Arg("item")
				if _, working := c.Arg("working"); working {
					return item, nil
				}
				gen, _ := strconv.ParseInt(c.ArgDefault("gen", "0"), 10, 64)
				return item, &store.Head{StoreID: c.ArgDefault("store", ""), Generation: gen, CommitID: c.ArgDefault("commit", "")}
			}
			gate := make(chan struct{})
			var renders atomic.Int32
			started := make(chan struct{}, 2)
			f.renderFor = func(c *httpx.Ctx) Render {
				body := "proxies: [" + c.Request.URL.RawQuery + "]"
				return func(context.Context) (*httpx.Response, error) {
					renders.Add(1)
					started <- struct{}{}
					<-gate
					return httpx.Bytes(200, "text/yaml; charset=utf-8", []byte(body)), nil
				}
			}
			var wg sync.WaitGroup
			results := make([]served, 2)
			wg.Add(1)
			go func() { defer wg.Done(); results[0] = f.serve(nil, base) }()
			<-started
			wg.Add(1)
			go func() { defer wg.Done(); results[1] = f.serve(nil, tt.second) }()
			select {
			case <-started:
			case <-time.After(10 * time.Second):
				close(gate)
				wg.Wait()
				t.Fatalf("second request joined the first render: %+v", results)
			}
			close(gate)
			wg.Wait()
			if got := renders.Load(); got != 2 {
				t.Fatalf("renders %d, want 2 (%+v)", got, results)
			}
			if results[0].body == results[1].body || results[0].status != 200 || results[1].status != 200 {
				t.Fatalf("results %+v", results)
			}
		})
	}
}
