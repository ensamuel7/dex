package stdlib

import (
	_ "embed"

	"github.com/ensamuel7/dex/ast"
)

//go:embed cruntime/http.c
var httpRuntime string

func init() {
	Register(&Module{
		Name: "http",
		Funcs: map[string]FuncDef{
			"route": {
				Params:     []ast.Type{ast.TypeString, ast.TypeString, ast.TypeString},
				ReturnType: ast.TypeVoid,
				CName:      "", // special codegen: function pointer resolution
			},
			"listen": {
				Params:     []ast.Type{ast.TypeInt},
				ReturnType: ast.TypeVoid,
				CName:      "dex_listen",
			},
		},
		CIncludes: `#include <stdio.h>
        #include <sys/socket.h>
        #include <sys/uio.h>
        #include <netinet/in.h>
        #include <unistd.h>
        #include <pthread.h>
        #include <signal.h>
        `,
		CFlags:   []string{"-pthread"},
		CRuntime: httpRuntime,
	})
}
