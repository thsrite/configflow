package contentsync

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"configflow/internal/deploy"
	"configflow/internal/jsonx"
)

type traceAction struct {
	Op      string
	Seconds int
	Patch   json.RawMessage
}
type traceReply struct {
	Status, Reason string
	Checked        bool
	Version        int
}
type observation struct {
	Reply                         *traceReply
	Jobs, Calls, Entries, Details int
}

func TestPythonCheckerHistories(t *testing.T) {
	data, err := os.ReadFile("testdata/checker.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name   string
		Limits struct {
			Entries int `json:"max_entries"`
			Details int `json:"max_details"`
		}
		Actions  []traceAction
		Expected []observation
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 9 {
		t.Fatalf("corpus shrank: %d", len(cases))
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			f := newFixture(t, func(o *Options) { o.MaxEntries = tc.Limits.Entries; o.MaxDetails = tc.Limits.Details })
			failures := map[deploy.InputKey]bool{}
			base := f.prepare
			f.prepare = func(ctx context.Context, target deploy.Target, s *deploy.Source, url, id string) (*deploy.Bundle, error) {
				key, _ := s.InputKey()
				if failures[key] {
					f.calls++
					return nil, fmt.Errorf("synthetic private error")
				}
				return base(ctx, target, s, url, id)
			}
			versions := map[string]int{}
			for i, action := range tc.Actions {
				var reply *traceReply
				var result any
				switch action.Op {
				case "status", "diff", "sync":
					source := f.source()
					key, _ := source.InputKey()
					failures[key] = f.cfg.GetBool("fail")
					switch action.Op {
					case "status":
						result = f.c.Status(f.ctx, f.target(), source, "https://config.example.test")
					case "diff":
						result = f.c.PushDiff(f.ctx, f.target(), source, "https://config.example.test")
					case "sync":
						result = f.c.CompareNow(f.ctx, f.target(), source, "https://config.example.test")
					}
				case "run":
					if f.spawn.count() > 0 {
						f.spawn.run(t)
					}
				case "advance":
					f.clock.Advance(time.Duration(action.Seconds) * time.Second)
				case "invalidate":
					f.c.Invalidate()
				case "agent", "config":
					v, err := jsonx.Parse(action.Patch)
					if err != nil {
						t.Fatal(err)
					}
					dst := f.cfg
					if action.Op == "agent" {
						dst = f.agent
					}
					dst.Update(v.(*jsonx.Object))
				default:
					t.Fatal(action.Op)
				}
				if result != nil {
					raw, err := json.Marshal(result)
					if err != nil {
						t.Fatal(err)
					}
					var wire struct {
						Status, Reason string
						CheckedAt      string `json:"checked_at"`
						Version        string
					}
					if err = json.Unmarshal(raw, &wire); err != nil {
						t.Fatal(err)
					}
					reply = &traceReply{Status: wire.Status, Reason: wire.Reason, Checked: wire.CheckedAt != ""}
					if wire.Version != "" {
						if versions[wire.Version] == 0 {
							versions[wire.Version] = len(versions) + 1
						}
						reply.Version = versions[wire.Version]
					}
				}
				got := observation{reply, f.spawn.count(), f.calls, len(f.c.entries), len(f.c.details)}
				if !reflect.DeepEqual(got, tc.Expected[i]) {
					t.Fatalf("step %d %s: got %+v (reply %+v), want %+v (reply %+v)", i, action.Op, got, reply, tc.Expected[i], tc.Expected[i].Reply)
				}
			}
		})
	}
}
