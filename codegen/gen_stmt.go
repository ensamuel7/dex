package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

func (g *Generator) genStmtInner(out *strings.Builder, stmt ast.Stmt, indent int) {
	prefix := strings.Repeat("    ", indent)

	switch s := stmt.(type) {
	case *ast.LetStmt:
		g.genLetStmt(out, s, prefix, indent)
	case *ast.ReturnStmt:
		g.genReturnStmt(out, s, prefix, indent)
	case *ast.ExprStmt:
		// Fire-and-forget spawn: nobody keeps the task handle, so the caller's
		// reference to its result channel is dropped here. The worker holds its
		// own reference, so the channel stays alive as long as the task needs it.
		if _, ok := s.Expr.(*ast.SpawnExpr); ok {
			out.WriteString(prefix + "dex_release(")
			g.genExpr(out, s.Expr)
			out.WriteString(");\n")
			break
		}
		// Check if this is a call that returns a heap type we need to release
		if call, ok := s.Expr.(*ast.CallExpr); ok {
			callRetType := g.typeOfExpr(call)
			if ast.IsHeapType(callRetType) {
				// Result is discarded, but may be a new allocation — release it
				g.beginStmtHoist()
				var body strings.Builder
				body.WriteString("dex_release(")
				g.genExpr(&body, s.Expr)
				body.WriteString(");\n")
				g.emitWithHoists(out, prefix, body.String())
				break
			}
		}
		g.beginStmtHoist()
		var exprBody strings.Builder
		g.genExpr(&exprBody, s.Expr)
		exprBody.WriteString(";\n")
		g.emitWithHoists(out, prefix, exprBody.String())

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
			// A borrowed right-hand side is still owned by wherever it came from,
			// so the variable becomes a second owner. One that was built here —
			// a method value, an indexed json.Value — already carries its own
			// reference and simply moves.
			if g.borrowsHeapValue(s.Value) {
				out.WriteString(fmt.Sprintf(" dex_retain(%s);", s.Name))
			}
			out.WriteString(" dex_release(_dex_old); }\n")
		} else if ast.IsStructType(varType) && ast.NeedsRelease(varType) {
			// Reassigning a struct replaces every heap field it owns. Retain what the
			// new value borrows before releasing the old, so `x = T{f: x.f}` is safe.
			def := ast.GetStructDef(varType)
			ctyp := g.cType(varType)
			out.WriteString(fmt.Sprintf("%s{ %s _dex_old = %s; %s = ", prefix, ctyp, s.Name, s.Name))
			g.genExpr(out, s.Value)
			out.WriteString(";\n")
			if lit, ok := s.Value.(*ast.StructLitExpr); ok {
				g.emitRetainStructLitFields(out, prefix+"    ", s.Name, varType, lit)
			} else if g.borrowsHeapValue(s.Value) {
				// Borrowed whole struct — the target becomes a second owner.
				for _, f := range def.Fields {
					if ast.IsHeapType(f.Type) {
						out.WriteString(fmt.Sprintf("%s    dex_retain(%s.%s);\n", prefix, s.Name, f.Name))
					}
				}
			}
			for _, f := range def.Fields {
				if ast.NeedsRelease(f.Type) {
					g.emitReleaseVar(out, prefix+"    ", "_dex_old."+f.Name, f.Type)
				}
			}
			out.WriteString(fmt.Sprintf("%s}\n", prefix))
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
		// Guard clause: everything after `if (x == null) { return ... }` runs only
		// when x is non-null, so bind the narrowed form for the rest of the block.
		if varName != "" && !isNotNull && s.Else == nil && alwaysExitsCodegen(s.Then) {
			g.emitNarrowing(out, prefix, varName)
		}

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
			// The map retains what it stores, so key and value are borrowed here
			// and any allocating operand is released after the statement.
			suffix := g.mapSuffix(arrType)
			g.beginStmtHoist()
			var body strings.Builder
			body.WriteString(fmt.Sprintf("dex_map_%s_set(", suffix))
			g.genExpr(&body, s.Array)
			body.WriteString(", ")
			g.genBorrowed(&body, s.Index)
			body.WriteString(", ")
			g.genBorrowed(&body, s.Value)
			body.WriteString(");\n")
			g.emitWithHoists(out, prefix, body.String())
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
		objType := g.typeOfExpr(s.Object)
		fieldType := g.typeOfExpr(s.Value)
		// For ref-type objects (&Struct), retain/release heap-typed fields
		// to keep proper reference counts when the struct outlives the function scope
		if ast.IsHeapType(fieldType) {
			// A struct field owns its reference, whether the struct is behind a ref or
			// held by value. Evaluate once into a temp to avoid double-evaluating side
			// effects, retain a borrowed value (an owned temporary already carries the
			// only reference, so retaining it would leak), release whatever the field
			// held before, then assign.
			accessOp := "."
			if ast.IsRefType(objType) {
				accessOp = "->"
			}
			tmpVal := g.nextTemp()
			out.WriteString(fmt.Sprintf("%s%s %s = ", prefix, g.cType(fieldType), tmpVal))
			g.genExpr(out, s.Value)
			out.WriteString(";\n")
			if g.borrowsHeapValue(s.Value) {
				out.WriteString(fmt.Sprintf("%sdex_retain(%s);\n", prefix, tmpVal))
			}
			out.WriteString(fmt.Sprintf("%sdex_release(", prefix))
			g.genExpr(out, s.Object)
			out.WriteString(fmt.Sprintf("%s%s);\n", accessOp, s.Field))
			out.WriteString(prefix)
			g.genExpr(out, s.Object)
			out.WriteString(fmt.Sprintf("%s%s = %s;\n", accessOp, s.Field, tmpVal))
		} else {
			out.WriteString(prefix)
			g.genExpr(out, s.Object)
			if ast.IsRefType(objType) {
				out.WriteString(fmt.Sprintf("->%s = ", s.Field))
			} else {
				out.WriteString(fmt.Sprintf(".%s = ", s.Field))
			}
			g.genExpr(out, s.Value)
			out.WriteString(";\n")
		}
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
		// Emit deferred calls before throwing
		g.emitDeferredCalls(out, prefix)
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

	case *ast.DeferStmt:
		// Accumulate deferred expression (will be emitted at function exit / return)
		g.deferExprs = append(g.deferExprs, s.Expr)

	case *ast.DestructureLetStmt:
		// Generate RHS into a temp variable
		rhsType := g.typeOfExpr(s.Value)
		if !ast.IsStructType(rhsType) {
			break
		}
		def := ast.GetStructDef(rhsType)
		if def == nil {
			break
		}
		tmpName := fmt.Sprintf("_destruct_%d", g.tempCounter)
		g.tempCounter++
		out.WriteString(fmt.Sprintf("%s%s %s = ", prefix, g.cType(rhsType), tmpName))
		g.genExpr(out, s.Value)
		out.WriteString(";\n")

		// For each destructured name, emit variable declaration from field access
		for _, name := range s.Names {
			for _, f := range def.Fields {
				if f.Name == name {
					g.varTypes[name] = f.Type
					if f.Type == ast.TypeString {
						g.strVars[name] = true
					}
					if ast.IsArrayType(f.Type) {
						g.arrVars[name] = f.Type
					}
					if ast.IsStructType(f.Type) {
						g.structVars[name] = f.Type
					}
					out.WriteString(fmt.Sprintf("%s%s %s = %s.%s;\n", prefix, g.cType(f.Type), name, tmpName, name))
					// Retain heap types since we're creating a new reference
					if ast.IsHeapType(f.Type) {
						out.WriteString(fmt.Sprintf("%sdex_retain(%s);\n", prefix, name))
						g.registerScopeVar(name, f.Type)
					}
					break
				}
			}
		}
	}
}
