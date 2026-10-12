package codegen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

// Reflection metadata and accessors.
//
// For each struct some reflect.* call named, this emits two tables of string
// literals — the field names and their kinds — and one accessor per kind. An
// accessor is a switch over the field index whose cases are ordinary field
// reads:
//
//	static DexString* _dex_refl_str_User(const Dex_User* s, int i) {
//	    switch (i) {
//	        case 1: return (DexString*)dex_retained(s->email);
//	        case 2: return (DexString*)dex_retained(s->name);
//	        default: break;
//	    }
//	    return dex_string_from_lit("");
//	}
//
// Two things follow from that shape, and they are the reason reflection is
// built this way here rather than from offsetof and casts.
//
// It is type-safe. Only the string-kind fields are cases in the string
// accessor, so asString on a long field falls to the default and returns ""
// rather than reinterpreting eight bytes of integer as a pointer. The C
// compiler checks every case, so a mistake in this file is a build failure
// rather than a corrupt read.
//
// It is as fast as a field access when the index is known. The index is a
// constant at most call sites — reflect.indexOf folds a literal name at compile
// time — and a switch on a constant is the one case it selects. The generated
// code for reflect.asString(unit, reflect.indexOf(unit, "name")) is the same
// load as unit.name, plus the retain that ownership requires.
//
// Reading a string field mints a reference. isNewAlloc treats every call as
// minting, so an accessor that handed back the struct's own pointer unretained
// would have its field released by the consumer while the struct still owned
// it. Retaining is what makes a reflective read obey the same ownership rule as
// every other call in the language.

// reflectKindName is the field kind reflect.fieldKind reports. "other" names a
// field that exists but that reflection cannot render at all — a map, a
// json.Value, an enum — and is distinct from "", which only ever means the
// index was out of range. "char" has no as* accessor of its own but toString
// renders it, so it is named rather than lumped in with "other".
func reflectKindName(t ast.Type) string {
	switch {
	case t == ast.TypeInt:
		return "int"
	case t == ast.TypeLong:
		return "long"
	case t == ast.TypeDouble:
		return "double"
	case t == ast.TypeBool:
		return "bool"
	case t == ast.TypeString:
		return "string"
	case t == ast.TypeChar:
		return "char"
	case ast.IsArrayType(t):
		return "array"
	case ast.IsStructType(t):
		return "struct"
	default:
		return "other"
	}
}

// reflectedStructDefs returns the structs owed metadata, module-provided types
// included, in a stable order so the generated C does not churn between builds.
func (g *Generator) reflectedStructDefs(program *ast.Program) []ast.StructDef {
	var defs []ast.StructDef
	seen := map[string]bool{}
	add := func(sd ast.StructDef) {
		if seen[sd.Name] || !ast.IsReflected(sd.Name) {
			return
		}
		seen[sd.Name] = true
		defs = append(defs, sd)
	}
	modNames := make([]string, 0, len(g.importedModules))
	for name := range g.importedModules {
		modNames = append(modNames, name)
	}
	sort.Strings(modNames)
	for _, name := range modNames {
		for _, sd := range g.importedModules[name].Types {
			add(sd)
		}
	}
	for _, sd := range program.Structs {
		add(sd)
	}
	return defs
}

// emitReflectMetadata writes the tables and accessors for every reflected
// struct. It runs after the module runtimes because the accessors call into
// them, and it emits nothing at all when no reflect.* call named a struct —
// which is what keeps reflection off the bill of a program that does not use
// it.
func (g *Generator) emitReflectMetadata(out *strings.Builder, program *ast.Program) {
	defs := g.reflectedStructDefs(program)
	if len(defs) == 0 {
		return
	}
	out.WriteString("\n// --- reflection metadata (one block per struct a reflect.* call named) ---\n")
	for _, sd := range defs {
		g.emitReflectForStruct(out, sd)
	}
}

func (g *Generator) emitReflectForStruct(out *strings.Builder, sd ast.StructDef) {
	cType := "Dex_" + sd.Name
	n := len(sd.Fields)

	out.WriteString(fmt.Sprintf("\nstatic const char* const _dex_refl_names_%s[%d] = {", sd.Name, max(n, 1)))
	for i, f := range sd.Fields {
		if i > 0 {
			out.WriteString(", ")
		}
		out.WriteString(fmt.Sprintf("%q", f.Name))
	}
	if n == 0 {
		// C forbids a zero-length array, and a struct with no fields still has
		// to answer fieldCount() and indexOf() without a special case.
		out.WriteString("0")
	}
	out.WriteString("};\n")

	out.WriteString(fmt.Sprintf("static const char* const _dex_refl_kinds_%s[%d] = {", sd.Name, max(n, 1)))
	for i, f := range sd.Fields {
		if i > 0 {
			out.WriteString(", ")
		}
		out.WriteString(fmt.Sprintf("%q", reflectKindName(f.Type)))
	}
	if n == 0 {
		out.WriteString("0")
	}
	out.WriteString("};\n")

	// Name and kind lookups. Both bounds-check, so an index computed at runtime
	// cannot read off the end of the table.
	out.WriteString(fmt.Sprintf("static DexString* _dex_refl_name_%s(int i) {\n", sd.Name))
	out.WriteString(fmt.Sprintf("    if (i < 0 || i >= %d) return dex_string_from_lit(\"\");\n", n))
	out.WriteString(fmt.Sprintf("    return dex_string_from_lit(_dex_refl_names_%s[i]);\n}\n", sd.Name))

	out.WriteString(fmt.Sprintf("static DexString* _dex_refl_kind_%s(int i) {\n", sd.Name))
	out.WriteString(fmt.Sprintf("    if (i < 0 || i >= %d) return dex_string_from_lit(\"\");\n", n))
	out.WriteString(fmt.Sprintf("    return dex_string_from_lit(_dex_refl_kinds_%s[i]);\n}\n", sd.Name))

	// Field tags, stored verbatim. A field without one carries "" rather than
	// being absent from the table, so the index still lines up with the others.
	out.WriteString(fmt.Sprintf("static const char* const _dex_refl_tags_%s[%d] = {", sd.Name, max(n, 1)))
	for i, f := range sd.Fields {
		if i > 0 {
			out.WriteString(", ")
		}
		out.WriteString(fmt.Sprintf("%q", f.Tag))
	}
	if n == 0 {
		out.WriteString("0")
	}
	out.WriteString("};\n")

	out.WriteString(fmt.Sprintf("static DexString* _dex_refl_tag_%s(int i, const char* key) {\n", sd.Name))
	out.WriteString(fmt.Sprintf("    if (i < 0 || i >= %d) return dex_string_from_lit(\"\");\n", n))
	out.WriteString("    char buf[512];\n")
	out.WriteString(fmt.Sprintf("    dex_refl_tag(_dex_refl_tags_%s[i], key, buf, sizeof(buf));\n", sd.Name))
	// from_lit copies; from_cstr would free this stack buffer.
	out.WriteString("    return dex_string_from_lit(buf);\n}\n")

	// Typed accessors. Each lists only the fields of its own kind, which is what
	// makes a mismatched read return a zero value instead of garbage.
	g.emitReflectAccessor(out, sd, cType, "int", "int", ast.TypeInt, "0")
	g.emitReflectAccessor(out, sd, cType, "long", "long", ast.TypeLong, "0")
	g.emitReflectAccessor(out, sd, cType, "dbl", "double", ast.TypeDouble, "0.0")
	g.emitReflectAccessor(out, sd, cType, "bool", "_Bool", ast.TypeBool, "0")

	out.WriteString(fmt.Sprintf("static DexString* _dex_refl_str_%s(const %s* s, int i) {\n", sd.Name, cType))
	out.WriteString("    switch (i) {\n")
	for i, f := range sd.Fields {
		if f.Type == ast.TypeString {
			out.WriteString(fmt.Sprintf("        case %d: return (DexString*)dex_retained(s->%s);\n", i, f.Name))
		}
	}
	out.WriteString("        default: break;\n    }\n")
	out.WriteString("    (void)s;\n    return dex_string_from_lit(\"\");\n}\n")

	// Typed writers, the same switch with the assignment the other way round.
	g.emitReflectSetter(out, sd, cType, "int", "int", ast.TypeInt)
	g.emitReflectSetter(out, sd, cType, "long", "long", ast.TypeLong)
	g.emitReflectSetter(out, sd, cType, "dbl", "double", ast.TypeDouble)
	g.emitReflectSetter(out, sd, cType, "bool", "_Bool", ast.TypeBool)
	g.emitReflectStringSetter(out, sd, cType)

	g.emitReflectShow(out, sd, cType)
	g.emitDbFiller(out, sd, cType)
}

// emitDbFiller writes the straight-line row reader behind db.fill: column i
// into field i, one assignment per field, no switch and no loop.
//
// It is straight-line because the field index is a constant at every position,
// which is the same reason a reflective read with a known index costs what a
// field read costs. A field reflection cannot read — an array, a map, a nested
// struct — is skipped, but its column position is still consumed, so the fields
// after it stay aligned with the projection.
//
// A string column mints its reference, so the field's old one is released and
// the new one moves in without a retain.
func (g *Generator) emitDbFiller(out *strings.Builder, sd ast.StructDef, cType string) {
	if g.importedModules["db"] == nil {
		return
	}
	keys := ast.DbFillKeys(sd.Name)
	if len(keys) == 0 {
		keys = []string{"db"}
	}
	for _, key := range keys {
		g.emitDbFillerForKey(out, sd, cType, key)
	}
}

func (g *Generator) emitDbFillerForKey(out *strings.Builder, sd ast.StructDef, cType, key string) {
	out.WriteString(fmt.Sprintf("static void %s(%s* s, int rows) {\n", dbFillerName(sd.Name, key), cType))
	// The column counter is separate from the field index. A field the
	// projection does not select — `db:"-"` — takes no column, so counting
	// fields would read everything after it one position out. This is the one
	// place the compiler reads a tag's meaning rather than just its syntax, and
	// it is the db module reading its own key.
	col := 0
	for _, f := range sd.Fields {
		if v, ok := ast.TagValue(f.Tag, key); ok && v == "-" {
			out.WriteString(fmt.Sprintf("    // %s is tagged %s:\"-\" — not selected, and takes no column\n", f.Name, key))
			continue
		}
		switch f.Type {
		case ast.TypeString:
			out.WriteString(fmt.Sprintf("    { DexString* _v = dex_db_col_dexstr(rows, %d); dex_release(s->%s); s->%s = _v; }\n", col, f.Name, f.Name))
		case ast.TypeLong:
			out.WriteString(fmt.Sprintf("    s->%s = dex_db_col_long(rows, %d);\n", f.Name, col))
		case ast.TypeInt:
			out.WriteString(fmt.Sprintf("    s->%s = dex_db_col_int(rows, %d);\n", f.Name, col))
		case ast.TypeDouble:
			out.WriteString(fmt.Sprintf("    s->%s = dex_db_col_double(rows, %d);\n", f.Name, col))
		case ast.TypeBool:
			out.WriteString(fmt.Sprintf("    s->%s = dex_db_col_bool(rows, %d);\n", f.Name, col))
		default:
			// A kind reflection cannot read still occupies its column: the
			// projection selected one for it, and the caller fills the field.
			out.WriteString(fmt.Sprintf("    // column %d: %s has kind %s — left to the caller\n", col, f.Name, reflectKindName(f.Type)))
		}
		col++
	}
	out.WriteString("    (void)s;\n    (void)rows;\n}\n")
}

// emitReflectSetter writes one value-kind writer. It reports whether it wrote,
// because a setter that silently does nothing is a worse failure than a read
// that returns zero — nothing downstream would ever notice.
func (g *Generator) emitReflectSetter(out *strings.Builder, sd ast.StructDef, cType, suffix, valType string, kind ast.Type) {
	out.WriteString(fmt.Sprintf("static _Bool _dex_refl_set_%s_%s(%s* s, int i, %s v) {\n", suffix, sd.Name, cType, valType))
	out.WriteString("    switch (i) {\n")
	for i, f := range sd.Fields {
		if f.Type == kind {
			out.WriteString(fmt.Sprintf("        case %d: s->%s = v; return 1;\n", i, f.Name))
		}
	}
	out.WriteString("        default: break;\n    }\n")
	out.WriteString("    (void)s;\n    (void)v;\n    return 0;\n}\n")
}

// emitReflectStringSetter writes the one setter with an ownership obligation: a
// struct owns its string fields, so the field's old reference is released and a
// reference to the new value is taken.
//
// Retain before release, not after. The two can be the same object — writing a
// field from a borrowed read of itself — and releasing first could drop the
// last reference to the very value being stored.
func (g *Generator) emitReflectStringSetter(out *strings.Builder, sd ast.StructDef, cType string) {
	out.WriteString(fmt.Sprintf("static _Bool _dex_refl_set_str_%s(%s* s, int i, DexString* v) {\n", sd.Name, cType))
	out.WriteString("    switch (i) {\n")
	for i, f := range sd.Fields {
		if f.Type == ast.TypeString {
			out.WriteString(fmt.Sprintf("        case %d: dex_retain(v); dex_release(s->%s); s->%s = v; return 1;\n", i, f.Name, f.Name))
		}
	}
	out.WriteString("        default: break;\n    }\n")
	out.WriteString("    (void)s;\n    (void)v;\n    return 0;\n}\n")
}

// emitReflectAccessor writes one value-kind accessor: the fields of exactly one
// type become the cases, everything else falls to the zero value.
func (g *Generator) emitReflectAccessor(out *strings.Builder, sd ast.StructDef, cType, suffix, retType string, kind ast.Type, zero string) {
	out.WriteString(fmt.Sprintf("static %s _dex_refl_%s_%s(const %s* s, int i) {\n", retType, suffix, sd.Name, cType))
	out.WriteString("    switch (i) {\n")
	for i, f := range sd.Fields {
		if f.Type == kind {
			out.WriteString(fmt.Sprintf("        case %d: return s->%s;\n", i, f.Name))
		}
	}
	out.WriteString("        default: break;\n    }\n")
	out.WriteString(fmt.Sprintf("    (void)s;\n    return %s;\n}\n", zero))
}

// emitReflectShow writes the any-kind renderer behind reflect.toString. A
// struct or array field has no single-line rendering, so it reports "" rather
// than inventing one — json.encode is the right tool for those.
func (g *Generator) emitReflectShow(out *strings.Builder, sd ast.StructDef, cType string) {
	out.WriteString(fmt.Sprintf("static DexString* _dex_refl_show_%s(const %s* s, int i) {\n", sd.Name, cType))
	// from_lit, not from_cstr: from_cstr takes ownership of a malloc'd C string
	// and frees it, and buf is on the stack.
	out.WriteString("    char buf[64];\n")
	out.WriteString("    switch (i) {\n")
	for i, f := range sd.Fields {
		switch {
		case f.Type == ast.TypeString:
			out.WriteString(fmt.Sprintf("        case %d: return (DexString*)dex_retained(s->%s);\n", i, f.Name))
		case f.Type == ast.TypeInt:
			out.WriteString(fmt.Sprintf("        case %d: snprintf(buf, sizeof(buf), \"%%d\", s->%s); return dex_string_from_lit(buf);\n", i, f.Name))
		case f.Type == ast.TypeLong:
			out.WriteString(fmt.Sprintf("        case %d: snprintf(buf, sizeof(buf), \"%%ld\", s->%s); return dex_string_from_lit(buf);\n", i, f.Name))
		case f.Type == ast.TypeDouble:
			out.WriteString(fmt.Sprintf("        case %d: snprintf(buf, sizeof(buf), \"%%g\", s->%s); return dex_string_from_lit(buf);\n", i, f.Name))
		case f.Type == ast.TypeBool:
			out.WriteString(fmt.Sprintf("        case %d: return dex_string_from_lit(s->%s ? \"true\" : \"false\");\n", i, f.Name))
		case f.Type == ast.TypeChar:
			out.WriteString(fmt.Sprintf("        case %d: buf[0] = (char)s->%s; buf[1] = '\\0'; return dex_string_from_lit(buf);\n", i, f.Name))
		}
	}
	out.WriteString("        default: break;\n    }\n")
	out.WriteString("    (void)s;\n    (void)buf;\n    return dex_string_from_lit(\"\");\n}\n")
}


// dbFillerName is the generated filler for one struct read under one tag key.
// The default key keeps its original name, so nothing that was already emitted
// changes shape.
func dbFillerName(structName, key string) string {
	if key == "db" {
		return "_dex_db_fill_" + structName
	}
	return "_dex_db_fill_" + structName + "_" + key
}
