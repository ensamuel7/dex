package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

// genJsonCall emits the json module's calls, which are polymorphic in ways a
// plain stdlib signature cannot express. Reports whether it handled the call.
func (g *Generator) genJsonCall(out *strings.Builder, e *ast.CallExpr) bool {
	// json.encode(value) — special codegen (returns const char*, needs wrapping)
	if e.Module == "json" && e.Name == "encode" {
		g.genJsonEncodeToString(out, e.Args[0], g.typeOfExpr(e.Args[0]))
		return true
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
				return true
			}
			// The parser only reads the text, so a source string that was just
			// built — json.decode(json.encode(v)) — is released after the
			// statement rather than being left behind.
			out.WriteString("dex_jv_parse_str(")
			g.genBorrowed(out, e.Args[0])
			out.WriteString(")")
			return true
		}
		// A json.Value source decoding into a struct is re-serialized so the one
		// struct decoder handles it, rather than a parallel tree-walking path.
		if g.typeOfExpr(e.Args[0]) == ast.TypeJsonValue {
			src := &ast.CallExpr{Module: "json", Name: "encode", Args: []ast.Expr{e.Args[0]}, Pos: e.Pos}
			forwarded := &ast.CallExpr{Module: "json", Name: "decode", Args: []ast.Expr{src}, ResolvedType: e.ResolvedType, Pos: e.Pos}
			g.jsonDecodeFromString(out, forwarded)
			return true
		}
		g.jsonDecodeFromString(out, e)
		return true
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
		return true
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
		return true
	}

	// json.arrayNew() — returns const char*, wrap
	if e.Module == "json" && e.Name == "arrayNew" {
		out.WriteString("dex_string_from_cstr(dex_json_array_new())")
		return true
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
			return true
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
			return true
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
		return true
	}

	// json.new() — returns const char*, wrap
	if e.Module == "json" && e.Name == "new" {
		out.WriteString("dex_string_from_cstr(dex_json_new())")
		return true
	}

	// db.col(rows, col) — polymorphic: dispatch by resolved return type
	return false
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
