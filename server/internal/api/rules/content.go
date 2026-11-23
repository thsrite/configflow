package rules

import (
	"context"
	"encoding/json"
	"time"

	"configflow/internal/rulesource"
)

// ContentOptions controls an explicit source read. Zero timeouts retain the
// existing 30-second defaults; a context deadline additionally bounds both.
type ContentOptions struct {
	AllowNetwork   bool
	ConnectTimeout time.Duration
	ReadTimeout    time.Duration
}

// ReadRulesetContent delegates to rulesource.Default().ReadJSON.
func ReadRulesetContent(ctx context.Context, config, item, library json.RawMessage, options ContentOptions) (string, error) {
	return rulesource.Default().ReadJSON(ctx, config, item, library, rulesource.ContentOptions{
		AllowNetwork:   options.AllowNetwork,
		ConnectTimeout: options.ConnectTimeout,
		ReadTimeout:    options.ReadTimeout,
	})
}
