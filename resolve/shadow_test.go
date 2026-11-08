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

// A module's parameter default is prefixed like the rest of the module: the
// checker pastes it into the call, in the flat namespace, wherever the call is.
func TestAModuleDefaultNamesItsModulesNames(t *testing.T) {
	program, err := resolveProject(t, map[string]string{
		"main.dx": `import "d"

fn main(): void {}
`,
		"d.dx": `import "other" as "o"

const WAIT: int = 1

fn base(): int {
    return 1
}

fn f(a: int = WAIT, b: int = base(), c: int = o.LIMIT, d: int = o.compute(), WAIT: int = WAIT): int {
    return a
}

struct S {
    fn m(a: int = WAIT, c: int = o.LIMIT): int {
        return a
    }
}
`,
		"other.dx": `const LIMIT: int = 2

fn compute(): int {
    return 3
}
`,
	})
	if err != nil {
		t.Fatal(err)
	}
	var fn *ast.Function
	for i := range program.Functions {
		if program.Functions[i].Name == "d_f" {
			fn = &program.Functions[i]
		}
	}
	if fn == nil {
		t.Fatal("no function d_f")
	}
	ident := func(e ast.Expr) string {
		if id, ok := e.(*ast.Ident); ok {
			return id.Name
		}
		return "not an identifier"
	}
	if got := ident(fn.Params[0].DefaultValue); got != "d_WAIT" {
		t.Errorf("a constant: %s", got)
	}
	if call := fn.Params[1].DefaultValue.(*ast.CallExpr); call.Name != "d_base" {
		t.Errorf("a function: %s", call.Name)
	}
	if got := ident(fn.Params[2].DefaultValue); got != "o_LIMIT" {
		t.Errorf("another module's constant, through an alias: %s", got)
	}
	if call := fn.Params[3].DefaultValue.(*ast.CallExpr); call.Name != "o_compute" || call.Module != "" {
		t.Errorf("another module's function keeps nothing for a caller's scope to capture: %q.%q", call.Module, call.Name)
	}
	if got := ident(fn.Params[4].DefaultValue); got != "d_WAIT" {
		t.Errorf("a parameter does not shadow what its own default names: %s", got)
	}

	// A method's signature reads its parameters from the struct, not the
	// flattened function, and a rewrite can replace the whole expression.
	for i := range program.Structs {
		if program.Structs[i].Name != "d_S" {
			continue
		}
		params := program.Structs[i].Methods[0].Params
		if got := ident(params[0].DefaultValue); got != "d_WAIT" {
			t.Errorf("a method's default: %s", got)
		}
		if got := ident(params[1].DefaultValue); got != "o_LIMIT" {
			t.Errorf("a method's default naming another module: %s", got)
		}
		return
	}
	t.Error("no struct d_S")
}

// A default may not reach another module's private function: that stays
// qualified, so the checker still refuses it at the call.
func TestAModuleDefaultKeepsAnothersPrivateQualified(t *testing.T) {
	program, err := resolveProject(t, map[string]string{
		"main.dx": `import "d"

fn main(): void {}
`,
		"d.dx": `import "other"

fn f(a: int = other.hidden()): int {
    return a
}
`,
		"other.dx": `private fn hidden(): int {
    return 1
}
`,
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := range program.Functions {
		if program.Functions[i].Name == "d_f" {
			call := program.Functions[i].Params[0].DefaultValue.(*ast.CallExpr)
			if call.Module != "other" {
				t.Errorf("a private function was flattened out of the checker's sight: %q.%q", call.Module, call.Name)
			}
			return
		}
	}
	t.Fatal("no function d_f")
}
