// Command generator invokes existing public generators on an isolated snapshot copy.
// It writes private JSONL results; it does not publish configurations or start loops.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"configflow/internal/agents/trafficstore"
	"configflow/internal/aggregations"
	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/mosdns"
	"configflow/internal/oxidns"
	"configflow/internal/provider"
	"configflow/internal/repo"
	"configflow/internal/server"
	"configflow/internal/subs"
	"configflow/internal/substore"
	"configflow/internal/yamlbridge"
	"configflow/internal/yamlx"
)

const baseURL = "http://127.0.0.1:5001"

// job contains private selectors shared with the Python worker.
type job struct {
	Index     int    `json:"index"`
	Kind      string `json:"kind"`
	ProfileID string `json:"profile_id"`
	AgentID   string `json:"agent_id"`
	SubID     string `json:"sub_id"`
	AggID     string `json:"agg_id"`
}

type result struct {
	Index   int    `json:"index"`
	Status  string `json:"status"`
	Content string `json:"content"`
	Error   string `json:"error,omitempty"`
}

type attachment struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Native  bool   `json:"native,omitempty"`
}

type download struct {
	Name      string          `json:"name"`
	URL       any             `json:"url"`
	LocalPath any             `json:"local_path"`
	Behavior  json.RawMessage `json:"behavior,omitempty"`
}

var errOffline = errors.New("offline")

func offlineFetch(context.Context, string, string) (*substore.SubscriptionResult, error) {
	return nil, errOffline
}

func oxidnsProfile(config *jsonx.Object) (*oxidns.Profile, error) {
	data, err := jsonx.Compact(config)
	if err != nil {
		return nil, err
	}
	return oxidns.ProfileFromJSON(data)
}

func objectByID(items []any, id string) (*jsonx.Object, error) {
	for _, item := range items {
		if obj, ok := item.(*jsonx.Object); ok && obj.GetString("id") == id {
			return obj, nil
		}
	}
	return nil, errors.New("generator selector is absent")
}

func encode(value any, err error) (string, error) {
	if err != nil {
		return "", err
	}
	content, err := json.Marshal(value)
	return string(content), err
}

// encodeLegacy serializes the opaque legacy objects returned by public APIs.
// Typed adapter records elsewhere use encoding/json directly.
func encodeLegacy(value []any, err error) (string, error) {
	if err != nil {
		return "", err
	}
	content, err := jsonx.Compact(value)
	return string(content), err
}

func providerContent(ctx context.Context, config *jsonx.Object, j job) (string, error) {
	sub, err := objectByID(config.GetList("subscriptions"), j.SubID)
	if err != nil {
		return "", err
	}
	main, err := convert.GenerateMihomoDocument(ctx, config, convert.GenerateOptions{BaseURL: baseURL, SkipProviderPreflight: true})
	if err != nil {
		return "", err
	}
	snapshot, err := provider.Capture(j.ProfileID, main, config)
	if err != nil {
		return "", err
	}
	rendered, err := provider.PrepareSubscriptionProvider(ctx, sub, config, snapshot, provider.PrepareOptions{Fetch: offlineFetch, SourcePrepared: true})
	if err != nil {
		return "", err
	}
	if j.Kind == "subscription-mihomo" {
		return rendered.Content, nil
	}
	node, err := yamlx.Parse([]byte(rendered.Content))
	if err != nil {
		return "", err
	}
	if err := yamlx.ProjectJSONKeys(node, yamlx.RejectNonStringKeys); err != nil {
		return "", err
	}
	parsed, err := yamlbridge.Value(node)
	if err != nil {
		return "", err
	}
	obj, ok := parsed.(*jsonx.Object)
	if !ok {
		return "", errors.New("provider output is not a mapping")
	}
	if j.Kind == "subscription-surge" {
		return convert.ConvertProxiesToSurgeText(obj.GetList("proxies"), substore.ProxiesToNodes)
	}
	return convert.ConvertProxiesToLoonText(obj.GetList("proxies"), substore.ProxiesToNodes)
}

func generate(ctx context.Context, j job) (string, error) {
	config, err := repo.GetConfig(ctx, j.ProfileID)
	if err != nil {
		return "", fmt.Errorf("load generator profile: %w", err)
	}
	opts := convert.GenerateOptions{BaseURL: baseURL, SkipProviderPreflight: true}
	if j.AgentID != "" {
		system, err := repo.GetSystemConfig(ctx)
		if err != nil {
			return "", err
		}
		agent, err := objectByID(system.GetList("agents"), j.AgentID)
		if err != nil {
			return "", err
		}
		opts.ForAgent = true
		opts.RegionProbe = agent.GetBool("domain_discovery_enabled")
		traffic, err := trafficstore.Get(ctx)
		if err != nil {
			return "", err
		}
		opts.MihomoCoreVersion = subs.ReportedCoreVersion(agent, traffic.StatusOf)
	}
	switch j.Kind {
	case "agent-deployment-bundle":
		return deploymentBundle(ctx, config, j, opts)
	case "aggregation-provider":
		agg, err := objectByID(config.GetList("subscription_aggregations"), j.AggID)
		if err != nil {
			return "", err
		}
		main, err := convert.GenerateMihomoDocument(ctx, config, convert.GenerateOptions{BaseURL: baseURL, SkipProviderPreflight: true})
		if err != nil {
			return "", err
		}
		a, err := jsonx.Compact(agg)
		if err != nil {
			return "", err
		}
		c, err := jsonx.Compact(config)
		if err != nil {
			return "", err
		}
		m, err := jsonx.Compact(main)
		if err != nil {
			return "", err
		}
		generated, err := aggregations.Generate(ctx, a, c, aggregations.Options{Main: m, Fetch: offlineFetch, SourcePrepared: true})
		if err != nil {
			return "", err
		}
		return generated.Content, nil
	case "mihomo", "agent-mihomo":
		return convert.GenerateMihomoConfig(ctx, config, opts)
	case "surge", "agent-surge":
		return convert.GenerateSurgeConfig(ctx, config, baseURL)
	case "loon":
		return convert.GenerateLoonConfig(ctx, config, baseURL)
	case "mosdns", "agent-mosdns":
		return mosdns.GenerateMosDNSConfig(config, baseURL)
	case "oxidns", "agent-oxidns":
		profile, err := oxidnsProfile(config)
		if err != nil {
			return "", err
		}
		return oxidns.Generate(profile, baseURL)
	case "mosdns-files":
		return encodeLegacy(mosdns.GetMosDNSCustomFiles(config))
	case "mosdns-downloads":
		return encodeLegacy(mosdns.GetMosDNSRulesetDownloads(config, baseURL))
	case "oxidns-files":
		profile, err := oxidnsProfile(config)
		if err != nil {
			return "", err
		}
		files, err := oxidns.CustomFiles(profile)
		if err != nil {
			return "", err
		}
		output := make([]attachment, 0, len(files))
		for _, file := range files {
			output = append(output, attachment{Path: file.Path, Content: string(file.Content), Native: file.Native})
		}
		return encode(output, nil)
	case "oxidns-downloads":
		profile, err := oxidnsProfile(config)
		if err != nil {
			return "", err
		}
		files, err := oxidns.RulesetDownloads(profile, baseURL)
		if err != nil {
			return "", err
		}
		output := make([]download, 0, len(files))
		for _, file := range files {
			item := download{Name: file.Name, URL: file.URL, LocalPath: file.LocalPath}
			if file.Behavior != nil {
				item.Behavior = oxidns.ToJSON(file.Behavior)
			}
			output = append(output, item)
		}
		return encode(output, nil)
	case "mihomo-providers", "mihomo-rules":
		var files []convert.Download
		if j.Kind == "mihomo-providers" {
			files, err = convert.GetMihomoProviderDownloads(ctx, config, baseURL, nil)
		} else {
			files, err = convert.GetMihomoRulesetDownloads(ctx, config, baseURL)
		}
		if err != nil {
			return "", err
		}
		output := make([]download, 0, len(files))
		for _, file := range files {
			output = append(output, download{Name: file.Name, URL: file.URL, LocalPath: file.LocalPath})
		}
		return encode(output, nil)
	case "subscription-mihomo", "subscription-surge", "subscription-loon":
		return providerContent(ctx, config, j)
	default:
		return "", errors.New("unknown generator kind")
	}
}

func run() error {
	if len(os.Args) != 3 {
		return errors.New("expected private manifest and output paths")
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		return fmt.Errorf("read generator manifest: %w", err)
	}
	var jobs []job
	if err := json.Unmarshal(data, &jobs); err != nil {
		return fmt.Errorf("decode generator manifest: %w", err)
	}
	repository, err := repo.OpenDefault(repo.DataDir())
	if err != nil {
		return fmt.Errorf("open generator copy: %w", err)
	}
	repo.SetRepository(repository)
	// Use the same public wiring as the command; deliberately never call lifecycle.Start.
	_ = server.NewApp()
	output, err := os.OpenFile(os.Args[2], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("open private generator output: %w", err)
	}
	defer output.Close()
	encoder := json.NewEncoder(output)
	for _, j := range jobs {
		ctx, cancel := context.WithTimeout(repo.WithScope(context.Background()), 90*time.Second)
		content, err := generate(ctx, j)
		repo.ResetConfigContext(ctx)
		cancel()
		r := result{Index: j.Index, Status: "ok", Content: content}
		if err != nil {
			r.Status = "error"
			r.Error = err.Error()
		}
		if err := encoder.Encode(r); err != nil {
			return fmt.Errorf("write generator result: %w", err)
		}
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		// Full details belong to private output files, never terminal diagnostics.
		logx.Get("proddiff.generator").Error("generator worker failed")
		os.Exit(2)
	}
}
