package resolve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/lexer"
	"github.com/ensamuel7/dex/parser"
)

// resolveProject writes files into a temporary directory, parses main.dx the
// way the build does, and resolves its imports.
func resolveProject(t *testing.T, files map[string]string) (*ast.Program, error) {
	t.Helper()
	dir := t.TempDir()
	for name, src := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mainPath := filepath.Join(dir, "main.dx")
	tokens, err := lexer.NewWithFile(files["main.dx"], mainPath).Tokenize()
	if err != nil {
		t.Fatal(err)
	}
	imports := ExtractImports(tokens)
	p := parser.New(tokens)
	for _, mt := range PreRegisterUserStructs(imports, dir) {
		p.AddModuleType(mt.Module, mt.Name, mt.Kind)
	}
	aliases, err := ModuleAliases(imports, dir, mainPath)
	if err != nil {
		return nil, err
	}
	p.SetModuleAliases(aliases)
	program, errs := p.Parse()
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	FlattenStructMethods(program)
	return program, ResolveUserModules(program, dir)
}

func TestTwoFilesWithOneBaseNameGetTwoIdentities(t *testing.T) {
	program, err := resolveProject(t, map[string]string{
		"main.dx": `import "a/front"
import "b/front" as "bfront"
fn main(): void {}
`,
		"a/front.dx": `import "util"
fn run(): int { return util.make() }
`,
		"b/front.dx": `import "util"
fn run(): int { return util.make() }
`,
		"a/util.dx": `fn make(): int { return 1 }
`,
		"b/util.dx": `fn make(): int { return 2 }
`,
	})
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, fn := range program.Functions {
		names[fn.Name] = true
	}
	for _, want := range []string{"util_make", "util_2_make", "front_run", "bfront_run"} {
		if !names[want] {
			t.Errorf("no function %s; have %v", want, names)
		}
	}
	// b/front's call reaches b/util, which took the second name.
	for _, fn := range program.Functions {
		if fn.Name != "bfront_run" {
			continue
		}
		ret := fn.Body[0].(*ast.ReturnStmt).Value.(*ast.CallExpr)
		if ret.Module != "util_2" {
			t.Errorf("bfront.run calls %s.make, want util_2.make", ret.Module)
		}
	}
}

func TestTwoImportsUnderOneNameInAFile(t *testing.T) {
	_, err := resolveProject(t, map[string]string{
		"main.dx": `import "a/util"
import "b/util"
fn main(): void {}
`,
		"a/util.dx": `fn make(): int { return 1 }
`,
		"b/util.dx": `fn make(): int { return 2 }
`,
	})
	if err == nil || !strings.Contains(err.Error(), "two imports are both named 'util'") {
		t.Fatalf("got %v, want the two-imports error", err)
	}
	if !strings.Contains(err.Error(), "main.dx:2:") {
		t.Errorf("error should point at the second import: %v", err)
	}
}

func TestExternalModuleIsLocalToItsFile(t *testing.T) {
	_, err := resolveProject(t, map[string]string{
		"main.dx": `import "lib"
external module cstr {
    include "<string.h>"
    fn length(s: string): long = "strlen"
}
fn main(): void {}
`,
		"lib.dx": `fn size(s: string): long { return cstr.length(s) }
`,
	})
	if err == nil || !strings.Contains(err.Error(), "external module 'cstr' is declared in the main program") {
		t.Fatalf("got %v, want the main-program external error", err)
	}
}

func TestModuleExternalIsRenamedToItsScopedName(t *testing.T) {
	program, err := resolveProject(t, map[string]string{
		"main.dx": `import "lib"
fn main(): void {}
`,
		"lib.dx": `external module cstr {
    include "<string.h>"
    fn length(s: string): long = "strlen"
}
fn size(s: string): long { return cstr.length(s) }
`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(program.Externals) != 1 || program.Externals[0].Name != "lib__cstr" || program.Externals[0].Local != "cstr" {
		t.Fatalf("externals = %+v, want lib__cstr written as cstr", program.Externals)
	}
	for _, fn := range program.Functions {
		if fn.Name == "lib_size" {
			call := fn.Body[0].(*ast.ReturnStmt).Value.(*ast.CallExpr)
			if call.Module != "lib__cstr" {
				t.Errorf("lib.size calls %s.length, want lib__cstr.length", call.Module)
			}
		}
	}
}

// A local variable named like a module wins inside the function declaring it.
func TestAliasRenameRespectsShadowing(t *testing.T) {
	program, err := resolveProject(t, map[string]string{
		"main.dx": `import "a/front"
import "b/util"
fn main(): void {}
fn reach(): int {
    return util.make()
}
fn shadow(): int {
    let util: int[] = [1]
    return util.len()
}
`,
		"a/front.dx": `import "util"
fn run(): int { return util.make() }
`,
		"a/util.dx": `fn make(): int { return 1 }
`,
		"b/util.dx": `fn make(): int { return 2 }
`,
	})
	if err != nil {
		t.Fatal(err)
	}
	// b/util is util_2 here, because a/front's import took `util` first.
	for _, fn := range program.Functions {
		switch fn.Name {
		case "reach":
			if call := fn.Body[0].(*ast.ReturnStmt).Value.(*ast.CallExpr); call.Module != "util_2" {
				t.Errorf("reach calls %s.make, want util_2.make", call.Module)
			}
		case "shadow":
			if call := fn.Body[1].(*ast.ReturnStmt).Value.(*ast.CallExpr); call.Module != "util" {
				t.Errorf("a local array named util was renamed to %s", call.Module)
			}
		}
	}
}

func TestMainGlobalsFollowModuleGlobals(t *testing.T) {
	program, err := resolveProject(t, map[string]string{
		"main.dx": `import "a/consts" as "c"
let TOP: int = c.LIMIT + 1
fn main(): void {}
`,
		"a/consts.dx": `const LIMIT: int = 100
`,
	})
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, gl := range program.GlobalLets {
		order = append(order, gl.Name)
	}
	if len(order) != 2 || order[0] != "c_LIMIT" || order[1] != "TOP" {
		t.Fatalf("globals in order %v, want the module's constant first", order)
	}
	// The alias in the initialiser was renamed and then flattened.
	if bin, ok := program.GlobalLets[1].Value.(*ast.BinaryExpr); !ok || bin.Left.(*ast.Ident).Name != "c_LIMIT" {
		t.Errorf("TOP's initialiser does not read c_LIMIT: %#v", program.GlobalLets[1].Value)
	}
}

func TestAliasRenamesGlobalInitialiserAndReceiverChain(t *testing.T) {
	program, err := resolveProject(t, map[string]string{
		"main.dx": `import "a/front"
import "b/util"
let FIRST: int = util.make()
fn main(): void {}
fn chained(): int {
    return util.cache.len()
}
`,
		"a/front.dx": `import "util"
fn run(): int { return util.make() }
`,
		"a/util.dx": `fn make(): int { return 1 }
`,
		"b/util.dx": `let cache: int[] = []
fn make(): int { return 2 }
`,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, gl := range program.GlobalLets {
		if gl.Name == "FIRST" {
			if call := gl.Value.(*ast.CallExpr); call.Module != "util_2" {
				t.Errorf("FIRST calls %s.make, want util_2.make", call.Module)
			}
		}
	}
	for _, fn := range program.Functions {
		if fn.Name == "chained" {
			call := fn.Body[0].(*ast.ReturnStmt).Value.(*ast.CallExpr)
			if !strings.HasPrefix(call.Module, "util_2") {
				t.Errorf("chained calls through %s, want util_2's cache", call.Module)
			}
		}
	}
}

// A generic is re-parsed after the main program's calls were renamed, so its
// instantiation has to be renamed the same way.
func TestInstantiatorRenamesAliasedModules(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lib.dx"), []byte("fn make(): int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := `import "lib" as "l"
fn pick<T>(x: T): int {
    return l.make()
}
fn main(): void {}
`
	tokens, err := lexer.New(src).Tokenize()
	if err != nil {
		t.Fatal(err)
	}
	p := parser.New(tokens)
	if _, errs := p.Parse(); len(errs) > 0 {
		t.Fatal(errs[0])
	}
	in := Instantiator{Parser: p, Aliases: map[string]string{"l": "lib"}}
	fn, _, err := in.InstantiateFunc("pick", []ast.Type{ast.TypeInt})
	if err != nil {
		t.Fatal(err)
	}
	if call := fn.Body[0].(*ast.ReturnStmt).Value.(*ast.CallExpr); call.Module != "lib" {
		t.Errorf("instantiation calls %s.make, want lib.make", call.Module)
	}
}
