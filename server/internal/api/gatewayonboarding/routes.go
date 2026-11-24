// Package gatewayonboarding translates authenticated, profile-scoped wizard
// requests. Preparation, cancellation and persistence belong to onboarding.
package gatewayonboarding

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"configflow/internal/auth"
	"configflow/internal/gateway/onboarding"
	"configflow/internal/httpx"
	"configflow/internal/repo"
	"configflow/internal/routersplit"
)

const maxBody = 262144

// Register adds the wizard's read, preview and atomic save routes.
func Register(r *httpx.Router) {
	Handler{DNSLoop: onboarding.CheckDNSLoop}.Register(r)
}

// Handler registers routes with their Repository, pure DNS-check and ownership
// collaborators. Register supplies the shared checker in production.
type Handler struct {
	Repo            *repo.Repository
	DNSLoop         onboarding.DNSLoopChecker
	AdoptOwnedWrite onboarding.AdoptOwnedWrite
	explicitAdopter bool
}

// NewHandler constructs a Handler with explicit collaborators. A nil
// AdoptOwnedWrite fails closed with ErrSetupUnavailable when a pending setup
// session exists, without falling back to the package-level hook.
func NewHandler(r *repo.Repository, dnsLoop onboarding.DNSLoopChecker, adopt onboarding.AdoptOwnedWrite) *Handler {
	return &Handler{
		Repo:            r,
		DNSLoop:         dnsLoop,
		AdoptOwnedWrite: adopt,
		explicitAdopter: true,
	}
}

// Register adds all onboarding methods with the same authentication policy.
func (h Handler) Register(r *httpx.Router) {
	r.Handle([]string{"GET", "POST"}, "/api/profiles/<profile_id>/gateway-onboarding", "profiles.gateway_onboarding", auth.Require(h.handle))
	r.POST("/api/profiles/<profile_id>/gateway-onboarding/preview", "profiles.gateway_onboarding", auth.Require(h.handle))
}
func body(c *httpx.Ctx) (json.RawMessage, error) {
	if c.Request.ContentLength > maxBody {
		return nil, errors.New(msgSize)
	}
	data, err := io.ReadAll(io.LimitReader(c.Request.Body, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBody {
		return nil, errors.New(msgSize)
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(data))
	// Python explicitly json.loads(request.stream) regardless of media type.
	previous := c.Request.Header.Get("Content-Type")
	c.Request.Header.Set("Content-Type", "application/json")
	defer c.Request.Header.Set("Content-Type", previous)
	var raw json.RawMessage
	if !c.BindSilent(&raw) {
		return nil, errors.New(msgJSON)
	}
	return raw, nil
}
func (h Handler) handle(c *httpx.Ctx) (*httpx.Response, error) {
	r := h.Repo
	var err error
	if r == nil {
		r, err = repo.GetRepository(c.Context())
		if err != nil {
			return nil, err
		}
	}
	op := onboarding.Operation{ProfileID: c.Param("profile_id"), BaseURL: strings.TrimRight(c.HostURL(), "/"), Write: c.Request.Method == "POST", Preview: strings.HasSuffix(c.Request.URL.Path, "/preview")}
	if op.Write {
		op.Body, err = body(c)
		if err != nil {
			return httpx.Message(400, false, err.Error()).NoStore(), nil
		}
	}
	var service *onboarding.Service
	if h.explicitAdopter {
		service = onboarding.New(r, onboarding.ServiceOptions{DNSLoop: h.DNSLoop, AdoptOwnedWrite: h.AdoptOwnedWrite})
	} else {
		service = &onboarding.Service{Repository: r, DNSLoop: h.DNSLoop, AdoptOwnedWrite: h.AdoptOwnedWrite}
	}
	data, err := service.Execute(c.Context(), op)
	if err != nil {
		if errors.Is(err, onboarding.ErrSetupUnavailable) {
			return httpx.Message(503, false, onboarding.ErrSetupUnavailable.Error()).NoStore(), nil
		}
		var blocked *onboarding.Blocked
		if errors.As(err, &blocked) {
			report := blocked.Report
			report.Success = false
			report.Message = blocked.Error()
			return httpx.JSONStatus(400, report).NoStore(), nil
		}
		var split *routersplit.RouterSplitError
		if errors.As(err, &split) {
			return httpx.JSONStatus(split.Status, struct {
				Success bool   `json:"success"`
				Code    string `json:"code"`
				Message string `json:"message"`
			}{false, split.Code, split.Error()}).NoStore(), nil
		}
		if onboarding.IsInputError(err) {
			return httpx.Message(400, false, err.Error()).NoStore(), nil
		}
		return nil, err
	}
	return httpx.JSON(data).NoStore(), nil
}
