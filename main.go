package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
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
	"github.com/ensamuel7/dex/stdlib"
	"github.com/ensamuel7/dex/token"
	"github.com/fsnotify/fsnotify"
)

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
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("Built: %s\n", binaryPath)

	case "run":
		binaryPath, err := build(filename)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
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
			fmt.Printf("FAIL: %s\n  %v\n", file, err)
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
			fmt.Fprintf(os.Stderr, "[dev] build error: %v\n", err)
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

	// Register module-provided struct types (e.g. HttpResponse)
	stdlib.RegisterAllModuleTypes()

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
	importPaths := extractImportPaths(tokens)
	typeNames := stdlib.ModuleTypesForImports(importPaths)

	// Parse
	p := parser.New(tokens)
	for _, name := range typeNames {
		p.AddStructName(name)
	}
	program, err := p.Parse()
	if err != nil {
		return "", fmt.Errorf("%s:%v", filename, err)
	}

	// Resolve user module imports (non-stdlib .dx files)
	sourceDir := filepath.Dir(filename)
	if sourceDir == "" {
		sourceDir = "."
	}
	absSourceDir, _ := filepath.Abs(sourceDir)
	if err := resolveUserModules(program, absSourceDir); err != nil {
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

// extractImportPaths scans tokens for import declarations and returns their paths.
func extractImportPaths(tokens []token.Token) []string {
	var paths []string
	for i := 0; i < len(tokens)-1; i++ {
		if tokens[i].Kind == token.TokenImport && tokens[i+1].Kind == token.TokenString {
			paths = append(paths, tokens[i+1].Value)
		}
	}
	return paths
}

// resolveUserModules finds non-stdlib imports, parses their .dx files,
// prefixes their functions, and merges them into the main program.
func resolveUserModules(program *ast.Program, sourceDir string) error {
	visited := map[string]bool{}    // fully resolved paths already merged
	processing := map[string]bool{} // currently being processed (circular detection)
	return resolveImports(program, sourceDir, visited, processing)
}

func resolveImports(program *ast.Program, sourceDir string, visited, processing map[string]bool) error {
	var userImports []ast.Import
	var stdlibImports []ast.Import

	for _, imp := range program.Imports {
		if stdlib.Lookup(imp.Path) != nil {
			stdlibImports = append(stdlibImports, imp)
		} else {
			userImports = append(userImports, imp)
		}
	}

	// Replace imports with only stdlib imports (user imports are resolved)
	program.Imports = stdlibImports

	for _, imp := range userImports {
		moduleName := filepath.Base(imp.Path)
		filePath := filepath.Join(sourceDir, imp.Path+".dx")
		absPath, err := filepath.Abs(filePath)
		if err != nil {
			return fmt.Errorf("cannot resolve path for import '%s': %v", imp.Path, err)
		}

		if visited[absPath] {
			// Already merged — just record the module name
			if !containsString(program.UserModules, moduleName) {
				program.UserModules = append(program.UserModules, moduleName)
			}
			continue
		}

		if processing[absPath] {
			return fmt.Errorf("circular import detected: '%s'", imp.Path)
		}

		source, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("cannot open user module '%s': %v", imp.Path, err)
		}

		// Lex
		lex := lexer.New(string(source))
		tokens, err := lex.Tokenize()
		if err != nil {
			return fmt.Errorf("%s: %v", filePath, err)
		}

		// Seed parser with module-provided struct type names
		modImportPaths := extractImportPaths(tokens)
		typeNames := stdlib.ModuleTypesForImports(modImportPaths)

		// Parse
		p := parser.New(tokens)
		for _, name := range typeNames {
			p.AddStructName(name)
		}
		modProgram, err := p.Parse()
		if err != nil {
			return fmt.Errorf("%s: %v", filePath, err)
		}

		// Mark as processing for circular detection, then resolve sub-imports
		processing[absPath] = true
		modDir := filepath.Dir(absPath)
		if err := resolveImports(modProgram, modDir, visited, processing); err != nil {
			return err
		}
		delete(processing, absPath)
		visited[absPath] = true

		// Collect module's own function names (before prefixing)
		modFuncNames := map[string]bool{}
		for _, fn := range modProgram.Functions {
			modFuncNames[fn.Name] = true
		}

		// Prefix functions and rewrite internal calls
		for i := range modProgram.Functions {
			fn := &modProgram.Functions[i]
			if fn.Name == "main" {
				continue // skip main from user modules
			}
			// Prefix function name
			fn.Name = moduleName + "_" + fn.Name
			// Rewrite internal unqualified calls in the body
			for _, stmt := range fn.Body {
				prefixCallsInStmt(stmt, moduleName, modFuncNames)
			}
		}

		// Merge into main program
		for _, fn := range modProgram.Functions {
			if fn.Name == "main" {
				continue
			}
			program.Functions = append(program.Functions, fn)
		}
		program.Structs = append(program.Structs, modProgram.Structs...)

		// Deduplicate stdlib imports from module
		for _, modImp := range modProgram.Imports {
			if stdlib.Lookup(modImp.Path) != nil && !containsImport(program.Imports, modImp.Path) {
				program.Imports = append(program.Imports, modImp)
			}
		}

		// Merge user modules from sub-modules
		for _, subMod := range modProgram.UserModules {
			if !containsString(program.UserModules, subMod) {
				program.UserModules = append(program.UserModules, subMod)
			}
		}

		if !containsString(program.UserModules, moduleName) {
			program.UserModules = append(program.UserModules, moduleName)
		}
	}

	return nil
}

func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func containsImport(imports []ast.Import, path string) bool {
	for _, imp := range imports {
		if imp.Path == path {
			return true
		}
	}
	return false
}

// prefixCallsInStmt walks a statement tree and rewrites unqualified calls
// to module-internal functions with the module prefix.
func prefixCallsInStmt(stmt ast.Stmt, moduleName string, modFuncNames map[string]bool) {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		prefixCallsInExpr(s.Expr, moduleName, modFuncNames)
	case *ast.LetStmt:
		prefixCallsInExpr(s.Value, moduleName, modFuncNames)
	case *ast.AssignStmt:
		prefixCallsInExpr(s.Value, moduleName, modFuncNames)
	case *ast.ReturnStmt:
		if s.Value != nil {
			prefixCallsInExpr(s.Value, moduleName, modFuncNames)
		}
	case *ast.IfStmt:
		prefixCallsInExpr(s.Cond, moduleName, modFuncNames)
		for _, st := range s.Then {
			prefixCallsInStmt(st, moduleName, modFuncNames)
		}
		for _, st := range s.Else {
			prefixCallsInStmt(st, moduleName, modFuncNames)
		}
	case *ast.WhileStmt:
		prefixCallsInExpr(s.Cond, moduleName, modFuncNames)
		for _, st := range s.Body {
			prefixCallsInStmt(st, moduleName, modFuncNames)
		}
	case *ast.ForStmt:
		if s.Init != nil {
			prefixCallsInStmt(s.Init, moduleName, modFuncNames)
		}
		if s.Cond != nil {
			prefixCallsInExpr(s.Cond, moduleName, modFuncNames)
		}
		if s.Post != nil {
			prefixCallsInStmt(s.Post, moduleName, modFuncNames)
		}
		for _, st := range s.Body {
			prefixCallsInStmt(st, moduleName, modFuncNames)
		}
	case *ast.ForeachStmt:
		prefixCallsInExpr(s.Iterable, moduleName, modFuncNames)
		for _, st := range s.Body {
			prefixCallsInStmt(st, moduleName, modFuncNames)
		}
	case *ast.BlockStmt:
		for _, st := range s.Stmts {
			prefixCallsInStmt(st, moduleName, modFuncNames)
		}
	case *ast.IndexAssignStmt:
		prefixCallsInExpr(s.Array, moduleName, modFuncNames)
		prefixCallsInExpr(s.Index, moduleName, modFuncNames)
		prefixCallsInExpr(s.Value, moduleName, modFuncNames)
	case *ast.FieldAssignStmt:
		prefixCallsInExpr(s.Object, moduleName, modFuncNames)
		prefixCallsInExpr(s.Value, moduleName, modFuncNames)
	case *ast.CompoundAssignStmt:
		prefixCallsInExpr(s.Value, moduleName, modFuncNames)
	case *ast.SendStmt:
		if s.Target != nil {
			prefixCallsInExpr(s.Target, moduleName, modFuncNames)
		}
		prefixCallsInExpr(s.Value, moduleName, modFuncNames)
	// BreakStmt, ContinueStmt, IncrementStmt, DecrementStmt — no expressions to rewrite
	}
}

// prefixCallsInExpr walks an expression tree and rewrites unqualified calls
// to module-internal functions with the module prefix.
func prefixCallsInExpr(expr ast.Expr, moduleName string, modFuncNames map[string]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.CallExpr:
		// Rewrite unqualified calls to module-internal functions
		if e.Module == "" && modFuncNames[e.Name] {
			e.Name = moduleName + "_" + e.Name
		}
		for _, arg := range e.Args {
			prefixCallsInExpr(arg, moduleName, modFuncNames)
		}
	case *ast.BinaryExpr:
		prefixCallsInExpr(e.Left, moduleName, modFuncNames)
		prefixCallsInExpr(e.Right, moduleName, modFuncNames)
	case *ast.UnaryExpr:
		prefixCallsInExpr(e.Operand, moduleName, modFuncNames)
	case *ast.IndexExpr:
		prefixCallsInExpr(e.Array, moduleName, modFuncNames)
		prefixCallsInExpr(e.Index, moduleName, modFuncNames)
	case *ast.ArrayLitExpr:
		for _, elem := range e.Elems {
			prefixCallsInExpr(elem, moduleName, modFuncNames)
		}
	case *ast.StructLitExpr:
		for _, val := range e.FieldValues {
			prefixCallsInExpr(val, moduleName, modFuncNames)
		}
	case *ast.FieldAccessExpr:
		prefixCallsInExpr(e.Object, moduleName, modFuncNames)
	case *ast.SpawnExpr:
		if e.Call != nil {
			prefixCallsInExpr(e.Call, moduleName, modFuncNames)
		}
		for _, stmt := range e.Body {
			prefixCallsInStmt(stmt, moduleName, modFuncNames)
		}
	case *ast.ReceiveExpr:
		prefixCallsInExpr(e.Source, moduleName, modFuncNames)
	// IntLit, FloatLit, BoolLit, StringLit, CharLit, Ident, ChannelExpr — no calls to rewrite
	}
}
