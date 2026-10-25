package parser

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ensamuel7/dex/lexer"
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
	for _, s := range []string{"(", "[", "{", "-", "!", "\"${", "fn f() { ", "fn f() { if (true) { ", "fn f(): int { return (", "fn f() { let a = [", "fn f() { let a = -", "fn f() { let a = x."} {
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

func FuzzParse(f *testing.F) {
	addExampleSeeds(f)
	f.Fuzz(func(t *testing.T, src string) {
		tokens, err := lexer.New(src).Tokenize()
		if err != nil {
			return
		}
		withTimeout(t, 10*time.Second, func() {
			_, _ = New(tokens).Parse()
		})
	})
}

func TestDeepNesting(t *testing.T) {
	shapes := map[string]func(n int) string{
		"paren": func(n int) string {
			return "fn main(): void { let a = " + strings.Repeat("(", n) + "1" + strings.Repeat(")", n) + " }"
		},
		"array": func(n int) string {
			return "fn main(): void { let a = " + strings.Repeat("[", n) + "1" + strings.Repeat("]", n) + " }"
		},
		"neg": func(n int) string { return "fn main(): void { let a = " + strings.Repeat("- ", n) + "1 }" },
		"not": func(n int) string { return "fn main(): void { let a = " + strings.Repeat("! ", n) + "true }" },
		"block": func(n int) string {
			return "fn main(): void { " + strings.Repeat("if (true) { ", n) + strings.Repeat("}", n) + " }"
		},
		"type": func(n int) string {
			return "fn f(g: " + strings.Repeat("fn(", n) + "int" + strings.Repeat("): int", n) + "): void {}"
		},
	}
	for name, build := range shapes {
		for _, n := range []int{maxNestingDepth / 2, 100000} {
			tokens, err := lexer.New(build(n)).Tokenize()
			if err != nil {
				t.Fatalf("%s %d: %v", name, n, err)
			}
			_, errs := New(tokens).Parse()
			tooDeep := false
			for _, e := range errs {
				if strings.Contains(e.Error(), "nesting too deep") {
					tooDeep = true
				}
			}
			if tooDeep != (n > maxNestingDepth) || (n <= maxNestingDepth && len(errs) > 0) {
				t.Errorf("%s %d: errors %v", name, n, errs)
			}
		}
	}
}
