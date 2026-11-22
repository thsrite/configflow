package subs

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"configflow/internal/model"
	"configflow/internal/testjson"
	"configflow/internal/yamlx"
)

// matchError checks a Go error against a recorded Python exception: the text
// must be equal for DialerReferenceError, any error will do for the other
// exception types (TypeError, KeyError ... of malformed documents).
func matchError(t *testing.T, where string, err error, wantRaw json.RawMessage) bool {
	t.Helper()
	msg, errType, isErr := isErrorRaw(wantRaw)
	if !isErr {
		if err != nil {
			t.Fatalf("%s: unexpected error %v", where, err)
		}
		return false
	}
	if err == nil {
		t.Fatalf("%s: want error %q, got none", where, msg)
	}
	var de *DialerReferenceError
	if errors.As(err, &de) && errType == "DialerReferenceError" && de.Message != msg {
		t.Fatalf("%s: error %q, want %q", where, de.Message, msg)
	}
	if errType == "DialerReferenceError" && !errors.As(err, &de) {
		t.Fatalf("%s: error %v is not a DialerReferenceError (%q)", where, err, msg)
	}
	return true
}

func loweredProfileRaw(t *testing.T, origRaw json.RawMessage, origCfg, runtime *model.Config) json.RawMessage {
	t.Helper()
	if runtime == origCfg {
		return origRaw
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(origRaw, &out); err != nil {
		t.Fatal(err)
	}
	groupJSON, err := model.Marshal(runtime.Profile.ProxyGroups)
	if err != nil {
		t.Fatal(err)
	}
	nodeJSON, err := model.Marshal(runtime.Nodes)
	if err != nil {
		t.Fatal(err)
	}
	out["proxy_groups"] = groupJSON
	out["nodes"] = nodeJSON
	return rawJSON(t, out)
}

func TestChainsCorpus(t *testing.T) {
	rows := loadCorpus(t, "chains.jsonl.gz")
	native := nativeChainExpectations(t)
	ops := sharedDialers()
	stats := map[string]int{}
	for i, row := range rows {
		wantRaw := row.OutRaw
		if entry, ok := native[fmt.Sprintf("chains/%d", i)]; ok {
			wantRaw = entry.Expected
		}
		var want map[string]json.RawMessage
		if err := json.Unmarshal(wantRaw, &want); err != nil {
			t.Fatalf("row %d want: %v", i, err)
		}
		where := func(what string) string { return fmt.Sprintf("%s row %d %s", what, i, row.InRaw) }
		if o18T1cDirtyInput(row.InRaw) {
			// Stored dirty data reads leniently since O18 T1c (accepted).
			stats["O18 T1c 接受: stored dirty data"]++
			continue
		}
		if msg, _, isErr := isErrorRaw(want["validate"]); isErr && o18T1cChainMissingEndpoint(row.InRaw, msg) {
			stats["疑似回归（交回 T1c 作者）: chain without entry/exit"]++
			continue
		}

		cfg, err := decodeConfigRaw(row.InRaw)
		if err == nil {
			err = referenceFailure(ops.Validate(cfg))
		}
		if matchError(t, where("validate"), err, want["validate"]) {
			stats["validate_errors"]++
		}

		cfg, err = decodeConfigRaw(row.InRaw)
		var lowered *model.Config
		if err == nil {
			lowered, err = LowerChains(cfg)
			if err != nil {
				err = dre(err.Error())
			}
		}
		_, hasProvidersMain := want["providers_main"]
		if matchError(t, where("lower"), err, want["lower"]) {
			stats["lower_errors"]++
			if !hasProvidersMain {
				continue
			}
		} else {
			stats["lowered"]++
			gotRaw := loweredProfileRaw(t, row.InRaw, cfg, lowered)
			if !testjson.Equal(want["lower"], gotRaw) {
				t.Fatalf("%s\n got  %s\n want %s\n diff %s", where("lower"), gotRaw, want["lower"], testjson.Diff(want["lower"], gotRaw))
			}
		}
		if !hasProvidersMain {
			continue
		}
		stats["providers"]++
		mainNode := yamlFromRaw(t, want["providers_in"])
		perr := referenceFailure(ChainProviders(lowered, mainNode))
		matchError(t, where("providers"), perr, want["providers_result"])
		gotMain := yamlToRaw(t, mainNode)
		if !testjson.Equal(want["providers_main"], gotMain) {
			t.Fatalf("%s\n got  %s\n want %s", where("providers"), gotMain, want["providers_main"])
		}
	}
	t.Logf("%d rows %v", len(rows), stats)
}

func TestChainProviderFilterWithoutSources(t *testing.T) {
	cfg, err := decodeConfigRaw(json.RawMessage(`{"proxy_groups":[{"id":"chain","name":"Chain","_chain_dialer":{"type":"node","id":"entry"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	mainNode := yamlFromRaw(t, json.RawMessage(`{"proxy-groups":[{"name":"Chain","type":"select","filter":"(`+"`"+`z"}]}`))
	// Python compiles sorting filters only when a provider consumes them. An
	// unused invalid pattern must not make a static-only chain fail generation.
	if err := ChainProviders(cfg, mainNode); err != nil {
		t.Fatal(err)
	}
	expected := yamlx.Map(yamlx.Str("proxy-groups"), yamlx.Seq(yamlx.Map(yamlx.Str("name"), yamlx.Str("Chain"), yamlx.Str("type"), yamlx.Str("select"))))
	if !yamlx.Equal(mainNode, expected) {
		t.Fatal(string(yamlToRaw(t, mainNode)))
	}
}
