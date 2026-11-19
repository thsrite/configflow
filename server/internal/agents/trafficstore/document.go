package trafficstore

import (
	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"encoding/json"
	"math/big"
)

// Day is a native view of one retained discovery bucket.
type Day struct {
	IPOnlyConns json.RawMessage `json:"ip_only_conns"`
	Dropped     json.RawMessage `json:"dropped"`
	Hosts       map[string]Host `json:"hosts"`
}

// Host holds the observations for one domain and their rule decisions.
type Host struct {
	FirstSeen string                     `json:"first_seen"`
	LastSeen  string                     `json:"last_seen"`
	Ports     map[string]json.RawMessage `json:"ports"`
	ByRoute   map[string]RouteStats      `json:"by_route"`
}

// RouteStats contains aggregate counters for a rule decision.
type RouteStats struct {
	Conns     json.RawMessage            `json:"conns"`
	Fails     json.RawMessage            `json:"fails"`
	ZeroDL    json.RawMessage            `json:"zero_dl"`
	Up        json.RawMessage            `json:"up"`
	Down      json.RawMessage            `json:"down"`
	FailKinds map[string]json.RawMessage `json:"fail_kinds"`
}

// Days returns a private typed view. Damaged records have already been isolated by LoadDocument.
func (d Document) Days() map[string]Day {
	out := map[string]Day{}
	for date, raw := range d.days.All() {
		var day Day
		if jsondecode.Unmarshal(raw, &day) == nil {
			out[date] = day
		}
	}
	return out
}

// StatusJSON returns an independent copy of the persisted metadata.
func (d Document) StatusJSON() json.RawMessage { return member(d.fields, "status") }

func readableDay(raw json.RawMessage) (model.Fields, bool) {
	day, err := object(raw)
	if err != nil {
		return day, false
	}
	for _, key := range []string{"ip_only_conns", "dropped"} {
		if value, exists := day.Get(key); exists && string(value) != "null" {
			if _, err = addNumber(value, zeroCount); err != nil {
				return day, false
			}
		}
	}
	hosts, err := readRecords(member(day, "hosts"))
	if err != nil {
		return day, false
	}
	for _, name := range hosts.keys() {
		raw := recordMember(hosts, name)
		host, err := object(raw)
		if err != nil {
			hosts.delete(name)
			continue
		}
		badHost := false
		for _, key := range []string{"first_seen", "last_seen"} {
			if raw, exists := host.Get(key); exists && string(raw) != "null" {
				var value string
				if jsondecode.Unmarshal(raw, &value) != nil {
					badHost = true
				}
			}
		}
		if portsRaw, exists := host.Get("ports"); exists {
			ports, err := readRecords(portsRaw)
			if err != nil {
				badHost = true
			} else {
				for _, port := range ports.keys() {
					raw := recordMember(ports, port)
					if _, err := parseCounter(raw); err != nil {
						ports.delete(port)
					}
				}
				if put(&host, "ports", ports) != nil {
					badHost = true
				}
			}
		}
		if badHost {
			hosts.delete(name)
			continue
		}
		routes, err := readRecords(member(host, "by_route"))
		if err != nil {
			hosts.delete(name)
			continue
		}
		for _, key := range routes.keys() {
			raw := recordMember(routes, key)
			stats, err := object(raw)
			if err != nil {
				routes.delete(key)
				continue
			}
			good := true
			for _, name := range countFields {
				if value, exists := stats.Get(name); exists && string(value) != "null" {
					if _, err = addNumber(value, zeroCount); err != nil {
						good = false
						break
					}
				}
			}
			if !good {
				routes.delete(key)
				continue
			}
			kindsRaw, exists := stats.Get("fail_kinds")
			if !exists || string(kindsRaw) == "null" {
				continue
			}
			kinds, err := object(kindsRaw)
			if err != nil {
				routes.delete(key)
				continue
			}
			for _, raw := range kinds.All() {
				if _, err = addNumber(raw, zeroCount); err != nil {
					good = false
					break
				}
			}
			if !good {
				routes.delete(key)
			}
		}
		if err = put(&host, "by_route", routes); err != nil {
			hosts.delete(name)
			continue
		}
		if err = recordPut(&hosts, name, host); err != nil {
			hosts.delete(name)
		}
	}
	if put(&day, "hosts", hosts) != nil {
		return day, false
	}
	return day, true
}

var zeroCount = new(big.Int)
