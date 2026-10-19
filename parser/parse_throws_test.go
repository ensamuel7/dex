package parser

import (
	"strings"
	"testing"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/lexer"
)

// A `throws` in the signature is recorded on the function, and its absence
// leaves the flag clear — the lint pass compares the two.
func TestParseThrowsInSignature(t *testing.T) {
	src := `
fn risky(): int throws {
    throw Exception("no")
}

fn safe(): int {
    return 1
}
`
	tokens, err := lexer.New(src).Tokenize()
	if err != nil {
		t.Fatalf("lexer error: %v", err)
	}
	p := New(tokens)
	p.AddStructName("Exception")
	program, errs := p.Parse()
	if len(errs) > 0 {
		t.Fatalf("parser error: %v", errs[0])
	}
	if len(program.Functions) != 2 {
		t.Fatalf("expected two functions, got %d", len(program.Functions))
	}
	if !program.Functions[0].Throws {
		t.Error("risky declares throws but the flag is clear")
	}
	if program.Functions[1].Throws {
		t.Error("safe declares nothing but the flag is set")
	}
	if program.Functions[0].Pos.Line != 2 {
		t.Errorf("function position is line %d, want 2", program.Functions[0].Pos.Line)
	}
}

// A type declared in a module registers under it, so two modules can each
// declare the same name — and a bare reference to one resolves only while it
// is unambiguous. These are parser-level checks on the resolution itself; the
// end-to-end case is examples/moduleTypes_test.dx.
func TestModuleScopedTypeResolution(t *testing.T) {
	ast.ResetStructTypes()
	ast.ResetEnumTypes()

	// Two modules, each declaring Response, as the pre-registration would.
	ast.RegisterStructType(ast.StructDef{Name: "alpha_Response", Module: "alpha"})
	ast.RegisterStructType(ast.StructDef{Name: "beta_Response", Module: "beta"})
	ast.RegisterStructType(ast.StructDef{Name: "onlyAlpha_Thing", Module: "onlyAlpha"})

	parse := func(src string, module string, mods [][2]string) error {
		tokens, err := lexer.New(src).Tokenize()
		if err != nil {
			return err
		}
		p := New(tokens)
		p.SetModule(module)
		for _, m := range mods {
			p.AddModuleType(m[0], m[1], "struct")
		}
		_, errs := p.Parse()
		if len(errs) > 0 {
			return errs[0]
		}
		return nil
	}

	both := [][2]string{{"alpha", "Response"}, {"beta", "Response"}}
	one := [][2]string{{"onlyAlpha", "Thing"}}

	// Qualified always resolves, even with both in scope.
	if err := parse("fn f(r: alpha.Response): void {\n}\n", "", both); err != nil {
		t.Errorf("a qualified type should resolve: %v", err)
	}

	// Bare resolves when exactly one module declares the name.
	if err := parse("fn f(t: Thing): void {\n}\n", "", one); err != nil {
		t.Errorf("an unambiguous bare type should resolve: %v", err)
	}

	// Bare is an error when two do, and the message names both and how to fix.
	err := parse("fn f(r: Response): void {\n}\n", "", both)
	if err == nil {
		t.Fatal("a bare name declared by two modules should not resolve")
	}
	for _, want := range []string{"ambiguous type 'Response'", "alpha.Response", "beta.Response", "qualify it"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message missing %q: %v", want, err)
		}
	}

	// Inside a module that declares the name, its own wins — no ambiguity.
	if err := parse("fn f(r: Response): void {\n}\n", "alpha", both); err != nil {
		t.Errorf("a module's own type should win over an import: %v", err)
	}
}

func TestCatchAndThrowsResolveModuleTypes(t *testing.T) {
	ast.ResetStructTypes()
	ast.RegisterStructType(ast.StructDef{Name: "db2_DbError", Module: "db2"})
	src := `
fn load(): int throws db2.DbError, Oops {
    try {
        return 1
    } catch (e: db2.DbError) {
        return 2
    }
}
`
	tokens, err := lexer.New(src).Tokenize()
	if err != nil {
		t.Fatalf("lexer error: %v", err)
	}
	p := New(tokens)
	p.AddModuleType("db2", "DbError", "struct")
	program, errs := p.Parse()
	if len(errs) > 0 {
		t.Fatalf("parser error: %v", errs[0])
	}
	fn := program.Functions[0]
	if strings.Join(fn.ThrowsTypes, ",") != "db2_DbError,Oops" {
		t.Errorf("throws = %v, want [db2_DbError Oops]", fn.ThrowsTypes)
	}
	try := fn.Body[0].(*ast.TryCatchStmt)
	if try.Catches[0].TypeName != "db2_DbError" {
		t.Errorf("catch type = %q, want db2_DbError", try.Catches[0].TypeName)
	}

	tokens, _ = lexer.New("fn f(): void throws nope.Missing {\n}\n").Tokenize()
	if _, errs := New(tokens).Parse(); len(errs) == 0 || !strings.Contains(errs[0].Error(), "unknown type 'nope.Missing'") {
		t.Errorf("an unknown qualified type should be reported, got %v", errs)
	}
}
