package convert

import (
	"bytes"
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

func TestReviewProviderFixNumericReferenceIDsRemainCompatible(t *testing.T) {
	source := `{"nodes":[{"id":123.0,"name":"N"}],"proxy_groups":[{"id":"g","name":"G","type":"select","proxies_order":[{"type":"node","id":123.0}]}]}`
	config := typedDialerConfig(t, source)
	if err := ConfigDialers(config); err != nil {
		t.Fatal(err)
	}
	refs := GroupRefs(config.Profile.ProxyGroups[0])
	if len(refs) != 1 || refs[0] != (Ref{"node", "123.0"}) {
		t.Fatalf("numeric reference changed: %+v", refs)
	}
	before, err := model.Marshal(config.Profile.ProxyGroups[0])
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(before, &fields); err != nil {
		t.Fatal(err)
	}
	// Plain records (O18 T1c) write the read text, not the stored number.
	if !bytes.Contains(fields["proxies_order"], []byte(`"id":"123.0"`)) {
		t.Fatalf("stored reference: %s", before)
	}
}
