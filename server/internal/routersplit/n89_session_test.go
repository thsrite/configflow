package routersplit

import (
	"configflow/internal/jsonx"
	"os"
	"testing"
)

func n89SessionPolicy(t *testing.T, input []*jsonx.Object, old string, actual *jsonx.Object) {
	t.Helper()
	raw, err := os.ReadFile("testdata/n89_session_expectation.json")
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonx.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	expected := value.(*jsonx.Object)
	if !jsonx.Equal(jsonx.ListOf(input), expected.Value("input")) {
		t.Fatal("independent session input changed")
	}
	if encode(expected.Value("original")) != old {
		t.Fatal("original Python session answer changed")
	}
	if !jsonx.Equal(actual, expected.Value("native")) {
		t.Fatalf("R5 completion policy got %s want %s", encode(actual), encode(expected.Value("native")))
	}
}
