package rules

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
	"configflow/internal/testjson"
)

func TestReviewComposeCommitFailureIsAtomic(t *testing.T) {
	s := fixture(t)
	before, err := s.Repo.ExportAllJSON(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	s.Repo.Store().BeforeCommit = func() error { calls++; return errors.New("synthetic commit failure") }
	t.Cleanup(func() { s.Repo.Store().BeforeCommit = nil })
	body := composeBody(record{"kind": stringJSON("new_library"), "library": json.RawMessage(`{"name":"Atomic","source_type":"content","behavior":"domain","content":"+.atomic.example.com\n"}`)}, "direct")
	s.Post("/api/rules/compose?profile=other", s.Login(), httpxtest.JSON(body)).WantStatus(500)
	if calls != 1 {
		t.Fatalf("commit seam not reached: %d", calls)
	}
	s.Repo.Store().BeforeCommit = nil
	reopened, err := repo.Open(s.DataDir, repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	after, err := reopened.ExportAllJSON(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !testjson.Equal(before, after) {
		t.Fatal("failed commit changed shared/profile/DNS storage")
	}
}

func TestReviewWriteCancellation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(*Service, context.Context) error
	}{
		{"create", func(s *Service, ctx context.Context) error {
			_, err := s.Create(ctx, "other", rawJSON(record{"id": stringJSON("new"), "rule_type": stringJSON("DOMAIN"), "value": stringJSON("new.example.com")}), false)
			return err
		}},
		{"mutate", func(s *Service, ctx context.Context) error {
			_, err := s.Mutate(ctx, "other", "rule_1", rawJSON(record{"remark": stringJSON("changed")}), false, false, "")
			return err
		}},
		{"delete", func(s *Service, ctx context.Context) error {
			_, err := s.Mutate(ctx, "other", "rule_1", nil, true, false, "")
			return err
		}},
		{"rename", func(s *Service, ctx context.Context) error {
			return s.Rename(ctx, "other", request[renameRequest](record{"ids": json.RawMessage(`["rule_1"]`), "group_name": stringJSON("changed")}))
		}},
		{"policy", func(s *Service, ctx context.Context) error {
			_, err := s.SetPolicy(ctx, "other", request[policyRequest](record{"ids": json.RawMessage(`["rule_1"]`), "policy": stringJSON("PROXY")}), "")
			return err
		}},
		{"reorder", func(s *Service, ctx context.Context) error {
			_, _, err := s.Reorder(ctx, "other", rawJSON(record{"ids": json.RawMessage(`["rule_1"]`), "position": stringJSON("bottom")}), "")
			return err
		}},
		{"batch", func(s *Service, ctx context.Context) error {
			_, err := s.Batch(ctx, "other", request[batchRequest](record{"domains": json.RawMessage(`["new.example.com"]`), "rule_type": stringJSON("DOMAIN")}))
			return err
		}},
		{"dns", func(s *Service, ctx context.Context) error {
			_, err := s.SetDNS(ctx, "other", request[dnsWriteRequest](record{"routes": json.RawMessage(`{"rule_1":"direct"}`)}), "")
			return err
		}},
		{"compose", func(s *Service, ctx context.Context) error {
			_, err := s.Compose(ctx, "other", request[composeRequest](composeBody(record{"kind": stringJSON("values"), "rule_type": stringJSON("DOMAIN"), "text": stringJSON("new.example.com")}, "default")), "")
			return err
		}},
	} {
		for _, stopped := range []bool{false, true} {
			mode := "request_disconnect"
			if stopped {
				mode = "shutdown"
			}
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				s := fixture(t)
				var ctx context.Context
				if stopped {
					runtime, work := reviewRuntimeContext(t, s.Context())
					runtime.Shutdown(context.Background())
					ctx = work
				} else {
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(s.Context())
					cancel()
				}
				before, err := s.Repo.Head(s.Context())
				if err != nil {
					t.Fatal(err)
				}
				repeats := 1
				if stopped {
					repeats = 16
				}
				for attempt := 0; attempt < repeats; attempt++ {
					err = tc.write(&Service{now: time.Now}, ctx)
					after, readErr := s.Repo.Head(s.Context())
					if readErr != nil {
						t.Fatal(readErr)
					}
					if stopped {
						if !errors.Is(err, context.Canceled) || before != after {
							t.Fatalf("stopped owner admitted write: attempt=%d err=%v changed=%v", attempt, err, before != after)
						}
					} else if err != nil || before == after {
						t.Fatalf("disconnected owner did not finish: err=%v changed=%v", err, before != after)
					}
				}
			})
		}
	}
}

func reviewRuntimeContext(t *testing.T, base context.Context) (*lifecycle.Runtime, context.Context) {
	t.Helper()
	runtime := lifecycle.NewRuntime(lifecycle.Options{})
	contexts := make(chan context.Context, 1)
	if err := runtime.Spawn(base, "capture", func(ctx context.Context) { contexts <- ctx }); err != nil {
		t.Fatal(err)
	}
	ctx := <-contexts
	if err := runtime.WaitIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Shutdown(context.Background()) })
	return runtime, ctx
}
