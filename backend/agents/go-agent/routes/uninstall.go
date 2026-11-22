package routes

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/exec"
	"time"

	"agent/svc"
)

// UninstallHandler 卸载功能处理程序
func UninstallHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var nativeInstance nativeAIOInstallation
		if cfg.NativeAIO {
			var err error
			nativeInstance, err = nativeAIOInstallationFor(cfg)
			if err == nil {
				err = nativeInstance.validateDirectory()
			}
			if err != nil {
				deploymentFailure(w, http.StatusConflict, err)
				return
			}
		}
		release, err := AcquireServiceOperation(cfg)
		if err != nil {
			deploymentFailure(w, http.StatusConflict, err)
			return
		}
		if err = deploymentBlocked(cfg); err != nil {
			release()
			deploymentFailure(w, http.StatusConflict, err)
			return
		}
		if err = checkLinuxGatewayUninstall(cfg); err != nil {
			release()
			deploymentFailure(w, http.StatusConflict, err)
			return
		}
		if err = checkOxiDNSHAUninstall(cfg); err != nil {
			release()
			deploymentFailure(w, http.StatusConflict, err)
			return
		}
		log.Println("Received uninstall request.")

		// 立即返回成功响应，避免连接中断
		JsonResponse(w, http.StatusOK, map[string]string{
			"success": "true",
			"message": "Uninstall process started. The agent will be removed in a few seconds.",
		})

		// 在 goroutine 中异步执行卸载操作
		go func() {
			defer release()
			// 等待 2 秒，确保响应已经发送
			log.Println("Waiting 2 seconds before starting uninstall...")
			time.Sleep(2 * time.Second)

			log.Println("Starting uninstall process...")

			// 检测系统类型和 init 系统
			initSystem := detectInitSystem()
			log.Printf("Detected init system: %s", initSystem)

			if err := RemoveRecoveryInstallation(cfg); err != nil {
				log.Printf("Uninstall stopped: recovery boot gate cleanup failed: %v", err)
				return
			}
			if cfg.NativeAIO {
				if err := nativeInstance.startUninstall(); err != nil {
					log.Printf("Native AIO uninstall failed: %v", err)
				}
				return
			}

			// 1. 删除二进制文件（先删除，避免服务停止后无法删除）
			log.Printf("Removing binary: /usr/local/bin/configflow-agent")
			if err := os.Remove("/usr/local/bin/configflow-agent"); err != nil && !os.IsNotExist(err) {
				log.Printf("Warning: failed to remove binary: %v", err)
			}

			// 2. 删除 agent 配置目录
			agentDir := "/opt/configflow-agent"
			log.Printf("Removing agent directory: %s", agentDir)
			if err := os.RemoveAll(agentDir); err != nil {
				log.Printf("Warning: failed to remove agent directory: %v", err)
			}

			// 3. 删除 ConfigFlow Agent 的日志文件
			log.Printf("Removing log file: /var/log/configflow-agent.log")
			if err := os.Remove("/var/log/configflow-agent.log"); err != nil && !os.IsNotExist(err) {
				log.Printf("Warning: failed to remove log file: %v", err)
			}

			// 4. 删除服务文件和停止服务
			if initSystem == "supervisor" {
				// Docker 镜像：停止核心，最后停止 Agent 自己（s6 不再拉起）
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				for _, program := range []string{cfg.ServiceName, "agent-" + cfg.ServiceName} {
					log.Printf("Stopping container program %s", program)
					if err := containerServices.Stop(ctx, program); err != nil {
						log.Printf("Warning: failed to stop %s: %v", program, err)
					}
				}
				cancel()
			} else if initSystem == "systemd" {
				// systemd: 先删除服务文件
				log.Printf("Removing systemd service file...")
				os.Remove("/etc/systemd/system/configflow-agent.service")

				// 禁用并停止服务
				executeCommand("systemctl disable configflow-agent")
				executeCommand("systemctl stop configflow-agent")
				executeCommand("systemctl daemon-reload")
			} else if initSystem == "openrc" {
				// OpenRC: 先删除服务文件
				log.Printf("Removing OpenRC service file...")
				os.Remove("/etc/init.d/configflow-agent")

				// 从运行级别移除并停止服务
				executeCommand("rc-update del configflow-agent default")
				executeCommand("rc-service configflow-agent stop")
				executeCommand("rc-update -u")
			}

			// 注意：不删除服务（mihomo/mosdns）的配置文件和日志
			// 因为这些服务可能是用户独立安装和配置的
			// Agent 只是管理这些服务，卸载 Agent 不应该影响服务本身

			log.Println("Uninstall completed successfully.")
			log.Printf("Note: Service (%s) configuration and binaries are preserved", cfg.ServiceName)
		}() // 结束 goroutine
	}
}

// detectInitSystem 检测系统使用的 init 系统
func detectInitSystem() string {
	// Docker 镜像（s6 管理 Agent 与核心）
	if svc.InImage() {
		return "supervisor"
	}

	// 检查 systemctl 是否存在
	if _, err := exec.LookPath("systemctl"); err == nil {
		return "systemd"
	}

	// 检查 rc-service 是否存在
	if _, err := exec.LookPath("rc-service"); err == nil {
		return "openrc"
	}

	return "unknown"
}

// executeCommand 执行shell命令
func executeCommand(command string) ([]byte, error) {
	log.Printf("Executing command: %s", command)
	cmd := exec.Command("sh", "-c", command)
	return cmd.CombinedOutput()
}
