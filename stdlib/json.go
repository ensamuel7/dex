package stdlib

import (
	_ "embed"

	"github.com/ensamuel7/dex/ast"
)

//go:embed cruntime/json.c
var jsonRuntime string

func init() {
	Register(&Module{
		Name: "json",
		Funcs: map[string]FuncDef{
			"new": {
				Params:     []ast.Type{},
				ReturnType: ast.TypeString,
				CName:      "dex_json_new",
			},
			"set": {
				Params:     nil, // polymorphic — special-cased in checker/codegen
				ReturnType: ast.TypeString,
				CName:      "",
			},
			"stringify": {
				Params:     []ast.Type{ast.TypeInt}, // placeholder — special-cased in checker/codegen
				ReturnType: ast.TypeString,
				CName:      "",
			},
			"setArray": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString, ast.TypeInt}, // placeholder
				ReturnType: ast.TypeString,
				CName:      "",
			},
		},
		CIncludes: "",
		CRuntime:  jsonRuntime,
	})
}
