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
			"setObj": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString, ast.TypeString},
				ParamNames: []string{"json", "key", "value"},
				ReturnType: ast.TypeString,
				CName:      "dex_json_set_obj",
				Doc:        "Set a key to a raw JSON object/array value (not quoted).",
			},
			"get": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"json", "key"},
				ReturnType: ast.TypeString,
				CName:      "dex_json_get",
				Doc:        "Get a value from a JSON object by its top-level key. String values come back unquoted; objects and arrays come back as raw JSON you can pass straight back into json.get to descend a level. Keys inside nested objects are not matched. Returns empty string if the key is not found.",
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
			"arrayNew": {
				Params:     []ast.Type{},
				ReturnType: ast.TypeString,
				CName:      "dex_json_array_new",
				Doc:        "Create a new empty JSON array string.",
			},
			"arrayLen": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"json"},
				ReturnType: ast.TypeInt,
				CName:      "dex_json_array_len",
				Doc:        "Get the length of a JSON array string.",
			},
			"arrayGet": {
				Params:     []ast.Type{ast.TypeString, ast.TypeInt},
				ParamNames: []string{"json", "index"},
				ReturnType: ast.TypeString,
				CName:      "dex_json_array_get",
				Doc:        "Get element at index from a JSON array. Strings are unquoted; objects/arrays returned as-is.",
			},
			"arrayGetRaw": {
				Params:     []ast.Type{ast.TypeString, ast.TypeInt},
				ParamNames: []string{"json", "index"},
				ReturnType: ast.TypeString,
				CName:      "dex_json_array_get_raw",
				Doc:        "Get raw element at index from a JSON array (strings keep quotes).",
			},
			"arrayPush": {
				Params:     nil, // polymorphic — special-cased in checker/codegen
				ParamNames: []string{"arr", "value"},
				ReturnType: ast.TypeString,
				CName:      "",
				Doc:        "Append a value to a JSON array. Value can be string, int, long, double, or bool.",
			},
			"objectify": {
				Params:     nil, // polymorphic — resolved from assignment context
				ParamNames: []string{"json"},
				ReturnType: ast.TypeString, // placeholder — overridden by ResolvedType
				CName:      "",
				Doc:        "Convert a JSON string into a typed struct.",
			},
			"arrayPushObj": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"arr", "obj"},
				ReturnType: ast.TypeString,
				CName:      "dex_json_array_push_obj",
				Doc:        "Append a JSON object/array string to a JSON array.",
			},
		},
		CRuntime: jsonRuntime,
	})
}
