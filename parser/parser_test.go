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
	prog, err := New(tokens).Parse()
	if err != nil {
		t.Fatalf("parser error: %v", err)
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
	_, err = New(tokens).Parse()
	return err
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
