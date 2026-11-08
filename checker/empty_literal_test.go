package checker

import (
	"strings"
	"testing"
)

const emptyLiteralDecls = `
struct Item {
    name: string
}

struct Holder {
    nums: int[]
    names: string[]
    items: Item[]
    counts: map[string, int]
    maybe: int[]?
}

struct Made(nums: int[], counts: map[string, int]) {}

enum Node {
    Raw(string, int[])
    Named(map[string, string])
}

interface Sized {
    fn size(xs: int[]): int
}

struct Sizer {
    fn size(xs: int[]): int {
        return xs.len()
    }
}

fn total(xs: int[]): int {
    return xs.len()
}

fn mapSize(m: map[string, string]): int {
    return m.len()
}

fn pick<T>(xs: T[]): int {
    return xs.len()
}

fn scalar(n: int): int {
    return n
}
`

// An empty literal takes its type from where it goes, at every site that knows.
func TestEmptyLiteralTakesItsTypeFromThePosition(t *testing.T) {
	for _, c := range []struct{ site, stmt string }{
		{"an int[] argument, whose element type is 0", `total([])`},
		{"a map argument", `mapSize({})`},
		{"a named argument", `total(xs: [])`},
		{"a function value's argument", `let f: fn(int[]): int = total
    f([])`},
		{"a method argument", `let s: Sizer = Sizer {}
    s.size([])`},
		{"an interface method argument", `let s: Sized = Sizer {}
    s.size([])`},
		{"struct fields", `let h: Holder = Holder { nums: [], names: [], items: [], counts: {}, maybe: [] }`},
		{"constructor arguments", `let m: Made = Made([], {})`},
		{"enum payloads", `let n: Node = Node.Raw("x", [])
    let o: Node = Node.Named({})`},
		{"an assignment", `let xs: int[] = [1]
    xs = []`},
		{"a map assignment", `let m: map[string, int] = {}
    m = {}`},
		{"an optional assignment", `let xs: int[]? = null
    xs = []`},
		{"a field assignment", `let h: Holder = Holder {}
    h.nums = []
    h.counts = {}`},
	} {
		src := emptyLiteralDecls + "\nfn main(): void {\n    " + c.stmt + "\n}\n"
		if err := checkGenericSource(t, src); err != nil {
			t.Errorf("%s: %v", c.site, err)
		}
	}
}

func TestEmptyLiteralInADefaultAndAReturn(t *testing.T) {
	src := emptyLiteralDecls + `
fn withDefaults(xs: int[] = [], m: map[string, string] = {}): int {
    return xs.len() + m.len()
}

fn returned(): int[] {
    return []
}

fn returnedMap(): map[string, int] {
    return {}
}

fn main(): void {
    assert(withDefaults() == 0)
    assert(returned().len() == 0)
    assert(returnedMap().len() == 0)
}
`
	if err := checkGenericSource(t, src); err != nil {
		t.Error(err)
	}
}

// Where the position says nothing, or says something a literal cannot be, the
// program is still refused.
func TestEmptyLiteralWithNothingToGoOn(t *testing.T) {
	for _, c := range []struct{ site, stmt, want string }{
		{"a bare let", `let x = []`, "cannot infer type of empty array literal"},
		{"a bare map let", `let x = {}`, "cannot infer"},
		{"a scalar parameter", `scalar([])`, "cannot infer type of empty array literal"},
		{"a scalar parameter, map", `scalar({})`, "cannot infer type of empty map literal"},
		{"a map parameter given []", `mapSize([])`, "cannot infer type of empty array literal"},
		{"an array parameter given {}", `total({})`, "cannot infer type of empty map literal"},
		{"a generic parameter", `pick([])`, "cannot infer"},
		{"a scalar assignment", `let n: int = 1
    n = []`, "cannot infer type of empty array literal"},
	} {
		src := emptyLiteralDecls + "\nfn main(): void {\n    " + c.stmt + "\n}\n"
		err := checkGenericSource(t, src)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want an error containing %q", c.site, err, c.want)
		}
	}
}
