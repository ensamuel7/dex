package stdlib

import "github.com/ensamuel7/dex/ast"

func init() {
	Register(&Module{
		Name: "fmt",
		Funcs: map[string]FuncDef{
			// print accepts any primitive type — special-cased in checker and codegen
			"print": {
				Params:     nil, // special: checked manually in checker
				ParamNames: []string{"value"},
				ReturnType: ast.TypeVoid,
				CName:      "", // special codegen
				Doc:        "Print a value to stdout followed by a newline.",
			},
		},
		CIncludes: "#include <stdio.h>\n",
	})
}
