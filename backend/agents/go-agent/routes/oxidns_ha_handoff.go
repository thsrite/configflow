package routes

// Planned publication only changes this instance's existing health-check
// weight. The durable preference survives a long upload or an Agent restart;
// it never suppresses automatic takeover when the peer stops answering DNS.
import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

const oxidnsHAHandoffProtocol = 1
const haHandoffPeerTimeout = 300 * time.Millisecond

var haHandoffIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,95}$`)
var haHandoffProbe = haDNSResolves

type haHandoffState struct {
	Version     int          `json:"version"`
	OperationID string       `json:"operation_id"`
	Token       string       `json:"token"`
	Spec        OxiDNSHASpec `json:"spec"`
	CreatedAt   string       `json:"created_at"`
}

func haHandoffPath(cfg *Config) (string, error) {
	journal, err := haJournalPath(cfg)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(journal), "oxidns-ha-handoff.json"), nil
}

func loadHAHandoff(cfg *Config, state *haState) (*haHandoffState, error) {
	path, err := haHandoffPath(cfg)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(data) > 4096 {
		return nil, fmt.Errorf("DNS 虚拟 IP 迁移记录无法读取")
	}
	var handoff haHandoffState
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&handoff); err != nil {
		return nil, fmt.Errorf("DNS 虚拟 IP 迁移记录无效")
	}
	if decoder.Decode(new(any)) != io.EOF || handoff.Version != oxidnsHAHandoffProtocol ||
		!haHandoffIDPattern.MatchString(handoff.OperationID) || !haTokenPattern.MatchString(handoff.Token) ||
		validateOxiDNSHASpec(handoff.Spec) != nil {
		return nil, fmt.Errorf("DNS 虚拟 IP 迁移记录无效")
	}
	if _, err := time.Parse(time.RFC3339Nano, handoff.CreatedAt); err != nil {
		return nil, fmt.Errorf("DNS 虚拟 IP 迁移记录时间无效")
	}
	// A removed or reconfigured pair must never inherit an earlier preference.
	if state == nil || !state.Active || state.Pending || state.Token != handoff.Token || state.Spec != handoff.Spec {
		return nil, nil
	}
	return &handoff, nil
}

func haPeerHealthy(cfg *Config, state *haState) bool {
	if state == nil {
		return false
	}
	_, name := haCheckTarget(cfg.ConfigPath)
	return haHandoffProbe(net.JoinHostPort(state.Spec.PeerIP, "53"), name, haHandoffPeerTimeout) == nil
}

func haYieldToPeer(cfg *Config, state *haState) bool {
	handoff, err := loadHAHandoff(cfg, state)
	// Damaged, stale or absent preferences fail open to normal health-based
	// election; an unavailable peer must never fence a healthy local server.
	return err == nil && handoff != nil && haPeerHealthy(cfg, state)
}

func haHandoffPublic(cfg *Config, state *haState, localHealthy bool) (any, error) {
	handoff, err := loadHAHandoff(cfg, state)
	if err != nil || handoff == nil {
		return nil, err
	}
	peerHealthy := haPeerHealthy(cfg, state)
	return map[string]any{"operation_id": handoff.OperationID, "active": true,
		"peer_healthy": peerHealthy, "yielding": localHealthy && peerHealthy,
		"created_at": handoff.CreatedAt}, nil
}

func changeHAHandoff(cfg *Config, operationID string, remove bool) error {
	if !haHandoffIDPattern.MatchString(operationID) {
		return fmt.Errorf("operation_id 必须为 1 到 96 位字母、数字、下划线或连字符")
	}
	if err := haPlatform(cfg); err != nil {
		return err
	}
	// This lock intentionally excludes only HA configuration changes. A
	// reconciliation request may arrive during the core's deployment lock.
	haMu.Lock()
	defer haMu.Unlock()
	unlock, err := lockHAJournal(cfg)
	if err != nil {
		return err
	}
	defer unlock()
	state, err := loadHAState(cfg)
	if err != nil {
		return err
	}
	handoff, err := loadHAHandoff(cfg, state)
	if err != nil {
		return err
	}
	if handoff != nil && handoff.OperationID != operationID {
		return fmt.Errorf("另一发布任务正在迁移虚拟 IP，请先确认原任务")
	}
	path, err := haHandoffPath(cfg)
	if err != nil {
		return err
	}
	if remove {
		if handoff == nil {
			return nil
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return syncDirectory(filepath.Dir(path))
	}
	if state == nil || !state.Active || state.Pending {
		return fmt.Errorf("DNS 高可用尚未生效，不能迁移虚拟 IP")
	}
	if err := haFilesDrift(cfg, state); err != nil {
		return err
	}
	if !haServiceRunning(cfg.ServiceManager) {
		return fmt.Errorf("keepalived 未运行，不能迁移虚拟 IP")
	}
	// An already persisted request remains idempotent while the peer is down.
	// Its effective preference is suspended by every health-check invocation.
	if handoff != nil {
		return nil
	}
	if !haPeerHealthy(cfg, state) {
		return fmt.Errorf("对端 DNS 未正常应答，未迁移虚拟 IP")
	}
	handoff = &haHandoffState{Version: oxidnsHAHandoffProtocol, OperationID: operationID,
		Token: state.Token, Spec: state.Spec, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	data, err := json.MarshalIndent(handoff, "", "  ")
	if err != nil {
		return err
	}
	return durableWrite(path, data, 0600)
}

// OxiDNSHAHandoffHandler serves an authenticated, fixed-target preference.
// No caller can select a different peer, VIP, health target or duration.
func OxiDNSHAHandoffHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			JsonResponse(w, http.StatusOK, haStatus(cfg))
			return
		}
		if r.Method != http.MethodPost && r.Method != http.MethodDelete {
			w.Header().Set("Allow", "GET, POST, DELETE")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			OperationID string `json:"operation_id"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil || decoder.Decode(new(any)) != io.EOF ||
			!haHandoffIDPattern.MatchString(request.OperationID) || len(r.URL.Query()) != 0 {
			haFailure(w, http.StatusBadRequest, fmt.Errorf("迁移请求必须只包含有效 operation_id"))
			return
		}
		if err := changeHAHandoff(cfg, request.OperationID, r.Method == http.MethodDelete); err != nil {
			haFailure(w, http.StatusConflict, err)
			return
		}
		JsonResponse(w, http.StatusOK, haStatus(cfg))
	}
}
