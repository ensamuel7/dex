package parser

import (
	"fmt"
	"strconv"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/token"
)

type Parser struct {
	tokens      []token.Token
	pos         int
	structNames map[string]bool
	enumNames   map[string]bool
}

func New(tokens []token.Token) *Parser {
	return &Parser{tokens: tokens, pos: 0, structNames: make(map[string]bool), enumNames: make(map[string]bool)}
}

// AddStructName registers a struct name so the parser recognizes it as a type.
// Used for module-provided struct types (e.g. HttpResponse from the http module).
func (p *Parser) AddStructName(name string) {
	p.structNames[name] = true
}

func (p *Parser) Parse() (*ast.Program, error) {
	program := &ast.Program{}

	// Parse import declarations
	for p.check(token.TokenImport) {
		p.advance() // consume 'import'
		if !p.check(token.TokenString) {
			return nil, p.errorf("expected string after 'import'")
		}
		path := p.current().Value
		p.advance()
		var alias string
		if p.check(token.TokenAs) {
			p.advance() // consume 'as'
			if !p.check(token.TokenString) {
				return nil, p.errorf("expected string after 'as'")
			}
			alias = p.current().Value
			p.advance()
		}
		program.Imports = append(program.Imports, ast.Import{Path: path, Alias: alias})
	}

	// Parse struct and enum definitions (with optional annotations and access modifiers)
	for p.isStructDefAhead() || p.isEnumDefAhead() {
		if p.isEnumDefAhead() {
			ed, err := p.parseEnumDef()
			if err != nil {
				return nil, err
			}
			program.Enums = append(program.Enums, *ed)
		} else {
			annotations := p.collectAnnotations()
			sd, err := p.parseStructDef()
			if err != nil {
				return nil, err
			}
			_ = annotations // struct-level annotations reserved for future use
			program.Structs = append(program.Structs, *sd)
		}
	}

	for !p.atEnd() {
		annotations := p.collectAnnotations()
		fn, err := p.parseFunction()
		if err != nil {
			return nil, err
		}
		fn.Annotations = annotations
		program.Functions = append(program.Functions, *fn)
	}

	return program, nil
}

// isStructDefAhead returns true if the current tokens form a struct definition,
// possibly preceded by annotations and/or an access modifier (public/private).
func (p *Parser) isStructDefAhead() bool {
	// Skip past any annotations to peek at what follows
	i := p.pos
	for i < len(p.tokens) && p.tokens[i].Kind == token.TokenAnnotation {
		i++
	}
	if i < len(p.tokens) && p.tokens[i].Kind == token.TokenStruct {
		return true
	}
	if i < len(p.tokens) && (p.tokens[i].Kind == token.TokenPublic || p.tokens[i].Kind == token.TokenPrivate) &&
		i+1 < len(p.tokens) && p.tokens[i+1].Kind == token.TokenStruct {
		return true
	}
	return false
}

// isEnumDefAhead returns true if the current token is the 'enum' keyword.
func (p *Parser) isEnumDefAhead() bool {
	i := p.pos
	for i < len(p.tokens) && p.tokens[i].Kind == token.TokenAnnotation {
		i++
	}
	return i < len(p.tokens) && p.tokens[i].Kind == token.TokenEnum
}

func (p *Parser) parseEnumDef() (*ast.EnumDef, error) {
	// Skip any annotations (reserved for future use)
	p.collectAnnotations()

	p.advance() // consume 'enum'

	name, err := p.expectIdent()
	if err != nil {
		return nil, err
	}

	if err := p.expect(token.TokenLBrace); err != nil {
		return nil, err
	}

	var variants []string
	for !p.check(token.TokenRBrace) && !p.atEnd() {
		variant, err := p.expectIdent()
		if err != nil {
			return nil, err
		}
		variants = append(variants, variant)
	}

	if err := p.expect(token.TokenRBrace); err != nil {
		return nil, err
	}

	if len(variants) == 0 {
		return nil, p.errorf("enum '%s' must have at least one variant", name)
	}

	p.enumNames[name] = true
	ast.RegisterEnumType(ast.EnumDef{Name: name, Variants: variants})

	return &ast.EnumDef{Name: name, Variants: variants}, nil
}

func (p *Parser) parseStructDef() (*ast.StructDef, error) {
	// Consume optional access modifier (ignored on struct itself for now)
	if p.check(token.TokenPublic) || p.check(token.TokenPrivate) {
		p.advance()
	}

	p.advance() // consume 'struct'

	name, err := p.expectIdent()
	if err != nil {
		return nil, err
	}

	// Check for auto-constructor syntax: struct Foo(x: int, y: string) { ... }
	var constructorParams []ast.StructField
	if p.check(token.TokenLParen) {
		p.advance() // consume '('
		if !p.check(token.TokenRParen) {
			for {
				paramName, err := p.expectIdent()
				if err != nil {
					return nil, err
				}
				if err := p.expect(token.TokenColon); err != nil {
					return nil, err
				}
				paramType, err := p.parseType()
				if err != nil {
					return nil, err
				}
				constructorParams = append(constructorParams, ast.StructField{Name: paramName, Type: paramType})
				if !p.match(token.TokenComma) {
					break
				}
			}
		}
		if err := p.expect(token.TokenRParen); err != nil {
			return nil, err
		}
	}

	if err := p.expect(token.TokenLBrace); err != nil {
		return nil, err
	}

	var fields []ast.StructField
	var methods []ast.Function

	// Constructor params become fields
	for _, cp := range constructorParams {
		fields = append(fields, cp)
	}

	for !p.check(token.TokenRBrace) && !p.atEnd() {
		// Collect optional annotations
		annotations := p.collectAnnotations()

		// Check for optional access modifier
		isPrivate := false
		isPublic := false
		if p.check(token.TokenPrivate) {
			isPrivate = true
			p.advance()
		} else if p.check(token.TokenPublic) {
			isPublic = true
			p.advance()
		}

		// Check if this is a method (fn/function keyword)
		if p.check(token.TokenFn) || p.check(token.TokenFunction) {
			fn, err := p.parseFunction()
			if err != nil {
				return nil, err
			}
			fn.IsPrivate = isPrivate
			fn.Annotations = annotations
			methods = append(methods, *fn)
			_ = isPublic
			continue
		}

		// Otherwise it's a field
		fieldName, err := p.expectIdent()
		if err != nil {
			return nil, err
		}
		if err := p.expect(token.TokenColon); err != nil {
			return nil, err
		}
		fieldType, err := p.parseType()
		if err != nil {
			return nil, err
		}
		fields = append(fields, ast.StructField{Name: fieldName, Type: fieldType, IsPrivate: isPrivate, Annotations: annotations})
	}

	if err := p.expect(token.TokenRBrace); err != nil {
		return nil, err
	}

	p.structNames[name] = true
	// Register in the global struct registry so parseType can resolve the type ID
	ast.RegisterStructType(ast.StructDef{Name: name, Fields: fields, ConstructorParams: constructorParams})

	return &ast.StructDef{Name: name, Fields: fields, Methods: methods, ConstructorParams: constructorParams}, nil
}

func (p *Parser) parseFunction() (*ast.Function, error) {
	// Check for optional access modifier
	isPrivate := false
	if p.check(token.TokenPrivate) {
		isPrivate = true
		p.advance()
	} else if p.check(token.TokenPublic) {
		p.advance()
	}

	if !p.check(token.TokenFn) && !p.check(token.TokenFunction) {
		return nil, p.errorf("expected 'fn' or 'function'")
	}
	p.advance()

	name, err := p.expectIdent()
	if err != nil {
		return nil, err
	}

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

	if err := p.expect(token.TokenColon); err != nil {
		return nil, err
	}

	retType, err := p.parseType()
	if err != nil {
		return nil, err
	}

	if err := p.expect(token.TokenLBrace); err != nil {
		return nil, err
	}

	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}

	return &ast.Function{
		Name:       name,
		Params:     params,
		ReturnType: retType,
		Body:       body,
		IsPrivate:  isPrivate,
	}, nil
}

func (p *Parser) parseParams() ([]ast.Param, error) {
	var params []ast.Param

	if p.check(token.TokenRParen) {
		return params, nil
	}

	for {
		annotations := p.collectAnnotations()

		name, err := p.expectIdent()
		if err != nil {
			return nil, err
		}

		if err := p.expect(token.TokenColon); err != nil {
			return nil, err
		}

		typ, err := p.parseType()
		if err != nil {
			return nil, err
		}

		params = append(params, ast.Param{Name: name, Type: typ, Annotations: annotations})

		if !p.match(token.TokenComma) {
			break
		}
	}

	return params, nil
}

func (p *Parser) parseType() (ast.Type, error) {
	// Reject &&Type (double-ref) — tokenized as TokenAnd
	if p.check(token.TokenAnd) {
		return 0, p.errorf("double-reference types (&&) are not allowed")
	}
	// Check for &Type reference prefix
	if p.check(token.TokenAmpersand) {
		p.advance() // consume '&'
		// Reject &&Type (double-ref) in case tokenizer split them
		if p.check(token.TokenAmpersand) {
			return 0, p.errorf("double-reference types (&&) are not allowed")
		}
		// Only struct types can be referenced
		if !p.check(token.TokenIdent) {
			return 0, p.errorf("reference types (&) are only allowed on struct types")
		}
		name := p.current().Value
		if !p.structNames[name] {
			return 0, p.errorf("reference types (&) are only allowed on struct types, got '%s'", name)
		}
		p.advance()
		t, ok := ast.LookupStructType(name)
		if !ok {
			return 0, p.errorf("unknown struct type '%s'", name)
		}
		return ast.RefTypeOf(t), nil
	}

	tok := p.current()
	var base ast.Type
	switch tok.Kind {
	case token.TokenIntKw:
		p.advance()
		base = ast.TypeInt
	case token.TokenBool:
		p.advance()
		base = ast.TypeBool
	case token.TokenStringKw:
		p.advance()
		base = ast.TypeString
	case token.TokenLong:
		p.advance()
		base = ast.TypeLong
	case token.TokenDouble:
		p.advance()
		base = ast.TypeDouble
	case token.TokenCharKw:
		p.advance()
		base = ast.TypeChar
	case token.TokenVoid:
		p.advance()
		return ast.TypeVoid, nil
	case token.TokenChan:
		p.advance() // consume 'chan'
		elemType, err := p.parseType()
		if err != nil {
			return 0, err
		}
		return ast.ChanTypeOf(elemType), nil
	case token.TokenWeak:
		p.advance() // consume 'weak'
		if err := p.expect(token.TokenLt); err != nil {
			return 0, p.errorf("expected '<' after 'weak'")
		}
		innerType, err := p.parseType()
		if err != nil {
			return 0, err
		}
		if err := p.expect(token.TokenGt); err != nil {
			return 0, p.errorf("expected '>' after weak inner type")
		}
		return ast.WeakTypeOf(innerType), nil
	case token.TokenFn, token.TokenFunction:
		p.advance() // consume 'fn' or 'function'
		if err := p.expect(token.TokenLParen); err != nil {
			return 0, err
		}
		var paramTypes []ast.Type
		if !p.check(token.TokenRParen) {
			for {
				pt, err := p.parseType()
				if err != nil {
					return 0, err
				}
				paramTypes = append(paramTypes, pt)
				if !p.match(token.TokenComma) {
					break
				}
			}
		}
		if err := p.expect(token.TokenRParen); err != nil {
			return 0, err
		}
		if err := p.expect(token.TokenColon); err != nil {
			return 0, err
		}
		retType, err := p.parseType()
		if err != nil {
			return 0, err
		}
		return ast.FuncTypeOf(paramTypes, retType), nil
	case token.TokenMap:
		p.advance() // consume 'map'
		if err := p.expect(token.TokenLBracket); err != nil {
			return 0, p.errorf("expected '[' after 'map'")
		}
		keyType, err := p.parseType()
		if err != nil {
			return 0, err
		}
		if err := p.expect(token.TokenComma); err != nil {
			return 0, p.errorf("expected ',' in map type")
		}
		valType, err := p.parseType()
		if err != nil {
			return 0, err
		}
		if err := p.expect(token.TokenRBracket); err != nil {
			return 0, p.errorf("expected ']' after map value type")
		}
		return ast.MapTypeOf(keyType, valType), nil
	case token.TokenIdent:
		name := tok.Value
		if p.structNames[name] {
			p.advance()
			t, ok := ast.LookupStructType(name)
			if !ok {
				return 0, p.errorf("unknown struct type '%s'", name)
			}
			base = t
		} else if p.enumNames[name] {
			p.advance()
			t, ok := ast.LookupEnumType(name)
			if !ok {
				return 0, p.errorf("unknown enum type '%s'", name)
			}
			base = t
		} else {
			return 0, p.errorf("unknown type '%s'", name)
		}
	default:
		return 0, p.errorf("expected type ('int', 'bool', 'string', 'long', 'double', 'char', 'fn', 'chan', or 'void')")
	}

	// Check for [] suffix to make array type
	if p.check(token.TokenLBracket) && p.pos+1 < len(p.tokens) && p.tokens[p.pos+1].Kind == token.TokenRBracket {
		p.advance() // consume '['
		p.advance() // consume ']'
		base = ast.ArrayTypeOf(base)
	}

	// Check for ? suffix to make optional type
	if p.check(token.TokenQuestion) {
		if ast.IsOptionalType(base) {
			return 0, p.errorf("double-optional types are not allowed")
		}
		p.advance() // consume '?'
		return ast.OptionalTypeOf(base), nil
	}

	return base, nil
}

func (p *Parser) parseBlock() ([]ast.Stmt, error) {
	var stmts []ast.Stmt

	for !p.check(token.TokenRBrace) && !p.atEnd() {
		stmt, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, stmt)
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
	default:
		return p.parseExprStmt()
	}
}

func (p *Parser) parseLetStmt() (ast.Stmt, error) {
	pos := p.nodePos()
	p.advance() // consume 'let'

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
	if p.check(token.TokenRBrace) || p.check(token.TokenEOF) {
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

	case token.TokenLBrace:
		// Empty map literal: {}
		if p.pos+1 < len(p.tokens) && p.tokens[p.pos+1].Kind == token.TokenRBrace {
			p.advance() // consume '{'
			p.advance() // consume '}'
			return &ast.MapLitExpr{Pos: pos}, nil
		}
		return nil, p.errorf("unexpected '{'")

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
		if p.check(token.TokenDot) {
			p.advance() // consume '.'
			memberName, err := p.expectIdent()
			if err != nil {
				return nil, err
			}
			// If followed by '(', it's a method/qualified call
			if p.check(token.TokenLParen) {
				p.advance() // consume '('
				args, err := p.parseArgs()
				if err != nil {
					return nil, err
				}
				if err := p.expect(token.TokenRParen); err != nil {
					return nil, err
				}
				return &ast.CallExpr{Pos: pos, Module: name, Name: memberName, Args: args}, nil
			}
			// Otherwise it's a field access
			return &ast.FieldAccessExpr{Pos: pos, Object: &ast.Ident{Pos: pos, Name: name}, Field: memberName}, nil
		}

		// Check for index expression: ident[expr]
		if p.check(token.TokenLBracket) {
			p.advance() // consume '['
			index, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			if err := p.expect(token.TokenRBracket); err != nil {
				return nil, err
			}
			return &ast.IndexExpr{Pos: pos, Array: &ast.Ident{Pos: pos, Name: name}, Index: index}, nil
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
			return &ast.CallExpr{Pos: pos, Name: name, Args: args}, nil
		}

		return &ast.Ident{Pos: pos, Name: name}, nil

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

	if p.check(token.TokenRParen) {
		return args, nil
	}

	for {
		expr, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		args = append(args, expr)

		if !p.match(token.TokenComma) {
			break
		}
	}

	return args, nil
}

// Helper methods

func (p *Parser) current() token.Token {
	if p.pos >= len(p.tokens) {
		return token.Token{Kind: token.TokenEOF, Line: 0, Col: 0}
	}
	return p.tokens[p.pos]
}

func (p *Parser) advance() {
	if p.pos < len(p.tokens) {
		p.pos++
	}
}

func (p *Parser) atEnd() bool {
	return p.current().Kind == token.TokenEOF
}

func (p *Parser) check(kind token.TokenKind) bool {
	return p.current().Kind == kind
}

func (p *Parser) match(kind token.TokenKind) bool {
	if p.check(kind) {
		p.advance()
		return true
	}
	return false
}

func (p *Parser) expect(kind token.TokenKind) error {
	if !p.check(kind) {
		return p.errorf("expected %s, got '%s'", kind, p.current().Value)
	}
	p.advance()
	return nil
}

func (p *Parser) expectIdent() (string, error) {
	tok := p.current()
	if tok.Kind != token.TokenIdent {
		return "", p.errorf("expected identifier, got '%s'", tok.Value)
	}
	p.advance()
	return tok.Value, nil
}

func (p *Parser) collectAnnotations() []string {
	var annotations []string
	for p.check(token.TokenAnnotation) {
		annotations = append(annotations, p.current().Value)
		p.advance()
	}
	return annotations
}

func (p *Parser) errorf(format string, args ...interface{}) error {
	tok := p.current()
	prefix := fmt.Sprintf("%d:%d: ", tok.Line, tok.Col)
	return fmt.Errorf(prefix+format, args...)
}

// pos returns the current token's position as an ast.Pos.
func (p *Parser) nodePos() ast.Pos {
	tok := p.current()
	return ast.Pos{Line: tok.Line, Col: tok.Col}
}
