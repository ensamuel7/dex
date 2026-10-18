package parser

import (
	"strings"
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

func parseExternalFails(t *testing.T, src, substr string) {
	t.Helper()
	toks, err := lexer.New(src).Tokenize()
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	_, errs := New(toks).Parse()
	if len(errs) == 0 {
		t.Fatalf("expected an error containing %q, got none", substr)
	}
	if !strings.Contains(errs[0].Error(), substr) {
		t.Fatalf("error = %q, want it to contain %q", errs[0], substr)
	}
}

// #[owned] already means something else on a variable, so the old spelling
// points at the new one rather than being quietly accepted.
func TestExternalRejectsOwnedAnnotation(t *testing.T) {
	parseExternalFails(t, `external module z {
		#[owned]
		fn f(s: string): string
	}
	fn main(): void {}`, "write the return type as 'owned string'")
}

func TestExternalRejectsAnnotations(t *testing.T) {
	parseExternalFails(t, `external module z {
		#[noEscape]
		fn f(s: string): string
	}
	fn main(): void {}`, "takes no annotations")
}

func TestParseExternalOwnedReturn(t *testing.T) {
	prog := parseExtern(t, `external module cstr {
		include "<string.h>"
		fn errorText(n: int): string = "strerror"
		fn dup(s: string): owned string = "strdup"
	}
	fn main(): void {}`)
	fns := prog.Externals[0].Funcs
	if fns[0].OwnedReturn {
		t.Error("errorText should not be owned")
	}
	if !fns[1].OwnedReturn {
		t.Error("dup should be owned")
	}
	if fns[1].ReturnType != ast.TypeString {
		t.Errorf("dup return = %v, want string", fns[1].ReturnType)
	}
}

func TestParseExternalHandleType(t *testing.T) {
	prog := parseExtern(t, `external module cio {
		include "<stdio.h>"
		type File closed by "fclose"
		fn open(path: string, mode: string): File? = "fopen"
	}
	fn main(): void {}`)
	em := prog.Externals[0]
	if len(em.Types) != 1 {
		t.Fatalf("got %d types, want 1", len(em.Types))
	}
	if em.Types[0].Name != "File" || em.Types[0].Closer != "fclose" {
		t.Errorf("type = %q closed by %q", em.Types[0].Name, em.Types[0].Closer)
	}
	// The handle resolves in type position, as an optional here.
	ret := em.Funcs[0].ReturnType
	if !ast.IsOptionalType(ret) || !ast.IsHandleType(ast.OptionalInnerType(ret)) {
		t.Errorf("open return = %v, want an optional handle", ret)
	}
}

func TestParseExternalHandleNeedsACloser(t *testing.T) {
	parseExternalFails(t, `external module cio {
		type File
		fn f(n: int): int
	}
	fn main(): void {}`, "closed by")
}
