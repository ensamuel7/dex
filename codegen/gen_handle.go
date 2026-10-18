package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

// handleOf unwraps an optional to find a handle type. A C function returning a
// handle commonly returns NULL on failure, so the declaration is usually
// optional and both spellings reach the same boxing.
func handleOf(t ast.Type) (ast.Type, bool) {
	if ast.IsHandleType(t) {
		return t, true
	}
	if ast.IsOptionalType(t) {
		if inner := ast.OptionalInnerType(t); ast.IsHandleType(inner) {
			return inner, true
		}
	}
	return 0, false
}

// handleCloserName is the box's destroy hook for one handle type.
func handleCloserName(module, name string) string {
	return "dex_handle_close_" + module + "_" + name
}

// emitHandleClosers writes one shim per declared handle type. The shim takes
// the box, so it cannot be the C closing function itself: that one takes the
// pointer, and its return type is its own business — fclose returns int.
//
// It must not free the box. destroy releases the contents and the runtime frees
// the object, which is the same contract dex_string_destroy and the interface
// box's dex_boxfree_ follow.
func (g *Generator) emitHandleClosers(out *strings.Builder, program *ast.Program) {
	for i := range program.Externals {
		em := &program.Externals[i]
		for _, t := range em.Types {
			out.WriteString(fmt.Sprintf("static void %s(void* p) {\n", handleCloserName(em.Name, t.Name)))
			out.WriteString("    DexHandle* h = (DexHandle*)p;\n")
			out.WriteString(fmt.Sprintf("    if (h->ptr) %s(h->ptr);\n", t.Closer))
			out.WriteString("}\n")
		}
	}
}

// externalCType is the C spelling of a type crossing the boundary, used only
// for a prototype this compiler writes itself.
func externalCType(t ast.Type) string {
	switch t {
	case ast.TypeInt:
		return "int"
	case ast.TypeLong:
		return "long"
	case ast.TypeDouble:
		return "double"
	case ast.TypeBool:
		return "_Bool"
	case ast.TypeChar:
		return "unsigned char"
	case ast.TypeString:
		return "const char*"
	case ast.TypeVoid:
		return "void"
	}
	if _, ok := handleOf(t); ok {
		return "void*"
	}
	return "int"
}

// emitExternalPrototypes declares the C functions of any external module that
// named no header.
//
// A header is the better path and stays the default, because the C compiler
// then cross-checks the declaration against the real one and a signature that
// disagrees is an error rather than a silent ABI mismatch. But a library in
// another language often has no header to point at — a Rust staticlib exports
// C-ABI symbols and nothing else — and writing one by hand to satisfy
// -Werror=implicit-function-declaration is friction without a benefit.
//
// Declaring no header is therefore the opt-out: the signature written in Dex
// is taken at its word, and getting it wrong is an ABI mismatch nothing will
// catch.
func (g *Generator) emitExternalPrototypes(out *strings.Builder, program *ast.Program) {
	for i := range program.Externals {
		em := &program.Externals[i]
		if len(em.Includes) > 0 {
			continue
		}
		for _, t := range em.Types {
			out.WriteString(fmt.Sprintf("extern void %s(void*);\n", t.Closer))
		}
		for _, f := range em.Funcs {
			params := make([]string, 0, len(f.Params))
			for _, pt := range f.Params {
				params = append(params, externalCType(pt))
			}
			sig := "void"
			if len(params) > 0 {
				sig = strings.Join(params, ", ")
			}
			out.WriteString(fmt.Sprintf("extern %s %s(%s);\n", externalCType(f.ReturnType), f.CName, sig))
		}
	}
}
