package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/stdlib"
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
	parts := strings.SplitN(chain, ".", 2)
	baseType, ok := g.varTypes[parts[0]]
	if !ok {
		return ast.TypeVoid
	}
	// Unwrap ref type
	if ast.IsRefType(baseType) {
		baseType = ast.RefInnerType(baseType)
	}
	if !ast.IsStructType(baseType) {
		return ast.TypeVoid
	}
	def := ast.GetStructDef(baseType)
	if def == nil {
		return ast.TypeVoid
	}
	for _, f := range def.Fields {
		if f.Name == parts[1] {
			return f.Type
		}
	}
	return ast.TypeVoid
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

func (g *Generator) genCallExpr(out *strings.Builder, e *ast.CallExpr) {
	// json.Value method calls: v.asInt(), v.has("k"), etc. A named receiver is
	// normalised into a receiver expression so both spellings take one path.
	if e.Recv != nil && g.typeOfExpr(e.Recv) == ast.TypeJsonValue {
		if g.genJsonValueMethodExpr(out, e.Recv, e.Name, e.Args, e.ResolvedType) {
			return
		}
	}
	if e.Recv == nil && e.Module != "" {
		// A plain variable, or a dotted field path such as msg.payload.
		recvType := g.varTypes[e.Module]
		if recvType != ast.TypeJsonValue && strings.Contains(e.Module, ".") {
			recvType = g.resolveFieldChainType(e.Module)
		}
		if recvType == ast.TypeJsonValue {
			recv := &ast.Ident{Pos: e.Pos, Name: e.Module}
			if g.genJsonValueMethodExpr(out, recv, e.Name, e.Args, e.ResolvedType) {
				return
			}
		}
	}

	// StringBuilder constructor
	if e.Name == "StringBuilder" && e.Module == "" && !e.IsConstructor {
		out.WriteString("dex_sb_new()")
		return
	}

	// Constructor call: emit struct literal from positional args
	if e.IsConstructor {
		structDef := ast.GetStructDef(e.StructType)
		out.WriteString(fmt.Sprintf("(Dex_%s){ ", structDef.Name))
		for i, cp := range structDef.ConstructorParams {
			if i > 0 {
				out.WriteString(", ")
			}
			out.WriteString(fmt.Sprintf(".%s = ", cp.Name))
			if ast.IsRefType(cp.Type) {
				argType := g.typeOfExpr(e.Args[i])
				if !ast.IsRefType(argType) {
					out.WriteString("&")
					g.genExpr(out, e.Args[i])
				} else if ident, ok := e.Args[i].(*ast.Ident); ok && isPrimitiveRef(argType) {
					out.WriteString(ident.Name)
				} else {
					g.genExpr(out, e.Args[i])
				}
			} else {
				g.genExpr(out, e.Args[i])
			}
		}
		out.WriteString(" }")
		return
	}

	// Method call: emit flattened function with instance as first arg
	if e.IsMethodCall {
		structDef := ast.GetStructDef(e.StructType)
		flatName := structDef.Name + "_" + e.Name
		// Check if struct belongs to a user module (needs module prefix)
		if modName, ok := g.structModules[structDef.Name]; ok {
			flatName = modName + "_" + flatName
		}
		out.WriteString(flatName)
		out.WriteString("(")
		// If instance is a ref type, dereference for value self param
		instanceType := g.resolveFieldChainType(e.Module)
		if ast.IsRefType(instanceType) {
			out.WriteString("*")
		}
		out.WriteString(e.Module) // the instance variable name
		for _, arg := range e.Args {
			out.WriteString(", ")
			// A method borrows its arguments just as a free function does, so an
			// argument built on the spot is released once the statement ends.
			g.genBorrowed(out, arg)
		}
		out.WriteString(")")
		return
	}

	// User module call: emit prefixed function name.
	// A local or global of the same name shadows the module — `chargers.push(x)`
	// on a variable called chargers is a method call, not a call into a module
	// that happens to share the name.
	_, shadowedByVar := g.varTypes[e.Module]
	if e.Module != "" && g.userModules[e.Module] && !shadowedByVar {
		out.WriteString(e.Module + "_" + e.Name)
		out.WriteString("(")
		modFn, hasModFn := g.funcs[e.Module+"_"+e.Name]
		for i, arg := range e.Args {
			if i > 0 {
				out.WriteString(", ")
			}
			if hasModFn && i < len(modFn.Params) && ast.IsRefType(modFn.Params[i].Type) {
				argType := g.typeOfExpr(arg)
				if !ast.IsRefType(argType) {
					out.WriteString("&")
					g.genExpr(out, arg)
				} else if ident, ok := arg.(*ast.Ident); ok && isPrimitiveRef(argType) {
					out.WriteString(ident.Name)
				} else {
					g.genExpr(out, arg)
				}
			} else {
				// The callee borrows its argument, exactly as for a call to a
				// function in the same file, so an argument that allocates is
				// released once the statement finishes rather than leaking.
				g.genBorrowed(out, arg)
			}
		}
		out.WriteString(")")
		return
	}

	// Special case: fmt.print / fmt.println — polymorphic print for any type
	if e.Module == "fmt" && (e.Name == "print" || e.Name == "println") {
		newline := e.Name == "println"
		argType := g.typeOfExpr(e.Args[0])
		// Auto-unwrap ref types for printing (e.g., &User → User)
		if ast.IsRefType(argType) {
			argType = ast.RefInnerType(argType)
		}
		if ast.IsArrayType(argType) {
			g.genPrintArray(out, e.Args[0], argType, newline)
			return
		}
		if ast.IsStructType(argType) {
			g.genPrintStruct(out, e.Args[0], argType, newline)
			return
		}
		nl := ""
		if newline {
			nl = "\\n"
		}
		var fmtStr string
		switch argType {
		case ast.TypeChar:
			fmtStr = "%c"
		case ast.TypeInt:
			fmtStr = "%d"
		case ast.TypeLong:
			fmtStr = "%ld"
		case ast.TypeDouble:
			fmtStr = "%f"
		case ast.TypeString:
			out.WriteString(fmt.Sprintf("printf(\"%%s%s\", ", nl))
			g.genBorrowed(out, e.Args[0])
			out.WriteString("->data)")
			return
		case ast.TypeBool:
			// Print bools as "true"/"false"
			out.WriteString(fmt.Sprintf("printf(\"%%s%s\", ", nl))
			out.WriteString("(")
			g.genExpr(out, e.Args[0])
			out.WriteString(") ? \"true\" : \"false\")")
			return
		default:
			fmtStr = "%d"
		}
		out.WriteString(fmt.Sprintf("printf(\"%s%s\", ", fmtStr, nl))
		g.genExpr(out, e.Args[0])
		out.WriteString(")")
		return
	}
	// json.encode(value) — special codegen (returns const char*, needs wrapping)
	if e.Module == "json" && e.Name == "encode" {
		g.genJsonEncodeToString(out, e.Args[0], g.typeOfExpr(e.Args[0]))
		return
	}

	// json.decode(jsonStr) — decode JSON string into a struct
	if e.Module == "json" && e.Name == "decode" {
		// json.Value target: parse the text into a document tree.
		decodeTarget := e.ResolvedType
		if ast.IsOptionalType(decodeTarget) {
			decodeTarget = ast.OptionalInnerType(decodeTarget)
		}
		if decodeTarget == ast.TypeJsonValue {
			if g.typeOfExpr(e.Args[0]) == ast.TypeJsonValue {
				// Already parsed — hand back a retained reference.
				out.WriteString("({ DexJsonValue* _jv = ")
				g.genExpr(out, e.Args[0])
				out.WriteString("; dex_retain(_jv); _jv; })")
				return
			}
			// The parser only reads the text, so a source string that was just
			// built — json.decode(json.encode(v)) — is released after the
			// statement rather than being left behind.
			out.WriteString("dex_jv_parse_str(")
			g.genBorrowed(out, e.Args[0])
			out.WriteString(")")
			return
		}
		// A json.Value source decoding into a struct is re-serialized so the one
		// struct decoder handles it, rather than a parallel tree-walking path.
		if g.typeOfExpr(e.Args[0]) == ast.TypeJsonValue {
			src := &ast.CallExpr{Module: "json", Name: "encode", Args: []ast.Expr{e.Args[0]}, Pos: e.Pos}
			forwarded := &ast.CallExpr{Module: "json", Name: "decode", Args: []ast.Expr{src}, ResolvedType: e.ResolvedType, Pos: e.Pos}
			g.jsonDecodeFromString(out, forwarded)
			return
		}
		g.jsonDecodeFromString(out, e)
		return
	}

	// json.setArray(obj, key, array) — special codegen
	if e.Module == "json" && e.Name == "setArray" {
		argIdent, ok := e.Args[2].(*ast.Ident)
		if ok {
			arrType := g.arrVars[argIdent.Name]
			if ast.IsStructArrayType(arrType) {
				// Struct array: use dex_json_set_struct_arr
				elemType := ast.ElementType(arrType)
				elemCType := g.cType(elemType)
				def := ast.GetStructDef(elemType)
				out.WriteString("dex_string_from_cstr(dex_json_set_struct_arr(")
				g.genStringData(out, e.Args[0])
				out.WriteString(", ")
				g.genStringData(out, e.Args[1])
				out.WriteString(", ")
				out.WriteString(argIdent.Name)
				out.WriteString(fmt.Sprintf(", sizeof(%s), %d, ", elemCType, len(def.Fields)))
				g.genStructFieldDescs(out, elemType, 0)
				out.WriteString("))")
			} else {
				var fn string
				switch arrType {
				case ast.TypeArrayInt:
					fn = "dex_json_set_arr_int"
				case ast.TypeArrayBool:
					fn = "dex_json_set_arr_bool"
				case ast.TypeArrayString:
					fn = "dex_json_set_arr_str"
				case ast.TypeArrayLong:
					fn = "dex_json_set_arr_long"
				case ast.TypeArrayDouble:
					fn = "dex_json_set_arr_double"
				case ast.TypeArrayChar:
					fn = "dex_json_set_arr_char"
				}
				// Bridge: extract ->data for string args, wrap result in dex_string_from_cstr
				out.WriteString(fmt.Sprintf("dex_string_from_cstr(%s(", fn))
				g.genStringData(out, e.Args[0])
				out.WriteString(", ")
				g.genStringData(out, e.Args[1])
				out.WriteString(", ")
				out.WriteString(argIdent.Name)
				out.WriteString("))")
			}
		}
		return
	}

	// json.arrayPush(arr, value) — polymorphic: dispatch by value type
	if e.Module == "json" && e.Name == "arrayPush" {
		valType := g.typeOfExpr(e.Args[1])
		var fn string
		switch valType {
		case ast.TypeInt:
			fn = "dex_json_array_push_int"
		case ast.TypeBool:
			fn = "dex_json_array_push_bool"
		case ast.TypeLong:
			fn = "dex_json_array_push_long"
		case ast.TypeDouble:
			fn = "dex_json_array_push_double"
		default:
			fn = "dex_json_array_push_str"
		}
		out.WriteString(fmt.Sprintf("dex_string_from_cstr(%s(", fn))
		g.genStringData(out, e.Args[0])
		out.WriteString(", ")
		if valType == ast.TypeString {
			g.genStringData(out, e.Args[1])
		} else {
			g.genExpr(out, e.Args[1])
		}
		out.WriteString("))")
		return
	}

	// json.arrayNew() — returns const char*, wrap
	if e.Module == "json" && e.Name == "arrayNew" {
		out.WriteString("dex_string_from_cstr(dex_json_array_new())")
		return
	}

	// json.set(obj, key, value) — polymorphic: dispatch by value type
	if e.Module == "json" && e.Name == "set" {
		valType := g.typeOfExpr(e.Args[2])
		// Handle struct array: serialize to JSON array of objects
		if ast.IsStructArrayType(valType) {
			elemType := ast.ElementType(valType)
			elemCType := g.cType(elemType)
			def := ast.GetStructDef(elemType)
			// Generate inline serialization using dex_json_set_arr_struct helper pattern
			out.WriteString("dex_string_from_cstr(dex_json_set_struct_arr(")
			g.genStringData(out, e.Args[0])
			out.WriteString(", ")
			g.genStringData(out, e.Args[1])
			out.WriteString(", ")
			g.genExpr(out, e.Args[2])
			out.WriteString(fmt.Sprintf(", sizeof(%s), %d, ", elemCType, len(def.Fields)))
			// Emit field descriptors as a compound literal
			g.genStructFieldDescs(out, elemType, 0)
			out.WriteString("))")
			return
		}
		// Handle primitive arrays with json.set
		if ast.IsArrayType(valType) {
			argIdent, ok := e.Args[2].(*ast.Ident)
			if ok {
				arrType := g.arrVars[argIdent.Name]
				var setArrFn string
				switch arrType {
				case ast.TypeArrayInt:
					setArrFn = "dex_json_set_arr_int"
				case ast.TypeArrayBool:
					setArrFn = "dex_json_set_arr_bool"
				case ast.TypeArrayString:
					setArrFn = "dex_json_set_arr_str"
				case ast.TypeArrayLong:
					setArrFn = "dex_json_set_arr_long"
				case ast.TypeArrayDouble:
					setArrFn = "dex_json_set_arr_double"
				case ast.TypeArrayChar:
					setArrFn = "dex_json_set_arr_char"
				}
				out.WriteString(fmt.Sprintf("dex_string_from_cstr(%s(", setArrFn))
				g.genStringData(out, e.Args[0])
				out.WriteString(", ")
				g.genStringData(out, e.Args[1])
				out.WriteString(", ")
				out.WriteString(argIdent.Name)
				out.WriteString("))")
			}
			return
		}
		var fn string
		switch valType {
		case ast.TypeInt:
			fn = "dex_json_set_int"
		case ast.TypeBool:
			fn = "dex_json_set_bool"
		case ast.TypeLong:
			fn = "dex_json_set_long"
		case ast.TypeDouble:
			fn = "dex_json_set_double"
		default:
			fn = "dex_json_set"
		}
		// Bridge: string args need ->data, result needs wrapping
		out.WriteString(fmt.Sprintf("dex_string_from_cstr(%s(", fn))
		g.genStringData(out, e.Args[0])
		out.WriteString(", ")
		g.genStringData(out, e.Args[1])
		out.WriteString(", ")
		// For the value arg: if string type, extract ->data
		if valType == ast.TypeString {
			g.genStringData(out, e.Args[2])
		} else {
			g.genExpr(out, e.Args[2])
		}
		out.WriteString("))")
		return
	}

	// json.new() — returns const char*, wrap
	if e.Module == "json" && e.Name == "new" {
		out.WriteString("dex_string_from_cstr(dex_json_new())")
		return
	}

	// db.col(rows, col) — polymorphic: dispatch by resolved return type
	if e.Module == "db" && e.Name == "col" {
		var fn string
		switch e.ResolvedType {
		case ast.TypeString:
			fn = "dex_db_col_str"
		case ast.TypeBool:
			fn = "dex_db_col_bool"
		case ast.TypeDouble:
			fn = "dex_db_col_double"
		default:
			fn = "dex_db_col_int"
		}
		if e.ResolvedType == ast.TypeString {
			out.WriteString("dex_db_col_dexstr(")
			g.genExpr(out, e.Args[0])
			out.WriteString(", ")
			g.genExpr(out, e.Args[1])
			out.WriteString(")")
		} else {
			out.WriteString(fn + "(")
			g.genExpr(out, e.Args[0])
			out.WriteString(", ")
			g.genExpr(out, e.Args[1])
			out.WriteString(")")
		}
		return
	}

	if e.Module == "http" && e.Name == "route" {
		// route("GET", "/path", handler) -> dex_route("GET", "/path", handler_name)
		// Resolve handler name
		var handlerName string
		switch h := e.Args[2].(type) {
		case *ast.StringLit:
			handlerName = h.Value
		case *ast.Ident:
			handlerName = h.Name
			if _, exists := g.funcs[handlerName]; !exists {
				for mod := range g.userModules {
					prefixed := mod + "_" + handlerName
					if _, ok := g.funcs[prefixed]; ok {
						handlerName = prefixed
						break
					}
				}
			}
		case *ast.CallExpr:
			handlerName = h.Name
			if h.Module != "" && g.userModules[h.Module] {
				handlerName = h.Module + "_" + h.Name
			}
		}
		// All wrappers now accept Dex_HttpRequest _req to match the new handler typedef.
		// For 0-param handlers, the wrapper ignores _req.
		// For 1-param handlers taking HttpRequest, the wrapper passes _req through.
		emitName := handlerName
		if fn, ok := g.funcs[handlerName]; ok {
			httpRespType, hasHttpResp := ast.LookupStructType("HttpResponse")
			httpReqType, _ := ast.LookupStructType("HttpRequest")
			handlerTakesReq := len(fn.Params) == 1 && fn.Params[0].Type == httpReqType

			if hasHttpResp && fn.ReturnType == httpRespType && handlerTakesReq {
				// Handler takes HttpRequest and returns HttpResponse — use directly
			} else {
				wrapperName := fmt.Sprintf("_dex_route_wrap_%d", g.routeWrapperCount)
				g.routeWrapperCount++
				var w strings.Builder
				w.WriteString(fmt.Sprintf("Dex_HttpResponse %s(Dex_HttpRequest _req) {\n", wrapperName))

				// Build the handler call
				var callStr string
				if handlerTakesReq {
					callStr = fmt.Sprintf("%s(_req)", handlerName)
				} else {
					callStr = fmt.Sprintf("%s()", handlerName)
				}

				if hasHttpResp && fn.ReturnType == httpRespType {
					// Handler returns HttpResponse but takes no request — call and return
					w.WriteString(fmt.Sprintf("    return %s;\n", callStr))
				} else if fn.ReturnType == ast.TypeString {
					w.WriteString(fmt.Sprintf("    DexString* _val = %s;\n", callStr))
					w.WriteString("    return (Dex_HttpResponse){200, _val, dex_string_from_lit(\"application/json\")};\n")
				} else {
					var fmtSpec string
					switch fn.ReturnType {
					case ast.TypeInt:
						fmtSpec = "%d"
					case ast.TypeLong:
						fmtSpec = "%ld"
					case ast.TypeDouble:
						fmtSpec = "%f"
					case ast.TypeBool:
						fmtSpec = "%s"
					default:
						fmtSpec = "%d"
					}
					w.WriteString(fmt.Sprintf("    %s _val = %s;\n", g.cType(fn.ReturnType), callStr))
					w.WriteString("    char _buf[64];\n")
					if fn.ReturnType == ast.TypeBool {
						w.WriteString("    snprintf(_buf, sizeof(_buf), \"%s\", _val ? \"true\" : \"false\");\n")
					} else {
						w.WriteString(fmt.Sprintf("    snprintf(_buf, sizeof(_buf), \"%s\", _val);\n", fmtSpec))
					}
					w.WriteString("    return (Dex_HttpResponse){200, dex_string_from_lit(_buf), dex_string_from_lit(\"application/json\")};\n")
				}
				w.WriteString("}\n")
				g.spawnWrappers.WriteString(w.String())
				emitName = wrapperName
			}
		}
		out.WriteString("dex_route(")
		g.genStringArg(out, e.Args[0])
		out.WriteString(", ")
		g.genStringArg(out, e.Args[1])
		out.WriteString(", ")
		out.WriteString(emitName)
		out.WriteString(")")
		return
	}

	// http.response(statusCode, body, contentType) -> HttpResponse struct literal
	// Retain borrowed string refs (body, contentType) for ownership transfer to worker thread.
	if e.Module == "http" && e.Name == "response" {
		out.WriteString("(Dex_HttpResponse){")
		g.genExpr(out, e.Args[0])
		out.WriteString(", ")
		g.genOwnedStringArg(out, e.Args[1])
		out.WriteString(", ")
		g.genOwnedStringArg(out, e.Args[2])
		out.WriteString("}")
		return
	}

	// ws.handleMessage(handler) — register message callback
	if e.Module == "ws" && e.Name == "handleMessage" {
		var handlerName string
		switch h := e.Args[0].(type) {
		case *ast.Ident:
			handlerName = h.Name
			if _, exists := g.funcs[handlerName]; !exists {
				for mod := range g.userModules {
					prefixed := mod + "_" + handlerName
					if _, ok := g.funcs[prefixed]; ok {
						handlerName = prefixed
						break
					}
				}
			}
		case *ast.CallExpr:
			handlerName = h.Name
			if h.Module != "" && g.userModules[h.Module] {
				handlerName = h.Module + "_" + h.Name
			}
		}
		out.WriteString("dex_ws_on_message = ")
		out.WriteString(handlerName)
		return
	}

	// ws.handleConnect(handler) — register connect callback
	if e.Module == "ws" && e.Name == "handleConnect" {
		var handlerName string
		switch h := e.Args[0].(type) {
		case *ast.Ident:
			handlerName = h.Name
			if _, exists := g.funcs[handlerName]; !exists {
				for mod := range g.userModules {
					prefixed := mod + "_" + handlerName
					if _, ok := g.funcs[prefixed]; ok {
						handlerName = prefixed
						break
					}
				}
			}
		case *ast.CallExpr:
			handlerName = h.Name
			if h.Module != "" && g.userModules[h.Module] {
				handlerName = h.Module + "_" + h.Name
			}
		}
		out.WriteString("dex_ws_on_connect = ")
		out.WriteString(handlerName)
		return
	}

	// ws.handleDisconnect(handler) — register disconnect callback
	if e.Module == "ws" && e.Name == "handleDisconnect" {
		var handlerName string
		switch h := e.Args[0].(type) {
		case *ast.Ident:
			handlerName = h.Name
			if _, exists := g.funcs[handlerName]; !exists {
				for mod := range g.userModules {
					prefixed := mod + "_" + handlerName
					if _, ok := g.funcs[prefixed]; ok {
						handlerName = prefixed
						break
					}
				}
			}
		case *ast.CallExpr:
			handlerName = h.Name
			if h.Module != "" && g.userModules[h.Module] {
				handlerName = h.Module + "_" + h.Name
			}
		}
		out.WriteString("dex_ws_on_disconnect = ")
		out.WriteString(handlerName)
		return
	}

	// ws.connect(url) -> Dex_Conn
	if e.Module == "ws" && e.Name == "connect" {
		out.WriteString("dex_ws_connect(")
		g.genStringArg(out, e.Args[0])
		out.WriteString(")")
		return
	}

	// ws.send(conn, msg)
	if e.Module == "ws" && e.Name == "send" {
		out.WriteString("dex_ws_send(")
		g.genExpr(out, e.Args[0])
		out.WriteString(", ")
		g.genStringArg(out, e.Args[1])
		out.WriteString(")")
		return
	}

	// ws.receive(conn) -> DexString*
	if e.Module == "ws" && e.Name == "receive" {
		out.WriteString("dex_ws_receive(")
		g.genExpr(out, e.Args[0])
		out.WriteString(")")
		return
	}

	// ws.close(conn)
	if e.Module == "ws" && e.Name == "close" {
		out.WriteString("dex_ws_close(")
		g.genExpr(out, e.Args[0])
		out.WriteString(")")
		return
	}

	// time.setTimeout / time.setInterval — resolve function name, emit C call
	if e.Module == "time" && (e.Name == "setTimeout" || e.Name == "setInterval") {
		var handlerName string
		switch h := e.Args[0].(type) {
		case *ast.Ident:
			handlerName = h.Name
		case *ast.CallExpr:
			handlerName = h.Name
			if h.Module != "" && g.userModules[h.Module] {
				handlerName = h.Module + "_" + h.Name
			}
		}
		if e.Name == "setTimeout" {
			out.WriteString("dex_time_set_timeout(")
		} else {
			out.WriteString("dex_time_set_interval(")
		}
		out.WriteString(handlerName)
		out.WriteString(", ")
		g.genExpr(out, e.Args[1])
		out.WriteString(")")
		return
	}

	// os.exec — returns ExecResult struct
	if e.Module == "os" && e.Name == "exec" {
		out.WriteString("dex_os_exec(")
		g.genStringArg(out, e.Args[0])
		out.WriteString(")")
		return
	}

	// HTTP client functions — bridge string args and wrap string returns
	if e.Module == "http" {
		switch e.Name {
		case "get":
			if len(e.Args) == 2 {
				out.WriteString("dex_http_get_h(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(")")
			} else {
				out.WriteString("dex_http_get(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(")")
			}
			return
		case "post":
			if len(e.Args) == 3 {
				out.WriteString("dex_http_post_h(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[2])
				out.WriteString(")")
			} else {
				out.WriteString("dex_http_post(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(")")
			}
			return
		case "put":
			if len(e.Args) == 3 {
				out.WriteString("dex_http_put_h(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[2])
				out.WriteString(")")
			} else {
				out.WriteString("dex_http_put(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(")")
			}
			return
		case "patch":
			if len(e.Args) == 3 {
				out.WriteString("dex_http_patch_h(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[2])
				out.WriteString(")")
			} else {
				out.WriteString("dex_http_patch(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(")")
			}
			return
		case "delete":
			if len(e.Args) == 2 {
				out.WriteString("dex_http_delete_h(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(")")
			} else {
				out.WriteString("dex_http_delete(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(")")
			}
			return
		case "request":
			out.WriteString("dex_http_request(")
			g.genStringArg(out, e.Args[0])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[1])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[2])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[3])
			out.WriteString(")")
			return
		case "header":
			out.WriteString("dex_string_from_cstr(dex_http_header(")
			g.genStringArg(out, e.Args[0])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[1])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[2])
			out.WriteString("))")
			return
		case "formNew":
			out.WriteString("dex_string_from_cstr(dex_http_form_new())")
			return
		case "formField":
			out.WriteString("dex_string_from_cstr(dex_http_form_field(")
			g.genStringArg(out, e.Args[0])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[1])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[2])
			out.WriteString("))")
			return
		case "formFile":
			out.WriteString("dex_string_from_cstr(dex_http_form_file(")
			g.genStringArg(out, e.Args[0])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[1])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[2])
			out.WriteString("))")
			return
		case "postForm":
			if len(e.Args) == 3 {
				out.WriteString("dex_http_post_form_h(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[2])
				out.WriteString(")")
			} else {
				out.WriteString("dex_http_post_form(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(")")
			}
			return
		case "listen":
			if len(e.Args) == 2 {
				out.WriteString("dex_listen_multi(")
				g.genExpr(out, e.Args[0])
				out.WriteString(", ")
				g.genExpr(out, e.Args[1])
				out.WriteString(")")
			} else {
				out.WriteString("dex_listen(")
				g.genExpr(out, e.Args[0])
				out.WriteString(")")
			}
			return
		}
	}

	// Built-in: close(channel)
	if e.Module == "" && e.Name == "close" {
		out.WriteString("dex_chan_close(")
		g.genExpr(out, e.Args[0])
		out.WriteString(")")
		return
	}

	// Built-in: assert(condition)
	if e.Module == "" && e.Name == "assert" {
		out.WriteString("if (!(")
		g.genExpr(out, e.Args[0])
		out.WriteString(")) { fprintf(stderr, \"FAIL: assert failed\\n\"); exit(1); }")
		return
	}

	// Check if this is a mutex method call (direct variable or field chain)
	if e.Module != "" {
		isMutex := false
		if t, ok := g.varTypes[e.Module]; ok && t == ast.TypeMutex {
			isMutex = true
		}
		if !isMutex && strings.Contains(e.Module, ".") {
			chainType := g.resolveFieldChainType(e.Module)
			if chainType == ast.TypeMutex {
				isMutex = true
			}
		}
		if isMutex {
			// Resolve dotted path with correct accessor (-> for ref types, . for value types)
			cPath := g.resolveFieldChainC(e.Module)
			switch e.Name {
			case "lock":
				out.WriteString(fmt.Sprintf("pthread_mutex_lock(&%s)", cPath))
				return
			case "unlock":
				out.WriteString(fmt.Sprintf("pthread_mutex_unlock(&%s)", cPath))
				return
			}
		}
	}

	// Check if this is a StringBuilder method call
	if e.Module != "" && g.sbVars[e.Module] {
		switch e.Name {
		case "append":
			argType := g.typeOfExpr(e.Args[0])
			var fn string
			switch argType {
			case ast.TypeString:
				fn = "dex_sb_append_str"
			case ast.TypeInt:
				fn = "dex_sb_append_int"
			case ast.TypeLong:
				fn = "dex_sb_append_long"
			case ast.TypeDouble:
				fn = "dex_sb_append_double"
			case ast.TypeBool:
				fn = "dex_sb_append_bool"
			case ast.TypeChar:
				fn = "dex_sb_append_char"
			default:
				fn = "dex_sb_append_str"
			}
			// append copies the bytes out of its argument, so an allocating
			// argument is borrowed and released after the statement.
			out.WriteString(fmt.Sprintf("%s(%s, ", fn, e.Module))
			g.genBorrowed(out, e.Args[0])
			out.WriteString(")")
			return
		case "toString":
			out.WriteString(fmt.Sprintf("dex_sb_toString(%s)", e.Module))
			return
		case "len":
			out.WriteString(fmt.Sprintf("dex_sb_len(%s)", e.Module))
			return
		case "clear":
			out.WriteString(fmt.Sprintf("dex_sb_clear(%s)", e.Module))
			return
		}
	}

	// Check if this is a map method call (e.Module is a variable name or field chain)
	if e.Module != "" {
		mapType, isMap := g.mapVars[e.Module]
		// Field chain resolution: e.g., self.myMap → look up field type
		if !isMap && strings.Contains(e.Module, ".") {
			chainType := g.resolveFieldChainType(e.Module)
			if ast.IsMapType(chainType) {
				mapType = chainType
				isMap = true
			}
		}
		if isMap {
			suffix := g.mapSuffix(mapType)
			// The map retains whatever it stores and only borrows lookup keys, so
			// every argument here is passed borrowed: an allocating key or value
			// is hoisted and released once the statement ends.
			switch e.Name {
			case "set":
				out.WriteString(fmt.Sprintf("dex_map_%s_set(%s, ", suffix, e.Module))
				g.genBorrowed(out, e.Args[0])
				out.WriteString(", ")
				g.genBorrowed(out, e.Args[1])
				out.WriteString(")")
				return
			case "get":
				out.WriteString(fmt.Sprintf("dex_map_%s_get(%s, ", suffix, e.Module))
				g.genBorrowed(out, e.Args[0])
				out.WriteString(")")
				return
			case "has":
				out.WriteString(fmt.Sprintf("dex_map_%s_has(%s, ", suffix, e.Module))
				g.genBorrowed(out, e.Args[0])
				out.WriteString(")")
				return
			case "remove":
				out.WriteString(fmt.Sprintf("dex_map_%s_remove(%s, ", suffix, e.Module))
				g.genBorrowed(out, e.Args[0])
				out.WriteString(")")
				return
			case "len":
				out.WriteString(fmt.Sprintf("dex_map_%s_len(%s)", suffix, e.Module))
				return
			case "clear":
				out.WriteString(fmt.Sprintf("dex_map_%s_clear(%s)", suffix, e.Module))
				return
			case "keys":
				out.WriteString(fmt.Sprintf("dex_map_%s_keys(%s)", suffix, e.Module))
				return
			case "values":
				out.WriteString(fmt.Sprintf("dex_map_%s_values(%s)", suffix, e.Module))
				return
			}
		}
	}

	// Check if this is an array method call (e.Module is a variable name or field chain)
	if e.Module != "" {
		arrType, ok := g.arrVars[e.Module]
		// Field chain resolution: e.g., charger.connectors → look up field type
		if !ok && strings.Contains(e.Module, ".") {
			chainType := g.resolveFieldChainType(e.Module)
			if ast.IsArrayType(chainType) {
				arrType = chainType
				ok = true
			}
		}
		if ok {
			if ast.IsStructArrayType(arrType) {
				// Struct array methods
				elemType := ast.ElementType(arrType)
				elemCType := g.cType(elemType)
				switch e.Name {
				case "push":
					out.WriteString(fmt.Sprintf("{ %s _push_tmp = ", elemCType))
					g.genExpr(out, e.Args[0])
					out.WriteString("; ")
					// Retain heap fields before push — but only borrowed ones.
					// New allocations (string literals, function calls) already have
					// refcount=1 which transfers to the array via memcpy.
					def := ast.GetStructDef(elemType)
					if def != nil {
						if structLit, ok := e.Args[0].(*ast.StructLitExpr); ok {
							// Struct literal: check each field individually
							fieldValueMap := make(map[string]ast.Expr, len(structLit.FieldNames))
							for i, fn := range structLit.FieldNames {
								fieldValueMap[fn] = structLit.FieldValues[i]
							}
							for _, f := range def.Fields {
								if ast.IsHeapType(f.Type) {
									if valExpr, found := fieldValueMap[f.Name]; found {
										if !g.isNewAlloc(valExpr) {
											out.WriteString(fmt.Sprintf("dex_retain(_push_tmp.%s); ", f.Name))
										}
									}
								}
							}
						} else if !g.isNewAlloc(e.Args[0]) {
							// A borrowed struct — a variable, or a field of one —
							// is still owned by whoever it came from, so the array
							// takes its own reference to each heap field.
							for _, f := range def.Fields {
								if ast.IsHeapType(f.Type) {
									out.WriteString(fmt.Sprintf("dex_retain(_push_tmp.%s); ", f.Name))
								}
							}
						}
						// A struct the expression just produced — the result of a
						// call, say — already owns its fields, and that ownership
						// transfers to the array. Retaining here would leave every
						// field one reference above zero forever.
					}
					out.WriteString(fmt.Sprintf("dex_array_struct_push(%s, &_push_tmp); }", e.Module))
					return
				case "len":
					out.WriteString(fmt.Sprintf("%s->len", e.Module))
					return
				case "pop":
					out.WriteString(fmt.Sprintf("dex_array_struct_pop(%s)", e.Module))
					return
				case "remove":
					out.WriteString(fmt.Sprintf("dex_array_struct_remove(%s, ", e.Module))
					g.genExpr(out, e.Args[0])
					out.WriteString(")")
					return
				case "reverse":
					out.WriteString(fmt.Sprintf("dex_array_struct_reverse(%s)", e.Module))
					return
				}
			} else {
				switch e.Name {
				case "push":
					pushFn := g.arrayPushFunc(arrType)
					out.WriteString(fmt.Sprintf("%s(%s, ", pushFn, e.Module))
					g.genExpr(out, e.Args[0])
					out.WriteString(")")
					return
				case "len":
					out.WriteString(fmt.Sprintf("%s->len", e.Module))
					return
				case "pop":
					popFn := g.arrayPopFunc(arrType)
					out.WriteString(fmt.Sprintf("%s(%s)", popFn, e.Module))
					return
				case "remove":
					removeFn := g.arrayRemoveFunc(arrType)
					out.WriteString(fmt.Sprintf("%s(%s, ", removeFn, e.Module))
					g.genExpr(out, e.Args[0])
					out.WriteString(")")
					return
				case "contains":
					containsFn := g.arrayContainsFunc(arrType)
					out.WriteString(fmt.Sprintf("%s(%s, ", containsFn, e.Module))
					g.genExpr(out, e.Args[0])
					out.WriteString(")")
					return
				case "indexOf":
					indexOfFn := g.arrayIndexOfFunc(arrType)
					out.WriteString(fmt.Sprintf("%s(%s, ", indexOfFn, e.Module))
					g.genExpr(out, e.Args[0])
					out.WriteString(")")
					return
				case "reverse":
					reverseFn := g.arrayReverseFunc(arrType)
					out.WriteString(fmt.Sprintf("%s(%s)", reverseFn, e.Module))
					return
				case "sort":
					sortArg := e.Args[0].(*ast.StringLit).Value
					var sortFn string
					if sortArg == "asc" {
						sortFn = g.arraySortAscFunc(arrType)
					} else {
						sortFn = g.arraySortDescFunc(arrType)
					}
					out.WriteString(fmt.Sprintf("%s(%s)", sortFn, e.Module))
					return
				}
			}
		}
	}


	// String method calls: s.len(), s.contains(), etc.
	if e.Module != "" {
		if g.strVars[e.Module] {
			g.usesStringMethods = true
			switch e.Name {
			case "len":
				out.WriteString(fmt.Sprintf("dex_str_len(%s)", e.Module))
				return
			case "contains":
				g.genStrMethodWithArgs(out, "dex_str_contains", e.Module, e.Args[:1])
				return
			case "startsWith":
				g.genStrMethodWithArgs(out, "dex_str_startsWith", e.Module, e.Args[:1])
				return
			case "endsWith":
				g.genStrMethodWithArgs(out, "dex_str_endsWith", e.Module, e.Args[:1])
				return
			case "indexOf":
				g.genStrMethodWithArgs(out, "dex_str_indexOf", e.Module, e.Args[:1])
				return
			case "toLower":
				out.WriteString(fmt.Sprintf("dex_str_toLower(%s)", e.Module))
				return
			case "toUpper":
				out.WriteString(fmt.Sprintf("dex_str_toUpper(%s)", e.Module))
				return
			case "trim":
				out.WriteString(fmt.Sprintf("dex_str_trim(%s)", e.Module))
				return
			case "split":
				g.genStrMethodWithArgs(out, "dex_str_split", e.Module, e.Args[:1])
				return
			case "substring":
				out.WriteString(fmt.Sprintf("dex_str_substring(%s, ", e.Module))
				g.genExpr(out, e.Args[0])
				out.WriteString(", ")
				g.genExpr(out, e.Args[1])
				out.WriteString(")")
				return
			case "replace":
				g.genStrMethodWithArgs(out, "dex_str_replace", e.Module, e.Args[:2])
				return
			case "charAt":
				out.WriteString(fmt.Sprintf("dex_str_charAt(%s, ", e.Module))
				g.genExpr(out, e.Args[0])
				out.WriteString(")")
				return
			case "isAlphanumeric", "isAlpha", "isDigit", "isNumeric", "isWhitespace", "isEmpty",
				"containsUppercase", "containsLowercase", "containsDigit":
				out.WriteString(fmt.Sprintf("dex_str_%s(%s)", e.Name, e.Module))
				return
			}
		}
	}

	// Qualified call with CName — look up from stdlib
	if e.Module != "" {
		funcDef, ok := stdlib.LookupFunc(e.Module, e.Name)
		if ok && funcDef.CName != "" {
			// Check if function returns string — needs wrapping
			if funcDef.ReturnType == ast.TypeString {
				out.WriteString("dex_string_from_cstr(")
				out.WriteString(funcDef.CName)
				out.WriteString("(")
				for i, arg := range e.Args {
					if i > 0 {
						out.WriteString(", ")
					}
					g.genStdlibArg(out, arg, funcDef, i)
				}
				out.WriteString("))")
			} else {
				out.WriteString(funcDef.CName)
				out.WriteString("(")
				for i, arg := range e.Args {
					if i > 0 {
						out.WriteString(", ")
					}
					g.genStdlibArg(out, arg, funcDef, i)
				}
				out.WriteString(")")
			}
			return
		}
	}

	// User-defined function call
	out.WriteString(e.Name)
	out.WriteString("(")
	fn, hasFn := g.funcs[e.Name]
	for i, arg := range e.Args {
		if i > 0 {
			out.WriteString(", ")
		}
		// Wrap argument for optional parameters if needed
		if hasFn && i < len(fn.Params) && ast.IsOptionalType(fn.Params[i].Type) {
			g.genOptionalArg(out, arg, fn.Params[i].Type)
		} else if hasFn && i < len(fn.Params) && ast.IsRefType(fn.Params[i].Type) {
			// Insert & when passing value to ref-typed param
			// but not if the arg is already a ref type
			argType := g.typeOfExpr(arg)
			if !ast.IsRefType(argType) {
				out.WriteString("&")
				g.genExpr(out, arg)
			} else if ident, ok := arg.(*ast.Ident); ok && isPrimitiveRef(argType) {
				// Primitive ref passed to ref param: emit raw pointer name
				out.WriteString(ident.Name)
			} else {
				g.genExpr(out, arg)
			}
		} else {
			// The callee borrows its argument, so an argument that allocates is
			// hoisted and released after the statement rather than leaking.
			g.genBorrowed(out, arg)
		}
	}
	out.WriteString(")")
}

// genOptionalArg generates an argument expression, wrapping it for optional parameters if needed.
func (g *Generator) genOptionalArg(out *strings.Builder, arg ast.Expr, paramType ast.Type) {
	inner := ast.OptionalInnerType(paramType)
	_, isNull := arg.(*ast.NullLit)
	argType := g.typeOfExpr(arg)

	// If the argument is already the optional type, emit directly
	if argType == paramType {
		g.genExpr(out, arg)
		return
	}

	if ast.IsValueType(inner) {
		ctyp := g.cType(paramType)
		if isNull {
			out.WriteString(fmt.Sprintf("(%s){0}", ctyp))
		} else {
			out.WriteString(fmt.Sprintf("(%s){1, ", ctyp))
			g.genExpr(out, arg)
			out.WriteString("}")
		}
	} else {
		// Heap/struct optional: NULL for null, value otherwise
		if isNull {
			out.WriteString("NULL")
		} else {
			g.genExpr(out, arg)
		}
	}
}

// genOwnedStringArg generates a string argument that transfers ownership (+1).
// If the expression is already a new allocation (+1 from call/literal/concat),
// it is emitted directly. Otherwise (borrowed ref like variable, field, index),
// it emits a retain to create an owned copy.
func (g *Generator) genOwnedStringArg(out *strings.Builder, arg ast.Expr) {
	if g.isNewAlloc(arg) {
		g.genExpr(out, arg)
	} else {
		tmp := g.nextTemp()
		out.WriteString(fmt.Sprintf("({ DexString* %s = ", tmp))
		g.genExpr(out, arg)
		out.WriteString(fmt.Sprintf("; dex_retain(%s); %s; })", tmp, tmp))
	}
}

// genStringArg generates a string argument for a stdlib function, extracting ->data
func (g *Generator) genStringArg(out *strings.Builder, arg ast.Expr) {
	argType := g.typeOfExpr(arg)
	if argType == ast.TypeString {
		if strLit, ok := arg.(*ast.StringLit); ok {
			// String literal — just use C string directly
			out.WriteString(fmt.Sprintf("%q", strLit.Value))
		} else {
			g.genExpr(out, arg)
			out.WriteString("->data")
		}
	} else {
		g.genExpr(out, arg)
	}
}

// genStrMethodWithArgs generates a string method call, wrapping any string literal
// arguments in a statement-expression that releases the temp after the call.
func (g *Generator) genStrMethodWithArgs(out *strings.Builder, cFunc, receiver string, args []ast.Expr) {
	// Check if any arg is a string literal (produces +1 ref that would leak)
	hasLitArg := false
	for _, arg := range args {
		if _, ok := arg.(*ast.StringLit); ok {
			hasLitArg = true
			break
		}
	}
	if !hasLitArg {
		// No string literal temps — emit directly
		out.WriteString(fmt.Sprintf("%s(%s", cFunc, receiver))
		for _, arg := range args {
			out.WriteString(", ")
			g.genExpr(out, arg)
		}
		out.WriteString(")")
		return
	}
	// Wrap in statement-expression to release string literal temps
	out.WriteString("({ ")
	var tmpNames []string
	for _, arg := range args {
		tmp := g.nextTemp()
		tmpNames = append(tmpNames, tmp)
		if _, ok := arg.(*ast.StringLit); ok {
			out.WriteString(fmt.Sprintf("DexString* %s = ", tmp))
		} else {
			// Non-literal args: capture the type from the expression
			out.WriteString(fmt.Sprintf("DexString* %s = ", tmp))
		}
		g.genExpr(out, arg)
		out.WriteString("; ")
	}
	resTmp := g.nextTemp()
	out.WriteString(fmt.Sprintf("__auto_type %s = %s(%s", resTmp, cFunc, receiver))
	for _, tmp := range tmpNames {
		out.WriteString(fmt.Sprintf(", %s", tmp))
	}
	out.WriteString("); ")
	// Release only the string literal temps
	for i, arg := range args {
		if _, ok := arg.(*ast.StringLit); ok {
			out.WriteString(fmt.Sprintf("dex_release(%s); ", tmpNames[i]))
		}
	}
	out.WriteString(fmt.Sprintf("%s; })", resTmp))
}

// genStdlibArg generates an argument for a stdlib function with CName,
// bridging DexString* to const char* when the stdlib expects it.
func (g *Generator) genStdlibArg(out *strings.Builder, arg ast.Expr, funcDef *stdlib.FuncDef, idx int) {
	argType := g.typeOfExpr(arg)
	if argType == ast.TypeString {
		// Stdlib functions with CName expect const char*
		if strLit, ok := arg.(*ast.StringLit); ok {
			out.WriteString(fmt.Sprintf("%q", strLit.Value))
		} else {
			// Only the bytes are read, and the DexString wrapping them is not
			// handed over, so an argument that allocates — a concatenated SQL
			// statement, say — is released once the statement finishes.
			g.genBorrowed(out, arg)
			out.WriteString("->data")
		}
	} else {
		g.genBorrowed(out, arg)
	}
}

// genJsonEncodeToString emits an expression producing a DexString* holding the
// JSON text of `arg`. Shared by json.encode and by json.Value construction, so
// there is a single definition of how each shape serializes.
func (g *Generator) genJsonEncodeToString(out *strings.Builder, arg ast.Expr, argType ast.Type) {
	if argType == ast.TypeJsonValue {
		// encode borrows the document, so a freshly-built one — a literal, or the
		// result of indexing — is released once its text has been produced.
		if g.jsonValueOwned(arg) {
			tmp := g.nextTemp()
			res := g.nextTemp()
			out.WriteString(fmt.Sprintf("({ DexJsonValue* %s = ", tmp))
			g.genExpr(out, arg)
			out.WriteString(fmt.Sprintf("; DexString* %s = dex_jv_encode(%s); dex_release(%s); %s; })", res, tmp, tmp, res))
			return
		}
		out.WriteString("dex_jv_encode(")
		g.genExpr(out, arg)
		out.WriteString(")")
		return
	}
	if ast.IsMapType(argType) {
		valType := ast.MapValueType(argType)
		suffix := g.mapSuffix(argType)
		mapCType := "DexMap_" + suffix
		wrapperName := fmt.Sprintf("_dex_json_map_%s_%d", suffix, g.routeWrapperCount)
		g.routeWrapperCount++
		var w strings.Builder
		w.WriteString(fmt.Sprintf("DexString* %s(%s* _m) {\n", wrapperName, mapCType))
		w.WriteString("    const char* _r = dex_json_new();\n")
		w.WriteString("    for (int _i = 0; _i < _m->cap; _i++) {\n")
		w.WriteString("        if (!_m->entries[_i].occupied) continue;\n")
		switch valType {
		case ast.TypeString:
			w.WriteString("        _r = dex_json_set(_r, _m->entries[_i].key->data, _m->entries[_i].value->data);\n")
		case ast.TypeInt:
			w.WriteString("        _r = dex_json_set_int(_r, _m->entries[_i].key->data, _m->entries[_i].value);\n")
		case ast.TypeBool:
			w.WriteString("        _r = dex_json_set_bool(_r, _m->entries[_i].key->data, _m->entries[_i].value);\n")
		case ast.TypeLong:
			w.WriteString("        _r = dex_json_set_long(_r, _m->entries[_i].key->data, _m->entries[_i].value);\n")
		case ast.TypeDouble:
			w.WriteString("        _r = dex_json_set_double(_r, _m->entries[_i].key->data, _m->entries[_i].value);\n")
		}
		w.WriteString("    }\n")
		w.WriteString("    return dex_string_from_cstr(_r);\n")
		w.WriteString("}\n")
		g.spawnWrappers.WriteString(w.String())
		out.WriteString(wrapperName + "(")
		g.genExpr(out, arg)
		out.WriteString(")")
		return
	}
	if ast.IsStructType(argType) && !ast.IsArrayType(argType) {
		// Struct stringify: use dex_json_encode_struct
		def := ast.GetStructDef(argType)
		out.WriteString("dex_string_from_cstr(dex_json_encode_struct(&")
		g.genExpr(out, arg)
		out.WriteString(fmt.Sprintf(", %d, ", len(def.Fields)))
		g.genStructFieldDescs(out, argType, 0)
		out.WriteString("))")
		return
	}
	// Arrays. The array's type comes from the expression rather than from a
	// variable-name lookup, so encoding the result of a call — json.encode(svc.list())
	// — works as readily as encoding a named local.
	arrType := argType
	if ident, isIdent := arg.(*ast.Ident); isIdent && !ast.IsArrayType(arrType) {
		arrType = g.arrVars[ident.Name]
	}
	if !ast.IsArrayType(arrType) {
		// Not something this function knows how to encode; the checker rejects
		// these, so emitting an empty document keeps the output valid C.
		out.WriteString("dex_string_from_lit(\"null\")")
		return
	}

	// The value is bound to a temporary first: it may be a call, and it must be
	// evaluated exactly once and released if it was freshly built.
	owned := g.isNewAlloc(arg)
	tmp := g.nextTemp()
	res := g.nextTemp()
	out.WriteString(fmt.Sprintf("({ %s %s = ", g.cType(arrType), tmp))
	g.genExpr(out, arg)
	out.WriteString(fmt.Sprintf("; DexString* %s = ", res))

	if ast.IsStructArrayType(arrType) {
		elemType := ast.ElementType(arrType)
		def := ast.GetStructDef(elemType)
		out.WriteString(fmt.Sprintf("dex_string_from_cstr(dex_json_stringify_struct_arr(%s, sizeof(%s), %d, ", tmp, g.cType(elemType), len(def.Fields)))
		g.genStructFieldDescs(out, elemType, 0)
		out.WriteString("))")
	} else {
		var fn string
		switch arrType {
		case ast.TypeArrayInt:
			fn = "dex_json_stringify_int"
		case ast.TypeArrayBool:
			fn = "dex_json_stringify_bool"
		case ast.TypeArrayString:
			fn = "dex_json_stringify_str"
		case ast.TypeArrayLong:
			fn = "dex_json_stringify_long"
		case ast.TypeArrayDouble:
			fn = "dex_json_stringify_double"
		case ast.TypeArrayChar:
			fn = "dex_json_stringify_char"
		}
		out.WriteString(fmt.Sprintf("dex_string_from_cstr(%s(%s))", fn, tmp))
	}

	out.WriteString("; ")
	if owned {
		out.WriteString(fmt.Sprintf("dex_release(%s); ", tmp))
	}
	out.WriteString(fmt.Sprintf("%s; })", res))
}

// jsonDecodeFromString emits a decode of JSON *text* into the call's resolved
// struct or struct-array target. json.Value targets and json.Value sources are
// handled by the caller before reaching here.
func (g *Generator) jsonDecodeFromString(out *strings.Builder, e *ast.CallExpr) {
	// Struct array target: walk the JSON array and decode each element.
	// Composed from json.c and arrays.c primitives, both of which are emitted
	// before user code whenever this expression can appear.
	arrTarget := e.ResolvedType
	checkedArr := false
	if ast.IsOptionalType(arrTarget) && ast.IsStructArrayType(ast.OptionalInnerType(arrTarget)) {
		arrTarget = ast.OptionalInnerType(arrTarget)
		checkedArr = true
	}
	if ast.IsStructArrayType(arrTarget) {
		elemType := ast.ElementType(arrTarget)
		elemCType := g.cType(elemType)
		def := ast.GetStructDef(elemType)
		cleanupFn := g.structArrayCleanupFunc(elemType)

		out.WriteString("({ const char* _jsrc = ")
		g.genStringData(out, e.Args[0])
		out.WriteString("; DexArrayStruct* _jarr = NULL; ")
		if checkedArr {
			out.WriteString("if (dex_json_is_array(_jsrc)) { ")
		} else {
			out.WriteString("{ ")
		}
		out.WriteString(fmt.Sprintf("_jarr = dex_array_struct_new(sizeof(%s), %s); ", elemCType, cleanupFn))
		out.WriteString("int _jn = dex_json_array_len(_jsrc); ")
		out.WriteString("for (int _ji = 0; _ji < _jn; _ji++) { ")
		out.WriteString("const char* _jel = dex_json_array_get_raw(_jsrc, _ji); ")
		out.WriteString(fmt.Sprintf("%s _jtmp; memset(&_jtmp, 0, sizeof(%s)); ", elemCType, elemCType))
		if checkedArr {
			out.WriteString(fmt.Sprintf("int _jok = dex_json_decode_struct_checked(_jel, &_jtmp, %d, ", len(def.Fields)))
			g.genStructFieldDescs(out, elemType, 0)
			out.WriteString("); free((void*)_jel); ")
			out.WriteString("if (!_jok) { dex_release(_jarr); _jarr = NULL; break; } ")
			out.WriteString("dex_array_struct_push(_jarr, &_jtmp); ")
		} else {
			out.WriteString(fmt.Sprintf("dex_json_decode_struct(_jel, &_jtmp, %d, ", len(def.Fields)))
			g.genStructFieldDescs(out, elemType, 0)
			out.WriteString("); free((void*)_jel); ")
			out.WriteString("dex_array_struct_push(_jarr, &_jtmp); ")
		}
		out.WriteString("} } _jarr; })")
		return
	}

	// An optional target means a checked decode: malformed JSON, or a present
	// key whose type does not fit the struct, yields NULL instead of zeroes.
	if ast.IsOptionalType(e.ResolvedType) && ast.IsStructType(ast.OptionalInnerType(e.ResolvedType)) {
		structType := ast.OptionalInnerType(e.ResolvedType)
		def := ast.GetStructDef(structType)
		innerCType := g.cType(structType)
		out.WriteString(fmt.Sprintf("({ %s* _jopt_tmp = (%s*)calloc(1, sizeof(%s)); if (!dex_json_decode_struct_checked(", innerCType, innerCType, innerCType))
		g.genStringData(out, e.Args[0])
		out.WriteString(fmt.Sprintf(", _jopt_tmp, %d, ", len(def.Fields)))
		g.genStructFieldDescs(out, structType, 0)
		out.WriteString(")) { free(_jopt_tmp); _jopt_tmp = NULL; } _jopt_tmp; })")
		return
	}
	structType := e.ResolvedType
	def := ast.GetStructDef(structType)
	cType := g.cType(structType)
	out.WriteString(fmt.Sprintf("({ %s _jobj_tmp = {0}; dex_json_decode_struct(", cType))
	g.genStringData(out, e.Args[0])
	out.WriteString(fmt.Sprintf(", &_jobj_tmp, %d, ", len(def.Fields)))
	g.genStructFieldDescs(out, structType, 0)
	out.WriteString("); _jobj_tmp; })")
	return
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
