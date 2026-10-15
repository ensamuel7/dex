package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

// A payload-carrying enum is a refcounted tagged union reached through a
// pointer. The pointer is what makes a recursive variant representable at all:
// `Add(Expr, Expr)` has no finite size as a value, and boxing it is the same
// answer Rust reaches for with Box.
//
// The representation is deliberately one pointer rather than the three words
// an interface value costs, and dispatch is a switch on the tag rather than an
// indirect call through a vtable. That is the whole reason this exists.
//
// Ordering: the forward declaration and the tag constants are emitted with the
// plain enums, before the struct typedefs, because a struct may hold one as a
// field. The definition below has to come after them, because a variant may
// carry a struct by value.

func (g *Generator) enumTagName(enumName, variant string) string {
	return fmt.Sprintf("Dex_%s_%s", enumName, variant)
}

func enumElemCleanupName(enumName string) string {
	return "dex_cleanup_enum_" + enumName
}

func (g *Generator) enumCtorName(enumName, variant string) string {
	return fmt.Sprintf("dex_enum_%s_%s", enumName, variant)
}

// genPayloadEnums writes the storage, destructor and constructors for every
// enum that carries a payload.
func (g *Generator) genPayloadEnums(out *strings.Builder, program *ast.Program) {
	for _, ed := range program.Enums {
		if !ed.HasPayload() {
			continue
		}
		g.genPayloadEnumDef(out, &ed)
	}
}

func (g *Generator) genPayloadEnumDef(out *strings.Builder, ed *ast.EnumDef) {
	name := ed.Name

	out.WriteString(fmt.Sprintf("struct Dex_%s {\n", name))
	out.WriteString("    DexObjHeader header;\n")
	out.WriteString("    int tag;\n")
	out.WriteString("    union {\n")
	for i, v := range ed.Variants {
		fields := ed.Payloads[i]
		if len(fields) == 0 {
			continue
		}
		out.WriteString("        struct { ")
		for j, ft := range fields {
			out.WriteString(fmt.Sprintf("%s f%d; ", g.cType(ft), j))
		}
		out.WriteString(fmt.Sprintf("} v_%s;\n", v))
	}
	// A union with no members is not C. An enum reaches this function only
	// when some variant carries fields, so this is unreachable in practice;
	// the placeholder keeps the generated code valid regardless.
	out.WriteString("        char _empty;\n")
	out.WriteString("    } u;\n")
	out.WriteString("};\n")

	// The destructor releases whatever the live variant owns, and nothing
	// else: reading another variant's members would be reading the wrong
	// union arm.
	out.WriteString(fmt.Sprintf("static void dex_enum_%s_destroy(void* _ptr) {\n", name))
	out.WriteString(fmt.Sprintf("    Dex_%s* _e = (Dex_%s*)_ptr;\n", name, name))
	out.WriteString("    switch (_e->tag) {\n")
	wrote := false
	for i, v := range ed.Variants {
		fields := ed.Payloads[i]
		var rel strings.Builder
		for j, ft := range fields {
			if !ast.NeedsRelease(ft) {
				continue
			}
			g.emitReleaseVar(&rel, "            ", fmt.Sprintf("_e->u.v_%s.f%d", v, j), ft)
		}
		if rel.Len() == 0 {
			continue
		}
		wrote = true
		out.WriteString(fmt.Sprintf("        case %s: {\n", g.enumTagName(name, v)))
		out.WriteString(rel.String())
		out.WriteString("            break;\n        }\n")
	}
	if !wrote {
		out.WriteString("        default: break;\n")
	} else {
		out.WriteString("        default: break;\n")
	}
	out.WriteString("    }\n}\n")

	// An element of an array of these is a pointer the array owns, so the
	// array's cleanup releases it. Same shape as dex_cleanup_iface_*.
	out.WriteString(fmt.Sprintf("static void %s(void* _ptr) { dex_release(*(Dex_%s**)_ptr); }\n",
		enumElemCleanupName(name), name))

	// One constructor per variant. The arguments arrive borrowed, the same
	// convention a struct literal's fields follow, so each heap field is
	// retained into the box and released by the destructor above.
	for i, v := range ed.Variants {
		fields := ed.Payloads[i]
		out.WriteString(fmt.Sprintf("static Dex_%s* %s(", name, g.enumCtorName(name, v)))
		if len(fields) == 0 {
			out.WriteString("void")
		}
		for j, ft := range fields {
			if j > 0 {
				out.WriteString(", ")
			}
			out.WriteString(fmt.Sprintf("%s f%d", g.cType(ft), j))
		}
		out.WriteString(") {\n")
		out.WriteString(fmt.Sprintf("    Dex_%s* _e = (Dex_%s*)dex_obj_alloc(sizeof(Dex_%s), dex_enum_%s_destroy);\n",
			name, name, name, name))
		out.WriteString(fmt.Sprintf("    _e->tag = %s;\n", g.enumTagName(name, v)))
		for j, ft := range fields {
			out.WriteString(fmt.Sprintf("    _e->u.v_%s.f%d = f%d;\n", v, j, j))
			if ast.NeedsRelease(ft) {
				var ret strings.Builder
				g.emitRetainValue(&ret, "    ", fmt.Sprintf("_e->u.v_%s.f%d", v, j), ft)
				out.WriteString(ret.String())
			}
		}
		out.WriteString("    return _e;\n}\n")
	}
	out.WriteString("\n")
}

// genEnumCtor builds a variant. The arguments go through genBorrowed for the
// same reason a struct literal's do: the constructor takes its own reference
// to every heap value it stores, so one that was minted in the argument
// position needs an owner to release it when the statement ends.
func (g *Generator) genEnumCtor(out *strings.Builder, e *ast.EnumCtorExpr) {
	def := ast.GetEnumDef(e.EnumType)
	fields, _ := def.VariantPayload(e.Variant)
	out.WriteString(g.enumCtorName(e.EnumName, e.Variant) + "(")
	for i, a := range e.Args {
		if i > 0 {
			out.WriteString(", ")
		}
		if i < len(fields) && ast.NeedsRelease(fields[i]) {
			g.genBorrowed(out, a)
			continue
		}
		g.genExpr(out, a)
	}
	out.WriteString(")")
}

// genEnumPatternBindings declares a pattern's bound names at the top of its
// arm. Only a single pattern per arm can bind, because two variants matched by
// one `case` carry different payloads and there would be no consistent type
// for a name to have.
func (g *Generator) genEnumPatternBindings(out *strings.Builder, prefix, tagVar string, tagType ast.Type, values []ast.Expr) {
	if len(values) != 1 {
		return
	}
	pat, ok := values[0].(*ast.EnumPatternExpr)
	if !ok {
		return
	}
	def := ast.GetEnumDef(tagType)
	if def == nil {
		return
	}
	fields, _ := def.VariantPayload(pat.Variant)
	for i, b := range pat.Binds {
		if b == "_" || i >= len(fields) {
			continue
		}
		out.WriteString(fmt.Sprintf("%s%s %s = %s->u.v_%s.f%d;\n",
			prefix, g.cType(fields[i]), b, tagVar, pat.Variant, i))
		g.varTypes[b] = fields[i]
		if ast.IsArrayType(fields[i]) {
			g.arrVars[b] = fields[i]
		}
		if ast.IsStructType(fields[i]) {
			g.structVars[b] = fields[i]
		}
		if fields[i] == ast.TypeString {
			g.strVars[b] = true
		}
		if ast.IsMapType(fields[i]) {
			g.mapVars[b] = fields[i]
		}
	}
}
