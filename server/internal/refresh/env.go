package refresh

import (
	"context"
	"sync"

	"configflow/internal/convert"
	"configflow/internal/rulefetch"
	"configflow/internal/substore"
)

// Env holds the I/O of the module: the clock (wall time for receipts and
// due times, monotonic time for durations, sleeps of the scheduler loop),
// the subscription download (sub_store_client.get_subscription_proxies_yaml),
// the rule download client, the HTTP client of fetch_userinfo, node ids and
// the Sub-Store node converter. Tests install fakes with SetEnv.
type Env struct {
	Clock substore.Clock
	// FetchSubscription is get_subscription_proxies_yaml(sub_id, url, **opts).
	FetchSubscription func(ctx context.Context, subID, url string, opts substore.FetchOptions) (*substore.SubscriptionResult, error)
	// RuleClient downloads rule sources (nil: rulefetch.DefaultClient).
	RuleClient *rulefetch.Client
	// HTTP is the requests client of fetch_userinfo.
	HTTP substore.HTTPClient
	// RandomHex is uuid.uuid4().hex[:8] (generated node ids).
	RandomHex func() string
	// Converter is sub_store_client.convert_proxy_string (nil: the
	// process-wide Sub-Store client at the time of the call).
	Converter convert.ProxyStringConverter
}

// DefaultEnv is the production environment on the process-wide Sub-Store
// service.
func DefaultEnv() *Env {
	return &Env{
		Clock: substore.SystemClock,
		FetchSubscription: func(ctx context.Context, subID, url string, opts substore.FetchOptions) (*substore.SubscriptionResult, error) {
			return substore.Default().Client.GetSubscriptionProxiesYAML(ctx, subID, url, "", opts)
		},
		HTTP:      substore.RequestsClient{},
		RandomHex: func() string { return substore.Default().Env.RandomHex() },
	}
}

var (
	envMu sync.Mutex
	env   *Env
)

func currentEnv() *Env {
	envMu.Lock()
	defer envMu.Unlock()
	if env == nil {
		env = DefaultEnv()
	}
	return env
}

func (e *Env) converter() convert.ProxyStringConverter {
	if e.Converter != nil {
		return e.Converter
	}
	return substore.Converter()
}

func (e *Env) ruleClient() *rulefetch.Client {
	if e.RuleClient != nil {
		return e.RuleClient
	}
	return rulefetch.DefaultClient
}

// SetEnv replaces the environment (tests) and returns a function restoring
// the previous one.
func SetEnv(e *Env) (restore func()) {
	envMu.Lock()
	previous := env
	env = e
	envMu.Unlock()
	return func() {
		envMu.Lock()
		env = previous
		envMu.Unlock()
	}
}
