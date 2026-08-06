package stdlib

import (
	_ "embed"

	"github.com/ensamuel7/dex/ast"
)

//go:embed cruntime/io.c
var ioRuntime string

func init() {
	Register(&Module{
		Name: "io",
		Funcs: map[string]FuncDef{
			"prompt": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"message"},
				ReturnType: ast.TypeVoid,
				CName:      "dex_io_prompt",
				Doc:        "Print a message to stdout without a trailing newline and flush.",
			},
			"readLine": {
				Params:     []ast.Type{},
				ReturnType: ast.TypeString,
				CName:      "dex_io_read_line",
				Doc:        "Read a line from stdin (strips trailing newline).",
			},
			"readInt": {
				Params:     []ast.Type{},
				ReturnType: ast.TypeInt,
				CName:      "dex_io_read_int",
				Doc:        "Read and parse an integer from stdin.",
			},
			"readDouble": {
				Params:     []ast.Type{},
				ReturnType: ast.TypeDouble,
				CName:      "dex_io_read_double",
				Doc:        "Read and parse a double from stdin.",
			},
			"readBool": {
				Params:     []ast.Type{},
				ReturnType: ast.TypeBool,
				CName:      "dex_io_read_bool",
				Doc:        "Read a boolean from stdin (\"true\" or \"false\", case-insensitive).",
			},
		},
		CRuntime: ioRuntime,
	})
}
