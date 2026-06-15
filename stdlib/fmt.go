package stdlib

import "github.com/ensamuel7/dex-lang/ast"

func init() {
	Register(&Module{
		Name: "fmt",
		Funcs: map[string]FuncDef{
			// print accepts any primitive type — special-cased in checker and codegen
			"print": {
				Params:     nil, // special: checked manually in checker
				ReturnType: ast.TypeVoid,
				CName:      "", // special codegen
			},
		},
		CIncludes: "#include <stdio.h>\n",
	})
}
