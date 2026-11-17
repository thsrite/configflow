package mosdns

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/taskstatus"
)

func TestCFG09NativeIOAndCancellationAreNotURLFailures(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	destination := filepath.Join(dir, "directory")
	if err := os.WriteFile(source, []byte("receipt"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	renameErr := os.Rename(source, destination)
	var link *os.LinkError
	if !errors.As(renameErr, &link) {
		t.Fatalf("missing real rename failure: %v", renameErr)
	}
	_, pathErr := os.Open(filepath.Join(source, "child"))
	var path *fs.PathError
	if !errors.As(pathErr, &path) {
		t.Fatalf("missing real path failure: %v", pathErr)
	}
	tests := []struct {
		name string
		err  error
	}{
		{"rename", renameErr}, {"wrapped-rename", fmt.Errorf("record receipt: %w", renameErr)},
		{"path", pathErr}, {"wrapped-path", fmt.Errorf("record receipt: %w", pathErr)},
		{"canceled", fmt.Errorf("record receipt: %w", context.Canceled)},
		{"deadline", fmt.Errorf("record receipt: %w", context.DeadlineExceeded)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if isBadRequest(tt.err) {
				t.Fatalf("server failure classified as URL refusal: %v", tt.err)
			}
		})
	}
}

func TestCFG09ReceiptFailureAfterDownload(t *testing.T) {
	for _, scenario := range []string{"valid", "invalid-url", "rename-directory", "permission"} {
		for _, boundary := range []string{"cause", "public"} {
			t.Run(scenario+"/"+boundary, func(t *testing.T) {
				const rawURL = "https://rules.example.com/cfg09.list"
				rule := jsonx.Obj("id", "cfg09-source", "name", "Synthetic CFG09", "source_type", "url", "url", rawURL, "behavior", "domain", "format", "text", "enabled", true)
				ctx, r := testRepo(t, jsonx.Obj("rule_library", []any{rule}))
				receipt := filepath.Join(r.DataDir(), taskstatus.Filename)
				switch scenario {
				case "rename-directory":
					if err := os.Mkdir(receipt, 0700); err != nil {
						t.Fatal(err)
					}
				case "permission":
					if os.Geteuid() == 0 {
						t.Fatal("real permission control requires the normal test-server user")
					}
					if err := os.Chmod(r.DataDir(), 0500); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = os.Chmod(r.DataDir(), 0700) })
				}
				calls := 0
				withFetch(t, func(context.Context, string, *jsonx.Object, string) ([]byte, error) {
					calls++
					return []byte("example.com\n"), nil
				})
				request := RuleProxyRequest{URL: rawURL, SourceID: "cfg09-source", Auth: AuthRequest{Internal: true}}
				if scenario == "invalid-url" {
					request.URL = "ftp://rules.example.com/cfg09.list"
					request.SourceID = ""
				}
				wantStatus := 200
				if scenario == "invalid-url" {
					wantStatus = 400
				} else if scenario != "valid" {
					wantStatus = 500
				}
				if boundary == "public" {
					response := RuleProxy(ctx, request)
					if response.Status != wantStatus {
						t.Fatalf("status=%d want%d: %v", response.Status, wantStatus, response.JSON)
					}
					if wantStatus == 500 && response.JSON["message"] != "Failed to process rule proxy request" {
						t.Fatalf("IO error changed to URL message: %v", response.JSON)
					}
					if wantStatus == 400 && response.JSON["message"] != "Invalid remote URL" {
						t.Fatalf("URL rejection changed: %v", response.JSON)
					}
				} else {
					_, err := ruleProxy(ctx, request)
					if scenario == "valid" {
						if err != nil {
							t.Fatal(err)
						}
					} else if scenario == "invalid-url" {
						if !isBadRequest(err) {
							t.Fatalf("invalid URL no longer rejected: %v", err)
						}
					} else {
						if err == nil || isBadRequest(err) {
							t.Fatalf("storage failure lost classification: %v", err)
						}
						if scenario == "rename-directory" {
							var cause *os.LinkError
							if !errors.As(err, &cause) || cause.New != receipt {
								t.Fatalf("rename cause lost: %v", err)
							}
						} else {
							var cause *fs.PathError
							if !errors.As(err, &cause) || !strings.Contains(cause.Path, ".task-status-") || !errors.Is(err, fs.ErrPermission) {
								t.Fatalf("receipt permission cause lost: %v", err)
							}
						}
					}
				}
				wantCalls := 1
				if scenario == "invalid-url" {
					wantCalls = 0
				}
				if calls != wantCalls {
					t.Fatalf("downloads=%d want%d", calls, wantCalls)
				}
				if scenario == "rename-directory" || scenario == "permission" {
					// The validated cache may already exist; do not pretend the whole request
					// rolled back merely because the later receipt failed.
					if len(ruleCacheFiles(r)) == 0 {
						t.Fatal("successful download/cache was incorrectly treated as absent")
					}
					temps, err := filepath.Glob(filepath.Join(r.DataDir(), ".task-status-*"))
					if err != nil || len(temps) != 0 {
						t.Fatalf("receipt temporary file leak: %v %v", temps, err)
					}
				}
			})
		}
	}
}
