package parser

import (
	"testing"

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
