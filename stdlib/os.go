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
			"args": {
				Params:     []ast.Type{},
				ParamNames: []string{},
				ReturnType: ast.TypeArrayString,
				CName:      "dex_os_args",
				RawReturn:  true,
				Doc:        "The program's command-line arguments, excluding the program name.",
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
				Params:     nil, // special-cased in checker/codegen
				ParamNames: []string{"command"},
				ReturnType: ast.TypeVoid, // actual return type resolved by checker
				CName:      "",
				Doc:        "Run a shell command and return an ExecResult with exitCode, output, and error.",
			},
			// os.execWith is special-cased in checker/codegen, as os.exec is
			"execWith": {
				Params:     nil,
				ParamNames: []string{"command", "options"},
				ReturnType: ast.TypeVoid,
				CName:      "",
				Doc:        "Run a shell command under ExecOptions: a deadline, and a file its output is appended to as it arrives.",
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
					{Name: "timedOut", Type: ast.TypeBool, Doc: "The command was stopped at its deadline (os.execWith only); exitCode is then 124."},
				},
			},
			{
				Name: "ExecOptions",
				Doc:  "Options for os.execWith(). Every field's zero value is its default, so an empty literal behaves exactly like os.exec().",
				Fields: []ast.StructField{
					{Name: "timeoutMs", Type: ast.TypeInt, Doc: "Stop the command and everything it started after this long. 0, the default, waits for ever."},
					{Name: "appendTo", Type: ast.TypeString, Doc: "Append stdout and stderr to this file as they arrive, interleaved in the order written, instead of returning them. \"\", the default, returns them in output and error."},
				},
			},
		},
		CRuntime: osRuntime,
	})
}
