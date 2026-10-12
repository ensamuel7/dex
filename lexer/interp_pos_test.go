package lexer

import (
	"testing"

	"github.com/ensamuel7/dex/token"
)

// An interpolated expression is tokenized by a sub-lexer reading a detached
// substring. Without the offset it is given, every token inside ${...} reports
// line 1 column 1 — and so does every diagnostic about it.
func TestInterpolatedExpressionKeepsItsPosition(t *testing.T) {
	src := "fn main(): void {\n    let n = 1\n    let s = \"n=${n}\"\n}\n"
	tokens, err := New(src).Tokenize()
	if err != nil {
		t.Fatalf("lexer error: %v", err)
	}

	var found bool
	for i, tok := range tokens {
		if tok.Kind != token.TokenInterpStringStart {
			continue
		}
		// The expression's tokens follow the start marker.
		expr := tokens[i+1]
		if expr.Kind != token.TokenIdent || expr.Value != "n" {
			t.Fatalf("expected the interpolated identifier, got %v %q", expr.Kind, expr.Value)
		}
		if expr.Line != 3 {
			t.Errorf("interpolated expression reports line %d, want 3", expr.Line)
		}
		if expr.Col <= 1 {
			t.Errorf("interpolated expression reports column %d, want a real column", expr.Col)
		}
		found = true
	}
	if !found {
		t.Fatal("no interpolated string was produced")
	}
}
