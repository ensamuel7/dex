package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/stdlib"
)

func (g *Generator) cType(t ast.Type) string {
	switch t {
	case ast.TypeInt:
		return "int"
	case ast.TypeBool:
		return "_Bool"
	case ast.TypeString:
		return "DexString*"
	case ast.TypeLong:
		return "long"
	case ast.TypeDouble:
		return "double"
	case ast.TypeArrayInt:
		return "DexArrayInt*"
	case ast.TypeArrayBool:
		return "DexArrayBool*"
	case ast.TypeArrayString:
		return "DexArrayString*"
	case ast.TypeArrayLong:
		return "DexArrayLong*"
	case ast.TypeArrayDouble:
		return "DexArrayDouble*"
	case ast.TypeChar:
		return "unsigned char"
	case ast.TypeArrayChar:
		return "DexArrayChar*"
	case ast.TypeStringBuilder:
		return "DexStringBuilder*"
	case ast.TypeMutex:
		return "pthread_mutex_t"
	case ast.TypeVoid:
		return "void"
	default:
		if ast.IsOptionalType(t) {
			inner := ast.OptionalInnerType(t)
			if ast.IsValueType(inner) {
				switch inner {
				case ast.TypeInt:
					return "DexOptInt"
				case ast.TypeBool:
					return "DexOptBool"
				case ast.TypeLong:
					return "DexOptLong"
				case ast.TypeDouble:
					return "DexOptDouble"
				case ast.TypeChar:
					return "DexOptChar"
				}
			}
			if ast.IsStructType(inner) {
				return "Dex_" + ast.StructName(inner) + "*"
			}
			// Heap types (string, arrays, channels, etc.) — same pointer type, NULL = absent
			return g.cType(inner)
		}
		if ast.IsStructArrayType(t) {
			return "DexArrayStruct*"
		}
		if ast.IsStructType(t) {
			return "Dex_" + ast.StructName(t)
		}
		if ast.IsChanType(t) || ast.IsTaskType(t) {
			return "DexChan*"
		}
		if ast.IsFuncType(t) {
			return g.funcTypedef(t)
		}
		if ast.IsWeakType(t) {
			return "DexWeakRef*"
		}
		if ast.IsRefType(t) {
			inner := ast.RefInnerType(t)
			switch inner {
			case ast.TypeInt:
				return "int*"
			case ast.TypeLong:
				return "long*"
			case ast.TypeDouble:
				return "double*"
			case ast.TypeBool:
				return "_Bool*"
			case ast.TypeChar:
				return "unsigned char*"
			}
			if ast.IsStructType(inner) {
				return "Dex_" + ast.StructName(inner) + "*"
			}
		}
		if ast.IsEnumType(t) {
			return "Dex_" + ast.EnumName(t)
		}
		if ast.IsInterfaceType(t) {
			return "Dex_" + ast.InterfaceName(t)
		}
		if ast.IsMapType(t) {
			return "DexMap_" + g.mapSuffix(t) + "*"
		}
		return "void"
	}
}

// funcTypedef returns (and lazily registers) a typedef name for a function pointer type.
func (g *Generator) funcTypedef(t ast.Type) string {
	if name, ok := g.funcTypedefs[t]; ok {
		return name
	}
	g.funcTypedefCnt++
	name := fmt.Sprintf("DexFn_%d", g.funcTypedefCnt)
	g.funcTypedefs[t] = name
	return name
}

func (g *Generator) arrayNewFunc(t ast.Type) string {
	switch t {
	case ast.TypeArrayInt:
		return "dex_array_int_new"
	case ast.TypeArrayBool:
		return "dex_array_bool_new"
	case ast.TypeArrayString:
		return "dex_array_string_new"
	case ast.TypeArrayLong:
		return "dex_array_long_new"
	case ast.TypeArrayDouble:
		return "dex_array_double_new"
	case ast.TypeArrayChar:
		return "dex_array_char_new"
	default:
		return ""
	}
}

func (g *Generator) arrayPushFunc(t ast.Type) string {
	switch t {
	case ast.TypeArrayInt:
		return "dex_array_int_push"
	case ast.TypeArrayBool:
		return "dex_array_bool_push"
	case ast.TypeArrayString:
		return "dex_array_string_push"
	case ast.TypeArrayLong:
		return "dex_array_long_push"
	case ast.TypeArrayDouble:
		return "dex_array_double_push"
	case ast.TypeArrayChar:
		return "dex_array_char_push"
	default:
		return ""
	}
}

func (g *Generator) arrayPopFunc(t ast.Type) string {
	switch t {
	case ast.TypeArrayInt:
		return "dex_array_int_pop"
	case ast.TypeArrayBool:
		return "dex_array_bool_pop"
	case ast.TypeArrayString:
		return "dex_array_string_pop"
	case ast.TypeArrayLong:
		return "dex_array_long_pop"
	case ast.TypeArrayDouble:
		return "dex_array_double_pop"
	case ast.TypeArrayChar:
		return "dex_array_char_pop"
	default:
		return ""
	}
}

func (g *Generator) arrayRemoveFunc(t ast.Type) string {
	switch t {
	case ast.TypeArrayInt:
		return "dex_array_int_remove"
	case ast.TypeArrayBool:
		return "dex_array_bool_remove"
	case ast.TypeArrayString:
		return "dex_array_string_remove"
	case ast.TypeArrayLong:
		return "dex_array_long_remove"
	case ast.TypeArrayDouble:
		return "dex_array_double_remove"
	case ast.TypeArrayChar:
		return "dex_array_char_remove"
	default:
		return ""
	}
}

func (g *Generator) arrayContainsFunc(t ast.Type) string {
	switch t {
	case ast.TypeArrayInt:
		return "dex_array_int_contains"
	case ast.TypeArrayBool:
		return "dex_array_bool_contains"
	case ast.TypeArrayString:
		return "dex_array_string_contains"
	case ast.TypeArrayLong:
		return "dex_array_long_contains"
	case ast.TypeArrayDouble:
		return "dex_array_double_contains"
	case ast.TypeArrayChar:
		return "dex_array_char_contains"
	default:
		return ""
	}
}

func (g *Generator) arrayIndexOfFunc(t ast.Type) string {
	switch t {
	case ast.TypeArrayInt:
		return "dex_array_int_indexOf"
	case ast.TypeArrayBool:
		return "dex_array_bool_indexOf"
	case ast.TypeArrayString:
		return "dex_array_string_indexOf"
	case ast.TypeArrayLong:
		return "dex_array_long_indexOf"
	case ast.TypeArrayDouble:
		return "dex_array_double_indexOf"
	case ast.TypeArrayChar:
		return "dex_array_char_indexOf"
	default:
		return ""
	}
}

func (g *Generator) arrayReverseFunc(t ast.Type) string {
	switch t {
	case ast.TypeArrayInt:
		return "dex_array_int_reverse"
	case ast.TypeArrayBool:
		return "dex_array_bool_reverse"
	case ast.TypeArrayString:
		return "dex_array_string_reverse"
	case ast.TypeArrayLong:
		return "dex_array_long_reverse"
	case ast.TypeArrayDouble:
		return "dex_array_double_reverse"
	case ast.TypeArrayChar:
		return "dex_array_char_reverse"
	default:
		return ""
	}
}

func (g *Generator) arraySortAscFunc(t ast.Type) string {
	switch t {
	case ast.TypeArrayInt:
		return "dex_array_int_sort_asc"
	case ast.TypeArrayString:
		return "dex_array_string_sort_asc"
	case ast.TypeArrayLong:
		return "dex_array_long_sort_asc"
	case ast.TypeArrayDouble:
		return "dex_array_double_sort_asc"
	case ast.TypeArrayChar:
		return "dex_array_char_sort_asc"
	default:
		return ""
	}
}

func (g *Generator) arraySortDescFunc(t ast.Type) string {
	switch t {
	case ast.TypeArrayInt:
		return "dex_array_int_sort_desc"
	case ast.TypeArrayString:
		return "dex_array_string_sort_desc"
	case ast.TypeArrayLong:
		return "dex_array_long_sort_desc"
	case ast.TypeArrayDouble:
		return "dex_array_double_sort_desc"
	case ast.TypeArrayChar:
		return "dex_array_char_sort_desc"
	default:
		return ""
	}
}

// structArrayCleanupFunc returns the cleanup function name for a struct type, or "NULL" if no cleanup needed.
func (g *Generator) structArrayCleanupFunc(elemType ast.Type) string {
	def := ast.GetStructDef(elemType)
	if def == nil {
		return "NULL"
	}
	for _, f := range def.Fields {
		if ast.NeedsRelease(f.Type) {
			return "dex_cleanup_" + def.Name
		}
	}
	return "NULL"
}

// mapSuffix returns the suffix for map C functions (e.g. "str_int")
func (g *Generator) mapSuffix(t ast.Type) string {
	return g.mapKeySuffix(ast.MapKeyType(t)) + "_" + g.mapValSuffix(ast.MapValueType(t))
}

func (g *Generator) mapKeySuffix(t ast.Type) string {
	switch t {
	case ast.TypeString:
		return "str"
	case ast.TypeInt:
		return "int"
	default:
		return "int"
	}
}

func (g *Generator) mapValSuffix(t ast.Type) string {
	switch t {
	case ast.TypeInt:
		return "int"
	case ast.TypeBool:
		return "bool"
	case ast.TypeString:
		return "str"
	case ast.TypeLong:
		return "long"
	case ast.TypeDouble:
		return "double"
	case ast.TypeChar:
		return "char"
	default:
		return "int"
	}
}

// typeOfExpr returns the type of an expression based on available information.
func (g *Generator) typeOfExpr(expr ast.Expr) ast.Type {
	switch e := expr.(type) {
	case *ast.NullLit:
		return ast.TypeNull
	case *ast.CharLit:
		return ast.TypeChar
	case *ast.IntLit:
		return ast.TypeInt
	case *ast.FloatLit:
		return ast.TypeDouble
	case *ast.BoolLit:
		return ast.TypeBool
	case *ast.StringLit:
		return ast.TypeString
	case *ast.Ident:
		// A narrowed optional reads as its inner type inside the guarded block
		if t, ok := g.narrowedTypes[e.Name]; ok {
			return t
		}
		if t, ok := g.varTypes[e.Name]; ok {
			return t
		}
		// Check if it's a function reference
		if fn, ok := g.funcs[e.Name]; ok {
			var paramTypes []ast.Type
			for _, p := range fn.Params {
				paramTypes = append(paramTypes, p.Type)
			}
			return ast.FuncTypeOf(paramTypes, fn.ReturnType)
		}
	case *ast.CallExpr:
		// Map method calls
		if e.Module != "" {
			if mapType, ok := g.mapVars[e.Module]; ok {
				switch e.Name {
				case "get":
					return ast.MapValueType(mapType)
				case "has":
					return ast.TypeBool
				case "len":
					return ast.TypeInt
				case "set", "remove", "clear":
					return ast.TypeVoid
				case "keys":
					return ast.ArrayTypeOf(ast.MapKeyType(mapType))
				case "values":
					return ast.ArrayTypeOf(ast.MapValueType(mapType))
				}
			}
			// Check field chain for map method calls (e.g., self.myMap.get("key"))
			if strings.Contains(e.Module, ".") {
				chainType := g.resolveFieldChainType(e.Module)
				if ast.IsMapType(chainType) {
					switch e.Name {
					case "get":
						return ast.MapValueType(chainType)
					case "has":
						return ast.TypeBool
					case "len":
						return ast.TypeInt
					case "set", "remove", "clear":
						return ast.TypeVoid
					case "keys":
						return ast.ArrayTypeOf(ast.MapKeyType(chainType))
					case "values":
						return ast.ArrayTypeOf(ast.MapValueType(chainType))
					}
				}
			}
		}
		// StringBuilder method calls
		if e.Module != "" && g.sbVars[e.Module] {
			switch e.Name {
			case "len":
				return ast.TypeInt
			case "toString":
				return ast.TypeString
			case "append", "clear":
				return ast.TypeVoid
			}
		}
		// String method calls
		// Array method calls, on a variable or a struct field chain
		if e.Module != "" {
			arrType, isArr := g.arrVars[e.Module]
			if !isArr && strings.Contains(e.Module, ".") {
				chainType := g.resolveFieldChainType(e.Module)
				if ast.IsArrayType(chainType) {
					arrType, isArr = chainType, true
				}
			}
			if isArr {
				switch e.Name {
				case "len", "indexOf":
					return ast.TypeInt
				case "contains":
					return ast.TypeBool
				case "pop":
					return ast.ElementType(arrType)
				case "push", "remove", "reverse", "sort":
					return ast.TypeVoid
				}
			}
		}
		if e.Module != "" && g.strVars[e.Module] {
			switch e.Name {
			case "len", "indexOf":
				return ast.TypeInt
			case "contains", "startsWith", "endsWith":
				return ast.TypeBool
			case "toLower", "toUpper", "trim", "substring", "replace":
				return ast.TypeString
			case "split":
				return ast.TypeArrayString
			case "charAt":
				return ast.TypeChar
			}
		}
		// Constructor call returns the struct type
		if e.IsConstructor {
			return e.StructType
		}
		// Method call: look up flattened function return type
		if e.IsMethodCall {
			structDef := ast.GetStructDef(e.StructType)
			if structDef != nil {
				flatName := structDef.Name + "_" + e.Name
				if modName, ok := g.structModules[structDef.Name]; ok {
					flatName = modName + "_" + flatName
				}
				if fn, ok := g.funcs[flatName]; ok {
					return fn.ReturnType
				}
			}
		}
		// Polymorphic return type: db.col and http client functions use ResolvedType
		if e.ResolvedType != 0 {
			return e.ResolvedType
		}
		if e.Module != "" && g.userModules[e.Module] {
			if fn, ok := g.funcs[e.Module+"_"+e.Name]; ok {
				return fn.ReturnType
			}
		}
		if e.Module != "" {
			funcDef, ok := stdlib.LookupFunc(e.Module, e.Name)
			if ok {
				return funcDef.ReturnType
			}
		}
		if fn, ok := g.funcs[e.Name]; ok {
			return fn.ReturnType
		}
		// Check if calling through a function-typed variable
		if t, ok := g.varTypes[e.Name]; ok && ast.IsFuncType(t) {
			return ast.FuncTypeReturn(t)
		}
	case *ast.BinaryExpr:
		if e.Op == ast.BinAdd && (g.isStringExpr(e.Left) || g.isStringExpr(e.Right)) {
			return ast.TypeString
		}
		// Comparison and logical operators return bool
		switch e.Op {
		case ast.BinEq, ast.BinNeq, ast.BinStrictEq, ast.BinStrictNeq,
			ast.BinLt, ast.BinGt, ast.BinLte, ast.BinGte,
			ast.BinAnd, ast.BinOr:
			return ast.TypeBool
		}
		return g.typeOfExpr(e.Left)
	case *ast.UnaryExpr:
		return g.typeOfExpr(e.Operand)
	case *ast.IndexExpr:
		if ident, ok := e.Array.(*ast.Ident); ok {
			if mapType, ok := g.mapVars[ident.Name]; ok {
				return ast.MapValueType(mapType)
			}
			if arrType, ok := g.arrVars[ident.Name]; ok {
				return ast.ElementType(arrType)
			}
		}
	case *ast.SliceExpr:
		if ident, ok := e.Array.(*ast.Ident); ok {
			if arrType, ok := g.arrVars[ident.Name]; ok {
				return arrType // slice returns same array type
			}
		}
	case *ast.StructLitExpr:
		if t, ok := ast.LookupStructType(e.Name); ok {
			return t
		}
	case *ast.FieldAccessExpr:
		objType := g.typeOfExpr(e.Object)
		// Unwrap ref type for field access
		if ast.IsRefType(objType) {
			objType = ast.RefInnerType(objType)
		}
		// Unwrap optional struct — narrowed access reads the inner struct's fields
		if ast.IsOptionalType(objType) && ast.IsStructType(ast.OptionalInnerType(objType)) {
			objType = ast.OptionalInnerType(objType)
		}
		if ast.IsStructType(objType) {
			def := ast.GetStructDef(objType)
			if def != nil {
				for _, f := range def.Fields {
					if f.Name == e.Field {
						return f.Type
					}
				}
			}
		}
	case *ast.SpawnExpr:
		return ast.TaskTypeOf(e.ReturnType)
	case *ast.ChannelExpr:
		return ast.ChanTypeOf(e.ElemType)
	case *ast.ReceiveExpr:
		srcType := g.typeOfExpr(e.Source)
		if ast.IsChanType(srcType) {
			return ast.ChanElemType(srcType)
		}
		if ast.IsTaskType(srcType) {
			return ast.TaskReturnType(srcType)
		}
	case *ast.EnumAccessExpr:
		return e.EnumType
	case *ast.MapLitExpr:
		return e.MapType
	case *ast.StringInterpExpr:
		return ast.TypeString
	case *ast.MatchExpr:
		return e.Type
	case *ast.LambdaExpr:
		var paramTypes []ast.Type
		for _, p := range e.Params {
			paramTypes = append(paramTypes, p.Type)
		}
		return ast.FuncTypeOf(paramTypes, e.ReturnType)
	}
	return ast.TypeVoid
}

// widerNumericType returns the wider of two numeric types.
// Widening order: int -> long -> double
func (g *Generator) widerNumericType(a, b ast.Type) ast.Type {
	rank := map[ast.Type]int{
		ast.TypeChar:   0,
		ast.TypeInt:    1,
		ast.TypeLong:   2,
		ast.TypeDouble: 3,
	}
	if rank[a] >= rank[b] {
		return a
	}
	return b
}

func (g *Generator) nonzeroCheckFunc(t ast.Type) string {
	switch t {
	case ast.TypeLong:
		return "dex_check_nonzero_long"
	case ast.TypeDouble:
		return "dex_check_nonzero_double"
	default:
		return "dex_check_nonzero_int"
	}
}

// jsonFieldKind returns the DexStructFieldDesc kind value for a DexLang type.
func (g *Generator) jsonFieldKind(t ast.Type) string {
	switch t {
	case ast.TypeInt:
		return "0"
	case ast.TypeBool:
		return "1"
	case ast.TypeString:
		return "2"
	case ast.TypeLong:
		return "3"
	case ast.TypeDouble:
		return "4"
	default:
		if ast.IsStructType(t) {
			return "5" // nested struct — carries its own descriptor array
		}
		return "0"
	}
}

// maxJSONStructDepth bounds nested struct descriptor emission. A struct cannot
// contain itself by value in C, so this only guards against pathological input.
const maxJSONStructDepth = 32

// genStructFieldDescs emits a C compound literal of DexStructFieldDesc describing
// every field of structType. Nested struct fields are emitted as kind 5 carrying
// their own descriptor array, so dex_json_encode_struct/dex_json_decode_struct
// can recurse into them instead of treating them as ints.
func (g *Generator) genStructFieldDescs(out *strings.Builder, structType ast.Type, depth int) {
	def := ast.GetStructDef(structType)
	cType := g.cType(structType)
	out.WriteString("(DexStructFieldDesc[]){ ")
	for i, f := range def.Fields {
		if i > 0 {
			out.WriteString(", ")
		}
		fieldOffset := fmt.Sprintf("offsetof(%s, %s)", cType, f.Name)
		if ast.IsStructType(f.Type) && depth < maxJSONStructDepth {
			sub := ast.GetStructDef(f.Type)
			out.WriteString(fmt.Sprintf("{\"%s\", %s, 5, %d, ", f.Name, fieldOffset, len(sub.Fields)))
			g.genStructFieldDescs(out, f.Type, depth+1)
			out.WriteString(", NULL, NULL}")
			continue
		}
		if encFn, decFn, ok := jsonArrayCodecNames(f.Type); ok {
			out.WriteString(fmt.Sprintf("{\"%s\", %s, 6, 0, NULL, %s, %s}", f.Name, fieldOffset, encFn, decFn))
			continue
		}
		if ast.IsStructArrayType(f.Type) && depth < maxJSONStructDepth {
			encFn, decFn := jsonStructArrayCodecNames(f.Type)
			out.WriteString(fmt.Sprintf("{\"%s\", %s, 6, 0, NULL, %s, %s}", f.Name, fieldOffset, encFn, decFn))
			continue
		}
		if ast.IsArrayType(f.Type) {
			// No codec available — kind 7 is skipped by encode and decode rather
			// than being misread as an int.
			out.WriteString(fmt.Sprintf("{\"%s\", %s, 7, 0, NULL, NULL, NULL}", f.Name, fieldOffset))
			continue
		}
		out.WriteString(fmt.Sprintf("{\"%s\", %s, %s, 0, NULL, NULL, NULL}", f.Name, fieldOffset, g.jsonFieldKind(f.Type)))
	}
	out.WriteString(" }")
}

func (g *Generator) cBinOp(op ast.BinOp) string {
	switch op {
	case ast.BinAdd:
		return "+"
	case ast.BinSub:
		return "-"
	case ast.BinMul:
		return "*"
	case ast.BinDiv:
		return "/"
	case ast.BinMod:
		return "%"
	case ast.BinEq:
		return "=="
	case ast.BinNeq:
		return "!="
	case ast.BinStrictEq:
		return "=="
	case ast.BinStrictNeq:
		return "!="
	case ast.BinLt:
		return "<"
	case ast.BinGt:
		return ">"
	case ast.BinLte:
		return "<="
	case ast.BinGte:
		return ">="
	case ast.BinAnd:
		return "&&"
	case ast.BinOr:
		return "||"
	default:
		return "?"
	}
}

// arrayCodecSpec describes the JSON codec pair emitted for one primitive array
// element type.
type arrayCodecSpec struct {
	suffix    string // dex_array_<suffix>_new / _push
	stringify string // dex_json_stringify_<stringify>
	elemCType string
	parse     string // expression converting the element string `_el` to elemCType
}

var arrayCodecSpecs = map[ast.Type]arrayCodecSpec{
	ast.TypeArrayInt:    {"int", "int", "int", "atoi(_el)"},
	ast.TypeArrayLong:   {"long", "long", "long", "atol(_el)"},
	ast.TypeArrayDouble: {"double", "double", "double", "atof(_el)"},
	ast.TypeArrayBool:   {"bool", "bool", "_Bool", "(strcmp(_el, \"true\") == 0)"},
	ast.TypeArrayChar:   {"char", "char", "unsigned char", "(unsigned char)_el[0]"},
	ast.TypeArrayString: {"string", "str", "DexString*", ""},
}

// jsonArrayCodecNames returns the encode/decode function names for an array
// field type, and whether a codec exists for it. Struct arrays have none.
func jsonArrayCodecNames(t ast.Type) (string, string, bool) {
	spec, ok := arrayCodecSpecs[t]
	if !ok {
		return "", "", false
	}
	return "dex_jarr_enc_" + spec.suffix, "dex_jarr_dec_" + spec.suffix, true
}

// jsonStructArrayCodecNames returns the encode/decode function names for a
// struct-array field type.
func jsonStructArrayCodecNames(t ast.Type) (string, string) {
	name := ast.StructName(ast.ElementType(t))
	return "dex_jarr_enc_s_" + name, "dex_jarr_dec_s_" + name
}

// emitArrayFieldCodecs emits one encode/decode pair per array element type that
// appears as a struct field anywhere in the program, for both primitive element
// types and struct element types.
func (g *Generator) emitArrayFieldCodecs(out *strings.Builder, program *ast.Program) {
	// These bridge the array runtime and the json module runtime, so they are
	// only valid when the json module is actually imported.
	if _, ok := g.importedModules["json"]; !ok {
		return
	}
	needed := map[ast.Type]bool{}
	var structArrs []ast.Type
	seenStructArr := map[ast.Type]bool{}
	for _, sd := range program.Structs {
		for _, f := range sd.Fields {
			if _, ok := arrayCodecSpecs[f.Type]; ok {
				needed[f.Type] = true
			}
			if ast.IsStructArrayType(f.Type) && !seenStructArr[f.Type] {
				seenStructArr[f.Type] = true
				structArrs = append(structArrs, f.Type)
			}
		}
	}
	for _, t := range structArrs {
		elemType := ast.ElementType(t)
		elemCType := g.cType(elemType)
		def := ast.GetStructDef(elemType)
		encName, decName := jsonStructArrayCodecNames(t)
		cleanupFn := g.structArrayCleanupFunc(elemType)

		out.WriteString(fmt.Sprintf("static const char* %s(void* _field) {\n", encName))
		out.WriteString("    DexArrayStruct* _a = *(DexArrayStruct**)_field;\n")
		out.WriteString("    if (!_a) return NULL;\n")
		out.WriteString(fmt.Sprintf("    return dex_json_stringify_struct_arr(_a, sizeof(%s), %d, ", elemCType, len(def.Fields)))
		g.genStructFieldDescs(out, elemType, 0)
		out.WriteString(");\n}\n")

		out.WriteString(fmt.Sprintf("static void %s(const char* _json, void* _field) {\n", decName))
		out.WriteString(fmt.Sprintf("    DexArrayStruct* _a = dex_array_struct_new(sizeof(%s), %s);\n", elemCType, cleanupFn))
		out.WriteString("    int _n = dex_json_array_len(_json);\n")
		out.WriteString("    for (int _i = 0; _i < _n; _i++) {\n")
		out.WriteString("        const char* _el = dex_json_array_get_raw(_json, _i);\n")
		out.WriteString(fmt.Sprintf("        %s _t; memset(&_t, 0, sizeof(%s));\n", elemCType, elemCType))
		out.WriteString(fmt.Sprintf("        dex_json_decode_struct(_el, &_t, %d, ", len(def.Fields)))
		g.genStructFieldDescs(out, elemType, 0)
		out.WriteString(");\n")
		out.WriteString("        dex_array_struct_push(_a, &_t);\n")
		out.WriteString("        free((void*)_el);\n")
		out.WriteString("    }\n")
		out.WriteString("    *(DexArrayStruct**)_field = _a;\n}\n")
	}
	if len(needed) == 0 {
		return
	}
	// Deterministic order so output is reproducible.
	order := []ast.Type{ast.TypeArrayInt, ast.TypeArrayLong, ast.TypeArrayDouble,
		ast.TypeArrayBool, ast.TypeArrayChar, ast.TypeArrayString}
	for _, t := range order {
		if !needed[t] {
			continue
		}
		spec := arrayCodecSpecs[t]
		encName, decName, _ := jsonArrayCodecNames(t)
		arrCType := g.cType(t)

		out.WriteString(fmt.Sprintf("static const char* %s(void* _field) {\n", encName))
		out.WriteString(fmt.Sprintf("    %s _a = *(%s*)_field;\n", arrCType, arrCType))
		out.WriteString("    if (!_a) return NULL;\n")
		out.WriteString(fmt.Sprintf("    return dex_json_stringify_%s(_a);\n", spec.stringify))
		out.WriteString("}\n")

		out.WriteString(fmt.Sprintf("static void %s(const char* _json, void* _field) {\n", decName))
		out.WriteString(fmt.Sprintf("    %s _a = dex_array_%s_new();\n", arrCType, spec.suffix))
		out.WriteString("    int _n = dex_json_array_len(_json);\n")
		out.WriteString("    for (int _i = 0; _i < _n; _i++) {\n")
		out.WriteString("        const char* _el = dex_json_array_get(_json, _i);\n")
		if t == ast.TypeArrayString {
			// dex_string_from_cstr takes ownership of _el, so it must not be freed here.
			out.WriteString(fmt.Sprintf("        dex_array_%s_push(_a, dex_string_from_cstr(_el));\n", spec.suffix))
		} else {
			out.WriteString(fmt.Sprintf("        dex_array_%s_push(_a, %s);\n", spec.suffix, spec.parse))
			out.WriteString("        free((void*)_el);\n")
		}
		out.WriteString("    }\n")
		out.WriteString(fmt.Sprintf("    *(%s*)_field = _a;\n", arrCType))
		out.WriteString("}\n")
	}
}
