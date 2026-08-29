package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

// isPrimitiveRef returns true when the type is a ref wrapping a primitive type.
func isPrimitiveRef(t ast.Type) bool {
	if !ast.IsRefType(t) {
		return false
	}
	inner := ast.RefInnerType(t)
	return inner == ast.TypeInt || inner == ast.TypeLong || inner == ast.TypeDouble || inner == ast.TypeBool || inner == ast.TypeChar
}

func (g *Generator) genExpr(out *strings.Builder, expr ast.Expr) {
	switch e := expr.(type) {
	case *ast.IntLit:
		out.WriteString(fmt.Sprintf("%d", e.Value))

	case *ast.CharLit:
		switch e.Value {
		case '\'':
			out.WriteString("'\\''")
		case '\\':
			out.WriteString("'\\\\'")
		case '\n':
			out.WriteString("'\\n'")
		case '\t':
			out.WriteString("'\\t'")
		default:
			out.WriteString(fmt.Sprintf("'%c'", e.Value))
		}

	case *ast.FloatLit:
		out.WriteString(fmt.Sprintf("%g", e.Value))

	case *ast.BoolLit:
		if e.Value {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}

	case *ast.StringLit:
		out.WriteString(fmt.Sprintf("dex_string_from_lit(%q)", e.Value))

	case *ast.NullLit:
		out.WriteString("NULL")

	case *ast.MutexLit:
		// Static initialiser, so it works equally as a local, a global, or a
		// designated initialiser inside a struct literal.
		out.WriteString("PTHREAD_MUTEX_INITIALIZER")

	case *ast.Ident:
		if narrowed, ok := g.narrowedVars[e.Name]; ok {
			out.WriteString(narrowed)
		} else if t, ok := g.varTypes[e.Name]; ok && isPrimitiveRef(t) {
			out.WriteString(fmt.Sprintf("(*%s)", e.Name))
		} else if _, isVar := g.varTypes[e.Name]; !isVar {
			// A top-level function named where a value is expected becomes a
			// closure with no environment.
			if fn, isFn := g.funcs[e.Name]; isFn {
				var params []ast.Type
				for _, p := range fn.Params {
					params = append(params, p.Type)
				}
				g.genFuncValue(out, e.Name, ast.FuncTypeOf(params, fn.ReturnType))
			} else {
				out.WriteString(e.Name)
			}
		} else {
			out.WriteString(e.Name)
		}

	case *ast.BinaryExpr:
		// Check if this is a null comparison
		if g.isNullComparison(e) {
			g.genNullComparison(out, e, true)
			return
		}
		// Check if this is a string operation
		if g.isStringExpr(e.Left) || g.isStringExpr(e.Right) {
			switch e.Op {
			case ast.BinAdd:
				g.genStringConcat(out, e)
				return
			case ast.BinEq, ast.BinStrictEq:
				out.WriteString("(strcmp(")
				g.genStringData(out, e.Left)
				out.WriteString(", ")
				g.genStringData(out, e.Right)
				out.WriteString(") == 0)")
				return
			case ast.BinNeq, ast.BinStrictNeq:
				out.WriteString("(strcmp(")
				g.genStringData(out, e.Left)
				out.WriteString(", ")
				g.genStringData(out, e.Right)
				out.WriteString(") != 0)")
				return
			}
		}

		// Cross-numeric operations: cast narrower operand to wider type
		if e.HasMixedTypes {
			widerType := g.widerNumericType(e.LeftType, e.RightType)
			castType := g.cType(widerType)
			out.WriteString("(")
			if e.LeftType != widerType {
				out.WriteString(fmt.Sprintf("(%s)", castType))
			}
			g.genExpr(out, e.Left)
			out.WriteString(fmt.Sprintf(" %s ", g.cBinOp(e.Op)))
			if e.Op == ast.BinDiv || e.Op == ast.BinMod {
				out.WriteString(g.nonzeroCheckFunc(widerType) + "(")
			}
			if e.RightType != widerType {
				out.WriteString(fmt.Sprintf("(%s)", castType))
			}
			g.genExpr(out, e.Right)
			if e.Op == ast.BinDiv || e.Op == ast.BinMod {
				out.WriteString(")")
			}
			out.WriteString(")")
			return
		}

		out.WriteString("(")
		g.genExpr(out, e.Left)
		out.WriteString(fmt.Sprintf(" %s ", g.cBinOp(e.Op)))
		if e.Op == ast.BinDiv || e.Op == ast.BinMod {
			rightType := g.typeOfExpr(e.Right)
			out.WriteString(g.nonzeroCheckFunc(rightType) + "(")
			g.genExpr(out, e.Right)
			out.WriteString(")")
		} else {
			g.genExpr(out, e.Right)
		}
		out.WriteString(")")

	case *ast.UnaryExpr:
		out.WriteString("(")
		switch e.Op {
		case ast.UnaryNeg:
			out.WriteString("-")
		case ast.UnaryNot:
			out.WriteString("!")
		}
		g.genExpr(out, e.Operand)
		out.WriteString(")")

	case *ast.IndexExpr:
		// Check if this is a map index
		arrType := g.typeOfExpr(e.Array)
		if arrType == ast.TypeJsonValue {
			// int index reads an array position, string index reads a key.
			g.genJsonValueIndex(out, e)
			break
		}
		if ast.IsMapType(arrType) {
			suffix := g.mapSuffix(arrType)
			out.WriteString(fmt.Sprintf("dex_map_%s_get(", suffix))
			g.genExpr(out, e.Array)
			out.WriteString(", ")
			g.genBorrowed(out, e.Index)
			out.WriteString(")")
			break
		}
		// Check if this is a struct array index
		if ast.IsStructArrayType(arrType) {
			elemType := ast.ElementType(arrType)
			elemCType := g.cType(elemType)
			out.WriteString(fmt.Sprintf("(dex_bounds_check("))
			g.genExpr(out, e.Index)
			out.WriteString(", ")
			g.genExpr(out, e.Array)
			out.WriteString(fmt.Sprintf("->len), *(%s*)dex_array_struct_get(", elemCType))
			g.genExpr(out, e.Array)
			out.WriteString(", ")
			g.genExpr(out, e.Index)
			out.WriteString("))")
		} else {
			out.WriteString("(dex_bounds_check(")
			g.genExpr(out, e.Index)
			out.WriteString(", ")
			g.genExpr(out, e.Array)
			out.WriteString("->len), ")
			g.genExpr(out, e.Array)
			out.WriteString("->data[")
			g.genExpr(out, e.Index)
			out.WriteString("])")
		}

	case *ast.SliceExpr:
		arrType := g.typeOfExpr(e.Array)
		var fn string
		switch arrType {
		case ast.TypeArrayInt:
			fn = "dex_array_int_slice"
		case ast.TypeArrayBool:
			fn = "dex_array_bool_slice"
		case ast.TypeArrayString:
			fn = "dex_array_string_slice"
		case ast.TypeArrayLong:
			fn = "dex_array_long_slice"
		case ast.TypeArrayDouble:
			fn = "dex_array_double_slice"
		case ast.TypeArrayChar:
			fn = "dex_array_char_slice"
		default:
			if ast.IsStructArrayType(arrType) {
				fn = "dex_array_struct_slice"
			}
		}
		if fn == "dex_array_struct_slice" {
			out.WriteString(fmt.Sprintf("%s(", fn))
			g.genExpr(out, e.Array)
			out.WriteString(", ")
			if e.Start != nil {
				g.genExpr(out, e.Start)
			} else {
				out.WriteString("0")
			}
			out.WriteString(", ")
			if e.End != nil {
				g.genExpr(out, e.End)
			} else {
				g.genExpr(out, e.Array)
				out.WriteString("->len")
			}
			out.WriteString(")")
		} else {
			out.WriteString(fmt.Sprintf("%s(", fn))
			g.genExpr(out, e.Array)
			out.WriteString(", ")
			if e.Start != nil {
				g.genExpr(out, e.Start)
			} else {
				out.WriteString("0")
			}
			out.WriteString(", ")
			if e.End != nil {
				g.genExpr(out, e.End)
			} else {
				g.genExpr(out, e.Array)
				out.WriteString("->len")
			}
			out.WriteString(")")
		}

	case *ast.ArrayLitExpr:
		if e.AsJsonValue {
			g.genJsonValue(out, e)
			break
		}
		g.genArrayLitExpr(out, e)

	case *ast.ObjectLitExpr:
		g.genJsonValue(out, e)

	case *ast.StructLitExpr:
		cName := "Dex_" + e.Name
		out.WriteString(fmt.Sprintf("(%s){ ", cName))
		structType, _ := ast.LookupStructType(e.Name)
		structDef := ast.GetStructDef(structType)
		// Track which fields are explicitly provided
		provided := make(map[string]bool, len(e.FieldNames))
		for _, fn := range e.FieldNames {
			provided[fn] = true
		}
		first := true
		// Emit explicitly provided fields
		for i, fn := range e.FieldNames {
			if !first {
				out.WriteString(", ")
			}
			first = false
			out.WriteString(fmt.Sprintf(".%s = ", fn))
			// Insert & for ref-typed fields (only if arg is not already a ref)
			var fieldType ast.Type
			if structDef != nil {
				for _, f := range structDef.Fields {
					if f.Name != fn {
						continue
					}
					fieldType = f.Type
					if ast.IsRefType(f.Type) {
						argType := g.typeOfExpr(e.FieldValues[i])
						if !ast.IsRefType(argType) {
							out.WriteString("&")
						}
					}
					break
				}
			}
			_ = fieldType
			g.genExpr(out, e.FieldValues[i])
		}
		// Emit zero values for missing fields
		if structDef != nil {
			for _, f := range structDef.Fields {
				if provided[f.Name] {
					continue
				}
				if !first {
					out.WriteString(", ")
				}
				first = false
				out.WriteString(fmt.Sprintf(".%s = ", f.Name))
				g.genZeroValue(out, f.Type)
			}
		}
		out.WriteString(" }")

	case *ast.FieldAccessExpr:
		if e.IsMethodValue {
			g.genMethodValueExpr(out, e)
			break
		}
		g.genExpr(out, e.Object)
		objType := g.typeOfExpr(e.Object)
		// Optional structs are represented as Dex_Foo*, so they dereference too.
		if ast.IsRefType(objType) || (ast.IsOptionalType(objType) && ast.IsStructType(ast.OptionalInnerType(objType))) {
			out.WriteString(fmt.Sprintf("->%s", e.Field))
		} else {
			out.WriteString(fmt.Sprintf(".%s", e.Field))
		}

	case *ast.CallExpr:
		g.genCallExpr(out, e)

	case *ast.SpawnExpr:
		g.genSpawnExpr(out, e)

	case *ast.ChannelExpr:
		ctyp := g.cType(e.ElemType)
		out.WriteString(fmt.Sprintf("dex_chan_new(sizeof(%s), 64)", ctyp))

	case *ast.EnumAccessExpr:
		out.WriteString(fmt.Sprintf("Dex_%s_%s", e.EnumName, e.Variant))

	case *ast.MapLitExpr:
		if e.AsJsonValue {
			g.genJsonValue(out, e)
			break
		}
		// An empty map in expression position — a struct literal field, say —
		// is just a fresh map of the type the context supplied.
		out.WriteString(fmt.Sprintf("dex_map_%s_new()", g.mapSuffix(e.MapType)))

	case *ast.ReceiveExpr:
		// receive in expression context — should typically be handled by LetStmt
		// Generate a temp variable
		srcType := g.typeOfExpr(e.Source)
		var elemType ast.Type
		if ast.IsChanType(srcType) {
			elemType = ast.ChanElemType(srcType)
		} else if ast.IsTaskType(srcType) {
			elemType = ast.TaskReturnType(srcType)
		} else {
			elemType = ast.TypeInt
		}
		ctyp := g.cType(elemType)
		// This is for nested expressions. For let statements, genStmt handles it specially.
		out.WriteString(fmt.Sprintf("({ %s _recv_tmp; dex_chan_recv(", ctyp))
		g.genExpr(out, e.Source)
		out.WriteString(", &_recv_tmp); _recv_tmp; })")

	case *ast.StringInterpExpr:
		g.genStringInterpExpr(out, e)

	case *ast.MatchExpr:
		g.genMatchExpr(out, e)

	case *ast.LambdaExpr:
		g.genLambdaExpr(out, e)
	}
}

// resolveFieldChainC converts a dotted field chain like "self.mu" to the correct
// C accessor chain, using "->" for ref/pointer types and "." for value types.
func (g *Generator) resolveFieldChainC(chain string) string {
	if !strings.Contains(chain, ".") {
		return chain
	}
	parts := strings.SplitN(chain, ".", 2)
	baseType, ok := g.varTypes[parts[0]]
	if !ok {
		return chain
	}
	if ast.IsRefType(baseType) {
		return parts[0] + "->" + parts[1]
	}
	return chain
}

// resolveFieldChainType resolves the type of a dotted field chain like "self.database".
func (g *Generator) resolveFieldChainType(chain string) ast.Type {
	if !strings.Contains(chain, ".") {
		if t, ok := g.varTypes[chain]; ok {
			return t
		}
		return ast.TypeVoid
	}
	parts := strings.Split(chain, ".")
	current, ok := g.varTypes[parts[0]]
	if !ok {
		return ast.TypeVoid
	}
	// Every segment after the first names a field of the type before it, so a
	// chain of any depth resolves the way one of length two always did.
	for _, field := range parts[1:] {
		if ast.IsRefType(current) {
			current = ast.RefInnerType(current)
		}
		if !ast.IsStructType(current) {
			return ast.TypeVoid
		}
		def := ast.GetStructDef(current)
		if def == nil {
			return ast.TypeVoid
		}
		next := ast.TypeVoid
		for _, f := range def.Fields {
			if f.Name == field {
				next = f.Type
				break
			}
		}
		if next == ast.TypeVoid {
			return ast.TypeVoid
		}
		current = next
	}
	return current
}

// fieldChainC renders a dotted receiver — "cmd.action" — as the C that reads it,
// so a method call on a field emits the same access an expression would.
func (g *Generator) fieldChainC(chain string) string {
	parts := strings.Split(chain, ".")
	var expr ast.Expr = &ast.Ident{Name: parts[0]}
	for _, field := range parts[1:] {
		expr = &ast.FieldAccessExpr{Object: expr, Field: field}
	}
	var out strings.Builder
	g.genExpr(&out, expr)
	return out.String()
}

// genStringData generates the raw C string (->data) for use in strcmp etc.
// The consumer only reads the bytes, so an operand that allocates is hoisted to a
// statement-scoped temporary and released once the statement is done; reading
// ->data off an unowned temporary would otherwise leak it.
func (g *Generator) genStringData(out *strings.Builder, expr ast.Expr) {
	if lit, ok := expr.(*ast.StringLit); ok {
		// String literals: just emit the C string literal directly for strcmp
		out.WriteString(fmt.Sprintf("%q", lit.Value))
		return
	}
	g.genBorrowed(out, expr)
	out.WriteString("->data")
}

// genArrayLitExpr builds an array literal in expression position — a struct
// literal field, a call argument, a return value. The let-statement path fills
// the backing storage directly; here the array has to be a value, so it is
// built inside a statement expression.
func (g *Generator) genArrayLitExpr(out *strings.Builder, e *ast.ArrayLitExpr) {
	arrType := ast.ArrayTypeOf(e.ElemType)
	if ast.IsStructType(e.ElemType) {
		arrType = ast.StructArrayTypeOf(e.ElemType)
		elemCType := g.cType(e.ElemType)
		tmp := g.nextTemp()
		out.WriteString(fmt.Sprintf("({ DexArrayStruct* %s = dex_array_struct_new(sizeof(%s), %s); ",
			tmp, elemCType, g.structArrayCleanupFunc(e.ElemType)))
		for _, elem := range e.Elems {
			out.WriteString(fmt.Sprintf("{ %s _el = ", elemCType))
			g.genExpr(out, elem)
			out.WriteString(fmt.Sprintf("; dex_array_struct_push(%s, &_el); } ", tmp))
		}
		out.WriteString(fmt.Sprintf("%s; })", tmp))
		return
	}

	tmp := g.nextTemp()
	out.WriteString(fmt.Sprintf("({ %s %s = %s(); ", g.cType(arrType), tmp, g.arrayNewFunc(arrType)))
	pushFn := g.arrayPushFunc(arrType)
	for _, elem := range e.Elems {
		out.WriteString(fmt.Sprintf("%s(%s, ", pushFn, tmp))
		g.genExpr(out, elem)
		out.WriteString("); ")
	}
	out.WriteString(fmt.Sprintf("%s; })", tmp))
}
