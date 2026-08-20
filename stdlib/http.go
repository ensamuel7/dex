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
		},
		Types: []ast.StructDef{
			{
				Name: "HttpResponse",
				Doc:  "Represents an HTTP response returned by client request functions (get, post, put, patch, delete, request).",
				Fields: []ast.StructField{
					{Name: "statusCode", Type: ast.TypeInt, Doc: "HTTP status code (e.g. 200, 404, 500)."},
					{Name: "body", Type: ast.TypeString, Doc: "Response body content as a string."},
					{Name: "contentType", Type: ast.TypeString, Doc: "Content-Type header from the response (e.g. \"application/json\"). Optional in struct literals — defaults to \"application/json\" on the server."},
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
				},
			},
		},
		CFlags:   []string{"-pthread", "-lcurl"},
		CRuntime: httpRuntime,
	})
}
