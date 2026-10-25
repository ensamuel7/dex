package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/checker"
	"github.com/ensamuel7/dex/codegen"
	"github.com/ensamuel7/dex/docgen"
	"github.com/ensamuel7/dex/lexer"
	"github.com/ensamuel7/dex/lsp"
	"github.com/ensamuel7/dex/parser"
	"github.com/ensamuel7/dex/resolve"
	"github.com/ensamuel7/dex/stdlib"
	"github.com/fsnotify/fsnotify"
	"golang.org/x/term"
)

// formatError applies ANSI color formatting to compiler error messages on TTY.
// Pattern: "filename:line:col: message" → bold location, red "error:", normal message
var errorLocPattern = regexp.MustCompile(`^(.+?:\d+:\d+:)\s*(.*)$`)

// Version is set at build time via -ldflags "-X main.Version=..."
var Version = "dev"

// Commit is the source commit, set by `make build` via -ldflags. A -dev stamp
// alone cannot tell two builds of the same version apart.
var Commit = ""

func formatError(msg string) string {
	if !term.IsTerminal(int(os.Stderr.Fd())) {
		return msg
	}
	if m := errorLocPattern.FindStringSubmatch(msg); m != nil {
		loc := m[1]
		rest := m[2]
		return fmt.Sprintf("\033[1m%s\033[0m \033[31merror:\033[0m %s", loc, rest)
	}
	return msg
}

func main() {
	// The resolver refuses a module whose dex.mod needs a newer compiler, so it
	// has to know which one this is. It cannot import main to ask.
	resolve.SetCompilerVersion(Version)

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(1)
	}

	command := os.Args[1]

	// Flags are pulled out before anything else so every command sees the same
	// set and a positional filename stays at args[0].
	args, err := parseFlags(os.Args[2:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if lintAsked && noLint {
		fmt.Fprintln(os.Stderr, "--no-lint cannot be combined with --lint or --lint=strict")
		os.Exit(1)
	}
	lintEnabled = lintOnByDefault(command)

	if command == "version" {
		if Commit != "" {
			fmt.Printf("dex version %s (%s)\n", Version, Commit)
		} else {
			fmt.Println("dex version " + Version)
		}
		return
	}

	if command == "lsp" {
		lsp.Run()
		return
	}

	if command == "docs" {
		// Find project root (directory containing go.mod)
		dir, _ := os.Getwd()
		htmlContent, err := docgen.Generate(dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		outDir := filepath.Join(dir, "docs")
		os.MkdirAll(outDir, 0755)
		outPath := filepath.Join(outDir, "index.html")
		if err := os.WriteFile(outPath, []byte(htmlContent), 0644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("Generated: %s\n", outPath)
		return
	}

	if command == "test" {
		runTests(args)
		return
	}

	if command == "format" {
		runFormat(args)
		return
	}

	if command == "update" {
		selfUpdate(forceUpdate)
		return
	}

	if command == "mod" {
		runMod(args)
		return
	}

	if command == "get" {
		runGet(args)
		return
	}

	if command == "lint" && listRules {
		printRules()
		return
	}

	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(1)
	}

	filename := args[0]

	switch command {
	case "build":
		binaryPath, err := build(filename)
		if err != nil {
			fmt.Fprintln(os.Stderr, formatError(err.Error()))
			os.Exit(1)
		}
		fmt.Printf("Built: %s\n", binaryPath)

	case "run":
		binaryPath, err := build(filename)
		if err != nil {
			fmt.Fprintln(os.Stderr, formatError(err.Error()))
			os.Exit(1)
		}
		defer os.Remove(binaryPath)

		cmd := exec.Command(binaryPath)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				os.Exit(exitErr.ExitCode())
			}
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

	case "dev":
		dev(filename)

	case "profile":
		runProfile(filename)

	case "check":
		check(filename)

	case "lint":
		lintOnly(filename)

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(1)
	}
}

func runTests(args []string) {
	var files []string

	roots := args
	if len(roots) == 0 {
		roots = []string{"."}
	}
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if !info.IsDir() {
			files = append(files, root)
			continue
		}
		err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() && strings.HasSuffix(path, "_test.dx") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "No *_test.dx files found")
		os.Exit(1)
	}

	passed := 0
	failed := 0

	for _, file := range files {
		binaryPath, err := build(file)
		if err != nil {
			fmt.Printf("FAIL: %s\n  %s\n", file, formatError(err.Error()))
			failed++
			continue
		}

		cmd := exec.Command(binaryPath)
		cmd.Env = append(os.Environ(), sanitizerEnv()...)
		output, err := cmd.CombinedOutput()
		os.Remove(binaryPath)

		if err != nil {
			fmt.Printf("FAIL: %s\n", file)
			if len(output) > 0 {
				// Indent output lines
				for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
					fmt.Printf("  %s\n", line)
				}
			}
			failed++
		} else {
			fmt.Printf("PASS: %s\n", file)
			passed++
		}
	}

	fmt.Printf("\n%d passed, %d failed\n", passed, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

func dev(filename string) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[dev] failed to create watcher: %v\n", err)
		os.Exit(1)
	}
	defer watcher.Close()

	// Watch all directories under the entry file's project dir
	projectDir := filepath.Dir(filename)
	if projectDir == "" || projectDir == "." {
		projectDir, _ = os.Getwd()
	} else {
		projectDir, _ = filepath.Abs(projectDir)
	}

	filepath.Walk(projectDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			// Skip hidden dirs, build dir, node_modules
			base := filepath.Base(path)
			if strings.HasPrefix(base, ".") || base == "build" || base == "node_modules" {
				return filepath.SkipDir
			}
			watcher.Add(path)
		}
		return nil
	})

	var cmd *exec.Cmd
	var binaryPath string

	killProcess := func() {
		if cmd != nil && cmd.Process != nil {
			cmd.Process.Signal(syscall.SIGTERM)
			done := make(chan struct{})
			go func() {
				cmd.Wait()
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				cmd.Process.Kill()
				<-done
			}
			cmd = nil
		}
	}

	cleanup := func() {
		killProcess()
		if binaryPath != "" {
			os.Remove(binaryPath)
		}
	}

	buildAndRun := func() {
		killProcess()
		if binaryPath != "" {
			os.Remove(binaryPath)
			binaryPath = ""
		}

		fmt.Println("[dev] building...")
		bp, err := build(filename)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[dev] build error: %s\n", formatError(err.Error()))
			fmt.Println("[dev] waiting for changes...")
			return
		}
		binaryPath = bp
		fmt.Printf("[dev] running %s\n", binaryPath)

		cmd = exec.Command(binaryPath)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "[dev] run error: %v\n", err)
			fmt.Println("[dev] waiting for changes...")
			return
		}

		// Monitor process exit in background
		go func(c *exec.Cmd) {
			err := c.Wait()
			if c == cmd {
				if err != nil {
					fmt.Fprintf(os.Stderr, "[dev] process exited: %v\n", err)
				} else {
					fmt.Println("[dev] process exited")
				}
			}
		}(cmd)
	}

	// Handle Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n[dev] shutting down...")
		cleanup()
		os.Exit(0)
	}()

	// Initial build and run
	buildAndRun()

	// Watch for changes with debounce
	debounce := time.NewTimer(0)
	if !debounce.Stop() {
		<-debounce.C
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) {
				if strings.HasSuffix(event.Name, ".dx") {
					debounce.Reset(200 * time.Millisecond)
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			fmt.Fprintf(os.Stderr, "[dev] watcher error: %v\n", err)
		case <-debounce.C:
			fmt.Println("[dev] change detected, rebuilding...")
			buildAndRun()
		}
	}
}

// frontend runs the pipeline up to and including type checking: lex, parse,
// flatten methods, merge user modules, check, then lint when asked. Building,
// `dex check` and `dex lint` all need exactly this much and nothing more, and
// keeping separate copies of it in step has not worked.
func frontend(filename string) (*ast.Program, error) {
	// Reset type registries to prevent state leaking between compilations
	ast.ResetStructTypes()
	ast.ResetDbFillKeys()
	ast.ResetChanTypes()
	ast.ResetTaskTypes()
	ast.ResetWeakTypes()
	ast.ResetStructArrayTypes()
	ast.ResetOptionalTypes()
	ast.ResetRefTypes()
	ast.ResetFuncTypes()
	ast.ResetMapTypes()
	ast.ResetEnumTypes()
	ast.ResetHandleTypes()

	// Register module-provided struct types (e.g. HttpResponse)
	stdlib.RegisterAllModuleTypes()

	// Register built-in Exception type for error handling
	ast.RegisterExceptionType()

	source, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", filename, err)
	}

	// Lex
	lex := lexer.NewWithFile(string(source), filename)
	tokens, err := lex.Tokenize()
	if err != nil {
		return nil, err
	}

	// Seed parser with module-provided struct type names from imported modules
	importPaths := resolve.ExtractImportPaths(tokens)
	typeNames := stdlib.ModuleTypesForImports(importPaths)

	// Pre-scan user module files for struct/enum names so the main parser
	// recognizes struct literal syntax (e.g. User { field: value }).
	sourceDir := filepath.Dir(filename)
	if sourceDir == "" {
		sourceDir = "."
	}
	absSourceDir, _ := filepath.Abs(sourceDir)
	if err := resolve.CheckModuleImports(resolve.ExtractImports(tokens), absSourceDir, filename); err != nil {
		return nil, err
	}
	moduleTypes := resolve.PreRegisterUserStructs(resolve.ExtractImports(tokens), absSourceDir)
	aliases, err := resolve.ModuleAliases(resolve.ExtractImports(tokens), absSourceDir, filename)
	if err != nil {
		return nil, err
	}

	// Parse
	p := parser.New(tokens)
	p.SetModuleAliases(aliases)
	for _, name := range typeNames {
		p.AddStructName(name)
	}
	for _, mt := range moduleTypes {
		p.AddModuleType(mt.Module, mt.Name, mt.Kind)
	}
	p.AddStructName("Exception") // built-in Exception type
	program, parseErrs := p.Parse()
	if len(parseErrs) > 0 {
		return nil, parseErrs[0]
	}

	// Flatten struct methods in the main program (before resolving modules)
	resolve.FlattenStructMethods(program)

	// Resolve user module imports (non-stdlib .dx files)
	if err := resolve.ResolveUserModules(program, absSourceDir); err != nil {
		return nil, err
	}

	// Type check. The parser is handed over as the instantiator for generics:
	// inferring a generic call's type arguments needs the argument types, which
	// only the checker has, while re-parsing the template with them bound is
	// something only the parser can do.
	ch := checker.New()
	ch.SetGenerics(program.GenericFuncs, resolve.Instantiator{Parser: p, Aliases: program.ModuleAliases})
	checkErrs := ch.Check(program)
	printWarnings(ch.Warnings())
	if len(checkErrs) > 0 {
		return nil, checkErrs[0]
	}

	// Lint runs on the checked AST: a method call only knows which struct it
	// belongs to once the checker has resolved it.
	if err := reportLint(program); err != nil {
		return nil, err
	}

	return program, nil
}

func build(filename string) (string, error) {
	program, err := frontend(filename)
	if err != nil {
		return "", err
	}
	resolve.RenameCCollisions(program)

	// Generate C
	gen := codegen.New()
	memCheck, err := configureGen(gen)
	if err != nil {
		return "", err
	}
	cCode := gen.Generate(program)

	// Ensure build output directory exists
	buildDir := "build"
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create build directory: %v", err)
	}

	// Write temp .c file into build/
	baseName := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	cFile := filepath.Join(buildDir, baseName+".c")
	if err := os.WriteFile(cFile, []byte(cCode), 0644); err != nil {
		return "", fmt.Errorf("failed to write C file: %v", err)
	}
	// DEX_KEEP_C leaves the generated C in build/ so compiler changes can be
	// inspected without reproducing the failure by hand.
	if os.Getenv("DEX_KEEP_C") == "" {
		defer os.Remove(cFile)
	}

	// Compile with gcc/cc.
	//
	// GNU ld resolves a library against what came before it on the command line,
	// so -l flags have to sit after the source file or every symbol they provide
	// reads as undefined. macOS's linker does not care; Linux does.
	binaryPath := filepath.Join(buildDir, baseName)
	compiler := "cc"
	var cflags, libflags []string
	for _, f := range gen.CompilerFlags() {
		if strings.HasPrefix(f, "-l") || strings.HasPrefix(f, "-L") || strings.HasPrefix(f, "-Wl,") {
			libflags = append(libflags, f)
		} else {
			cflags = append(cflags, f)
		}
	}
	libflags = keepLastOccurrence(libflags)
	if names, _ := sanitizers(); len(names) > 0 {
		cflags = slices.DeleteFunc(cflags, func(f string) bool { return strings.HasPrefix(f, "-D_FORTIFY_SOURCE") })
	}
	cflags = append(cflags, hardeningFlags(compiler)...)
	cflags = append(cflags, strings.Fields(os.Getenv("DEX_CFLAGS"))...)
	// A stdlib function whose C signature disagrees with how it is registered —
	// a DexString* declared where the generated call passes char*, because
	// RawParams was left off — produced nothing worse than a warning, compiled,
	// and then corrupted the heap when the callee read a length out of a raw
	// buffer. That has cost real debugging time, so the mismatch is fatal here
	// rather than advisory.
	// The generated C sits in build/, so a header named by an external module
	// has to be looked for beside the source rather than beside the output.
	// `include "counter.hpp"` is how you bind a shim you wrote yourself, and
	// that header lives with your code.
	srcDir := filepath.Dir(filename)
	if srcDir == "" {
		srcDir = "."
	}
	if abs, err := filepath.Abs(srcDir); err == nil {
		srcDir = abs
	}
	args := append([]string{"-o", binaryPath,
		"-I" + srcDir,
		"-Werror=incompatible-pointer-types",
		"-Werror=implicit-function-declaration",
	}, cflags...)
	sanFlags, err := sanitizerFlags()
	if err != nil {
		return "", err
	}
	args = append(args, sanFlags...)
	// The checker aborts on a bad release, so the build carries symbols for the
	// resulting backtrace.
	if memCheck {
		args = append(args, "-g", "-fno-omit-frame-pointer")
	}
	args = append(args, cFile)
	args = append(args, libflags...)
	cmd := exec.Command(compiler, args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("C compilation failed: %v", err)
	}

	return binaryPath, nil
}

var (
	hardeningOnce   sync.Once
	hardeningCached []string
)

func hardeningFlags(compiler string) []string {
	hardeningOnce.Do(func() {
		dir, err := os.MkdirTemp("", "dex-probe")
		if err != nil {
			return
		}
		defer os.RemoveAll(dir)
		src := filepath.Join(dir, "p.c")
		if os.WriteFile(src, []byte("int main(void) { return 0; }\n"), 0644) != nil {
			return
		}
		supports := func(flags ...string) bool {
			args := append([]string{"-Werror", "-O2"}, flags...)
			args = append(args, src, "-o", filepath.Join(dir, "p"))
			return exec.Command(compiler, args...).Run() == nil
		}
		names, _ := sanitizers()
		if len(names) == 0 && supports("-U_FORTIFY_SOURCE", "-D_FORTIFY_SOURCE=3") {
			hardeningCached = append(hardeningCached, "-U_FORTIFY_SOURCE", "-D_FORTIFY_SOURCE=3")
		}
		candidates := []string{"-fstack-clash-protection", "-mbranch-protection=standard", "-fcf-protection=full"}
		if !slices.Contains(names, "memory") {
			candidates = append(candidates, "-ftrivial-auto-var-init=zero")
		}
		for _, f := range candidates {
			if supports(f) {
				hardeningCached = append(hardeningCached, f)
			}
		}
	})
	return hardeningCached
}

func sanitizers() ([]string, error) {
	v := os.Getenv("DEX_SANITIZE")
	if v == "" || v == "0" {
		return nil, nil
	}
	var out []string
	for _, name := range strings.Split(v, ",") {
		switch strings.TrimSpace(name) {
		case "1", "address", "asan":
			out = append(out, "address")
		case "undefined", "ub", "ubsan":
			out = append(out, "undefined")
		case "thread", "tsan":
			out = append(out, "thread")
		case "memory", "msan":
			out = append(out, "memory")
		case "leak", "lsan":
			out = append(out, "leak")
		default:
			return nil, fmt.Errorf("DEX_SANITIZE: unknown sanitizer %q (address, undefined, thread, memory, leak)", name)
		}
	}
	has := func(n string) bool { return slices.Contains(out, n) }
	if has("thread") && (has("address") || has("memory") || has("leak")) {
		return nil, fmt.Errorf("DEX_SANITIZE: thread cannot be combined with address, memory or leak")
	}
	if has("memory") && (has("address") || has("leak")) {
		return nil, fmt.Errorf("DEX_SANITIZE: memory cannot be combined with address or leak")
	}
	return out, nil
}

func sanitizerFlags() ([]string, error) {
	names, err := sanitizers()
	if err != nil || len(names) == 0 {
		return nil, err
	}
	flags := []string{"-fsanitize=" + strings.Join(names, ","), "-fno-omit-frame-pointer", "-g"}
	if slices.Contains(names, "undefined") {
		flags = append(flags, "-fno-sanitize-recover=all")
	}
	if slices.Contains(names, "memory") {
		flags = append(flags, "-fsanitize-memory-track-origins")
	}
	return flags, nil
}

func sanitizerEnv() []string {
	names, _ := sanitizers()
	var env []string
	for _, n := range names {
		switch n {
		case "address":
			if runtime.GOOS == "linux" {
				env = append(env, "ASAN_OPTIONS=detect_leaks=1:abort_on_error=0")
			}
		case "undefined":
			env = append(env, "UBSAN_OPTIONS=print_stacktrace=1:halt_on_error=1")
		case "thread":
			env = append(env, "TSAN_OPTIONS=halt_on_error=1:second_deadlock_stack=1")
		}
	}
	return env
}

// configureGen turns on the instrumented build modes the environment asks for,
// and reports whether the memory checker is one of them.
//
// DEX_MEM_CHECK=1 builds in the debug memory checker: every retain and release
// is validated against the object's header, a freed object is kept in
// quarantine so a second release panics instead of corrupting the allocator,
// and anything still alive at exit is reported against the .dx statement that
// allocated it.
//
// DEX_PROFILE=1 (or `dex profile`) builds in the memory profiler instead. The
// two cannot share a build: the checker's quarantine holds freed memory back,
// which is exactly what would make the profiler's numbers wrong.
func configureGen(gen *codegen.Generator) (bool, error) {
	memCheck := os.Getenv("DEX_MEM_CHECK") == "1"
	profile := profileBuild || os.Getenv("DEX_PROFILE") == "1"
	if memCheck && profile {
		return false, fmt.Errorf("DEX_MEM_CHECK and the profiler cannot be combined: the checker's quarantine would count as memory the program holds")
	}
	if memCheck {
		gen.EnableMemCheck()
	}
	if profile {
		gen.EnableProfile()
	}
	return memCheck, nil
}

// generateC runs the compilation pipeline up to C code generation (no cc invocation).
// Returns the path to the generated .c file.
func generateC(filename string) (string, error) {
	program, err := frontend(filename)
	if err != nil {
		return "", err
	}
	resolve.RenameCCollisions(program)
	gen := codegen.New()
	if _, err := configureGen(gen); err != nil {
		return "", err
	}
	cCode := gen.Generate(program)

	buildDir := "build"
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create build directory: %v", err)
	}

	baseName := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	cFile := filepath.Join(buildDir, baseName+".c")
	if err := os.WriteFile(cFile, []byte(cCode), 0644); err != nil {
		return "", fmt.Errorf("failed to write C file: %v", err)
	}

	return cFile, nil
}

func selfUpdate(force bool) {
	// 1. Query GitHub API for latest release
	resp, err := http.Get("https://api.github.com/repos/ensamuel7/dex/releases/latest")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to check for updates: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "Failed to check for updates: HTTP %d\n", resp.StatusCode)
		os.Exit(1)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse release info: %v\n", err)
		os.Exit(1)
	}

	latest := strings.TrimPrefix(release.TagName, "v")

	// 2. Compare versions (skip if already up-to-date, unless forced or dev build)
	if !force && Version != "dev" && Version == latest {
		fmt.Printf("dex is already up to date (v%s)\n", Version)
		return
	}

	if Version == "dev" {
		fmt.Println("Running dev build, downloading latest release...")
	} else if !force {
		fmt.Printf("Updating dex: v%s -> v%s\n", Version, latest)
	} else {
		fmt.Printf("Force updating dex to v%s\n", latest)
	}

	// 3. Build tarball name for current OS/arch
	tarballName := fmt.Sprintf("dex-%s-%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	downloadURL := fmt.Sprintf("https://github.com/ensamuel7/dex/releases/latest/download/%s", tarballName)

	// 4. Download tarball
	dlResp, err := http.Get(downloadURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to download update: %v\n", err)
		os.Exit(1)
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "Failed to download update: HTTP %d\n", dlResp.StatusCode)
		fmt.Fprintf(os.Stderr, "No release found for %s/%s\n", runtime.GOOS, runtime.GOARCH)
		os.Exit(1)
	}

	// 5. Extract binary from tar.gz
	gz, err := gzip.NewReader(dlResp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to decompress archive: %v\n", err)
		os.Exit(1)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	var binaryData []byte
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read archive: %v\n", err)
			os.Exit(1)
		}
		if filepath.Base(hdr.Name) == "dex" && !hdr.FileInfo().IsDir() {
			binaryData, err = io.ReadAll(tr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to extract binary: %v\n", err)
				os.Exit(1)
			}
			break
		}
	}

	if binaryData == nil {
		fmt.Fprintln(os.Stderr, "Could not find 'dex' binary in archive")
		os.Exit(1)
	}

	// Write extracted binary to a temp file
	tmpFile, err := os.CreateTemp("", "dex-update-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create temp file: %v\n", err)
		os.Exit(1)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(binaryData); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		fmt.Fprintf(os.Stderr, "Failed to write temp binary: %v\n", err)
		os.Exit(1)
	}
	if err := tmpFile.Chmod(0755); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		fmt.Fprintf(os.Stderr, "Failed to set permissions: %v\n", err)
		os.Exit(1)
	}
	tmpFile.Close()

	// 6. Get current executable path
	execPath, err := os.Executable()
	if err != nil {
		os.Remove(tmpPath)
		fmt.Fprintf(os.Stderr, "Failed to determine executable path: %v\n", err)
		os.Exit(1)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		os.Remove(tmpPath)
		fmt.Fprintf(os.Stderr, "Failed to resolve executable path: %v\n", err)
		os.Exit(1)
	}

	// 7. Replace binary - try direct rename, fall back to sudo mv
	if err := os.Rename(tmpPath, execPath); err != nil {
		// Rename failed (likely permission denied), try sudo mv
		cmd := exec.Command("sudo", "mv", tmpPath, execPath)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			os.Remove(tmpPath)
			fmt.Fprintf(os.Stderr, "Failed to install update: %v\n", err)
			os.Exit(1)
		}
	}

	// 8. Print success
	if Version == "dev" {
		fmt.Printf("Updated dex: dev -> v%s\n", latest)
	} else {
		fmt.Printf("Updated dex: v%s -> v%s\n", Version, latest)
	}
}

func check(filename string) {
	// Verify cppcheck is installed
	if _, err := exec.LookPath("cppcheck"); err != nil {
		fmt.Fprintln(os.Stderr, "cppcheck not found. Install it to use 'dex check':")
		fmt.Fprintln(os.Stderr, "  macOS:  brew install cppcheck")
		fmt.Fprintln(os.Stderr, "  Linux:  sudo apt install cppcheck")
		os.Exit(1)
	}

	// Generate C code from .dx source
	cFile, err := generateC(filename)
	if err != nil {
		fmt.Fprintln(os.Stderr, formatError(err.Error()))
		os.Exit(1)
	}
	defer os.Remove(cFile)

	fmt.Printf("Checking %s -> %s\n", filename, cFile)

	// Run cppcheck with warning, performance and portability checks
	cmd := exec.Command("cppcheck",
		"--enable=warning,performance,portability",
		"--std=c11",
		"--suppress=unusedFunction",
		"--suppress=missingIncludeSystem",
		"--error-exitcode=0",
		cFile,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "cppcheck failed: %v\n", err)
		os.Exit(1)
	}
}

// printWarnings reports non-fatal checker diagnostics. They never stop a build —
// deprecated calls keep working — but they say what to move to.
func printWarnings(warnings []error) {
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "warning: %v\n", w)
	}
}

func keepLastOccurrence(flags []string) []string {
	last := make(map[string]int, len(flags))
	for i, f := range flags {
		last[f] = i
	}
	out := flags[:0:0]
	for i, f := range flags {
		if last[f] == i {
			out = append(out, f)
		}
	}
	return out
}

func runMod(args []string) {
	if len(args) == 2 && args[0] == "init" {
		dir, err := os.Getwd()
		exitOn(err)
		file, err := resolve.InitMod(dir, args[1])
		exitOn(err)
		fmt.Printf("Created %s\n", file)
		return
	}
	if len(args) == 1 && (args[0] == "download" || args[0] == "verify") {
		mod := currentMod()
		fetched, err := resolve.Download(mod)
		exitOn(err)
		for _, f := range fetched {
			fmt.Printf("%s %s\n", f.Path, f.Version)
		}
		if args[0] == "verify" {
			fmt.Println("all modules verified")
		}
		return
	}
	fmt.Fprintln(os.Stderr, `usage:
  dex mod init <module path>   start a dex.mod naming this module, as in github.com/acme/app
  dex mod download             fetch every module dex.mod requires into the module cache
  dex mod verify               check the cached modules against dex.sum`)
	os.Exit(1)
}

func runGet(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: dex get <module>[@version]\n\nAdds the module to dex.mod and downloads it, as in: dex get github.com/acme/greet@v1.2.0")
		os.Exit(1)
	}
	mod := currentMod()
	for _, arg := range args {
		fetched, err := resolve.Get(mod, arg)
		exitOn(err)
		for _, f := range fetched {
			note := ""
			if f.Indirect {
				note = " (needed by another module)"
			}
			fmt.Printf("added %s %s%s\n", f.Path, f.Version, note)
			if f.NeedsDex != "" {
				fmt.Fprintf(os.Stderr, "warning: %s %s needs dex %s or later; this is dex %s — upgrade with `dex update` before building with it\n",
					f.Path, f.Version, f.NeedsDex, Version)
			}
		}
	}
}

func currentMod() *resolve.Mod {
	dir, err := os.Getwd()
	exitOn(err)
	mod, err := resolve.FindMod(dir)
	exitOn(err)
	if mod == nil {
		fmt.Fprintln(os.Stderr, "no dex.mod here or above — create one with: dex mod init <module path>")
		os.Exit(1)
	}
	return mod
}

func exitOn(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
