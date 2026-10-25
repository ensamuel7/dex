package ast

import (
	"reflect"
	"testing"
)

func TestLayoutSortsFieldsByAlignment(t *testing.T) {
	inner := RegisterStructType(StructDef{Name: "LayoutInner", Fields: []StructField{
		{Name: "flag", Type: TypeBool},
		{Name: "count", Type: TypeLong},
	}})
	l := LayoutOf(inner)
	if l.Size != 16 || l.Align != 8 || l.Padding != 7 || l.Declared != 16 {
		t.Fatalf("inner layout %+v", l)
	}
	if want := []string{"count", "flag"}; !reflect.DeepEqual(l.Order, want) {
		t.Fatalf("inner order %v, want %v", l.Order, want)
	}

	l = LayoutFields([]StructField{
		{Name: "a", Type: TypeBool},
		{Name: "b", Type: TypeLong},
		{Name: "c", Type: TypeBool},
		{Name: "d", Type: TypeInt},
		{Name: "e", Type: inner},
		{Name: "f", Type: TypeString},
	})
	if l.Size != 40 || l.Declared != 48 || l.Padding != 2 {
		t.Fatalf("size %d declared %d padding %d, want 40, 48 and 2", l.Size, l.Declared, l.Padding)
	}
	if want := []string{"b", "e", "f", "d", "a", "c"}; !reflect.DeepEqual(l.Order, want) {
		t.Fatalf("order %v, want %v", l.Order, want)
	}
	var offsets []int64
	for _, f := range l.Fields {
		offsets = append(offsets, f.Offset)
	}
	if want := []int64{0, 8, 24, 32, 36, 37}; !reflect.DeepEqual(offsets, want) {
		t.Fatalf("offsets %v, want %v", offsets, want)
	}
}

func TestLayoutOfEmptyStructIsZero(t *testing.T) {
	if l := LayoutFields(nil); l.Size != 0 || l.Declared != 0 {
		t.Fatalf("empty layout %+v", l)
	}
}
