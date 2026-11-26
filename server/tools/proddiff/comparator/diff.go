package main

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// reply is one HTTP answer. Status 0 means no HTTP response (transport
// failure). Generator outputs carry no headers (HasHeaders false), and
// header comparison is skipped for them.
type reply struct {
	Status      int
	ContentType string
	Body        []byte
	Headers     map[string]string // lower-case names
	HasHeaders  bool
}

func httpReply(status int, contentType string, body []byte, headers map[string]string) reply {
	if headers == nil {
		headers = map[string]string{}
	}
	return reply{Status: status, ContentType: contentType, Body: body, Headers: headers, HasHeaders: true}
}

func bodyReply(status int, contentType string, body []byte) reply {
	return reply{Status: status, ContentType: contentType, Body: body}
}

var transportFailure = httpReply(0, "", []byte{}, nil)

// diff is one schema-only difference: a key path and a difference type.
type diff struct{ Path, Kind string }

// leftLabel names the reference side in parse/generation/transport labels.
const leftLabel = "base"

// ignoredHeaders are transport/framing headers of two HTTP servers; every other header is compared.
var ignoredHeaders = map[string]bool{"date": true, "server": true, "content-length": true, "connection": true,
	"keep-alive": true, "transfer-encoding": true, "content-type": true}

// keySet holds the public schema names that may appear in reports.
type keySet struct {
	keys    map[string]bool
	headers map[string]string
}

func (k *keySet) safeKey(key any) string {
	if s, ok := key.(string); ok && k.keys[s] {
		return s
	}
	return "<key>"
}

func (k *keySet) safeHeader(name string) string {
	if v, ok := k.headers[name]; ok {
		return v
	}
	return "<key>"
}

// comparator compares two replies of the same request.
type comparator struct {
	keys *keySet
}

// differences walks two values; mapping keys are reported only if public.
func (c *comparator) differences(a, b any, path string) []diff {
	if pyType(a) != pyType(b) {
		// Equal numbers of different kinds stay a separate class: typed consumers
		// (Go Agent structs, yaml.v3 int fields) reject 1.0 where they expect 1.
		if isNumber(a) && isNumber(b) && numericEqual(a, b) {
			return []diff{{path, "int-float"}}
		}
		return []diff{{path, "type"}}
	}
	switch x := a.(type) {
	case *Map:
		y := b.(*Map)
		var out []diff
		for _, key := range unionKeys(x, y) {
			child := path + "." + c.keys.safeKey(key)
			left, inLeft := x.Get(key)
			right, inRight := y.Get(key)
			switch {
			case !inLeft:
				out = append(out, diff{child, "extra-in-go"})
			case !inRight:
				out = append(out, diff{child, "missing-in-go"})
			default:
				out = append(out, c.differences(left, right, child)...)
			}
		}
		return out
	case []any:
		y := b.([]any)
		var out []diff
		if len(x) != len(y) {
			out = append(out, diff{path, "length"})
		}
		for i := 0; i < len(x) && i < len(y); i++ {
			out = append(out, c.differences(x[i], y[i], fmt.Sprintf("%s[%d]", path, i))...)
		}
		return out
	}
	if pyEqual(a, b) {
		return nil
	}
	return []diff{{path, "value"}}
}

var installerRoutes = map[string]bool{"/api/agents/install-script": true, "/api/agents/core-install-script": true,
	"/api/agents/docker-agent-compose": true, "/api/agents/docker-agent-run": true}

// registrationToken matches the O9-H2 bootstrap token each process mints itself.
var registrationToken = regexp.MustCompile(`(_REGISTRATION_TOKEN=|"registration_token": ")[A-Za-z0-9_-]{43}([^A-Za-z0-9_-]|$)`)

// maskRegistrationToken masks only that exact installer field (Go-vs-Go).
func maskRegistrationToken(r reply, route string) reply {
	if !installerRoutes[route] || r.Status != 200 ||
		!(strings.HasPrefix(r.ContentType, "text/plain") || strings.HasPrefix(r.ContentType, "text/yaml")) {
		return r
	}
	if !utf8Valid(r.Body) {
		return r
	}
	masked := registrationToken.ReplaceAllString(string(r.Body), "${1}<registration-token>${2}")
	r.Body = []byte(masked)
	return r
}

// responseDiffs compares two replies of one route; rules are the loaded
// normalization rules; identities/proofs may be nil.
func (c *comparator) responseDiffs(left, right reply, route string, rules []rule, identities *agentIdentities, proofs *artifactProofs) ([]diff, error) {
	left, right = maskRegistrationToken(left, route), maskRegistrationToken(right, route)
	return c.comparedDiffs(left, right, route, rules, identities, proofs)
}

// side is the body plus compared headers of one reply.
type side struct {
	body   any
	header *Map
}

func comparedHeaders(r reply) *Map {
	m := newMap()
	if !r.HasHeaders {
		return m
	}
	names := make([]string, 0, len(r.Headers))
	for name := range r.Headers {
		if !ignoredHeaders[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		m.Set(name, r.Headers[name])
	}
	return m
}

var yamlRootKey = regexp.MustCompile(`(?m)^[A-Za-z_][A-Za-z_0-9-]*: `)

func (c *comparator) comparedDiffs(left, right reply, route string, rules []rule, identities *agentIdentities, proofs *artifactProofs) ([]diff, error) {
	var out []diff
	if (left.Status == 0) != (right.Status == 0) {
		return []diff{{"status", "transport-one-sided"}}, nil
	}
	if left.Status != right.Status {
		out = append(out, diff{"status", "value"})
	}
	norm := func(s string) string { return strings.ReplaceAll(strings.ToLower(s), " ", "") }
	if norm(left.ContentType) != norm(right.ContentType) {
		out = append(out, diff{"Content-Type", "value"})
	}
	if left.Status == 0 && right.Status == 0 {
		// Both sides hung or refused: never an agreement.
		out = append(out, diff{"status", "transport-both"})
	}
	if left.Status == 0 || right.Status == 0 {
		return out, nil
	}
	var values []any
	for _, item := range []struct {
		label string
		r     reply
	}{{leftLabel, left}, {"go", right}} {
		v, err := parsed(item.r.Body, item.r.ContentType, route, "")
		if err != nil {
			out = append(out, diff{"body", "parse-" + item.label})
			continue
		}
		values = append(values, v)
	}
	if !strings.Contains(left.ContentType, "json") && !strings.Contains(right.ContentType, "json") &&
		!strings.HasPrefix(string(left.Body), "PK\x03\x04") {
		l1, l2 := lineEndings(left.Body)
		r1, r2 := lineEndings(right.Body)
		if l1 != r1 || l2 != r2 {
			out = append(out, diff{"body", "line-endings"})
		}
	}
	both := len(values) == 2
	a := side{header: comparedHeaders(left)}
	b := side{header: comparedHeaders(right)}
	if both {
		a.body, b.body = values[0], values[1]
	}
	if proofs != nil && artifactRoutes[route] && left.Status == 200 && right.Status == 200 {
		found, err := proofs.normalize(a.body, b.body)
		if err != nil {
			return nil, err
		}
		out = append(out, found...)
	}
	am, aIsMap := a.body.(*Map)
	bm, bIsMap := b.body.(*Map)
	if both && aIsMap && bIsMap {
		out = append(out, c.embeddedDocuments(am, bm, route)...)
	}
	if identities != nil && agentComparisonRoutes[route] {
		identities.normalize(a.body, b.body, route)
	}
	applyRules(rules, route, &a, &b)
	if left.HasHeaders && right.HasHeaders {
		for _, name := range unionKeys(a.header, b.header) {
			path := "header." + c.keys.safeHeader(name.(string))
			l, inLeft := a.header.Get(name)
			r, inRight := b.header.Get(name)
			switch {
			case !inLeft:
				out = append(out, diff{path, "extra-in-go"})
			case !inRight:
				out = append(out, diff{path, "missing-in-go"})
			case !pyEqual(l, r):
				out = append(out, diff{path, "value"})
			}
		}
	}
	if both {
		out = append(out, c.differences(a.body, b.body, "body")...)
	}
	return out, nil
}

// embeddedDocuments compares generated YAML embedded in JSON structurally.
func (c *comparator) embeddedDocuments(a, b *Map, route string) []diff {
	var out []diff
	for _, key := range []string{"content", "config", "compose"} {
		lv, _ := a.Get(key)
		rv, _ := b.Get(key)
		leftText, lok := lv.(string)
		rightText, rok := rv.(string)
		if !lok || !rok {
			continue
		}
		// Agent pull has three YAML services and one INI service (Surge). Only
		// this route's content gets the explicit mapping contract.
		if route == "/api/agents/<agent_id>/config" && key == "content" {
			leftYAML, lerr := loadYAML([]byte(leftText))
			rightYAML, rerr := loadYAML([]byte(rightText))
			if lerr == nil && rerr == nil {
				lm, lIsMap := leftYAML.(*Map)
				rm, rIsMap := rightYAML.(*Map)
				if lIsMap && rIsMap {
					a.Set(key, lm)
					b.Set(key, rm)
					if pyEqual(lm, rm) {
						// Digests identify each side's exact generated bytes.
						for _, field := range []struct {
							name   string
							length int
						}{{"md5", 32}, {"version", 8}} {
							av, aok := a.Get(field.name)
							bv, bok := b.Get(field.name)
							if !aok || !bok {
								continue
							}
							if pyEqual(av, md5Prefix(leftText, field.length)) && pyEqual(bv, md5Prefix(rightText, field.length)) {
								a.Set(field.name, "<verified-content-digest>")
								b.Set(field.name, "<verified-content-digest>")
							} else {
								out = append(out, diff{"body." + field.name, "invalid-content-digest"})
							}
						}
					}
					continue
				}
			}
		}
		serviceYAML := key == "compose" || yamlRootKey.MatchString(leftText)
		for _, service := range []string{"mihomo", "mosdns", "oxidns"} {
			if strings.Contains(route, "/"+service) {
				serviceYAML = true
			}
		}
		l1, l2 := lineEndings([]byte(leftText))
		r1, r2 := lineEndings([]byte(rightText))
		if l1 != r1 || l2 != r2 {
			out = append(out, diff{"body." + key, "line-endings"})
		}
		if serviceYAML {
			leftValue, lerr := loadYAML([]byte(leftText))
			rightValue, rerr := loadYAML([]byte(rightText))
			if lerr == nil && rerr == nil {
				a.Set(key, leftValue)
				b.Set(key, rightValue)
			}
		} else {
			a.Set(key, textLines(leftText))
			b.Set(key, textLines(rightText))
		}
	}
	return out
}

func md5Prefix(text string, length int) string {
	sum := md5.Sum([]byte(text))
	return hex.EncodeToString(sum[:])[:length]
}
