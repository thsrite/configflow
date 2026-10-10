package main

import (
	"agent/upgrade"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
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
	Name             string `json:"name"`
	Host             string `json:"host"`
	Port             int    `json:"port"`
	ServiceType      string `json:"service_type"`
	DeploymentMethod string `json:"deployment_method"`
	Version          string `json:"version"`
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

	log.Printf("Registration successful! Agent ID: %s", regResp.ID)
	c.AgentID = regResp.ID
	if regResp.Token != "" {
		c.Token = regResp.Token
	}

	log.Printf("Saving configuration...")
	if err := c.Save(); err != nil {
		log.Printf("Failed to save configuration: %v", err)
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	log.Printf("Configuration saved successfully")
	return nil
}

// RegisterAgent 向中央服务器注册 agent
func (c *Config) RegisterAgent() error {
	log.Println("Agent not registered. Attempting to register...")

	localIP, err := getLocalIP()
	if err != nil {
		log.Printf("Warning: could not get local IP: %v. Falling back to agent_host.", err)
		localIP = c.AgentHost
	}

	// 优先使用配置中的 AgentIP
	hostIP := c.AgentIP
	if hostIP == "" {
		hostIP = localIP
	}

	log.Printf("Registering agent with name: %s, host: %s, port: %d, service_type: %s",
		c.AgentName, hostIP, c.AgentPort, c.ServiceType)

	reqBody := RegisterRequest{
		Name:             c.AgentName,
		Host:             hostIP,
		Port:             c.AgentPort,
		ServiceType:      c.ServiceType,
		DeploymentMethod: c.DeploymentMethod,
		Version:          AgentVersion,
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
	log.Printf("Service status: %s", status)

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
				log.Printf("System metrics collected: CPU %.2f%%, Memory %.2f%%, Disk %.2f%%, Network ↑%d B/s ↓%d B/s",
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

var heartbeatHTTPClient = &http.Client{Timeout: 10 * time.Second}

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
		}
		// 旧服务端不返回该字段，解码失败或缺省都视为关闭
		_ = json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&result)
		domainDiscovery.SetEnabled(c, result.DomainDiscoveryEnabled)

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
	log.Printf("Sending heartbeat...")

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
