package agents

import (
	"encoding/json"
	"errors"
	"strings"

	"configflow/internal/auth"
	"configflow/internal/deploy"
	"configflow/internal/httpx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rules"
)

type deployHandler struct {
	deps Deps
}

func (h *deployHandler) register(r *httpx.Router) {
	r.GET("/api/agents/<agent_id>/config", "agents.get_agent_config", h.getAgentConfig)
	r.POST("/api/agents/<agent_id>/push-config", "agents.push_config_to_agent", auth.Require(h.pushAgentConfig))
	r.POST("/api/agents/<agent_id>/generate-config", "agents.generate_final_agent_config", auth.Require(h.generateAgentFinal))
	r.GET("/api/agents/<agent_id>/generated-config/<preview_id>/download", "agents.download_final_agent_config", auth.Require(h.generatedFile(true)))
	r.GET("/api/agents/<agent_id>/generated-config/<preview_id>/file", "agents.read_final_agent_file", auth.Require(h.generatedFile(false)))
	r.GET("/api/agents/<agent_id>/deployments/<deployment_id>", "agents.get_agent_deployment", auth.Require(h.deploymentAction("poll")))
	r.POST("/api/agents/<agent_id>/deployments/<deployment_id>/activate", "agents.activate_agent_deployment", auth.Require(h.deploymentAction("activate")))
	r.POST("/api/agents/<agent_id>/deployments/<deployment_id>/rollback", "agents.rollback_agent_deployment", auth.Require(h.deploymentAction("rollback")))
	r.POST("/api/agents/<agent_id>/deployments/<deployment_id>/recover", "agents.recover_agent_deployment", auth.Require(h.deploymentAction("recover")))
}
func (h *deployHandler) getAgentConfig(c *httpx.Ctx) (*httpx.Response, error) {
	token, _ := c.Arg("token")
	if !auth.IsTokenWithinLength(token) {
		return failure(401, "Token required"), nil
	}
	m, err := h.deps.manager(c.Context())
	if err != nil {
		return nil, err
	}
	a, err := m.ReadAgentByToken(c.Context(), token)
	if err != nil {
		return nil, err
	}
	if a == nil || a.ID != c.Param("agent_id") {
		return failure(401, "Invalid token"), nil
	}
	source, err := loadContentSource(c.Context(), m, a, nil)
	if err != nil {
		return failure(409, "Agent profile unavailable: "+err.Error()), nil
	}
	target, err := deploy.NewTypedTarget(a)
	if err != nil {
		return nil, err
	}
	result, err := deploy.GenerateAgentConfig(c.Context(), source, target, strings.TrimRight(c.URLRoot(), "/"))
	if err != nil {
		var strategy *rules.StrategyReferenceError
		if errors.As(err, &strategy) {
			return failure(400, err.Error()), nil
		}
		if errors.Is(err, repo.ErrRepository) {
			return failure(409, "Agent profile unavailable: "+err.Error()), nil
		}
		return failure(500, err.Error()), nil
	}
	return httpx.JSON(map[string]any{"success": true, "profile_id": source.ProfileID(), "content": result.Content, "md5": result.MD5, "version": result.Version}), nil
}

func deployResponse(r deploy.Response) (*httpx.Response, error) {
	if r.Err != nil {
		if response, handled := deviceResponseFailure(r.Err); handled {
			return response, nil
		}
		return nil, r.Err
	}
	out := httpx.JSONStatus(r.Status, r.Body)
	if r.NoStore {
		out.NoStore()
	}
	return out, nil
}
func (h *deployHandler) pushAgentConfig(c *httpx.Ctx) (*httpx.Response, error) {
	m, e := h.deps.manager(c.Context())
	if e != nil {
		return nil, e
	}
	var body json.RawMessage
	if !c.BindSilent(&body) {
		body = json.RawMessage("null")
	}
	proto, _ := c.Header("X-Forwarded-Proto")
	host, _ := c.Header("X-Forwarded-Host")
	return deployResponse(deploy.PushConfig(c.Context(), m, c.Param("agent_id"), deploy.PushRequest{Body: body, Scheme: c.Scheme(), Host: c.Host(), ForwardedProto: proto, ForwardedHost: host}))
}
func (h *deployHandler) generateAgentFinal(c *httpx.Ctx) (*httpx.Response, error) {
	m, e := h.deps.manager(c.Context())
	if e != nil {
		return nil, e
	}
	var body json.RawMessage
	if !c.BindSilent(&body) {
		body = json.RawMessage("null")
	}
	return deployResponse(deploy.GenerateFinal(c.Context(), m, c.Param("agent_id"), deploy.GenerateRequest{Body: body, HostURL: c.HostURL()}))
}
func (h *deployHandler) deploymentAction(action string) httpx.HandlerFunc {
	return func(c *httpx.Ctx) (*httpx.Response, error) {
		m, e := h.deps.manager(c.Context())
		if e != nil {
			return nil, e
		}
		var result *model.Deployment
		switch action {
		case "poll":
			result, e = m.GetDeploymentModel(c.Context(), c.Param("agent_id"), c.Param("deployment_id"))
		case "activate":
			result, e = m.ActivateDeploymentModel(c.Context(), c.Param("agent_id"), c.Param("deployment_id"))
		case "rollback":
			result, e = m.RollbackDeploymentModel(c.Context(), c.Param("agent_id"), c.Param("deployment_id"))
		case "recover":
			result, e = m.RecoverDeploymentModel(c.Context(), c.Param("agent_id"), c.Param("deployment_id"))
		}
		if e != nil {
			if response, handled := deviceResponseFailure(e); handled {
				return response, nil
			}
			return nil, e
		}
		if action == "poll" {
			return deployResponse(deploy.DeploymentPollResponse(result))
		}
		response := deploy.DeploymentResponse(result)
		if action == "recover" {
			response = acceptedRecovery(response, result)
		}
		return deployResponse(response)
	}
}
func (h *deployHandler) generatedFile(download bool) httpx.HandlerFunc {
	return func(c *httpx.Ctx) (*httpx.Response, error) {
		m, e := h.deps.manager(c.Context())
		if e != nil {
			return nil, e
		}
		pv, e := h.deps.loadPreview(m, c.Param("preview_id"), c.Param("agent_id"))
		if e != nil {
			return previewError(e)
		}
		var data []byte
		mime := "application/octet-stream"
		name := ""
		if download {
			data, e = pv.Zip()
			mime = "application/zip"
			name = pv.ServiceType + "-final-config.zip"
		} else {
			path, _ := c.Arg("path")
			var ok bool
			data, ok, e = pv.File(path)
			if e == nil && !ok {
				return failure(404, msgGeneratedFileMissing), nil
			}
		}
		if e != nil {
			return nil, e
		}
		out, e := httpx.File(httpx.FileOptions{Data: data, MimeType: mime, AsAttachment: download, DownloadName: name})
		if e != nil {
			return nil, e
		}
		return out.NoStore(), nil
	}
}
func previewError(e error) (*httpx.Response, error) {
	var changed *deploy.PreviewChanged
	if errors.As(e, &changed) {
		return httpx.JSONStatus(409, map[string]any{"success": false, "code": "preview_changed", "message": e.Error()}), nil
	}
	return nil, e
}

// acceptedRecovery answers an Agent-accepted recovery (task now rolling_back,
// no device error code) with 202 like an accepted manual rollback. Python
// recorded it as success=false and answered 500, so the Agents page reported
// 「无法重新恢复」 and stopped tracking while the device was recovering (lab L2).
func acceptedRecovery(response deploy.Response, result *model.Deployment) deploy.Response {
	if response.Err == nil && result != nil && result.Status == "rolling_back" && response.Status == 500 {
		response.Status = 202
	}
	return response
}
