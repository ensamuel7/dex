package lsp

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const reflectFeatureSource = `import "fmt"
import "reflect"

struct User {
    id: long
    email: string
    score: double
    active: bool
}

fn describe(c: &User): string {
    let sb = StringBuilder()
    let i: int = 0
    while (i < reflect.fieldCount(c)) {
        sb.append(reflect.fieldName(c, i))
        sb.append("=")
        sb.append(reflect.toString(c, i))
        i = i + 1
    }
    return sb.toString()
}

fn main(): void {
    let unit: User = User { id: 1, email: "a", score: 1.0, active: true }
    let name: string = reflect.typeName(unit)
    let n: int = reflect.fieldCount(unit)
    let names: string[] = reflect.fields(unit)
    let kind: string = reflect.fieldKind(unit, 0)
    let idx: int = reflect.indexOf(unit, "email")
    let present: bool = reflect.has(unit, "id")
    let id: long = reflect.asLong(unit, 0)
    let tag: string = reflect.asString(unit, 1)
    let kw: double = reflect.asDouble(unit, 2)
    let live: bool = reflect.asBool(unit, 3)
    let shown: string = reflect.toString(unit, 0)
    fmt.println(describe(unit) + name + kind + tag + shown)
}
`

// The editor extension is a thin LSP client, so everything an editor shows for
// the reflect module has to come from the server. Completion must offer the
// whole surface.
func TestReflectApiSurfacedByLSP(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "m.dx")
	if err := os.WriteFile(path, []byte(reflectFeatureSource), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToURI(path)
	s := &Server{documents: map[string]string{uri: reflectFeatureSource}}

	var labels []string
	for _, it := range s.moduleCompletions("reflect", reflectFeatureSource, uri) {
		labels = append(labels, it.Label)
	}
	joined := strings.Join(labels, ",")
	for _, want := range []string{
		"typeName", "fieldCount", "fields", "fieldName", "fieldKind",
		"indexOf", "has", "asInt", "asLong", "asDouble", "asBool",
		"asString", "toString",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("completion missing reflect.%s; got: %s", want, joined)
		}
	}
}

// reflect is on the list of modules an editor offers to import.
func TestReflectOfferedAsModule(t *testing.T) {
	found := false
	for _, name := range sortedModuleNames() {
		if name == "reflect" {
			found = true
		}
	}
	if !found {
		t.Errorf("reflect is not offered as a module; got: %v", sortedModuleNames())
	}
}

// Every reflect function is polymorphic in its subject, so hover must render the
// written-out signature rather than an empty parameter list.
func TestReflectHoverRendersPolymorphicSignature(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "m.dx")
	if err := os.WriteFile(path, []byte(reflectFeatureSource), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToURI(path)
	s := &Server{documents: map[string]string{uri: reflectFeatureSource}}

	lines := strings.Split(reflectFeatureSource, "\n")
	for name, wantSig := range map[string]string{
		"fieldCount": "reflect.fieldCount(value: struct|&struct): int",
		"asString":   "reflect.asString(value: struct|&struct, index: int): string",
		"indexOf":    "reflect.indexOf(value: struct|&struct, name: string): int",
		"fields":     "reflect.fields(value: struct|&struct): string[]",
	} {
		line, col := -1, -1
		for i, l := range lines {
			if c := strings.Index(l, "reflect."+name); c >= 0 {
				line, col = i, c+len("reflect.")
				break
			}
		}
		if line < 0 {
			t.Fatalf("could not locate reflect.%s in source", name)
		}
		got := s.hoverAt(uri, reflectFeatureSource, Position{Line: line, Character: col + 1})
		if !strings.Contains(got, wantSig) {
			t.Errorf("hover for reflect.%s missing %q; got:\n%s", name, wantSig, got)
		}
	}
}

// The LSP shares the compiler front end, so a stale rule here shows up as red
// squiggles on valid code in the editor.
func TestNoFalseDiagnosticsOnReflect(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "features.dx")
	if err := os.WriteFile(path, []byte(reflectFeatureSource), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToURI(path)

	buf := &bytes.Buffer{}
	s := &Server{
		documents:    map[string]string{uri: reflectFeatureSource},
		writer:       buf,
		logger:       log.New(io.Discard, "", 0),
		importedURIs: map[string][]string{},
	}
	s.diagnose(uri, reflectFeatureSource)

	frame := regexp.MustCompile(`(?s)Content-Length: \d+\r\n\r\n`)
	for _, part := range frame.Split(buf.String(), -1) {
		if part == "" {
			continue
		}
		var msg struct {
			Params struct {
				Diagnostics []Diagnostic `json:"diagnostics"`
			} `json:"params"`
		}
		if err := json.Unmarshal([]byte(part), &msg); err != nil {
			continue
		}
		for _, d := range msg.Params.Diagnostics {
			t.Errorf("unexpected diagnostic at %d:%d — %s",
				d.Range.Start.Line+1, d.Range.Start.Character+1, d.Message)
		}
	}
}
