package resolve

import (
	"testing"

	"github.com/ensamuel7/dex/ast"
)

// fieldReads counts, in the flattened method R_<method>, how many references to
// name were left bare (a local) and how many were rewritten to self.<name>.
func fieldReads(t *testing.T, src, method, name string) (bare, viaSelf int) {
	t.Helper()
	program, err := resolveProject(t, map[string]string{"main.dx": src})
	if err != nil {
		t.Fatal(err)
	}
	var fn *ast.Function
	for i := range program.Functions {
		if program.Functions[i].Name == "R_"+method {
			fn = &program.Functions[i]
		}
	}
	if fn == nil {
		t.Fatalf("no flattened method R_%s", method)
	}
	VisitStmts(fn.Body, nil, func(e ast.Expr) {
		switch x := e.(type) {
		case *ast.Ident:
			if x.Name == name {
				bare++
			}
		case *ast.FieldAccessExpr:
			if obj, ok := x.Object.(*ast.Ident); ok && obj.Name == "self" && x.Field == name {
				viaSelf++
			}
		}
	})
	return bare, viaSelf
}

// Every way of binding a name shadows a field of the same name, within the
// block that binds it. Each case has one use inside the binding's scope, which
// must stay bare, and one after it, which must read the field.
func TestEveryBindingShadowsAField(t *testing.T) {
	for _, c := range []struct {
		form, body string
	}{
		{"foreach value", `
        foreach ([1, 2] as item) { use(item) }
        use(item)`},
		{"foreach index", `
        foreach ([1, 2] as item, v) { use(item) }
        use(item)`},
		{"lambda parameter", `
        let f: fn(int): int = fn(item: int): int { return item }
        use(item)`},
		{"catch binding", `
        try { use(1) } catch (item: Exception) { print(item.message) }
        use(item)`},
		{"switch pattern", `
        switch (n) {
            case Node.Box(item): { use(item) }
            default: {}
        }
        use(item)`},
		{"let in a block", `
        if (true) { let item: int = 1
            use(item) }
        use(item)`},
		{"let in a for init", `
        for (let item: int = 0; item < 1; item++) {}
        use(item)`},
	} {
		src := `enum Node {
    Box(int)
    Other
}

fn use(x: int): void {}

struct R {
    item: int

    fn m(n: Node): void {` + c.body + `
    }
}

fn main(): void {}
`
		bare, viaSelf := fieldReads(t, src, "m", "item")
		if bare == 0 {
			t.Errorf("%s: the use inside its scope was rewritten to self.item", c.form)
		}
		if viaSelf != 1 {
			t.Errorf("%s: the use after its scope should read the field once, got %d self.item", c.form, viaSelf)
		}
	}
}

// A `let` in one branch is not visible in its sibling. With one flat set of
// names for the whole method, the else branch's `item` stayed bare and named a
// local that does not exist there.
func TestALetDoesNotShadowASiblingBranch(t *testing.T) {
	src := `struct R {
    item: int

    fn m(flag: bool): int {
        if (flag) {
            let item: int = 1
            return item
        } else {
            return item
        }
    }
}

fn main(): void {}
`
	if bare, viaSelf := fieldReads(t, src, "m", "item"); bare != 1 || viaSelf != 1 {
		t.Errorf("want one bare item (the let) and one self.item (the else), got %d and %d", bare, viaSelf)
	}
}

// The module prefixer withholds a rename from any name the function binds.
// It used to see only `let` and foreach, so a lambda parameter, catch binding or
// pattern named like a module-level function was rebound to the function.
func TestCollectDeclaredNamesSeesEveryBinding(t *testing.T) {
	src := `enum Node {
    Box(int)
    Other
}

fn m(n: Node): void {
    let a: int = 1
    foreach ([1] as b, c) {}
    let f: fn(int): int = fn(d: int): int { return d }
    try { a = 2 } catch (e: Exception) {}
    switch (n) {
        case Node.Box(g): {}
        default: {}
    }
}

fn main(): void {}
`
	program, err := resolveProject(t, map[string]string{"main.dx": src})
	if err != nil {
		t.Fatal(err)
	}
	var fn *ast.Function
	for i := range program.Functions {
		if program.Functions[i].Name == "m" {
			fn = &program.Functions[i]
		}
	}
	got := map[string]bool{}
	collectDeclaredNames(fn.Body, got)
	for _, want := range []string{"a", "b", "c", "f", "d", "e", "g"} {
		if !got[want] {
			t.Errorf("collectDeclaredNames missed %q; got %v", want, got)
		}
	}
}
