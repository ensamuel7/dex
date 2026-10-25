package format

import (
	"strings"

	"github.com/ensamuel7/dex/token"
)

// printer lays the token stream out. Everything it needs to know about shape —
// which brace opens a block rather than a literal, which `<` opens a type
// argument list rather than a comparison, where one statement ends and the next
// begins — is decided once in classify and then only read.
type printer struct {
	it       []item
	match    []int  // opener <-> closer, -1 when unmatched
	block    []bool // for a brace: a block rather than a literal
	generic  []bool // for < and >: a type argument list rather than a comparison
	boundary []bool // a new segment starts at this index
	segStart []int  // index of the first token of this token's segment

	buf        strings.Builder
	col        int
	reserve    int // columns owed to punctuation not yet written, such as a trailing comma
	lineIndent int
	needIndent bool
}

func newPrinter(items []item) *printer {
	p := &printer{
		it:       items,
		match:    make([]int, len(items)),
		block:    make([]bool, len(items)),
		generic:  make([]bool, len(items)),
		boundary: make([]bool, len(items)),
		segStart: make([]int, len(items)),
	}
	for i := range p.match {
		p.match[i] = -1
	}
	return p
}

// A segment is the run of tokens that wants to live on one line: a statement, a
// declaration's header up to and including its opening brace, a closing brace,
// a comment. Breaks inside one are the formatter's business; breaks between two
// are not negotiable.
func (p *printer) run() string {
	p.classify()

	n := len(p.it)
	if n == 0 {
		return ""
	}

	indent := 0
	prevEnd := -1
	i := 0
	for i < n {
		if p.it[i].kind == token.TokenRBrace && p.block[i] {
			indent -= IndentWidth
			if indent < 0 {
				indent = 0
			}
		}

		end := p.segmentEnd(i)

		if prevEnd < 0 {
			p.lineIndent = indent
			p.needIndent = true
		} else {
			p.separate(prevEnd, i, indent)
		}

		p.segment(i, end, indent+IndentWidth)

		if last := end - 1; p.it[last].kind == token.TokenLBrace && p.block[last] {
			indent += IndentWidth
		}

		prevEnd = end - 1
		i = end
	}

	out := strings.TrimRight(p.buf.String(), " \t\n")
	if out == "" {
		return ""
	}
	return out + "\n"
}

func (p *printer) segmentEnd(lo int) int {
	for j := lo + 1; j < len(p.it); j++ {
		if p.boundary[j] {
			return j
		}
	}
	return len(p.it)
}

// separate emits what goes between two segments: a space when they belong on
// one line after all, otherwise a newline, and a blank line where the author
// left one or where a top-level declaration needs the air.
func (p *printer) separate(prevIdx, curIdx, indent int) {
	prev, cur := p.it[prevIdx], p.it[curIdx]

	if cur.kind == token.TokenComment && cur.line == prev.end {
		p.write(" ")
		return
	}
	if prev.kind == token.TokenRBrace && p.block[prevIdx] && continuesBlock(cur.kind) {
		p.write(" ")
		return
	}

	blank := cur.line-prev.end >= 2
	if prev.kind == token.TokenLBrace && p.block[prevIdx] {
		blank = false
	}
	if cur.kind == token.TokenRBrace && p.block[curIdx] {
		blank = false
	}
	if p.wantsBlankBefore(prevIdx, curIdx, indent) {
		blank = true
	}

	p.newline(indent)
	if blank {
		p.buf.WriteByte('\n')
	}
}

// wantsBlankBefore adds the blank line a reader needs and the author may not
// have left: one before every top-level declaration, and one after the import
// block. A comment directly above a declaration belongs to it, so the blank
// goes above the comment rather than between the two.
func (p *printer) wantsBlankBefore(prevIdx, curIdx, indent int) bool {
	if indent != 0 {
		return false
	}
	if prev := p.it[prevIdx].kind; prev == token.TokenComment || prev == token.TokenAnnotation {
		return false
	}
	if p.it[p.segStart[prevIdx]].kind == token.TokenImport && p.it[curIdx].kind != token.TokenImport {
		return true
	}
	if p.it[curIdx].kind == token.TokenConst && p.it[p.segStart[prevIdx]].kind == token.TokenConst {
		// A run of related constants is written as a run on purpose. Only
		// declarations with bodies get the air whether they asked for it or not.
		return false
	}
	if p.it[curIdx].kind == token.TokenComment {
		// A comment above a declaration belongs to it, so the air goes above
		// the comment rather than between the comment and what it describes.
		for k := curIdx + 1; k < len(p.it); k++ {
			if p.it[k].kind == token.TokenComment {
				continue
			}
			return declStart(p.it[k].kind)
		}
		return false
	}
	return declStart(p.it[curIdx].kind)
}

func declStart(k token.TokenKind) bool {
	switch k {
	case token.TokenFn, token.TokenFunction, token.TokenStruct, token.TokenEnum,
		token.TokenInterface, token.TokenConst, token.TokenAnnotation,
		token.TokenPublic, token.TokenPrivate:
		return true
	}
	return false
}

func continuesBlock(k token.TokenKind) bool {
	switch k {
	case token.TokenElse, token.TokenCatch, token.TokenFinally:
		return true
	}
	return false
}

// segment prints [lo,hi) on one line if it fits, and otherwise breaks it. cont
// is the indent its continuation lines take — one level in from the statement
// when the statement is being broken up, and the current level when the caller
// has already indented, so that the operands of one expression line up with
// each other instead of stepping right once per nesting level.
func (p *printer) segment(lo, hi, cont int) {
	if p.cursor()+p.flatWidth(lo, hi)+p.reserve <= LineWidth {
		p.flat(lo, hi)
		return
	}
	p.broken(lo, hi, cont)
}

func (p *printer) flat(lo, hi int) {
	for k := lo; k < hi; k++ {
		if k > lo && p.space(k) {
			p.write(" ")
		}
		p.write(p.it[k].text)
	}
}

func (p *printer) flatWidth(lo, hi int) int {
	w := 0
	for k := lo; k < hi; k++ {
		// A line comment runs to the end of the line, so nothing after one can
		// share it: a run with a comment anywhere but last never fits.
		if p.it[k].kind == token.TokenComment && k < hi-1 {
			return LineWidth * 4
		}
		if k > lo && p.space(k) {
			w++
		}
		w += p.it[k].width()
	}
	return w
}

// broken prints a segment too wide for one line. Where it breaks is decided
// entirely by what fits, never by where the author happened to press return:
// the layout of a statement is a function of the statement, so two people
// writing the same code get the same file back.
//
// The order is the one that keeps the most meaning: split at the loosest-binding
// operator in the statement first, because that is the seam a reader already
// sees, and only then open up the bracketed groups.
func (p *printer) broken(lo, hi, cont int) {
	if ops := p.loosestOps(lo, hi); len(ops) > 0 && !p.preferGroup(ops[0], hi, cont) {
		start := lo
		for _, op := range ops {
			p.segment(start, op+1, cont)
			p.newline(cont)
			start = op + 1
		}
		p.segment(start, hi, cont)
		return
	}

	for k := lo; k < hi; {
		// After a comment the line has already been ended, so nothing separates.
		if k > lo && p.space(k) && p.it[k-1].kind != token.TokenComment {
			p.write(" ")
		}

		if p.it[k].kind == token.TokenComment {
			p.write(p.it[k].text)
			if k+1 < hi {
				p.newline(cont)
			}
			k++
			continue
		}

		j := p.match[k]
		if !isOpener(p.it[k].kind) || j < 0 || j >= hi {
			p.write(p.it[k].text)
			k++
			continue
		}

		switch {
		case p.staysFlat(k, j, hi):
			p.flat(k, j+1)
		case p.hugged(k, j):
			// A call whose only argument is a literal or a lambda keeps the
			// brackets together — `f({` ... `})` — rather than indenting the
			// argument one level for nothing.
			p.write(p.it[k].text)
			inner := p.match[k] - 1
			p.expand(k+1, inner, p.elements(k+1, inner))
			p.write(p.it[j].text)
		default:
			p.expand(k, j, p.elements(k, j))
		}
		k = j + 1
	}
}

// staysFlat decides whether a group can be left on one line. The measurement
// runs to the end of the segment rather than to the group's own closing
// bracket, because what follows the group shares the line with it: a parameter
// list that fits exactly leaves `): void {` hanging past the margin.
//
// The exception is a group with a later group to open. Expanding that one may
// be all the line needs, and expanding both when one would do is how a short
// call ends up spread over five lines.
func (p *printer) staysFlat(open, close, hi int) bool {
	if p.cursor()+p.flatWidth(open, hi)+p.reserve <= LineWidth {
		return true
	}
	// Opening this group cannot rescue the line: what precedes the bracket is
	// already past the margin, so breaking after it buys nothing and costs two
	// lines. `newCertificationController(` is the real case.
	if p.cursor()+1 > LineWidth {
		return true
	}
	if p.cursor()+p.flatWidth(open, close+1)+p.reserve > LineWidth {
		return false
	}
	for k := close + 1; k < hi; k++ {
		if isOpener(p.it[k].kind) && p.match[k] < hi && len(p.elements(k, p.match[k])) > 1 {
			return true
		}
	}
	return false
}

// preferGroup decides between the two ways to lay out an assignment that does
// not fit: break after the `=`, or keep the right-hand side on the line and
// open up a group inside it. The second reads better whenever there is a group
// to open, because the name and what it is being given stay on one line:
//
//	let obj: string = json.encode({
//	    message: "hi"
//	})
//
// rather than an `=` left hanging at the end of a line. With no group to open —
// a long arithmetic expression, say — there is nothing to prefer and the break
// after the operator is the only one available.
func (p *printer) preferGroup(op, hi, cont int) bool {
	if p.opPrecedence(op) != 1 {
		return false
	}
	// A hugged group reads better kept on the line whatever else would fit,
	// because the brackets stay with the call they belong to.
	for k := op + 1; k < hi; k++ {
		if isOpener(p.it[k].kind) && p.match[k] > k+1 && p.match[k] < hi && p.hugged(k, p.match[k]) {
			return true
		}
	}
	// Otherwise the right-hand side going to its own line is the better of the
	// two breaks whenever it fits there. Opening a group instead spreads a call
	// whose arguments are three short names over five lines while the line
	// above it is still the long one.
	if cont+p.flatWidth(op+1, hi)+p.reserve <= LineWidth {
		return false
	}
	for k := op + 1; k < hi; k++ {
		if !isOpener(p.it[k].kind) || p.match[k] <= k+1 || p.match[k] >= hi {
			continue
		}
		// Only a group worth opening counts: a comma list, or a sole literal or
		// lambda that can be hugged. A call with one short argument is neither,
		// and opening it strands that argument on a line of its own while the
		// line above is still too long.
		if len(p.elements(k, p.match[k])) > 1 || p.hugged(k, p.match[k]) {
			return true
		}
	}
	return false
}

// hugged reports whether a group holds exactly one element and that element is
// itself one bracketed group, which is the shape worth keeping tight.
func (p *printer) hugged(open, close int) bool {
	elems := p.elements(open, close)
	if len(elems) != 1 {
		return false
	}
	el := elems[0]
	return isOpener(p.it[el.lo].kind) && p.match[el.lo] == el.hi-1 && el.hi-1 > el.lo+1
}

// loosestOps returns the positions of the lowest-precedence binary operators
// sitting at the top level of [lo,hi) — the ones to break at. An operator that
// is prefix rather than infix has no left operand to break away from, and a `<`
// or `>` that turned out to be a type argument list is not an operator at all.
func (p *printer) loosestOps(lo, hi int) []int {
	best := 0
	depth := 0
	for k := lo; k < hi; k++ {
		switch {
		case isOpener(p.it[k].kind):
			depth++
		case isCloser(p.it[k].kind):
			depth--
		case depth == 0 && k > lo:
			if prec := p.opPrecedence(k); prec > 0 && (best == 0 || prec < best) {
				best = prec
			}
		}
	}
	if best == 0 {
		return nil
	}

	var ops []int
	depth = 0
	for k := lo; k < hi; k++ {
		switch {
		case isOpener(p.it[k].kind):
			depth++
		case isCloser(p.it[k].kind):
			depth--
		case depth == 0 && k > lo && p.opPrecedence(k) == best:
			// Nothing follows the last operator in the range, so breaking after
			// it would leave a dangling line.
			if p.codeAfter(k, hi) {
				ops = append(ops, k)
			}
		}
	}
	return ops
}

func (p *printer) codeAfter(k, hi int) bool {
	for j := k + 1; j < hi; j++ {
		if p.it[j].kind != token.TokenComment {
			return true
		}
	}
	return false
}

func (p *printer) opPrecedence(i int) int {
	if p.generic[i] {
		return 0
	}
	if isPrefixOperator(p, i) {
		return 0
	}
	switch p.it[i].kind {
	case token.TokenAssign, token.TokenPlusAssign, token.TokenMinusAssign,
		token.TokenStarAssign, token.TokenSlashAssign, token.TokenModAssign,
		token.TokenFatArrow:
		return 1
	case token.TokenOr:
		return 2
	case token.TokenAnd:
		return 3
	case token.TokenEq, token.TokenNeq, token.TokenStrictEq, token.TokenStrictNeq,
		token.TokenLt, token.TokenGt, token.TokenLte, token.TokenGte:
		return 4
	case token.TokenPlus, token.TokenMinus:
		return 5
	case token.TokenStar, token.TokenSlash, token.TokenPercent:
		return 6
	}
	return 0
}

type span struct{ lo, hi int }

// elements splits a group at its own commas, which is the only place a group is
// allowed to be broken up. The commas are not returned: expand reprints them,
// and never a trailing one — the parser reads a comma as "another element
// follows" and rejects a dangling one.
func (p *printer) elements(open, close int) []span {
	var out []span
	depth := 0
	start := open + 1
	for k := open + 1; k < close; k++ {
		switch {
		case isOpener(p.it[k].kind):
			depth++
		case isCloser(p.it[k].kind):
			depth--
		case p.it[k].kind == token.TokenComma && depth == 0:
			out = append(out, span{start, k})
			start = k + 1
		}
	}
	if start < close {
		out = append(out, span{start, close})
	}
	return out
}

func (p *printer) expand(open, close int, elems []span) {
	base := p.lineIndent
	inner := base + IndentWidth

	p.write(p.it[open].text)
	for idx, el := range elems {
		// A comment written on the same line as the comma before it belongs to
		// the element before the comma, so it stays on that line.
		for el.lo < el.hi && p.it[el.lo].kind == token.TokenComment && p.it[el.lo].line == p.it[el.lo-1].end {
			p.write(" " + p.it[el.lo].text)
			el.lo++
		}
		if el.lo >= el.hi {
			continue
		}
		p.newline(inner)
		// The comma that follows every element but the last shares the line
		// with it, so the element has one column less to work with. Measuring
		// without it puts an element that fits exactly one column over.
		last := idx == len(elems)-1
		if !last {
			p.reserve++
		}
		p.segment(el.lo, el.hi, inner)
		if !last {
			p.reserve--
			p.write(",")
		}
	}
	p.newline(base)
	p.write(p.it[close].text)
}

// cursor is the column the next character will land in. It is not p.col until
// the line's indentation has actually been written, and every fit test is made
// before that — an indent left out of the measurement is a line measured four
// or eight columns short of where it ends up.
func (p *printer) cursor() int {
	if p.needIndent {
		return p.lineIndent
	}
	return p.col
}

func (p *printer) write(s string) {
	if p.needIndent {
		p.buf.WriteString(strings.Repeat(" ", p.lineIndent))
		p.col = p.lineIndent
		p.needIndent = false
	}
	p.buf.WriteString(s)
	p.col += len([]rune(s))
}

func (p *printer) newline(indent int) {
	p.buf.WriteByte('\n')
	p.lineIndent = indent
	p.needIndent = true
	p.col = 0
}
