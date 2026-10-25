package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

// emitReturn closes the try frames this return jumps out of, then returns.
//
// The value has to be evaluated already — a bare name, a literal, or a
// temporary. A return expression can itself throw, and until it has produced a
// value the enclosing try is still the handler for it: popping first sent
// `return load(path)` to the caller's catch instead of the one wrapped around
// it, which is exactly the bug this ordering exists to avoid.
func (g *Generator) emitReturn(out *strings.Builder, prefix, value string) {
	g.emitExcFramePops(out, prefix, g.tryDepth)
	if value == "" {
		out.WriteString(prefix + "return;\n")
		return
	}
	out.WriteString(fmt.Sprintf("%sreturn %s;\n", prefix, value))
}

// genReturnStmt emits a return, releasing everything in scope on the way out
// while leaving the returned value owned by the caller.
func (g *Generator) genReturnStmt(out *strings.Builder, s *ast.ReturnStmt, prefix string, indent int) {
	// Bare return (no value) — void function
	if s.Value == nil {
		g.emitHoistReleases(out, prefix)
		g.emitDeferredCalls(out, prefix)
		g.emitCleanupAll(out, prefix, "")
		// main() is void in Dex and int in C, so an early `return` inside it
		// has to carry an exit status even though the source gave none. A bare
		// `return;` there is a C error, which turned an ordinary early exit —
		// "could not connect, say so and stop" — into a build failure.
		if g.currentFn != nil && g.currentFn.Name == "main" {
			g.emitReturn(out, prefix, "0")
			return
		}
		g.emitReturn(out, prefix, "")
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
				g.emitReturn(out, prefix, fmt.Sprintf("(%s){0}", ctyp))
			} else {
				out.WriteString(fmt.Sprintf("%s%s _ret_tmp = (%s){1, ", prefix, ctyp, ctyp))
				g.genExpr(out, s.Value)
				out.WriteString("};\n")
				g.emitHoistReleases(out, prefix)
				g.emitDeferredCalls(out, prefix)
				g.emitCleanupAll(out, prefix, "")
				g.emitReturn(out, prefix, "_ret_tmp")
			}
		} else {
			// Heap/struct optional: NULL for null, value otherwise
			if isNull {
				g.emitHoistReleases(out, prefix)
				g.emitDeferredCalls(out, prefix)
				g.emitCleanupAll(out, prefix, "")
				g.emitReturn(out, prefix, "NULL")
			} else if ast.IsHeapType(inner) || ast.IsHeapType(retType) {
				if ident, ok := s.Value.(*ast.Ident); ok {
					out.WriteString(fmt.Sprintf("%sdex_retain(%s);\n", prefix, ident.Name))
					g.emitHoistReleases(out, prefix)
					g.emitDeferredCalls(out, prefix)
					g.emitCleanupAll(out, prefix, "")
					g.emitReturn(out, prefix, ident.Name)
				} else {
					ctyp := g.cType(retType)
					out.WriteString(fmt.Sprintf("%s%s _ret_tmp = ", prefix, ctyp))
					g.genExpr(out, s.Value)
					out.WriteString(";\n")
					if g.borrowsHeapValue(s.Value) {
						out.WriteString(fmt.Sprintf("%sdex_retain(_ret_tmp);\n", prefix))
					}
					g.emitHoistReleases(out, prefix)
					g.emitDeferredCalls(out, prefix)
					g.emitCleanupAll(out, prefix, "")
					g.emitReturn(out, prefix, "_ret_tmp")
				}
			} else if ast.IsStructType(inner) {
				out.WriteString(fmt.Sprintf("%s%s _ret_tmp = ", prefix, g.cType(retType)))
				g.genOptionalStructOwned(out, s.Value, retType)
				out.WriteString(";\n")
				g.emitHoistReleases(out, prefix)
				g.emitDeferredCalls(out, prefix)
				g.emitCleanupAll(out, prefix, "")
				g.emitReturn(out, prefix, "_ret_tmp")
			} else {
				g.emitHoistReleases(out, prefix)
				g.emitDeferredCalls(out, prefix)
				var body strings.Builder
				g.genExpr(&body, s.Value)
				if g.tryDepth > 0 {
					// Evaluated before the frames are popped, because it may
					// throw and this try is still its handler.
					out.WriteString(fmt.Sprintf("%s%s _ret_tmp = %s;\n", prefix, g.cType(retType), body.String()))
					g.emitReturn(out, prefix, "_ret_tmp")
				} else {
					out.WriteString(fmt.Sprintf("%sreturn %s;\n", prefix, body.String()))
				}
			}
		}
		return
	}
	// Returning an interface: the value handed back owns a box, so the coercion
	// mints one here and the scope cleanup below releases only the locals. A
	// value that is already an interface is retained instead — the local that
	// held it is about to be released, and the caller needs a live reference.
	if ast.IsInterfaceType(retType) {
		savedPrelude := g.stmtPrelude
		g.stmtPrelude = &strings.Builder{}
		var body strings.Builder
		alreadyIface := ast.IsInterfaceType(g.typeOfExpr(s.Value))
		if alreadyIface {
			g.genExpr(&body, s.Value)
		} else {
			g.genInterfaceValue(&body, s.Value, retType)
		}
		prelude := g.stmtPrelude.String()
		g.stmtPrelude = savedPrelude

		if prelude != "" {
			if g.stmtTempsRegistered && g.stmtTempMark == "" {
				g.stmtTempMark = g.nextCleanupMark()
				out.WriteString(fmt.Sprintf("%ssize_t %s = _dex_cleanup_top;\n", prefix, g.stmtTempMark))
			}
			out.WriteString(prefix + prelude + "\n")
		}
		out.WriteString(fmt.Sprintf("%s%s _ret_tmp = %s;\n", prefix, g.cType(retType), body.String()))
		if alreadyIface && g.borrowsHeapValue(s.Value) {
			out.WriteString(fmt.Sprintf("%sdex_retain(_ret_tmp._obj);\n", prefix))
		}
		g.emitHoistReleases(out, prefix)
		g.emitDeferredCalls(out, prefix)
		g.emitCleanupAll(out, prefix, "")
		g.emitReturn(out, prefix, "_ret_tmp")
		return
	}
	if ast.IsHeapType(retType) {
		// Retain the return value, clean up everything else, then return
		if ident, ok := s.Value.(*ast.Ident); ok {
			out.WriteString(fmt.Sprintf("%sdex_retain(%s);\n", prefix, ident.Name))
			g.emitHoistReleases(out, prefix)
			g.emitDeferredCalls(out, prefix)
			g.emitCleanupAll(out, prefix, "")
			g.emitReturn(out, prefix, ident.Name)
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
			g.emitReturn(out, prefix, "_ret_tmp")
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
					if ident, ok := s.Value.(*ast.Ident); ok && g.isScopeVar(ident.Name) {
						// `return m` — leave m's fields alone; the caller owns them now.
						exceptVar = ident.Name
					} else if _, ok := s.Value.(*ast.Ident); ok {
						// `return p` for a by-value parameter: its fields are the
						// caller's, so there is nothing to move and the copy needs
						// its own references. Without them the caller released the
						// struct it passed in and then the one it got back, both
						// sharing one set of strings.
						g.emitRetainBorrowedStructFields(out, prefix, "_ret_tmp", retType)
					} else if _, _, isLit := structAssignments(s.Value); isLit {
						// `return Msg{...}` or `return Msg(...)` — only fields
						// taken from a borrowed reference need a retain to
						// survive the cleanup.
						g.emitRetainReturnedLitFields(out, prefix, retType, s.Value)
					} else if g.borrowsHeapValue(s.Value) {
						// `return all[0]` or `return holder.inner` — a copy out
						// of a container this scope owns. The copy aliases the
						// container's heap fields and the cleanup below releases
						// the container, so without this the caller is handed
						// pointers into memory that has just been freed.
						//
						// The same hole the `let` branch had, in the other
						// statement that moves a struct across a scope: strings
						// were retained there by a separate path, so an id or a
						// name survived and the struct looked correct, while an
						// array field read out of it gave a nonsense length and
						// the next read segfaulted. Only `exceptVar` above and
						// literals were covered; a borrowed expression was not.
						g.emitRetainBorrowedStructFields(out, prefix, "_ret_tmp", retType)
					}
				}
				g.emitHoistReleases(out, prefix)
				g.emitDeferredCalls(out, prefix)
				g.emitCleanupAll(out, prefix, exceptVar)
				g.emitReturn(out, prefix, "_ret_tmp")
			} else {
				g.emitHoistReleases(out, prefix)
				g.emitDeferredCalls(out, prefix)
				g.emitCleanupAll(out, prefix, "")
				g.emitReturn(out, prefix, "")
			}
		} else if _, _, isLit := structAssignments(s.Value); isLit && ast.IsStructType(retType) && g.structLitBorrowsHeapField(retType, s.Value) {
			// No locals to clean up, but the literal still copied a borrowed
			// heap reference — typically a parameter, which the caller
			// releases once the call returns. The struct outlives that, so it
			// has to take its own reference.
			ctyp := g.cType(retType)
			out.WriteString(fmt.Sprintf("%s%s _ret_tmp = ", prefix, ctyp))
			g.genExpr(out, s.Value)
			out.WriteString(";\n")
			g.emitRetainReturnedLitFields(out, prefix, retType, s.Value)
			g.emitHoistReleases(out, prefix)
			g.emitReturn(out, prefix, "_ret_tmp")
		} else if ast.IsStructType(retType) && g.borrowsHeapValue(s.Value) &&
			g.structHasReleasableField(retType) {
			// The same copy-out-of-a-container as above, in a function with no
			// locals to clean up: `fn first(all: Holder[]): Holder { return
			// all[0] }`, or returning a by-value parameter's field.
			//
			// Nothing here releases the container — it is the caller's — so
			// there is no cleanup to survive, and that is exactly why the
			// retain is needed. The caller receives the copy as an owned value
			// and releases its fields when that value dies, *and* releases the
			// container's when the container dies: two releases against one
			// reference. This is the one that was reached by a bare
			// `return expr` with no temporary at all, so neither arm above
			// could have caught it.
			ctyp := g.cType(retType)
			out.WriteString(fmt.Sprintf("%s%s _ret_tmp = ", prefix, ctyp))
			g.genExpr(out, s.Value)
			out.WriteString(";\n")
			g.emitRetainBorrowedStructFields(out, prefix, "_ret_tmp", retType)
			g.emitHoistReleases(out, prefix)
			g.emitReturn(out, prefix, "_ret_tmp")
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
				g.emitReturn(out, prefix, "_ret_tmp")
			} else if g.tryDepth > 0 {
				out.WriteString(fmt.Sprintf("%s%s _ret_tmp = %s;\n", prefix, g.cType(retType), retBody.String()))
				g.emitReturn(out, prefix, "_ret_tmp")
			} else {
				out.WriteString(fmt.Sprintf("%sreturn %s;\n", prefix, retBody.String()))
			}
		}
	}

}
