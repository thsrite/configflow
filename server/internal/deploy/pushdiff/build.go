package pushdiff

import (
	"fmt"
	"sort"

	"configflow/internal/deploy"
)

func canonical(f deploy.File) string {
	if f.Role == deploy.RoleConfig {
		return "config.yaml"
	}
	return f.Path
}
func entries(m deploy.Manifest) map[string]deploy.File {
	out := map[string]deploy.File{}
	for _, f := range m.Files {
		out[canonical(f)] = f
	}
	return out
}
func publicFile(f deploy.File, status string) File {
	out := File{Path: f.Path, Status: status}
	if f.Role != "" {
		out.Role = ptr(f.Role)
	}
	if f.Name != "" {
		out.Name = ptr(markedString(f.Name, false, nil))
	}
	return out
}
func sizeSummary(old, new []byte) *Summary {
	s := &Summary{Kind: "size"}
	if old != nil {
		s.Before = ptr(len(old))
	}
	if new != nil {
		s.After = ptr(len(new))
	}
	return s
}

// Build compares the applied view and optional private receipt to a candidate.
// lists, when non-nil, receives immutable compressed node/rule entries by path.
// Reading the receipt is the caller's job; nil explicitly means unavailable.
func Build(agent AppliedView, candidate *deploy.Bundle, receipt *deploy.Receipt, lists map[string]*FileEntries) (Diff, error) {
	return build(agent, candidate, receipt, lists, MaxDetailBytes)
}

func build(agent AppliedView, candidate *deploy.Bundle, receipt *deploy.Receipt, lists map[string]*FileEntries, detailLimit int) (Diff, error) {
	result := Diff{Status: "changed"}
	if candidate == nil {
		return result, fmt.Errorf("compare push bundle: missing candidate")
	}
	baseline := agent.Files
	if baseline == nil && receipt != nil {
		var err error
		baseline, err = deploy.Digests(deploy.ComparisonManifest(receipt.Manifest, receipt.ComparisonConfig))
		if err != nil {
			return result, fmt.Errorf("baseline digests: %w", err)
		}
	}
	if baseline == nil {
		result.Reason = "baseline_files_missing"
		return result, nil
	}
	result.Content = "full"
	if receipt == nil {
		result.Content = "digests"
		reason := "bundle_missing"
		if agent.ServiceType == "mosdns" {
			reason = "service_without_bundle"
		}
		result.ContentReason = &reason
	}
	m := candidate.Manifest()
	newEntries := entries(m)
	oldEntries := map[string]deploy.File{}
	if receipt != nil {
		oldEntries = entries(receipt.Manifest)
	}
	digests, err := deploy.Digests(deploy.ComparisonManifest(m, candidate.Comparison()))
	if err != nil {
		return result, fmt.Errorf("candidate digests: %w", err)
	}
	statuses := map[string]string{}
	for p, digest := range baseline {
		status := "removed"
		if newDigest, ok := digests[p]; ok {
			status = "modified"
			if newDigest == digest {
				status = "unchanged"
			}
		}
		statuses[p] = status
	}
	for p := range digests {
		if _, ok := baseline[p]; !ok {
			statuses[p] = "added"
		}
	}
	entryFor := func(p string) deploy.File {
		if f, ok := newEntries[p]; ok {
			return f
		}
		if f, ok := oldEntries[p]; ok {
			return f
		}
		f := deploy.File{Path: p}
		if p == "config.yaml" {
			f.Role = deploy.RoleConfig
		}
		return f
	}
	roleOrder := map[deploy.Role]int{deploy.RoleConfig: 0, deploy.RoleProvider: 1, deploy.RoleRule: 2, deploy.RoleCustom: 3}
	statusOrder := map[string]int{"modified": 0, "added": 1, "removed": 2, "unchanged": 3}
	less := func(a, b string) bool {
		x, y := entryFor(a), entryFor(b)
		rx, ok := roleOrder[x.Role]
		if !ok {
			rx = 4
		}
		ry, ok := roleOrder[y.Role]
		if !ok {
			ry = 4
		}
		if rx != ry {
			return rx < ry
		}
		if statusOrder[statuses[a]] != statusOrder[statuses[b]] {
			return statusOrder[statuses[a]] < statusOrder[statuses[b]]
		}
		nx, ny := x.Name, y.Name
		if nx == "" {
			nx = x.Path
		}
		if ny == "" {
			ny = y.Path
		}
		if nx == ny {
			return a < b
		}
		return nx < ny
	}
	changed, unchanged := []string{}, []string{}
	for p, status := range statuses {
		switch status {
		case "added":
			result.Counts.Added++
		case "removed":
			result.Counts.Removed++
		case "modified":
			result.Counts.Modified++
		case "unchanged":
			result.Counts.Unchanged++
		}
		if status == "unchanged" {
			unchanged = append(unchanged, p)
		} else {
			changed = append(changed, p)
		}
	}
	sort.Slice(changed, func(i, j int) bool { return less(changed[i], changed[j]) })
	sort.Slice(unchanged, func(i, j int) bool { return less(unchanged[i], unchanged[j]) })
	listed := changed[:min(MaxListedFiles, len(changed))]
	result.FilesOmitted = len(changed) - len(listed)
	result.UnchangedOmitted = max(0, len(unchanged)-MaxListedFiles)
	newPaths, oldPaths := []string{}, []string{}
	for _, p := range listed {
		if f, ok := newEntries[p]; ok {
			newPaths = append(newPaths, f.Path)
		}
		if f, ok := oldEntries[p]; ok {
			oldPaths = append(oldPaths, f.Path)
		}
	}
	// Files() without paths reads everything. Avoid it when the requested set is empty.
	newBytes, oldBytes := map[string][]byte{}, map[string][]byte{}
	if len(newPaths) > 0 {
		newBytes, err = candidate.Files(newPaths...)
		if err != nil {
			return result, fmt.Errorf("read candidate diff files: %w", err)
		}
	}
	if len(oldPaths) > 0 && receipt != nil && receipt.Bundle != nil {
		oldBytes, err = receipt.Bundle.Files(oldPaths...)
		if err != nil {
			return result, fmt.Errorf("read baseline diff files: %w", err)
		}
	}
	var budget *detailBudget
	var keep *keeper
	if receipt != nil {
		budget = &detailBudget{left: detailLimit}
		if lists != nil {
			keep = &keeper{left: MaxKeptBytes}
		}
	}
	detailed := append([]string(nil), listed...)
	sort.SliceStable(detailed, func(i, j int) bool { return statuses[detailed[i]] == "modified" && statuses[detailed[j]] != "modified" })
	public := map[string]File{}
	for _, p := range detailed {
		status := statuses[p]
		f := entryFor(p)
		old, new := oldBytes[oldEntries[p].Path], newBytes[newEntries[p].Path]
		knownOld := status == "added" || old != nil
		item := publicFile(f, status)
		if knownOld || status != "modified" {
			switch f.Role {
			case deploy.RoleConfig:
				if old != nil && new != nil {
					config := Config(old, new, oldEntries[p].Path, newEntries[p].Path)
					result.Config = &config
					changes := 0
					if config.Total != nil {
						changes = *config.Total
					}
					reformatted := false
					if config.Reformatted != nil {
						reformatted = *config.Reformatted
					}
					item.Summary = &Summary{Kind: "config", Before: ptr(len(old)), After: ptr(len(new)), Changes: &changes, Reformatted: &reformatted}
				} else {
					item.Summary = sizeSummary(old, new)
				}
			case deploy.RoleProvider:
				if old != nil || new != nil {
					item.Summary, item.Details = nodeChange(old, new, budget, keep)
				}
			case deploy.RoleRule:
				if old != nil || new != nil {
					item.Summary, item.Details = ruleChange(old, new, budget, keep)
				}
			default:
				if status != "removed" || old != nil {
					item.Summary = sizeSummary(old, new)
				}
			}
		}
		if keep != nil && keep.last != nil {
			lists[f.Path] = keep.last
			keep.last = nil
		}
		public[p] = item
	}
	result.Files = []File{}
	for _, p := range listed {
		result.Files = append(result.Files, public[p])
	}
	result.Unchanged = []File{}
	for _, p := range unchanged[:min(MaxListedFiles, len(unchanged))] {
		result.Unchanged = append(result.Unchanged, publicFile(newEntries[p], "unchanged"))
	}
	if result.Config == nil && statuses["config.yaml"] == "modified" {
		reason := "bundle_missing"
		if result.ContentReason != nil {
			reason = *result.ContentReason
		}
		result.Config = &ConfigDiff{Format: "yaml", Status: "unavailable", Reason: reason, Items: []Row{}}
	}
	return result, nil
}
