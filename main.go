package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ensamuel7/dex-lang/ast"
	"github.com/ensamuel7/dex-lang/checker"
	"github.com/ensamuel7/dex-lang/codegen"
	"github.com/ensamuel7/dex-lang/docgen"
	"github.com/ensamuel7/dex-lang/lexer"
	"github.com/ensamuel7/dex-lang/lsp"
	"github.com/ensamuel7/dex-lang/parser"
	_ "github.com/ensamuel7/dex-lang/stdlib"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: dex <build|run|test|lsp|docs> <file.dx>")
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
		fmt.Fprintln(os.Stderr, "Usage: dex <build|run> <file.dx>")
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

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		fmt.Fprintln(os.Stderr, "Usage: dex <build|run|test|lsp|docs> <file.dx>")
		os.Exit(1)
	}
}

func runTests() {
	var files []string

	if len(os.Args) >= 3 {
		// Run specific test file
		files = append(files, os.Args[2])
	} else {
		// Discover *_test.dx files in current directory
		matches, err := filepath.Glob("*_test.dx")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if len(matches) == 0 {
			fmt.Fprintln(os.Stderr, "No *_test.dx files found")
			os.Exit(1)
		}
		files = matches
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

func build(filename string) (string, error) {
	// Reset type registries to prevent state leaking between compilations
	ast.ResetStructTypes()
	ast.ResetChanTypes()
	ast.ResetTaskTypes()

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

	// Parse
	p := parser.New(tokens)
	program, err := p.Parse()
	if err != nil {
		return "", fmt.Errorf("%s:%v", filename, err)
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
