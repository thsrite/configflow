package agents

import (
	"encoding/json"
	"fmt"
	"testing"

	"configflow/internal/deploy"

	"configflow/internal/agents"
	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/redact"
)

// publicAgentEncodedModel is the projection before O10-R4 (whole record
// encoded, split and re-encoded), kept as the test oracle for publicAgentView.
func publicAgentEncodedModel(record *model.Agent, secrets redact.Secrets) (map[string]json.RawMessage, error) {
	raw, err := model.Marshal(record)
	if err != nil {
		return nil, err
	}
	secrets = redact.CollectCredentials(json.RawMessage(raw), secrets)
	var public map[string]json.RawMessage
	if err = jsondecode.Unmarshal(raw, &public); err != nil {
		return nil, err
	}
	for _, key := range []string{"token", "applied_content", "dns_follow_job"} {
		delete(public, key)
	}
	if record.OxiDNSHAPublication != nil && record.OxiDNSHAPublication.HasMembers() {
		summary, err := agents.HASummaryModel(record.OxiDNSHAPublication)
		if err != nil {
			return nil, err
		}
		public["oxidns_ha_publication"], err = model.Marshal(summary)
		if err != nil {
			return nil, err
		}
	}
	if raw, present := public["deployments"]; present && len(raw) > 0 && raw[0] == '{' {
		public["deployments"], err = publicDeploymentsModel(record)
		if err != nil {
			return nil, err
		}
	}
	if record.LatestDeployment != nil {
		public["latest_deployment"], err = publicDeploymentModel(record.LatestDeployment)
		if err != nil {
			return nil, err
		}
	}
	if record.LatestUpgrade != nil {
		state, err := agents.PublicUpgradeStateModel(record.LatestUpgrade)
		if err != nil {
			return nil, err
		}
		public["latest_upgrade"], err = model.Marshal(state)
		if err != nil {
			return nil, err
		}
	}
	// SanitizeCredentials owns all secret discovery and text redaction. The HTTP
	// writer may perform its independent stored-secret pass afterwards.
	publicRaw, err := json.Marshal(public)
	if err != nil {
		return nil, err
	}
	sanitized := redact.SanitizeCredentials(json.RawMessage(publicRaw), secrets)
	if err = jsondecode.Unmarshal(sanitized, &public); err != nil {
		return nil, err
	}
	return public, nil
}

// TestPublicAgentViewMatchesEncodedProjection: the member-wise projection
// equals the whole-record projection it replaced when given the record's own
// credentials, for every member shape the projection distinguishes.
func TestPublicAgentViewMatchesEncodedProjection(t *testing.T) {
	cases := []string{
		`{"id":"device","token":"secret-token","applied_content":"secret-content","dns_follow_job":{"token":"secret-job"},"future":{"keep":[1,true,null]},"latest_deployment":null}`,
		`{"id":"device","deployments":{"one":{"status":"failed","message":"Connection refused","prepared_content":"secret-prepared","oxidns_validation_receipt":{"agent_id":"secret-receipt"},"events":[{"status":"failed","message":"timeout"}]}},"latest_deployment":{"status":"succeeded","message":"ok","prepared_content":"secret-latest"},"latest_upgrade":{"status":"failed","error":"Connection refused","message":"timeout","events":[{"status":"failed","message":"Connection refused"}]}}`,
		`{"oxidns_ha_publication":{"id":"pair","status":"failed","message":"失败","members":["a","b"],"children":{"a":"secret-child"},"private":"secret-private"},"latest_deployment":{"ha_publication":{"status":"failed","message":"同步失败"},"pending":true,"http_status":202,"prepared_content":"secret-content"}}`,
		`{"deployments":{},"latest_upgrade":{},"oxidns_ha_publication":{}}`,
		`{"deployments":null,"latest_deployment":"legacy scalar","latest_upgrade":false}`,
		`{"deployments":["not","an","object"],"ipv6_verifications":{"r":{"status":"ok"}}}`,
		`{"deployments":{"a":{"status":"ready"},"b":"scalar","c":null,"d":{"status":"failed","message":"timeout"}}}`,
		`{"deployments":{"x":{"status":"ready","password":"secret-pass"},"y":{"status":7}},"name":"secret-pass-shown"}`,
		`{}`,
		`{"id":"legacy","host":"192.0.2.8","port":8080,"version":"1.0.0","name":7,"dns_follow":null,"latest_upgrade":null,"cpu":{"usage_percent":1.5},"service_status":"running"}`,
		`{"id":"ha","oxidns_ha_publication":{"id":"pair","status":"succeeded","phase":"done","message":"已同步","owner_agent_id":"a","owner_deployment_id":"d","active_agent_id":"b","members":["a","b"],"warnings":["提示"],"routers":["router"],"children":{"a":"secret-child"},"private_key":"secret-key"}}`,
		`{"id":"upgrade","latest_upgrade":{"status":"rolled_back","message":"Authorization: Bearer hidden","error":"Agent download write failed","events":[{"at":"2026-01-01T00:00:00","status":"failed","message":"password=hidden","unexpected":"secret-event"}]}}`,
		`{"id":"old","future":{"name":"保留","list":[null,{},[]]},"api_key":"secret-legacy","note":"uses secret-legacy"}`,
	}
	for i, text := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			var record model.Agent
			if err := json.Unmarshal([]byte(text), &record); err != nil {
				t.Fatal(err)
			}
			want, err := publicAgentEncodedModel(&record, nil)
			if err != nil {
				t.Fatal(err)
			}
			got, err := publicAgentModel(&record)
			if err != nil {
				t.Fatal(err)
			}
			wantJSON, _ := json.Marshal(want)
			gotJSON, _ := json.Marshal(got)
			if string(wantJSON) != string(gotJSON) {
				t.Fatalf("member-wise %s\nencoded    %s", gotJSON, wantJSON)
			}
		})
	}
}

// TestSourceFromConfigMatchesEncodedSource: a content source built on the
// borrowed Config has the same raw snapshot, input key and typed view as one
// decoded from the Config's encoding.
func TestSourceFromConfigMatchesEncodedSource(t *testing.T) {
	f := newContentHTTP(t, true, false)
	config, err := f.s.Repo.Config(f.s.Context(), "default", nil)
	if err != nil {
		t.Fatal(err)
	}
	direct, err := deploy.SourceFromConfig(config, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := model.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := deploy.SourceFromJSON(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	directRaw, _ := direct.Raw()
	decodedRaw, _ := decoded.Raw()
	directKey, err1 := direct.InputKey()
	decodedKey, err2 := decoded.InputKey()
	if string(directRaw) != string(decodedRaw) || err1 != nil || err2 != nil || directKey != decodedKey || direct.ProfileID() != decoded.ProfileID() {
		t.Fatalf("sources differ: %v %v", err1, err2)
	}
	var directView, decodedView []byte
	_ = direct.ViewConfig(func(c *model.Config) error { directView, err = model.Marshal(c); return err })
	_ = decoded.ViewConfig(func(c *model.Config) error { decodedView, err = model.Marshal(c); return err })
	if string(directView) != string(decodedView) || len(directView) == 0 {
		t.Fatal("typed views differ")
	}
}
