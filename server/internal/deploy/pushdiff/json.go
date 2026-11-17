package pushdiff

import "encoding/json"

// MarshalJSON omits full-comparison fields when no baseline exists.
func (d Diff) MarshalJSON() ([]byte, error) {
	if d.Reason != "" {
		return json.Marshal(struct {
			Status string `json:"status"`
			Files  []File `json:"files"`
			Reason string `json:"reason"`
		}{d.Status, d.Files, d.Reason})
	}
	type plain Diff
	return json.Marshal(plain(d))
}

// MarshalJSON selects a leaf-value or list row without ambiguous null fields.
func (r Row) MarshalJSON() ([]byte, error) {
	if r.List == nil {
		type leaf Row
		return json.Marshal(leaf(r))
	}
	return json.Marshal(struct {
		Key       string    `json:"key"`
		Path      *[]string `json:"path,omitempty"`
		Change    string    `json:"change"`
		Sensitive bool      `json:"sensitive"`
		List      *ListDiff `json:"list"`
	}{r.Key, r.Path, r.Change, r.Sensitive, r.List})
}

// MarshalJSON distinguishes brief nested fields from full list comparisons.
func (l ListDiff) MarshalJSON() ([]byte, error) {
	if !l.brief {
		return json.Marshal(struct {
			Before    int     `json:"before"`
			After     int     `json:"after"`
			Added     int     `json:"added"`
			Removed   int     `json:"removed"`
			Modified  int     `json:"modified"`
			Reordered bool    `json:"reordered"`
			Entries   []Entry `json:"entries"`
			Omitted   int     `json:"omitted"`
			Identity  string  `json:"identity,omitempty"`
		}{l.Before, l.After, l.Added, l.Removed, l.Modified, l.Reordered, l.Entries, l.Omitted, l.Identity})
	}
	return json.Marshal(struct {
		Before       int    `json:"before"`
		After        int    `json:"after"`
		Added        int    `json:"added"`
		Removed      int    `json:"removed"`
		Modified     int    `json:"modified"`
		Reordered    bool   `json:"reordered"`
		AddedItems   []Text `json:"added_items"`
		RemovedItems []Text `json:"removed_items"`
	}{l.Before, l.After, l.Added, l.Removed, l.Modified, l.Reordered, l.AddedItems, l.RemovedItems})
}

// MarshalJSON reports only the input limit fields when rendering was skipped.
func (r RawDiff) MarshalJSON() ([]byte, error) {
	if r.Reason != "" {
		return json.Marshal(struct {
			Lines  []Text `json:"lines"`
			Reason string `json:"reason"`
			Total  int    `json:"total_lines"`
		}{nil, r.Reason, r.TotalLines})
	}
	return json.Marshal(struct {
		Lines     []Text `json:"lines"`
		Truncated bool   `json:"truncated"`
		Omitted   int    `json:"omitted_lines"`
	}{r.Lines, r.Truncated, r.OmittedLines})
}

// UnmarshalJSON retains the brief form when paged node fields are decoded.
func (l *ListDiff) UnmarshalJSON(data []byte) error {
	type plain ListDiff
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*l = ListDiff(value)
	_, l.brief = fields["added_items"]
	return nil
}
