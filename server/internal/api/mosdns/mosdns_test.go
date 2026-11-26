package mosdns

import (
	"configflow/internal/model"
	"encoding/json"
	"testing"
)

// R5 replaces Python int() at the stored settings boundary: only integral
// int64 numbers and strict decimal text are consumed. The legacy Python
// coercions are retained here as explicit divergence controls.
func TestCacheIntegersUseSharedR5(t *testing.T) {
	cases := []struct {
		raw    string
		native model.Int
		python string
	}{
		{"true", 7, "1"}, {"false", 7, "0"}, {"60.9", 7, "60"}, {"-0.5", 7, "0"},
		{"1e+20", 7, "100000000000000000000"}, {"1e+30", 7, "1000000000000000019884624838656"},
		{"100000000000000000000", 7, "100000000000000000000"},
		{`"128"`, 128, "128"}, {`" 1_000 "`, 7, "1000"}, {`"1__0"`, 7, "7"}, {`"_1"`, 7, "7"},
		{`"+7"`, 7, "7"}, {`"-3"`, -3, "-3"}, {`"12.5"`, 7, "7"}, {`"x"`, 7, "7"},
		{"[1]", 7, "7"}, {"null", 7, "7"}, {"{}", 7, "7"}, {"1e400", 7, "7"}, {"1e2", 100, "100"},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			var settings model.MosDNSSettings
			if err := json.Unmarshal([]byte(`{"cache_size":`+tc.raw+`}`), &settings); err != nil {
				t.Fatal(err)
			}
			if got := or(settings.CacheSize, 7); got != tc.native {
				t.Fatalf("native=%d want=%d (Python=%s)", got, tc.native, tc.python)
			}
		})
	}
}
