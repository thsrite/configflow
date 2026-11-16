package agents

import (
	"bytes"
	"errors"
	"io"

	"configflow/internal/agents"
	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/installer"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

// Routes of backend/routes/docker_ha_installation.py (A09): Docker HA guide
// generation and narrowly scoped installer enrollment.

func registerDockerHA(r *httpx.Router) {
	r.POST("/api/agents/docker-oxidns-ha-guide", "agents.docker_oxidns_ha_guide", auth.Require(dockerOxidnsHAGuide))
	r.POST("/api/agents/docker-oxidns-ha-enroll/<session_id>", "agents.docker_oxidns_ha_enroll", dockerOxidnsHAEnroll)
}

const (
	dockerHABodyTooLarge = "安装请求内容过大。"
	dockerHANeedsObject  = "安装请求需要 JSON 对象。"
)

// dockerHABody is _body(allow_check).
func dockerHABody(c *httpx.Ctx, allowCheck bool) (*jsonx.Object, error) {
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
	value, err := c.JSONForceError()
	if err != nil {
		if len(raw) > 16384 {
			return nil, &installer.EnrollmentError{Message: dockerHABodyTooLarge, Status: 413}
		}
		return nil, &installer.ValueError{Message: dockerHANeedsObject}
	}
	object, isObject := value.(*jsonx.Object)
	if len(raw) > 16384 && (!allowCheck || !isObject || object.GetOr("action", nil) != "check") {
		return nil, &installer.EnrollmentError{Message: dockerHABodyTooLarge, Status: 413}
	}
	if !isObject {
		return nil, &installer.ValueError{Message: dockerHANeedsObject}
	}
	return object, nil
}

// isPythonValueError reports whether `except ValueError` catches err.
func isPythonValueError(err error) bool {
	var ve *installer.ValueError
	var conflict *agents.DeploymentConflict
	return errors.As(err, &ve) || errors.As(err, &conflict) || repo.IsValueError(err)
}

// dockerHARespond is _respond(callback).
func dockerHARespond(callback func() (*jsonx.Object, error)) (*httpx.Response, error) {
	result, err := callback()
	status := 200
	if err != nil {
		var enrollment *installer.EnrollmentError
		switch {
		case errors.As(err, &enrollment):
			result, status = jsonx.Obj("success", false, "message", enrollment.Message), enrollment.Status
		case isPythonValueError(err):
			result, status = jsonx.Obj("success", false, "message", err.Error()), 400
		default:
			return nil, err
		}
	}
	return httpx.JSON(result).WithStatus(status).WithHeader("Cache-Control", "no-store"), nil
}

func dockerOxidnsHAGuide(c *httpx.Ctx) (*httpx.Response, error) {
	return dockerHARespond(func() (*jsonx.Object, error) {
		body, err := dockerHABody(c, true)
		if err != nil {
			return nil, err
		}
		result, err := installer.GenerateGuide(body, nil)
		if err != nil {
			return nil, err
		}
		if body.GetOr("action", "generate") == "generate" && body.GetOr("mode", "new") == "new" {
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
			capability, err := installer.IssueEnrollment(repository.DataDir(), body, profileID)
			if err != nil {
				return nil, err
			}
			if result, err = installer.GenerateGuide(body, capability); err != nil {
				return nil, err
			}
			result.Set("enrollment_expires_at", capability.ExpiresAt)
			result.Set("warnings", append(result.GetList("warnings"), "新装命令的本机安装凭据有效期为 24 小时；过期后重新生成。生成命令不会创建设备记录。"))
		}
		return result, nil
	})
}

func dockerOxidnsHAEnroll(c *httpx.Ctx) (*httpx.Response, error) {
	return dockerHARespond(func() (*jsonx.Object, error) {
		body, err := dockerHABody(c, false)
		if err != nil {
			return nil, err
		}
		if body.Len() != 0 {
			return nil, &installer.ValueError{Message: "安装登记无需提交设备信息，目标由生成命令时固定。"}
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
		identity, err := installer.Enroll(ctx, repository.DataDir(), manager, c.Param("session_id"), token, ok)
		if err != nil {
			return nil, err
		}
		result := jsonx.Obj("success", true)
		result.Update(identity)
		return result, nil
	})
}
