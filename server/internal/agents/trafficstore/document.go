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
		if _, err = addNumber(member(day, key), zeroCount); err != nil {
			return day, false
		}
	}
	hosts, err := object(member(day, "hosts"))
	if err != nil {
		return day, false
	}
	for name, raw := range hosts.All() {
		host, err := object(raw)
		if err != nil {
			hosts.Delete(name)
			continue
		}
		routes, err := object(member(host, "by_route"))
		if err != nil {
			hosts.Delete(name)
			continue
		}
		for key, raw := range routes.All() {
			stats, err := object(raw)
			if err != nil {
				routes.Delete(key)
				continue
			}
			good := true
			for _, name := range countFields {
				if _, err = addNumber(member(stats, name), zeroCount); err != nil {
					good = false
					break
				}
			}
			if !good {
				routes.Delete(key)
				continue
			}
			kinds, err := object(member(stats, "fail_kinds"))
			if err != nil {
				routes.Delete(key)
				continue
			}
			for _, raw := range kinds.All() {
				if _, err = addNumber(raw, zeroCount); err != nil {
					good = false
					break
				}
			}
			if !good {
				routes.Delete(key)
			}
		}
		if err = put(&host, "by_route", routes); err != nil {
			hosts.Delete(name)
			continue
		}
		if err = put(&hosts, name, host); err != nil {
			hosts.Delete(name)
		}
	}
	if put(&day, "hosts", hosts) != nil {
		return day, false
	}
	return day, true
}

var zeroCount = new(big.Int)
