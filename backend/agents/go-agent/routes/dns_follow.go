package routes

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"agent/svc"
)

// A DNS follow summary is deliberately smaller than a configuration download:
// the panel receives only the listener belonging to a confirmed publication.
// All checks run under the same lock as publication, restart and rollback.
func confirmedDNSDeployment(cfg *Config, id string, allFiles bool) ([]byte, error) {
	state, err := loadDeployment(cfg, id)
	if err != nil || state.Status != "succeeded" || !state.Success || state.ManualRollback {
		return nil, fmt.Errorf("该发布尚未确认成功或已回滚，请先成功发布配置")
	}
	data, err := readDNSConfigFile(filepath.Join(deploymentRoot(cfg), "active-manifest.json"))
	var manifest DeploymentManifest
	if err != nil || json.Unmarshal(data, &manifest) != nil || manifest.DeploymentID != id ||
		manifest.ProtocolVersion != 1 || manifest.ServiceType != cfg.ServiceType ||
		cfg.AgentID == "" || manifest.AgentID != cfg.AgentID || manifest.ConfigPath != filepath.Base(cfg.ConfigPath) {
		return nil, fmt.Errorf("设备当前运行配置与指定发布不一致，请刷新发布状态后重试")
	}
	if len(manifest.Files) == 0 || len(manifest.Files) > maxDeploymentFiles {
		return nil, fmt.Errorf("无法确认当前发布的文件清单，请重新发布配置")
	}
	var configFile *DeploymentFile
	seen := make(map[string]bool)
	for i := range manifest.Files {
		file := &manifest.Files[i]
		if !safeRelative(file.Path) || seen[file.Path] || file.Size < 0 || file.Size > maxExpandedSize || !validDigest(file.SHA256) {
			return nil, fmt.Errorf("无法确认当前发布的文件清单，请重新发布配置")
		}
		seen[file.Path] = true
		if file.Path == manifest.ConfigPath {
			configFile = file
		}
	}
	if configFile == nil {
		return nil, fmt.Errorf("当前发布缺少主配置的校验信息，请重新发布配置")
	}
	if rejectSymlinkPath(filepath.Dir(cfg.ConfigPath), manifest.ConfigPath) != nil {
		return nil, fmt.Errorf("设备配置路径已改变，请重新发布配置")
	}
	// Use the publication's own size bound. Large valid Mihomo configurations
	// must not be rejected by the smaller optional peer-inspection limit.
	content, err := readFollowConfig(cfg.ConfigPath, configFile.Size)
	if err != nil {
		return nil, fmt.Errorf("无法读取设备当前配置，请检查配置文件是否可读")
	}
	digest := sha256.Sum256(content)
	if hex.EncodeToString(digest[:]) != configFile.SHA256 || int64(len(content)) != configFile.Size {
		return nil, fmt.Errorf("设备配置在发布后被修改，请先重新发布配置再同步 DNS")
	}
	// Rule files OxiDNS refreshed itself are not edits; the follow publication
	// replaces them with the server's bytes anyway.
	if allFiles && verifyActiveManagedFiles(filepath.Dir(cfg.ConfigPath), &manifest, content) != nil {
		return nil, fmt.Errorf("设备受管文件在发布后被修改或缺失，请先重新发布配置再同步 DNS")
	}
	return content, nil
}

func readFollowConfig(name string, size int64) ([]byte, error) {
	info, err := os.Lstat(name)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("configuration file changed")
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("configuration file changed")
	}
	return io.ReadAll(io.LimitReader(f, size+1))
}

func dnsFollowLock(cfg *Config, w http.ResponseWriter) (func(), bool) {
	release, err := AcquireServiceOperation(cfg)
	if err != nil {
		JsonResponse(w, http.StatusConflict, map[string]any{"success": false, "message": "设备正在执行其他操作，暂时无法确认 DNS 配置，请稍后重试"})
		return nil, false
	}
	return release, true
}

func dnsFollowFailure(w http.ResponseWriter, status int, err error) {
	JsonResponse(w, status, map[string]any{"success": false, "message": err.Error()})
}

func dnsFollowRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		dnsFollowFailure(w, http.StatusMethodNotAllowed, fmt.Errorf("仅支持读取 DNS 状态"))
		return "", false
	}
	id := r.URL.Query().Get("deployment_id")
	if !deploymentIDPattern.MatchString(id) {
		dnsFollowFailure(w, http.StatusBadRequest, fmt.Errorf("需要有效的发布编号才能确认 DNS 状态"))
		return "", false
	}
	return id, true
}

// DNSActiveHandler verifies the complete OxiDNS baseline before an automatic
// publication, including managed rule files edited outside ConfigFlow.
func DNSActiveHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := dnsFollowRequest(w, r)
		if !ok {
			return
		}
		if cfg.ServiceType != "oxidns" {
			dnsFollowFailure(w, http.StatusBadRequest, fmt.Errorf("当前 Agent 不是 OxiDNS"))
			return
		}
		release, ok := dnsFollowLock(cfg, w)
		if !ok {
			return
		}
		defer release()
		if _, err := confirmedDNSDeployment(cfg, id, true); err != nil {
			dnsFollowFailure(w, http.StatusConflict, err)
			return
		}
		// Its old upstream may have just moved to the new Mihomo port. Asking
		// OxiDNS to resolve through that old address would block its own repair.
		if running, err := serviceRunning(cfg); err != nil || !running {
			dnsFollowFailure(w, http.StatusServiceUnavailable, fmt.Errorf("OxiDNS 未运行或无法确认服务状态，请恢复服务后重试"))
			return
		}
		// Detect edits made outside the Agent lock while probing service health.
		if _, err := confirmedDNSDeployment(cfg, id, true); err != nil {
			dnsFollowFailure(w, http.StatusConflict, err)
			return
		}
		JsonResponse(w, http.StatusOK, map[string]any{"success": true, "agent_id": cfg.AgentID, "deployment_id": id})
	}
}

func parseFollowListener(content []byte) (string, string, int, error) {
	var doc map[string]any
	if yaml.Unmarshal(content, &doc) != nil {
		return "", "", 0, fmt.Errorf("无法解析 Mihomo 当前 DNS 配置")
	}
	dns, _ := doc["dns"].(map[string]any)
	if dns["enable"] != true {
		return "", "", 0, fmt.Errorf("Mihomo DNS 未开启，请开启 dns.enable 并重新发布")
	}
	listen, _ := dns["listen"].(string)
	listen = strings.TrimSpace(listen)
	host, portText, err := net.SplitHostPort(listen)
	valid := err == nil && len(portText) > 0 && len(portText) <= 5
	for _, digit := range portText {
		valid = valid && digit >= '0' && digit <= '9'
	}
	port, err := strconv.Atoi(portText)
	if !valid || err != nil || port < 1 || port > 65535 || strings.ContainsAny(host, "/\\@?#\t\r\n ") {
		return "", "", 0, fmt.Errorf("Mihomo dns.listen 不是有效的监听地址和端口，请修改后重新发布")
	}
	if host == "" || host == "*" {
		host = "0.0.0.0"
	}
	return listen, host, port, nil
}

func DNSListenerHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := dnsFollowRequest(w, r)
		if !ok {
			return
		}
		if cfg.ServiceType != "mihomo" {
			dnsFollowFailure(w, http.StatusBadRequest, fmt.Errorf("当前 Agent 不是 Mihomo"))
			return
		}
		release, ok := dnsFollowLock(cfg, w)
		if !ok {
			return
		}
		defer release()
		content, err := confirmedDNSDeployment(cfg, id, false)
		if err != nil {
			dnsFollowFailure(w, http.StatusConflict, err)
			return
		}
		listen, host, port, err := parseFollowListener(content)
		if err != nil {
			dnsFollowFailure(w, http.StatusConflict, err)
			return
		}
		if err := deploymentProbe(cfg); err != nil {
			dnsFollowFailure(w, http.StatusServiceUnavailable, fmt.Errorf("Mihomo 未运行或健康检查未通过，请恢复服务后重试"))
			return
		}
		if _, err := confirmedDNSDeployment(cfg, id, false); err != nil {
			dnsFollowFailure(w, http.StatusConflict, err)
			return
		}
		JsonResponse(w, http.StatusOK, map[string]any{
			"success": true, "agent_id": cfg.AgentID, "deployment_id": id,
			"enabled": true, "listen": listen, "host": host, "port": port,
		})
	}
}

func sameNamespaceDNSPeer(cfg *Config, read func(string) ([]byte, error)) (string, error) {
	unavailable := fmt.Errorf("未能确认同机 All in One 的 Mihomo Agent，请检查两个 Agent 的安装与注册，或填写可达的 Mihomo DNS 地址")
	if cfg.ServiceType != "oxidns" || cfg.AgentID == "" || dnsPeerService(cfg) != "mihomo" {
		return "", unavailable
	}
	peerPath := "/opt/configflow-agent/config-mihomo.json"
	if cfg.NativeAIO {
		if _, err := nativeAIOInstallationFor(cfg); err != nil {
			return "", unavailable
		}
		peerPath = "/opt/configflow-agent/mihomo/config.json"
	} else if cfg.AgentConfigFile != "/opt/configflow-agent/config-oxidns.json" || cfg.ServiceManager != svc.ContainerManager || cfg.ServiceUnit != "oxidns" {
		return "", unavailable
	}
	data, err := read(peerPath)
	var peer Config
	err = errors.Join(err, json.Unmarshal(data, &peer))
	// A peer configuration written before 1.9.3-go says "supervisor".
	peer.ServiceManager = svc.NormalizeManager(peer.ServiceManager)
	if err != nil || peer.ServiceType != "mihomo" ||
		peer.AgentID == "" || peer.AgentID == cfg.AgentID || !filepath.IsAbs(peer.ConfigPath) ||
		peer.ConfigPath == cfg.ConfigPath || peer.ServerURL != cfg.ServerURL {
		return "", unavailable
	}
	if cfg.NativeAIO {
		peer.AgentConfigFile = peerPath
		if _, err := nativeAIOInstallationFor(&peer); err != nil || peer.ServiceManager != cfg.ServiceManager {
			return "", unavailable
		}
	} else if peer.NativeAIO || peer.ServiceManager != svc.ContainerManager || peer.ServiceUnit != "mihomo" {
		return "", unavailable
	}
	return peer.AgentID, nil
}

func DNSPeerHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			dnsFollowFailure(w, http.StatusMethodNotAllowed, fmt.Errorf("仅支持读取 DNS 状态"))
			return
		}
		peerID, err := sameNamespaceDNSPeer(cfg, readDNSConfigFile)
		if err != nil {
			dnsFollowFailure(w, http.StatusConflict, err)
			return
		}
		JsonResponse(w, http.StatusOK, map[string]any{"success": true, "peer_agent_id": peerID, "service_type": "mihomo"})
	}
}
