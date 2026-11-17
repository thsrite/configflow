package main

import (
	"configflow/internal/convert"
	"configflow/internal/deploy"
	"configflow/internal/jsonx"
	"configflow/internal/mosdns"
	"configflow/internal/oxidns"
	"configflow/internal/provider"
	"configflow/internal/repo"
	"configflow/internal/yamlx"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
)

// deploymentBundle compares the public Build API on a captured production
// snapshot. Final-device adaptation requires A14 and the L2 guest tests.
func deploymentBundle(ctx context.Context, config *jsonx.Object, j job, opts convert.GenerateOptions) (string, error) {
	system, err := repo.GetSystemConfig(ctx)
	if err != nil {
		return "", err
	}
	agent, err := objectByID(system.GetList("agents"), j.AgentID)
	if err != nil {
		return "", err
	}
	in := deploy.BuildInput{Target: deploy.NewTarget(agent), Source: deploy.NewSource(config, nil), BaseURL: baseURL, DeploymentID: "proddiff-bundle", LocalDNSWatchdog: true}
	service := agent.GetString("service_type")
	switch service {
	case "mihomo":
		main, err := convert.GenerateMihomoDocument(ctx, config, opts)
		if err != nil {
			return "", err
		}
		content, err := yamlx.SafeDumpUnicode(main)
		if err != nil {
			return "", err
		}
		in.ConfigContent = content
		prepared, err := provider.PrepareProviderBundle(ctx, config, main, provider.BundleOptions{RenderAll: true, SubscriptionFetch: offlineFetch})
		if err != nil {
			return "", err
		}
		for _, p := range prepared {
			local, ok := p.LocalPath.(string)
			if !ok {
				return "", errors.New("provider has no bundled path")
			}
			in.ProviderDownloads = append(in.ProviderDownloads, deploy.ProviderFile{Name: p.Name, URL: p.URL, LocalPath: local, Content: []byte(p.Content)})
		}
	case "mosdns":
		in.ConfigContent, err = mosdns.GenerateMosDNSConfig(config, baseURL)
		if err != nil {
			return "", err
		}
		downloads, err := mosdns.GetMosDNSRulesetDownloads(config, baseURL)
		if err != nil {
			return "", err
		}
		if err = decodeBundleDownloads(downloads, &in); err != nil {
			return "", err
		}
		files, err := mosdns.GetMosDNSCustomFiles(config)
		if err != nil {
			return "", err
		}
		raw, err := json.Marshal(files)
		if err != nil {
			return "", err
		}
		var custom []struct{ Path, Content string }
		if err = json.Unmarshal(raw, &custom); err != nil {
			return "", err
		}
		for _, f := range custom {
			in.CustomFiles = append(in.CustomFiles, deploy.CustomFile{Path: f.Path, Content: []byte(f.Content)})
		}
	case "oxidns":
		profile := oxidns.NewProfile(config)
		in.ConfigContent, err = oxidns.Generate(profile, baseURL)
		if err != nil {
			return "", err
		}
		downloads, err := oxidns.RulesetDownloads(profile, baseURL)
		if err != nil {
			return "", err
		}
		for _, d := range downloads {
			behavior := ""
			if d.Behavior != nil {
				behavior = d.Behavior.Value
			}
			in.RulesetDownloads = append(in.RulesetDownloads, convert.Download{Name: d.Name, URL: d.URL, LocalPath: d.LocalPath, Behavior: behavior})
		}
		files, err := oxidns.CustomFiles(profile)
		if err != nil {
			return "", err
		}
		for _, f := range files {
			in.CustomFiles = append(in.CustomFiles, deploy.CustomFile{Path: f.Path, Content: f.Content, Native: f.Native})
		}
	default:
		return "", errors.New("Unsupported transactional deployment service")
	}
	bundle, err := deploy.Build(ctx, in)
	if err != nil {
		return "", err
	}
	return bundleView(bundle)
}
func decodeBundleDownloads(downloads []any, in *deploy.BuildInput) error {
	raw, err := json.Marshal(downloads)
	if err != nil {
		return err
	}
	var list []struct {
		Name, URL, Behavior, Format string
		LocalPath                   string `json:"local_path"`
	}
	if err = json.Unmarshal(raw, &list); err != nil {
		return err
	}
	for _, d := range list {
		in.RulesetDownloads = append(in.RulesetDownloads, convert.Download{Name: d.Name, URL: d.URL, LocalPath: d.LocalPath, Behavior: d.Behavior, Format: d.Format})
	}
	return nil
}
func bundleView(b *deploy.Bundle) (string, error) {
	m := b.Manifest()
	files, err := b.Files()
	if err != nil {
		return "", err
	}
	type entry struct {
		Path     string          `json:"path"`
		Role     deploy.Role     `json:"role"`
		Name     string          `json:"name,omitempty"`
		Size     *int64          `json:"size,omitempty"`
		SHA256   string          `json:"sha256,omitempty"`
		Content  string          `json:"content,omitempty"`
		Document json.RawMessage `json:"document,omitempty"`
	}
	out := struct {
		ProtocolVersion int     `json:"protocol_version"`
		ServiceType     string  `json:"service_type"`
		ConfigPath      string  `json:"config_path"`
		Files           []entry `json:"files"`
	}{m.ProtocolVersion, m.ServiceType, m.ConfigPath, []entry{}}
	for _, f := range m.Files {
		e := entry{Path: f.Path, Role: f.Role, Name: f.Name}
		data, exists := files[f.Path]
		if !exists {
			return "", errors.New("bundle manifest file is absent")
		}
		if err := verifyBundleFile(f, data); err != nil {
			return "", err
		}
		if f.Role == deploy.RoleConfig || f.Role == deploy.RoleProvider {
			n, err := yamlx.Parse(files[f.Path])
			if err != nil {
				return "", err
			}
			e.Document, err = yamlx.ToJSON(n)
			if err != nil {
				return "", err
			}
		} else {
			size := f.Size
			e.Size = &size
			e.SHA256 = f.SHA256
			e.Content = base64.StdEncoding.EncodeToString(files[f.Path])
		}
		out.Files = append(out.Files, e)
	}
	return encode(out, nil)
}

// verifyBundleFile checks real protocol metadata against each side's own bytes
// before comparing generated main/provider YAML structurally across languages.
func verifyBundleFile(file deploy.File, data []byte) error {
	sum := sha256.Sum256(data)
	if file.Size != int64(len(data)) || file.SHA256 != hex.EncodeToString(sum[:]) {
		return errors.New("bundle file integrity mismatch")
	}
	return nil
}
