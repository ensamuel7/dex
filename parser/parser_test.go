package parser

import (
	"strings"
	"testing"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/lexer"
)

// parse is a test helper that lexes and parses a full program.
func parse(t *testing.T, source string) *ast.Program {
	t.Helper()
	tokens, err := lexer.New(source).Tokenize()
	if err != nil {
		t.Fatalf("lexer error: %v", err)
	}
	prog, errs := New(tokens).Parse()
	if len(errs) > 0 {
		t.Fatalf("parser error: %v", errs[0])
	}
	return prog
}

// parseExpr is a helper that wraps an expression in a function and returns the ExprStmt.
func parseExpr(t *testing.T, expr string) ast.Expr {
	t.Helper()
	prog := parse(t, "fn test(): int { "+expr+" }")
	if len(prog.Functions) == 0 || len(prog.Functions[0].Body) == 0 {
		t.Fatal("no statements parsed")
	}
	es, ok := prog.Functions[0].Body[0].(*ast.ExprStmt)
	if !ok {
		t.Fatalf("expected ExprStmt, got %T", prog.Functions[0].Body[0])
	}
	return es.Expr
}

// parseStmt is a helper that wraps a statement in a function and returns it.
func parseStmt(t *testing.T, stmt string) ast.Stmt {
	t.Helper()
	prog := parse(t, "fn test(): int { "+stmt+" }")
	if len(prog.Functions) == 0 || len(prog.Functions[0].Body) == 0 {
		t.Fatal("no statements parsed")
	}
	return prog.Functions[0].Body[0]
}

// parseError is a helper that expects a parse error.
func parseError(t *testing.T, source string) error {
	t.Helper()
	tokens, err := lexer.New(source).Tokenize()
	if err != nil {
		return err
	}
	_, errs := New(tokens).Parse()
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// --- Literal tests ---

func TestIntLit(t *testing.T) {
	e := parseExpr(t, "42")
	lit, ok := e.(*ast.IntLit)
	if !ok {
		t.Fatalf("expected IntLit, got %T", e)
	}
	if lit.Value != 42 {
		t.Errorf("IntLit.Value = %d, want 42", lit.Value)
	}
}

func TestFloatLit(t *testing.T) {
	e := parseExpr(t, "3.14")
	lit, ok := e.(*ast.FloatLit)
	if !ok {
		t.Fatalf("expected FloatLit, got %T", e)
	}
	if lit.Value != 3.14 {
		t.Errorf("FloatLit.Value = %f, want 3.14", lit.Value)
	}
}

func TestBoolLitTrue(t *testing.T) {
	e := parseExpr(t, "true")
	lit, ok := e.(*ast.BoolLit)
	if !ok {
		t.Fatalf("expected BoolLit, got %T", e)
	}
	if lit.Value != true {
		t.Error("BoolLit.Value = false, want true")
	}
}

func TestBoolLitFalse(t *testing.T) {
	e := parseExpr(t, "false")
	lit, ok := e.(*ast.BoolLit)
	if !ok {
		t.Fatalf("expected BoolLit, got %T", e)
	}
	if lit.Value != false {
		t.Error("BoolLit.Value = true, want false")
	}
}

func TestStringLit(t *testing.T) {
	e := parseExpr(t, `"hello"`)
	lit, ok := e.(*ast.StringLit)
	if !ok {
		t.Fatalf("expected StringLit, got %T", e)
	}
	if lit.Value != "hello" {
		t.Errorf("StringLit.Value = %q, want %q", lit.Value, "hello")
	}
}

func TestArrayLitExpr(t *testing.T) {
	e := parseExpr(t, "[1, 2, 3]")
	arr, ok := e.(*ast.ArrayLitExpr)
	if !ok {
		t.Fatalf("expected ArrayLitExpr, got %T", e)
	}
	if len(arr.Elems) != 3 {
		t.Fatalf("ArrayLitExpr has %d elements, want 3", len(arr.Elems))
	}
	for i, want := range []int{1, 2, 3} {
		lit, ok := arr.Elems[i].(*ast.IntLit)
		if !ok {
			t.Errorf("element[%d] is %T, want IntLit", i, arr.Elems[i])
			continue
		}
		if lit.Value != want {
			t.Errorf("element[%d] = %d, want %d", i, lit.Value, want)
		}
	}
}

func TestEmptyArrayLit(t *testing.T) {
	e := parseExpr(t, "[]")
	arr, ok := e.(*ast.ArrayLitExpr)
	if !ok {
		t.Fatalf("expected ArrayLitExpr, got %T", e)
	}
	if len(arr.Elems) != 0 {
		t.Errorf("ArrayLitExpr has %d elements, want 0", len(arr.Elems))
	}
}

// --- Expression tests ---

func TestIdent(t *testing.T) {
	e := parseExpr(t, "x")
	id, ok := e.(*ast.Ident)
	if !ok {
		t.Fatalf("expected Ident, got %T", e)
	}
	if id.Name != "x" {
		t.Errorf("Ident.Name = %q, want %q", id.Name, "x")
	}
}

func TestBinaryExprAllOps(t *testing.T) {
	tests := []struct {
		source string
		op     ast.BinOp
	}{
		{"1 + 2", ast.BinAdd},
		{"1 - 2", ast.BinSub},
		{"1 * 2", ast.BinMul},
		{"1 / 2", ast.BinDiv},
		{"1 % 2", ast.BinMod},
		{"1 == 2", ast.BinEq},
		{"1 != 2", ast.BinNeq},
		{"1 === 2", ast.BinStrictEq},
		{"1 !== 2", ast.BinStrictNeq},
		{"1 < 2", ast.BinLt},
		{"1 > 2", ast.BinGt},
		{"1 <= 2", ast.BinLte},
		{"1 >= 2", ast.BinGte},
		{"true && false", ast.BinAnd},
		{"true || false", ast.BinOr},
	}
	for _, tt := range tests {
		e := parseExpr(t, tt.source)
		bin, ok := e.(*ast.BinaryExpr)
		if !ok {
			t.Errorf("parseExpr(%q) = %T, want BinaryExpr", tt.source, e)
			continue
		}
		if bin.Op != tt.op {
			t.Errorf("parseExpr(%q).Op = %d, want %d", tt.source, bin.Op, tt.op)
		}
	}
}

func TestUnaryExprNeg(t *testing.T) {
	e := parseExpr(t, "-42")
	un, ok := e.(*ast.UnaryExpr)
	if !ok {
		t.Fatalf("expected UnaryExpr, got %T", e)
	}
	if un.Op != ast.UnaryNeg {
		t.Errorf("Op = %d, want UnaryNeg", un.Op)
	}
	lit, ok := un.Operand.(*ast.IntLit)
	if !ok {
		t.Fatalf("Operand is %T, want IntLit", un.Operand)
	}
	if lit.Value != 42 {
		t.Errorf("Operand.Value = %d, want 42", lit.Value)
	}
}

func TestUnaryExprNot(t *testing.T) {
	e := parseExpr(t, "!true")
	un, ok := e.(*ast.UnaryExpr)
	if !ok {
		t.Fatalf("expected UnaryExpr, got %T", e)
	}
	if un.Op != ast.UnaryNot {
		t.Errorf("Op = %d, want UnaryNot", un.Op)
	}
}

func TestCallExpr(t *testing.T) {
	e := parseExpr(t, "foo(1, 2)")
	call, ok := e.(*ast.CallExpr)
	if !ok {
		t.Fatalf("expected CallExpr, got %T", e)
	}
	if call.Name != "foo" {
		t.Errorf("Name = %q, want %q", call.Name, "foo")
	}
	if call.Module != "" {
		t.Errorf("Module = %q, want empty", call.Module)
	}
	if len(call.Args) != 2 {
		t.Errorf("Args count = %d, want 2", len(call.Args))
	}
}

func TestCallExprNoArgs(t *testing.T) {
	e := parseExpr(t, "bar()")
	call, ok := e.(*ast.CallExpr)
	if !ok {
		t.Fatalf("expected CallExpr, got %T", e)
	}
	if call.Name != "bar" {
		t.Errorf("Name = %q, want %q", call.Name, "bar")
	}
	if len(call.Args) != 0 {
		t.Errorf("Args count = %d, want 0", len(call.Args))
	}
}

func TestModuleCallExpr(t *testing.T) {
	e := parseExpr(t, `fmt.print(42)`)
	call, ok := e.(*ast.CallExpr)
	if !ok {
		t.Fatalf("expected CallExpr, got %T", e)
	}
	if call.Module != "fmt" {
		t.Errorf("Module = %q, want %q", call.Module, "fmt")
	}
	if call.Name != "print" {
		t.Errorf("Name = %q, want %q", call.Name, "print")
	}
	if len(call.Args) != 1 {
		t.Errorf("Args count = %d, want 1", len(call.Args))
	}
}

func TestIndexExpr(t *testing.T) {
	e := parseExpr(t, "arr[0]")
	idx, ok := e.(*ast.IndexExpr)
	if !ok {
		t.Fatalf("expected IndexExpr, got %T", e)
	}
	arr, ok := idx.Array.(*ast.Ident)
	if !ok {
		t.Fatalf("Array is %T, want Ident", idx.Array)
	}
	if arr.Name != "arr" {
		t.Errorf("Array.Name = %q, want %q", arr.Name, "arr")
	}
	idxLit, ok := idx.Index.(*ast.IntLit)
	if !ok {
		t.Fatalf("Index is %T, want IntLit", idx.Index)
	}
	if idxLit.Value != 0 {
		t.Errorf("Index.Value = %d, want 0", idxLit.Value)
	}
}

func TestParenthesizedExpr(t *testing.T) {
	e := parseExpr(t, "(1 + 2)")
	bin, ok := e.(*ast.BinaryExpr)
	if !ok {
		t.Fatalf("expected BinaryExpr, got %T", e)
	}
	if bin.Op != ast.BinAdd {
		t.Errorf("Op = %d, want BinAdd", bin.Op)
	}
}

// --- Operator precedence tests ---

func TestMulOverAdd(t *testing.T) {
	// 1 + 2 * 3 => BinAdd(1, BinMul(2, 3))
	e := parseExpr(t, "1 + 2 * 3")
	bin, ok := e.(*ast.BinaryExpr)
	if !ok {
		t.Fatalf("expected BinaryExpr, got %T", e)
	}
	if bin.Op != ast.BinAdd {
		t.Errorf("top Op = %d, want BinAdd", bin.Op)
	}
	right, ok := bin.Right.(*ast.BinaryExpr)
	if !ok {
		t.Fatalf("Right is %T, want BinaryExpr", bin.Right)
	}
	if right.Op != ast.BinMul {
		t.Errorf("right Op = %d, want BinMul", right.Op)
	}
}

func TestAndOverOr(t *testing.T) {
	// true || false && true => BinOr(true, BinAnd(false, true))
	e := parseExpr(t, "true || false && true")
	bin, ok := e.(*ast.BinaryExpr)
	if !ok {
		t.Fatalf("expected BinaryExpr, got %T", e)
	}
	if bin.Op != ast.BinOr {
		t.Errorf("top Op = %d, want BinOr", bin.Op)
	}
	right, ok := bin.Right.(*ast.BinaryExpr)
	if !ok {
		t.Fatalf("Right is %T, want BinaryExpr", bin.Right)
	}
	if right.Op != ast.BinAnd {
		t.Errorf("right Op = %d, want BinAnd", right.Op)
	}
}

func TestComparisonOverEquality(t *testing.T) {
	// 1 == 2 < 3 => BinEq(1, BinLt(2, 3))
	e := parseExpr(t, "1 == 2 < 3")
	bin, ok := e.(*ast.BinaryExpr)
	if !ok {
		t.Fatalf("expected BinaryExpr, got %T", e)
	}
	if bin.Op != ast.BinEq {
		t.Errorf("top Op = %d, want BinEq", bin.Op)
	}
	right, ok := bin.Right.(*ast.BinaryExpr)
	if !ok {
		t.Fatalf("Right is %T, want BinaryExpr", bin.Right)
	}
	if right.Op != ast.BinLt {
		t.Errorf("right Op = %d, want BinLt", right.Op)
	}
}

func TestParensOverridePrecedence(t *testing.T) {
	// (1 + 2) * 3 => BinMul(BinAdd(1, 2), 3)
	e := parseExpr(t, "(1 + 2) * 3")
	bin, ok := e.(*ast.BinaryExpr)
	if !ok {
		t.Fatalf("expected BinaryExpr, got %T", e)
	}
	if bin.Op != ast.BinMul {
		t.Errorf("top Op = %d, want BinMul", bin.Op)
	}
	left, ok := bin.Left.(*ast.BinaryExpr)
	if !ok {
		t.Fatalf("Left is %T, want BinaryExpr", bin.Left)
	}
	if left.Op != ast.BinAdd {
		t.Errorf("left Op = %d, want BinAdd", left.Op)
	}
}

// --- Statement tests ---

func TestLetStmt(t *testing.T) {
	tests := []struct {
		source   string
		name     string
		wantType ast.Type
	}{
		{"let x: int = 42", "x", ast.TypeInt},
		{"let b: bool = true", "b", ast.TypeBool},
		{"let s: string = \"hi\"", "s", ast.TypeString},
		{"let l: long = 100", "l", ast.TypeLong},
		{"let d: double = 3.14", "d", ast.TypeDouble},
		{"let a: int[] = [1, 2]", "a", ast.TypeArrayInt},
		{"let a: string[] = []", "a", ast.TypeArrayString},
	}
	for _, tt := range tests {
		s := parseStmt(t, tt.source)
		ls, ok := s.(*ast.LetStmt)
		if !ok {
			t.Errorf("parseStmt(%q) = %T, want LetStmt", tt.source, s)
			continue
		}
		if ls.Name != tt.name {
			t.Errorf("LetStmt.Name = %q, want %q", ls.Name, tt.name)
		}
		if ls.Type != tt.wantType {
			t.Errorf("LetStmt.Type = %d, want %d", ls.Type, tt.wantType)
		}
	}
}

func TestReturnStmt(t *testing.T) {
	s := parseStmt(t, "return 42")
	rs, ok := s.(*ast.ReturnStmt)
	if !ok {
		t.Fatalf("expected ReturnStmt, got %T", s)
	}
	lit, ok := rs.Value.(*ast.IntLit)
	if !ok {
		t.Fatalf("Value is %T, want IntLit", rs.Value)
	}
	if lit.Value != 42 {
		t.Errorf("Value = %d, want 42", lit.Value)
	}
}

func TestIfStmt(t *testing.T) {
	s := parseStmt(t, "if (true) { return 1 }")
	is, ok := s.(*ast.IfStmt)
	if !ok {
		t.Fatalf("expected IfStmt, got %T", s)
	}
	if _, ok := is.Cond.(*ast.BoolLit); !ok {
		t.Errorf("Cond is %T, want BoolLit", is.Cond)
	}
	if len(is.Then) != 1 {
		t.Errorf("Then has %d stmts, want 1", len(is.Then))
	}
	if is.Else != nil {
		t.Errorf("Else should be nil")
	}
}

func TestIfElseStmt(t *testing.T) {
	s := parseStmt(t, "if (true) { return 1 } else { return 2 }")
	is, ok := s.(*ast.IfStmt)
	if !ok {
		t.Fatalf("expected IfStmt, got %T", s)
	}
	if len(is.Then) != 1 {
		t.Errorf("Then has %d stmts, want 1", len(is.Then))
	}
	if len(is.Else) != 1 {
		t.Errorf("Else has %d stmts, want 1", len(is.Else))
	}
}

func TestElseIfStmt(t *testing.T) {
	s := parseStmt(t, "if (true) { return 1 } else if (false) { return 2 }")
	is, ok := s.(*ast.IfStmt)
	if !ok {
		t.Fatalf("expected IfStmt, got %T", s)
	}
	if len(is.Else) != 1 {
		t.Errorf("Else has %d stmts, want 1 (the nested IfStmt)", len(is.Else))
	}
	nested, ok := is.Else[0].(*ast.IfStmt)
	if !ok {
		t.Fatalf("Else[0] is %T, want IfStmt", is.Else[0])
	}
	if _, ok := nested.Cond.(*ast.BoolLit); !ok {
		t.Errorf("nested Cond is %T, want BoolLit", nested.Cond)
	}
}

func TestWhileStmt(t *testing.T) {
	s := parseStmt(t, "while (true) { return 1 }")
	ws, ok := s.(*ast.WhileStmt)
	if !ok {
		t.Fatalf("expected WhileStmt, got %T", s)
	}
	if _, ok := ws.Cond.(*ast.BoolLit); !ok {
		t.Errorf("Cond is %T, want BoolLit", ws.Cond)
	}
	if len(ws.Body) != 1 {
		t.Errorf("Body has %d stmts, want 1", len(ws.Body))
	}
}

func TestAssignStmt(t *testing.T) {
	prog := parse(t, `fn test(): int {
		let x: int = 1
		x = 2
	}`)
	body := prog.Functions[0].Body
	if len(body) < 2 {
		t.Fatalf("expected 2 stmts, got %d", len(body))
	}
	as, ok := body[1].(*ast.AssignStmt)
	if !ok {
		t.Fatalf("body[1] is %T, want AssignStmt", body[1])
	}
	if as.Name != "x" {
		t.Errorf("Name = %q, want %q", as.Name, "x")
	}
}

func TestIndexAssignStmt(t *testing.T) {
	prog := parse(t, `fn test(): int {
		let a: int[] = [1, 2]
		a[0] = 10
	}`)
	body := prog.Functions[0].Body
	if len(body) < 2 {
		t.Fatalf("expected 2 stmts, got %d", len(body))
	}
	ias, ok := body[1].(*ast.IndexAssignStmt)
	if !ok {
		t.Fatalf("body[1] is %T, want IndexAssignStmt", body[1])
	}
	arr, ok := ias.Array.(*ast.Ident)
	if !ok {
		t.Fatalf("Array is %T, want Ident", ias.Array)
	}
	if arr.Name != "a" {
		t.Errorf("Array.Name = %q, want %q", arr.Name, "a")
	}
}

func TestBlockStmt(t *testing.T) {
	s := parseStmt(t, "{ return 1 }")
	bs, ok := s.(*ast.BlockStmt)
	if !ok {
		t.Fatalf("expected BlockStmt, got %T", s)
	}
	if len(bs.Stmts) != 1 {
		t.Errorf("Stmts has %d items, want 1", len(bs.Stmts))
	}
}

func TestExprStmt(t *testing.T) {
	e := parseStmt(t, "foo()")
	es, ok := e.(*ast.ExprStmt)
	if !ok {
		t.Fatalf("expected ExprStmt, got %T", e)
	}
	call, ok := es.Expr.(*ast.CallExpr)
	if !ok {
		t.Fatalf("Expr is %T, want CallExpr", es.Expr)
	}
	if call.Name != "foo" {
		t.Errorf("Name = %q, want %q", call.Name, "foo")
	}
}

// --- Function tests ---

func TestFunctionNoParams(t *testing.T) {
	prog := parse(t, "fn main(): void { }")
	if len(prog.Functions) != 1 {
		t.Fatalf("expected 1 function, got %d", len(prog.Functions))
	}
	fn := prog.Functions[0]
	if fn.Name != "main" {
		t.Errorf("Name = %q, want %q", fn.Name, "main")
	}
	if len(fn.Params) != 0 {
		t.Errorf("Params count = %d, want 0", len(fn.Params))
	}
	if fn.ReturnType != ast.TypeVoid {
		t.Errorf("ReturnType = %d, want TypeVoid", fn.ReturnType)
	}
}

func TestFunctionWithParams(t *testing.T) {
	prog := parse(t, "fn add(a: int, b: int): int { return a }")
	fn := prog.Functions[0]
	if fn.Name != "add" {
		t.Errorf("Name = %q, want %q", fn.Name, "add")
	}
	if len(fn.Params) != 2 {
		t.Fatalf("Params count = %d, want 2", len(fn.Params))
	}
	if fn.Params[0].Name != "a" || fn.Params[0].Type != ast.TypeInt {
		t.Errorf("Param[0] = {%q, %d}, want {a, TypeInt}", fn.Params[0].Name, fn.Params[0].Type)
	}
	if fn.Params[1].Name != "b" || fn.Params[1].Type != ast.TypeInt {
		t.Errorf("Param[1] = {%q, %d}, want {b, TypeInt}", fn.Params[1].Name, fn.Params[1].Type)
	}
}

func TestFunctionReturnTypes(t *testing.T) {
	tests := []struct {
		source     string
		returnType ast.Type
	}{
		{"fn f(): int { return 0 }", ast.TypeInt},
		{"fn f(): bool { return true }", ast.TypeBool},
		{"fn f(): string { return \"hi\" }", ast.TypeString},
		{"fn f(): long { return 0 }", ast.TypeLong},
		{"fn f(): double { return 0.0 }", ast.TypeDouble},
		{"fn f(): int[] { return [1] }", ast.TypeArrayInt},
	}
	for _, tt := range tests {
		prog := parse(t, tt.source)
		if prog.Functions[0].ReturnType != tt.returnType {
			t.Errorf("parse(%q) ReturnType = %d, want %d", tt.source, prog.Functions[0].ReturnType, tt.returnType)
		}
	}
}

func TestFunctionKeyword(t *testing.T) {
	prog := parse(t, "function main(): void { }")
	if len(prog.Functions) != 1 {
		t.Fatalf("expected 1 function, got %d", len(prog.Functions))
	}
	if prog.Functions[0].Name != "main" {
		t.Errorf("Name = %q, want %q", prog.Functions[0].Name, "main")
	}
}

// --- Import tests ---

func TestSingleImport(t *testing.T) {
	prog := parse(t, `import "fmt" fn main(): void { }`)
	if len(prog.Imports) != 1 {
		t.Fatalf("expected 1 import, got %d", len(prog.Imports))
	}
	if prog.Imports[0].Path != "fmt" {
		t.Errorf("Import.Path = %q, want %q", prog.Imports[0].Path, "fmt")
	}
}

func TestMultipleImports(t *testing.T) {
	prog := parse(t, `import "fmt" import "json" fn main(): void { }`)
	if len(prog.Imports) != 2 {
		t.Fatalf("expected 2 imports, got %d", len(prog.Imports))
	}
	if prog.Imports[0].Path != "fmt" {
		t.Errorf("Import[0].Path = %q, want %q", prog.Imports[0].Path, "fmt")
	}
	if prog.Imports[1].Path != "json" {
		t.Errorf("Import[1].Path = %q, want %q", prog.Imports[1].Path, "json")
	}
}

// --- Full program test ---

func TestFullProgram(t *testing.T) {
	source := `
		fn add(a: int, b: int): int {
			return a + b
		}
		fn main(): void {
			let x: int = add(1, 2)
		}
	`
	prog := parse(t, source)
	if len(prog.Functions) != 2 {
		t.Fatalf("expected 2 functions, got %d", len(prog.Functions))
	}
	if prog.Functions[0].Name != "add" {
		t.Errorf("fn[0].Name = %q, want %q", prog.Functions[0].Name, "add")
	}
	if prog.Functions[1].Name != "main" {
		t.Errorf("fn[1].Name = %q, want %q", prog.Functions[1].Name, "main")
	}
}

// --- Error cases ---

func TestErrorMissingBrace(t *testing.T) {
	err := parseError(t, "fn main(): void {")
	if err == nil {
		t.Fatal("expected error for missing closing brace")
	}
}

func TestErrorMissingType(t *testing.T) {
	err := parseError(t, "fn main(): { return 0 }")
	if err == nil {
		t.Fatal("expected error for missing return type")
	}
}

func TestErrorUnexpectedToken(t *testing.T) {
	err := parseError(t, "fn main(): void { @ }")
	if err == nil {
		t.Fatal("expected error for unexpected token")
	}
}

func TestErrorMissingFnKeyword(t *testing.T) {
	err := parseError(t, "main(): void { }")
	if err == nil {
		t.Fatal("expected error for missing fn keyword")
	}
	if !strings.Contains(err.Error(), "expected 'fn'") {
		t.Errorf("error = %q, want to contain 'expected 'fn''", err.Error())
	}
}

// --- Break and Continue ---

func TestBreakStmt(t *testing.T) {
	s := parseStmt(t, "break")
	_, ok := s.(*ast.BreakStmt)
	if !ok {
		t.Fatalf("expected BreakStmt, got %T", s)
	}
}

func TestContinueStmt(t *testing.T) {
	s := parseStmt(t, "continue")
	_, ok := s.(*ast.ContinueStmt)
	if !ok {
		t.Fatalf("expected ContinueStmt, got %T", s)
	}
}

// --- For loop ---

func TestForStmt(t *testing.T) {
	prog := parse(t, `fn main(): void {
		for(let i: int = 0; i < 10; i++) { }
	}`)
	body := prog.Functions[0].Body
	if len(body) != 1 {
		t.Fatalf("expected 1 stmt, got %d", len(body))
	}
	fs, ok := body[0].(*ast.ForStmt)
	if !ok {
		t.Fatalf("expected ForStmt, got %T", body[0])
	}
	// Check init is LetStmt
	init, ok := fs.Init.(*ast.LetStmt)
	if !ok {
		t.Fatalf("Init is %T, want LetStmt", fs.Init)
	}
	if init.Name != "i" {
		t.Errorf("Init.Name = %q, want %q", init.Name, "i")
	}
	// Check post is IncrementStmt
	post, ok := fs.Post.(*ast.IncrementStmt)
	if !ok {
		t.Fatalf("Post is %T, want IncrementStmt", fs.Post)
	}
	if post.Name != "i" {
		t.Errorf("Post.Name = %q, want %q", post.Name, "i")
	}
}

func TestForStmtWithCompoundAssign(t *testing.T) {
	prog := parse(t, `fn main(): void {
		for(let i: int = 0; i < 100; i += 2) { }
	}`)
	body := prog.Functions[0].Body
	fs, ok := body[0].(*ast.ForStmt)
	if !ok {
		t.Fatalf("expected ForStmt, got %T", body[0])
	}
	post, ok := fs.Post.(*ast.CompoundAssignStmt)
	if !ok {
		t.Fatalf("Post is %T, want CompoundAssignStmt", fs.Post)
	}
	if post.Name != "i" || post.Op != ast.BinAdd {
		t.Errorf("Post = {%q, %d}, want {i, BinAdd}", post.Name, post.Op)
	}
}

// --- Foreach ---

func TestForeachStmtValueOnly(t *testing.T) {
	prog := parse(t, `fn main(): void {
		let nums: int[] = [1, 2, 3]
		foreach(nums as n) { }
	}`)
	body := prog.Functions[0].Body
	fe, ok := body[1].(*ast.ForeachStmt)
	if !ok {
		t.Fatalf("expected ForeachStmt, got %T", body[1])
	}
	if fe.ValueVar != "n" {
		t.Errorf("ValueVar = %q, want %q", fe.ValueVar, "n")
	}
	if fe.IndexVar != "" {
		t.Errorf("IndexVar = %q, want empty", fe.IndexVar)
	}
}

func TestForeachStmtWithIndex(t *testing.T) {
	prog := parse(t, `fn main(): void {
		let nums: int[] = [1, 2, 3]
		foreach(nums as i, n) { }
	}`)
	body := prog.Functions[0].Body
	fe, ok := body[1].(*ast.ForeachStmt)
	if !ok {
		t.Fatalf("expected ForeachStmt, got %T", body[1])
	}
	if fe.IndexVar != "i" {
		t.Errorf("IndexVar = %q, want %q", fe.IndexVar, "i")
	}
	if fe.ValueVar != "n" {
		t.Errorf("ValueVar = %q, want %q", fe.ValueVar, "n")
	}
}

// --- Type inference ---

func TestLetStmtInferred(t *testing.T) {
	s := parseStmt(t, `let x = 42`)
	ls, ok := s.(*ast.LetStmt)
	if !ok {
		t.Fatalf("expected LetStmt, got %T", s)
	}
	if ls.Name != "x" {
		t.Errorf("Name = %q, want %q", ls.Name, "x")
	}
	if ls.Type != ast.TypeInferred {
		t.Errorf("Type = %d, want TypeInferred (%d)", ls.Type, ast.TypeInferred)
	}
}

// --- Increment/Decrement/CompoundAssign ---

func TestIncrementStmt(t *testing.T) {
	prog := parse(t, `fn main(): void {
		let x: int = 0
		x++
	}`)
	body := prog.Functions[0].Body
	inc, ok := body[1].(*ast.IncrementStmt)
	if !ok {
		t.Fatalf("expected IncrementStmt, got %T", body[1])
	}
	if inc.Name != "x" {
		t.Errorf("Name = %q, want %q", inc.Name, "x")
	}
}

func TestDecrementStmt(t *testing.T) {
	prog := parse(t, `fn main(): void {
		let x: int = 0
		x--
	}`)
	body := prog.Functions[0].Body
	dec, ok := body[1].(*ast.DecrementStmt)
	if !ok {
		t.Fatalf("expected DecrementStmt, got %T", body[1])
	}
	if dec.Name != "x" {
		t.Errorf("Name = %q, want %q", dec.Name, "x")
	}
}

func TestCompoundAssignStmt(t *testing.T) {
	prog := parse(t, `fn main(): void {
		let x: int = 0
		x += 5
	}`)
	body := prog.Functions[0].Body
	ca, ok := body[1].(*ast.CompoundAssignStmt)
	if !ok {
		t.Fatalf("expected CompoundAssignStmt, got %T", body[1])
	}
	if ca.Name != "x" || ca.Op != ast.BinAdd {
		t.Errorf("CompoundAssign = {%q, %d}, want {x, BinAdd}", ca.Name, ca.Op)
	}
}

func TestCompoundAssignSubStmt(t *testing.T) {
	prog := parse(t, `fn main(): void {
		let x: int = 10
		x -= 3
	}`)
	body := prog.Functions[0].Body
	ca, ok := body[1].(*ast.CompoundAssignStmt)
	if !ok {
		t.Fatalf("expected CompoundAssignStmt, got %T", body[1])
	}
	if ca.Name != "x" || ca.Op != ast.BinSub {
		t.Errorf("CompoundAssign = {%q, %d}, want {x, BinSub}", ca.Name, ca.Op)
	}
}

// --- Optional semicolons ---

func TestOptionalSemicolonStatements(t *testing.T) {
	prog := parse(t, `fn main(): void {
		let x: int = 5;
		let y: int = 10;
		x = 20;
		return;
	}`)
	body := prog.Functions[0].Body
	if len(body) != 4 {
		t.Fatalf("expected 4 stmts, got %d", len(body))
	}
	ls, ok := body[0].(*ast.LetStmt)
	if !ok {
		t.Fatalf("body[0] is %T, want LetStmt", body[0])
	}
	if ls.Name != "x" {
		t.Errorf("body[0].Name = %q, want %q", ls.Name, "x")
	}
	ls2, ok := body[1].(*ast.LetStmt)
	if !ok {
		t.Fatalf("body[1] is %T, want LetStmt", body[1])
	}
	if ls2.Name != "y" {
		t.Errorf("body[1].Name = %q, want %q", ls2.Name, "y")
	}
}

func TestMixedSemicolonAndNoSemicolon(t *testing.T) {
	prog := parse(t, `fn main(): void {
		let x: int = 5;
		let y: int = 10
		x = 20;
		y = 30
	}`)
	body := prog.Functions[0].Body
	if len(body) != 4 {
		t.Fatalf("expected 4 stmts, got %d", len(body))
	}
}

func TestSemicolonAfterImport(t *testing.T) {
	prog := parse(t, `import "fmt"; import "json"; fn main(): void { }`)
	if len(prog.Imports) != 2 {
		t.Fatalf("expected 2 imports, got %d", len(prog.Imports))
	}
	if prog.Imports[0].Path != "fmt" {
		t.Errorf("Import[0].Path = %q, want %q", prog.Imports[0].Path, "fmt")
	}
	if prog.Imports[1].Path != "json" {
		t.Errorf("Import[1].Path = %q, want %q", prog.Imports[1].Path, "json")
	}
}

// --- Chained field/method access ---

func TestChainedFieldMethodCall(t *testing.T) {
	// req.params.get("id") should parse as CallExpr with Module="req.params", Name="get"
	e := parseExpr(t, `req.params.get("id")`)
	call, ok := e.(*ast.CallExpr)
	if !ok {
		t.Fatalf("expected CallExpr, got %T", e)
	}
	if call.Module != "req.params" {
		t.Errorf("Module = %q, want %q", call.Module, "req.params")
	}
	if call.Name != "get" {
		t.Errorf("Name = %q, want %q", call.Name, "get")
	}
	if len(call.Args) != 1 {
		t.Errorf("Args count = %d, want 1", len(call.Args))
	}
}

func TestChainedFieldMethodCallNoArgs(t *testing.T) {
	// obj.field.len() should parse as CallExpr with Module="obj.field", Name="len"
	e := parseExpr(t, `obj.field.len()`)
	call, ok := e.(*ast.CallExpr)
	if !ok {
		t.Fatalf("expected CallExpr, got %T", e)
	}
	if call.Module != "obj.field" {
		t.Errorf("Module = %q, want %q", call.Module, "obj.field")
	}
	if call.Name != "len" {
		t.Errorf("Name = %q, want %q", call.Name, "len")
	}
	if len(call.Args) != 0 {
		t.Errorf("Args count = %d, want 0", len(call.Args))
	}
}

func TestChainedFieldMethodCallMultipleArgs(t *testing.T) {
	// req.params.set("key", "value") should parse as CallExpr with Module="req.params"
	e := parseExpr(t, `req.params.set("key", "value")`)
	call, ok := e.(*ast.CallExpr)
	if !ok {
		t.Fatalf("expected CallExpr, got %T", e)
	}
	if call.Module != "req.params" {
		t.Errorf("Module = %q, want %q", call.Module, "req.params")
	}
	if call.Name != "set" {
		t.Errorf("Name = %q, want %q", call.Name, "set")
	}
	if len(call.Args) != 2 {
		t.Errorf("Args count = %d, want 2", len(call.Args))
	}
}

func TestDeepChainedMethodCall(t *testing.T) {
	// a.b.c.method() should parse as CallExpr with Module="a.b.c", Name="method"
	e := parseExpr(t, `a.b.c.method()`)
	call, ok := e.(*ast.CallExpr)
	if !ok {
		t.Fatalf("expected CallExpr, got %T", e)
	}
	if call.Module != "a.b.c" {
		t.Errorf("Module = %q, want %q", call.Module, "a.b.c")
	}
	if call.Name != "method" {
		t.Errorf("Name = %q, want %q", call.Name, "method")
	}
}

func TestFieldAccessNoChain(t *testing.T) {
	// req.path should still parse as FieldAccessExpr (no chaining)
	e := parseExpr(t, `req.path`)
	fa, ok := e.(*ast.FieldAccessExpr)
	if !ok {
		t.Fatalf("expected FieldAccessExpr, got %T", e)
	}
	ident, ok := fa.Object.(*ast.Ident)
	if !ok {
		t.Fatalf("Object is %T, want Ident", fa.Object)
	}
	if ident.Name != "req" {
		t.Errorf("Object.Name = %q, want %q", ident.Name, "req")
	}
	if fa.Field != "path" {
		t.Errorf("Field = %q, want %q", fa.Field, "path")
	}
}

func TestModuleCallStillWorks(t *testing.T) {
	// fmt.println(42) should still parse as CallExpr with Module="fmt" (not chained)
	e := parseExpr(t, `fmt.println(42)`)
	call, ok := e.(*ast.CallExpr)
	if !ok {
		t.Fatalf("expected CallExpr, got %T", e)
	}
	if call.Module != "fmt" {
		t.Errorf("Module = %q, want %q", call.Module, "fmt")
	}
	if call.Name != "println" {
		t.Errorf("Name = %q, want %q", call.Name, "println")
	}
}

// --- Struct definition tests ---

func TestParseStructDefBasic(t *testing.T) {
	prog := parse(t, `
		struct Point {
			x: int
			y: int
		}
		fn main(): void { }
	`)
	if len(prog.Structs) != 1 {
		t.Fatalf("expected 1 struct, got %d", len(prog.Structs))
	}
	sd := prog.Structs[0]
	if sd.Name != "Point" {
		t.Errorf("StructDef.Name = %q, want %q", sd.Name, "Point")
	}
	if len(sd.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(sd.Fields))
	}
	if sd.Fields[0].Name != "x" || sd.Fields[0].Type != ast.TypeInt {
		t.Errorf("Field[0] = {%q, %d}, want {x, TypeInt}", sd.Fields[0].Name, sd.Fields[0].Type)
	}
	if sd.Fields[1].Name != "y" || sd.Fields[1].Type != ast.TypeInt {
		t.Errorf("Field[1] = {%q, %d}, want {y, TypeInt}", sd.Fields[1].Name, sd.Fields[1].Type)
	}
}

func TestParseStructDefWithConstructor(t *testing.T) {
	prog := parse(t, `
		struct Point(x: int, y: int) {
		}
		fn main(): void { }
	`)
	if len(prog.Structs) != 1 {
		t.Fatalf("expected 1 struct, got %d", len(prog.Structs))
	}
	sd := prog.Structs[0]
	if sd.Name != "Point" {
		t.Errorf("StructDef.Name = %q, want %q", sd.Name, "Point")
	}
	if len(sd.ConstructorParams) != 2 {
		t.Fatalf("expected 2 constructor params, got %d", len(sd.ConstructorParams))
	}
	if sd.ConstructorParams[0].Name != "x" {
		t.Errorf("ConstructorParams[0].Name = %q, want %q", sd.ConstructorParams[0].Name, "x")
	}
	if sd.ConstructorParams[1].Name != "y" {
		t.Errorf("ConstructorParams[1].Name = %q, want %q", sd.ConstructorParams[1].Name, "y")
	}
	// Constructor params become fields
	if len(sd.Fields) != 2 {
		t.Fatalf("expected 2 fields (from constructor params), got %d", len(sd.Fields))
	}
}

func TestParseStructDefWithMethods(t *testing.T) {
	prog := parse(t, `
		struct Counter {
			value: int
			fn increment(): void { }
			fn getValue(): int { return 0 }
		}
		fn main(): void { }
	`)
	if len(prog.Structs) != 1 {
		t.Fatalf("expected 1 struct, got %d", len(prog.Structs))
	}
	sd := prog.Structs[0]
	if len(sd.Fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(sd.Fields))
	}
	if sd.Fields[0].Name != "value" {
		t.Errorf("Field[0].Name = %q, want %q", sd.Fields[0].Name, "value")
	}
	if len(sd.Methods) != 2 {
		t.Fatalf("expected 2 methods, got %d", len(sd.Methods))
	}
	if sd.Methods[0].Name != "increment" {
		t.Errorf("Method[0].Name = %q, want %q", sd.Methods[0].Name, "increment")
	}
	if sd.Methods[1].Name != "getValue" {
		t.Errorf("Method[1].Name = %q, want %q", sd.Methods[1].Name, "getValue")
	}
}

func TestParseStructDefWithAccessModifiers(t *testing.T) {
	prog := parse(t, `
		public struct User {
			public name: string
			private age: int
		}
		fn main(): void { }
	`)
	if len(prog.Structs) != 1 {
		t.Fatalf("expected 1 struct, got %d", len(prog.Structs))
	}
	sd := prog.Structs[0]
	if sd.Name != "User" {
		t.Errorf("StructDef.Name = %q, want %q", sd.Name, "User")
	}
	if len(sd.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(sd.Fields))
	}
	if sd.Fields[0].IsPrivate {
		t.Errorf("Field[0] (name) should not be private")
	}
	if !sd.Fields[1].IsPrivate {
		t.Errorf("Field[1] (age) should be private")
	}
}

func TestParseStructDefWithPrivateMethod(t *testing.T) {
	prog := parse(t, `
		struct Service {
			data: int
			private fn helper(): void { }
			fn publicMethod(): void { }
		}
		fn main(): void { }
	`)
	sd := prog.Structs[0]
	if len(sd.Methods) != 2 {
		t.Fatalf("expected 2 methods, got %d", len(sd.Methods))
	}
	if !sd.Methods[0].IsPrivate {
		t.Errorf("Method[0] (helper) should be private")
	}
	if sd.Methods[1].IsPrivate {
		t.Errorf("Method[1] (publicMethod) should not be private")
	}
}

func TestParseStructLiteral(t *testing.T) {
	// Struct literal requires the struct to be defined in the same source
	prog := parse(t, `
		struct Point {
			x: int
			y: int
		}
		fn main(): void {
			let p = Point { x: 1, y: 2 }
		}
	`)
	body := prog.Functions[0].Body
	if len(body) != 1 {
		t.Fatalf("expected 1 stmt, got %d", len(body))
	}
	ls, ok := body[0].(*ast.LetStmt)
	if !ok {
		t.Fatalf("expected LetStmt, got %T", body[0])
	}
	sl, ok := ls.Value.(*ast.StructLitExpr)
	if !ok {
		t.Fatalf("expected StructLitExpr, got %T", ls.Value)
	}
	if sl.Name != "Point" {
		t.Errorf("StructLitExpr.Name = %q, want %q", sl.Name, "Point")
	}
	if len(sl.FieldNames) != 2 {
		t.Fatalf("expected 2 field names, got %d", len(sl.FieldNames))
	}
	if sl.FieldNames[0] != "x" || sl.FieldNames[1] != "y" {
		t.Errorf("FieldNames = %v, want [x, y]", sl.FieldNames)
	}
}

// --- Enum definition tests ---

func TestParseEnumDef(t *testing.T) {
	prog := parse(t, `
		enum Color {
			Red
			Green
			Blue
		}
		fn main(): void { }
	`)
	if len(prog.Enums) != 1 {
		t.Fatalf("expected 1 enum, got %d", len(prog.Enums))
	}
	ed := prog.Enums[0]
	if ed.Name != "Color" {
		t.Errorf("EnumDef.Name = %q, want %q", ed.Name, "Color")
	}
	if len(ed.Variants) != 3 {
		t.Fatalf("expected 3 variants, got %d", len(ed.Variants))
	}
	want := []string{"Red", "Green", "Blue"}
	for i, v := range want {
		if ed.Variants[i] != v {
			t.Errorf("Variant[%d] = %q, want %q", i, ed.Variants[i], v)
		}
	}
}

func TestParseEnumDefMultiple(t *testing.T) {
	prog := parse(t, `
		enum Direction {
			North
			South
			East
			West
		}
		enum Status {
			Active
			Inactive
		}
		fn main(): void { }
	`)
	if len(prog.Enums) != 2 {
		t.Fatalf("expected 2 enums, got %d", len(prog.Enums))
	}
	if prog.Enums[0].Name != "Direction" {
		t.Errorf("Enum[0].Name = %q, want %q", prog.Enums[0].Name, "Direction")
	}
	if prog.Enums[1].Name != "Status" {
		t.Errorf("Enum[1].Name = %q, want %q", prog.Enums[1].Name, "Status")
	}
	if len(prog.Enums[0].Variants) != 4 {
		t.Errorf("Direction has %d variants, want 4", len(prog.Enums[0].Variants))
	}
	if len(prog.Enums[1].Variants) != 2 {
		t.Errorf("Status has %d variants, want 2", len(prog.Enums[1].Variants))
	}
}

func TestParseEnumAccess(t *testing.T) {
	prog := parse(t, `
		enum Color {
			Red
			Green
			Blue
		}
		fn main(): void {
			let c = Color.Red
		}
	`)
	body := prog.Functions[0].Body
	if len(body) != 1 {
		t.Fatalf("expected 1 stmt, got %d", len(body))
	}
	ls, ok := body[0].(*ast.LetStmt)
	if !ok {
		t.Fatalf("expected LetStmt, got %T", body[0])
	}
	ea, ok := ls.Value.(*ast.EnumAccessExpr)
	if !ok {
		t.Fatalf("expected EnumAccessExpr, got %T", ls.Value)
	}
	if ea.EnumName != "Color" {
		t.Errorf("EnumAccessExpr.EnumName = %q, want %q", ea.EnumName, "Color")
	}
	if ea.Variant != "Red" {
		t.Errorf("EnumAccessExpr.Variant = %q, want %q", ea.Variant, "Red")
	}
}

func TestParseEnumEmptyError(t *testing.T) {
	err := parseError(t, `
		enum Empty {
		}
		fn main(): void { }
	`)
	if err == nil {
		t.Fatal("expected error for empty enum")
	}
	if !strings.Contains(err.Error(), "must have at least one variant") {
		t.Errorf("error = %q, want to contain 'must have at least one variant'", err.Error())
	}
}

// --- Interface definition tests ---

func TestParseInterfaceDef(t *testing.T) {
	prog := parse(t, `
		interface Printable {
			fn toString(): string
		}
		fn main(): void { }
	`)
	if len(prog.Interfaces) != 1 {
		t.Fatalf("expected 1 interface, got %d", len(prog.Interfaces))
	}
	iface := prog.Interfaces[0]
	if iface.Name != "Printable" {
		t.Errorf("InterfaceDef.Name = %q, want %q", iface.Name, "Printable")
	}
	if len(iface.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(iface.Methods))
	}
	if iface.Methods[0].Name != "toString" {
		t.Errorf("Method[0].Name = %q, want %q", iface.Methods[0].Name, "toString")
	}
	if iface.Methods[0].ReturnType != ast.TypeString {
		t.Errorf("Method[0].ReturnType = %d, want TypeString", iface.Methods[0].ReturnType)
	}
}

func TestParseInterfaceDefMultipleMethods(t *testing.T) {
	prog := parse(t, `
		interface Shape {
			fn area(): double
			fn perimeter(): double
			fn name(): string
		}
		fn main(): void { }
	`)
	if len(prog.Interfaces) != 1 {
		t.Fatalf("expected 1 interface, got %d", len(prog.Interfaces))
	}
	iface := prog.Interfaces[0]
	if len(iface.Methods) != 3 {
		t.Fatalf("expected 3 methods, got %d", len(iface.Methods))
	}
	if iface.Methods[0].Name != "area" {
		t.Errorf("Method[0].Name = %q, want %q", iface.Methods[0].Name, "area")
	}
	if iface.Methods[1].Name != "perimeter" {
		t.Errorf("Method[1].Name = %q, want %q", iface.Methods[1].Name, "perimeter")
	}
	if iface.Methods[2].Name != "name" {
		t.Errorf("Method[2].Name = %q, want %q", iface.Methods[2].Name, "name")
	}
}

func TestParseInterfaceDefWithParams(t *testing.T) {
	prog := parse(t, `
		interface Comparable {
			fn compare(other: int): int
		}
		fn main(): void { }
	`)
	iface := prog.Interfaces[0]
	if len(iface.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(iface.Methods))
	}
	m := iface.Methods[0]
	if m.Name != "compare" {
		t.Errorf("Method.Name = %q, want %q", m.Name, "compare")
	}
	if len(m.Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(m.Params))
	}
	if m.Params[0] != ast.TypeInt {
		t.Errorf("Method.Params[0] = %d, want TypeInt", m.Params[0])
	}
	if m.ReturnType != ast.TypeInt {
		t.Errorf("Method.ReturnType = %d, want TypeInt", m.ReturnType)
	}
}

func TestParseInterfaceDefVoidReturn(t *testing.T) {
	// Interface method without explicit return type defaults to void
	prog := parse(t, `
		interface Runnable {
			fn run()
		}
		fn main(): void { }
	`)
	iface := prog.Interfaces[0]
	if iface.Methods[0].ReturnType != ast.TypeVoid {
		t.Errorf("Method.ReturnType = %d, want TypeVoid", iface.Methods[0].ReturnType)
	}
}

// --- Const statement tests ---

func TestParseConstStmtInferred(t *testing.T) {
	s := parseStmt(t, "const x = 42")
	ls, ok := s.(*ast.LetStmt)
	if !ok {
		t.Fatalf("expected LetStmt, got %T", s)
	}
	if ls.Name != "x" {
		t.Errorf("Name = %q, want %q", ls.Name, "x")
	}
	if !ls.IsConst {
		t.Errorf("IsConst = false, want true")
	}
	if ls.Type != ast.TypeInferred {
		t.Errorf("Type = %d, want TypeInferred (%d)", ls.Type, ast.TypeInferred)
	}
	lit, ok := ls.Value.(*ast.IntLit)
	if !ok {
		t.Fatalf("Value is %T, want IntLit", ls.Value)
	}
	if lit.Value != 42 {
		t.Errorf("Value = %d, want 42", lit.Value)
	}
}

func TestParseConstStmtTyped(t *testing.T) {
	s := parseStmt(t, `const name: string = "DexLang"`)
	ls, ok := s.(*ast.LetStmt)
	if !ok {
		t.Fatalf("expected LetStmt, got %T", s)
	}
	if ls.Name != "name" {
		t.Errorf("Name = %q, want %q", ls.Name, "name")
	}
	if !ls.IsConst {
		t.Errorf("IsConst = false, want true")
	}
	if ls.Type != ast.TypeString {
		t.Errorf("Type = %d, want TypeString", ls.Type)
	}
}

func TestParseConstStmtDouble(t *testing.T) {
	s := parseStmt(t, "const pi: double = 3.14")
	ls, ok := s.(*ast.LetStmt)
	if !ok {
		t.Fatalf("expected LetStmt, got %T", s)
	}
	if !ls.IsConst {
		t.Errorf("IsConst = false, want true")
	}
	if ls.Type != ast.TypeDouble {
		t.Errorf("Type = %d, want TypeDouble", ls.Type)
	}
}

// --- Switch statement tests ---

func TestParseSwitchStmtBasic(t *testing.T) {
	s := parseStmt(t, `switch (x) {
		case 1: {
			return 10
		}
		case 2: {
			return 20
		}
	}`)
	sw, ok := s.(*ast.SwitchStmt)
	if !ok {
		t.Fatalf("expected SwitchStmt, got %T", s)
	}
	tag, ok := sw.Tag.(*ast.Ident)
	if !ok {
		t.Fatalf("Tag is %T, want Ident", sw.Tag)
	}
	if tag.Name != "x" {
		t.Errorf("Tag.Name = %q, want %q", tag.Name, "x")
	}
	if len(sw.Cases) != 2 {
		t.Fatalf("expected 2 cases, got %d", len(sw.Cases))
	}
	if len(sw.Cases[0].Values) != 1 {
		t.Fatalf("Case[0] has %d values, want 1", len(sw.Cases[0].Values))
	}
	if len(sw.Cases[0].Body) != 1 {
		t.Errorf("Case[0] has %d body stmts, want 1", len(sw.Cases[0].Body))
	}
	if sw.Default != nil {
		t.Errorf("Default should be nil")
	}
}

func TestParseSwitchStmtWithDefault(t *testing.T) {
	s := parseStmt(t, `switch (x) {
		case 1: {
			return 10
		}
		default: {
			return -1
		}
	}`)
	sw, ok := s.(*ast.SwitchStmt)
	if !ok {
		t.Fatalf("expected SwitchStmt, got %T", s)
	}
	if len(sw.Cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(sw.Cases))
	}
	if sw.Default == nil {
		t.Fatal("Default should not be nil")
	}
	if len(sw.Default) != 1 {
		t.Errorf("Default has %d stmts, want 1", len(sw.Default))
	}
}

func TestParseSwitchStmtMultiValue(t *testing.T) {
	s := parseStmt(t, `switch (action) {
		case "Heartbeat", "StatusNotification": {
			return 1
		}
	}`)
	sw, ok := s.(*ast.SwitchStmt)
	if !ok {
		t.Fatalf("expected SwitchStmt, got %T", s)
	}
	if len(sw.Cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(sw.Cases))
	}
	if len(sw.Cases[0].Values) != 2 {
		t.Fatalf("Case[0] has %d values, want 2", len(sw.Cases[0].Values))
	}
	v0, ok := sw.Cases[0].Values[0].(*ast.StringLit)
	if !ok {
		t.Fatalf("Values[0] is %T, want StringLit", sw.Cases[0].Values[0])
	}
	if v0.Value != "Heartbeat" {
		t.Errorf("Values[0] = %q, want %q", v0.Value, "Heartbeat")
	}
	v1, ok := sw.Cases[0].Values[1].(*ast.StringLit)
	if !ok {
		t.Fatalf("Values[1] is %T, want StringLit", sw.Cases[0].Values[1])
	}
	if v1.Value != "StatusNotification" {
		t.Errorf("Values[1] = %q, want %q", v1.Value, "StatusNotification")
	}
}

// --- Try/catch statement tests ---

func TestParseTryCatchStmt(t *testing.T) {
	s := parseStmt(t, `try {
		return 1
	} catch (e: Exception) {
		return 0
	}`)
	tc, ok := s.(*ast.TryCatchStmt)
	if !ok {
		t.Fatalf("expected TryCatchStmt, got %T", s)
	}
	if len(tc.Body) != 1 {
		t.Errorf("Body has %d stmts, want 1", len(tc.Body))
	}
	if tc.CatchVar != "e" {
		t.Errorf("CatchVar = %q, want %q", tc.CatchVar, "e")
	}
	if len(tc.CatchBody) != 1 {
		t.Errorf("CatchBody has %d stmts, want 1", len(tc.CatchBody))
	}
	if tc.FinallyBody != nil {
		t.Errorf("FinallyBody should be nil")
	}
}

func TestParseTryCatchFinallyStmt(t *testing.T) {
	s := parseStmt(t, `try {
		return 1
	} catch (err: Exception) {
		return 0
	} finally {
		return 2
	}`)
	tc, ok := s.(*ast.TryCatchStmt)
	if !ok {
		t.Fatalf("expected TryCatchStmt, got %T", s)
	}
	if tc.CatchVar != "err" {
		t.Errorf("CatchVar = %q, want %q", tc.CatchVar, "err")
	}
	if tc.CatchBody == nil {
		t.Fatal("CatchBody should not be nil")
	}
	if tc.FinallyBody == nil {
		t.Fatal("FinallyBody should not be nil")
	}
	if len(tc.FinallyBody) != 1 {
		t.Errorf("FinallyBody has %d stmts, want 1", len(tc.FinallyBody))
	}
}

func TestParseTryFinallyStmt(t *testing.T) {
	s := parseStmt(t, `try {
		return 1
	} finally {
		return 2
	}`)
	tc, ok := s.(*ast.TryCatchStmt)
	if !ok {
		t.Fatalf("expected TryCatchStmt, got %T", s)
	}
	if tc.CatchBody != nil {
		t.Errorf("CatchBody should be nil, got %d stmts", len(tc.CatchBody))
	}
	if tc.CatchVar != "" {
		t.Errorf("CatchVar should be empty, got %q", tc.CatchVar)
	}
	if tc.FinallyBody == nil {
		t.Fatal("FinallyBody should not be nil")
	}
}

func TestParseTryWithoutCatchOrFinallyError(t *testing.T) {
	err := parseError(t, `fn main(): void { try { return 1 } }`)
	if err == nil {
		t.Fatal("expected error for try without catch or finally")
	}
	if !strings.Contains(err.Error(), "must have at least a catch or finally") {
		t.Errorf("error = %q, want to contain 'must have at least a catch or finally'", err.Error())
	}
}

func TestParseTryCatchWrongTypeError(t *testing.T) {
	err := parseError(t, `fn main(): void { try { } catch (e: WrongType) { } }`)
	if err == nil {
		t.Fatal("expected error for non-Exception catch type")
	}
	if !strings.Contains(err.Error(), "Exception") {
		t.Errorf("error = %q, want to contain 'Exception'", err.Error())
	}
}

// --- Throw statement tests ---

func TestParseThrowStmt(t *testing.T) {
	s := parseStmt(t, `throw 42`)
	ts, ok := s.(*ast.ThrowStmt)
	if !ok {
		t.Fatalf("expected ThrowStmt, got %T", s)
	}
	lit, ok := ts.Value.(*ast.IntLit)
	if !ok {
		t.Fatalf("Value is %T, want IntLit", ts.Value)
	}
	if lit.Value != 42 {
		t.Errorf("Value = %d, want 42", lit.Value)
	}
}

func TestParseThrowStmtWithCall(t *testing.T) {
	s := parseStmt(t, `throw makeError()`)
	ts, ok := s.(*ast.ThrowStmt)
	if !ok {
		t.Fatalf("expected ThrowStmt, got %T", s)
	}
	call, ok := ts.Value.(*ast.CallExpr)
	if !ok {
		t.Fatalf("Value is %T, want CallExpr", ts.Value)
	}
	if call.Name != "makeError" {
		t.Errorf("Value.Name = %q, want %q", call.Name, "makeError")
	}
}

// --- Defer statement tests ---

func TestParseDeferStmt(t *testing.T) {
	s := parseStmt(t, `defer cleanup()`)
	ds, ok := s.(*ast.DeferStmt)
	if !ok {
		t.Fatalf("expected DeferStmt, got %T", s)
	}
	call, ok := ds.Expr.(*ast.CallExpr)
	if !ok {
		t.Fatalf("Expr is %T, want CallExpr", ds.Expr)
	}
	if call.Name != "cleanup" {
		t.Errorf("Expr.Name = %q, want %q", call.Name, "cleanup")
	}
}

func TestParseDeferStmtModuleCall(t *testing.T) {
	s := parseStmt(t, `defer file.close()`)
	ds, ok := s.(*ast.DeferStmt)
	if !ok {
		t.Fatalf("expected DeferStmt, got %T", s)
	}
	call, ok := ds.Expr.(*ast.CallExpr)
	if !ok {
		t.Fatalf("Expr is %T, want CallExpr", ds.Expr)
	}
	if call.Module != "file" {
		t.Errorf("Expr.Module = %q, want %q", call.Module, "file")
	}
	if call.Name != "close" {
		t.Errorf("Expr.Name = %q, want %q", call.Name, "close")
	}
}

// --- Destructure let statement tests ---

func TestParseDestructureLetStmt(t *testing.T) {
	s := parseStmt(t, `let { name, age } = person`)
	ds, ok := s.(*ast.DestructureLetStmt)
	if !ok {
		t.Fatalf("expected DestructureLetStmt, got %T", s)
	}
	if len(ds.Names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(ds.Names))
	}
	if ds.Names[0] != "name" {
		t.Errorf("Names[0] = %q, want %q", ds.Names[0], "name")
	}
	if ds.Names[1] != "age" {
		t.Errorf("Names[1] = %q, want %q", ds.Names[1], "age")
	}
	if ds.IsConst {
		t.Errorf("IsConst = true, want false")
	}
	ident, ok := ds.Value.(*ast.Ident)
	if !ok {
		t.Fatalf("Value is %T, want Ident", ds.Value)
	}
	if ident.Name != "person" {
		t.Errorf("Value.Name = %q, want %q", ident.Name, "person")
	}
}

func TestParseDestructureConstStmt(t *testing.T) {
	s := parseStmt(t, `const { x, y, z } = point`)
	ds, ok := s.(*ast.DestructureLetStmt)
	if !ok {
		t.Fatalf("expected DestructureLetStmt, got %T", s)
	}
	if len(ds.Names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(ds.Names))
	}
	if !ds.IsConst {
		t.Errorf("IsConst = false, want true")
	}
	if ds.Names[0] != "x" || ds.Names[1] != "y" || ds.Names[2] != "z" {
		t.Errorf("Names = %v, want [x, y, z]", ds.Names)
	}
}

func TestParseDestructureSingleName(t *testing.T) {
	s := parseStmt(t, `let { value } = wrapper`)
	ds, ok := s.(*ast.DestructureLetStmt)
	if !ok {
		t.Fatalf("expected DestructureLetStmt, got %T", s)
	}
	if len(ds.Names) != 1 {
		t.Fatalf("expected 1 name, got %d", len(ds.Names))
	}
	if ds.Names[0] != "value" {
		t.Errorf("Names[0] = %q, want %q", ds.Names[0], "value")
	}
}

// --- Spawn expression tests ---

func TestParseSpawnExprBlock(t *testing.T) {
	e := parseExpr(t, `spawn { return 42 }`)
	spawn, ok := e.(*ast.SpawnExpr)
	if !ok {
		t.Fatalf("expected SpawnExpr, got %T", e)
	}
	if spawn.Body == nil {
		t.Fatal("SpawnExpr.Body should not be nil")
	}
	if len(spawn.Body) != 1 {
		t.Fatalf("SpawnExpr.Body has %d stmts, want 1", len(spawn.Body))
	}
	if spawn.Call != nil {
		t.Errorf("SpawnExpr.Call should be nil for block spawn")
	}
}

func TestParseSpawnExprCall(t *testing.T) {
	e := parseExpr(t, `spawn multiply(21)`)
	spawn, ok := e.(*ast.SpawnExpr)
	if !ok {
		t.Fatalf("expected SpawnExpr, got %T", e)
	}
	if spawn.Call == nil {
		t.Fatal("SpawnExpr.Call should not be nil")
	}
	call, ok := spawn.Call.(*ast.CallExpr)
	if !ok {
		t.Fatalf("SpawnExpr.Call is %T, want CallExpr", spawn.Call)
	}
	if call.Name != "multiply" {
		t.Errorf("Call.Name = %q, want %q", call.Name, "multiply")
	}
	if len(call.Args) != 1 {
		t.Errorf("Call.Args count = %d, want 1", len(call.Args))
	}
	if spawn.Body != nil {
		t.Errorf("SpawnExpr.Body should be nil for call spawn")
	}
}

func TestParseSpawnStmt(t *testing.T) {
	// spawn as statement (fire-and-forget) wraps in ExprStmt
	s := parseStmt(t, `spawn doWork()`)
	es, ok := s.(*ast.ExprStmt)
	if !ok {
		t.Fatalf("expected ExprStmt, got %T", s)
	}
	spawn, ok := es.Expr.(*ast.SpawnExpr)
	if !ok {
		t.Fatalf("expected SpawnExpr, got %T", es.Expr)
	}
	if spawn.Call == nil {
		t.Fatal("SpawnExpr.Call should not be nil")
	}
}

// --- String interpolation tests ---

func TestParseStringInterp(t *testing.T) {
	e := parseExpr(t, `"hello ${name}!"`)
	interp, ok := e.(*ast.StringInterpExpr)
	if !ok {
		t.Fatalf("expected StringInterpExpr, got %T", e)
	}
	// Parts: "hello " + name + "!"
	if len(interp.Parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(interp.Parts))
	}
	// First part: StringLit "hello "
	s0, ok := interp.Parts[0].(*ast.StringLit)
	if !ok {
		t.Fatalf("Parts[0] is %T, want StringLit", interp.Parts[0])
	}
	if s0.Value != "hello " {
		t.Errorf("Parts[0].Value = %q, want %q", s0.Value, "hello ")
	}
	// Second part: Ident "name"
	id, ok := interp.Parts[1].(*ast.Ident)
	if !ok {
		t.Fatalf("Parts[1] is %T, want Ident", interp.Parts[1])
	}
	if id.Name != "name" {
		t.Errorf("Parts[1].Name = %q, want %q", id.Name, "name")
	}
	// Third part: StringLit "!"
	s2, ok := interp.Parts[2].(*ast.StringLit)
	if !ok {
		t.Fatalf("Parts[2] is %T, want StringLit", interp.Parts[2])
	}
	if s2.Value != "!" {
		t.Errorf("Parts[2].Value = %q, want %q", s2.Value, "!")
	}
}

func TestParseStringInterpMultiple(t *testing.T) {
	e := parseExpr(t, `"${a} and ${b}"`)
	interp, ok := e.(*ast.StringInterpExpr)
	if !ok {
		t.Fatalf("expected StringInterpExpr, got %T", e)
	}
	// Parts: "" + a + " and " + b + ""
	if len(interp.Parts) != 5 {
		t.Fatalf("expected 5 parts, got %d", len(interp.Parts))
	}
	// Check interpolated expressions
	id1, ok := interp.Parts[1].(*ast.Ident)
	if !ok {
		t.Fatalf("Parts[1] is %T, want Ident", interp.Parts[1])
	}
	if id1.Name != "a" {
		t.Errorf("Parts[1].Name = %q, want %q", id1.Name, "a")
	}
	mid, ok := interp.Parts[2].(*ast.StringLit)
	if !ok {
		t.Fatalf("Parts[2] is %T, want StringLit", interp.Parts[2])
	}
	if mid.Value != " and " {
		t.Errorf("Parts[2].Value = %q, want %q", mid.Value, " and ")
	}
	id2, ok := interp.Parts[3].(*ast.Ident)
	if !ok {
		t.Fatalf("Parts[3] is %T, want Ident", interp.Parts[3])
	}
	if id2.Name != "b" {
		t.Errorf("Parts[3].Name = %q, want %q", id2.Name, "b")
	}
}

func TestParseStringInterpWithExpr(t *testing.T) {
	e := parseExpr(t, `"result: ${1 + 2}"`)
	interp, ok := e.(*ast.StringInterpExpr)
	if !ok {
		t.Fatalf("expected StringInterpExpr, got %T", e)
	}
	// Parts: "result: " + (1 + 2) + ""
	if len(interp.Parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(interp.Parts))
	}
	bin, ok := interp.Parts[1].(*ast.BinaryExpr)
	if !ok {
		t.Fatalf("Parts[1] is %T, want BinaryExpr", interp.Parts[1])
	}
	if bin.Op != ast.BinAdd {
		t.Errorf("Parts[1].Op = %d, want BinAdd", bin.Op)
	}
}

// --- Lambda expression tests ---

func TestParseLambdaExprBasic(t *testing.T) {
	e := parseExpr(t, `fn(x: int): int { return x + 1 }`)
	lambda, ok := e.(*ast.LambdaExpr)
	if !ok {
		t.Fatalf("expected LambdaExpr, got %T", e)
	}
	if len(lambda.Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(lambda.Params))
	}
	if lambda.Params[0].Name != "x" {
		t.Errorf("Params[0].Name = %q, want %q", lambda.Params[0].Name, "x")
	}
	if lambda.Params[0].Type != ast.TypeInt {
		t.Errorf("Params[0].Type = %d, want TypeInt", lambda.Params[0].Type)
	}
	if lambda.ReturnType != ast.TypeInt {
		t.Errorf("ReturnType = %d, want TypeInt", lambda.ReturnType)
	}
	if len(lambda.Body) != 1 {
		t.Errorf("Body has %d stmts, want 1", len(lambda.Body))
	}
}

func TestParseLambdaExprNoReturnType(t *testing.T) {
	e := parseExpr(t, `fn(x: int) { return x }`)
	lambda, ok := e.(*ast.LambdaExpr)
	if !ok {
		t.Fatalf("expected LambdaExpr, got %T", e)
	}
	if lambda.ReturnType != ast.TypeVoid {
		t.Errorf("ReturnType = %d, want TypeVoid (default)", lambda.ReturnType)
	}
}

func TestParseLambdaExprNoParams(t *testing.T) {
	e := parseExpr(t, `fn(): void { return }`)
	lambda, ok := e.(*ast.LambdaExpr)
	if !ok {
		t.Fatalf("expected LambdaExpr, got %T", e)
	}
	if len(lambda.Params) != 0 {
		t.Errorf("Params count = %d, want 0", len(lambda.Params))
	}
}

func TestParseLambdaExprMultipleParams(t *testing.T) {
	e := parseExpr(t, `fn(a: int, b: int): int { return a + b }`)
	lambda, ok := e.(*ast.LambdaExpr)
	if !ok {
		t.Fatalf("expected LambdaExpr, got %T", e)
	}
	if len(lambda.Params) != 2 {
		t.Fatalf("expected 2 params, got %d", len(lambda.Params))
	}
	if lambda.Params[0].Name != "a" || lambda.Params[1].Name != "b" {
		t.Errorf("Params = {%q, %q}, want {a, b}", lambda.Params[0].Name, lambda.Params[1].Name)
	}
}

func TestParseLambdaExprWithFunctionKeyword(t *testing.T) {
	e := parseExpr(t, `function(x: int): int { return x }`)
	lambda, ok := e.(*ast.LambdaExpr)
	if !ok {
		t.Fatalf("expected LambdaExpr, got %T", e)
	}
	if len(lambda.Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(lambda.Params))
	}
	if lambda.Params[0].Name != "x" {
		t.Errorf("Params[0].Name = %q, want %q", lambda.Params[0].Name, "x")
	}
}

// --- Match expression tests ---

func TestParseMatchExprBasic(t *testing.T) {
	e := parseExpr(t, `match (x) {
		1 => 10
		2 => 20
	}`)
	m, ok := e.(*ast.MatchExpr)
	if !ok {
		t.Fatalf("expected MatchExpr, got %T", e)
	}
	tag, ok := m.Tag.(*ast.Ident)
	if !ok {
		t.Fatalf("Tag is %T, want Ident", m.Tag)
	}
	if tag.Name != "x" {
		t.Errorf("Tag.Name = %q, want %q", tag.Name, "x")
	}
	if len(m.Arms) != 2 {
		t.Fatalf("expected 2 arms, got %d", len(m.Arms))
	}
	// First arm: 1 => 10
	if len(m.Arms[0].Patterns) != 1 {
		t.Fatalf("Arm[0] has %d patterns, want 1", len(m.Arms[0].Patterns))
	}
	pat0, ok := m.Arms[0].Patterns[0].(*ast.IntLit)
	if !ok {
		t.Fatalf("Arm[0].Pattern[0] is %T, want IntLit", m.Arms[0].Patterns[0])
	}
	if pat0.Value != 1 {
		t.Errorf("Arm[0].Pattern[0] = %d, want 1", pat0.Value)
	}
	body0, ok := m.Arms[0].Body.(*ast.IntLit)
	if !ok {
		t.Fatalf("Arm[0].Body is %T, want IntLit", m.Arms[0].Body)
	}
	if body0.Value != 10 {
		t.Errorf("Arm[0].Body = %d, want 10", body0.Value)
	}
}

func TestParseMatchExprWithWildcard(t *testing.T) {
	e := parseExpr(t, `match (x) {
		1 => 10
		_ => 0
	}`)
	m, ok := e.(*ast.MatchExpr)
	if !ok {
		t.Fatalf("expected MatchExpr, got %T", e)
	}
	if len(m.Arms) != 2 {
		t.Fatalf("expected 2 arms, got %d", len(m.Arms))
	}
	if m.Arms[0].IsWildcard {
		t.Errorf("Arm[0] should not be wildcard")
	}
	if !m.Arms[1].IsWildcard {
		t.Errorf("Arm[1] should be wildcard")
	}
	body1, ok := m.Arms[1].Body.(*ast.IntLit)
	if !ok {
		t.Fatalf("Arm[1].Body is %T, want IntLit", m.Arms[1].Body)
	}
	if body1.Value != 0 {
		t.Errorf("Arm[1].Body = %d, want 0", body1.Value)
	}
}

func TestParseMatchExprMultiPattern(t *testing.T) {
	e := parseExpr(t, `match (x) {
		1, 2, 3 => 100
		_ => 0
	}`)
	m, ok := e.(*ast.MatchExpr)
	if !ok {
		t.Fatalf("expected MatchExpr, got %T", e)
	}
	if len(m.Arms) != 2 {
		t.Fatalf("expected 2 arms, got %d", len(m.Arms))
	}
	if len(m.Arms[0].Patterns) != 3 {
		t.Fatalf("Arm[0] has %d patterns, want 3", len(m.Arms[0].Patterns))
	}
}

func TestParseMatchExprStringPatterns(t *testing.T) {
	e := parseExpr(t, `match (cmd) {
		"start" => 1
		"stop" => 2
		_ => -1
	}`)
	m, ok := e.(*ast.MatchExpr)
	if !ok {
		t.Fatalf("expected MatchExpr, got %T", e)
	}
	if len(m.Arms) != 3 {
		t.Fatalf("expected 3 arms, got %d", len(m.Arms))
	}
	p0, ok := m.Arms[0].Patterns[0].(*ast.StringLit)
	if !ok {
		t.Fatalf("Arm[0].Pattern[0] is %T, want StringLit", m.Arms[0].Patterns[0])
	}
	if p0.Value != "start" {
		t.Errorf("Arm[0].Pattern[0] = %q, want %q", p0.Value, "start")
	}
}

// --- parseType tests (advanced types) ---

func TestParseTypeChan(t *testing.T) {
	prog := parse(t, `fn test(): void { let ch: chan int = channel(int) }`)
	body := prog.Functions[0].Body
	ls, ok := body[0].(*ast.LetStmt)
	if !ok {
		t.Fatalf("expected LetStmt, got %T", body[0])
	}
	if !ast.IsChanType(ls.Type) {
		t.Errorf("Type = %d, want a channel type", ls.Type)
	}
	if ast.ChanElemType(ls.Type) != ast.TypeInt {
		t.Errorf("ChanElemType = %d, want TypeInt", ast.ChanElemType(ls.Type))
	}
}

func TestParseTypeChanString(t *testing.T) {
	prog := parse(t, `fn test(): void { let ch: chan string = channel(string) }`)
	body := prog.Functions[0].Body
	ls, ok := body[0].(*ast.LetStmt)
	if !ok {
		t.Fatalf("expected LetStmt, got %T", body[0])
	}
	if !ast.IsChanType(ls.Type) {
		t.Errorf("Type = %d, want a channel type", ls.Type)
	}
	if ast.ChanElemType(ls.Type) != ast.TypeString {
		t.Errorf("ChanElemType = %d, want TypeString", ast.ChanElemType(ls.Type))
	}
}

func TestParseTypeFuncRef(t *testing.T) {
	prog := parse(t, `fn test(): void { let f: fn(int): int = multiply }`)
	body := prog.Functions[0].Body
	ls, ok := body[0].(*ast.LetStmt)
	if !ok {
		t.Fatalf("expected LetStmt, got %T", body[0])
	}
	if !ast.IsFuncType(ls.Type) {
		t.Errorf("Type = %d, want a func type", ls.Type)
	}
	params := ast.FuncTypeParams(ls.Type)
	if len(params) != 1 || params[0] != ast.TypeInt {
		t.Errorf("FuncTypeParams = %v, want [TypeInt]", params)
	}
	if ast.FuncTypeReturn(ls.Type) != ast.TypeInt {
		t.Errorf("FuncTypeReturn = %d, want TypeInt", ast.FuncTypeReturn(ls.Type))
	}
}

func TestParseTypeFuncRefMultipleParams(t *testing.T) {
	prog := parse(t, `fn test(): void { let f: fn(int, string): bool = check }`)
	body := prog.Functions[0].Body
	ls, ok := body[0].(*ast.LetStmt)
	if !ok {
		t.Fatalf("expected LetStmt, got %T", body[0])
	}
	if !ast.IsFuncType(ls.Type) {
		t.Errorf("Type = %d, want a func type", ls.Type)
	}
	params := ast.FuncTypeParams(ls.Type)
	if len(params) != 2 {
		t.Fatalf("FuncTypeParams has %d params, want 2", len(params))
	}
	if params[0] != ast.TypeInt {
		t.Errorf("Params[0] = %d, want TypeInt", params[0])
	}
	if params[1] != ast.TypeString {
		t.Errorf("Params[1] = %d, want TypeString", params[1])
	}
	if ast.FuncTypeReturn(ls.Type) != ast.TypeBool {
		t.Errorf("FuncTypeReturn = %d, want TypeBool", ast.FuncTypeReturn(ls.Type))
	}
}

func TestParseTypeOptional(t *testing.T) {
	prog := parse(t, `fn test(): void { let x: int? }`)
	body := prog.Functions[0].Body
	ls, ok := body[0].(*ast.LetStmt)
	if !ok {
		t.Fatalf("expected LetStmt, got %T", body[0])
	}
	if !ast.IsOptionalType(ls.Type) {
		t.Errorf("Type = %d, want an optional type", ls.Type)
	}
	if ast.OptionalInnerType(ls.Type) != ast.TypeInt {
		t.Errorf("OptionalInnerType = %d, want TypeInt", ast.OptionalInnerType(ls.Type))
	}
}

func TestParseTypeOptionalString(t *testing.T) {
	prog := parse(t, `fn test(): void { let s: string? }`)
	body := prog.Functions[0].Body
	ls := body[0].(*ast.LetStmt)
	if !ast.IsOptionalType(ls.Type) {
		t.Errorf("Type = %d, want an optional type", ls.Type)
	}
	if ast.OptionalInnerType(ls.Type) != ast.TypeString {
		t.Errorf("OptionalInnerType = %d, want TypeString", ast.OptionalInnerType(ls.Type))
	}
}

func TestParseTypeRef(t *testing.T) {
	// Ref types work on primitives
	prog := parse(t, `fn test(): void { let r: &int = x }`)
	body := prog.Functions[0].Body
	ls, ok := body[0].(*ast.LetStmt)
	if !ok {
		t.Fatalf("expected LetStmt, got %T", body[0])
	}
	if !ast.IsRefType(ls.Type) {
		t.Errorf("Type = %d, want a ref type", ls.Type)
	}
	if ast.RefInnerType(ls.Type) != ast.TypeInt {
		t.Errorf("RefInnerType = %d, want TypeInt", ast.RefInnerType(ls.Type))
	}
}

func TestParseTypeRefBool(t *testing.T) {
	prog := parse(t, `fn test(): void { let r: &bool = x }`)
	body := prog.Functions[0].Body
	ls := body[0].(*ast.LetStmt)
	if !ast.IsRefType(ls.Type) {
		t.Errorf("Type = %d, want a ref type", ls.Type)
	}
	if ast.RefInnerType(ls.Type) != ast.TypeBool {
		t.Errorf("RefInnerType = %d, want TypeBool", ast.RefInnerType(ls.Type))
	}
}

func TestParseTypeMutex(t *testing.T) {
	prog := parse(t, `fn test(): void { let m: mutex = x }`)
	body := prog.Functions[0].Body
	ls := body[0].(*ast.LetStmt)
	if ls.Type != ast.TypeMutex {
		t.Errorf("Type = %d, want TypeMutex", ls.Type)
	}
}

func TestParseTypeMap(t *testing.T) {
	prog := parse(t, `fn test(): void { let m: map[string, int] = {} }`)
	body := prog.Functions[0].Body
	ls := body[0].(*ast.LetStmt)
	if !ast.IsMapType(ls.Type) {
		t.Errorf("Type = %d, want a map type", ls.Type)
	}
	if ast.MapKeyType(ls.Type) != ast.TypeString {
		t.Errorf("MapKeyType = %d, want TypeString", ast.MapKeyType(ls.Type))
	}
	if ast.MapValueType(ls.Type) != ast.TypeInt {
		t.Errorf("MapValueType = %d, want TypeInt", ast.MapValueType(ls.Type))
	}
}

func TestParseTypeChar(t *testing.T) {
	prog := parse(t, `fn test(): void { let c: char = 'a' }`)
	body := prog.Functions[0].Body
	ls := body[0].(*ast.LetStmt)
	if ls.Type != ast.TypeChar {
		t.Errorf("Type = %d, want TypeChar", ls.Type)
	}
}

func TestParseTypeArrayOfPrimitives(t *testing.T) {
	tests := []struct {
		typeStr  string
		wantType ast.Type
	}{
		{"bool[]", ast.TypeArrayBool},
		{"long[]", ast.TypeArrayLong},
		{"double[]", ast.TypeArrayDouble},
		{"char[]", ast.TypeArrayChar},
	}
	for _, tt := range tests {
		prog := parse(t, `fn test(): void { let a: `+tt.typeStr+` = [] }`)
		body := prog.Functions[0].Body
		ls := body[0].(*ast.LetStmt)
		if ls.Type != tt.wantType {
			t.Errorf("Type for %q = %d, want %d", tt.typeStr, ls.Type, tt.wantType)
		}
	}
}

// --- For loop initializer tests (parseForInit) ---

func TestParseForInitConst(t *testing.T) {
	prog := parse(t, `fn main(): void {
		for(const i: int = 0; i < 10; i++) { }
	}`)
	body := prog.Functions[0].Body
	fs, ok := body[0].(*ast.ForStmt)
	if !ok {
		t.Fatalf("expected ForStmt, got %T", body[0])
	}
	init, ok := fs.Init.(*ast.LetStmt)
	if !ok {
		t.Fatalf("Init is %T, want LetStmt", fs.Init)
	}
	if !init.IsConst {
		t.Errorf("Init.IsConst = false, want true")
	}
	if init.Name != "i" {
		t.Errorf("Init.Name = %q, want %q", init.Name, "i")
	}
}

func TestParseForInitAssign(t *testing.T) {
	prog := parse(t, `fn main(): void {
		let i: int = 0
		for(i = 0; i < 10; i++) { }
	}`)
	body := prog.Functions[0].Body
	fs, ok := body[1].(*ast.ForStmt)
	if !ok {
		t.Fatalf("expected ForStmt, got %T", body[1])
	}
	init, ok := fs.Init.(*ast.AssignStmt)
	if !ok {
		t.Fatalf("Init is %T, want AssignStmt", fs.Init)
	}
	if init.Name != "i" {
		t.Errorf("Init.Name = %q, want %q", init.Name, "i")
	}
}

// --- For loop post-expression tests (parseForPost) ---

func TestParseForPostDecrement(t *testing.T) {
	prog := parse(t, `fn main(): void {
		for(let i: int = 10; i > 0; i--) { }
	}`)
	body := prog.Functions[0].Body
	fs := body[0].(*ast.ForStmt)
	post, ok := fs.Post.(*ast.DecrementStmt)
	if !ok {
		t.Fatalf("Post is %T, want DecrementStmt", fs.Post)
	}
	if post.Name != "i" {
		t.Errorf("Post.Name = %q, want %q", post.Name, "i")
	}
}

func TestParseForPostSubAssign(t *testing.T) {
	prog := parse(t, `fn main(): void {
		for(let i: int = 100; i > 0; i -= 10) { }
	}`)
	body := prog.Functions[0].Body
	fs := body[0].(*ast.ForStmt)
	post, ok := fs.Post.(*ast.CompoundAssignStmt)
	if !ok {
		t.Fatalf("Post is %T, want CompoundAssignStmt", fs.Post)
	}
	if post.Name != "i" || post.Op != ast.BinSub {
		t.Errorf("Post = {%q, %d}, want {i, BinSub}", post.Name, post.Op)
	}
}

func TestParseForPostMulAssign(t *testing.T) {
	prog := parse(t, `fn main(): void {
		for(let i: int = 1; i < 100; i *= 2) { }
	}`)
	body := prog.Functions[0].Body
	fs := body[0].(*ast.ForStmt)
	post, ok := fs.Post.(*ast.CompoundAssignStmt)
	if !ok {
		t.Fatalf("Post is %T, want CompoundAssignStmt", fs.Post)
	}
	if post.Name != "i" || post.Op != ast.BinMul {
		t.Errorf("Post = {%q, %d}, want {i, BinMul}", post.Name, post.Op)
	}
}

func TestParseForPostDivAssign(t *testing.T) {
	prog := parse(t, `fn main(): void {
		for(let i: int = 100; i > 0; i /= 2) { }
	}`)
	body := prog.Functions[0].Body
	fs := body[0].(*ast.ForStmt)
	post, ok := fs.Post.(*ast.CompoundAssignStmt)
	if !ok {
		t.Fatalf("Post is %T, want CompoundAssignStmt", fs.Post)
	}
	if post.Name != "i" || post.Op != ast.BinDiv {
		t.Errorf("Post = {%q, %d}, want {i, BinDiv}", post.Name, post.Op)
	}
}

func TestParseForPostModAssign(t *testing.T) {
	prog := parse(t, `fn main(): void {
		for(let i: int = 100; i > 0; i %= 7) { }
	}`)
	body := prog.Functions[0].Body
	fs := body[0].(*ast.ForStmt)
	post, ok := fs.Post.(*ast.CompoundAssignStmt)
	if !ok {
		t.Fatalf("Post is %T, want CompoundAssignStmt", fs.Post)
	}
	if post.Name != "i" || post.Op != ast.BinMod {
		t.Errorf("Post = {%q, %d}, want {i, BinMod}", post.Name, post.Op)
	}
}

func TestParseForPostAssign(t *testing.T) {
	prog := parse(t, `fn main(): void {
		for(let i: int = 0; i < 10; i = i + 1) { }
	}`)
	body := prog.Functions[0].Body
	fs := body[0].(*ast.ForStmt)
	post, ok := fs.Post.(*ast.AssignStmt)
	if !ok {
		t.Fatalf("Post is %T, want AssignStmt", fs.Post)
	}
	if post.Name != "i" {
		t.Errorf("Post.Name = %q, want %q", post.Name, "i")
	}
}

// --- Error cases for new constructs ---

func TestErrorSwitchMissingLBrace(t *testing.T) {
	err := parseError(t, `fn main(): void { switch (x) }`)
	if err == nil {
		t.Fatal("expected error for switch missing opening brace")
	}
}

func TestErrorStructMissingName(t *testing.T) {
	err := parseError(t, `struct { x: int } fn main(): void { }`)
	if err == nil {
		t.Fatal("expected error for struct missing name")
	}
}

func TestErrorEnumMissingName(t *testing.T) {
	err := parseError(t, `enum { Red Green } fn main(): void { }`)
	if err == nil {
		t.Fatal("expected error for enum missing name")
	}
}

func TestErrorInterfaceMissingBrace(t *testing.T) {
	err := parseError(t, `interface Foo fn main(): void { }`)
	if err == nil {
		t.Fatal("expected error for interface missing opening brace")
	}
}
