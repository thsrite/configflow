package health

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"configflow/internal/convert"
	"configflow/internal/jsonresp"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/subs"
	"configflow/internal/substore"
)

func init() {
	currentProbeEnv.Collaborators = defaultCollaborators(nil)
}

func defaultCollaborators(converterFn func() convert.ProxyStringConverter) Collaborators {
	return Collaborators{
		// Removed by N8-8a (convert typed migration).
		NeedsRemoteConversion: func(proxyString string) bool {
			if proxyString == "" {
				return false
			}
			parsed, err := convert.ParseStructuredProxyString(proxyString)
			return err == nil && parsed == nil
		},
		// Removed by N8-8a + N8-9 S-subs/substore (convert + substore typed migration).
		ConvertNode: func(ctx context.Context, nodeRaw json.RawMessage) (fields model.Fields, ok bool) {
			defer func() {
				if recover() != nil {
					fields = model.Fields{}
					ok = false
				}
			}()
			parsed, err := jsonx.Parse(nodeRaw)
			if err != nil {
				return model.Fields{}, false
			}
			node, isObj := parsed.(*jsonx.Object)
			if !isObj || node == nil {
				return model.Fields{}, false
			}
			node = node.DeepCopy()
			var conv convert.ProxyStringConverter
			if converterFn != nil {
				conv = converterFn()
			}
			if conv == nil {
				conv = substore.Converter()
			}
			var proxy *jsonx.Object
			if jsonx.Truthy(node.Value("proxy_string")) || node.Has("params") {
				converted, err := convert.ConvertNodeToMihomo(ctx, node, conv)
				if err != nil {
					return model.Fields{}, false
				}
				proxy = converted
			} else {
				proxy = node
				convert.FixProxyFields(proxy)
			}
			if proxy == nil || !jsonx.Truthy(proxy.Value("type")) {
				return model.Fields{}, false
			}
			proxy.Set("name", node.Value("name"))
			encoded, err := jsonx.Compact(proxy)
			if err != nil {
				return model.Fields{}, false
			}
			if err := json.Unmarshal(encoded, &fields); err != nil {
				return model.Fields{}, false
			}
			return fields, true
		},
		// Removed by N8-9 S-subs/substore (substore typed migration).
		LoadSubscriptionCache: func(ctx context.Context, subID string) (json.RawMessage, error) {
			cache, err := substore.LoadSubscriptionCache(ctx, subID)
			if err != nil || cache == nil {
				return nil, err
			}
			raw, err := jsonx.Compact(cache)
			return json.RawMessage(raw), err
		},
		// Removed by N8-9 S-subs (subs typed migration).
		FilterSubscriptionNodes: func(sub model.Subscription, nodes []json.RawMessage) ([]json.RawMessage, error) {
			subBytes, err := model.Marshal(&sub)
			if err != nil {
				return nil, err
			}
			subParsed, err := jsonx.Parse(subBytes)
			if err != nil {
				return nil, err
			}
			subObj, ok := subParsed.(*jsonx.Object)
			if !ok {
				return nil, fmt.Errorf("subscription record must encode to object")
			}
			legacyNodes := make([]any, 0, len(nodes))
			for _, rawNode := range nodes {
				val, err := jsonx.Parse(rawNode)
				if err != nil {
					continue
				}
				legacyNodes = append(legacyNodes, val)
			}
			kept := subs.FilterSubscriptionItems(subObj, legacyNodes)
			out := make([]json.RawMessage, 0, len(kept))
			for _, item := range kept {
				rawItem, err := jsonx.Compact(item)
				if err != nil {
					return nil, err
				}
				out = append(out, json.RawMessage(rawItem))
			}
			return out, nil
		},
		// Removed in N8-11 when repo provides typed shared JSON file helpers.
		ReadLatencyFile: func(ctx context.Context) (json.RawMessage, bool) {
			repository, err := repo.GetRepository(ctx)
			if err != nil {
				return nil, false
			}
			data, err := repository.ReadSharedJSON(ctx, LatencyFile)
			if err != nil || data == nil {
				return nil, false
			}
			raw, err := jsonx.Compact(data)
			if err != nil {
				return nil, false
			}
			return json.RawMessage(raw), true
		},
		// Removed in N8-11 when repo provides typed shared JSON file helpers.
		WriteLatencyFile: func(ctx context.Context, data json.RawMessage) error {
			repository, err := repo.GetRepository(ctx)
			if err != nil {
				return err
			}
			parsed, err := jsonx.Parse(data)
			if err != nil {
				return err
			}
			_, err = repository.WriteSharedJSON(ctx, LatencyFile, parsed)
			return err
		},
	}
}

// Env is the legacy environment configuration retained for api/nodes tests.
// Removed when api/nodes migrates to ProbeEnv.
type Env struct {
	LookupEnv   func(string) (string, bool)
	Environ     func() []string
	Runner      Runner
	Now         func() time.Time
	ProbeBinary func() (string, error)
	Converter   convert.ProxyStringConverter
}

// DefaultEnv returns the default legacy Env.
func DefaultEnv() *Env {
	return &Env{
		LookupEnv: os.LookupEnv,
		Environ:   os.Environ,
		Runner:    ExecRunner{},
		Now:       time.Now,
	}
}

var currentLegacyEnv = DefaultEnv()

type dynamicLegacyRunner struct{ env *Env }

func (d dynamicLegacyRunner) Run(ctx context.Context, cmd Command) (Result, error) {
	if d.env != nil && d.env.Runner != nil {
		return d.env.Runner.Run(ctx, cmd)
	}
	return ExecRunner{}.Run(ctx, cmd)
}

func probeEnvFromLegacy(env *Env) *ProbeEnv {
	if env == nil {
		env = DefaultEnv()
	}
	p := &ProbeEnv{
		LookupEnv: func(key string) (string, bool) {
			if env.LookupEnv != nil {
				return env.LookupEnv(key)
			}
			return "", false
		},
		Environ: func() []string {
			if env.Environ != nil {
				return env.Environ()
			}
			return nil
		},
		Runner: dynamicLegacyRunner{env: env},
		Now: func() time.Time {
			if env.Now != nil {
				return env.Now()
			}
			return time.Now()
		},
		Collaborators: defaultCollaborators(func() convert.ProxyStringConverter {
			return env.Converter
		}),
	}
	p.ProbeBinary = func() (string, error) {
		if env.ProbeBinary != nil {
			return env.ProbeBinary()
		}
		return FindBinary(p)
	}
	return p
}

// SetEnv installs env as the active environment and returns a restore callback.
// Removed when api/nodes migrates to SetProbeEnv.
func SetEnv(env *Env) (restore func()) {
	prevLegacy := currentLegacyEnv
	currentLegacyEnv = env
	restoreProbe := SetProbeEnv(probeEnvFromLegacy(env))
	return func() {
		currentLegacyEnv = prevLegacy
		restoreProbe()
	}
}

// CurrentEnv returns the currently installed legacy Env.
// Removed when api/nodes migrates off legacy Env.
func CurrentEnv() *Env { return currentLegacyEnv }

// FindProbeBinary locates the worker binary using a legacy Env.
// Removed when api/nodes migrates to FindBinary.
func FindProbeBinary(e *Env) (string, error) {
	p := probeEnvFromLegacy(e)
	p.ProbeBinary = nil
	return FindBinary(p)
}

func toRawJSON(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	raw, err := jsonx.Compact(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func encodeToLegacyValue(value any) (any, error) {
	raw, err := jsonresp.Encode(value)
	if err != nil {
		return nil, err
	}
	return jsonx.Parse(raw)
}

func encodeToLegacyObject(value any) (*jsonx.Object, error) {
	parsed, err := encodeToLegacyValue(value)
	if err != nil {
		return nil, err
	}
	obj, ok := parsed.(*jsonx.Object)
	if !ok {
		return nil, fmt.Errorf("expected JSON object, got %T", parsed)
	}
	return obj, nil
}

func encodeToLegacySlice(value any) ([]any, error) {
	parsed, err := encodeToLegacyValue(value)
	if err != nil {
		return nil, err
	}
	list, ok := parsed.([]any)
	if !ok {
		return nil, fmt.Errorf("expected JSON array, got %T", parsed)
	}
	return list, nil
}

// NormalizeDNSServers adapts legacy jsonx values to NormalizeDNSServersJSON.
// Removed when api/nodes migrates off jsonx.
func NormalizeDNSServers(value any, bootstrap bool) ([]any, error) {
	raw, err := jsonx.Compact(value)
	if err != nil {
		message := DNSError
		if bootstrap {
			message = BootstrapError
		}
		return nil, valueError(message)
	}
	servers, err := NormalizeDNSServersJSON(raw, bootstrap)
	if err != nil {
		return nil, err
	}
	return encodeToLegacySlice(servers)
}

// NormalizeConfig adapts legacy jsonx values to NormalizeDNSConfigJSON.
// Removed when api/nodes migrates off jsonx.
func NormalizeConfig(value any) (*jsonx.Object, error) {
	raw, err := jsonx.Compact(value)
	if err != nil {
		return nil, valueError(msgSelectDNSMode)
	}
	cfg, err := NormalizeDNSConfigJSON(raw)
	if err != nil {
		return nil, err
	}
	return encodeToLegacyObject(cfg)
}

// LoadConfig adapts LoadDNSConfig to a legacy *jsonx.Object.
// Removed when api/nodes migrates off jsonx.
func LoadConfig(ctx context.Context) (*jsonx.Object, error) {
	cfg, err := LoadDNSConfig(ctx)
	if err != nil {
		return nil, err
	}
	return encodeToLegacyObject(cfg)
}

// EffectiveSettings adapts EffectiveDNSSettingsJSON to a legacy *jsonx.Object.
// Removed when api/nodes migrates off jsonx.
func EffectiveSettings(ctx context.Context, value any) (*jsonx.Object, error) {
	raw, err := toRawJSON(value)
	if err != nil {
		return nil, err
	}
	effective, err := EffectiveDNSSettingsJSON(ctx, raw)
	if err != nil {
		return nil, err
	}
	return encodeToLegacyObject(effective)
}

// DescribeSettings adapts DescribeDNSSettingsJSON to a legacy *jsonx.Object.
// Removed when api/nodes migrates off jsonx.
func DescribeSettings(ctx context.Context, value any) (*jsonx.Object, error) {
	raw, err := toRawJSON(value)
	if err != nil {
		return nil, err
	}
	view, err := DescribeDNSSettingsJSON(ctx, raw)
	if err != nil {
		return nil, err
	}
	return encodeToLegacyObject(view)
}

// SaveConfig adapts SaveDNSConfigJSON to a legacy *jsonx.Object.
// Removed when api/nodes migrates off jsonx.
func SaveConfig(ctx context.Context, value any) (*jsonx.Object, error) {
	raw, err := jsonx.Compact(value)
	if err != nil {
		return nil, valueError(msgSelectDNSMode)
	}
	view, err := SaveDNSConfigJSON(ctx, raw)
	if err != nil {
		return nil, err
	}
	return encodeToLegacyObject(view)
}

// CheckSettings adapts CheckDNSSettingsJSON to a legacy *jsonx.Object.
// Removed when api/nodes migrates off jsonx.
func CheckSettings(ctx context.Context, value any) (*jsonx.Object, error) {
	raw, err := toRawJSON(value)
	if err != nil {
		return nil, err
	}
	result, err := CheckDNSSettingsJSON(ctx, raw)
	if err != nil {
		return nil, err
	}
	return encodeToLegacyObject(result)
}

// DNSSettings adapts ResolveDNSSettings to a legacy slice.
// Removed when api/nodes migrates off jsonx.
func DNSSettings(ctx context.Context) (any, error) {
	servers, err := ResolveDNSSettings(ctx)
	if err != nil {
		return nil, err
	}
	return encodeToLegacySlice(servers)
}

// Settings adapts ResolveSettings to a legacy *jsonx.Object.
// Removed when api/nodes migrates off jsonx.
func Settings(ctx context.Context) (*jsonx.Object, error) {
	settings, err := ResolveSettings(ctx)
	if err != nil {
		return nil, err
	}
	return encodeToLegacyObject(settings)
}

// CollectCandidates adapts CollectCandidateNodes to legacy *jsonx.Object maps.
// Removed when api/nodes migrates off jsonx.
func CollectCandidates(ctx context.Context) ([]string, map[string]*jsonx.Object, error) {
	list, err := CollectCandidateNodes(ctx)
	if err != nil {
		return nil, nil, err
	}
	keys := make([]string, len(list))
	nodes := make(map[string]*jsonx.Object, len(list))
	for i, c := range list {
		keys[i] = c.Key
		parsed, err := jsonx.Parse(c.Raw)
		if err != nil {
			return nil, nil, err
		}
		obj, ok := parsed.(*jsonx.Object)
		if !ok {
			return nil, nil, fmt.Errorf("candidate %s did not decode to object", c.Key)
		}
		nodes[c.Key] = obj
	}
	return keys, nodes, nil
}

// LoadResults adapts LoadLatencyResults to a legacy *jsonx.Object.
// Removed when api/nodes migrates off jsonx.
func LoadResults(ctx context.Context) *jsonx.Object {
	obj, err := encodeToLegacyObject(LoadLatencyResults(ctx))
	if err != nil {
		return jsonx.NewObject()
	}
	return obj
}

// RunTests adapts RunLatencyTests to a legacy *jsonx.Object.
// Removed when api/nodes migrates off jsonx.
func RunTests(ctx context.Context, names, keys []string) (*jsonx.Object, error) {
	outcome, err := RunLatencyTests(ctx, names, keys)
	if err != nil {
		return nil, err
	}
	return encodeToLegacyObject(outcome)
}

// Internal legacy shims used only by replay harnesses in package health.

func ipv4Server(value any, message string) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", valueError(message)
	}
	return ipv4ServerString(text, message)
}

func server(value any) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", valueError(DNSError)
	}
	return serverString(text)
}

func proxyOf(ctx context.Context, node *jsonx.Object, converter convert.ProxyStringConverter) *jsonx.Object {
	if node == nil {
		return nil
	}
	raw, err := jsonx.Compact(node)
	if err != nil {
		return nil
	}
	fields, ok := defaultCollaborators(func() convert.ProxyStringConverter {
		return converter
	}).ConvertNode(ctx, raw)
	if !ok {
		return nil
	}
	obj, err := encodeToLegacyObject(fields)
	if err != nil {
		return nil
	}
	return obj
}

func needsSubStore(node *jsonx.Object) bool {
	if node == nil {
		return false
	}
	raw, ok := node.Value("proxy_string").(string)
	if !ok || raw == "" {
		return false
	}
	return defaultCollaborators(nil).NeedsRemoteConversion(raw)
}

func runProbe(ctx context.Context, proxies []*jsonx.Object, options *jsonx.Object) (*jsonx.Object, error) {
	typedProxies := make([]model.Fields, len(proxies))
	for i, p := range proxies {
		raw, err := jsonx.Compact(p)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &typedProxies[i]); err != nil {
			return nil, err
		}
	}
	var typedOptions model.Fields
	if options != nil {
		raw, err := jsonx.Compact(options)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &typedOptions); err != nil {
			return nil, err
		}
	}
	results, err := runProbeFields(ctx, typedProxies, typedOptions)
	if err != nil {
		return nil, err
	}
	return encodeToLegacyObject(results)
}
