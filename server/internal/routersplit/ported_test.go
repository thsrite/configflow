package routersplit

import (
	"context"
	"errors"
	"os"
	"regexp"
	"testing"

	"configflow/internal/gateway/gwbase"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

// Behaviour the recorded replay cannot observe: Go types (errors.As), the
// fail-closed ProfileSessions hook, the frontend copy of the messages.

// test_invalid_marker_fails_closed: `isinstance(error.value, LinuxGatewayError)`
// and code/status — as Go's errors.As.
func TestRouterSplitErrorIsALinuxGatewayError(t *testing.T) {
	config := jsonx.Obj("mihomo", jsonx.Obj("router_split", jsonx.Obj("version", true)))
	err := CheckGeneration(config)
	var gateway *gwbase.LinuxGatewayError
	if !errors.As(err, &gateway) || gateway.Message != Messages[CodeInvalid] {
		t.Fatalf("errors.As(LinuxGatewayError) failed for %#v", err)
	}
	var split *RouterSplitError
	if !errors.As(err, &split) || split.Code != CodeInvalid || split.Status != 409 || split.PythonClass() != "RouterSplitError" {
		t.Fatalf("RouterSplitError fields: %#v", split)
	}
	if err.Error() != Messages[CodeInvalid] {
		t.Fatalf("message %q", err.Error())
	}
	if e := NewRouterSplitError("m", "C", 400); e.Status != 400 {
		t.Fatalf("explicit status lost: %d", e.Status)
	}
}

// The setup sessions come from config C36 (setup_flow.profile_sessions);
// until it registers the hook every session read fails closed.
func TestProfileSessionsHookFailsClosedUntilRegistered(t *testing.T) {
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	if _, err := r.CreateProfile(ctx, jsonx.Obj("id", "home", "name", "home"), ""); err != nil {
		t.Fatal(err)
	}
	restore := SetProfileSessions(nil)
	defer restore()
	var missing *HookMissingError
	if _, err := LegacyContext(ctx, r, "home"); !errors.As(err, &missing) {
		t.Fatalf("LegacyContext without the hook: %v", err)
	}
	gateway := jsonx.Obj("mihomo", jsonx.Obj("linux_gateway", jsonx.Obj("enabled", true)))
	if _, err := PublishLegacySource(ctx, r, "home", gateway, "a"); !errors.As(err, &missing) {
		t.Fatalf("PublishLegacySource without the hook: %v", err)
	}
	// Marked or profile-less calls never read the sessions.
	if source, err := PublishLegacySource(ctx, r, "", gateway, "a"); err != nil || source != nil {
		t.Fatalf("no profile: %v %v", source, err)
	}
	session := jsonx.Obj("id", "s1", "goal", "router", "completed", 1)
	SetProfileSessions(func(_ context.Context, got *repo.Repository, profileID string) ([]*jsonx.Object, error) {
		if got != r || profileID != "home" {
			t.Errorf("hook got %p %q", got, profileID)
		}
		return []*jsonx.Object{session}, nil
	})
	legacy, err := LegacyContext(ctx, r, "home")
	if err != nil {
		t.Fatal(err)
	}
	want := `{"session_goal":"router","session_id":"s1","session_completed":true,"has_gateway_session":false,"mihomo_agent_ids":[]}`
	n89SessionPolicy(t, []*jsonx.Object{session}, want, legacy)

	state, err := RouterSplitState(ctx, r, "home", gateway, nil)
	if err != nil || state.Value("mode") != "legacy" || state.Value("source") != "setup" {
		t.Fatalf("state %s %v", encode(state), err)
	}
}

// test_frontend_keeps_the_same_router_split_copy (router_split half; the
// FORWARDING_* constants belong to agents A20).
func TestFrontendKeepsTheSameRouterSplitCopy(t *testing.T) {
	data, err := os.ReadFile("../../../frontend/src/lib/routerSplit.ts")
	if err != nil {
		t.Skip("frontend not present:", err)
	}
	source := string(data)
	frontend := map[string]string{}
	for _, m := range regexp.MustCompile(`(?m)^\s+(ROUTER_SPLIT_[A-Z_]+): '([^']*)',$`).FindAllStringSubmatch(source, -1) {
		frontend[m[1]] = m[2]
	}
	if len(frontend) != len(Messages) {
		t.Fatalf("frontend has %d messages, Go %d", len(frontend), len(Messages))
	}
	for code, message := range Messages {
		if frontend[code] != message {
			t.Errorf("%s: frontend %q go %q", code, frontend[code], message)
		}
	}
	if !regexp.MustCompile(regexp.QuoteMeta("LEGACY_RESTORE_BLOCKED = '" + LegacyRestoreBlocked + "'")).MatchString(source) {
		t.Error("LEGACY_RESTORE_BLOCKED differs from the frontend copy")
	}
}

// full_tun_document keeps YAML anchors shared (copy.deepcopy's memo), so the
// dumped base YAML has the same &id001 / *id001 as Python's.
func TestFullTunBaseYAMLKeepsAliases(t *testing.T) {
	text := "base: &b [1, 2]\nlist: *b\n"
	got, err := FullTunBaseYAML(text)
	if err != nil {
		t.Fatal(err)
	}
	want := "base: &id001\n- 1\n- 2\nlist: *id001\ntun:\n  enable: true\n  stack: mixed\n  auto-route: true\n" +
		"  auto-redirect: true\n  auto-detect-interface: true\n  dns-hijack:\n  - any:53\n  - tcp://any:53\n"
	if !sameYAML(got, want) {
		t.Fatalf("got\n%s", got)
	}
}
