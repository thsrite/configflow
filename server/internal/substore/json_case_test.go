package substore

import (
	"context"
	"testing"
)

type auditVersionClient struct{ body string }

func (c auditVersionClient) Do(context.Context, *Request) (*Response, error) {
	return &Response{StatusCode: 200, Body: []byte(c.body)}, nil
}

func TestAuditVersionReplyExactMembers(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`{"Version":"spoofed"}`, ""},
		{`{"data":{"version":"proven","VERSION":"spoofed"}}`, "proven"},
		{`{"version":"proven","VERSION":"spoofed"}`, "proven"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			e := Env{HTTP: auditVersionClient{tc.raw}}
			if got := e.FetchRunningVersion(context.Background(), "https://example.com", 5); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
