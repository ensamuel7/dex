package stdlib

import (
	_ "embed"

	"github.com/ensamuel7/dex/ast"
)

//go:embed cruntime/math.c
var mathRuntime string

func init() {
	Register(&Module{
		Name: "math",
		Funcs: map[string]FuncDef{
			// Constants (zero-arg, return double)
			"pi": {
				Params:     []ast.Type{},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_pi",
			},
			"e": {
				Params:     []ast.Type{},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_e",
			},
			// Trigonometric
			"sin": {
				Params:     []ast.Type{ast.TypeDouble},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_sin",
			},
			"cos": {
				Params:     []ast.Type{ast.TypeDouble},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_cos",
			},
			"tan": {
				Params:     []ast.Type{ast.TypeDouble},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_tan",
			},
			"asin": {
				Params:     []ast.Type{ast.TypeDouble},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_asin",
			},
			"acos": {
				Params:     []ast.Type{ast.TypeDouble},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_acos",
			},
			"atan": {
				Params:     []ast.Type{ast.TypeDouble},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_atan",
			},
			// Power / root
			"sqrt": {
				Params:     []ast.Type{ast.TypeDouble},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_sqrt",
			},
			"pow": {
				Params:     []ast.Type{ast.TypeDouble, ast.TypeDouble},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_pow",
			},
			"exp": {
				Params:     []ast.Type{ast.TypeDouble},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_exp",
			},
			// Rounding
			"floor": {
				Params:     []ast.Type{ast.TypeDouble},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_floor",
			},
			"ceil": {
				Params:     []ast.Type{ast.TypeDouble},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_ceil",
			},
			"round": {
				Params:     []ast.Type{ast.TypeDouble},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_round",
			},
			// Other
			"abs": {
				Params:     []ast.Type{ast.TypeDouble},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_abs",
			},
			"log": {
				Params:     []ast.Type{ast.TypeDouble},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_log",
			},
			"log2": {
				Params:     []ast.Type{ast.TypeDouble},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_log2",
			},
			"log10": {
				Params:     []ast.Type{ast.TypeDouble},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_log10",
			},
			// Two-arg
			"min": {
				Params:     []ast.Type{ast.TypeDouble, ast.TypeDouble},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_min",
			},
			"max": {
				Params:     []ast.Type{ast.TypeDouble, ast.TypeDouble},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_max",
			},
		},
		CFlags: []string{"-lm"},
		CRuntime:  mathRuntime,
	})
}
