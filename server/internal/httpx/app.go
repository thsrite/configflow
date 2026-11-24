package httpx

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"unicode/utf8"

	"configflow/internal/lifecycle"
	"configflow/internal/localhttp"
	"configflow/internal/logx"
	"configflow/internal/repo"
)

// BeforeFunc is a before_request function: a non-nil Response or error stops
// the request (the after-request functions still run).
type BeforeFunc func(c *Ctx) (*Response, error)

// AfterFunc is an after_request function; it may modify the response.
type AfterFunc func(c *Ctx, r *Response)

// App is the Flask application: the URL map plus the request pipeline of
// backend/app.py, with authentication moved before profile resolution (O9-L6):
//
//	before: (auth gate, added by auth.Install) → profile context → extra BeforeRequest functions
//	view (or the routing error, or Flask's automatic OPTIONS response)
//	errors: registered ErrorHandlers (repository errors first), HTTPError pages, else 500
//	after:  extra AfterRequest functions → config pull log → X-ConfigFlow-Profile
//	        and reset_config_context → global JSON redaction → CORS
//
// App implements http.Handler (the API as the Flask backend served it, for
// every path); see Edge for the web front end that also serves the SPA.
type App struct {
	Router  *Router
	Repo    *repo.Repository
	Runtime *lifecycle.Runtime

	before        []BeforeFunc
	after         []AfterFunc
	errorHandlers []ErrorHandler

	// Logger receives "Exception on <path> [<method>]" for unhandled
	// errors (Flask's app.logger, named after backend/app.py).
	Logger *logx.Logger
}

// New returns an App with the built-in hooks installed. Register route
// packages on app.Router, then call auth.Install(app) to add the global
// authentication gate.
func New() *App {
	app := &App{Router: NewRouter(), Logger: logx.Get("backend.app")}
	app.before = []BeforeFunc{profileContext, requestSeen}
	app.errorHandlers = []ErrorHandler{RepositoryErrorHandler}
	return app
}

// BeforeRequest appends a before_request function (runs after the
// authentication gate and the profile context).
func (a *App) BeforeRequest(f BeforeFunc) { a.before = append(a.before, f) }

// InsertBeforeRequest adds a before_request function at position i (auth
// uses 0: before the profile context).
func (a *App) InsertBeforeRequest(i int, f BeforeFunc) {
	a.before = append(a.before[:i], append([]BeforeFunc{f}, a.before[i:]...)...)
}

// AfterRequest adds an after_request function. Functions added later run
// earlier, as in Flask; all of them run before the built-in hooks.
func (a *App) AfterRequest(f AfterFunc) { a.after = append(a.after, f) }

// HandleError adds an error handler, tried before the built-in ones.
func (a *App) HandleError(h ErrorHandler) {
	a.errorHandlers = append([]ErrorHandler{h}, a.errorHandlers...)
}

// RegisterLocal makes the app the target of localhttp.Do (in-process
// callbacks to this server's own /api URLs).
func (a *App) RegisterLocal() { localhttp.Register(a, lifecycle.DefaultSpawner{}) }

// ServeHTTP runs one request through the pipeline. The request is bound to the
// live lifecycle generation, so work an owner detaches from it (PORTING 4b)
// still stops at the shutdown cancellation phase.
func (a *App) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := repo.WithScope(lifecycle.BindRequest(req.Context()))
	defer repo.ResetConfigContext(ctx)
	c := a.newCtx(ctx, req)
	r := a.process(c)
	writeResponse(w, req, r)
}

func (a *App) newCtx(ctx context.Context, req *http.Request) *Ctx {
	path := req.URL.Path
	if path == "" {
		path = "/"
	}
	if !utf8.ValidString(path) {
		path = strings.ToValidUTF8(path, "�")
	}
	return &Ctx{Request: req, app: a, ctx: ctx, path: path}
}

// process is Flask's full_dispatch_request + finalize_request.
func (a *App) process(c *Ctx) (response *Response) {
	match := a.Router.Match(c.Request.Method, c.path)
	if match.route != nil {
		c.route, c.params = match.route, match.params
		c.Endpoint, c.Blueprint = match.route.Endpoint, match.route.Blueprint()
	}
	if c.params == nil {
		c.params = map[string]string{}
	}
	response = a.dispatch(c, match)
	// Bind/JSONSilent may consume a limited body and return a fallback response.
	// The transport limit must still win, including for chunked uploads (O9-M1).
	var oversized *http.MaxBytesError
	if errors.As(c.bodyErr, &oversized) || bodyLimitExceeded(c.Request) {
		response = (&HTTPError{Code: http.StatusRequestEntityTooLarge}).response(c.path)
	}
	a.finalize(c, response)
	return response
}

func (a *App) dispatch(c *Ctx, match matchResult) (response *Response) {
	defer func() {
		if p := recover(); p != nil {
			response = a.internalError(c, fmt.Errorf("panic: %v\n%s", p, debug.Stack()))
		}
	}()
	for _, before := range a.before {
		r, err := before(c)
		if err != nil {
			return a.handleError(c, err)
		}
		if r != nil {
			return r
		}
	}
	switch {
	case match.redirect != "":
		location := c.HostURL() + strings.TrimPrefix(match.redirect, "/")
		if c.Request.URL.RawQuery != "" {
			location += "?" + c.Request.URL.RawQuery
		}
		return (&HTTPError{Code: 308, Location: location}).Page()
	case match.route == nil && len(match.allowed) > 0:
		return a.handleError(c, &HTTPError{Code: 405, Description: DescMethodNotAllowed, Allow: match.allowed})
	case match.route == nil:
		return a.handleError(c, NotFound())
	case c.Request.Method == "OPTIONS" && !containsMethod(match.route.Methods, "OPTIONS"):
		// make_default_options_response
		r := Empty(200)
		r.Header().Set("Allow", strings.Join(a.Router.AllowedMethods(c.path), ", "))
		return r
	}
	r, err := match.route.Handler(c)
	if err != nil {
		return a.handleError(c, err)
	}
	if r == nil {
		return a.internalError(c, errors.New("The view function did not return a valid response."))
	}
	return r
}

func containsMethod(methods []string, m string) bool {
	for _, x := range methods {
		if x == m {
			return true
		}
	}
	return false
}

func (a *App) handleError(c *Ctx, err error) *Response {
	var oversized *http.MaxBytesError
	if errors.As(err, &oversized) {
		return (&HTTPError{Code: http.StatusRequestEntityTooLarge}).response(c.path)
	}
	for _, h := range a.errorHandlers {
		if r := h(c, err); r != nil {
			return r
		}
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.response(c.path)
	}
	return a.internalError(c, err)
}

func (a *App) internalError(c *Ctx, err error) *Response {
	// net/http cancels the request when the client disconnects. That matching
	// cancellation is an ended wait, not a failed application operation. Keep
	// logging internal cancellations and unrelated failures on a live request.
	if cause := c.Request.Context().Err(); cause == nil || !errors.Is(err, cause) {
		a.Logger.Error("Exception on " + c.path + " [" + c.Request.Method + "]\n" + err.Error())
	}
	return (&HTTPError{Code: 500, Description: DescInternal}).response(c.path)
}

// finalize runs the after-request functions in Flask's order.
func (a *App) finalize(c *Ctx, r *Response) {
	defer func() {
		if p := recover(); p != nil {
			*r = *a.internalError(c, fmt.Errorf("panic in after_request: %v\n%s", p, debug.Stack()))
			cors(c, r)
		}
	}()
	for i := len(a.after) - 1; i >= 0; i-- {
		a.after[i](c, r)
	}
	logConfigPull(c, r)
	if c.ProfileID != "" && r.Header().Get("X-ConfigFlow-Profile") == "" {
		r.Header().Set("X-ConfigFlow-Profile", c.ProfileID)
	}
	repo.ResetConfigContext(c.ctx)
	preservePlain := c.route != nil && c.route.PreservePlainErrors && strings.HasPrefix(r.Header().Get("Content-Type"), "text/plain")
	if r.Status >= 400 && preservePlain {
		r.Header().Del("Content-Disposition")
	}
	if r.Status >= 400 && (c.path == "/api" || strings.HasPrefix(c.path, "/api/")) && !strings.HasPrefix(r.Header().Get("Content-Type"), "application/json") && !preservePlain {
		// API failures always have a JSON envelope, including explicit HTML/text
		// failures returned by a handler. Do not echo arbitrary diagnostic bodies.
		description := http.StatusText(r.Status)
		if strings.HasPrefix(r.Header().Get("Content-Type"), "text/plain") {
			description = strings.TrimSuffix(string(r.Body()), "\n")
		}
		failure := (&HTTPError{Code: r.Status, Description: description}).response(c.path)
		for key, values := range r.Header() {
			if key != "Content-Type" && key != "Content-Length" {
				failure.Header()[key] = values
			}
		}
		*r = *failure
	}
	if err := writeJSON(c, r); err != nil {
		*r = *a.internalError(c, err)
	}
	if strings.HasPrefix(r.Header().Get("Content-Type"), "text/") {
		r.Header().Set("X-Content-Type-Options", "nosniff")
	}
	cors(c, r)
}

// requestSeen runs after profile resolution and the auth gate installed at
// index 0. Public heartbeats count; rejected profiles or credentials do not.
func requestSeen(_ *Ctx) (*Response, error) {
	lifecycle.RequestSeen()
	return nil, nil
}

// profileContext is install_profile_context's before_request: bind the
// request to its profile (route argument, ?profile=, ?profile_id=,
// X-ConfigFlow-Profile, else the default profile) unless it is global; an
// invalid or unknown profile ends the request with the repository error.
func profileContext(c *Ctx) (*Response, error) {
	repo.ResetConfigContext(c.ctx)
	pathProfile, hasPath := c.params["profile_id"]
	info := repo.RequestInfo{Endpoint: c.Endpoint}
	info.Global = repo.IsGlobalRequest(repo.RequestRoute{
		HasPathProfileID: hasPath, Endpoint: c.Endpoint, Blueprint: c.Blueprint, Path: c.path,
	})
	if hasPath {
		info.PathProfileID = &pathProfile
	}
	if v, ok := c.Arg("profile"); ok {
		info.QueryProfile = &v
	}
	if v, ok := c.Arg("profile_id"); ok {
		info.QueryProfileID = &v
	}
	if v, ok := c.Header("X-ConfigFlow-Profile"); ok {
		info.HeaderProfile = &v
	}
	repo.SetRequest(c.ctx, info)
	if info.Global {
		c.ProfileID = ""
		return nil, nil
	}
	id, err := repo.ResolveProfile(c.ctx, nil, nil)
	if err != nil {
		return nil, err
	}
	c.ProfileID = id
	return nil, nil
}

var configPullPath = regexp.MustCompile(`^/api/config/(?:([A-Za-z0-9][A-Za-z0-9_-]{0,63})/)?(mihomo|surge|loon|mosdns|oxidns)(?:/[^/]+)?$`)

var configLogger = logx.Get("backend.routes.config")

// logConfigPull is routes/config.register_config_pull_log: client
// subscriptions are the only evidence a device fetched its configuration.
func logConfigPull(c *Ctx, r *Response) {
	if c.Request.Method != "GET" || c.IsInternalCall() {
		return
	}
	m := configPullPath.FindStringSubmatch(c.path)
	if m == nil {
		return
	}
	level := logx.Info
	if r.Status >= 400 {
		level = logx.Warning
	}
	profile := m[1]
	if profile == "" {
		profile = "默认"
	}
	agent := c.HeaderDefault("User-Agent", "")
	if agent == "" {
		agent = "-"
	}
	configLogger.Log(level, "配置被拉取："+m[2]+"（配置空间 "+profile+"），HTTP "+strconv.Itoa(r.Status)+
		"，来源 "+logx.ClientIP(c.Request)+"，客户端 "+logx.LogText(agent, 80))
}
