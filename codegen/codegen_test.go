package codegen

import (
	"strings"
	"testing"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/checker"
	"github.com/ensamuel7/dex/lexer"
	"github.com/ensamuel7/dex/parser"
	"github.com/ensamuel7/dex/stdlib"
	"github.com/ensamuel7/dex/token"
)

// generate runs the full pipeline (lex -> parse -> check -> codegen) and returns the C code.
func generate(t *testing.T, source string) string {
	t.Helper()
	ast.ResetStructTypes()
	ast.ResetChanTypes()
	ast.ResetTaskTypes()
	ast.ResetWeakTypes()
	ast.ResetStructArrayTypes()
	stdlib.RegisterAllModuleTypes()

	tokens, err := lexer.New(source).Tokenize()
	if err != nil {
		t.Fatalf("lexer error: %v", err)
	}

	importPaths := extractCodegenImportPaths(tokens)
	typeNames := stdlib.ModuleTypesForImports(importPaths)

	p := parser.New(tokens)
	for _, name := range typeNames {
		p.AddStructName(name)
	}
	prog, err := p.Parse()
	if err != nil {
		t.Fatalf("parser error: %v", err)
	}
	if err := checker.New().Check(prog); err != nil {
		t.Fatalf("checker error: %v", err)
	}
	return New().Generate(prog)
}

func extractCodegenImportPaths(tokens []token.Token) []string {
	var paths []string
	for i := 0; i < len(tokens)-1; i++ {
		if tokens[i].Kind == token.TokenImport && tokens[i+1].Kind == token.TokenString {
			paths = append(paths, tokens[i+1].Value)
		}
	}
	return paths
}

// assertContains checks that the output contains the expected substring.
func assertContains(t *testing.T, output, substr string) {
	t.Helper()
	if !strings.Contains(output, substr) {
		t.Errorf("output does not contain %q.\nOutput:\n%s", substr, output)
	}
}

// assertNotContains checks that the output does NOT contain the given substring.
func assertNotContains(t *testing.T, output, substr string) {
	t.Helper()
	if strings.Contains(output, substr) {
		t.Errorf("output should not contain %q.\nOutput:\n%s", substr, output)
	}
}

// --- Literal codegen ---

func TestCodegenIntLit(t *testing.T) {
	out := generate(t, "fn main(): int { return 42 }")
	assertContains(t, out, "return 42;")
}

func TestCodegenFloatLit(t *testing.T) {
	out := generate(t, "fn main(): double { return 3.14 }")
	assertContains(t, out, "return 3.14;")
}

func TestCodegenBoolLit(t *testing.T) {
	out := generate(t, "fn main(): bool { return true }")
	assertContains(t, out, "return true;")
	assertContains(t, out, "stdbool.h")

	out2 := generate(t, "fn main(): bool { return false }")
	assertContains(t, out2, "return false;")
}

func TestCodegenStringLit(t *testing.T) {
	out := generate(t, `fn main(): string { return "hello" }`)
	assertContains(t, out, `"hello"`)
}

func TestCodegenStringEscaping(t *testing.T) {
	out := generate(t, `fn main(): string { return "a\nb" }`)
	assertContains(t, out, `"a\nb"`)
}

// --- Binary ops ---

func TestCodegenBinaryArithmetic(t *testing.T) {
	tests := []struct {
		source   string
		expected string
	}{
		{"fn main(): int { return 1 + 2 }", "(1 + 2)"},
		{"fn main(): int { return 3 - 1 }", "(3 - 1)"},
		{"fn main(): int { return 2 * 3 }", "(2 * 3)"},
		{"fn main(): int { return 6 / 2 }", "(6 / dex_check_nonzero_int(2))"},
		{"fn main(): int { return 7 % 3 }", "(7 % dex_check_nonzero_int(3))"},
	}
	for _, tt := range tests {
		out := generate(t, tt.source)
		assertContains(t, out, tt.expected)
	}
}

func TestCodegenStringConcat(t *testing.T) {
	out := generate(t, `fn main(): string { return "a" + "b" }`)
	assertContains(t, out, "dex_str_concat(")
}

func TestCodegenStringComparison(t *testing.T) {
	out := generate(t, `fn main(): bool { return "a" == "b" }`)
	assertContains(t, out, "strcmp(")
	assertContains(t, out, "== 0")
}

func TestCodegenStringNeq(t *testing.T) {
	out := generate(t, `fn main(): bool { return "a" != "b" }`)
	assertContains(t, out, "strcmp(")
	assertContains(t, out, "!= 0")
}

func TestCodegenStrictEq(t *testing.T) {
	out := generate(t, `fn main(): bool { return 1 === 2 }`)
	assertContains(t, out, "(1 == 2)")
}

func TestCodegenStrictNeq(t *testing.T) {
	out := generate(t, `fn main(): bool { return 1 !== 2 }`)
	assertContains(t, out, "(1 != 2)")
}

func TestCodegenStrictEqString(t *testing.T) {
	out := generate(t, `fn main(): bool { return "a" === "b" }`)
	assertContains(t, out, "strcmp(")
	assertContains(t, out, "== 0")
}

func TestCodegenCrossNumericEqIntLong(t *testing.T) {
	out := generate(t, `fn main(): bool {
		let x: int = 42
		let y: long = 42
		return x == y
	}`)
	assertContains(t, out, "(long)")
}

func TestCodegenCrossNumericEqIntDouble(t *testing.T) {
	out := generate(t, `fn main(): bool {
		let x: int = 42
		let y: double = 42.0
		return x == y
	}`)
	assertContains(t, out, "(double)")
}

func TestCodegenCrossNumericEqLongDouble(t *testing.T) {
	out := generate(t, `fn main(): bool {
		let x: long = 100
		let y: double = 100.0
		return x == y
	}`)
	assertContains(t, out, "(double)")
}

func TestCodegenAssert(t *testing.T) {
	out := generate(t, `fn main(): void {
		assert(true)
	}`)
	assertContains(t, out, "if (!(true))")
	assertContains(t, out, "fprintf(stderr,")
	assertContains(t, out, "exit(1)")
	assertContains(t, out, "stdio.h")
	assertContains(t, out, "stdlib.h")
}

// --- Unary ops ---

func TestCodegenUnaryNeg(t *testing.T) {
	out := generate(t, "fn main(): int { return -42 }")
	assertContains(t, out, "(-42)")
}

func TestCodegenUnaryNot(t *testing.T) {
	out := generate(t, "fn main(): bool { return !true }")
	assertContains(t, out, "(!true)")
}

// --- Variables ---

func TestCodegenLetInt(t *testing.T) {
	out := generate(t, "fn main(): int { let x: int = 42 return x }")
	assertContains(t, out, "int x = 42;")
}

func TestCodegenLetBool(t *testing.T) {
	out := generate(t, "fn main(): void { let b: bool = true }")
	assertContains(t, out, "_Bool b = true;")
}

func TestCodegenLetString(t *testing.T) {
	out := generate(t, `fn main(): void { let s: string = "hi" }`)
	assertContains(t, out, `DexString* s = dex_string_from_lit("hi");`)
}

func TestCodegenLetLong(t *testing.T) {
	out := generate(t, `fn f(l: long): long { return l }
		fn main(): void {}`)
	assertContains(t, out, "long f(long l)")
}

func TestCodegenLetDouble(t *testing.T) {
	out := generate(t, "fn main(): double { let d: double = 3.14 return d }")
	assertContains(t, out, "double d = 3.14;")
}

func TestCodegenLetArrayInt(t *testing.T) {
	out := generate(t, "fn main(): int { let a: int[] = [1, 2] return a[0] }")
	assertContains(t, out, "DexArrayInt* a = dex_array_int_new();")
	assertContains(t, out, "a->data[0] = 1;")
	assertContains(t, out, "a->data[1] = 2;")
	assertContains(t, out, "a->len = 2;")
}

// --- Control flow ---

func TestCodegenIfElse(t *testing.T) {
	out := generate(t, `fn main(): int {
		if (true) { return 1 } else { return 0 }
	}`)
	assertContains(t, out, "if (true)")
	assertContains(t, out, "} else {")
}

func TestCodegenWhile(t *testing.T) {
	out := generate(t, `fn main(): int {
		let x: int = 0
		while (x < 10) { x = x + 1 }
		return x
	}`)
	assertContains(t, out, "while (x < 10)")
}

// --- Functions ---

func TestCodegenFunctionNoParams(t *testing.T) {
	out := generate(t, "fn main(): int { return 0 }")
	assertContains(t, out, "int main(")
	assertContains(t, out, "return 0;")
}

func TestCodegenVoidMainImplicitReturn(t *testing.T) {
	out := generate(t, "fn main(): void {}")
	assertContains(t, out, "int main(")
	assertContains(t, out, "return 0;")
}

func TestCodegenFunctionWithParams(t *testing.T) {
	out := generate(t, `
		fn add(a: int, b: int): int { return a + b }
		fn main(): int { return add(1, 2) }
	`)
	assertContains(t, out, "int add(int a, int b)")
	assertContains(t, out, "add(1, 2)")
}

func TestCodegenMultipleFunctions(t *testing.T) {
	out := generate(t, `
		fn foo(): int { return 1 }
		fn bar(): int { return 2 }
		fn main(): int { return foo() + bar() }
	`)
	assertContains(t, out, "int foo(")
	assertContains(t, out, "int bar(")
	assertContains(t, out, "int main(")
}

func TestCodegenVoidStringFunc(t *testing.T) {
	out := generate(t, `fn handler(): string { return "ok" }
		fn main(): void {}`)
	assertContains(t, out, "DexString* handler(void)")
}

// --- Arrays ---

func TestCodegenArrayPush(t *testing.T) {
	out := generate(t, `fn main(): void {
		let a: int[] = [1]
		a.push(2)
	}`)
	assertContains(t, out, "dex_array_int_push(a, 2)")
}

func TestCodegenArrayLen(t *testing.T) {
	out := generate(t, `fn main(): int {
		let a: int[] = [1, 2, 3]
		return a.len()
	}`)
	assertContains(t, out, "a->len")
}

func TestCodegenArrayIndexing(t *testing.T) {
	out := generate(t, `fn main(): int {
		let a: int[] = [10, 20]
		return a[0]
	}`)
	assertContains(t, out, "dex_bounds_check(0, a->len)")
	assertContains(t, out, "a->data[0]")
}

func TestCodegenEmptyArray(t *testing.T) {
	out := generate(t, `fn main(): void {
		let a: int[] = []
	}`)
	assertContains(t, out, "DexArrayInt* a = dex_array_int_new();")
}

func TestCodegenIndexAssign(t *testing.T) {
	out := generate(t, `fn main(): void {
		let a: int[] = [1, 2]
		a[0] = 99
	}`)
	assertContains(t, out, "dex_bounds_check(0, a->len);")
	assertContains(t, out, "a->data[0] = 99;")
}

// --- Stdlib codegen ---

func TestCodegenFmtPrint(t *testing.T) {
	out := generate(t, `import "fmt" fn main(): void { fmt.print(42) }`)
	assertContains(t, out, `printf("%d\n", 42)`)
}

func TestCodegenFmtPrintStr(t *testing.T) {
	out := generate(t, `import "fmt" fn main(): void { fmt.print("hi") }`)
	assertContains(t, out, `printf("%s\n"`)
}

func TestCodegenFmtPrintLong(t *testing.T) {
	out := generate(t, `import "fmt" fn f(l: long): void {
		fmt.print(l)
	}
	fn main(): void {}`)
	assertContains(t, out, `printf("%ld\n"`)
}

func TestCodegenFmtPrintDouble(t *testing.T) {
	out := generate(t, `import "fmt" fn main(): void { fmt.print(3.14) }`)
	assertContains(t, out, `printf("%f\n"`)
}

func TestCodegenFmtPrintBool(t *testing.T) {
	out := generate(t, `import "fmt" fn main(): void { fmt.print(true) }`)
	assertContains(t, out, `printf("%s\n"`)
	assertContains(t, out, `"true" : "false"`)
}

func TestCodegenJsonNew(t *testing.T) {
	out := generate(t, `import "json" fn main(): string { return json.new() }`)
	assertContains(t, out, "dex_string_from_cstr(dex_json_new())")
}

func TestCodegenJsonSet(t *testing.T) {
	out := generate(t, `import "json" fn main(): string {
		let obj: string = json.new()
		return json.set(obj, "key", "val")
	}`)
	assertContains(t, out, "dex_json_set(")
}

func TestCodegenJsonStringify(t *testing.T) {
	out := generate(t, `import "json" fn main(): string {
		let a: int[] = [1, 2]
		return json.stringify(a)
	}`)
	assertContains(t, out, "dex_string_from_cstr(dex_json_stringify_int(a))")
}

func TestCodegenJsonSetArr(t *testing.T) {
	out := generate(t, `import "json" fn main(): string {
		let obj: string = json.new()
		let a: int[] = [1, 2]
		return json.setArray(obj, "nums", a)
	}`)
	assertContains(t, out, "dex_json_set_arr_int(")
}

func TestCodegenHttpRoute(t *testing.T) {
	out := generate(t, `import "http" fn handler(): string { return "ok" }
		fn main(): void {
			http.route("GET", "/", "handler")
		}
	`)
	assertContains(t, out, "dex_route(")
	assertContains(t, out, "handler)")
}

func TestCodegenHttpListen(t *testing.T) {
	out := generate(t, `import "http" fn handler(): string { return "ok" }
		fn main(): void {
			http.listen(8080)
		}
	`)
	assertContains(t, out, "dex_listen(8080)")
}

// --- Feature detection ---

func TestFeatureDetectBool(t *testing.T) {
	out := generate(t, "fn main(): bool { return true }")
	assertContains(t, out, "stdbool.h")
}

func TestFeatureDetectNoBool(t *testing.T) {
	out := generate(t, "fn main(): int { return 42 }")
	assertNotContains(t, out, "stdbool.h")
}

func TestFeatureDetectString(t *testing.T) {
	out := generate(t, `fn main(): string { return "hi" }`)
	assertContains(t, out, "dex_str_concat")
}

func TestFeatureDetectNoString(t *testing.T) {
	out := generate(t, "fn main(): int { return 42 }")
	assertNotContains(t, out, "dex_str_concat")
}

func TestFeatureDetectArray(t *testing.T) {
	out := generate(t, `fn main(): void {
		let a: int[] = [1, 2]
	}`)
	assertContains(t, out, "DexArrayInt")
}

func TestFeatureDetectNoArray(t *testing.T) {
	out := generate(t, "fn main(): int { return 42 }")
	assertNotContains(t, out, "DexArrayInt")
}

// --- Assign statement ---

func TestCodegenAssign(t *testing.T) {
	out := generate(t, `fn main(): int {
		let x: int = 1
		x = 2
		return x
	}`)
	assertContains(t, out, "x = 2;")
}

// --- Block statement ---

func TestCodegenBlock(t *testing.T) {
	out := generate(t, `fn main(): void {
		{ let x: int = 1 }
	}`)
	assertContains(t, out, "{\n")
}

// --- Type mappings ---

func TestCTypeMapping(t *testing.T) {
	g := New()
	tests := []struct {
		typ  ast.Type
		want string
	}{
		{ast.TypeInt, "int"},
		{ast.TypeBool, "_Bool"},
		{ast.TypeString, "DexString*"},
		{ast.TypeLong, "long"},
		{ast.TypeDouble, "double"},
		{ast.TypeVoid, "void"},
		{ast.TypeArrayInt, "DexArrayInt*"},
		{ast.TypeArrayBool, "DexArrayBool*"},
		{ast.TypeArrayString, "DexArrayString*"},
		{ast.TypeArrayLong, "DexArrayLong*"},
		{ast.TypeArrayDouble, "DexArrayDouble*"},
	}
	for _, tt := range tests {
		got := g.cType(tt.typ)
		if got != tt.want {
			t.Errorf("cType(%d) = %q, want %q", tt.typ, got, tt.want)
		}
	}
}

func TestCBinOpMapping(t *testing.T) {
	g := New()
	tests := []struct {
		op   ast.BinOp
		want string
	}{
		{ast.BinAdd, "+"},
		{ast.BinSub, "-"},
		{ast.BinMul, "*"},
		{ast.BinDiv, "/"},
		{ast.BinMod, "%"},
		{ast.BinEq, "=="},
		{ast.BinNeq, "!="},
		{ast.BinStrictEq, "=="},
		{ast.BinStrictNeq, "!="},
		{ast.BinLt, "<"},
		{ast.BinGt, ">"},
		{ast.BinLte, "<="},
		{ast.BinGte, ">="},
		{ast.BinAnd, "&&"},
		{ast.BinOr, "||"},
	}
	for _, tt := range tests {
		got := g.cBinOp(tt.op)
		if got != tt.want {
			t.Errorf("cBinOp(%d) = %q, want %q", tt.op, got, tt.want)
		}
	}
}

// --- Array type codegen variants ---

func TestCodegenArrayBool(t *testing.T) {
	out := generate(t, `fn main(): void {
		let a: bool[] = [true, false]
	}`)
	assertContains(t, out, "DexArrayBool* a = dex_array_bool_new();")
}

func TestCodegenArrayString(t *testing.T) {
	out := generate(t, `fn main(): void {
		let a: string[] = ["a", "b"]
	}`)
	assertContains(t, out, "DexArrayString* a = dex_array_string_new();")
}

func TestCodegenArrayLong(t *testing.T) {
	out := generate(t, `fn main(): void {
		let a: long[] = []
	}`)
	assertContains(t, out, "DexArrayLong* a = dex_array_long_new();")
}

func TestCodegenArrayDouble(t *testing.T) {
	out := generate(t, `fn main(): void {
		let a: double[] = []
	}`)
	assertContains(t, out, "DexArrayDouble* a = dex_array_double_new();")
}

func TestCodegenArraySort(t *testing.T) {
	out := generate(t, `fn main(): void {
		let a: int[] = [3, 1, 2]
		a.sort("asc")
	}`)
	assertContains(t, out, "dex_array_int_sort_asc(a)")

	out2 := generate(t, `fn main(): void {
		let a: string[] = ["c", "a"]
		a.sort("desc")
	}`)
	assertContains(t, out2, "dex_array_string_sort_desc(a)")
}

func TestCodegenArrayPop(t *testing.T) {
	out := generate(t, `fn main(): void {
		let a: int[] = [1, 2, 3]
		let x: int = a.pop()
	}`)
	assertContains(t, out, "dex_array_int_pop(a)")
}

func TestCodegenArrayRemove(t *testing.T) {
	out := generate(t, `fn main(): void {
		let a: int[] = [1, 2, 3]
		a.remove(0)
	}`)
	assertContains(t, out, "dex_array_int_remove(a, 0)")
}

func TestCodegenArrayContains(t *testing.T) {
	out := generate(t, `fn main(): void {
		let a: int[] = [1, 2, 3]
		let b: bool = a.contains(2)
	}`)
	assertContains(t, out, "dex_array_int_contains(a, 2)")
}

func TestCodegenArrayIndexOf(t *testing.T) {
	out := generate(t, `fn main(): void {
		let a: string[] = ["a", "b"]
		let i: int = a.indexOf("b")
	}`)
	assertContains(t, out, `dex_array_string_indexOf(a, dex_string_from_lit("b"))`)
}

func TestCodegenCrossNumericAdd(t *testing.T) {
	out := generate(t, `fn main(): void {
		let a: int = 1
		let b: double = 2.0
		let c: double = a + b
	}`)
	assertContains(t, out, "(double)")
}

func TestCodegenCrossNumericDiv(t *testing.T) {
	out := generate(t, `fn main(): void {
		let a: double = 10.0
		let b: int = 3
		let c: double = a / b
	}`)
	assertContains(t, out, "(double)")
	assertContains(t, out, "dex_check_nonzero_double(")
}

func TestCodegenCrossNumericMul(t *testing.T) {
	out := generate(t, `fn main(): void {
		let a: int = 2
		let b: long = 3
		let c: long = a * b
	}`)
	assertContains(t, out, "(long)")
}

func TestCodegenArrayReverse(t *testing.T) {
	out := generate(t, `fn main(): void {
		let a: int[] = [1, 2, 3]
		a.reverse()
	}`)
	assertContains(t, out, "dex_array_int_reverse(a)")
}

// --- Break and Continue ---

func TestCodegenBreak(t *testing.T) {
	out := generate(t, `fn main(): void {
		while (true) {
			break
		}
	}`)
	assertContains(t, out, "break;")
}

func TestCodegenContinue(t *testing.T) {
	out := generate(t, `fn main(): void {
		while (true) {
			continue
		}
	}`)
	assertContains(t, out, "continue;")
}

// --- For loop ---

func TestCodegenForLoop(t *testing.T) {
	out := generate(t, `fn main(): void {
		for(let i: int = 0; i < 10; i++) { }
	}`)
	assertContains(t, out, "for (int i = 0; i < 10; i++)")
}

func TestCodegenForLoopDecrement(t *testing.T) {
	out := generate(t, `fn main(): void {
		for(let i: int = 10; i > 0; i--) { }
	}`)
	assertContains(t, out, "for (int i = 10; i > 0; i--)")
}

func TestCodegenForLoopCompoundAssign(t *testing.T) {
	out := generate(t, `fn main(): void {
		for(let i: int = 0; i < 100; i += 2) { }
	}`)
	assertContains(t, out, "for (int i = 0; i < 100; i += 2)")
}

func TestCodegenForLoopBody(t *testing.T) {
	out := generate(t, `fn main(): void {
		let sum: int = 0
		for(let i: int = 0; i < 5; i++) {
			sum += i
		}
	}`)
	assertContains(t, out, "for (int i = 0; i < 5; i++)")
	assertContains(t, out, "sum += i;")
}

// --- Foreach ---

func TestCodegenForeachValueOnly(t *testing.T) {
	out := generate(t, `fn main(): void {
		let a: int[] = [1, 2, 3]
		foreach(a as n) {
			let x: int = n
		}
	}`)
	assertContains(t, out, "for (int _foreach_idx_0 = 0; _foreach_idx_0 < a->len; _foreach_idx_0++)")
	assertContains(t, out, "int n = a->data[_foreach_idx_0];")
}

func TestCodegenForeachWithIndex(t *testing.T) {
	out := generate(t, `fn main(): void {
		let a: int[] = [10, 20, 30]
		foreach(a as i, n) {
			let x: int = i + n
		}
	}`)
	assertContains(t, out, "int n = a->data[_foreach_idx_0];")
	assertContains(t, out, "int i = _foreach_idx_0;")
}

func TestCodegenForeachUniqueCounters(t *testing.T) {
	out := generate(t, `fn main(): void {
		let a: int[] = [1, 2]
		let b: int[] = [3, 4]
		foreach(a as x) { }
		foreach(b as y) { }
	}`)
	assertContains(t, out, "_foreach_idx_0")
	assertContains(t, out, "_foreach_idx_1")
}

// --- Increment/Decrement/CompoundAssign ---

func TestCodegenIncrement(t *testing.T) {
	out := generate(t, `fn main(): void {
		let x: int = 0
		x++
	}`)
	assertContains(t, out, "x++;")
}

func TestCodegenDecrement(t *testing.T) {
	out := generate(t, `fn main(): void {
		let x: int = 10
		x--
	}`)
	assertContains(t, out, "x--;")
}

func TestCodegenCompoundAssignAdd(t *testing.T) {
	out := generate(t, `fn main(): void {
		let x: int = 0
		x += 5
	}`)
	assertContains(t, out, "x += 5;")
}

func TestCodegenCompoundAssignSub(t *testing.T) {
	out := generate(t, `fn main(): void {
		let x: int = 10
		x -= 3
	}`)
	assertContains(t, out, "x -= 3;")
}

// --- Type Inference ---

func TestCodegenTypeInferenceInt(t *testing.T) {
	out := generate(t, `fn main(): void {
		let x = 42
	}`)
	assertContains(t, out, "int x = 42;")
}

func TestCodegenTypeInferenceDouble(t *testing.T) {
	out := generate(t, `fn main(): void {
		let x = 3.14
	}`)
	assertContains(t, out, "double x = 3.14;")
}

func TestCodegenTypeInferenceString(t *testing.T) {
	out := generate(t, `fn main(): void {
		let x = "hello"
	}`)
	assertContains(t, out, `DexString* x = dex_string_from_lit("hello");`)
}

func TestCodegenTypeInferenceBool(t *testing.T) {
	out := generate(t, `fn main(): void {
		let x = true
	}`)
	assertContains(t, out, "_Bool x = true;")
}

func TestCodegenTypeInferenceArray(t *testing.T) {
	out := generate(t, `fn main(): void {
		let a = [1, 2, 3]
	}`)
	assertContains(t, out, "DexArrayInt* a = dex_array_int_new();")
	assertContains(t, out, "a->data[0] = 1;")
	assertContains(t, out, "a->len = 3;")
}

// --- Runtime safety checks ---

func TestCodegenBoundsCheckRead(t *testing.T) {
	out := generate(t, `fn main(): int {
		let a: int[] = [1, 2, 3]
		return a[1]
	}`)
	assertContains(t, out, "(dex_bounds_check(1, a->len), a->data[1])")
}

func TestCodegenBoundsCheckReadVariable(t *testing.T) {
	out := generate(t, `fn main(): int {
		let a: int[] = [1, 2, 3]
		let i: int = 0
		return a[i]
	}`)
	assertContains(t, out, "(dex_bounds_check(i, a->len), a->data[i])")
}

func TestCodegenBoundsCheckWrite(t *testing.T) {
	out := generate(t, `fn main(): void {
		let a: int[] = [1, 2, 3]
		a[1] = 42
	}`)
	assertContains(t, out, "dex_bounds_check(1, a->len);")
	assertContains(t, out, "a->data[1] = 42;")
}

func TestCodegenDivByZeroCheckInt(t *testing.T) {
	out := generate(t, `fn main(): int {
		let a: int = 10
		let b: int = 2
		return a / b
	}`)
	assertContains(t, out, "dex_check_nonzero_int(b)")
}

func TestCodegenModByZeroCheckInt(t *testing.T) {
	out := generate(t, `fn main(): int {
		let a: int = 10
		let b: int = 3
		return a % b
	}`)
	assertContains(t, out, "dex_check_nonzero_int(b)")
}

func TestCodegenDivByZeroCheckMixedTypes(t *testing.T) {
	out := generate(t, `fn main(): void {
		let a: int = 10
		let b: long = 3
		let c: long = a / b
	}`)
	assertContains(t, out, "dex_check_nonzero_long(")
}

func TestCodegenNoDivCheckForOtherOps(t *testing.T) {
	out := generate(t, `fn main(): int {
		let a: int = 10
		let b: int = 2
		return a + b
	}`)
	assertNotContains(t, out, "dex_check_nonzero")
}

// --- HTTP Client Codegen ---

func TestCodegenHttpGet(t *testing.T) {
	out := generate(t, `import "http"
fn main(): void {
	let resp: HttpResponse = http.get("https://example.com")
}`)
	assertContains(t, out, "dex_http_get(")
	assertContains(t, out, "Dex_HttpResponse")
}

func TestCodegenHttpPost(t *testing.T) {
	out := generate(t, `import "http"
fn main(): void {
	let resp: HttpResponse = http.post("https://example.com", "{}")
}`)
	assertContains(t, out, "dex_http_post(")
}

func TestCodegenHttpPut(t *testing.T) {
	out := generate(t, `import "http"
fn main(): void {
	let resp: HttpResponse = http.put("https://example.com/1", "{}")
}`)
	assertContains(t, out, "dex_http_put(")
}

func TestCodegenHttpPatch(t *testing.T) {
	out := generate(t, `import "http"
fn main(): void {
	let resp: HttpResponse = http.patch("https://example.com/1", "{}")
}`)
	assertContains(t, out, "dex_http_patch(")
}

func TestCodegenHttpDelete(t *testing.T) {
	out := generate(t, `import "http"
fn main(): void {
	let resp: HttpResponse = http.delete("https://example.com/1")
}`)
	assertContains(t, out, "dex_http_delete(")
}

func TestCodegenHttpRequest(t *testing.T) {
	out := generate(t, `import "http"
fn main(): void {
	let resp: HttpResponse = http.request("POST", "https://example.com", "{}", "Authorization: Bearer token")
}`)
	assertContains(t, out, "dex_http_request(")
}

func TestCodegenHttpFormFunctions(t *testing.T) {
	out := generate(t, `import "http"
fn main(): void {
	let form: string = http.formNew()
	form = http.formField(form, "name", "Alice")
	form = http.formFile(form, "avatar", "/path/to/file.jpg")
	let resp: HttpResponse = http.postForm("https://example.com/upload", form)
}`)
	assertContains(t, out, "dex_http_form_new()")
	assertContains(t, out, "dex_http_form_field(")
	assertContains(t, out, "dex_http_form_file(")
	assertContains(t, out, "dex_http_post_form(")
}

func TestCodegenHttpStructTypedefBeforeRuntime(t *testing.T) {
	out := generate(t, `import "http"
fn main(): void {
	let resp: HttpResponse = http.get("https://example.com")
}`)
	// Struct typedef must appear before the C runtime that uses it
	typedefIdx := strings.Index(out, "typedef struct {")
	runtimeIdx := strings.Index(out, "dex_route_entry")
	if typedefIdx < 0 || runtimeIdx < 0 {
		t.Fatal("expected both typedef and runtime in output")
	}
	if typedefIdx > runtimeIdx {
		t.Error("struct typedef should appear before module C runtime")
	}
}
