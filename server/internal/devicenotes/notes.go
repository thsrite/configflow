// Package devicenotes owns display-only client names, legacy policy-name
// fallback and name edit plans. It does not write settings or publish policies.
package devicenotes

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"configflow/internal/clientrange"
)

// MaxNotes is the maximum number of explicit stored display names.
const MaxNotes = 2048

// Names includes empty names that deliberately hide legacy policy names.
type Names map[string]string

// NormalizeName validates a display name before trimming surrounding space.
func NormalizeName(name string) (string, error) {
	if utf8.RuneCountInString(name) > 80 || strings.ContainsFunc(name, func(r rune) bool { return r < 32 || r == 127 }) {
		return "", errors.New(NameMessage)
	}
	return strings.TrimSpace(name), nil
}

type noteEntry struct {
	address string
	name    json.RawMessage
}

// Parse validates a stored JSON object, preserving entry order for diagnostics.
func Parse(raw json.RawMessage) (Names, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, errors.New(NotesMessage)
	}
	var entries []noteEntry
	positions := map[string]int{}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, errors.New(NotesMessage)
		}
		address, ok := key.(string)
		if !ok {
			return nil, errors.New(NotesMessage)
		}
		var name json.RawMessage
		if err := decoder.Decode(&name); err != nil {
			return nil, errors.New(NotesMessage)
		}
		if i, exists := positions[address]; exists {
			entries[i].name = name
		} else {
			positions[address] = len(entries)
			entries = append(entries, noteEntry{address, name})
		}
	}
	if _, err := decoder.Token(); err != nil {
		return nil, errors.New(NotesMessage)
	}
	var extra json.RawMessage
	if decoder.Decode(&extra) != io.EOF || len(entries) > MaxNotes {
		return nil, errors.New(NotesMessage)
	}
	names := Names{}
	for _, entry := range entries {
		address, ok := clientrange.Normalize(entry.address)
		if !ok {
			return nil, errors.New(AddressMessage)
		}
		if _, exists := names[address]; exists {
			return nil, fmt.Errorf(duplicateNoteMessage, address)
		}
		var name *string
		if json.Unmarshal(entry.name, &name) != nil || name == nil {
			return nil, errors.New(NameMessage)
		}
		text, err := NormalizeName(*name)
		if err != nil {
			return nil, err
		}
		names[address] = text
	}
	return names, nil
}

func normalizeNames(notes Names) (Names, error) {
	if len(notes) > MaxNotes {
		return nil, errors.New(NotesMessage)
	}
	result := Names{}
	for _, key := range slices.Sorted(maps.Keys(notes)) {
		address, ok := clientrange.Normalize(key)
		if !ok {
			return nil, errors.New(AddressMessage)
		}
		if _, exists := result[address]; exists {
			return nil, fmt.Errorf(duplicateNoteMessage, address)
		}
		name, err := NormalizeName(notes[key])
		if err != nil {
			return nil, err
		}
		result[address] = name
	}
	return result, nil
}

// State is a validated snapshot of notes and legacy policy rows.
type State struct {
	notes    Names
	policies []Policy
}

// New reads the two stored fields. Callers supply {} and [] for absent fields.
func New(notes, policies json.RawMessage) (State, error) {
	rows, err := ParsePolicies(policies)
	if err != nil {
		return State{}, err
	}
	names, err := Parse(notes)
	if err != nil {
		return State{}, err
	}
	return State{names, rows}, nil
}

// StoredNames returns an owned merged map, including explicit hidden names.
func (s State) StoredNames() Names {
	result := Names{}
	for _, row := range s.policies {
		if row.Name != "" {
			result[row.IP] = row.Name
		}
	}
	maps.Copy(result, s.notes)
	return result
}

// ReadNames returns only names that should be displayed.
func (s State) ReadNames() Names {
	result := s.StoredNames()
	for ip, name := range result {
		if name == "" {
			delete(result, ip)
		}
	}
	return result
}

func (s State) assign(notes Names, ip, name string) {
	if name != "" {
		notes[ip] = name
		return
	}
	for _, row := range s.policies {
		if row.IP == ip && row.Name != "" {
			notes[ip] = ""
			return
		}
	}
	delete(notes, ip)
}

// Set returns replacement notes without modifying the snapshot or policy rows.
func (s State) Set(ip, name string) (Names, error) {
	notes := Names{}
	maps.Copy(notes, s.notes)
	s.assign(notes, ip, name)
	return normalizeNames(notes)
}

// Set changes stored notes, consulting legacy policies only when hiding a name.
// A valid nonempty note can repair a profile whose unrelated legacy rows are bad.
func Set(rawNotes, rawPolicies json.RawMessage, ip, name string) (Names, error) {
	notes, err := Parse(rawNotes)
	if err != nil {
		return nil, err
	}
	state := State{notes: notes}
	if name == "" {
		state.policies, err = ParsePolicies(rawPolicies)
		if err != nil {
			return nil, err
		}
	}
	return state.Set(ip, name)
}

// Revision identifies names shown beside the stored policy rows. The digest is
// based on ordered rows and Go-native compact UTF-8 JSON, not map iteration.
func (s State) Revision() (string, error) {
	names := s.ReadNames()
	rows := make([][2]string, 0, len(s.policies))
	for _, row := range s.policies {
		rows = append(rows, [2]string{row.IP, names[row.IP]})
	}
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(rows); err != nil {
		return "", fmt.Errorf("encode device name revision: %w", err)
	}
	// This digest is persisted alongside edits, so retain UTF-8 separator
	// characters as the stored revision protocol requires. Consume other JSON
	// escapes atomically: a literal backslash followed by u2028 is user text.
	raw := bytes.TrimSuffix(encoded.Bytes(), []byte("\n"))
	utf8JSON := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\\' && i+1 < len(raw) {
			if bytes.HasPrefix(raw[i:], []byte(`\u2028`)) {
				utf8JSON = append(utf8JSON, "\u2028"...)
				i += 5
				continue
			}
			if bytes.HasPrefix(raw[i:], []byte(`\u2029`)) {
				utf8JSON = append(utf8JSON, "\u2029"...)
				i += 5
				continue
			}
			utf8JSON = append(utf8JSON, raw[i], raw[i+1])
			i++
			continue
		}
		utf8JSON = append(utf8JSON, raw[i])
	}
	sum := sha256.Sum256(utf8JSON)
	return hex.EncodeToString(sum[:]), nil
}

// Prepare returns normalized rows and a separate display-name write plan.
func (s State) Prepare(rows []Policy) ([]Policy, Names, error) {
	rows, err := normalizePolicies(rows)
	if err != nil {
		return nil, nil, err
	}
	names, notes := s.ReadNames(), Names{}
	maps.Copy(notes, s.notes)
	previous, kept := map[string]bool{}, map[string]bool{}
	for _, row := range s.policies {
		previous[row.IP] = true
	}
	for i := range rows {
		row := &rows[i]
		if !previous[row.IP] && row.Name == "" {
			row.Name = names[row.IP]
		}
		s.assign(notes, row.IP, row.Name)
		kept[row.IP] = true
	}
	for _, row := range s.policies {
		if row.Name == "" || kept[row.IP] {
			continue
		}
		if name, exists := notes[row.IP]; exists {
			if name == "" {
				delete(notes, row.IP)
			}
		} else if len(notes) < MaxNotes {
			notes[row.IP] = row.Name
		}
	}
	notes, err = normalizeNames(notes)
	return rows, notes, err
}

// SameRoutes compares only addresses and modes, preserving row order.
func SameRoutes(a, b []Policy) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].IP != b[i].IP || a[i].Mode != b[i].Mode {
			return false
		}
	}
	return true
}
