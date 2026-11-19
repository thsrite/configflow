package gateway

import (
	"context"

	"configflow/internal/model"
	"configflow/internal/rulefetch"
)

// RuleSourceReader reads a rule declaration using captured fetch settings and cache identities.
type RuleSourceReader func(context.Context, model.RuleConfig, *model.RuleLibraryItem, *rulefetch.SourceConfig) (string, error)

// SetRuleSourceReader installs the model-based reader and returns its restore.
func SetRuleSourceReader(read RuleSourceReader) func() {
	contentMu.Lock()
	previous := ruleSourceReader
	ruleSourceReader = read
	contentMu.Unlock()
	return func() {
		contentMu.Lock()
		ruleSourceReader = previous
		contentMu.Unlock()
	}
}
