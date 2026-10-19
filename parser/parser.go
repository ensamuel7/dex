package parser

import (
	"fmt"
	"sort"
	"strings"

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
	handleNames    map[string]ast.Type
	moduleAliases  map[string]string
	errors         []error
	maxErrors      int
	lastArgNames   []string // set by parseArgs when named arguments are used
	// module is the user module this file declares, empty for the main program.
	// A type declared here registers under it, so two modules can each declare
	// a `Response` — the same way each can declare a `make`.
	module string
	// candidates maps a bare type name to the flat names visible in this file:
	// its own module's, and one per import that declares it. A bare name with
	// several candidates is ambiguous and says so, rather than silently
	// resolving to whichever registered last.
	candidates map[string][]string
	// Generic templates captured as tokens, by name, and the substitution in
	// force while one is being re-parsed for an instantiation. Both are shared
	// with a child parser so a template can instantiate another one.
	genericStructs map[string]*ast.GenericDecl
	genericFuncs   map[string]*ast.GenericDecl
	subst          genericSubst
	// instantiatedStructs collects the struct definitions built by parse-time
	// instantiation, so Parse can append them to the program.
	instantiatedStructs *[]ast.StructDef
}

func New(tokens []token.Token) *Parser {
	return &Parser{
		tokens:              tokens,
		pos:                 0,
		structNames:         make(map[string]bool),
		enumNames:           make(map[string]bool),
		handleNames:         make(map[string]ast.Type),
		interfaceNames:      make(map[string]bool),
		maxErrors:           20,
		candidates:          make(map[string][]string),
		genericStructs:      make(map[string]*ast.GenericDecl),
		genericFuncs:        make(map[string]*ast.GenericDecl),
		instantiatedStructs: &[]ast.StructDef{},
	}
}

// SetModule names the user module this file declares. Its types register under
// that name, and a bare reference inside it resolves to its own first.
func (p *Parser) SetModule(name string) {
	p.module = name
}

func (p *Parser) SetModuleAliases(aliases map[string]string) {
	p.moduleAliases = aliases
}

func (p *Parser) moduleIdentity(qualifier string) string {
	if id, ok := p.moduleAliases[qualifier]; ok {
		return id
	}
	return qualifier
}

// AddModuleType records that `module` declares a type called `bare`, so a
// reference to it from this file resolves — qualified as `module.bare`, or
// unqualified when nothing else in scope declares that name.
func (p *Parser) AddModuleType(module, bare, kind string) {
	flat := ast.QualifiedStructName(module, bare)
	switch kind {
	case "enum":
		p.enumNames[flat] = true
	case "interface":
		p.interfaceNames[flat] = true
	default:
		p.structNames[flat] = true
	}
	for _, existing := range p.candidates[bare] {
		if existing == flat {
			return
		}
	}
	p.candidates[bare] = append(p.candidates[bare], flat)
}

// resolveTypeName turns a type name as written into the flat name it refers to.
//
// Its own module's type wins, the way a local wins over an imported module's
// name: inside `alpha` a bare `Response` is alpha's, even if an import also
// declares one. Otherwise exactly one candidate resolves, several is an error
// that names them, and none falls through to the bare name — which is how the
// main program's own types and the struct types a stdlib module provides are
// still found.
func (p *Parser) resolveTypeName(name string) (string, error) {
	// Inside a template being re-parsed, the template's own name means this
	// instantiation — so `Box { value: v }` in struct Box<T> resolves to
	// Box_int. This is the one hook every name-reading site shares, which is
	// why the rename lives here rather than at each of them.
	if alias, ok := p.subst.names[name]; ok {
		if p.module != "" {
			return ast.QualifiedStructName(p.module, alias), nil
		}
		return alias, nil
	}
	if p.module != "" {
		own := ast.QualifiedStructName(p.module, name)
		if p.structNames[own] || p.enumNames[own] {
			return own, nil
		}
	} else if p.structNames[name] || p.enumNames[name] {
		// The main program has no module prefix, so its own declarations are
		// registered under the bare name. Without this they lost to an
		// imported module's type of the same name: the candidate list had one
		// entry and won, so `struct SignupController` in main.dx resolved to
		// the imported one and reported a missing field on a type the author
		// never wrote. A module's own declarations already win this way; the
		// main program's did not.
		return name, nil
	}
	switch cands := p.candidates[name]; len(cands) {
	case 0:
		return name, nil
	case 1:
		return cands[0], nil
	default:
		return "", p.errorf("ambiguous type '%s': declared by %s — qualify it, as in %s",
			name, modulesOf(cands), ast.DisplayStructName(cands[0]))
	}
}

// modulesOf lists the modules a set of candidates came from, for the message.
func modulesOf(flats []string) string {
	names := make([]string, 0, len(flats))
	for _, flat := range flats {
		names = append(names, "'"+ast.DisplayStructName(flat)+"'")
	}
	sort.Strings(names)
	return strings.Join(names, " and ")
}

// knownEnumName is knownTypeName restricted to enums, for the syntax that only
// an enum can start: EnumName.Variant.
func (p *Parser) knownEnumName(name string) bool {
	if p.enumNames[name] {
		return true
	}
	if p.module != "" && p.enumNames[ast.QualifiedStructName(p.module, name)] {
		return true
	}
	for _, flat := range p.candidates[name] {
		if p.enumNames[flat] {
			return true
		}
	}
	return false
}

// knownTypeName reports whether a name refers to a type at all, under either
// spelling, which is what the literal and reference-type syntax checks ask.
func (p *Parser) knownTypeName(name string) bool {
	if p.structNames[name] || p.enumNames[name] {
		return true
	}
	if p.module != "" {
		own := ast.QualifiedStructName(p.module, name)
		if p.structNames[own] || p.enumNames[own] {
			return true
		}
	}
	return len(p.candidates[name]) > 0
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
			token.TokenLet, token.TokenConst, token.TokenExternal:
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

// AddInterfaceName registers an interface name, for a module-declared one the
// main file has to recognize in type position.
func (p *Parser) AddInterfaceName(name string) {
	p.interfaceNames[name] = true
}

// AddEnumName registers an enum name so the parser recognizes it as a type and
// as the subject of a variant access. An imported module's enums have to come
// through here rather than through AddStructName: registered as a struct, the
// name resolves in type position and then fails the struct lookup, which is
// why `module.Enum` used to be reported as an unknown struct type.
func (p *Parser) AddEnumName(name string) {
	p.enumNames[name] = true
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
		importPos := p.nodePos()
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
		program.Imports = append(program.Imports, ast.Import{Pos: importPos, Path: path, Alias: alias})
		p.match(token.TokenSemicolon) // optional trailing semicolon
	}

	// Parse the rest of the file. Declarations may appear in any order — a
	// module-level const above the struct it describes reads better than one
	// forced below it, and nothing about the grammar requires the separation.
	for !p.atEnd() {
		switch {
		// Captured before the ordinary cases, because a generic declaration is
		// not parsed where it is written — it is re-parsed per instantiation.
		case p.isGenericDeclAheadAny():
			isFunc, _ := p.isGenericDeclAhead()
			decl, err := p.captureGenericDecl()
			if err != nil {
				p.recordError(err)
				p.synchronize()
				continue
			}
			if isFunc {
				params, ret, sigErr := p.parseGenericSignature(decl)
				if sigErr != nil {
					p.recordError(sigErr)
					continue
				}
				decl.Params = params
				decl.ReturnType = ret
				p.genericFuncs[decl.Name] = decl
				program.GenericFuncs = append(program.GenericFuncs, *decl)
			} else {
				p.genericStructs[decl.Name] = decl
				program.GenericStructs = append(program.GenericStructs, *decl)
			}

		case p.isExternalModuleAhead():
			em, err := p.parseExternalModule()
			if err != nil {
				p.recordError(err)
				p.synchronize()
				continue
			}
			program.Externals = append(program.Externals, *em)

		case p.isInterfaceDefAhead():
			ifaceDef, err := p.parseInterfaceDef()
			if err != nil {
				p.recordError(err)
				p.synchronize()
				continue
			}
			program.Interfaces = append(program.Interfaces, *ifaceDef)

		case p.isEnumDefAhead():
			ed, err := p.parseEnumDef()
			if err != nil {
				p.recordError(err)
				p.synchronize()
				continue
			}
			program.Enums = append(program.Enums, *ed)

		case p.isStructDefAhead():
			annotations := p.collectAnnotations()
			sd, err := p.parseStructDef()
			if err != nil {
				p.recordError(err)
				p.synchronize()
				continue
			}
			_ = annotations // struct-level annotations reserved for future use
			program.Structs = append(program.Structs, *sd)

		case p.isGlobalLetAhead():
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

		default:
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
	}

	// A generic struct instantiated from a type position produced a real struct
	// definition; it joins the program as though it had been written out.
	program.Structs = append(program.Structs, *p.instantiatedStructs...)

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

	name, err := p.expectDeclName("an interface name")
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

		methodName, err := p.expectDeclName("a method name")
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

	// A module's interface belongs to it, the way its structs and enums do, so
	// two modules can each declare a `Handler` and the generated C names differ
	// without any further work.
	flat := name
	if p.module != "" {
		flat = ast.QualifiedStructName(p.module, name)
	}
	p.interfaceNames[flat] = true
	def := ast.InterfaceDef{Name: flat, Module: p.module, Methods: methods}
	ast.RegisterInterfaceType(def)

	return &def, nil
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

	name, err := p.expectDeclName("an enum name")
	if err != nil {
		return nil, err
	}

	if err := p.expect(token.TokenLBrace); err != nil {
		return nil, err
	}

	// The enum is registered before its payload types are read, because a
	// variant may name the enum itself -- `Add(Expr, Expr)` inside `enum Expr`
	// -- and resolveTypeName has to find it. The entry is rewritten with the
	// payloads once they are known; RegisterEnumType updates in place.
	flatName := ast.QualifiedStructName(p.module, name)
	p.enumNames[flatName] = true
	ast.RegisterEnumType(ast.EnumDef{Name: flatName, Module: p.module})

	var variants []string
	var payloads [][]ast.Type
	for !p.check(token.TokenRBrace) && !p.atEnd() {
		variant, err := p.expectDeclName("an enum variant")
		if err != nil {
			return nil, err
		}
		var fields []ast.Type
		if p.check(token.TokenLParen) {
			p.advance() // consume '('
			if p.check(token.TokenRParen) {
				return nil, p.errorf("enum variant '%s.%s' has empty parentheses; drop them for a variant that carries nothing", name, variant)
			}
			for {
				ft, err := p.parseType()
				if err != nil {
					return nil, err
				}
				fields = append(fields, ft)
				if !p.check(token.TokenComma) {
					break
				}
				p.advance()
			}
			if err := p.expect(token.TokenRParen); err != nil {
				return nil, err
			}
		}
		variants = append(variants, variant)
		payloads = append(payloads, fields)
	}

	if err := p.expect(token.TokenRBrace); err != nil {
		return nil, err
	}

	if len(variants) == 0 {
		return nil, p.errorf("enum '%s' must have at least one variant", name)
	}

	def := ast.EnumDef{Name: flatName, Module: p.module, Variants: variants, Payloads: payloads}
	ast.RegisterEnumType(def)

	return &def, nil
}

func (p *Parser) parseStructDef() (*ast.StructDef, error) {
	// Consume optional access modifier (ignored on struct itself for now)
	if p.check(token.TokenPublic) || p.check(token.TokenPrivate) {
		p.advance()
	}

	p.advance() // consume 'struct'

	name, err := p.expectDeclName("a struct name")
	if err != nil {
		return nil, err
	}
	name, err = p.applyGenericDeclName(name)
	if err != nil {
		return nil, err
	}

	// Check for auto-constructor syntax: struct Foo(x: int, y: string) { ... }
	var constructorParams []ast.StructField
	if p.check(token.TokenLParen) {
		p.advance() // consume '('
		if !p.check(token.TokenRParen) {
			for {
				paramName, err := p.expectDeclName("a constructor parameter")
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

	// Registered before the body is parsed, so a method inside it can name the
	// struct it belongs to — `fn limit(n: int): &Query` within struct Query.
	// RegisterStructType updates an existing entry, so the call after the body
	// fills in the real fields over this placeholder.
	flatName := ast.QualifiedStructName(p.module, name)
	p.structNames[flatName] = true
	ast.RegisterStructType(ast.StructDef{Name: flatName, Module: p.module, ConstructorParams: constructorParams})

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
		fieldName, err := p.expectDeclName("a field name")
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
		// An optional tag follows the type: name: type `db:"..."`.
		tag := ""
		if p.check(token.TokenTag) {
			tag = p.current().Value
			p.advance()
		}
		fields = append(fields, ast.StructField{Name: fieldName, Type: fieldType, IsPrivate: isPrivate, Annotations: annotations, Tag: tag})
	}

	if err := p.expect(token.TokenRBrace); err != nil {
		return nil, err
	}

	p.structNames[flatName] = true
	// Methods go into the registry too, not only onto the returned def:
	// interface satisfaction is decided from the registered type, and without
	// them every struct was found to be missing every method.
	ast.RegisterStructType(ast.StructDef{Name: flatName, Module: p.module, Fields: fields, Methods: methods, ConstructorParams: constructorParams})

	return &ast.StructDef{Name: flatName, Module: p.module, Fields: fields, Methods: methods, ConstructorParams: constructorParams}, nil
}

func (p *Parser) parseFunction() (*ast.Function, error) {
	declPos := p.nodePos()

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

	name, err := p.expectDeclName("a function name")
	if err != nil {
		return nil, err
	}
	name, err = p.applyGenericDeclName(name)
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

	// `throws` sits after the return type: fn parse(s: string): int throws.
	// It declares that an exception may escape this function, so a caller is
	// told to handle it rather than discovering it at runtime. Naming types
	// after it — `throws ParseError, IOError` — says which.
	throws := p.match(token.TokenThrows)
	var throwsTypes []string
	if throws {
		for p.check(token.TokenIdent) {
			name, err := p.expectIdent()
			if err != nil {
				return nil, err
			}
			throwsTypes = append(throwsTypes, name)
			if !p.match(token.TokenComma) {
				break
			}
		}
	}

	if err := p.expect(token.TokenLBrace); err != nil {
		return nil, err
	}

	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}

	return &ast.Function{
		Pos:         declPos,
		Name:        name,
		Params:      params,
		ReturnType:  retType,
		Body:        body,
		IsPrivate:   isPrivate,
		Throws:      throws,
		ThrowsTypes: throwsTypes,
	}, nil
}

func (p *Parser) parseParams() ([]ast.Param, error) {
	var params []ast.Param

	if p.check(token.TokenRParen) {
		return params, nil
	}

	for {
		annotations := p.collectAnnotations()

		paramPos := p.nodePos()
		name, err := p.expectDeclName("a parameter name")
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

		param := ast.Param{Pos: paramPos, Name: name, Type: typ, Annotations: annotations}

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
		if !p.knownTypeName(name) {
			return 0, p.errorf("reference types (&) are only allowed on struct or primitive types, got '%s'", name)
		}
		p.advance()
		flat, err := p.resolveTypeName(name)
		if err != nil {
			return 0, err
		}
		t, ok := ast.LookupStructType(flat)
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
		if !mapKeyTypeSupported(keyType) {
			return 0, p.errorf("a map key must be string or int, not %s", describeType(keyType))
		}
		if err := p.expect(token.TokenComma); err != nil {
			return 0, p.errorf("expected ',' in map type")
		}
		valType, err := p.parseType()
		if err != nil {
			return 0, err
		}
		if !mapValueTypeSupported(valType) {
			return 0, p.errorf("a map value must be int, bool, string, long, double or char, not %s", describeType(valType))
		}
		if err := p.expect(token.TokenRBracket); err != nil {
			return 0, p.errorf("expected ']' after map value type")
		}
		return ast.MapTypeOf(keyType, valType), nil
	case token.TokenIdent:
		name := tok.Value
		// A type parameter of the template currently being re-parsed. Checked
		// first: a parameter named like a struct in scope still means the
		// parameter inside its own declaration.
		if t, ok := p.subst.types[name]; ok {
			p.advance()
			base = t
		} else if _, isGeneric := p.genericStructs[name]; isGeneric {
			// `Box<int>` in a type position: the instantiation is built here
			// and from now on it is an ordinary struct type.
			p.advance()
			if !p.check(token.TokenLt) {
				return 0, p.errorf("generic struct %s needs type arguments, as %s<...>", name, name)
			}
			args, err := p.parseTypeArgList()
			if err != nil {
				return 0, err
			}
			t, err := p.instantiateStruct(name, args)
			if err != nil {
				return 0, err
			}
			base = t
		} else if name == "StringBuilder" {
			p.advance()
			base = ast.TypeStringBuilder
		} else if t, ok := p.handleNames[name]; ok {
			p.advance()
			base = t
		} else if p.knownTypeName(name) && !p.enumNames[name] && !p.interfaceNames[name] {
			p.advance()
			flat, err := p.resolveTypeName(name)
			if err != nil {
				return 0, err
			}
			if t, ok := ast.LookupStructType(flat); ok {
				base = t
			} else if t, ok := ast.LookupEnumType(flat); ok {
				base = t
			} else if t, ok := ast.LookupInterfaceType(flat); ok {
				// A module's interface resolves here too: it is registered
				// under the module that declared it, so the bare name reaches
				// it through the same candidate list a struct uses.
				base = t
			} else {
				return 0, p.errorf("unknown type '%s'", name)
			}
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
			// json.Value is a built-in heap type rather than a module struct, so
			// it is resolved here rather than through the struct registry.
			if name == "json" && qualifiedName == "Value" {
				p.advance()
				p.advance()
				p.advance()
				base = ast.TypeJsonValue
			} else if flat := ast.QualifiedStructName(p.moduleIdentity(name), qualifiedName); p.structNames[flat] || p.enumNames[flat] || p.interfaceNames[flat] {
				// A user module's type, named by the module that declares it.
				p.advance() // consume module name
				p.advance() // consume '.'
				p.advance() // consume type name
				if t, ok := ast.LookupStructType(flat); ok {
					base = t
				} else if t, ok := ast.LookupEnumType(flat); ok {
					base = t
				} else if t, ok := ast.LookupInterfaceType(flat); ok {
					base = t
				} else {
					return 0, p.errorf("unknown type '%s.%s'", name, qualifiedName)
				}
			} else if p.structNames[qualifiedName] {
				// A struct type a stdlib module provides, registered under its
				// bare name: http.HttpRequest, ws.Conn.
				p.advance() // consume module name
				p.advance() // consume '.'
				p.advance() // consume type name
				t, ok := ast.LookupStructType(qualifiedName)
				if !ok {
					return 0, p.errorf("unknown struct type '%s.%s'", name, qualifiedName)
				}
				base = t
			} else if p.enumNames[qualifiedName] {
				p.advance() // consume module name
				p.advance() // consume '.'
				p.advance() // consume type name
				t, ok := ast.LookupEnumType(qualifiedName)
				if !ok {
					return 0, p.errorf("unknown enum type '%s.%s'", name, qualifiedName)
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
		arr := ast.ArrayTypeOf(base)
		// ArrayTypeOf answers void for an element type it has no array for,
		// which silently turned an unsupported element into a void parameter
		// and reported the mistake several statements later as a method call
		// on something that is not a container.
		if arr == ast.TypeVoid {
			return 0, p.errorf("cannot make an array of this type")
		}
		base = arr
	}

	// Check for ? suffix to make optional type
	if p.check(token.TokenQuestion) {
		if ast.IsOptionalType(base) {
			return 0, p.errorf("double-optional types are not allowed")
		}
		// An interface value is a vtable pair, not a pointer, so it has no
		// spelling for absent. Supporting this needs the present-flag layout a
		// value-type optional uses, and a release through the flag; until then
		// it is refused here rather than compiled into a comparison against
		// NULL that C rejects.
		if ast.IsInterfaceType(base) {
			return 0, p.errorf("cannot make the interface %s optional: an interface value is a vtable pair rather than a pointer, so it has no null. Hold the concrete struct type, or an array", ast.InterfaceName(base))
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

// expectDeclName reads a name being *declared* and refuses `self`.
//
// `self` is not a lexer keyword, and deliberately so: it has to remain an
// ordinary expression, because `self.cols` and `return self` are the whole
// point of it. What it must not be is a name anyone else can introduce —
// FlattenStructMethods names a method's receiver parameter `self`, so a
// user-written `self` elsewhere reads as a receiver where there is none, and
// inside a method it would shadow the real one.
//
// So it is reserved at the declaration sites rather than at the lexer: a
// parameter, a let, a const, a field, a loop variable, a catch binding, a
// function, a struct, an enum, an interface or one of their members.
func (p *Parser) expectDeclName(what string) (string, error) {
	tok := p.current()
	name, err := p.expectIdent()
	if err != nil {
		return "", err
	}
	if name == "self" {
		return "", p.errorAt(tok, "'self' is reserved: it names the receiver inside a struct method and cannot be used as %s", what)
	}
	return name, nil
}

func (p *Parser) collectAnnotations() []string {
	var annotations []string
	for p.check(token.TokenAnnotation) {
		annotations = append(annotations, p.current().Value)
		p.advance()
	}
	return annotations
}

// errorAt reports against a token the parser has already consumed, so a
// diagnostic can point at the name it is about rather than the one after it.
func (p *Parser) errorAt(tok token.Token, format string, args ...interface{}) error {
	var prefix string
	if tok.File != "" {
		prefix = fmt.Sprintf("%s:%d:%d: ", tok.File, tok.Line, tok.Col)
	} else {
		prefix = fmt.Sprintf("%d:%d: ", tok.Line, tok.Col)
	}
	return fmt.Errorf(prefix+format, args...)
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

// The map runtime is generated per key/value suffix pair, and only these have
// one. An unsupported type used to fall through to the "int" suffix, which
// silently truncated a long key and produced a C error about Dex_Person and int
// for a struct value — a diagnostic about the generated code rather than about
// the program. Rejecting the type where it is built says what is wrong instead.
func mapKeyTypeSupported(t ast.Type) bool {
	return t == ast.TypeString || t == ast.TypeInt
}

func mapValueTypeSupported(t ast.Type) bool {
	switch t {
	case ast.TypeInt, ast.TypeBool, ast.TypeString, ast.TypeLong, ast.TypeDouble, ast.TypeChar:
		return true
	}
	return false
}

// describeType names a type for a parse-time message. The checker's renderer is
// richer but lives downstream of here.
func describeType(t ast.Type) string {
	switch t {
	case ast.TypeInt:
		return "int"
	case ast.TypeBool:
		return "bool"
	case ast.TypeString:
		return "string"
	case ast.TypeLong:
		return "long"
	case ast.TypeDouble:
		return "double"
	case ast.TypeChar:
		return "char"
	case ast.TypeVoid:
		return "void"
	}
	switch {
	case ast.IsInterfaceType(t):
		return "the interface " + ast.InterfaceName(t)
	case ast.IsStructType(t):
		return "the struct " + ast.DisplayStructName(ast.StructName(t))
	case ast.IsEnumType(t):
		return "the enum " + ast.EnumName(t)
	case ast.IsArrayType(t):
		return "an array"
	case ast.IsMapType(t):
		return "a map"
	case ast.IsOptionalType(t):
		return "an optional"
	}
	return "that type"
}
