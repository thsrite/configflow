package agents

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

// These are the eight original Python TypeError rows, not a type-wide bypass.
// The full original archive and every complete compact row are independently
// pinned. The coordinator retired CPython exception bytes, preserving rejection
// and no-write behavior and requiring structured upstream 502 at the HTTP edge.
var statusTypeErrorRows = map[int]string{
	0:  "4065cd4c874f452703e92ab296b8fce78a66d354a0e93d34cc033efaef6bf845",
	1:  "7eafcf96d50ea5d4c0d01dfccb96b7c7616888a135fe13e39b8e6350e2223af3",
	14: "d241b3eaa1d7d354b42bef2e772d3b5a7f46266ee6809c71a6b72f39272c3b9a",
	15: "6046d8fc0345a1f98487e423bd0ec4da0c00a8078a47be33729cda5cbb130394",
	21: "150f557947fedd97ec9e6d0edeb12c9fcbf652f836c682c436e093f0a86aea6e",
	22: "64aefffac704dfd35784a9ce0ba603bc93c540fdcefb93748da1aabff2b963ed",
	28: "20cd2d5c442788a5816c23f7fe5a70f8b51d8338b82482f3fa0c815a03d9f1df",
	29: "ed83e69b6c2b7d71c8d829a9a41379f78c0d79f4af41262c40fa82be08ff91bc",
}

func verifyNativeRecordStatusRow(t *testing.T, index int, rowRaw json.RawMessage, rowErr string, got error) {
	t.Helper()
	archive, err := os.ReadFile("testdata/a01_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(archive)) != "6c8e582ea7e4801aaac9a25594f474e2e0d33ccceb845b8d7e849cf816d04681" {
		t.Fatal("original Python archive changed")
	}
	want, present := statusTypeErrorRows[index]
	if !present {
		t.Fatalf("unreviewed Python error row %d", index)
	}
	raw := compactRawJSON(t, rowRaw)
	if fmt.Sprintf("%x", sha256.Sum256(raw)) != want {
		t.Fatalf("original Python row %d changed: %s", index, raw)
	}
	class, _, found := strings.Cut(rowErr, ": ")
	if !found || class != "TypeError" {
		t.Fatalf("original Python error class changed: %s", rowErr)
	}
	if !errors.Is(got, ErrInvalidDeploymentStatus) {
		t.Fatalf("row=%d native did not reject upstream shape: %v", index, got)
	}
}

// The original single nil-Agent call and Python TypeError text are pinned here;
// the exception implementation is retired, while refusal and no-write remain.
func verifyDeletedAgentRequestReference(t *testing.T, got error) {
	t.Helper()
	const original = `{"agent":null,"method":"POST","suffix":"deployments/x/rollback","python_error":"TypeError: 'NoneType' object is not subscriptable"}`
	if fmt.Sprintf("%x", sha256.Sum256([]byte(original))) != "a8817226ddcc49c6a74c6cc05fd0bad64ca0b6e7cad911f17150f95cc9cc183f" {
		t.Fatal("original nil Agent reference changed")
	}
	var reference struct {
		Error string `json:"python_error"`
	}
	if err := json.Unmarshal([]byte(original), &reference); err != nil {
		t.Fatal(err)
	}
	class, _, _ := strings.Cut(reference.Error, ": ")
	if class != "TypeError" || !errors.Is(got, ErrAgentEndpoint) || IsTransportError(got) {
		t.Fatalf("native deleted Agent refusal=%v, Python class=%s", got, class)
	}
}
