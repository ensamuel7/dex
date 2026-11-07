package resolve

import (
	"reflect"
	"sort"
	"testing"

	"github.com/ensamuel7/dex/ast"
)

func parseBody(t *testing.T, body string) []ast.Stmt {
	t.Helper()
	src := `struct Oops(message: string) {}

enum V {
    N(int)
    Other
}

fn use(x: int): void {}

fn m(): void {` + body + `
}

fn main(): void {}
`
	program, err := resolveProject(t, map[string]string{"main.dx": src})
	if err != nil {
		t.Fatal(err)
	}
	for i := range program.Functions {
		if program.Functions[i].Name == "m" {
			return program.Functions[i].Body
		}
	}
	t.Fatal("no function m")
	return nil
}

// freeVars is FreeNames without the function being called, which is not a
// variable and which every case below names.
func freeVars(names map[string]bool) []string {
	var out []string
	for n := range names {
		if n != "use" {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// A name is free where nothing binds it, in every statement kind, and a binding
// reaches only as far as its own block.
func TestFreeNames(t *testing.T) {
	for _, c := range []struct {
		form, body string
		want       []string
	}{
		{"expression", `use(a + b)`, []string{"a", "b"}},
		{"a try body and its catch", `try { use(a) } catch (e: Oops) { use(b) } finally { use(c) }`, []string{"a", "b", "c"}},
		{"a throw", `throw Oops("${a}")`, []string{"a"}},
		{"a switch tag and its cases", `switch (k) {
            case 1: { use(a) }
            default: { use(b) }
        }`, []string{"a", "b", "k"}},
		{"an enum constructor", `let v: V = V.N(a)`, []string{"a"}},
		{"a method receiver", `recv.call(a)`, []string{"a", "recv"}},
		{"a chained receiver", `recv.field.call()`, []string{"recv"}},
		{"assignment targets", `a = 1
        b += 2
        c++
        d--`, []string{"a", "b", "c", "d"}},
		{"a catch binding", `try { use(1) } catch (a: Oops) { print(a.message) }`, nil},
		{"a foreach variable", `foreach (xs as a, b) { use(a + b) }`, []string{"xs"}},
		{"a pattern binding", `switch (k) {
            case V.N(a): { use(a) }
            default: {}
        }`, []string{"k"}},
		{"a let, before and after", `let a: int = 1
        use(a)`, nil},
		{"several lets on a line", `let a, b: int = 1
        use(a + b)`, nil},
		{"a lambda's parameter", `let f: fn(int): int = fn(a: int): int { return a + b }`, []string{"b"}},
		{"a spawn body", `spawn {
            let a: int = 1
            use(a + b)
        }`, []string{"b"}},
		{"a sibling of a branch that binds it", `if (true) {
            let a: int = 1
            use(a)
        } else {
            use(a)
        }`, []string{"a"}},
		{"after the block that bound it", `if (true) {
            let a: int = 1
            use(a)
        }
        use(a)`, []string{"a"}},
		{"after the loop that bound it", `foreach (xs as a) {}
        use(a)`, []string{"a", "xs"}},
	} {
		got := freeVars(FreeNames(parseBody(t, c.body)))
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: free names %v, want %v", c.form, got, c.want)
		}
	}
}

// DeclaredInFrame sees every binding in any block of the frame, and stops at a
// nested closure, whose bindings are its own.
func TestDeclaredInFrame(t *testing.T) {
	body := parseBody(t, `let a: int = 1
    foreach (xs as b, c) {}
    try { use(1) } catch (d: Oops) {}
    switch (k) {
        case V.N(e): {}
        default: {}
    }
    if (true) {
        let g: int = 1
    }
    let h: fn(int): int = fn(inner: int): int {
        let alsoInner: int = 1
        return inner
    }`)
	got := DeclaredInFrame(body)
	for _, want := range []string{"a", "b", "c", "d", "e", "g", "h"} {
		if !got[want] {
			t.Errorf("missed %q in %v", want, got)
		}
	}
	for _, not := range []string{"inner", "alsoInner", "xs", "k"} {
		if got[not] {
			t.Errorf("%q is not declared in this frame: %v", not, got)
		}
	}
}
