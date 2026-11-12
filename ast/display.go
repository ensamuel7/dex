package ast

func DisplayType(t Type) string {
	switch t {
	case TypeNull:
		return "null"
	case TypeInt:
		return "int"
	case TypeBool:
		return "bool"
	case TypeString:
		return "string"
	case TypeVoid:
		return "void"
	case TypeLong:
		return "long"
	case TypeDouble:
		return "double"
	case TypeArrayInt:
		return "int[]"
	case TypeArrayBool:
		return "bool[]"
	case TypeArrayString:
		return "string[]"
	case TypeArrayLong:
		return "long[]"
	case TypeArrayDouble:
		return "double[]"
	case TypeChar:
		return "char"
	case TypeArrayChar:
		return "char[]"
	case TypeStringBuilder:
		return "StringBuilder"
	case TypeJsonValue:
		return "json.Value"
	case TypeMutex:
		return "mutex"
	default:
		if IsRefType(t) {
			return "&" + DisplayType(RefInnerType(t))
		}
		if IsOptionalType(t) {
			return DisplayType(OptionalInnerType(t)) + "?"
		}
		if IsStructArrayType(t) {
			elemType := ElementType(t)
			if IsEnumType(elemType) {
				return DisplayStructName(EnumName(elemType)) + "[]"
			}
			return DisplayStructName(StructName(elemType)) + "[]"
		}
		if IsStructType(t) {
			return DisplayStructName(StructName(t))
		}
		if IsChanType(t) {
			return "chan " + DisplayType(ChanElemType(t))
		}
		if IsTaskType(t) {
			return "task " + DisplayType(TaskReturnType(t))
		}
		if IsFuncType(t) {
			params := FuncTypeParams(t)
			ret := FuncTypeReturn(t)
			s := "fn("
			for i, p := range params {
				if i > 0 {
					s += ", "
				}
				s += DisplayType(p)
			}
			s += "): " + DisplayType(ret)
			return s
		}
		if IsWeakType(t) {
			return "weak<" + DisplayType(WeakInnerType(t)) + ">"
		}
		if IsEnumType(t) {
			return DisplayStructName(EnumName(t))
		}
		if IsMapType(t) {
			return "map[" + DisplayType(MapKeyType(t)) + ", " + DisplayType(MapValueType(t)) + "]"
		}
		if IsInterfaceType(t) {
			return DisplayStructName(InterfaceName(t))
		}
		return "unknown"
	}
}
