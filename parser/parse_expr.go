package parser

import (
	"strconv"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/token"
)

// Pratt-style precedence climbing

func precedence(kind token.TokenKind) int {
	switch kind {
	case token.TokenOr:
		return 1
	case token.TokenAnd:
		return 2
	case token.TokenEq, token.TokenNeq, token.TokenStrictEq, token.TokenStrictNeq:
		return 3
	case token.TokenLt, token.TokenGt, token.TokenLte, token.TokenGte:
		return 4
	case token.TokenPlus, token.TokenMinus:
		return 5
	case token.TokenStar, token.TokenSlash, token.TokenPercent:
		return 6
	default:
		return 0
	}
}

func tokenToBinOp(kind token.TokenKind) ast.BinOp {
	switch kind {
	case token.TokenPlus:
		return ast.BinAdd
	case token.TokenMinus:
		return ast.BinSub
	case token.TokenStar:
		return ast.BinMul
	case token.TokenSlash:
		return ast.BinDiv
	case token.TokenPercent:
		return ast.BinMod
	case token.TokenEq:
		return ast.BinEq
	case token.TokenNeq:
		return ast.BinNeq
	case token.TokenStrictEq:
		return ast.BinStrictEq
	case token.TokenStrictNeq:
		return ast.BinStrictNeq
	case token.TokenLt:
		return ast.BinLt
	case token.TokenGt:
		return ast.BinGt
	case token.TokenLte:
		return ast.BinLte
	case token.TokenGte:
		return ast.BinGte
	case token.TokenAnd:
		return ast.BinAnd
	case token.TokenOr:
		return ast.BinOr
	default:
		return -1
	}
}

func (p *Parser) parseExpr(minPrec int) (ast.Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}

	for {
		opPos := p.nodePos()
		op := p.current().Kind
		prec := precedence(op)
		if prec <= minPrec {
			break
		}

		p.advance()

		right, err := p.parseExpr(prec)
		if err != nil {
			return nil, err
		}

		left = &ast.BinaryExpr{
			Pos:   opPos,
			Op:    tokenToBinOp(op),
			Left:  left,
			Right: right,
		}
	}

	return left, nil
}

func (p *Parser) parseUnary() (ast.Expr, error) {
	if p.check(token.TokenMinus) {
		pos := p.nodePos()
		p.advance()
		operand, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &ast.UnaryExpr{Pos: pos, Op: ast.UnaryNeg, Operand: operand}, nil
	}

	if p.check(token.TokenBang) {
		pos := p.nodePos()
		p.advance()
		operand, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &ast.UnaryExpr{Pos: pos, Op: ast.UnaryNot, Operand: operand}, nil
	}

	return p.parsePrimary()
}


// parsePostfix consumes trailing index and field-access operators after a base
// expression, so chains like a.b[i], arr[i].field, and m[i][j].x parse fully
// instead of stopping at the first operator. A ".name(" sequence is left
// unconsumed: method calls on an arbitrary expression are not representable.
func (p *Parser) parsePostfix(base ast.Expr, pos ast.Pos) (ast.Expr, error) {
	for {
		if p.check(token.TokenLBracket) {
			p.advance() // consume '['
			idx, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			if err := p.expect(token.TokenRBracket); err != nil {
				return nil, err
			}
			base = &ast.IndexExpr{Pos: pos, Array: base, Index: idx}
			continue
		}
		if p.check(token.TokenDot) && p.pos+1 < len(p.tokens) && p.tokens[p.pos+1].Kind == token.TokenIdent {
			// A method call on an arbitrary expression, as in parsed[0].asInt():
			// the receiver travels on CallExpr.Recv.
			if p.pos+2 < len(p.tokens) && p.tokens[p.pos+2].Kind == token.TokenLParen {
				p.advance() // consume '.'
				name, err := p.expectIdent()
				if err != nil {
					return nil, err
				}
				p.advance() // consume '('
				var args []ast.Expr
				if !p.check(token.TokenRParen) {
					for {
						arg, err := p.parseExpr(0)
						if err != nil {
							return nil, err
						}
						args = append(args, arg)
						if !p.match(token.TokenComma) {
							break
						}
					}
				}
				if err := p.expect(token.TokenRParen); err != nil {
					return nil, err
				}
				base = &ast.CallExpr{Pos: pos, Name: name, Args: args, Recv: base}
				continue
			}
			p.advance() // consume '.'
			field, err := p.expectIdent()
			if err != nil {
				return nil, err
			}
			base = &ast.FieldAccessExpr{Pos: pos, Object: base, Field: field}
			continue
		}
		return base, nil
	}
}

func (p *Parser) parsePrimary() (ast.Expr, error) {
	tok := p.current()
	pos := p.nodePos()

	switch tok.Kind {
	case token.TokenInt:
		p.advance()
		val, err := strconv.Atoi(tok.Value)
		if err != nil {
			return nil, p.errorf("invalid integer literal: %s", tok.Value)
		}
		return &ast.IntLit{Pos: pos, Value: val}, nil

	case token.TokenFloat:
		p.advance()
		val, err := strconv.ParseFloat(tok.Value, 64)
		if err != nil {
			return nil, p.errorf("invalid float literal: %s", tok.Value)
		}
		return &ast.FloatLit{Pos: pos, Value: val}, nil

	case token.TokenString:
		p.advance()
		return &ast.StringLit{Pos: pos, Value: tok.Value}, nil

	case token.TokenInterpStringStart:
		return p.parseStringInterp()

	case token.TokenMatch:
		return p.parseMatchExpr()

	case token.TokenChar:
		p.advance()
		runes := []rune(tok.Value)
		if len(runes) != 1 {
			return nil, p.errorf("invalid char literal")
		}
		return &ast.CharLit{Pos: pos, Value: runes[0]}, nil

	case token.TokenTrue:
		p.advance()
		return &ast.BoolLit{Pos: pos, Value: true}, nil

	case token.TokenFalse:
		p.advance()
		return &ast.BoolLit{Pos: pos, Value: false}, nil

	case token.TokenNull:
		p.advance()
		return &ast.NullLit{Pos: pos}, nil

	case token.TokenMutex:
		// `mutex` in value position yields a fresh unlocked mutex,
		// as in `let mu: mutex = mutex`.
		p.advance()
		return &ast.MutexLit{Pos: pos}, nil

	case token.TokenLBrace:
		// Empty literal: {}. Whether this is an empty map or an empty JSON object
		// depends on the expected type, which only the checker knows.
		if p.pos+1 < len(p.tokens) && p.tokens[p.pos+1].Kind == token.TokenRBrace {
			p.advance() // consume '{'
			p.advance() // consume '}'
			return &ast.MapLitExpr{Pos: pos}, nil
		}
		return p.parseObjectLit()

	case token.TokenLBracket:
		// Array literal: [expr, expr, ...]
		p.advance() // consume '['
		var elems []ast.Expr
		if !p.check(token.TokenRBracket) {
			for {
				elem, err := p.parseExpr(0)
				if err != nil {
					return nil, err
				}
				elems = append(elems, elem)
				if !p.match(token.TokenComma) {
					break
				}
			}
		}
		if err := p.expect(token.TokenRBracket); err != nil {
			return nil, err
		}
		return &ast.ArrayLitExpr{Pos: pos, Elems: elems}, nil

	case token.TokenSpawn:
		return p.parseSpawnExpr()

	case token.TokenIdent:
		name := tok.Value

		// Check for receive(expr)
		if name == "receive" && p.pos+1 < len(p.tokens) && p.tokens[p.pos+1].Kind == token.TokenLParen {
			p.advance() // consume 'receive'
			p.advance() // consume '('
			source, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			if err := p.expect(token.TokenRParen); err != nil {
				return nil, err
			}
			return &ast.ReceiveExpr{Pos: pos, Source: source}, nil
		}

		// Check for channel(type)
		if name == "channel" && p.pos+1 < len(p.tokens) && p.tokens[p.pos+1].Kind == token.TokenLParen {
			p.advance() // consume 'channel'
			p.advance() // consume '('
			elemType, err := p.parseType()
			if err != nil {
				return nil, err
			}
			if err := p.expect(token.TokenRParen); err != nil {
				return nil, err
			}
			return &ast.ChannelExpr{Pos: pos, ElemType: elemType}, nil
		}

		// Check for enum access: EnumName.Variant (before advance)
		if p.enumNames[name] && p.pos+1 < len(p.tokens) && p.tokens[p.pos+1].Kind == token.TokenDot {
			p.advance() // consume enum name
			p.advance() // consume '.'
			variant, err := p.expectIdent()
			if err != nil {
				return nil, err
			}
			enumType, _ := ast.LookupEnumType(name)
			return &ast.EnumAccessExpr{Pos: pos, EnumName: name, Variant: variant, EnumType: enumType}, nil
		}

		p.advance()

		// Check for struct literal: StructName { field: value, ... }
		if p.structNames[name] && p.check(token.TokenLBrace) {
			p.advance() // consume '{'
			var fieldNames []string
			var fieldValues []ast.Expr
			if !p.check(token.TokenRBrace) {
				for {
					fn, err := p.expectIdent()
					if err != nil {
						return nil, err
					}
					if err := p.expect(token.TokenColon); err != nil {
						return nil, err
					}
					val, err := p.parseExpr(0)
					if err != nil {
						return nil, err
					}
					fieldNames = append(fieldNames, fn)
					fieldValues = append(fieldValues, val)
					if !p.match(token.TokenComma) {
						break
					}
				}
			}
			if err := p.expect(token.TokenRBrace); err != nil {
				return nil, err
			}
			return &ast.StructLitExpr{Pos: pos, Name: name, FieldNames: fieldNames, FieldValues: fieldValues}, nil
		}

		// Check for qualified call or field access: ident.something
		// Supports chaining: ident.field.field, ident.field.method(), etc.
		if p.check(token.TokenDot) {
			p.advance() // consume '.'
			memberName, err := p.expectIdent()
			if err != nil {
				return nil, err
			}

			// Build dotted chain: keep consuming dots to handle a.b.c.method()
			// qualifiedName accumulates the dotted prefix (e.g. "req.params").
			// members accumulates the same chain as individual field names, so a
			// chain that does not end in a call can be rebuilt as nested
			// FieldAccessExpr nodes instead of being collapsed to its first segment.
			members := []string{memberName}
			qualifiedName := name + "." + memberName
			for p.check(token.TokenDot) {
				p.advance() // consume '.'
				nextMember, err := p.expectIdent()
				if err != nil {
					return nil, err
				}
				if p.check(token.TokenLParen) {
					// This is a method call on the chain: qualifiedName.nextMember(...)
					p.advance() // consume '('
					args, err := p.parseArgs()
					if err != nil {
						return nil, err
					}
					if err := p.expect(token.TokenRParen); err != nil {
						return nil, err
					}
					callExpr := &ast.CallExpr{Pos: pos, Module: qualifiedName, Name: nextMember, Args: args}
					if p.lastArgNames != nil {
						callExpr.ArgNames = p.lastArgNames
						p.lastArgNames = nil
					}
					return callExpr, nil
				}
				// Not a call, continue building the chain
				members = append(members, nextMember)
				qualifiedName = qualifiedName + "." + nextMember
			}

			// Check for module-qualified struct literal: module.StructName { field: value, ... }
			if len(members) == 1 && p.structNames[memberName] && p.check(token.TokenLBrace) {
				p.advance() // consume '{'
				var fieldNames []string
				var fieldValues []ast.Expr
				if !p.check(token.TokenRBrace) {
					for {
						fn, err := p.expectIdent()
						if err != nil {
							return nil, err
						}
						if err := p.expect(token.TokenColon); err != nil {
							return nil, err
						}
						val, err := p.parseExpr(0)
						if err != nil {
							return nil, err
						}
						fieldNames = append(fieldNames, fn)
						fieldValues = append(fieldValues, val)
						if !p.match(token.TokenComma) {
							break
						}
					}
				}
				if err := p.expect(token.TokenRBrace); err != nil {
					return nil, err
				}
				return &ast.StructLitExpr{Pos: pos, Name: memberName, FieldNames: fieldNames, FieldValues: fieldValues}, nil
			}

			// If followed by '(', it's a method/qualified call
			if len(members) == 1 && p.check(token.TokenLParen) {
				p.advance() // consume '('
				args, err := p.parseArgs()
				if err != nil {
					return nil, err
				}
				if err := p.expect(token.TokenRParen); err != nil {
					return nil, err
				}
				callExpr := &ast.CallExpr{Pos: pos, Module: name, Name: memberName, Args: args}
				if p.lastArgNames != nil {
					callExpr.ArgNames = p.lastArgNames
					p.lastArgNames = nil
				}
				return callExpr, nil
			}
			// Otherwise it's a field access chain: a.b.c parses as ((a.b).c)
			var chain ast.Expr = &ast.Ident{Pos: pos, Name: name}
			for _, m := range members {
				chain = &ast.FieldAccessExpr{Pos: pos, Object: chain, Field: m}
			}
			return p.parsePostfix(chain, pos)
		}

		// Check for index or slice expression: ident[expr] or ident[start:end]
		if p.check(token.TokenLBracket) {
			p.advance() // consume '['
			// Check for [:end] form
			if p.check(token.TokenColon) {
				p.advance() // consume ':'
				var end ast.Expr
				if !p.check(token.TokenRBracket) {
					var err error
					end, err = p.parseExpr(0)
					if err != nil {
						return nil, err
					}
				}
				if err := p.expect(token.TokenRBracket); err != nil {
					return nil, err
				}
				return &ast.SliceExpr{Pos: pos, Array: &ast.Ident{Pos: pos, Name: name}, Start: nil, End: end}, nil
			}
			first, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			// Check for slice syntax: arr[start:end]
			if p.check(token.TokenColon) {
				p.advance() // consume ':'
				var end ast.Expr
				if !p.check(token.TokenRBracket) {
					end, err = p.parseExpr(0)
					if err != nil {
						return nil, err
					}
				}
				if err := p.expect(token.TokenRBracket); err != nil {
					return nil, err
				}
				return &ast.SliceExpr{Pos: pos, Array: &ast.Ident{Pos: pos, Name: name}, Start: first, End: end}, nil
			}
			if err := p.expect(token.TokenRBracket); err != nil {
				return nil, err
			}
			return p.parsePostfix(&ast.IndexExpr{Pos: pos, Array: &ast.Ident{Pos: pos, Name: name}, Index: first}, pos)
		}

		// Check for function call
		if p.check(token.TokenLParen) {
			p.advance()
			args, err := p.parseArgs()
			if err != nil {
				return nil, err
			}
			if err := p.expect(token.TokenRParen); err != nil {
				return nil, err
			}
			callExpr := &ast.CallExpr{Pos: pos, Name: name, Args: args}
			if p.lastArgNames != nil {
				callExpr.ArgNames = p.lastArgNames
				p.lastArgNames = nil
			}
			return callExpr, nil
		}

		return &ast.Ident{Pos: pos, Name: name}, nil

	case token.TokenFn, token.TokenFunction:
		// Lambda expression: fn(params): returnType { body }
		return p.parseLambdaExpr()

	case token.TokenLParen:
		p.advance()
		expr, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		if err := p.expect(token.TokenRParen); err != nil {
			return nil, err
		}
		return expr, nil

	default:
		return nil, p.errorf("unexpected token '%s'", tok.Value)
	}
}

func (p *Parser) parseSpawnExpr() (ast.Expr, error) {
	pos := p.nodePos()
	p.advance() // consume 'spawn'

	// spawn { body }
	if p.check(token.TokenLBrace) {
		p.advance() // consume '{'
		body, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		return &ast.SpawnExpr{Pos: pos, Body: body}, nil
	}

	// spawn fn(args) — parse a call expression
	callExpr, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	if _, ok := callExpr.(*ast.CallExpr); !ok {
		return nil, p.errorf("spawn expression must be followed by a block or function call")
	}
	return &ast.SpawnExpr{Pos: pos, Call: callExpr}, nil
}

func (p *Parser) parseArgs() ([]ast.Expr, error) {
	var args []ast.Expr
	var argNames []string
	hasNamedArgs := false

	if p.check(token.TokenRParen) {
		return args, nil
	}

	for {
		// Check for named argument: ident: expr
		// Lookahead: if we see TokenIdent followed by TokenColon, and the colon is NOT
		// part of a type annotation (which would need to be followed by a type keyword),
		// treat as named arg.
		if p.check(token.TokenIdent) && p.pos+1 < len(p.tokens) && p.tokens[p.pos+1].Kind == token.TokenColon {
			// Check it's not a struct literal field (those are inside {}) or a type annotation
			// Since we're inside parseArgs (inside parens), ident: is always a named arg
			name := p.current().Value
			p.advance() // consume ident
			p.advance() // consume ':'
			expr, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			args = append(args, expr)
			argNames = append(argNames, name)
			hasNamedArgs = true

			if !p.match(token.TokenComma) {
				break
			}
			continue
		}

		if hasNamedArgs {
			return nil, p.errorf("positional arguments cannot follow named arguments")
		}

		expr, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		args = append(args, expr)
		argNames = append(argNames, "")

		if !p.match(token.TokenComma) {
			break
		}
	}

	// Only set ArgNames on the CallExpr if there are named args
	// (the caller will need to handle this)
	if hasNamedArgs {
		// Store argNames in a package-level var that the caller can pick up
		p.lastArgNames = argNames
	} else {
		p.lastArgNames = nil
	}

	return args, nil
}

func (p *Parser) parseStringInterp() (ast.Expr, error) {
	pos := p.nodePos()
	var parts []ast.Expr

	// Current token is TokenInterpStringStart
	parts = append(parts, &ast.StringLit{Pos: pos, Value: p.current().Value})
	p.advance()

	// Parse first interpolated expression
	expr, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	parts = append(parts, expr)

	// Parse remaining mid/end parts
	for {
		if p.check(token.TokenInterpStringMid) {
			parts = append(parts, &ast.StringLit{Pos: p.nodePos(), Value: p.current().Value})
			p.advance()
			expr, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			parts = append(parts, expr)
		} else if p.check(token.TokenInterpStringEnd) {
			parts = append(parts, &ast.StringLit{Pos: p.nodePos(), Value: p.current().Value})
			p.advance()
			break
		} else {
			return nil, p.errorf("expected string interpolation continuation or end, got '%s'", p.current().Value)
		}
	}

	return &ast.StringInterpExpr{Pos: pos, Parts: parts}, nil
}

func (p *Parser) parseMatchExpr() (ast.Expr, error) {
	pos := p.nodePos()
	p.advance() // consume 'match'

	if err := p.expect(token.TokenLParen); err != nil {
		return nil, err
	}

	tag, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}

	if err := p.expect(token.TokenRParen); err != nil {
		return nil, err
	}

	if err := p.expect(token.TokenLBrace); err != nil {
		return nil, err
	}

	var arms []ast.MatchArm

	for !p.check(token.TokenRBrace) && !p.atEnd() {
		armPos := p.nodePos()

		// Check for wildcard: _
		if p.check(token.TokenIdent) && p.current().Value == "_" {
			p.advance() // consume '_'
			if err := p.expect(token.TokenFatArrow); err != nil {
				return nil, err
			}
			body, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			arms = append(arms, ast.MatchArm{Pos: armPos, IsWildcard: true, Body: body})
		} else {
			// Parse comma-separated pattern expressions
			var patterns []ast.Expr
			for {
				pat, err := p.parseExpr(0)
				if err != nil {
					return nil, err
				}
				patterns = append(patterns, pat)
				if !p.match(token.TokenComma) {
					break
				}
				// Check if next is => (end of patterns)
				if p.check(token.TokenFatArrow) {
					break
				}
			}

			if err := p.expect(token.TokenFatArrow); err != nil {
				return nil, err
			}

			body, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}

			arms = append(arms, ast.MatchArm{Pos: armPos, Patterns: patterns, Body: body})
		}
	}

	if err := p.expect(token.TokenRBrace); err != nil {
		return nil, err
	}

	return &ast.MatchExpr{Pos: pos, Tag: tag, Arms: arms}, nil
}

func (p *Parser) parseLambdaExpr() (ast.Expr, error) {
	pos := p.nodePos()
	p.advance() // consume 'fn' or 'function'

	if err := p.expect(token.TokenLParen); err != nil {
		return nil, err
	}

	params, err := p.parseParams()
	if err != nil {
		return nil, err
	}

	if err := p.expect(token.TokenRParen); err != nil {
		return nil, err
	}

	// Parse return type
	retType := ast.TypeVoid
	if p.match(token.TokenColon) {
		retType, err = p.parseType()
		if err != nil {
			return nil, err
		}
	}

	// Parse body
	if err := p.expect(token.TokenLBrace); err != nil {
		return nil, err
	}

	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}

	return &ast.LambdaExpr{Pos: pos, Params: params, ReturnType: retType, Body: body}, nil
}


// parseObjectLit parses a JSON object literal: { name: "Dex", "wire-key": 1 }.
// Keys may be bare identifiers, string literals, or keywords that happen to read
// like field names — `type` and `status` are ordinary keys on the wire even
// where they are reserved words in Dex.
func (p *Parser) parseObjectLit() (ast.Expr, error) {
	pos := p.nodePos()
	if err := p.expect(token.TokenLBrace); err != nil {
		return nil, err
	}
	var keys []string
	var values []ast.Expr
	seen := map[string]bool{}
	for !p.check(token.TokenRBrace) && !p.atEnd() {
		tok := p.current()
		var key string
		switch {
		case tok.Kind == token.TokenString:
			key = tok.Value
		case tok.Kind == token.TokenIdent || tok.Value != "":
			// Any single-token spelling is accepted as a key. JSON keys are not
			// Dex identifiers, so refusing reserved words here would block
			// perfectly ordinary payloads.
			key = tok.Value
		default:
			return nil, p.errorf("expected object key, got '%s'", tok.Value)
		}
		p.advance()
		if seen[key] {
			return nil, p.errorf("duplicate key '%s' in object literal", key)
		}
		seen[key] = true
		if err := p.expect(token.TokenColon); err != nil {
			return nil, p.errorf("expected ':' after object key '%s'", key)
		}
		value, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
		values = append(values, value)
		if !p.match(token.TokenComma) {
			break
		}
	}
	if err := p.expect(token.TokenRBrace); err != nil {
		return nil, p.errorf("expected '}' to close object literal")
	}
	return &ast.ObjectLitExpr{Pos: pos, Keys: keys, Values: values}, nil
}
