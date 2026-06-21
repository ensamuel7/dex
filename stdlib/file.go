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
				ParamNames: []string{"path"},
				ReturnType: ast.TypeString,
				CName:      "dex_file_read",
				Doc:        "Read the entire contents of a file as a string.",
			},
			"write": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"path", "content"},
				ReturnType: ast.TypeBool,
				CName:      "dex_file_write",
				Doc:        "Write content to a file, overwriting if it exists.",
			},
			"append": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"path", "content"},
				ReturnType: ast.TypeBool,
				CName:      "dex_file_append",
				Doc:        "Append content to the end of a file.",
			},
			"exists": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"path"},
				ReturnType: ast.TypeBool,
				CName:      "dex_file_exists",
				Doc:        "Check if a file exists at the given path.",
			},
			"remove": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"path"},
				ReturnType: ast.TypeBool,
				CName:      "dex_file_remove",
				Doc:        "Delete a file at the given path.",
			},
			"rename": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"oldPath", "newPath"},
				ReturnType: ast.TypeBool,
				CName:      "dex_file_rename",
				Doc:        "Rename a file from oldPath to newPath.",
			},
			"move": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString},
				ParamNames: []string{"src", "dst"},
				ReturnType: ast.TypeBool,
				CName:      "dex_file_rename",
				Doc:        "Move a file from src to dst.",
			},
		},
		CFlags:   []string{"-lcurl"},
		CRuntime: fileRuntime,
	})
}
