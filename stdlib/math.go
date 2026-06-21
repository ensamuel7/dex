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
				Doc:        "Return the value of pi (3.14159...).",
			},
			"e": {
				Params:     []ast.Type{},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_e",
				Doc:        "Return Euler's number (2.71828...).",
			},
			// Trigonometric
			"sin": {
				Params:     []ast.Type{ast.TypeDouble},
				ParamNames: []string{"x"},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_sin",
				Doc:        "Return the sine of x (in radians).",
			},
			"cos": {
				Params:     []ast.Type{ast.TypeDouble},
				ParamNames: []string{"x"},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_cos",
				Doc:        "Return the cosine of x (in radians).",
			},
			"tan": {
				Params:     []ast.Type{ast.TypeDouble},
				ParamNames: []string{"x"},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_tan",
				Doc:        "Return the tangent of x (in radians).",
			},
			"asin": {
				Params:     []ast.Type{ast.TypeDouble},
				ParamNames: []string{"x"},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_asin",
				Doc:        "Return the arc sine of x in radians.",
			},
			"acos": {
				Params:     []ast.Type{ast.TypeDouble},
				ParamNames: []string{"x"},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_acos",
				Doc:        "Return the arc cosine of x in radians.",
			},
			"atan": {
				Params:     []ast.Type{ast.TypeDouble},
				ParamNames: []string{"x"},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_atan",
				Doc:        "Return the arc tangent of x in radians.",
			},
			// Power / root
			"sqrt": {
				Params:     []ast.Type{ast.TypeDouble},
				ParamNames: []string{"x"},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_sqrt",
				Doc:        "Return the square root of x.",
			},
			"pow": {
				Params:     []ast.Type{ast.TypeDouble, ast.TypeDouble},
				ParamNames: []string{"base", "exp"},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_pow",
				Doc:        "Return base raised to the power of exp.",
			},
			"exp": {
				Params:     []ast.Type{ast.TypeDouble},
				ParamNames: []string{"x"},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_exp",
				Doc:        "Return e raised to the power of x.",
			},
			// Rounding
			"floor": {
				Params:     []ast.Type{ast.TypeDouble},
				ParamNames: []string{"x"},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_floor",
				Doc:        "Round x down to the nearest integer.",
			},
			"ceil": {
				Params:     []ast.Type{ast.TypeDouble},
				ParamNames: []string{"x"},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_ceil",
				Doc:        "Round x up to the nearest integer.",
			},
			"round": {
				Params:     []ast.Type{ast.TypeDouble},
				ParamNames: []string{"x"},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_round",
				Doc:        "Round x to the nearest integer.",
			},
			// Other
			"abs": {
				Params:     []ast.Type{ast.TypeDouble},
				ParamNames: []string{"x"},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_abs",
				Doc:        "Return the absolute value of x.",
			},
			"log": {
				Params:     []ast.Type{ast.TypeDouble},
				ParamNames: []string{"x"},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_log",
				Doc:        "Return the natural logarithm of x.",
			},
			"log2": {
				Params:     []ast.Type{ast.TypeDouble},
				ParamNames: []string{"x"},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_log2",
				Doc:        "Return the base-2 logarithm of x.",
			},
			"log10": {
				Params:     []ast.Type{ast.TypeDouble},
				ParamNames: []string{"x"},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_log10",
				Doc:        "Return the base-10 logarithm of x.",
			},
			// Two-arg
			"min": {
				Params:     []ast.Type{ast.TypeDouble, ast.TypeDouble},
				ParamNames: []string{"a", "b"},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_min",
				Doc:        "Return the smaller of a and b.",
			},
			"max": {
				Params:     []ast.Type{ast.TypeDouble, ast.TypeDouble},
				ParamNames: []string{"a", "b"},
				ReturnType: ast.TypeDouble,
				CName:      "dex_math_max",
				Doc:        "Return the larger of a and b.",
			},
		},
		CFlags:   []string{"-lm"},
		CRuntime: mathRuntime,
	})
}
