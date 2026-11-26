package ipv6

import (
	"context"
	"testing"

	"configflow/internal/agents"
)

func TestAdapterRejectsDuplicateAgentReplyKeys(t *testing.T) {
	s, ctx := fixture(t)
	a := s.lookup(ctx, "source")
	for _, body := range []string{`{"success":false,"success":true}`, `{"success":true,"proof":{"id":"a","id":"b"}}`} {
		s.Manager.SetClient(clientFunc(func(context.Context, agents.Request) (*agents.Response, error) {
			return &agents.Response{StatusCode: 200, Body: []byte(body)}, nil
		}))
		_, err := s.request(ctx, a, "", nil)
		requireCode(t, err, "INVALID_RESPONSE", 502)
	}
}
