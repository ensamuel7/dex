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
