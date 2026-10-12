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
