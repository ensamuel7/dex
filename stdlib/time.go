package stdlib

import (
	_ "embed"

	"github.com/ensamuel7/dex/ast"
)

//go:embed cruntime/time.c
var timeRuntime string

func init() {
	Register(&Module{
		Name: "time",
		Funcs: map[string]FuncDef{
			"now": {
				Params:     []ast.Type{},
				ReturnType: ast.TypeLong,
				CName:      "dex_time_now",
				Doc:        "Return the current Unix timestamp in seconds.",
			},
			"nowNs": {
				Params:     []ast.Type{},
				ReturnType: ast.TypeLong,
				CName:      "dex_time_now_ns",
				Doc:        "Return the current Unix timestamp in nanoseconds.",
			},
			"sleep": {
				Params:     []ast.Type{ast.TypeInt},
				ParamNames: []string{"ms"},
				ReturnType: ast.TypeVoid,
				CName:      "dex_time_sleep",
				Doc:        "Sleep for the specified number of milliseconds.",
			},
		},
		CRuntime: timeRuntime,
	})
}
