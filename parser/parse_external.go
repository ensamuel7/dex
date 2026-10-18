package parser

import (
	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/token"
)

func (p *Parser) isExternalModuleAhead() bool {
	return p.pos < len(p.tokens) && p.tokens[p.pos].Kind == token.TokenExternal
}

// softKeyword reports whether the current token is an identifier spelled word.
// `module`, `include`, `link` and `pkg` are only keywords inside an external
// block, so they stay usable as ordinary names everywhere else.
func (p *Parser) softKeyword(word string) bool {
	return p.check(token.TokenIdent) && p.current().Value == word
}

func (p *Parser) parseExternalModule() (*ast.ExternalModule, error) {
	pos := p.nodePos()
	p.advance() // consume 'external'

	if !p.softKeyword("module") {
		return nil, p.errorf("expected 'module' after 'external', as in external module zlib { ... }")
	}
	p.advance()

	name, err := p.expectDeclName("a module name")
	if err != nil {
		return nil, err
	}

	if err := p.expect(token.TokenLBrace); err != nil {
		return nil, err
	}

	mod := &ast.ExternalModule{Pos: pos, Name: name}
	for !p.check(token.TokenRBrace) && !p.atEnd() {
		switch {
		case p.softKeyword("include"):
			p.advance()
			if !p.check(token.TokenString) {
				return nil, p.errorf("expected a header in quotes after 'include', as in include \"<zlib.h>\"")
			}
			mod.Includes = append(mod.Includes, p.current().Value)
			p.advance()

		case p.softKeyword("pkg"):
			p.advance()
			if !p.check(token.TokenString) {
				return nil, p.errorf("expected a library name in quotes after 'pkg', as in pkg \"openssl\"")
			}
			mod.Packages = append(mod.Packages, p.current().Value)
			p.advance()

		case p.softKeyword("type"):
			t, err := p.parseExternalType(name)
			if err != nil {
				return nil, err
			}
			mod.Types = append(mod.Types, *t)

		case p.softKeyword("link"):
			p.advance()
			if !p.check(token.TokenString) {
				return nil, p.errorf("expected a flag in quotes after 'link', as in link \"-lz\"")
			}
			mod.Links = append(mod.Links, p.current().Value)
			p.advance()

		case p.check(token.TokenAnnotation):
			a := p.current().Value
			if a == ast.AnnotOwned {
				return nil, p.errorf("write the return type as 'owned string' rather than #[owned] — #[owned] on a variable already means something else")
			}
			return nil, p.errorf("external module '%s' takes no annotations, and '#[%s]' is not one it knows", name, a)

		case p.check(token.TokenFn), p.check(token.TokenFunction):
			fn, err := p.parseExternalFunc(name)
			if err != nil {
				return nil, err
			}
			mod.Funcs = append(mod.Funcs, *fn)

		default:
			return nil, p.errorf("expected 'fn', 'type', 'include', 'link' or 'pkg' in external module '%s', got %s", name, p.current().Kind)
		}
		p.match(token.TokenSemicolon)
	}

	if err := p.expect(token.TokenRBrace); err != nil {
		return nil, err
	}
	return mod, nil
}

func (p *Parser) parseExternalFunc(moduleName string) (*ast.ExternalFunc, error) {
	pos := p.nodePos()
	p.advance() // consume 'fn'

	name, err := p.expectDeclName("a function name")
	if err != nil {
		return nil, err
	}

	if err := p.expect(token.TokenLParen); err != nil {
		return nil, err
	}

	fn := &ast.ExternalFunc{Pos: pos, Name: name, ReturnType: ast.TypeVoid}
	if !p.check(token.TokenRParen) {
		for {
			// A parameter name is required, so the declaration reads as a
			// signature rather than a list of types.
			pname, err := p.expectDeclName("a parameter name")
			if err != nil {
				return nil, err
			}
			if err := p.expect(token.TokenColon); err != nil {
				return nil, err
			}
			ptype, err := p.parseType()
			if err != nil {
				return nil, err
			}
			fn.ParamNames = append(fn.ParamNames, pname)
			fn.Params = append(fn.Params, ptype)
			if !p.match(token.TokenComma) {
				break
			}
		}
	}

	if err := p.expect(token.TokenRParen); err != nil {
		return nil, err
	}

	if p.match(token.TokenColon) {
		// `owned` says the C function malloc'd what it returned, so Dex frees
		// it. Soft, so `owned` stays usable as an ordinary name.
		if p.softKeyword("owned") {
			p.advance()
			fn.OwnedReturn = true
		}
		fn.ReturnType, err = p.parseType()
		if err != nil {
			return nil, err
		}
		// `freed by "f"` names the library's own deallocator, for a string
		// that plain free must not touch: sqlite3_free, xmlFree, OPENSSL_free.
		if p.softKeyword("freed") {
			p.advance()
			if !p.softKeyword("by") {
				return nil, p.errorf("expected 'by' after 'freed' on %s.%s", moduleName, name)
			}
			p.advance()
			if !p.check(token.TokenString) {
				return nil, p.errorf("expected the freeing function in quotes on %s.%s, as in freed by \"sqlite3_free\"", moduleName, name)
			}
			fn.ReturnFreer = p.current().Value
			p.advance()
		}
	}

	// `= "symbol"` names a C function that differs from the declared name.
	fn.CName = name
	if p.match(token.TokenAssign) {
		if !p.check(token.TokenString) {
			return nil, p.errorf("expected a C symbol in quotes after '=' on %s.%s", moduleName, name)
		}
		fn.CName = p.current().Value
		p.advance()
	}
	return fn, nil
}

// parseExternalType reads `type Name closed by "cfunc"`. The name is registered
// as a type immediately, so the fn declarations below it in the same block can
// use it — the language's declare-before-use rule, which falls out of reading
// the block top to bottom.
func (p *Parser) parseExternalType(moduleName string) (*ast.ExternalType, error) {
	pos := p.nodePos()
	p.advance() // consume 'type'

	name, err := p.expectDeclName("a type name")
	if err != nil {
		return nil, err
	}

	if !p.softKeyword("closed") {
		return nil, p.errorf("expected 'closed by \"<function>\"' after type %s.%s — a handle needs the C function that closes it", moduleName, name)
	}
	p.advance()
	if !p.softKeyword("by") {
		return nil, p.errorf("expected 'by' after 'closed' on type %s.%s", moduleName, name)
	}
	p.advance()
	if !p.check(token.TokenString) {
		return nil, p.errorf("expected the closing function in quotes on type %s.%s, as in closed by \"fclose\"", moduleName, name)
	}
	closer := p.current().Value
	p.advance()

	p.handleNames[name] = ast.HandleTypeOf(moduleName, name, closer)
	return &ast.ExternalType{Pos: pos, Name: name, Closer: closer}, nil
}
