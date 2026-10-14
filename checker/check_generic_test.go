package checker

import (
	"strings"
	"testing"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/lexer"
	"github.com/ensamuel7/dex/parser"
	"github.com/ensamuel7/dex/stdlib"
)

// checkGenericSource runs the pipeline the driver does for a program with
// generics: the parser is handed to the checker as the instantiator, because
// inferring a call's type arguments needs the argument types the checker has
// and re-parsing the template needs the parser.
func checkGenericSource(t *testing.T, source string) error {
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
	ast.ResetTypeParams()
	stdlib.RegisterAllModuleTypes()
	ast.RegisterExceptionType()

	tokens, err := lexer.New(source).Tokenize()
	if err != nil {
		t.Fatalf("lexer error: %v", err)
	}
	p := parser.New(tokens)
	prog, parseErrs := p.Parse()
	if len(parseErrs) > 0 {
		return parseErrs[0]
	}
	c := New()
	c.SetGenerics(prog.GenericFuncs, p)
	if errs := c.Check(prog); len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func mustCheckGeneric(t *testing.T, source string) {
	t.Helper()
	if err := checkGenericSource(t, source); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func mustFailGeneric(t *testing.T, source, substr string) {
	t.Helper()
	err := checkGenericSource(t, source)
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("error = %q, want to contain %q", err.Error(), substr)
	}
}

// --- Inference ---

func TestGenericInferFromArgument(t *testing.T) {
	mustCheckGeneric(t, `
	fn identity<T>(x: T): T {
		return x
	}
	fn main(): int {
		identity(1)
		identity("s")
		identity(true)
		return 0
	}`)
}

// Two parameters of one type parameter agree, so the binding holds.
func TestGenericRepeatedTypeParam(t *testing.T) {
	mustCheckGeneric(t, `
	fn pick<T>(a: T, b: T): T {
		return a
	}
	fn main(): int {
		pick(1, 2)
		pick("a", "b")
		return 0
	}`)
}

// They disagree, and the message names the argument that broke it rather than
// only that the call did not type-check.
func TestGenericConflictingBinding(t *testing.T) {
	mustFailGeneric(t, `
	fn pick<T>(a: T, b: T): T {
		return a
	}
	fn main(): int {
		pick(1, "a")
		return 0
	}`,
		"binds T to int from an earlier argument, but argument 2 is string")
}

func TestGenericIndependentTypeParams(t *testing.T) {
	mustCheckGeneric(t, `
	fn firstOfTwo<A, B>(a: A, b: B): A {
		return a
	}
	fn main(): int {
		firstOfTwo(1, "s")
		firstOfTwo("s", 1)
		return 0
	}`)
}

// Inference descends into a container, so T comes from the element type.
func TestGenericInferThroughArray(t *testing.T) {
	mustCheckGeneric(t, `
	fn firstElem<T>(xs: T[]): T {
		return xs[0]
	}
	fn main(): int {
		let nums: int[] = [1, 2]
		firstElem(nums)
		return 0
	}`)
}

// A type parameter that appears in no parameter cannot be inferred, and the
// message says which one and why.
func TestGenericUninferableTypeParam(t *testing.T) {
	mustFailGeneric(t, `
	fn makeDefault<T>(n: int): int {
		return n
	}
	fn main(): int {
		makeDefault(1)
		return 0
	}`,
		"appears in no parameter's type")
}

func TestGenericArgCount(t *testing.T) {
	mustFailGeneric(t, `
	fn pick<T>(a: T, b: T): T {
		return a
	}
	fn main(): int {
		pick(1)
		return 0
	}`,
		"takes exactly 2 argument(s), got 1")
}

// --- Instantiation ---

// A generic that calls a generic: the inner instantiation is created while the
// outer one's body is checked, which is what the worklist is for.
func TestGenericNestedInstantiation(t *testing.T) {
	mustCheckGeneric(t, `
	fn identity<T>(x: T): T {
		return x
	}
	fn twice<T>(x: T): T {
		return identity(identity(x))
	}
	fn main(): int {
		twice(1)
		twice("s")
		return 0
	}`)
}

// A recursive generic terminates: the instantiation is marked before its body
// is checked, so the self-call finds it already built.
func TestGenericRecursion(t *testing.T) {
	mustCheckGeneric(t, `
	fn countDown<T>(x: T, n: int): T {
		if (n <= 0) {
			return x
		}
		return countDown(x, n - 1)
	}
	fn main(): int {
		countDown("s", 3)
		return 0
	}`)
}

// --- Generic structs ---

func TestGenericStructInstantiation(t *testing.T) {
	mustCheckGeneric(t, `
	struct Box<T> {
		value: T

		fn get(): T {
			return value
		}
	}
	fn main(): int {
		let b: Box<int> = Box<int> { value: 1 }
		b.get()
		let s: Box<string> = Box<string> { value: "x" }
		s.get()
		return 0
	}`)
}

func TestGenericStructTwoParams(t *testing.T) {
	mustCheckGeneric(t, `
	struct Pair<K, V> {
		key: K
		value: V
	}
	fn main(): int {
		let p: Pair<string, int> = Pair<string, int> { key: "a", value: 1 }
		return 0
	}`)
}

// A generic struct needs its type arguments: without them the name is a
// template rather than a type.
func TestGenericStructNeedsTypeArgs(t *testing.T) {
	mustFailGeneric(t, `
	struct Box<T> {
		value: T
	}
	fn main(): int {
		let b: Box = Box { value: 1 }
		return 0
	}`,
		"needs type arguments")
}

func TestGenericStructWrongTypeArgCount(t *testing.T) {
	mustFailGeneric(t, `
	struct Pair<K, V> {
		key: K
		value: V
	}
	fn main(): int {
		let p: Pair<int> = Pair<int> { key: 1, value: 2 }
		return 0
	}`,
		"takes 2 type argument(s), got 1")
}

// A field of the instantiated struct has the concrete type, so assigning the
// wrong one is an ordinary mismatch rather than anything generic.
func TestGenericStructFieldTypeEnforced(t *testing.T) {
	mustFailGeneric(t, `
	struct Box<T> {
		value: T
	}
	fn main(): int {
		let b: Box<int> = Box<int> { value: "not an int" }
		return 0
	}`,
		"value")
}
