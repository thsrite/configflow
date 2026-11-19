package dnsha

import (
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

func TestChildReservationsRejectProjectedDeploymentIDs(t *testing.T) {
	for _, tt := range []struct {
		raw      string
		accepted bool
	}{{`{"primary":"7"}`, true}, {`{"primary":7}`, false}, {`{"primary":null}`, false}} {
		t.Run(tt.raw, func(t *testing.T) {
			f := fixtureNew(t, Options{}, false, nil)
			var journal model.HAJournal
			if err := json.Unmarshal([]byte(`{"id":"pair","children":`+tt.raw+`}`), &journal); err != nil {
				t.Fatal(err)
			}
			before, err := f.r.Store().Fingerprint()
			if err != nil {
				t.Fatal(err)
			}
			err = f.s.beginChildren(f.ctx, &journal)
			if (err == nil) != tt.accepted {
				t.Fatalf("raw=%s accepted=%v err=%v", tt.raw, err == nil, err)
			}
			if !tt.accepted {
				if after, err := f.r.Store().Fingerprint(); err != nil || after != before {
					t.Fatal("invalid child reserved journal", err)
				}
			}
		})
	}
}
