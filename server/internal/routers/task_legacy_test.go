package routers

import (
	"configflow/internal/jsonx"
	"configflow/internal/taskstatus"
	"context"
	"encoding/json"
)

func ruleSyncRows(ctx context.Context, statuses *jsonx.Object) ([]*jsonx.Object, error) {
	b, _ := json.Marshal(statuses)
	var snapshot taskstatus.Snapshot
	_ = json.Unmarshal(b, &snapshot)
	rows, err := ruleSyncTasks(ctx, snapshot)
	if err != nil {
		return nil, err
	}
	out := []*jsonx.Object{}
	for _, r := range rows {
		b, e := json.Marshal(r)
		if e != nil {
			return nil, e
		}
		var v jsonx.Object
		if e = json.Unmarshal(b, &v); e != nil {
			return nil, e
		}
		out = append(out, &v)
	}
	return out, nil
}
