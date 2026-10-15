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
