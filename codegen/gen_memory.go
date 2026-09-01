package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

// Scope management
func (g *Generator) pushScope() {
	g.scopeStack = append(g.scopeStack, nil)
}

func (g *Generator) popScope(out *strings.Builder, prefix string) {
	if len(g.scopeStack) == 0 {
		return
	}
	scope := g.scopeStack[len(g.scopeStack)-1]
	g.scopeStack = g.scopeStack[:len(g.scopeStack)-1]
	for i := len(scope) - 1; i >= 0; i-- {
		sv := scope[i]
		g.emitReleaseVar(out, prefix, sv.name, sv.typ)
	}
}

func (g *Generator) registerScopeVar(name string, typ ast.Type) {
	if !ast.NeedsRelease(typ) {
		return
	}
	if len(g.scopeStack) == 0 {
		return
	}
	idx := len(g.scopeStack) - 1
	g.scopeStack[idx] = append(g.scopeStack[idx], scopeVar{name: name, typ: typ})
}

func (g *Generator) emitReleaseVar(out *strings.Builder, prefix, name string, typ ast.Type) {
	if ast.IsOptionalType(typ) {
		inner := ast.OptionalInnerType(typ)
		if ast.IsValueType(inner) {
			// Value-type optional: no cleanup needed
			return
		}
		if ast.IsHeapType(inner) {
			// Heap-type optional: guard with null check
			out.WriteString(fmt.Sprintf("%sif (%s) { dex_release(%s); }\n", prefix, name, name))
			return
		}
		if ast.IsStructType(inner) {
			// Struct-type optional: free heap fields then free pointer
			def := ast.GetStructDef(inner)
			if def != nil {
				out.WriteString(fmt.Sprintf("%sif (%s) {\n", prefix, name))
				for _, f := range def.Fields {
					if ast.NeedsRelease(f.Type) {
						g.emitReleaseVar(out, prefix+"    ", name+"->"+f.Name, f.Type)
					}
				}
				out.WriteString(fmt.Sprintf("%s    free(%s);\n", prefix, name))
				out.WriteString(fmt.Sprintf("%s}\n", prefix))
			}
			return
		}
		return
	}
	if ast.IsHeapType(typ) {
		annots := g.varAnnotations[name]
		if ast.HasAnnotation(annots, ast.AnnotOwned) {
			out.WriteString(fmt.Sprintf("%sdex_owned_free(%s);\n", prefix, name))
		} else if ast.HasAnnotation(annots, ast.AnnotRegion) {
			// Skip — arena handles it
		} else {
			out.WriteString(fmt.Sprintf("%sdex_release(%s);\n", prefix, name))
		}
	} else if ast.IsStructType(typ) {
		def := ast.GetStructDef(typ)
		if def != nil {
			for _, f := range def.Fields {
				if ast.NeedsRelease(f.Type) {
					g.emitReleaseVar(out, prefix, name+"."+f.Name, f.Type)
				}
			}
		}
	}
}

// emitCleanupAll releases all vars in ALL scopes (for return statements), skipping exceptVar
func (g *Generator) emitCleanupAll(out *strings.Builder, prefix string, exceptVar string) {
	for i := len(g.scopeStack) - 1; i >= 0; i-- {
		scope := g.scopeStack[i]
		for j := len(scope) - 1; j >= 0; j-- {
			sv := scope[j]
			if sv.name == exceptVar {
				continue
			}
			g.emitReleaseVar(out, prefix, sv.name, sv.typ)
		}
	}
}

// emitCleanupInnerScopes releases vars from inner scopes down to (and including) targetDepth
func (g *Generator) emitCleanupInnerScopes(out *strings.Builder, prefix string, targetDepth int) {
	for i := len(g.scopeStack) - 1; i >= targetDepth; i-- {
		scope := g.scopeStack[i]
		for j := len(scope) - 1; j >= 0; j-- {
			sv := scope[j]
			g.emitReleaseVar(out, prefix, sv.name, sv.typ)
		}
	}
}

// emitDeferredCalls emits accumulated defer expressions in LIFO order (last defer first).
func (g *Generator) emitDeferredCalls(out *strings.Builder, prefix string) {
	for i := len(g.deferExprs) - 1; i >= 0; i-- {
		out.WriteString(prefix)
		g.genExpr(out, g.deferExprs[i])
		out.WriteString(";\n")
	}
}

// hasDefers returns true if there are pending deferred expressions.
func (g *Generator) hasDefers() bool {
	return len(g.deferExprs) > 0
}

// isBorrowedExpr reports whether an expression yields a reference owned by
// someone else (a variable, an array element, or a struct field) rather than a
// freshly-created temporary. Borrowed references must be retained when they are
// stored somewhere that outlives the current owner; owned temporaries must not
// be, or they leak.
func isBorrowedExpr(e ast.Expr) bool {
	switch e.(type) {
	case *ast.Ident, *ast.IndexExpr, *ast.FieldAccessExpr:
		return true
	}
	return false
}

// borrowsHeapValue is the generator-aware form of isBorrowedExpr. It exists
// because json.Value does not follow the general rule: indexing one mints a
// reference where indexing an array only borrows the element. Ownership
// decisions go through here so the two predicates cannot disagree.
func (g *Generator) borrowsHeapValue(e ast.Expr) bool {
	if g.typeOfExpr(e) == ast.TypeJsonValue {
		return !g.jsonValueOwned(e)
	}
	// Naming a method builds a closure over the receiver rather than reading a
	// field, so it is a fresh reference despite looking like a field access.
	if fa, ok := e.(*ast.FieldAccessExpr); ok && fa.IsMethodValue {
		return false
	}
	return isBorrowedExpr(e)
}

// emitRetainReturnedLitFields retains the heap fields of a returned struct
// literal that were initialised from a borrowed reference. Those references are
// owned by locals that the caller-side cleanup is about to release, so without
// this the returned struct would carry dangling pointers. Fields built from a
// fresh temporary already carry the only reference and are deliberately skipped
// so ownership simply moves to the caller.
func (g *Generator) emitRetainReturnedLitFields(out *strings.Builder, prefix string, retType ast.Type, lit *ast.StructLitExpr) {
	g.emitRetainStructLitFields(out, prefix, "_ret_tmp", retType, lit)
}

// emitRetainStructLitFields retains the heap fields of a struct literal that were
// initialised from a borrowed reference. The literal is a C compound literal, so
// it copies those pointers without taking ownership; without this the struct and
// the borrowed variable would each release the same reference at scope exit.
func (g *Generator) emitRetainStructLitFields(out *strings.Builder, prefix, target string, structType ast.Type, lit *ast.StructLitExpr) {
	def := ast.GetStructDef(structType)
	if def == nil {
		return
	}
	fieldTypes := make(map[string]ast.Type, len(def.Fields))
	for _, f := range def.Fields {
		fieldTypes[f.Name] = f.Type
	}
	for i, name := range lit.FieldNames {
		if i >= len(lit.FieldValues) {
			break
		}
		ft, ok := fieldTypes[name]
		if !ok || !ast.NeedsRelease(ft) || !ast.IsHeapType(ft) {
			continue
		}
		if g.borrowsHeapValue(lit.FieldValues[i]) {
			out.WriteString(fmt.Sprintf("%sdex_retain(%s.%s);\n", prefix, target, name))
		}
	}
}

// emitRetainBorrowedStructFields retains the reference-counted fields of a
// struct that was *copied* out of somewhere it does not own — an array element,
// a field of another struct, another variable.
//
// The copy duplicates those field pointers without taking a reference, and
// emitReleaseVar releases every one of them at scope exit. Without the matching
// retain the scope frees memory whose real owner is still using it. The symptom
// is not a crash at the copy: it is a hang or a corrupted allocator on the
// *next* allocation, pointing at a line that has nothing to do with it.
//
//	let rule: Rule = rules[i]     // copies siteIds, takes no reference
//	let n: long[] = rule.siteIds  // aliases it again
//	                              // scope exit released both. Twice, from one.
//
// The mirror of emitRetainStructLitFields, for copies rather than literals, and
// it recurses the same way emitReleaseVar does so a nested struct field is not
// left unbalanced.
func (g *Generator) emitRetainBorrowedStructFields(out *strings.Builder, prefix, target string, structType ast.Type) {
	def := ast.GetStructDef(structType)
	if def == nil {
		return
	}
	for _, f := range def.Fields {
		if !ast.NeedsRelease(f.Type) {
			continue
		}
		field := target + "." + f.Name
		switch {
		case ast.IsOptionalType(f.Type):
			// Only the heap-backed optionals are released, so only those are
			// retained. dex_retain is NULL-safe, which is what absent looks like.
			if ast.IsHeapType(ast.OptionalInnerType(f.Type)) {
				out.WriteString(fmt.Sprintf("%sdex_retain(%s);\n", prefix, field))
			}
		case ast.IsHeapType(f.Type):
			out.WriteString(fmt.Sprintf("%sdex_retain(%s);\n", prefix, field))
		case ast.IsStructType(f.Type):
			g.emitRetainBorrowedStructFields(out, prefix, field, f.Type)
		}
	}
}

// hasHeapVarsInScope checks if there are any heap vars tracked in any scope
func (g *Generator) hasHeapVarsInScope() bool {
	for _, scope := range g.scopeStack {
		if len(scope) > 0 {
			return true
		}
	}
	return false
}
