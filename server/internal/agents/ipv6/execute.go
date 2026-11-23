package ipv6

import (
	"configflow/internal/jsonresp"
	"context"
	"encoding/json"
	"strings"
	"time"

	"configflow/internal/lifecycle"
)

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
func safeChecks(data Document, prefix string) []Check {
	var values []json.RawMessage
	_ = json.Unmarshal(data["checks"], &values)
	out := []Check{}
	for _, b := range values[:min(80, len(values))] {
		v := object(b)
		if v == nil {
			continue
		}
		status := text(v["status"])
		if status != "ok" && status != "warning" && status != "error" && status != "unknown" {
			status = "unknown"
		}
		id := "unknown"
		if b, ok := v["id"]; ok {
			id = display(b)
		}
		label := msgVerifyResult
		if b, ok := v["label"]; ok {
			label = display(b)
		}
		message := msgNoEvidence
		if b, ok := v["message"]; ok {
			message = display(b)
		}
		details := []string{}
		var rawDetails []json.RawMessage
		_ = json.Unmarshal(v["details"], &rawDetails)
		for _, d := range rawDetails[:min(16, len(rawDetails))] {
			details = append(details, truncate(display(d), 1000))
		}
		c := check(prefix+truncate(id, 100), truncate(label, 200), status, truncate(message, 2000), details...)
		c.Scope = truncate(text(v["scope"]), 100)
		out = append(out, c)
	}
	return out
}
func runtimeChecks(runtime Document, hash, publication string) []Check {
	out := []Check{}
	present := map[string]bool{}
	for _, c := range safeChecks(runtime, "runtime_") {
		if c.ID == "runtime_client_path" {
			continue
		}
		out = append(out, c)
		present[strings.TrimPrefix(c.ID, "runtime_")] = true
	}
	if hash == "" {
		message := msgNoPublication
		if publication == "changed" {
			message = msgNotPublished
		}
		out = append(out, check("runtime_expected_config", msgRuntimeVersion, "unknown", message))
	}
	missing := []string{}
	for _, key := range []string{"deployed_config", "dns_a", "dns_aaaa", "node_ipv6_transport", "proxy_http", "runtime_core", "runtime_ipv6", "runtime_tun"} {
		if !present[key] {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		out = append(out, check("runtime_evidence", msgRuntimeEvidence, "unknown", msgRuntimeMissing, missing...))
	}
	return out
}
func verificationURL(config Document, saved Settings) string {
	if saved.TestURL != "" {
		return saved.TestURL
	}
	_, doc := ReadConfiguration(config)
	groups := field(doc, "proxy-groups")
	if groups != nil {
		for _, g := range groups.Content {
			kind := scalar(field(g, "type"))
			if kind == "url-test" || kind == "fallback" || kind == "load-balance" {
				candidate := scalar(field(g, "url"))
				if candidate != "" && validURL(candidate) == nil {
					return candidate
				}
			}
		}
	}
	return defaultURL()
}
func (s *Service) executeChecks(ctx context.Context, a, config Document, saved Settings, hash string, report Document) ([]Check, error) {
	if digest(verificationIdentity(s.lookup(ctx, text(a["id"])))) != digest(verificationIdentity(a)) {
		return nil, failure("AGENT_CHANGED", 409, msgAgentChanged)
	}
	inspection, e := s.inspect(ctx, a, text(report["profile_id"]), config)
	if e != nil {
		return nil, e
	}
	checks := safeChecks(object(jsonresp.Value(inspection)), "")
	for _, r := range inspection.Routers {
		for _, c := range safeChecks(object(jsonresp.Value(r)), "router_"+r.ID+"_") {
			name := r.Name
			if name == "" {
				name = "ROS"
			}
			c.Label = name + " · " + c.Label
			checks = append(checks, c)
		}
	}
	testURL := verificationURL(config, saved)
	publication := "unknown"
	if hash != "" {
		publication = "synced"
	}
	if text(report["deployment_id"]) == "" {
		var revision *int64
		_ = json.Unmarshal(report["config_revision"], &revision)
		publication, hash = s.published(ctx, a, config, revision)
	}
	payload := Document{"test_url": jsonresp.Value(testURL)}
	if hash != "" {
		payload["expected_config_sha256"] = jsonresp.Value(hash)
	}
	runtime, e := s.request(ctx, a, "verify", payload)
	if e != nil {
		return checks, e
	}
	checks = append(checks, runtimeChecks(runtime, hash, publication)...)
	if saved.ClientAgentID == "" {
		return append(checks, check("client_not_configured", msgClientPath, "unknown", msgNoClient)), nil
	}
	clientChecks, e := s.clientChecks(ctx, a, config, saved, testURL, text(report["run_id"]))
	return append(checks, clientChecks...), e
}
func (s *Service) clientChecks(ctx context.Context, a, config Document, saved Settings, testURL, runID string) ([]Check, error) {
	client, e := s.client(ctx, a, saved.ClientAgentID, true)
	if e != nil {
		return nil, e
	}
	settings, doc := ReadConfiguration(config)
	payload := Document{"test_url": jsonresp.Value(testURL), "request_id": jsonresp.Value(runID)}
	dns := field(doc, "dns")
	if scalar(field(dns, "enhanced-mode")) == "fake-ip" && field(dns, "fake-ip-range") != nil {
		payload["fake_ipv4_range"] = jsonNode(field(dns, "fake-ip-range"))
	}
	if settings.FakeIPv6Enabled {
		payload["fake_ipv6_range"] = settings.FakeIPv6Range
	}
	watchCtx, stop := context.WithCancel(ctx)
	defer stop()
	ready := make(chan struct{}, 1)
	observations := make(chan observed, 1)
	// These fanout workers are children of the current owner and are joined on
	// every path. Their explicit stop context is preserved by the request owner.
	e = lifecycle.Spawn(watchCtx, "ipv6-router-observe", func(job context.Context) {
		defer func() {
			if recover() != nil {
				logger.Error("IPv6 router observer did not complete")
				observations <- observed{}
			}
		}()
		observations <- observe(watchCtx, config, a, testURL, ready)
	})
	if e != nil {
		return nil, e
	}
	timer := lifecycle.ClockFrom(ctx).NewTimer(3 * time.Second)
	select {
	case <-ready:
	case <-timer.C():
	case <-ctx.Done():
	}
	timer.Stop()
	traces := make(chan Document, 1)
	host, _ := targetURL(testURL)
	e = lifecycle.Spawn(ctx, "ipv6-connection-observe", func(job context.Context) {
		defer func() {
			if recover() != nil {
				logger.Error("IPv6 connection observer did not complete")
				traces <- nil
			}
		}()
		trace, _ := s.request(job, a, "verify-connections", Document{"host": jsonresp.Value(host), "request_id": jsonresp.Value(runID)})
		traces <- trace
	})
	if e != nil {
		stop()
		<-observations
		return nil, e
	}
	wait(ctx, 250*time.Millisecond)
	clientReport, requestError := s.request(ctx, client, "verify-client", payload)
	trace := <-traces
	stop()
	seen := <-observations
	if requestError != nil {
		return nil, requestError
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if digest(verificationIdentity(s.lookup(ctx, text(client["id"])))) != digest(verificationIdentity(client)) {
		return nil, failure("CLIENT_CHANGED", 409, msgClientChanged)
	}
	checks := safeChecks(clientReport, "")
	if text(clientReport["request_id"]) != runID || text(trace["request_id"]) != runID {
		checks = append(checks, check("client_request_identity", msgRequestIdentity, "unknown", msgRequestMismatch))
		clientReport, trace = nil, nil
	}
	return append(checks, evaluate(ctx, config, a, clientReport, trace, testURL, seen)...), nil
}
