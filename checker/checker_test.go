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
	ast.ResetInterfaceTypes()
	stdlib.RegisterAllModuleTypes()
	ast.RegisterExceptionType()

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
	prog, parseErrs := p.Parse()
	if len(parseErrs) > 0 {
		t.Fatalf("parser error: %v", parseErrs[0])
	}
	checkErrs := New().Check(prog)
	if len(checkErrs) > 0 {
		return checkErrs[0]
	}
	return nil
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
	mustFail(t, `fn main(): void { let x: int = 1 + "a" }`, "type mismatch in let: expected int, got string")
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
	`, "missing argument for required parameter")
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
		fmt.println(42)
		return 0
	}`)
}

func TestInvalidImport(t *testing.T) {
	mustFail(t, `import "nonexistent" fn main(): int { return 0 }`, "unknown import 'nonexistent'")
}

func TestModuleNotImported(t *testing.T) {
	mustFail(t, `fn main(): int {
		fmt.println(42)
		return 0
	}`, "module 'fmt' is not imported")
}

// --- Stdlib calls ---

func TestFmtPrint(t *testing.T) {
	mustCheck(t, `import "fmt" fn main(): int {
		fmt.println(42)
		return 0
	}`)
}

func TestFmtPrintStr(t *testing.T) {
	mustCheck(t, `import "fmt" fn main(): int {
		fmt.println("hello")
		return 0
	}`)
}

func TestFmtPrintLong(t *testing.T) {
	mustCheck(t, `import "fmt" fn main(): int {
		let l: long = 100
		fmt.println(l)
		return 0
	}`)
}

func TestFmtPrintDouble(t *testing.T) {
	mustCheck(t, `import "fmt" fn main(): int {
		fmt.println(3.14)
		return 0
	}`)
}

func TestFmtPrintBool(t *testing.T) {
	mustCheck(t, `import "fmt" fn main(): int {
		fmt.println(true)
		return 0
	}`)
}

func TestFmtPrintWrongArgCount(t *testing.T) {
	mustFail(t, `import "fmt" fn main(): int {
		fmt.println(1, 2)
		return 0
	}`, "fmt.println() takes exactly 1 argument")
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
		return json.encode(a)
	}`)
}

func TestJsonStringifyNonArray(t *testing.T) {
	mustFail(t, `import "json" fn main(): string {
		return json.encode(42)
	}`, "json.encode() argument must be a json.Value, array, struct, or map")
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
	fmt.println(resp.statusCode)
	fmt.println(resp.body)
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

// --- Route parameters: chained field.method() access on HttpRequest.params ---

func TestHttpRouteParamsGet(t *testing.T) {
	mustCheck(t, `import "http"
fn handler(req: http.HttpRequest): http.HttpResponse {
	let id: string = req.params.get("id")
	return http.response(200, id, "text/plain")
}
fn main(): void {
	http.route("GET", "/users/:id", handler)
}`)
}

func TestHttpRouteParamsHas(t *testing.T) {
	mustCheck(t, `import "http"
fn handler(req: http.HttpRequest): http.HttpResponse {
	let exists: bool = req.params.has("id")
	return http.response(200, "ok", "text/plain")
}
fn main(): void {
	http.route("GET", "/users/:id", handler)
}`)
}

func TestHttpRouteParamsSet(t *testing.T) {
	mustCheck(t, `import "http"
fn handler(req: http.HttpRequest): http.HttpResponse {
	req.params.set("key", "value")
	return http.response(200, "ok", "text/plain")
}
fn main(): void {
	http.route("GET", "/test", handler)
}`)
}

func TestHttpRouteParamsLen(t *testing.T) {
	mustCheck(t, `import "http"
fn handler(req: http.HttpRequest): http.HttpResponse {
	let n: int = req.params.len()
	return http.response(200, "ok", "text/plain")
}
fn main(): void {
	http.route("GET", "/test", handler)
}`)
}

func TestHttpRouteParamsGetWrongKeyType(t *testing.T) {
	mustFail(t, `import "http"
fn handler(req: http.HttpRequest): http.HttpResponse {
	let id: string = req.params.get(42)
	return http.response(200, id, "text/plain")
}
fn main(): void {
	http.route("GET", "/users/:id", handler)
}`, "key must be string, got int")
}

func TestHttpRouteParamsSetWrongValueType(t *testing.T) {
	mustFail(t, `import "http"
fn handler(req: http.HttpRequest): http.HttpResponse {
	req.params.set("key", 42)
	return http.response(200, "ok", "text/plain")
}
fn main(): void {
	http.route("GET", "/test", handler)
}`, "value must be string, got int")
}

func TestHttpRouteParamsGetReturnsString(t *testing.T) {
	// params.get() returns string — assigning to int should fail
	mustFail(t, `import "http"
fn handler(req: http.HttpRequest): http.HttpResponse {
	let id: int = req.params.get("id")
	return http.response(200, "ok", "text/plain")
}
fn main(): void {
	http.route("GET", "/users/:id", handler)
}`, "type mismatch in let: expected int, got string")
}

func TestHttpRouteParamsMultiple(t *testing.T) {
	mustCheck(t, `import "http"
fn handler(req: http.HttpRequest): http.HttpResponse {
	let postId: string = req.params.get("postId")
	let commentId: string = req.params.get("commentId")
	return http.response(200, postId, "text/plain")
}
fn main(): void {
	http.route("GET", "/posts/:postId/comments/:commentId", handler)
}`)
}

func TestHttpRouteWithParamsAndExistingFields(t *testing.T) {
	// Verify that existing fields (method, path, body, query) still work alongside params
	mustCheck(t, `import "http"
fn handler(req: http.HttpRequest): http.HttpResponse {
	let m: string = req.method
	let p: string = req.path
	let b: string = req.body
	let q: string = req.query
	let id: string = req.params.get("id")
	return http.response(200, b, "text/plain")
}
fn main(): void {
	http.route("POST", "/items/:id", handler)
}`)
}

func TestHttpRouteNoParamsStillWorks(t *testing.T) {
	// Routes without :param segments should still compile
	mustCheck(t, `import "http"
fn handler(req: http.HttpRequest): http.HttpResponse {
	return http.response(200, req.body, "text/plain")
}
fn main(): void {
	http.route("GET", "/static/path", handler)
}`)
}

func TestHttpRouteZeroParamHandlerStillWorks(t *testing.T) {
	// Backward-compatible: 0-param handlers still work
	mustCheck(t, `import "http"
fn handler(): string {
	return "hello"
}
fn main(): void {
	http.route("GET", "/hello", handler)
}`)
}

// =============================================================================
// String methods
// =============================================================================

func TestStringMethodLen(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let s: string = "hello"
		let n: int = s.len()
	}`)
}

func TestStringMethodContains(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let s: string = "hello"
		let b: bool = s.contains("ell")
	}`)
}

func TestStringMethodStartsWith(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let s: string = "hello"
		let b: bool = s.startsWith("he")
	}`)
}

func TestStringMethodEndsWith(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let s: string = "hello"
		let b: bool = s.endsWith("lo")
	}`)
}

func TestStringMethodIndexOf(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let s: string = "hello"
		let i: int = s.indexOf("ll")
	}`)
}

func TestStringMethodToLower(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let s: string = "HELLO"
		let lower: string = s.toLower()
	}`)
}

func TestStringMethodToUpper(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let s: string = "hello"
		let upper: string = s.toUpper()
	}`)
}

func TestStringMethodTrim(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let s: string = "  hello  "
		let trimmed: string = s.trim()
	}`)
}

func TestStringMethodSubstring(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let s: string = "hello"
		let sub: string = s.substring(1, 3)
	}`)
}

func TestStringMethodReplace(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let s: string = "hello"
		let replaced: string = s.replace("l", "r")
	}`)
}

func TestStringMethodCharAt(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let s: string = "hello"
		let ch: char = s.charAt(0)
	}`)
}

func TestStringMethodSplit(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let s: string = "a,b,c"
		let parts: string[] = s.split(",")
	}`)
}

func TestStringMethodIsAlphanumeric(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let s: string = "abc123"
		let b: bool = s.isAlphanumeric()
	}`)
}

func TestStringMethodIsAlpha(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let s: string = "abc"
		let b: bool = s.isAlpha()
	}`)
}

func TestStringMethodIsDigit(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let s: string = "123"
		let b: bool = s.isDigit()
	}`)
}

func TestStringMethodIsEmpty(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let s: string = ""
		let b: bool = s.isEmpty()
	}`)
}

func TestStringMethodContainsUppercase(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let s: string = "Hello"
		let b: bool = s.containsUppercase()
	}`)
}

func TestStringMethodAllMethods(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let s: string = "hello world"
		let n: int = s.len()
		let b: bool = s.contains("ell")
		let b2: bool = s.startsWith("he")
		let b3: bool = s.endsWith("ld")
		let i: int = s.indexOf("ll")
		let lower: string = s.toLower()
		let upper: string = s.toUpper()
		let trimmed: string = s.trim()
		let sub: string = s.substring(1, 3)
		let replaced: string = s.replace("l", "r")
		let ch: char = s.charAt(0)
		let parts: string[] = s.split(",")
	}`)
}

// --- String method error cases ---

func TestStringMethodLenWithArgs(t *testing.T) {
	mustFail(t, `fn main(): void {
		let s: string = "hello"
		let n: int = s.len(1)
	}`, "len() takes no arguments")
}

func TestStringMethodContainsWrongType(t *testing.T) {
	mustFail(t, `fn main(): void {
		let s: string = "hello"
		let b: bool = s.contains(42)
	}`, "contains() argument must be string, got int")
}

func TestStringMethodStartsWithWrongType(t *testing.T) {
	mustFail(t, `fn main(): void {
		let s: string = "hello"
		let b: bool = s.startsWith(42)
	}`, "startsWith() argument must be string, got int")
}

func TestStringMethodEndsWithWrongArgCount(t *testing.T) {
	mustFail(t, `fn main(): void {
		let s: string = "hello"
		let b: bool = s.endsWith("a", "b")
	}`, "endsWith() takes exactly 1 argument")
}

func TestStringMethodIndexOfWrongType(t *testing.T) {
	mustFail(t, `fn main(): void {
		let s: string = "hello"
		let i: int = s.indexOf(42)
	}`, "indexOf() argument must be string, got int")
}

func TestStringMethodToLowerWithArgs(t *testing.T) {
	mustFail(t, `fn main(): void {
		let s: string = "hello"
		let r: string = s.toLower("x")
	}`, "toLower() takes no arguments")
}

func TestStringMethodToUpperWithArgs(t *testing.T) {
	mustFail(t, `fn main(): void {
		let s: string = "hello"
		let r: string = s.toUpper("x")
	}`, "toUpper() takes no arguments")
}

func TestStringMethodTrimWithArgs(t *testing.T) {
	mustFail(t, `fn main(): void {
		let s: string = "hello"
		let r: string = s.trim("x")
	}`, "trim() takes no arguments")
}

func TestStringMethodSubstringWrongArgCount(t *testing.T) {
	mustFail(t, `fn main(): void {
		let s: string = "hello"
		let r: string = s.substring(1)
	}`, "substring() takes exactly 2 arguments")
}

func TestStringMethodSubstringWrongType(t *testing.T) {
	mustFail(t, `fn main(): void {
		let s: string = "hello"
		let r: string = s.substring("a", 3)
	}`, "substring() argument 1 must be int, got string")
}

func TestStringMethodReplaceWrongArgCount(t *testing.T) {
	mustFail(t, `fn main(): void {
		let s: string = "hello"
		let r: string = s.replace("a")
	}`, "replace() takes exactly 2 arguments")
}

func TestStringMethodReplaceWrongType(t *testing.T) {
	mustFail(t, `fn main(): void {
		let s: string = "hello"
		let r: string = s.replace("a", 42)
	}`, "replace() argument 2 must be string, got int")
}

func TestStringMethodCharAtWrongType(t *testing.T) {
	mustFail(t, `fn main(): void {
		let s: string = "hello"
		let ch: char = s.charAt("x")
	}`, "charAt() argument must be int, got string")
}

func TestStringMethodCharAtWrongArgCount(t *testing.T) {
	mustFail(t, `fn main(): void {
		let s: string = "hello"
		let ch: char = s.charAt(0, 1)
	}`, "charAt() takes exactly 1 argument")
}

func TestStringMethodSplitWrongType(t *testing.T) {
	mustFail(t, `fn main(): void {
		let s: string = "a,b,c"
		let parts: string[] = s.split(42)
	}`, "split() argument must be string, got int")
}

func TestStringMethodUndefined(t *testing.T) {
	mustFail(t, `fn main(): void {
		let s: string = "hello"
		s.nonexistent()
	}`, "undefined method 'nonexistent' on string type")
}

// =============================================================================
// StringBuilder methods
// =============================================================================

func TestStringBuilderCreate(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let sb: StringBuilder = StringBuilder()
	}`)
}

func TestStringBuilderAppendString(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let sb: StringBuilder = StringBuilder()
		sb.append("hello")
	}`)
}

func TestStringBuilderAppendInt(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let sb: StringBuilder = StringBuilder()
		sb.append(42)
	}`)
}

func TestStringBuilderAppendBool(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let sb: StringBuilder = StringBuilder()
		sb.append(true)
	}`)
}

func TestStringBuilderAppendDouble(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let sb: StringBuilder = StringBuilder()
		sb.append(3.14)
	}`)
}

func TestStringBuilderLen(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let sb: StringBuilder = StringBuilder()
		let n: int = sb.len()
	}`)
}

func TestStringBuilderToString(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let sb: StringBuilder = StringBuilder()
		sb.append("hello")
		let s: string = sb.toString()
	}`)
}

func TestStringBuilderClear(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let sb: StringBuilder = StringBuilder()
		sb.append("hello")
		sb.clear()
	}`)
}

func TestStringBuilderAllMethods(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let sb: StringBuilder = StringBuilder()
		sb.append("hello")
		sb.append(42)
		sb.append(3.14)
		sb.append(true)
		let n: int = sb.len()
		let s: string = sb.toString()
		sb.clear()
	}`)
}

// --- StringBuilder error cases ---

func TestStringBuilderAppendWrongArgCount(t *testing.T) {
	mustFail(t, `fn main(): void {
		let sb: StringBuilder = StringBuilder()
		sb.append("a", "b")
	}`, "append() takes exactly 1 argument")
}

func TestStringBuilderLenWithArgs(t *testing.T) {
	mustFail(t, `fn main(): void {
		let sb: StringBuilder = StringBuilder()
		sb.len(1)
	}`, "len() takes no arguments")
}

func TestStringBuilderToStringWithArgs(t *testing.T) {
	mustFail(t, `fn main(): void {
		let sb: StringBuilder = StringBuilder()
		sb.toString(1)
	}`, "toString() takes no arguments")
}

func TestStringBuilderClearWithArgs(t *testing.T) {
	mustFail(t, `fn main(): void {
		let sb: StringBuilder = StringBuilder()
		sb.clear(1)
	}`, "clear() takes no arguments")
}

func TestStringBuilderUndefinedMethod(t *testing.T) {
	mustFail(t, `fn main(): void {
		let sb: StringBuilder = StringBuilder()
		sb.nonexistent()
	}`, "undefined method 'nonexistent' on StringBuilder type")
}

// =============================================================================
// Map methods (remove, clear, keys, values, len)
// =============================================================================

func TestMapRemove(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let m: map[string, int] = {}
		m.set("a", 1)
		m.remove("a")
	}`)
}

func TestMapClear(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let m: map[string, int] = {}
		m.set("a", 1)
		m.clear()
	}`)
}

func TestMapKeys(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let m: map[string, int] = {}
		m.set("a", 1)
		let k: string[] = m.keys()
	}`)
}

func TestMapValues(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let m: map[string, int] = {}
		m.set("a", 1)
		let v: int[] = m.values()
	}`)
}

func TestMapLen(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let m: map[string, int] = {}
		let n: int = m.len()
	}`)
}

func TestMapAllMethods(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let m: map[string, int] = {}
		m.set("a", 1)
		m.set("b", 2)
		let v: int = m.get("a")
		let b: bool = m.has("a")
		m.remove("a")
		let k: string[] = m.keys()
		let vals: int[] = m.values()
		let n: int = m.len()
		m.clear()
	}`)
}

func TestMapIntKeyMethods(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let m: map[int, string] = {}
		m.set(1, "hello")
		m.remove(1)
		let k: int[] = m.keys()
		let v: string[] = m.values()
		m.clear()
	}`)
}

// --- Map method error cases ---

func TestMapRemoveWrongKeyType(t *testing.T) {
	mustFail(t, `fn main(): void {
		let m: map[string, int] = {}
		m.remove(42)
	}`, "remove() key must be string, got int")
}

func TestMapRemoveWrongArgCount(t *testing.T) {
	mustFail(t, `fn main(): void {
		let m: map[string, int] = {}
		m.remove("a", "b")
	}`, "remove() takes exactly 1 argument")
}

func TestMapClearWithArgs(t *testing.T) {
	mustFail(t, `fn main(): void {
		let m: map[string, int] = {}
		m.clear(1)
	}`, "clear() takes no arguments")
}

func TestMapKeysWithArgs(t *testing.T) {
	mustFail(t, `fn main(): void {
		let m: map[string, int] = {}
		m.keys(1)
	}`, "keys() takes no arguments")
}

func TestMapValuesWithArgs(t *testing.T) {
	mustFail(t, `fn main(): void {
		let m: map[string, int] = {}
		m.values(1)
	}`, "values() takes no arguments")
}

func TestMapLenWithArgs(t *testing.T) {
	mustFail(t, `fn main(): void {
		let m: map[string, int] = {}
		m.len(1)
	}`, "len() takes no arguments")
}

func TestMapUndefinedMethod(t *testing.T) {
	mustFail(t, `fn main(): void {
		let m: map[string, int] = {}
		m.nonexistent()
	}`, "undefined method 'nonexistent' on map type")
}

// =============================================================================
// Const declarations
// =============================================================================

func TestConstInt(t *testing.T) {
	mustCheck(t, `fn main(): void {
		const x: int = 42
		let y: int = x
	}`)
}

func TestConstDouble(t *testing.T) {
	mustCheck(t, `fn main(): void {
		const PI: double = 3.14
		let y: double = PI
	}`)
}

func TestConstString(t *testing.T) {
	mustCheck(t, `fn main(): void {
		const NAME: string = "hello"
		let y: string = NAME
	}`)
}

func TestConstBool(t *testing.T) {
	mustCheck(t, `fn main(): void {
		const flag: bool = true
		let y: bool = flag
	}`)
}

func TestConstLong(t *testing.T) {
	mustCheck(t, `fn main(): void {
		const big: long = 1000000
		let y: long = big
	}`)
}

func TestConstInferred(t *testing.T) {
	mustCheck(t, `fn main(): void {
		const x = 42
		let y: int = x
	}`)
}

func TestConstCannotReassign(t *testing.T) {
	mustFail(t, `fn main(): void {
		const x: int = 42
		x = 10
	}`, "cannot reassign const variable 'x'")
}

func TestConstCannotIncrement(t *testing.T) {
	mustFail(t, `fn main(): void {
		const x: int = 42
		x++
	}`, "cannot modify const variable 'x'")
}

func TestConstCannotDecrement(t *testing.T) {
	mustFail(t, `fn main(): void {
		const x: int = 42
		x--
	}`, "cannot modify const variable 'x'")
}

func TestConstCannotCompoundAssign(t *testing.T) {
	mustFail(t, `fn main(): void {
		const x: int = 42
		x += 1
	}`, "cannot modify const variable 'x'")
}

// =============================================================================
// Struct definitions
// =============================================================================

func TestStructLiteral(t *testing.T) {
	mustCheck(t, `
struct Point {
	x: int
	y: int
}
fn main(): void {
	let p = Point { x: 1, y: 2 }
	let px: int = p.x
	let py: int = p.y
}`)
}

func TestStructFieldAccess(t *testing.T) {
	mustCheck(t, `
struct Config {
	name: string
	value: int
}
fn main(): void {
	let cfg = Config { name: "test", value: 42 }
	let n: string = cfg.name
	let v: int = cfg.value
}`)
}

func TestStructFieldAssign(t *testing.T) {
	mustCheck(t, `
struct Point {
	x: int
	y: int
}
fn main(): void {
	let p = Point { x: 1, y: 2 }
	p.x = 10
	p.y = 20
}`)
}

func TestStructWithConstructor(t *testing.T) {
	mustCheck(t, `
struct Point(x: int, y: int) {
}
fn main(): void {
	let p = Point(3, 4)
	let px: int = p.x
}`)
}

func TestStructWithMethods(t *testing.T) {
	mustCheck(t, `
struct Point(x: int, y: int) {
	fn sum(): int {
		return x + y
	}
	fn scale(factor: int): int {
		return x * factor + y * factor
	}
}
fn main(): void {
	let p = Point(3, 4)
	let s: int = p.sum()
	let sc: int = p.scale(2)
}`)
}

func TestStructDuplicateField(t *testing.T) {
	mustFail(t, `
struct Bad {
	x: int
	x: int
}
fn main(): void {}`, "duplicate field 'x' in struct 'Bad'")
}

func TestStructDuplicateName(t *testing.T) {
	mustFail(t, `
struct Foo { x: int }
struct Foo { y: int }
fn main(): void {}`, "duplicate struct type 'Foo'")
}

func TestStructFieldAccessUndefined(t *testing.T) {
	mustFail(t, `
struct Point {
	x: int
	y: int
}
fn main(): void {
	let p = Point { x: 1, y: 2 }
	let z: int = p.z
}`, "struct 'Point' has no field 'z'")
}

func TestStructFieldAssignTypeMismatch(t *testing.T) {
	mustFail(t, `
struct Point {
	x: int
	y: int
}
fn main(): void {
	let p = Point { x: 1, y: 2 }
	p.x = "hello"
}`, "type mismatch in field assignment")
}

// =============================================================================
// Enum definitions
// =============================================================================

func TestEnumDefinition(t *testing.T) {
	mustCheck(t, `
enum Color {
	Red
	Green
	Blue
}
fn main(): void {
	let c: Color = Color.Red
}`)
}

func TestEnumComparison(t *testing.T) {
	mustCheck(t, `
enum Color {
	Red
	Green
	Blue
}
fn main(): void {
	let c: Color = Color.Red
	let b: bool = c == Color.Green
	let b2: bool = c != Color.Blue
}`)
}

func TestEnumDuplicateVariant(t *testing.T) {
	mustFail(t, `
enum Color {
	Red
	Red
}
fn main(): void {}`, "duplicate variant 'Red' in enum 'Color'")
}

func TestEnumUndefinedVariant(t *testing.T) {
	mustFail(t, `
enum Color {
	Red
	Green
	Blue
}
fn main(): void {
	let c: Color = Color.Yellow
}`, "enum 'Color' has no variant 'Yellow'")
}

func TestEnumInSwitch(t *testing.T) {
	mustCheck(t, `
import "fmt"
enum Direction {
	North
	South
	East
	West
}
fn main(): void {
	let d: Direction = Direction.East
	switch (d) {
		case Direction.North: {
			fmt.println("north")
		}
		case Direction.East: {
			fmt.println("east")
		}
		default: {
			fmt.println("other")
		}
	}
}`)
}

func TestEnumReassign(t *testing.T) {
	mustCheck(t, `
enum Color {
	Red
	Green
	Blue
}
fn main(): void {
	let c: Color = Color.Red
	c = Color.Blue
}`)
}

func TestEnumArray(t *testing.T) {
	mustCheck(t, `
enum Color {
	Red
	Green
	Blue
}
fn main(): void {
	let colors: Color[] = []
	colors.push(Color.Red)
	colors.push(Color.Green)
	let n: int = colors.len()
}`)
}

// =============================================================================
// Switch statements
// =============================================================================

func TestSwitchInt(t *testing.T) {
	mustCheck(t, `
import "fmt"
fn main(): void {
	let x: int = 1
	switch (x) {
		case 1: {
			fmt.println("one")
		}
		case 2: {
			fmt.println("two")
		}
		default: {
			fmt.println("other")
		}
	}
}`)
}

func TestSwitchString(t *testing.T) {
	mustCheck(t, `
import "fmt"
fn main(): void {
	let action: string = "Heartbeat"
	switch (action) {
		case "BootNotification": {
			fmt.println("boot")
		}
		case "Heartbeat", "StatusNotification": {
			fmt.println("status")
		}
		default: {
			fmt.println("unknown")
		}
	}
}`)
}

func TestSwitchChar(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let grade: char = 'B'
		let score: int = 0
		switch (grade) {
			case 'A': {
				score = 100
			}
			case 'B': {
				score = 85
			}
			default: {
				score = 0
			}
		}
	}`)
}

func TestSwitchWithoutDefault(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let x: int = 1
		switch (x) {
			case 1: {
				let y: int = 10
			}
			case 2: {
				let y: int = 20
			}
		}
	}`)
}

func TestSwitchCaseTypeMismatch(t *testing.T) {
	mustFail(t, `fn main(): void {
		let x: int = 1
		switch (x) {
			case "one": {
				let y: int = 10
			}
		}
	}`, "case value type string does not match switch tag type int")
}

// =============================================================================
// Try/catch
// =============================================================================

func TestTryCatch(t *testing.T) {
	mustCheck(t, `fn main(): void {
		try {
			let x: int = 1
		} catch (e: Exception) {
			let msg: string = e.message
		}
	}`)
}

func TestTryCatchFinally(t *testing.T) {
	mustCheck(t, `fn main(): void {
		try {
			let x: int = 1
		} catch (e: Exception) {
			let msg: string = e.message
		} finally {
			let cleanup: int = 0
		}
	}`)
}

func TestTryFinally(t *testing.T) {
	mustCheck(t, `fn main(): void {
		try {
			let x: int = 1
		} finally {
			let cleanup: int = 0
		}
	}`)
}

func TestThrow(t *testing.T) {
	mustCheck(t, `fn main(): void {
		throw Exception("something went wrong")
	}`)
}

func TestThrowNonException(t *testing.T) {
	mustFail(t, `fn main(): void {
		throw "error"
	}`, "throw requires Exception type, got string")
}

// =============================================================================
// For loop variants
// =============================================================================

func TestForLoopCompoundAssignStep(t *testing.T) {
	mustCheck(t, `fn main(): void {
		for (let i: int = 0; i < 10; i += 1) {
		}
	}`)
}

func TestForLoopCompoundAssignDoubleStep(t *testing.T) {
	mustCheck(t, `fn main(): void {
		for (let i: int = 0; i < 100; i += 2) {
		}
	}`)
}

func TestForLoopSubtractStep(t *testing.T) {
	mustCheck(t, `fn main(): void {
		for (let i: int = 10; i > 0; i -= 1) {
		}
	}`)
}

func TestForLoopWithBody(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let sum: int = 0
		for (let i: int = 0; i < 10; i += 1) {
			sum += i
		}
	}`)
}

// =============================================================================
// canAssign — type assignment compatibility
// =============================================================================

func TestCanAssignIntLiteralToLong(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let x: long = 42
	}`)
}

func TestCanAssignIntLiteralToDouble(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let x: double = 42
	}`)
}

func TestCanAssignCharLiteralToInt(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let x: int = 'A'
	}`)
}

func TestCanAssignCharLiteralToLong(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let x: long = 'A'
	}`)
}

func TestCanAssignCharLiteralToDouble(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let x: double = 'A'
	}`)
}

func TestCannotAssignIntVarToLong(t *testing.T) {
	mustFail(t, `fn main(): void {
		let a: int = 42
		let b: long = a
	}`, "type mismatch in let: expected long, got int")
}

func TestCannotAssignStringToInt(t *testing.T) {
	mustFail(t, `fn main(): void {
		let x: int = "hello"
	}`, "type mismatch in let")
}

func TestCannotAssignBoolToInt(t *testing.T) {
	mustFail(t, `fn main(): void {
		let x: int = true
	}`, "type mismatch in let")
}

func TestCannotAssignIntToString(t *testing.T) {
	mustFail(t, `fn main(): void {
		let x: string = 42
	}`, "type mismatch in let")
}

func TestCannotAssignIntToBool(t *testing.T) {
	mustFail(t, `fn main(): void {
		let x: bool = 42
	}`, "type mismatch in let")
}

func TestCanAssignIntLiteralToFunctionParamLong(t *testing.T) {
	mustCheck(t, `
fn greet(n: long): long { return n }
fn main(): void {
	let x: long = greet(42)
}`)
}

func TestCanAssignIntLiteralToFunctionParamDouble(t *testing.T) {
	mustCheck(t, `
fn compute(n: double): double { return n }
fn main(): void {
	let x: double = compute(42)
}`)
}

// =============================================================================
// Optional types and null
// =============================================================================

func TestNullToOptionalInt(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let x: int? = null
	}`)
}

func TestNullToOptionalString(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let x: string? = null
	}`)
}

func TestValueToOptional(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let x: int? = 42
	}`)
}

func TestOptionalNullCheck(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let x: int? = null
		if (x != null) {
			let y: int = x
		}
	}`)
}

func TestCannotAssignNullToNonOptional(t *testing.T) {
	mustFail(t, `fn main(): void {
		let x: int = null
	}`, "type mismatch in let")
}

// =============================================================================
// typeName coverage — exercise various type displays
// =============================================================================

func TestTypeNameLong(t *testing.T) {
	// Trigger an error that shows "long" in the message
	mustFail(t, `fn main(): void {
		let x: long = 42
		let y: string = x
	}`, "type mismatch in let: expected string, got long")
}

func TestTypeNameDouble(t *testing.T) {
	mustFail(t, `fn main(): void {
		let x: double = 3.14
		let y: string = x
	}`, "type mismatch in let: expected string, got double")
}

func TestTypeNameChar(t *testing.T) {
	mustFail(t, `fn main(): void {
		let x: char = 'A'
		let y: string = x
	}`, "type mismatch in let: expected string, got char")
}

func TestTypeNameBoolArray(t *testing.T) {
	mustFail(t, `fn main(): void {
		let x: bool[] = [true, false]
		let y: int = x
	}`, "type mismatch in let: expected int, got bool[]")
}

func TestTypeNameStringArray(t *testing.T) {
	mustFail(t, `fn main(): void {
		let x: string[] = ["a", "b"]
		let y: int = x
	}`, "type mismatch in let: expected int, got string[]")
}

func TestTypeNameLongArray(t *testing.T) {
	mustFail(t, `fn main(): void {
		let x: long[] = []
		let y: int = x
	}`, "type mismatch in let: expected int, got long[]")
}

func TestTypeNameDoubleArray(t *testing.T) {
	mustFail(t, `fn main(): void {
		let x: double[] = []
		let y: int = x
	}`, "type mismatch in let: expected int, got double[]")
}

func TestTypeNameCharArray(t *testing.T) {
	mustFail(t, `fn main(): void {
		let x: char[] = []
		let y: int = x
	}`, "type mismatch in let: expected int, got char[]")
}

func TestTypeNameVoid(t *testing.T) {
	// Return type mismatch triggers void display
	mustFail(t, `fn foo(): void {}
fn main(): int {
		return foo()
	}`, "return type mismatch: expected int, got void")
}

func TestTypeNameMapType(t *testing.T) {
	mustFail(t, `fn main(): void {
		let m: map[string, int] = {}
		let y: int = m
	}`, "type mismatch in let: expected int, got map[string, int]")
}

func TestTypeNameNull(t *testing.T) {
	mustFail(t, `fn main(): int {
		return null
	}`, "return type mismatch: expected int, got null")
}

// =============================================================================
// isPrimitiveType — switch tag must be primitive
// =============================================================================

func TestSwitchBoolTag(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let b: bool = true
		switch (b) {
			case true: {
				let x: int = 1
			}
			case false: {
				let x: int = 0
			}
		}
	}`)
}

func TestSwitchLongTag(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let x: long = 42
		switch (x) {
			case 1: {
				let y: int = 1
			}
			default: {
				let y: int = 0
			}
		}
	}`)
}

func TestSwitchDoubleTag(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let x: double = 3.14
		switch (x) {
			case 3.14: {
				let y: int = 1
			}
			default: {
				let y: int = 0
			}
		}
	}`)
}

func TestSwitchNonPrimitiveTagFails(t *testing.T) {
	mustFail(t, `fn main(): void {
		let a: int[] = [1, 2, 3]
		switch (a) {
			default: {
				let y: int = 0
			}
		}
	}`, "switch tag must be a primitive type")
}

// =============================================================================
// isValidFieldType — struct fields
// =============================================================================

func TestStructFieldInt(t *testing.T) {
	mustCheck(t, `
struct S { x: int }
fn main(): void {
	let s = S { x: 1 }
}`)
}

func TestStructFieldBool(t *testing.T) {
	mustCheck(t, `
struct S { x: bool }
fn main(): void {
	let s = S { x: true }
}`)
}

func TestStructFieldString(t *testing.T) {
	mustCheck(t, `
struct S { x: string }
fn main(): void {
	let s = S { x: "hello" }
}`)
}

func TestStructFieldLong(t *testing.T) {
	mustCheck(t, `
struct S { x: long }
fn main(): void {
	let s = S { x: 42 }
}`)
}

func TestStructFieldDouble(t *testing.T) {
	mustCheck(t, `
struct S { x: double }
fn main(): void {
	let s = S { x: 3.14 }
}`)
}

func TestStructFieldChar(t *testing.T) {
	mustCheck(t, `
struct S { x: char }
fn main(): void {
	let s = S { x: 'A' }
}`)
}

// =============================================================================
// Named arguments (resolveNamedArgs)
// =============================================================================

func TestNamedArguments(t *testing.T) {
	mustCheck(t, `
fn greet(name: string, age: int): string {
	return name
}
fn main(): void {
	let r: string = greet(name: "Alice", age: 30)
}`)
}

func TestNamedArgumentsReordered(t *testing.T) {
	mustCheck(t, `
fn greet(name: string, age: int): string {
	return name
}
fn main(): void {
	let r: string = greet(age: 30, name: "Alice")
}`)
}

func TestNamedArgumentsUnknownParam(t *testing.T) {
	mustFail(t, `
fn greet(name: string, age: int): string {
	return name
}
fn main(): void {
	greet(name: "Alice", height: 170)
}`, "unknown parameter name 'height'")
}

func TestNamedArgumentsDuplicate(t *testing.T) {
	mustFail(t, `
fn greet(name: string, age: int): string {
	return name
}
fn main(): void {
	greet(name: "Alice", name: "Bob")
}`, "duplicate argument for parameter 'name'")
}

// =============================================================================
// Default parameters (fillDefaultArgs)
// =============================================================================

func TestDefaultParameters(t *testing.T) {
	mustCheck(t, `
fn greet(name: string, greeting: string = "Hello"): string {
	return greeting
}
fn main(): void {
	let r1: string = greet("Alice")
	let r2: string = greet("Bob", "Hi")
}`)
}

func TestDefaultParametersMissing(t *testing.T) {
	mustFail(t, `
fn greet(name: string, age: int): string {
	return name
}
fn main(): void {
	greet("Alice")
}`, "missing argument for required parameter")
}

// =============================================================================
// Foreach loop
// =============================================================================

func TestForeachValueTypeSafety(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let a: string[] = ["a", "b", "c"]
		foreach (a as s) {
			let x: string = s
		}
	}`)
}

func TestForeachWithIndexTypeSafety(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let a: int[] = [10, 20, 30]
		foreach (a as i, n) {
			let idx: int = i
			let val: int = n
		}
	}`)
}

// =============================================================================
// Block statements
// =============================================================================

func TestBlockScopeIsolation(t *testing.T) {
	mustFail(t, `fn main(): int {
		if (true) {
			let inner: int = 42
		}
		return inner
	}`, "undefined variable 'inner'")
}

// =============================================================================
// Return statement
// =============================================================================

func TestBareReturnInVoid(t *testing.T) {
	mustCheck(t, `fn main(): void {
		return
	}`)
}

func TestBareReturnInNonVoid(t *testing.T) {
	mustFail(t, `fn main(): int {
		return
	}`, "return statement must have a value in non-void function")
}

// =============================================================================
// Index assignment
// =============================================================================

func TestArrayIndexAssignTypeMismatch(t *testing.T) {
	mustFail(t, `fn main(): void {
		let a: int[] = [1, 2, 3]
		a[0] = "hello"
	}`, "type mismatch in index assignment")
}

func TestMapIndexAssign(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let m: map[string, int] = {}
		m["key"] = 42
	}`)
}

func TestMapIndexAssignTypeMismatch(t *testing.T) {
	mustFail(t, `fn main(): void {
		let m: map[string, int] = {}
		m["key"] = "hello"
	}`, "type mismatch in map assignment")
}

func TestMapIndexRead(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let m: map[string, int] = {}
		m["key"] = 42
		let v: int = m["key"]
	}`)
}

func TestMapIndexReadWrongKeyType(t *testing.T) {
	mustFail(t, `fn main(): void {
		let m: map[string, int] = {}
		let v: int = m[42]
	}`, "map key must be string, got int")
}

func TestIndexOnNonArrayNonMap(t *testing.T) {
	mustFail(t, `fn main(): void {
		let x: int = 42
		let y: int = x[0]
	}`, "index operator requires an array or map type")
}

// =============================================================================
// Empty map literal
// =============================================================================

func TestEmptyMapLiteral(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let m: map[string, int] = {}
	}`)
}

func TestEmptyMapLiteralNonMapType(t *testing.T) {
	mustFail(t, `fn main(): void {
		let x: int = {}
	}`, "cannot assign empty map literal to non-map type")
}

// =============================================================================
// validateAnnotations
// =============================================================================

func TestAnnotationUnknown(t *testing.T) {
	mustFail(t, `fn main(): void {
		#[unknown]
		let x: string = "hello"
	}`, "unknown annotation '#[unknown]'")
}

func TestAnnotationOnPrimitive(t *testing.T) {
	mustFail(t, `fn main(): void {
		#[owned]
		let x: int = 42
	}`, "annotations are only allowed on heap types")
}

func TestAnnotationOwnedOnString(t *testing.T) {
	mustCheck(t, `fn main(): void {
		#[owned]
		let x: string = "hello"
	}`)
}

// =============================================================================
// String interpolation
// =============================================================================

func TestStringInterpolation(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let name: string = "world"
		let msg: string = "hello ${name}"
	}`)
}

func TestStringInterpolationWithInt(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let n: int = 42
		let msg: string = "number: ${n}"
	}`)
}

// =============================================================================
// Char literal
// =============================================================================

func TestCharLiteral(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let c: char = 'A'
	}`)
}

func TestCharModulo(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let c: char = 'Z'
		let r: char = c % 'A'
	}`)
}

// =============================================================================
// Null literal
// =============================================================================

func TestNullLiteral(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let x: int? = null
	}`)
}

func TestCannotInferTypeOfNull(t *testing.T) {
	mustFail(t, `fn main(): void {
		let x = null
	}`, "cannot infer type of null")
}

// =============================================================================
// Function references
// =============================================================================

func TestFunctionReference(t *testing.T) {
	mustCheck(t, `
fn add(a: int, b: int): int { return a + b }
fn main(): void {
	let f = add
	let r: int = f(1, 2)
}`)
}

// =============================================================================
// StringBuilder constructor
// =============================================================================

func TestStringBuilderConstructorNoArgs(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let sb = StringBuilder()
	}`)
}

func TestStringBuilderConstructorWithArgsFails(t *testing.T) {
	mustFail(t, `fn main(): void {
		let sb = StringBuilder("hello")
	}`, "StringBuilder() takes no arguments")
}

// =============================================================================
// Array widening (array literal with implicit element widening)
// =============================================================================

func TestArrayWidenIntToLong(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let a: long[] = [1, 2, 3]
	}`)
}

func TestArrayWidenIntToDouble(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let a: double[] = [1, 2, 3]
	}`)
}

// =============================================================================
// String coercion in concatenation
// =============================================================================

func TestStringCoercionIntConcat(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let n: int = 42
		let s: string = "number: " + n
	}`)
}

func TestStringCoercionBoolConcat(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let b: bool = true
		let s: string = "flag: " + b
	}`)
}

// =============================================================================
// Defer statement
// =============================================================================

func TestDeferStatement(t *testing.T) {
	mustCheck(t, `
import "fmt"
fn main(): void {
	defer fmt.println("cleanup")
	fmt.println("main")
}`)
}

// =============================================================================
// Interface definitions and structural typing
// =============================================================================

func TestInterfaceDefinition(t *testing.T) {
	// Interface defs register correctly; struct without matching methods fails
	mustCheck(t, `
interface Greeter {
	fn greet(): string
}
fn main(): void {
}`)
}

func TestInterfaceNotSatisfied(t *testing.T) {
	mustFail(t, `
interface Stringer {
	fn toString(): string
}
struct Empty {
	x: int
}
fn printStr(s: Stringer): void {
}
fn main(): void {
	let e = Empty { x: 1 }
	printStr(e)
}`, "expected Stringer, got Empty")
}

// =============================================================================
// Destructuring let
// =============================================================================

func TestDestructureLet(t *testing.T) {
	mustCheck(t, `
struct Point {
	x: int
	y: int
}
fn main(): void {
	let p = Point { x: 1, y: 2 }
	let { x, y } = p
	let a: int = x
	let b: int = y
}`)
}

func TestDestructureLetWrongField(t *testing.T) {
	mustFail(t, `
struct Point {
	x: int
	y: int
}
fn main(): void {
	let p = Point { x: 1, y: 2 }
	let { z } = p
}`, "struct 'Point' has no field 'z'")
}

func TestDestructureNonStruct(t *testing.T) {
	mustFail(t, `fn main(): void {
		let x: int = 42
		let { y } = x
	}`, "destructuring requires a struct type")
}

// =============================================================================
// Enum switch with isPrimitiveType
// =============================================================================

func TestEnumSwitchIsPrimitive(t *testing.T) {
	mustCheck(t, `
enum Status {
	Active
	Inactive
	Pending
}
fn main(): void {
	let s: Status = Status.Active
	switch (s) {
		case Status.Active: {
			let x: int = 1
		}
		case Status.Inactive: {
			let x: int = 2
		}
		default: {
			let x: int = 0
		}
	}
}`)
}

// =============================================================================
// Additional checkExpr coverage: StructLitExpr errors
// =============================================================================

func TestStructLitExtraFields(t *testing.T) {
	mustFail(t, `
struct Point {
	x: int
	y: int
}
fn main(): void {
	let p = Point { x: 1, y: 2, z: 3 }
}`, "struct 'Point' has 2 fields, got 3")
}

func TestStructLitFieldTypeMismatch(t *testing.T) {
	mustFail(t, `
struct Point {
	x: int
	y: int
}
fn main(): void {
	let p = Point { x: 1, y: "hello" }
}`, "field 'y' of struct 'Point': expected int, got string")
}

// =============================================================================
// Additional misc coverage
// =============================================================================

func TestIndexAssignNonArray(t *testing.T) {
	mustFail(t, `fn main(): void {
		let x: int = 42
		x[0] = 1
	}`, "index assignment requires an array or map type")
}

func TestFieldAssignNonStruct(t *testing.T) {
	mustFail(t, `fn main(): void {
		let x: int = 42
		x.field = 1
	}`, "field assignment requires a struct type")
}

func TestFieldAccessNonStruct(t *testing.T) {
	mustFail(t, `fn main(): void {
		let x: int = 42
		let y: int = x.field
	}`, "field access requires a struct type")
}
