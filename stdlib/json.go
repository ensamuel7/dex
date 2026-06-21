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
				Doc:        "Create a new empty JSON object.",
			},
			"set": {
				Params:     nil, // polymorphic — special-cased in checker/codegen
				ParamNames: []string{"json", "key", "value"},
				ReturnType: ast.TypeString,
				CName:      "",
				Doc:        "Set a key-value pair on a JSON object.",
			},
			"stringify": {
				Params:     []ast.Type{ast.TypeInt}, // placeholder — special-cased in checker/codegen
				ParamNames: []string{"value"},
				ReturnType: ast.TypeString,
				CName:      "",
				Doc:        "Convert a value to a JSON string.",
			},
			"setArray": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString, ast.TypeInt}, // placeholder
				ParamNames: []string{"json", "key", "length"},
				ReturnType: ast.TypeString,
				CName:      "",
				Doc:        "Set an array field on a JSON object.",
			},
		},
		CRuntime: jsonRuntime,
	})
}
