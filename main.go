package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
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
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: dex <build|run|dev|test|lsp|docs> <file.dx>")
		os.Exit(1)
	}

	command := os.Args[1]

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
		runTests()
		return
	}

	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: dex <build|run|dev> <file.dx>")
		os.Exit(1)
	}

	filename := os.Args[2]

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

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		fmt.Fprintln(os.Stderr, "Usage: dex <build|run|dev|test|lsp|docs> <file.dx>")
		os.Exit(1)
	}
}

func runTests() {
	var files []string

	if len(os.Args) >= 3 {
		// Run specific test file
		files = append(files, os.Args[2])
	} else {
		// Discover *_test.dx files recursively
		err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
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
		if len(files) == 0 {
			fmt.Fprintln(os.Stderr, "No *_test.dx files found")
			os.Exit(1)
		}
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

func build(filename string) (string, error) {
	// Reset type registries to prevent state leaking between compilations
	ast.ResetStructTypes()
	ast.ResetChanTypes()
	ast.ResetTaskTypes()
	ast.ResetWeakTypes()
	ast.ResetStructArrayTypes()
	ast.ResetOptionalTypes()
	ast.ResetRefTypes()

	// Register module-provided struct types (e.g. HttpResponse)
	stdlib.RegisterAllModuleTypes()

	// Register built-in Exception type for error handling
	ast.RegisterExceptionType()

	source, err := os.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("%s: %v", filename, err)
	}

	// Lex
	lex := lexer.New(string(source))
	tokens, err := lex.Tokenize()
	if err != nil {
		return "", fmt.Errorf("%s:%v", filename, err)
	}

	// Seed parser with module-provided struct type names from imported modules
	importPaths := resolve.ExtractImportPaths(tokens)
	typeNames := stdlib.ModuleTypesForImports(importPaths)

	// Parse
	p := parser.New(tokens)
	for _, name := range typeNames {
		p.AddStructName(name)
	}
	p.AddStructName("Exception") // built-in Exception type
	program, err := p.Parse()
	if err != nil {
		return "", fmt.Errorf("%s:%v", filename, err)
	}

	// Flatten struct methods in the main program (before resolving modules)
	resolve.FlattenStructMethods(program)

	// Resolve user module imports (non-stdlib .dx files)
	sourceDir := filepath.Dir(filename)
	if sourceDir == "" {
		sourceDir = "."
	}
	absSourceDir, _ := filepath.Abs(sourceDir)
	if err := resolve.ResolveUserModules(program, absSourceDir); err != nil {
		return "", fmt.Errorf("%s: %v", filename, err)
	}

	// Type check
	ch := checker.New()
	if err := ch.Check(program); err != nil {
		return "", fmt.Errorf("%s: %v", filename, err)
	}

	// Generate C
	gen := codegen.New()
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
	defer os.Remove(cFile)

	// Compile with gcc/cc
	binaryPath := filepath.Join(buildDir, baseName)
	compiler := "cc"
	args := append([]string{"-o", binaryPath}, gen.CompilerFlags()...)
	args = append(args, cFile)
	cmd := exec.Command(compiler, args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("C compilation failed: %v", err)
	}

	return binaryPath, nil
}

