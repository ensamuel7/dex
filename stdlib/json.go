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
			"encode": {
				Params:     nil, // polymorphic — special-cased in checker/codegen
				ParamNames: []string{"value"},
				ReturnType: ast.TypeString,
				CName:      "",
				Doc:        "Convert a value to a JSON string. Accepts a json.Value, struct, array, or map[string, V]; structs recurse into nested fields to any depth.",
			},
			"decode": {
				Params:     nil, // polymorphic — resolved from assignment context
				ParamNames: []string{"json"},
				ReturnType: ast.TypeString, // placeholder — overridden by ResolvedType
				CName:      "",
				Doc:        "Convert JSON into a typed value. Annotate the target as json.Value for a dynamic document you can index and walk, or as a struct. Recurses into nested struct fields to any depth; a nested object missing from the JSON leaves that sub-struct zeroed. Annotate the target as optional (let x: MyStruct? = json.decode(...)) for a checked decode that returns null on malformed JSON or a type mismatch instead of zero values. The target may also be a struct array (let xs: MyStruct[] = json.decode(...)), decoding a JSON array of objects.",
			},
		},
		// json.Value's runtime is appended rather than kept in json.c so the
		// document-tree code stays separate from the legacy string-scanning
		// helpers it is replacing.
		CRuntime: jsonRuntime + "\n" + jsonValueRuntime,
	})
}
