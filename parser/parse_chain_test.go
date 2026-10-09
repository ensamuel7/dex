package parser

import (
	"testing"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/lexer"
)

func parseChainExpr(t *testing.T, src string) *ast.Program {
	t.Helper()
	tokens, err := lexer.New(src).Tokenize()
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	prog, errs := New(tokens).Parse()
	if len(errs) > 0 {
		t.Fatalf("parse %q: %v", src, errs)
	}
	return prog
}

// A call used to terminate an expression, so everything after it was left
// unconsumed and reported as a syntax error.
func TestParsesChainsOffCallResults(t *testing.T) {
	for _, src := range []string{
		`fn main(): void { let n: int = sb.toString().len() }`,
		`fn main(): void { let n: int = str.fromInt(1).trim().len() }`,
		`fn main(): void { let s: string = csv.split(",")[2] }`,
		`fn main(): void { let n: int = csv.split(",")[2].len() }`,
		`fn main(): void { let n: int = "a,b".split(",").len() }`,
		`fn main(): void { let n: int = build().field }`,
		`fn main(): void { let n: int = "  x ".trim().toUpper().len() }`,
	} {
		parseChainExpr(t, src)
	}
}

// The receiver of a chained call travels on CallExpr.Recv.
func TestChainedCallCarriesReceiver(t *testing.T) {
	prog := parseChainExpr(t, `fn main(): void { let n: int = sb.toString().len() }`)
	let0, ok := prog.Functions[0].Body[0].(*ast.LetStmt)
	if !ok {
		t.Fatalf("expected a let statement, got %T", prog.Functions[0].Body[0])
	}
	outer, ok := let0.Value.(*ast.CallExpr)
	if !ok {
		t.Fatalf("expected a call, got %T", let0.Value)
	}
	if outer.Name != "len" {
		t.Errorf("outer call = %q, want len", outer.Name)
	}
	if outer.Recv == nil {
		t.Fatal("outer call has no receiver")
	}
	inner, ok := outer.Recv.(*ast.CallExpr)
	if !ok {
		t.Fatalf("receiver = %T, want a call", outer.Recv)
	}
	if inner.Name != "toString" || inner.Module != "sb" {
		t.Errorf("inner call = %s.%s, want sb.toString", inner.Module, inner.Name)
	}
}
