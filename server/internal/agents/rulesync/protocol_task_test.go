package rulesync

import (
	"encoding/json"
	"testing"
)

func TestNativeTaskReadsExactProtocolKeys(t *testing.T) {
	for _, tc := range []struct {
		raw, id, status string
		reject          bool
	}{
		{`{"id":"owned","ID":"forged","status":"running","Status":"succeeded"}`, "owned", "running", false},
		{`{"ID":"forged","Status":"succeeded"}`, "", "", false},
		{`{"id":"owned","id":"forged"}`, "", "", true},
		{`{"id":false}`, "", "", true},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			var task nativeTask
			err := json.Unmarshal([]byte(tc.raw), &task)
			if tc.reject {
				if err == nil {
					t.Fatal("accepted malformed task")
				}
				return
			}
			if err != nil || task.ID != tc.id || task.Status != tc.status {
				t.Fatalf("%+v %v", task, err)
			}
		})
	}
}
