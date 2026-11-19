package refresh

import (
	"context"
	"errors"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/rulefetch"
)

// RuleSourceKey adapts an existing task ID to its native string identity.
func RuleSourceKey(id any) string { return RuleTaskKey(legacyID(id)) }
func legacyID(value any) string   { var id model.String; _ = decodeLegacy(value, &id); return string(id) }

// DescribeFailure adapts controlled errors from baseline collaborators.
func DescribeFailure(err error) string { return FailureReason(errorFromLegacy(err)) }
func errorFromLegacy(err error) error {
	var reporter interface{ ErrorClass() string }
	if errors.As(err, &reporter) && reporter.ErrorClass() == "ValueError" {
		return &PublicFailure{Reason: err.Error(), Cause: err}
	}
	return err
}

// RuleOptions is the current unported rule read boundary.
type RuleOptions struct {
	Config     *jsonx.Object
	Fetch      func(string) (any, error)
	Timeout    time.Duration
	AllowStale bool
}

func (o RuleOptions) native() RuleReadOptions {
	result := RuleReadOptions{Timeout: o.Timeout, AllowStale: o.AllowStale}
	if o.Config != nil {
		input := &rulefetch.SourceConfig{}
		_ = decodeLegacy(o.Config.GetList("rule_library"), &input.Library)
		_ = decodeLegacy(o.Config.GetObject("system_config"), &input.Settings)
		result.Config = input
	}
	if o.Fetch != nil {
		result.Fetch = func(ctx context.Context, url string) (rulefetch.Content, error) {
			if err := ctx.Err(); err != nil {
				return rulefetch.Content{}, err
			}
			value, err := o.Fetch(url)
			if err != nil {
				return rulefetch.Content{}, errorFromLegacy(err)
			}
			if err := ctx.Err(); err != nil {
				return rulefetch.Content{}, err
			}
			switch content := value.(type) {
			case string:
				return rulefetch.Content{Data: []byte(content), Text: true}, nil
			case []byte:
				return rulefetch.Content{Data: content}, nil
			}
			return rulefetch.Content{}, errors.New("rule content must be text or bytes")
		}
	}
	return result
}

// RefreshRuleSource adapts legacy resource/option records.
func RefreshRuleSource(ctx context.Context, rule *jsonx.Object, trigger string, opt RuleOptions) ([]byte, error, error) {
	return RefreshRemote(ctx, libraryRecord(rule), trigger, opt.native())
}

// ReadRuleSource adapts legacy consumers of the source cache.
func ReadRuleSource(ctx context.Context, rule *jsonx.Object, opt RuleOptions) ([]byte, error, error) {
	return ReadRemote(ctx, libraryRecord(rule), opt.native())
}

// ErrorClass returns the stable native logging category.
func (e *TaskBusy) ErrorClass() string { return "TaskBusy" }

// ErrorClass returns the stable native logging category.
func (e *LookupError) ErrorClass() string { return "LookupError" }

// ErrorClass returns the stable native logging category.
func (e *StaleRuleCache) ErrorClass() string { return "StaleRuleCache" }

// ErrorClass returns the stable native logging category.
func (e *SourceValidationError) ErrorClass() string {
	if e.CacheShape {
		return "DialerReferenceError"
	}
	return "ValueError"
}
