package subs

import (
	"encoding/json"
	"strconv"
	"testing"

	"configflow/internal/testjson"
	"configflow/internal/yamlx"
)

func TestProxyServerDNSCorpus(t *testing.T) {
	rows := loadCorpus(t, "dns.jsonl.gz")
	native := nativeDNSExpectations(t)
	kinds := map[string]int{}
	for i, row := range rows {
		kinds[row.Fn]++
		wantRaw := row.OutRaw
		if entry, ok := native[strconv.Itoa(i)]; ok {
			wantRaw = entry.Out
		}
		switch row.Fn {
		case "compress":
			got := Compress(strList(row.In))
			if got == nil {
				got = []string{}
			}
			if !testjson.Equal(wantRaw, got) {
				t.Fatalf("row %d compress(%s)\n got  %s\n want %s", i, row.InRaw, compact(t, got), wantRaw)
			}
		case "listener_address":
			text, _ := row.In.(string)
			addr, reason := DNSListener(text)
			var a, r any
			if addr != "" {
				a = addr
			}
			if reason != "" {
				r = reason
			}
			got := []any{a, r}
			if !testjson.Equal(wantRaw, got) {
				t.Fatalf("row %d listener_address(%s): got %s want %s", i, row.InRaw, compact(t, got), wantRaw)
			}
		case "parse_core_version":
			text, _ := row.In.(string)
			v, ok := CoreVersion(text)
			var got any
			if ok {
				got = []json.Number{json.Number(v[0].String()), json.Number(v[1].String()), json.Number(v[2].String())}
			}
			if !testjson.Equal(wantRaw, got) {
				t.Fatalf("row %d parse_core_version(%s): got %s want %s", i, row.InRaw, compact(t, got), wantRaw)
			}
		case "core_too_old":
			text, _ := row.In.(string)
			var want bool
			if err := json.Unmarshal(wantRaw, &want); err != nil {
				t.Fatal(err)
			}
			if got := IsCoreTooOld(text); got != want {
				t.Fatalf("row %d core_too_old(%s): got %v", i, row.InRaw, got)
			}
		case "final_target":
			var in struct {
				Hosts  json.RawMessage `json:"hosts"`
				Domain string          `json:"domain"`
			}
			if err := json.Unmarshal(row.InRaw, &in); err != nil {
				t.Fatal(err)
			}
			got := NewHostResolver(yamlFromRaw(t, in.Hosts)).Target(in.Domain)
			var g any
			if got != "" {
				g = got
			}
			if !testjson.Equal(wantRaw, g) {
				t.Fatalf("row %d final_target(%s): got %s want %s", i, row.InRaw, compact(t, g), wantRaw)
			}
		case "apply_node_dns":
			var in struct {
				Main        json.RawMessage `json:"main"`
				Merged      json.RawMessage `json:"merged"`
				CoreVersion any             `json:"core_version"`
			}
			if err := json.Unmarshal(row.InRaw, &in); err != nil {
				t.Fatal(err)
			}
			mainNode := yamlFromRaw(t, in.Main)
			mergedNode := yamlFromRaw(t, in.Merged)
			core, _ := in.CoreVersion.(string)
			report, err := ApplyDNS(mainNode, mergedNode, core)
			if err != nil {
				t.Fatalf("row %d apply_node_dns error: %v", i, err)
			}
			got := map[string]any{
				"report": report,
				"main":   yamlToRaw(t, mainNode),
			}
			if !testjson.Equal(wantRaw, got) {
				t.Fatalf("row %d apply_node_dns\n in   %s\n got  %s\n want %s\n diff %s", i, row.InRaw, compact(t, got), wantRaw, testjson.Diff(wantRaw, got))
			}
		}
	}
	t.Logf("%v", kinds)
}

func yamlMapKeys(n *yamlx.Node) []string {
	var keys []string
	if n == nil {
		return keys
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		keys = append(keys, n.Content[i].Value)
	}
	return keys
}
