// Package pushdiff compares immutable deployment bundles for the pending-push
// dialog. It performs no network, repository, or device operations.
package pushdiff

import "configflow/internal/deploy"

// Response and storage bounds are shared by config, file, and paging views.
const (
	MaxConfigRows    = 200
	MaxListEntries   = 60
	MaxEntryFields   = 20
	MaxFieldItems    = 10
	MaxValueChars    = 240
	MaxRawLines      = 1500
	MaxRawBytes      = 160 * 1024
	MaxRawInputLines = 60000
	MaxListedFiles   = 500
	MaxDepth         = 64
	FirstPage        = 20
	MaxDetailBytes   = 256 * 1024
	MaxKeptEntries   = 100000
	MaxKeptFileBytes = 16 * 1024 * 1024
	MaxKeptBytes     = 48 * 1024 * 1024
	PageSize         = 50
	MaxPageSize      = 200
	MaxPageBytes     = 512 * 1024
	MaxQueryChars    = 200
	FormatNoteBytes  = 512
)

// AppliedView is the captured baseline identity; nil Files means no saved digests.
type AppliedView struct {
	ServiceType string
	Files       map[string]string
}

// Diff is the complete pending-push response. A missing baseline has only status,
// files and reason; MarshalJSON preserves that distinction from a full result.
type Diff struct {
	Status           string      `json:"status"`
	Files            []File      `json:"files"`
	Reason           string      `json:"reason,omitempty"`
	Content          string      `json:"content,omitempty"`
	ContentReason    *string     `json:"content_reason"`
	Counts           Counts      `json:"counts"`
	FilesOmitted     int         `json:"files_omitted"`
	Unchanged        []File      `json:"unchanged"`
	UnchangedOmitted int         `json:"unchanged_omitted"`
	Config           *ConfigDiff `json:"config"`
}

// Counts counts every manifest member, including unlisted files.
type Counts struct {
	Added     int `json:"added"`
	Removed   int `json:"removed"`
	Modified  int `json:"modified"`
	Unchanged int `json:"unchanged"`
}

// File is one public manifest member and its optional content comparison.
type File struct {
	Path    string       `json:"path"`
	Role    *deploy.Role `json:"role"`
	Status  string       `json:"status"`
	Name    *Text        `json:"name,omitempty"`
	Summary *Summary     `json:"summary,omitempty"`
	Details *Details     `json:"details,omitempty"`
}

// Summary holds either byte sizes, config changes, node counts, or rule counts.
type Summary struct {
	Kind        string `json:"kind"`
	Before      *int   `json:"before"`
	After       *int   `json:"after"`
	Added       *int   `json:"added,omitempty"`
	Removed     *int   `json:"removed,omitempty"`
	Modified    *int   `json:"modified,omitempty"`
	Changes     *int   `json:"changes,omitempty"`
	Reformatted *bool  `json:"reformatted,omitempty"`
}

// ConfigDiff reports YAML leaves and the canonical unified diff.
type ConfigDiff struct {
	Format      string   `json:"format"`
	Status      string   `json:"status"`
	Reason      string   `json:"reason,omitempty"`
	Items       []Row    `json:"items"`
	Omitted     int      `json:"omitted"`
	Total       *int     `json:"total,omitempty"`
	Reformatted *bool    `json:"reformatted,omitempty"`
	Raw         *RawDiff `json:"raw,omitempty"`
}

// Row is a changed leaf or list. Nested entry rows omit Path.
type Row struct {
	Key       string    `json:"key"`
	Path      *[]string `json:"path,omitempty"`
	Change    string    `json:"change"`
	Sensitive bool      `json:"sensitive"`
	Old       *Text     `json:"old"`
	New       *Text     `json:"new"`
	List      *ListDiff `json:"list,omitempty"`
}

// ListDiff counts list edits, with either detailed entries or brief field items.
type ListDiff struct {
	Before       int     `json:"before"`
	After        int     `json:"after"`
	Added        int     `json:"added"`
	Removed      int     `json:"removed"`
	Modified     int     `json:"modified"`
	Reordered    bool    `json:"reordered"`
	Identity     string  `json:"identity,omitempty"`
	Entries      []Entry `json:"entries"`
	Omitted      int     `json:"omitted"`
	AddedItems   []Text  `json:"added_items"`
	RemovedItems []Text  `json:"removed_items"`
	brief        bool
}

// Entry is one node, rule, or inline list edit.
type Entry struct {
	Change        string `json:"change"`
	Text          Text   `json:"text"`
	Info          *Text  `json:"info,omitempty"`
	Nth           int    `json:"nth,omitempty"`
	Fields        *[]Row `json:"fields,omitempty"`
	FieldsOmitted *int   `json:"fields_omitted,omitempty"`
}

// Details contains the first page of every nonempty change group.
type Details struct {
	Kind     string  `json:"kind"`
	Groups   []Group `json:"groups"`
	Limited  bool    `json:"limited,omitempty"`
	Repeated int     `json:"repeated,omitempty"`
	Paging   bool    `json:"paging,omitempty"`
}

// Group carries exact totals and the number retained for paging.
type Group struct {
	Change    string  `json:"change"`
	Total     int     `json:"total"`
	Entries   []Entry `json:"entries"`
	Omitted   int     `json:"omitted"`
	Available *int    `json:"available,omitempty"`
}

// RawDiff is a bounded unified diff or a reason that it was too large.
type RawDiff struct {
	Lines        []Text `json:"lines"`
	Truncated    bool   `json:"truncated"`
	OmittedLines int    `json:"omitted_lines"`
	Reason       string `json:"reason,omitempty"`
	TotalLines   int    `json:"total_lines,omitempty"`
}

// Page is a bounded, optionally filtered slice of a compressed group.
type Page struct {
	Total      int     `json:"total"`
	Offset     int     `json:"offset"`
	Entries    []Entry `json:"entries"`
	NextOffset *int    `json:"next_offset"`
}

func ptr[T any](v T) *T { return &v }
