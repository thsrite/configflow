package httpx

import (
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"strconv"
	"strings"

	"configflow/internal/repo"
)

func (e *HTTPError) response(path string) *Response {
	if e.Code < 400 || !(path == "/api" || strings.HasPrefix(path, "/api/")) {
		return e.Page()
	}
	data, _ := json.Marshal(struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}{false, e.Description})
	r := RawJSON(e.Code, data)
	if len(e.Allow) > 0 {
		r.Header().Set("Allow", strings.Join(e.Allow, ", "))
	}
	return r
}

// HTTPError is a werkzeug.exceptions.HTTPException: returned from a handler
// (or from Ctx.JSON) it becomes Werkzeug's HTML error page. Error() is
// Python's str(exc), "415 Unsupported Media Type: <description>", which some
// handlers put into their own messages.
type HTTPError struct {
	Code        int
	Description string
	// Allow is sent as the Allow header (405).
	Allow []string
	// Location is sent as the Location header (redirects).
	Location string
}

func (e *HTTPError) Error() string {
	return statusName(e.Code) + ": " + e.Description
}

func statusName(code int) string {
	return itoa(code) + " " + reasonPhrase(code)
}

func reasonPhrase(code int) string {
	if code == http.StatusRequestEntityTooLarge {
		return "Request Entity Too Large"
	}
	return http.StatusText(code)
}

func itoa(n int) string { return strconv.Itoa(n) }

// Werkzeug's default descriptions.
const (
	DescBadRequest       = "The browser (or proxy) sent a request that this server could not understand."
	DescNotFound         = "The requested URL was not found on the server. If you entered the URL manually please check your spelling and try again."
	DescMethodNotAllowed = "The method is not allowed for the requested URL."
	DescTooLarge         = "The data value transmitted exceeds the capacity limit."
	DescUnsupportedMedia = "The server does not support the media type transmitted in the request."
	DescInternal         = "The server encountered an internal error and was unable to complete your request. Either the server is overloaded or there is an error in the application."
)

// Errors for handlers.
var (
	ErrNotFound = &HTTPError{Code: 404, Description: DescNotFound}
)

// BadRequest is werkzeug's BadRequest with an optional description.
func BadRequest(description string) *HTTPError {
	if description == "" {
		description = DescBadRequest
	}
	return &HTTPError{Code: 400, Description: description}
}

// NotFound is werkzeug's NotFound (abort(404)).
func NotFound() *HTTPError { return &HTTPError{Code: 404, Description: DescNotFound} }

// Page renders the error as Werkzeug's HTML response.
func (e *HTTPError) Page() *Response {
	name := html.EscapeString(reasonPhrase(e.Code))
	description := strings.ReplaceAll(escape(e.Description), "\n", "<br>")
	var body string
	if e.Code >= 300 && e.Code < 400 && e.Location != "" {
		body = redirectBody(e.Location)
	} else {
		body = "<!doctype html>\n<html lang=en>\n<title>" + itoa(e.Code) + " " + name + "</title>\n<h1>" +
			name + "</h1>\n<p>" + description + "</p>\n"
	}
	r := Bytes(e.Code, "text/html; charset=utf-8", []byte(body))
	if len(e.Allow) > 0 {
		r.Header().Set("Allow", strings.Join(e.Allow, ", "))
	}
	if e.Location != "" {
		r.Header().Set("Location", e.Location)
	}
	return r
}

func redirectBody(location string) string {
	safe := escape(location)
	return "<!doctype html>\n<html lang=en>\n<title>Redirecting...</title>\n<h1>Redirecting...</h1>\n" +
		"<p>You should be redirected automatically to the target URL: <a href=\"" + safe + "\">" + safe +
		"</a>. If not, click the link.\n"
}

// escape is markupsafe.escape.
func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&#34;", "'", "&#39;").Replace(s)
}

// ErrorHandler turns an error returned by a handler (or a before-request
// hook) into a response; it returns nil for errors it does not handle.
type ErrorHandler func(c *Ctx, err error) *Response

// RepositoryErrorHandler is the set of @app.errorhandler functions of
// backend/routes/__init__.py for ProfileRepositoryError and its subclasses.
func RepositoryErrorHandler(_ *Ctx, err error) *Response {
	var e *repo.Error
	if !errors.As(err, &e) {
		return nil
	}
	switch {
	case errors.Is(err, repo.ErrValidation):
		return Message(400, false, e.Error())
	case errors.Is(err, repo.ErrNotFound):
		return Message(404, false, "Profile not found: "+e.Message)
	case errors.Is(err, repo.ErrExists), errors.Is(err, repo.ErrInUse):
		if errors.Is(err, repo.ErrInUse) {
			usages := e.Usages
			if usages == nil {
				usages = []any{}
			}
			return JSONStatus(409, struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
				Usages  []any  `json:"usages"`
			}{false, e.Error(), usages})
		}
		return Message(409, false, e.Error())
	}
	return Message(400, false, e.Error())
}
