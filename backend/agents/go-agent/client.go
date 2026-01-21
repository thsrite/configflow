package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

const AgentVersion = "1.0.5-go"

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
	
	log.Printf("Register request body: %s", string(jsonData))

	registerURL := fmt.Sprintf("%s/api/agents/register", c.ServerURL)
	log.Printf("Sending register request to: %s", registerURL)
	resp, err := http.Post(registerURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Failed to send register request: %v", err)
		return nil, fmt.Errorf("failed to send register request: %w", err)
	}
	defer resp.Body.Close()
	
	log.Printf("Register request response status: %d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		// 读取响应体以便记录错误信息
		respBody := make([]byte, 1024)
		n, _ := resp.Body.Read(respBody)
		log.Printf("Register request failed with status code: %d, response: %s", resp.StatusCode, string(respBody[:n]))
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
	c.Token = regResp.Token

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
	status := getServiceStatus(c.ServiceName)
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

// sendHeartbeatRequest 发送心跳请求到服务器
func (c *Config) sendHeartbeatRequest(jsonData []byte) error {
	heartbeatURL := fmt.Sprintf("%s/api/agents/%s/heartbeat", c.ServerURL, c.AgentID)
	log.Printf("Sending heartbeat to: %s", heartbeatURL)
	
	req, err := http.NewRequest("POST", heartbeatURL, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Error: Failed to create heartbeat request: %v", err)
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error: Failed to send heartbeat: %v", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		status := getServiceStatus(c.ServiceName)
		log.Printf("Heartbeat sent successfully (service status: %s)", status)
	} else {
		// 读取响应体以便记录错误信息
		respBody := make([]byte, 1024)
		n, _ := resp.Body.Read(respBody)
		log.Printf("Error: Heartbeat failed with status code: %d, response: %s", resp.StatusCode, string(respBody[:n]))
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