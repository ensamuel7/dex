package profview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitSite(t *testing.T) {
	cases := []struct {
		in   string
		file string
		line int
	}{
		{"main.dx:12", "main.dx", 12},
		{"lib/util.dx:3", "lib/util.dx", 3},
		{"C:/code/a.dx:7", "C:/code/a.dx", 7},
		{"<program start>", "", 0},
		{"<other sites>", "", 0},
		{"main.dx:0", "", 0},
		{"main.dx:x", "", 0},
	}
	for _, c := range cases {
		f, l := SplitSite(c.in)
		if f != c.file || l != c.line {
			t.Errorf("SplitSite(%q) = %q, %d; want %q, %d", c.in, f, l, c.file, c.line)
		}
	}
}

func TestSourcesReadsRelativeToBase(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.dx"), []byte("fn main(): void {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &Profile{Sites: []Site{{"app.dx:1"}, {"app.dx:2"}, {"gone.dx:4"}, {"<program start>"}}}
	got := Sources(p, dir)
	if len(got) != 1 || got["app.dx"] != "fn main(): void {}\n" {
		t.Errorf("Sources = %v; want only app.dx", got)
	}
}

// A source file is user text placed inside a <script> element, so it must not
// be able to end that element early.
func TestRenderKeepsSourceInsideTheScript(t *testing.T) {
	dir := t.TempDir()
	evil := "let s = \"</script><script>alert(1)</script>\"\n"
	if err := os.WriteFile(filepath.Join(dir, "x.dx"), []byte(evil), 0o644); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"version":1,"duration_ms":1,"interval_ms":1,"total":{"allocs":1,"frees":1,"alloc_bytes":32,"live_objects":0,"live_bytes":0,"peak_bytes":32,"peak_ms":0},"sites":[{"site":"x.dx:1","allocs":1,"frees":1,"alloc_bytes":32,"live_bytes":0,"peak_bytes":32,"at_peak":32}],"timeline":[[0,0,0],[1,0,32]]}`)
	page, err := Render(raw, "x</title>", dir)
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	if strings.Count(html, "</script>") != 1 {
		t.Errorf("source text closed the script element early")
	}
	if strings.Contains(html, "x</title> memory") {
		t.Errorf("title was not escaped")
	}
	if !strings.Contains(html, `"x.dx:1"`) {
		t.Errorf("profile data missing from the page")
	}
}

func TestRenderRejectsOtherJSON(t *testing.T) {
	if _, err := Render([]byte(`{"version":2}`), "x", ""); err == nil {
		t.Error("expected an error for an unknown version")
	}
	if _, err := Render([]byte(`not json`), "x", ""); err == nil {
		t.Error("expected an error for invalid JSON")
	}
}
