package ast

import (
	"runtime"
	"sort"
)

type FieldLayout struct {
	Name   string
	Offset int64
	Size   int64
	Align  int64
}

type StructLayout struct {
	Size    int64
	Align   int64
	Padding int64
	Fields  []FieldLayout
	Packed  int64
	Order   []string
}

func mutexLayout() (int64, int64) {
	if runtime.GOOS == "darwin" {
		return 64, 8
	}
	return 40, 8
}

func TypeLayout(t Type) (size, align int64) {
	switch t {
	case TypeInt:
		return 4, 4
	case TypeBool, TypeChar:
		return 1, 1
	case TypeLong, TypeDouble:
		return 8, 8
	case TypeMutex:
		return mutexLayout()
	}
	if IsOptionalType(t) {
		inner := OptionalInnerType(t)
		if IsEnumType(inner) && !IsPayloadEnumType(inner) {
			return 4, 4
		}
		if IsValueType(inner) {
			s, a := TypeLayout(inner)
			return alignUp(a+s, a), a
		}
		return 8, 8
	}
	if IsStructType(t) && !IsStructArrayType(t) {
		l := LayoutOf(t)
		return l.Size, l.Align
	}
	if IsInterfaceType(t) {
		n := int64(0)
		if def := GetInterfaceDef(t); def != nil {
			n = int64(len(def.Methods))
		}
		return 16 + 8*n, 8
	}
	if IsEnumType(t) && !IsPayloadEnumType(t) {
		return 4, 4
	}
	return 8, 8
}

func alignUp(n, a int64) int64 {
	if a <= 1 {
		return n
	}
	return (n + a - 1) / a * a
}

func LayoutOf(t Type) StructLayout {
	def := GetStructDef(t)
	if def == nil {
		return StructLayout{Align: 1}
	}
	return LayoutFields(def.Fields)
}

func LayoutFields(fields []StructField) StructLayout {
	var l StructLayout
	l.Align = 1
	var used int64
	for _, f := range fields {
		s, a := TypeLayout(f.Type)
		l.Size = alignUp(l.Size, a)
		l.Fields = append(l.Fields, FieldLayout{Name: f.Name, Offset: l.Size, Size: s, Align: a})
		l.Size += s
		used += s
		if a > l.Align {
			l.Align = a
		}
	}
	l.Size = alignUp(l.Size, l.Align)
	l.Padding = l.Size - used

	sorted := append([]FieldLayout(nil), l.Fields...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Align > sorted[j].Align })
	var packed int64
	for _, f := range sorted {
		packed = alignUp(packed, f.Align) + f.Size
		l.Order = append(l.Order, f.Name)
	}
	l.Packed = alignUp(packed, l.Align)
	return l
}
