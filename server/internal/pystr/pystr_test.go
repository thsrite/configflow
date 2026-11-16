package pystr

import (
	"encoding/hex"
	"encoding/json"
	"math"
	"math/big"
	"os"
	"testing"

	"configflow/internal/jsonx"
)

type reference struct {
	Lower   [][2]string `json:"lower"`
	Strip   [][2]string `json:"strip"`
	IsDigit [][2]any    `json:"isdigit"`
	Decode  [][2]string `json:"decode"`
	Int     [][4]any    `json:"int"`
	Float   [][3]any    `json:"float"`
}

func loadReference(t *testing.T) reference {
	t.Helper()
	data, err := os.ReadFile("testdata/ref.json")
	if err != nil {
		t.Fatal(err)
	}
	var ref reference
	if err := json.Unmarshal(data, &ref); err != nil {
		t.Fatal(err)
	}
	return ref
}

// pyFloatRepr is repr(float) for the reference comparison.
func pyFloatRepr(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	case math.IsNaN(f):
		return "nan"
	}
	return jsonx.Float(f).String()
}

// pyHex is hex(int).
func pyHex(n *big.Int) string {
	if n.Sign() < 0 {
		return "-0x" + new(big.Int).Neg(n).Text(16)
	}
	return "0x" + n.Text(16)
}

func TestMatchesCPython(t *testing.T) {
	ref := loadReference(t)
	for _, row := range ref.Lower {
		if got := Lower(row[0]); got != row[1] {
			t.Errorf("Lower(%q) = %q, want %q", row[0], got, row[1])
		}
	}
	for _, row := range ref.Strip {
		if got := Strip(row[0]); got != row[1] {
			t.Errorf("Strip(%q) = %q, want %q", row[0], got, row[1])
		}
	}
	for _, row := range ref.IsDigit {
		if got := IsDigit(row[0].(string)); got != row[1].(bool) {
			t.Errorf("IsDigit(%q) = %v, want %v", row[0], got, row[1])
		}
	}
	for _, row := range ref.Decode {
		data, err := hex.DecodeString(row[0])
		if err != nil {
			t.Fatal(err)
		}
		if got := UTF8DecodeError(data); got != row[1] {
			t.Errorf("UTF8DecodeError(%x) = %q, want %q", data, got, row[1])
		}
	}
	for _, row := range ref.Int {
		text, base := row[0].(string), int(row[1].(float64))
		got, err := Int(text, base)
		switch {
		case row[2] != nil && (err != nil || pyHex(got) != row[2].(string)):
			t.Errorf("int(%q, %d) = %v, %v; want %s", text, base, got, err, row[2])
		case row[2] == nil && (err == nil || err.Error() != row[3].(string)):
			t.Errorf("int(%q, %d) = %v, %v; want error %q", text, base, got, err, row[3])
		}
	}
	for _, row := range ref.Float {
		text := row[0].(string)
		got, err := Float(text)
		switch {
		case row[1] != nil && (err != nil || pyFloatRepr(got) != row[1].(string)):
			t.Errorf("float(%q) = %v, %v; want %s", text, got, err, row[1])
		case row[1] == nil && (err == nil || err.Error() != row[2].(string)):
			t.Errorf("float(%q) = %v, %v; want error %q", text, got, err, row[2])
		}
	}
	t.Logf("%d lower, %d strip, %d isdigit, %d decode, %d int, %d float references", len(ref.Lower), len(ref.Strip),
		len(ref.IsDigit), len(ref.Decode), len(ref.Int), len(ref.Float))
}
