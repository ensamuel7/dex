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
			},
			"nowNs": {
				Params:     []ast.Type{},
				ReturnType: ast.TypeLong,
				CName:      "dex_time_now_ns",
			},
			"sleep": {
				Params:     []ast.Type{ast.TypeInt},
				ReturnType: ast.TypeVoid,
				CName:      "dex_time_sleep",
			},
		},
		CIncludes: `#include <time.h>
#include <unistd.h>
`,
		CRuntime: timeRuntime,
	})
}
