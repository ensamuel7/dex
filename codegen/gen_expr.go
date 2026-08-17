package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/stdlib"
)

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

	case *ast.Ident:
		if narrowed, ok := g.narrowedVars[e.Name]; ok {
			out.WriteString(narrowed)
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
		if ast.IsMapType(arrType) {
			suffix := g.mapSuffix(arrType)
			out.WriteString(fmt.Sprintf("dex_map_%s_get(", suffix))
			g.genExpr(out, e.Array)
			out.WriteString(", ")
			g.genExpr(out, e.Index)
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

	case *ast.ArrayLitExpr:
		// Non-let context — shouldn't normally happen since checker ensures array literals
		// are used in let statements, but handle defensively
		out.WriteString("/* array literal */")

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
			if structDef != nil {
				for _, f := range structDef.Fields {
					if f.Name == fn && ast.IsRefType(f.Type) {
						argType := g.typeOfExpr(e.FieldValues[i])
						if !ast.IsRefType(argType) {
							out.WriteString("&")
						}
						break
					}
				}
			}
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
		if ast.IsRefType(objType) {
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
		// Empty map literal in non-let context (shouldn't normally happen)
		out.WriteString("/* map literal */")

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
	}
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
func (g *Generator) genStringData(out *strings.Builder, expr ast.Expr) {
	if _, ok := expr.(*ast.StringLit); ok {
		// String literals: just emit the C string literal directly for strcmp
		out.WriteString(fmt.Sprintf("%q", expr.(*ast.StringLit).Value))
		return
	}
	g.genExpr(out, expr)
	out.WriteString("->data")
}

func (g *Generator) genCallExpr(out *strings.Builder, e *ast.CallExpr) {
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
				}
			}
			g.genExpr(out, e.Args[i])
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
			g.genExpr(out, arg)
		}
		out.WriteString(")")
		return
	}

	// User module call: emit prefixed function name
	if e.Module != "" && g.userModules[e.Module] {
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
				}
			}
			g.genExpr(out, arg)
		}
		out.WriteString(")")
		return
	}

	// Special case: fmt.print / fmt.println — polymorphic print for any type
	if e.Module == "fmt" && (e.Name == "print" || e.Name == "println") {
		newline := e.Name == "println"
		argType := g.typeOfExpr(e.Args[0])
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
			g.genExpr(out, e.Args[0])
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
	// json.stringify(array or struct) — special codegen (returns const char*, needs wrapping)
	if e.Module == "json" && e.Name == "stringify" {
		argType := g.typeOfExpr(e.Args[0])
		if ast.IsStructType(argType) && !ast.IsArrayType(argType) {
			// Struct stringify: use dex_json_encode_struct
			def := ast.GetStructDef(argType)
			cType := g.cType(argType)
			out.WriteString(fmt.Sprintf("dex_string_from_cstr(dex_json_encode_struct(&"))
			g.genExpr(out, e.Args[0])
			out.WriteString(fmt.Sprintf(", %d, ", len(def.Fields)))
			out.WriteString("(DexStructFieldDesc[]){ ")
			for i, f := range def.Fields {
				if i > 0 {
					out.WriteString(", ")
				}
				fieldOffset := fmt.Sprintf("offsetof(%s, %s)", cType, f.Name)
				out.WriteString(fmt.Sprintf("{\"%s\", %s, %s}", f.Name, fieldOffset, g.jsonFieldKind(f.Type)))
			}
			out.WriteString(" }))")
			return
		}
		argIdent, ok := e.Args[0].(*ast.Ident)
		if ok {
			arrType := g.arrVars[argIdent.Name]
			if ast.IsStructArrayType(arrType) {
				// Struct array stringify: use dex_json_set_struct_arr with empty obj
				elemType := ast.ElementType(arrType)
				elemCType := g.cType(elemType)
				def := ast.GetStructDef(elemType)
				out.WriteString(fmt.Sprintf("dex_string_from_cstr(dex_json_set_struct_arr(\"{}\", \"_\", %s, sizeof(%s), %d, ", argIdent.Name, elemCType, len(def.Fields)))
				out.WriteString("(DexStructFieldDesc[]){ ")
				for i, f := range def.Fields {
					if i > 0 {
						out.WriteString(", ")
					}
					fieldOffset := fmt.Sprintf("offsetof(%s, %s)", elemCType, f.Name)
					out.WriteString(fmt.Sprintf("{\"%s\", %s, %s}", f.Name, fieldOffset, g.jsonFieldKind(f.Type)))
				}
				out.WriteString(" }))")
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
				out.WriteString(fmt.Sprintf("dex_string_from_cstr(%s(%s))", fn, argIdent.Name))
			}
		}
		return
	}

	// json.objectify(jsonStr) — decode JSON string into a struct
	if e.Module == "json" && e.Name == "objectify" {
		structType := e.ResolvedType
		def := ast.GetStructDef(structType)
		cType := g.cType(structType)
		out.WriteString(fmt.Sprintf("({ %s _jobj_tmp = {0}; dex_json_decode_struct(", cType))
		g.genExpr(out, e.Args[0])
		out.WriteString(fmt.Sprintf("->data, &_jobj_tmp, %d, ", len(def.Fields)))
		out.WriteString("(DexStructFieldDesc[]){ ")
		for i, f := range def.Fields {
			if i > 0 {
				out.WriteString(", ")
			}
			fieldOffset := fmt.Sprintf("offsetof(%s, %s)", cType, f.Name)
			out.WriteString(fmt.Sprintf("{\"%s\", %s, %s}", f.Name, fieldOffset, g.jsonFieldKind(f.Type)))
		}
		out.WriteString(" }); _jobj_tmp; })")
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
				g.genExpr(out, e.Args[0])
				out.WriteString("->data, ")
				g.genExpr(out, e.Args[1])
				out.WriteString("->data, ")
				out.WriteString(argIdent.Name)
				out.WriteString(fmt.Sprintf(", sizeof(%s), %d, ", elemCType, len(def.Fields)))
				out.WriteString("(DexStructFieldDesc[]){ ")
				for i, f := range def.Fields {
					if i > 0 {
						out.WriteString(", ")
					}
					fieldOffset := fmt.Sprintf("offsetof(%s, %s)", elemCType, f.Name)
					var fieldKind string
					switch f.Type {
					case ast.TypeInt:
						fieldKind = "0"
					case ast.TypeBool:
						fieldKind = "1"
					case ast.TypeString:
						fieldKind = "2"
					case ast.TypeLong:
						fieldKind = "3"
					case ast.TypeDouble:
						fieldKind = "4"
					default:
						fieldKind = "0"
					}
					out.WriteString(fmt.Sprintf("{\"%s\", %s, %s}", f.Name, fieldOffset, fieldKind))
				}
				out.WriteString(" }))")
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
				g.genExpr(out, e.Args[0])
				out.WriteString("->data, ")
				g.genExpr(out, e.Args[1])
				out.WriteString("->data, ")
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
		g.genExpr(out, e.Args[0])
		out.WriteString("->data, ")
		if valType == ast.TypeString {
			g.genExpr(out, e.Args[1])
			out.WriteString("->data")
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
			g.genExpr(out, e.Args[0])
			out.WriteString("->data, ")
			g.genExpr(out, e.Args[1])
			out.WriteString("->data, ")
			g.genExpr(out, e.Args[2])
			out.WriteString(fmt.Sprintf(", sizeof(%s), %d, ", elemCType, len(def.Fields)))
			// Emit field descriptors as a compound literal
			out.WriteString(fmt.Sprintf("(DexStructFieldDesc[]){ "))
			for i, f := range def.Fields {
				if i > 0 {
					out.WriteString(", ")
				}
				fieldOffset := fmt.Sprintf("offsetof(%s, %s)", elemCType, f.Name)
				var fieldKind string
				switch f.Type {
				case ast.TypeInt:
					fieldKind = "0"
				case ast.TypeBool:
					fieldKind = "1"
				case ast.TypeString:
					fieldKind = "2"
				case ast.TypeLong:
					fieldKind = "3"
				case ast.TypeDouble:
					fieldKind = "4"
				default:
					fieldKind = "0"
				}
				out.WriteString(fmt.Sprintf("{\"%s\", %s, %s}", f.Name, fieldOffset, fieldKind))
			}
			out.WriteString(" }))")
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
				g.genExpr(out, e.Args[0])
				out.WriteString("->data, ")
				g.genExpr(out, e.Args[1])
				out.WriteString("->data, ")
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
		g.genExpr(out, e.Args[0])
		out.WriteString("->data, ")
		g.genExpr(out, e.Args[1])
		out.WriteString("->data, ")
		// For the value arg: if string type, extract ->data
		if valType == ast.TypeString {
			g.genExpr(out, e.Args[2])
			out.WriteString("->data")
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
			out.WriteString(fmt.Sprintf("dex_string_from_cstr(%s(", fn))
			g.genExpr(out, e.Args[0])
			out.WriteString(", ")
			g.genExpr(out, e.Args[1])
			out.WriteString("))")
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
			out.WriteString(fmt.Sprintf("%s(%s, ", fn, e.Module))
			g.genExpr(out, e.Args[0])
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
			switch e.Name {
			case "set":
				out.WriteString(fmt.Sprintf("dex_map_%s_set(%s, ", suffix, e.Module))
				g.genExpr(out, e.Args[0])
				out.WriteString(", ")
				g.genExpr(out, e.Args[1])
				out.WriteString(")")
				return
			case "get":
				out.WriteString(fmt.Sprintf("dex_map_%s_get(%s, ", suffix, e.Module))
				g.genExpr(out, e.Args[0])
				out.WriteString(")")
				return
			case "has":
				out.WriteString(fmt.Sprintf("dex_map_%s_has(%s, ", suffix, e.Module))
				g.genExpr(out, e.Args[0])
				out.WriteString(")")
				return
			case "remove":
				out.WriteString(fmt.Sprintf("dex_map_%s_remove(%s, ", suffix, e.Module))
				g.genExpr(out, e.Args[0])
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

	// Check if this is an array method call (e.Module is a variable name)
	if e.Module != "" {
		if arrType, ok := g.arrVars[e.Module]; ok {
			if ast.IsStructArrayType(arrType) {
				// Struct array methods
				elemType := ast.ElementType(arrType)
				elemCType := g.cType(elemType)
				switch e.Name {
				case "push":
					out.WriteString(fmt.Sprintf("{ %s _push_tmp = ", elemCType))
					g.genExpr(out, e.Args[0])
					out.WriteString("; ")
					// Retain heap fields before push
					def := ast.GetStructDef(elemType)
					if def != nil {
						for _, f := range def.Fields {
							if ast.IsHeapType(f.Type) {
								out.WriteString(fmt.Sprintf("dex_retain(_push_tmp.%s); ", f.Name))
							}
						}
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
			// Insert & when passing struct value to ref-typed param
			// but not if the arg is already a ref type
			argType := g.typeOfExpr(arg)
			if !ast.IsRefType(argType) {
				out.WriteString("&")
			}
			g.genExpr(out, arg)
		} else {
			g.genExpr(out, arg)
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
			g.genExpr(out, arg)
			out.WriteString("->data")
		}
	} else {
		g.genExpr(out, arg)
	}
}

func (g *Generator) genSpawnExpr(out *strings.Builder, e *ast.SpawnExpr) {
	idx := g.spawnCounter
	g.spawnCounter++
	wrapperName := fmt.Sprintf("_dex_spawn_%d", idx)
	ctxType := fmt.Sprintf("_dex_spawn_%d_ctx", idx)

	if e.Body != nil {
		// Spawn block: spawn { body }
		// Determine captured variables from outer scope used in body
		captured := g.findCapturedVars(e.Body)

		// Build context struct
		g.spawnWrappers.WriteString(fmt.Sprintf("typedef struct { DexChan* _ch;"))
		for _, cv := range captured {
			g.spawnWrappers.WriteString(fmt.Sprintf(" %s %s;", g.cType(cv.typ), cv.name))
		}
		g.spawnWrappers.WriteString(fmt.Sprintf(" } %s;\n", ctxType))

		// Build wrapper function
		g.spawnWrappers.WriteString(fmt.Sprintf("void* %s(void* _raw) {\n", wrapperName))
		g.spawnWrappers.WriteString(fmt.Sprintf("    %s* _ctx = (%s*)_raw;\n", ctxType, ctxType))
		g.spawnWrappers.WriteString("    DexChan* _ch = _ctx->_ch;\n")
		for _, cv := range captured {
			g.spawnWrappers.WriteString(fmt.Sprintf("    %s %s = _ctx->%s;\n", g.cType(cv.typ), cv.name, cv.name))
		}
		g.spawnWrappers.WriteString("    free(_raw);\n")

		// Generate body statements into wrapper
		var bodyBuf strings.Builder
		for _, stmt := range e.Body {
			g.genStmt(&bodyBuf, stmt, 1)
		}
		g.spawnWrappers.WriteString(bodyBuf.String())
		// Release heap-typed captures before returning
		for _, cv := range captured {
			if ast.IsHeapType(cv.typ) {
				g.spawnWrappers.WriteString(fmt.Sprintf("    dex_release(%s);\n", cv.name))
			}
		}
		g.spawnWrappers.WriteString("    dex_release(_ch);\n")
		g.spawnWrappers.WriteString("    return NULL;\n")
		g.spawnWrappers.WriteString("}\n")

		// Generate call site inline (using GCC statement expression)
		retCType := g.cType(e.ReturnType)
		if retCType == "void" {
			retCType = "int" // placeholder for sizeof in fire-and-forget
		}
		out.WriteString(fmt.Sprintf("({ DexChan* _spawn_ch = dex_chan_new(sizeof(%s), 64); ", retCType))
		out.WriteString(fmt.Sprintf("%s* _spawn_ctx = (%s*)malloc(sizeof(%s)); ", ctxType, ctxType, ctxType))
		out.WriteString("_spawn_ctx->_ch = _spawn_ch; dex_retain(_spawn_ch); ")
		for _, cv := range captured {
			out.WriteString(fmt.Sprintf("_spawn_ctx->%s = %s; ", cv.name, cv.name))
			if ast.IsHeapType(cv.typ) {
				out.WriteString(fmt.Sprintf("dex_retain(%s); ", cv.name))
			}
		}
		out.WriteString(fmt.Sprintf("pthread_t _spawn_t_%d; ", idx))
		out.WriteString(fmt.Sprintf("pthread_create(&_spawn_t_%d, NULL, %s, _spawn_ctx); ", idx, wrapperName))
		out.WriteString(fmt.Sprintf("pthread_detach(_spawn_t_%d); ", idx))
		out.WriteString("_spawn_ch; })")
	} else if e.Call != nil {
		// Spawn function call: spawn fn(args)
		call := e.Call.(*ast.CallExpr)

		// Build context struct with channel + args
		g.spawnWrappers.WriteString(fmt.Sprintf("typedef struct { DexChan* _ch;"))
		for i, arg := range call.Args {
			argType := g.typeOfExpr(arg)
			g.spawnWrappers.WriteString(fmt.Sprintf(" %s _a%d;", g.cType(argType), i))
		}
		g.spawnWrappers.WriteString(fmt.Sprintf(" } %s;\n", ctxType))

		// Build wrapper function
		g.spawnWrappers.WriteString(fmt.Sprintf("void* %s(void* _raw) {\n", wrapperName))
		g.spawnWrappers.WriteString(fmt.Sprintf("    %s* _ctx = (%s*)_raw;\n", ctxType, ctxType))
		g.spawnWrappers.WriteString("    DexChan* _ch = _ctx->_ch;\n")
		for i, arg := range call.Args {
			argType := g.typeOfExpr(arg)
			g.spawnWrappers.WriteString(fmt.Sprintf("    %s _a%d = _ctx->_a%d;\n", g.cType(argType), i, i))
		}
		g.spawnWrappers.WriteString("    free(_raw);\n")

		// Call the function
		retType := e.ReturnType
		callName := call.Name
		if call.Module != "" {
			callName = call.Module + "_" + call.Name
		}
		if retType != ast.TypeVoid {
			g.spawnWrappers.WriteString(fmt.Sprintf("    %s _ret = %s(", g.cType(retType), callName))
		} else {
			g.spawnWrappers.WriteString(fmt.Sprintf("    %s(", callName))
		}
		for i := range call.Args {
			if i > 0 {
				g.spawnWrappers.WriteString(", ")
			}
			g.spawnWrappers.WriteString(fmt.Sprintf("_a%d", i))
		}
		g.spawnWrappers.WriteString(");\n")
		if retType != ast.TypeVoid {
			g.spawnWrappers.WriteString("    dex_chan_send(_ch, &_ret);\n")
		}
		// Release heap-typed args before returning
		for i, arg := range call.Args {
			argType := g.typeOfExpr(arg)
			if ast.IsHeapType(argType) {
				g.spawnWrappers.WriteString(fmt.Sprintf("    dex_release(_a%d);\n", i))
			}
		}
		g.spawnWrappers.WriteString("    dex_release(_ch);\n")
		g.spawnWrappers.WriteString("    return NULL;\n")
		g.spawnWrappers.WriteString("}\n")

		// Generate call site
		retCType := g.cType(e.ReturnType)
		if retCType == "void" {
			retCType = "int" // use int for void tasks to have a valid sizeof
		}
		out.WriteString(fmt.Sprintf("({ DexChan* _spawn_ch = dex_chan_new(sizeof(%s), 1); ", retCType))
		out.WriteString(fmt.Sprintf("%s* _spawn_ctx = (%s*)malloc(sizeof(%s)); ", ctxType, ctxType, ctxType))
		out.WriteString("_spawn_ctx->_ch = _spawn_ch; dex_retain(_spawn_ch); ")
		for i, arg := range call.Args {
			out.WriteString(fmt.Sprintf("_spawn_ctx->_a%d = ", i))
			g.genExpr(out, arg)
			out.WriteString("; ")
			argType := g.typeOfExpr(arg)
			if ast.IsHeapType(argType) {
				out.WriteString(fmt.Sprintf("dex_retain(_spawn_ctx->_a%d); ", i))
			}
		}
		out.WriteString(fmt.Sprintf("pthread_t _spawn_t_%d; ", idx))
		out.WriteString(fmt.Sprintf("pthread_create(&_spawn_t_%d, NULL, %s, _spawn_ctx); ", idx, wrapperName))
		out.WriteString(fmt.Sprintf("pthread_detach(_spawn_t_%d); ", idx))
		out.WriteString("_spawn_ch; })")
	}
}

type capturedVar struct {
	name string
	typ  ast.Type
}

// findCapturedVars identifies variables referenced in spawn body that are defined in the outer scope.
func (g *Generator) findCapturedVars(body []ast.Stmt) []capturedVar {
	used := make(map[string]bool)
	defined := make(map[string]bool) // vars defined within the spawn body
	g.collectUsedVars(body, used, defined)

	var result []capturedVar
	for name := range used {
		if defined[name] {
			continue
		}
		if typ, ok := g.varTypes[name]; ok {
			result = append(result, capturedVar{name: name, typ: typ})
		}
	}
	return result
}

func (g *Generator) collectUsedVars(stmts []ast.Stmt, used, defined map[string]bool) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.LetStmt:
			g.collectUsedVarsExpr(s.Value, used)
			defined[s.Name] = true
		case *ast.ExprStmt:
			g.collectUsedVarsExpr(s.Expr, used)
		case *ast.ReturnStmt:
			if s.Value != nil {
				g.collectUsedVarsExpr(s.Value, used)
			}
		case *ast.AssignStmt:
			g.collectUsedVarsExpr(s.Value, used)
			used[s.Name] = true
		case *ast.SendStmt:
			if s.Target != nil {
				g.collectUsedVarsExpr(s.Target, used)
			}
			g.collectUsedVarsExpr(s.Value, used)
		case *ast.IfStmt:
			g.collectUsedVarsExpr(s.Cond, used)
			g.collectUsedVars(s.Then, used, defined)
			g.collectUsedVars(s.Else, used, defined)
		case *ast.WhileStmt:
			g.collectUsedVarsExpr(s.Cond, used)
			g.collectUsedVars(s.Body, used, defined)
		case *ast.ForStmt:
			g.collectUsedVars([]ast.Stmt{s.Init}, used, defined)
			g.collectUsedVarsExpr(s.Cond, used)
			g.collectUsedVars([]ast.Stmt{s.Post}, used, defined)
			g.collectUsedVars(s.Body, used, defined)
		case *ast.ForeachStmt:
			g.collectUsedVarsExpr(s.Iterable, used)
			defined[s.ValueVar] = true
			if s.IndexVar != "" {
				defined[s.IndexVar] = true
			}
			g.collectUsedVars(s.Body, used, defined)
		case *ast.BlockStmt:
			g.collectUsedVars(s.Stmts, used, defined)
		case *ast.IncrementStmt:
			used[s.Name] = true
		case *ast.DecrementStmt:
			used[s.Name] = true
		case *ast.CompoundAssignStmt:
			used[s.Name] = true
			g.collectUsedVarsExpr(s.Value, used)
		case *ast.IndexAssignStmt:
			g.collectUsedVarsExpr(s.Array, used)
			g.collectUsedVarsExpr(s.Index, used)
			g.collectUsedVarsExpr(s.Value, used)
		case *ast.FieldAssignStmt:
			g.collectUsedVarsExpr(s.Object, used)
			g.collectUsedVarsExpr(s.Value, used)
		}
	}
}

func (g *Generator) collectUsedVarsExpr(expr ast.Expr, used map[string]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.Ident:
		used[e.Name] = true
	case *ast.BinaryExpr:
		g.collectUsedVarsExpr(e.Left, used)
		g.collectUsedVarsExpr(e.Right, used)
	case *ast.UnaryExpr:
		g.collectUsedVarsExpr(e.Operand, used)
	case *ast.CallExpr:
		for _, arg := range e.Args {
			g.collectUsedVarsExpr(arg, used)
		}
	case *ast.IndexExpr:
		g.collectUsedVarsExpr(e.Array, used)
		g.collectUsedVarsExpr(e.Index, used)
	case *ast.FieldAccessExpr:
		g.collectUsedVarsExpr(e.Object, used)
	case *ast.ArrayLitExpr:
		for _, elem := range e.Elems {
			g.collectUsedVarsExpr(elem, used)
		}
	case *ast.StructLitExpr:
		for _, v := range e.FieldValues {
			g.collectUsedVarsExpr(v, used)
		}
	case *ast.SpawnExpr:
		if e.Body != nil {
			defined := make(map[string]bool)
			g.collectUsedVars(e.Body, used, defined)
		}
		if e.Call != nil {
			g.collectUsedVarsExpr(e.Call, used)
		}
	case *ast.ReceiveExpr:
		g.collectUsedVarsExpr(e.Source, used)
	case *ast.ChannelExpr:
		// no vars
	case *ast.NullLit:
		// no vars
	case *ast.EnumAccessExpr:
		// no vars
	case *ast.MapLitExpr:
		// no vars
	}
}

// genExprNoParen generates an expression without wrapping outer parens,
// used for if/while conditions which already provide parens.
func (g *Generator) genExprNoParen(out *strings.Builder, expr ast.Expr) {
	switch e := expr.(type) {
	case *ast.BinaryExpr:
		// Check null comparison in no-paren context
		if g.isNullComparison(e) {
			g.genNullComparison(out, e, false)
			return
		}
		// Check string comparison in no-paren context too
		if g.isStringExpr(e.Left) || g.isStringExpr(e.Right) {
			switch e.Op {
			case ast.BinEq, ast.BinStrictEq:
				out.WriteString("strcmp(")
				g.genStringData(out, e.Left)
				out.WriteString(", ")
				g.genStringData(out, e.Right)
				out.WriteString(") == 0")
				return
			case ast.BinNeq, ast.BinStrictNeq:
				out.WriteString("strcmp(")
				g.genStringData(out, e.Left)
				out.WriteString(", ")
				g.genStringData(out, e.Right)
				out.WriteString(") != 0")
				return
			}
		}
		// Cross-numeric operations in no-paren context
		if e.HasMixedTypes {
			widerType := g.widerNumericType(e.LeftType, e.RightType)
			castType := g.cType(widerType)
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
			return
		}
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
	default:
		g.genExpr(out, expr)
	}
}

// isNullComparison checks if a binary expression is a null comparison.
func (g *Generator) isNullComparison(e *ast.BinaryExpr) bool {
	if e.Op != ast.BinEq && e.Op != ast.BinNeq {
		return false
	}
	_, leftNull := e.Left.(*ast.NullLit)
	_, rightNull := e.Right.(*ast.NullLit)
	return leftNull || rightNull
}

// genNullComparison emits a null comparison for optional types.
func (g *Generator) genNullComparison(out *strings.Builder, e *ast.BinaryExpr, withParen bool) {
	var nonNullExpr ast.Expr
	if _, ok := e.Left.(*ast.NullLit); ok {
		nonNullExpr = e.Right
	} else {
		nonNullExpr = e.Left
	}

	// Determine the type of the non-null side
	nonNullType := g.typeOfExpr(nonNullExpr)
	isEq := e.Op == ast.BinEq

	if ast.IsOptionalType(nonNullType) {
		inner := ast.OptionalInnerType(nonNullType)
		if ast.IsValueType(inner) {
			// Value-type optional: check .has_value
			if withParen {
				out.WriteString("(")
			}
			if isEq {
				out.WriteString("!")
			}
			g.genExpr(out, nonNullExpr)
			out.WriteString(".has_value")
			if withParen {
				out.WriteString(")")
			}
			return
		}
	}
	// Heap/struct optional: check == NULL or != NULL
	if withParen {
		out.WriteString("(")
	}
	g.genExpr(out, nonNullExpr)
	if isEq {
		out.WriteString(" == NULL")
	} else {
		out.WriteString(" != NULL")
	}
	if withParen {
		out.WriteString(")")
	}
}

// flattenStringConcat flattens a chain of BinaryExpr{BinAdd} nodes on strings
// into a list of leaf operands (left-to-right).
func (g *Generator) flattenStringConcat(expr ast.Expr) []ast.Expr {
	bin, ok := expr.(*ast.BinaryExpr)
	if !ok || bin.Op != ast.BinAdd || !(g.isStringExpr(bin.Left) || g.isStringExpr(bin.Right)) {
		return []ast.Expr{expr}
	}
	var result []ast.Expr
	result = append(result, g.flattenStringConcat(bin.Left)...)
	result = append(result, g.flattenStringConcat(bin.Right)...)
	return result
}

// genStringConcat generates a linearized string concatenation chain.
// It flattens nested BinAdd nodes, emits each operand that produces a +1 allocation
// into a temp variable (registered in pendingReleases), and chains dex_str_concat calls
// while releasing intermediates.
func (g *Generator) genStringConcat(out *strings.Builder, expr *ast.BinaryExpr) {
	operands := g.flattenStringConcat(expr)

	if len(operands) < 2 {
		// Shouldn't happen, but handle defensively
		g.genExpr(out, operands[0])
		return
	}

	// For a simple 2-operand concat, release +1 operand temps inline
	if len(operands) == 2 {
		leftNew := g.isNewAlloc(operands[0])
		rightNew := g.isNewAlloc(operands[1])
		if !leftNew && !rightNew {
			// Both borrowed — simple emit
			out.WriteString("dex_str_concat(")
			g.genExpr(out, operands[0])
			out.WriteString(", ")
			g.genExpr(out, operands[1])
			out.WriteString(")")
			return
		}
		// At least one operand is +1 — use statement-expression to release it
		out.WriteString("({ ")
		lTmp := g.nextTemp()
		out.WriteString(fmt.Sprintf("DexString* %s = ", lTmp))
		g.genExpr(out, operands[0])
		out.WriteString("; ")
		rTmp := g.nextTemp()
		out.WriteString(fmt.Sprintf("DexString* %s = ", rTmp))
		g.genExpr(out, operands[1])
		out.WriteString("; ")
		resTmp := g.nextTemp()
		out.WriteString(fmt.Sprintf("DexString* %s = dex_str_concat(%s, %s); ", resTmp, lTmp, rTmp))
		if leftNew {
			out.WriteString(fmt.Sprintf("dex_release(%s); ", lTmp))
		}
		if rightNew {
			out.WriteString(fmt.Sprintf("dex_release(%s); ", rTmp))
		}
		out.WriteString(fmt.Sprintf("%s; })", resTmp))
		return
	}

	// For 3+ operands, use StringBuilder for efficient single-allocation concatenation.
	g.usesStringBuilder = true
	g.usesString = true
	sbTmp := g.nextTemp()
	resTmp := g.nextTemp()
	out.WriteString(fmt.Sprintf("({ DexStringBuilder* %s = dex_sb_new(); ", sbTmp))

	// Append each operand and release +1 allocations
	operandVars := make([]string, len(operands))
	for i, op := range operands {
		tmpName := g.nextTemp()
		operandVars[i] = tmpName
		out.WriteString(fmt.Sprintf("DexString* %s = ", tmpName))
		g.genExpr(out, op)
		out.WriteString(fmt.Sprintf("; dex_sb_append_str(%s, %s); ", sbTmp, tmpName))
		if g.isNewAlloc(op) {
			out.WriteString(fmt.Sprintf("dex_release(%s); ", tmpName))
		}
	}

	out.WriteString(fmt.Sprintf("DexString* %s = dex_sb_toString(%s); dex_release(%s); %s; })", resTmp, sbTmp, sbTmp, resTmp))
}

// isStringExpr checks if an expression is known to produce a string type.
func (g *Generator) isStringExpr(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.StringLit:
		return true
	case *ast.CallExpr:
		// Polymorphic return type: uses ResolvedType if set
		if e.ResolvedType != 0 {
			return e.ResolvedType == ast.TypeString
		}
		// Map method calls: get() on a map with string values
		if e.Module != "" {
			if mapType, ok := g.mapVars[e.Module]; ok {
				if e.Name == "get" {
					return ast.MapValueType(mapType) == ast.TypeString
				}
				return false
			}
			// Field chain map access (e.g., self.myMap.get("key"))
			if strings.Contains(e.Module, ".") {
				chainType := g.resolveFieldChainType(e.Module)
				if ast.IsMapType(chainType) {
					if e.Name == "get" {
						return ast.MapValueType(chainType) == ast.TypeString
					}
					return false
				}
			}
		}
		// StringBuilder methods that return strings
		if e.Module != "" && g.sbVars[e.Module] {
			return e.Name == "toString"
		}
		// String methods that return strings
		if e.Module != "" && g.strVars[e.Module] {
			switch e.Name {
			case "toLower", "toUpper", "trim", "substring", "replace":
				return true
			}
			return false
		}
		// Check stdlib functions
		if e.Module != "" {
			funcDef, ok := stdlib.LookupFunc(e.Module, e.Name)
			if ok {
				return funcDef.ReturnType == ast.TypeString
			}
			return false
		}
		// User-defined functions
		if fn, ok := g.funcs[e.Name]; ok {
			return fn.ReturnType == ast.TypeString
		}
	case *ast.Ident:
		return g.strVars[e.Name]
	case *ast.IndexExpr:
		// Check if indexing a map with string values
		if ident, ok := e.Array.(*ast.Ident); ok {
			if mapType, ok := g.mapVars[ident.Name]; ok {
				return ast.MapValueType(mapType) == ast.TypeString
			}
			if arrType, ok := g.arrVars[ident.Name]; ok {
				return arrType == ast.TypeArrayString
			}
		}
		return false
	case *ast.BinaryExpr:
		if e.Op == ast.BinAdd {
			return g.isStringExpr(e.Left) || g.isStringExpr(e.Right)
		}
	case *ast.FieldAccessExpr:
		return g.typeOfExpr(e) == ast.TypeString
	}
	return false
}

// genPrintArray generates C code to print an array as [elem, elem, ...].
func (g *Generator) genPrintArray(out *strings.Builder, expr ast.Expr, arrType ast.Type, newline bool) {
	elemType := ast.ElementType(arrType)
	cnt := g.printCounter
	g.printCounter++
	iterVar := fmt.Sprintf("_pi%d", cnt)

	out.WriteString("do { ")

	// Store the array pointer in a temp variable
	arrVar := fmt.Sprintf("_pa%d", cnt)
	if ast.IsStructArrayType(arrType) {
		out.WriteString(fmt.Sprintf("DexArrayStruct* %s = (DexArrayStruct*)", arrVar))
	} else {
		out.WriteString(fmt.Sprintf("%s %s = ", g.cType(arrType), arrVar))
	}
	g.genExpr(out, expr)
	out.WriteString("; ")

	out.WriteString("printf(\"[\"); ")
	out.WriteString(fmt.Sprintf("for (int %s = 0; %s < %s->len; %s++) { ", iterVar, iterVar, arrVar, iterVar))
	out.WriteString(fmt.Sprintf("if (%s > 0) printf(\", \"); ", iterVar))

	// Generate print for each element
	elemExpr := fmt.Sprintf("%s->data[%s]", arrVar, iterVar)
	if ast.IsStructArrayType(arrType) {
		elemCType := g.cType(elemType)
		elemExpr = fmt.Sprintf("*(%s*)dex_array_struct_get(%s, %s)", elemCType, arrVar, iterVar)
	}
	g.genPrintElem(out, elemExpr, elemType)
	out.WriteString("; ")

	out.WriteString("} ")
	if newline {
		out.WriteString("printf(\"]\\n\"); ")
	} else {
		out.WriteString("printf(\"]\"); ")
	}
	out.WriteString("} while(0)")
}

// genPrintElem generates C code to print a single element given as a C expression string.
func (g *Generator) genPrintElem(out *strings.Builder, cExpr string, typ ast.Type) {
	if ast.IsArrayType(typ) {
		// Nested array: recurse
		elemType := ast.ElementType(typ)
		cnt := g.printCounter
		g.printCounter++
		iterVar := fmt.Sprintf("_pi%d", cnt)
		arrVar := fmt.Sprintf("_pa%d", cnt)

		if ast.IsStructArrayType(typ) {
			out.WriteString(fmt.Sprintf("{ DexArrayStruct* %s = (DexArrayStruct*)%s; ", arrVar, cExpr))
		} else {
			out.WriteString(fmt.Sprintf("{ %s %s = %s; ", g.cType(typ), arrVar, cExpr))
		}
		out.WriteString("printf(\"[\"); ")
		out.WriteString(fmt.Sprintf("for (int %s = 0; %s < %s->len; %s++) { ", iterVar, iterVar, arrVar, iterVar))
		out.WriteString(fmt.Sprintf("if (%s > 0) printf(\", \"); ", iterVar))

		innerExpr := fmt.Sprintf("%s->data[%s]", arrVar, iterVar)
		if ast.IsStructArrayType(typ) {
			innerCType := g.cType(elemType)
			innerExpr = fmt.Sprintf("*(%s*)dex_array_struct_get(%s, %s)", innerCType, arrVar, iterVar)
		}
		g.genPrintElem(out, innerExpr, elemType)
		out.WriteString("; ")

		out.WriteString("} printf(\"]\"); }")
		return
	}
	if ast.IsStructType(typ) {
		// Nested struct: recurse
		def := ast.GetStructDef(typ)
		if def == nil {
			out.WriteString(fmt.Sprintf("printf(\"%%d\", %s)", cExpr))
			return
		}
		cnt := g.printCounter
		g.printCounter++
		structVar := fmt.Sprintf("_ps%d", cnt)
		cType := g.cType(typ)
		out.WriteString(fmt.Sprintf("{ %s %s = %s; ", cType, structVar, cExpr))
		out.WriteString(fmt.Sprintf("printf(\"%s{\"); ", def.Name))
		for i, f := range def.Fields {
			if i > 0 {
				out.WriteString("printf(\", \"); ")
			}
			fieldExpr := fmt.Sprintf("%s.%s", structVar, f.Name)
			out.WriteString(fmt.Sprintf("printf(\"%s: \"); ", f.Name))
			g.genPrintElem(out, fieldExpr, f.Type)
			out.WriteString("; ")
		}
		out.WriteString("printf(\"}\"); }")
		return
	}
	// Primitive types
	switch typ {
	case ast.TypeChar:
		out.WriteString(fmt.Sprintf("printf(\"%%c\", %s)", cExpr))
	case ast.TypeInt:
		out.WriteString(fmt.Sprintf("printf(\"%%d\", %s)", cExpr))
	case ast.TypeLong:
		out.WriteString(fmt.Sprintf("printf(\"%%ld\", %s)", cExpr))
	case ast.TypeDouble:
		out.WriteString(fmt.Sprintf("printf(\"%%f\", %s)", cExpr))
	case ast.TypeString:
		out.WriteString(fmt.Sprintf("printf(\"%%s\", (%s) ? (%s)->data : \"\")", cExpr, cExpr))
	case ast.TypeBool:
		out.WriteString(fmt.Sprintf("printf(\"%%s\", (%s) ? \"true\" : \"false\")", cExpr))
	default:
		// enums and other integer-like types
		out.WriteString(fmt.Sprintf("printf(\"%%d\", %s)", cExpr))
	}
}

// genPrintStruct generates C code to print a struct as Name{field: value, ...}.
func (g *Generator) genPrintStruct(out *strings.Builder, expr ast.Expr, structType ast.Type, newline bool) {
	def := ast.GetStructDef(structType)
	if def == nil {
		out.WriteString("printf(\"<unknown struct>\")")
		return
	}
	cnt := g.printCounter
	g.printCounter++
	structVar := fmt.Sprintf("_ps%d", cnt)
	cType := g.cType(structType)

	out.WriteString("do { ")
	out.WriteString(fmt.Sprintf("%s %s = ", cType, structVar))
	g.genExpr(out, expr)
	out.WriteString("; ")

	out.WriteString(fmt.Sprintf("printf(\"%s{\"); ", def.Name))
	for i, f := range def.Fields {
		if i > 0 {
			out.WriteString("printf(\", \"); ")
		}
		fieldExpr := fmt.Sprintf("%s.%s", structVar, f.Name)
		out.WriteString(fmt.Sprintf("printf(\"%s: \"); ", f.Name))
		g.genPrintElem(out, fieldExpr, f.Type)
		out.WriteString("; ")
	}
	if newline {
		out.WriteString("printf(\"}\\n\"); ")
	} else {
		out.WriteString("printf(\"}\"); ")
	}
	out.WriteString("} while(0)")
}

// genZeroValue emits the C zero value for a DexLang type.
// int/long/char → 0, double → 0.0, bool → 0, string → dex_string_from_lit(""),
// structs → recursive zero-init, arrays/other pointers → NULL.
func (g *Generator) genZeroValue(out *strings.Builder, typ ast.Type) {
	switch typ {
	case ast.TypeInt, ast.TypeChar:
		out.WriteString("0")
	case ast.TypeLong:
		out.WriteString("0L")
	case ast.TypeDouble:
		out.WriteString("0.0")
	case ast.TypeBool:
		out.WriteString("0")
	case ast.TypeString:
		out.WriteString("dex_string_from_lit(\"\")")
	default:
		if ast.IsStructType(typ) {
			def := ast.GetStructDef(typ)
			if def == nil {
				out.WriteString("((" + g.cType(typ) + "){ 0 })")
				return
			}
			cName := "Dex_" + def.Name
			out.WriteString(fmt.Sprintf("(%s){ ", cName))
			for i, f := range def.Fields {
				if i > 0 {
					out.WriteString(", ")
				}
				out.WriteString(fmt.Sprintf(".%s = ", f.Name))
				g.genZeroValue(out, f.Type)
			}
			out.WriteString(" }")
			return
		}
		// Arrays, optionals, and other pointer types: NULL
		out.WriteString("NULL")
	}
}
