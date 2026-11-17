package agents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"configflow/internal/deploy"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/lifecycle"
)

// TestRealPreparationProof runs only inside proddiff.sh --a13-proof's isolated
// namespace. Real reference preparation supplies bundles, never status or diff
// answers. Every HTTP field is compared; independent times/nonces are checked
// for format and stable identity before pairing their values for comparison.
func TestRealPreparationProof(t *testing.T) {
	root := os.Getenv("CONFIGFLOW_A13_PROOF")
	if root == "" {
		t.Skip("run server/scripts/proddiff.sh --a13-proof on a test server")
	}
	var fixtures []struct {
		Route, Path string
		Status      int
		Headers     map[string]string
		Body        json.RawMessage
	}
	raw, err := os.ReadFile(filepath.Join(root, "fixtures.json"))
	if err != nil || json.Unmarshal(raw, &fixtures) != nil || len(fixtures) == 0 {
		t.Fatal("invalid private proof fixtures")
	}
	if err := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()}); err != nil {
		t.Fatal("configure proof runtime")
	}
	lifecycle.Start(context.Background())
	WireContentSync()
	s := httpxtest.New(t, httpxtest.Options{DataDir: filepath.Join(root, "go"), DefaultFactories: true, Register: httpxtest.Routes(Register)})
	restoreAbsent := SetContentPrepare(nil)
	defer restoreAbsent()
	defer lifecycle.Stop(8 * time.Second)
	defer contentChecks.close()
	decode := func(raw []byte) any {
		t.Helper()
		var value any
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if dec.Decode(&value) != nil {
			t.Fatal("invalid proof response JSON")
		}
		return value
	}
	checking := func(value any) bool {
		if records, ok := value.([]any); ok {
			for _, record := range records {
				if item, ok := record.(map[string]any); ok {
					if state, ok := item["content_sync"].(map[string]any); ok && state["status"] == "checking" {
						return true
					}
				}
			}
		} else if item, ok := value.(map[string]any); ok {
			if item["status"] == "checking" {
				return true
			}
			if state, ok := item["content_sync"].(map[string]any); ok && state["status"] == "checking" {
				return true
			}
		}
		return false
	}
	get := func(index int) any {
		t.Helper()
		f := fixtures[index]
		deadline := time.Now().Add(30 * time.Second)
		for {
			r := s.Get(f.Path)
			if r.Status != f.Status {
				t.Fatalf("case %d: HTTP status mismatch", index)
			}
			for name, value := range f.Headers {
				if r.Header.Get(name) != value {
					t.Fatalf("case %d: header %s mismatch", index, name)
				}
			}
			value := decode(r.Body)
			if !checking(value) {
				return value
			}
			if time.Now().After(deadline) {
				t.Fatalf("case %d: worker did not settle", index)
			}
			time.Sleep(time.Millisecond)
		}
	}
	// Reproduce each of the exact twenty production-differential triplets before
	// injection. A settled oracle also has four list/detail timestamps absent
	// from the original asynchronous first poll; only those extra paths may vary.
	observed := map[string]bool{}
	for i, f := range fixtures {
		proofDifferences(f.Route, "body", decode(f.Body), get(i), observed)
	}
	allowed := map[string]bool{}
	for _, line := range strings.Split(a13DeferredFields, "\n") {
		allowed[line] = true
		if !observed[line] {
			t.Fatal("an authorized deferred field was not reproduced")
		}
	}
	for _, path := range []string{"body[0].content_sync.checked_at", "body[1].content_sync.checked_at", "body[2].content_sync.checked_at"} {
		allowed["/api/agents\t"+path+"\tmissing-in-go"] = true
	}
	allowed["/api/agents/<agent_id>\tbody.content_sync.checked_at\tmissing-in-go"] = true
	for difference := range observed {
		if !allowed[difference] {
			t.Fatal("unexpected difference without preparation; not deferred")
		}
	}
	restorePrepared := SetContentPrepare(func(_ context.Context, target deploy.Target, _ *deploy.Source, _, _ string) (*deploy.Bundle, error) {
		name := sha256.Sum256([]byte(target.ID))
		base := filepath.Join(root, "prepared", hex.EncodeToString(name[:]))
		var record struct {
			Manifest   deploy.Manifest `json:"manifest"`
			SHA256     string          `json:"sha256"`
			Version    string          `json:"config_version"`
			Comparison *deploy.File    `json:"comparison_config"`
		}
		data, err := os.ReadFile(base + ".json")
		if err != nil || json.Unmarshal(data, &record) != nil {
			return nil, fmt.Errorf("missing reference preparation")
		}
		archive, err := os.ReadFile(base + ".tar.gz")
		if err != nil {
			return nil, fmt.Errorf("missing reference archive")
		}
		return deploy.Open(record.Manifest, archive, record.SHA256, record.Version, record.Comparison)
	})
	defer restorePrepared()
	times, versions := map[string]string{}, map[string]string{}
	for i, f := range fixtures {
		want, got := decode(f.Body), get(i)
		pairProofMetadata(t, want, got, times, versions)
		if !reflect.DeepEqual(want, got) {
			// Do not print private paths or values from the response. Detailed
			// schema differences stay in the protected proof directory.
			diffs := map[string]bool{}
			proofDifferences(f.Route, "body", want, got, diffs)
			data, _ := json.Marshal(diffs)
			_ = os.WriteFile(filepath.Join(root, "mismatch.json"), data, 0o600)
			t.Fatalf("case %d: prepared HTTP response differs; not deferred", i)
		}
	}
	t.Logf("%d HTTP cases: all 20 exact differences reproduced absent preparation; full responses match real prepared bundles", len(fixtures))
}

func proofDifferences(route, path string, want, got any, out map[string]bool) {
	add := func(kind string) { out[route+"\t"+path+"\t"+kind] = true }
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			add("type")
			return
		}
		for key, value := range w {
			if other, exists := g[key]; exists {
				proofDifferences(route, path+"."+key, value, other, out)
			} else {
				out[route+"\t"+path+"."+key+"\tmissing-in-go"] = true
			}
		}
		for key := range g {
			if _, exists := w[key]; !exists {
				out[route+"\t"+path+"."+key+"\textra-in-go"] = true
			}
		}
	case []any:
		g, ok := got.([]any)
		if !ok {
			add("type")
			return
		}
		if len(w) != len(g) {
			add("length")
			return
		}
		for i := range w {
			proofDifferences(route, fmt.Sprintf("%s[%d]", path, i), w[i], g[i], out)
		}
	default:
		if !reflect.DeepEqual(want, got) {
			add("value")
		}
	}
}

var proofVersion = regexp.MustCompile(`^[0-9a-f]{16}$`)

func pairProofMetadata(t *testing.T, want, got any, times, versions map[string]string) {
	t.Helper()
	if records, ok := want.([]any); ok {
		others, ok := got.([]any)
		if !ok || len(records) != len(others) {
			return
		}
		for i := range records {
			pairProofMetadata(t, records[i], others[i], times, versions)
		}
		return
	}
	w, wok := want.(map[string]any)
	g, gok := got.(map[string]any)
	if !wok || !gok {
		return
	}
	for _, key := range []string{"checked_at", "version"} {
		if key == "version" && w["status"] != "changed" {
			continue // an Agent's ordinary software version is not a random nonce
		}
		a, aok := w[key].(string)
		b, bok := g[key].(string)
		if !aok || !bok {
			continue
		} // structural comparison must catch missing/wrong types
		identities := times
		valid := func(v string) bool {
			_, err := time.Parse(time.RFC3339Nano, v)
			return err == nil && strings.HasSuffix(v, "Z")
		}
		if key == "version" {
			identities, valid = versions, proofVersion.MatchString
		}
		if !valid(a) || !valid(b) {
			t.Fatal("invalid comparison time/version")
		}
		if old, exists := identities[a]; exists && old != b {
			t.Fatal("cached comparison identity changed")
		}
		for other, value := range identities {
			if other != a && value == b {
				t.Fatal("distinct comparison identities collided")
			}
		}
		identities[a] = b
		g[key] = a
	}
	if state, exists := w["content_sync"]; exists {
		pairProofMetadata(t, state, g["content_sync"], times, versions)
	}
}

const a13DeferredFields = `/api/agents	body[0].content_sync.reason	extra-in-go
/api/agents	body[0].content_sync.status	value
/api/agents	body[1].content_sync.reason	extra-in-go
/api/agents	body[1].content_sync.status	value
/api/agents	body[2].content_sync.reason	extra-in-go
/api/agents	body[2].content_sync.status	value
/api/agents/<agent_id>	body.content_sync.reason	extra-in-go
/api/agents/<agent_id>	body.content_sync.status	value
/api/agents/<agent_id>/push-diff	body.checked_at	missing-in-go
/api/agents/<agent_id>/push-diff	body.config	missing-in-go
/api/agents/<agent_id>/push-diff	body.content	missing-in-go
/api/agents/<agent_id>/push-diff	body.content_reason	missing-in-go
/api/agents/<agent_id>/push-diff	body.counts	missing-in-go
/api/agents/<agent_id>/push-diff	body.files	missing-in-go
/api/agents/<agent_id>/push-diff	body.files_omitted	missing-in-go
/api/agents/<agent_id>/push-diff	body.reason	extra-in-go
/api/agents/<agent_id>/push-diff	body.status	value
/api/agents/<agent_id>/push-diff	body.unchanged	missing-in-go
/api/agents/<agent_id>/push-diff	body.unchanged_omitted	missing-in-go
/api/agents/<agent_id>/push-diff	body.version	missing-in-go`
