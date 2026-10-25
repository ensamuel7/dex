package checker

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/lexer"
	"github.com/ensamuel7/dex/parser"
	"github.com/ensamuel7/dex/resolve"
	"github.com/ensamuel7/dex/stdlib"
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
	for _, s := range deepShapes(200) {
		f.Add(s)
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

func resetRegistries() {
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
	ast.ResetInterfaceTypes()
	stdlib.RegisterAllModuleTypes()
	ast.RegisterExceptionType()
}

func frontend(src, dir string) {
	resetRegistries()
	tokens, err := lexer.NewWithFile(src, filepath.Join(dir, "fuzz.dx")).Tokenize()
	if err != nil {
		return
	}
	imports := resolve.ExtractImports(tokens)
	for _, imp := range imports {
		if stdlib.Lookup(imp.Path) == nil {
			return
		}
	}
	aliases, err := resolve.ModuleAliases(imports, dir, filepath.Join(dir, "fuzz.dx"))
	if err != nil {
		return
	}
	p := parser.New(tokens)
	p.SetModuleAliases(aliases)
	for _, name := range stdlib.ModuleTypesForImports(resolve.ExtractImportPaths(tokens)) {
		p.AddStructName(name)
	}
	p.AddStructName("Exception")
	program, errs := p.Parse()
	if len(errs) > 0 {
		return
	}
	resolve.FlattenStructMethods(program)
	if err := resolve.ResolveUserModules(program, dir); err != nil {
		return
	}
	ch := New()
	ch.SetGenerics(program.GenericFuncs, resolve.Instantiator{Parser: p, Aliases: program.ModuleAliases})
	if len(ch.Check(program)) > 0 {
		return
	}
	resolve.RenameCCollisions(program)
}

func FuzzCheck(f *testing.F) {
	addExampleSeeds(f)
	dir := f.TempDir()
	f.Fuzz(func(t *testing.T, src string) {
		withTimeout(t, 20*time.Second, func() {
			frontend(src, dir)
		})
	})
}

func deepShapes(n int) []string {
	return []string{
		"fn main(): void { let a = " + strings.Repeat("(", n) + "1" + strings.Repeat(")", n) + " }",
		"fn main(): void { let a = 1" + strings.Repeat(" + 1", n) + " }",
		"fn main(): void { let a = true" + strings.Repeat(" && true", n) + " }",
		"struct P { p: int }\nfn main(): void { let a = P { p: 1 }\n let b = a" + strings.Repeat(".p", n) + " }",
		"fn main(): void { let a = [1]\n let b = a" + strings.Repeat("[0]", n) + " }",
		"fn main(): void { let a = \"x\"" + strings.Repeat(".len()", n) + " }",
		"fn main(): void { let a = " + strings.Repeat("[", n) + "1" + strings.Repeat("]", n) + " }",
		"fn main(): void { let a: int" + strings.Repeat("[]", n) + " = [] }",
		"fn main(): void { " + strings.Repeat("if (true) { ", n) + strings.Repeat("}", n) + " }",
		"fn main(): void { let f = " + strings.Repeat("fn(): void { ", n) + strings.Repeat("}", n) + " }",
		"fn main(): void { let a = " + strings.Repeat("\"${", n) + "1" + strings.Repeat("}\"", n) + " }",
		"fn main(): void { let a = " + strings.Repeat("- ", n) + "1 }",
		"fn main(): void { let a: int" + strings.Repeat("?", n) + " = 1 }",
		"fn main(): void { if (true) {}" + strings.Repeat(" else if (true) {}", n) + " }",
	}
}

func TestDeepNesting(t *testing.T) {
	dir := t.TempDir()
	for _, src := range deepShapes(100000) {
		withTimeout(t, 60*time.Second, func() {
			frontend(src, dir)
		})
	}
}
