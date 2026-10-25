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
	Size     int64
	Align    int64
	Padding  int64
	Declared int64
	Fields   []FieldLayout
	Order    []string
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
	if def.DeclaredOrder {
		l := place(def.Fields)
		l.Declared = l.Size
		for _, f := range l.Fields {
			l.Order = append(l.Order, f.Name)
		}
		return l
	}
	return LayoutFields(def.Fields)
}

func LayoutFields(fields []StructField) StructLayout {
	l := place(LaidOutFields(fields))
	l.Declared = place(fields).Size
	for _, f := range l.Fields {
		l.Order = append(l.Order, f.Name)
	}
	return l
}

func place(fields []StructField) StructLayout {
	l := StructLayout{Align: 1}
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
	return l
}

func LaidOutFields(fields []StructField) []StructField {
	out := append([]StructField(nil), fields...)
	sort.SliceStable(out, func(i, j int) bool {
		_, ai := TypeLayout(out[i].Type)
		_, aj := TypeLayout(out[j].Type)
		return ai > aj
	})
	return out
}
