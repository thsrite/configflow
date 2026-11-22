package routes

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strings"
)

// RestartService 执行服务重启命令，返回成功命令的输出。
// Docker 镜像的重启命令（configflow-agent svc restart）对未运行的程序直接启动。
func RestartService(cfg *Config) ([]byte, error) {
	log.Print("Executing restart command")
	output, err := executeURLCommand(cfg.RestartCommand, cfg.Token)
	if err != nil {
		log.Print("Restart command failed")
		return nil, err
	}
	ReconcileLinuxGateway(cfg)
	return output, nil
}

// RestartHandler 处理服务重启请求
func RestartHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		release, err := AcquireServiceOperation(cfg)
		if err != nil {
			deploymentFailure(w, http.StatusConflict, err)
			return
		}
		defer release()
		if err = deploymentBlocked(cfg); err != nil {
			deploymentFailure(w, http.StatusConflict, err)
			return
		}
		log.Println("Received restart request.")
		log.Print("Executing restart command")

		_, err = executeURLCommand(cfg.RestartCommand, cfg.Token)
		if err != nil {
			log.Print("Restart command failed")
			deploymentFailure(w, http.StatusInternalServerError, diagnosticFailure("Restart failed", err, nil))
			return
		}

		log.Println("Service restarted successfully.")
		ReconcileLinuxGateway(cfg)
		JsonResponse(w, http.StatusOK, map[string]string{"success": "true", "message": "Service restarted"})
	}
}

// executeURLCommand 执行URL或shell格式的命令。
func executeURLCommand(command string, secrets ...string) ([]byte, error) {
	log.Print("Executing configured command")

	if strings.HasPrefix(command, "http://") || strings.HasPrefix(command, "https://") {
		log.Print("Executing URL command")
		resp, err := http.Post(command, "application/json", nil)
		if err != nil {
			log.Print("Failed to execute URL command")
			return nil, diagnosticFailure("failed to execute URL command", err, nil, secrets...)
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			var output diagnosticOutput
			_, readErr := io.Copy(&output, resp.Body)
			log.Printf("URL command failed with status %d", resp.StatusCode)
			return nil, diagnosticFailure(fmt.Sprintf("URL command failed with status %d", resp.StatusCode), readErr, output.Bytes(), secrets...)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Print("Failed to read URL command response")
			return nil, diagnosticFailure("failed to read URL command response", err, nil, secrets...)
		}

		log.Print("URL command executed successfully")
		return body, nil
	}

	log.Print("Executing shell command")

	cmd := exec.Command("sh", "-c", command)
	output, err := runDiagnosticCommand(nil, cmd, "shell command", secrets...)
	if err != nil {
		log.Print("Shell command failed")
		return nil, err
	}
	log.Print("Shell command executed successfully")
	return output, nil
}
