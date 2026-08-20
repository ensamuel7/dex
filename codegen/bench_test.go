package codegen

import (
	"testing"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/checker"
	"github.com/ensamuel7/dex/lexer"
	"github.com/ensamuel7/dex/parser"
	"github.com/ensamuel7/dex/stdlib"
	"github.com/ensamuel7/dex/token"
)

// --- Benchmark programs of varying complexity ---

const benchSmall = `fn main(): void {
	let x: int = 42
	let y: int = x + 1
}`

const benchMedium = `fn add(a: int, b: int): int {
	return a + b
}

fn factorial(n: int): int {
	if (n <= 1) {
		return 1
	}
	return n * factorial(n - 1)
}

fn main(): void {
	let result: int = add(1, 2)
	let f: int = factorial(10)
	let greeting: string = "hello"
	let nums: int[] = [1, 2, 3, 4, 5]
	for(let i: int = 0; i < 5; i++) {
		nums[i] = nums[i] * 2
	}
}
`

const benchLarge = `
struct Point {
	x: int
	y: int
}

struct Line {
	start: Point
	end: Point
}

enum Color {
	Red,
	Green,
	Blue,
	Alpha
}

fn distance(p1: Point, p2: Point): double {
	let dx: int = p2.x - p1.x
	let dy: int = p2.y - p1.y
	return 0.0
}

fn add(a: int, b: int): int {
	return a + b
}

fn multiply(a: int, b: int): int {
	return a * b
}

fn max(a: int, b: int): int {
	if (a > b) {
		return a
	}
	return b
}

fn fibonacci(n: int): int {
	if (n <= 1) {
		return n
	}
	return fibonacci(n - 1) + fibonacci(n - 2)
}

fn classifyColor(c: Color): string {
	switch (c) {
		case Color.Red: {
			return "warm"
		}
		case Color.Green: {
			return "cool"
		}
		case Color.Blue: {
			return "cool"
		}
		default: {
			return "other"
		}
	}
	return "unknown"
}

fn main(): void {
	let origin = Point { x: 0, y: 0 }
	let target = Point { x: 3, y: 4 }
	let line = Line { start: origin, end: target }

	let sum: int = 0
	for(let i: int = 0; i < 100; i++) {
		sum = sum + i
	}

	let nums: int[] = [10, 20, 30, 40, 50]
	let total: int = 0
	foreach(nums as n) {
		total = total + n
	}

	let fib: int = fibonacci(10)
	let m: int = max(42, 99)
	let product: int = multiply(add(3, 4), 5)

	let color: Color = Color.Red
	let label: string = classifyColor(color)
	let greeting: string = "Hello, world!"
	let count: int = 0
	while (count < 10) {
		count++
	}
}
`

// --- Pipeline helpers (non-testing.T versions for benchmarks) ---

func benchLex(source string) ([]token.Token, error) {
	return lexer.New(source).Tokenize()
}

func benchParse(tokens []token.Token) (*ast.Program, []error) {
	return parser.New(tokens).Parse()
}

func benchCheck(prog *ast.Program) []error {
	return checker.New().Check(prog)
}

func benchCodegen(prog *ast.Program) string {
	return New().Generate(prog)
}

func resetTypes() {
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
}

// --- Lexer benchmarks ---

func BenchmarkLexSmall(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchLex(benchSmall)
	}
}

func BenchmarkLexMedium(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchLex(benchMedium)
	}
}

func BenchmarkLexLarge(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchLex(benchLarge)
	}
}

// --- Parser benchmarks ---

func BenchmarkParseSmall(b *testing.B) {
	tokens, _ := benchLex(benchSmall)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchParse(tokens)
	}
}

func BenchmarkParseMedium(b *testing.B) {
	tokens, _ := benchLex(benchMedium)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchParse(tokens)
	}
}

func BenchmarkParseLarge(b *testing.B) {
	tokens, _ := benchLex(benchLarge)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchParse(tokens)
	}
}

// --- Checker benchmarks ---
// Note: The checker modifies AST state (e.g. struct type registrations), so we must
// re-parse for each iteration when structs/enums are involved.

func BenchmarkCheckSmall(b *testing.B) {
	tokens, _ := benchLex(benchSmall)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resetTypes()
		prog, _ := benchParse(tokens)
		benchCheck(prog)
	}
}

func BenchmarkCheckMedium(b *testing.B) {
	tokens, _ := benchLex(benchMedium)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resetTypes()
		prog, _ := benchParse(tokens)
		benchCheck(prog)
	}
}

func BenchmarkCheckLarge(b *testing.B) {
	tokens, _ := benchLex(benchLarge)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resetTypes()
		prog, _ := benchParse(tokens)
		benchCheck(prog)
	}
}

// --- Codegen benchmarks ---

func BenchmarkCodegenSmall(b *testing.B) {
	resetTypes()
	tokens, _ := benchLex(benchSmall)
	prog, _ := benchParse(tokens)
	benchCheck(prog)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchCodegen(prog)
	}
}

func BenchmarkCodegenMedium(b *testing.B) {
	resetTypes()
	tokens, _ := benchLex(benchMedium)
	prog, _ := benchParse(tokens)
	benchCheck(prog)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchCodegen(prog)
	}
}

func BenchmarkCodegenLarge(b *testing.B) {
	tokens, _ := benchLex(benchLarge)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resetTypes()
		prog, _ := benchParse(tokens)
		benchCheck(prog)
		benchCodegen(prog)
	}
}

// --- Full pipeline benchmarks ---

func BenchmarkFullPipelineSmall(b *testing.B) {
	for i := 0; i < b.N; i++ {
		resetTypes()
		tokens, _ := benchLex(benchSmall)
		prog, _ := benchParse(tokens)
		benchCheck(prog)
		benchCodegen(prog)
	}
}

func BenchmarkFullPipelineMedium(b *testing.B) {
	for i := 0; i < b.N; i++ {
		resetTypes()
		tokens, _ := benchLex(benchMedium)
		prog, _ := benchParse(tokens)
		benchCheck(prog)
		benchCodegen(prog)
	}
}

func BenchmarkFullPipelineLarge(b *testing.B) {
	for i := 0; i < b.N; i++ {
		resetTypes()
		tokens, _ := benchLex(benchLarge)
		prog, _ := benchParse(tokens)
		benchCheck(prog)
		benchCodegen(prog)
	}
}
