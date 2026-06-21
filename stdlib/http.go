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
				ReturnType: ast.TypeVoid,
				CName:      "", // special codegen: function pointer resolution
			},
			"listen": {
				Params:     []ast.Type{ast.TypeInt},
				ReturnType: ast.TypeVoid,
				CName:      "dex_listen",
			},
			// HTTP client functions — special-cased in checker/codegen (Params: nil, CName: "")
			"get": {
				Params:     nil,
				ReturnType: ast.TypeVoid, // actual return type resolved by checker
				CName:      "",
			},
			"post": {
				Params:     nil,
				ReturnType: ast.TypeVoid,
				CName:      "",
			},
			"put": {
				Params:     nil,
				ReturnType: ast.TypeVoid,
				CName:      "",
			},
			"patch": {
				Params:     nil,
				ReturnType: ast.TypeVoid,
				CName:      "",
			},
			"delete": {
				Params:     nil,
				ReturnType: ast.TypeVoid,
				CName:      "",
			},
			"request": {
				Params:     nil,
				ReturnType: ast.TypeVoid,
				CName:      "",
			},
			"formNew": {
				Params:     nil,
				ReturnType: ast.TypeString,
				CName:      "",
			},
			"formField": {
				Params:     nil,
				ReturnType: ast.TypeString,
				CName:      "",
			},
			"formFile": {
				Params:     nil,
				ReturnType: ast.TypeString,
				CName:      "",
			},
			"header": {
				Params:     nil,
				ReturnType: ast.TypeString,
				CName:      "",
			},
			"postForm": {
				Params:     nil,
				ReturnType: ast.TypeVoid,
				CName:      "",
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
		CIncludes: `#include <stdio.h>
        #include <stdlib.h>
        #include <string.h>
        #include <sys/socket.h>
        #include <sys/uio.h>
        #include <netinet/in.h>
        #include <unistd.h>
        #include <pthread.h>
        #include <signal.h>
        #include <curl/curl.h>
        `,
		CFlags:   []string{"-pthread", "-lcurl"},
		CRuntime: httpRuntime,
	})
}
