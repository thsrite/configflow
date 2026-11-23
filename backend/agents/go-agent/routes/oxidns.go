package routes

import (
	"context"
	"fmt"
	"gopkg.in/yaml.v3"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func prepareOxiDNSRuntimeDirectories(cfg *Config) error {
	data, err := os.ReadFile(cfg.ConfigPath)
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return err
	}
	plugins, _ := doc["plugins"].([]any)
	uid, gid := -1, -1
	if info, err := os.Stat(cfg.ConfigPath); err == nil {
		uid, gid = fileOwnership(info)
	}
	for _, value := range plugins {
		plugin, _ := value.(map[string]any)
		args, _ := plugin["args"].(map[string]any)
		var key string
		switch plugin["type"] {
		case "dynamic_domain_set", "query_recorder":
			key = "path"
		case "cache":
			key = "dump_file"
		}
		path, _ := args[key].(string)
		if path == "" {
			continue
		}
		// External/runtime-expanded locations are owned by the installed core.
		// Never create or rewrite them as part of an Agent configuration revision.
		if filepath.IsAbs(path) || strings.Contains(path, "${") {
			continue
		}
		rel := filepath.ToSlash(filepath.Clean(path))
		if !safeRelative(rel) {
			return fmt.Errorf("OxiDNS runtime path escapes service directory")
		}
		root := filepath.Dir(cfg.ConfigPath)
		if err := rejectSymlinkPath(root, rel); err != nil {
			return err
		}
		if err := ensureManagedParent(root, rel, uid, gid); err != nil {
			return err
		}
	}
	return nil
}

// The transaction root is the config directory. Reject a systemd core whose
// runtime resolves rule/state paths elsewhere before any configuration changes.
func ValidateOxiDNSServiceDirectory(cfg *Config) error {
	if cfg.ServiceType != "oxidns" || cfg.ServiceManager != "systemd" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, err := runDiagnosticCommand(ctx, exec.CommandContext(ctx, "systemctl", "show", cfg.ServiceUnit, "--property=ExecStart", "--property=WorkingDirectory"), "inspect OxiDNS service working directory", cfg.Token)
	if err != nil {
		return err
	}
	return validateOxiDNSSystemdDirectory(string(data), cfg.ConfigPath)
}

func validateOxiDNSSystemdDirectory(properties, expectedConfig string) error {
	expected := filepath.Dir(expectedConfig)
	working, command, config := "", "", "config.yaml"
	for _, line := range strings.Split(properties, "\n") {
		if strings.HasPrefix(line, "WorkingDirectory=") {
			working = strings.TrimPrefix(line, "WorkingDirectory=")
		}
		if strings.HasPrefix(line, "ExecStart=") {
			command = strings.TrimPrefix(line, "ExecStart=")
		}
	}
	if index := strings.Index(command, "argv[]="); index >= 0 {
		command = strings.SplitN(command[index+7:], ";", 2)[0]
	}
	args := strings.Fields(command)
	for i, arg := range args {
		if (arg == "-d" || arg == "--dir" || arg == "--working-dir") && i+1 < len(args) {
			working = args[i+1]
		}
		for _, prefix := range []string{"-d=", "--dir=", "--working-dir="} {
			if strings.HasPrefix(arg, prefix) {
				working = strings.TrimPrefix(arg, prefix)
			}
		}
		if (arg == "-c" || arg == "--config") && i+1 < len(args) {
			config = args[i+1]
		}
		for _, prefix := range []string{"-c=", "--config="} {
			if strings.HasPrefix(arg, prefix) {
				config = strings.TrimPrefix(arg, prefix)
			}
		}
		// Clap also accepts attached short-option values (e.g. -cother.yaml).
		if strings.HasPrefix(arg, "-c") && !strings.HasPrefix(arg, "--") && len(arg) > 2 {
			config = strings.TrimPrefix(arg[2:], "=")
		}
		if strings.HasPrefix(arg, "-d") && !strings.HasPrefix(arg, "--") && len(arg) > 2 {
			working = strings.TrimPrefix(arg[2:], "=")
		}
	}
	decodePath := func(value string) string {
		value = strings.Trim(value, "\"'")
		if decoded, err := strconv.Unquote("\"" + value + "\""); err == nil {
			return decoded
		}
		return value
	}
	working, config = decodePath(working), decodePath(config)
	if command == "" || working == "" || !filepath.IsAbs(working) || filepath.Clean(working) != filepath.Clean(expected) {
		return fmt.Errorf("OxiDNS service -d/WorkingDirectory must equal the Agent config directory %s; update the core service working directory or select its actual same-directory configuration before installing/pushing", expected)
	}
	// OxiDNS changes cwd to -d before loading -c; without -c it loads config.yaml.
	if !filepath.IsAbs(config) {
		config = filepath.Join(working, config)
	}
	if !filepath.IsAbs(expectedConfig) || filepath.Clean(config) != filepath.Clean(expectedConfig) {
		return fmt.Errorf("OxiDNS service -c/--config must select the Agent config_path %s; update the Agent path or core service before installing/pushing", expectedConfig)
	}
	return nil
}

// OxiDNS state belongs to the running service, not a configuration revision.
// Native checking uses an isolated state directory and never opens the live DB.
func validateOxiDNS(cfg *Config, doc map[string]any, stage, config string, manifest *DeploymentManifest) error {
	for _, f := range manifest.Files {
		if f.Path == "state" || strings.HasPrefix(f.Path, "state/") {
			return fmt.Errorf("OxiDNS runtime state cannot be replaced by a deployment")
		}
	}
	if includes, ok := doc["include"].([]any); ok && len(includes) > 0 {
		return fmt.Errorf("OxiDNS deployment must inline included plugin definitions")
	}
	// The shadow document is only for native validation. Absolute/environment
	// runtime outputs are redirected into the staging directory so checking can
	// never open or change a running service's SQLite/cache/provider state.
	shadowBytes, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	var shadow map[string]any
	if err := yaml.Unmarshal(shadowBytes, &shadow); err != nil {
		return err
	}
	plugins, ok := shadow["plugins"].([]any)
	if !ok {
		return fmt.Errorf("OxiDNS plugins must be a sequence")
	}
	shadowNeeded := false
	runtimeExpressions := []string{}
	listed := map[string]bool{}
	for _, file := range manifest.Files {
		listed[file.Path] = true
	}
	checkReference := func(value any) error {
		raw, ok := value.(string)
		if !ok || raw == "" {
			return fmt.Errorf("invalid OxiDNS resource file reference")
		}
		// These are explicitly provisioned on the target. The native core checks
		// their format/existence; they are never included in the managed manifest.
		if filepath.IsAbs(raw) || strings.Contains(raw, "${") {
			return nil
		}
		rel := filepath.ToSlash(filepath.Clean(raw))
		if !safeRelative(rel) || !listed[rel] {
			return fmt.Errorf("OxiDNS resource files must be included in the deployment: %s", raw)
		}
		_, err := resolveStagedReference(cfg, stage, raw)
		return err
	}
	tags := map[string]bool{}
	for index, value := range plugins {
		p, ok := value.(map[string]any)
		if !ok {
			return pluginLocation(fmt.Errorf("invalid OxiDNS plugin"), stage, manifest, index)
		}
		args, _ := p["args"].(map[string]any)
		kind, _ := p["type"].(string)
		tag, _ := p["tag"].(string)
		if kind == "" || tag == "" || tags[tag] {
			field := "tag"
			if kind == "" {
				field = "type"
			}
			return pluginLocation(fmt.Errorf("OxiDNS plugin type/tag missing or duplicated"), stage, manifest, index, field)
		}
		tags[tag] = true
		if err := validateOxiDNSTimezone(kind, args); err != nil {
			return pluginLocation(err, stage, manifest, index, "args", "timezone")
		}
		type resourceReference struct {
			value any
			path  []any
		}
		references := []resourceReference{}
		for _, key := range []string{"file", "cert", "key"} {
			if file, ok := args[key]; ok && file != "" && file != nil {
				references = append(references, resourceReference{file, []any{"args", key}})
			}
		}
		if values, exists := args["files"]; exists {
			files, ok := values.([]any)
			if !ok {
				return pluginLocation(fmt.Errorf("OxiDNS files must be a sequence"), stage, manifest, index, "args", "files")
			}
			for fileIndex, file := range files {
				references = append(references, resourceReference{file, []any{"args", "files", fileIndex}})
			}
		}
		if persistent, ok := args["persistent"].(map[string]any); ok {
			if files, ok := persistent["files"].([]any); ok {
				for fileIndex, file := range files {
					references = append(references, resourceReference{file, []any{"args", "persistent", "files", fileIndex}})
				}
			}
		}
		if tls, ok := args["tls"].(map[string]any); ok {
			if ca, exists := tls["ca"]; exists && ca != "" && ca != nil {
				references = append(references, resourceReference{ca, []any{"args", "tls", "ca"}})
			}
		}
		for _, reference := range references {
			if err := checkReference(reference.value); err != nil {
				return pluginLocation(err, stage, manifest, index, reference.path...)
			}
		}
		// Keep all generated persistent outputs outside the managed manifest.
		key := ""
		switch kind {
		case "dynamic_domain_set", "query_recorder":
			key = "path"
		case "cache":
			key = "dump_file"
		}
		if key != "" {
			raw, _ := args[key].(string)
			if raw != "" {
				rel := filepath.ToSlash(filepath.Clean(raw))
				external := filepath.IsAbs(raw) || strings.Contains(raw, "${")
				if !external && !safeRelative(rel) {
					return pluginLocation(fmt.Errorf("OxiDNS runtime paths must be relative to the service directory"), stage, manifest, index, "args", key)
				}
				if filepath.IsAbs(raw) {
					if relative, err := filepath.Rel(filepath.Dir(cfg.ConfigPath), raw); err == nil {
						rel = filepath.ToSlash(relative)
					}
				}
				for _, f := range manifest.Files {
					if f.Path == rel {
						return pluginLocation(fmt.Errorf("OxiDNS runtime file collides with managed deployment file"), stage, manifest, index, "args", key)
					}
				}
				if external {
					if strings.Contains(raw, "${") {
						runtimeExpressions = append(runtimeExpressions, raw)
					}
					rel = fmt.Sprintf(".configflow-check-state/%d/runtime", index)
					args[key] = rel
					shadowNeeded = true
				}
				if err := os.MkdirAll(filepath.Join(stage, filepath.Dir(rel)), 0700); err != nil {
					return pluginLocation(err, stage, manifest, index, "args", key)
				}
			}
		}
	}
	if api, ok := shadow["api"].(map[string]any); ok {
		if http, ok := api["http"].(map[string]any); ok {
			if ssl, ok := http["ssl"].(map[string]any); ok {
				for _, key := range []string{"cert", "key", "client_ca"} {
					if value, ok := ssl[key]; ok && value != "" && value != nil {
						if err := checkReference(value); err != nil {
							return err
						}
					}
				}
			}
			if webui, ok := http["webui"].(map[string]any); ok {
				if root, ok := webui["root"].(string); ok && root != "" {
					index, _ := webui["index"].(string)
					if index == "" {
						index = "index.html"
					}
					if err := checkReference(filepath.Join(root, index)); err != nil {
						return err
					}
				}
			}
		}
	}
	if shadowNeeded {
		if len(runtimeExpressions) > 0 {
			// OxiDNS expands the entire parsed YAML tree before deserializing
			// Config, whose unknown top-level fields are ignored. Keep each
			// original expression in inert metadata so the installed core still
			// validates missing variables, defaults and escaping itself, while
			// plugin runtime paths remain isolated. Comments would not be expanded.
			key := "__configflow_check_runtime_expressions"
			for {
				if _, exists := shadow[key]; !exists {
					break
				}
				key += "_"
			}
			shadow[key] = runtimeExpressions
		}
		content, err := yaml.Marshal(shadow)
		if err != nil {
			return err
		}
		shadowFile, err := os.CreateTemp(stage, ".configflow-check-*.yaml")
		if err != nil {
			return err
		}
		defer os.Remove(shadowFile.Name())
		if _, err := shadowFile.Write(content); err != nil {
			shadowFile.Close()
			return err
		}
		if err := shadowFile.Close(); err != nil {
			return err
		}
		config = shadowFile.Name()
	}
	binary := cfg.ServiceBinary
	if binary == "" {
		binary = "oxidns"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "check", "-c", config, "-d", stage)
	command.Dir = stage
	if _, err := runDiagnosticCommand(ctx, command, "OxiDNS native configuration check", append(configurationDiagnosticSecrets(doc), cfg.Token)...); err != nil {
		return nativeConfigurationLocation(err, stage, config, manifest, false)
	}
	return verifyManifestFiles(stage, manifest)
}

// Derive credentials only for the configured local management endpoint. An
// arbitrary health_url must never receive the core's management credentials.
func oxidnsHealthEndpoint(doc map[string]any, override string) (string, string, string, error) {
	api, _ := doc["api"].(map[string]any)
	endpoint, _ := api["http"].(string)
	httpConfig, _ := api["http"].(map[string]any)
	if httpConfig != nil {
		endpoint, _ = httpConfig["listen"].(string)
	}
	if endpoint == "" {
		return override, "", "", nil
	}
	scheme := "http"
	if ssl, ok := httpConfig["ssl"].(map[string]any); ok && len(ssl) > 0 {
		// A loopback URL cannot authenticate a hostname certificate or client-cert
		// endpoint. Keep actual DNS + service probes; never disable TLS validation.
		if override == "" {
			return "", "", "", nil
		}
		scheme = "https"
	}
	expected := scheme + "://" + loopbackAddress(endpoint) + "/api/readyz"
	target := expected
	if override != "" {
		target = override
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.Host == "" {
		return "", "", "", fmt.Errorf("invalid OxiDNS health URL")
	}
	// Only the endpoint defined in the active config may receive Basic auth.
	configured, _ := url.Parse(expected)
	sameHost := parsed.Host == configured.Host
	if !sameHost {
		targetHost, targetPort, _ := net.SplitHostPort(parsed.Host)
		configHost, configPort, _ := net.SplitHostPort(configured.Host)
		sameHost = targetPort == configPort && net.ParseIP(targetHost) != nil && net.ParseIP(configHost) != nil && net.ParseIP(targetHost).Equal(net.ParseIP(configHost))
	}
	username, password := "", ""
	if sameHost && parsed.Scheme == configured.Scheme {
		auth, _ := httpConfig["auth"].(map[string]any)
		if auth["type"] == "basic" {
			username, _ = auth["username"].(string)
			password, _ = auth["password"].(string)
		}
	}
	return target, username, password, nil
}
