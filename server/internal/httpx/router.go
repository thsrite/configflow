package httpx

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// HandlerFunc is a view function. Return a Response, or an error for the
// registered error handlers (repository errors become their JSON bodies,
// *HTTPError its Werkzeug page, anything else a 500 page). A nil Response
// with a nil error is Flask's "view did not return a valid response": 500.
type HandlerFunc func(c *Ctx) (*Response, error)

// Route is one Werkzeug rule: a path pattern, its methods and the Flask
// endpoint name ("blueprint.function").
type Route struct {
	// Methods as declared; HEAD is added for GET and OPTIONS for every
	// route, as Flask does.
	Methods  []string
	Path     string
	Endpoint string
	Handler  HandlerFunc
	// PreservePlainErrors keeps explicit text/plain error bodies returned by
	// shell-facing downloads. JSON and unexpected errors retain API policy.
	PreservePlainErrors bool
	parts               []part
	allowed             []string
}

// Blueprint is request.blueprint: the endpoint before its last dot.
func (r *Route) Blueprint() string {
	if i := strings.LastIndexByte(r.Endpoint, '.'); i >= 0 {
		return r.Endpoint[:i]
	}
	return ""
}

type partKind int

const (
	partStatic partKind = iota
	partString          // <name> or <string:name>: one non-empty segment
	partPath            // <path:name>: one or more segments, the first non-empty
)

type part struct {
	kind  partKind
	value string // static text or parameter name
}

// Router holds the URL map. Route packages add their rules in their
// Register(*httpx.Router) function.
type Router struct {
	routes     []*Route
	byEndpoint map[string][]*Route
}

// NewRouter returns an empty URL map.
func NewRouter() *Router {
	return &Router{byEndpoint: map[string][]*Route{}}
}

// Handle adds a rule. path uses Flask syntax: static segments, <name>
// (Werkzeug's default string converter: one segment, not empty) and
// <path:name> (the rest of the path, slashes included). endpoint is the
// Flask endpoint name, "blueprint.function" (e.g. "agents.agent_heartbeat"):
// the authentication exemptions, the profile selection and the response
// redaction whitelists are keyed by it, so use exactly the Python name. One
// endpoint may have several rules (Python: stacked @route decorators); it
// panics on malformed patterns or an endpoint reused for another handler.
func (r *Router) Handle(methods []string, path, endpoint string, handler HandlerFunc) *Route {
	if endpoint == "" || handler == nil {
		panic("httpx: route needs an endpoint and a handler: " + path)
	}
	parts, err := parsePattern(path)
	if err != nil {
		panic(err)
	}
	route := &Route{Methods: upper(methods), Path: path, Endpoint: endpoint, Handler: handler, parts: parts}
	route.allowed = allowedMethods(route.Methods)
	r.routes = append(r.routes, route)
	r.byEndpoint[endpoint] = append(r.byEndpoint[endpoint], route)
	return route
}

// GET, POST, PUT, PATCH and DELETE are Handle with one method.
func (r *Router) GET(path, endpoint string, h HandlerFunc) *Route {
	return r.Handle([]string{"GET"}, path, endpoint, h)
}
func (r *Router) POST(path, endpoint string, h HandlerFunc) *Route {
	return r.Handle([]string{"POST"}, path, endpoint, h)
}
func (r *Router) PUT(path, endpoint string, h HandlerFunc) *Route {
	return r.Handle([]string{"PUT"}, path, endpoint, h)
}
func (r *Router) PATCH(path, endpoint string, h HandlerFunc) *Route {
	return r.Handle([]string{"PATCH"}, path, endpoint, h)
}
func (r *Router) DELETE(path, endpoint string, h HandlerFunc) *Route {
	return r.Handle([]string{"DELETE"}, path, endpoint, h)
}

// Routes lists the rules in registration order.
func (r *Router) Routes() []*Route { return slices.Clone(r.routes) }

// HasEndpoint reports whether a rule with that endpoint exists.
func (r *Router) HasEndpoint(endpoint string) bool { return len(r.byEndpoint[endpoint]) > 0 }

func upper(methods []string) []string {
	out := make([]string, 0, len(methods))
	for _, m := range methods {
		out = append(out, strings.ToUpper(m))
	}
	return out
}

func allowedMethods(methods []string) []string {
	set := map[string]bool{"OPTIONS": true}
	for _, m := range methods {
		set[m] = true
		if m == "GET" {
			set["HEAD"] = true
		}
	}
	out := make([]string, 0, len(set))
	for m := range set {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

func parsePattern(path string) ([]part, error) {
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("httpx: pattern must start with /: %q", path)
	}
	var parts []part
	for _, segment := range strings.Split(path[1:], "/") {
		if strings.HasPrefix(segment, "<") && strings.HasSuffix(segment, ">") {
			inner := segment[1 : len(segment)-1]
			kind := partString
			if converter, name, ok := strings.Cut(inner, ":"); ok {
				switch converter {
				case "string":
				case "path":
					kind = partPath
				default:
					return nil, fmt.Errorf("httpx: unsupported converter %q in %q", converter, path)
				}
				inner = name
			}
			if inner == "" {
				return nil, fmt.Errorf("httpx: empty parameter name in %q", path)
			}
			parts = append(parts, part{kind: kind, value: inner})
			continue
		}
		if strings.ContainsAny(segment, "<>") {
			return nil, fmt.Errorf("httpx: parameters must fill a whole segment: %q", path)
		}
		parts = append(parts, part{kind: partStatic, value: segment})
	}
	return parts, nil
}

// match tries the rule against the request segments. It returns the
// parameters on success.
func (rt *Route) match(segments []string) (map[string]string, bool) {
	params := map[string]string{}
	if matchParts(rt.parts, segments, params) {
		return params, true
	}
	return nil, false
}

func matchParts(parts []part, segments []string, params map[string]string) bool {
	if len(parts) == 0 {
		return len(segments) == 0
	}
	if len(segments) == 0 {
		return false
	}
	p := parts[0]
	switch p.kind {
	case partStatic:
		return segments[0] == p.value && matchParts(parts[1:], segments[1:], params)
	case partString:
		if segments[0] == "" {
			return false
		}
		params[p.value] = segments[0]
		if matchParts(parts[1:], segments[1:], params) {
			return true
		}
		delete(params, p.value)
		return false
	default: // partPath: shortest first, Werkzeug's non-greedy "[^/].*?"
		if segments[0] == "" {
			return false
		}
		for n := 1; n <= len(segments); n++ {
			params[p.value] = strings.Join(segments[:n], "/")
			if matchParts(parts[1:], segments[n:], params) {
				return true
			}
		}
		delete(params, p.value)
		return false
	}
}

// rank orders candidate rules like Werkzeug's state machine matcher:
// segment by segment, a static segment before a converter and a one-segment
// converter before a path converter; ties keep registration order.
func rank(a, b *Route) int {
	for i := 0; i < len(a.parts) && i < len(b.parts); i++ {
		if a.parts[i].kind != b.parts[i].kind {
			return int(a.parts[i].kind) - int(b.parts[i].kind)
		}
	}
	return 0
}

// matchResult is the outcome of URL matching.
type matchResult struct {
	route    *Route
	params   map[string]string
	allowed  []string // for 405: the methods of every rule matching the path
	redirect string   // for 308: the corrected path
}

// Match finds the rule for a decoded request path. It returns the route,
// or nil with either the allowed methods (405) or a redirect path (308,
// Werkzeug's strict-slashes and merge-slashes redirects), or neither (404).
func (r *Router) Match(method, path string) matchResult {
	if result := r.match(method, path); result.route != nil || len(result.allowed) > 0 {
		return result
	}
	// Werkzeug redirects to the canonical URL when only it matches.
	if strings.Contains(path, "//") {
		merged := path
		for strings.Contains(merged, "//") {
			merged = strings.ReplaceAll(merged, "//", "/")
		}
		if result := r.match(method, merged); result.route != nil || len(result.allowed) > 0 {
			return matchResult{redirect: merged}
		}
	}
	if !strings.HasSuffix(path, "/") {
		if result := r.match(method, path+"/"); result.route != nil && strings.HasSuffix(result.route.Path, "/") {
			return matchResult{redirect: path + "/"}
		}
	}
	return matchResult{}
}

func (r *Router) match(method, path string) matchResult {
	if !strings.HasPrefix(path, "/") {
		return matchResult{}
	}
	segments := strings.Split(path[1:], "/")
	type candidate struct {
		route  *Route
		params map[string]string
	}
	var candidates []candidate
	for _, route := range r.routes {
		if params, ok := route.match(segments); ok {
			candidates = append(candidates, candidate{route, params})
		}
	}
	if len(candidates) == 0 {
		return matchResult{}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return rank(candidates[i].route, candidates[j].route) < 0 })
	allowed := map[string]bool{}
	for _, c := range candidates {
		if slices.Contains(c.route.allowed, method) {
			return matchResult{route: c.route, params: c.params}
		}
		for _, m := range c.route.allowed {
			allowed[m] = true
		}
	}
	methods := make([]string, 0, len(allowed))
	for m := range allowed {
		methods = append(methods, m)
	}
	sort.Strings(methods)
	return matchResult{allowed: methods}
}

// AllowedMethods returns every method the rules matching path accept
// (Flask's automatic OPTIONS response lists them in Allow).
func (r *Router) AllowedMethods(path string) []string {
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	set := map[string]bool{}
	for _, route := range r.routes {
		if _, ok := route.match(segments); ok {
			for _, m := range route.allowed {
				set[m] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for m := range set {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}
