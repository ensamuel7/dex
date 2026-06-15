package lexer

import (
	"fmt"
	"unicode"

	"github.com/ensamuel7/dex/token"
)

type Lexer struct {
	source []rune
	pos    int
	line   int
	col    int
}

func New(source string) *Lexer {
	return &Lexer{
		source: []rune(source),
		pos:    0,
		line:   1,
		col:    1,
	}
}

func (l *Lexer) Tokenize() ([]token.Token, error) {
	var tokens []token.Token

	for {
		l.skipWhitespaceAndComments()

		if l.pos >= len(l.source) {
			tokens = append(tokens, token.Token{Kind: token.TokenEOF, Value: "", Line: l.line, Col: l.col})
			break
		}

		ch := l.source[l.pos]
		startLine := l.line
		startCol := l.col

		// Three-character tokens
		if l.pos+2 < len(l.source) {
			three := string(l.source[l.pos : l.pos+3])
			switch three {
			case "===":
				tokens = append(tokens, token.Token{Kind: token.TokenStrictEq, Value: "===", Line: startLine, Col: startCol})
				l.advance()
				l.advance()
				l.advance()
				continue
			case "!==":
				tokens = append(tokens, token.Token{Kind: token.TokenStrictNeq, Value: "!==", Line: startLine, Col: startCol})
				l.advance()
				l.advance()
				l.advance()
				continue
			}
		}

		// Two-character tokens
		if l.pos+1 < len(l.source) {
			two := string(l.source[l.pos : l.pos+2])
			switch two {
			case "++":
				tokens = append(tokens, token.Token{Kind: token.TokenPlusPlus, Value: "++", Line: startLine, Col: startCol})
				l.advance()
				l.advance()
				continue
			case "--":
				tokens = append(tokens, token.Token{Kind: token.TokenMinusMinus, Value: "--", Line: startLine, Col: startCol})
				l.advance()
				l.advance()
				continue
			case "+=":
				tokens = append(tokens, token.Token{Kind: token.TokenPlusAssign, Value: "+=", Line: startLine, Col: startCol})
				l.advance()
				l.advance()
				continue
			case "-=":
				tokens = append(tokens, token.Token{Kind: token.TokenMinusAssign, Value: "-=", Line: startLine, Col: startCol})
				l.advance()
				l.advance()
				continue
			case "==":
				tokens = append(tokens, token.Token{Kind: token.TokenEq, Value: "==", Line: startLine, Col: startCol})
				l.advance()
				l.advance()
				continue
			case "!=":
				tokens = append(tokens, token.Token{Kind: token.TokenNeq, Value: "!=", Line: startLine, Col: startCol})
				l.advance()
				l.advance()
				continue
			case "<=":
				tokens = append(tokens, token.Token{Kind: token.TokenLte, Value: "<=", Line: startLine, Col: startCol})
				l.advance()
				l.advance()
				continue
			case ">=":
				tokens = append(tokens, token.Token{Kind: token.TokenGte, Value: ">=", Line: startLine, Col: startCol})
				l.advance()
				l.advance()
				continue
			case "&&":
				tokens = append(tokens, token.Token{Kind: token.TokenAnd, Value: "&&", Line: startLine, Col: startCol})
				l.advance()
				l.advance()
				continue
			case "||":
				tokens = append(tokens, token.Token{Kind: token.TokenOr, Value: "||", Line: startLine, Col: startCol})
				l.advance()
				l.advance()
				continue
			}
		}

		// Single-character tokens
		switch ch {
		case '+':
			tokens = append(tokens, token.Token{Kind: token.TokenPlus, Value: "+", Line: startLine, Col: startCol})
			l.advance()
			continue
		case '-':
			tokens = append(tokens, token.Token{Kind: token.TokenMinus, Value: "-", Line: startLine, Col: startCol})
			l.advance()
			continue
		case '*':
			tokens = append(tokens, token.Token{Kind: token.TokenStar, Value: "*", Line: startLine, Col: startCol})
			l.advance()
			continue
		case '/':
			tokens = append(tokens, token.Token{Kind: token.TokenSlash, Value: "/", Line: startLine, Col: startCol})
			l.advance()
			continue
		case '=':
			tokens = append(tokens, token.Token{Kind: token.TokenAssign, Value: "=", Line: startLine, Col: startCol})
			l.advance()
			continue
		case '!':
			tokens = append(tokens, token.Token{Kind: token.TokenBang, Value: "!", Line: startLine, Col: startCol})
			l.advance()
			continue
		case '<':
			tokens = append(tokens, token.Token{Kind: token.TokenLt, Value: "<", Line: startLine, Col: startCol})
			l.advance()
			continue
		case '>':
			tokens = append(tokens, token.Token{Kind: token.TokenGt, Value: ">", Line: startLine, Col: startCol})
			l.advance()
			continue
		case '(':
			tokens = append(tokens, token.Token{Kind: token.TokenLParen, Value: "(", Line: startLine, Col: startCol})
			l.advance()
			continue
		case ')':
			tokens = append(tokens, token.Token{Kind: token.TokenRParen, Value: ")", Line: startLine, Col: startCol})
			l.advance()
			continue
		case '{':
			tokens = append(tokens, token.Token{Kind: token.TokenLBrace, Value: "{", Line: startLine, Col: startCol})
			l.advance()
			continue
		case '}':
			tokens = append(tokens, token.Token{Kind: token.TokenRBrace, Value: "}", Line: startLine, Col: startCol})
			l.advance()
			continue
		case ':':
			tokens = append(tokens, token.Token{Kind: token.TokenColon, Value: ":", Line: startLine, Col: startCol})
			l.advance()
			continue
		case ',':
			tokens = append(tokens, token.Token{Kind: token.TokenComma, Value: ",", Line: startLine, Col: startCol})
			l.advance()
			continue
		case '.':
			tokens = append(tokens, token.Token{Kind: token.TokenDot, Value: ".", Line: startLine, Col: startCol})
			l.advance()
			continue
		case '[':
			tokens = append(tokens, token.Token{Kind: token.TokenLBracket, Value: "[", Line: startLine, Col: startCol})
			l.advance()
			continue
		case ']':
			tokens = append(tokens, token.Token{Kind: token.TokenRBracket, Value: "]", Line: startLine, Col: startCol})
			l.advance()
			continue
		case ';':
			tokens = append(tokens, token.Token{Kind: token.TokenSemicolon, Value: ";", Line: startLine, Col: startCol})
			l.advance()
			continue
		}

		// String literals
		if ch == '"' {
			l.advance() // consume opening quote
			var str []rune
			for l.pos < len(l.source) && l.source[l.pos] != '"' {
				if l.source[l.pos] == '\\' && l.pos+1 < len(l.source) {
					l.advance()
					switch l.source[l.pos] {
					case '"':
						str = append(str, '"')
					case '\\':
						str = append(str, '\\')
					case 'n':
						str = append(str, '\n')
					case 't':
						str = append(str, '\t')
					default:
						return nil, fmt.Errorf("%d:%d: unknown escape sequence '\\%c'", l.line, l.col, l.source[l.pos])
					}
					l.advance()
				} else {
					str = append(str, l.source[l.pos])
					l.advance()
				}
			}
			if l.pos >= len(l.source) {
				return nil, fmt.Errorf("%d:%d: unterminated string literal", startLine, startCol)
			}
			l.advance() // consume closing quote
			tokens = append(tokens, token.Token{Kind: token.TokenString, Value: string(str), Line: startLine, Col: startCol})
			continue
		}

		// Percent operator
		if ch == '%' {
			tokens = append(tokens, token.Token{Kind: token.TokenPercent, Value: "%", Line: startLine, Col: startCol})
			l.advance()
			continue
		}

		// Numeric literals (int or float)
		if unicode.IsDigit(ch) {
			start := l.pos
			for l.pos < len(l.source) && unicode.IsDigit(l.source[l.pos]) {
				l.advance()
			}
			// Check for decimal point (float literal)
			if l.pos < len(l.source) && l.source[l.pos] == '.' && l.pos+1 < len(l.source) && unicode.IsDigit(l.source[l.pos+1]) {
				l.advance() // consume '.'
				for l.pos < len(l.source) && unicode.IsDigit(l.source[l.pos]) {
					l.advance()
				}
				value := string(l.source[start:l.pos])
				tokens = append(tokens, token.Token{Kind: token.TokenFloat, Value: value, Line: startLine, Col: startCol})
				continue
			}
			value := string(l.source[start:l.pos])
			tokens = append(tokens, token.Token{Kind: token.TokenInt, Value: value, Line: startLine, Col: startCol})
			continue
		}

		// Identifiers and keywords
		if unicode.IsLetter(ch) || ch == '_' {
			start := l.pos
			for l.pos < len(l.source) && (unicode.IsLetter(l.source[l.pos]) || unicode.IsDigit(l.source[l.pos]) || l.source[l.pos] == '_') {
				l.advance()
			}
			value := string(l.source[start:l.pos])
			kind, isKeyword := token.Keywords[value]
			if !isKeyword {
				kind = token.TokenIdent
			}
			tokens = append(tokens, token.Token{Kind: kind, Value: value, Line: startLine, Col: startCol})
			continue
		}

		return nil, fmt.Errorf("%d:%d: unexpected character '%c'", l.line, l.col, ch)
	}

	return tokens, nil
}

func (l *Lexer) advance() {
	if l.pos < len(l.source) {
		if l.source[l.pos] == '\n' {
			l.line++
			l.col = 1
		} else {
			l.col++
		}
		l.pos++
	}
}

func (l *Lexer) skipWhitespaceAndComments() {
	for l.pos < len(l.source) {
		ch := l.source[l.pos]

		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			l.advance()
			continue
		}

		// Line comments
		if ch == '/' && l.pos+1 < len(l.source) && l.source[l.pos+1] == '/' {
			for l.pos < len(l.source) && l.source[l.pos] != '\n' {
				l.advance()
			}
			continue
		}

		break
	}
}
