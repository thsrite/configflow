package subscriptions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/lifecycle"
	"configflow/internal/logx"
)

func TestProviderCancellationLoggingKeepsRealFailures(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		name    string
		ctx     context.Context
		err     error
		wantLog bool
	}{
		{"canceled request", ctx, fmt.Errorf("read resource: %w", context.Canceled), false},
		{"real failure after disconnect", ctx, errors.New("synthetic failure"), true},
		{"internal cancellation", context.Background(), context.Canceled, true},
		{"accepted detached owner", lifecycle.Detach(ctx), context.Canceled, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			root, err := logx.Setup(logx.Config{Level: logx.Error, Console: &output})
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			response, err := providerFailure(tc.ctx, tc.err)
			if err != nil || response.Status != 500 || !bytes.Contains(response.Body(), []byte(msgProviderFailed)) {
				t.Fatalf("provider error response changed: %v", err)
			}
			if got := strings.Contains(output.String(), msgProviderFailed); got != tc.wantLog {
				t.Fatalf("provider failure logged=%v want %v", got, tc.wantLog)
			}
		})
	}
}
