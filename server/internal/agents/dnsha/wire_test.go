package dnsha

import (
	"net/url"
	"testing"
)

func TestRawKeepalivedSpecProtocol(t *testing.T) {
	f := fixtureNew(t, Options{}, false, nil)
	primary, err := f.s.get(f.ctx, "primary")
	if err != nil {
		t.Fatal(err)
	}
	secondary, err := f.s.get(f.ctx, "secondary")
	if err != nil {
		t.Fatal(err)
	}
	expected := `{"safety_version":1,"vip":"192.0.2.53","vrid":53,"priority":150,"self_ip":"192.0.2.20","peer_ip":"192.0.2.21"}`
	payload := rawObject(spec(vip, 53, "primary", primary, secondary))
	if _, err = f.s.call(f.ctx, primary, "POST", "/preview", payload, 30); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.call(f.ctx, primary, "PUT", "", payload, 90); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.call(f.ctx, primary, "DELETE", "", nil, 90); err != nil {
		t.Fatal(err)
	}
	requests := f.client.calls()
	if len(requests) != 3 {
		t.Fatal("unexpected request count", len(requests))
	}
	for _, request := range requests {
		u, err := url.Parse(request.URL)
		if err != nil {
			t.Fatal(err)
		}
		if u.Host != primaryHost+":8082" || request.Header.Get("Authorization") != "Bearer synthetic-primary-token" || !request.Latin1Headers || request.NoProxy || request.NoRedirects {
			t.Fatal("HA transport policy changed")
		}
		if request.Method == "DELETE" {
			if len(request.Body) != 0 || request.Header.Get("Content-Type") != "" {
				t.Fatal("DELETE carried a body")
			}
		} else if string(request.Body) != expected || request.Header.Get("Content-Type") != "application/json" {
			t.Fatal("raw spec body or content type changed", string(request.Body))
		}
		want := 90.0
		if request.Method == "POST" {
			want = 30
		}
		if request.Timeout.Connect.Seconds() != 5 || request.Timeout.Read.Seconds() != want {
			t.Fatal("production HA timeout changed", request.Timeout)
		}
	}
	observed := f.segment.Calls()
	if len(observed) != 3 || string(observed[0].Body) != expected || string(observed[1].Body) != expected || len(observed[2].Body) != 0 {
		t.Fatal("wire receiver saw different raw bytes")
	}
}
