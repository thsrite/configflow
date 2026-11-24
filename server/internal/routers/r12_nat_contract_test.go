package routers

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/rospolicy"
	"encoding/json"
	"reflect"
	"testing"
)

var _ device.Op = (*rospolicy.NATListSync)(nil)
var _ device.Op = (*device.NATUpdateOp)(nil)

func TestR12TypedNATContract(t *testing.T) {
	sync := r07NewSync()
	sync.Summary = "synthetic"
	var op device.Op = &sync
	if op.OpKind() != "nat_list_sync" || op.OpSummary() != sync.Summary {
		t.Fatal("OpHead is not exposed through the R12 interface")
	}
	stages := []device.Stage{{Key: device.StageNATBypass, Title: "synthetic", Ops: []device.Op{op}}}
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
	if !reflect.DeepEqual(sync.DstRemove, []ros.Resource{}) {
		t.Fatal("typed operation changed required empty arrays")
	}
}
