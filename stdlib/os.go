package stdlib

import (
	_ "embed"

	"github.com/ensamuel7/dex/ast"
)

//go:embed cruntime/os.c
var osRuntime string

func init() {
	Register(&Module{
		Name: "os",
		Funcs: map[string]FuncDef{
			"env": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"name"},
				ReturnType: ast.TypeString,
				CName:      "dex_os_env",
				Doc:        "Read an environment variable (returns empty string if unset).",
			},
			"exit": {
				Params:     []ast.Type{ast.TypeInt},
				ParamNames: []string{"code"},
				ReturnType: ast.TypeVoid,
				CName:      "dex_os_exit",
				Doc:        "Exit the process with the given status code.",
			},
			// os.exec is special-cased in checker/codegen (like http.get)
			"exec": {
				Params:     nil,
				ParamNames: []string{"command"},
				ReturnType: ast.TypeVoid, // actual return type resolved by checker
				CName:      "",
				Doc:        "Run a shell command and return an ExecResult with exitCode, output, and error.",
			},
		},
		Types: []ast.StructDef{
			{
				Name: "ExecResult",
				Doc:  "Represents the result of running a shell command via os.exec().",
				Fields: []ast.StructField{
					{Name: "exitCode", Type: ast.TypeInt, Doc: "Exit code of the command (0 = success)."},
					{Name: "output", Type: ast.TypeString, Doc: "Standard output captured from the command."},
					{Name: "error", Type: ast.TypeString, Doc: "Standard error captured from the command."},
				},
			},
		},
		CRuntime:  osRuntime,
		CIncludes: "#include <stdlib.h>\n#include <stdio.h>\n#include <string.h>\n",
	})
}
