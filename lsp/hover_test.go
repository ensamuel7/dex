package lsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const hoverSource = `struct Query {
    from: string
    max: int

    fn table(name: string): &Query {
        self.from = name
        return self
    }

    private fn reset(): void {
        self.max = 0
    }
}

struct Plain {
    id: int
}

struct Loose {
    on: bool
    total: long
    off: bool
}

fn main(): void {
    let q: Query = Query { from: "t", max: 1 }
    print(q.table("u").from)
    print(q.from)
}
`

func hoverServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "h.dx")
	if err := os.WriteFile(path, []byte(hoverSource), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToURI(path)
	return &Server{documents: map[string]string{uri: hoverSource}}, uri
}

func hoverLineCol(t *testing.T, needle, word string) Position {
	t.Helper()
	for i, line := range strings.Split(hoverSource, "\n") {
		if !strings.Contains(line, needle) {
			continue
		}
		c := strings.Index(line, word)
		if c < 0 {
			t.Fatalf("%q not on the line holding %q", word, needle)
		}
		return Position{Line: i, Character: c + 1}
	}
	t.Fatalf("no line holds %q", needle)
	return Position{}
}

// A struct's methods are part of what hovering its name has to answer: the
// registry entry the parser writes carries only fields, so they come from the
// declaration in the program.
func TestHoverStructListsMethods(t *testing.T) {
	s, uri := hoverServer(t)
	got := s.hoverAt(uri, hoverSource, hoverLineCol(t, "struct Query", "Query"))

	for _, want := range []string{
		"**Fields:**",
		"- `from`: `string`",
		"**Methods:**",
		"fn table(name: string): &Query",
		"private fn reset(): void",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("hover over Query missing %q:\n%s", want, got)
		}
	}
	// The receiver is implicit at the call site, so it is not a parameter here.
	if strings.Contains(got, "self:") {
		t.Errorf("hover over Query shows the lowered self parameter:\n%s", got)
	}
}

// A struct with no methods keeps the old shape: no empty Methods heading.
func TestHoverStructWithoutMethods(t *testing.T) {
	s, uri := hoverServer(t)
	got := s.hoverAt(uri, hoverSource, hoverLineCol(t, "struct Plain", "Plain"))

	if !strings.Contains(got, "- `id`: `int`") {
		t.Errorf("hover over Plain missing its field:\n%s", got)
	}
	if strings.Contains(got, "**Methods:**") {
		t.Errorf("hover over Plain has a Methods heading:\n%s", got)
	}
}

func TestHoverMethodOnValue(t *testing.T) {
	s, uri := hoverServer(t)
	got := s.hoverAt(uri, hoverSource, hoverLineCol(t, "q.table(", "table"))

	if !strings.Contains(got, "(method) Query.table(name: string): &Query") {
		t.Errorf("hover over q.table did not describe the method:\n%s", got)
	}
}

func TestHoverFieldOnValue(t *testing.T) {
	s, uri := hoverServer(t)
	got := s.hoverAt(uri, hoverSource, hoverLineCol(t, "print(q.from)", "from"))

	if !strings.Contains(got, "(field) Query.from: string") {
		t.Errorf("hover over a field did not describe it:\n%s", got)
	}
}

const arrayHoverSource = `struct User {
    name: string
}

fn main(): void {
    let nums: int[] = [1, 2]
    let inferred = [1, 2, 3]
    let users: User[] = [User { name: "a" }]
    foreach (users as person) {
        let n: int = person.name.len()
    }
    let c: int = nums.len()
    users.push(User { name: "b" })
}
`

func arrayHover(t *testing.T, needle, word string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "a.dx")
	if err := os.WriteFile(path, []byte(arrayHoverSource), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToURI(path)
	s := &Server{documents: map[string]string{uri: arrayHoverSource}}
	for i, line := range strings.Split(arrayHoverSource, "\n") {
		if strings.Contains(line, needle) {
			return s.hoverAt(uri, arrayHoverSource, Position{Line: i, Character: strings.Index(line, word)})
		}
	}
	t.Fatalf("no line holds %q", needle)
	return ""
}

func TestHoverArrayVariable(t *testing.T) {
	got := arrayHover(t, "let nums", "nums")
	for _, want := range []string{"(variable) nums: int[]", "Array of `int`", "push(value: int): void", "sort(direction: string): void"} {
		if !strings.Contains(got, want) {
			t.Errorf("hover missing %q:\n%s", want, got)
		}
	}
}

func TestHoverInferredArray(t *testing.T) {
	if got := arrayHover(t, "let inferred", "inferred"); !strings.Contains(got, "(variable) inferred: int[]") {
		t.Errorf("an inferred array should hover with its type:\n%s", got)
	}
}

func TestHoverStructArrayListsElementFields(t *testing.T) {
	got := arrayHover(t, "let users", "users")
	if !strings.Contains(got, "**Element fields:**") || !strings.Contains(got, "`name`: `string`") {
		t.Errorf("hover should list the element's fields:\n%s", got)
	}
	if strings.Contains(got, "contains(") || strings.Contains(got, "sort(") {
		t.Errorf("a struct array has no contains or sort:\n%s", got)
	}
}

func TestHoverForeachVariable(t *testing.T) {
	if got := arrayHover(t, "foreach (users as person)", "person"); !strings.Contains(got, "Type: `User`") {
		t.Errorf("the loop variable should hover with the element type:\n%s", got)
	}
}

func TestHoverArrayMethod(t *testing.T) {
	if got := arrayHover(t, "let c: int = nums.len()", "len"); !strings.Contains(got, "(method) int[].len(): int") {
		t.Errorf("got:\n%s", got)
	}
	if got := arrayHover(t, "users.push", "push"); !strings.Contains(got, "(method) User[].push(value: User): void") {
		t.Errorf("got:\n%s", got)
	}
}

func TestHoverStructShowsSizeAndBetterOrder(t *testing.T) {
	s, uri := hoverServer(t)
	got := s.hoverAt(uri, hoverSource, hoverLineCol(t, "struct Loose", "Loose"))
	for _, want := range []string{
		"**Size:** 16 bytes (align 8, 6 bytes of padding)",
		"Laid out as `total, on, off`; in declared order it would be 24 bytes.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("hover over Loose missing %q:\n%s", want, got)
		}
	}

	plain := s.hoverAt(uri, hoverSource, hoverLineCol(t, "struct Plain", "Plain"))
	if !strings.Contains(plain, "**Size:** 4 bytes (align 4)") || strings.Contains(plain, "Laid out as") {
		t.Errorf("hover over Plain:\n%s", plain)
	}
}
