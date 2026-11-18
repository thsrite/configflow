package repo

import (
	"testing"

	"configflow/internal/jsonx"
)

// The typed resolution must give the legacy view byte for byte on every
// valid document: the reference base, each collect_refs fixture that
// Python resolved, and a full default repository, with and without roots.
func TestTypedConfigMatchesLegacyResolution(t *testing.T) {
	documents := map[string]*jsonx.Object{"base": refBase(t)}
	for _, item := range refLoad(t).GetList("collect_refs") {
		record := item.(*jsonx.Object)
		if record.Has("error") {
			continue
		}
		document := refBase(t)
		refPatch(t, document, record.GetList("patch"))
		if ValidateDocument(document) != nil {
			continue
		}
		documents["collect_refs/"+refText(t, record.Value("patch"))] = document
	}
	r := must(OpenDefault(t.TempDir()))(t)
	documents["default repository"] = must(r.ExportAll(bg, false))(t)
	rootSets := []ResourceRoots{nil, {"nodes": nil}, {"subscriptions": nil, "subscription_aggregations": nil}}
	compared := 0
	for label, document := range documents {
		typed := must(modelFromLegacy(document, nil))(t)
		for profileID := range document.GetObject("profiles").All() {
			for _, roots := range rootSets {
				for _, agents := range []bool{true, false} {
					want, wantErr := resolveProfile(document, profileID, roots, agents)
					got, err := legacyConfig(typed, profileID, roots, agents)
					if (err == nil) != (wantErr == nil) {
						t.Fatalf("%s/%s: errors differ: %v / %v", label, profileID, err, wantErr)
					}
					if err != nil {
						if err.Error() != wantErr.Error() {
							t.Fatalf("%s/%s: %v / %v", label, profileID, err, wantErr)
						}
						continue
					}
					if left, right := refText(t, got), refText(t, want); left != right {
						t.Fatalf("%s/%s roots=%v agents=%v:\n go %s\nold %s", label, profileID, roots, agents, left, right)
					}
					compared++
				}
			}
		}
	}
	if compared < 12 {
		t.Fatalf("only %d views compared", compared)
	}
	t.Logf("%d documents, %d views identical", len(documents), compared)
}
