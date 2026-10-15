package codegen

import (
	"strings"
	"testing"
)

// A program whose only heap value is an interface box still needs the refcount
// runtime. usesRefcount was derived from strings, arrays, concurrency and
// closures, so a struct holding an interface field referenced DexObjHeader and
// dex_retain without either ever being declared, and the C did not compile.
func TestInterfaceFieldEmitsRefcountRuntime(t *testing.T) {
	out := generate(t, `
	interface Expr { fn eval(): long }
	struct Lit {
		v: long
		fn eval(): long { return v }
	}
	struct Add {
		left: Expr
		right: Expr
		fn eval(): long { return left.eval() + right.eval() }
	}
	fn main(): void {
		let a: Expr = Lit{v: 1}
		let b: Expr = Lit{v: 2}
		let s: Expr = Add{left: a, right: b}
	}`)
	assertContains(t, out, "typedef struct DexObjHeader {")
	assertContains(t, out, "dex_obj_alloc")
}

// Coercing a struct literal into an interface hoists the literal into a
// statement temporary. The hoists used to be wrapped in a block, which scoped
// the declaration to it: `s` below was out of scope on the following line. The
// declaration has to sit outside whatever the hoist needed.
func TestInterfaceLetFromStructLitStaysInScope(t *testing.T) {
	out := generate(t, `
	interface Expr { fn eval(): long }
	struct Lit {
		v: long
		fn eval(): long { return v }
	}
	struct Add {
		left: Expr
		right: Expr
		fn eval(): long { return left.eval() + right.eval() }
	}
	fn main(): void {
		let a: Expr = Lit{v: 1}
		let b: Expr = Lit{v: 2}
		let s: Expr = Add{left: a, right: b}
		let total: long = s.eval()
	}`)
	// The temporary is declared, the interface variable is not inside its block.
	assertContains(t, out, "Dex_Expr s = dex_iface_Expr_from_Add(")
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Dex_Expr s =") && strings.HasPrefix(strings.TrimSpace(line), "{") {
			t.Fatalf("interface declaration is scoped to its hoist block: %s", strings.TrimSpace(line))
		}
	}
}

// An exhaustive payload-enum switch closes with an unconditional else, so C
// can see the chain is total. Without it, a function whose every arm returns
// draws "does not return a value in all control paths" -- a warning on
// perfectly ordinary code, and an error under -Werror.
func TestExhaustiveEnumSwitchClosesWithElse(t *testing.T) {
	out := generate(t, `
	enum Expr {
		Lit(long)
		Add(Expr, Expr)
	}
	fn eval(e: Expr): long {
		switch (e) {
			case Expr.Lit(v): { return v }
			case Expr.Add(a, b): { return eval(a) + eval(b) }
		}
	}
	fn main(): void {
		let x: long = eval(Expr.Lit(1))
	}`)
	assertContains(t, out, "} else {")
	// Two variants, one tag test: the second arm is reached by elimination.
	if n := strings.Count(out, "->tag == "); n != 1 {
		t.Fatalf("expected exactly 1 tag test in an exhaustive 2-variant switch, got %d", n)
	}
}

// A switch with a default keeps the ordinary chain: the default is the else.
func TestEnumSwitchWithDefaultKeepsChain(t *testing.T) {
	out := generate(t, `
	enum Expr {
		Lit(long)
		Add(Expr, Expr)
	}
	fn f(e: Expr): long {
		switch (e) {
			case Expr.Lit(v): { return v }
			default: { return 0 }
		}
		return 0
	}
	fn main(): void {
		let x: long = f(Expr.Lit(1))
	}`)
	assertContains(t, out, "_switch_tag_0->tag == Dex_Expr_Lit")
}
