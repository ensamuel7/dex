package parser

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/token"
)

// Generics are monomorphised by re-parsing.
//
// A generic declaration is captured as the tokens it was written as and skipped
// rather than parsed. Instantiating it re-parses those tokens with its type
// parameters bound to concrete types and its own name mangled, which produces
// an ordinary function or struct the checker and codegen already understand.
//
// Re-parsing rather than substituting over a parsed body is the whole reason
// this is tractable here: the parser is where a type name becomes an ast.Type,
// so binding `T` to `int` is one lookup in a substitution map. Substituting
// after the fact would mean deep-copying and type-remapping all forty-nine
// expression and statement kinds, and every one missed is a node that silently
// keeps the type parameter.
//
// The declare-before-use rule the language already has is what makes a
// parse-time instantiation of a generic struct legal: `Box<int>` can only be
// written below `struct Box<T>`, so the template is always captured by the time
// a use site needs it.

// typeSubst binds a type parameter name to the concrete type it stands for
// during one instantiation, and nameSubst renames the declaration itself and
// any recursive reference to it. Both are empty while ordinary code is parsed.
type genericSubst struct {
	types map[string]ast.Type
	names map[string]string
}

// isGenericDeclAhead reports whether the declaration at the cursor carries a
// type parameter list, and which kind it is.
//
// The `<` is unambiguous here: a declaration name is never followed by a
// less-than in any other production.
func (p *Parser) isGenericDeclAhead() (isFunc, isStruct bool) {
	i := p.pos
	for i < len(p.tokens) && p.tokens[i].Kind == token.TokenAnnotation {
		i++
	}
	for i < len(p.tokens) && (p.tokens[i].Kind == token.TokenPublic || p.tokens[i].Kind == token.TokenPrivate) {
		i++
	}
	if i >= len(p.tokens) {
		return false, false
	}
	kind := p.tokens[i].Kind
	if kind != token.TokenStruct && kind != token.TokenFn && kind != token.TokenFunction {
		return false, false
	}
	// name, then '<'
	if i+2 >= len(p.tokens) || p.tokens[i+1].Kind != token.TokenIdent {
		return false, false
	}
	if p.tokens[i+2].Kind != token.TokenLt {
		return false, false
	}
	return kind != token.TokenStruct, kind == token.TokenStruct
}

// captureGenericDecl records the declaration at the cursor as a template and
// advances past it. The tokens are kept verbatim, including the type parameter
// list, because the re-parse reads the list again to know what to bind.
func (p *Parser) captureGenericDecl() (*ast.GenericDecl, error) {
	start := p.pos
	for p.pos < len(p.tokens) && p.tokens[p.pos].Kind == token.TokenAnnotation {
		p.advance()
	}
	for p.check(token.TokenPublic) || p.check(token.TokenPrivate) {
		p.advance()
	}
	declPos := p.nodePos()
	p.advance() // 'struct' | 'fn'

	if !p.check(token.TokenIdent) {
		return nil, p.errorf("expected a name after a generic declaration")
	}
	name := p.current().Value
	p.advance()

	params, err := p.parseTypeParamList()
	if err != nil {
		return nil, err
	}
	if len(params) == 0 {
		return nil, p.errorf("'%s' declares an empty type parameter list; write it without the angle brackets", name)
	}

	// Skip to the body and consume it, so the declaration is captured whole.
	for p.pos < len(p.tokens) && !p.check(token.TokenLBrace) {
		p.advance()
	}
	if !p.check(token.TokenLBrace) {
		return nil, p.errorf("expected a body for generic '%s'", name)
	}
	depth := 0
	for p.pos < len(p.tokens) {
		switch p.current().Kind {
		case token.TokenLBrace:
			depth++
		case token.TokenRBrace:
			depth--
		}
		p.advance()
		if depth == 0 {
			break
		}
	}
	if depth != 0 {
		return nil, p.errorf("unterminated body for generic '%s'", name)
	}

	return &ast.GenericDecl{
		Pos:        declPos,
		Name:       name,
		TypeParams: params,
		Tokens:     toTokenLikes(p.tokens[start:p.pos]),
		Module:     p.module,
	}, nil
}

// parseTypeParamList reads `<T, U>` and returns the names.
func (p *Parser) parseTypeParamList() ([]string, error) {
	if err := p.expect(token.TokenLt); err != nil {
		return nil, p.errorf("expected '<' to open a type parameter list")
	}
	var names []string
	for {
		if !p.check(token.TokenIdent) {
			return nil, p.errorf("expected a type parameter name")
		}
		name := p.current().Value
		for _, existing := range names {
			if existing == name {
				return nil, p.errorf("duplicate type parameter '%s'", name)
			}
		}
		names = append(names, name)
		p.advance()
		if p.check(token.TokenComma) {
			p.advance()
			continue
		}
		break
	}
	if err := p.expect(token.TokenGt); err != nil {
		return nil, p.errorf("expected '>' to close a type parameter list")
	}
	return names, nil
}

// parseTypeArgList reads `<int, string>` in a use position and returns the
// concrete types.
func (p *Parser) parseTypeArgList() ([]ast.Type, error) {
	if err := p.expect(token.TokenLt); err != nil {
		return nil, p.errorf("expected '<' to open a type argument list")
	}
	var args []ast.Type
	for {
		t, err := p.parseType()
		if err != nil {
			return nil, err
		}
		args = append(args, t)
		if p.check(token.TokenComma) {
			p.advance()
			continue
		}
		break
	}
	if err := p.expect(token.TokenGt); err != nil {
		return nil, p.errorf("expected '>' to close a type argument list")
	}
	return args, nil
}

// MangleGenericName is the flat name an instantiation registers under. It is
// the identity of the instantiation everywhere downstream, so codegen's
// `Dex_Box_int` falls out with no codegen change at all.
func MangleGenericName(name string, args []ast.Type) string {
	var b strings.Builder
	b.WriteString(name)
	for _, a := range args {
		b.WriteString("_")
		b.WriteString(typeMangle(a))
	}
	return b.String()
}

// typeMangle names a type in a way that is unique and legal in a C identifier.
func typeMangle(t ast.Type) string {
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
	case ast.IsArrayType(t):
		return typeMangle(ast.ElementType(t)) + "arr"
	case ast.IsOptionalType(t):
		return typeMangle(ast.OptionalInnerType(t)) + "opt"
	case ast.IsStructType(t):
		return ast.StructName(t)
	case ast.IsEnumType(t):
		return ast.EnumName(t)
	case ast.IsInterfaceType(t):
		return ast.InterfaceName(t)
	}
	return "t"
}

func toTokenLikes(src []token.Token) []ast.TokenLike {
	out := make([]ast.TokenLike, len(src))
	for i, t := range src {
		out[i] = ast.TokenLike{Kind: int(t.Kind), Value: t.Value, Line: t.Line, Col: t.Col, File: t.File}
	}
	return out
}

func fromTokenLikes(src []ast.TokenLike) []token.Token {
	out := make([]token.Token, len(src))
	for i, t := range src {
		out[i] = token.Token{Kind: token.TokenKind(t.Kind), Value: t.Value, Line: t.Line, Col: t.Col, File: t.File}
	}
	return out
}

// isGenericDeclAheadAny is the dispatch test: either kind of generic
// declaration is at the cursor.
func (p *Parser) isGenericDeclAheadAny() bool {
	isFunc, isStruct := p.isGenericDeclAhead()
	return isFunc || isStruct
}

// applyGenericDeclName is called by parseStructDef and parseFunction right
// after the declared name. Re-parsing a template for an instantiation means the
// declaration still carries its type parameter list, which is consumed and
// discarded because the substitution already binds it, and the name it declares
// is the mangled one rather than the template's.
func (p *Parser) applyGenericDeclName(name string) (string, error) {
	if len(p.subst.names) == 0 {
		return name, nil
	}
	if p.check(token.TokenLt) {
		if _, err := p.parseTypeParamList(); err != nil {
			return "", err
		}
	}
	if alias, ok := p.subst.names[name]; ok {
		return alias, nil
	}
	return name, nil
}

// childParser builds a parser over a template's tokens that shares this one's
// name tables, so a type the template refers to still resolves and a generic it
// instantiates in turn is found.
func (p *Parser) childParser(decl *ast.GenericDecl, sub genericSubst) *Parser {
	child := New(fromTokenLikes(decl.Tokens))
	child.structNames = p.structNames
	child.enumNames = p.enumNames
	child.interfaceNames = p.interfaceNames
	child.candidates = p.candidates
	child.genericStructs = p.genericStructs
	child.genericFuncs = p.genericFuncs
	child.instantiatedStructs = p.instantiatedStructs
	child.module = decl.Module
	child.subst = sub
	return child
}

// substFor binds a template's type parameters to the arguments given.
func substFor(decl *ast.GenericDecl, args []ast.Type, flat string) genericSubst {
	sub := genericSubst{
		types: make(map[string]ast.Type, len(decl.TypeParams)),
		names: map[string]string{decl.Name: flat},
	}
	for i, tp := range decl.TypeParams {
		sub.types[tp] = args[i]
	}
	return sub
}

// instantiateStruct builds the concrete struct for `Name<args>` and returns its
// type. An instantiation already built is returned as it stands, which is what
// makes two mentions of `Box<int>` the same type rather than two.
func (p *Parser) instantiateStruct(name string, args []ast.Type) (ast.Type, error) {
	decl := p.genericStructs[name]
	if decl == nil {
		return 0, p.errorf("unknown generic struct '%s'", name)
	}
	if len(args) != len(decl.TypeParams) {
		return 0, p.errorf("%s takes %d type argument(s), got %d",
			name, len(decl.TypeParams), len(args))
	}
	for _, a := range args {
		if ast.IsTypeParamType(a) {
			// Instantiating with a parameter still in scope: the outer template
			// is being re-parsed, and the inner one is built when the outer is.
			return 0, p.errorf("cannot instantiate %s with an unbound type parameter", name)
		}
	}

	flat := ast.QualifiedStructName(decl.Module, MangleGenericName(name, args))
	if t, ok := ast.LookupStructType(flat); ok && p.structNames[flat] {
		return t, nil
	}
	// Registered before the body is parsed so a self-referential method
	// signature inside the template resolves, the same reason parseStructDef
	// registers a placeholder ahead of its own body.
	p.structNames[flat] = true

	child := p.childParser(decl, substFor(decl, args, MangleGenericName(name, args)))
	sd, err := child.parseStructDef()
	if err != nil {
		delete(p.structNames, flat)
		return 0, err
	}
	*p.instantiatedStructs = append(*p.instantiatedStructs, *sd)

	t, ok := ast.LookupStructType(flat)
	if !ok {
		return 0, p.errorf("internal error: instantiating %s did not register %s", name, flat)
	}
	return t, nil
}

// InstantiateFunc builds the concrete function for a generic called with the
// given type arguments, and reports the flat name it registered under. The
// checker drives this: inference needs argument types, which only it has.
//
// Returning the function rather than appending it anywhere keeps the parser out
// of the business of deciding when a function is checked.
func (p *Parser) InstantiateFunc(name string, args []ast.Type) (*ast.Function, []ast.StructDef, error) {
	decl := p.genericFuncs[name]
	if decl == nil {
		return nil, nil, fmt.Errorf("unknown generic function '%s'", name)
	}
	if len(args) != len(decl.TypeParams) {
		return nil, nil, fmt.Errorf("%s takes %d type argument(s), got %d",
			name, len(decl.TypeParams), len(args))
	}

	flat := MangleGenericName(name, args)
	before := len(*p.instantiatedStructs)
	child := p.childParser(decl, substFor(decl, args, flat))
	fn, err := child.parseFunction()
	if err != nil {
		return nil, nil, err
	}
	if len(child.errors) > 0 {
		return nil, nil, child.errors[0]
	}
	// A generic function may name a generic struct, which instantiates while
	// its body is parsed; those definitions have to reach the program too.
	var newStructs []ast.StructDef
	if len(*p.instantiatedStructs) > before {
		newStructs = append(newStructs, (*p.instantiatedStructs)[before:]...)
	}
	return fn, newStructs, nil
}

// GenericFuncNames reports the generic functions this file declared, so the
// checker knows which calls to resolve by instantiation.
func (p *Parser) GenericFuncNames() map[string][]string {
	out := make(map[string][]string, len(p.genericFuncs))
	for name, decl := range p.genericFuncs {
		out[name] = decl.TypeParams
	}
	return out
}

// parseGenericSignature reads a generic function's parameter list and return
// type with its type parameters bound to type-parameter types, so inference at
// a call site can unify an argument type against a parameter that is still
// written as `T`. Only the signature is read; the body stays as tokens until an
// instantiation re-parses it.
func (p *Parser) parseGenericSignature(decl *ast.GenericDecl) ([]ast.Param, ast.Type, error) {
	sub := genericSubst{
		types: make(map[string]ast.Type, len(decl.TypeParams)),
		names: map[string]string{},
	}
	for _, tp := range decl.TypeParams {
		sub.types[tp] = ast.RegisterTypeParam(tp)
	}
	child := p.childParser(decl, sub)

	for child.check(token.TokenAnnotation) {
		child.advance()
	}
	for child.check(token.TokenPublic) || child.check(token.TokenPrivate) {
		child.advance()
	}
	if !child.check(token.TokenFn) && !child.check(token.TokenFunction) {
		return nil, 0, child.errorf("expected 'fn' in generic declaration '%s'", decl.Name)
	}
	child.advance()
	if !child.check(token.TokenIdent) {
		return nil, 0, child.errorf("expected a name in generic declaration")
	}
	child.advance()
	if _, err := child.parseTypeParamList(); err != nil {
		return nil, 0, err
	}
	if err := child.expect(token.TokenLParen); err != nil {
		return nil, 0, child.errorf("expected '(' after generic function '%s'", decl.Name)
	}
	params, err := child.parseParams()
	if err != nil {
		return nil, 0, err
	}
	if err := child.expect(token.TokenRParen); err != nil {
		return nil, 0, child.errorf("expected ')' after the parameters of '%s'", decl.Name)
	}
	if err := child.expect(token.TokenColon); err != nil {
		return nil, 0, child.errorf("generic function '%s' needs a return type", decl.Name)
	}
	ret, err := child.parseType()
	if err != nil {
		return nil, 0, err
	}
	return params, ret, nil
}
