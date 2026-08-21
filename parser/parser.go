package parser

import (
	"fmt"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/token"
)

// maxParseErrors is a sentinel type used to abort parsing via panic
// when the error limit is reached.
type maxParseErrors struct{}

type Parser struct {
	tokens         []token.Token
	pos            int
	structNames    map[string]bool
	enumNames      map[string]bool
	interfaceNames map[string]bool
	errors         []error
	maxErrors      int
	lastArgNames   []string // set by parseArgs when named arguments are used
}

func New(tokens []token.Token) *Parser {
	return &Parser{tokens: tokens, pos: 0, structNames: make(map[string]bool), enumNames: make(map[string]bool), interfaceNames: make(map[string]bool), maxErrors: 20}
}

// recordError appends an error and panics with maxParseErrors if the limit is reached.
func (p *Parser) recordError(err error) {
	p.errors = append(p.errors, err)
	if len(p.errors) >= p.maxErrors {
		panic(maxParseErrors{})
	}
}

// synchronize advances past tokens until the next top-level declaration boundary.
func (p *Parser) synchronize() {
	for !p.atEnd() {
		switch p.current().Kind {
		case token.TokenFn, token.TokenFunction, token.TokenStruct, token.TokenEnum,
			token.TokenPublic, token.TokenPrivate, token.TokenAnnotation,
			token.TokenLet, token.TokenConst:
			return
		}
		p.advance()
	}
}

// AddStructName registers a struct name so the parser recognizes it as a type.
// Used for module-provided struct types (e.g. HttpResponse from the http module).
func (p *Parser) AddStructName(name string) {
	p.structNames[name] = true
}

func (p *Parser) Parse() (program *ast.Program, errs []error) {
	program = &ast.Program{}

	// Recover from maxParseErrors sentinel panic
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(maxParseErrors); !ok {
				panic(r) // re-panic if not our sentinel
			}
		}
		errs = p.errors
	}()

	// Parse import declarations
	for p.check(token.TokenImport) {
		p.advance() // consume 'import'
		if !p.check(token.TokenString) {
			p.recordError(p.errorf("expected string after 'import'"))
			p.synchronize()
			continue
		}
		path := p.current().Value
		p.advance()
		var alias string
		if p.check(token.TokenAs) {
			p.advance() // consume 'as'
			if !p.check(token.TokenString) {
				p.recordError(p.errorf("expected string after 'as'"))
				p.synchronize()
				continue
			}
			alias = p.current().Value
			p.advance()
		}
		program.Imports = append(program.Imports, ast.Import{Path: path, Alias: alias})
		p.match(token.TokenSemicolon) // optional trailing semicolon
	}

	// Parse struct, enum, and interface definitions (with optional annotations and access modifiers)
	for p.isStructDefAhead() || p.isEnumDefAhead() || p.isInterfaceDefAhead() {
		if p.isInterfaceDefAhead() {
			ifaceDef, err := p.parseInterfaceDef()
			if err != nil {
				p.recordError(err)
				p.synchronize()
				continue
			}
			program.Interfaces = append(program.Interfaces, *ifaceDef)
		} else if p.isEnumDefAhead() {
			ed, err := p.parseEnumDef()
			if err != nil {
				p.recordError(err)
				p.synchronize()
				continue
			}
			program.Enums = append(program.Enums, *ed)
		} else {
			annotations := p.collectAnnotations()
			sd, err := p.parseStructDef()
			if err != nil {
				p.recordError(err)
				p.synchronize()
				continue
			}
			_ = annotations // struct-level annotations reserved for future use
			program.Structs = append(program.Structs, *sd)
		}
	}

	// Parse module-level let/const declarations
	for p.isGlobalLetAhead() {
		annotations := p.collectAnnotations()
		var stmt ast.Stmt
		var err error
		if p.check(token.TokenLet) {
			stmt, err = p.parseLetStmt()
		} else {
			stmt, err = p.parseConstStmt()
		}
		if err != nil {
			p.recordError(err)
			p.synchronize()
			continue
		}
		if letStmt, ok := stmt.(*ast.LetStmt); ok {
			letStmt.Annotations = annotations
			program.GlobalLets = append(program.GlobalLets, *letStmt)
		}
		p.match(token.TokenSemicolon)
	}

	for !p.atEnd() {
		annotations := p.collectAnnotations()
		fn, err := p.parseFunction()
		if err != nil {
			p.recordError(err)
			p.synchronize()
			continue
		}
		fn.Annotations = annotations
		program.Functions = append(program.Functions, *fn)
	}

	return program, p.errors
}

// isGlobalLetAhead returns true if the current tokens form a module-level let/const
// declaration, possibly preceded by annotations.
func (p *Parser) isGlobalLetAhead() bool {
	i := p.pos
	for i < len(p.tokens) && p.tokens[i].Kind == token.TokenAnnotation {
		i++
	}
	return i < len(p.tokens) && (p.tokens[i].Kind == token.TokenLet || p.tokens[i].Kind == token.TokenConst)
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

// isInterfaceDefAhead returns true if the current token is the 'interface' keyword.
func (p *Parser) isInterfaceDefAhead() bool {
	return p.pos < len(p.tokens) && p.tokens[p.pos].Kind == token.TokenInterface
}

func (p *Parser) parseInterfaceDef() (*ast.InterfaceDef, error) {
	p.advance() // consume 'interface'

	name, err := p.expectIdent()
	if err != nil {
		return nil, err
	}

	if err := p.expect(token.TokenLBrace); err != nil {
		return nil, err
	}

	var methods []ast.InterfaceMethod
	for !p.check(token.TokenRBrace) && !p.atEnd() {
		// Expect: fn methodName(params): returnType
		if !p.check(token.TokenFn) && !p.check(token.TokenFunction) {
			return nil, p.errorf("expected 'fn' in interface method declaration")
		}
		p.advance() // consume 'fn'/'function'

		methodName, err := p.expectIdent()
		if err != nil {
			return nil, err
		}

		if err := p.expect(token.TokenLParen); err != nil {
			return nil, err
		}

		// Parse parameter types (no names needed, but accept name: type for consistency)
		var paramTypes []ast.Type
		if !p.check(token.TokenRParen) {
			for {
				// Try to skip optional param name with colon
				if p.check(token.TokenIdent) && p.pos+1 < len(p.tokens) && p.tokens[p.pos+1].Kind == token.TokenColon {
					p.advance() // skip name
					p.advance() // skip ':'
				}
				paramType, err := p.parseType()
				if err != nil {
					return nil, err
				}
				paramTypes = append(paramTypes, paramType)
				if !p.match(token.TokenComma) {
					break
				}
			}
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

		methods = append(methods, ast.InterfaceMethod{Name: methodName, Params: paramTypes, ReturnType: retType})
	}

	if err := p.expect(token.TokenRBrace); err != nil {
		return nil, err
	}

	p.interfaceNames[name] = true
	ast.RegisterInterfaceType(ast.InterfaceDef{Name: name, Methods: methods})

	return &ast.InterfaceDef{Name: name, Methods: methods}, nil
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

		param := ast.Param{Name: name, Type: typ, Annotations: annotations}

		// Check for default value: = expr
		if p.match(token.TokenAssign) {
			defaultVal, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			param.DefaultValue = defaultVal
		}

		params = append(params, param)

		if !p.match(token.TokenComma) {
			break
		}
	}

	// Validate: once a param has a default, all subsequent params must too
	seenDefault := false
	for _, param := range params {
		if param.DefaultValue != nil {
			seenDefault = true
		} else if seenDefault {
			return nil, p.errorf("parameter '%s' must have a default value (required after first default parameter)", param.Name)
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
		// Check for primitive type keywords first
		tok := p.current()
		switch tok.Kind {
		case token.TokenIntKw:
			p.advance()
			return ast.RefTypeOf(ast.TypeInt), nil
		case token.TokenLong:
			p.advance()
			return ast.RefTypeOf(ast.TypeLong), nil
		case token.TokenDouble:
			p.advance()
			return ast.RefTypeOf(ast.TypeDouble), nil
		case token.TokenBool:
			p.advance()
			return ast.RefTypeOf(ast.TypeBool), nil
		case token.TokenCharKw:
			p.advance()
			return ast.RefTypeOf(ast.TypeChar), nil
		}
		// Struct types can be referenced
		if !p.check(token.TokenIdent) {
			return 0, p.errorf("reference types (&) are only allowed on struct or primitive types")
		}
		name := p.current().Value
		if !p.structNames[name] {
			return 0, p.errorf("reference types (&) are only allowed on struct or primitive types, got '%s'", name)
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
	case token.TokenMutex:
		p.advance()
		return ast.TypeMutex, nil
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
		if name == "StringBuilder" {
			p.advance()
			base = ast.TypeStringBuilder
		} else if p.structNames[name] {
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
		} else if p.interfaceNames[name] {
			p.advance()
			t, ok := ast.LookupInterfaceType(name)
			if !ok {
				return 0, p.errorf("unknown interface type '%s'", name)
			}
			base = t
		} else if p.pos+2 < len(p.tokens) && p.tokens[p.pos+1].Kind == token.TokenDot && p.tokens[p.pos+2].Kind == token.TokenIdent {
			// Module-qualified type: module.TypeName (e.g. http.HttpRequest, ws.Conn)
			qualifiedName := p.tokens[p.pos+2].Value
			if p.structNames[qualifiedName] {
				p.advance() // consume module name
				p.advance() // consume '.'
				p.advance() // consume type name
				t, ok := ast.LookupStructType(qualifiedName)
				if !ok {
					return 0, p.errorf("unknown struct type '%s.%s'", name, qualifiedName)
				}
				base = t
			} else {
				return 0, p.errorf("unknown type '%s.%s'", name, qualifiedName)
			}
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
	var prefix string
	if tok.File != "" {
		prefix = fmt.Sprintf("%s:%d:%d: ", tok.File, tok.Line, tok.Col)
	} else {
		prefix = fmt.Sprintf("%d:%d: ", tok.Line, tok.Col)
	}
	return fmt.Errorf(prefix+format, args...)
}

// pos returns the current token's position as an ast.Pos.
func (p *Parser) nodePos() ast.Pos {
	tok := p.current()
	return ast.Pos{File: tok.File, Line: tok.Line, Col: tok.Col}
}
