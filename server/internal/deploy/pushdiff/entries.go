package pushdiff

import (
	"bytes"
	"compress/zlib"
	"configflow/internal/jsondecode"
	"io"
	"strings"

	"configflow/internal/jsonresp"
	"configflow/internal/redact"
	"golang.org/x/text/cases"
)

// Query is normalized search text; its zero value means no filter.
type Query string

// SearchQuery folds case, normalizes line breaks/NUL, and trims whitespace.
func SearchQuery(text string) Query {
	text = strings.NewReplacer("\n", " ", "\r", " ", "\x00", " ").Replace(text)
	return Query(cases.Fold().String(strings.TrimSpace(text)))
}

type storedGroup struct {
	count           int
	entries, search []byte
}

// FileEntries keeps bounded zlib-compressed entry and public-search lines.
// It is immutable after construction and safe for concurrent readers.
type FileEntries struct{ groups map[string]storedGroup }

// Size returns compressed bytes held by all groups.
func (f *FileEntries) Size() int {
	n := 0
	if f != nil {
		for _, g := range f.groups {
			n += len(g.entries) + len(g.search)
		}
	}
	return n
}
func inflate(data []byte) []byte {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	defer r.Close()
	out, err := io.ReadAll(io.LimitReader(r, MaxKeptFileBytes+1))
	if err != nil || len(out) > MaxKeptFileBytes {
		return nil
	}
	return out
}

// Page returns up to MaxPageSize entries and MaxPageBytes (at least one entry).
// Search never includes marked credentials or stored credential ranges.
func (f *FileEntries) Page(change string, offset, limit int, q Query, find func(string) []redact.Range) Page {
	offset = max(0, offset)
	if limit <= 0 {
		limit = PageSize
	}
	limit = min(limit, MaxPageSize)
	page := Page{Offset: offset, Entries: []Entry{}}
	if f == nil {
		return page
	}
	g, ok := f.groups[change]
	if !ok {
		return page
	}
	lines := bytes.Split(inflate(g.entries), []byte{'\n'})
	indices := []int{}
	if q != "" {
		search := bytes.Split(inflate(g.search), []byte{'\n'})
		for i := 0; i < g.count && i < len(search); i++ {
			line := string(search[i])
			if strings.Contains(cases.Fold().String(line), string(q)) {
				if find != nil {
					ranges := find(line)
					if len(ranges) > 0 && !matchesOutside(line, string(q), ranges) {
						continue
					}
				}
				indices = append(indices, i)
			}
		}
	} else {
		indices = make([]int, g.count)
		for i := range indices {
			indices[i] = i
		}
	}
	page.Total = len(indices)
	if offset >= len(indices) {
		return page
	}
	size := 0
	for _, i := range indices[offset:min(len(indices), offset+limit)] {
		if i >= len(lines) {
			break
		}
		raw := lines[i]
		if len(page.Entries) > 0 && size+len(raw) > MaxPageBytes {
			break
		}
		var entry Entry
		if jsondecode.Unmarshal(raw, &entry) != nil {
			break
		}
		page.Entries = append(page.Entries, entry)
		size += len(raw)
	}
	end := offset + len(page.Entries)
	if end < page.Total {
		page.NextOffset = &end
	}
	return page
}
func matchesOutside(s, needle string, ranges []redact.Range) bool {
	r := []rune(s)
	var b strings.Builder
	pos := 0
	for _, span := range mergeRanges(ranges, len(r)) {
		b.WriteString(string(r[pos:span.Start]))
		b.WriteByte(0)
		pos = span.End
	}
	b.WriteString(string(r[pos:]))
	return strings.Contains(cases.Fold().String(b.String()), needle)
}
func publicPieces(t *Text) []string {
	out := []string{}
	if t != nil {
		for _, p := range t.merged() {
			if !p.secret && p.text != "" {
				out = append(out, p.text)
			}
		}
	}
	return out
}
func searchText(e Entry) string {
	parts := append(publicPieces(&e.Text), publicPieces(e.Info)...)
	if e.Fields != nil {
		for _, f := range *e.Fields {
			parts = append(parts, f.Key)
			if f.Sensitive {
				continue
			}
			parts = append(parts, publicPieces(f.Old)...)
			parts = append(parts, publicPieces(f.New)...)
			if f.List != nil {
				for _, t := range append(append([]Text{}, f.List.AddedItems...), f.List.RemovedItems...) {
					parts = append(parts, publicPieces(&t)...)
				}
			}
		}
	}
	return strings.ReplaceAll(strings.Join(parts, "\x00"), "\n", " ")
}

// encodeEntry uses standard JSON; spaced counts include display separators,
// while storage uses compact JSON lines.
func encodeEntry(e Entry, spaced bool) []byte {
	raw, _ := jsonresp.EncodeUTF8(e)
	if !spaced {
		return raw
	}
	var out bytes.Buffer
	quoted, escaped := false, false
	for _, c := range raw {
		out.WriteByte(c)
		if escaped {
			escaped = false
			continue
		}
		if quoted && c == '\\' {
			escaped = true
			continue
		}
		if c == '"' {
			quoted = !quoted
		}
		if !quoted && (c == ',' || c == ':') {
			out.WriteByte(' ')
		}
	}
	return out.Bytes()
}

type detailBudget struct {
	left  int
	spent bool
}

func (b *detailBudget) take(e Entry) bool {
	if b.spent {
		return false
	}
	n := len(encodeEntry(e, true))
	if n > b.left {
		b.spent = true
		return false
	}
	b.left -= n
	return true
}

type keeper struct {
	left int
	last *FileEntries
}
type groupWriter struct {
	quota, limit, count, raw int
	full                     bool
	entries, search          bytes.Buffer
	ew, sw                   *zlib.Writer
}

func newWriter(quota, limit int) *groupWriter {
	w := &groupWriter{quota: quota, limit: limit, full: quota <= 0 || limit <= 0}
	w.ew = zlib.NewWriter(&w.entries)
	w.sw = zlib.NewWriter(&w.search)
	return w
}
func (w *groupWriter) add(e Entry) bool {
	if w.full {
		return false
	}
	line := append(encodeEntry(e, false), '\n')
	search := []byte(searchText(e) + "\n")
	size := len(line) + len(search)
	if size > w.limit-w.raw {
		w.full = true
		return false
	}
	w.raw += size
	w.count++
	_, _ = w.ew.Write(line)
	_, _ = w.sw.Write(search)
	if w.count >= w.quota {
		w.full = true
	}
	return true
}
func (w *groupWriter) close() storedGroup {
	_ = w.ew.Close()
	_ = w.sw.Close()
	return storedGroup{w.count, w.entries.Bytes(), w.search.Bytes()}
}
func quotas(totals []int, limit int) []int {
	out := make([]int, len(totals))
	pending := []int{}
	for i, total := range totals {
		if total > 0 {
			pending = append(pending, i)
		}
	}
	for limit > 0 && len(pending) > 0 {
		share := max(1, limit/len(pending))
		next := []int{}
		for _, i := range pending {
			give := min(share, totals[i]-out[i], limit)
			out[i] += give
			limit -= give
			if out[i] < totals[i] {
				next = append(next, i)
			}
		}
		pending = next
	}
	return out
}

type producer func(change string, yield func(Entry) bool)

func listGroups(kind string, changes []string, totals []int, produce producer, budget *detailBudget, keep *keeper, repeated int) *Details {
	details := &Details{Kind: kind, Groups: []Group{}, Repeated: repeated}
	var stored *FileEntries
	available := map[string]int{}
	left, pending := 0, 0
	var quota []int
	if keep != nil && keep.left > 0 {
		stored = &FileEntries{groups: map[string]storedGroup{}}
		left = min(MaxKeptFileBytes, keep.left)
		quota = quotas(totals, MaxKeptEntries)
		for _, n := range totals {
			if n > 0 {
				pending++
			}
		}
	}
	for i, change := range changes {
		if totals[i] == 0 {
			continue
		}
		var writer *groupWriter
		if stored != nil {
			writer = newWriter(quota[i], left/max(1, pending))
		}
		group := Group{Change: change, Total: totals[i], Entries: []Entry{}}
		showing := true
		produce(change, func(e Entry) bool {
			if showing {
				showing = len(group.Entries) < FirstPage && budget.take(e)
				if showing {
					group.Entries = append(group.Entries, e)
				}
			}
			kept := writer != nil && writer.add(e)
			return showing || kept
		})
		if writer != nil {
			left -= writer.raw
			keep.left -= writer.raw
			pending--
			stored.groups[change] = writer.close()
			available[change] = writer.count
		}
		group.Omitted = group.Total - len(group.Entries)
		details.Groups = append(details.Groups, group)
	}
	details.Limited = budget.spent
	any := false
	for _, n := range available {
		any = any || n > 0
	}
	if any {
		keep.last = stored
		details.Paging = true
		for i := range details.Groups {
			g := &details.Groups[i]
			g.Available = ptr(available[g.Change])
		}
	}
	return details
}
