package codegen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

// genReflectCall emits the reflect module. Reports whether it handled the call.
//
// The emission here is mostly a dispatch to the accessors gen_reflect.go wrote
// for the subject's struct. What it adds is constant folding: a field count, a
// type name and a literal field name are all known while compiling, so they
// become literals in the C rather than lookups at runtime. That is what lets a
// library be written against reflection and still compile to direct field
// access at the places where the field is actually known.
func (g *Generator) genReflectCall(out *strings.Builder, e *ast.CallExpr) bool {
	if e.Module != "reflect" {
		return false
	}
	// typeName, fieldCount, fields, fieldName, fieldKind, indexOf and has answer
	// from the subject's *type* and never read the value, so nothing else here
	// would emit the subject expression at all — and a subject that does work,
	// like fleet[next()], would simply never run. Evaluating and discarding it
	// first means reflection evaluates its subject the way every other
	// expression in the language does, rather than being the one place a call
	// disappears. A plain variable cannot do work, so it is left alone.
	//
	// Note this inherits an existing wrinkle rather than fixing it: an indexed
	// access emits its index expression twice, once for the bounds check and
	// once for the access, so fleet[next()] calls next() twice here exactly as
	// it does in fleet[next()].tag. That is the index codegen's behaviour, not
	// reflection's.
	if _, isIdent := e.Args[0].(*ast.Ident); !isIdent {
		switch e.Name {
		case "typeName", "fieldCount", "fields", "fieldName", "fieldKind", "fieldTag", "fieldTags", "indexOf", "has":
			out.WriteString("(((void)(")
			g.genExpr(out, e.Args[0])
			out.WriteString(")), ")
			handled := g.genReflectCallBody(out, e)
			out.WriteString(")")
			return handled
		}
	}
	return g.genReflectCallBody(out, e)
}

func (g *Generator) genReflectCallBody(out *strings.Builder, e *ast.CallExpr) bool {
	subject := e.Args[0]
	subjectType := g.typeOfExpr(subject)
	if ast.IsRefType(subjectType) {
		subjectType = ast.RefInnerType(subjectType)
	}
	def := ast.GetStructDef(subjectType)
	n := len(def.Fields)

	switch e.Name {
	// --- Known entirely at compile time -------------------------------------
	case "typeName":
		out.WriteString(fmt.Sprintf("dex_string_from_lit(%q)", def.Name))
		return true
	case "fieldCount":
		out.WriteString(strconv.Itoa(n))
		return true

	case "indexOf", "has":
		// A literal name resolves now. This is the path that makes a reflective
		// read cost what a field read costs: the index becomes a constant, and
		// the accessor's switch collapses to the one case it selects.
		if lit, isLit := e.Args[1].(*ast.StringLit); isLit {
			idx := reflectFieldIndex(def, lit.Value)
			if e.Name == "has" {
				out.WriteString(boolLiteral(idx >= 0))
			} else {
				out.WriteString(strconv.Itoa(idx))
			}
			return true
		}
		// A name computed at runtime is the one case that has to search.
		var lookup strings.Builder
		lookup.WriteString("dex_refl_index_of((")
		g.genBorrowed(&lookup, e.Args[1])
		lookup.WriteString(fmt.Sprintf(")->data, %d, _dex_refl_names_%s)", n, def.Name))
		if e.Name == "has" {
			out.WriteString("((" + lookup.String() + ") >= 0)")
		} else {
			out.WriteString(lookup.String())
		}
		return true

	// --- Table lookups: no subject needed, only its type --------------------
	case "fieldName":
		out.WriteString(fmt.Sprintf("_dex_refl_name_%s(", def.Name))
		g.genExpr(out, e.Args[1])
		out.WriteString(")")
		return true
	case "fieldKind":
		out.WriteString(fmt.Sprintf("_dex_refl_kind_%s(", def.Name))
		g.genExpr(out, e.Args[1])
		out.WriteString(")")
		return true
	case "fieldTag":
		out.WriteString(fmt.Sprintf("_dex_refl_tag_%s(", def.Name))
		g.genExpr(out, e.Args[1])
		out.WriteString(", ")
		// The key is read only, so a minted one is released after the statement.
		if lit, isLit := e.Args[2].(*ast.StringLit); isLit {
			out.WriteString(fmt.Sprintf("%q", lit.Value))
		} else {
			out.WriteString("(")
			g.genBorrowed(out, e.Args[2])
			out.WriteString(")->data")
		}
		out.WriteString(")")
		return true

	case "fields":
		g.genReflectFields(out, def)
		return true
	case "fieldTags":
		g.genReflectFieldTags(out, def, e.Args[1])
		return true
	}

	// --- Typed writers ------------------------------------------------------
	if suffix := reflectSetterSuffix(e.Name); suffix != "" {
		out.WriteString(fmt.Sprintf("_dex_refl_set_%s_%s(", suffix, def.Name))
		g.genReflectMutableSubject(out, subject)
		out.WriteString(", ")
		g.genExpr(out, e.Args[1])
		out.WriteString(", ")
		if e.Name == "setString" {
			// The setter takes its own reference, so a value built for the call
			// is released after the statement rather than being handed over —
			// the same convention dex_array_string_push follows.
			g.genBorrowed(out, e.Args[2])
		} else {
			g.genExpr(out, e.Args[2])
		}
		out.WriteString(")")
		return true
	}

	// --- Typed accessors ----------------------------------------------------
	accessor := map[string]string{
		"asInt":    "int",
		"asLong":   "long",
		"asDouble": "dbl",
		"asBool":   "bool",
		"asString": "str",
		"toString": "show",
	}[e.Name]
	if accessor == "" {
		return false
	}
	out.WriteString(fmt.Sprintf("_dex_refl_%s_%s(", accessor, def.Name))
	g.genReflectSubject(out, subject)
	out.WriteString(", ")
	g.genExpr(out, e.Args[1])
	out.WriteString(")")
	return true
}

// genReflectSubject emits the subject as a pointer. A &T is already one; a
// struct value has its address taken, which the checker guarantees is legal by
// requiring the subject be a variable, field or element rather than a
// temporary.
func (g *Generator) genReflectSubject(out *strings.Builder, subject ast.Expr) {
	if ast.IsRefType(g.typeOfExpr(subject)) {
		g.genExpr(out, subject)
		return
	}
	if _, isIndex := subject.(*ast.IndexExpr); isIndex {
		// An indexed struct element compiles to a bounds check and a deref
		// joined by a comma, and C says the result of a comma operator is not an
		// lvalue — so &(fleet[0]) does not build. The element is copied into a
		// local instead and that is what gets addressed.
		//
		// The copy is safe to leave uncleaned precisely because it is shallow:
		// it shares the element's heap pointers rather than owning them, and the
		// accessors retain whatever they hand back. Nothing here adjusts a
		// refcount, so there is nothing to undo. That is also why the checker
		// still refuses a call result — that struct owns its fields, and a copy
		// of it would leave them with no owner.
		tmp := g.nextTemp()
		out.WriteString(fmt.Sprintf("({ %s %s = ", g.cType(g.typeOfExpr(subject)), tmp))
		g.genExpr(out, subject)
		out.WriteString(fmt.Sprintf("; &%s; })", tmp))
		return
	}
	out.WriteString("&(")
	g.genExpr(out, subject)
	out.WriteString(")")
}

// genReflectMutableSubject emits a setter's subject as a writable pointer.
//
// It is genReflectSubject without the indexed-element case, which copies the
// element and addresses the copy: a read can do that safely because it only
// reads shared pointers, but a write would land in the copy and be discarded.
// The checker rejects an element subject for a setter so that path is
// unreachable here rather than silently wrong.
func (g *Generator) genReflectMutableSubject(out *strings.Builder, subject ast.Expr) {
	if ast.IsRefType(g.typeOfExpr(subject)) {
		g.genExpr(out, subject)
		return
	}
	out.WriteString("&(")
	g.genExpr(out, subject)
	out.WriteString(")")
}

// reflectSetterSuffix is the accessor suffix a set* function writes through, or
// "" for anything that is not a setter.
func reflectSetterSuffix(name string) string {
	switch name {
	case "setInt":
		return "int"
	case "setLong":
		return "long"
	case "setDouble":
		return "dbl"
	case "setBool":
		return "bool"
	case "setString":
		return "str"
	}
	return ""
}

// genReflectFields builds the field-name array. The names are literals, so this
// is a fixed sequence of pushes rather than a loop over the table.
func (g *Generator) genReflectFields(out *strings.Builder, def *ast.StructDef) {
	tmp := g.nextTemp()
	out.WriteString(fmt.Sprintf("({ DexArrayString* %s = dex_array_string_new(); ", tmp))
	for _, f := range def.Fields {
		// push retains what it is handed, and from_lit minted a reference, so
		// the literal is released afterwards — the container owns its elements.
		out.WriteString(fmt.Sprintf("{ DexString* _e = dex_string_from_lit(%q); dex_array_string_push(%s, _e); dex_release(_e); } ", f.Name, tmp))
	}
	out.WriteString(fmt.Sprintf("%s; })", tmp))
}

// genReflectFieldTags builds the array of every field's tag value for one key.
//
// This is the piece that lets a library be written against struct metadata
// without the language having generics. A function cannot take "any struct" as
// a parameter, so the generic half of such a library cannot live in a user
// module at all — but reflect.fields() and reflect.fieldTags() hand it two
// plain string arrays, and a function over two string arrays needs no
// polymorphism. The caller names the struct once, at the only place that knows
// which struct it is.
//
// The key is read once into a local rather than re-emitted per field: it may be
// an expression, and a tag table of thirty fields would otherwise evaluate it
// thirty times.
func (g *Generator) genReflectFieldTags(out *strings.Builder, def *ast.StructDef, key ast.Expr) {
	arr := g.nextTemp()
	keyVar := g.nextTemp()
	out.WriteString("({ const char* " + keyVar + " = ")
	if lit, isLit := key.(*ast.StringLit); isLit {
		out.WriteString(fmt.Sprintf("%q", lit.Value))
	} else {
		out.WriteString("(")
		g.genBorrowed(out, key)
		out.WriteString(")->data")
	}
	out.WriteString(fmt.Sprintf("; DexArrayString* %s = dex_array_string_new(); ", arr))
	for i := range def.Fields {
		// push retains what it is handed and the lookup minted a reference, so
		// the lookup's reference is released afterwards.
		out.WriteString(fmt.Sprintf("{ DexString* _e = _dex_refl_tag_%s(%d, %s); dex_array_string_push(%s, _e); dex_release(_e); } ",
			def.Name, i, keyVar, arr))
	}
	out.WriteString(fmt.Sprintf("%s; })", arr))
}

// reflectFieldIndex is the declaration-order position of a named field, or -1.
func reflectFieldIndex(def *ast.StructDef, name string) int {
	for i, f := range def.Fields {
		if f.Name == name {
			return i
		}
	}
	return -1
}

func boolLiteral(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
