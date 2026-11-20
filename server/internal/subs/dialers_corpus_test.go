package subs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/testjson"
	"configflow/internal/yamlx"
)

// corpusConvert mirrors conv() of testdata/gen.py, the stand-in for
// convert_node_to_mihomo used in the differential run.
func corpusConvert(node model.Node) (*yamlx.Node, error) {
	if node.Type == "bad" {
		return nil, nil
	}
	data, err := model.Marshal(node)
	if err != nil {
		return nil, err
	}
	src, err := yamlx.FromJSON(data)
	if err != nil {
		return nil, err
	}
	kind := yamlx.Get(src, "type")
	if kind == nil {
		kind = yamlx.Str("ss")
	}
	nameNode := yamlx.Get(src, "name")
	if nameNode == nil {
		nameNode = yamlx.Null()
	}
	serverNode := yamlx.Get(src, "server")
	if serverNode == nil {
		serverNode = yamlx.Null()
	}
	portNode := yamlx.Get(src, "port")
	if portNode == nil {
		portNode = yamlx.Null()
	}
	out := yamlx.Map(
		yamlx.Str("name"), yamlx.Clone(nameNode),
		yamlx.Str("type"), yamlx.Clone(kind),
		yamlx.Str("server"), yamlx.Clone(serverNode),
		yamlx.Str("port"), yamlx.Clone(portNode),
	)
	if params := yamlx.Get(src, "params"); params != nil && len(params.Content) > 0 {
		for i := 0; i+1 < len(params.Content); i += 2 {
			yamlx.Set(out, params.Content[i].Value, yamlx.Clone(params.Content[i+1]))
		}
	}
	return out, nil
}

func TestDialersCorpus(t *testing.T) {
	rows := loadCorpus(t, "dialers.jsonl.gz")
	native := nativeDialersExpectations(t)
	oracle := nativeReaderCases(t)
	kinds := map[string]int{}
	errorsSeen := 0
	for i, row := range rows {
		kinds[row.Fn]++
		switch row.Fn {
		case "extract_subscription_dialers":
			wantRaw := row.OutRaw
			if expected, changed := changedNativeCase(t, oracle, fmt.Sprintf("subs/dialers/%d", i)); changed {
				if expected.HasError {
					wantRaw = rawJSON(t, map[string]string{"error": expected.Message})
				} else {
					wantRaw = expected.Value
				}
			}
			if entry, ok := native[fmt.Sprintf("dialers/%d", i)]; ok {
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(entry.Expected, &fields); err != nil {
					t.Fatal(err)
				}
				if rawErr, hasErr := fields["error"]; hasErr {
					wantRaw = rawJSON(t, map[string]json.RawMessage{"error": rawErr})
				} else {
					wantRaw = fields["value"]
				}
			}
			got, err := ExtractDialers(row.In.(string))
			if _, _, isErr := isErrorRaw(wantRaw); isErr {
				if err == nil {
					t.Fatalf("row %d extract: want error, got %s", i, compact(t, got))
				}
				errorsSeen++
				continue
			}
			var g any
			if got != nil {
				g = got
			}
			if err != nil {
				t.Fatalf("row %d extract(%q): unexpected error %v", i, row.In, err)
			}
			if !testjson.Equal(wantRaw, g) {
				t.Fatalf("row %d extract(%q)\n got  %s\n want %s", i, row.In, compact(t, g), wantRaw)
			}
		case "adapt_subscription_proxies":
			var in struct {
				Subscription string          `json:"subscription"`
				Proxies      json.RawMessage `json:"proxies"`
				Main         json.RawMessage `json:"main"`
				Record       json.RawMessage `json:"record"`
				Chains       bool            `json:"chains"`
			}
			if err := json.Unmarshal(row.InRaw, &in); err != nil {
				t.Fatal(err)
			}
			proxiesNode := yamlFromRaw(t, in.Proxies)
			mainNode := yamlFromRaw(t, in.Main)
			var record *DialersData
			if err := jsondecode.Unmarshal(in.Record, &record); err != nil {
				t.Fatalf("row %d record: %v", i, err)
			}
			selected, err := AdaptProxies(in.Subscription, proxiesNode.Content, mainNode, record, in.Chains)
			if err != nil {
				t.Fatalf("row %d adapt error: %v", i, err)
			}
			gotRaw := yamlToRaw(t, yamlx.Seq(selected...))
			if !testjson.Equal(row.OutRaw, gotRaw) {
				t.Fatalf("row %d adapt %s\n got  %s\n want %s", i, row.InRaw, gotRaw, row.OutRaw)
			}
		case "add_dialer_groups":
			var in struct {
				Caches map[string]json.RawMessage `json:"caches"`
				Main   json.RawMessage            `json:"main"`
				Config json.RawMessage            `json:"config"`
			}
			if err := json.Unmarshal(row.InRaw, &in); err != nil {
				t.Fatal(err)
			}
			mainNode := yamlFromRaw(t, in.Main)
			cfg, err := decodeConfigRaw(in.Config)
			if err != nil {
				t.Fatalf("row %d config: %v", i, err)
			}
			inputs := DialerInputs{
				LoadCache: func(_ context.Context, id string) (*SubscriptionCache, error) {
					raw, ok := in.Caches[id]
					if !ok {
						return nil, nil
					}
					var cache *SubscriptionCache
					if err := jsondecode.Unmarshal(raw, &cache); err != nil {
						return nil, err
					}
					return cache, nil
				},
				Convert: func(_ context.Context, node model.Node) (*yamlx.Node, error) {
					return corpusConvert(node)
				},
			}
			report, err := AddGroups(context.Background(), mainNode, cfg, inputs)
			var want struct {
				Report json.RawMessage `json:"report"`
				Main   json.RawMessage `json:"main"`
			}
			if err := json.Unmarshal(row.OutRaw, &want); err != nil {
				t.Fatal(err)
			}
			if msg, _, isErr := isErrorRaw(want.Report); isErr {
				errorsSeen++
				if err == nil {
					t.Fatalf("row %d add_dialer_groups: want error %q, got report %s", i, msg, compact(t, report))
				}
				var de *DialerReferenceError
				if errors.As(err, &de) && de.Message != msg {
					t.Fatalf("row %d add_dialer_groups: error %q, want %q", i, de.Message, msg)
				}
			} else if err != nil {
				t.Fatalf("row %d add_dialer_groups: unexpected error %v", i, err)
			} else {
				var r any
				if report != nil {
					r = report
				}
				if !testjson.Equal(want.Report, r) {
					t.Fatalf("row %d add_dialer_groups report\n in   %s\n got  %s\n want %s", i, row.InRaw, compact(t, r), want.Report)
				}
			}
			gotMain := yamlToRaw(t, mainNode)
			if !testjson.Equal(want.Main, gotMain) {
				t.Fatalf("row %d add_dialer_groups main\n in   %s\n got  %s\n want %s", i, row.InRaw, gotMain, want.Main)
			}
		}
	}
	t.Logf("%v, %d Python exceptions", kinds, errorsSeen)
}
