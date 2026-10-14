package parser

import (
	"strings"
	"testing"

	"github.com/ensamuel7/dex/ast"
)

// A generic declaration is captured as tokens and skipped, so it does not
// appear among the program's ordinary functions or structs.
func TestParseGenericCapture(t *testing.T) {
	prog := parse(t, `
	fn identity<T>(x: T): T {
		return x
	}
	struct Box<T> {
		value: T

		fn get(): T {
			return value
		}
	}
	fn main(): int {
		return 0
	}`)

	if len(prog.GenericFuncs) != 1 {
		t.Fatalf("GenericFuncs = %d, want 1", len(prog.GenericFuncs))
	}
	gf := prog.GenericFuncs[0]
	if gf.Name != "identity" {
		t.Errorf("generic func name = %q, want %q", gf.Name, "identity")
	}
	if len(gf.TypeParams) != 1 || gf.TypeParams[0] != "T" {
		t.Errorf("type params = %v, want [T]", gf.TypeParams)
	}
	if len(gf.Tokens) == 0 {
		t.Error("generic func captured no tokens")
	}
	// The signature is parsed at capture, because inference needs it.
	if len(gf.Params) != 1 {
		t.Fatalf("captured params = %d, want 1", len(gf.Params))
	}
	if !ast.IsTypeParamType(gf.Params[0].Type) {
		t.Errorf("param type = %v, want a type parameter", gf.Params[0].Type)
	}
	if !ast.IsTypeParamType(gf.ReturnType) {
		t.Errorf("return type = %v, want a type parameter", gf.ReturnType)
	}

	if len(prog.GenericStructs) != 1 || prog.GenericStructs[0].Name != "Box" {
		t.Fatalf("GenericStructs = %+v, want one named Box", prog.GenericStructs)
	}

	// The templates themselves are not ordinary declarations.
	for _, fn := range prog.Functions {
		if fn.Name == "identity" {
			t.Error("the generic template was parsed as an ordinary function")
		}
	}
	for _, sd := range prog.Structs {
		if sd.Name == "Box" {
			t.Error("the generic template was parsed as an ordinary struct")
		}
	}
}

// Instantiating a generic struct from a type position produces a real struct
// definition, named after its type arguments, that joins the program.
func TestParseGenericStructInstantiation(t *testing.T) {
	prog := parse(t, `
	struct Box<T> {
		value: T
	}
	fn main(): int {
		let a: Box<int> = Box<int> { value: 1 }
		let b: Box<string> = Box<string> { value: "s" }
		let c: Box<int> = Box<int> { value: 2 }
		return 0
	}`)

	var names []string
	for _, sd := range prog.Structs {
		names = append(names, sd.Name)
	}
	joined := strings.Join(names, ",")
	for _, want := range []string{"Box_int", "Box_string"} {
		if !strings.Contains(joined, want) {
			t.Errorf("instantiated structs = %q, want to contain %q", joined, want)
		}
	}
	// Box<int> appears twice but is one type, not two.
	count := 0
	for _, n := range names {
		if n == "Box_int" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Box_int instantiated %d times, want 1", count)
	}
}

func TestMangleGenericName(t *testing.T) {
	tests := []struct {
		name string
		args []ast.Type
		want string
	}{
		{"Box", []ast.Type{ast.TypeInt}, "Box_int"},
		{"Box", []ast.Type{ast.TypeString}, "Box_string"},
		{"Pair", []ast.Type{ast.TypeString, ast.TypeInt}, "Pair_string_int"},
		{"max", []ast.Type{ast.TypeDouble}, "max_double"},
		{"all", []ast.Type{ast.ArrayTypeOf(ast.TypeInt)}, "all_intarr"},
	}
	for _, tt := range tests {
		if got := MangleGenericName(tt.name, tt.args); got != tt.want {
			t.Errorf("MangleGenericName(%q, %v) = %q, want %q", tt.name, tt.args, got, tt.want)
		}
	}
}

func TestParseGenericErrors(t *testing.T) {
	tests := []struct {
		name   string
		source string
		substr string
	}{
		{
			name:   "duplicate type parameter",
			source: `fn f<T, T>(a: T): T { return a }`,
			substr: "duplicate type parameter 'T'",
		},
		{
			name: "generic struct without type arguments",
			source: `struct Box<T> { value: T }
			fn main(): int {
				let b: Box = Box { value: 1 }
				return 0
			}`,
			substr: "needs type arguments",
		},
		{
			name: "wrong type argument count",
			source: `struct Pair<K, V> { key: K
				value: V }
			fn main(): int {
				let p: Pair<int> = Pair<int> { key: 1, value: 2 }
				return 0
			}`,
			substr: "takes 2 type argument(s), got 1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseError(t, tt.source)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.substr)
			}
			if !strings.Contains(err.Error(), tt.substr) {
				t.Fatalf("error = %q, want to contain %q", err.Error(), tt.substr)
			}
		})
	}
}
