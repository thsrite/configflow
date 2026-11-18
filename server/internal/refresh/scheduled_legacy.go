package refresh

import (
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/rulefetch"
	"configflow/internal/substore"
	"configflow/internal/taskstatus"
	"context"
	"errors"
	"time"
)

// SubscriptionKey adapts an existing task ID to its native string identity.
func SubscriptionKey(id any) string { return SourceTaskKey(legacyID(id)) }

// RuleSourceKey adapts an existing task ID to its native string identity.
func RuleSourceKey(id any) string { return RuleTaskKey(legacyID(id)) }
func legacyID(value any) string   { var id model.String; _ = decodeLegacy(value, &id); return string(id) }

// DescribeFailure adapts controlled errors from baseline collaborators.
func DescribeFailure(err error) string { return FailureReason(errorFromLegacy(err)) }
func errorFromLegacy(err error) error {
	var reporter interface{ PythonClass() string }
	if errors.As(err, &reporter) && reporter.PythonClass() == "ValueError" {
		return &PublicFailure{Reason: err.Error(), Cause: err}
	}
	return err
}

// NextRunAt adapts the legacy scalar and receipt boundary to NextDue.
func NextRunAt(entry any, interval jsonx.Number, baseline any, now *time.Time) (*time.Time, error) {
	seconds, ok := interval.Int64()
	if !ok {
		return nil, ErrScheduleTime
	}
	receipt := &taskstatus.Receipt{}
	_ = decodeLegacy(entry, receipt)
	text, _ := baseline.(string)
	return NextDue(receipt, model.Int(seconds), optionalText(text), now)
}
func parseTime(value any) (time.Time, bool) { text, _ := value.(string); return parseStamp(text) }

// SubscriptionCacheTime adapts the native nullable cache timestamp.
func SubscriptionCacheTime(ctx context.Context, sub *jsonx.Object) (any, error) {
	at, err := SourceCacheAt(ctx, sourceRecord(sub))
	if at == nil {
		return nil, err
	}
	return *at, err
}

// RuleCacheTime adapts the native nullable file timestamp.
func RuleCacheTime(ctx context.Context, rule *jsonx.Object) (any, error) {
	at, err := RuleCacheAt(ctx, libraryRecord(rule))
	if at == nil {
		return nil, err
	}
	return *at, err
}

// IsLibrarySource adapts a legacy rule declaration.
func IsLibrarySource(ctx context.Context, rule *jsonx.Object) (bool, error) {
	return StoredRule(ctx, libraryRecord(rule))
}

// RefreshSubscription adapts baseline task/source callers.
func RefreshSubscription(ctx context.Context, sub *jsonx.Object, trigger string) ([]any, *substore.SubscriptionResult, *jsonx.Object, error) {
	nodes, result, cache, err := RefreshSource(ctx, sourceRecord(sub), trigger)
	if err != nil {
		return nil, nil, nil, err
	}
	return legacyNodes(nodes), oldResult(result), legacyObject(cache), nil
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

// FindResource adapts a native current resource snapshot.
func FindResource(ctx context.Context, kind, id string) (*jsonx.Object, error) {
	resource, err := LookupResource(ctx, kind, id)
	if err != nil {
		return nil, err
	}
	if resource.Subscription != nil {
		return legacyObject(resource.Subscription), nil
	}
	return legacyObject(resource.Rule), nil
}

// RunNow adapts the manual task entry point.
func RunNow(ctx context.Context, kind, id string) (string, error) {
	return RunResourceNow(ctx, kind, id)
}
func (e *TaskBusy) PythonClass() string       { return "TaskBusy" }
func (e *LookupError) PythonClass() string    { return "LookupError" }
func (e *StaleRuleCache) PythonClass() string { return "StaleRuleCache" }
