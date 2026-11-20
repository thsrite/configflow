package rules

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"configflow/internal/model"
	"configflow/internal/rulefetch"
)

// ContentOptions controls an explicit source read. Zero timeouts retain the
// existing 30-second defaults; a context deadline additionally bounds both.
type ContentOptions struct {
	AllowNetwork   bool
	ConnectTimeout time.Duration
	ReadTimeout    time.Duration
}

// ReadRulesetContent adapts native publish-snapshot values to the existing C32
// source owner. Cache, inline, remote and alternate-URL decisions stay in
// sourceContent. The caller supplies its write-owner context; an explicit
// download deadline is preserved, rather than detached here.
func ReadRulesetContent(ctx context.Context, config, item, library json.RawMessage, options ContentOptions) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if options.ConnectTimeout < 0 || options.ReadTimeout < 0 {
		return "", errors.New("rule content timeouts must not be negative")
	}
	profile, err := model.DecodeConfig(config)
	if err != nil {
		return "", err
	}
	var row model.RuleConfig
	if err := json.Unmarshal(item, &row); err != nil {
		return "", err
	}
	var lib *model.RuleLibraryItem
	if trimmed := bytes.TrimSpace(library); len(trimmed) != 0 && !bytes.Equal(trimmed, []byte("null")) {
		lib = &model.RuleLibraryItem{}
		if err := json.Unmarshal(trimmed, lib); err != nil {
			return "", err
		}
	}
	s := &Service{client: rulefetch.DefaultClient}
	return s.sourceContent(ctx, fetchConfig(profile), row, lib, sourceOptions{offline: !options.AllowNetwork,
		connectTimeout: options.ConnectTimeout, readTimeout: options.ReadTimeout, boundDeadline: true})
}
