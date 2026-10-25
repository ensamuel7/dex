package format

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ensamuel7/dex/lexer"
	"github.com/ensamuel7/dex/parser"
)

func addExampleSeeds(f *testing.F) {
	_ = filepath.WalkDir("../examples", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".dx") {
			return nil
		}
		if src, err := os.ReadFile(path); err == nil {
			f.Add(string(src))
		}
		return nil
	})
	for _, s := range []string{"(", "[", "{", "\"${", "fn f() { if x { "} {
		f.Add(strings.Repeat(s, 200))
	}
}

func withTimeout(t *testing.T, d time.Duration, fn func()) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("no result after %v", d)
	}
}

func parses(src string) bool {
	tokens, err := lexer.New(src).Tokenize()
	if err != nil {
		return false
	}
	_, errs := parser.New(tokens).Parse()
	return len(errs) == 0
}

func FuzzFormat(f *testing.F) {
	addExampleSeeds(f)
	f.Fuzz(func(t *testing.T, src string) {
		withTimeout(t, 20*time.Second, func() {
			once, err := Source(src, "fuzz.dx")
			if err != nil {
				return
			}
			twice, err := Source(once, "fuzz.dx")
			if err != nil {
				t.Errorf("formatted output does not format: %v\ninput:\n%s\noutput:\n%s", err, src, once)
				return
			}
			if twice != once {
				t.Errorf("not idempotent\ninput:\n%q\nonce:\n%q\ntwice:\n%q", src, once, twice)
				return
			}
			if parses(src) && !parses(once) {
				t.Errorf("formatted output no longer parses\ninput:\n%q\noutput:\n%q", src, once)
			}
		})
	})
}
