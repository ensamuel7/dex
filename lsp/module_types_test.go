package lsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeModule writes a .dx file into dir and returns its path.
func writeModule(t *testing.T, dir, name, src string) string {
	t.Helper()
	p := filepath.Join(dir, name+".dx")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// A user module that imports another user module and uses its struct type
// through a qualified annotation (ocppMessage.OcppMessage) must parse cleanly
// when diagnosed on its own, exactly as it does through `dex build`.
func TestDiagnoseImportedFileResolvesQualifiedUserTypes(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "ocppMessage", `struct OcppMessage {
    action: string
}

fn make(action: string): OcppMessage {
    return OcppMessage { action: action }
}
`)
	handler := writeModule(t, dir, "handler", `import "ocppMessage"

fn describe(m: ocppMessage.OcppMessage): string {
    return m.action
}
`)

	if diags := diagnoseImportedFile(handler); len(diags) > 0 {
		t.Fatalf("expected no diagnostics, got %+v", diags)
	}
}

// Hovering a user-module function call (module.name) must report the function
// signature. ResolveUserModules merges module functions under a "<module>_<name>"
// prefix, so a lookup by the bare name finds nothing.
func TestHoverUserModuleFunction(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "ocppMessage", `struct OcppMessage {
    action: string
}

fn parseMessage(raw: string): OcppMessage {
    return OcppMessage { action: raw }
}
`)
	mainPath := writeModule(t, dir, "main", "")

	text := `import "ocppMessage"

fn main(): void {
    let m: ocppMessage.OcppMessage = ocppMessage.parseMessage("Boot")
}
`
	// Position the cursor inside "parseMessage" on line 4 (0-based line 3).
	line := 3
	col := strings.Index(strings.Split(text, "\n")[line], "parseMessage(")

	s := &Server{}
	got := s.hoverAt(pathToURI(mainPath), text, Position{Line: line, Character: col + 1})
	if !strings.Contains(got, "fn ocppMessage.parseMessage(raw: string): OcppMessage") {
		t.Fatalf("expected function signature in hover, got:\n%s", got)
	}
}

// Member completion for a user module must still work when that module's own
// source depends on types from a further user module.
func TestUserModuleCompletionsWithQualifiedUserTypes(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "ocppMessage", `struct OcppMessage {
    action: string
}
`)
	writeModule(t, dir, "handler", `import "ocppMessage"

fn describe(m: ocppMessage.OcppMessage): string {
    return m.action
}
`)
	mainPath := writeModule(t, dir, "main", "")

	text := `import "handler"

fn main(): void {
    handler.
}
`
	s := &Server{}
	items := s.userModuleCompletions("handler", text, pathToURI(mainPath))
	found := false
	for _, it := range items {
		if it.Label == "describe" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 'describe' completion from handler module, got %+v", items)
	}
}
