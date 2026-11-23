package installer

import (
	"encoding/json"
	"math/big"
	"strings"

	"configflow/internal/taskstatus"
)

func parseInstallInteger(text string) (*big.Int, error) {
	if strings.TrimSpace(text) == "" {
		return nil, invalidInput("Invalid installer integer")
	}
	raw, err := json.Marshal(text)
	if err != nil {
		return nil, err
	}
	n, err := taskstatus.Count(raw)
	if err != nil {
		return nil, invalidInput("Invalid installer integer")
	}
	return n, nil
}
func inRange(n *big.Int, low, high int64) bool {
	return n.Cmp(big.NewInt(low)) >= 0 && n.Cmp(big.NewInt(high)) <= 0
}

// asciiDigits is re.fullmatch(r"[0-9]+", text).
func asciiDigits(text string) bool {
	if text == "" {
		return false
	}
	for i := 0; i < len(text); i++ {
		if text[i] < '0' || text[i] > '9' {
			return false
		}
	}
	return true
}

// matchesClass is re.fullmatch(r"[<class>]+", text) for an ASCII class given
// as the set of allowed bytes besides letters and digits.
func matchesClass(text, extra string) bool {
	if text == "" {
		return false
	}
	for i := 0; i < len(text); i++ {
		c := text[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte(extra, c) >= 0) {
			return false
		}
	}
	return true
}

// isUnitName is re.fullmatch(r"[A-Za-z0-9_][A-Za-z0-9_.@-]*", unit).
func isUnitName(unit string) bool {
	return unit != "" && matchesClass(unit[:1], "_") && (len(unit) == 1 || matchesClass(unit[1:], "_.@-"))
}
