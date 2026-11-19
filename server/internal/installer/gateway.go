package installer

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"

	"configflow/internal/resource"
	"configflow/internal/weburl"
)

// Port of backend/agents/gateway_install_script.py: the pinned, opt-in
// Linux gateway core installer.

type textItem struct{ key, value string }

// ReleaseAsset is one CORE_RELEASES asset.
type ReleaseAsset struct{ URL, SHA256 string }

// CoreRelease is one CORE_RELEASES entry.
type CoreRelease struct {
	Service string
	Version string
	Assets  map[string]ReleaseAsset
}

// TextValue returns a SELECTIVE_TEXT / FULL_TUN_TEXT value by key.
func TextValue(table []textItem, key string) string {
	for _, item := range table {
		if item.key == key {
			return item.value
		}
	}
	return ""
}

// AgentsStaticDir is the directory download_agent_binary serves:
// $AGENTS_STATIC_DIR (default /opt/configflow/static/agents), or
// backend/static/agents when that does not exist.
func AgentsStaticDir() string {
	directory := os.Getenv("AGENTS_STATIC_DIR")
	if directory == "" {
		directory = "/opt/configflow/static/agents"
	}
	if _, err := os.Stat(directory); err != nil {
		directory = resource.FromEnv().Backend("static/agents")
	}
	return directory
}

// LocalAgentChecksums is local_agent_checksums(): the SHA256 of the
// binaries served by agents.download_agent_binary.
func LocalAgentChecksums() (map[string]string, error) {
	directory := AgentsStaticDir()
	if value, set := os.LookupEnv("AGENTS_STATIC_DIR"); set && value == "" {
		// Path('') is '.', which exists (os.path.exists('') in the download
		// route is False, hence AgentsStaticDir's default there).
		directory = "."
	}
	checksums := map[string]string{}
	for _, arch := range []string{"linux-amd64", "linux-arm64"} {
		binary := filepath.Join(directory, "configflow-agent-"+arch)
		info, err := os.Stat(binary)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		stream, err := os.Open(binary)
		if err != nil {
			return nil, err
		}
		digest := sha256.New()
		_, err = io.Copy(digest, stream)
		stream.Close()
		if err != nil {
			return nil, err
		}
		checksums[arch] = hex.EncodeToString(digest.Sum(nil))
	}
	return checksums, nil
}

// httpURL is _http_url(value, name).
func httpURL(value, name string) (string, error) {
	if hasControl(value) {
		return "", valueError(name + " must be an HTTP(S) URL")
	}
	parsed, err := weburl.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || weburl.HasCredentials(parsed) || parsed.Fragment != "" {
		return "", valueError(name + " must be an HTTP(S) URL without credentials or fragment")
	}
	return strings.TrimRight(value, "/"), nil
}

// GatewayInstallOptions contains the native gateway renderer input.
// A nil AgentChecksums hashes the local binaries; a non-nil empty map explicitly
// supplies no checksums. Callers supply both Agent ports.
type GatewayInstallOptions struct {
	ServerURL         string
	AgentName         string
	AgentIP           string
	BinaryDownloadURL string
	MihomoAgentPort   int
	OxidnsAgentPort   int
	EnrollmentURL     string
	EnrollmentToken   string
	AgentChecksums    map[string]string
	InstallationID    string
	FullTun           bool
	portError         error
}

func isSHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !(s[i] >= '0' && s[i] <= '9' || s[i] >= 'a' && s[i] <= 'f') {
			return false
		}
	}
	return true
}

// RenderGatewayInstall is generate_gateway_install_script.
func RenderGatewayInstall(o GatewayInstallOptions) (string, error) {
	serverURL, err := httpURL(o.ServerURL, "server_url")
	if err != nil {
		return "", err
	}
	download := o.BinaryDownloadURL
	if download == "" {
		download = serverURL + "/api/agents/download"
	}
	binaryDownloadURL, err := httpURL(download, "binary_download_url")
	if err != nil {
		return "", err
	}
	if o.AgentIP != "" {
		address, err := parseIPv4(o.AgentIP)
		if err != nil {
			return "", valueError("Gateway Agent address must be an IPv4 address")
		}
		if isLoopback(address) || isMulticast(address) || isUnspecified(address) || isLinkLocal(address) {
			return "", valueError("Gateway Agent address must be a usable LAN IPv4 address")
		}
	}
	if (o.EnrollmentURL != "") != (o.EnrollmentToken != "") {
		return "", valueError("Both enrollment_url and enrollment_token are required together")
	}
	enrollmentURL := o.EnrollmentURL
	if enrollmentURL != "" {
		if enrollmentURL, err = httpURL(enrollmentURL, "enrollment_url"); err != nil {
			return "", err
		}
		a, err := weburl.Parse(enrollmentURL)
		if err != nil {
			return "", valueError(err.Error())
		}
		b, err := weburl.Parse(serverURL)
		if err != nil {
			return "", valueError(err.Error())
		}
		if a.Scheme != b.Scheme || a.User.String() != b.User.String() || a.Host != b.Host {
			return "", valueError("Enrollment endpoint must use the ConfigFlow server origin")
		}
	}
	if o.EnrollmentToken != "" && !IsSessionToken(o.EnrollmentToken) {
		return "", valueError("Invalid installation-session token")
	}
	checksums := o.AgentChecksums
	if checksums == nil {
		if checksums, err = LocalAgentChecksums(); err != nil {
			return "", err
		}
	}
	if len(checksums) == 0 {
		return "", valueError(msgAgentBinaryUnavailable)
	}
	for arch, digest := range checksums {
		if (arch != "linux-amd64" && arch != "linux-arm64") || !isSHA256Hex(digest) {
			return "", valueError("Invalid Agent architecture or SHA256 digest")
		}
	}
	aioOptions := DefaultAIOOptions(serverURL, o.AgentName)
	aioOptions.portError = o.portError
	aioOptions.AgentIP = o.AgentIP
	aioOptions.BinaryDownloadURL = binaryDownloadURL
	if o.MihomoAgentPort != 0 {
		aioOptions.MihomoAgentPort = o.MihomoAgentPort
	}
	if o.OxidnsAgentPort != 0 {
		aioOptions.OxidnsAgentPort = o.OxidnsAgentPort
	}
	aioOptions.MihomoServiceBinary = "/usr/local/bin/mihomo"
	aioOptions.OxidnsServiceBinary = "/usr/local/bin/oxidns"
	aioOptions.NonInteractive = true
	aioOptions.VerifiedAgentPath = "$GATEWAY_STAGE/configflow-agent"
	aioOptions.InstallationID = o.InstallationID
	if o.InstallationID != "" {
		aioOptions.InstallationToken = o.EnrollmentToken
	}
	aioOptions.ManageIPv4Forwarding = o.FullTun
	aioOptions.ManageTun = o.FullTun
	aioOptions.TunPrepareAssumeEnabled = o.FullTun
	if o.FullTun {
		aioOptions.InstallationDescription = "为上一步准备好的 Mihomo 和 OxiDNS 安装 Agent，开启并持久化 IPv4 转发、准备 TUN；不改核心配置、不重启核心。"
	} else {
		aioOptions.InstallationDescription = "为上一步准备好的 Mihomo 和 OxiDNS 安装 Agent，不改核心配置、不重启核心。"
	}
	aio, err := RenderAIOInstall(aioOptions)
	if err != nil {
		return "", err
	}
	values := map[string]string{
		"aio_script":                 aio,
		"agent_install_dependencies": strings.TrimRight(AgentInstallDependencies, "\n"),
		"binary_download_url_shell":  shlexQuote(binaryDownloadURL),
		"enrollment_url_shell":       shlexQuote(enrollmentURL),
		"enrollment_token_shell":     shlexQuote(o.EnrollmentToken),
		"agent_amd64_sha_shell":      shlexQuote(checksums["linux-amd64"]),
		"agent_arm64_sha_shell":      shlexQuote(checksums["linux-arm64"]),
	}
	table := SelectiveText
	if o.FullTun {
		table = FullTunText
	}
	for _, item := range table {
		values[item.key] = item.value
	}
	for _, release := range CoreReleases {
		values[release.Service+"_version_shell"] = shlexQuote(release.Version)
		for _, arch := range []string{"amd64", "arm64"} {
			asset := release.Assets["linux-"+arch]
			values[release.Service+"_"+arch+"_url_shell"] = shlexQuote(asset.URL)
			values[release.Service+"_"+arch+"_sha_shell"] = shlexQuote(asset.SHA256)
		}
	}
	return WithInstallOutput(substitute(installGatewayTemplate, values)), nil
}
