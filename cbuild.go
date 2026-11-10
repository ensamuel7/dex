package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/csplit"
)

// Compiling the generated C. A large program is cut into one unit per core,
// compiled in parallel and joined by link-time optimisation; see
// csplit/README.md for the design and the measurements behind it.

// minUnitBytes is the least function-body text worth a unit of its own. Every
// unit also parses every declaration (about 2 MB for a program that imports
// http and db), so below this a split costs more than it saves.
const minUnitBytes = 512 << 10

// buildJobs is how many compiler processes a build may run: DEX_JOBS, or one
// per core. DEX_JOBS=1 compiles the program as one unit, as before.
func buildJobs() int {
	if v := os.Getenv("DEX_JOBS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			return n
		}
	}
	return runtime.NumCPU()
}

// DEX_SPLIT_MIN overrides minUnitBytes, so tests can split a small program.
func unitBytes() int {
	if n, err := strconv.Atoi(os.Getenv("DEX_SPLIT_MIN")); err == nil && n > 0 {
		return n
	}
	return minUnitBytes
}

var (
	clangOnce sync.Once
	isClang   bool
)

func compilerIsClang(compiler string) bool {
	clangOnce.Do(func() {
		out, _ := exec.Command(compiler, "--version").Output()
		isClang = bytes.Contains(bytes.ToLower(out), []byte("clang"))
	})
	return isClang
}

// splittable reports whether the program's C may be cut into units. A header
// of the program's own (an external module's `include "x.h"`) is pasted into
// every unit by the preprocessor, where the scanner cannot see it, and may
// define static state that must exist once, so such a program stays whole.
func splittable(program *ast.Program) bool {
	for _, em := range program.Externals {
		for _, h := range em.Includes {
			if h != "" && h[0] != '<' {
				return false
			}
		}
	}
	return true
}

// compileC compiles cCode (already written to cFile) into binaryPath. flags
// are the compile options, libflags what goes after the inputs on the link.
func compileC(compiler, cFile, cCode, binaryPath string, flags, libflags []string, split bool) error {
	if jobs := buildJobs(); split && jobs > 1 {
		units, err := csplit.Split(cCode, jobs, unitBytes())
		if err == nil && len(units) > 1 {
			diag, err := compileUnits(compiler, cFile, units, binaryPath, flags, libflags, jobs)
			if err == nil {
				os.Stderr.Write(diag)
				return nil
			}
			// A split the scanner got wrong must not cost a build: the one
			// unit it came from is still there, and compiles as it always did.
			fmt.Fprintf(os.Stderr, "dex: the parallel C build failed (%v), so this is built as one unit; DEX_JOBS=1 skips the attempt. Its output was:\n%s\n", err, firstLines(diag, 30))
		}
	}
	return compileOne(compiler, cFile, binaryPath, flags, libflags)
}

func compileOne(compiler, cFile, binaryPath string, flags, libflags []string) error {
	flags = ltoFlags(compiler, flags, false)
	args := append([]string{"-o", binaryPath}, flags...)
	args = append(args, cFile)
	args = append(args, libflags...)
	cmd := exec.Command(compiler, args...)
	cmd.Stderr = os.Stderr
	cleanup := withMake(cmd, compiler)
	defer cleanup()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("C compilation failed: %v", err)
	}
	return nil
}

// compileUnits compiles each unit to an object in parallel and links them. An
// object whose unit text and flags are unchanged since the last build is
// reused. It returns the compiler's diagnostics, each repeated one (a warning
// in a header every unit includes) shown once.
func compileUnits(compiler, cFile string, units []string, binaryPath string, flags, libflags []string, jobs int) ([]byte, error) {
	dir := strings.TrimSuffix(cFile, ".c") + ".units"
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	// A C source among the flags (DEX_SQLITE's sqlite3.c, or one an external
	// module links) is compiled as a unit of its own; an object or archive
	// goes to the link as it is.
	var sources, inputs []string
	flags = slices.DeleteFunc(slices.Clone(flags), func(f string) bool {
		if strings.HasPrefix(f, "-") {
			return false
		}
		switch filepath.Ext(f) {
		case ".c", ".cc", ".cpp", ".cxx", ".m":
			sources = append(sources, f)
			return true
		case ".o", ".a", ".so", ".dylib":
			inputs = append(inputs, f)
			return true
		}
		return false
	})
	unitFlags := append(ltoFlags(compiler, flags, true),
		// Runtime functions that were static now have external linkage; hidden
		// keeps them out of the dynamic symbol table, where a shared library
		// could otherwise bind to them instead of its own.
		"-fvisibility=hidden",
		// A static variable with no initializer becomes a tentative
		// definition, which Apple clang still puts in a common section and
		// then cannot align as the static was.
		"-fno-common")
	key := compiler + "\x00" + strings.Join(unitFlags, "\x00")

	type job struct {
		base string // object path without ".o"
		src  string // the source to compile
		text string // for a program unit, the text to write to src first
	}
	var work []job
	for i, text := range units {
		base := filepath.Join(dir, fmt.Sprintf("u%d", i))
		work = append(work, job{base, base + ".c", text})
	}
	for i, src := range sources {
		work = append(work, job{filepath.Join(dir, fmt.Sprintf("src%d", i)), src, ""})
	}

	objs := make([]string, len(work))
	diags := make([][]byte, len(work))
	errs := make([]error, len(work))
	sem := make(chan struct{}, jobs)
	var wg sync.WaitGroup
	keep := os.Getenv("DEX_KEEP_C") != ""
	for i, j := range work {
		objs[i] = j.base + ".o"
		content := j.text
		if j.text == "" {
			b, err := os.ReadFile(j.src)
			if err != nil {
				return nil, err
			}
			content = j.src + "\x00" + string(b)
		} else if keep {
			os.WriteFile(j.src, []byte(j.text), 0644)
		}
		sum := sha256.Sum256([]byte(key + "\x00" + content))
		hash := hex.EncodeToString(sum[:])
		if old, err := os.ReadFile(j.base + ".key"); err == nil && string(old) == hash {
			if _, err := os.Stat(objs[i]); err == nil {
				continue
			}
		}
		wg.Add(1)
		go func(i int, j job, hash string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			os.Remove(j.base + ".key")
			if j.text != "" {
				if err := os.WriteFile(j.src, []byte(j.text), 0644); err != nil {
					errs[i] = err
					return
				}
				if !keep {
					defer os.Remove(j.src)
				}
			}
			args := append(append([]string{}, unitFlags...), "-c", j.src, "-o", objs[i])
			var stderr bytes.Buffer
			cmd := exec.Command(compiler, args...)
			cmd.Stderr = &stderr
			errs[i] = cmd.Run()
			diags[i] = stderr.Bytes()
			if errs[i] == nil {
				os.WriteFile(j.base+".key", []byte(hash), 0644)
			}
		}(i, j, hash)
	}
	wg.Wait()
	// Objects from a build that had more units.
	for i := len(units); ; i++ {
		base := filepath.Join(dir, fmt.Sprintf("u%d", i))
		if os.Remove(base+".o") != nil {
			break
		}
		os.Remove(base + ".key")
	}
	objs = append(objs, inputs...)
	diag := dedupeDiagnostics(diags)
	for i, err := range errs {
		if err != nil {
			return diag, fmt.Errorf("%s: %v", work[i].src, err)
		}
	}

	args := append([]string{"-o", binaryPath}, linkFlags(ltoFlags(compiler, flags, true))...)
	if compilerIsClang(compiler) && runtime.GOOS == "darwin" {
		// ThinLTO's per-unit backends are most of the link; the linker
		// reuses one whose inputs have not changed (pruned by its own policy).
		args = append(args, "-Wl,-cache_path_lto,"+filepath.Join(dir, "lto"))
	}
	args = append(args, objs...)
	args = append(args, libflags...)
	var stderr bytes.Buffer
	cmd := exec.Command(compiler, args...)
	cmd.Stderr = &stderr
	cleanup := withMake(cmd, compiler)
	defer cleanup()
	err := cmd.Run()
	diag = append(diag, stderr.Bytes()...)
	if err != nil {
		return diag, fmt.Errorf("link: %v", err)
	}
	return diag, nil
}

// ltoFlags rewrites the link-time optimisation flag for the compiler. clang
// joins split units with ThinLTO, whose per-unit backends run in parallel;
// full LTO would merge them back into one serial job. gcc runs its partitions
// in parallel under -flto=auto (given a make, which withMake provides).
func ltoFlags(compiler string, flags []string, split bool) []string {
	out := make([]string, 0, len(flags))
	clang := compilerIsClang(compiler)
	for _, f := range flags {
		switch {
		case f == "-flto" || strings.HasPrefix(f, "-flto="):
			switch {
			case clang && split:
				f = "-flto=thin"
			case clang:
				// One unit: whatever was asked for, unchanged.
			default:
				f = "-flto=auto"
			}
		case f == "-flto-partition=one" && !clang:
			// Only ever chosen because make was missing; withMake supplies one.
			continue
		}
		out = append(out, f)
	}
	return out
}

// linkFlags drops the options that only mean something to the preprocessor
// or the compiler's diagnostics, which the link would warn are unused.
func linkFlags(flags []string) []string {
	return slices.DeleteFunc(slices.Clone(flags), func(f string) bool {
		return strings.HasPrefix(f, "-I") || strings.HasPrefix(f, "-D") || strings.HasPrefix(f, "-U") ||
			(strings.HasPrefix(f, "-W") && !strings.HasPrefix(f, "-Wl,"))
	})
}

// withMake gives a gcc link a make to run its partitions in parallel when the
// system has none, by putting this executable on PATH under that name (see
// makeshim.go). It returns the cleanup.
func withMake(cmd *exec.Cmd, compiler string) func() {
	if compilerIsClang(compiler) {
		return func() {}
	}
	if _, err := exec.LookPath("make"); err == nil {
		return func() {}
	}
	self, err := os.Executable()
	if err != nil {
		return func() {}
	}
	dir, err := os.MkdirTemp("", "dex-make")
	if err != nil {
		return func() {}
	}
	if err := os.Symlink(self, filepath.Join(dir, "make")); err != nil {
		os.RemoveAll(dir)
		return func() {}
	}
	env := os.Environ()
	env = slices.DeleteFunc(env, func(kv string) bool { return strings.HasPrefix(kv, "PATH=") || strings.HasPrefix(kv, "MAKE=") })
	cmd.Env = append(env, "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func() { os.RemoveAll(dir) }
}

var diagStart = regexp.MustCompile(`^(\S.*:\d+:\d+: |In file included from |\S.*: In function )`)

// dedupeDiagnostics joins the units' compiler output, dropping a diagnostic
// already shown by an earlier unit.
func dedupeDiagnostics(diags [][]byte) []byte {
	seen := map[string]bool{}
	var out bytes.Buffer
	for _, d := range diags {
		var block []string
		flush := func() {
			if len(block) == 0 {
				return
			}
			// The unit file name differs between copies of one diagnostic.
			k := unitName.ReplaceAllString(strings.Join(block, "\n"), "")
			if !seen[k] {
				seen[k] = true
				for _, l := range block {
					out.WriteString(l + "\n")
				}
			}
			block = nil
		}
		for _, line := range strings.Split(strings.TrimRight(string(d), "\n"), "\n") {
			if line == "" {
				continue
			}
			if diagStart.MatchString(line) && !strings.Contains(line, ": note: ") {
				flush()
			}
			block = append(block, line)
		}
		flush()
	}
	return out.Bytes()
}

var unitName = regexp.MustCompile(`u\d+\.c(:\d+)*|(?m)^\s*\d+ \|`)

func firstLines(b []byte, n int) string {
	lines := strings.SplitN(string(b), "\n", n+1)
	if len(lines) > n {
		lines = append(lines[:n], "...")
	}
	return strings.Join(lines, "\n")
}
