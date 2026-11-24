package deploy

import (
	"configflow/internal/agents"
	"configflow/internal/agents/monitor"
	"configflow/internal/gateway"
	"configflow/internal/model"
	"configflow/internal/yamlx"
	"context"
	"fmt"
	"net/http"
	"strconv"
)

// PublishOptions captures activation, snapshot revision and the original publication origin.
type PublishOptions struct {
	Activate                       bool
	ConfigRevision                 *Revision
	BaseURL                        string
	ResourceConfig, SourceSnapshot *Source
}

// PushInput selects a transactional delivery or the legacy Surge request.
type PushInput struct {
	ConfigContent                          string
	Bundle                                 *Bundle
	Publish                                PublishOptions
	RestartAfterUpdate                     bool
	ExpectedProfileID, ExpectedServiceType *string
}

func publishDirectWith(ctx context.Context, m *agents.Manager, id string, b *Bundle, o PublishOptions, enroll IPv6Enroller) (answer *model.Deployment, err error) {
	if b == nil {
		return nil, preparation("A complete deployment bundle is required")
	}
	manifest := b.Manifest()
	did := manifest.DeploymentID
	t, found, err := targetByID(ctx, m, id)
	if err != nil {
		return nil, err
	}
	if !found {
		return result(false, "", "Agent not found", 404), nil
	}
	if _, exists := taskViewOf(t, did); !exists {
		reservation, e := m.ReserveDeployment(ctx, id, did)
		if e != nil {
			return nil, e
		}
		if !resultView(reservation).Success {
			return &reservation.Deployment, nil
		}
	}
	defer m.FinishDeploymentPreparation(id, did)
	// An uncertain upload is never converted to a preparation failure.
	defer func() {
		if err == nil {
			return
		}
		if ctx.Err() != nil {
			answer = nil
			err = ctx.Err()
			return
		}
		if agents.IsTransportError(err) {
			current, ok, e := targetByID(ctx, m, id)
			if e != nil {
				err = e
				return
			}
			submitted := false
			if ok {
				v, _ := taskViewOf(current, did)
				submitted = v.Submitted
			}
			status, message := "failed", msgCapabilitiesFailed
			if submitted {
				status, message = "unknown", msgUploadUnknown
			}
			answer, err = recordUpdate(ctx, m, id, did, Update{Success: &submitted, Status: &status, Message: &message, ErrorDetails: new(safeFailure(err, t.Token))}, agents.RecordOptions{UploadReceipt: true})
			return
		}
		if IsPreparationFailure(err) {
			details := safeFailure(err, t.Token)
			answer, err = recordUpdate(ctx, m, id, did, Update{Success: new(false), Status: new("failed"), FailedStage: new("preparing"), Message: new(firstLine(details, 300)), ErrorDetails: &details}, agents.RecordOptions{})
		}
	}()
	if t.ProfileID != manifest.ProfileID {
		return nil, preparation("Agent profile changed during preparation")
	}
	if t.ServiceType != manifest.ServiceType {
		return nil, preparation("Agent service type changed during preparation")
	}
	if t.ServiceType == "mihomo" && !gateway.SupportsGateway(t.Version) {
		files, e := b.Files(manifest.ConfigPath)
		if e != nil {
			return nil, e
		}
		node, e := yamlx.Parse(files[manifest.ConfigPath])
		if e != nil {
			return nil, e
		}
		if isTrue(yamlx.Get(yamlx.Get(node, "x-configflow-linux-gateway"), "enabled")) {
			if e = gateway.RequireGatewayAgent(t.Version); e != nil {
				return nil, e
			}
		}
	}
	caps, err := queryCapabilities(ctx, m, t)
	if err != nil {
		return nil, err
	}
	if !hasProtocol(caps.DeploymentProtocols) || caps.ServiceType != t.ServiceType {
		return recordUpdate(ctx, m, id, did, Update{Success: new(false), Status: new("failed"), HTTPStatus: 409, Message: new(msgTransactionalUnsupported)}, agents.RecordOptions{})
	}
	if manifest.ReloadOnly && !hasProtocol(caps.ReloadOnlyProtocols) {
		return recordUpdate(ctx, m, id, did, Update{Success: new(false), Status: new("failed"), HTTPStatus: 409, Message: new(msgReloadUpgrade)}, agents.RecordOptions{})
	}
	path := caps.ConfigPath
	if path == "" {
		path = "config.yaml"
	}
	if manifest.FinalConfig && (!hasProtocol(caps.FinalConfigProtocols) || path != manifest.ConfigPath) {
		return nil, preparation(msgFinalTargetChanged)
	}
	b, err = Retarget(b, path)
	if err != nil {
		return nil, err
	}
	current, ok, err := targetByID(ctx, m, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, preparation("Deployment ownership changed during preparation")
	}
	previous, _ := taskViewOf(current, did)
	if (previous.Status == "failed" || previous.Status == "rolled_back") && !previous.Submitted {
		return taskResult(current, did), nil
	}
	contentBundle := b
	if manifest.FinalConfig {
		contentBundle, err = withoutResources(b)
		if err != nil {
			return nil, err
		}
	}
	sourceSHA, err := contentBundle.SHA256()
	if err != nil {
		return nil, err
	}
	if previous.Submitted {
		sha := previous.SourceSHA256
		if sha == "" {
			sha = previous.SHA256
		}
		if sha != sourceSHA {
			return resultWithID(result(false, "", "Deployment ID already belongs to different content", 409), did), nil
		}
		return m.GetDeploymentModel(ctx, id, did)
	}
	if !manifest.FinalConfig && t.ServiceType == "mihomo" {
		if _, err = recordUpdate(ctx, m, id, did, Update{Status: new("preparing"), Message: new(msgCheckingGeo)}, agents.RecordOptions{}); err != nil {
			return nil, err
		}
		source := o.ResourceConfig
		if source == nil {
			source = o.SourceSnapshot
		}
		if source == nil {
			cfg, e := m.Repository().Config(ctx, t.ProfileID, nil)
			if e != nil {
				return nil, e
			}
			raw, e := model.Marshal(cfg)
			if e != nil {
				return nil, e
			}
			source, e = SourceFromJSON(raw, nil)
			if e != nil {
				return nil, e
			}
		}
		b, err = PrepareMihomoResources(ctx, b, caps, source, Progress{Phase: func(message string) {
			_, _ = recordUpdate(ctx, m, id, did, Update{Status: new("preparing"), Message: &message}, agents.RecordOptions{})
		}})
		if err != nil {
			return nil, err
		}
	}
	var proof []byte
	if t.ServiceType == "oxidns" {
		if _, err = recordUpdate(ctx, m, id, did, Update{Status: new("preparing"), Message: new(msgCheckingOxiDNS)}, agents.RecordOptions{}); err != nil {
			return nil, err
		}
		v, e := ValidateBundle(id, b)
		if e != nil {
			return nil, e
		}
		proof, err = encodeJSON(v)
		if err != nil {
			return nil, err
		}
	}
	compare := ComparisonManifest(contentBundle.Manifest(), contentBundle.Comparison())
	fp, err := Fingerprint(compare)
	if err != nil {
		return nil, err
	}
	files, err := Digests(compare)
	if err != nil {
		return nil, err
	}
	prepared := PreparedContent{Fingerprint: fp, ProfileID: compare.ProfileID, ServiceType: compare.ServiceType, ConfigPath: compare.ConfigPath, BaseURL: o.BaseURL, Files: files}
	if t.ServiceType == "oxidns" {
		for _, f := range contentBundle.Manifest().Files {
			if f.Role == RoleConfig {
				prepared.DeployedConfigSHA256 = f.SHA256
			}
		}
		source := o.SourceSnapshot
		if source == nil {
			source = o.ResourceConfig
		}
		if source != nil {
			prepared.SettingsDigest = SettingsDigest(source.Member("oxidns"))
		}
	}
	raw, err := encodeJSON(prepared)
	if err != nil {
		return nil, err
	}
	if err = m.RememberPreparedContent(ctx, id, did, raw, proof); err != nil {
		return nil, err
	}
	if t.ServiceType == "mihomo" && o.SourceSnapshot != nil && enroll != nil {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if err = enroll(ctx, m, id, did, o.SourceSnapshot); err != nil {
			return nil, err
		}
	}
	if o.SourceSnapshot != nil && (t.ServiceType == "mihomo" || t.ServiceType == "oxidns") {
		if err = NewReceipts(m.Repository().DataDir()).Capture(ctx, id, contentBundle, o.SourceSnapshot, o.BaseURL, contentBundle.Comparison()); err != nil {
			return nil, err
		}
	}
	archive, err := b.Archive()
	if err != nil {
		return nil, err
	}
	sha, err := b.SHA256()
	if err != nil {
		return nil, err
	}
	sourceSHA, err = contentBundle.SHA256()
	if err != nil {
		return nil, err
	}
	revision := o.ConfigRevision
	if revision == nil {
		revision = &Revision{Null: true}
	}
	answer, err = recordUpdate(ctx, m, id, did, Update{Success: new(true), Status: new("uploading"), Submitted: new(true), Message: new(fmt.Sprintf(msgUploading, len(b.Manifest().Files), commaNumber(len(archive)))), SHA256: &sha, SourceSHA256: &sourceSHA, ConfigVersion: new(b.ConfigVersion()), ConfigRevision: revision}, agents.RecordOptions{})
	if err != nil {
		return nil, err
	}
	if v := resultView(answer); v.Status != "uploading" || !v.Submitted {
		return answer, nil
	}
	if o.Activate && t.ServiceType == "oxidns" {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if err = monitor.CheckpointStatistics(ctx, monitor.TypedFromAgents(m), id, did, prepared.DeployedConfigSHA256); err != nil {
			return nil, err
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	timeout := agents.Pair(10, 120)
	reply, err := m.DeploymentRequestModel(ctx, t.Record, "POST", "deployments", http.Header{"Content-Type": {"application/gzip"}, "X-Deployment-ID": {did}, "X-Content-SHA256": {sha}, "X-Activate": {strconv.FormatBool(o.Activate)}}, archive, &timeout)
	if err != nil {
		return nil, err
	}
	return uploadRecord(ctx, m, id, did, normalizeUpload(reply))
}
func withoutResources(b *Bundle) (*Bundle, error) {
	m := b.Manifest()
	fs, err := b.Contents()
	if err != nil {
		return nil, err
	}
	filtered := make([]File, 0, len(m.Files))
	for _, f := range m.Files {
		if f.Role == RoleResource {
			delete(fs, f.Path)
		} else {
			filtered = append(filtered, f)
		}
	}
	m.Files = filtered
	return Pack(m, fs, b.ConfigVersion(), b.Comparison())
}
