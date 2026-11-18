package rulesync

import (
	"configflow/internal/deploy"
	"configflow/internal/oxidns"
	"configflow/internal/yamlx"
	"context"
	"strings"
)

// PrepareCandidate generates only opted-in rule artifacts; all other delivered
// files and the main document's non-rule fields come from the verified device.
func PrepareCandidate(ctx context.Context, target deploy.Target, source *deploy.Source, baseline Baseline, settings Settings, deploymentID string, forceRefresh bool) (bundle *deploy.Bundle, err error) {
	err = deploy.RunDetached(ctx, "rule-sync-prepare-"+target.ID, func(ctx context.Context) error {
		previous, e := ReadBundleFiles(baseline.Bundle)
		if e != nil {
			return e
		}
		receipt := baseline.Receipt
		if receipt == nil {
			return refuse(msgSnapshotVerify)
		}
		content, downloads, e := renderCandidate(ctx, target, source, receipt.BaseURL)
		if e != nil {
			return e
		}
		reusable := map[string]bool{}
		oldSource, e := deploy.SourceFromJSON(receipt.Source, nil)
		if e != nil {
			return e
		}
		if settings.Library && !forceRefresh {
			oldEntries, newEntries, e := candidateEntries(target.ServiceType, previous[baseline.Bundle.Manifest().ConfigPath].Data, []byte(content), oldSource, source, receipt.BaseURL)
			if e == nil {
				reusable = UnchangedRulePaths(oldEntries, newEntries, oldSource, source, receipt.BaseURL)
			}
		}
		forced := map[string]bool{}
		// Python forces the generated OxiDNS downloads; a Mihomo candidate has none
		// (its materializer reads the final rule-providers graph).
		if forceRefresh && target.ServiceType == "oxidns" {
			entries, e := oxiEntries(source, receipt.BaseURL)
			if e != nil {
				return e
			}
			for _, d := range entries {
				path, e := deploy.RelativePath(d.LocalPath)
				if e != nil {
					return e
				}
				forced[path] = true
			}
		}
		pins := []deploy.PinnedFile{}
		for path, file := range previous {
			if file.Role != deploy.RoleConfig && !forced[path] && (file.Role != deploy.RoleRule || !settings.Library || reusable[path] || target.ServiceType == "oxidns" && !strings.HasPrefix(path, "rules/rule_")) {
				pins = append(pins, deploy.PinnedFile{Path: path, Content: file.Data, Role: file.Role})
			}
		}
		localWatchdog := true
		if target.ServiceType == "oxidns" {
			old, e := parseMapping(previous[baseline.Bundle.Manifest().ConfigPath].Data)
			if e != nil {
				return e
			}
			with, e := oxidns.EnsureLocalDNSWatchdog(old)
			if e != nil {
				return e
			}
			localWatchdog = yamlx.Equal(with, old)
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		candidate, e := deploy.Build(ctx, deploy.BuildInput{Target: target, Source: source, ConfigContent: content, RulesetDownloads: downloads, PinnedFiles: pins, BaseURL: receipt.BaseURL, DeploymentID: deploymentID, LocalDNSWatchdog: localWatchdog})
		if e != nil {
			return e
		}
		candidate, e = deploy.Retarget(candidate, baseline.Bundle.Manifest().ConfigPath)
		if e != nil {
			return e
		}
		var comparison *deploy.File
		for _, file := range candidate.Manifest().Files {
			if file.Role == deploy.RoleConfig {
				copy := file
				comparison = &copy
				break
			}
		}
		candidate, e = IsolateRuleBundle(baseline.Bundle, candidate, target.ServiceType)
		if e != nil {
			return e
		}
		manifest := candidate.Manifest()
		manifest.ReloadOnly = true
		files, e := candidate.Contents()
		if e != nil {
			return e
		}
		bundle, e = deploy.Pack(manifest, files, candidate.ConfigVersion(), comparison)
		return e
	})
	return
}
func candidateEntries(service string, oldMain, newMain []byte, base, current *deploy.Source, baseURL string) ([]RuleEntry, []RuleEntry, error) {
	if service == "mihomo" {
		old, e := yamlx.Parse(oldMain)
		if e != nil {
			return nil, nil, e
		}
		next, e := yamlx.Parse(newMain)
		if e != nil {
			return nil, nil, e
		}
		return MihomoRuleEntries(old), MihomoRuleEntries(next), nil
	}
	old, e := oxiEntries(base, baseURL)
	if e != nil {
		return nil, nil, e
	}
	next, e := oxiEntries(current, baseURL)
	return old, next, e
}
func oxiEntries(source *deploy.Source, baseURL string) ([]RuleEntry, error) {
	raw, e := source.Raw()
	if e != nil {
		return nil, e
	}
	p, e := oxidns.ProfileFromJSON(raw)
	if e != nil {
		return nil, e
	}
	entries, e := oxidns.RulesetDownloads(p, baseURL)
	if e != nil {
		return nil, e
	}
	out := make([]RuleEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, RuleEntry{URL: entry.URL, LocalPath: entry.LocalPath})
	}
	return out, nil
}
