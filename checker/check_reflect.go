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
	case "fieldTag", "setInt", "setLong", "setDouble", "setBool", "setString":
		wantArgs = 3
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
	if _, isSetter := reflectSetterKind(e.Name); isSetter {
		if err := c.checkReflectSetterSubject(e); err != nil {
			return 0, err
		}
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
	// fieldTags selects by key, not by index, so it is checked here beside the
	// other name-selected lookups rather than falling through to the index rules.
	if e.Name == "fieldTags" {
		if selector != ast.TypeString {
			return 0, c.errAt(e.Pos, "reflect.fieldTags() key must be a string, got %s", typeName(selector))
		}
		return ast.TypeArrayString, nil
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

	if e.Name == "fieldTag" {
		keyType, err := c.checkExpr(e.Args[2])
		if err != nil {
			return 0, err
		}
		if keyType != ast.TypeString {
			return 0, c.errAt(e.Pos, "reflect.fieldTag() key must be a string, got %s", typeName(keyType))
		}
		return ast.TypeString, nil
	}

	if want, isSetter := reflectSetterKind(e.Name); isSetter {
		valType, err := c.checkExpr(e.Args[2])
		if err != nil {
			return 0, err
		}
		// canAssign rather than equality, so an int literal widens into a long
		// or double field the way it does at every other call in the language.
		if !canAssign(want, valType, e.Args[2]) {
			return 0, c.errAt(e.Pos, "reflect.%s() value must be %s, got %s", e.Name, typeName(want), typeName(valType))
		}
		return ast.TypeBool, nil
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

// reflectSetterKind is the field type a set* function writes. The second result
// reports whether the name is a setter at all — it cannot be folded into a zero
// type, because ast.TypeInt is itself 0.
func reflectSetterKind(name string) (ast.Type, bool) {
	switch name {
	case "setInt":
		return ast.TypeInt, true
	case "setLong":
		return ast.TypeLong, true
	case "setDouble":
		return ast.TypeDouble, true
	case "setBool":
		return ast.TypeBool, true
	case "setString":
		return ast.TypeString, true
	}
	return 0, false
}

// checkReflectSetterSubject enforces what a *write* needs beyond what a read
// needs. A getter only has to be able to take the subject's address; a setter
// changes it, and two otherwise-addressable subjects are wrong to change.
//
// An element, because the language has no way to mutate a struct array element
// in place — `ps[0].n = 7` is a parse error — and reflection should not be the
// one back door into it. The accessors reach an element by copying it, which a
// read can do safely and a write cannot.
//
// A by-value struct parameter, because it is a copy that *shares* the caller's
// heap pointers without owning them. Writing a string field through it releases
// a reference the caller still holds, which is a use-after-free rather than
// merely a lost update; and even for a value field the caller never sees the
// write, so it is a bug either way. `&Struct` is the form that means what the
// caller wanted.
func (c *Checker) checkReflectSetterSubject(e *ast.CallExpr) error {
	if _, isIndex := e.Args[0].(*ast.IndexExpr); isIndex {
		return c.errAt(e.Pos, "reflect.%s() cannot write through an array element — bind it to a variable, write that, and put it back", e.Name)
	}
	return c.checkWritableStructSubject(e.Args[0], e.Pos, "reflect."+e.Name+"()")
}

// checkWritableStructSubject is the shared half: whatever is about to be
// written through must be something the write will actually be seen in, and
// something whose string fields this scope owns. Shared with db.fill, which
// assigns to a struct's fields exactly as a setter does.
func (c *Checker) checkWritableStructSubject(subject ast.Expr, pos ast.Pos, what string) error {
	root := reflectSubjectRoot(subject)
	if root == "" {
		return nil
	}
	if c.isConst(root) {
		return c.errAt(pos, "cannot modify const variable '%s' through %s", root, what)
	}
	if paramType, isValueParam := c.valueParams[root]; isValueParam {
		return c.errAt(pos, "%s cannot write to '%s': a by-value struct parameter is a copy that shares the caller's string references, so writing one would release a reference the caller still holds — take it as &%s instead", what, root, typeName(paramType))
	}
	return nil
}

// reflectSubjectRoot is the variable a subject expression ultimately reads
// from, so that `outer.inner` is judged by what `outer` is.
func reflectSubjectRoot(e ast.Expr) string {
	for {
		switch v := e.(type) {
		case *ast.Ident:
			return v.Name
		case *ast.FieldAccessExpr:
			e = v.Object
		default:
			return ""
		}
	}
}

// checkDbFillCall type-checks db.fill(into, rows).
//
// It lives beside the reflection checks because it is the same kind of call —
// polymorphic in a struct subject, resolved here rather than by a FuncDef — and
// because it writes through that subject, so it is held to exactly the rules a
// reflect setter is held to. Those rules are not about reflection; they are
// about what it is sound to write through, and the generated filler assigns to
// the subject's fields the same way a setter does.
func (c *Checker) checkDbFillCall(e *ast.CallExpr) (ast.Type, error) {
	if len(e.Args) != 2 && len(e.Args) != 3 {
		return 0, c.errAt(e.Pos, "db.fill() takes 2 or 3 arguments, got %d", len(e.Args))
	}
	subject, err := c.checkExpr(e.Args[0])
	if err != nil {
		return 0, err
	}
	if ast.IsRefType(subject) {
		subject = ast.RefInnerType(subject)
	}
	if !ast.IsStructType(subject) || ast.IsArrayType(subject) {
		return 0, c.errAt(e.Pos, "db.fill() must fill a struct, got %s", typeName(subject))
	}
	switch e.Args[0].(type) {
	case *ast.Ident, *ast.FieldAccessExpr:
	default:
		return 0, c.errAt(e.Pos, "db.fill() must fill a variable or a field, not a temporary — assign it to a variable first")
	}
	if err := c.checkWritableStructSubject(e.Args[0], e.Pos, "db.fill()"); err != nil {
		return 0, err
	}
	rowsType, err := c.checkExpr(e.Args[1])
	if err != nil {
		return 0, err
	}
	if rowsType != ast.TypeInt {
		return 0, c.errAt(e.Pos, "db.fill() result handle must be int, got %s", typeName(rowsType))
	}
	// Which tag key marks the fields this projection leaves out. A struct read
	// by two projections needs a filler per key, so the key has to be known
	// while compiling — a literal, not an expression.
	key := "db"
	if len(e.Args) == 3 {
		lit, isLit := e.Args[2].(*ast.StringLit)
		if !isLit {
			return 0, c.errAt(e.Pos, "db.fill() tag key must be a string literal, because the filler is generated per key")
		}
		if lit.Value == "" {
			return 0, c.errAt(e.Pos, "db.fill() tag key cannot be empty")
		}
		key = lit.Value
	}
	// The generated filler is emitted per struct alongside the reflection
	// metadata, so the struct has to be marked the same way a reflect.* call
	// marks it.
	name := ast.GetStructDef(subject).Name
	ast.MarkReflected(name)
	ast.MarkDbFilled(name, key)
	e.ResolvedKey = key
	return ast.TypeVoid, nil
}
