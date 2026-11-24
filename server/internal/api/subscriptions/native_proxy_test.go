package subscriptions

import (
	"bytes"
	"configflow/internal/model"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/logx"
	"configflow/internal/refresh"
)

func TestCachedProxyPolicySkipsAreCountedWithoutNodeSecrets(t *testing.T) {
	var logs bytes.Buffer
	if _, err := logx.Setup(logx.Config{Level: logx.Warning, Console: &logs}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := logx.Setup(logx.Config{Level: logx.Info, Console: io.Discard}); err != nil {
			t.Error(err)
		}
	})
	node := func(name, source string) proxy {
		o := proxy{}
		o.set("name", name)
		if source != "" {
			o.set("proxy_string", source)
		} else {
			o.set("type", "http")
			o.set("server", "example.com")
			o.set("port", 80)
		}
		return o
	}
	nodes := []proxy{
		node("good", ""),
		node("private-node-a", "type: ss\npassword: fixture-secret-value\nx: &x [*x]"),
		node("private-node-b", "type: ss\npassword: fixture-secret-value\nx: !!bool fixture-secret-value"),
		node("other", ""),
	}
	h := NewHandler(nil, nil, nil, nil)
	got, err := h.cachedProxies(context.Background(), model.Subscription{}, nodes, false)
	if err != nil || len(got) != 2 || got[0].text("name") != "good" || got[1].text("name") != "other" {
		t.Fatalf("lenient: %v %v", got, err)
	}
	for _, message := range []string{fmt.Sprintf(msgProxyYAMLSkipped, 2, "alias_recursion"), fmt.Sprintf(msgProxyYAMLSkipped, 3, "yaml_policy"), fmt.Sprintf(msgProxyYAMLSkipSummary, 2)} {
		if !strings.Contains(logs.String(), message) {
			t.Fatalf("missing warning %q", message)
		}
	}
	for _, secret := range []string{"private-node-a", "private-node-b", "fixture-secret-value", "password:", "proxy_string"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("warning leaked node content: %s", secret)
		}
	}
	logs.Reset()
	got, err = h.cachedProxies(context.Background(), model.Subscription{}, nodes, true)
	var yamlError *convert.ProxyYAMLError
	if got != nil || !errors.As(err, &yamlError) || !referenceError(err) {
		t.Fatalf("strict validation: %v %v", got, err)
	}
	if logs.Len() != 0 {
		t.Fatal("strict path both logged and returned its error")
	}
}

func TestStagedIdentityComesFromNodeOwnerNotProtocolParameters(t *testing.T) {
	source := model.Subscription{ID: "s", Name: "Synthetic"}
	item := proxy{}
	for key, value := range map[string]any{"id": "upstream-protocol-id", "name": "Remote", "type": "http", "server": "example.com", "port": 80} {
		item.set(key, value)
	}
	update, err := NewHandler(nil, nil, nil, nil).stageNodes(source, []proxy{item}, &refresh.SourceResult{Source: "prepared_yaml", Text: "proxies: []"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(update.updates) != 1 || len(update.updates[0].Nodes) != 1 {
		t.Fatalf("missing staged node: %+v", update)
	}
	node := update.updates[0].Nodes[0]
	if !strings.HasPrefix(node.ID, "node_") || node.SubscriptionID != "s" || node.SubscriptionName != "Synthetic" {
		t.Fatalf("wrong owned identity: %+v", node)
	}
	raw, err := json.Marshal(node.Params)
	if err != nil {
		t.Fatal(err)
	}
	var params map[string]json.RawMessage
	if err := json.Unmarshal(raw, &params); err != nil {
		t.Fatal(err)
	}
	if string(params["id"]) != `"upstream-protocol-id"` {
		t.Fatalf("protocol parameter was lost: %s", raw)
	}
}

func (o proxy) set(key string, value any) { o[key], _ = json.Marshal(value) }
