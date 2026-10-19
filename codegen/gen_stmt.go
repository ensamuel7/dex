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
			if ast.NeedsRelease(callRetType) && !g.isHandleOutCall(call) {
				g.beginStmtHoist()
				var body strings.Builder
				body.WriteString(fmt.Sprintf("{ %s _dex_discard = ", g.cType(callRetType)))
				g.genExpr(&body, s.Expr)
				body.WriteString(";\n")
				g.emitReleaseVar(&body, prefix+"    ", "_dex_discard", callRetType)
				body.WriteString(prefix + "}\n")
				g.emitWithHoists(out, prefix, body.String())
				break
			}
		}
		g.beginStmtHoist()
		var exprBody strings.Builder
		if call, ok := s.Expr.(*ast.CallExpr); ok && g.isHandleOutCall(call) {
			exprBody.WriteString("(void)")
		}
		g.genExpr(&exprBody, s.Expr)
		exprBody.WriteString(";\n")
		g.emitWithHoists(out, prefix, exprBody.String())

	case *ast.AssignStmt:
		varType := g.varTypes[s.Name]
		if ast.IsInterfaceType(varType) {
			// The old box is released after the new value is in place, so
			// assigning a variable from a borrowed read of itself cannot free
			// what it is about to hold.
			alreadyIface := ast.IsInterfaceType(g.typeOfExpr(s.Value))
			out.WriteString(fmt.Sprintf("%s{ void* _dex_old = %s._obj; %s = ", prefix, s.Name, s.Name))
			if alreadyIface {
				g.genExpr(out, s.Value)
			} else {
				g.genInterfaceValue(out, s.Value, varType)
			}
			out.WriteString(";")
			// Only a *borrowed* interface value needs a second owner. Keying
			// this on "the right-hand side is already an interface" retained a
			// box that was minted rather than borrowed — `r = makeExpr()`
			// handed over its only reference and then took another, so the new
			// box was never freed and one leaked per assignment. This is the
			// same question the heap-type path below asks.
			if alreadyIface && g.borrowsHeapValue(s.Value) {
				// Forwarded rather than minted: the source still owns it.
				out.WriteString(fmt.Sprintf(" dex_retain(%s._obj);", s.Name))
			}
			out.WriteString(" if (_dex_old) { dex_release(_dex_old); } }\n")
			g.emitUnwindSlotUpdate(out, prefix, s.Name)
			break
		}
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
				out.WriteString(fmt.Sprintf("%s{ %s _dex_old = %s; %s = ", prefix, g.cType(varType), s.Name, s.Name))
				g.genOptionalStructOwned(out, s.Value, varType)
				out.WriteString("; dex_optbox_release(_dex_old); }\n")
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
			if _, _, isLit := structAssignments(s.Value); isLit {
				g.emitRetainStructLitFields(out, prefix+"    ", s.Name, varType, s.Value)
			} else if g.borrowsHeapValue(s.Value) {
				// Borrowed whole struct — the target becomes a second owner.
				//
				// Through emitRetainBorrowedStructFields, which recurses, because
				// the release loop below goes through emitReleaseVar and that
				// descends into a nested struct field. A flat loop over
				// IsHeapType here did not: a nested struct is not a heap type, so
				// `page = plain` retained plain's own strings and silently skipped
				// `plain.headers.raw`, leaving it one reference short. Both
				// locals then released it at scope exit — a use-after-free from
				// an ordinary reassignment, and the declaration path one line
				// away had been doing it correctly all along.
				g.emitRetainBorrowedStructFields(out, prefix+"    ", s.Name, varType)
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
		g.genCondition(out, s.Cond)
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
		g.genCondition(out, s.Cond)
		out.WriteString(") {\n")
		savedLoop := g.isInLoop
		savedDepth := g.loopDepth
		savedTryDepth := g.loopTryDepth
		g.isInLoop = true
		g.loopDepth = len(g.scopeStack)
		g.loopTryDepth = g.tryDepth
		g.pushScope()
		for _, stmt := range s.Body {
			g.genStmt(out, stmt, indent+1)
		}
		g.popScope(out, prefix+"    ")
		g.isInLoop = savedLoop
		g.loopDepth = savedDepth
		g.loopTryDepth = savedTryDepth
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
		savedTryDepth := g.loopTryDepth
		g.isInLoop = true
		g.loopDepth = len(g.scopeStack)
		g.loopTryDepth = g.tryDepth
		g.pushScope()
		for _, stmt := range s.Body {
			g.genStmt(out, stmt, indent+1)
		}
		g.popScope(out, prefix+"    ")
		g.isInLoop = savedLoop
		g.loopDepth = savedDepth
		g.loopTryDepth = savedTryDepth
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

		ownsIterable := ast.IsHeapType(arrType) && !g.borrowsHeapValue(s.Iterable)
		if ownsIterable {
			arrExpr = fmt.Sprintf("_foreach_arr_%d", idx)
			out.WriteString(fmt.Sprintf("%s{\n%s%s %s;\n", prefix, prefix, g.cType(arrType), arrExpr))
			g.pushScope()
			g.beginStmtHoist()
			var init strings.Builder
			init.WriteString(arrExpr + " = ")
			g.genExpr(&init, s.Iterable)
			init.WriteString(";\n")
			g.emitWithHoists(out, prefix, init.String())
			g.varTypes[arrExpr] = arrType
			g.registerScopeVar(out, prefix, arrExpr, arrType)
		}

		out.WriteString(fmt.Sprintf("%sfor (int %s = 0; %s < %s->len; %s++) {\n",
			prefix, idxVar, idxVar, arrExpr, idxVar))
		savedLoop := g.isInLoop
		savedDepth := g.loopDepth
		savedTryDepth := g.loopTryDepth
		g.isInLoop = true
		g.loopDepth = len(g.scopeStack)
		g.loopTryDepth = g.tryDepth
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
		g.loopTryDepth = savedTryDepth
		out.WriteString(fmt.Sprintf("%s}\n", prefix))
		if ownsIterable {
			g.popScope(out, prefix)
			out.WriteString(prefix + "}\n")
		}

	case *ast.BreakStmt:
		if g.isInLoop {
			g.emitCleanupInnerScopes(out, prefix, g.loopDepth)
		}
		// A break out of a try body leaves that frame behind as surely as a
		// return does.
		g.emitExcFramePops(out, prefix, g.tryDepth-g.loopTryDepth)
		out.WriteString(fmt.Sprintf("%sbreak;\n", prefix))

	case *ast.ContinueStmt:
		if g.isInLoop {
			g.emitCleanupInnerScopes(out, prefix, g.loopDepth)
		}
		g.emitExcFramePops(out, prefix, g.tryDepth-g.loopTryDepth)
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
		g.beginStmtHoist()
		var body strings.Builder
		if t, ok := g.varTypes[s.Name]; ok && isPrimitiveRef(t) {
			body.WriteString(fmt.Sprintf("(*%s) %s= ", s.Name, g.cBinOp(s.Op)))
		} else {
			body.WriteString(fmt.Sprintf("%s %s= ", s.Name, g.cBinOp(s.Op)))
		}
		g.genExpr(&body, s.Value)
		body.WriteString(";\n")
		g.emitWithHoists(out, prefix, body.String())

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

			// A string array's slot owns its reference, the same way a struct
			// field does. Overwriting one therefore has to release what was
			// there and take a reference to what replaces it — a borrowed
			// value is still owned by somebody else, and an owned temporary
			// already carries the only reference there is.
			//
			// Written without either, `names[0] = other` leaked the old
			// string and freed `other` when the array was released.
			if ast.IsHeapType(ast.ElementType(arrType)) {
				tmpVal := g.nextTemp()
				tmpSlot := g.nextTemp()
				out.WriteString(fmt.Sprintf("%s%s %s = ", prefix, g.cType(ast.ElementType(arrType)), tmpVal))
				g.genExpr(out, s.Value)
				out.WriteString(";\n")
				if !g.isNewAlloc(s.Value) {
					out.WriteString(fmt.Sprintf("%sdex_retain(%s);\n", prefix, tmpVal))
				}
				out.WriteString(fmt.Sprintf("%s%s* %s = &", prefix, g.cType(ast.ElementType(arrType)), tmpSlot))
				g.genExpr(out, s.Array)
				out.WriteString("->data[")
				g.genExpr(out, s.Index)
				out.WriteString("];\n")
				out.WriteString(fmt.Sprintf("%sdex_release(*%s);\n", prefix, tmpSlot))
				out.WriteString(fmt.Sprintf("%s*%s = %s;\n", prefix, tmpSlot, tmpVal))
			} else {
				out.WriteString(prefix)
				g.genExpr(out, s.Array)
				out.WriteString("->data[")
				g.genExpr(out, s.Index)
				out.WriteString("] = ")
				g.genExpr(out, s.Value)
				out.WriteString(";\n")
			}
		}

	case *ast.FieldAssignStmt:
		objType := g.typeOfExpr(s.Object)
		fieldType := g.typeOfExpr(s.Value)
		declaredType := g.typeOfExpr(&ast.FieldAccessExpr{Pos: s.Pos, Object: s.Object, Field: s.Field})
		if ast.IsStructOptionalType(declaredType) {
			accessOp := "."
			if ast.IsRefType(objType) {
				accessOp = "->"
			}
			tmpVal := g.nextTemp()
			out.WriteString(fmt.Sprintf("%s%s %s = ", prefix, g.cType(declaredType), tmpVal))
			g.genOptionalStructOwned(out, s.Value, declaredType)
			out.WriteString(";\n")
			out.WriteString(fmt.Sprintf("%sdex_optbox_release(", prefix))
			g.genExpr(out, s.Object)
			out.WriteString(fmt.Sprintf("%s%s);\n", accessOp, s.Field))
			out.WriteString(prefix)
			g.genExpr(out, s.Object)
			out.WriteString(fmt.Sprintf("%s%s = %s;\n", accessOp, s.Field, tmpVal))
		} else if ast.IsHeapType(fieldType) {
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
			// The field's old value has just been released, so its cleanup
			// entry — if it has one — has to follow the new value.
			if ident, ok := s.Object.(*ast.Ident); ok {
				g.emitUnwindFieldUpdate(out, prefix, ident.Name, s.Field)
			}
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
		isPayloadEnum := ast.IsPayloadEnumType(tagType)
		// The checker proves a payload-enum switch with no default covers every
		// variant, so the last arm can be an unconditional else. Without it C
		// cannot see that the chain is total and warns that a function whose
		// every arm returns does not return on all paths -- a warning on
		// perfectly ordinary code, and an error under -Werror.
		exhaustive := isPayloadEnum && !s.HasDefault && len(s.Cases) > 0
		for i, sc := range s.Cases {
			last := i == len(s.Cases)-1
			if exhaustive && last && i > 0 {
				out.WriteString(fmt.Sprintf("%s} else {\n", prefix))
				g.pushScope()
				g.genEnumPatternBindings(out, prefix+"    ", tagVar, tagType, sc.Values)
				for _, stmt := range sc.Body {
					g.genStmt(out, stmt, indent+1)
				}
				g.popScope(out, prefix+"    ")
				continue
			}
			if i == 0 {
				out.WriteString(fmt.Sprintf("%sif (", prefix))
			} else {
				out.WriteString(fmt.Sprintf("%s} else if (", prefix))
			}
			for j, val := range sc.Values {
				if j > 0 {
					out.WriteString(" || ")
				}
				switch {
				case isPayloadEnum:
					// The tag is a pointer to a tagged union, so the test is on
					// the discriminant rather than on the value.
					variant := ""
					switch v := val.(type) {
					case *ast.EnumPatternExpr:
						variant = v.Variant
					case *ast.EnumAccessExpr:
						variant = v.Variant
					}
					out.WriteString(fmt.Sprintf("%s->tag == %s", tagVar,
						g.enumTagName(ast.EnumName(tagType), variant)))
				case isString:
					out.WriteString(fmt.Sprintf("strcmp(%s->data, ", tagVar))
					g.genStringData(out, val)
					out.WriteString(") == 0")
				default:
					out.WriteString(fmt.Sprintf("%s == ", tagVar))
					g.genExpr(out, val)
				}
			}
			out.WriteString(") {\n")
			g.pushScope()
			// A pattern's bindings are read out of the union at the top of the
			// arm. They borrow: the enum being switched on owns them and
			// outlives the arm, so none is retained here and none is released
			// at the end of it. That is also why they are declared directly
			// rather than registered as scope variables.
			if isPayloadEnum {
				g.genEnumPatternBindings(out, prefix+"    ", tagVar, tagType, sc.Values)
			}
			for _, stmt := range sc.Body {
				g.genStmt(out, stmt, indent+1)
			}
			g.popScope(out, prefix+"    ")
		}
		if s.HasDefault {
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
		// A literal message is static storage and can be thrown directly.
		if call, ok := s.Value.(*ast.CallExpr); ok && call.Name == "Exception" && len(call.Args) == 1 {
			if strLit, isLit := call.Args[0].(*ast.StringLit); isLit {
				out.WriteString(fmt.Sprintf("%s_dex_throw(\"Exception\", %q);\n", prefix, strLit.Value))
				break
			}
			g.genThrowMessage(out, prefix, call.Args[0], false)
			break
		}
		// Throwing a struct held in a variable consumes it: the message is
		// staged and the struct's heap fields are released, here rather than at
		// a scope exit the longjmp will skip. So the cleanup entries for those
		// fields are given up first — left in place, the unwind would release
		// them a second time.
		if ident, ok := s.Value.(*ast.Ident); ok {
			g.emitUnwindEntriesCleared(out, prefix, ident.Name)
		}
		g.genThrowMessage(out, prefix, s.Value, true)

	case *ast.TryCatchStmt:
		id := g.tryCatchCounter
		g.tryCatchCounter++
		hasFinally := s.FinallyBody != nil

		// The cleanup height as it stands before the try. Anything registered
		// above this belongs to a frame the longjmp skipped, or to the try's own
		// body, and nothing released it on the way out.
		cleanupMark := g.nextCleanupMark()
		out.WriteString(fmt.Sprintf("%ssize_t %s = _dex_cleanup_top;\n", prefix, cleanupMark))
		// And the defer stack's height, so the catch can run the deferred calls
		// of every frame the longjmp skipped. Only when the program defers at
		// all: the stack lives in the closure runtime, which a program with no
		// deferred call anywhere does not carry.
		deferMark := ""
		if g.usesDefer {
			deferMark = g.nextCleanupMark()
			out.WriteString(fmt.Sprintf("%ssize_t %s = _dex_defer_top;\n", prefix, deferMark))
		}

		// Push exception frame
		out.WriteString(fmt.Sprintf("%s_dex_exc_push_frame();\n", prefix))
		out.WriteString(fmt.Sprintf("%svolatile int _dex_exc_%d_caught = 0;\n", prefix, id))
		out.WriteString(fmt.Sprintf("%svolatile int _dex_exc_%d_handled = 0;\n", prefix, id))
		out.WriteString(fmt.Sprintf("%sif (DEX_SETJMP(_dex_exc_stack[_dex_exc_top].env) == 0) {\n", prefix))
		out.WriteString(fmt.Sprintf("%s    _dex_exc_stack[_dex_exc_top].active = 0;\n", prefix))

		// Try body
		g.pushScope()
		g.tryDepth++
		for _, stmt := range s.Body {
			g.genStmt(out, stmt, indent+1)
		}
		g.tryDepth--
		g.popScope(out, prefix+"    ")
		out.WriteString(fmt.Sprintf("%s} else {\n", prefix))
		out.WriteString(fmt.Sprintf("%s    _dex_exc_%d_caught = 1;\n", prefix, id))
		// The skipped frames' deferred calls run first — they are user code,
		// and may read what they captured — and then what those frames held is
		// released. Both happen before any handler runs: the catch body
		// allocates, and the entries above the mark are already dead.
		if deferMark != "" {
			out.WriteString(fmt.Sprintf("%s    _dex_defer_run_to(%s);\n", prefix, deferMark))
		}
		out.WriteString(fmt.Sprintf("%s    _dex_cleanup_unwind_to(%s);\n", prefix, cleanupMark))
		out.WriteString(fmt.Sprintf("%s}\n", prefix))

		// Pop exception frame
		out.WriteString(fmt.Sprintf("%s_dex_exc_top--;\n", prefix))

		// Dispatch to the first clause whose type matches what was thrown. The
		// frame was popped, so the exception it carries is one past the top.
		if len(s.Catches) > 0 {
			thrownType := fmt.Sprintf("_dex_exc_stack[_dex_exc_top + 1].type")
			out.WriteString(fmt.Sprintf("%sif (_dex_exc_%d_caught) {\n", prefix, id))
			for i := range s.Catches {
				clause := &s.Catches[i]
				keyword := "if"
				if i > 0 {
					keyword = "} else if"
				}
				out.WriteString(fmt.Sprintf("%s    %s (_dex_exc_matches(%s, %q)) {\n",
					prefix, keyword, thrownType, clause.TypeName))
				out.WriteString(fmt.Sprintf("%s        _dex_exc_%d_handled = 1;\n", prefix, id))

				// The caught value is rebuilt here: its own fields from the
				// staged payload, its message from the staged text.
				cType := g.cType(clause.Type)
				out.WriteString(fmt.Sprintf("%s        %s %s; _dex_exc_restore(%q, &%s, sizeof(%s));\n",
					prefix, cType, clause.VarName, clause.TypeName, clause.VarName, clause.VarName))
				out.WriteString(fmt.Sprintf("%s        DexString* %s_msg = dex_string_from_lit(_dex_exc_stack[_dex_exc_top + 1].message);\n",
					prefix, clause.VarName))
				out.WriteString(fmt.Sprintf("%s        %s.message = %s_msg;\n", prefix, clause.VarName, clause.VarName))

				g.pushScope()
				g.varTypes[clause.VarName] = clause.Type
				g.structVars[clause.VarName] = clause.Type
				g.registerScopeVar(out, prefix+"        ", clause.VarName+"_msg", ast.TypeString)
				for _, stmt := range clause.Body {
					g.genStmt(out, stmt, indent+2)
				}
				g.popScope(out, prefix+"        ")
			}
			out.WriteString(fmt.Sprintf("%s    }\n", prefix))
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

		// An exception no clause claimed carries on outward, after the finally
		// has run. A try with only a finally always lands here.
		out.WriteString(fmt.Sprintf("%sif (_dex_exc_%d_caught && !_dex_exc_%d_handled) {\n", prefix, id, id))
		out.WriteString(fmt.Sprintf("%s    _dex_rethrow(_dex_exc_stack[_dex_exc_top + 1].type, _dex_exc_stack[_dex_exc_top + 1].message);\n", prefix))
		out.WriteString(fmt.Sprintf("%s}\n", prefix))

	case *ast.DeferStmt:
		// Registered where the defer runs, not accumulated at compile time.
		// That is what makes it conditional — a defer in a branch not taken
		// registers nothing — what makes one in a loop run per iteration, and
		// what lets an unwind run it: the catch runs everything registered
		// above its own height.
		//
		// The call becomes a closure, so what it needs is captured and owned by
		// the closure rather than read out of a frame that may be gone by then.
		// Capture happens here, which is also when the arguments are evaluated.
		lam := &ast.LambdaExpr{
			Pos:        s.Pos,
			ReturnType: ast.TypeVoid,
			Body:       []ast.Stmt{&ast.ExprStmt{Pos: s.Pos, Expr: s.Expr}},
		}
		out.WriteString(prefix + "_dex_defer_push(")
		g.genLambdaClosure(out, lam)
		out.WriteString(");\n")

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
						g.registerScopeVar(out, prefix, name, f.Type)
					}
					break
				}
			}
		}
	}
}

// genThrowMessage emits a throw whose message is built at runtime.
//
// The message text is staged into the exception runtime first, then the string
// that produced it is released, and only then does the throw longjmp. The order
// is forced: longjmp skips every release the unwound frames would have run, so
// a message built for the throw — `throw Exception("no " + name)` — had nothing
// left to own it and leaked one string per throw.
//
// A message that was only borrowed is not released, on the usual terms.
// genThrowMessage emits a throw whose message is not a literal. The order is
// the whole of it: the message text is staged into the runtime's buffer, the
// struct's own fields are staged as bytes, whatever the expression allocated is
// released, and only then does the longjmp happen — after a longjmp there is no
// way to release anything.
func (g *Generator) genThrowMessage(out *strings.Builder, prefix string, msg ast.Expr, fromStruct bool) {
	field := ""
	if fromStruct {
		field = ".message"
	}
	tmp := g.nextTemp()
	typ := ast.TypeString
	typeName := "Exception"
	if fromStruct {
		typ = g.typeOfExpr(msg)
		if ast.IsStructType(typ) {
			typeName = ast.StructName(typ)
		}
	}
	out.WriteString(fmt.Sprintf("%s{ %s %s = ", prefix, g.cType(typ), tmp))
	g.genExpr(out, msg)
	out.WriteString(fmt.Sprintf("; _dex_exc_stage(%s%s->data);", tmp, field))
	if fromStruct {
		// The remaining fields travel as bytes. The assert is here rather than
		// in the runtime because a payload too large to carry should stop the
		// build, not silently arrive zeroed.
		out.WriteString(fmt.Sprintf(" _Static_assert(sizeof(%s) <= DEX_EXCEPTION_PAYLOAD_MAX, \"exception payload too large to cross a throw\");", g.cType(typ)))
		out.WriteString(fmt.Sprintf(" _dex_exc_stage_payload(&%s, sizeof(%s));", tmp, tmp))
		if ast.NeedsRelease(typ) {
			var rel strings.Builder
			g.emitReleaseVar(&rel, "", tmp, typ)
			out.WriteString(" " + strings.ReplaceAll(strings.TrimRight(rel.String(), "\n"), "\n", " "))
		}
	} else if g.isNewAlloc(msg) {
		out.WriteString(fmt.Sprintf(" dex_release(%s);", tmp))
	}
	out.WriteString(fmt.Sprintf(" _dex_throw_staged(%q); }\n", typeName))
}
