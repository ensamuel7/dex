package ast

import "testing"

func TestIsArrayType(t *testing.T) {
	tests := []struct {
		typ  Type
		want bool
	}{
		{TypeArrayInt, true},
		{TypeArrayBool, true},
		{TypeArrayString, true},
		{TypeArrayLong, true},
		{TypeArrayDouble, true},
		{TypeInt, false},
		{TypeBool, false},
		{TypeString, false},
		{TypeVoid, false},
		{TypeLong, false},
		{TypeDouble, false},
	}
	for _, tt := range tests {
		got := IsArrayType(tt.typ)
		if got != tt.want {
			t.Errorf("IsArrayType(%d) = %v, want %v", tt.typ, got, tt.want)
		}
	}
}

func TestElementType(t *testing.T) {
	tests := []struct {
		arrType  Type
		wantElem Type
	}{
		{TypeArrayInt, TypeInt},
		{TypeArrayBool, TypeBool},
		{TypeArrayString, TypeString},
		{TypeArrayLong, TypeLong},
		{TypeArrayDouble, TypeDouble},
		{TypeInt, TypeVoid},
		{TypeBool, TypeVoid},
		{TypeString, TypeVoid},
		{TypeVoid, TypeVoid},
		{TypeLong, TypeVoid},
		{TypeDouble, TypeVoid},
	}
	for _, tt := range tests {
		got := ElementType(tt.arrType)
		if got != tt.wantElem {
			t.Errorf("ElementType(%d) = %d, want %d", tt.arrType, got, tt.wantElem)
		}
	}
}

func TestArrayTypeOf(t *testing.T) {
	tests := []struct {
		elemType Type
		wantArr  Type
	}{
		{TypeInt, TypeArrayInt},
		{TypeBool, TypeArrayBool},
		{TypeString, TypeArrayString},
		{TypeLong, TypeArrayLong},
		{TypeDouble, TypeArrayDouble},
		{TypeVoid, TypeVoid},
		{TypeArrayInt, TypeVoid},
	}
	for _, tt := range tests {
		got := ArrayTypeOf(tt.elemType)
		if got != tt.wantArr {
			t.Errorf("ArrayTypeOf(%d) = %d, want %d", tt.elemType, got, tt.wantArr)
		}
	}
}
