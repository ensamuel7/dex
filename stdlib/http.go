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
				Params:     []ast.Type{ast.TypeInt},
				ParamNames: []string{"port"},
				ReturnType: ast.TypeVoid,
				CName:      "dex_listen",
				Doc:        "Start the HTTP server on the specified port.",
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
		},
		Types: []ast.StructDef{
			{
				Name: "HttpResponse",
				Fields: []ast.StructField{
					{Name: "statusCode", Type: ast.TypeInt},
					{Name: "body", Type: ast.TypeString},
				},
			},
		},
		CFlags:   []string{"-pthread", "-lcurl"},
		CRuntime: httpRuntime,
	})
}
