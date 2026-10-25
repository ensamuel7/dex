package ast

import (
	"reflect"
	"testing"
)

func TestLayoutMatchesCStructRules(t *testing.T) {
	inner := RegisterStructType(StructDef{Name: "LayoutInner", Fields: []StructField{
		{Name: "flag", Type: TypeBool},
		{Name: "count", Type: TypeLong},
	}})
	l := LayoutOf(inner)
	if l.Size != 16 || l.Align != 8 || l.Padding != 7 || l.Packed != 16 {
		t.Fatalf("inner layout %+v", l)
	}

	l = LayoutFields([]StructField{
		{Name: "a", Type: TypeBool},
		{Name: "b", Type: TypeLong},
		{Name: "c", Type: TypeBool},
		{Name: "d", Type: TypeInt},
		{Name: "e", Type: inner},
		{Name: "f", Type: TypeString},
	})
	if l.Size != 48 || l.Packed != 40 {
		t.Fatalf("size %d packed %d, want 48 and 40", l.Size, l.Packed)
	}
	if want := []string{"b", "e", "f", "d", "a", "c"}; !reflect.DeepEqual(l.Order, want) {
		t.Fatalf("order %v, want %v", l.Order, want)
	}
	if got := []int64{l.Fields[0].Offset, l.Fields[1].Offset, l.Fields[3].Offset, l.Fields[4].Offset}; !reflect.DeepEqual(got, []int64{0, 8, 20, 24}) {
		t.Fatalf("offsets %v", got)
	}
}

func TestLayoutOfEmptyStructIsZero(t *testing.T) {
	if l := LayoutFields(nil); l.Size != 0 || l.Packed != 0 {
		t.Fatalf("empty layout %+v", l)
	}
}
