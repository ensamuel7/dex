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

// The exact shape from examples/fluent_test.dx: a method naming its own struct
// as a &T return type, assigning through self, and returning self.
const fluentDiagSource = `import "fmt"

struct Stmt {
    table: string
    cols: string

    fn into(name: string): &Stmt {
        self.table = name
        return self
    }

    fn col(name: string): &Stmt {
        if (self.cols.isEmpty()) {
            self.cols = name
        } else {
            self.cols = self.cols + ", " + name
        }
        return self
    }

    fn text(): string {
        return "INSERT INTO " + self.table + " (" + self.cols + ")"
    }
}

fn main(): void {
    let s: Stmt = Stmt { table: "", cols: "" }
    s.into("users").col("id").col("email")
    fmt.println(s.text())
    fmt.println(s.cols.len())
}
`

func fluentDiagnostics(t *testing.T, source string) []Diagnostic {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fluent.dx")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToURI(path)

	buf := &bytes.Buffer{}
	s := &Server{
		documents:    map[string]string{uri: source},
		writer:       buf,
		logger:       log.New(io.Discard, "", 0),
		importedURIs: map[string][]string{},
	}
	s.diagnose(uri, source)

	var out []Diagnostic
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
		out = append(out, msg.Params.Diagnostics...)
	}
	return out
}

// The LSP shares the compiler front end, so a stale rule here shows up as red
// squiggles on code that compiles.
func TestNoFalseDiagnosticsOnFluent(t *testing.T) {
	for _, d := range fluentDiagnostics(t, fluentDiagSource) {
		t.Errorf("unexpected diagnostic at %d:%d — %s",
			d.Range.Start.Line+1, d.Range.Start.Character+1, d.Message)
	}
}

// Chaining off a call result must not be reported either.
func TestNoFalseDiagnosticsOnChaining(t *testing.T) {
	src := `import "fmt"
import "str"

fn main(): void {
    let sb = StringBuilder()
    sb.append("hi")
    fmt.println(sb.toString().len())
    fmt.println(str.fromInt(42).trim().len())
    let csv: string = "a,bb,ccc"
    fmt.println(csv.split(",").len())
    fmt.println(csv.split(",")[2].len())
    fmt.println("  mixed  ".trim().toUpper())
}
`
	for _, d := range fluentDiagnostics(t, src) {
		t.Errorf("unexpected diagnostic at %d:%d — %s",
			d.Range.Start.Line+1, d.Range.Start.Character+1, d.Message)
	}
}

// A genuine mistake must still be reported — proof the checks above are not
// passing because diagnostics stopped working.
func TestFluentStillReportsRealErrors(t *testing.T) {
	src := `struct Stmt {
    table: string
    fn into(name: string): &Stmt {
        self.nope = name
        return self
    }
}
fn main(): void {
    let s: Stmt = Stmt { table: "" }
    s.into("x")
}
`
	diags := fluentDiagnostics(t, src)
	if len(diags) == 0 {
		t.Fatal("assigning to a field that does not exist produced no diagnostic")
	}
	var joined []string
	for _, d := range diags {
		joined = append(joined, d.Message)
	}
	if !strings.Contains(strings.Join(joined, "; "), "nope") {
		t.Errorf("diagnostics do not mention the bad field: %v", joined)
	}
}
