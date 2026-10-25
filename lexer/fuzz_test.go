package lexer

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	for _, s := range []string{"(", "[", "{", "\"${", "\"${\"${", "/*", "'"} {
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

func FuzzTokenize(f *testing.F) {
	addExampleSeeds(f)
	f.Fuzz(func(t *testing.T, src string) {
		withTimeout(t, 10*time.Second, func() {
			_, _ = New(src).Tokenize()
			lx := NewWithFile(src, "fuzz.dx")
			lx.KeepComments = true
			_, _ = lx.Tokenize()
		})
	})
}

func TestDeepInterpolation(t *testing.T) {
	for _, n := range []int{maxInterpDepth, 100000} {
		src := strings.Repeat("\"${", n) + "1" + strings.Repeat("}\"", n)
		_, err := New(src).Tokenize()
		tooDeep := err != nil && strings.Contains(err.Error(), "nested too deep")
		if tooDeep != (n > maxInterpDepth) {
			t.Errorf("depth %d: got %v", n, err)
		}
	}
}
