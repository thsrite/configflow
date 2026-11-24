// Command generator invokes existing public generators on an isolated snapshot copy.
// It writes private JSONL results; it does not publish configurations or start loops.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"configflow/internal/agents/trafficstore"
	"configflow/internal/aggregations"
	"configflow/internal/convert"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/mosdns"
	"configflow/internal/oxidns"
	"configflow/internal/provider"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/server"
	"configflow/internal/subs"
	"configflow/internal/substore"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
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

func offlineFetch(context.Context, string, string) (*refresh.SourceResult, error) {
	return nil, errOffline
}

func findAgent(agents []model.Agent, id string) (model.Agent, error) {
	for _, agent := range agents {
		if agent.ID == id {
			return agent, nil
		}
	}
	return model.Agent{}, errors.New("generator selector is absent")
}

func findSubscription(items []model.Subscription, id string) (model.Subscription, error) {
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return model.Subscription{}, errors.New("generator selector is absent")
}

func findAggregation(items []model.Aggregation, id string) (model.Aggregation, error) {
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return model.Aggregation{}, errors.New("generator selector is absent")
}

func encode(value any, err error) (string, error) {
	if err != nil {
		return "", err
	}
	content, err := json.Marshal(value)
	return string(content), err
}

func providerContent(ctx context.Context, srv *server.App, config *model.Config, j job) (string, error) {
	sub, err := findSubscription(config.Subscriptions, j.SubID)
	if err != nil {
		return "", err
	}
	main, err := convert.MihomoDocument(ctx, config, convert.MihomoOptions{BaseURL: baseURL, SkipProviderPreflight: true})
	if err != nil {
		return "", err
	}
	snapshot, err := provider.CaptureDelivery(j.ProfileID, main, config.Profile)
	if err != nil {
		return "", err
	}
	rendered, err := srv.Provider.PrepareSubscription(ctx, sub, snapshot, provider.SubscriptionOptions{Fetch: offlineFetch, SourcePrepared: true})
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
	if node == nil || node.Kind != yaml.MappingNode {
		return "", errors.New("provider output is not a mapping")
	}
	var proxies []*yamlx.Node
	if seq := yamlx.Resolve(yamlx.Get(node, "proxies")); seq != nil && seq.Kind == yaml.SequenceNode {
		proxies = seq.Content
	}
	format := "loon"
	if j.Kind == "subscription-surge" {
		format = "surge"
	}
	return convert.ClientProxyText(proxies, substore.ProxyNodes, format)
}

func generate(ctx context.Context, srv *server.App, repository *repo.Repository, j job) (string, error) {
	config, err := repository.Config(ctx, j.ProfileID, nil)
	if err != nil {
		return "", fmt.Errorf("load generator profile: %w", err)
	}
	opts := convert.MihomoOptions{BaseURL: baseURL, SkipProviderPreflight: true}
	if j.AgentID != "" {
		system, err := repository.System(ctx)
		if err != nil {
			return "", err
		}
		agent, err := findAgent(system.Agents, j.AgentID)
		if err != nil {
			return "", err
		}
		opts.ForAgent = true
		opts.RegionProbe = agent.DomainDiscoveryEnabled != nil && *agent.DomainDiscoveryEnabled
		traffic := trafficstore.NewNative(filepath.Join(repository.DataDir(), "traffic"))
		if rawAgent, err := model.Marshal(agent); err == nil {
			var core subs.CoreAgent
			if json.Unmarshal(rawAgent, &core) == nil {
				if ver := subs.AgentCoreVersion(core, func(id string) subs.CoreStatus {
					st, _ := subs.DecodeCoreStatus(traffic.StatusJSON(id))
					return st
				}); ver != "" {
					if rawVer, err := json.Marshal(ver); err == nil {
						opts.CoreVersion = rawVer
					}
				}
			}
		}
	}
	switch j.Kind {
	case "agent-deployment-bundle":
		return deploymentBundle(ctx, srv, repository, config, j, opts)
	case "aggregation-provider":
		agg, err := findAggregation(config.Aggregations, j.AggID)
		if err != nil {
			return "", err
		}
		main, err := convert.MihomoDocument(ctx, config, convert.MihomoOptions{BaseURL: baseURL, SkipProviderPreflight: true})
		if err != nil {
			return "", err
		}
		mainJSON, err := yamlx.ToJSON(main)
		if err != nil {
			return "", err
		}
		generated, err := srv.Aggregations.GenerateConfig(ctx, agg, config, aggregations.Options{Main: mainJSON, Fetch: offlineFetch, SourcePrepared: true})
		if err != nil {
			return "", err
		}
		return generated.Content, nil
	case "mihomo", "agent-mihomo":
		return convert.MihomoConfig(ctx, config, opts)
	case "surge", "agent-surge":
		return convert.SurgeConfig(ctx, config, baseURL)
	case "loon":
		return convert.LoonConfig(ctx, config, baseURL)
	case "mosdns", "agent-mosdns":
		return mosdns.Generate(config, baseURL)
	case "oxidns", "agent-oxidns":
		profile, err := oxidns.NewTypedProfile(config)
		if err != nil {
			return "", err
		}
		return oxidns.Generate(profile, baseURL)
	case "mosdns-files":
		return encode(mosdns.CustomFiles(config))
	case "mosdns-downloads":
		return encode(mosdns.Downloads(config, baseURL))
	case "oxidns-files":
		profile, err := oxidns.NewTypedProfile(config)
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
		profile, err := oxidns.NewTypedProfile(config)
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
			if file.Behavior != "" {
				item.Behavior = oxidns.ToJSON(oxidns.S(file.Behavior))
			}
			output = append(output, item)
		}
		return encode(output, nil)
	case "mihomo-providers", "mihomo-rules":
		var files []convert.ResourceDownload
		if j.Kind == "mihomo-providers" {
			files, err = convert.MihomoProviderDownloads(ctx, config, baseURL, nil)
		} else {
			files, err = convert.MihomoRulesetDownloads(ctx, config, baseURL)
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
		return providerContent(ctx, srv, config, j)
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
	// Use the same public wiring as the command; deliberately never call lifecycle.Start.
	srv, err := server.New(context.Background(), server.Options{Repo: repository})
	if err != nil {
		return fmt.Errorf("wire generator server: %w", err)
	}
	defer srv.Agents.Close()
	output, err := os.OpenFile(os.Args[2], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("open private generator output: %w", err)
	}
	defer output.Close()
	encoder := json.NewEncoder(output)
	for _, j := range jobs {
		ctx, cancel := context.WithTimeout(repo.WithScope(context.Background()), 90*time.Second)
		content, err := generate(ctx, srv, repository, j)
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
