// Package clients owns saved client names/policies and live LAN discovery.
// Router configuration writes and fixed-IP device operations belong to routers.
package clients

import (
	"bytes"
	"configflow/internal/jsondecode"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"configflow/internal/clientrange"
	"configflow/internal/devicenotes"
	"configflow/internal/lifecycle"
	"configflow/internal/oxidns"
)

// ErrDeviceChanged means the submitted row no longer matches the stored draft.
var ErrDeviceChanged = errors.New(messageChanged)

// Device is one saved client identity and optional opaque reservation record.
// R17 owns the reservation schema; this layer preserves all of its fields.
type Device struct {
	IP      string          `json:"ip"`
	Name    string          `json:"name"`
	Mode    string          `json:"mode"`
	FixedIP json.RawMessage `json:"fixed_ip,omitempty"`
}

// DeviceList joins names and policies without changing either stored field.
type DeviceList struct {
	Devices     []Device           `json:"devices"`
	Editable    bool               `json:"editable"`
	Reason      string             `json:"reason"`
	SideEntries []oxidns.SideEntry `json:"side_entries"`
}

// Change describes a validated optimistic update to one saved client row.
type Change struct {
	IP           string `json:"ip"`
	Name         string `json:"name"`
	Mode         string `json:"mode"`
	PreviousName string `json:"previous_name"`
	PreviousMode string `json:"previous_mode"`
}

func settingsError(message string) error { return &oxidns.SettingsError{Msg: message} }

// NormalizeChange validates and canonicalizes the editable fields. HTTP shape
// validation (including missing/null/non-string fields) belongs to R17.
func NormalizeChange(c Change) (Change, error) {
	ip, ok := clientrange.Normalize(c.IP)
	if !ok {
		return Change{}, settingsError(devicenotes.AddressMessage)
	}
	c.IP = ip
	var err error
	c.Name, err = devicenotes.NormalizeName(c.Name)
	if err != nil {
		return Change{}, settingsError(err.Error())
	}
	c.PreviousName, err = devicenotes.NormalizeName(c.PreviousName)
	if err != nil {
		return Change{}, settingsError(err.Error())
	}
	valid := func(mode string) bool { return mode == "" || mode == "smart" || mode == "direct" }
	if !valid(c.Mode) || !valid(c.PreviousMode) {
		return Change{}, settingsError(messageMode)
	}
	return c, nil
}

// ListDevices reads a complete profile document. It is exported for Agent
// readiness and R17; callers retain their own profile/repository ownership.
func ListDevices(config json.RawMessage) (DeviceList, error) {
	p, err := oxidns.ProfileFromJSON(config)
	if err != nil {
		return DeviceList{}, err
	}
	return ListProfileDevices(config, p)
}

// ListProfileDevices lists the devices of an already decoded profile. The raw
// config supplies the ordered notes and fixed IP bindings of that same profile.
func ListProfileDevices(config json.RawMessage, p *oxidns.Profile) (DeviceList, error) {
	view, err := oxidns.ReadPolicies(p.Settings(), p)
	if err != nil {
		return DeviceList{}, err
	}
	devices := []Device{}
	if err = jsondecode.Unmarshal(oxidns.ToJSON(view.Policies), &devices); err != nil {
		return DeviceList{}, err
	}
	names, err := oxidns.ReadNotes(p)
	if err != nil {
		return DeviceList{}, err
	}
	var doc struct {
		Notes json.RawMessage `json:"device_notes"`
		Fixed json.RawMessage `json:"client_fixed_ips"`
	}
	if err = jsondecode.Unmarshal(config, &doc); err != nil {
		return DeviceList{}, err
	}
	listed := map[string]bool{}
	for _, d := range devices {
		listed[d.IP] = true
	}
	// Explicit notes keep their stored insertion order; legacy policy-only names
	// already occur in devices. No map iteration controls the public row order.
	for _, key := range objectKeys(doc.Notes) {
		ip, ok := clientrange.Normalize(key)
		if !ok {
			continue
		}
		if name := names[ip]; name != "" && !listed[ip] {
			devices = append(devices, Device{IP: ip, Name: name})
			listed[ip] = true
		}
	}
	var bindings map[string]json.RawMessage
	_ = json.Unmarshal(doc.Fixed, &bindings)
	for i := range devices {
		devices[i].FixedIP = bytes.Clone(bindings[devices[i].IP])
	}
	return DeviceList{devices, view.Editable, view.Reason, view.SideEntries}, nil
}
func objectKeys(raw json.RawMessage) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil
	}
	keys := []string{}
	for dec.More() {
		tok, err = dec.Token()
		if err != nil {
			return nil
		}
		key, ok := tok.(string)
		if !ok {
			return nil
		}
		keys = append(keys, key)
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			return nil
		}
	}
	return keys
}

// UpdateDevice returns a replacement complete profile. Call it inside the
// repository transaction callback so optimistic checks and the commit are atomic.
// It detaches browser cancellation, retains Runtime shutdown, and never mutates
// the input, including on validation/conflict failures.
func UpdateDevice(ctx context.Context, config json.RawMessage, c Change, baseURL string) (json.RawMessage, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var err error
	c, err = NormalizeChange(c)
	if err != nil {
		return nil, err
	}
	list, err := ListDevices(config)
	if err != nil {
		return nil, err
	}
	current := Device{}
	for _, d := range list.Devices {
		if d.IP == c.IP {
			current = d
			break
		}
	}
	if current.Name != c.PreviousName || current.Mode != c.PreviousMode {
		return nil, ErrDeviceChanged
	}
	var doc map[string]json.RawMessage
	if err = json.Unmarshal(config, &doc); err != nil {
		return nil, err
	}
	p, err := oxidns.ProfileFromJSON(config)
	if err != nil {
		return nil, err
	}
	if current.Mode != c.Mode {
		names, err := oxidns.ReadNotes(p)
		if err != nil {
			return nil, err
		}
		names[c.IP] = c.Name
		var settings struct {
			Policies json.RawMessage `json:"device_policies"`
		}
		_ = jsondecode.Unmarshal(doc["oxidns"], &settings)
		if len(settings.Policies) == 0 {
			settings.Policies = json.RawMessage(`[]`)
		}
		rows, err := devicenotes.ParsePolicies(settings.Policies)
		if err != nil {
			return nil, settingsError(err.Error())
		}
		updated := []devicenotes.Policy{}
		found := false
		for _, row := range rows {
			row.Name = names[row.IP]
			if row.IP == c.IP {
				found = true
				if c.Mode == "" {
					continue
				}
				row.Mode = c.Mode
			}
			updated = append(updated, row)
		}
		if !found && c.Mode != "" {
			updated = append(updated, devicenotes.Policy{IP: c.IP, Name: c.Name, Mode: c.Mode})
		}
		settingsNode, err := oxidns.ApplyPolicies(p.Settings(), p, updated, baseURL)
		if err != nil {
			return nil, err
		}
		doc["oxidns"] = oxidns.ToJSON(settingsNode)
	}
	// Name hiding must consult the replacement policy rows, not the old ones.
	var policy struct {
		Policies json.RawMessage `json:"device_policies"`
	}
	_ = jsondecode.Unmarshal(doc["oxidns"], &policy)
	if len(policy.Policies) == 0 {
		policy.Policies = json.RawMessage(`[]`)
	}
	notes := doc["device_notes"]
	if len(notes) == 0 {
		notes = json.RawMessage(`{}`)
	}
	updatedNotes, err := devicenotes.Set(notes, policy.Policies, c.IP, c.Name)
	if err != nil {
		return nil, settingsError(err.Error())
	}
	doc["device_notes"], err = marshalNotes(notes, updatedNotes)
	if err != nil {
		return nil, err
	}
	if c.Name == "" && c.Mode == "" {
		if raw, ok := doc["client_fixed_ips"]; ok {
			var bindings map[string]json.RawMessage
			if json.Unmarshal(raw, &bindings) == nil && bindings != nil {
				delete(bindings, c.IP)
				doc["client_fixed_ips"], err = json.Marshal(bindings)
				if err != nil {
					return nil, err
				}
			}
		}
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encode client device update: %w", err)
	}
	return out, nil
}

// Keep the displayed insertion order of saved rows across a rename.
func marshalNotes(previous json.RawMessage, notes devicenotes.Names) (json.RawMessage, error) {
	keys := []string{}
	seen := map[string]bool{}
	for _, key := range objectKeys(previous) {
		ip, ok := clientrange.Normalize(key)
		if !ok || seen[ip] {
			continue
		}
		if _, ok := notes[ip]; ok {
			keys = append(keys, ip)
			seen[ip] = true
		}
	}
	extra := []string{}
	for key := range notes {
		if !seen[key] {
			extra = append(extra, key)
		}
	}
	slices.Sort(extra)
	keys = append(keys, extra...)
	var b bytes.Buffer
	b.WriteByte('{')
	for i, key := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		v, err := json.Marshal(notes[key])
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}
