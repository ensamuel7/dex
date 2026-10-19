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
// through a qualified annotation (wireMessage.WireMessage) must parse cleanly
// when diagnosed on its own, exactly as it does through `dex build`.
func TestDiagnoseImportedFileResolvesQualifiedUserTypes(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "wireMessage", `struct WireMessage {
    action: string
}

fn make(action: string): WireMessage {
    return WireMessage { action: action }
}
`)
	handler := writeModule(t, dir, "handler", `import "wireMessage"

fn describe(m: wireMessage.WireMessage): string {
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
	writeModule(t, dir, "wireMessage", `struct WireMessage {
    action: string
}

fn parseMessage(raw: string): WireMessage {
    return WireMessage { action: raw }
}
`)
	mainPath := writeModule(t, dir, "main", "")

	text := `import "wireMessage"

fn main(): void {
    let m: wireMessage.WireMessage = wireMessage.parseMessage("Boot")
}
`
	// Position the cursor inside "parseMessage" on line 4 (0-based line 3).
	line := 3
	col := strings.Index(strings.Split(text, "\n")[line], "parseMessage(")

	s := &Server{}
	got := s.hoverAt(pathToURI(mainPath), text, Position{Line: line, Character: col + 1})
	// The return type is shown the way the file writes it: a type declared in a
	// module carries that module, now that two modules may each declare one.
	if !strings.Contains(got, "fn wireMessage.parseMessage(raw: string): wireMessage.WireMessage") {
		t.Fatalf("expected function signature in hover, got:\n%s", got)
	}
}

// Member completion for a user module must still work when that module's own
// source depends on types from a further user module.
func TestUserModuleCompletionsWithQualifiedUserTypes(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "wireMessage", `struct WireMessage {
    action: string
}
`)
	writeModule(t, dir, "handler", `import "wireMessage"

fn describe(m: wireMessage.WireMessage): string {
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

// The imported file's standalone syntax check must not leave its types registered bare.
func TestDiagnoseModuleEnumFieldMatchesQualifiedVariant(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"front.dx": `import "util"

fn isSmall(): bool {
    return util.make().kind == util.Kind.Small
}
`,
		"util.dx": `enum Kind {
    Small
    Large
}

struct Item {
    kind: Kind
}

fn make(): Item {
    return Item { kind: Kind.Small }
}
`,
	})
	s, uri, _ := openAndDiagnose(t, dir, "front.dx")
	if d := s.lastDiags[uri]; len(d) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", d)
	}
}
