package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"

	"configflow/internal/convert"
	"configflow/internal/deploy"
	"configflow/internal/model"
	"configflow/internal/mosdns"
	"configflow/internal/oxidns"
	"configflow/internal/provider"
	"configflow/internal/repo"
	"configflow/internal/yamlx"
)

func deployTarget(agent model.Agent) (deploy.Target, error) {
	return deploy.NewTypedTarget(&agent)
}

func deploySource(config *model.Config) (*deploy.Source, error) {
	raw, err := model.Marshal(config)
	if err != nil {
		return nil, err
	}
	return deploy.SourceFromJSON(raw, nil)
}

// deploymentBundle compares the public Build API on a captured production
// snapshot. Final-device adaptation requires A14 and the L2 guest tests.
func deploymentBundle(ctx context.Context, repository *repo.Repository, config *model.Config, j job, opts convert.MihomoOptions) (string, error) {
	system, err := repository.System(ctx)
	if err != nil {
		return "", err
	}
	agent, err := findAgent(system.Agents, j.AgentID)
	if err != nil {
		return "", err
	}
	target, err := deployTarget(agent)
	if err != nil {
		return "", err
	}
	source, err := deploySource(config)
	if err != nil {
		return "", err
	}
	in := deploy.BuildInput{Target: target, Source: source, BaseURL: baseURL, DeploymentID: "proddiff-bundle", LocalDNSWatchdog: true}
	switch agent.ServiceType {
	case "mihomo":
		main, err := convert.MihomoDocument(ctx, config, opts)
		if err != nil {
			return "", err
		}
		data, err := yamlx.Marshal(main)
		if err != nil {
			return "", err
		}
		in.ConfigContent = string(data)
		prepared, err := provider.PrepareBundle(ctx, config, main, provider.DeliveryOptions{RenderAll: true, SubscriptionFetch: offlineFetch})
		if err != nil {
			return "", err
		}
		for _, p := range prepared {
			if p.LocalPath == "" {
				return "", errors.New("provider has no bundled path")
			}
			in.ProviderDownloads = append(in.ProviderDownloads, deploy.ProviderFile{Name: p.Name, URL: p.URL, LocalPath: p.LocalPath, Content: []byte(p.Content)})
		}
	case "mosdns":
		in.ConfigContent, err = mosdns.Generate(config, baseURL)
		if err != nil {
			return "", err
		}
		downloads, err := mosdns.Downloads(config, baseURL)
		if err != nil {
			return "", err
		}
		for _, d := range downloads {
			in.RulesetDownloads = append(in.RulesetDownloads, convert.ResourceDownload{Name: d.Name, URL: d.URL, LocalPath: d.LocalPath, Behavior: d.Behavior})
		}
		files, err := mosdns.CustomFiles(config)
		if err != nil {
			return "", err
		}
		for _, f := range files {
			in.CustomFiles = append(in.CustomFiles, deploy.CustomFile{Path: f.Path, Content: []byte(f.Content)})
		}
	case "oxidns":
		profile, err := oxidns.NewTypedProfile(config)
		if err != nil {
			return "", err
		}
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
			in.RulesetDownloads = append(in.RulesetDownloads, convert.ResourceDownload{Name: d.Name, URL: d.URL, LocalPath: d.LocalPath, Behavior: behavior})
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
