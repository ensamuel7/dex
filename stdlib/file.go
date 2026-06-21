package stdlib

import (
	_ "embed"

	"github.com/ensamuel7/dex/ast"
)

//go:embed cruntime/file.c
var fileRuntime string

func init() {
	Register(&Module{
		Name: "file",
		Funcs: map[string]FuncDef{
			"read": {
				Params:     []ast.Type{ast.TypeString},
				ReturnType: ast.TypeString,
				CName:      "dex_file_read",
			},
			"write": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ReturnType: ast.TypeBool,
				CName:      "dex_file_write",
			},
			"append": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ReturnType: ast.TypeBool,
				CName:      "dex_file_append",
			},
			"exists": {
				Params:     []ast.Type{ast.TypeString},
				ReturnType: ast.TypeBool,
				CName:      "dex_file_exists",
			},
			"remove": {
				Params:     []ast.Type{ast.TypeString},
				ReturnType: ast.TypeBool,
				CName:      "dex_file_remove",
			},
			"rename": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ReturnType: ast.TypeBool,
				CName:      "dex_file_rename",
			},
			"move": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ReturnType: ast.TypeBool,
				CName:      "dex_file_rename",
			},
			"upload": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ReturnType: ast.TypeString,
				CName:      "dex_file_upload",
			},
		},
		CFlags:   []string{"-lcurl"},
		CRuntime: fileRuntime,
	})
}
