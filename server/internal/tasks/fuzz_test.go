package tasks

import (
	"encoding/json"
	"testing"
)

func FuzzTaskHistoryCounters(f *testing.F) {
	for _, seed := range []string{`null`, `true`, `false`, `0`, `-1`, `1.75`, `1e20`, `"42"`, `"9223372036854775808123"`, `"bad"`, `[]`, `{}`, `NaN`, `1e999`, `[1]`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 16384 || !json.Valid(raw) {
			return
		}
		count, err := historyCount(raw)
		if err != nil {
			return
		}
		if count == nil {
			t.Fatal("successful conversion returned nil")
		}
		encoded, err := json.Marshal(count)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := historyCount(encoded)
		if err != nil || decoded.Cmp(count) != 0 {
			t.Fatal("integer conversion lost precision", string(raw), err)
		}
	})
}
