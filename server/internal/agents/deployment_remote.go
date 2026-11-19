package agents

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/redact"
)

// ErrAgentEndpoint means the stored Agent lacks a usable endpoint or credential.
var ErrAgentEndpoint = errors.New(msgAgentEndpoint)

// CallAgent sends raw request bytes with the original stored string credential.
// It never derives authorization from an R5 scalar projection.
func (m *Manager) CallAgent(ctx context.Context, a *model.Agent, method, path string, header http.Header, body []byte, timeout Timeout) (*Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if a == nil {
		return nil, ErrAgentEndpoint
	}
	host, hostOK := storedText(a.MemberJSON("host"))
	token, tokenOK := storedText(a.MemberJSON("token"))
	portRaw, portPresent, portErr := a.MemberJSON("port")
	if !hostOK || !tokenOK || !portPresent || portErr != nil {
		return nil, ErrAgentEndpoint
	}
	h := make(http.Header)
	h.Set("Authorization", "Bearer "+token)
	for key, values := range header {
		h[http.CanonicalHeaderKey(key)] = append([]string(nil), values...)
	}
	port := strings.TrimSpace(string(portRaw))
	if text, valid := storedText(portRaw, portPresent, nil); valid {
		port = text
	}
	response, err := m.httpClient().Do(ctx, Request{Method: method, URL: "http://" + host + ":" + port + path, Header: h, Body: body, Timeout: timeout, Latin1Headers: true})
	if err != nil {
		if !IsTransportError(err) && !errors.Is(err, ErrHeaderEncoding) {
			err = &TransportError{Err: err}
		}
	}
	return response, err
}

// DeploymentRequestModel sends one deployment request, sanitizes the reply and
// retries only the explicit busy rejection which proves no action was accepted.
func (m *Manager) DeploymentRequestModel(ctx context.Context, a *model.Agent, method, suffix string, header http.Header, body []byte, timeout *Timeout) (*model.Deployment, error) {
	if a == nil {
		return nil, ErrAgentEndpoint
	}
	requestTimeout := Pair(5, 30)
	if timeout != nil {
		requestTimeout = *timeout
	}
	retryBusy := method == http.MethodPost && (suffix == "deployments" || strings.HasPrefix(suffix, "deployments/") && (strings.HasSuffix(suffix, "/activate") || strings.HasSuffix(suffix, "/rollback") || strings.HasSuffix(suffix, "/recover")))
	deadline := m.monotonic() + 2
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		response, err := m.CallAgent(ctx, a, method, "/api/"+suffix, header, body, requestTimeout)
		if err != nil {
			return nil, err
		}
		var result model.Deployment
		var object model.Fields
		data := response.jsonData()
		if !json.Valid(data) {
			if response.StatusCode == 200 {
				return nil, ErrInvalidAgentReply
			}
			failure, e := deploymentFailure("Agent returned HTTP "+strconv.Itoa(response.StatusCode), 0)
			if e != nil {
				return nil, e
			}
			result = *failure
		} else if json.Unmarshal(data, &object) != nil || json.Unmarshal(data, &result) != nil {
			if response.StatusCode == 200 {
				return nil, ErrInvalidAgentReply
			}
			failure, e := deploymentFailure("Invalid Agent response", 0)
			if e != nil {
				return nil, e
			}
			result = *failure
		}
		if response.StatusCode >= 400 {
			if err := result.SetJSON("success", []byte(`false`)); err != nil {
				return nil, err
			}
			raw, _ := json.Marshal(response.StatusCode)
			if err := result.SetJSON("http_status", raw); err != nil {
				return nil, err
			}
		}
		busy := response.StatusCode == 409 && originalDeploymentText(&result, "message") == busyMessage
		secrets := agentDiagnosticSecrets(a)
		for _, key := range []string{"message", "error", "error_details", "rollback_error", "rollback_error_details", "rollback_message", "failed_stage"} {
			raw, present, err := result.MemberJSON(key)
			if err != nil {
				return nil, err
			}
			if !present {
				continue
			}
			value, _ := storedText(raw, present, nil)
			encoded, err := json.Marshal(redact.DiagnosticText(value, secrets, 0))
			if err != nil {
				return nil, err
			}
			if err := result.SetJSON(key, encoded); err != nil {
				return nil, err
			}
		}
		report, err := SanitizeDNSReport(&result, secrets)
		if err != nil {
			return nil, err
		}
		if err := result.Clear("dns_checks"); err != nil {
			return nil, err
		}
		if err := result.Clear("dns_checked_at"); err != nil {
			return nil, err
		}
		if err := applyDNSReport(&result, report); err != nil {
			return nil, err
		}
		remaining := deadline - m.monotonic()
		if !retryBusy || !busy || attempt == 20 || remaining <= 0 {
			return &result, nil
		}
		delay := time.Duration(min(0.1, remaining) * float64(time.Second))
		if m.sleep != nil {
			m.sleep(delay)
		} else {
			timer := lifecycle.ClockFrom(ctx).NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C():
			}
			timer.Stop()
		}
	}
}

func (m *Manager) recordDeploymentReply(ctx context.Context, agentID, id string, state *model.Deployment, options RecordOptions) (*model.Deployment, error) {
	raw, err := model.Marshal(state)
	if err != nil {
		return nil, err
	}
	patch, err := DecodeDeploymentUpdate(raw)
	if err != nil {
		return nil, err
	}
	return m.RecordDeploymentModel(ctx, agentID, id, patch, options)
}
