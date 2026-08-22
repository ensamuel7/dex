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
		return "0"
	}
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
