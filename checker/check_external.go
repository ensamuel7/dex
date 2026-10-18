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
	return false
}

// checkExternals validates every external module declared in the program. A
// declaration that reaches codegen unchecked is a C compile error at best and
// a corrupted heap at worst, so everything is rejected here by name.
func (c *Checker) checkExternals(program *ast.Program) {
	seen := map[string]bool{}
	for i := range program.Externals {
		em := &program.Externals[i]

		if seen[em.Name] {
			c.addError(c.errAt(em.Pos, "external module '%s' is declared twice", em.Name))
			continue
		}
		seen[em.Name] = true

		// A name the stdlib already provides would be ambiguous at every call
		// site, and which one won would depend on lookup order.
		if stdlib.Lookup(em.Name) != nil {
			c.addError(c.errAt(em.Pos, "external module '%s' shadows the stdlib module of the same name — give it another name", em.Name))
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

		fnSeen := map[string]bool{}
		for _, f := range em.Funcs {
			if fnSeen[f.Name] {
				c.addError(c.errAt(f.Pos, "external module '%s' declares '%s' twice", em.Name, f.Name))
				continue
			}
			fnSeen[f.Name] = true

			for j, pt := range f.Params {
				if !externalTypeSupported(pt) {
					c.addError(c.errAt(f.Pos, "%s.%s() parameter '%s' is %s, which cannot cross the C boundary yet — only int, long, double, bool, char and string can",
						em.Name, f.Name, f.ParamNames[j], typeName(pt)))
				}
			}
			for _, a := range f.Annotations {
				if a != ast.AnnotOwned {
					c.addError(c.errAt(f.Pos, "unknown annotation '#[%s]' on %s.%s() — an external declaration takes only #[owned]", a, em.Name, f.Name))
				}
			}
			// #[owned] says the callee malloc'd the string it returned, so it
			// means nothing anywhere else.
			if f.OwnedReturn && f.ReturnType != ast.TypeString {
				c.addError(c.errAt(f.Pos, "#[owned] on %s.%s() needs a string return, not %s — it says the C function malloc'd the string and Dex should free it",
					em.Name, f.Name, typeName(f.ReturnType)))
			}

			if f.ReturnType != ast.TypeVoid && !externalTypeSupported(f.ReturnType) {
				c.addError(c.errAt(f.Pos, "%s.%s() returns %s, which cannot cross the C boundary yet — only int, long, double, bool, char and string can",
					em.Name, f.Name, typeName(f.ReturnType)))
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
