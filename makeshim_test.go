package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The makefile shape gcc's lto-wrapper writes: one rule per partition, two
// recipe lines each (the second ignoring failure), and an "all" listing them
// over continuation lines.
func TestMiniMakeRunsLtoWrapperMakefile(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.o"), filepath.Join(dir, "b.o")
	mk := filepath.Join(dir, "x.mk")
	os.WriteFile(mk, []byte(a+":\n\t@echo a > '"+a+"'\n\t@-false\n"+
		b+":\n\t@echo b > '"+b+"'\n"+
		".PHONY: all\nall: \\\n\t"+a+" \\\n\t"+b+"\n"), 0644)
	if code := miniMake([]string{"-f", mk, "-j4", "all"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, f := range []string{a, b} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("%s not made: %v", f, err)
		}
	}

	os.WriteFile(mk, []byte(a+":\n\t@false\nall: "+a+"\n"), 0644)
	if code := miniMake([]string{"-f", mk, "-j2", "all"}); code == 0 {
		t.Error("a failing recipe should fail the make")
	}
}
