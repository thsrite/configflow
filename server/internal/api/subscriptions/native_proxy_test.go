package subscriptions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/logx"
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
	node := func(name, source string) object {
		o := object{}
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
	nodes := []object{
		node("good", ""),
		node("private-node-a", "type: ss\npassword: fixture-secret-value\nx: &x [*x]"),
		node("private-node-b", "type: ss\npassword: fixture-secret-value\nx: !!bool fixture-secret-value"),
		node("other", ""),
	}
	got, err := cachedProxies(context.Background(), object{}, nodes, false)
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
	got, err = cachedProxies(context.Background(), object{}, nodes, true)
	var yamlError *convert.ProxyYAMLError
	if got != nil || !errors.As(err, &yamlError) || !referenceError(err) {
		t.Fatalf("strict validation: %v %v", got, err)
	}
	if logs.Len() != 0 {
		t.Fatal("strict path both logged and returned its error")
	}
}
