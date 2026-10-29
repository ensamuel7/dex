package lsp

import (
	"strings"
	"testing"
)

func TestCompletionOffersHttpServingModes(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"main.dx": `import "http"

fn main(): void {
    http.
}
`,
	})
	items := completionsFor(t, dir, "main.dx", 3, 9)
	for _, name := range []string{"inline", "uring", "limits"} {
		item := findItem(items, name)
		if item == nil {
			t.Fatalf("completion after http. did not offer %s", name)
		}
		if !strings.Contains(item.Detail, "void") {
			t.Fatalf("%s carries no signature: %+v", name, item)
		}
	}
}
