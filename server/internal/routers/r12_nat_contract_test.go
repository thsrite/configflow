package routers

import (
	"encoding/json"
	"reflect"
	"testing"
)

var _ Op = (*NATListSync)(nil)
var _ Op = (*NATUpdateOp)(nil)

func TestR12TypedNATContract(t *testing.T) {
	sync := r07NewSync()
	sync.Summary = "synthetic"
	var op Op = &sync
	if op.OpKind() != "nat_list_sync" || op.OpSummary() != sync.Summary {
		t.Fatal("OpHead is not exposed through the R12 interface")
	}
	stages := []Stage{{Key: StageNATBypass, Title: "synthetic", Ops: []Op{op}}}
	raw, err := json.Marshal(stages)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []map[string]json.RawMessage
	if err = json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded[0]) != 4 || string(decoded[0]["manual"]) != "false" {
		t.Fatalf("Stage JSON presence differs: %s", raw)
	}
	d := NewRouterOSWithHTTP("", nil)
	done := d.ReuseAddressReads()
	defer done()
	d.addressReads["L"] = addressProof{Rows: []Row{{".id": "*1"}}}
	d.InvalidateAddressReads()
	if len(d.addressReads) != 0 {
		t.Fatal("exported invalidation kept a proof")
	}
	if !reflect.DeepEqual(sync.DstRemove, []ROSResource{}) {
		t.Fatal("typed operation changed required empty arrays")
	}
}
