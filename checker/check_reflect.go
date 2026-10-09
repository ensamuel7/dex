package checker

import (
	"github.com/ensamuel7/dex/ast"
)

// checkReflectCall type-checks the reflect module. Every function there takes a
// struct of any type as its subject, which no FuncDef signature can express, so
// all of them are resolved here.
//
// This is also where reflection metadata is requested: resolving the subject to
// a struct marks that struct as reflected, and codegen emits tables only for
// the ones marked. The checker is the right place for it because it is already
// visiting every call and has already worked out the subject's type — no
// separate traversal has to agree with this one about what counts as a use.
func (c *Checker) checkReflectCall(e *ast.CallExpr) (ast.Type, error) {
	// Arity, by shape: a subject alone, or a subject and a selector.
	wantArgs := 2
	switch e.Name {
	case "typeName", "fieldCount", "fields":
		wantArgs = 1
	}
	if len(e.Args) != wantArgs {
		return 0, c.errAt(e.Pos, "reflect.%s() takes exactly %d argument(s), got %d", e.Name, wantArgs, len(e.Args))
	}

	subject, err := c.checkExpr(e.Args[0])
	if err != nil {
		return 0, err
	}
	// A &T receiver reflects over what it points at, so a helper that takes its
	// subject by reference reads the same as one that takes it by value.
	if ast.IsRefType(subject) {
		subject = ast.RefInnerType(subject)
	}
	if !ast.IsStructType(subject) || ast.IsArrayType(subject) {
		return 0, c.errAt(e.Pos, "reflect.%s() subject must be a struct, got %s", e.Name, typeName(subject))
	}
	// The accessors take the subject by pointer, so it has to be something whose
	// address can be taken. A temporary — a call result, a struct literal — owns
	// its heap fields, and reading one through reflection would leave nobody to
	// release them; binding it to a variable first makes that ownership visible
	// and is the only thing this rules out.
	switch e.Args[0].(type) {
	case *ast.Ident, *ast.FieldAccessExpr, *ast.IndexExpr:
	default:
		return 0, c.errAt(e.Pos, "reflect.%s() subject must be a variable, field or element, not a temporary — assign it to a variable first", e.Name)
	}

	def := ast.GetStructDef(subject)
	ast.MarkReflected(def.Name)

	if wantArgs == 1 {
		switch e.Name {
		case "typeName":
			return ast.TypeString, nil
		case "fieldCount":
			return ast.TypeInt, nil
		case "fields":
			return ast.TypeArrayString, nil
		}
	}

	// The selector is a name for the lookups and an index for the accessors.
	selector, err := c.checkExpr(e.Args[1])
	if err != nil {
		return 0, err
	}
	switch e.Name {
	case "indexOf", "has":
		if selector != ast.TypeString {
			return 0, c.errAt(e.Pos, "reflect.%s() field name must be a string, got %s", e.Name, typeName(selector))
		}
		if e.Name == "has" {
			return ast.TypeBool, nil
		}
		return ast.TypeInt, nil
	}

	if selector != ast.TypeInt {
		return 0, c.errAt(e.Pos, "reflect.%s() field index must be an int, got %s — use reflect.indexOf() to turn a name into one", e.Name, typeName(selector))
	}
	// A literal index is checked here rather than left to return a zero value at
	// runtime: it cannot become valid later, so it is a mistake in the source.
	if lit, isLit := e.Args[1].(*ast.IntLit); isLit {
		if lit.Value < 0 || int(lit.Value) >= len(def.Fields) {
			return 0, c.errAt(e.Pos, "field index %d is out of range: %s has %d field(s)", lit.Value, def.Name, len(def.Fields))
		}
	}

	switch e.Name {
	case "fieldName", "fieldKind", "asString", "toString":
		return ast.TypeString, nil
	case "asInt":
		return ast.TypeInt, nil
	case "asLong":
		return ast.TypeLong, nil
	case "asDouble":
		return ast.TypeDouble, nil
	case "asBool":
		return ast.TypeBool, nil
	}
	return 0, c.errAt(e.Pos, "undefined function '%s' in module 'reflect'", e.Name)
}
