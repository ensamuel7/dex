package checker

import (
	"strings"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/stdlib"
)

// externalTypeSupported reports whether a type can cross the C boundary: the
// scalars, plus string, which crosses as a NUL-terminated const char*. Bytes
// that may contain a NUL cross as a pointer and a length, the way the C
// functions that take them are already written, so a string parameter needs no
// special marking.
//
// The refusal is here rather than left to the C compiler because the generated
// call would otherwise be a diagnostic about Dex_Person and int rather than
// about the program, which is the mistake mapKeyTypeSupported exists to avoid.
func externalTypeSupported(t ast.Type) bool {
	switch t {
	case ast.TypeInt, ast.TypeLong, ast.TypeDouble, ast.TypeBool, ast.TypeChar, ast.TypeString:
		return true
	}
	// An opaque handle crosses as the C pointer inside its box. An optional one
	// crosses too: NULL is its absent value, which is what the C function
	// returning it already means by NULL.
	if ast.IsHandleType(t) {
		return true
	}
	if ast.IsOptionalType(t) && ast.IsHandleType(ast.OptionalInnerType(t)) {
		return true
	}
	return false
}

// externalOutTypeSupported reports what a C function may write through an
// out-parameter: a scalar, whose address crosses directly, or a handle, which C
// writes as a raw pointer and Dex then boxes. A string would need to know who
// allocated what C wrote, which a declaration has no way to say yet.
func externalOutTypeSupported(t ast.Type) bool {
	switch t {
	case ast.TypeInt, ast.TypeLong, ast.TypeDouble, ast.TypeBool, ast.TypeChar:
		return true
	}
	return ast.IsHandleType(t) || (ast.IsOptionalType(t) && ast.IsHandleType(ast.OptionalInnerType(t)))
}

// checkExternals validates every external module declared in the program. A
// declaration that reaches codegen unchecked is a C compile error at best and
// a corrupted heap at worst, so everything is rejected here by name.
func (c *Checker) checkExternals(program *ast.Program) {
	seen := map[string]bool{}
	for i := range program.Externals {
		em := &program.Externals[i]
		// The name written, not the one a module's block is scoped under.
		shown := em.Name
		if em.Local != "" {
			shown = em.Local
		}

		if seen[em.Name] {
			c.addError(c.errAt(em.Pos, "external module '%s' is declared twice", shown))
			continue
		}
		seen[em.Name] = true

		// A name the stdlib already provides would be ambiguous at every call
		// site, and which one won would depend on lookup order.
		if stdlib.Lookup(shown) != nil {
			c.addError(c.errAt(em.Pos, "external module '%s' shadows the stdlib module of the same name — give it another name", shown))
		}

		for _, h := range em.Includes {
			if !validInclude(h) {
				c.addError(c.errAt(em.Pos, "include %q is not a header name — write include \"<zlib.h>\" for a system header or include \"local.h\" for one beside the source", h))
			}
		}
		for _, l := range em.Links {
			if !validLinkFlag(l) {
				c.addError(c.errAt(em.Pos, "link %q is not allowed: only -l<name>, -L<path> and a bare library name are, so that a module cannot pass arbitrary flags to the C compiler", l))
			}
		}

		for _, name := range em.Packages {
			if !stdlib.ValidPkgName(name) {
				c.addError(c.errAt(em.Pos, "pkg %q is not a library name", name))
				continue
			}
			if _, err := stdlib.PkgConfig(name); err != nil {
				c.addError(c.errAt(em.Pos, "%v", err))
			}
		}

		typeSeen := map[string]bool{}
		for _, t := range em.Types {
			if typeSeen[t.Name] {
				c.addError(c.errAt(t.Pos, "external module '%s' declares type '%s' twice", shown, t.Name))
				continue
			}
			typeSeen[t.Name] = true
			if t.Closer == "" {
				c.addError(c.errAt(t.Pos, "type %s.%s needs a closing function, as in closed by \"fclose\"", shown, t.Name))
			} else if !validCSymbol(t.Closer) {
				c.addError(c.errAt(t.Pos, "closed by %q on type %s.%s is not a C identifier", t.Closer, shown, t.Name))
			}
			if _, taken := ast.LookupStructType(t.Name); taken {
				c.addError(c.errAt(t.Pos, "external type '%s' collides with a struct of the same name", t.Name))
			}
		}

		fnSeen := map[string]bool{}
		for _, f := range em.Funcs {
			if fnSeen[f.Name] {
				c.addError(c.errAt(f.Pos, "external module '%s' declares '%s' twice", shown, f.Name))
				continue
			}
			fnSeen[f.Name] = true

			if !validCSymbol(f.CName) {
				c.addError(c.errAt(f.Pos, "%q on %s.%s() is not a C identifier", f.CName, shown, f.Name))
			}

			for j, pt := range f.Params {
				if ast.IsRefType(pt) {
					if !externalOutTypeSupported(ast.RefInnerType(pt)) {
						c.addError(c.errAt(f.Pos, "%s.%s() parameter '%s' is %s, which cannot be written by C — an out-parameter is &int, &long, &double, &bool, &char or a handle",
							shown, f.Name, f.ParamNames[j], typeName(pt)))
					}
					continue
				}
				if !externalTypeSupported(pt) {
					c.addError(c.errAt(f.Pos, "%s.%s() parameter '%s' is %s, which cannot cross the C boundary yet — only int, long, double, bool, char and string can",
						shown, f.Name, f.ParamNames[j], typeName(pt)))
				}
			}
			// `owned` says the callee malloc'd the string it returned, so it
			// means nothing on any other type.
			if f.OwnedReturn && f.ReturnType != ast.TypeString {
				c.addError(c.errAt(f.Pos, "'owned' on %s.%s() needs a string return, not %s — it says the C function malloc'd the string and Dex should free it",
					shown, f.Name, typeName(f.ReturnType)))
			}
			if f.ReturnFreer != "" && !f.OwnedReturn {
				c.addError(c.errAt(f.Pos, "'freed by' on %s.%s() needs an 'owned string' return — without 'owned' Dex never frees the string, so there is nothing to free it with",
					shown, f.Name))
			} else if f.ReturnFreer != "" && !validCSymbol(f.ReturnFreer) {
				c.addError(c.errAt(f.Pos, "freed by %q on %s.%s() is not a C identifier", f.ReturnFreer, shown, f.Name))
			}

			if f.ReturnType != ast.TypeVoid && !externalTypeSupported(f.ReturnType) {
				c.addError(c.errAt(f.Pos, "%s.%s() returns %s, which cannot cross the C boundary yet — only int, long, double, bool, char and string can",
					shown, f.Name, typeName(f.ReturnType)))
			}
		}
	}
}

// validInclude accepts a system header in angle brackets, an explicitly
// quoted one, or a bare name — which becomes a quoted include, so a header
// beside the source needs no escaped quotes to name.
func validInclude(h string) bool {
	if len(h) < 3 || strings.ContainsAny(h, "\n\\ \t;&|$`'()") || strings.Contains(h, "..") {
		return false
	}
	if h[0] == '<' {
		return h[len(h)-1] == '>'
	}
	if h[0] == '"' {
		return h[len(h)-1] == '"'
	}
	return !strings.ContainsAny(h, "<>\"")
}

// validLinkFlag keeps an external block from handing the C compiler whatever it
// likes. A module fetched from somewhere else would otherwise be arbitrary
// code execution at build time, and that is much harder to take back than to
// refuse now.
func validLinkFlag(l string) bool {
	if l == "" || strings.ContainsAny(l, " \t\n\"'$`;&|<>()") {
		return false
	}
	switch {
	case strings.HasPrefix(l, "-l"):
		return len(l) > 2
	case strings.HasPrefix(l, "-L"):
		return len(l) > 2 && !strings.Contains(l, "..")
	case strings.HasPrefix(l, "-"):
		return false
	}
	return true
}

// validCSymbol accepts a C identifier and nothing else. A symbol name is pasted
// into the generated C as written, so anything wider would let a module fetched
// from somewhere else run code at build time — the hole validLinkFlag closes
// for flags.
func validCSymbol(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// checkOutArg checks an argument a C function writes through. It has to be a
// variable, because there is nowhere else for the write to land, and of exactly
// the referenced type: C writes the representation it was declared with, so an
// int that widened to a long would be half written. A handle has to be the
// optional spelling when the declaration is, since C may write NULL — and a
// variable narrowed to the non-optional type inside a null check would then
// hold null where the checker had promised it could not.
func (c *Checker) checkOutArg(e *ast.CallExpr, i int, param ast.Type) error {
	want := ast.RefInnerType(param)
	// null passes NULL, which is how a C function is told to skip an output
	// the caller does not want — sqlite3_prepare_v2's pzTail, say.
	if _, isNull := e.Args[i].(*ast.NullLit); isNull {
		return nil
	}
	id, ok := e.Args[i].(*ast.Ident)
	if !ok {
		return c.errAt(e.Pos, "%s.%s() argument %d is written by the C function, so it must be a variable of type %s",
			e.Module, e.Name, i+1, typeName(want))
	}
	if c.isConst(id.Name) {
		return c.errAt(e.Pos, "%s.%s() argument %d is written by the C function, and '%s' is a const",
			e.Module, e.Name, i+1, id.Name)
	}
	got, ok := c.resolve(id.Name)
	if !ok {
		return c.errAt(e.Pos, "undefined variable '%s'", id.Name)
	}
	if got != want && got != param {
		return c.errAt(e.Pos, "%s.%s() argument %d is written by the C function as %s, but '%s' is %s",
			e.Module, e.Name, i+1, typeName(want), id.Name, typeName(got))
	}
	return nil
}
