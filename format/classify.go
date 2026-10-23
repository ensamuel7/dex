package format

import "github.com/ensamuel7/dex/token"

// classify fills in the four tables the printer reads. It is one forward pass,
// which is what lets a decision use the ones already made: a brace is resolved
// against the statement it appears in, and the statement it appears in is
// bounded by the braces resolved before it.
func (p *printer) classify() {
	p.pairBrackets()
	p.markGenerics()

	seg := 0
	// The brackets open at this token, innermost last. A comment inside a
	// literal or an argument list is part of the statement around it: if it
	// ended the segment, the halves either side would each be printed on their
	// own, flat, and `P {` / `a: 1, // why` / `b: 2 }` is what came out.
	var open []int
	for i := range p.it {
		if i > 0 && p.startsSegment(i) && !p.commentInGroup(i, open) {
			p.boundary[i] = true
			seg = i
		}
		p.segStart[i] = seg

		if p.it[i].kind == token.TokenLBrace {
			p.block[i] = p.isBlockBrace(i, seg)
			if j := p.match[i]; j >= 0 {
				p.block[j] = p.block[i]
			}
		}
		switch {
		case isOpener(p.it[i].kind):
			open = append(open, i)
		case isCloser(p.it[i].kind) && len(open) > 0:
			open = open[:len(open)-1]
		}
	}
}

// commentInGroup reports whether the break before token i is one a comment
// asked for, inside a bracket that is not a block. A block's statements are
// segments of their own, so a lambda body inside a call keeps the old rule.
func (p *printer) commentInGroup(i int, open []int) bool {
	if p.it[i].kind != token.TokenComment && p.it[i-1].kind != token.TokenComment {
		return false
	}
	if len(open) == 0 {
		return false
	}
	top := open[len(open)-1]
	return !(p.it[top].kind == token.TokenLBrace && p.block[top])
}

func (p *printer) pairBrackets() {
	var stack []int
	for i, t := range p.it {
		switch {
		case isOpener(t.kind):
			stack = append(stack, i)
		case isCloser(t.kind):
			if len(stack) == 0 {
				continue
			}
			j := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			p.match[i], p.match[j] = j, i
		}
	}
}

// markGenerics finds the `<` ... `>` pairs that are a type argument list. The
// test is deliberately narrow — the contents must all be type tokens and the
// token after the closing `>` must be one that can follow a type — because the
// alternative reading is a comparison, and `a < b` must not be printed as if it
// were one.
func (p *printer) markGenerics() {
	for i, t := range p.it {
		if t.kind != token.TokenLt || i == 0 {
			continue
		}
		if prev := p.it[i-1].kind; prev != token.TokenIdent && !isTypeKeyword(prev) {
			continue
		}

		depth := 1
		j := i + 1
		for ; j < len(p.it); j++ {
			k := p.it[j].kind
			if k == token.TokenLt {
				depth++
				continue
			}
			if k == token.TokenGt {
				depth--
				if depth == 0 {
					break
				}
				continue
			}
			if !isTypeToken(k) {
				break
			}
		}
		if j >= len(p.it) || depth != 0 || j == i+1 {
			continue
		}
		if j+1 < len(p.it) && !followsType(p.it[j+1].kind) {
			continue
		}

		p.generic[i], p.generic[j] = true, true
	}
}

// startsSegment answers where one line's worth of tokens ends and the next
// begins. The forced cases are structural: a block brace and a comment each own
// their line. The judgement call is an author's line break, which starts a new
// segment only when the token before it could end a statement and the token
// after it could begin one — otherwise the break is a continuation, and whether
// it survives depends on whether the line fits.
func (p *printer) startsSegment(i int) bool {
	prev, cur := p.it[i-1], p.it[i]

	if prev.kind == token.TokenComment {
		return true
	}
	if cur.kind == token.TokenComment {
		return cur.line > prev.line
	}

	if prev.kind == token.TokenLBrace && p.block[i-1] {
		// An empty block stays as `{}`.
		return p.match[i-1] != i
	}
	if cur.kind == token.TokenRBrace && p.block[i] {
		return true
	}
	if prev.kind == token.TokenRBrace && p.block[i-1] {
		// `})`, `}.map(`, `} else {`: a closing brace keeps what hangs off it.
		switch cur.kind {
		case token.TokenRParen, token.TokenRBracket, token.TokenComma, token.TokenDot,
			token.TokenSemicolon, token.TokenElse, token.TokenCatch, token.TokenFinally:
			return false
		}
		return true
	}

	return cur.line > prev.line && canEnd(prev.kind) && canStart(cur.kind)
}

// isBlockBrace separates a brace that opens a body from one that opens a struct,
// object or map literal. Both are spelled `{` after a name — `struct Foo {` and
// `Foo { x: 1 }` — so the answer comes from the statement around it rather than
// from the token before it.
//
// Getting this wrong is a layout bug and never a correctness one: the lexer
// discards newlines, so the parser cannot tell where the formatter put them.
func (p *printer) isBlockBrace(i, seg int) bool {
	switch p.it[i-1].kind {
	case token.TokenRParen, token.TokenElse, token.TokenTry, token.TokenFinally,
		token.TokenFatArrow, token.TokenDefer, token.TokenSpawn:
		return true
	}
	if p.it[i-1].kind == token.TokenRBrace && p.block[i-1] {
		return true
	}

	// `case 1: {` and `default: {`: the brace after a switch label opens a body,
	// and a colon is otherwise what separates a literal's key from its value.
	if p.it[i-1].kind == token.TokenColon {
		switch p.it[seg].kind {
		case token.TokenCase, token.TokenDefault:
			return true
		}
	}

	// A declaration header: the brace closes a signature that began with fn,
	// struct, enum or interface at the statement's own bracket depth. This is
	// what makes a lambda's body a block in `let f = fn(x: int): int { ... }`,
	// where the token before the brace is a return type.
	//
	// The keyword is looked for at the brace's own depth rather than the
	// statement's, so a lambda passed as an argument — `g(fn(): void {` — has a
	// body too. At depth 0 only, its statements were laid out as a literal's
	// elements and joined onto one line.
	depth := 0
	depths := make([]int, i-seg)
	for k := seg; k < i; k++ {
		depths[k-seg] = depth
		switch {
		case isOpener(p.it[k].kind):
			depth++
		case isCloser(p.it[k].kind):
			depth--
		}
	}
	introducer := -1
	for k := seg; k < i; k++ {
		if depths[k-seg] == depth && isDeclKeyword(p.it[k].kind) {
			introducer = k
		}
	}
	if introducer < 0 {
		return false
	}
	for k := introducer + 1; k < i; k++ {
		if isOpener(p.it[k].kind) {
			k = p.match[k]
			if k < 0 {
				return false
			}
			continue
		}
		if !isSignatureToken(p.it[k].kind) {
			return false
		}
	}
	return true
}

// space is the spacing rule between two adjacent tokens on one line. It is
// written as a sequence of exceptions ending in "a space", because that is what
// the code reads like: tight brackets, tight separators, tight prefixes, and
// everything else spaced.
func (p *printer) space(i int) bool {
	prev, cur := p.it[i-1], p.it[i]
	pk, ck := prev.kind, cur.kind

	if ck == token.TokenComment || pk == token.TokenComment {
		return true
	}

	// A generic's angle brackets bind tight on both sides.
	if p.generic[i] {
		return false
	}
	if p.generic[i-1] && (pk == token.TokenLt || ck == token.TokenGt) {
		return false
	}
	if p.generic[i-1] && pk == token.TokenLt {
		return false
	}

	// What binds tightest is the token on the left: nothing follows an opening
	// bracket, a dot or a prefix operator with a space between it, whatever
	// comes next.
	switch pk {
	case token.TokenLParen, token.TokenLBracket, token.TokenDot, token.TokenBang,
		token.TokenAmpersand:
		return false
	}

	switch ck {
	case token.TokenRParen, token.TokenRBracket, token.TokenComma, token.TokenColon,
		token.TokenSemicolon, token.TokenDot, token.TokenQuestion,
		token.TokenPlusPlus, token.TokenMinusMinus:
		return false
	case token.TokenRBrace:
		// `{}` and `{ x: 1 }`.
		return p.match[i] != i-1
	case token.TokenLParen, token.TokenLBracket:
		return !p.callsOrIndexes(i - 1)
	}

	// A literal's interior is spaced; a block's brace is followed by a newline
	// and never consulted here.
	if pk == token.TokenLBrace {
		return true
	}

	if isPrefixOperator(p, i-1) {
		return false
	}

	return true
}

// callsOrIndexes reports whether the token at i can be the thing a following
// `(` or `[` applies to. A control keyword cannot, which is what keeps
// `if (x)` spaced and `f(x)` tight.
func (p *printer) callsOrIndexes(i int) bool {
	switch k := p.it[i].kind; {
	case k == token.TokenIdent, k == token.TokenRParen, k == token.TokenRBracket,
		k == token.TokenString, k == token.TokenRBrace,
		k == token.TokenFn, k == token.TokenFunction:
		// `fn(x: int)` is a lambda's parameter list, not a keyword followed by
		// a parenthesised expression.
		return true
	case k == token.TokenGt:
		return p.generic[i]
	case isTypeKeyword(k):
		return true
	}
	return false
}

// isPrefixOperator reports whether the operator at i is a prefix rather than an
// infix one, which is decided by what comes before it: nothing an operand can
// end with means there is no left operand.
func isPrefixOperator(p *printer, i int) bool {
	switch p.it[i].kind {
	case token.TokenMinus, token.TokenPlus, token.TokenBang, token.TokenAmpersand:
	default:
		return false
	}
	if i == 0 {
		return true
	}
	return !endsOperand(p.it[i-1].kind)
}

// endsOperand reports whether a token can be the end of a value, which is what
// decides whether the operator after it has a left operand. It is narrower than
// canEnd: `return` ends a statement but not a value, so the `-` in
// `return -x` is a prefix operator.
func endsOperand(k token.TokenKind) bool {
	switch k {
	case token.TokenIdent, token.TokenInt, token.TokenFloat, token.TokenString,
		token.TokenChar, token.TokenTrue, token.TokenFalse, token.TokenNull,
		token.TokenRParen, token.TokenRBracket, token.TokenRBrace,
		token.TokenPlusPlus, token.TokenMinusMinus, token.TokenMutex:
		return true
	}
	return false
}

func isOpener(k token.TokenKind) bool {
	return k == token.TokenLParen || k == token.TokenLBracket || k == token.TokenLBrace
}

func isCloser(k token.TokenKind) bool {
	return k == token.TokenRParen || k == token.TokenRBracket || k == token.TokenRBrace
}

func isDeclKeyword(k token.TokenKind) bool {
	switch k {
	case token.TokenFn, token.TokenFunction, token.TokenStruct, token.TokenEnum,
		token.TokenInterface, token.TokenExternal:
		return true
	}
	return false
}

// isSignatureToken is what may sit between a declaration's keyword and its
// opening brace once the bracket groups are skipped: a name, a return type, the
// punctuation that introduces one, and the throws effect.
func isSignatureToken(k token.TokenKind) bool {
	switch k {
	case token.TokenIdent, token.TokenColon, token.TokenComma, token.TokenDot,
		token.TokenQuestion, token.TokenAmpersand, token.TokenLt, token.TokenGt,
		token.TokenThrows, token.TokenWeak, token.TokenPublic, token.TokenPrivate:
		return true
	}
	return isTypeKeyword(k)
}

func isTypeKeyword(k token.TokenKind) bool {
	switch k {
	case token.TokenIntKw, token.TokenBool, token.TokenStringKw, token.TokenLong,
		token.TokenDouble, token.TokenVoid, token.TokenCharKw, token.TokenMap,
		token.TokenChan, token.TokenMutex:
		return true
	}
	return false
}

func isTypeToken(k token.TokenKind) bool {
	switch k {
	case token.TokenIdent, token.TokenLBracket, token.TokenRBracket, token.TokenComma,
		token.TokenQuestion, token.TokenAmpersand, token.TokenDot:
		return true
	}
	return isTypeKeyword(k)
}

// followsType is the set of tokens that may come after a type argument list,
// and so the last check that `<` ... `>` was one. `Box<int> {`, `f<int>(`,
// `: Box<int>,` and `= Box<int>` are all in it; an identifier is not, which is
// what rejects `f(a < b, c > d)`.
func followsType(k token.TokenKind) bool {
	switch k {
	case token.TokenLParen, token.TokenLBrace, token.TokenAssign, token.TokenRParen,
		token.TokenComma, token.TokenRBracket, token.TokenGt, token.TokenSemicolon,
		token.TokenLBracket:
		return true
	}
	return false
}

// canEnd reports whether a token can be the last one of a statement, and so
// whether a line break after it is a statement boundary rather than a
// continuation.
func canEnd(k token.TokenKind) bool {
	switch k {
	case token.TokenIdent, token.TokenInt, token.TokenFloat, token.TokenString,
		token.TokenChar, token.TokenTrue, token.TokenFalse, token.TokenNull,
		token.TokenRParen, token.TokenRBracket, token.TokenRBrace,
		token.TokenPlusPlus, token.TokenMinusMinus, token.TokenTag,
		token.TokenAnnotation, token.TokenBreak, token.TokenContinue,
		token.TokenReturn, token.TokenQuestion, token.TokenGt, token.TokenMutex:
		return true
	}
	return isTypeKeyword(k)
}

// canStart reports whether a token can be the first one of a statement. An
// operator, a separator and a closing bracket cannot, which is what makes a
// line beginning with `.`, `&&` or `+` a continuation of the line above.
func canStart(k token.TokenKind) bool {
	switch k {
	case token.TokenIdent, token.TokenLet, token.TokenConst, token.TokenReturn,
		token.TokenIf, token.TokenWhile, token.TokenFor, token.TokenForeach,
		token.TokenSwitch, token.TokenMatch, token.TokenTry, token.TokenThrow,
		token.TokenDefer, token.TokenSpawn, token.TokenBreak, token.TokenContinue,
		token.TokenFn, token.TokenFunction, token.TokenStruct, token.TokenEnum,
		token.TokenInterface, token.TokenImport, token.TokenPublic, token.TokenPrivate,
		token.TokenExternal, token.TokenAnnotation, token.TokenCase, token.TokenDefault,
		token.TokenWeak,
		token.TokenInt, token.TokenFloat, token.TokenString, token.TokenChar,
		token.TokenTrue, token.TokenFalse, token.TokenNull:
		return true
	}
	return isTypeKeyword(k)
}
