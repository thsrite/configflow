// Package httpx is the HTTP layer every route package plugs into: a
// Werkzeug-compatible URL map, Flask's request object and responses, and
// the request pipeline of backend/app.py (same-origin CORS, authentication
// before profile selection, error handlers, the global JSON token redaction,
// X-ConfigFlow-Profile). It also contains Edge, the built-in replacement of
// docker/nginx.conf, and Invoke, the in-process REST call used by MCP.
//
// # Declaring routes
//
// Each blueprint file of backend/routes becomes a package
// internal/api/<blueprint> with
//
//	func Register(r *httpx.Router) {
//	    r.GET("/api/agents", "agents.handle_agents", auth.Require(listAgents))
//	    r.Handle([]string{"GET", "POST"}, "/api/agents/<agent_id>/notes", "agents.client_notes", auth.Require(notes))
//	    r.POST("/api/agents/<agent_id>/heartbeat", "agents.agent_heartbeat", heartbeat) // checks Agent tokens itself
//	}
//
// The path is the full Flask rule (blueprint url_prefix + route), with
// <name> and <path:name> converters. The endpoint is the Flask endpoint
// "blueprint.function": the blueprint name of the Blueprint(...) the Python
// function is registered on (not the module: routes/client_tools.py
// registers on "agents"), then the function name. The authentication gate,
// the profile selection (repo.IsGlobalRequest) and the redaction whitelists
// are keyed by it. Wrap views that Python decorated with @require_auth in
// auth.Require. Add the Register function to internal/server.
//
// Routing follows Werkzeug: static segments win over converters, HEAD is
// served by GET routes, OPTIONS is answered automatically (Allow header),
// unknown paths give the 404 page and known paths with another method the
// 405 page with Allow, a missing trailing slash or doubled slashes give a
// 308 redirect. Before-request functions run before routing errors, so an
// unauthenticated request to an unknown /api path gets 401, as in Flask.
//
// # Reading the request (Ctx)
//
//	c.Context()            request context with the repo.Scope: pass it to every repo call
//	c.Param("agent_id")    view argument
//	c.Arg("lines")         request.args.get (first value, Werkzeug query parsing); ArgDefault, ArgInt (type=int), ArgList
//	c.Header("X-Foo")      request.headers.get; HeaderDefault
//	c.Bind(&dto)           native strict DTO binding: 415 media / 400 malformed or incompatible JSON
//	c.BindSilent(&dto)     false + zero destination on failure; true for valid JSON
//	httpx.Number           exact literal/form; explicit IsIntegerLiteral/IsBool validation
//	Number.Text()          raw string contents; integer string syntax stays with N3
//
// Binding matches struct names exactly and ignores unknown fields by default.
// DTOs requiring extra-member rejection provide UnmarshalJSON with a
// map[string]json.RawMessage. Custom codecs decode ordinary DTOs via jsondecode.
// Number/RawMessage fields preserve business validation of wrong forms rather
// than turning every wrong field type into a generic decoder error. The native
// binders reject non-finite JSON.
//
//	c.Data(), c.Stream(), c.ContentLength()   request.get_data(), request.stream, request.content_length
//	c.Form(name)           request.form.get
//	c.Method(), c.Path(), c.RemoteAddr(), c.Host(), c.HostURL(), c.URLRoot(), c.Scheme()
//	c.IsInternalCall()     the request came from Invoke (MCP) and is already authenticated
//	c.ProfileID            g.configflow_profile_id ("" for global requests)
//	c.Set/c.Get            flask.g for later hooks
//
// # Responses
//
//	httpx.JSON(v)                       jsonify(v); JSONStatus(status, v); Message(status, success, msg)
//	httpx.External(c, v)                metadata through the same JSON response exit
//	httpx.RawJSON(status, body)         Response(body, mimetype="application/json") (still redacted by the hook)
//	httpx.Text(status, s), Bytes(status, contentType, body), Empty(status)
//	httpx.File(FileOptions{...})        send_file: attachment name, ETag/Last-Modified, 304, Range, never redacted
//	r.WithStatus(n), r.WithHeader(k, v), r.NoStore()   (Cache-Control: no-store)
//
// Values may be native structs, maps, slices, scalars, or json.RawMessage.
// writeJSON encodes first, applies one global token-redaction pass,
// then writes UTF-8 JSON with sorted keys and no trailing newline. The policy
// drops rule_proxy_token / retired_rule_proxy_tokens / rule_fetch_proxy keys and
// replaces any string containing a stored credential with "[REDACTED]", except
// for the endpoint exemptions in sanitize.go. Passthrough files/streams are kept.
//
// # Errors
//
// Return the error. *repo.Error becomes the JSON bodies of
// backend/routes/__init__.py (400 validation, 404 "Profile not found: id",
// 409 exists / in use with "usages", 400 other repository errors);
// *HTTPError renders Werkzeug's page (BadRequest(""), NotFound(), or
// &HTTPError{Code, Description}); anything else, a panic, or a nil response
// is logged as "Exception on <path> [<method>]" and answered with the 500
// page. A returned cancellation matching an ended HTTP request is not logged
// as an application exception. Unrelated errors, panics and cancellations on
// live requests still are. Views that catch exceptions in Python (`except Exception as e:
// return {'success': False, 'message': str(e)}, 500`) do the same in Go
// with err.Error(). App.HandleError adds handlers for other error types.
//
// # Hooks
//
// App.BeforeRequest / AfterRequest add Flask hooks (e.g. the lazy start of
// the Agent rule sync loop). Background loops and shutdown work go to
// internal/lifecycle; repository hooks to repo.RegisterHooks; both from the
// owning package's wiring function listed in internal/server.
//
// # In-process calls
//
// App.Invoke(ctx, InvokeRequest{Method, Path, Query, JSON, Header}) runs a
// request through the whole pipeline with the per-process internal token
// (X-ConfigFlow-Internal), as mcp_server/invoker.call_api did with Flask's
// test client. App.RegisterLocal makes localhttp.Do dispatch to the app.
//
// # Tests
//
// internal/httpx/httpxtest builds the app on a temporary data directory;
// see its package documentation.
package httpx
