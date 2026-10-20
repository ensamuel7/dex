package lsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ensamuel7/dex/ast"
)

const receiverHoverSource = `fn main(): void {
    let csv: string = " a,b "
    let cols: string[] = csv.split(",")
    let stock: map[string, int] = {}
    stock.set("pear", 4)
    let ids: map[int, string] = {}
    let names: string[] = ids.values()
    let sb = StringBuilder()
    sb.append(1)
    let out: string = sb.toString()
    let mu: mutex = mutex
}
`

// receiverHover hovers word on the first line of receiverHoverSource holding
// needle.
func receiverHover(t *testing.T, needle, word string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "b.dx")
	if err := os.WriteFile(path, []byte(receiverHoverSource), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToURI(path)
	s := &Server{documents: map[string]string{uri: receiverHoverSource}}
	for i, line := range strings.Split(receiverHoverSource, "\n") {
		if strings.Contains(line, needle) {
			return s.hoverAt(uri, receiverHoverSource, Position{Line: i, Character: strings.Index(line, word)})
		}
	}
	t.Fatalf("no line holds %q", needle)
	return ""
}

func TestHoverStringMethod(t *testing.T) {
	got := receiverHover(t, "csv.split", "split")
	if !strings.Contains(got, "(method) string.split(delimiter: string): string[]") {
		t.Errorf("got:\n%s", got)
	}
}

// A map's methods carry its own key and value types, not a generic K and V.
func TestHoverMapMethodFillsKeyAndValue(t *testing.T) {
	if got := receiverHover(t, "stock.set", "set"); !strings.Contains(got, "(method) map[string, int].set(key: string, value: int): void") {
		t.Errorf("got:\n%s", got)
	}
	if got := receiverHover(t, "ids.values", "values"); !strings.Contains(got, "(method) map[int, string].values(): string[]") {
		t.Errorf("got:\n%s", got)
	}
}

func TestHoverMapVariableListsMethods(t *testing.T) {
	got := receiverHover(t, "let stock", "stock")
	for _, want := range []string{"(variable) stock: map[string, int]", "Map from `string` to `int`", "get(key: string): int", "keys(): string[]"} {
		if !strings.Contains(got, want) {
			t.Errorf("hover missing %q:\n%s", want, got)
		}
	}
}

// StringBuilder() used to hover as `unknown`: typeName had no case for it.
func TestHoverStringBuilder(t *testing.T) {
	got := receiverHover(t, "let sb", "sb")
	if !strings.Contains(got, "(variable) sb: StringBuilder") || !strings.Contains(got, "toString(): string") {
		t.Errorf("got:\n%s", got)
	}
	if got := receiverHover(t, "sb.append", "append"); !strings.Contains(got, "(method) StringBuilder.append(") {
		t.Errorf("got:\n%s", got)
	}
}

func TestHoverMutexHasItsType(t *testing.T) {
	if got := receiverHover(t, "let mu", "mu"); !strings.Contains(got, "Type: `mutex`") {
		t.Errorf("got:\n%s", got)
	}
}

// A string hover stays short: it does not list all twenty-one methods.
func TestHoverStringVariableIsShort(t *testing.T) {
	got := receiverHover(t, "let csv", "csv")
	if !strings.Contains(got, "Type: `string`") || strings.Contains(got, "**Methods:**") {
		t.Errorf("got:\n%s", got)
	}
}

func TestHoverUnknownMethodOnBuiltinIsEmpty(t *testing.T) {
	if _, ok := builtinMethodOf(ast.TypeString, "nope"); ok {
		t.Error("a string has no method 'nope'")
	}
}

// Completion after a dot it cannot type offers the same rows hover reads,
// labelled with the receiver they belong to.
func TestBuiltinMethodCompletionsShareTheTables(t *testing.T) {
	want := map[string]bool{
		"T[].push(value: T): void":                       false,
		"string.trim(): string":                          false,
		"map[K, V].set(key: K, value: V): void":          false,
		"StringBuilder.toString(): string":               false,
		"map[K, V].remove(key: K): void":                 false,
		"T[].remove(index: int): void":                   false,
		"string.substring(start: int, end: int): string": false,
	}
	for _, it := range builtinMethodCompletions() {
		if _, ok := want[it.Detail]; ok {
			want[it.Detail] = true
		}
		if it.Documentation == "" {
			t.Errorf("%s has no documentation", it.Detail)
		}
	}
	for detail, seen := range want {
		if !seen {
			t.Errorf("completion is missing %q", detail)
		}
	}
}
