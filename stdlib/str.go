package stdlib

import (
	_ "embed"

	"github.com/ensamuel7/dex/ast"
)

//go:embed cruntime/str.c
var strRuntime string

func init() {
	Register(&Module{
		Name: "str",
		Funcs: map[string]FuncDef{
			"fromInt": {
				Params:     []ast.Type{ast.TypeInt},
				ParamNames: []string{"n"},
				ReturnType: ast.TypeString,
				CName:      "dex_str_fromInt",
				Doc:        "Convert an integer to its string representation.",
			},
			"fromLong": {
				Params:     []ast.Type{ast.TypeLong},
				ParamNames: []string{"n"},
				ReturnType: ast.TypeString,
				CName:      "dex_str_fromLong",
				Doc:        "Convert a long to its string representation.",
			},
			"fromDouble": {
				Params:     []ast.Type{ast.TypeDouble},
				ParamNames: []string{"d"},
				ReturnType: ast.TypeString,
				CName:      "dex_str_fromDouble",
				Doc:        "Convert a double to its string representation.",
			},
			"fromBool": {
				Params:     []ast.Type{ast.TypeBool},
				ParamNames: []string{"b"},
				ReturnType: ast.TypeString,
				CName:      "dex_str_fromBool",
				Doc:        "Convert a boolean to \"true\" or \"false\".",
			},
			"toInt": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"s"},
				ReturnType: ast.TypeInt,
				CName:      "dex_str_toInt",
				Doc:        "Parse a string as an integer.",
			},
			"toLong": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"s"},
				ReturnType: ast.TypeLong,
				CName:      "dex_str_toLong",
				Doc:        "Parse a string as a long integer.",
			},
			"toDouble": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"s"},
				ReturnType: ast.TypeDouble,
				CName:      "dex_str_toDouble",
				Doc:        "Parse a string as a double.",
			},
		},
		CRuntime: strRuntime,
	})
}
