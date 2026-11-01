package routes

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var mihomoGeoAssets = []string{"Country.mmdb", "GeoIP.dat", "GeoSite.dat", "geoip.metadb", "geoip.db", "geosite.dat", "geoip.dat", "ASN.mmdb"}

var errServiceTransition = errors.New("service startup or shutdown is in progress")

func validateServiceLifecycle(cfg *Config) error {
	if cfg.ServiceType != "mihomo" && cfg.ServiceType != "mosdns" && cfg.ServiceType != "oxidns" {
		return fmt.Errorf("unsupported service type")
	}
	switch cfg.ServiceManager {
	case "supervisor", "systemd", "openrc":
		if cfg.ServiceUnit == "" {
			return fmt.Errorf("service_unit is required")
		}
	case "command":
		if cfg.StopCommand == "" || cfg.StartCommand == "" || cfg.StatusCommand == "" {
			return fmt.Errorf("explicit stop/start/status commands are required")
		}
	default:
		return fmt.Errorf("configure a service manager before transactional deployment")
	}
	return nil
}
func serviceCommand(cfg *Config, action string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	switch cfg.ServiceManager {
	case "systemd":
		a := action
		if a == "status" {
			a = "is-active"
		}
		cmd = exec.CommandContext(ctx, "systemctl", a, cfg.ServiceUnit)
	case "openrc":
		cmd = exec.CommandContext(ctx, "rc-service", cfg.ServiceUnit, action)
	case "supervisor":
		cmd = exec.CommandContext(ctx, "supervisorctl", "-c", "/etc/supervisor/supervisord.conf", action, cfg.ServiceUnit)
	case "command":
		value := cfg.StatusCommand
		if action == "stop" {
			value = cfg.StopCommand
		}
		if action == "start" {
			value = cfg.StartCommand
		}
		cmd = exec.CommandContext(ctx, "sh", "-c", value)
	default:
		return nil, fmt.Errorf("service manager is not configured")
	}
	cmd.Dir = filepath.Dir(cfg.ConfigPath)
	return runDiagnosticCommand(ctx, cmd, "service "+action, cfg.Token)
}
func serviceRunning(cfg *Config) (bool, error) {
	if e := validateServiceLifecycle(cfg); e != nil {
		return false, e
	}
	out, e := serviceCommand(cfg, "status")
	if cfg.ServiceManager == "supervisor" {
		v := string(out)
		if e == nil && strings.Contains(v, "RUNNING") {
			return true, nil
		}
		if strings.Contains(v, "STARTING") || strings.Contains(v, "BACKOFF") || strings.Contains(v, "STOPPING") {
			return false, errServiceTransition
		}
		for _, state := range []string{"STOPPED", "EXITED", "FATAL"} {
			if strings.Contains(v, state) {
				return false, nil
			}
		}
		return false, diagnosticFailure("cannot determine service status", e, out, cfg.Token)
	}
	if cfg.ServiceManager == "systemd" {
		v := strings.TrimSpace(string(out))
		if v == "activating" || v == "deactivating" || v == "reloading" {
			return false, errServiceTransition
		}
	}
	if e == nil {
		return true, nil
	}
	code := commandExitCode(e)
	if cfg.ServiceManager == "openrc" {
		v := string(out)
		if (code == openrcStatusStopping && strings.Contains(v, "status: stopping")) || (code == openrcStatusStarting && strings.Contains(v, "status: starting")) {
			return false, errServiceTransition
		}
	}
	if (cfg.ServiceManager == "systemd" && code == 3) || (cfg.ServiceManager == "openrc" && (code == openrcStatusStopped || code == openrcStatusCrashed)) || (cfg.ServiceManager == "command" && (code == 1 || code == 3)) {
		return false, nil
	}
	return false, diagnosticFailure("cannot determine service status", e, out, cfg.Token)
}

// OpenRC status exit codes (openrc-run.sh _status). Crashed means OpenRC still
// records the service as started although its daemon has exited.
const (
	openrcStatusStopped  = 3
	openrcStatusStopping = 4
	openrcStatusStarting = 8
	openrcStatusCrashed  = 32
)

func commandExitCode(err error) int {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}
func stopService(cfg *Config) error {
	// Stop the manager even for EXITED/BACKOFF/activating: those states may have
	// a scheduled auto-restart and are not a quiescent backup boundary.
	out, commandErr := serviceCommand(cfg, "stop")
	running, e := serviceRunning(cfg)
	if e != nil {
		if commandErr != nil {
			return diagnosticFailure("service stop failed; cannot confirm service status", commandErr, out, cfg.Token)
		}
		return e
	}
	if running {
		return diagnosticFailure("service remains running after stop", commandErr, out, cfg.Token)
	}
	if commandErr != nil {
		if cfg.ServiceManager == "supervisor" && (strings.Contains(string(out), "NOT_RUNNING") || strings.Contains(string(out), "not running")) {
			return nil
		}
		return commandErr
	}
	return nil
}
func startService(cfg *Config) error {
	if cfg.ServiceType == "oxidns" {
		if err := prepareOxiDNSRuntimeDirectories(cfg); err != nil {
			return err
		}
	}
	// OpenRC treats start of a crashed service as already started and does
	// nothing; zap clears that record so start launches the daemon again.
	if cfg.ServiceManager == "openrc" {
		if _, e := serviceCommand(cfg, "status"); commandExitCode(e) == openrcStatusCrashed {
			if _, e = serviceCommand(cfg, "zap"); e != nil {
				return e
			}
		}
	}
	if _, e := serviceCommand(cfg, "start"); e != nil {
		return withServiceLog(cfg, e)
	}
	return nil
}

const serviceFailureLogLines = 30

// recentServiceLog returns the tail of the core's own error output so a core
// that exits during startup explains itself in the deployment failure.
func recentServiceLog(cfg *Config) (LogSource, string) {
	sources := coreLogSources(cfg)
	for _, id := range []string{"core-err", "core", "core-file"} {
		for _, source := range sources {
			if source.ID != id || source.Kind == "unavailable" {
				continue
			}
			logs, e := ReadLogSource(source, serviceFailureLogLines)
			if e == nil && strings.TrimSpace(logs) != "" {
				return source, strings.TrimSpace(logs)
			}
		}
	}
	return LogSource{}, ""
}

func withServiceLog(cfg *Config, err error) error {
	if err == nil {
		return nil
	}
	source, logs := recentServiceLog(cfg)
	if logs == "" {
		return err
	}
	summary, details := deploymentErrorFields(err)
	reason := ""
	for _, line := range strings.Split(logs, "\n") {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "error") || strings.Contains(lower, "fatal") || strings.Contains(lower, "panic") || strings.Contains(lower, "failed") {
			reason = strings.TrimSpace(line)
		}
	}
	if reason != "" && !strings.Contains(summary, reason) {
		summary += ": " + reason
	}
	where := source.Path
	if where == "" {
		where = source.Unit
	}
	if details != "" {
		details += "\n\n"
	}
	details += fmt.Sprintf("%s 最近 %d 行（%s）：\n%s", source.Label, serviceFailureLogLines, where, logs)
	return diagnosticWithSecrets(&deploymentDiagnosticError{summary: summary, details: details, cause: err}, cfg.Token)
}

func validateStagedDeployment(cfg *Config, s *DeploymentState) (result error) {
	// Add file context to every validation error without doing any work on the
	// successful path or changing the original error chain.
	defer func() {
		if result != nil {
			_, details := deploymentErrorFields(result)
			if !strings.Contains(details, "文件：") {
				root := filepath.Join(deploymentPath(cfg, s.DeploymentID), "stage")
				data := readDiagnosticFile(filepath.Join(root, s.Manifest.ConfigPath))
				line := 0
				if strings.Contains(result.Error(), "yaml:") {
					if match := diagnosticLineNumber.FindStringSubmatch(result.Error()); match != nil {
						line, _ = strconv.Atoi(match[1])
					}
				}
				result = diagnosticLocation(result, s.Manifest.ConfigPath, "", "", line, data, true)
			}
		}
	}()
	if err := ValidateOxiDNSServiceDirectory(cfg); err != nil {
		return err
	}
	root := filepath.Join(deploymentPath(cfg, s.DeploymentID), "stage")
	name := filepath.Join(root, s.Manifest.ConfigPath)
	data, e := os.ReadFile(name)
	if e != nil {
		return e
	}
	original := string(data)
	if cfg.ServiceType == "mihomo" {
		modified, err := adaptNativeAIODNS(cfg, string(data))
		if err != nil {
			return err
		}
		modified, err = adaptGatewayDNS(cfg, modified)
		if err != nil {
			return err
		}
		data = []byte(modified)
	}
	var doc map[string]interface{}
	if e = yaml.Unmarshal(data, &doc); e != nil || len(doc) == 0 {
		return diagnosticFailure("configuration must be a nonempty YAML mapping", e, nil)
	}
	if cfg.ServiceType == "mosdns" {
		if e = validateMosDNS(doc, root, s.Manifest); e != nil {
			return e
		}
		return nil
	}
	if cfg.ServiceType == "oxidns" {
		return validateOxiDNS(cfg, doc, root, name, s.Manifest)
	}
	if cfg.ServiceType != "mihomo" {
		return fmt.Errorf("unsupported service type")
	}
	if e = checkLinuxGatewayCore(cfg, doc); e != nil {
		return e
	}
	listed := map[string]bool{}
	for _, f := range s.Manifest.Files {
		listed[f.Path] = true
	}
	for _, kind := range []string{"proxy-providers", "rule-providers"} {
		providers, ok := doc[kind].(map[string]interface{})
		if !ok {
			continue
		}
		for label, value := range providers {
			provider, ok := value.(map[string]interface{})
			if !ok {
				return configurationLocation(fmt.Errorf("invalid %s entry %s", kind, label), s.Manifest.ConfigPath, data, kind, label)
			}
			if raw, ok := provider["path"].(string); ok && raw != "" {
				rel, e := resolveStagedReference(cfg, root, raw)
				if e != nil {
					return configurationLocation(e, s.Manifest.ConfigPath, data, kind, label, "path")
				}
				if !listed[rel] {
					return configurationLocation(fmt.Errorf("provider %s is missing from deployment", label), s.Manifest.ConfigPath, data, kind, label, "path")
				}
				if rel != raw {
					provider["path"] = rel
				}
			} else if provider["type"] != "inline" {
				return configurationLocation(fmt.Errorf("provider %s requires a bundled local path", label), s.Manifest.ConfigPath, data, kind, label)
			}
		}
	}
	// Geo assets first obtained by a prior deployment remain part of subsequent
	// effective manifests. Otherwise stale-file cleanup would remove the live
	// asset immediately after a successful offline native validation used it.
	managedResources := map[string]bool{}
	if b, e := os.ReadFile(filepath.Join(deploymentRoot(cfg), "active-manifest.json")); e == nil {
		var active DeploymentManifest
		if e = json.Unmarshal(b, &active); e != nil {
			return fmt.Errorf("invalid active deployment manifest")
		}
		for _, entry := range active.Files {
			if entry.Role == "resource" {
				managedResources[entry.Path] = true
			}
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	// Existing independently installed Geo resources remain external to the bundle.
	// Copy them for native validation so -t never reads a mutable live config tree.
	for _, asset := range mihomoGeoAssetNames(filepath.Dir(cfg.ConfigPath)) {
		if _, e := os.Stat(filepath.Join(root, asset)); e == nil {
			continue
		}
		src := filepath.Join(filepath.Dir(cfg.ConfigPath), asset)
		if info, e := os.Lstat(src); e == nil && info.Mode().IsRegular() {
			if e = durableCopy(src, filepath.Join(root, asset), 0600); e != nil {
				return e
			}
		}
	}
	// Normalization of managed absolute/./ paths must be part of the final bytes.
	normalized, e := yaml.Marshal(doc)
	if e != nil {
		return e
	}
	if s.Manifest.FinalConfig && string(normalized) != original {
		return fmt.Errorf("设备环境已改变，最终配置与预览不一致，请重新生成后推送")
	}
	if e = durableWrite(name, normalized, 0600); e != nil {
		return e
	}
	for i := range s.Manifest.Files {
		if s.Manifest.Files[i].Path == s.Manifest.ConfigPath {
			h, n, e := hashFile(name)
			if e != nil {
				return e
			}
			s.Manifest.Files[i].SHA256 = h
			s.Manifest.Files[i].Size = n
		}
	}
	binaryPath := cfg.ServiceBinary
	if binaryPath == "" {
		binaryPath = "mihomo"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binaryPath, "-t", "-d", root, "-f", name)
	cmd.Dir = root
	if _, e = runDiagnosticCommand(ctx, cmd, "Mihomo configuration check", append(configurationDiagnosticSecrets(doc), cfg.Token)...); e != nil {
		return nativeConfigurationLocation(mihomoResourceFailure(cfg, e), root, name, s.Manifest, true)
	}
	// Native checks can obtain missing Geo resources. Include those exact bytes
	// in the effective transaction instead of losing them when staging ends.
	for _, asset := range mihomoGeoAssetNames(root) {
		if listed[asset] {
			continue
		}
		if _, e := os.Lstat(filepath.Join(filepath.Dir(cfg.ConfigPath), asset)); e == nil && !managedResources[asset] {
			continue
		}
		if info, e := os.Lstat(filepath.Join(root, asset)); e == nil && info.Mode().IsRegular() {
			h, n, e := hashFile(filepath.Join(root, asset))
			if e != nil {
				return e
			}
			s.Manifest.Files = append(s.Manifest.Files, DeploymentFile{Path: asset, Size: n, SHA256: h, Role: "resource"})
		}
	}
	return verifyManifestFiles(root, s.Manifest)
}
func resolveStagedReference(cfg *Config, stage, raw string) (string, error) {
	rel := filepath.Clean(raw)
	if filepath.IsAbs(raw) {
		var e error
		rel, e = filepath.Rel(filepath.Dir(cfg.ConfigPath), raw)
		if e != nil {
			return "", fmt.Errorf("invalid configuration file reference")
		}
	}
	rel = filepath.ToSlash(rel)
	if !safeRelative(rel) {
		return "", fmt.Errorf("configuration file reference escapes service directory")
	}
	if e := rejectSymlinkPath(stage, rel); e != nil {
		return "", e
	}
	info, e := os.Stat(filepath.Join(stage, rel))
	if e != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("referenced file missing: %s", rel)
	}
	return rel, nil
}
func validateMosDNS(doc map[string]interface{}, stage string, m *DeploymentManifest) error {
	plugins, ok := doc["plugins"].([]interface{})
	if !ok || len(plugins) == 0 {
		return fmt.Errorf("MosDNS plugins must be a nonempty sequence")
	}
	tags := map[string]bool{}
	listed := map[string]bool{}
	for _, f := range m.Files {
		listed[f.Path] = true
	}
	for index, v := range plugins {
		p, ok := v.(map[string]interface{})
		if !ok {
			return pluginLocation(fmt.Errorf("invalid MosDNS plugin at index %d", index), stage, m, index)
		}
		kind, _ := p["type"].(string)
		tag, _ := p["tag"].(string)
		if kind == "" {
			return pluginLocation(fmt.Errorf("MosDNS plugin type missing at index %d (tag %q)", index, tag), stage, m, index, "type")
		}
		if tag != "" {
			if tags[tag] {
				return pluginLocation(fmt.Errorf("duplicate MosDNS plugin tag %q", tag), stage, m, index, "tag")
			}
			tags[tag] = true
		}
		args, _ := p["args"].(map[string]interface{})
		if args == nil {
			continue
		}
		if values, ok := args["files"].([]interface{}); ok {
			for fileIndex, value := range values {
				raw, ok := value.(string)
				if !ok {
					return pluginLocation(fmt.Errorf("invalid MosDNS file reference"), stage, m, index, "args", "files", fileIndex)
				}
				rel := filepath.ToSlash(filepath.Clean(raw))
				if !safeRelative(rel) || !listed[rel] {
					return pluginLocation(fmt.Errorf("MosDNS file must be bundled using a relative path: %s", raw), stage, m, index, "args", "files", fileIndex)
				}
				if _, e := os.Stat(filepath.Join(stage, rel)); e != nil {
					return pluginLocation(fmt.Errorf("MosDNS file missing: %s", rel), stage, m, index, "args", "files", fileIndex)
				}
			}
		}
	}
	return nil
}
func loopbackAddress(raw string) string {
	host, port, e := net.SplitHostPort(raw)
	if e != nil {
		return raw
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	if host == "::" {
		host = "::1"
	}
	return net.JoinHostPort(host, port)
}
func deploymentProbe(cfg *Config) error {
	running, e := serviceRunning(cfg)
	if e != nil {
		return e
	}
	if !running {
		return fmt.Errorf("service is not running")
	}
	data, e := os.ReadFile(cfg.ConfigPath)
	if e != nil {
		return e
	}
	var doc map[string]interface{}
	if e = yaml.Unmarshal(data, &doc); e != nil {
		return diagnosticFailure("cannot parse active configuration", e, nil)
	}
	url := cfg.HealthURL
	secret := ""
	dnsAddress := cfg.HealthDNSAddress
	if cfg.ServiceType == "mihomo" && url == "" {
		if v, ok := doc["external-controller"].(string); ok && v != "" {
			url = "http://" + loopbackAddress(v) + "/version"
			secret, _ = doc["secret"].(string)
		}
	}
	username, password := "", ""
	if cfg.ServiceType == "oxidns" {
		var err error
		url, username, password, err = oxidnsHealthEndpoint(doc, url)
		if err != nil {
			return err
		}
	}
	if url != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if e != nil {
			return diagnosticFailure("invalid health URL", e, nil)
		}
		if secret != "" {
			req.Header.Set("Authorization", "Bearer "+secret)
		}
		if username != "" {
			req.SetBasicAuth(username, password)
		}
		client := http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
		resp, e := client.Do(req)
		if e != nil {
			return diagnosticFailure("service HTTP health probe failed", e, nil)
		}
		resp.Body.Close()
		client.CloseIdleConnections()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("service HTTP health probe returned %d", resp.StatusCode)
		}
	}
	if dnsAddress == "" && (cfg.ServiceType == "mosdns" || cfg.ServiceType == "oxidns") {
		if plugins, ok := doc["plugins"].([]interface{}); ok {
			for _, value := range plugins {
				p, _ := value.(map[string]interface{})
				if p["type"] == "udp_server" {
					a, _ := p["args"].(map[string]interface{})
					dnsAddress, _ = a["listen"].(string)
					if dnsAddress != "" {
						break
					}
				}
			}
		}
	}
	if dnsAddress != "" {
		name := cfg.HealthDNSName
		if name == "" {
			name = "example.com"
		}
		if e = dnsHealthQuery(loopbackAddress(dnsAddress), name); e != nil {
			return e
		}
	}
	if (cfg.ServiceType == "mosdns" || cfg.ServiceType == "oxidns") && dnsAddress == "" {
		return fmt.Errorf("configure health_dns_address for DNS verification")
	}
	unixController, _ := doc["external-controller-unix"].(string)
	if cfg.ServiceType == "mihomo" && url == "" && dnsAddress == "" && unixController == "" {
		// Without a controller, probe an enabled proxy listener. The process status
		// stability window and native validation complement this socket check.
		port := 0
		for _, key := range []string{"mixed-port", "port", "socks-port"} {
			if v, ok := doc[key].(int); ok && v > 0 {
				port = v
				break
			}
		}
		if port == 0 {
			return fmt.Errorf("configure health_url or health_dns_address for Mihomo")
		}
		// Actual proxy protocol checks below distinguish a working listener
		// from an unrelated process occupying the configured port.
	}
	if cfg.ServiceType == "mihomo" {
		if e := mihomoRuntimeProbe(doc); e != nil {
			return e
		}
	}
	return nil
}

func mihomoRuntimeProbe(doc map[string]interface{}) error {
	controller, _ := doc["external-controller"].(string)
	unixController, _ := doc["external-controller-unix"].(string)
	if controller == "" && unixController != "" {
		if err := mihomoUnixControllerProbe(doc); err != nil {
			return err
		}
	}
	if controller, ok := doc["external-controller"].(string); ok && controller != "" {
		secret, _ := doc["secret"].(string)
		for kind, endpoint := range map[string]string{"proxy-providers": "/providers/proxies", "rule-providers": "/providers/rules"} {
			expected, _ := doc[kind].(map[string]interface{})
			if len(expected) == 0 {
				continue
			}
			req, e := http.NewRequest(http.MethodGet, "http://"+loopbackAddress(controller)+endpoint, nil)
			if e != nil {
				return diagnosticFailure("invalid Mihomo controller address", e, nil)
			}
			if secret != "" {
				req.Header.Set("Authorization", "Bearer "+secret)
			}
			client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
			resp, e := client.Do(req)
			if e != nil {
				return diagnosticFailure("Mihomo provider initialization probe failed", e, nil)
			}
			var response struct {
				Providers map[string]json.RawMessage `json:"providers"`
			}
			e = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&response)
			resp.Body.Close()
			client.CloseIdleConnections()
			if e != nil || resp.StatusCode != 200 {
				return diagnosticFailure(fmt.Sprintf("Mihomo provider status unavailable (HTTP %d)", resp.StatusCode), e, nil)
			}
			for name := range expected {
				if _, ok := response.Providers[name]; !ok {
					return fmt.Errorf("Mihomo provider is not initialized: %s", name)
				}
			}
		}
	}
	bind := "127.0.0.1"
	if doc["allow-lan"] == true {
		if configured, ok := doc["bind-address"].(string); ok && configured != "" && configured != "*" && configured != "0.0.0.0" {
			bind = strings.Trim(configured, "[]")
			if bind == "::" {
				bind = "::1"
			}
		}
	}
	for _, key := range []string{"mixed-port", "port", "socks-port"} {
		if port, ok := doc[key].(int); ok && port > 0 {
			if e := probeMihomoProxy(net.JoinHostPort(bind, strconv.Itoa(port)), key); e != nil {
				return diagnosticFailure(fmt.Sprintf("Mihomo %s proxy handshake failed", key), e, nil)
			}
		}
	}
	if tun, ok := doc["tun"].(map[string]interface{}); ok && tun["enable"] == true {
		if device, ok := tun["device"].(string); ok && device != "" {
			iface, e := net.InterfaceByName(device)
			if e != nil || iface.Flags&net.FlagUp == 0 {
				return diagnosticFailure(fmt.Sprintf("Mihomo TUN interface %q is unavailable", device), e, nil)
			}
		}
	}
	return nil
}
func checkDeploymentHealth(cfg *Config) error {
	timeout := cfg.DeploymentHealthTimeout
	if timeout <= 0 {
		timeout = 30
	}
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	consecutive := 0
	var last error
	for {
		last = deploymentProbe(cfg)
		if last == nil {
			consecutive++
			if consecutive >= 3 {
				return nil
			}
		} else {
			consecutive = 0
		}
		if time.Now().After(deadline) {
			if last == nil {
				last = fmt.Errorf("service did not remain healthy long enough")
			}
			return withServiceLog(cfg, last)
		}
		time.Sleep(500 * time.Millisecond)
	}
}
func dnsHealthQuery(address, name string) error {
	name = strings.TrimSuffix(name, ".")
	if name == "" || len(name) > 253 {
		return fmt.Errorf("invalid DNS probe name")
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 {
			return fmt.Errorf("invalid DNS probe name")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resolver := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		d := net.Dialer{Timeout: 2 * time.Second}
		return d.DialContext(ctx, network, address)
	}}
	// The standard resolver validates packet IDs, DNS encoding and actual answers;
	// an absolute name avoids search-domain or system resolver fallbacks.
	addresses, e := resolver.LookupIP(ctx, "ip4", name+".")
	if e != nil || len(addresses) == 0 {
		return diagnosticFailure("DNS probe received no successful address answer", e, nil)
	}
	return nil
}

func waitServiceState(cfg *Config) (bool, error) {
	timeout := cfg.DeploymentHealthTimeout
	if timeout <= 0 {
		timeout = 30
	}
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	for {
		running, e := serviceRunning(cfg)
		if !errors.Is(e, errServiceTransition) {
			return running, e
		}
		if time.Now().After(deadline) {
			return false, fmt.Errorf("service did not finish startup or shutdown before deployment timeout")
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func probeMihomoProxy(address, kind string) error {
	c, e := net.DialTimeout("tcp", address, time.Second)
	if e != nil {
		return e
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(time.Second))
	if kind == "mixed-port" || kind == "socks-port" {
		if _, e = c.Write([]byte{5, 2, 0, 2}); e != nil {
			return e
		}
		reply := make([]byte, 2)
		if _, e = io.ReadFull(c, reply); e != nil {
			return e
		}
		if reply[0] != 5 || (reply[1] != 0 && reply[1] != 2) {
			return fmt.Errorf("not a SOCKS5 proxy")
		}
		return nil
	}
	// Probe without a routable destination, so health does not depend on DNS or
	// an external provider. Mihomo can acknowledge CONNECT before rejecting its
	// invalid target; that acknowledgement still verifies the proxy handshake.
	if _, e = io.WriteString(c, "CONNECT / HTTP/1.1\r\nHost: /\r\nConnection: close\r\n\r\n"); e != nil {
		return e
	}
	response, e := http.ReadResponse(bufio.NewReader(io.LimitReader(c, 16<<10)), &http.Request{Method: http.MethodConnect})
	if e != nil {
		return e
	}
	response.Body.Close()
	// Do not accept a generic 200 OK from an unrelated HTTP server occupying
	// the proxy port. Mihomo emits this specific CONNECT acknowledgement.
	established := response.StatusCode == http.StatusOK && strings.EqualFold(response.Status, "200 Connection established")
	if !established && response.StatusCode != 400 && response.StatusCode != 403 && response.StatusCode != 405 && response.StatusCode != 407 {
		return fmt.Errorf("unexpected HTTP proxy response: %s", response.Status)
	}
	return nil
}

func carryPersistentGeoResources(cfg *Config, s *DeploymentState) (bool, error) {
	if cfg.ServiceType != "mihomo" {
		return false, nil
	}
	b, e := os.ReadFile(filepath.Join(deploymentRoot(cfg), "active-manifest.json"))
	if os.IsNotExist(e) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	var old DeploymentManifest
	if e = json.Unmarshal(b, &old); e != nil {
		return false, fmt.Errorf("invalid active deployment manifest")
	}
	listed := map[string]bool{}
	for _, file := range s.Manifest.Files {
		listed[file.Path] = true
	}
	known := map[string]bool{}
	for _, asset := range mihomoGeoAssets {
		known[strings.ToLower(asset)] = true
	}
	changed := false
	root := filepath.Dir(cfg.ConfigPath)
	stage := filepath.Join(deploymentPath(cfg, s.DeploymentID), "stage")
	for _, file := range old.Files {
		if file.Role != "resource" || !known[strings.ToLower(file.Path)] || listed[file.Path] {
			continue
		}
		if e := rejectSymlinkPath(root, file.Path); e != nil {
			return false, e
		}
		if e := rejectSymlinkPath(stage, file.Path); e != nil {
			return false, e
		}
		if e := durableCopy(filepath.Join(root, file.Path), filepath.Join(stage, file.Path), 0600); e != nil {
			return false, fmt.Errorf("cannot carry persistent Geo resource %s", file.Path)
		}
		h, n, e := hashFile(filepath.Join(stage, file.Path))
		if e != nil {
			return false, e
		}
		s.Manifest.Files = append(s.Manifest.Files, DeploymentFile{Path: file.Path, Size: n, SHA256: h, Role: "resource"})
		listed[file.Path] = true
		changed = true
	}
	return changed, nil
}
