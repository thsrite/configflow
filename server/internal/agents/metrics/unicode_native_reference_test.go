package metrics

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"
	"unicode/utf8"

	"configflow/internal/jsonx"
	"golang.org/x/text/cases"
)

type nativeCaseRow struct {
	oldLower, newLower                       string
	oldAfter, oldBefore, newAfter, newBefore bool
}
type nativeCaseReference struct{ lower, sigma map[rune]nativeCaseRow }

func loadNativeCaseReference(t *testing.T) nativeCaseReference {
	t.Helper()
	if cases.UnicodeVersion != "17.0.0" {
		t.Fatal("unreviewed native Unicode version", cases.UnicodeVersion)
	}
	policy := struct {
		path, sha    string
		lower, sigma int
	}{"testdata/n810_unicode17_native.json", "9407e75fcbec0ab213813acb038bb8176834e861181e4358ef642ec26676f268", 55, 307}
	data, err := os.ReadFile(policy.path)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != policy.sha {
		t.Fatal("independent official UCD expectations changed")
	}
	var reference struct {
		Archive string `json:"corpus_sha256"`
		Version string `json:"unicode_version"`
		Lower   []struct {
			Original json.RawMessage `json:"original"`
			SHA      string          `json:"sha256"`
			Native   struct {
				Lower string `json:"lower"`
			} `json:"native"`
			Evidence string `json:"evidence"`
		} `json:"lower"`
		Sigma []struct {
			Original json.RawMessage `json:"original"`
			SHA      string          `json:"sha256"`
			Native   struct {
				After  bool `json:"after"`
				Before bool `json:"before"`
			} `json:"native"`
			Evidence []string `json:"evidence"`
		} `json:"sigma"`
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if reference.Archive != "0e2e8016db1a1976c3af5f4d0c9c3d1dae3e7befe6bf190985df44235a17cca7" || cases.UnicodeVersion != reference.Version || len(reference.Lower) != policy.lower || len(reference.Sigma) != policy.sigma {
		t.Fatal("unreviewed Unicode policy scope")
	}
	archive, err := os.ReadFile("testdata/a05_reference.json")
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(archive)) != reference.Archive {
		t.Fatal("original A05 archive changed", err)
	}
	result := nativeCaseReference{lower: map[rune]nativeCaseRow{}, sigma: map[rune]nativeCaseRow{}}
	bound := func(original json.RawMessage, hash string) *jsonx.Object {
		t.Helper()
		value, err := jsonx.Parse(original)
		if err != nil {
			t.Fatal(err)
		}
		row := value.(*jsonx.Object)
		raw, err := jsonx.Compact(row)
		if err != nil || fmt.Sprintf("%x", sha256.Sum256(raw)) != hash {
			t.Fatal("original Unicode row binding changed")
		}
		return row
	}
	codepoint := func(row *jsonx.Object) rune {
		t.Helper()
		cp, err := strconv.ParseInt(row.GetString("codepoint"), 16, 32)
		if err != nil || !utf8.ValidRune(rune(cp)) {
			t.Fatal("invalid bound codepoint", err)
		}
		return rune(cp)
	}
	for _, entry := range reference.Lower {
		row := bound(entry.Original, entry.SHA)
		cp := codepoint(row)
		if entry.Evidence == "" || row.GetString("lower") == entry.Native.Lower {
			t.Fatal("empty official lower exception", cp)
		}
		if _, duplicate := result.lower[cp]; duplicate {
			t.Fatal("duplicate lower row", cp)
		}
		result.lower[cp] = nativeCaseRow{oldLower: row.GetString("lower"), newLower: entry.Native.Lower}
	}
	for _, entry := range reference.Sigma {
		row := bound(entry.Original, entry.SHA)
		cp := codepoint(row)
		if len(entry.Evidence) == 0 {
			t.Fatal("missing official sigma evidence", cp)
		}
		oldAfter, oldBefore := row.GetBool("after"), row.GetBool("before")
		if oldAfter == entry.Native.After && oldBefore == entry.Native.Before {
			t.Fatal("empty sigma exception", cp)
		}
		if _, duplicate := result.sigma[cp]; duplicate {
			t.Fatal("duplicate sigma row", cp)
		}
		result.sigma[cp] = nativeCaseRow{oldAfter: oldAfter, oldBefore: oldBefore, newAfter: entry.Native.After, newBefore: entry.Native.Before}
	}
	return result
}

func (r nativeCaseReference) checkLower(cp rune, got, original string) error {
	expected := original
	if row, present := r.lower[cp]; present {
		if original != row.oldLower {
			return fmt.Errorf("U+%04X original lower expectation changed", cp)
		}
		expected = row.newLower
	}
	if got != expected {
		return fmt.Errorf("U+%04X lower=%q independent want=%q", cp, got, expected)
	}
	return nil
}
func (r nativeCaseReference) checkSigma(cp rune, after, before, originalAfter, originalBefore bool) error {
	wantAfter, wantBefore := originalAfter, originalBefore
	if row, present := r.sigma[cp]; present {
		if originalAfter != row.oldAfter || originalBefore != row.oldBefore {
			return fmt.Errorf("U+%04X original sigma expectations changed", cp)
		}
		wantAfter, wantBefore = row.newAfter, row.newBefore
	}
	if after != wantAfter || before != wantBefore {
		return fmt.Errorf("U+%04X final sigma=%v/%v independent want=%v/%v", cp, after, before, wantAfter, wantBefore)
	}
	return nil
}

func TestNativeUnicodePolicyRowsAreActuallyChecked(t *testing.T) {
	reference := loadNativeCaseReference(t)
	if len(reference.lower)+len(reference.sigma) == 0 {
		t.Fatal("empty policy binding coverage")
	}
	if reference.checkLower('A', "forged value", "a") == nil {
		t.Fatal("normal lowercase comparison bypassed")
	}
	for cp, row := range reference.lower {
		if reference.checkLower(cp, row.newLower, row.oldLower) != nil || reference.checkLower(cp, row.oldLower, row.oldLower) == nil || reference.checkLower(cp, row.newLower, "forged original") == nil {
			t.Fatal("lower bound row bypassed", cp)
		}
	}
	for cp, row := range reference.sigma {
		if reference.checkSigma(cp, row.newAfter, row.newBefore, row.oldAfter, row.oldBefore) != nil || reference.checkSigma(cp, row.oldAfter, row.oldBefore, row.oldAfter, row.oldBefore) == nil || reference.checkSigma(cp, row.newAfter, row.newBefore, !row.oldAfter, row.oldBefore) == nil {
			t.Fatal("sigma bound row bypassed", cp)
		}
	}
}

func FuzzNativeTrafficDomainCase(f *testing.F) {
	for _, text := range []string{"example.com", "ΟΣ.example", "ΟΣΑ.example", "İ.example", "\u1c89.example", "AΣ\u0295"} {
		f.Add(text)
	}
	f.Fuzz(func(t *testing.T, value string) {
		if !utf8.ValidString(value) {
			return
		}
		lower := lowerTrafficDomain(value)
		if !utf8.ValidString(lower) || lowerTrafficDomain(lower) != lower {
			t.Fatal("lowercase lost UTF-8 or idempotence")
		}
	})
}
