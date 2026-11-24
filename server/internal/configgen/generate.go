// Package configgen materializes manual and subscription configurations through
// the shared converters. It owns detached file/archive generation; deployment
// publication and frozen artifacts remain with the Agent deployment owner.
package configgen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"configflow/internal/agents/trafficstore"
	"configflow/internal/convert"
	"configflow/internal/faultprobe"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/mosdns"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
	"configflow/internal/subs"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// Engine identifies one supported generated configuration format.
type Engine string

// Supported engine identifiers.
const (
	Mihomo Engine = "mihomo"
	Surge  Engine = "surge"
	Loon   Engine = "loon"
	MosDNS Engine = "mosdns"
	OxiDNS Engine = "oxidns"
)

// Valid reports whether engine has a native converter.
func (e Engine) Valid() bool {
	switch e {
	case Mihomo, Surge, Loon, MosDNS, OxiDNS:
		return true
	}
	return false
}

// Output contains generated text and the optional Mihomo materialization report.
type Output struct {
	Content             string
	SubscriptionHosts   json.RawMessage
	SubscriptionDialers json.RawMessage
}

// Download holds one response payload or the atomically saved generated path.
type Download struct {
	Path   string
	Data   []byte
	Name   string
	MIME   string
	Output Output
}

// RenderConfig generates from a typed resolved configuration snapshot.
func RenderConfig(ctx context.Context, r *repo.Repository, config *model.Config, engine Engine, baseURL string) (Output, error) {
	var out Output
	var err error
	switch engine {
	case Mihomo:
		report := &convert.GenerationReport{}
		out.Content, err = convert.MihomoConfig(ctx, config, convert.MihomoOptions{BaseURL: baseURL, Report: report})
		if err == nil {
			if report.SubscriptionHosts != nil {
				if dns := yamlx.Resolve(yamlx.Get(report.SubscriptionHosts, "dns")); dns != nil && dns.Kind == yaml.MappingNode {
					if applied := yamlx.Resolve(yamlx.Get(dns, "applied")); applied != nil && applied.Kind == yaml.ScalarNode && applied.ShortTag() == "!!bool" && applied.Value == "true" {
						profileID := ""
						if config != nil {
							profileID = config.ProfileID
						}
						if outdated, readErr := outdatedAgents(ctx, r, profileID); readErr == nil {
							if raw, mErr := json.Marshal(outdated); mErr == nil {
								if node, yErr := yamlx.FromJSON(raw); yErr == nil {
									yamlx.Set(dns, "outdated_agents", node)
								}
							}
						}
					}
				}
				if raw, yErr := yamlx.ToJSON(report.SubscriptionHosts); yErr == nil {
					out.SubscriptionHosts = raw
				}
			}
			if report.SubscriptionDialers != nil {
				if raw, yErr := yamlx.ToJSON(report.SubscriptionDialers); yErr == nil {
					out.SubscriptionDialers = raw
				}
			}
		}
	case Surge:
		out.Content, err = convert.SurgeConfig(ctx, config, baseURL)
	case Loon:
		out.Content, err = convert.LoonConfig(ctx, config, baseURL)
	case MosDNS:
		out.Content, err = mosdns.Generate(config, baseURL)
	case OxiDNS:
		profile, e := oxidns.NewTypedProfile(config)
		if e != nil {
			return out, e
		}
		profile.Repo = r
		out.Content, err = oxidns.Generate(profile, baseURL)
	default:
		err = errors.New("unsupported configuration engine")
	}
	return out, err
}

// Build owns manual download materialization. After Detach every call capable
// of persisting data is preceded by the Runtime cancellation check.
func Build(ctx context.Context, repository *repo.Repository, id string, engine Engine, baseURL string) (Download, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return Download{}, err
	}
	faultprobe.Point(ctx, faultprobe.GenerateBuild)
	if repository == nil {
		return Download{}, errors.New("configgen: repository is nil")
	}
	profileID, err := repository.ResolveProfileID(ctx, id)
	if err != nil {
		return Download{}, err
	}
	config, err := repository.Config(ctx, profileID, nil)
	if err != nil {
		return Download{}, err
	}
	return build(ctx, repository, config, engine, baseURL)
}

func build(ctx context.Context, r *repo.Repository, config *model.Config, engine Engine, baseURL string) (Download, error) {
	if err := ctx.Err(); err != nil {
		return Download{}, err
	}
	out, err := RenderConfig(ctx, r, config, engine, baseURL)
	if err != nil {
		return Download{}, err
	}
	return materialize(ctx, r, config, out, engine, baseURL)
}

func materialize(ctx context.Context, r *repo.Repository, config *model.Config, out Output, engine Engine, baseURL string) (Download, error) {
	if err := ctx.Err(); err != nil {
		return Download{}, err
	}
	result := Download{Output: out}
	switch engine {
	case Mihomo, Surge, Loon:
		name := map[Engine]string{Mihomo: "config.yaml", Surge: "config.conf", Loon: "loon.lcf"}[engine]
		result.Name = map[Engine]string{Mihomo: "mihomo.yaml", Surge: "surge.conf", Loon: "loon.lcf"}[engine]
		result.MIME = map[Engine]string{Mihomo: "application/octet-stream", Surge: "text/plain; charset=utf-8", Loon: "application/octet-stream"}[engine]
		if r == nil {
			return Download{}, errors.New("configgen: repository is nil")
		}
		if err := ctx.Err(); err != nil {
			return Download{}, err
		}
		var err error
		result.Path, err = r.WriteGenerated(ctx, config.ProfileID, name, []byte(out.Content))
		if err != nil {
			return Download{}, err
		}
	case MosDNS:
		downloads, err := mosdns.Downloads(config, baseURL)
		if err != nil {
			return Download{}, err
		}
		files, err := mosdns.CustomFiles(config)
		if err != nil {
			return Download{}, err
		}
		if err = ctx.Err(); err != nil {
			return Download{}, err
		}
		result.Data, err = mosdns.BuildZIP(ctx, config, out.Content, downloads, files, mosdns.ArchiveOptions{BaseURL: baseURL})
		if err != nil {
			return Download{}, err
		}
		result.Name = "mosdns-config.zip"
		result.MIME = "application/zip"
	case OxiDNS:
		profile, err := oxidns.NewTypedProfile(config)
		if err != nil {
			return Download{}, err
		}
		rows, err := oxidns.RulesetDownloads(profile, baseURL)
		if err != nil {
			return Download{}, err
		}
		downloads := make([]mosdns.Download, 0, len(rows))
		for _, row := range rows {
			downloads = append(downloads, mosdns.Download{Name: row.Name, URL: row.URL, LocalPath: row.LocalPath, Behavior: row.Behavior})
		}
		files, err := oxidns.CustomFiles(profile)
		if err != nil {
			return Download{}, err
		}
		if err := ctx.Err(); err != nil {
			return Download{}, err
		}
		result.Data, err = oxidns.BuildTypedZIP(ctx, config, out.Content, downloads, files, baseURL)
		if err != nil {
			return Download{}, err
		}
		result.Name = "oxidns-config.zip"
		result.MIME = "application/zip"
	default:
		return Download{}, fmt.Errorf("unsupported configuration engine %q", engine)
	}
	return result, nil
}

func coreAgentOf(agent *model.Agent) subs.CoreAgent {
	core := subs.CoreAgent{
		ID:               agent.ID,
		Name:             agent.Name,
		ProfileID:        agent.ProfileID,
		DiscoveryEnabled: agent.DomainDiscoveryEnabled,
	}
	if agent.ServiceType != "" {
		serviceType := agent.ServiceType
		core.ServiceType = &serviceType
	}
	return core
}

func outdatedAgents(ctx context.Context, r *repo.Repository, id string) ([]subs.CoreReport, error) {
	if r == nil {
		return nil, errors.New("configgen: repository is nil")
	}
	var list []subs.CoreAgent
	if err := r.View(ctx, func(doc *model.Document) error {
		list = make([]subs.CoreAgent, 0, len(doc.System.Agents))
		for i := range doc.System.Agents {
			list = append(list, coreAgentOf(&doc.System.Agents[i]))
		}
		return nil
	}); err != nil {
		return nil, err
	}
	defaultID, err := r.DefaultProfileID(ctx)
	if err != nil {
		return nil, err
	}
	store := trafficstore.NewNative(r.DataDir())
	names := make(map[string]string, len(list))
	for _, item := range list {
		names[item.ID] = item.Name
	}
	at := lifecycle.ClockFrom(ctx).Now()
	return subs.OldCoreAgents(list, id, defaultID, func(agentID string) subs.CoreStatus {
		status, _ := subs.DecodeCoreStatus(store.StatusJSON(agentID))
		if version := strings.TrimSpace(status.MihomoVersion); version != "" {
			subs.WarnUnrecognizedCoreVersion(agentID, names[agentID], version, at)
		}
		return status
	}), nil
}
