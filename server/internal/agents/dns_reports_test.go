package agents

import (
	"bytes"
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

func TestTypedDNSReportUsesOriginalTokens(t *testing.T) {
	for _, tt := range []struct {
		raw     string
		present bool
		level   string
		fix     bool
	}{
		{`{"dns_checks":null}`, false, "", false},
		{`{"dns_checks":{}}`, false, "", false},
		{`{"dns_checks":[{"name":7,"level":"pass","message":"ok"}]}`, true, "warn", true},
		{`{"dns_checks":[{"name":"listener","level":"pass","message":7}]}`, true, "warn", true},
		{`{"dns_checks":[{"name":7,"Name":"forged","level":"pass","message":"ok"}]}`, true, "warn", true},
		{`{"dns_checks":[{"name":"listener","level":"pass","message":"ok","fix":7,"future":{"x":2.00}}]}`, true, "pass", false},
	} {
		var payload model.Deployment
		if err := json.Unmarshal([]byte(tt.raw), &payload); err != nil {
			t.Fatal(err)
		}
		before, _ := json.Marshal(payload)
		report, err := SanitizeDNSReport(&payload, nil)
		if err != nil || (report.Checks != nil) != tt.present {
			t.Fatalf("input=%s report=%+v err=%v", tt.raw, report, err)
		}
		if tt.present {
			if len(*report.Checks) != 1 || (*report.Checks)[0].Level != tt.level || ((*report.Checks)[0].Fix != nil) != tt.fix {
				t.Fatalf("input=%s checks=%+v", tt.raw, *report.Checks)
			}
		}
		raw, _ := json.Marshal(report)
		if bytes.Contains(raw, []byte("future")) {
			t.Fatal("unknown field escaped")
		}
		after, _ := json.Marshal(payload)
		if !bytes.Equal(before, after) {
			t.Fatal("report source mutated")
		}
	}
}
