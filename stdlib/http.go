package stdlib

import (
	_ "embed"

	"github.com/ensamuel7/dex/ast"
)

//go:embed cruntime/http.c
var httpRuntime string

func init() {
	Register(&Module{
		Name: "http",
		Funcs: map[string]FuncDef{
			"route": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString, ast.TypeString},
				ParamNames: []string{"method", "path", "handler"},
				ReturnType: ast.TypeVoid,
				CName:      "", // special codegen: function pointer resolution
				Doc:        "Register a route handler for the given HTTP method and path.",
			},
			"listenOn": {
				Params:     nil, // special-cased in checker: (string, int) or (string, int, int)
				ParamNames: []string{"host", "port", "workers"},
				ReturnType: ast.TypeVoid,
				CName:      "",
				Doc:        "Start the HTTP server bound to one interface. http.listen() binds every interface; this one names the address, for a service behind a proxy that should not also be reachable directly. The host must be an IPv4 address — \"127.0.0.1\", or \"0.0.0.0\" for every interface. Optional third argument is the worker process count (0 = one per CPU).",
			},
			"uring": {
				Params:     []ast.Type{},
				ReturnType: ast.TypeVoid,
				CName:      "dex_http_set_uring",
				Doc:        "Serve with io_uring instead of epoll, on Linux. Call before listen. Accepts, reads, writes and closes are queued on one ring and submitted together, so a pass of the loop costs one system call however many connections it served. Handlers run on the thread that owns the ring, as with http.inline(). Where io_uring is unavailable — another platform, an older kernel, or a container that blocks it — the server says so and uses epoll.",
			},
			"inline": {
				Params:     []ast.Type{},
				ReturnType: ast.TypeVoid,
				CName:      "dex_http_set_inline",
				Doc:        "Run every handler on the thread that owns the sockets instead of handing it to the pool. Call before listen. Each request then costs no thread handoff, lock or wakeup, and its response is written straight away, which is the fastest choice on one core when handlers are short and only touch local state such as an SQLite file. Handlers run one at a time, so a handler that blocks, on an outbound request or a sleep, stalls every connection while it does.",
			},
			"threads": {
				Params:     []ast.Type{ast.TypeInt},
				ParamNames: []string{"count"},
				ReturnType: ast.TypeVoid,
				CName:      "dex_http_set_threads",
				Doc:        "How many threads run handlers. Call before listen. 0, the default, sizes the pool from the CPU count with a floor of four — the right hedge when a handler might block on a database or an outbound request. Set it lower when handlers are CPU-bound and serialised behind a lock on a small box, where extra threads only add context switches to work that cannot overlap.",
			},
			"limits": {
				Params:     []ast.Type{ast.TypeInt, ast.TypeInt, ast.TypeInt},
				ParamNames: []string{"maxBodyBytes", "idleTimeoutSecs", "maxConnections"},
				ReturnType: ast.TypeVoid,
				CName:      "dex_http_set_limits",
				Doc:        "Bound what one client can cost the server. Call before listen. Defaults are 8 MB, 30 seconds and 8192 connections; a 0 for any argument keeps that default, so raising the body limit for uploads does not mean restating the others. The header block is capped at 32 KB and is not adjustable. A body larger than the limit is answered 413, headers that never terminate 431, and a connection that stops making progress for the idle timeout is closed. The connection limit is also held below the process's open-file limit. Past fifteen sixteenths of it, responses carry Connection: close so idle clients reconnect for their next request, and at the limit new connections wait in the listen queue until one closes.",
			},
			"notFound": {
				Params:     nil, // special codegen: function value, like route
				ParamNames: []string{"handler"},
				ReturnType: ast.TypeVoid,
				CName:      "",
				Doc:        "Answer requests no route matched. The handler takes the same shapes http.route() accepts, so a program can give its 404 its own body and content type instead of the built-in one. Registering twice replaces the first.",
			},
			"listen": {
				Params:     nil, // special-cased in checker: 1-2 int args
				ParamNames: []string{"port", "workers"},
				ReturnType: ast.TypeVoid,
				CName:      "",
				Doc:        "Start the HTTP server on the specified port. Optional second argument specifies the number of worker processes (0 = auto-detect based on CPU cores).",
			},
			// HTTP client functions — special-cased in checker/codegen (Params: nil, CName: "")
			"get": {
				Params:     nil,
				ParamNames: []string{"url", "headers"},
				ReturnType: ast.TypeVoid, // actual return type resolved by checker
				CName:      "",
				Doc:        "Send an HTTP GET request.",
			},
			"post": {
				Params:     nil,
				ParamNames: []string{"url", "body", "headers"},
				ReturnType: ast.TypeVoid,
				CName:      "",
				Doc:        "Send an HTTP POST request.",
			},
			"put": {
				Params:     nil,
				ParamNames: []string{"url", "body", "headers"},
				ReturnType: ast.TypeVoid,
				CName:      "",
				Doc:        "Send an HTTP PUT request.",
			},
			"patch": {
				Params:     nil,
				ParamNames: []string{"url", "body", "headers"},
				ReturnType: ast.TypeVoid,
				CName:      "",
				Doc:        "Send an HTTP PATCH request.",
			},
			"delete": {
				Params:     nil,
				ParamNames: []string{"url", "headers"},
				ReturnType: ast.TypeVoid,
				CName:      "",
				Doc:        "Send an HTTP DELETE request.",
			},
			"request": {
				Params:     nil,
				ParamNames: []string{"method", "url", "body", "headers"},
				ReturnType: ast.TypeVoid,
				CName:      "",
				Doc:        "Send a custom HTTP request with the given method.",
			},
			"requestWith": {
				Params:     nil,
				ParamNames: []string{"method", "url", "body", "headers", "options"},
				ReturnType: ast.TypeVoid,
				CName:      "",
				Doc:        "Send a request under explicit HttpOptions: timeouts, redirect following, and TLS verification. Use it for any host you do not control.",
			},
			"formNew": {
				Params:     nil,
				ReturnType: ast.TypeString,
				CName:      "",
				Doc:        "Create a new multipart form.",
			},
			"formField": {
				Params:     nil,
				ParamNames: []string{"form", "name", "value"},
				ReturnType: ast.TypeString,
				CName:      "",
				Doc:        "Add a text field to a multipart form.",
			},
			"formFile": {
				Params:     nil,
				ParamNames: []string{"form", "name", "path"},
				ReturnType: ast.TypeString,
				CName:      "",
				Doc:        "Add a file field to a multipart form.",
			},
			"header": {
				Params:     nil,
				ParamNames: []string{"headers", "key", "value"},
				ReturnType: ast.TypeString,
				CName:      "",
				Doc:        "Add a header key-value pair to a headers string.",
			},
			"postForm": {
				Params:     nil,
				ParamNames: []string{"url", "form", "headers"},
				ReturnType: ast.TypeVoid,
				CName:      "",
				Doc:        "Send an HTTP POST request with multipart form data.",
			},
			"upload": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"path", "url"},
				ReturnType: ast.TypeString,
				CName:      "dex_file_upload",
				Doc:        "Upload a file to the given URL and return the response.",
			},
			"response": {
				Params:     nil,
				ParamNames: []string{"statusCode", "body", "contentType"},
				ReturnType: ast.TypeVoid, // actual return type resolved by checker
				CName:      "",
				Doc:        "Create an HttpResponse with the given status code, body, and content type.",
			},
			"responseWith": {
				Params:     nil,
				ParamNames: []string{"statusCode", "body", "contentType", "headers"},
				ReturnType: ast.TypeVoid, // actual return type resolved by checker
				CName:      "",
				Doc:        "Create an HttpResponse carrying extra headers, newline-separated \"Key: Value\" pairs as built by http.header(). Use for Set-Cookie (which may repeat) and the Access-Control-* family.",
			},
		},
		Types: []ast.StructDef{
			{
				Name: "HttpOptions",
				Doc:  "Options for http.requestWith(). Every field's zero value is its default, so an empty literal behaves exactly like http.request().",
				Fields: []ast.StructField{
					{Name: "timeoutMs", Type: ast.TypeInt, Doc: "Deadline for the whole request. 0 for the default of 30000."},
					{Name: "connectTimeoutMs", Type: ast.TypeInt, Doc: "Deadline for the connection alone. 0 for the default of 10000."},
					{Name: "maxRedirects", Type: ast.TypeInt, Doc: "How many redirects to follow. 0, the default, follows none and hands you the 3xx with its Location header."},
					{Name: "insecureTls", Type: ast.TypeBool, Doc: "Skip certificate and hostname verification. Named for the danger rather than the safety so that the zero value verifies: set it only when a broken certificate is the thing being inspected."},
				},
			},
			{
				Name: "HttpResponse",
				Doc:  "Represents an HTTP response returned by client request functions (get, post, put, patch, delete, request).",
				Fields: []ast.StructField{
					{Name: "statusCode", Type: ast.TypeInt, Doc: "HTTP status code (e.g. 200, 404, 500)."},
					{Name: "body", Type: ast.TypeString, Doc: "Response body content as a string."},
					{Name: "contentType", Type: ast.TypeString, Doc: "Content-Type header from the response (e.g. \"application/json\"). Optional in struct literals — defaults to \"application/json\" on the server."},
					{Name: "headers", Type: ast.TypeString, Doc: "Headers, newline-separated \"Key: Value\" pairs as built by http.header(). On a response you build, the extra headers to send; on a response from a client call, the headers that arrived, without the status line. Empty for none. A key may repeat, which is what Set-Cookie needs."},
				},
			},
			{
				Name: "HttpRequest",
				Doc:  "Represents an incoming HTTP request passed to route handlers.",
				Fields: []ast.StructField{
					{Name: "method", Type: ast.TypeString, Doc: "HTTP method (e.g. \"GET\", \"POST\")."},
					{Name: "path", Type: ast.TypeString, Doc: "Request path (e.g. \"/api/foo\")."},
					{Name: "body", Type: ast.TypeString, Doc: "Request body (empty string if none)."},
					{Name: "query", Type: ast.TypeString, Doc: "Raw query string after '?' (empty if none)."},
					{Name: "params", Type: ast.MapTypeOf(ast.TypeString, ast.TypeString), Doc: "Route parameters extracted from :param segments (e.g. \"/users/:id\" → params.get(\"id\"))."},
					{Name: "headers", Type: ast.MapTypeOf(ast.TypeString, ast.TypeString), Doc: "Request headers. Keys are lower-cased, because HTTP header names are case-insensitive — read \"authorization\", not \"Authorization\"."},
				},
			},
		},
		CFlags:   []string{"-pthread", "-lcurl"},
		CRuntime: httpRuntime,
	})
}
