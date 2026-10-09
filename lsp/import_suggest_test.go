package lsp

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// openAndDiagnose loads one file of a project into a server and runs the
// diagnostic pass, so the code-action tests work from real checker errors.
func openAndDiagnose(t *testing.T, dir, rel string) (*Server, string, string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	uri := pathToURI(path)
	s := &Server{
		documents:    map[string]string{uri: string(src)},
		writer:       io.Discard,
		logger:       log.New(io.Discard, "", 0),
		importedURIs: map[string][]string{},
		lastDiags:    map[string][]Diagnostic{},
	}
	s.diagnose(uri, string(src))
	return s, uri, string(src)
}

// codeActionsAt asks for the quick fixes offered at a 0-based position, with no
// client-supplied diagnostics so the server falls back on what it published.
func codeActionsAt(t *testing.T, s *Server, uri string, line, char int) []CodeAction {
	t.Helper()
	var captured []CodeAction
	s.respond = func(id *json.RawMessage, result interface{}) {
		captured, _ = result.([]CodeAction)
	}
	params, _ := json.Marshal(map[string]interface{}{
		"textDocument": map[string]string{"uri": uri},
		"range": map[string]interface{}{
			"start": map[string]int{"line": line, "character": char},
			"end":   map[string]int{"line": line, "character": char},
		},
		"context": map[string]interface{}{"diagnostics": []Diagnostic{}},
	})
	raw := json.RawMessage(params)
	s.handleCodeAction(&jsonrpcMessage{Params: raw})
	return captured
}

func findAction(actions []CodeAction, title string) *CodeAction {
	for i := range actions {
		if actions[i].Title == title {
			return &actions[i]
		}
	}
	return nil
}

func titles(actions []CodeAction) []string {
	var out []string
	for _, a := range actions {
		out = append(out, a.Title)
	}
	return out
}

func TestCodeActionImportsStdlibModule(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"main.dx": `fn main(): void {
    fmt.println("hi")
}
`,
	})
	s, uri, _ := openAndDiagnose(t, dir, "main.dx")

	actions := codeActionsAt(t, s, uri, 1, 5)
	act := findAction(actions, `Import "fmt"`)
	if act == nil {
		t.Fatalf("no import fix for an unimported stdlib module, got %v", titles(actions))
	}
	edits := act.Edit.Changes[uri]
	if len(edits) != 1 || edits[0].NewText != "import \"fmt\"\n" || edits[0].Range.Start.Line != 0 {
		t.Fatalf("unexpected import edit: %+v", edits)
	}
}

func TestCodeActionImportsUserModule(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"main.dx": `fn main(): void {
    userService.init(1)
}
`,
		"service/userService.dx": `fn init(conn: int): void {
}
`,
	})
	s, uri, _ := openAndDiagnose(t, dir, "main.dx")

	actions := codeActionsAt(t, s, uri, 1, 5)
	act := findAction(actions, `Import "service/userService"`)
	if act == nil {
		t.Fatalf("no import fix for an unimported user module, got %v", titles(actions))
	}
	if got := act.Edit.Changes[uri][0].NewText; got != "import \"service/userService\"\n" {
		t.Fatalf("unexpected import edit %q", got)
	}
}

// An unqualified call never resolves in Dex, so the fix has to both import the
// module and route the call through it.
func TestCodeActionQualifiesBareStdlibCall(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"main.dx": `fn main(): void {
    println("hi")
}
`,
	})
	s, uri, _ := openAndDiagnose(t, dir, "main.dx")

	actions := codeActionsAt(t, s, uri, 1, 5)
	act := findAction(actions, `Import "fmt" and use fmt.println`)
	if act == nil {
		t.Fatalf("no auto-import fix for a bare stdlib call, got %v", titles(actions))
	}
	edits := act.Edit.Changes[uri]
	if len(edits) != 2 {
		t.Fatalf("expected an import and a qualify edit, got %+v", edits)
	}
	if edits[1].NewText != "fmt.println" || edits[1].Range.Start.Line != 1 || edits[1].Range.Start.Character != 4 {
		t.Fatalf("qualify edit does not cover the call: %+v", edits[1])
	}
}

func TestCodeActionQualifiesBareUserCall(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"main.dx": `fn main(): void {
    start(1)
}
`,
		"service/userService.dx": `fn start(conn: int): void {
}
`,
	})
	s, uri, _ := openAndDiagnose(t, dir, "main.dx")

	actions := codeActionsAt(t, s, uri, 1, 5)
	if findAction(actions, `Import "service/userService" and use userService.start`) == nil {
		t.Fatalf("no auto-import fix for a bare user-module call, got %v", titles(actions))
	}
}

// A private function is not importable, so it must not be suggested.
func TestCodeActionSkipsPrivateUserFunction(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"main.dx": `fn main(): void {
    secret(1)
}
`,
		"helpers.dx": `private fn secret(conn: int): void {
}
`,
	})
	s, uri, _ := openAndDiagnose(t, dir, "main.dx")

	if actions := codeActionsAt(t, s, uri, 1, 5); len(actions) != 0 {
		t.Fatalf("private function should not be offered: %v", titles(actions))
	}
}

// When the module is already imported the name only needs qualifying, and no
// second import line should be written.
func TestCodeActionQualifiesWithoutDuplicateImport(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"main.dx": `import "fmt"

fn main(): void {
    println("hi")
}
`,
	})
	s, uri, _ := openAndDiagnose(t, dir, "main.dx")

	actions := codeActionsAt(t, s, uri, 3, 5)
	act := findAction(actions, "Use fmt.println")
	if act == nil {
		t.Fatalf("no qualify fix for an imported module, got %v", titles(actions))
	}
	if edits := act.Edit.Changes[uri]; len(edits) != 1 {
		t.Fatalf("expected only the qualify edit, got %+v", edits)
	}
}

func completionsFor(t *testing.T, dir, rel string, line, char int) []CompletionItem {
	t.Helper()
	path := filepath.Join(dir, rel)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{documents: map[string]string{pathToURI(path): string(src)}}
	return s.completionsAt(string(src), Position{Line: line, Character: char}, pathToURI(path))
}

func findItem(items []CompletionItem, label string) *CompletionItem {
	for i := range items {
		if items[i].Label == label {
			return &items[i]
		}
	}
	return nil
}

func TestCompletionOffersUnimportedStdlibFunction(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"main.dx": `fn main(): void {
    prin
}
`,
	})
	items := completionsFor(t, dir, "main.dx", 1, 8)
	item := findItem(items, "fmt.println")
	if item == nil {
		t.Fatal("completion did not offer fmt.println for a bare prefix")
	}
	if item.FilterText != "println" {
		t.Fatalf("filterText should match what was typed, got %q", item.FilterText)
	}
	if len(item.AdditionalTextEdits) != 1 || !strings.Contains(item.AdditionalTextEdits[0].NewText, `import "fmt"`) {
		t.Fatalf("completion item carries no import edit: %+v", item.AdditionalTextEdits)
	}
}

// Once the module is imported the auto-import duplicate drops out of the list.
func TestCompletionSkipsImportedModules(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"main.dx": `import "fmt"

fn main(): void {
    prin
}
`,
	})
	items := completionsFor(t, dir, "main.dx", 3, 8)
	if findItem(items, "fmt.println") != nil {
		t.Fatal("fmt is already imported; no auto-import item expected")
	}
}

func TestCompletionOffersUnimportedUserModuleMembers(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"main.dx": `fn main(): void {
    userService.
}
`,
		"service/userService.dx": `fn start(conn: int): void {
}

private fn hidden(): void {
}
`,
	})
	items := completionsFor(t, dir, "main.dx", 1, 19)
	item := findItem(items, "start")
	if item == nil {
		t.Fatal("members of an unimported user module were not offered")
	}
	if len(item.AdditionalTextEdits) != 1 ||
		!strings.Contains(item.AdditionalTextEdits[0].NewText, `import "service/userService"`) {
		t.Fatalf("member completion carries no import edit: %+v", item.AdditionalTextEdits)
	}
	if findItem(items, "hidden") != nil {
		t.Fatal("a private function must not be offered to importers")
	}
}
