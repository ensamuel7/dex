package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/stdlib"
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
	if ast.IsRefType(t) {
		return externalCType(ast.RefInnerType(t)) + "*"
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

// handleOutParams lists the parameters through which a C function writes a
// handle. Those need a raw pointer for C to write into and a box made
// afterwards; a scalar out-parameter is just the variable's address.
func handleOutParams(fd *stdlib.FuncDef, args []ast.Expr) []int {
	var out []int
	for i, pt := range fd.Params {
		if _, isNull := args[i].(*ast.NullLit); isNull {
			continue
		}
		if ast.IsRefType(pt) {
			if _, ok := handleOf(ast.RefInnerType(pt)); ok {
				out = append(out, i)
			}
		}
	}
	return out
}

// genHandleOutCall wraps a call that writes handles through out-parameters. C
// writes a raw pointer into a local; once the call returns, each is boxed and
// stored in the caller's variable the way an assignment stores a value: the old
// box is released after the new one is in place, and the unwind slot follows.
// The local starts NULL, so a C function that leaves it untouched on failure
// leaves the variable null.
//
// The local is void*, but C writes it through its own pointer type — sqlite3**,
// char** — and clang now gives distinct pointer types distinct aliasing classes
// by default (-pointer-tbaa), so where the C function is inlined from a header
// the optimiser may assume its store cannot change a void* and keep the NULL.
// memcpy reads the slot as characters, which alias every store.
func (g *Generator) genHandleOutCall(out *strings.Builder, e *ast.CallExpr, fd *stdlib.FuncDef, outs []int) {
	if g.outRaws == nil {
		g.outRaws = map[ast.Expr]string{}
		g.outWrapped = map[*ast.CallExpr]bool{}
	}
	out.WriteString("({ ")
	raws := make(map[int]string, len(outs))
	for _, i := range outs {
		raw := g.nextTemp()
		raws[i] = raw
		g.outRaws[e.Args[i]] = raw
		out.WriteString(fmt.Sprintf("void* %s = NULL; ", raw))
	}
	var call strings.Builder
	g.outWrapped[e] = true
	g.genCallExpr(&call, e)
	delete(g.outWrapped, e)
	for _, i := range outs {
		delete(g.outRaws, e.Args[i])
	}
	result := ""
	if fd.ReturnType == ast.TypeVoid {
		out.WriteString(call.String() + "; ")
	} else {
		result = g.nextTemp()
		out.WriteString(fmt.Sprintf("__auto_type %s = %s; ", result, call.String()))
	}
	for _, i := range outs {
		name := e.Args[i].(*ast.Ident).Name
		var lv strings.Builder
		g.genExpr(&lv, e.Args[i])
		ht, _ := handleOf(ast.RefInnerType(fd.Params[i]))
		out.WriteString(fmt.Sprintf("{ void* _dex_written; memcpy(&_dex_written, &%s, sizeof _dex_written); DexHandle* _dex_old = %s; %s = dex_handle_new(_dex_written, %s); if (_dex_old) { dex_release(_dex_old); } } ",
			raws[i], lv.String(), lv.String(), handleCloserName(ast.HandleModule(ht), ast.HandleName(ht))))
		var slot strings.Builder
		g.emitUnwindSlotUpdate(&slot, "", name)
		out.WriteString(strings.ReplaceAll(strings.TrimSpace(slot.String()), "\n", " "))
		out.WriteString(" ")
	}
	if result != "" {
		out.WriteString(result + "; ")
	}
	out.WriteString("})")
}

func (g *Generator) isHandleOutCall(call *ast.CallExpr) bool {
	if call.Module == "" {
		return false
	}
	fd, ok := g.lookupFunc(call.Module, call.Name)
	return ok && fd.CName != "" && len(handleOutParams(fd, call.Args)) > 0
}
