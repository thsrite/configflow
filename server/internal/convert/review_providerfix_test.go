package convert

import (
	"bytes"
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

func TestReviewProviderFixOrderTypeRemainsStrict(t *testing.T) {
	for _, value := range []string{`123`, `123.0`, `true`, `[]`, `{}`} {
		t.Run(value, func(t *testing.T) {
			config := typedDialerConfig(t, `{"nodes":[{"id":"n","name":"N"}],"proxy_groups":[{"id":"g","name":"G","proxies_order":[{"type":`+value+`,"id":"n"}]}]}`)
			want := "proxies_order.type 必须是字符串，当前值：" + value
			if err := ConfigDialers(config); err == nil || err.Error() != want {
				t.Fatalf("want %q, got %v", want, err)
			}
		})
	}
}

func TestReviewProviderFixGroupTypeRemainsStrict(t *testing.T) {
	for _, value := range []string{`123`, `false`, `[]`, `{}`} {
		config := typedDialerConfig(t, `{"proxy_groups":[{"id":"g","name":"G","type":`+value+`}]}`)
		want := "type 必须是字符串，当前值：" + value
		if err := ConfigDialers(config); err == nil || err.Error() != want {
			t.Errorf("want %q, got %v", want, err)
		}
	}
}

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
	if !bytes.Contains(fields["proxies_order"], []byte(`"id":123.0`)) {
		t.Fatalf("stored reference changed: %s", before)
	}
}
