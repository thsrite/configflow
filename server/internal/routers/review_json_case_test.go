package routers

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"configflow/internal/devhttp"
	"configflow/internal/jsondecode"
	"configflow/internal/model"
)

func TestReviewStoredConnectionCredentials(t *testing.T) {
	router, agent := &model.Router{}, &model.Agent{}
	for _, tc := range []struct {
		name, raw string
		dst       any
		check     func() bool
	}{
		{"router", `{ "id":"router-review", "username":"old-user", "password":"synthetic-password", "Password":"opaque-extension" }`, router, func() bool { return router.Username == "old-user" && router.Password == "synthetic-password" }},
		{"agent", `{ "id":"agent-review", "token":"synthetic-token", "Token":"opaque-extension" }`, agent, func() bool { return agent.Token == "synthetic-token" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := jsondecode.Unmarshal([]byte(tc.raw), tc.dst); err != nil {
				t.Fatal(err)
			}
			if !tc.check() {
				t.Fatal("stored credential changed")
			}
			encoded, err := model.Marshal(tc.dst)
			if _, plain := tc.dst.(*model.Agent); plain {
				// Agent records are plain (O18 T1b): the case alias is dropped.
				if err != nil || string(encoded) != `{"id":"agent-review","token":"synthetic-token"}` {
					t.Fatalf("stored record changed: %s (%v)", encoded, err)
				}
				return
			}
			if err != nil || string(encoded) != tc.raw {
				t.Fatalf("stored record changed (%v)", err)
			}
		})
	}
}

func TestReviewDeviceCorpusExactKeys(t *testing.T) {
	f, err := os.Open("testdata/r03_corpus.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	scanner := bufio.NewScanner(gz)
	scanner.Buffer(make([]byte, 64<<10), 1<<26)
	seen := map[string]bool{}
	for scanner.Scan() {
		var row struct {
			Fn string          `json:"fn"`
			In json.RawMessage `json:"in"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		if seen[row.Fn] || row.Fn != "assess_nat" && row.Fn != "ikuai_read" {
			continue
		}
		seen[row.Fn] = true
		var input struct {
			State struct {
				Context struct {
					Routes json.RawMessage `json:"routes"`
				} `json:"nat_context"`
			} `json:"state"`
			Groups json.RawMessage `json:"groups"`
		}
		if err := json.Unmarshal(row.In, &input); err != nil {
			t.Fatal(err)
		}
		raw, required := input.State.Context.Routes, "dst-address"
		if row.Fn == "ikuai_read" {
			raw, required = input.Groups, "group_name"
		}
		t.Run(row.Fn, func(t *testing.T) {
			var native, got []Row
			d := json.NewDecoder(bytes.NewReader(raw))
			d.UseNumber()
			if err := d.Decode(&native); err != nil {
				t.Fatal(err)
			}
			response := devhttp.Response{Body: raw}
			if err := response.JSON(&got); err != nil {
				t.Fatal(err)
			}
			if len(got) == 0 || got[0][required] == nil || !reflect.DeepEqual(got, native) {
				t.Fatal("recorded device keys/values changed")
			}
		})
		if len(seen) == 2 {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 {
		t.Fatal("missing RouterOS/iKuai corpus controls")
	}
}
