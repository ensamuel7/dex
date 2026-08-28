package lsp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeProject lays out a small multi-file project and returns the root dir.
func writeProject(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// defineAt runs textDocument/definition at a position and returns the resolved
// file path and 0-based line, or "" when nothing resolved.
func defineAt(t *testing.T, dir, mainRel string, line, char int) (string, int) {
	t.Helper()
	mainPath := filepath.Join(dir, mainRel)
	src, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	uri := pathToURI(mainPath)

	var captured map[string]interface{}
	s := &Server{
		documents: map[string]string{uri: string(src)},
		respond: func(id *json.RawMessage, result interface{}) {
			if m, ok := result.(map[string]interface{}); ok {
				captured = m
			}
		},
	}
	params, _ := json.Marshal(map[string]interface{}{
		"textDocument": map[string]string{"uri": uri},
		"position":     map[string]int{"line": line, "character": char},
	})
	raw := json.RawMessage(params)
	s.handleDefinition(&jsonrpcMessage{Params: raw})

	if captured == nil {
		return "", 0
	}
	gotURI, _ := captured["uri"].(string)
	rng, _ := captured["range"].(Range)
	return uriToPath(gotURI), rng.Start.Line
}

// The reported bug: a module-qualified call could not be followed to the file
// that defines it, because only the open document was ever searched.
func TestDefinitionFollowsModuleQualifiedCall(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"main.dx": `import "service/chargerService"

fn main(): void {
    chargerService.init(1)
}
`,
		"service/chargerService.dx": `let dbConn: int = 0

fn init(conn: int): void {
    dbConn = conn
}
`,
	})

	// Cursor on `init` in `chargerService.init(1)`.
	gotFile, gotLine := defineAt(t, dir, "main.dx", 3, 19)
	want := filepath.Join(dir, "service/chargerService.dx")
	if gotFile != want {
		t.Fatalf("file = %q, want %q", gotFile, want)
	}
	if gotLine != 2 {
		t.Errorf("line = %d, want 2 (the `fn init` line)", gotLine)
	}
}

// Putting the cursor on the module name itself opens that module's file.
func TestDefinitionOnModuleNameOpensItsFile(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"main.dx": `import "service/chargerService"

fn main(): void {
    chargerService.init(1)
}
`,
		"service/chargerService.dx": "fn init(conn: int): void {\n}\n",
	})

	gotFile, gotLine := defineAt(t, dir, "main.dx", 3, 6)
	want := filepath.Join(dir, "service/chargerService.dx")
	if gotFile != want {
		t.Fatalf("file = %q, want %q", gotFile, want)
	}
	if gotLine != 0 {
		t.Errorf("line = %d, want 0", gotLine)
	}
}

// A struct type used unqualified still resolves into the module that declares it.
func TestDefinitionFindsStructInImportedModule(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"main.dx": `import "types/charger"

fn main(): void {
    let c: charger.Charger = charger.Charger{id: 1}
}
`,
		"types/charger.dx": `struct Charger {
    id: int
}
`,
	})

	// Cursor on the `Charger` of the type annotation.
	gotFile, gotLine := defineAt(t, dir, "main.dx", 3, 20)
	want := filepath.Join(dir, "types/charger.dx")
	if gotFile != want {
		t.Fatalf("file = %q, want %q", gotFile, want)
	}
	if gotLine != 0 {
		t.Errorf("line = %d, want 0", gotLine)
	}
}

// Definitions in the open file still win over same-named ones elsewhere.
func TestDefinitionPrefersLocalFile(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"main.dx": `import "other"

fn helper(): int {
    return 1
}

fn main(): void {
    let x: int = helper()
}
`,
		"other.dx": "fn helper(): int {\n    return 2\n}\n",
	})

	gotFile, gotLine := defineAt(t, dir, "main.dx", 7, 18)
	want := filepath.Join(dir, "main.dx")
	if gotFile != want {
		t.Fatalf("file = %q, want %q", gotFile, want)
	}
	if gotLine != 2 {
		t.Errorf("line = %d, want 2", gotLine)
	}
}

// Transitive imports are reachable: main imports a handler, which imports a
// service, and the service's functions are still jumpable from the handler.
func TestDefinitionFollowsTransitiveImports(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"main.dx": `import "handler/api"

fn main(): void {
    api.start()
}
`,
		"handler/api.dx": `import "../service/charger"

fn start(): void {
    charger.boot()
}
`,
		"service/charger.dx": "fn boot(): void {\n}\n",
	})

	gotFile, _ := defineAt(t, dir, "handler/api.dx", 3, 13)
	want := filepath.Join(dir, "service/charger.dx")
	if gotFile != want {
		t.Fatalf("file = %q, want %q", gotFile, want)
	}
}
