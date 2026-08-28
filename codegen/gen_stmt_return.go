package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

// genReturnStmt emits a return, releasing everything in scope on the way out
// while leaving the returned value owned by the caller.
func (g *Generator) genReturnStmt(out *strings.Builder, s *ast.ReturnStmt, prefix string, indent int) {
	// Bare return (no value) — void function
	if s.Value == nil {
		g.emitHoistReleases(out, prefix)
		g.emitDeferredCalls(out, prefix)
		g.emitCleanupAll(out, prefix, "")
		out.WriteString(fmt.Sprintf("%sreturn;\n", prefix))
		return
	}
	retType := g.currentFn.ReturnType
	if ast.IsOptionalType(retType) {
		inner := ast.OptionalInnerType(retType)
		_, isNull := s.Value.(*ast.NullLit)
		if ast.IsValueType(inner) {
			ctyp := g.cType(retType)
			if isNull {
				g.emitHoistReleases(out, prefix)
				g.emitDeferredCalls(out, prefix)
				g.emitCleanupAll(out, prefix, "")
				out.WriteString(fmt.Sprintf("%sreturn (%s){0};\n", prefix, ctyp))
			} else {
				out.WriteString(fmt.Sprintf("%s%s _ret_tmp = (%s){1, ", prefix, ctyp, ctyp))
				g.genExpr(out, s.Value)
				out.WriteString("};\n")
				g.emitHoistReleases(out, prefix)
				g.emitDeferredCalls(out, prefix)
				g.emitCleanupAll(out, prefix, "")
				out.WriteString(fmt.Sprintf("%sreturn _ret_tmp;\n", prefix))
			}
		} else {
			// Heap/struct optional: NULL for null, value otherwise
			if isNull {
				g.emitHoistReleases(out, prefix)
				g.emitDeferredCalls(out, prefix)
				g.emitCleanupAll(out, prefix, "")
				out.WriteString(fmt.Sprintf("%sreturn NULL;\n", prefix))
			} else if ast.IsHeapType(inner) || ast.IsHeapType(retType) {
				if ident, ok := s.Value.(*ast.Ident); ok {
					out.WriteString(fmt.Sprintf("%sdex_retain(%s);\n", prefix, ident.Name))
					g.emitHoistReleases(out, prefix)
					g.emitDeferredCalls(out, prefix)
					g.emitCleanupAll(out, prefix, "")
					out.WriteString(fmt.Sprintf("%sreturn %s;\n", prefix, ident.Name))
				} else {
					ctyp := g.cType(retType)
					out.WriteString(fmt.Sprintf("%s%s _ret_tmp = ", prefix, ctyp))
					g.genExpr(out, s.Value)
					out.WriteString(";\n")
					g.emitHoistReleases(out, prefix)
					g.emitDeferredCalls(out, prefix)
					g.emitCleanupAll(out, prefix, "")
					out.WriteString(fmt.Sprintf("%sreturn _ret_tmp;\n", prefix))
				}
			} else if ast.IsStructType(inner) && g.typeOfExpr(s.Value) != retType {
				// Bare struct value returned as T? — box it so the caller
				// receives the Dex_Foo* that the optional representation expects.
				innerCType := "Dex_" + ast.StructName(inner)
				out.WriteString(fmt.Sprintf("%s%s* _ret_tmp = (%s*)malloc(sizeof(%s));\n", prefix, innerCType, innerCType, innerCType))
				out.WriteString(fmt.Sprintf("%s*_ret_tmp = ", prefix))
				g.genExpr(out, s.Value)
				out.WriteString(";\n")
				g.emitHoistReleases(out, prefix)
				g.emitDeferredCalls(out, prefix)
				g.emitCleanupAll(out, prefix, "")
				out.WriteString(fmt.Sprintf("%sreturn _ret_tmp;\n", prefix))
			} else {
				g.emitHoistReleases(out, prefix)
				g.emitDeferredCalls(out, prefix)
				out.WriteString(fmt.Sprintf("%sreturn ", prefix))
				g.genExpr(out, s.Value)
				out.WriteString(";\n")
			}
		}
		return
	}
	if ast.IsHeapType(retType) {
		// Retain the return value, clean up everything else, then return
		if ident, ok := s.Value.(*ast.Ident); ok {
			out.WriteString(fmt.Sprintf("%sdex_retain(%s);\n", prefix, ident.Name))
			g.emitHoistReleases(out, prefix)
			g.emitDeferredCalls(out, prefix)
			g.emitCleanupAll(out, prefix, "")
			out.WriteString(fmt.Sprintf("%sreturn %s;\n", prefix, ident.Name))
		} else {
			// Expression result — eval into temp
			out.WriteString(fmt.Sprintf("%s%s _ret_tmp = ", prefix, g.cType(retType)))
			g.genExpr(out, s.Value)
			out.WriteString(";\n")
			// A borrowed result (e.g. `return arr[i]` or `return s.field`) is owned
			// by a container that the cleanup below releases. Retain it so the
			// caller receives a live reference instead of a dangling one.
			if g.borrowsHeapValue(s.Value) {
				out.WriteString(fmt.Sprintf("%sdex_retain(_ret_tmp);\n", prefix))
			}
			g.emitHoistReleases(out, prefix)
			g.emitDeferredCalls(out, prefix)
			g.emitCleanupAll(out, prefix, "")
			out.WriteString(fmt.Sprintf("%sreturn _ret_tmp;\n", prefix))
		}
	} else {
		// Non-heap return — clean up all heap vars first
		if g.hasHeapVarsInScope() || g.hasDefers() {
			// Evaluate return value into temp to avoid use-after-free
			ctyp := g.cType(retType)
			if retType != ast.TypeVoid {
				out.WriteString(fmt.Sprintf("%s%s _ret_tmp = ", prefix, ctyp))
				g.genExpr(out, s.Value)
				out.WriteString(";\n")
				// A returned struct is a shallow copy, so its heap fields alias the
				// very references the cleanup below would release. Move ownership to
				// the caller instead of freeing what we are about to hand back.
				exceptVar := ""
				if ast.IsStructType(retType) {
					if ident, ok := s.Value.(*ast.Ident); ok {
						// `return m` — leave m's fields alone; the caller owns them now.
						exceptVar = ident.Name
					} else if lit, ok := s.Value.(*ast.StructLitExpr); ok {
						// `return Msg{...}` — only fields taken from a borrowed
						// reference need a retain to survive the cleanup.
						g.emitRetainReturnedLitFields(out, prefix, retType, lit)
					}
				}
				g.emitHoistReleases(out, prefix)
				g.emitDeferredCalls(out, prefix)
				g.emitCleanupAll(out, prefix, exceptVar)
				out.WriteString(fmt.Sprintf("%sreturn _ret_tmp;\n", prefix))
			} else {
				g.emitHoistReleases(out, prefix)
				g.emitDeferredCalls(out, prefix)
				g.emitCleanupAll(out, prefix, "")
				out.WriteString(fmt.Sprintf("%sreturn;\n", prefix))
			}
		} else if lit, ok := s.Value.(*ast.StructLitExpr); ok && ast.IsStructType(retType) && g.structLitBorrowsHeapField(retType, lit) {
			// No locals to clean up, but the literal still copied a borrowed
			// heap reference — typically a parameter, which the caller
			// releases once the call returns. The struct outlives that, so it
			// has to take its own reference.
			ctyp := g.cType(retType)
			out.WriteString(fmt.Sprintf("%s%s _ret_tmp = ", prefix, ctyp))
			g.genExpr(out, s.Value)
			out.WriteString(";\n")
			g.emitRetainReturnedLitFields(out, prefix, retType, lit)
			out.WriteString(fmt.Sprintf("%sreturn _ret_tmp;\n", prefix))
		} else {
			// Nothing in scope to clean up, but the return expression may
			// still have hoisted a temporary — a string literal handed to a
			// function that only borrows it. That has to be released, which
			// means computing the result first.
			var retBody strings.Builder
			g.genExpr(&retBody, s.Value)
			if len(g.stmtTemps) > 0 {
				out.WriteString(fmt.Sprintf("%s%s _ret_tmp = %s;\n", prefix, g.cType(retType), retBody.String()))
				g.emitHoistReleases(out, prefix)
				out.WriteString(fmt.Sprintf("%sreturn _ret_tmp;\n", prefix))
			} else {
				out.WriteString(fmt.Sprintf("%sreturn %s;\n", prefix, retBody.String()))
			}
		}
	}

}
