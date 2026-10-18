package format

import (
	"fmt"

	"github.com/ensamuel7/dex/lexer"
	"github.com/ensamuel7/dex/token"
)

// verify re-lexes the formatted text and insists on the same token stream it
// started from. This is what makes the formatter safe to run over a whole
// codebase unattended: a bug in a spacing rule or a string escape becomes a
// refusal to write the file rather than a silent edit to what the program says.
//
// Positions are deliberately not compared — moving tokens is the entire job.
func verify(before []token.Token, out, filename string) error {
	lx := lexer.NewWithFile(out, filename)
	lx.KeepComments = true
	after, err := lx.Tokenize()
	if err != nil {
		return fmt.Errorf("%s: formatting produced source that does not lex: %w", filename, err)
	}

	for i := 0; i < len(before) && i < len(after); i++ {
		b, a := before[i], after[i]
		if b.Kind == a.Kind && b.Value == a.Value {
			continue
		}
		return fmt.Errorf("%s: formatting changed the program at %d:%d: %s %q became %s %q",
			filename, b.Line, b.Col, b.Kind, b.Value, a.Kind, a.Value)
	}
	if len(before) != len(after) {
		return fmt.Errorf("%s: formatting changed the program: %d tokens became %d",
			filename, len(before), len(after))
	}
	return nil
}
