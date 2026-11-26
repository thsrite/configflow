package dnsha

import (
	"configflow/internal/jsonresp"
	"os"
	"path/filepath"
	"testing"

	"configflow/internal/deploy"
)

func TestMaterializationPreservesFuturePayloadMembers(t *testing.T) {
	f := fixtureNew(t, Options{}, false, nil)
	_, err := f.s.Configure(f.ctx, "primary", []byte(`{"peer_id":"secondary","vip":"192.0.2.53"}`), "")
	if err != nil {
		t.Fatal(err)
	}
	a, err := f.s.get(f.ctx, "primary")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.s.folder(a.OxiDNSHAPublication), "payload.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	original := parseObject(data)
	original["future_payload"] = []byte(`{"opaque":[null,false,7],"version":"next"}`)
	if err = deploy.WriteFileAtomic(f.ctx, path, jsonresp.Value(original)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Advance(f.ctx, "primary"); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	saved := parseObject(data)
	if string(saved["future_payload"]) != string(original["future_payload"]) {
		t.Fatal("materialization lost future payload member")
	}
	if len(saved.obj("bundle")) == 0 {
		t.Fatal("test did not materialize the payload")
	}
}
