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

const jsonFeatureSource = `import "json"

struct Connector {
    id: int
    kind: string
}

struct Charger {
    tag: string
    connectors: int[]
    names: string[]
    ports: Connector[]
}

fn main(): void {
    let ids: int[] = [1, 2]
    let c: Charger = Charger { tag: "a", connectors: ids, names: ["x"], ports: [Connector{id: 1, kind: "AC"}] }
    let s: string = json.encode(c)
    let one: Charger? = json.decode(s)
    if (one != null) {
        let n: int = one.connectors.len()
        assert(n == 2)
    }
    let many: Connector[] = json.decode("[]")
    let m: int = many.len()
    assert(m == 0)
}
`

// The renamed encode/decode API must be what completion and hover offer, and the
// removed names must be gone.
func TestJSONApiSurfacedByLSP(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "m.dx")
	if err := os.WriteFile(path, []byte(jsonFeatureSource), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToURI(path)
	s := &Server{documents: map[string]string{uri: jsonFeatureSource}}

	var labels []string
	for _, it := range s.moduleCompletions("json", jsonFeatureSource, uri) {
		labels = append(labels, it.Label)
	}
	joined := strings.Join(labels, ",")
	for _, want := range []string{"encode", "decode"} {
		if !strings.Contains(joined, want) {
			t.Errorf("completion missing %q; got: %s", want, joined)
		}
	}
	for _, gone := range []string{"objectify", "stringify"} {
		if strings.Contains(joined, gone) {
			t.Errorf("completion still offers removed %q: %s", gone, joined)
		}
	}

	// Hover must render the polymorphic signatures, not a placeholder param type.
	lines := strings.Split(jsonFeatureSource, "\n")
	for name, wantSig := range map[string]string{
		"encode": "json.encode(value: T[]|struct|map[string, V]): string",
		"decode": "json.decode(json: string)",
	} {
		line, col := -1, -1
		for i, l := range lines {
			if c := strings.Index(l, "json."+name); c >= 0 {
				line, col = i, c+len("json.")
				break
			}
		}
		if line < 0 {
			t.Fatalf("could not locate json.%s in source", name)
		}
		got := s.hoverAt(uri, jsonFeatureSource, Position{Line: line, Character: col + 1})
		if !strings.Contains(got, wantSig) {
			t.Errorf("hover for json.%s missing %q; got:\n%s", name, wantSig, got)
		}
	}
}

// Array struct fields, checked decode and struct-array decode must not produce
// diagnostics — the LSP shares the compiler front end, so a stale rule here
// shows up as red squiggles on valid code.
func TestNoFalseDiagnosticsOnJSONFeatures(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "features.dx")
	if err := os.WriteFile(path, []byte(jsonFeatureSource), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToURI(path)

	buf := &bytes.Buffer{}
	s := &Server{
		documents:    map[string]string{uri: jsonFeatureSource},
		writer:       buf,
		logger:       log.New(io.Discard, "", 0),
		importedURIs: map[string][]string{},
	}
	s.diagnose(uri, jsonFeatureSource)

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
