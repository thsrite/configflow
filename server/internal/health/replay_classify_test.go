package health

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"configflow/internal/health/check"
	"configflow/internal/model"
	"configflow/internal/testjson"
)

type typedExpectation struct {
	File      string          `json:"file"`
	Row       int             `json:"row"`
	Fn        string          `json:"fn"`
	Contract  string          `json:"contract"`
	Out       json.RawMessage `json:"out"`
	ErrorKind string          `json:"error_kind"`
	Message   string          `json:"message"`
}

func nativeExpectations(t *testing.T, filename string) map[int]typedExpectation {
	t.Helper()
	data, err := os.ReadFile("testdata/n89_expected.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Sources      map[string]string  `json:"sources"`
		Expectations []typedExpectation `json:"expectations"`
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(original)
	if reference.Sources[filepath.Base(filename)] != hex.EncodeToString(sum[:]) {
		t.Fatal("original corpus changed; review native reference inputs")
	}
	out := map[int]typedExpectation{}
	for _, expected := range reference.Expectations {
		if expected.File != filepath.Base(filename) {
			continue
		}
		if _, duplicate := out[expected.Row]; duplicate || expected.Contract == "" {
			t.Fatal("invalid typed expectation index")
		}
		out[expected.Row] = expected
	}
	return out
}

func sameNativeOutput(expected json.RawMessage, out any) (bool, error) {
	if _, err := testjson.Parse(expected); err != nil {
		return false, err
	}
	return testjson.Equal(out, expected), nil
}

func TestNativeReferenceChecksValuesAndSiblings(t *testing.T) {
	want := json.RawMessage(`{"verdict":"both_ok","confidence":60}`)
	for _, raw := range []string{`{"verdict":"both_ok","confidence":59}`, `{"verdict":"both_ok"}`, `{"verdict":"wrong","confidence":60}`} {
		same, err := sameNativeOutput(want, json.RawMessage(raw))
		if err != nil || same {
			t.Fatalf("reference accepted changed result: %s (%v)", raw, err)
		}
	}
	if same, err := sameNativeOutput(want, want); err != nil || !same {
		t.Fatal("positive control rejected", err)
	}
}

func assertNativeExpectation(t *testing.T, want typedExpectation, fn string, out any, err error) {
	t.Helper()
	if fn != want.Fn {
		t.Fatalf("typed reference row changed: %s vs %s", fn, want.Fn)
	}
	var cv *check.ValidationError
	var hv *ValidationError
	var unavailable *ProbeUnavailable
	var shape *json.UnmarshalTypeError
	var syntax *json.SyntaxError
	validError := false
	switch want.ErrorKind {
	case "check":
		validError = errors.As(err, &cv) && cv.Message == want.Message
	case "health":
		validError = errors.As(err, &hv) && hv.Message == want.Message
	case "probe-unavailable":
		validError = errors.As(err, &unavailable) && unavailable.Message == want.Message
	case "decode":
		validError = errors.Is(err, model.ErrType) || errors.Is(err, model.ErrJSON) || errors.As(err, &shape) || errors.As(err, &syntax)
	case "":
		if err != nil {
			t.Errorf("native reference %s row %d: unexpected error %v", want.File, want.Row, err)
			return
		}
		same, compareErr := sameNativeOutput(want.Out, out)
		if compareErr != nil {
			t.Fatal(compareErr)
		}
		if !same {
			t.Errorf("native reference %s row %d %s: want %s, got %s", want.File, want.Row, fn, want.Out, encode(out))
		}
		return
	default:
		t.Fatalf("unknown typed reference error kind %q", want.ErrorKind)
	}
	if !validError {
		t.Errorf("native reference %s row %d %s: want %s %q, got %T %v", want.File, want.Row, fn, want.ErrorKind, want.Message, err, err)
	}
}

func isIntegralInt64(v any) (int64, bool) {
	var number model.Int
	if err := decodeTypedArg(v, &number); err != nil {
		return 0, false
	}
	return int64(number), true
}
