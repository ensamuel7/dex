package parser

import (
	"testing"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/lexer"
)

func parseExtern(t *testing.T, src string) *ast.Program {
	t.Helper()
	toks, err := lexer.New(src).Tokenize()
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	prog, errs := New(toks).Parse()
	if len(errs) > 0 {
		t.Fatalf("parse: %v", errs[0])
	}
	return prog
}

func TestParseExternalModule(t *testing.T) {
	prog := parseExtern(t, `external module zlib {
		include "<zlib.h>"
		link "-lz"
		fn compressBound(sourceLen: long): long
		fn flags(): long = "zlibCompileFlags"
	}
	fn main(): void {}`)

	if len(prog.Externals) != 1 {
		t.Fatalf("got %d external modules, want 1", len(prog.Externals))
	}
	em := prog.Externals[0]
	if em.Name != "zlib" {
		t.Errorf("name = %q, want zlib", em.Name)
	}
	if len(em.Includes) != 1 || em.Includes[0] != "<zlib.h>" {
		t.Errorf("includes = %v", em.Includes)
	}
	if len(em.Links) != 1 || em.Links[0] != "-lz" {
		t.Errorf("links = %v", em.Links)
	}
	if len(em.Funcs) != 2 {
		t.Fatalf("got %d funcs, want 2", len(em.Funcs))
	}
	// With no `= "symbol"`, the C name is the declared name.
	if em.Funcs[0].CName != "compressBound" {
		t.Errorf("CName = %q, want compressBound", em.Funcs[0].CName)
	}
	if em.Funcs[0].ParamNames[0] != "sourceLen" || em.Funcs[0].Params[0] != ast.TypeLong {
		t.Errorf("param = %v %v", em.Funcs[0].ParamNames, em.Funcs[0].Params)
	}
	// The explicit form overrides it.
	if em.Funcs[1].Name != "flags" || em.Funcs[1].CName != "zlibCompileFlags" {
		t.Errorf("got %q -> %q", em.Funcs[1].Name, em.Funcs[1].CName)
	}
	if len(em.Funcs[1].Params) != 0 {
		t.Errorf("no-arg function got %d params", len(em.Funcs[1].Params))
	}
}

func TestParseExternalDefaultsToVoid(t *testing.T) {
	prog := parseExtern(t, `external module c {
		fn seed(n: int)
	}
	fn main(): void {}`)
	if prog.Externals[0].Funcs[0].ReturnType != ast.TypeVoid {
		t.Errorf("return type = %v, want void", prog.Externals[0].Funcs[0].ReturnType)
	}
}

// module, include and link are keywords only inside an external block, so they
// stay usable as ordinary names.
func TestExternalSoftKeywordsStayUsable(t *testing.T) {
	prog := parseExtern(t, `fn main(): void {
		let module: int = 1
		let include: int = 2
		let link: int = 3
	}`)
	if len(prog.Functions) != 1 {
		t.Fatalf("got %d functions", len(prog.Functions))
	}
}

func TestParseExternalRequiresModuleKeyword(t *testing.T) {
	toks, _ := lexer.New("external zlib { fn f(): int }\nfn main(): void {}").Tokenize()
	_, errs := New(toks).Parse()
	if len(errs) == 0 {
		t.Fatal("expected an error for a missing 'module'")
	}
}
