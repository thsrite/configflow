package routerfake

import (
	"crypto/md5"
	"fmt"
	"maps"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const addressPath = "/ip/firewall/address-list"

var quotedList = regexp.MustCompile(`\(\$groups->("(?:[^"\\]|\\.)*")\)`)
var fetchList = regexp.MustCompile(`print as-value where list=("(?:[^"\\]|\\.)*")`)
var fetchRange = regexp.MustCompile(`\$i >= (\d+) && \$i < (\d+)`)
var quotedValue = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)

func consoleString(value string) string {
	parts, ok := splitConsole(value)
	if !ok || len(parts) != 1 {
		return ""
	}
	return parts[0]
}
func encodeMember(row Row) string {
	parts := []string{}
	for _, key := range slices.Sorted(maps.Keys(row)) {
		if key != "timeout" && key != "creation-time" {
			parts = append(parts, key+"="+text(row[key]))
		}
	}
	return strconv.Itoa(len(parts)) + ":" + strings.Join(parts, ";") + "\n"
}
func memberDigest(rows []Row) (int, string) {
	rows = slices.Clone(rows)
	slices.SortFunc(rows, func(a, b Row) int { return strings.Compare(text(a[".id"]), text(b[".id"])) })
	hashes := ""
	for start := 0; start < len(rows) || start == 0; start += 500 {
		chunk := ""
		for _, row := range rows[start:min(start+500, len(rows))] {
			chunk += encodeMember(row)
		}
		hashes += fmt.Sprintf("%x", md5.Sum([]byte(chunk)))
	}
	return len(rows), fmt.Sprintf("%x", md5.Sum([]byte(hashes)))
}
func (f *Fake) listMembers(name string) []Row {
	out := []Row{}
	for _, row := range f.tables[addressPath] {
		if text(row["list"]) == name {
			out = append(out, row)
		}
	}
	return out
}
func (f *Fake) executeList(script string, fault *Fault) (Row, bool, bool) {
	if strings.Contains(script, "cf-digest:") {
		lines := []string{}
		for i, m := range quotedList.FindAllStringSubmatch(script, -1) {
			n, hash := memberDigest(f.listMembers(consoleString(m[1])))
			lines = append(lines, fmt.Sprintf("cf-digest:%d:%d:%s", i, n, hash))
		}
		return Row{"ret": strings.Join(lines, "\r\n")}, false, true
	}
	if strings.Contains(script, "cf-rows:") {
		m, r := fetchList.FindStringSubmatch(script), fetchRange.FindStringSubmatch(script)
		if len(m) != 2 || len(r) != 3 {
			return Row{"ret": "syntax error"}, false, true
		}
		start, _ := strconv.Atoi(r[1])
		end, _ := strconv.Atoi(r[2])
		rows := f.listMembers(consoleString(m[1]))
		lines := []string{}
		for _, row := range rows[min(start, len(rows)):min(end, len(rows))] {
			lines = append(lines, "cf-row:"+strings.TrimSuffix(encodeMember(row), "\n"))
		}
		lines = append(lines, fmt.Sprintf("cf-rows:%d", len(rows)))
		return Row{"ret": strings.Join(lines, "\r\n")}, false, true
	}
	if strings.Contains(script, "cf-names:") {
		matches := quotedValue.FindAllString(script, -1)
		if len(matches) == 0 {
			return Row{"ret": "syntax error"}, false, true
		}
		pattern, err := regexp.Compile(consoleString(matches[0]))
		if err != nil {
			return Row{"ret": "syntax error"}, false, true
		}
		excluded := []string{}
		for _, q := range matches[1:] {
			excluded = append(excluded, consoleString(q))
		}
		names := map[string]bool{}
		for _, row := range f.tables[addressPath] {
			name := text(row["list"])
			if pattern.MatchString(name) && !slices.Contains(excluded, name) {
				names[name] = true
			}
		}
		return Row{"ret": "cf-names:\n" + strings.Join(slices.Sorted(maps.Keys(names)), "\n")}, false, true
	}
	if !strings.HasPrefix(script, ":local n 0\n:local m \"\"\n:foreach a in={") {
		return nil, false, false
	}
	items, tail, ok := strings.Cut(strings.TrimPrefix(script, ":local n 0\n:local m \"\"\n:foreach a in={"), "} do={:onerror e in={/ip/firewall/address-list/add ")
	if !ok {
		return Row{"ret": "syntax error"}, false, true
	}
	params, _, ok := strings.Cut(tail, " address=$a}")
	if !ok {
		return Row{"ret": "syntax error"}, false, true
	}
	_, shared, ok := parseAddCommand(addressPath + "/add " + params)
	if !ok {
		return Row{"ret": "syntax error"}, false, true
	}
	failed, first := 0, ""
	for i, q := range quotedValue.FindAllString(items, -1) {
		value := consoleString(q)
		values := cloneRow(shared)
		values["address"] = value
		wf := f.requestFault("PUT", addressPath)
		var err error
		if wf != nil && !wf.AfterCommit {
			err = fmt.Errorf("%v", wf.body()["detail"])
		} else {
			_, err = f.add(addressPath, values)
		}
		if wf != nil && wf.AfterCommit && wf.LoseReply {
			return nil, true, true
		}
		if fault != nil && fault.PatchCreated[i] != nil {
			rows := f.tables[addressPath]
			if len(rows) > 0 {
				maps.Copy(rows[len(rows)-1], fault.PatchCreated[i])
			}
		}
		if err != nil {
			failed++
			if first == "" {
				first = value + " " + err.Error()
			}
		}
	}
	if fault != nil {
		for path, rows := range fault.ExtraRows {
			for _, row := range rows {
				f.tables[path] = append(f.tables[path], cloneRow(row))
			}
		}
	}
	return Row{"ret": fmt.Sprintf("cf-done:%d:%s", failed, first)}, fault != nil && fault.AfterCommit && fault.LoseReply, true
}
func storedAddress(value string) string {
	p, err := netip.ParsePrefix(value)
	if err == nil {
		p = p.Masked()
		if p.Bits() == p.Addr().BitLen() {
			return p.Addr().String()
		}
		return p.String()
	}
	if a, err := netip.ParseAddr(value); err == nil {
		return a.String()
	}
	return value
}
