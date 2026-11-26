package gateway

import (
	"context"

	"configflow/internal/model"
	"configflow/internal/rulefetch"
)

// RuleSourceReader reads a rule declaration using captured fetch settings and cache identities.
type RuleSourceReader func(context.Context, model.RuleConfig, *model.RuleLibraryItem, *rulefetch.SourceConfig) (string, error)
