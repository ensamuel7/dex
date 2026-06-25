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
				Doc:        "Set a key-value pair on a JSON object. Value can be string, int, bool, long, double, or array.",
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
			"get": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"json", "key"},
				ReturnType: ast.TypeString,
				CName:      "dex_json_get",
				Doc:        "Get a string value from a JSON object by key. Returns empty string if key not found.",
			},
			"getInt": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"json", "key"},
				ReturnType: ast.TypeInt,
				CName:      "dex_json_get_int",
				Doc:        "Get an integer value from a JSON object by key. Returns 0 if key not found.",
			},
			"getBool": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"json", "key"},
				ReturnType: ast.TypeBool,
				CName:      "dex_json_get_bool",
				Doc:        "Get a boolean value from a JSON object by key. Returns false if key not found.",
			},
			"getDouble": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"json", "key"},
				ReturnType: ast.TypeDouble,
				CName:      "dex_json_get_double",
				Doc:        "Get a double value from a JSON object by key. Returns 0.0 if key not found.",
			},
			"getLong": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"json", "key"},
				ReturnType: ast.TypeLong,
				CName:      "dex_json_get_long",
				Doc:        "Get a long integer value from a JSON object by key. Returns 0 if key not found.",
			},
		},
		CRuntime: jsonRuntime,
	})
}
