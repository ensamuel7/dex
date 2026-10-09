package codegen

import (
	"strings"
	"testing"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/checker"
	"github.com/ensamuel7/dex/lexer"
	"github.com/ensamuel7/dex/parser"
	"github.com/ensamuel7/dex/resolve"
	"github.com/ensamuel7/dex/stdlib"
)

// generateWithMethods mirrors the real pipeline, which flattens struct methods
// into functions before checking. The shared generate() helper skips that step,
// so no codegen test built with it ever sees a struct method at all.
func generateWithMethods(t *testing.T, source string) string {
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
	p := parser.New(tokens)
	for _, name := range stdlib.ModuleTypesForImports(extractCodegenImportPaths(tokens)) {
		p.AddStructName(name)
	}
	prog, parseErrs := p.Parse()
	if len(parseErrs) > 0 {
		t.Fatalf("parser error: %v", parseErrs[0])
	}
	resolve.FlattenStructMethods(prog)
	if checkErrs := checker.New().Check(prog); len(checkErrs) > 0 {
		t.Fatalf("checker error: %v", checkErrs[0])
	}
	return New().Generate(prog)
}

// The receiver is a pointer, so a method can mutate the instance it was called
// on. By value, an assignment to a field compiled and then did nothing.
func TestMethodReceiverIsAPointer(t *testing.T) {
	c := generateWithMethods(t, `import "fmt"
	struct Q {
		n: int
		fn bump(): void { self.n = self.n + 1 }
	}
	fn main(): void {
		let q: Q = Q { n: 1 }
		q.bump()
		fmt.println(q.n)
	}`)
	if !strings.Contains(c, "Q_bump(Dex_Q* self)") {
		t.Errorf("receiver is not a pointer:\n%s", c)
	}
	if !strings.Contains(c, "Q_bump(&q)") {
		t.Errorf("call site does not pass the instance's address:\n%s", c)
	}
	if !strings.Contains(c, "self->n") {
		t.Errorf("field access through a pointer receiver should use ->:\n%s", c)
	}
}

// A chain threads one instance through, so each link must receive the pointer
// the previous link returned rather than a fresh address-of.
func TestFluentChainThreadsOneInstance(t *testing.T) {
	c := generateWithMethods(t, `import "fmt"
	struct Q {
		n: int
		fn bump(): &Q { self.n = self.n + 1  return self }
	}
	fn main(): void {
		let q: Q = Q { n: 0 }
		q.bump().bump()
		fmt.println(q.n)
	}`)
	if !strings.Contains(c, "Q_bump(&q)") {
		t.Errorf("first link should take the variable's address:\n%s", c)
	}
	// The second link is handed the hoisted pointer, not &<temporary>.
	if strings.Contains(c, "Q_bump(&Q_bump") {
		t.Errorf("second link took the address of a returned pointer:\n%s", c)
	}
	if !strings.Contains(c, "Dex_Q* _dex_tmp_") {
		t.Errorf("the returned reference was not bound to a temporary:\n%s", c)
	}
}

// A method returning a reference is assigned straight across; taking its
// address asked C for the address of an rvalue.
func TestRefReturnAssignedWithoutAddressOf(t *testing.T) {
	c := generateWithMethods(t, `import "fmt"
	struct Q {
		n: int
		fn bump(): &Q { self.n = self.n + 1  return self }
	}
	fn main(): void {
		let q: Q = Q { n: 0 }
		let r: &Q = q.bump()
		fmt.println(r.n)
	}`)
	if strings.Contains(c, "= &Q_bump") {
		t.Errorf("took the address of a ref-returning call:\n%s", c)
	}
}

// A method value still copies its receiver, so a mutating method reached that
// way changes the copy rather than the original.
func TestMethodValueCopiesPointerReceiver(t *testing.T) {
	c := generateWithMethods(t, `import "fmt"
	struct Q {
		n: int
		fn get(): int { return self.n }
	}
	fn main(): void {
		let q: Q = Q { n: 7 }
		let f: fn(): int = q.get
		fmt.println(f())
	}`)
	// The environment holds a value and the flattened function takes its address.
	if !strings.Contains(c, "(&_e->self") {
		t.Errorf("method-value thunk should pass the environment copy by address:\n%s", c)
	}
}
