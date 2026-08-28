package stdlib

import (
	_ "embed"

	"github.com/ensamuel7/dex/ast"
)

//go:embed cruntime/json.c
var jsonRuntime string

//go:embed cruntime/json_value.c
var jsonValueRuntime string

func init() {
	Register(&Module{
		Name: "json",
		Funcs: map[string]FuncDef{
			"new": {
				Params:     []ast.Type{},
				ReturnType: ast.TypeString,
				CName:      "dex_json_new",
				Doc:        "Deprecated: use an object literal, e.g. let v: json.Value = {}. Create a new empty JSON object.",
			},
			"set": {
				Params:     nil, // polymorphic — special-cased in checker/codegen
				ParamNames: []string{"json", "key", "value"},
				ReturnType: ast.TypeString,
				CName:      "",
				Doc:        "Deprecated: use an object literal, e.g. let v: json.Value = { key: value }. Set a key-value pair on a JSON object.",
			},
			"encode": {
				Params:     nil, // polymorphic — special-cased in checker/codegen
				ParamNames: []string{"value"},
				ReturnType: ast.TypeString,
				CName:      "",
				Doc:        "Convert a value to a JSON string. Accepts a json.Value, struct, array, or map[string, V]; structs recurse into nested fields to any depth.",
			},
			"setArray": {
				Params:     nil, // polymorphic — special-cased in checker/codegen
				ParamNames: []string{"json", "key", "length"},
				ReturnType: ast.TypeString,
				CName:      "",
				Doc:        "Deprecated: use an object literal, e.g. { key: arr }. Set an array field on a JSON object.",
			},
			"setObj": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString, ast.TypeString},
				ParamNames: []string{"json", "key", "value"},
				ReturnType: ast.TypeString,
				CName:      "dex_json_set_obj",
				Doc:        "Deprecated: a nested json.Value needs no special call. Set a key to a raw JSON object/array value.",
			},
			"get": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"json", "key"},
				ReturnType: ast.TypeString,
				CName:      "dex_json_get",
				Doc:        "Deprecated: use indexing, e.g. v[\"key\"].asString(). Get a value from a JSON object by its top-level key. String values come back unquoted; objects and arrays come back as raw JSON you can pass straight back into json.get to descend a level. Keys inside nested objects are not matched. Returns empty string if the key is not found.",
			},
			"getInt": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"json", "key"},
				ReturnType: ast.TypeInt,
				CName:      "dex_json_get_int",
				Doc:        "Deprecated: use indexing, e.g. v[\"key\"].asInt(). Get an integer value from a JSON object by key. Returns 0 if key not found.",
			},
			"getBool": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"json", "key"},
				ReturnType: ast.TypeBool,
				CName:      "dex_json_get_bool",
				Doc:        "Deprecated: use indexing, e.g. v[\"key\"].asBool(). Get a boolean value from a JSON object by key. Returns false if key not found.",
			},
			"getDouble": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"json", "key"},
				ReturnType: ast.TypeDouble,
				CName:      "dex_json_get_double",
				Doc:        "Deprecated: use indexing, e.g. v[\"key\"].asDouble(). Get a double value from a JSON object by key. Returns 0.0 if key not found.",
			},
			"getLong": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"json", "key"},
				ReturnType: ast.TypeLong,
				CName:      "dex_json_get_long",
				Doc:        "Deprecated: use indexing, e.g. v[\"key\"].asLong(). Get a long integer value from a JSON object by key. Returns 0 if key not found.",
			},
			"arrayNew": {
				Params:     []ast.Type{},
				ReturnType: ast.TypeString,
				CName:      "dex_json_array_new",
				Doc:        "Deprecated: use an array literal, e.g. let v: json.Value = []. Create a new empty JSON array string.",
			},
			"arrayLen": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"json"},
				ReturnType: ast.TypeInt,
				CName:      "dex_json_array_len",
				Doc:        "Deprecated: use v.len(). Get the length of a JSON array string.",
			},
			"arrayGet": {
				Params:     []ast.Type{ast.TypeString, ast.TypeInt},
				ParamNames: []string{"json", "index"},
				ReturnType: ast.TypeString,
				CName:      "dex_json_array_get",
				Doc:        "Deprecated: use indexing, e.g. v[0].asString(). Get element at index from a JSON array. Strings are unquoted; objects/arrays returned as-is.",
			},
			"arrayGetRaw": {
				Params:     []ast.Type{ast.TypeString, ast.TypeInt},
				ParamNames: []string{"json", "index"},
				ReturnType: ast.TypeString,
				CName:      "dex_json_array_get_raw",
				Doc:        "Deprecated: use indexing — v[0] is already a json.Value. Get raw element at index from a JSON array.",
			},
			"arrayPush": {
				Params:     nil, // polymorphic — special-cased in checker/codegen
				ParamNames: []string{"arr", "value"},
				ReturnType: ast.TypeString,
				CName:      "",
				Doc:        "Deprecated: use an array literal, e.g. let v: json.Value = [a, b, c]. Append a value to a JSON array.",
			},
			"decode": {
				Params:     nil, // polymorphic — resolved from assignment context
				ParamNames: []string{"json"},
				ReturnType: ast.TypeString, // placeholder — overridden by ResolvedType
				CName:      "",
				Doc:        "Convert JSON into a typed value. Annotate the target as json.Value for a dynamic document you can index and walk, or as a struct. Recurses into nested struct fields to any depth; a nested object missing from the JSON leaves that sub-struct zeroed. Annotate the target as optional (let x: MyStruct? = json.decode(...)) for a checked decode that returns null on malformed JSON or a type mismatch instead of zero values. The target may also be a struct array (let xs: MyStruct[] = json.decode(...)), decoding a JSON array of objects.",
			},
			"arrayPushObj": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"arr", "obj"},
				ReturnType: ast.TypeString,
				CName:      "dex_json_array_push_obj",
				Doc:        "Deprecated: a nested json.Value needs no special call. Append a JSON object/array string to a JSON array.",
			},
		},
		// json.Value's runtime is appended rather than kept in json.c so the
		// document-tree code stays separate from the legacy string-scanning
		// helpers it is replacing.
		CRuntime: jsonRuntime + "\n" + jsonValueRuntime,
	})
}
