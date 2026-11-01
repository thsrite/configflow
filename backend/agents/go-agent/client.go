package main

import (
	"agent/routes"
	"agent/upgrade"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

const AgentVersion = upgrade.Version

// 全局监控数据收集器
var metricsCollector *MetricsCollector

// 初始化监控收集器
func init() {
	metricsCollector = NewMetricsCollector()
}

type RegisterRequest struct {
	Name              string `json:"name"`
	Host              string `json:"host"`
	Port              int    `json:"port"`
	ServiceType       string `json:"service_type"`
	DeploymentMethod  string `json:"deployment_method"`
	Version           string `json:"version"`
	InstallationID    string `json:"installation_id,omitempty"`
	InstallationToken string `json:"installation_token,omitempty"`
}

type RegisterResponse struct {
	Success bool   `json:"success"`
	ID      string `json:"id"`
	Token   string `json:"token"`
}

type HeartbeatRequest struct {
	Version       string         `json:"version"`
	ServiceStatus string         `json:"service_status"`
	SystemMetrics *SystemMetrics `json:"system_metrics,omitempty"`
}

// sendRegisterRequest 发送注册请求到服务器
func (c *Config) sendRegisterRequest(reqBody RegisterRequest) (*RegisterResponse, error) {
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		log.Printf("Failed to marshal register request: %v", err)
		return nil, fmt.Errorf("failed to marshal register request: %w", err)
	}

	log.Printf("Register request prepared (body length: %d)", len(jsonData))

	registerURL := fmt.Sprintf("%s/api/agents/register", c.ServerURL)
	log.Printf("Sending register request to: %s", redactURLForLog(registerURL))
	req, err := http.NewRequest(http.MethodPost, registerURL, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Failed to create register request for %s", redactURLForLog(registerURL))
		return nil, safeURLFailure("failed to create register request for", registerURL, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("Failed to send register request to %s", redactURLForLog(registerURL))
		return nil, safeURLFailure("failed to send register request to", registerURL, err)
	}
	defer resp.Body.Close()

	log.Printf("Register request response status: %d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		log.Printf("Register request failed with status code: %d", resp.StatusCode)
		return nil, fmt.Errorf("register request failed with status code: %d", resp.StatusCode)
	}

	var regResp RegisterResponse
	if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
		log.Printf("Failed to decode register response: %v", err)
		return nil, fmt.Errorf("failed to decode register response: %w", err)
	}

	log.Printf("Register response: success=%t, id=%s", regResp.Success, regResp.ID)
	return &regResp, nil
}

// handleRegisterResponse 处理注册响应
func (c *Config) handleRegisterResponse(regResp *RegisterResponse) error {
	if !regResp.Success {
		log.Printf("Registration failed: server returned success=false")
		return fmt.Errorf("registration failed: server returned success=false")
	}

	if regResp.ID == "" || (regResp.Token == "" && c.Token == "") {
		return fmt.Errorf("registration response is missing Agent credentials")
	}
	previousID, previousToken := c.AgentID, c.Token
	previousInstallationID, previousInstallationToken := c.InstallationID, c.InstallationToken

	log.Printf("Registration successful! Agent ID: %s", regResp.ID)
	c.AgentID = regResp.ID
	if regResp.Token != "" {
		c.Token = regResp.Token
	}

	// Enrollment uses the durable Agent credential from here on. Never let an
	// expired one-time installation capability block a later re-registration.
	c.InstallationID = ""
	c.InstallationToken = ""

	log.Printf("Saving configuration...")
	if err := c.Save(); err != nil {
		c.AgentID, c.Token = previousID, previousToken
		c.InstallationID, c.InstallationToken = previousInstallationID, previousInstallationToken
		log.Printf("Failed to save configuration: %v", err)
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	log.Printf("Configuration saved successfully")
	return nil
}

// RegisterAgent 向中央服务器注册 agent
func (c *Config) RegisterAgent() error {
	log.Println("Agent not registered. Attempting to register...")

	// 优先使用配置中的 AgentIP
	hostIP := strings.TrimSpace(c.AgentIP)
	if hostIP == "" {
		localIP, err := getLocalIP(routes.OxiDNSHAVirtualIPs(&routes.Config{AgentConfigFile: c.filePath, ServiceType: c.ServiceType, NativeAIO: c.NativeAIO})...)
		if err != nil {
			// A wildcard listener is not an address the server can connect to.
			// Empty Host lets the server use the registration request's source IP.
			localIP = ""
			if fallback := net.ParseIP(strings.TrimSpace(c.AgentHost)); fallback != nil && fallback.IsGlobalUnicast() && !fallback.IsLoopback() {
				localIP = fallback.String()
			}
			log.Printf("Warning: could not get local IP: %v. Using registration fallback %q (empty means request source IP).", err, localIP)
		}
		hostIP = localIP
	}

	log.Printf("Registering agent with name: %s, host: %s, port: %d, service_type: %s",
		c.AgentName, hostIP, c.AgentPort, c.ServiceType)

	reqBody := RegisterRequest{
		Name:              c.AgentName,
		Host:              hostIP,
		Port:              c.AgentPort,
		ServiceType:       c.ServiceType,
		DeploymentMethod:  c.DeploymentMethod,
		Version:           AgentVersion,
		InstallationID:    c.InstallationID,
		InstallationToken: c.InstallationToken,
	}

	// 发送注册请求
	regResp, err := c.sendRegisterRequest(reqBody)
	if err != nil {
		return err
	}

	// 处理注册响应
	if err := c.handleRegisterResponse(regResp); err != nil {
		return err
	}

	return nil
}

// createHeartbeatRequest 创建心跳请求
func (c *Config) createHeartbeatRequest() ([]byte, error) {
	status := c.managedServiceStatus()
	logDebugf("Service status: %s", status)

	reqBody := HeartbeatRequest{
		Version:       AgentVersion,
		ServiceStatus: status,
	}

	// 收集系统监控数据（如果启用）
	if c.IsMetricsEnabled() {
		// 使用 defer + recover 确保监控模块的 panic 不会导致整个心跳失败
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("Error: Metrics collection panic recovered: %v (continuing without metrics)", r)
				}
			}()

			metrics, err := metricsCollector.CollectSystemMetrics()
			if err != nil {
				log.Printf("Warning: Failed to collect system metrics: %v (continuing without metrics)", err)
				return
			}

			// 验证监控数据的有效性
			if metrics != nil {
				reqBody.SystemMetrics = metrics
				logDebugf("System metrics collected: CPU %.2f%%, Memory %.2f%%, Disk %.2f%%, Network ↑%d B/s ↓%d B/s",
					metrics.CPU.UsagePercent,
					metrics.Memory.UsedPercent,
					metrics.Disk.UsedPercent,
					metrics.Network.SpeedSent,
					metrics.Network.SpeedRecv)
			} else {
				log.Printf("Warning: Metrics collection returned nil (continuing without metrics)")
			}
		}()
	} else {
		log.Printf("System metrics collection is disabled in config")
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		log.Printf("Error: Failed to marshal heartbeat request: %v", err)
		return nil, err
	}

	return jsonData, nil
}

func newHeartbeatHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Heartbeats are infrequent and may cross a TUN reload. A connection can
	// remain established while no longer delivering responses, especially with
	// HTTP/2. Give each heartbeat its own connection so the next scheduled
	// attempt can recover without restarting the Agent. Do not change the
	// shared transport used by registration and other requests.
	transport.DisableKeepAlives = true
	return &http.Client{Transport: transport, Timeout: 10 * time.Second}
}

var heartbeatHTTPClient = newHeartbeatHTTPClient()

// sendHeartbeatRequest 发送心跳请求到服务器
func (c *Config) sendHeartbeatRequest(jsonData []byte) error {
	heartbeatURL := fmt.Sprintf("%s/api/agents/%s/heartbeat", c.ServerURL, c.AgentID)
	logDebugf("Sending heartbeat to: %s", redactURLForLog(heartbeatURL))

	req, err := http.NewRequest("POST", heartbeatURL, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Error: Failed to create heartbeat request for %s", redactURLForLog(heartbeatURL))
		return safeURLFailure("failed to create heartbeat request for", heartbeatURL, err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)

	client := heartbeatHTTPClient
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error: Failed to send heartbeat to %s", redactURLForLog(heartbeatURL))
		return safeURLFailure("failed to send heartbeat to", heartbeatURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var result struct {
			DomainDiscoveryEnabled bool `json:"domain_discovery_enabled"`
			TrafficHistoryEnabled  bool `json:"traffic_history_enabled"`
		}
		// Older servers may return an empty body or omit the optional field.
		// Read the complete, bounded response: a valid JSON prefix alone does
		// not prove that the response finished before the heartbeat deadline.
		const maxResponseBytes = 64 * 1024
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
		if err != nil {
			return safeURLFailure("failed to read heartbeat response from", heartbeatURL, err)
		}
		if len(body) > maxResponseBytes {
			return safeURLFailure("failed to read heartbeat response from", heartbeatURL, errors.New("heartbeat response exceeds size limit"))
		}
		if len(bytes.TrimSpace(body)) != 0 {
			if err := json.Unmarshal(body, &result); err != nil {
				return safeURLFailure("failed to decode heartbeat response from", heartbeatURL, err)
			}
		}
		domainDiscovery.SetEnabled(c, result.DomainDiscoveryEnabled)
		trafficHistory.SetEnabled(c, result.TrafficHistoryEnabled)

		status := c.managedServiceStatus()
		// 心跳成功是常态，仅在服务状态发生变化时记录，避免日志无限累积
		if status != lastReportedStatus {
			log.Printf("Heartbeat sent successfully (service status: %s -> %s)", lastReportedStatus, status)
			lastReportedStatus = status
		} else {
			logDebugf("Heartbeat sent successfully (service status: %s)", status)
		}
	} else {
		log.Printf("Error: Heartbeat failed with status code: %d", resp.StatusCode)
	}

	return nil
}

// SendHeartbeat 发送心跳
func (c *Config) SendHeartbeat() {
	logDebugf("Sending heartbeat...")

	if c.AgentID == "" || c.Token == "" {
		log.Println("Skipping heartbeat: agent_id or token is empty.")
		return
	}

	// 创建心跳请求
	jsonData, err := c.createHeartbeatRequest()
	if err != nil {
		return
	}

	// 发送心跳请求
	if err := c.sendHeartbeatRequest(jsonData); err != nil {
		log.Printf("Error sending heartbeat: %v", err)
	}
}
