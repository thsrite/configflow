package agents_test

// Regression tests of the A03 review; the Python reference for each case is
// the Flask differential (difftest reviewRequests), these run without it.

import (
	"strings"
	"testing"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
)

func TestHeartbeatMetricIntBeyondFloatRangeIsOverflowError(t *testing.T) {
	h := newHeartbeat(t)
	huge := "1" + strings.Repeat("0", 400)
	post := func(metrics string) *httpxtest.Result {
		return h.s.Post(h.path(), httpxtest.Bearer(h.token),
			httpxtest.Body([]byte(`{"system_metrics": `+metrics+`}`), "application/json"))
	}
	overflow := jsonx.Obj("success", false, "message", "int too large to convert to float")
	expectHeartbeatNative(t, h, `{"system_metrics": {"memory": {"total": `+huge+`}}}`, 500, overflow)
	expectHeartbeatNative(t, h, `{"system_metrics": {"cpu": {"usage_percent": -`+huge+`}}}`, 500, overflow)
	// Python stops at the first failing section in _METRIC_FIELDS order and
	// checks a section's keys before its values.
	post(`{"network": {"bytes_sent": ` + huge + `}, "cpu": {"usage_percent": 101}}`).WantStatus(400)
	post(`{"cpu": {"usage_percent": 101}, "network": {"bytes_sent": ` + huge + `}}`).WantStatus(400)
	post(`{"cpu": {"core_count": ` + huge + `, "bogus": 1}}`).WantStatus(400)
	expectHeartbeatNative(t, h, `{"system_metrics": {"cpu": {"core_count": `+huge+`, "usage_percent": true}}}`, 500, overflow)
	post(`{"network": {"bytes_sent": 18446744073709551616}}`).WantStatus(400)
}

func TestHeartbeatIntBeyondDigitLimitIsValueError(t *testing.T) {
	h := newHeartbeat(t)
	expectHeartbeatNative(t, h, `{"version": 1`+strings.Repeat("0", 4300)+`}`, 500, jsonx.Obj("success", false, "message",
		"Exceeds the limit (4300 digits) for integer string conversion: value has 4301 digits; use sys.set_int_max_str_digits() to increase the limit"))
}

func TestRegistrationForwardedForIsLatin1(t *testing.T) {
	s := newServer(t)
	s.Post("/api/agents/register", httpxtest.JSON(jsonx.Obj("name", "fwd")),
		httpxtest.Header("X-Forwarded-For", "\xa0caf\xc3\xa9\xa0, 1.2.3.4")).WantStatus(200)
	agents := system(t, s).GetList("agents")
	if got := agents[len(agents)-1].(*jsonx.Object).GetString("host"); got != "cafÃ©" {
		t.Fatalf("host %q", got)
	}
}
