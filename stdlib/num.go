package stdlib

import "github.com/ensamuel7/dex/ast"

// The numeric conversions. They exist because widening is deliberately not
// implicit for a computed value: `let b: long = a` where `a` is an int is a
// type error on purpose, so that a narrowing or a precision loss is always
// something somebody wrote down. A literal still widens on its own, because
// its type is chosen by where it is used.
//
// Every one of these is polymorphic in its argument and compiles to a C cast,
// so they are declared with no Params and resolved in the checker.
func init() {
	Register(&Module{
		Name: "num",
		Funcs: map[string]FuncDef{
			"toInt": {
				Params:     nil,
				ParamNames: []string{"value"},
				ReturnType: ast.TypeInt,
				CName:      "",
				Doc:        "Convert an int, long, double or char to an int. Truncates toward zero.",
			},
			"toLong": {
				Params:     nil,
				ParamNames: []string{"value"},
				ReturnType: ast.TypeLong,
				CName:      "",
				Doc:        "Convert an int, long, double or char to a long. Truncates toward zero.",
			},
			"toDouble": {
				Params:     nil,
				ParamNames: []string{"value"},
				ReturnType: ast.TypeDouble,
				CName:      "",
				Doc:        "Convert an int, long, double or char to a double.",
			},
			"toChar": {
				Params:     nil,
				ParamNames: []string{"value"},
				ReturnType: ast.TypeChar,
				CName:      "",
				Doc:        "Convert an int, long or char to a char, by character code.",
			},
		},
	})
}
