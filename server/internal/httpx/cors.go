package httpx

import (
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// corsMethods is Flask-CORS's ALL_METHODS as it serializes them (sorted,
// joined with ", ").
const corsMethods = "DELETE, GET, HEAD, OPTIONS, PATCH, POST, PUT"

// edgeOriginKey carries only the Edge's pre-proxy origin, never forwarded
// headers supplied by a client. Edge otherwise rewrites Host and TLS like nginx.
type edgeOriginKey struct{}

func requestOrigin(req *http.Request) string {
	if origin, ok := req.Context().Value(edgeOriginKey{}).(string); ok {
		return origin
	}
	scheme := "http"
	if req.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + req.Host
}

func canonicalOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Opaque != "" || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") {
		return ""
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	host := strings.ToLower(u.Host)
	if u.Scheme == "http" {
		host = strings.TrimSuffix(host, ":80")
	} else {
		host = strings.TrimSuffix(host, ":443")
	}
	return u.Scheme + "://" + host
}

// cors permits only the request's own scheme, hostname and port (O9-H1).
// Requests without an Origin or with a foreign/invalid Origin get no CORS headers.
func cors(c *Ctx, r *Response) {
	h := r.Header()
	origin, _ := c.Header("Origin")
	own := canonicalOrigin(requestOrigin(c.Request))
	if own == "" || canonicalOrigin(origin) != own {
		for name := range h {
			if strings.HasPrefix(strings.ToLower(name), "access-control-") {
				h.Del(name)
			}
		}
		return
	}
	h.Set("Access-Control-Allow-Origin", origin)
	if c.Request.Method == "OPTIONS" {
		requested, _ := c.Header("Access-Control-Request-Method")
		requested = strings.ToUpper(requested)
		// Flask-CORS tests membership in the serialized string.
		if requested != "" && strings.Contains(corsMethods, requested) {
			if requestHeaders, _ := c.Header("Access-Control-Request-Headers"); requestHeaders != "" {
				var names []string
				for _, name := range strings.Split(requestHeaders, ",") {
					names = append(names, strings.TrimSpace(name))
				}
				sort.Strings(names)
				if joined := strings.Join(names, ", "); joined != "" {
					h.Set("Access-Control-Allow-Headers", joined)
				}
			}
			h.Set("Access-Control-Allow-Methods", corsMethods)
		}
	}
	h.Add("Vary", "Origin")
}
