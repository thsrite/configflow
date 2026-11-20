package deploy

import (
	"bytes"
	"configflow/internal/agents"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/provider"
	"configflow/internal/repo"
	"configflow/internal/yamlx"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// PushRequest contains the original HTTP inputs used to choose the publication origin.
type PushRequest struct {
	Body                                        []byte
	Scheme, Host, ForwardedProto, ForwardedHost string
}

// GenerateRequest binds the final generation body and request origin.
type GenerateRequest struct {
	Body    []byte
	HostURL string
}

// Response is the domain owner's HTTP outcome. Err is a shutdown/infrastructure failure.
type Response struct {
	Status  int
	Body    any
	NoStore bool
	Err     error
}

// routeLogger is the Python route module's logger for owner-level route events.
var routeLogger = logx.Get("backend.routes.agents")

func rejected(status int, message string) Response {
	return Response{Status: status, Body: map[string]any{"success": false, "message": message}}
}

// DeploymentResponse translates protocol task state into the action route's HTTP status.
func DeploymentResponse(r *model.Deployment) Response {
	v := resultView(r)
	code := v.HTTPStatus
	if code == 0 {
		if !v.Success {
			code = 500
			if v.Status == "rolled_back" || v.Status == "rollback_failed" {
				code = 409
			}
		} else if v.Status != "" && !httpTerminal(v.Status) {
			code = 202
		} else {
			code = 200
		}
	}
	return Response{Status: code, Body: PublicDeployment(r)}
}
func httpTerminal(status string) bool { return finished(status) || status == "rollback_failed" }

// PushConfig owns guard checking, reservation, preparation, cache commit and publication.
func PushConfig(ctx context.Context, m *agents.Manager, id string, r PushRequest) (out Response) {
	err := RunDetached(ctx, "agent-publish-"+id, func(ctx context.Context) error { out = pushConfig(ctx, m, id, r); return out.Err })
	if err != nil {
		out = Response{Err: err}
	}
	return
}
func pushConfig(ctx context.Context, m *agents.Manager, id string, r PushRequest) (out Response) {
	var did string
	var owns bool
	var t Target
	fail := func(err error) Response {
		if ctx.Err() != nil {
			return Response{Err: ctx.Err()}
		}
		var changed *PreviewChanged
		if errors.As(err, &changed) {
			if owns {
				_, e := recordUpdate(ctx, m, id, did, Update{Success: ptr(false), Status: ptr("failed"), Message: ptr(err.Error())}, agents.RecordOptions{})
				if e != nil {
					return Response{Err: e}
				}
			}
			return Response{Status: 409, Body: map[string]any{"success": false, "status": "failed", "code": "preview_changed", "message": err.Error()}}
		}
		status := 500
		message := "Deployment preparation failed"
		details := safeFailure(err, t.Token)
		if IsPreparationFailure(err) {
			status = 400
			message = firstLine(details, 300)
		} else {
			// Python's route logs only the exception class, never its text.
			routeLogger.Errorf("Agent deployment preparation failed (%T)", err)
			details = safeFailure(fmt.Errorf("%T: %w", err, err), t.Token)
		}
		body := map[string]any{"success": false, "status": "failed", "failed_stage": "preparing", "message": message, "error_details": details}
		var p *PreparationError
		if errors.As(err, &p) {
			if p.Issues != nil {
				body["issues"] = p.Issues
			}
			if p.Remedy != nil {
				body["remedy"] = p.Remedy
			}
		}
		if did != "" {
			record, e := recordUpdate(ctx, m, id, did, Update{Success: ptr(false), Status: ptr("failed"), FailedStage: ptr("preparing"), Message: &message, ErrorDetails: &details}, agents.RecordOptions{})
			if e != nil {
				return Response{Err: e}
			}
			body["deployment_id"] = did
			body["events"] = resultEvents(record)
		}
		return Response{Status: status, Body: publicBody(body)}
	}
	var found bool
	var err error
	t, found, err = targetByID(ctx, m, id)
	if err != nil {
		return fail(err)
	}
	if !found {
		return rejected(404, msgAgentMissing)
	}
	data, ok := decodePushBody(r.Body)
	if !ok {
		return rejected(400, msgPushBody)
	}
	previews := previewsFor(m)
	var generated *Preview
	if raw, has := data["generation_id"]; has {
		key, valid := rawString(raw)
		if !valid {
			return fail(&PreviewChanged{msgPreviewUnavailable})
		}
		generated, err = previews.Load(key, id)
		if err != nil {
			return fail(err)
		}
		if v, has := data["deployment_id"]; has && string(v) != "null" {
			value, ok := rawString(v)
			if !ok || value != generated.DeploymentID {
				return fail(&PreviewChanged{msgGenerationTaskMismatch})
			}
		}
		if _, has := taskViewOf(t, generated.DeploymentID); has {
			answer, e := m.GetDeploymentModel(ctx, id, generated.DeploymentID)
			if e != nil {
				return fail(e)
			}
			return DeploymentResponse(answer)
		}
		if err = previews.RequireCurrent(ctx, m, generated); err != nil {
			return fail(err)
		}
		for k, v := range map[string]any{"base_url": generated.BaseURL, "deployment_id": generated.DeploymentID, "expected_profile_id": generated.ProfileID, "expected_service_type": generated.ServiceType, "expected_config_revision": generated.ConfigRevision} {
			data[k], _ = json.Marshal(v)
		}
	}
	base := ""
	if v, has := data["base_url"]; has {
		var valid bool
		base, valid = rawString(v)
		if !valid {
			return fail(fmt.Errorf("base_url must be text"))
		}
		base = strings.TrimSpace(base)
	}
	if base == "" {
		scheme, host := r.Scheme, r.Host
		if r.ForwardedProto != "" {
			scheme = r.ForwardedProto
		}
		if r.ForwardedHost != "" {
			host = r.ForwardedHost
		}
		base = scheme + "://" + host
	}
	has := func(k string) bool { _, ok := data[k]; return ok }
	legacy := has("expected_config_revision") && has("expected_profile_id") && has("expected_oxidns_revision") && !has("expected_service_type")
	guarded := (has("expected_service_type") || has("expected_config_revision")) && !legacy
	expectedRevision, validRevision := rawRevision(data["expected_config_revision"])
	profile, validProfile := rawString(data["expected_profile_id"])
	service, validService := rawString(data["expected_service_type"])
	if legacy && !validRevision {
		return rejected(409, msgConfigChanged)
	}
	if guarded && (!validProfile || !validService || !validRevision) {
		return rejected(400, msgIncompleteGuard)
	}
	source, revision, err := publishSource(ctx, m, t.ProfileID, guarded || legacy)
	if err != nil {
		if errors.Is(err, repo.ErrRepository) {
			return rejected(409, fmt.Sprintf(msgProfileUnavailable, err))
		}
		return fail(err)
	}
	if legacy && (revision.Null || expectedRevision != revision.Value) {
		return rejected(409, msgConfigChanged)
	}
	if guarded && (profile != t.ProfileID || service != t.ServiceType || revision.Null || expectedRevision != revision.Value) {
		return rejected(409, msgSavedDraftChanged)
	}
	if has("expected_oxidns_revision") || (has("expected_profile_id") && !guarded) {
		n, e := yamlx.FromJSON(source.Member("oxidns"))
		if e != nil {
			return fail(e)
		}
		expected, _ := rawString(data["expected_oxidns_revision"])
		if t.ServiceType != "oxidns" || profile != t.ProfileID || expected != oxidns.Revision(n) {
			return Response{Status: 409, Body: map[string]any{"success": false, "code": "draft_changed", "message": msgDraftChanged}}
		}
	}
	if t.ServiceType == "surge" {
		content := ""
		if generated != nil {
			content = generated.ConfigContent
		} else {
			cfg, e := GenerateAgentConfig(ctx, source, t, base)
			if e != nil {
				return fail(e)
			}
			content = cfg.Content
		}
		restart := true
		if raw, has := data["restart"]; has {
			// A legacy explicit null means no restart, rather than the absent default.
			if string(raw) == "null" {
				restart = false
			} else if e := json.Unmarshal(raw, &restart); e != nil {
				return fail(preparation(msgRestartInvalid))
			}
		}
		self := t.Version != "" && agents.CompareVersions(t.Version, "1.1.0-go") >= 0
		in := PushInput{ConfigContent: content, Publish: PublishOptions{ConfigRevision: revision}, RestartAfterUpdate: restart && self}
		if guarded {
			in.ExpectedProfileID = &t.ProfileID
			in.ExpectedServiceType = &t.ServiceType
		}
		answer, e := Push(ctx, m, id, in)
		if e != nil {
			return fail(e)
		}
		if resultView(answer).Success {
			answer = resultField(answer, "profile_id", t.ProfileID)
			if restart {
				var v any = map[string]any{"success": true, "message": msgSelfRestart}
				if !self {
					v, e = m.RunServiceOperation(ctx, id, agents.RestartService, nil, nil)
					if e != nil {
						return fail(e)
					}
				}
				answer = resultField(answer, "restart", v)
			}
		}
		code := 200
		if !resultView(answer).Success {
			code = resultView(answer).HTTPStatus
			if code == 0 {
				code = 500
			}
		}
		return Response{Status: code, Body: PublicDeployment(answer)}
	}
	if t.ServiceType != "mihomo" && t.ServiceType != "mosdns" && t.ServiceType != "oxidns" {
		return rejected(400, msgUnsupportedService)
	}
	activate := true
	if raw, has := data["restart"]; has {
		if string(raw) == "null" || json.Unmarshal(raw, &activate) != nil {
			return rejected(400, msgRestartInvalid)
		}
	}
	if raw, has := data["deployment_id"]; has && !emptyJSON(raw) {
		var ok bool
		did, ok = rawString(raw)
		if !ok {
			return fail(preparation("Invalid deployment ID"))
		}
	}
	if did == "" {
		did, err = randomID()
		if err != nil {
			return fail(err)
		}
	}
	reservation, err := m.ReserveDeployment(ctx, id, did)
	if err != nil {
		return fail(err)
	}
	reserved := resultView(reservation)
	if !reserved.Success {
		code := reserved.HTTPStatus
		if code == 0 {
			code = 409
		}
		return Response{Status: code, Body: PublicDeployment(reservation)}
	}
	if reserved.Existing {
		answer, e := m.GetDeploymentModel(ctx, id, did)
		if e != nil {
			return fail(e)
		}
		return DeploymentResponse(answer)
	}
	owns = true
	defer m.FinishDeploymentPreparation(id, did)
	if guarded && (reserved.ProfileID != t.ProfileID || reserved.ServiceType != t.ServiceType) {
		answer, e := recordUpdate(ctx, m, id, did, Update{Success: ptr(false), Status: ptr("failed"), HTTPStatus: 409, Message: ptr(msgBindingChanged)}, agents.RecordOptions{})
		if e != nil {
			return fail(e)
		}
		return Response{Status: 409, Body: PublicDeployment(answer)}
	}
	if _, err = recordUpdate(ctx, m, id, did, Update{Status: ptr("preparing"), Message: ptr(msgPreparing)}, agents.RecordOptions{}); err != nil {
		return fail(err)
	}
	var prepared *Prepared
	if generated != nil {
		if err = previews.RequireCurrent(ctx, m, generated); err != nil {
			return fail(err)
		}
		source, err = SourceFromJSON(generated.Source, generated.ConfigRevision)
		if err != nil {
			return fail(err)
		}
		prepared = &Prepared{Bundle: generated.Bundle, ConfigContent: generated.Public().Content}
	} else {
		prepared, err = PrepareContent(ctx, m, t, source, PrepareOptions{BaseURL: base, DeploymentID: did, StoreDownloads: true, Progress: publishProgress(ctx, m, id, did, t.Token)})
		if err != nil {
			return fail(err)
		}
	}
	if len(prepared.Providers) > 0 {
		if ctx.Err() != nil {
			return Response{Err: ctx.Err()}
		}
		if err = provider.CommitArtifacts(ctx, t.ProfileID, prepared.Providers); err != nil {
			return fail(err)
		}
	}
	if _, err = recordUpdate(ctx, m, id, did, Update{Status: ptr("preparing"), Message: ptr(msgFilesReady)}, agents.RecordOptions{}); err != nil {
		return fail(err)
	}
	answer, err := Push(ctx, m, id, PushInput{ConfigContent: prepared.ConfigContent, Bundle: prepared.Bundle, Publish: PublishOptions{Activate: activate, ConfigRevision: revision, BaseURL: base, ResourceConfig: source, SourceSnapshot: source}})
	if err != nil {
		return fail(err)
	}
	manifest := prepared.Bundle.Manifest()
	providers, rules := 0, 0
	for _, f := range manifest.Files {
		if f.Role == RoleProvider {
			providers++
		}
		if f.Role == RoleRule {
			rules++
		}
	}
	for k, v := range map[string]any{"profile_id": t.ProfileID, "file_count": len(manifest.Files), "provider_count": providers, "rule_count": rules} {
		answer = resultField(answer, k, v)
	}
	return DeploymentResponse(answer)
}

// GenerateFinal freezes target-adapted files without reserving or publishing a task.
func GenerateFinal(ctx context.Context, m *agents.Manager, id string, r GenerateRequest) (out Response) {
	err := RunDetached(ctx, "agent-generate-"+id, func(ctx context.Context) error {
		t, found, e := targetByID(ctx, m, id)
		if e != nil {
			return e
		}
		if !found {
			out = rejected(404, msgAgentMissing)
			return nil
		}
		data, ok := decodeObject(r.Body)
		profile, _ := rawString(data["expected_profile_id"])
		service, _ := rawString(data["expected_service_type"])
		if !ok || profile != t.ProfileID || service != t.ServiceType {
			out = rejected(409, msgGenerateTargetChanged)
			return nil
		}
		fail := func(e error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			var changed *PreviewChanged
			if errors.As(e, &changed) {
				out = Response{Status: 409, Body: map[string]any{"success": false, "code": "preview_changed", "message": e.Error()}}
			} else {
				out = rejected(400, safeFailure(e, t.Token))
			}
			return nil
		}
		pair, e := m.ReadHAPair(ctx, id)
		if e != nil {
			return fail(e)
		}
		members := []Target{t}
		if len(pair) > 0 {
			members = nil
			for i := range pair {
				target, e := NewTypedTarget(&pair[i])
				if e != nil {
					return fail(e)
				}
				members = append(members, target)
			}
		}
		src, revision, e := publishSource(ctx, m, t.ProfileID, true)
		if e != nil {
			return fail(e)
		}
		base := r.HostURL
		if v, ok := rawString(data["base_url"]); ok && v != "" {
			base = v
		}
		base = strings.TrimRight(base, "/")
		input := PreviewInput{Target: t, Source: src, ConfigRevision: revision, BaseURL: base, Members: members}
		var prepared *Prepared
		if t.ServiceType == "surge" {
			generated, e := GenerateAgentConfig(ctx, src, t, base)
			if e != nil {
				return fail(e)
			}
			input.ConfigContent = generated.Content
		} else {
			prepared, e = PrepareContent(ctx, m, t, src, PrepareOptions{BaseURL: base, StoreDownloads: true})
			if e != nil {
				return fail(e)
			}
			input.Bundle, e = FinalizeBundle(ctx, m, t, prepared.Bundle, src)
			if e != nil {
				return fail(e)
			}
			for _, peer := range members {
				if peer.ID == id {
					continue
				}
				other, err := FinalizeBundle(ctx, m, peer, input.Bundle, src)
				if err != nil {
					return fail(err)
				}
				same, err := input.Bundle.Equal(other)
				if err != nil {
					return fail(err)
				}
				if !same {
					return fail(&Refusal{Message: msgHAFinalDifferent})
				}
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		pv, e := previewsFor(m).Save(ctx, input)
		if e != nil {
			return fail(e)
		}
		if e = previewsFor(m).RequireCurrent(ctx, m, pv); e != nil {
			return fail(e)
		}
		public := pv.Public()
		body := map[string]any{}
		raw, _ := json.Marshal(public)
		_ = json.Unmarshal(raw, &body)
		body["success"] = true
		body["subscription_hosts"] = nil
		body["subscription_dialers"] = nil
		if prepared != nil {
			if len(prepared.SubscriptionHosts) > 0 {
				body["subscription_hosts"] = prepared.SubscriptionHosts
			}
			if len(prepared.SubscriptionDialers) > 0 {
				body["subscription_dialers"] = prepared.SubscriptionDialers
			}
		}
		out = Response{Status: 200, Body: body, NoStore: true}
		return nil
	})
	if err != nil {
		return Response{Err: err}
	}
	return
}
func decodeObject(raw []byte) (map[string]json.RawMessage, bool) {
	var m map[string]json.RawMessage
	if json.Unmarshal(bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf}), &m) != nil || m == nil {
		return nil, false
	}
	return m, true
}
func decodePushBody(raw []byte) (map[string]json.RawMessage, bool) {
	m, ok := decodeObject(raw)
	if ok {
		return m, true
	}
	switch strings.TrimSpace(string(raw)) {
	case "", "null", "false", "0", `""`, "[]":
		return map[string]json.RawMessage{}, true
	}
	if !json.Valid(raw) {
		return map[string]json.RawMessage{}, true
	}
	return nil, false
}
func rawString(raw []byte) (string, bool) {
	var s string
	if len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}
func rawRevision(raw []byte) (int64, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	n, e := strconv.ParseInt(string(raw), 10, 64)
	return n, e == nil && n >= 0
}

// DeploymentPollResponse returns terminal failures as successfully retrieved tasks.
func DeploymentPollResponse(r *model.Deployment) Response {
	v := resultView(r)
	code := v.HTTPStatus
	if code == 0 || httpTerminal(v.Status) {
		code = 200
	}
	return Response{Status: code, Body: PublicDeployment(r)}
}
