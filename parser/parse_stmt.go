package parser

import (
	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/token"
)

func (p *Parser) parseBlock() ([]ast.Stmt, error) {
	var stmts []ast.Stmt

	for !p.check(token.TokenRBrace) && !p.atEnd() {
		stmt, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, stmt)
		p.match(token.TokenSemicolon) // optional trailing semicolon
	}

	if err := p.expect(token.TokenRBrace); err != nil {
		return nil, err
	}

	return stmts, nil
}

func (p *Parser) parseStmt() (ast.Stmt, error) {
	// Collect annotations that may precede let/const
	if p.check(token.TokenAnnotation) {
		annotations := p.collectAnnotations()
		switch p.current().Kind {
		case token.TokenLet:
			stmt, err := p.parseLetStmt()
			if err != nil {
				return nil, err
			}
			stmt.(*ast.LetStmt).Annotations = annotations
			return stmt, nil
		case token.TokenConst:
			stmt, err := p.parseConstStmt()
			if err != nil {
				return nil, err
			}
			stmt.(*ast.LetStmt).Annotations = annotations
			return stmt, nil
		default:
			return nil, p.errorf("annotations are only allowed before 'let' or 'const' declarations")
		}
	}

	switch p.current().Kind {
	case token.TokenLet:
		return p.parseLetStmt()
	case token.TokenConst:
		return p.parseConstStmt()
	case token.TokenReturn:
		return p.parseReturnStmt()
	case token.TokenIf:
		return p.parseIfStmt()
	case token.TokenWhile:
		return p.parseWhileStmt()
	case token.TokenFor:
		return p.parseForStmt()
	case token.TokenForeach:
		return p.parseForeachStmt()
	case token.TokenBreak:
		pos := p.nodePos()
		p.advance()
		return &ast.BreakStmt{Pos: pos}, nil
	case token.TokenContinue:
		pos := p.nodePos()
		p.advance()
		return &ast.ContinueStmt{Pos: pos}, nil
	case token.TokenLBrace:
		pos := p.nodePos()
		p.advance()
		stmts, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		return &ast.BlockStmt{Pos: pos, Stmts: stmts}, nil
	case token.TokenSpawn:
		// spawn as statement (fire-and-forget): parse as ExprStmt wrapping SpawnExpr
		pos := p.nodePos()
		expr, err := p.parseSpawnExpr()
		if err != nil {
			return nil, err
		}
		return &ast.ExprStmt{Pos: pos, Expr: expr}, nil
	case token.TokenTry:
		return p.parseTryStmt()
	case token.TokenThrow:
		return p.parseThrowStmt()
	case token.TokenSwitch:
		return p.parseSwitchStmt()
	case token.TokenDefer:
		return p.parseDeferStmt()
	default:
		return p.parseExprStmt()
	}
}

func (p *Parser) parseLetStmt() (ast.Stmt, error) {
	pos := p.nodePos()
	p.advance() // consume 'let'

	// Check for destructuring: let { name1, name2 } = expr
	if p.check(token.TokenLBrace) {
		return p.parseDestructureLetStmt(pos, false)
	}

	name, err := p.expectIdent()
	if err != nil {
		return nil, err
	}

	// Type inference: if next token is '=' instead of ':', infer the type
	if p.check(token.TokenAssign) {
		p.advance() // consume '='
		value, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		return &ast.LetStmt{Pos: pos, Name: name, Type: ast.TypeInferred, Value: value}, nil
	}

	if err := p.expect(token.TokenColon); err != nil {
		return nil, err
	}

	typ, err := p.parseType()
	if err != nil {
		return nil, err
	}

	// Optional types without initializer default to null
	if ast.IsOptionalType(typ) && !p.check(token.TokenAssign) {
		return &ast.LetStmt{Pos: pos, Name: name, Type: typ, Value: &ast.NullLit{}}, nil
	}

	if err := p.expect(token.TokenAssign); err != nil {
		return nil, err
	}

	value, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}

	return &ast.LetStmt{Pos: pos, Name: name, Type: typ, Value: value}, nil
}

func (p *Parser) parseConstStmt() (ast.Stmt, error) {
	pos := p.nodePos()
	p.advance() // consume 'const'

	// Check for destructuring: const { name1, name2 } = expr
	if p.check(token.TokenLBrace) {
		return p.parseDestructureLetStmt(pos, true)
	}

	name, err := p.expectIdent()
	if err != nil {
		return nil, err
	}

	// Type inference: if next token is '=' instead of ':', infer the type
	if p.check(token.TokenAssign) {
		p.advance() // consume '='
		value, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		return &ast.LetStmt{Pos: pos, Name: name, Type: ast.TypeInferred, Value: value, IsConst: true}, nil
	}

	if err := p.expect(token.TokenColon); err != nil {
		return nil, err
	}

	typ, err := p.parseType()
	if err != nil {
		return nil, err
	}

	if err := p.expect(token.TokenAssign); err != nil {
		return nil, err
	}

	value, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}

	return &ast.LetStmt{Pos: pos, Name: name, Type: typ, Value: value, IsConst: true}, nil
}

func (p *Parser) parseReturnStmt() (ast.Stmt, error) {
	pos := p.nodePos()
	p.advance() // consume 'return'

	// Bare return (no expression) for void functions
	if p.check(token.TokenRBrace) || p.check(token.TokenEOF) || p.check(token.TokenSemicolon) {
		return &ast.ReturnStmt{Pos: pos, Value: nil}, nil
	}

	value, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}

	return &ast.ReturnStmt{Pos: pos, Value: value}, nil
}

func (p *Parser) parseIfStmt() (ast.Stmt, error) {
	pos := p.nodePos()
	p.advance() // consume 'if'

	if err := p.expect(token.TokenLParen); err != nil {
		return nil, err
	}

	cond, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}

	if err := p.expect(token.TokenRParen); err != nil {
		return nil, err
	}

	if err := p.expect(token.TokenLBrace); err != nil {
		return nil, err
	}

	then, err := p.parseBlock()
	if err != nil {
		return nil, err
	}

	var elseBlock []ast.Stmt
	if p.match(token.TokenElse) {
		if p.check(token.TokenIf) {
			// else if
			elseIfStmt, err := p.parseIfStmt()
			if err != nil {
				return nil, err
			}
			elseBlock = []ast.Stmt{elseIfStmt}
		} else {
			if err := p.expect(token.TokenLBrace); err != nil {
				return nil, err
			}
			elseBlock, err = p.parseBlock()
			if err != nil {
				return nil, err
			}
		}
	}

	return &ast.IfStmt{Pos: pos, Cond: cond, Then: then, Else: elseBlock}, nil
}

func (p *Parser) parseWhileStmt() (ast.Stmt, error) {
	pos := p.nodePos()
	p.advance() // consume 'while'

	if err := p.expect(token.TokenLParen); err != nil {
		return nil, err
	}

	cond, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}

	if err := p.expect(token.TokenRParen); err != nil {
		return nil, err
	}

	if err := p.expect(token.TokenLBrace); err != nil {
		return nil, err
	}

	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}

	return &ast.WhileStmt{Pos: pos, Cond: cond, Body: body}, nil
}

func (p *Parser) parseForStmt() (ast.Stmt, error) {
	pos := p.nodePos()
	p.advance() // consume 'for'

	if err := p.expect(token.TokenLParen); err != nil {
		return nil, err
	}

	// Parse init statement (let or assignment)
	init, err := p.parseForInit()
	if err != nil {
		return nil, err
	}

	if err := p.expect(token.TokenSemicolon); err != nil {
		return nil, err
	}

	// Parse condition expression
	cond, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}

	if err := p.expect(token.TokenSemicolon); err != nil {
		return nil, err
	}

	// Parse post statement (i++, i--, i += x, i -= x, i = x)
	post, err := p.parseForPost()
	if err != nil {
		return nil, err
	}

	if err := p.expect(token.TokenRParen); err != nil {
		return nil, err
	}

	if err := p.expect(token.TokenLBrace); err != nil {
		return nil, err
	}

	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}

	return &ast.ForStmt{Pos: pos, Init: init, Cond: cond, Post: post, Body: body}, nil
}

func (p *Parser) parseForInit() (ast.Stmt, error) {
	if p.check(token.TokenLet) {
		return p.parseLetStmt()
	}
	if p.check(token.TokenConst) {
		return p.parseConstStmt()
	}
	// Simple assignment: ident = expr
	pos := p.nodePos()
	name, err := p.expectIdent()
	if err != nil {
		return nil, err
	}
	if err := p.expect(token.TokenAssign); err != nil {
		return nil, err
	}
	value, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	return &ast.AssignStmt{Pos: pos, Name: name, Value: value}, nil
}

func (p *Parser) parseForPost() (ast.Stmt, error) {
	pos := p.nodePos()
	name, err := p.expectIdent()
	if err != nil {
		return nil, err
	}

	switch p.current().Kind {
	case token.TokenPlusPlus:
		p.advance()
		return &ast.IncrementStmt{Pos: pos, Name: name}, nil
	case token.TokenMinusMinus:
		p.advance()
		return &ast.DecrementStmt{Pos: pos, Name: name}, nil
	case token.TokenPlusAssign:
		p.advance()
		value, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		return &ast.CompoundAssignStmt{Pos: pos, Name: name, Op: ast.BinAdd, Value: value}, nil
	case token.TokenMinusAssign:
		p.advance()
		value, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		return &ast.CompoundAssignStmt{Pos: pos, Name: name, Op: ast.BinSub, Value: value}, nil
	case token.TokenStarAssign:
		p.advance()
		value, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		return &ast.CompoundAssignStmt{Pos: pos, Name: name, Op: ast.BinMul, Value: value}, nil
	case token.TokenSlashAssign:
		p.advance()
		value, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		return &ast.CompoundAssignStmt{Pos: pos, Name: name, Op: ast.BinDiv, Value: value}, nil
	case token.TokenModAssign:
		p.advance()
		value, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		return &ast.CompoundAssignStmt{Pos: pos, Name: name, Op: ast.BinMod, Value: value}, nil
	case token.TokenAssign:
		p.advance()
		value, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		return &ast.AssignStmt{Pos: pos, Name: name, Value: value}, nil
	default:
		return nil, p.errorf("expected '++', '--', '+=', '-=', or '=' in for loop post statement")
	}
}

func (p *Parser) parseForeachStmt() (ast.Stmt, error) {
	pos := p.nodePos()
	p.advance() // consume 'foreach'

	if err := p.expect(token.TokenLParen); err != nil {
		return nil, err
	}

	// Parse iterable expression
	iterable, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}

	// Expect 'as' keyword
	if err := p.expect(token.TokenAs); err != nil {
		return nil, err
	}

	// Parse first identifier
	firstName, err := p.expectIdent()
	if err != nil {
		return nil, err
	}

	var indexVar, valueVar string
	// If comma follows, first is index, second is value
	if p.match(token.TokenComma) {
		indexVar = firstName
		valueVar2, err := p.expectIdent()
		if err != nil {
			return nil, err
		}
		valueVar = valueVar2
	} else {
		// No comma: first is value, no index
		valueVar = firstName
	}

	if err := p.expect(token.TokenRParen); err != nil {
		return nil, err
	}

	if err := p.expect(token.TokenLBrace); err != nil {
		return nil, err
	}

	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}

	return &ast.ForeachStmt{
		Pos:      pos,
		Iterable: iterable,
		IndexVar: indexVar,
		ValueVar: valueVar,
		Body:     body,
	}, nil
}

func (p *Parser) parseTryStmt() (ast.Stmt, error) {
	pos := p.nodePos()
	p.advance() // consume 'try'

	if err := p.expect(token.TokenLBrace); err != nil {
		return nil, err
	}

	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}

	var catchVar string
	var catchBody []ast.Stmt
	var finallyBody []ast.Stmt

	// Optional catch clause
	if p.check(token.TokenCatch) {
		p.advance() // consume 'catch'

		if err := p.expect(token.TokenLParen); err != nil {
			return nil, err
		}

		name, err := p.expectIdent()
		if err != nil {
			return nil, err
		}
		catchVar = name

		if err := p.expect(token.TokenColon); err != nil {
			return nil, err
		}

		// Must be Exception type
		typeName, err := p.expectIdent()
		if err != nil {
			return nil, err
		}
		if typeName != "Exception" {
			return nil, p.errorf("catch clause must use Exception type, got '%s'", typeName)
		}

		if err := p.expect(token.TokenRParen); err != nil {
			return nil, err
		}

		if err := p.expect(token.TokenLBrace); err != nil {
			return nil, err
		}

		catchBody, err = p.parseBlock()
		if err != nil {
			return nil, err
		}
	}

	// Optional finally clause
	if p.check(token.TokenFinally) {
		p.advance() // consume 'finally'

		if err := p.expect(token.TokenLBrace); err != nil {
			return nil, err
		}

		finallyBody, err = p.parseBlock()
		if err != nil {
			return nil, err
		}
	}

	// Must have at least one of catch or finally
	if catchBody == nil && finallyBody == nil {
		return nil, p.errorf("try statement must have at least a catch or finally clause")
	}

	return &ast.TryCatchStmt{
		Pos:         pos,
		Body:        body,
		CatchVar:    catchVar,
		CatchBody:   catchBody,
		FinallyBody: finallyBody,
	}, nil
}

func (p *Parser) parseThrowStmt() (ast.Stmt, error) {
	pos := p.nodePos()
	p.advance() // consume 'throw'

	value, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}

	return &ast.ThrowStmt{Pos: pos, Value: value}, nil
}

func (p *Parser) parseSwitchStmt() (ast.Stmt, error) {
	pos := p.nodePos()
	p.advance() // consume 'switch'

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

	var cases []ast.SwitchCase
	var defaultBody []ast.Stmt

	for !p.check(token.TokenRBrace) && !p.atEnd() {
		if p.check(token.TokenCase) {
			casePos := p.nodePos()
			p.advance() // consume 'case'

			// Parse one or more comma-separated values
			var values []ast.Expr
			for {
				val, err := p.parseExpr(0)
				if err != nil {
					return nil, err
				}
				values = append(values, val)
				if !p.match(token.TokenComma) {
					break
				}
			}

			if err := p.expect(token.TokenColon); err != nil {
				return nil, err
			}

			if err := p.expect(token.TokenLBrace); err != nil {
				return nil, err
			}

			body, err := p.parseBlock()
			if err != nil {
				return nil, err
			}

			cases = append(cases, ast.SwitchCase{Pos: casePos, Values: values, Body: body})
		} else if p.check(token.TokenDefault) {
			p.advance() // consume 'default'

			if err := p.expect(token.TokenColon); err != nil {
				return nil, err
			}

			if err := p.expect(token.TokenLBrace); err != nil {
				return nil, err
			}

			defaultBody, err = p.parseBlock()
			if err != nil {
				return nil, err
			}
		} else {
			return nil, p.errorf("expected 'case' or 'default' in switch statement, got '%s'", p.current().Value)
		}
	}

	if err := p.expect(token.TokenRBrace); err != nil {
		return nil, err
	}

	return &ast.SwitchStmt{Pos: pos, Tag: tag, Cases: cases, Default: defaultBody}, nil
}

func (p *Parser) parseExprStmt() (ast.Stmt, error) {
	// Check for send(value) or send(channel, value)
	if p.check(token.TokenIdent) && p.current().Value == "send" &&
		p.pos+1 < len(p.tokens) && p.tokens[p.pos+1].Kind == token.TokenLParen {
		pos := p.nodePos()
		p.advance() // consume 'send'
		p.advance() // consume '('
		first, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		if p.match(token.TokenComma) {
			// send(channel, value)
			second, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			if err := p.expect(token.TokenRParen); err != nil {
				return nil, err
			}
			return &ast.SendStmt{Pos: pos, Target: first, Value: second}, nil
		}
		// send(value) — implicit target
		if err := p.expect(token.TokenRParen); err != nil {
			return nil, err
		}
		return &ast.SendStmt{Pos: pos, Target: nil, Value: first}, nil
	}

	// Check for close(expr) — parse as CallExpr
	// (handled naturally by the expression parser as a regular function call)

	// Check for field assignment: ident.field = expr
	if p.check(token.TokenIdent) &&
		p.pos+1 < len(p.tokens) && p.tokens[p.pos+1].Kind == token.TokenDot &&
		p.pos+2 < len(p.tokens) && p.tokens[p.pos+2].Kind == token.TokenIdent &&
		p.pos+3 < len(p.tokens) && p.tokens[p.pos+3].Kind == token.TokenAssign {
		pos := p.nodePos()
		objName := p.current().Value
		p.advance() // consume ident
		p.advance() // consume '.'
		fieldName := p.current().Value
		p.advance() // consume field name
		p.advance() // consume '='
		value, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		return &ast.FieldAssignStmt{
			Pos:    pos,
			Object: &ast.Ident{Pos: pos, Name: objName},
			Field:  fieldName,
			Value:  value,
		}, nil
	}

	// Check for index assignment: ident[expr] = expr
	if p.check(token.TokenIdent) && p.pos+1 < len(p.tokens) && p.tokens[p.pos+1].Kind == token.TokenLBracket {
		saved := p.pos
		pos := p.nodePos()
		name := p.current().Value
		p.advance() // consume ident
		p.advance() // consume '['
		index, err := p.parseExpr(0)
		if err != nil {
			// backtrack
			p.pos = saved
		} else if p.check(token.TokenRBracket) {
			p.advance() // consume ']'
			if p.check(token.TokenAssign) {
				p.advance() // consume '='
				value, err := p.parseExpr(0)
				if err != nil {
					return nil, err
				}
				return &ast.IndexAssignStmt{
					Pos:   pos,
					Array: &ast.Ident{Pos: pos, Name: name},
					Index: index,
					Value: value,
				}, nil
			}
			// Not an assignment — backtrack
			p.pos = saved
		} else {
			p.pos = saved
		}
	}

	// Check for increment/decrement/compound assign: ident++ / ident-- / ident += expr / ident -= expr
	if p.check(token.TokenIdent) && p.pos+1 < len(p.tokens) {
		pos := p.nodePos()
		switch p.tokens[p.pos+1].Kind {
		case token.TokenPlusPlus:
			name := p.current().Value
			p.advance() // consume ident
			p.advance() // consume '++'
			return &ast.IncrementStmt{Pos: pos, Name: name}, nil
		case token.TokenMinusMinus:
			name := p.current().Value
			p.advance() // consume ident
			p.advance() // consume '--'
			return &ast.DecrementStmt{Pos: pos, Name: name}, nil
		case token.TokenPlusAssign:
			name := p.current().Value
			p.advance() // consume ident
			p.advance() // consume '+='
			value, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			return &ast.CompoundAssignStmt{Pos: pos, Name: name, Op: ast.BinAdd, Value: value}, nil
		case token.TokenMinusAssign:
			name := p.current().Value
			p.advance() // consume ident
			p.advance() // consume '-='
			value, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			return &ast.CompoundAssignStmt{Pos: pos, Name: name, Op: ast.BinSub, Value: value}, nil
		case token.TokenStarAssign:
			name := p.current().Value
			p.advance() // consume ident
			p.advance() // consume '*='
			value, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			return &ast.CompoundAssignStmt{Pos: pos, Name: name, Op: ast.BinMul, Value: value}, nil
		case token.TokenSlashAssign:
			name := p.current().Value
			p.advance() // consume ident
			p.advance() // consume '/='
			value, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			return &ast.CompoundAssignStmt{Pos: pos, Name: name, Op: ast.BinDiv, Value: value}, nil
		case token.TokenModAssign:
			name := p.current().Value
			p.advance() // consume ident
			p.advance() // consume '%='
			value, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			return &ast.CompoundAssignStmt{Pos: pos, Name: name, Op: ast.BinMod, Value: value}, nil
		}
	}

	// Check for assignment: ident = expr
	if p.check(token.TokenIdent) && p.pos+1 < len(p.tokens) && p.tokens[p.pos+1].Kind == token.TokenAssign {
		pos := p.nodePos()
		name := p.current().Value
		p.advance() // consume ident
		p.advance() // consume '='
		value, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		return &ast.AssignStmt{Pos: pos, Name: name, Value: value}, nil
	}

	pos := p.nodePos()
	expr, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	return &ast.ExprStmt{Pos: pos, Expr: expr}, nil
}

func (p *Parser) parseDeferStmt() (ast.Stmt, error) {
	pos := p.nodePos()
	p.advance() // consume 'defer'

	expr, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}

	return &ast.DeferStmt{Pos: pos, Expr: expr}, nil
}

func (p *Parser) parseDestructureLetStmt(pos ast.Pos, isConst bool) (ast.Stmt, error) {
	p.advance() // consume '{'

	var names []string
	for !p.check(token.TokenRBrace) && !p.atEnd() {
		name, err := p.expectIdent()
		if err != nil {
			return nil, err
		}
		names = append(names, name)
		if !p.match(token.TokenComma) {
			break
		}
	}

	if err := p.expect(token.TokenRBrace); err != nil {
		return nil, err
	}

	if err := p.expect(token.TokenAssign); err != nil {
		return nil, err
	}

	value, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}

	return &ast.DestructureLetStmt{Pos: pos, Names: names, Value: value, IsConst: isConst}, nil
}
