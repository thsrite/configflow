package routers

import (
	"bytes"
	"configflow/internal/routers/ros"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"configflow/internal/devhttp"
	"configflow/internal/redact"
	"configflow/internal/routers/routerfake"
)

// F1: only the router password is a secret in device diagnostics. A RouterOS
// user named "api" must not turn "rest-api" in a device reply into
// "rest-[REDACTED]"; a password echo, also URL-encoded, stays hidden.
func TestR07RedactionHidesOnlyPassword(t *testing.T) {
	tests := []struct {
		name, username, password, echo string
		console                        bool
		hidden                         bool
	}{
		{"username in REST detail", "api", "synthetic-password", "no such command (rest-api)", false, false},
		{"username in console output", "api", "synthetic-password", "failure: api user lacks policy", true, false},
		{"password in REST detail", "api", "synthetic-password", "bad value synthetic-password", false, true},
		{"encoded password in REST detail", "api", "p@ss word", "bad value p%40ss+word", false, true},
		{"password in console output", "api", "synthetic-password", "failure: synthetic-password", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := r07FakeState()
			if tt.console {
				state.Faults = []routerfake.Fault{{Method: "POST", Path: "/execute", Count: 1, Status: 200, Body: routerfake.Row{"ret": tt.echo}}}
			} else {
				state.Faults = []routerfake.Fault{{Method: "GET", Path: ros.PathNAT, Count: 1, Status: 400, Body: routerfake.Row{"message": "Bad Request", "detail": tt.echo}}}
			}
			fake := routerfake.New(nil)
			fake.Reset(state)
			server := httptest.NewServer(fake)
			defer server.Close()
			d := NewRouterOS(server.URL, tt.username, tt.password, false)
			defer d.Close()
			var err error
			if tt.console {
				_, err = d.OccupiedAddressNames(context.Background(), []string{"cf-.*"}, nil)
			} else {
				_, err = d.NATResources(context.Background())
			}
			e, ok := errors.AsType[*RouterError](err)
			if !ok {
				t.Fatalf("error = %#v, want RouterError", err)
			}
			if tt.hidden {
				if strings.Contains(e.Message, "password") || strings.Contains(e.Message, "p%40ss") || !strings.Contains(e.Message, redact.Redacted) {
					t.Fatalf("password echo not hidden: %q", e.Message)
				}
				return
			}
			if !strings.Contains(e.Message, tt.echo) || strings.Contains(e.Message, redact.Redacted) {
				t.Fatalf("message = %q, want the device text %q", e.Message, tt.echo)
			}
		})
	}
}

// Standing checks (R06): every device request of the NAT-list writes and
// their compensation keeps the 10 s device timeout, and the raw request
// carries exactly the body Python's requests sends (none for GET/DELETE).
func TestR07RequestsKeepTimeoutAndRawBodies(t *testing.T) {
	const nat = "/ip/firewall/nat"
	tables := r07FakeState().Tables
	for i := range 5 {
		tables[nat] = append([]routerfake.Row{{".id": fmt.Sprintf("*%X", 0x100+i), "chain": "srcnat", "action": "accept", "comment": "cf-mihomo",
			"src-address": "192.0.2.128/25", "dst-address": fmt.Sprintf("198.51.100.%d/32", i), "disabled": "false"}}, tables[nat]...)
	}
	fake := routerfake.New(nil)
	// The batched remove fails: the guarded per-ID DELETE fallback must run too.
	fake.Reset(routerfake.State{Tables: tables, Faults: []routerfake.Fault{{Method: "POST", Path: nat + "/remove", Count: 1, Status: 400}}})
	type wire struct{ method, path, body, contentType string }
	var mu sync.Mutex
	seen := []wire{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, wire{r.Method, strings.TrimPrefix(r.URL.Path, "/rest"), string(data), r.Header.Get("Content-Type")})
		mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(data))
		fake.ServeHTTP(w, r)
	}))
	defer server.Close()
	recorder := &r06TimeoutRecorder{client: devhttp.New(devhttp.Config{BasicAuth: true, Username: "synthetic-user", Password: "synthetic-password"})}
	d := NewRouterOSWithHTTP(server.URL, recorder)
	defer d.Close()
	ctx := context.Background()

	undo, err := d.ApplyNATLists(ctx, r07NewSync())
	if err != nil {
		t.Fatal(err)
	}
	if err = d.RestoreNATLists(ctx, *undo); err != nil {
		t.Fatal(err)
	}
	rows, err := d.List(ctx, nat)
	if err != nil {
		t.Fatal(err)
	}
	legacy := []ROSResource{}
	for _, row := range rows {
		if row.Is("comment", "cf-mihomo") {
			legacy = append(legacy, ros.NewResource(nat, row))
		}
	}
	journal := []NATStep{}
	if err = d.DeleteNATRules(ctx, legacy, &journal); err != nil {
		t.Fatal(err)
	}
	slices.Reverse(journal)
	if err = d.RestoreNATLists(ctx, NATListUndo{Kind: "nat_list_restore", Steps: journal, Keep: []string{}}); err != nil {
		t.Fatal(err)
	}
	if _, err = d.LocateAddresses(ctx, []string{"*1"}); err != nil {
		t.Fatal(err)
	}
	after := fake.Snapshot().Tables[nat]
	if len(after) != len(tables[nat]) {
		t.Fatalf("legacy rules not restored: %d rows, want %d", len(after), len(tables[nat]))
	}

	for i, timeout := range recorder.timeouts {
		if timeout != ros.Timeout {
			t.Errorf("%s used timeout %s, want %s", recorder.requests[i], timeout, ros.Timeout)
		}
	}
	counts := map[string]int{}
	for _, call := range seen {
		counts[call.method]++
		switch call.method {
		case http.MethodGet, http.MethodDelete:
			if call.body != "" || call.contentType != "" {
				t.Errorf("%s %s sent body %q with content type %q, want none", call.method, call.path, call.body, call.contentType)
			}
			continue
		}
		if call.contentType != "application/json" {
			t.Errorf("%s %s content type %q", call.method, call.path, call.contentType)
		}
		var body map[string]any
		if err := json.Unmarshal([]byte(call.body), &body); err != nil {
			t.Fatalf("%s %s raw body %q: %v", call.method, call.path, call.body, err)
		}
		keys := slices.Sorted(func(yield func(string) bool) {
			for key := range body {
				if !yield(key) {
					return
				}
			}
		})
		switch {
		case call.method == http.MethodPost && call.path == "/execute":
			if !reflect.DeepEqual(keys, []string{"as-string", "script"}) || body["as-string"] != "" {
				t.Errorf("/execute raw body %q", call.body)
			}
		case call.method == http.MethodPost && strings.HasSuffix(call.path, "/remove"):
			if !reflect.DeepEqual(keys, []string{".id"}) {
				t.Errorf("%s raw body %q", call.path, call.body)
			}
		case call.method == http.MethodPut && call.path == nat:
			want := []string{"action", "chain", "comment", "disabled", "dst-address-list", "place-before", "src-address-list"}
			if !reflect.DeepEqual(keys, want) {
				t.Errorf("NAT PUT raw body %q", call.body)
			}
		default:
			t.Errorf("unexpected write %s %s %q", call.method, call.path, call.body)
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodDelete, http.MethodPost, http.MethodPut} {
		if counts[method] == 0 {
			t.Errorf("no %s request observed", method)
		}
	}
}

// F4: a list downloaded inside a reuse scope is reused from the scope's own
// copy; a caller that modifies the rows it received cannot change what a
// later read in the same scope returns.
func TestR07ReuseScopeOwnsDownloadedRows(t *testing.T) {
	state := r07FakeState()
	state.Tables[ros.PathAddresses] = []routerfake.Row{{".id": "*10", "list": "L", "address": "198.51.100.1", "comment": "external", "disabled": "false"}}
	d, _ := newFakeRouterOS(t, state)
	defer d.ReuseAddressReads()()
	first, err := d.ReadAddresses(context.Background(), []string{"L"})
	if err != nil {
		t.Fatal(err)
	}
	first["L"][0]["comment"] = "changed by caller"
	second, err := d.ReadAddresses(context.Background(), []string{"L"})
	if err != nil {
		t.Fatal(err)
	}
	if got := second["L"][0].Text("comment"); got != "external" {
		t.Fatalf("reused rows were modified through an earlier result: comment %q", got)
	}
}

// F5: the member check is a set comparison like Python's; a repeated key in
// the planned members does not fail a correct read-back.
func TestR07VerifyComparesMemberSets(t *testing.T) {
	rule := Row{".id": "*5", "chain": "srcnat", "action": "accept", "src-address-list": NATSrcList, "dst-address-list": NATDstList, "comment": NATListComment, "disabled": "false"}
	member := func(id, list, address string) Row {
		return Row{".id": id, "list": list, "address": address, "comment": NATListComment, "disabled": "false"}
	}
	state := &RouterState{
		NatContext: &NatContext{Rules: []Row{rule}},
		NATLists: &NATLists{
			Src:        []Row{member("*20", NATSrcList, "192.0.2.128/25")},
			Dst:        []Row{member("*30", NATDstList, "198.51.100.0/24")},
			MangleRefs: []ListRef{}, FirewallRefs: []ListRef{},
		},
	}
	id := "*5"
	check := &NATListCheck{Rule: &id, Sources: []string{"192.0.2.128/25"}, Destinations: []string{"198.51.100.0/24"}, Removed: []string{},
		Members: NATListMembers{Src: []string{"192.0.2.128/25"}, Dst: []string{"198.51.100.0/24", "198.51.100.0/24"}}}
	if err := VerifyNATLists(context.Background(), check, state); err != nil {
		t.Fatal(err)
	}
	check.Members.Dst = []string{"198.51.100.0/24", "203.0.113.0/24"}
	if err := VerifyNATLists(context.Background(), check, state); err == nil {
		t.Fatal("missing planned member accepted")
	}
}

// F2: only a single IPv4 network is normalised to a member key; a range keeps
// its text (Python _key uses IPv4Network, never a range summary).
func TestR07NATKeyKeepsRanges(t *testing.T) {
	tests := []struct{ in, want string }{
		{"198.51.100.7", "198.51.100.7/32"},
		{" 198.51.100.7/24 ", "198.51.100.0/24"},
		{"198.51.100.0/255.255.255.0", "198.51.100.0/24"},
		{"198.51.100.0-198.51.100.255", "198.51.100.0-198.51.100.255"},
		{"198.51.100.4-198.51.100.4", "198.51.100.4-198.51.100.4"},
		{"2001:db8::/32", "2001:db8::/32"},
	}
	for _, tt := range tests {
		if got := natKey(tt.in); got != tt.want {
			t.Errorf("natKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// F3: the bisection over collapsed exclusions answers exactly like the
// pairwise scan it replaces.
func TestR07PrefixOverlapMatchesPairwise(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 11))
	random := func() netip.Prefix {
		addr := netip.AddrFrom4([4]byte{198, 18, byte(r.IntN(4)), byte(r.IntN(256))})
		return netip.PrefixFrom(addr, 20+r.IntN(13)).Masked()
	}
	for round := range 200 {
		exclusions := make([]netip.Prefix, r.IntN(12))
		for i := range exclusions {
			exclusions[i] = random()
		}
		overlaps := prefixOverlap(exclusions)
		for range 50 {
			n := random()
			if got, want := overlaps(n), overlapsAny(n, exclusions); got != want {
				t.Fatalf("round %d: %s against %v = %v, want %v", round, n, exclusions, got, want)
			}
		}
	}
}
