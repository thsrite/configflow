package rules

import (
	"context"
	"encoding/json"
	"errors"
	"time"

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
	profile, err := sourceObject(config)
	if err != nil {
		return "", err
	}
	row, err := sourceObject(item)
	if err != nil {
		return "", err
	}
	lib := row
	if len(library) != 0 && string(library) != "null" {
		lib, err = sourceObject(library)
		if err != nil {
			return "", err
		}
	} else {
		lib = nil
	}
	s := &Service{client: rulefetch.DefaultClient}
	return s.sourceContent(ctx, profile, row, lib, sourceOptions{offline: !options.AllowNetwork,
		connectTimeout: options.ConnectTimeout, readTimeout: options.ReadTimeout, boundDeadline: true})
}
