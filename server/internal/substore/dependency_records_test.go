package substore

import (
	"strings"
	"testing"
)

// W9 review: the update completion time is a Unix timestamp; after a real
// update it must stay a plain decimal token (as before N8-11 W9), not the
// exponent form ("1.7637e+09") of strconv's shortest 'g' format.
func TestUpdateStateFinishedAtToken(t *testing.T) {
	tests := []struct {
		name string
		at   float64
		want string
	}{
		{"idle", 0, `"finished_at":0.0`},
		{"whole second", 1763700000, `"finished_at":1763700000.0`},
		{"fractional", 1763700000.123456, `"finished_at":1763700000.123456`},
		{"quarter", 1763700000.25, `"finished_at":1763700000.25`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := UpdateState{State: "success", FinishedAt: tt.at}.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), tt.want) {
				t.Fatalf("got %s, want %s", raw, tt.want)
			}
		})
	}
}
