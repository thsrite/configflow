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

// CallAgent sends raw request bytes with the stored credential.
func (m *Manager) CallAgent(ctx context.Context, a *model.Agent, method, path string, header http.Header, body []byte, timeout Timeout) (*Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// A missing (empty) host or token is no endpoint: never call the
	// panel's own address with the Agent's credential.
	if a == nil || a.Host == "" || a.Token == "" {
		return nil, ErrAgentEndpoint
	}
	h := make(http.Header)
	h.Set("Authorization", "Bearer "+a.Token)
	for key, values := range header {
		h[http.CanonicalHeaderKey(key)] = append([]string(nil), values...)
	}
	port := strconv.FormatInt(int64(a.Port), 10)
	// O9 round 2 L2: Agent protocol endpoints never redirect; refuse 3xx
	// following so a compromised Agent cannot turn the panel into an SSRF proxy.
	response, err := m.httpClient().Do(ctx, Request{Method: method, URL: "http://" + a.Host + ":" + port + path, Header: h, Body: body, Timeout: timeout, NoRedirects: true, Latin1Headers: true})
	if err != nil {
		if !IsTransportError(err) && !errors.Is(err, ErrHeaderEncoding) {
			err = &TransportError{Err: err}
		}
	}
	return response, err
}

// DeploymentReply is a deployment response: a sanitized reply of an Agent
// endpoint or a journal the panel holds, plus response-only members such as
// "existing". The members a deployment journal declares are read through the
// embedded typed view; the reply itself, including members the journal does
// not declare (capabilities, preview content, listener data), is kept in
// order and written by MarshalJSON. Change it only through Set and Remove.
type DeploymentReply struct {
	model.Deployment
	members model.OrderedMap[json.RawMessage]
}

// UnmarshalJSON decodes a reply object; its typed view is decoded leniently.
func (r *DeploymentReply) UnmarshalJSON(data []byte) error {
	if err := requireJSONObject(data); err != nil {
		return err
	}
	var members model.OrderedMap[json.RawMessage]
	if err := json.Unmarshal(data, &members); err != nil {
		return err
	}
	var typed model.Deployment
	if err := json.Unmarshal(data, &typed); err != nil {
		return err
	}
	r.Deployment, r.members = typed, members
	return nil
}

// MarshalJSON writes the reply members in order.
func (r DeploymentReply) MarshalJSON() ([]byte, error) { return r.members.MarshalJSON() }

// MemberJSON returns one reply member as received (or as set).
func (r *DeploymentReply) MemberJSON(name string) (json.RawMessage, bool) {
	if r == nil {
		return nil, false
	}
	return r.members.Get(name)
}

// Set replaces or appends one reply member and its typed view.
func (r *DeploymentReply) Set(name string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if _, _, err := model.DecodeStoredMember(&r.Deployment, name, raw, name); err != nil {
		return err
	}
	r.members.Set(name, raw)
	return nil
}

// Remove drops one reply member and clears its typed view.
func (r *DeploymentReply) Remove(name string) error {
	if _, _, err := model.DecodeStoredMember(&r.Deployment, name, []byte("null"), name); err != nil {
		return err
	}
	r.members.Delete(name)
	return nil
}

// journalReply is the reply for a deployment journal the panel holds.
func journalReply(state *model.Deployment) (*DeploymentReply, error) {
	reply := &DeploymentReply{Deployment: *state}
	members, err := model.EncodeMembers(state)
	if err != nil {
		return nil, err
	}
	for _, member := range members {
		reply.members.Set(member.Name, member.Value)
	}
	return reply, nil
}

// replyFailure is a reply the panel creates for an unusable Agent answer.
func replyFailure(message string) *DeploymentReply {
	reply := &DeploymentReply{}
	_ = reply.Set("success", false)
	_ = reply.Set("message", message)
	return reply
}

// DeploymentRequestModel sends one deployment request, sanitizes the reply and
// retries only the explicit busy rejection which proves no action was accepted.
func (m *Manager) DeploymentRequestModel(ctx context.Context, a *model.Agent, method, suffix string, header http.Header, body []byte, timeout *Timeout) (*DeploymentReply, error) {
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
		result := &DeploymentReply{}
		data := response.jsonData()
		if !json.Valid(data) {
			if response.StatusCode == 200 {
				return nil, ErrInvalidAgentReply
			}
			result = replyFailure("Agent returned HTTP " + strconv.Itoa(response.StatusCode))
		} else if json.Unmarshal(data, result) != nil {
			if response.StatusCode == 200 {
				return nil, ErrInvalidAgentReply
			}
			result = replyFailure("Invalid Agent response")
		}
		if response.StatusCode >= 300 {
			if err := result.Set("success", false); err != nil {
				return nil, err
			}
			if err := result.Set("http_status", response.StatusCode); err != nil {
				return nil, err
			}
			if response.StatusCode < 400 && textOf(result.Message) == "" && textOf(result.Error) == "" {
				if err := result.Set("message", "Agent returned HTTP "+strconv.Itoa(response.StatusCode)); err != nil {
					return nil, err
				}
			}
		}
		busy := response.StatusCode == 409 && textOf(result.Message) == busyMessage
		secrets := agentDiagnosticSecrets(a)
		for _, key := range []string{"message", "error", "error_details", "rollback_error", "rollback_error_details", "rollback_message", "failed_stage"} {
			raw, present := result.MemberJSON(key)
			if !present {
				continue
			}
			var value string
			if json.Unmarshal(raw, &value) != nil {
				value = ""
			}
			if err := result.Set(key, redact.DiagnosticText(value, secrets, 0)); err != nil {
				return nil, err
			}
		}
		report, err := SanitizeDNSReport(&result.Deployment, secrets)
		if err != nil {
			return nil, err
		}
		if err := result.Remove("dns_checks"); err != nil {
			return nil, err
		}
		if err := result.Remove("dns_checked_at"); err != nil {
			return nil, err
		}
		if report.Checks != nil {
			if err := result.Set("dns_checks", *report.Checks); err != nil {
				return nil, err
			}
		}
		if report.CheckedAt != nil {
			if err := result.Set("dns_checked_at", *report.CheckedAt); err != nil {
				return nil, err
			}
		}
		remaining := deadline - m.monotonic()
		if !retryBusy || !busy || attempt == 20 || remaining <= 0 {
			return result, nil
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

// textOf dereferences an optional text member.
func textOf(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (m *Manager) recordDeploymentReply(ctx context.Context, agentID, id string, reply *DeploymentReply, options RecordOptions) (*model.Deployment, error) {
	raw, err := json.Marshal(reply)
	if err != nil {
		return nil, err
	}
	patch, err := DecodeDeploymentUpdate(raw)
	if err != nil {
		return nil, err
	}
	return m.RecordDeploymentModel(ctx, agentID, id, patch, options)
}
