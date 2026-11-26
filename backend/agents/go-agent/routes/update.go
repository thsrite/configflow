package routes

import (
	"agent/upgrade"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"time"
)

// AgentUpdateRequest 更新请求结构
type AgentUpdateRequest struct {
	Version     string `json:"version"`
	DownloadURL string `json:"download_url"`
	MD5Sum      string `json:"md5sum,omitempty"` // 可选的 MD5 校验和
}

// UpdateResponse 更新响应结构
type UpdateResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

var updateLockWait = 2 * time.Second
var errRuleUpdateBusy = errors.New("an OxiDNS rule update is in progress")

// HandleUpdate returns a durable task. Its worker has a separate system service.
func HandleUpdate(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		binary, err := os.Executable()
		if err != nil {
			deploymentFailure(w, 500, err)
			return
		}
		if r.Method == http.MethodGet {
			status, err := upgrade.ReadStatus(binary, cfg.AgentID)
			if err != nil {
				deploymentFailure(w, 404, fmt.Errorf("update not recorded"))
				return
			}
			JsonResponse(w, 200, status)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(405)
			return
		}
		var request upgrade.Request
		if err := json.NewDecoder(io.LimitReader(r.Body, 16385)).Decode(&request); err != nil {
			deploymentFailure(w, 400, fmt.Errorf("invalid update request"))
			return
		}
		release, err := waitServiceOperation(cfg, true, updateLockWait)
		if err == errDeploymentBusy && oxRuleUpdateRunning() {
			// A rule update keeps the lock for about a minute; name it so the
			// admin waits for it instead of guessing which operation is running.
			err = errRuleUpdateBusy
		}
		if err != nil {
			deploymentFailure(w, 409, err)
			return
		}
		defer release()
		if err = deploymentBlocked(cfg); err != nil {
			deploymentFailure(w, 409, err)
			return
		}
		status, err := upgrade.Prepare(cfg.AgentConfigFile, binary, request)
		if err != nil {
			deploymentFailure(w, 409, err)
			return
		}
		JsonResponse(w, 202, status)
	}
}

func UpgradeInfoHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(405)
			return
		}
		binary, err := os.Executable()
		if err != nil {
			deploymentFailure(w, 500, err)
			return
		}
		ready, _ := upgrade.MigrationReady(cfg.AgentConfigFile, binary)
		method := "shell"
		if config, err := upgrade.ReadConfig(cfg.AgentConfigFile); err == nil && config.Docker() {
			method = "docker"
		}
		JsonResponse(w, 200, UpgradeInfoResponse{
			AgentID:          cfg.AgentID,
			Architecture:     detectArchitecture(),
			DeploymentMethod: method,
			MigrationReady:   ready,
			UpdateProtocol:   2,
			Version:          upgrade.Version,
		})
	}
}

// detectArchitecture 检测系统架构
func detectArchitecture() string {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	// 转换为文件名格式
	switch goarch {
	case "amd64":
		return "linux-amd64"
	case "arm64":
		return "linux-arm64"
	case "arm":
		// 检查是否为 ARMv7
		return "linux-armv7"
	default:
		// 默认返回 amd64
		logger.Warnf("未知架构: %s/%s，默认使用 linux-amd64", goos, goarch)
		return "linux-amd64"
	}
}
