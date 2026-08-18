package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

func (g *Generator) genFunction(out *strings.Builder, fn *ast.Function) {
	// Reset var tracking for this function scope
	g.strVars = make(map[string]bool)
	g.arrVars = make(map[string]ast.Type)
	g.structVars = make(map[string]ast.Type)
	g.mapVars = make(map[string]ast.Type)
	g.sbVars = make(map[string]bool)
	g.varTypes = make(map[string]ast.Type)
	g.varAnnotations = make(map[string][]string)
	g.foreachCounter = 0
	g.scopeStack = nil
	g.currentFn = fn
	g.isInLoop = false
	g.loopDepth = 0
	g.narrowedVars = make(map[string]string)

	// Register params (not tracked in scope — callee-borrows convention)
	for _, p := range fn.Params {
		g.varTypes[p.Name] = p.Type
		if p.Type == ast.TypeString {
			g.strVars[p.Name] = true
		}
		if ast.IsArrayType(p.Type) {
			g.arrVars[p.Name] = p.Type
		}
		if ast.IsStructType(p.Type) {
			g.structVars[p.Name] = p.Type
		}
		if ast.IsMapType(p.Type) {
			g.mapVars[p.Name] = p.Type
		}
		if p.Type == ast.TypeStringBuilder {
			g.sbVars[p.Name] = true
		}
	}

	// For main(), always emit "int main" in C regardless of Dex return type
	retType := g.cType(fn.ReturnType)
	if fn.Name == "main" {
		retType = "int"
	}
	name := fn.Name

	if fn.ReturnType == ast.TypeString && len(fn.Params) == 0 {
		out.WriteString(fmt.Sprintf("%s %s(void) {\n", retType, name))
	} else {
		out.WriteString(fmt.Sprintf("%s %s(", retType, name))
		for i, p := range fn.Params {
			if i > 0 {
				out.WriteString(", ")
			}
			out.WriteString(fmt.Sprintf("%s %s", g.cType(p.Type), p.Name))
		}
		out.WriteString(") {\n")
	}

	// Check if function uses #[region] — emit arena setup
	fnUsesArena := g.functionUsesRegion(fn)

	// Push function-level scope
	g.pushScope()

	if fnUsesArena {
		out.WriteString("    DexArena* _arena = dex_arena_new(65536);\n")
	}

	for _, stmt := range fn.Body {
		g.genStmt(out, stmt, 1)
	}

	if fnUsesArena {
		out.WriteString("    dex_arena_destroy(_arena);\n")
	}

	// For void main(), insert cleanup + implicit return 0
	if fn.Name == "main" && fn.ReturnType == ast.TypeVoid {
		g.popScope(out, "    ")
		out.WriteString("    return 0;\n")
	} else {
		// Pop function scope (cleanup for functions that fall through without return)
		g.popScope(out, "    ")
	}

	out.WriteString("}\n")
}

func (g *Generator) genStmt(out *strings.Builder, stmt ast.Stmt, indent int) {
	prefix := strings.Repeat("    ", indent)

	switch s := stmt.(type) {
	case *ast.LetStmt:
		g.varTypes[s.Name] = s.Type
		if len(s.Annotations) > 0 {
			g.varAnnotations[s.Name] = s.Annotations
		}
		if s.Type == ast.TypeString {
			g.strVars[s.Name] = true
		}
		if ast.IsArrayType(s.Type) {
			g.arrVars[s.Name] = s.Type
		}
		if ast.IsStructType(s.Type) {
			g.structVars[s.Name] = s.Type
		}
		if ast.IsMapType(s.Type) {
			g.mapVars[s.Name] = s.Type
		}
		if s.Type == ast.TypeStringBuilder {
			g.sbVars[s.Name] = true
		}
		// Special case for map literal: let m: map[K,V] = {}
		if _, ok := s.Value.(*ast.MapLitExpr); ok {
			suffix := g.mapSuffix(s.Type)
			out.WriteString(fmt.Sprintf("%s%s %s = dex_map_%s_new();\n", prefix, g.cType(s.Type), s.Name, suffix))
			g.registerScopeVar(s.Name, s.Type)
			break
		}
		// Special case for channel/task types: let ch = channel(int), let t = spawn { ... }
		if ast.IsChanType(s.Type) || ast.IsTaskType(s.Type) {
			// Already handled by varTypes above; ensure cType works
		}
		// Special case for receive expression: let val = receive(task)
		if recvExpr, ok := s.Value.(*ast.ReceiveExpr); ok {
			ctyp := g.cType(s.Type)
			out.WriteString(fmt.Sprintf("%s%s %s; dex_chan_recv(", prefix, ctyp, s.Name))
			g.genExpr(out, recvExpr.Source)
			out.WriteString(fmt.Sprintf(", &%s);\n", s.Name))
			g.registerScopeVar(s.Name, s.Type)
			break
		}
		// Special case for array literal value
		if arrLit, ok := s.Value.(*ast.ArrayLitExpr); ok {
			if ast.IsStructArrayType(s.Type) {
				// Struct array: use generic DexArrayStruct
				elemType := ast.ElementType(s.Type)
				elemCType := g.cType(elemType)
				cleanupFn := g.structArrayCleanupFunc(elemType)
				out.WriteString(fmt.Sprintf("%sDexArrayStruct* %s = dex_array_struct_new(sizeof(%s), %s);\n", prefix, s.Name, elemCType, cleanupFn))
				if len(arrLit.Elems) > 0 {
					for _, elem := range arrLit.Elems {
						out.WriteString(fmt.Sprintf("%s{ %s _tmp_elem = ", prefix, elemCType))
						g.genExpr(out, elem)
						out.WriteString(fmt.Sprintf("; dex_array_struct_push(%s, &_tmp_elem); }\n", s.Name))
					}
				}
				g.registerScopeVar(s.Name, s.Type)
				break
			}
			cNewFn := g.arrayNewFunc(s.Type)
			out.WriteString(fmt.Sprintf("%s%s %s = %s();\n", prefix, g.cType(s.Type), s.Name, cNewFn))
			if len(arrLit.Elems) > 0 {
				// Inline initialize data
				for i, elem := range arrLit.Elems {
					if s.Type == ast.TypeArrayString {
						// For string arrays, we need to retain the string element
						out.WriteString(fmt.Sprintf("%s%s->data[%d] = ", prefix, s.Name, i))
						g.genExpr(out, elem)
						out.WriteString(";\n")
						// String literals from genExpr produce +1 refs via dex_string_from_lit
						// so no extra retain needed
					} else {
						out.WriteString(fmt.Sprintf("%s%s->data[%d] = ", prefix, s.Name, i))
						g.genExpr(out, elem)
						out.WriteString(";\n")
					}
				}
				out.WriteString(fmt.Sprintf("%s%s->len = %d;\n", prefix, s.Name, len(arrLit.Elems)))
			}
			g.registerScopeVar(s.Name, s.Type)
			break
		}
		// Special case for string declarations
		if s.Type == ast.TypeString {
			if ast.HasAnnotation(s.Annotations, ast.AnnotRegion) {
				// #[region] string — allocate from arena
				if strLit, ok := s.Value.(*ast.StringLit); ok {
					out.WriteString(fmt.Sprintf("%sDexString* %s = dex_arena_string(_arena, %q, %d);\n", prefix, s.Name, strLit.Value, len(strLit.Value)))
				} else {
					out.WriteString(fmt.Sprintf("%sDexString* %s = ", prefix, s.Name))
					g.genExpr(out, s.Value)
					out.WriteString(";\n")
				}
				g.registerScopeVar(s.Name, s.Type)
				break
			}
			if strLit, ok := s.Value.(*ast.StringLit); ok {
				out.WriteString(fmt.Sprintf("%sDexString* %s = dex_string_from_lit(%q);\n", prefix, s.Name, strLit.Value))
			} else {
				out.WriteString(fmt.Sprintf("%sDexString* %s = ", prefix, s.Name))
				g.genExpr(out, s.Value)
				out.WriteString(";\n")
				// If RHS is a borrowed reference (variable, array index, or field access), retain it
				// But not for #[owned] — ownership transfer, no retain
				isBorrowed := false
				if _, ok := s.Value.(*ast.Ident); ok {
					isBorrowed = true
				}
				if _, ok := s.Value.(*ast.IndexExpr); ok {
					isBorrowed = true
				}
				if _, ok := s.Value.(*ast.FieldAccessExpr); ok {
					isBorrowed = true
				}
				if isBorrowed && !ast.HasAnnotation(s.Annotations, ast.AnnotOwned) {
					out.WriteString(fmt.Sprintf("%sdex_retain(%s);\n", prefix, s.Name))
				}
			}
			g.registerScopeVar(s.Name, s.Type)
			// Emit debug cycle tracking if annotated
			if ast.HasAnnotation(s.Annotations, ast.AnnotDebugCycles) {
				out.WriteString(fmt.Sprintf("%sdex_cycle_track(%s, %q);\n", prefix, s.Name, s.Name))
			}
			break
		}
		// Special case for optional type declarations
		if ast.IsOptionalType(s.Type) {
			g.usesOptional = true
			inner := ast.OptionalInnerType(s.Type)
			_, isNull := s.Value.(*ast.NullLit)
			// Check if the RHS already produces the optional type (e.g., calling a function that returns T?)
			valType := g.typeOfExpr(s.Value)
			valIsOptional := valType == s.Type
			if ast.IsValueType(inner) {
				ctyp := g.cType(s.Type)
				if isNull {
					out.WriteString(fmt.Sprintf("%s%s %s = {0};\n", prefix, ctyp, s.Name))
				} else if valIsOptional {
					out.WriteString(fmt.Sprintf("%s%s %s = ", prefix, ctyp, s.Name))
					g.genExpr(out, s.Value)
					out.WriteString(";\n")
				} else {
					out.WriteString(fmt.Sprintf("%s%s %s = {1, ", prefix, ctyp, s.Name))
					g.genExpr(out, s.Value)
					out.WriteString("};\n")
				}
			} else if ast.IsStructType(inner) {
				ctyp := g.cType(s.Type) // Dex_Foo*
				if isNull {
					out.WriteString(fmt.Sprintf("%s%s %s = NULL;\n", prefix, ctyp, s.Name))
				} else {
					innerCType := "Dex_" + ast.StructName(inner)
					out.WriteString(fmt.Sprintf("%s%s %s = (%s*)malloc(sizeof(%s));\n", prefix, ctyp, s.Name, innerCType, innerCType))
					out.WriteString(fmt.Sprintf("%s*%s = ", prefix, s.Name))
					g.genExpr(out, s.Value)
					out.WriteString(";\n")
				}
			} else {
				// Heap type (string, array, etc.) — same pointer, NULL = absent
				ctyp := g.cType(s.Type)
				if isNull {
					out.WriteString(fmt.Sprintf("%s%s %s = NULL;\n", prefix, ctyp, s.Name))
				} else if inner == ast.TypeString {
					// Handle string optional like regular string init
					if strLit, ok := s.Value.(*ast.StringLit); ok {
						out.WriteString(fmt.Sprintf("%s%s %s = dex_string_from_lit(%q);\n", prefix, ctyp, s.Name, strLit.Value))
					} else {
						out.WriteString(fmt.Sprintf("%s%s %s = ", prefix, ctyp, s.Name))
						g.genExpr(out, s.Value)
						out.WriteString(";\n")
						if _, ok := s.Value.(*ast.Ident); ok {
							out.WriteString(fmt.Sprintf("%sdex_retain(%s);\n", prefix, s.Name))
						}
					}
				} else {
					out.WriteString(fmt.Sprintf("%s%s %s = ", prefix, ctyp, s.Name))
					g.genExpr(out, s.Value)
					out.WriteString(";\n")
				}
			}
			g.registerScopeVar(s.Name, s.Type)
			break
		}
		// Special case for ref type declarations
		if ast.IsRefType(s.Type) {
			ctyp := g.cType(s.Type)
			out.WriteString(fmt.Sprintf("%s%s %s = &", prefix, ctyp, s.Name))
			g.genExpr(out, s.Value)
			out.WriteString(";\n")
			g.varTypes[s.Name] = s.Type
			break
		}
		// Special case for weak reference declarations
		if ast.IsWeakType(s.Type) {
			out.WriteString(fmt.Sprintf("%sDexWeakRef* %s = dex_weak_new(", prefix, s.Name))
			g.genExpr(out, s.Value)
			out.WriteString(");\n")
			g.registerScopeVar(s.Name, s.Type)
			break
		}
		constPrefix := ""
		if s.IsConst {
			constPrefix = "const "
		}
		out.WriteString(fmt.Sprintf("%s%s%s %s = ", prefix, constPrefix, g.cType(s.Type), s.Name))
		g.genExpr(out, s.Value)
		out.WriteString(";\n")
		g.registerScopeVar(s.Name, s.Type)
		// Emit debug cycle tracking if annotated
		if ast.HasAnnotation(s.Annotations, ast.AnnotDebugCycles) && ast.IsHeapType(s.Type) {
			out.WriteString(fmt.Sprintf("%sdex_cycle_track(%s, %q);\n", prefix, s.Name, s.Name))
		}

	case *ast.ReturnStmt:
		// Bare return (no value) — void function
		if s.Value == nil {
			g.emitCleanupAll(out, prefix, "")
			out.WriteString(fmt.Sprintf("%sreturn;\n", prefix))
			break
		}
		retType := g.currentFn.ReturnType
		if ast.IsOptionalType(retType) {
			inner := ast.OptionalInnerType(retType)
			_, isNull := s.Value.(*ast.NullLit)
			if ast.IsValueType(inner) {
				ctyp := g.cType(retType)
				if isNull {
					g.emitCleanupAll(out, prefix, "")
					out.WriteString(fmt.Sprintf("%sreturn (%s){0};\n", prefix, ctyp))
				} else {
					out.WriteString(fmt.Sprintf("%s%s _ret_tmp = (%s){1, ", prefix, ctyp, ctyp))
					g.genExpr(out, s.Value)
					out.WriteString("};\n")
					g.emitCleanupAll(out, prefix, "")
					out.WriteString(fmt.Sprintf("%sreturn _ret_tmp;\n", prefix))
				}
			} else {
				// Heap/struct optional: NULL for null, value otherwise
				if isNull {
					g.emitCleanupAll(out, prefix, "")
					out.WriteString(fmt.Sprintf("%sreturn NULL;\n", prefix))
				} else if ast.IsHeapType(inner) || ast.IsHeapType(retType) {
					if ident, ok := s.Value.(*ast.Ident); ok {
						out.WriteString(fmt.Sprintf("%sdex_retain(%s);\n", prefix, ident.Name))
						g.emitCleanupAll(out, prefix, "")
						out.WriteString(fmt.Sprintf("%sreturn %s;\n", prefix, ident.Name))
					} else {
						ctyp := g.cType(retType)
						out.WriteString(fmt.Sprintf("%s%s _ret_tmp = ", prefix, ctyp))
						g.genExpr(out, s.Value)
						out.WriteString(";\n")
						g.emitCleanupAll(out, prefix, "")
						out.WriteString(fmt.Sprintf("%sreturn _ret_tmp;\n", prefix))
					}
				} else {
					out.WriteString(fmt.Sprintf("%sreturn ", prefix))
					g.genExpr(out, s.Value)
					out.WriteString(";\n")
				}
			}
			break
		}
		if ast.IsHeapType(retType) {
			// Retain the return value, clean up everything else, then return
			if ident, ok := s.Value.(*ast.Ident); ok {
				out.WriteString(fmt.Sprintf("%sdex_retain(%s);\n", prefix, ident.Name))
				g.emitCleanupAll(out, prefix, "")
				out.WriteString(fmt.Sprintf("%sreturn %s;\n", prefix, ident.Name))
			} else {
				// Expression result — eval into temp
				out.WriteString(fmt.Sprintf("%s%s _ret_tmp = ", prefix, g.cType(retType)))
				g.genExpr(out, s.Value)
				out.WriteString(";\n")
				g.emitCleanupAll(out, prefix, "")
				out.WriteString(fmt.Sprintf("%sreturn _ret_tmp;\n", prefix))
			}
		} else {
			// Non-heap return — clean up all heap vars first
			if g.hasHeapVarsInScope() {
				// Evaluate return value into temp to avoid use-after-free
				ctyp := g.cType(retType)
				if retType != ast.TypeVoid {
					out.WriteString(fmt.Sprintf("%s%s _ret_tmp = ", prefix, ctyp))
					g.genExpr(out, s.Value)
					out.WriteString(";\n")
					g.emitCleanupAll(out, prefix, "")
					out.WriteString(fmt.Sprintf("%sreturn _ret_tmp;\n", prefix))
				} else {
					g.emitCleanupAll(out, prefix, "")
					out.WriteString(fmt.Sprintf("%sreturn;\n", prefix))
				}
			} else {
				out.WriteString(fmt.Sprintf("%sreturn ", prefix))
				g.genExpr(out, s.Value)
				out.WriteString(";\n")
			}
		}

	case *ast.ExprStmt:
		// Fire-and-forget spawn: suppress unused value warning by casting to void
		if _, ok := s.Expr.(*ast.SpawnExpr); ok {
			out.WriteString(prefix + "(void)")
			g.genExpr(out, s.Expr)
			out.WriteString(";\n")
			break
		}
		// Check if this is a call that returns a heap type we need to release
		if call, ok := s.Expr.(*ast.CallExpr); ok {
			callRetType := g.typeOfExpr(call)
			if ast.IsHeapType(callRetType) {
				// Result is discarded, but may be a new allocation — release it
				out.WriteString(fmt.Sprintf("%sdex_release(", prefix))
				g.genExpr(out, s.Expr)
				out.WriteString(");\n")
				break
			}
		}
		out.WriteString(prefix)
		g.genExpr(out, s.Expr)
		out.WriteString(";\n")

	case *ast.AssignStmt:
		varType := g.varTypes[s.Name]
		if isPrimitiveRef(varType) {
			out.WriteString(fmt.Sprintf("%s(*%s) = ", prefix, s.Name))
			g.genExpr(out, s.Value)
			out.WriteString(";\n")
			break
		}
		if ast.IsOptionalType(varType) {
			inner := ast.OptionalInnerType(varType)
			_, isNull := s.Value.(*ast.NullLit)
			if ast.IsValueType(inner) {
				ctyp := g.cType(varType)
				if isNull {
					out.WriteString(fmt.Sprintf("%s%s = (%s){0};\n", prefix, s.Name, ctyp))
				} else {
					out.WriteString(fmt.Sprintf("%s%s = (%s){1, ", prefix, s.Name, ctyp))
					g.genExpr(out, s.Value)
					out.WriteString("};\n")
				}
			} else if ast.IsStructType(inner) {
				if isNull {
					innerCType := "Dex_" + ast.StructName(inner)
					out.WriteString(fmt.Sprintf("%sif (%s) { free(%s); } %s = NULL;\n", prefix, s.Name, s.Name, s.Name))
					_ = innerCType
				} else {
					innerCType := "Dex_" + ast.StructName(inner)
					out.WriteString(fmt.Sprintf("%sif (!%s) { %s = (%s*)malloc(sizeof(%s)); }\n", prefix, s.Name, s.Name, innerCType, innerCType))
					out.WriteString(fmt.Sprintf("%s*%s = ", prefix, s.Name))
					g.genExpr(out, s.Value)
					out.WriteString(";\n")
				}
			} else {
				// Heap type optional (string, array, etc.)
				if isNull {
					if ast.NeedsRelease(inner) {
						out.WriteString(fmt.Sprintf("%sif (%s) { dex_release(%s); } %s = NULL;\n", prefix, s.Name, s.Name, s.Name))
					} else {
						out.WriteString(fmt.Sprintf("%s%s = NULL;\n", prefix, s.Name))
					}
				} else {
					if ast.NeedsRelease(inner) {
						out.WriteString(fmt.Sprintf("%s{ %s _dex_old = %s; %s = ", prefix, g.cType(varType), s.Name, s.Name))
						g.genExpr(out, s.Value)
						out.WriteString(";")
						isBorrowed := false
						if _, ok := s.Value.(*ast.Ident); ok {
							isBorrowed = true
						}
						if _, ok := s.Value.(*ast.IndexExpr); ok {
							isBorrowed = true
						}
						if _, ok := s.Value.(*ast.FieldAccessExpr); ok {
							isBorrowed = true
						}
						if isBorrowed {
							out.WriteString(fmt.Sprintf(" dex_retain(%s);", s.Name))
						}
						out.WriteString(" if (_dex_old) { dex_release(_dex_old); } }\n")
					} else {
						out.WriteString(fmt.Sprintf("%s%s = ", prefix, s.Name))
						g.genExpr(out, s.Value)
						out.WriteString(";\n")
					}
				}
			}
			break
		}
		if ast.IsHeapType(varType) {
			// For heap-typed reassignment: old = var; var = new_val; release(old);
			out.WriteString(fmt.Sprintf("%s{ %s _dex_old = %s; %s = ", prefix, g.cType(varType), s.Name, s.Name))
			g.genExpr(out, s.Value)
			out.WriteString(";")
			// If the RHS is a borrowed reference (variable, array index, or field access), retain
			isBorrowed := false
			if _, ok := s.Value.(*ast.Ident); ok {
				isBorrowed = true
			}
			if _, ok := s.Value.(*ast.IndexExpr); ok {
				isBorrowed = true
			}
			if _, ok := s.Value.(*ast.FieldAccessExpr); ok {
				isBorrowed = true
			}
			if isBorrowed {
				out.WriteString(fmt.Sprintf(" dex_retain(%s);", s.Name))
			}
			out.WriteString(" dex_release(_dex_old); }\n")
		} else {
			out.WriteString(fmt.Sprintf("%s%s = ", prefix, s.Name))
			g.genExpr(out, s.Value)
			out.WriteString(";\n")
		}

	case *ast.IfStmt:
		out.WriteString(fmt.Sprintf("%sif (", prefix))
		g.genExprNoParen(out, s.Cond)
		out.WriteString(") {\n")
		g.pushScope()
		// Detect null check for type narrowing
		varName, isNotNull := extractNullCheckCodegen(s.Cond)
		if varName != "" && isNotNull {
			g.emitNarrowing(out, prefix+"    ", varName)
		}
		for _, stmt := range s.Then {
			g.genStmt(out, stmt, indent+1)
		}
		g.clearNarrowing(varName)
		g.popScope(out, prefix+"    ")
		if s.Else != nil {
			out.WriteString(fmt.Sprintf("%s} else {\n", prefix))
			g.pushScope()
			if varName != "" && !isNotNull {
				g.emitNarrowing(out, prefix+"    ", varName)
			}
			for _, stmt := range s.Else {
				g.genStmt(out, stmt, indent+1)
			}
			if varName != "" && !isNotNull {
				g.clearNarrowing(varName)
			}
			g.popScope(out, prefix+"    ")
		}
		out.WriteString(fmt.Sprintf("%s}\n", prefix))

	case *ast.WhileStmt:
		out.WriteString(fmt.Sprintf("%swhile (", prefix))
		g.genExprNoParen(out, s.Cond)
		out.WriteString(") {\n")
		savedLoop := g.isInLoop
		savedDepth := g.loopDepth
		g.isInLoop = true
		g.loopDepth = len(g.scopeStack)
		g.pushScope()
		for _, stmt := range s.Body {
			g.genStmt(out, stmt, indent+1)
		}
		g.popScope(out, prefix+"    ")
		g.isInLoop = savedLoop
		g.loopDepth = savedDepth
		out.WriteString(fmt.Sprintf("%s}\n", prefix))

	case *ast.ForStmt:
		out.WriteString(fmt.Sprintf("%sfor (", prefix))
		g.genForInit(out, s.Init)
		out.WriteString("; ")
		g.genExprNoParen(out, s.Cond)
		out.WriteString("; ")
		g.genForPost(out, s.Post)
		out.WriteString(") {\n")
		savedLoop := g.isInLoop
		savedDepth := g.loopDepth
		g.isInLoop = true
		g.loopDepth = len(g.scopeStack)
		g.pushScope()
		for _, stmt := range s.Body {
			g.genStmt(out, stmt, indent+1)
		}
		g.popScope(out, prefix+"    ")
		g.isInLoop = savedLoop
		g.loopDepth = savedDepth
		out.WriteString(fmt.Sprintf("%s}\n", prefix))

	case *ast.ForeachStmt:
		idx := g.foreachCounter
		g.foreachCounter++
		idxVar := fmt.Sprintf("_foreach_idx_%d", idx)
		// Determine the array expression name for ->len and ->data access
		arrExpr := g.exprToString(s.Iterable)
		// Get element type from the iterable
		arrType := g.typeOfExpr(s.Iterable)
		elemType := ast.ElementType(arrType)
		elemCType := g.cType(elemType)

		out.WriteString(fmt.Sprintf("%sfor (int %s = 0; %s < %s->len; %s++) {\n",
			prefix, idxVar, idxVar, arrExpr, idxVar))
		savedLoop := g.isInLoop
		savedDepth := g.loopDepth
		g.isInLoop = true
		g.loopDepth = len(g.scopeStack)
		g.pushScope()
		// Declare value variable
		innerPrefix := strings.Repeat("    ", indent+1)
		if ast.IsStructArrayType(arrType) {
			out.WriteString(fmt.Sprintf("%s%s %s = *(%s*)dex_array_struct_get(%s, %s);\n",
				innerPrefix, elemCType, s.ValueVar, elemCType, arrExpr, idxVar))
		} else {
			out.WriteString(fmt.Sprintf("%s%s %s = %s->data[%s];\n",
				innerPrefix, elemCType, s.ValueVar, arrExpr, idxVar))
		}
		// Register the value variable type
		g.varTypes[s.ValueVar] = elemType
		if elemType == ast.TypeString {
			g.strVars[s.ValueVar] = true
		}
		if ast.IsStructType(elemType) {
			g.structVars[s.ValueVar] = elemType
		}
		// Declare index variable if used
		if s.IndexVar != "" {
			out.WriteString(fmt.Sprintf("%sint %s = %s;\n", innerPrefix, s.IndexVar, idxVar))
			g.varTypes[s.IndexVar] = ast.TypeInt
		}
		for _, stmt := range s.Body {
			g.genStmt(out, stmt, indent+1)
		}
		g.popScope(out, innerPrefix)
		g.isInLoop = savedLoop
		g.loopDepth = savedDepth
		out.WriteString(fmt.Sprintf("%s}\n", prefix))

	case *ast.BreakStmt:
		if g.isInLoop {
			g.emitCleanupInnerScopes(out, prefix, g.loopDepth)
		}
		out.WriteString(fmt.Sprintf("%sbreak;\n", prefix))

	case *ast.ContinueStmt:
		if g.isInLoop {
			g.emitCleanupInnerScopes(out, prefix, g.loopDepth)
		}
		out.WriteString(fmt.Sprintf("%scontinue;\n", prefix))

	case *ast.IncrementStmt:
		if t, ok := g.varTypes[s.Name]; ok && isPrimitiveRef(t) {
			out.WriteString(fmt.Sprintf("%s(*%s)++;\n", prefix, s.Name))
		} else {
			out.WriteString(fmt.Sprintf("%s%s++;\n", prefix, s.Name))
		}

	case *ast.DecrementStmt:
		if t, ok := g.varTypes[s.Name]; ok && isPrimitiveRef(t) {
			out.WriteString(fmt.Sprintf("%s(*%s)--;\n", prefix, s.Name))
		} else {
			out.WriteString(fmt.Sprintf("%s%s--;\n", prefix, s.Name))
		}

	case *ast.CompoundAssignStmt:
		if t, ok := g.varTypes[s.Name]; ok && isPrimitiveRef(t) {
			out.WriteString(fmt.Sprintf("%s(*%s) %s= ", prefix, s.Name, g.cBinOp(s.Op)))
		} else {
			out.WriteString(fmt.Sprintf("%s%s %s= ", prefix, s.Name, g.cBinOp(s.Op)))
		}
		g.genExpr(out, s.Value)
		out.WriteString(";\n")

	case *ast.IndexAssignStmt:
		arrType := g.typeOfExpr(s.Array)
		if ast.IsMapType(arrType) {
			suffix := g.mapSuffix(arrType)
			out.WriteString(fmt.Sprintf("%sdex_map_%s_set(", prefix, suffix))
			g.genExpr(out, s.Array)
			out.WriteString(", ")
			g.genExpr(out, s.Index)
			out.WriteString(", ")
			g.genExpr(out, s.Value)
			out.WriteString(");\n")
			break
		}
		if ast.IsStructArrayType(arrType) {
			elemType := ast.ElementType(arrType)
			elemCType := g.cType(elemType)
			out.WriteString(prefix)
			out.WriteString("dex_bounds_check(")
			g.genExpr(out, s.Index)
			out.WriteString(", ")
			g.genExpr(out, s.Array)
			out.WriteString("->len);\n")
			out.WriteString(fmt.Sprintf("%s{ %s _assign_tmp = ", prefix, elemCType))
			g.genExpr(out, s.Value)
			out.WriteString(fmt.Sprintf("; memcpy(dex_array_struct_get("))
			g.genExpr(out, s.Array)
			out.WriteString(", ")
			g.genExpr(out, s.Index)
			out.WriteString(fmt.Sprintf("), &_assign_tmp, sizeof(%s)); }\n", elemCType))
		} else {
			out.WriteString(prefix)
			out.WriteString("dex_bounds_check(")
			g.genExpr(out, s.Index)
			out.WriteString(", ")
			g.genExpr(out, s.Array)
			out.WriteString("->len);\n")
			out.WriteString(prefix)
			g.genExpr(out, s.Array)
			out.WriteString("->data[")
			g.genExpr(out, s.Index)
			out.WriteString("] = ")
			g.genExpr(out, s.Value)
			out.WriteString(";\n")
		}

	case *ast.FieldAssignStmt:
		out.WriteString(prefix)
		g.genExpr(out, s.Object)
		objType := g.typeOfExpr(s.Object)
		if ast.IsRefType(objType) {
			out.WriteString(fmt.Sprintf("->%s = ", s.Field))
		} else {
			out.WriteString(fmt.Sprintf(".%s = ", s.Field))
		}
		g.genExpr(out, s.Value)
		out.WriteString(";\n")
		// Emit cycle check if debug(cycles) is enabled and field is heap-typed
		if g.usesDebugCycles {
			fieldType := g.typeOfExpr(s.Value)
			if ast.IsHeapType(fieldType) {
				out.WriteString(fmt.Sprintf("%sdex_cycle_check_assign(&", prefix))
				g.genExpr(out, s.Object)
				out.WriteString(", ")
				g.genExpr(out, s.Value)
				out.WriteString(");\n")
			}
		}

	case *ast.BlockStmt:
		out.WriteString(fmt.Sprintf("%s{\n", prefix))
		g.pushScope()
		for _, stmt := range s.Stmts {
			g.genStmt(out, stmt, indent+1)
		}
		g.popScope(out, prefix+"    ")
		out.WriteString(fmt.Sprintf("%s}\n", prefix))

	case *ast.SendStmt:
		valType := g.typeOfExpr(s.Value)
		ctyp := g.cType(valType)
		out.WriteString(fmt.Sprintf("%s{ %s _send_val = ", prefix, ctyp))
		g.genExpr(out, s.Value)
		out.WriteString("; dex_chan_send(")
		if s.Target != nil {
			g.genExpr(out, s.Target)
		} else {
			out.WriteString("_ch")
		}
		out.WriteString(", &_send_val); }\n")

	case *ast.SwitchStmt:
		id := g.switchCounter
		g.switchCounter++
		// Evaluate the tag into a temp variable to avoid re-evaluation
		tagType := g.typeOfExpr(s.Tag)
		tagVar := fmt.Sprintf("_switch_tag_%d", id)
		out.WriteString(fmt.Sprintf("%s%s %s = ", prefix, g.cType(tagType), tagVar))
		g.genExpr(out, s.Tag)
		out.WriteString(";\n")
		// Generate as if/else if/else chain (supports string comparison via strcmp)
		isString := tagType == ast.TypeString
		for i, sc := range s.Cases {
			if i == 0 {
				out.WriteString(fmt.Sprintf("%sif (", prefix))
			} else {
				out.WriteString(fmt.Sprintf("%s} else if (", prefix))
			}
			for j, val := range sc.Values {
				if j > 0 {
					out.WriteString(" || ")
				}
				if isString {
					out.WriteString(fmt.Sprintf("strcmp(%s->data, ", tagVar))
					g.genStringData(out, val)
					out.WriteString(") == 0")
				} else {
					out.WriteString(fmt.Sprintf("%s == ", tagVar))
					g.genExpr(out, val)
				}
			}
			out.WriteString(") {\n")
			g.pushScope()
			for _, stmt := range sc.Body {
				g.genStmt(out, stmt, indent+1)
			}
			g.popScope(out, prefix+"    ")
		}
		if s.Default != nil {
			if len(s.Cases) > 0 {
				out.WriteString(fmt.Sprintf("%s} else {\n", prefix))
			} else {
				out.WriteString(fmt.Sprintf("%s{\n", prefix))
			}
			g.pushScope()
			for _, stmt := range s.Default {
				g.genStmt(out, stmt, indent+1)
			}
			g.popScope(out, prefix+"    ")
			out.WriteString(fmt.Sprintf("%s}\n", prefix))
		} else if len(s.Cases) > 0 {
			out.WriteString(fmt.Sprintf("%s}\n", prefix))
		}
		// Release the tag temp if it was a new allocation (not a variable reference)
		if ast.IsHeapType(tagType) && g.isNewAlloc(s.Tag) {
			out.WriteString(fmt.Sprintf("%sdex_release(%s);\n", prefix, tagVar))
		}

	case *ast.ThrowStmt:
		// Extract message from Exception constructor: Exception("msg") -> _dex_throw(msg)
		out.WriteString(fmt.Sprintf("%s_dex_throw(", prefix))
		if call, ok := s.Value.(*ast.CallExpr); ok && call.Name == "Exception" && len(call.Args) == 1 {
			// Direct constructor call: extract message arg
			if strLit, ok := call.Args[0].(*ast.StringLit); ok {
				out.WriteString(fmt.Sprintf("%q", strLit.Value))
			} else {
				// Expression that produces a DexString — extract ->data
				out.WriteString("(")
				g.genExpr(out, call.Args[0])
				out.WriteString(")->data")
			}
		} else {
			// General expression that produces an Exception struct — extract .message->data
			out.WriteString("(")
			g.genExpr(out, s.Value)
			out.WriteString(").message->data")
		}
		out.WriteString(");\n")

	case *ast.TryCatchStmt:
		id := g.tryCatchCounter
		g.tryCatchCounter++
		hasCatch := s.CatchBody != nil
		hasFinally := s.FinallyBody != nil

		// Push exception frame
		out.WriteString(fmt.Sprintf("%s_dex_exc_top++;\n", prefix))
		out.WriteString(fmt.Sprintf("%svolatile int _dex_exc_%d_caught = 0;\n", prefix, id))
		out.WriteString(fmt.Sprintf("%sif (setjmp(_dex_exc_stack[_dex_exc_top].env) == 0) {\n", prefix))
		out.WriteString(fmt.Sprintf("%s    _dex_exc_stack[_dex_exc_top].active = 0;\n", prefix))

		// Try body
		g.pushScope()
		for _, stmt := range s.Body {
			g.genStmt(out, stmt, indent+1)
		}
		g.popScope(out, prefix+"    ")
		out.WriteString(fmt.Sprintf("%s} else {\n", prefix))
		out.WriteString(fmt.Sprintf("%s    _dex_exc_%d_caught = 1;\n", prefix, id))
		out.WriteString(fmt.Sprintf("%s}\n", prefix))

		// Pop exception frame
		out.WriteString(fmt.Sprintf("%s_dex_exc_top--;\n", prefix))

		// Catch block
		if hasCatch {
			out.WriteString(fmt.Sprintf("%sif (_dex_exc_%d_caught) {\n", prefix, id))
			// Create Exception struct with message from exception stack
			out.WriteString(fmt.Sprintf("%s    DexString* %s_msg = dex_string_from_lit(_dex_exc_stack[_dex_exc_top + 1].message);\n", prefix, s.CatchVar))
			out.WriteString(fmt.Sprintf("%s    Dex_Exception %s = { .message = %s_msg };\n", prefix, s.CatchVar, s.CatchVar))

			g.pushScope()
			excType, _ := ast.LookupStructType("Exception")
			g.varTypes[s.CatchVar] = excType
			g.structVars[s.CatchVar] = excType
			g.registerScopeVar(s.CatchVar+"_msg", ast.TypeString)
			for _, stmt := range s.CatchBody {
				g.genStmt(out, stmt, indent+1)
			}
			g.popScope(out, prefix+"    ")
			out.WriteString(fmt.Sprintf("%s}\n", prefix))
		}

		// Finally block
		if hasFinally {
			g.pushScope()
			for _, stmt := range s.FinallyBody {
				g.genStmt(out, stmt, indent)
			}
			g.popScope(out, prefix)
		}

		// If no catch clause, re-throw after finally
		if !hasCatch {
			out.WriteString(fmt.Sprintf("%sif (_dex_exc_%d_caught) {\n", prefix, id))
			out.WriteString(fmt.Sprintf("%s    _dex_throw(_dex_exc_stack[_dex_exc_top + 1].message);\n", prefix))
			out.WriteString(fmt.Sprintf("%s}\n", prefix))
		}
	}
}

// extractNullCheckCodegen detects null comparison patterns in conditions for codegen narrowing.
func extractNullCheckCodegen(cond ast.Expr) (string, bool) {
	binExpr, ok := cond.(*ast.BinaryExpr)
	if !ok {
		return "", false
	}
	if binExpr.Op != ast.BinEq && binExpr.Op != ast.BinNeq {
		return "", false
	}
	if ident, ok := binExpr.Left.(*ast.Ident); ok {
		if _, ok := binExpr.Right.(*ast.NullLit); ok {
			return ident.Name, binExpr.Op == ast.BinNeq
		}
	}
	if _, ok := binExpr.Left.(*ast.NullLit); ok {
		if ident, ok := binExpr.Right.(*ast.Ident); ok {
			return ident.Name, binExpr.Op == ast.BinNeq
		}
	}
	return "", false
}

// emitNarrowing emits a narrowed variable declaration for an optional variable
// and registers it in the narrowedVars map so genExpr uses the narrowed name.
func (g *Generator) emitNarrowing(out *strings.Builder, prefix, varName string) {
	varType, ok := g.varTypes[varName]
	if !ok || !ast.IsOptionalType(varType) {
		return
	}
	inner := ast.OptionalInnerType(varType)
	if ast.IsValueType(inner) {
		narrowedName := "_narrow_" + varName
		out.WriteString(fmt.Sprintf("%s%s %s = %s.value;\n", prefix, g.cType(inner), narrowedName, varName))
		g.narrowedVars[varName] = narrowedName
	}
	// For heap types (string, array), no narrowing needed — same pointer
	// For struct types, no narrowing needed — it's already a pointer
}

// clearNarrowing removes a variable from the narrowedVars map.
func (g *Generator) clearNarrowing(varName string) {
	delete(g.narrowedVars, varName)
}

// genForInit generates the init part of a for loop (no trailing semicolon).
func (g *Generator) genForInit(out *strings.Builder, stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		g.varTypes[s.Name] = s.Type
		if s.Type == ast.TypeString {
			g.strVars[s.Name] = true
		}
		if ast.IsArrayType(s.Type) {
			g.arrVars[s.Name] = s.Type
		}
		out.WriteString(fmt.Sprintf("%s %s = ", g.cType(s.Type), s.Name))
		g.genExpr(out, s.Value)
	case *ast.AssignStmt:
		out.WriteString(fmt.Sprintf("%s = ", s.Name))
		g.genExpr(out, s.Value)
	}
}

// genForPost generates the post part of a for loop (no trailing semicolon).
func (g *Generator) genForPost(out *strings.Builder, stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.IncrementStmt:
		if t, ok := g.varTypes[s.Name]; ok && isPrimitiveRef(t) {
			out.WriteString(fmt.Sprintf("(*%s)++", s.Name))
		} else {
			out.WriteString(fmt.Sprintf("%s++", s.Name))
		}
	case *ast.DecrementStmt:
		if t, ok := g.varTypes[s.Name]; ok && isPrimitiveRef(t) {
			out.WriteString(fmt.Sprintf("(*%s)--", s.Name))
		} else {
			out.WriteString(fmt.Sprintf("%s--", s.Name))
		}
	case *ast.CompoundAssignStmt:
		if t, ok := g.varTypes[s.Name]; ok && isPrimitiveRef(t) {
			out.WriteString(fmt.Sprintf("(*%s) %s= ", s.Name, g.cBinOp(s.Op)))
		} else {
			out.WriteString(fmt.Sprintf("%s %s= ", s.Name, g.cBinOp(s.Op)))
		}
		g.genExpr(out, s.Value)
	case *ast.AssignStmt:
		if isPrimitiveRef(g.varTypes[s.Name]) {
			out.WriteString(fmt.Sprintf("(*%s) = ", s.Name))
		} else {
			out.WriteString(fmt.Sprintf("%s = ", s.Name))
		}
		g.genExpr(out, s.Value)
	}
}

// exprToString renders an expression to a string (for use in foreach).
func (g *Generator) exprToString(expr ast.Expr) string {
	var buf strings.Builder
	g.genExpr(&buf, expr)
	return buf.String()
}

// isNewAlloc returns true if the expression produces a +1 ref (new allocation).
// Variable references are borrowed (not +1).
func (g *Generator) isNewAlloc(expr ast.Expr) bool {
	switch expr.(type) {
	case *ast.Ident:
		return false // borrowed reference
	case *ast.FieldAccessExpr:
		return false // borrowed from struct field
	case *ast.IndexExpr:
		return false // borrowed from array/map element
	case *ast.StringLit:
		return true // dex_string_from_lit produces +1
	case *ast.CallExpr:
		return true // function calls produce +1
	case *ast.BinaryExpr:
		return true // concat produces +1
	case *ast.ReceiveExpr:
		return true // channel receive produces +1
	default:
		return false // conservative: assume borrowed to avoid premature free
	}
}
