package agents_test

import (
	"net/http"
	"testing"

	"configflow/internal/httpx/httpxtest"
)

func TestDNSFollowEveryRouteRequiresPanelAuthAndHEADIsReadOnly(t *testing.T) {
	srv, fake, admin := panelServer(t, "1.8.60-go")
	path := "/api/agents/dns/dns-follow"
	for _, tc := range []struct{ method, suffix string }{{"GET", ""}, {"HEAD", ""}, {"PUT", ""}, {"DELETE", ""}, {"POST", "/retry"}} {
		t.Run(tc.method+tc.suffix, func(t *testing.T) {
			srv.Do(tc.method, path+tc.suffix).WantStatus(http.StatusUnauthorized)
			srv.Do(tc.method, path+tc.suffix, httpxtest.Bearer(dnsToken)).WantStatus(http.StatusUnauthorized)
		})
	}
	if len(fake.calls()) != 0 {
		t.Fatal("unauthorized request reached Agent")
	}
	before := srv.Repo.CommitCount()
	get := srv.Get(path, admin)
	head := srv.Do(http.MethodHead, path, admin).WantStatus(get.Status)
	if len(head.Body) != 0 || srv.Repo.CommitCount() != before || len(fake.calls()) != 0 {
		t.Fatal("read options/HEAD wrote or contacted device")
	}
}
