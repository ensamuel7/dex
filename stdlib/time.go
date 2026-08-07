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
			"setTimeout": {
				Params:     nil,
				ParamNames: []string{"fn", "ms"},
				ReturnType: ast.TypeVoid,
				CName:      "",
				Doc:        "Call a function once after the specified number of milliseconds.",
			},
			"setInterval": {
				Params:     nil,
				ParamNames: []string{"fn", "ms"},
				ReturnType: ast.TypeVoid, // actual return type resolved by checker
				CName:      "",
				Doc:        "Call a function repeatedly every specified number of milliseconds. Returns a timer ID.",
			},
			"clearInterval": {
				Params:     []ast.Type{ast.TypeInt},
				ParamNames: []string{"id"},
				ReturnType: ast.TypeVoid,
				CName:      "dex_time_clear_interval",
				Doc:        "Stop a repeating interval by its timer ID.",
			},
		},
		CFlags:   []string{"-pthread"},
		CRuntime: timeRuntime,
	})
}
