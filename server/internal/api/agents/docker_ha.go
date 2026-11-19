package agents

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"configflow/internal/agents"
	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/installer"
	"configflow/internal/jsondecode"
	"configflow/internal/repo"
)

// Routes of backend/routes/docker_ha_installation.py (A09): Docker HA guide
// generation and narrowly scoped installer enrollment.

func registerDockerHA(r *httpx.Router) {
	r.POST("/api/agents/docker-oxidns-ha-guide", "agents.docker_oxidns_ha_guide", auth.Require(dockerOxidnsHAGuide))
	r.POST("/api/agents/docker-oxidns-ha-enroll/<session_id>", "agents.docker_oxidns_ha_enroll", dockerOxidnsHAEnroll)
}

const (
	dockerHABodyTooLarge     = "安装请求内容过大。"
	dockerHANeedsObject      = "安装请求需要 JSON 对象。"
	dockerHAEnrollmentNotice = "新装命令的本机安装凭据有效期为 24 小时；过期后重新生成。生成命令不会创建设备记录。"
	dockerHAEnrollmentNoBody = "安装登记无需提交设备信息，目标由生成命令时固定。"
)

// dockerHABody is _body(allow_check).
func dockerHABody(c *httpx.Ctx, allowCheck bool) (json.RawMessage, error) {
	limit := 16384
	if allowCheck {
		limit = 128 * 1024
	}
	var raw []byte
	if stream := c.Stream(); stream != nil {
		raw, _ = io.ReadAll(io.LimitReader(stream, int64(limit)+1))
	}
	if len(raw) > limit {
		return nil, &installer.EnrollmentError{Message: dockerHABodyTooLarge, Status: 413}
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(raw))
	var object map[string]json.RawMessage
	err := bindForce(c, &object)
	if err != nil {
		if len(raw) > 16384 {
			return nil, &installer.EnrollmentError{Message: dockerHABodyTooLarge, Status: 413}
		}
		return nil, &installer.ValueError{Message: dockerHANeedsObject}
	}
	isObject := object != nil
	var action string
	_ = jsondecode.Unmarshal(object["action"], &action)
	if len(raw) > 16384 && (!allowCheck || !isObject || action != "check") {
		return nil, &installer.EnrollmentError{Message: dockerHABodyTooLarge, Status: 413}
	}
	if !isObject {
		return nil, &installer.ValueError{Message: dockerHANeedsObject}
	}
	return json.Marshal(object)
}

// isPythonValueError reports whether `except ValueError` catches err.
func isPythonValueError(err error) bool {
	var ve *installer.ValueError
	var conflict *agents.DeploymentConflict
	return errors.As(err, &ve) || errors.As(err, &conflict) || repo.IsValueError(err)
}

// dockerHARespond is _respond(callback).
func dockerHARespond(callback func() (any, error)) (*httpx.Response, error) {
	result, err := callback()
	status := 200
	if err != nil {
		var enrollment *installer.EnrollmentError
		switch {
		case errors.As(err, &enrollment):
			result, status = map[string]any{"success": false, "message": enrollment.Message}, enrollment.Status
		case isPythonValueError(err):
			result, status = map[string]any{"success": false, "message": err.Error()}, 400
		default:
			return nil, err
		}
	}
	return httpx.JSON(result).WithStatus(status).WithHeader("Cache-Control", "no-store"), nil
}

func dockerOxidnsHAGuide(c *httpx.Ctx) (*httpx.Response, error) {
	return dockerHARespond(func() (any, error) {
		body, err := dockerHABody(c, true)
		if err != nil {
			return nil, err
		}
		raw := body
		var options struct {
			Action string `json:"action"`
			Mode   string `json:"mode"`
		}
		options.Action, options.Mode = "generate", "new"
		if err := jsondecode.Unmarshal(raw, &options); err != nil {
			return nil, err
		}
		result, err := installer.GenerateGuideJSON(raw, nil)
		if err != nil {
			return nil, err
		}
		if options.Action == "generate" && options.Mode == "new" {
			ctx := c.Context()
			var profileID string
			if header, present := c.Header("X-ConfigFlow-Profile"); present {
				profileID, err = repo.ResolveProfile(ctx, &header, nil)
			} else {
				profileID, err = repo.ResolveProfile(ctx, nil, nil)
			}
			if err != nil {
				return nil, err
			}
			repository, err := repo.GetRepository(ctx)
			if err != nil {
				return nil, err
			}
			capability, err := installer.IssueCapability(c.Context(), repository.DataDir(), raw, profileID)
			if err != nil {
				return nil, err
			}
			if result, err = installer.GenerateGuideJSON(raw, capability); err != nil {
				return nil, err
			}
			result.EnrollmentExpiresAt = new(capability.ExpiresAt)
			result.Warnings = append(result.Warnings, dockerHAEnrollmentNotice)
		}
		return result, nil
	})
}

func dockerOxidnsHAEnroll(c *httpx.Ctx) (*httpx.Response, error) {
	return dockerHARespond(func() (any, error) {
		body, err := dockerHABody(c, false)
		if err != nil {
			return nil, err
		}
		var all map[string]json.RawMessage
		if err := jsondecode.Unmarshal(body, &all); err != nil {
			return nil, err
		}
		if len(all) != 0 {
			return nil, &installer.ValueError{Message: dockerHAEnrollmentNoBody}
		}
		ctx := c.Context()
		repository, err := repo.GetRepository(ctx)
		if err != nil {
			return nil, err
		}
		manager, err := agents.Get(ctx)
		if err != nil {
			return nil, err
		}
		header, present := c.Header("Authorization")
		token, ok := auth.ParseBearerToken(header, present)
		identity, err := installer.EnrollIdentity(ctx, repository.DataDir(), manager, c.Param("session_id"), token, ok)
		if err != nil {
			return nil, err
		}
		return struct {
			Success bool `json:"success"`
			installer.AgentIdentity
		}{true, identity}, nil
	})
}
