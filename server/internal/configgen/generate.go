// Package configgen materializes manual and subscription configurations through
// the shared converters. It owns detached file/archive generation; deployment
// publication and frozen artifacts remain with the Agent deployment owner.
package configgen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"configflow/internal/faultprobe"
	"configflow/internal/agents/trafficstore"
	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/mosdns"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
	"configflow/internal/subs"
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
	SubscriptionHosts   *jsonx.Object
	SubscriptionDialers *jsonx.Object
}

// Download holds one response payload or the atomically saved generated path.
type Download struct {
	Path   string
	Data   []byte
	Name   string
	MIME   string
	Output Output
}

// Render generates from the caller's captured configuration snapshot.
func Render(ctx context.Context, config *jsonx.Object, engine Engine, baseURL string) (Output, error) {
	var out Output
	var err error
	switch engine {
	case Mihomo:
		report := jsonx.NewObject()
		out.Content, err = convert.GenerateMihomoConfig(ctx, config, convert.GenerateOptions{BaseURL: baseURL, Report: report})
		if err == nil {
			out.SubscriptionHosts, _ = jsonx.AsObject(report.Value("subscription_hosts"))
			out.SubscriptionDialers, _ = jsonx.AsObject(report.Value("subscription_dialers"))
			if dns, ok := jsonx.AsObject(out.SubscriptionHosts.Value("dns")); ok && dns.GetBool("applied") {
				if outdated, readErr := outdatedAgents(ctx, config.GetString("profile_id")); readErr == nil {
					dns.Set("outdated_agents", outdated)
				}
			}
		}
	case Surge:
		out.Content, err = convert.GenerateSurgeConfig(ctx, config, baseURL)
	case Loon:
		out.Content, err = convert.GenerateLoonConfig(ctx, config, baseURL)
	case MosDNS:
		out.Content, err = mosdns.GenerateMosDNSConfig(config, baseURL)
	case OxiDNS:
		out.Content, err = oxidns.Generate(oxidns.NewProfile(config), baseURL)
	default:
		err = errors.New("unsupported configuration engine")
	}
	return out, err
}

// Build owns manual download materialization. After Detach every call capable
// of persisting data is preceded by the Runtime cancellation check.
func Build(ctx context.Context, id string, engine Engine, baseURL string) (Download, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return Download{}, err
	}
	faultprobe.Point(ctx, faultprobe.GenerateBuild)
	config, err := repo.GetConfig(ctx, id)
	if err != nil {
		return Download{}, err
	}
	if err := ctx.Err(); err != nil {
		return Download{}, err
	}
	out, err := Render(ctx, config, engine, baseURL)
	if err != nil {
		return Download{}, err
	}
	result := Download{Output: out}
	switch engine {
	case Mihomo, Surge, Loon:
		name := map[Engine]string{Mihomo: "config.yaml", Surge: "config.conf", Loon: "loon.lcf"}[engine]
		result.Name = map[Engine]string{Mihomo: "mihomo.yaml", Surge: "surge.conf", Loon: "loon.lcf"}[engine]
		result.MIME = map[Engine]string{Mihomo: "application/octet-stream", Surge: "text/plain; charset=utf-8", Loon: "application/octet-stream"}[engine]
		r, err := repo.GetRepository(ctx)
		if err != nil {
			return Download{}, err
		}
		if err := ctx.Err(); err != nil {
			return Download{}, err
		}
		result.Path, err = r.WriteGenerated(ctx, config.GetString("profile_id"), name, []byte(out.Content))
		if err != nil {
			return Download{}, err
		}
	case MosDNS:
		rows, err := mosdns.GetMosDNSRulesetDownloads(config, baseURL)
		if err != nil {
			return Download{}, err
		}
		downloads, err := mosdns.DecodeList[mosdns.Download](rows)
		if err != nil {
			return Download{}, err
		}
		rows, err = mosdns.GetMosDNSCustomFiles(config)
		if err != nil {
			return Download{}, err
		}
		files, err := mosdns.DecodeList[mosdns.File](rows)
		if err != nil {
			return Download{}, err
		}
		if err := ctx.Err(); err != nil {
			return Download{}, err
		}
		result.Data, err = mosdns.BuildMosDNSZip(ctx, config, out.Content, downloads, files, mosdns.ArchiveOptions{BaseURL: baseURL})
		if err != nil {
			return Download{}, err
		}
		result.Name = "mosdns-config.zip"
		result.MIME = "application/zip"
	case OxiDNS:
		profile := oxidns.NewProfile(config)
		rows, err := oxidns.RulesetDownloads(profile, baseURL)
		if err != nil {
			return Download{}, err
		}
		raw, err := json.Marshal(rows)
		if err != nil {
			return Download{}, err
		}
		var downloads []mosdns.Download
		if err = json.Unmarshal(raw, &downloads); err != nil {
			return Download{}, err
		}
		files, err := oxidns.CustomFiles(profile)
		if err != nil {
			return Download{}, err
		}
		if err := ctx.Err(); err != nil {
			return Download{}, err
		}
		result.Data, err = oxidns.BuildZIP(ctx, config, out.Content, downloads, files, baseURL)
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

func outdatedAgents(ctx context.Context, id string) ([]any, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	system, err := r.GetSystem(ctx)
	if err != nil {
		return nil, err
	}
	defaultID, err := r.DefaultProfileID(ctx)
	if err != nil {
		return nil, err
	}
	list := []*jsonx.Object{}
	for _, value := range system.GetList("agents") {
		if agent, ok := jsonx.AsObject(value); ok {
			list = append(list, agent)
		}
	}
	store, err := trafficstore.Get(ctx)
	if err != nil {
		return nil, err
	}
	return subs.OutdatedAgents(list, id, defaultID, store.StatusOf), nil
}
