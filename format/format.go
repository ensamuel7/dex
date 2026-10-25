// Package format is `dex format`: one canonical shape for a .dx file, so that how
// code is laid out stops being a thing anyone decides.
//
// It works on the token stream rather than the AST, and that is the whole
// design. An AST printer has to know all forty-nine statement and expression
// kinds, and every kind it does not know is code it silently drops — a
// formatter that loses a line is worse than no formatter. A token printer can
// only ever lay the same tokens out differently, and Source proves it did
// exactly that by re-lexing its own output and comparing the two streams before
// returning (verify). A bug here is therefore a formatting bug, never a
// deletion.
//
// Newlines are not significant to the DexLang grammar — the lexer discards them
// without emitting a token — so every break this package makes or removes is
// cosmetic by construction. That is why it is allowed to join lines that fit.
package format

import (
	"strings"
	"unicode/utf8"

	"github.com/ensamuel7/dex/lexer"
	"github.com/ensamuel7/dex/token"
)

const (
	// LineWidth is the column a group is allowed to reach before it breaks.
	// Wider than gofmt has any opinion about, because the complaint this
	// formatter answers was lines cut off far earlier than they needed to be.
	// 120 keeps a declaration whose type is module-qualified on one line,
	// which is most of them in a real codebase.
	LineWidth = 120

	// IndentWidth is one level of indentation, matching the existing corpus.
	IndentWidth = 4
)

// Source formats one file's worth of DexLang source. The returned text ends in
// exactly one newline. An error means either the file does not lex or the
// formatter failed its own equivalence check, and in both cases the caller
// should leave the file alone.
func Source(src, filename string) (string, error) {
	lx := lexer.NewWithFile(src, filename)
	lx.KeepComments = true
	raw, err := lx.Tokenize()
	if err != nil {
		return "", err
	}

	out := newPrinter(collapse(raw)).run()

	if err := verify(raw, out, filename); err != nil {
		return "", err
	}
	return out, nil
}

// item is one printable unit: a token with its text already rendered. An
// interpolated string is several tokens in the stream and one item here, so the
// printer never has to reason about a literal's interior.
type item struct {
	kind token.TokenKind
	text string
	line int
	end  int
}

func newItem(kind token.TokenKind, text string, line int) item {
	return item{kind: kind, text: text, line: line, end: line + strings.Count(text, "\n")}
}

func (it item) width() int { return utf8.RuneCountInString(it.text) }

// collapse renders each token and folds an interpolated string's start/expr/mid/
// end run back into the single literal it was written as. The run is nestable —
// an interpolation can contain a string that interpolates — so it recurses.
func collapse(raw []token.Token) []item {
	var items []item
	i := 0
	for i < len(raw) {
		t := raw[i]
		if t.Kind == token.TokenEOF {
			break
		}
		if t.Kind == token.TokenInterpStringStart {
			text, next := renderInterp(raw, i)
			items = append(items, newItem(token.TokenString, text, t.Line))
			i = next
			continue
		}
		items = append(items, newItem(t.Kind, render(t), t.Line))
		i++
	}
	return items
}

// renderInterp reassembles "text${expr}text" from the run starting at lo, and
// returns the index just past the run's end token. The expressions inside are
// printed flat: an interpolation is a literal, and a line break inside one
// reads as a mistake even when it fits.
func renderInterp(raw []token.Token, lo int) (string, int) {
	var b strings.Builder
	b.WriteByte('"')
	b.WriteString(escapeString(raw[lo].Value))

	i := lo + 1
	for i < len(raw) {
		switch raw[i].Kind {
		case token.TokenInterpStringMid:
			b.WriteString("}")
			b.WriteString(escapeString(raw[i].Value))
			b.WriteString("${")
			i++
			continue
		case token.TokenInterpStringEnd:
			b.WriteString("}")
			b.WriteString(escapeString(raw[i].Value))
			b.WriteByte('"')
			return b.String(), i + 1
		case token.TokenInterpStringStart:
			// A nested interpolated string inside the expression.
			inner, next := renderInterp(raw, i)
			b.WriteString(inner)
			i = next
			continue
		}
		// An expression token. The "${" belongs ahead of the first one.
		if i == lo+1 {
			b.WriteString("${")
		}
		sub := collapse(raw[i : i+1])
		if len(sub) == 1 {
			if needsSpaceBefore(raw, i) {
				b.WriteByte(' ')
			}
			b.WriteString(sub[0].text)
		}
		i++
	}
	b.WriteByte('"')
	return b.String(), i
}

// needsSpaceBefore is the spacing rule used inside an interpolation, where the
// printer's precomputed tables are not available. Interpolated expressions are
// short — a name, a field, a call — so a plain "space between two words" test
// covers them.
func needsSpaceBefore(raw []token.Token, i int) bool {
	if i == 0 {
		return false
	}
	prev, cur := raw[i-1].Kind, raw[i].Kind
	if prev == token.TokenInterpStringStart || prev == token.TokenInterpStringMid {
		return false
	}
	switch cur {
	case token.TokenLParen, token.TokenRParen, token.TokenLBracket, token.TokenRBracket,
		token.TokenComma, token.TokenDot, token.TokenColon:
		return false
	}
	switch prev {
	case token.TokenLParen, token.TokenLBracket, token.TokenDot, token.TokenBang:
		return false
	}
	return true
}

func render(t token.Token) string {
	switch t.Kind {
	case token.TokenString:
		return `"` + escapeString(t.Value) + `"`
	case token.TokenChar:
		return "'" + escapeChar(t.Value) + "'"
	case token.TokenTag:
		return "`" + t.Value + "`"
	case token.TokenAnnotation:
		return "#[" + t.Value + "]"
	case token.TokenComment:
		return t.Value
	case token.TokenIdent, token.TokenInt, token.TokenFloat:
		return t.Value
	}
	if t.Value != "" {
		return t.Value
	}
	return t.Kind.String()
}

// escapeString re-escapes a decoded literal. The lexer accepts four escapes, so
// these are the four that can be written back; anything else the file held was
// raw and stays raw.
func escapeString(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func escapeChar(s string) string {
	switch s {
	case `\`:
		return `\\`
	case "'":
		return `\'`
	case "\n":
		return `\n`
	case "\t":
		return `\t`
	}
	return s
}
