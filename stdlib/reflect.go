package stdlib

import (
	_ "embed"

	"github.com/ensamuel7/dex/ast"
)

//go:embed cruntime/reflect.c
var reflectRuntime string

// The reflect module reads a struct's fields by position rather than by name,
// so a program can walk a value whose shape it does not have written down.
//
// Every function here is polymorphic in its first argument — the subject is any
// struct — so none can be given a signature. They carry `Params: nil` and
// `CName: ""` and are resolved in checker/check_stdlib.go and emitted in
// codegen/gen_call_reflect.go, the same arrangement json.encode uses.
//
// Two properties are worth stating because they are what makes reflection
// affordable here rather than the usual tradeoff.
//
// Metadata is opt-in: the checker records each struct a reflect.* call names and
// codegen emits tables for exactly those, so a program that does not import
// reflect is byte-for-byte what it was before this module existed.
//
// Access is typed: codegen emits one accessor per kind per struct, each a switch
// over the field index whose cases are ordinary field reads. The C compiler
// type-checks every case, so asLong on a string field cannot reinterpret a
// pointer as an integer — the field is simply not a case in that switch, and the
// call returns a zero value.
func init() {
	Register(&Module{
		Name:     "reflect",
		CRuntime: reflectRuntime,
		Funcs: map[string]FuncDef{
			"typeName": {
				Params:     nil,
				ParamNames: []string{"value"},
				ReturnType: ast.TypeString,
				CName:      "",
				Doc:        "Name of the struct's type, as written in its declaration. Known at compile time, so this costs no more than a string literal.",
			},
			"fieldCount": {
				Params:     nil,
				ParamNames: []string{"value"},
				ReturnType: ast.TypeInt,
				CName:      "",
				Doc:        "How many fields the struct declares. A compile-time constant — it becomes a literal in the generated C, so a loop over it can be unrolled by the C compiler.",
			},
			"fields": {
				Params:     nil,
				ParamNames: []string{"value"},
				ReturnType: ast.TypeArrayString,
				CName:      "",
				Doc:        "The struct's field names, in declaration order. Allocates a fresh array; prefer fieldName() in a loop when the names are only being read.",
			},
			"fieldName": {
				Params:     nil,
				ParamNames: []string{"value", "index"},
				ReturnType: ast.TypeString,
				CName:      "",
				Doc:        "Name of the field at this index, or \"\" if the index is out of range. Reads a table of string literals rather than allocating.",
			},
			"fieldKind": {
				Params:     nil,
				ParamNames: []string{"value", "index"},
				ReturnType: ast.TypeString,
				CName:      "",
				Doc:        "Type of the field at this index as a string: \"int\", \"long\", \"double\", \"bool\", \"string\", \"char\", \"struct\", \"array\", or \"other\" for a field reflection cannot read; \"\" if the index is out of range. Compare it to pick which as* accessor to call.",
			},
			"indexOf": {
				Params:     nil,
				ParamNames: []string{"value", "name"},
				ReturnType: ast.TypeInt,
				CName:      "",
				Doc:        "Index of the named field, or -1 if the struct has no such field. Folded to a constant when the name is a string literal, so reflect.asString(v, reflect.indexOf(v, \"id\")) compiles to the same load as v.id.",
			},
			"has": {
				Params:     nil,
				ParamNames: []string{"value", "name"},
				ReturnType: ast.TypeBool,
				CName:      "",
				Doc:        "Whether the struct declares a field with this name. Folded to a constant when the name is a string literal.",
			},
			"asInt": {
				Params:     nil,
				ParamNames: []string{"value", "index"},
				ReturnType: ast.TypeInt,
				CName:      "",
				Doc:        "Read an int field by index. Returns 0 if the index is out of range or names a field of another type.",
			},
			"asLong": {
				Params:     nil,
				ParamNames: []string{"value", "index"},
				ReturnType: ast.TypeLong,
				CName:      "",
				Doc:        "Read a long field by index. Returns 0 if the index is out of range or names a field of another type.",
			},
			"asDouble": {
				Params:     nil,
				ParamNames: []string{"value", "index"},
				ReturnType: ast.TypeDouble,
				CName:      "",
				Doc:        "Read a double field by index. Returns 0.0 if the index is out of range or names a field of another type.",
			},
			"asBool": {
				Params:     nil,
				ParamNames: []string{"value", "index"},
				ReturnType: ast.TypeBool,
				CName:      "",
				Doc:        "Read a bool field by index. Returns false if the index is out of range or names a field of another type.",
			},
			"asString": {
				Params:     nil,
				ParamNames: []string{"value", "index"},
				ReturnType: ast.TypeString,
				CName:      "",
				Doc:        "Read a string field by index. Returns \"\" if the index is out of range or names a field of another type. Use toString() to render a field of any type.",
			},
			"toString": {
				Params:     nil,
				ParamNames: []string{"value", "index"},
				ReturnType: ast.TypeString,
				CName:      "",
				Doc:        "Render the field at this index as text whatever its type — an int as its digits, a bool as \"true\" or \"false\", a string as itself. Returns \"\" for an out-of-range index, and for a struct or array field, which have no single-line rendering; use json.encode for those.",
			},
		},
	})
}
