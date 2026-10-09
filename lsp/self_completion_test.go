package lsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const selfSource = `struct Query {
    from: string
    max: int

    fn table(name: string): &Query {
        self.from = name
        return self
    }

    fn limit(n: int): &Query {
        self.
        return self
    }
}

fn outside(): int {
    return 1
}
`

func selfServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "m.dx")
	if err := os.WriteFile(path, []byte(selfSource), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToURI(path)
	return &Server{documents: map[string]string{uri: selfSource}}, uri
}

// `self.` reaches the enclosing struct's fields and its other methods. Before,
// it was looked up as a module name and completed to nothing.
func TestSelfDotCompletesFieldsAndMethods(t *testing.T) {
	s, uri := selfServer(t)
	// The bare `self.` line inside fn limit, found rather than hardcoded so the
	// test survives an edit to the fixture above it.
	lines := strings.Split(selfSource, "\n")
	line := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "self." {
			line = i
			break
		}
	}
	if line < 0 {
		t.Fatal("test fixture has no bare `self.` line")
	}
	col := strings.Index(lines[line], "self.") + len("self.")
	var labels []string
	for _, it := range s.completionsAt(selfSource, Position{Line: line, Character: col}, uri) {
		labels = append(labels, it.Label)
	}
	joined := strings.Join(labels, ",")
	for _, want := range []string{"from", "max", "table", "limit"} {
		if !strings.Contains(joined, want) {
			t.Errorf("self. completion missing %q; got: %s", want, joined)
		}
	}
}

// `self` is offered inside a struct body.
func TestSelfOfferedInsideStruct(t *testing.T) {
	s, uri := selfServer(t)
	var found bool
	for _, it := range s.completionsAt(selfSource, Position{Line: 5, Character: 8}, uri) {
		if it.Label == "self" {
			found = true
			if !strings.Contains(it.Detail, "Query") {
				t.Errorf("self detail = %q, want it to name Query", it.Detail)
			}
		}
	}
	if !found {
		t.Error("self was not offered inside a struct method")
	}
}

// And not offered outside one, where it would not compile.
func TestSelfNotOfferedOutsideStruct(t *testing.T) {
	s, uri := selfServer(t)
	line := 25 // inside fn outside
	for _, it := range s.completionsAt(selfSource, Position{Line: line, Character: 4}, uri) {
		if it.Label == "self" {
			t.Error("self was offered outside a struct body")
		}
	}
}

func TestEnclosingStructName(t *testing.T) {
	cases := map[int]string{
		0:  "Query", // the struct header line itself
		1:  "Query",
		5:  "Query",
		25: "", // inside fn outside, after the struct closed
	}
	for line, want := range cases {
		if got := enclosingStructName(selfSource, line); got != want {
			t.Errorf("enclosingStructName(line %d) = %q, want %q", line, got, want)
		}
	}
}
