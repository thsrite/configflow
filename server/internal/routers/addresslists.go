package routers

import (
	"configflow/internal/jsondecode"
	"configflow/internal/routers/device"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// AddressDigest proves the exact stable fields of an address list.
type AddressDigest struct {
	Count int
	MD5   string
}

// EncodeAddressRow is RouterOS's associative-array text, without runtime fields.
func EncodeAddressRow(row Row) string {
	keys := slices.Sorted(maps.Keys(row))
	parts := []string{}
	for _, key := range keys {
		if key != "creation-time" && key != "timeout" {
			parts = append(parts, key+"="+row[key])
		}
	}
	return strconv.Itoa(len(parts)) + ":" + strings.Join(parts, ";") + "\n"
}

// DigestAddresses matches RouterOS's 500-row chunked MD5 proof, ordered by ID.
func DigestAddresses(rows []Row) AddressDigest {
	ordered := slices.Clone(rows)
	slices.SortFunc(ordered, func(a, b Row) int { return strings.Compare(a.id(), b.id()) })
	hashes := ""
	for start := 0; start < len(ordered) || start == 0; start += 500 {
		var chunk strings.Builder
		for _, row := range ordered[start:min(start+500, len(ordered))] {
			chunk.WriteString(EncodeAddressRow(row))
		}
		hashes += fmt.Sprintf("%x", md5.Sum([]byte(chunk.String())))
	}
	return AddressDigest{len(rows), fmt.Sprintf("%x", md5.Sum([]byte(hashes)))}
}

// RouterOS array-valued fields have no portable :tostr wire contract. Keep
// their full REST values rather than manufacturing a digest proof for them.
func digestableAddresses(rows []Row) bool {
	for _, row := range rows {
		for key, value := range row {
			if key == "timeout" || key == "creation-time" {
				continue
			}
			if structuredText(value) {
				return false
			}
		}
	}
	return true
}

func decodeAddressRow(text string) Row {
	count, body, ok := strings.Cut(text, ":")
	n, err := strconv.Atoi(count)
	if !ok || err != nil || n < 0 {
		return nil
	}
	parts := []string{}
	if body != "" {
		parts = strings.Split(body, ";")
	}
	if len(parts) != n {
		return nil
	}
	row := Row{}
	for _, part := range parts {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return nil
		}
		row[key] = value
	}
	return row
}

// AddressDigestScript produces one scan and one count/hash output per name.
func AddressDigestScript(names []string) string {
	lists := []string{}
	for _, name := range names {
		lists = append(lists, "list="+ScriptString(name))
	}
	lines := []string{":local groups ({})", `:foreach r in=[/ip/firewall/address-list/print as-value where (` + strings.Join(lists, " || ") + `)] do={:set ($r->"creation-time"); :set ($r->"timeout"); :local l ($r->"list"); :if ([:typeof ($groups->$l)] = "nothing") do={:set ($groups->$l) ({})}; :set ($groups->$l->[:tostr ($r->".id")]) ([:len $r] . ":" . [:tostr $r] . "\n")}`}
	for i, name := range names {
		lines = append(lines, "{\n:local chunk \"\"; :local hashes \"\"; :local n 0\n"+`:foreach id,line in=($groups->`+ScriptString(name)+`) do={:set chunk ($chunk . $line); :set n ($n+1); :if ($n % 500 = 0) do={:set hashes ($hashes . [:convert $chunk transform=md5 to=hex]); :set chunk ""}}`+"\n"+`:if (($n % 500 != 0) || ($n = 0)) do={:set hashes ($hashes . [:convert $chunk transform=md5 to=hex])}`+"\n"+fmt.Sprintf(`:put ("cf-digest:%d:" . $n . ":" . [:convert $hashes transform=md5 to=hex])`, i)+"\n}")
	}
	return strings.Join(lines, "\n")
}

func executeOutput(value json.RawMessage) string { return asRow(value).text("ret") }
func shortConsoleOutput(output string) string {
	s := strings.TrimSpace(output)
	if s == "" {
		return messageROSNoOutput
	}
	return string([]rune(s)[:min(200, len([]rune(s)))])
}

func (d *RouterOS) routerDigests(ctx context.Context, names []string) (map[string]AddressDigest, error) {
	value, err := d.request(ctx, "POST", "/execute", Row{"script": AddressDigestScript(names), "as-string": ""}, rosTimeout)
	if err != nil {
		return nil, err
	}
	output := executeOutput(value)
	result := map[string]AddressDigest{}
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r", ""), "\n") {
		if !strings.HasPrefix(line, "cf-digest:") {
			continue
		}
		parts := strings.SplitN(strings.TrimPrefix(line, "cf-digest:"), ":", 3)
		if len(parts) != 3 {
			continue
		}
		i, e := strconv.Atoi(parts[0])
		n, e2 := strconv.Atoi(parts[1])
		if e == nil && e2 == nil && i >= 0 && i < len(names) && n >= 0 {
			result[names[i]] = AddressDigest{n, strings.TrimSpace(parts[2])}
		}
	}
	if len(result) != len(names) {
		return nil, device.Error(fmt.Sprintf(messageAddressDigest, d.safeDeviceText(shortConsoleOutput(output))))
	}
	return result, nil
}

func (d *RouterOS) filteredAddresses(ctx context.Context, name string) ([]Row, error) {
	rows, err := d.list(ctx, rosAddresses+"?"+url.Values{"list": {name}}.Encode())
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(rows, func(row Row) bool { return row.text("list") != name }), nil
}

// FetchAddresses reads pages of 2000 console rows, falling back to scoped REST
// when separators, duplicate IDs or changing page totals invalidate the output.
func (d *RouterOS) FetchAddresses(ctx context.Context, name string) ([]Row, error) {
	rows := []Row{}
	total := -1
	for start := 0; ; start += 2000 {
		script := ":local i 0\n" + `:foreach r in=[/ip/firewall/address-list/print as-value where list=` + ScriptString(name) + fmt.Sprintf(`] do={:if ($i >= %d && $i < %d) do={:set ($r->"creation-time"); :set ($r->"timeout"); :put ("cf-row:" . [:len $r] . ":" . [:tostr $r])}; :set i ($i+1)}`, start, start+2000) + "\n" + `:put ("cf-rows:" . $i)`
		value, err := d.request(ctx, "POST", "/execute", Row{"script": script, "as-string": ""}, rosTimeout)
		if err != nil {
			return nil, err
		}
		output := executeOutput(value)
		n := -1
		valid := true
		for _, line := range strings.Split(strings.ReplaceAll(output, "\r", ""), "\n") {
			if strings.HasPrefix(line, "cf-rows:") {
				parsed, e := strconv.Atoi(strings.TrimPrefix(line, "cf-rows:"))
				if e == nil && n < 0 {
					n = parsed
				}
			}
			if strings.HasPrefix(line, "cf-row:") {
				row := decodeAddressRow(strings.TrimPrefix(line, "cf-row:"))
				if row == nil || row.text("list") != name {
					valid = false
				} else {
					rows = append(rows, row)
				}
			}
		}
		if !valid || n < 0 || (total >= 0 && total != n) {
			return d.filteredAddresses(ctx, name)
		}
		total = n
		if start+2000 >= total {
			if len(rows) != total || len(rowIDs(rows)) != len(rows) {
				return d.filteredAddresses(ctx, name)
			}
			return rows, nil
		}
	}
}

type addressProof struct {
	Digest *AddressDigest
	Rows   []Row
}

func cloneRows(rows []Row) []Row {
	out := make([]Row, len(rows))
	for i, row := range rows {
		out[i] = cloneRow(row)
	}
	return out
}

// RememberAddresses stores exact proven members for subsequent digest reads.
func (d *RouterOS) RememberAddresses(name string, rows []Row) {
	if d.addressCache == nil {
		d.addressCache = map[string][]Row{}
	}
	d.addressCache[name] = cloneRows(rows)
}

// ExpectAddresses proves earlier reads without widening the known-list scope.
func (d *RouterOS) ExpectAddresses(name string, rows []Row) {
	if d.addressExpected == nil {
		d.addressExpected = map[string][]Row{}
	}
	d.addressExpected[name] = cloneRows(rows)
}

// ReuseAddressReads scopes proof reuse to one preview/release. Nested scopes share
// the outer proof cache. The returned closure must be deferred by the caller.
func (d *RouterOS) ReuseAddressReads() func() {
	if d.addressReads != nil {
		return func() {}
	}
	d.addressReads = map[string]addressProof{}
	return func() { d.addressReads = nil }
}

// InvalidateAddressReads drops all proofs reused in the current scope before a write.
func (d *RouterOS) InvalidateAddressReads() { clear(d.addressReads) }
func (d *RouterOS) knownAddresses() map[string][]Row {
	known := map[string][]Row{}
	var manifest rosManifest
	_ = jsondecode.Unmarshal(d.policyManifest, &manifest)
	for _, item := range manifest.Resources {
		if item.Path == rosAddresses && item.Config.text("list") != "" {
			row := cloneRow(item.Config)
			row[".id"] = item.ID
			name := row.text("list")
			known[name] = append(known[name], row)
		}
	}
	maps.Copy(known, d.addressCache)
	return known
}

// ReadAddresses downloads only named lists, proving known contents by digest.
func (d *RouterOS) ReadAddresses(ctx context.Context, names []string) (map[string][]Row, error) {
	names = slices.Clone(names)
	slices.Sort(names)
	names = slices.Compact(names)
	known := d.knownAddresses()
	proof := maps.Clone(d.addressExpected)
	if proof == nil {
		proof = map[string][]Row{}
	}
	maps.Copy(proof, known)
	reads := d.addressReads
	if reads == nil {
		reads = map[string]addressProof{}
	}
	expected := map[string]AddressDigest{}
	unproven := []string{}
	reusable := map[string]bool{}
	for _, name := range names {
		rows, has := proof[name]
		if has && !digestableAddresses(rows) {
			has = false
		}
		var want *AddressDigest
		if has {
			v := DigestAddresses(rows)
			expected[name] = v
			want = &v
		}
		cached, ok := reads[name]
		if ok && cached.Digest == nil && want != nil && DigestAddresses(cached.Rows) == *want {
			cached.Digest = want
			reads[name] = cached
		}
		if ok && ((cached.Digest == nil && want == nil) || (cached.Digest != nil && want != nil && *cached.Digest == *want)) {
			reusable[name] = true
		} else if has {
			unproven = append(unproven, name)
		}
	}
	if len(unproven) > 0 {
		actual, err := d.routerDigests(ctx, unproven)
		if err != nil {
			return nil, err
		}
		for _, name := range unproven {
			if actual[name] == expected[name] {
				v := expected[name]
				reads[name] = addressProof{&v, cloneRows(proof[name])}
				reusable[name] = true
			}
		}
	}
	result := map[string][]Row{}
	for _, name := range names {
		if reusable[name] {
			result[name] = cloneRows(reads[name].Rows)
		} else {
			rows, err := d.filteredAddresses(ctx, name)
			if err != nil {
				return nil, err
			}
			result[name] = rows
			// The scope keeps its own copy: callers may modify the rows they get.
			p := addressProof{Rows: cloneRows(rows)}
			if _, ok := known[name]; ok {
				d.RememberAddresses(name, rows)
				v := DigestAddresses(rows)
				p.Digest = &v
			} else if _, ok := d.addressExpected[name]; ok {
				d.ExpectAddresses(name, rows)
				v := DigestAddresses(rows)
				p.Digest = &v
			}
			if !digestableAddresses(rows) {
				p.Digest = nil
			}
			reads[name] = p
		}
		slices.SortFunc(result[name], func(a, b Row) int { return strings.Compare(a.id(), b.id()) })
	}
	return result, nil
}

// LocateAddresses finds managed IDs even when moved outside the lists read.
func (d *RouterOS) LocateAddresses(ctx context.Context, ids []string) ([]Row, error) {
	ids = device.Unique(ids)
	if len(ids) > 20 {
		rows, err := d.list(ctx, rosAddresses)
		if err != nil {
			return nil, err
		}
		wanted := device.StringSet(ids...)
		return slices.DeleteFunc(rows, func(row Row) bool { return !wanted[row.id()] }), nil
	}
	rows := []Row{}
	for _, id := range ids {
		row, err := d.find(ctx, rosAddresses, id)
		if err != nil {
			return nil, err
		}
		if row != nil && row.id() == id {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

// OccupiedAddressNames reserves list families without downloading their members.
func (d *RouterOS) OccupiedAddressNames(ctx context.Context, patterns, exclude []string) ([]string, error) {
	exclude = slices.Clone(exclude)
	slices.Sort(exclude)
	excluded := ""
	for _, name := range exclude {
		excluded += " && list!=" + ScriptString(name)
	}
	script := ":local names ({})\n" + `:foreach i in=[/ip/firewall/address-list/find where list~` + ScriptString("^("+strings.Join(patterns, "|")+")$") + excluded + `] do={:set ($names->[/ip/firewall/address-list/get $i list]) 1}` + "\n:local out \"\"\n" + `:foreach name,v in=$names do={:set out ($out . $name . "\n")}` + "\n" + `:put ("cf-names:\n" . $out)`
	value, err := d.request(ctx, "POST", "/execute", Row{"script": script, "as-string": ""}, rosTimeout)
	if err != nil {
		return nil, err
	}
	output := executeOutput(value)
	_, body, ok := strings.Cut(output, "cf-names:")
	if !ok {
		return nil, device.Error(fmt.Sprintf(messageAddressNames, d.safeDeviceText(shortConsoleOutput(output))))
	}
	names := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r", ""), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			names = append(names, s)
		}
	}
	return device.Unique(names), nil
}
