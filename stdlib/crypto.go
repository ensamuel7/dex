package stdlib

import (
	_ "embed"

	"github.com/ensamuel7/dex/ast"
)

//go:embed cruntime/crypto.c
var cryptoRuntime string

func init() {
	Register(&Module{
		Name: "crypto",
		Funcs: map[string]FuncDef{
			"uuid": {
				Params:     []ast.Type{},
				ReturnType: ast.TypeString,
				CName:      "dex_crypto_uuid",
				Doc:        "Generate a random UUID v4 string.",
			},
		},
		CRuntime: cryptoRuntime,
	})
}
