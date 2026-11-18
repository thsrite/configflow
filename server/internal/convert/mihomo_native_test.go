package convert

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

func nativeGenerationEnv(t testing.TB) MihomoEnvironment {
	t.Helper()
	return MihomoEnvironmentFromLegacy(MihomoEnv{
		ProviderInterval: func(*jsonx.Object) (int64, error) { return 3600, nil },
	})
}

func TestNativeMihomoMissingCollaborators(t *testing.T) {
	env := nativeGenerationEnv(t)
	value := reflect.ValueOf(&env).Elem()
	count := 0
	for i := 0; i < value.NumField(); i++ {
		if value.Field(i).Kind() != reflect.Func || value.Field(i).IsNil() {
			continue
		}
		name := value.Type().Field(i).Name
		t.Run(name, func(t *testing.T) {
			broken := env
			reflect.ValueOf(&broken).Elem().Field(i).SetZero()
			t.Cleanup(SetMihomoEnvironment(broken))
			doc, err := MihomoDocument(context.Background(), &model.Config{Profile: &model.Profile{}}, MihomoOptions{SkipProviderPreflight: true})
			var missing *HookMissingError
			if !errors.As(err, &missing) || doc != nil {
				t.Fatalf("missing %s: doc=%v error=%v", name, doc, err)
			}
		})
		count++
	}
	if count != 12 {
		t.Fatalf("checked %d required callbacks", count)
	}
}

func TestNativeMihomoGraphPresentation(t *testing.T) {
	source := "defaults: &original {secret: '0o17', flag: true}\ncopy: *original\nmerged: {<<: *original, name: test}\n"
	input, err := yamlx.Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	before, err := yamlx.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	output, err := outputGraph(input)
	if err != nil {
		t.Fatal(err)
	}
	data, err := yamlx.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	reparsed, err := yamlx.Parse(data)
	if err != nil || !yamlx.Equal(input, reparsed) {
		t.Fatalf("round trip: %s %v", data, err)
	}
	if !bytes.Contains(data, []byte("&id001")) || !bytes.Contains(data, []byte("*id001")) || bytes.Contains(data, []byte("<<:")) {
		t.Fatalf("unexpected presentation: %s", data)
	}
	after, _ := yamlx.Marshal(input)
	if !bytes.Equal(before, after) {
		t.Fatal("presentation modified source")
	}
}

func TestNativeMihomoAliasEntrances(t *testing.T) {
	t.Cleanup(SetMihomoIntegrations(MihomoIntegrations{GatewayEnabled: func(*model.Config) (bool, error) { return true, nil }}))
	t.Cleanup(SetMihomoEnvironment(nativeGenerationEnv(t)))
	texts := []struct {
		name, text string
		want       error
	}{
		{"self", "payload: &self [*self]", yamlx.ErrAliasRecursion},
		{"budget", func() string {
			var s strings.Builder
			s.WriteString("a0: &a0 [0]\n")
			for depth := 1; depth <= 40; depth++ {
				fmt.Fprintf(&s, "a%d: &a%d [*a%d, *a%d]\n", depth, depth, depth-1, depth-1)
			}
			return s.String()
		}(), yamlx.ErrAliasBudget},
	}
	for _, tt := range texts {
		t.Run(tt.name, func(t *testing.T) {
			config := &model.Config{Profile: &model.Profile{Mihomo: model.MihomoSettings{CustomConfig: tt.text}}}
			doc, err := MihomoDocument(context.Background(), config, MihomoOptions{SkipProviderPreflight: true})
			if doc != nil || !errors.Is(err, tt.want) {
				t.Fatalf("document: %v", err)
			}
			out, err := MihomoConfig(context.Background(), config, MihomoOptions{SkipProviderPreflight: true})
			if out != "" || !errors.Is(err, tt.want) {
				t.Fatalf("config: %v", err)
			}
			downloads, err := MihomoProviderDownloads(context.Background(), config, "", nil)
			if downloads != nil || !errors.Is(err, tt.want) {
				t.Fatalf("provider downloads: %v", err)
			}
			downloads, err = MihomoRulesetDownloads(context.Background(), config, "")
			if downloads != nil || !errors.Is(err, tt.want) {
				t.Fatalf("ruleset downloads: %v", err)
			}
		})
	}
	self := yamlx.Map()
	self.Anchor = "self"
	yamlx.Set(self, "self", &yamlx.Node{Kind: yaml.AliasNode, Value: "self", Alias: self})
	if out, err := MihomoProviderDownloads(context.Background(), nil, "", self); out != nil || !errors.Is(err, yamlx.ErrAliasRecursion) {
		t.Fatal(out, err)
	}
	// Repeated aliases retain their physical target.
	leaf := yamlx.Seq(yamlx.Int(1))
	leaf.Anchor = "leaf"
	root := yamlx.Map(yamlx.Str("leaf"), leaf)
	for i := 0; i < 40; i++ {
		yamlx.Set(root, fmt.Sprintf("copy%d", i), &yamlx.Node{Kind: yaml.AliasNode, Value: "leaf", Alias: leaf})
	}
	graph, err := outputGraph(root)
	if err != nil {
		t.Fatal(err)
	}
	if yamlx.Resolve(yamlx.Get(graph, "copy39")) != yamlx.Get(graph, "leaf") {
		t.Fatal("sharing lost")
	}
	var linear strings.Builder
	linear.WriteString("a0: &a0 {v: leaf}\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&linear, "a%d: &a%d {child: *a%d}\n", i, i, i-1)
	}
	config := &model.Config{Profile: &model.Profile{Mihomo: model.MihomoSettings{CustomConfig: linear.String()}}}
	doc, err := MihomoDocument(context.Background(), config, MihomoOptions{SkipProviderPreflight: true})
	if err != nil {
		t.Fatal("linear alias chain rejected", err)
	}
	if yamlx.Resolve(yamlx.Get(yamlx.Get(doc, "a40"), "child")) != yamlx.Get(doc, "a39") {
		t.Fatal("linear chain sharing lost")
	}

}

func TestNativeMihomoDownloads(t *testing.T) {
	doc, err := yamlx.Parse([]byte("proxy-providers: {main: {type: http, url: 'https://example.com/a', path: ./a.yaml}}"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := MihomoProviderDownloads(context.Background(), nil, "", doc)
	if err != nil || len(got) != 1 || got[0].URL != "https://example.com/a" || got[0].LocalPath != "./a.yaml" {
		t.Fatal(got, err)
	}
	yamlx.Set(yamlx.Get(yamlx.Get(doc, "proxy-providers"), "main"), "url", yamlx.Int(5))
	if _, err := MihomoProviderDownloads(context.Background(), nil, "", doc); err == nil {
		t.Fatal("non-text URL accepted")
	}
	if _, err := MihomoRulesetDownloads(context.Background(), nil, ""); err == nil {
		t.Fatal("missing config accepted")
	}
	for _, bad := range []*yamlx.Node{yamlx.Seq(), yamlx.Map(yamlx.Str("proxy-providers"), yamlx.Map(yamlx.Str("bad"), yamlx.Int(1)))} {
		if _, err := MihomoProviderDownloads(context.Background(), nil, "", bad); err == nil {
			t.Fatal("malformed provider document accepted")
		}
	}
	address, err := appendProfileQuery("https://example.com/a?x=a+b&x=#part", [][2]string{{"token", "a b/c?"}})
	if err != nil || address != "https://example.com/a?x=a%20b&x=&token=a%20b%2Fc%3F#part" {
		t.Fatal(address, err)
	}
}

func FuzzNativeMihomoGraph(f *testing.F) {
	for _, text := range []string{"a: &a {secret: '0o17'}\nb: *a", "a: &a [*a]", "a: [1, true, 2001-01-01, .inf]", "a: {<<: {x: 1}, x: 2}"} {
		f.Add(text)
	}
	f.Fuzz(func(t *testing.T, text string) {
		input, err := yamlx.Parse([]byte(text))
		if err != nil {
			return
		}
		if input, err = projectReadGraph(input, yamlx.RejectNonStringKeys); err != nil {
			return
		}
		before, err := yamlx.Marshal(input)
		if err != nil {
			return
		}
		output, err := outputGraph(input)
		if err != nil {
			return
		}
		data, err := yamlx.Marshal(output)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := yamlx.Parse(data)
		if err != nil || !yamlx.Equal(parsed, input) {
			t.Fatalf("round trip %q: %v", data, err)
		}
		after, err := yamlx.Marshal(input)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("input changed", err)
		}
	})
}

func FuzzNativeMihomoGeneration(f *testing.F) {
	for _, text := range []string{"{}", `{"mihomo":{"custom_config":"ipv6: false"}}`, `{"nodes":[],"proxy_groups":[]}`, `{"mihomo":{"custom_config":"payload: &self [*self]"}}`} {
		f.Add([]byte(text))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var profile model.Profile
		if json.Unmarshal(data, &profile) != nil {
			return
		}
		config := &model.Config{Profile: &profile}
		before, err := model.Marshal(config)
		if err != nil {
			return
		}
		restore := SetMihomoEnvironment(nativeGenerationEnv(t))
		defer restore()
		output, err := MihomoConfig(context.Background(), config, MihomoOptions{SkipProviderPreflight: true})
		if err == nil {
			if _, err := yamlx.Parse([]byte(output)); err != nil {
				t.Fatalf("invalid generated YAML: %v", err)
			}
		}
		after, err := model.Marshal(config)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("generation modified input", err)
		}
	})
}

type wireNativeConverter struct{ calls int }

func (c *wireNativeConverter) ConvertProxy(context.Context, string) (*yamlx.Node, error) {
	c.calls++
	return yamlx.Map(yamlx.Str("name"), yamlx.Str("test"), yamlx.Str("type"), yamlx.Str("http")), nil
}
func TestNativeMihomoClientsShareRegistration(t *testing.T) {
	converter := &wireNativeConverter{}
	t.Cleanup(SetMihomoEnvironment(MihomoEnvironment{Converter: converter}))
	result := clientConverter().ConvertProxyString(context.Background(), "http://example.com")
	if result.GetString("name") != "test" || converter.calls != 1 {
		t.Fatal("unported client did not use typed registration")
	}
	var missing *HookMissingError
	if result, err := (legacyNodeConverter{}).ConvertProxy(context.Background(), "http://example.com"); result != nil || !errors.As(err, &missing) {
		t.Fatal("missing converter silently skipped", err)
	}
}
