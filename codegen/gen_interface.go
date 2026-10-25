package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

// An interface value is a pointer to the concrete struct plus a table of its
// methods, built at the point where a struct is passed as the interface. The
// table is filled from the concrete type, which the compiler knows at every
// such site, so dispatch costs an indirect call and no lookup.
//
// _data borrows: it points at the caller's struct rather than at a copy, so an
// interface value must not outlive the struct it was built from. That is why
// the checker refuses an interface as a return type, a struct field, or a
// container element — those are exactly the positions that would let it.

// genInterfaceValue emits the vtable literal coercing a struct expression to an
// interface.
func (g *Generator) genInterfaceValue(out *strings.Builder, arg ast.Expr, ifaceType ast.Type) {
	ifaceDef := ast.GetInterfaceDef(ifaceType)
	structType := g.typeOfExpr(arg)
	if ast.IsRefType(structType) {
		structType = ast.RefInnerType(structType)
	}
	structDef := ast.GetStructDef(structType)
	if ifaceDef == nil || structDef == nil {
		g.genExpr(out, arg)
		return
	}

	// Already an interface value — passing a Greeter parameter straight on.
	if ast.IsInterfaceType(g.typeOfExpr(arg)) {
		g.genExpr(out, arg)
		return
	}

	// Mints: the constructor allocates the box and takes a reference to each of
	// the copy's heap fields. Whoever consumes this owns it — a return hands it
	// to the caller, a let to the scope, a field to the struct. An argument is
	// the one position where nothing does, which is why genArgMaybeInterface
	// hoists it into a statement-scoped temporary instead of calling here bare.
	out.WriteString(interfaceCtorName(ifaceDef.Name, structDef.Name))
	out.WriteString("(")
	if ast.IsRefType(g.typeOfExpr(arg)) {
		out.WriteString("*")
		g.genExpr(out, arg)
	} else {
		g.genBorrowed(out, arg)
	}
	out.WriteString(")")
}

func (g *Generator) structMethodCName(def *ast.StructDef, method string) string {
	name := def.Name + "_" + method
	if modName, ok := g.structModules[def.Name]; ok {
		name = modName + "_" + name
	}
	return name
}

// genInterfaceCall emits a call dispatched through the receiver's vtable.
func (g *Generator) genInterfaceCall(out *strings.Builder, e *ast.CallExpr) {
	recv := e.Module
	if strings.Contains(recv, ".") {
		recv = g.fieldChainC(recv)
	}
	out.WriteString(fmt.Sprintf("%s._vtable.%s(%s._data", recv, e.Name, recv))
	for _, arg := range e.Args {
		out.WriteString(", ")
		g.genBorrowed(out, arg)
	}
	out.WriteString(")")
}

// genArgMaybeInterface writes an argument, coercing a struct to an interface
// when that is what the parameter asks for. Every call path that can reach a
// function with an interface parameter goes through it.
func (g *Generator) genArgMaybeInterface(out *strings.Builder, arg ast.Expr, paramType ast.Type) bool {
	if !ast.IsInterfaceType(paramType) {
		return false
	}
	// Forwarding an interface value the caller already owns: a borrow, so no
	// box is built and none is released.
	if ast.IsInterfaceType(g.typeOfExpr(arg)) {
		g.genBorrowed(out, arg)
		return true
	}
	// A parameter is borrowed, so the box minted for the coercion has no owner
	// at the callee. It is hoisted into a statement-scoped temporary and
	// released once the statement ends — the same treatment genBorrowed gives a
	// string built in an argument.
	if !g.hoistingEnabled() {
		g.genInterfaceValue(out, arg, paramType)
		return true
	}
	savedPrelude := g.stmtPrelude
	g.stmtPrelude = &strings.Builder{}
	var exprBuf strings.Builder
	g.genInterfaceValue(&exprBuf, arg, paramType)
	nested := g.stmtPrelude.String()
	g.stmtPrelude = savedPrelude

	tmp := g.nextTemp()
	g.stmtPrelude.WriteString(nested)
	g.stmtPrelude.WriteString(fmt.Sprintf("%s %s = %s; ", g.cType(paramType), tmp, exprBuf.String()))
	g.registerTempForUnwind(g.stmtPrelude, tmp, paramType)
	g.stmtTemps = append(g.stmtTemps, scopeVar{name: tmp, typ: paramType})
	out.WriteString(tmp)
	return true
}

// --- Owning interface values ---
//
// An interface value owns the struct it was built from: coercion allocates a
// refcounted box, copies the struct in, and takes a reference to each of the
// struct's heap fields. _data points at the copy inside the box and _obj at the
// box itself, so dispatch is unchanged and the reference count lives where the
// existing dex_retain / dex_release can reach it.
//
// Owning rather than borrowing is what lets an interface be returned, stored in
// a struct field, and held in a container — none of which a pointer to a
// caller's frame could survive. The cost is one pooled allocation per coercion
// and the consequence is that mutation through an interface reaches the copy
// rather than the original, which is the same trade Go makes.
//
// The constructor retains the copy's heap fields and callers pass a *borrowed*
// struct, the convention dex_array_string_push already follows: a struct built
// in the argument position is hoisted to a statement-scoped temporary and
// released at the end of the statement, so the retain here is what keeps the
// box's copy alive past it.

// interfaceBoxName is the box holding a copy of a concrete struct.
func interfaceBoxName(structName string) string {
	return "DexBox_" + structName
}

// interfaceCtorName constructs an interface value from a concrete struct.
func interfaceCtorName(ifaceName, structName string) string {
	return "dex_iface_" + ifaceName + "_from_" + structName
}

func vtableThunkName(ifaceName, structName, method string) string {
	return "dex_vt_" + ifaceName + "_" + structName + "_" + method
}

// interfaceElemCleanupName tears down one interface element of a container.
func interfaceElemCleanupName(ifaceName string) string {
	return "dex_cleanup_iface_" + ifaceName
}

// structSatisfiesIface answers whether a struct has every method the interface
// declares, with matching signatures. The checker has already rejected the
// cases that do not, so this only decides which constructors to emit.
func structSatisfiesIface(sd ast.StructDef, id ast.InterfaceDef) bool {
	for _, im := range id.Methods {
		var found *ast.Function
		for i := range sd.Methods {
			if sd.Methods[i].Name == im.Name {
				found = &sd.Methods[i]
				break
			}
		}
		if found == nil || len(found.Params) != len(im.Params) || found.ReturnType != im.ReturnType {
			return false
		}
		for i, pt := range im.Params {
			if found.Params[i].Type != pt {
				return false
			}
		}
	}
	return true
}

// emitInterfaceBoxes writes the box type and destructor for every struct that
// satisfies an interface, and a constructor for every pair. Emitting by
// satisfaction rather than by use means no collection pass over the AST; an
// unused constructor is a static function the C compiler drops.
func (g *Generator) emitInterfaceBoxes(out *strings.Builder, program *ast.Program) {
	if len(program.Interfaces) == 0 {
		return
	}

	boxed := make(map[string]bool)
	for _, sd := range program.Structs {
		satisfies := false
		for _, id := range program.Interfaces {
			if structSatisfiesIface(sd, id) {
				satisfies = true
				break
			}
		}
		if !satisfies || boxed[sd.Name] {
			continue
		}
		boxed[sd.Name] = true

		box := interfaceBoxName(sd.Name)
		out.WriteString(fmt.Sprintf("typedef struct { DexObjHeader hdr; Dex_%s value; } %s;\n", sd.Name, box))

		// The box's destructor releases the copy's heap fields. A struct with
		// none needs no destructor at all, and dex_release tolerates NULL
		// there, so nothing is emitted for it.
		var fields strings.Builder
		structType, ok := ast.LookupStructType(sd.Name)
		if ok {
			g.emitReleaseVar(&fields, "    ", "b->value", structType)
		}
		if fields.Len() > 0 {
			out.WriteString(fmt.Sprintf("static void dex_boxfree_%s(void* ptr) {\n", sd.Name))
			out.WriteString(fmt.Sprintf("    %s* b = (%s*)ptr;\n", box, box))
			out.WriteString(fields.String())
			out.WriteString("}\n")
		}
	}

	// A by-value container — an array, a map, an optional — stores the
	// interface value itself and tears an element down through a function
	// pointer, which is this. It releases the box the element owns; the element
	// memory belongs to the container.
	for _, id := range program.Interfaces {
		out.WriteString(fmt.Sprintf("static void %s(void* ptr) { dex_release(((Dex_%s*)ptr)->_obj); }\n",
			interfaceElemCleanupName(id.Name), id.Name))
	}

	for _, id := range program.Interfaces {
		for _, sd := range program.Structs {
			if !structSatisfiesIface(sd, id) {
				continue
			}
			g.emitInterfaceCtor(out, sd, id)
		}
	}
	out.WriteString("\n")
}

func (g *Generator) emitInterfaceCtor(out *strings.Builder, sd ast.StructDef, id ast.InterfaceDef) {
	box := interfaceBoxName(sd.Name)
	destroy := "NULL"
	structType, ok := ast.LookupStructType(sd.Name)
	if ok {
		var probe strings.Builder
		g.emitReleaseVar(&probe, "", "b->value", structType)
		if probe.Len() > 0 {
			destroy = "dex_boxfree_" + sd.Name
		}
	}

	structDef := ast.GetStructDef(structType)
	for _, m := range id.Methods {
		ret := g.cType(m.ReturnType)
		out.WriteString(fmt.Sprintf("static %s %s(void* self", ret, vtableThunkName(id.Name, sd.Name, m.Name)))
		for i, pt := range m.Params {
			out.WriteString(fmt.Sprintf(", %s a%d", g.cType(pt), i))
		}
		out.WriteString(") { ")
		if m.ReturnType != ast.TypeVoid {
			out.WriteString("return ")
		}
		out.WriteString(fmt.Sprintf("%s((Dex_%s*)self", g.structMethodCName(structDef, m.Name), sd.Name))
		for i := range m.Params {
			out.WriteString(fmt.Sprintf(", a%d", i))
		}
		out.WriteString("); }\n")
	}
	out.WriteString(fmt.Sprintf("static Dex_%s %s(Dex_%s v) {\n",
		id.Name, interfaceCtorName(id.Name, sd.Name), sd.Name))
	out.WriteString(fmt.Sprintf("    %s* b = (%s*)dex_obj_alloc(sizeof(%s), %s);\n", box, box, box, destroy))
	out.WriteString("    b->value = v;\n")
	if ok {
		g.emitRetainBorrowedStructFields(out, "    ", "b->value", structType)
	}
	out.WriteString(fmt.Sprintf("    return (Dex_%s){ ._data = &b->value, ._obj = b, ._vtable = { ", id.Name))
	for i, m := range id.Methods {
		if i > 0 {
			out.WriteString(", ")
		}
		out.WriteString(fmt.Sprintf(".%s = %s", m.Name, vtableThunkName(id.Name, sd.Name, m.Name)))
	}
	out.WriteString(" } };\n}\n")
}
