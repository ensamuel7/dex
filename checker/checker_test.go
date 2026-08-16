package checker

import (
	"strings"
	"testing"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/lexer"
	"github.com/ensamuel7/dex/parser"
	"github.com/ensamuel7/dex/stdlib"
	"github.com/ensamuel7/dex/token"
)

// checkSource runs the full pipeline (lex -> parse -> check) and returns the error if any.
func checkSource(t *testing.T, source string) error {
	t.Helper()
	ast.ResetStructTypes()
	ast.ResetChanTypes()
	ast.ResetTaskTypes()
	ast.ResetWeakTypes()
	ast.ResetStructArrayTypes()
	ast.ResetOptionalTypes()
	ast.ResetRefTypes()
	ast.ResetFuncTypes()
	ast.ResetMapTypes()
	ast.ResetEnumTypes()
	stdlib.RegisterAllModuleTypes()

	tokens, err := lexer.New(source).Tokenize()
	if err != nil {
		t.Fatalf("lexer error: %v", err)
	}

	// Seed parser with module type names from imports
	importPaths := extractTestImportPaths(tokens)
	typeNames := stdlib.ModuleTypesForImports(importPaths)

	p := parser.New(tokens)
	for _, name := range typeNames {
		p.AddStructName(name)
	}
	prog, err := p.Parse()
	if err != nil {
		t.Fatalf("parser error: %v", err)
	}
	return New().Check(prog)
}

func extractTestImportPaths(tokens []token.Token) []string {
	var paths []string
	for i := 0; i < len(tokens)-1; i++ {
		if tokens[i].Kind == token.TokenImport && tokens[i+1].Kind == token.TokenString {
			paths = append(paths, tokens[i+1].Value)
		}
	}
	return paths
}

// mustCheck asserts that the source type-checks without error.
func mustCheck(t *testing.T, source string) {
	t.Helper()
	if err := checkSource(t, source); err != nil {
		t.Fatalf("unexpected check error: %v", err)
	}
}

// mustFail asserts that the source produces a type-check error containing substr.
func mustFail(t *testing.T, source string, substr string) {
	t.Helper()
	err := checkSource(t, source)
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("error = %q, want to contain %q", err.Error(), substr)
	}
}

// --- Literal type inference ---

func TestLiteralTypes(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{"int", "fn main(): void { let x: int = 42 }"},
		{"float->double", "fn main(): double { let x: double = 3.14 return x }"},
		{"bool", "fn main(): bool { let x: bool = true return x }"},
		{"string", "fn main(): string { let x: string = \"hi\" return x }"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mustCheck(t, tt.source)
		})
	}
}

// --- Binary ops ---

func TestBinaryArithmetic(t *testing.T) {
	mustCheck(t, "fn main(): void { let x: int = 1 + 2 }")
	mustCheck(t, "fn main(): void { let x: int = 1 - 2 }")
	mustCheck(t, "fn main(): void { let x: int = 1 * 2 }")
	mustCheck(t, "fn main(): void { let x: int = 1 / 2 }")
	mustCheck(t, "fn main(): void { let x: int = 1 % 2 }")
}

func TestBinaryIntArithmetic(t *testing.T) {
	mustCheck(t, "fn main(): void { let x: int = 1 + 2 }")
	mustCheck(t, "fn main(): void { let x: int = 3 - 1 }")
	mustCheck(t, "fn main(): void { let x: int = 2 * 3 }")
	mustCheck(t, "fn main(): void { let x: int = 6 / 2 }")
	mustCheck(t, "fn main(): void { let x: int = 7 % 3 }")
}

func TestBinaryDoubleArithmetic(t *testing.T) {
	mustCheck(t, "fn main(): double { return 1.0 + 2.0 }")
	mustCheck(t, "fn main(): double { return 3.0 - 1.0 }")
	mustCheck(t, "fn main(): double { return 2.0 * 3.0 }")
	mustCheck(t, "fn main(): double { return 6.0 / 2.0 }")
}

func TestStringConcat(t *testing.T) {
	mustCheck(t, `fn main(): string { return "a" + "b" }`)
}

func TestBinaryComparisons(t *testing.T) {
	mustCheck(t, "fn main(): bool { return 1 < 2 }")
	mustCheck(t, "fn main(): bool { return 1 > 2 }")
	mustCheck(t, "fn main(): bool { return 1 <= 2 }")
	mustCheck(t, "fn main(): bool { return 1 >= 2 }")
}

func TestBinaryEquality(t *testing.T) {
	mustCheck(t, "fn main(): bool { return 1 == 2 }")
	mustCheck(t, "fn main(): bool { return 1 != 2 }")
	mustCheck(t, `fn main(): bool { return "a" == "b" }`)
	mustCheck(t, "fn main(): bool { return true == false }")
}

func TestBinaryLogical(t *testing.T) {
	mustCheck(t, "fn main(): bool { return true && false }")
	mustCheck(t, "fn main(): bool { return true || false }")
}

// --- Unary ops ---

func TestUnaryNegation(t *testing.T) {
	mustCheck(t, "fn main(): void { let x: int = -42 }")
	mustCheck(t, "fn main(): double { return -3.14 }")
}

func TestUnaryNot(t *testing.T) {
	mustCheck(t, "fn main(): bool { return !true }")
}

// --- Type mismatches ---

func TestTypeMismatchIntPlusString(t *testing.T) {
	mustFail(t, `fn main(): void { let x: int = 1 + "a" }`, "'+' requires matching numeric or string operands")
}

func TestTypeMismatchIntPlusBool(t *testing.T) {
	mustFail(t, `fn main(): void { let x: int = 1 + true }`, "'+' requires matching numeric or string operands")
}

func TestTypeMismatchIntEqString(t *testing.T) {
	mustFail(t, `fn main(): bool { return 1 == "a" }`, "equality operators require matching types, or both numeric")
}

func TestCrossNumericEquality(t *testing.T) {
	mustCheck(t, `fn main(): bool {
		let x: int = 42
		let y: long = 42
		return x == y
	}`)
	mustCheck(t, `fn main(): bool {
		let x: int = 42
		let y: double = 42.0
		return x != y
	}`)
	mustCheck(t, `fn main(): bool {
		let x: long = 100
		let y: double = 100.0
		return x == y
	}`)
}

func TestStrictEquality(t *testing.T) {
	mustCheck(t, `fn main(): bool { return 1 === 2 }`)
	mustCheck(t, `fn main(): bool { return 1 !== 2 }`)
	mustCheck(t, `fn main(): bool { return true === false }`)
	mustCheck(t, `fn main(): bool { return "a" === "b" }`)
}

func TestStrictEqualityTypeMismatch(t *testing.T) {
	mustFail(t, `fn main(): bool {
		let x: int = 1
		let y: long = 2
		return x === y
	}`, "strict equality requires matching types")
}

func TestAssertBuiltin(t *testing.T) {
	mustCheck(t, `fn main(): void {
		assert(true)
	}`)
}

func TestAssertWrongArgType(t *testing.T) {
	mustFail(t, `fn main(): void {
		assert(42)
	}`, "assert() argument must be bool")
}

func TestAssertWrongArgCount(t *testing.T) {
	mustFail(t, `fn main(): void {
		assert(true, false)
	}`, "assert() takes exactly 1 argument")
}

func TestTypeMismatchArithBool(t *testing.T) {
	mustFail(t, `fn main(): void { let x: int = true - false }`, "arithmetic operators require numeric operands")
}

func TestTypeMismatchModDouble(t *testing.T) {
	mustFail(t, `fn main(): double { return 1.0 % 2.0 }`, "'%' requires char, int, or long operands")
}

func TestTypeMismatchLogicalInt(t *testing.T) {
	mustFail(t, `fn main(): bool { return 1 && 2 }`, "logical operators require bool operands")
}

func TestTypeMismatchUnaryNegBool(t *testing.T) {
	mustFail(t, `fn main(): void { let x: int = -true }`, "unary '-' requires numeric operand")
}

func TestTypeMismatchUnaryNotInt(t *testing.T) {
	mustFail(t, `fn main(): bool { return !1 }`, "unary '!' requires bool operand")
}

func TestTypeMismatchCompareStringInt(t *testing.T) {
	mustFail(t, `fn main(): bool { return 1 < "a" }`, "comparison operators require numeric operands")
}

func TestCrossNumericArithmetic(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let a: int = 1
		let b: double = 2.0
		let c: double = a + b
	}`)
	mustCheck(t, `fn main(): void {
		let a: int = 1
		let b: long = 2
		let c: long = a * b
	}`)
	mustCheck(t, `fn main(): void {
		let a: double = 10.0
		let b: int = 3
		let c: double = a / b
	}`)
	mustCheck(t, `fn main(): void {
		let a: long = 100
		let b: int = 50
		let c: long = a - b
	}`)
}

func TestCrossNumericArithmeticResultType(t *testing.T) {
	mustCheck(t, `fn main(): double {
		let x: double = 5.0 / 2
		return x
	}`)
}

func TestCrossNumericComparison(t *testing.T) {
	mustCheck(t, `fn main(): bool {
		let a: int = 1
		let b: double = 2.0
		return a < b
	}`)
	mustCheck(t, `fn main(): bool {
		let a: long = 100
		let b: int = 50
		return a > b
	}`)
}

func TestCrossNumericMod(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let a: int = 10
		let b: long = 3
		let c: long = a % b
	}`)
	mustFail(t, `fn main(): void {
		let a: int = 10
		let b: double = 3.0
		let c: double = a % b
	}`, "'%' requires char, int, or long operands")
}

// --- Variables ---

func TestLetAndAssign(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let x: int = 1
		x = 2
	}`)
}

func TestUndefinedVariable(t *testing.T) {
	mustFail(t, "fn main(): void { let y: int = x }", "undefined variable 'x'")
}

func TestAssignTypeMismatch(t *testing.T) {
	mustFail(t, `fn main(): void {
		let x: int = 1
		x = "hello"
	}`, "type mismatch in assignment")
}

func TestAssignUndefined(t *testing.T) {
	mustFail(t, `fn main(): void {
		x = 1
	}`, "undefined variable 'x'")
}

func TestLetTypeMismatch(t *testing.T) {
	mustFail(t, `fn main(): void {
		let x: int = "hello"
	}`, "type mismatch in let")
}

// --- Arrays ---

func TestArrayLiteral(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let a: int[] = [1, 2, 3]
	}`)
}

func TestArrayIndexing(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let a: int[] = [10, 20, 30]
		let x: int = a[1]
	}`)
}

func TestArrayIndexAssign(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let a: int[] = [1, 2, 3]
		a[0] = 99
	}`)
}

func TestArrayPush(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let a: int[] = [1, 2]
		a.push(3)
	}`)
}

func TestArrayLen(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let a: int[] = [1, 2, 3]
		let n: int = a.len()
	}`)
}

func TestEmptyArrayLiteral(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let a: int[] = []
	}`)
}

func TestEmptyArrayNonArrayType(t *testing.T) {
	mustFail(t, `fn main(): void {
		let x: int = []
	}`, "cannot assign empty array literal to non-array type")
}

func TestArrayMixedTypes(t *testing.T) {
	mustFail(t, `fn main(): int {
		let a: int[] = [1, "hello"]
		return 0
	}`, "array elements must be the same type")
}

func TestArrayPushWrongType(t *testing.T) {
	mustFail(t, `fn main(): int {
		let a: int[] = [1, 2]
		a.push("hello")
		return 0
	}`, "push() argument must be int")
}

func TestArrayIndexNonInt(t *testing.T) {
	mustFail(t, `fn main(): int {
		let a: int[] = [1, 2]
		return a[true]
	}`, "array index must be int")
}

// --- Functions ---

func TestFunctionCall(t *testing.T) {
	mustCheck(t, `
		fn add(a: int, b: int): int { return a + b }
		fn main(): int { return add(1, 2) }
	`)
}

func TestFunctionCallWrongArgCount(t *testing.T) {
	mustFail(t, `
		fn add(a: int, b: int): int { return a + b }
		fn main(): int { return add(1) }
	`, "expects 2 arguments, got 1")
}

func TestFunctionCallWrongArgType(t *testing.T) {
	mustFail(t, `
		fn add(a: int, b: int): int { return a + b }
		fn main(): int { return add(1, "x") }
	`, "expected int, got string")
}

func TestReturnTypeMismatch(t *testing.T) {
	mustFail(t, `fn main(): int { return "hello" }`, "return type mismatch")
}

func TestUndefinedFunction(t *testing.T) {
	mustFail(t, `fn main(): int { return foo() }`, "undefined function 'foo'")
}

// --- Control flow ---

func TestIfRequiresBool(t *testing.T) {
	mustFail(t, `fn main(): int {
		if (1) { return 1 }
		return 0
	}`, "if condition must be bool")
}

func TestWhileRequiresBool(t *testing.T) {
	mustFail(t, `fn main(): int {
		while (1) { return 1 }
		return 0
	}`, "while condition must be bool")
}

func TestScoping(t *testing.T) {
	// Variable defined in if-then block should not be visible outside
	mustFail(t, `fn main(): int {
		if (true) {
			let x: int = 1
		}
		return x
	}`, "undefined variable 'x'")
}

// --- Imports ---

func TestValidImport(t *testing.T) {
	mustCheck(t, `import "fmt" fn main(): int {
		fmt.print(42)
		return 0
	}`)
}

func TestInvalidImport(t *testing.T) {
	mustFail(t, `import "nonexistent" fn main(): int { return 0 }`, "unknown import 'nonexistent'")
}

func TestModuleNotImported(t *testing.T) {
	mustFail(t, `fn main(): int {
		fmt.print(42)
		return 0
	}`, "module 'fmt' is not imported")
}

// --- Stdlib calls ---

func TestFmtPrint(t *testing.T) {
	mustCheck(t, `import "fmt" fn main(): int {
		fmt.print(42)
		return 0
	}`)
}

func TestFmtPrintStr(t *testing.T) {
	mustCheck(t, `import "fmt" fn main(): int {
		fmt.print("hello")
		return 0
	}`)
}

func TestFmtPrintLong(t *testing.T) {
	mustCheck(t, `import "fmt" fn main(): int {
		let l: long = 100
		fmt.print(l)
		return 0
	}`)
}

func TestFmtPrintDouble(t *testing.T) {
	mustCheck(t, `import "fmt" fn main(): int {
		fmt.print(3.14)
		return 0
	}`)
}

func TestFmtPrintBool(t *testing.T) {
	mustCheck(t, `import "fmt" fn main(): int {
		fmt.print(true)
		return 0
	}`)
}

func TestFmtPrintWrongArgCount(t *testing.T) {
	mustFail(t, `import "fmt" fn main(): int {
		fmt.print(1, 2)
		return 0
	}`, "fmt.print() takes exactly 1 argument")
}

func TestJsonNew(t *testing.T) {
	mustCheck(t, `import "json" fn main(): string {
		return json.new()
	}`)
}

func TestJsonSet(t *testing.T) {
	mustCheck(t, `import "json" fn main(): string {
		let obj: string = json.new()
		return json.set(obj, "key", "value")
	}`)
}

func TestJsonStringify(t *testing.T) {
	mustCheck(t, `import "json" fn main(): string {
		let a: int[] = [1, 2, 3]
		return json.stringify(a)
	}`)
}

func TestJsonStringifyNonArray(t *testing.T) {
	mustFail(t, `import "json" fn main(): string {
		return json.stringify(42)
	}`, "json.stringify() argument must be an array or struct type")
}

func TestJsonSetArr(t *testing.T) {
	mustCheck(t, `import "json" fn main(): string {
		let obj: string = json.new()
		let a: int[] = [1, 2]
		return json.setArray(obj, "nums", a)
	}`)
}

func TestHttpRoute(t *testing.T) {
	mustCheck(t, `import "http" fn handler(): string { return "ok" }
		fn main(): int {
			http.route("GET", "/", "handler")
			return 0
		}
	`)
}

func TestHttpRouteInvalidHandler(t *testing.T) {
	mustFail(t, `import "http" fn main(): int {
		http.route("GET", "/", "nonexistent")
		return 0
	}`, "handler 'nonexistent' is not a defined function")
}

func TestHttpRouteHandlerWrongSignature(t *testing.T) {
	mustFail(t, `import "http" fn handler(x: int): string { return "ok" }
		fn main(): int {
			http.route("GET", "/", "handler")
			return 0
		}
	`, "handler 'handler' parameter must be http.HttpRequest, got int")
}

func TestHttpRouteHandlerNonStringReturn(t *testing.T) {
	mustCheck(t, `import "http" fn handler(): int { return 0 }
		fn main(): int {
			http.route("GET", "/", "handler")
			return 0
		}
	`)
}

// --- Array methods ---

func TestArraySort(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let a: int[] = [3, 1, 2]
		a.sort("asc")
	}`)
	mustCheck(t, `fn main(): void {
		let a: string[] = ["c", "a", "b"]
		a.sort("desc")
	}`)
}

func TestArraySortInvalidArg(t *testing.T) {
	mustFail(t, `fn main(): void {
		let a: int[] = [3, 1, 2]
		a.sort(42)
	}`, "sort() argument must be string")
}

func TestArraySortBoolNotAllowed(t *testing.T) {
	mustFail(t, `fn main(): void {
		let a: bool[] = [true, false]
		a.sort("asc")
	}`, "sort() is not supported on bool arrays")
}

func TestArrayPop(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let a: int[] = [1, 2, 3]
		let x: int = a.pop()
	}`)
}

func TestArrayPopWithArgs(t *testing.T) {
	mustFail(t, `fn main(): void {
		let a: int[] = [1, 2, 3]
		a.pop(1)
	}`, "pop() takes no arguments")
}

func TestArrayRemove(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let a: int[] = [1, 2, 3]
		a.remove(0)
	}`)
}

func TestArrayRemoveNonInt(t *testing.T) {
	mustFail(t, `fn main(): void {
		let a: int[] = [1, 2, 3]
		a.remove("hello")
	}`, "remove() argument must be int")
}

func TestArrayContains(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let a: int[] = [1, 2, 3]
		let b: bool = a.contains(2)
	}`)
}

func TestArrayContainsWrongType(t *testing.T) {
	mustFail(t, `fn main(): void {
		let a: int[] = [1, 2, 3]
		a.contains("hello")
	}`, "contains() argument must be int")
}

func TestArrayIndexOf(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let a: string[] = ["a", "b", "c"]
		let i: int = a.indexOf("b")
	}`)
}

func TestArrayIndexOfWrongType(t *testing.T) {
	mustFail(t, `fn main(): void {
		let a: string[] = ["a", "b"]
		a.indexOf(42)
	}`, "indexOf() argument must be string")
}

func TestArrayReverse(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let a: int[] = [1, 2, 3]
		a.reverse()
	}`)
}

func TestArrayReverseWithArgs(t *testing.T) {
	mustFail(t, `fn main(): void {
		let a: int[] = [1, 2, 3]
		a.reverse(1)
	}`, "reverse() takes no arguments")
}

func TestUndefinedStdlibFunc(t *testing.T) {
	mustFail(t, `import "fmt" fn main(): int {
		fmt.nonexistent(42)
		return 0
	}`, "undefined function 'nonexistent' in module 'fmt'")
}

// --- Break and Continue ---

func TestBreakInWhile(t *testing.T) {
	mustCheck(t, `fn main(): void {
		while (true) {
			break
		}
	}`)
}

func TestContinueInWhile(t *testing.T) {
	mustCheck(t, `fn main(): void {
		while (true) {
			continue
		}
	}`)
}

func TestBreakOutsideLoop(t *testing.T) {
	mustFail(t, `fn main(): void {
		break
	}`, "'break' outside of loop")
}

func TestContinueOutsideLoop(t *testing.T) {
	mustFail(t, `fn main(): void {
		continue
	}`, "'continue' outside of loop")
}

func TestBreakInFor(t *testing.T) {
	mustCheck(t, `fn main(): void {
		for(let i: int = 0; i < 10; i++) {
			break
		}
	}`)
}

func TestContinueInForeach(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let a: int[] = [1, 2, 3]
		foreach(a as n) {
			continue
		}
	}`)
}

// --- For loop ---

func TestForLoop(t *testing.T) {
	mustCheck(t, `fn main(): void {
		for(let i: int = 0; i < 10; i++) {
			let x: int = i
		}
	}`)
}

func TestForLoopCondMustBeBool(t *testing.T) {
	mustFail(t, `fn main(): void {
		for(let i: int = 0; i + 1; i++) { }
	}`, "for condition must be bool")
}

func TestForLoopWithDecrement(t *testing.T) {
	mustCheck(t, `fn main(): void {
		for(let i: int = 10; i > 0; i--) { }
	}`)
}

func TestForLoopWithCompoundAssign(t *testing.T) {
	mustCheck(t, `fn main(): void {
		for(let i: int = 0; i < 100; i += 2) { }
	}`)
}

func TestForLoopScopeIsolation(t *testing.T) {
	mustFail(t, `fn main(): int {
		for(let i: int = 0; i < 10; i++) { }
		return i
	}`, "undefined variable 'i'")
}

// --- Foreach ---

func TestForeachValueOnly(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let a: int[] = [1, 2, 3]
		foreach(a as n) {
			let x: int = n
		}
	}`)
}

func TestForeachWithIndex(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let a: string[] = ["a", "b", "c"]
		foreach(a as i, s) {
			let idx: int = i
			let val: string = s
		}
	}`)
}

func TestForeachNonArray(t *testing.T) {
	mustFail(t, `fn main(): void {
		let x: int = 42
		foreach(x as n) { }
	}`, "foreach requires an array type")
}

// --- Increment/Decrement/CompoundAssign ---

func TestIncrementNumeric(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let x: int = 0
		x++
	}`)
}

func TestIncrementNonNumeric(t *testing.T) {
	mustFail(t, `fn main(): void {
		let x: string = "hello"
		x++
	}`, "'++' requires numeric variable")
}

func TestDecrementNumeric(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let x: int = 10
		x--
	}`)
}

func TestDecrementNonNumeric(t *testing.T) {
	mustFail(t, `fn main(): void {
		let x: bool = true
		x--
	}`, "'--' requires numeric variable")
}

func TestCompoundAssignAdd(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let x: int = 0
		x += 5
	}`)
}

func TestCompoundAssignSub(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let x: int = 10
		x -= 3
	}`)
}

func TestCompoundAssignNonNumeric(t *testing.T) {
	mustFail(t, `fn main(): void {
		let x: string = "hello"
		x += 1
	}`, "compound assignment requires numeric variable")
}

func TestCompoundAssignUndefined(t *testing.T) {
	mustFail(t, `fn main(): void {
		x += 1
	}`, "undefined variable 'x'")
}

// --- Type inference ---

func TestTypeInferenceInt(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let x = 42
		let y: int = x
	}`)
}

func TestTypeInferenceDouble(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let x = 3.14
		let y: double = x
	}`)
}

func TestTypeInferenceString(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let x = "hello"
		let y: string = x
	}`)
}

func TestTypeInferenceBool(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let x = true
		let y: bool = x
	}`)
}

func TestTypeInferenceArray(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let x = [1, 2, 3]
		let y: int = x[0]
	}`)
}

func TestTypeInferenceEmptyArrayFails(t *testing.T) {
	mustFail(t, `fn main(): void {
		let x = []
	}`, "cannot infer type of empty array literal")
}

func TestTypeInferenceExpression(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let a: int = 5
		let b = a + 3
		let c: int = b
	}`)
}

func TestTypeInferenceInForLoop(t *testing.T) {
	mustCheck(t, `fn main(): void {
		for(let i = 0; i < 10; i++) {
			let x: int = i
		}
	}`)
}

// --- HTTP Client ---

func TestHttpGet(t *testing.T) {
	mustCheck(t, `import "http"
fn main(): void {
	let resp: HttpResponse = http.get("https://example.com")
}`)
}

func TestHttpGetFieldAccess(t *testing.T) {
	mustCheck(t, `import "http"
import "fmt"
fn main(): void {
	let resp: HttpResponse = http.get("https://example.com")
	fmt.print(resp.statusCode)
	fmt.print(resp.body)
}`)
}

func TestHttpPost(t *testing.T) {
	mustCheck(t, `import "http"
fn main(): void {
	let resp: HttpResponse = http.post("https://example.com", "{}")
}`)
}

func TestHttpPut(t *testing.T) {
	mustCheck(t, `import "http"
fn main(): void {
	let resp: HttpResponse = http.put("https://example.com/1", "{}")
}`)
}

func TestHttpPatch(t *testing.T) {
	mustCheck(t, `import "http"
fn main(): void {
	let resp: HttpResponse = http.patch("https://example.com/1", "{}")
}`)
}

func TestHttpDelete(t *testing.T) {
	mustCheck(t, `import "http"
fn main(): void {
	let resp: HttpResponse = http.delete("https://example.com/1")
}`)
}

func TestHttpRequest(t *testing.T) {
	mustCheck(t, `import "http"
fn main(): void {
	let resp: HttpResponse = http.request("POST", "https://example.com", "{}", "Authorization: Bearer token")
}`)
}

func TestHttpFormFunctions(t *testing.T) {
	mustCheck(t, `import "http"
fn main(): void {
	let form: string = http.formNew()
	form = http.formField(form, "name", "Alice")
	form = http.formFile(form, "avatar", "/path/to/file.jpg")
	let resp: HttpResponse = http.postForm("https://example.com/upload", form)
}`)
}

func TestHttpGetWrongArgs(t *testing.T) {
	mustFail(t, `import "http"
fn main(): void {
	let resp: HttpResponse = http.get(123)
}`, "http.get() argument 1 must be string")
}

func TestHttpGetTooManyArgs(t *testing.T) {
	mustFail(t, `import "http"
fn main(): void {
	let resp: HttpResponse = http.get("a", "b", "c")
}`, "http.get() takes 1-2 arguments")
}

func TestHttpPostWrongArgCount(t *testing.T) {
	mustFail(t, `import "http"
fn main(): void {
	let resp: HttpResponse = http.post("url")
}`, "http.post() takes 2-3 arguments")
}

func TestHttpFormNewWrongArgs(t *testing.T) {
	mustFail(t, `import "http"
fn main(): void {
	let form: string = http.formNew("extra")
}`, "http.formNew() takes no arguments")
}

func TestHttpFormFieldWrongArgCount(t *testing.T) {
	mustFail(t, `import "http"
fn main(): void {
	let form: string = http.formField("a", "b")
}`, "http.formField() takes exactly 3 arguments")
}
