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

// --- Struct type tests ---

func TestStructTypes(t *testing.T) {
	ResetStructTypes()

	// RegisterStructType: register a new struct
	def := StructDef{
		Name:   "Point",
		Fields: []StructField{{Name: "x", Type: TypeInt}, {Name: "y", Type: TypeInt}},
	}
	id := RegisterStructType(def)
	if id != TypeStructBase {
		t.Errorf("RegisterStructType(Point) = %d, want %d", id, TypeStructBase)
	}

	// RegisterStructType: register a second struct
	def2 := StructDef{
		Name:   "Color",
		Fields: []StructField{{Name: "r", Type: TypeInt}},
	}
	id2 := RegisterStructType(def2)
	if id2 != TypeStructBase+1 {
		t.Errorf("RegisterStructType(Color) = %d, want %d", id2, TypeStructBase+1)
	}

	// RegisterStructType: re-register same name updates and returns same ID
	defUpdated := StructDef{
		Name:   "Point",
		Fields: []StructField{{Name: "x", Type: TypeDouble}, {Name: "y", Type: TypeDouble}, {Name: "z", Type: TypeDouble}},
	}
	idAgain := RegisterStructType(defUpdated)
	if idAgain != id {
		t.Errorf("RegisterStructType(Point again) = %d, want same ID %d", idAgain, id)
	}
	// Verify update took effect
	got := GetStructDef(id)
	if got == nil {
		t.Fatal("GetStructDef(Point) returned nil after update")
	}
	if len(got.Fields) != 3 {
		t.Errorf("GetStructDef(Point) after update has %d fields, want 3", len(got.Fields))
	}

	// LookupStructType: found
	lookID, ok := LookupStructType("Point")
	if !ok || lookID != id {
		t.Errorf("LookupStructType(Point) = (%d, %v), want (%d, true)", lookID, ok, id)
	}

	// LookupStructType: not found
	_, ok = LookupStructType("Missing")
	if ok {
		t.Error("LookupStructType(Missing) = (_, true), want (_, false)")
	}

	// GetStructDef: valid ID
	if GetStructDef(id) == nil {
		t.Error("GetStructDef(valid) = nil, want non-nil")
	}

	// GetStructDef: invalid ID
	if GetStructDef(TypeInt) != nil {
		t.Error("GetStructDef(TypeInt) = non-nil, want nil")
	}
	if GetStructDef(TypeStructBase+999) != nil {
		t.Error("GetStructDef(out of range) = non-nil, want nil")
	}

	// StructName: valid
	if name := StructName(id); name != "Point" {
		t.Errorf("StructName(valid) = %q, want %q", name, "Point")
	}

	// StructName: invalid
	if name := StructName(TypeInt); name != "unknown" {
		t.Errorf("StructName(TypeInt) = %q, want %q", name, "unknown")
	}

	// IsStructType
	tests := []struct {
		typ  Type
		want bool
	}{
		{id, true},
		{id2, true},
		{TypeInt, false},
		{TypeBool, false},
		{TypeString, false},
		{TypeVoid, false},
		{TypeChanBase, false},
	}
	for _, tt := range tests {
		if got := IsStructType(tt.typ); got != tt.want {
			t.Errorf("IsStructType(%d) = %v, want %v", tt.typ, got, tt.want)
		}
	}

	// AllStructNames
	names := AllStructNames()
	if len(names) != 2 {
		t.Errorf("AllStructNames() returned %d names, want 2", len(names))
	}
	nameSet := map[string]bool{}
	for _, n := range names {
		nameSet[n] = true
	}
	if !nameSet["Point"] || !nameSet["Color"] {
		t.Errorf("AllStructNames() = %v, want [Point, Color]", names)
	}
}

// --- Heap/Release/Value type tests ---

func TestIsHeapType(t *testing.T) {
	ResetChanTypes()
	ResetTaskTypes()
	ResetWeakTypes()
	ResetMapTypes()

	chanID := ChanTypeOf(TypeInt)
	taskID := TaskTypeOf(TypeInt)
	weakID := WeakTypeOf(TypeInt)
	mapID := MapTypeOf(TypeString, TypeInt)

	tests := []struct {
		typ  Type
		want bool
	}{
		{TypeString, true},
		{TypeStringBuilder, true},
		{TypeArrayInt, true},
		{TypeArrayBool, true},
		{TypeArrayString, true},
		{TypeArrayLong, true},
		{TypeArrayDouble, true},
		{chanID, true},
		{taskID, true},
		{weakID, true},
		{mapID, true},
		{TypeInt, false},
		{TypeBool, false},
		{TypeVoid, false},
		{TypeLong, false},
		{TypeDouble, false},
		{TypeChar, false},
	}
	for _, tt := range tests {
		if got := IsHeapType(tt.typ); got != tt.want {
			t.Errorf("IsHeapType(%d) = %v, want %v", tt.typ, got, tt.want)
		}
	}
}

func TestNeedsRelease(t *testing.T) {
	ResetStructTypes()
	ResetOptionalTypes()
	ResetChanTypes()
	ResetTaskTypes()
	ResetWeakTypes()
	ResetMapTypes()

	// Struct with no heap fields
	pureStruct := RegisterStructType(StructDef{
		Name:   "Pure",
		Fields: []StructField{{Name: "x", Type: TypeInt}, {Name: "y", Type: TypeBool}},
	})
	// Struct with a heap field
	heapStruct := RegisterStructType(StructDef{
		Name:   "WithString",
		Fields: []StructField{{Name: "name", Type: TypeString}},
	})
	// Optional wrapping a heap type
	optString := OptionalTypeOf(TypeString)
	// Optional wrapping a value type
	optInt := OptionalTypeOf(TypeInt)

	tests := []struct {
		typ  Type
		want bool
	}{
		{TypeString, true},
		{TypeArrayInt, true},
		{heapStruct, true},
		{optString, true},
		{TypeInt, false},
		{TypeBool, false},
		{pureStruct, false},
		{optInt, false},
	}
	for _, tt := range tests {
		if got := NeedsRelease(tt.typ); got != tt.want {
			t.Errorf("NeedsRelease(%d) = %v, want %v", tt.typ, got, tt.want)
		}
	}
}

func TestIsValueType(t *testing.T) {
	ResetEnumTypes()

	enumID := RegisterEnumType(EnumDef{Name: "Color", Variants: []string{"Red", "Green", "Blue"}})

	tests := []struct {
		typ  Type
		want bool
	}{
		{TypeInt, true},
		{TypeBool, true},
		{TypeLong, true},
		{TypeDouble, true},
		{TypeChar, true},
		{enumID, true},
		{TypeString, false},
		{TypeVoid, false},
		{TypeArrayInt, false},
	}
	for _, tt := range tests {
		if got := IsValueType(tt.typ); got != tt.want {
			t.Errorf("IsValueType(%d) = %v, want %v", tt.typ, got, tt.want)
		}
	}
}

// --- Channel type tests ---

func TestChanTypes(t *testing.T) {
	ResetChanTypes()

	// ChanTypeOf: create channel of int
	chanInt := ChanTypeOf(TypeInt)
	if chanInt != TypeChanBase {
		t.Errorf("ChanTypeOf(TypeInt) = %d, want %d", chanInt, TypeChanBase)
	}

	// ChanTypeOf: idempotent
	chanIntAgain := ChanTypeOf(TypeInt)
	if chanIntAgain != chanInt {
		t.Errorf("ChanTypeOf(TypeInt) second call = %d, want %d", chanIntAgain, chanInt)
	}

	// ChanTypeOf: different element type gets different ID
	chanString := ChanTypeOf(TypeString)
	if chanString == chanInt {
		t.Error("ChanTypeOf(TypeString) should differ from ChanTypeOf(TypeInt)")
	}
	if chanString != TypeChanBase+1 {
		t.Errorf("ChanTypeOf(TypeString) = %d, want %d", chanString, TypeChanBase+1)
	}

	// IsChanType
	tests := []struct {
		typ  Type
		want bool
	}{
		{chanInt, true},
		{chanString, true},
		{TypeInt, false},
		{TypeString, false},
		{TypeTaskBase, false},
	}
	for _, tt := range tests {
		if got := IsChanType(tt.typ); got != tt.want {
			t.Errorf("IsChanType(%d) = %v, want %v", tt.typ, got, tt.want)
		}
	}

	// ChanElemType: valid
	if got := ChanElemType(chanInt); got != TypeInt {
		t.Errorf("ChanElemType(chanInt) = %d, want %d", got, TypeInt)
	}
	if got := ChanElemType(chanString); got != TypeString {
		t.Errorf("ChanElemType(chanString) = %d, want %d", got, TypeString)
	}

	// ChanElemType: invalid
	if got := ChanElemType(TypeInt); got != TypeVoid {
		t.Errorf("ChanElemType(TypeInt) = %d, want %d (TypeVoid)", got, TypeVoid)
	}
}

// --- Task type tests ---

func TestTaskTypes(t *testing.T) {
	ResetTaskTypes()

	// TaskTypeOf: create task returning int
	taskInt := TaskTypeOf(TypeInt)
	if taskInt != TypeTaskBase {
		t.Errorf("TaskTypeOf(TypeInt) = %d, want %d", taskInt, TypeTaskBase)
	}

	// TaskTypeOf: idempotent
	taskIntAgain := TaskTypeOf(TypeInt)
	if taskIntAgain != taskInt {
		t.Errorf("TaskTypeOf(TypeInt) second call = %d, want %d", taskIntAgain, taskInt)
	}

	// TaskTypeOf: different return type gets different ID
	taskString := TaskTypeOf(TypeString)
	if taskString == taskInt {
		t.Error("TaskTypeOf(TypeString) should differ from TaskTypeOf(TypeInt)")
	}
	if taskString != TypeTaskBase+1 {
		t.Errorf("TaskTypeOf(TypeString) = %d, want %d", taskString, TypeTaskBase+1)
	}

	// IsTaskType
	tests := []struct {
		typ  Type
		want bool
	}{
		{taskInt, true},
		{taskString, true},
		{TypeInt, false},
		{TypeString, false},
		{TypeFuncBase, false},
	}
	for _, tt := range tests {
		if got := IsTaskType(tt.typ); got != tt.want {
			t.Errorf("IsTaskType(%d) = %v, want %v", tt.typ, got, tt.want)
		}
	}

	// TaskReturnType: valid
	if got := TaskReturnType(taskInt); got != TypeInt {
		t.Errorf("TaskReturnType(taskInt) = %d, want %d", got, TypeInt)
	}
	if got := TaskReturnType(taskString); got != TypeString {
		t.Errorf("TaskReturnType(taskString) = %d, want %d", got, TypeString)
	}

	// TaskReturnType: invalid
	if got := TaskReturnType(TypeInt); got != TypeVoid {
		t.Errorf("TaskReturnType(TypeInt) = %d, want %d (TypeVoid)", got, TypeVoid)
	}
}

// --- Function type tests ---

func TestFuncTypes(t *testing.T) {
	ResetFuncTypes()

	// FuncTypeOf: create function type (int, bool) -> string
	params := []Type{TypeInt, TypeBool}
	funcID := FuncTypeOf(params, TypeString)
	if funcID != TypeFuncBase {
		t.Errorf("FuncTypeOf([int,bool], string) = %d, want %d", funcID, TypeFuncBase)
	}

	// FuncTypeOf: idempotent
	funcIDAgain := FuncTypeOf([]Type{TypeInt, TypeBool}, TypeString)
	if funcIDAgain != funcID {
		t.Errorf("FuncTypeOf same signature again = %d, want %d", funcIDAgain, funcID)
	}

	// FuncTypeOf: different signature gets different ID
	funcID2 := FuncTypeOf([]Type{TypeString}, TypeInt)
	if funcID2 == funcID {
		t.Error("FuncTypeOf with different signature should get different ID")
	}
	if funcID2 != TypeFuncBase+1 {
		t.Errorf("FuncTypeOf([string], int) = %d, want %d", funcID2, TypeFuncBase+1)
	}

	// FuncTypeOf: no params
	funcID3 := FuncTypeOf([]Type{}, TypeVoid)
	if funcID3 != TypeFuncBase+2 {
		t.Errorf("FuncTypeOf([], void) = %d, want %d", funcID3, TypeFuncBase+2)
	}

	// IsFuncType
	tests := []struct {
		typ  Type
		want bool
	}{
		{funcID, true},
		{funcID2, true},
		{funcID3, true},
		{TypeInt, false},
		{TypeString, false},
		{TypeWeakBase, false},
	}
	for _, tt := range tests {
		if got := IsFuncType(tt.typ); got != tt.want {
			t.Errorf("IsFuncType(%d) = %v, want %v", tt.typ, got, tt.want)
		}
	}

	// FuncTypeParams: valid
	gotParams := FuncTypeParams(funcID)
	if len(gotParams) != 2 || gotParams[0] != TypeInt || gotParams[1] != TypeBool {
		t.Errorf("FuncTypeParams(funcID) = %v, want [TypeInt, TypeBool]", gotParams)
	}

	// FuncTypeParams: no params
	gotParams3 := FuncTypeParams(funcID3)
	if len(gotParams3) != 0 {
		t.Errorf("FuncTypeParams(funcID3) = %v, want []", gotParams3)
	}

	// FuncTypeParams: invalid
	if got := FuncTypeParams(TypeInt); got != nil {
		t.Errorf("FuncTypeParams(TypeInt) = %v, want nil", got)
	}

	// FuncTypeReturn: valid
	if got := FuncTypeReturn(funcID); got != TypeString {
		t.Errorf("FuncTypeReturn(funcID) = %d, want %d (TypeString)", got, TypeString)
	}
	if got := FuncTypeReturn(funcID2); got != TypeInt {
		t.Errorf("FuncTypeReturn(funcID2) = %d, want %d (TypeInt)", got, TypeInt)
	}

	// FuncTypeReturn: invalid
	if got := FuncTypeReturn(TypeInt); got != TypeVoid {
		t.Errorf("FuncTypeReturn(TypeInt) = %d, want %d (TypeVoid)", got, TypeVoid)
	}
}

// --- Weak ref type tests ---

func TestWeakTypes(t *testing.T) {
	ResetWeakTypes()
	ResetStructTypes()

	structID := RegisterStructType(StructDef{Name: "Node", Fields: []StructField{{Name: "val", Type: TypeInt}}})

	// WeakTypeOf: create weak ref
	weakNode := WeakTypeOf(structID)
	if weakNode != TypeWeakBase {
		t.Errorf("WeakTypeOf(Node) = %d, want %d", weakNode, TypeWeakBase)
	}

	// WeakTypeOf: idempotent
	weakNodeAgain := WeakTypeOf(structID)
	if weakNodeAgain != weakNode {
		t.Errorf("WeakTypeOf(Node) second call = %d, want %d", weakNodeAgain, weakNode)
	}

	// WeakTypeOf: different inner type
	weakInt := WeakTypeOf(TypeInt)
	if weakInt == weakNode {
		t.Error("WeakTypeOf(TypeInt) should differ from WeakTypeOf(Node)")
	}

	// IsWeakType
	tests := []struct {
		typ  Type
		want bool
	}{
		{weakNode, true},
		{weakInt, true},
		{TypeInt, false},
		{TypeString, false},
		{TypeStructArrayBase, false},
	}
	for _, tt := range tests {
		if got := IsWeakType(tt.typ); got != tt.want {
			t.Errorf("IsWeakType(%d) = %v, want %v", tt.typ, got, tt.want)
		}
	}

	// WeakInnerType: valid
	if got := WeakInnerType(weakNode); got != structID {
		t.Errorf("WeakInnerType(weakNode) = %d, want %d", got, structID)
	}
	if got := WeakInnerType(weakInt); got != TypeInt {
		t.Errorf("WeakInnerType(weakInt) = %d, want %d", got, TypeInt)
	}

	// WeakInnerType: invalid
	if got := WeakInnerType(TypeInt); got != TypeVoid {
		t.Errorf("WeakInnerType(TypeInt) = %d, want %d (TypeVoid)", got, TypeVoid)
	}
}

// --- Struct array type tests ---

func TestStructArrayTypes(t *testing.T) {
	ResetStructArrayTypes()
	ResetStructTypes()

	structID := RegisterStructType(StructDef{Name: "Item", Fields: []StructField{{Name: "id", Type: TypeInt}}})

	// StructArrayTypeOf: create struct array
	arrItem := StructArrayTypeOf(structID)
	if arrItem != TypeStructArrayBase {
		t.Errorf("StructArrayTypeOf(Item) = %d, want %d", arrItem, TypeStructArrayBase)
	}

	// StructArrayTypeOf: idempotent
	arrItemAgain := StructArrayTypeOf(structID)
	if arrItemAgain != arrItem {
		t.Errorf("StructArrayTypeOf(Item) second call = %d, want %d", arrItemAgain, arrItem)
	}

	// StructArrayTypeOf: different element type
	structID2 := RegisterStructType(StructDef{Name: "Other"})
	arrOther := StructArrayTypeOf(structID2)
	if arrOther == arrItem {
		t.Error("StructArrayTypeOf(Other) should differ from StructArrayTypeOf(Item)")
	}

	// IsStructArrayType
	tests := []struct {
		typ  Type
		want bool
	}{
		{arrItem, true},
		{arrOther, true},
		{TypeInt, false},
		{TypeArrayInt, false},
		{TypeOptionalBase, false},
	}
	for _, tt := range tests {
		if got := IsStructArrayType(tt.typ); got != tt.want {
			t.Errorf("IsStructArrayType(%d) = %v, want %v", tt.typ, got, tt.want)
		}
	}

	// StructArrayElemType: valid
	if got := StructArrayElemType(arrItem); got != structID {
		t.Errorf("StructArrayElemType(arrItem) = %d, want %d", got, structID)
	}

	// StructArrayElemType: invalid
	if got := StructArrayElemType(TypeInt); got != TypeVoid {
		t.Errorf("StructArrayElemType(TypeInt) = %d, want %d (TypeVoid)", got, TypeVoid)
	}
}

// --- Optional type tests ---

func TestOptionalTypes(t *testing.T) {
	ResetOptionalTypes()

	// OptionalTypeOf: create optional int
	optInt := OptionalTypeOf(TypeInt)
	if optInt != TypeOptionalBase {
		t.Errorf("OptionalTypeOf(TypeInt) = %d, want %d", optInt, TypeOptionalBase)
	}

	// OptionalTypeOf: idempotent
	optIntAgain := OptionalTypeOf(TypeInt)
	if optIntAgain != optInt {
		t.Errorf("OptionalTypeOf(TypeInt) second call = %d, want %d", optIntAgain, optInt)
	}

	// OptionalTypeOf: different inner type
	optString := OptionalTypeOf(TypeString)
	if optString == optInt {
		t.Error("OptionalTypeOf(TypeString) should differ from OptionalTypeOf(TypeInt)")
	}
	if optString != TypeOptionalBase+1 {
		t.Errorf("OptionalTypeOf(TypeString) = %d, want %d", optString, TypeOptionalBase+1)
	}

	// IsOptionalType
	tests := []struct {
		typ  Type
		want bool
	}{
		{optInt, true},
		{optString, true},
		{TypeInt, false},
		{TypeString, false},
		{TypeRefBase, false},
	}
	for _, tt := range tests {
		if got := IsOptionalType(tt.typ); got != tt.want {
			t.Errorf("IsOptionalType(%d) = %v, want %v", tt.typ, got, tt.want)
		}
	}

	// OptionalInnerType: valid
	if got := OptionalInnerType(optInt); got != TypeInt {
		t.Errorf("OptionalInnerType(optInt) = %d, want %d", got, TypeInt)
	}
	if got := OptionalInnerType(optString); got != TypeString {
		t.Errorf("OptionalInnerType(optString) = %d, want %d", got, TypeString)
	}

	// OptionalInnerType: invalid
	if got := OptionalInnerType(TypeInt); got != TypeVoid {
		t.Errorf("OptionalInnerType(TypeInt) = %d, want %d (TypeVoid)", got, TypeVoid)
	}
}

// --- Ref type tests ---

func TestRefTypes(t *testing.T) {
	ResetRefTypes()

	// RefTypeOf: create ref to int
	refInt := RefTypeOf(TypeInt)
	if refInt != TypeRefBase {
		t.Errorf("RefTypeOf(TypeInt) = %d, want %d", refInt, TypeRefBase)
	}

	// RefTypeOf: idempotent
	refIntAgain := RefTypeOf(TypeInt)
	if refIntAgain != refInt {
		t.Errorf("RefTypeOf(TypeInt) second call = %d, want %d", refIntAgain, refInt)
	}

	// RefTypeOf: different inner type
	refString := RefTypeOf(TypeString)
	if refString == refInt {
		t.Error("RefTypeOf(TypeString) should differ from RefTypeOf(TypeInt)")
	}
	if refString != TypeRefBase+1 {
		t.Errorf("RefTypeOf(TypeString) = %d, want %d", refString, TypeRefBase+1)
	}

	// IsRefType
	tests := []struct {
		typ  Type
		want bool
	}{
		{refInt, true},
		{refString, true},
		{TypeInt, false},
		{TypeString, false},
		{TypeMapBase, false},
	}
	for _, tt := range tests {
		if got := IsRefType(tt.typ); got != tt.want {
			t.Errorf("IsRefType(%d) = %v, want %v", tt.typ, got, tt.want)
		}
	}

	// RefInnerType: valid
	if got := RefInnerType(refInt); got != TypeInt {
		t.Errorf("RefInnerType(refInt) = %d, want %d", got, TypeInt)
	}
	if got := RefInnerType(refString); got != TypeString {
		t.Errorf("RefInnerType(refString) = %d, want %d", got, TypeString)
	}

	// RefInnerType: invalid
	if got := RefInnerType(TypeInt); got != TypeVoid {
		t.Errorf("RefInnerType(TypeInt) = %d, want %d (TypeVoid)", got, TypeVoid)
	}
}

// --- Map type tests ---

func TestMapTypes(t *testing.T) {
	ResetMapTypes()

	// MapTypeOf: create map[string]int
	mapStrInt := MapTypeOf(TypeString, TypeInt)
	if mapStrInt != TypeMapBase {
		t.Errorf("MapTypeOf(string, int) = %d, want %d", mapStrInt, TypeMapBase)
	}

	// MapTypeOf: idempotent
	mapStrIntAgain := MapTypeOf(TypeString, TypeInt)
	if mapStrIntAgain != mapStrInt {
		t.Errorf("MapTypeOf(string, int) second call = %d, want %d", mapStrIntAgain, mapStrInt)
	}

	// MapTypeOf: different key/value types
	mapIntBool := MapTypeOf(TypeInt, TypeBool)
	if mapIntBool == mapStrInt {
		t.Error("MapTypeOf(int, bool) should differ from MapTypeOf(string, int)")
	}
	if mapIntBool != TypeMapBase+1 {
		t.Errorf("MapTypeOf(int, bool) = %d, want %d", mapIntBool, TypeMapBase+1)
	}

	// MapTypeOf: same key different value is different
	mapStrBool := MapTypeOf(TypeString, TypeBool)
	if mapStrBool == mapStrInt {
		t.Error("MapTypeOf(string, bool) should differ from MapTypeOf(string, int)")
	}

	// IsMapType
	tests := []struct {
		typ  Type
		want bool
	}{
		{mapStrInt, true},
		{mapIntBool, true},
		{mapStrBool, true},
		{TypeInt, false},
		{TypeString, false},
		{TypeEnumBase, false},
	}
	for _, tt := range tests {
		if got := IsMapType(tt.typ); got != tt.want {
			t.Errorf("IsMapType(%d) = %v, want %v", tt.typ, got, tt.want)
		}
	}

	// MapKeyType: valid
	if got := MapKeyType(mapStrInt); got != TypeString {
		t.Errorf("MapKeyType(mapStrInt) = %d, want %d (TypeString)", got, TypeString)
	}
	if got := MapKeyType(mapIntBool); got != TypeInt {
		t.Errorf("MapKeyType(mapIntBool) = %d, want %d (TypeInt)", got, TypeInt)
	}

	// MapKeyType: invalid
	if got := MapKeyType(TypeInt); got != TypeVoid {
		t.Errorf("MapKeyType(TypeInt) = %d, want %d (TypeVoid)", got, TypeVoid)
	}

	// MapValueType: valid
	if got := MapValueType(mapStrInt); got != TypeInt {
		t.Errorf("MapValueType(mapStrInt) = %d, want %d (TypeInt)", got, TypeInt)
	}
	if got := MapValueType(mapIntBool); got != TypeBool {
		t.Errorf("MapValueType(mapIntBool) = %d, want %d (TypeBool)", got, TypeBool)
	}

	// MapValueType: invalid
	if got := MapValueType(TypeInt); got != TypeVoid {
		t.Errorf("MapValueType(TypeInt) = %d, want %d (TypeVoid)", got, TypeVoid)
	}
}

// --- Enum type tests ---

func TestEnumTypes(t *testing.T) {
	ResetEnumTypes()

	// RegisterEnumType: register new enum
	def := EnumDef{Name: "Color", Variants: []string{"Red", "Green", "Blue"}}
	id := RegisterEnumType(def)
	if id != TypeEnumBase {
		t.Errorf("RegisterEnumType(Color) = %d, want %d", id, TypeEnumBase)
	}

	// RegisterEnumType: register second enum
	def2 := EnumDef{Name: "Direction", Variants: []string{"Up", "Down", "Left", "Right"}}
	id2 := RegisterEnumType(def2)
	if id2 != TypeEnumBase+1 {
		t.Errorf("RegisterEnumType(Direction) = %d, want %d", id2, TypeEnumBase+1)
	}

	// RegisterEnumType: re-register updates and returns same ID
	defUpdated := EnumDef{Name: "Color", Variants: []string{"Red", "Green", "Blue", "Alpha"}}
	idAgain := RegisterEnumType(defUpdated)
	if idAgain != id {
		t.Errorf("RegisterEnumType(Color again) = %d, want same ID %d", idAgain, id)
	}
	gotDef := GetEnumDef(id)
	if gotDef == nil {
		t.Fatal("GetEnumDef(Color) returned nil after update")
	}
	if len(gotDef.Variants) != 4 {
		t.Errorf("GetEnumDef(Color) after update has %d variants, want 4", len(gotDef.Variants))
	}

	// LookupEnumType: found
	lookID, ok := LookupEnumType("Color")
	if !ok || lookID != id {
		t.Errorf("LookupEnumType(Color) = (%d, %v), want (%d, true)", lookID, ok, id)
	}

	// LookupEnumType: not found
	_, ok = LookupEnumType("Missing")
	if ok {
		t.Error("LookupEnumType(Missing) = (_, true), want (_, false)")
	}

	// GetEnumDef: valid
	if GetEnumDef(id) == nil {
		t.Error("GetEnumDef(valid) = nil, want non-nil")
	}

	// GetEnumDef: invalid
	if GetEnumDef(TypeInt) != nil {
		t.Error("GetEnumDef(TypeInt) = non-nil, want nil")
	}

	// IsEnumType
	tests := []struct {
		typ  Type
		want bool
	}{
		{id, true},
		{id2, true},
		{TypeInt, false},
		{TypeString, false},
		{TypeInterfaceBase, false},
	}
	for _, tt := range tests {
		if got := IsEnumType(tt.typ); got != tt.want {
			t.Errorf("IsEnumType(%d) = %v, want %v", tt.typ, got, tt.want)
		}
	}

	// EnumName: valid
	if name := EnumName(id); name != "Color" {
		t.Errorf("EnumName(Color) = %q, want %q", name, "Color")
	}

	// EnumName: invalid
	if name := EnumName(TypeInt); name != "unknown" {
		t.Errorf("EnumName(TypeInt) = %q, want %q", name, "unknown")
	}
}

// --- Interface type tests ---

func TestInterfaceTypes(t *testing.T) {
	ResetInterfaceTypes()

	// RegisterInterfaceType: register new interface
	def := InterfaceDef{
		Name: "Stringer",
		Methods: []InterfaceMethod{
			{Name: "toString", Params: []Type{}, ReturnType: TypeString},
		},
	}
	id := RegisterInterfaceType(def)
	if id != TypeInterfaceBase {
		t.Errorf("RegisterInterfaceType(Stringer) = %d, want %d", id, TypeInterfaceBase)
	}

	// RegisterInterfaceType: register second interface
	def2 := InterfaceDef{
		Name: "Comparable",
		Methods: []InterfaceMethod{
			{Name: "compareTo", Params: []Type{TypeInt}, ReturnType: TypeInt},
		},
	}
	id2 := RegisterInterfaceType(def2)
	if id2 != TypeInterfaceBase+1 {
		t.Errorf("RegisterInterfaceType(Comparable) = %d, want %d", id2, TypeInterfaceBase+1)
	}

	// RegisterInterfaceType: re-register updates and returns same ID
	defUpdated := InterfaceDef{
		Name: "Stringer",
		Methods: []InterfaceMethod{
			{Name: "toString", Params: []Type{}, ReturnType: TypeString},
			{Name: "toDebugString", Params: []Type{}, ReturnType: TypeString},
		},
	}
	idAgain := RegisterInterfaceType(defUpdated)
	if idAgain != id {
		t.Errorf("RegisterInterfaceType(Stringer again) = %d, want same ID %d", idAgain, id)
	}
	gotDef := GetInterfaceDef(id)
	if gotDef == nil {
		t.Fatal("GetInterfaceDef(Stringer) returned nil after update")
	}
	if len(gotDef.Methods) != 2 {
		t.Errorf("GetInterfaceDef(Stringer) after update has %d methods, want 2", len(gotDef.Methods))
	}

	// LookupInterfaceType: found
	lookID, ok := LookupInterfaceType("Stringer")
	if !ok || lookID != id {
		t.Errorf("LookupInterfaceType(Stringer) = (%d, %v), want (%d, true)", lookID, ok, id)
	}

	// LookupInterfaceType: not found
	_, ok = LookupInterfaceType("Missing")
	if ok {
		t.Error("LookupInterfaceType(Missing) = (_, true), want (_, false)")
	}

	// GetInterfaceDef: valid
	if GetInterfaceDef(id) == nil {
		t.Error("GetInterfaceDef(valid) = nil, want non-nil")
	}

	// GetInterfaceDef: invalid
	if GetInterfaceDef(TypeInt) != nil {
		t.Error("GetInterfaceDef(TypeInt) = non-nil, want nil")
	}

	// IsInterfaceType
	tests := []struct {
		typ  Type
		want bool
	}{
		{id, true},
		{id2, true},
		{TypeInt, false},
		{TypeString, false},
		{TypeEnumBase, false},
	}
	for _, tt := range tests {
		if got := IsInterfaceType(tt.typ); got != tt.want {
			t.Errorf("IsInterfaceType(%d) = %v, want %v", tt.typ, got, tt.want)
		}
	}

	// InterfaceName: valid
	if name := InterfaceName(id); name != "Stringer" {
		t.Errorf("InterfaceName(Stringer) = %q, want %q", name, "Stringer")
	}

	// InterfaceName: invalid
	if name := InterfaceName(TypeInt); name != "unknown" {
		t.Errorf("InterfaceName(TypeInt) = %q, want %q", name, "unknown")
	}
}

// --- HasAnnotation tests ---

func TestHasAnnotation(t *testing.T) {
	tests := []struct {
		annotations []string
		name        string
		want        bool
	}{
		{[]string{"owned", "region"}, "owned", true},
		{[]string{"owned", "region"}, "region", true},
		{[]string{"owned", "region"}, "noEscape", false},
		{[]string{}, "owned", false},
		{nil, "owned", false},
		{[]string{"debug(cycles)"}, "debug(cycles)", true},
		{[]string{"owned"}, "own", false},
	}
	for _, tt := range tests {
		got := HasAnnotation(tt.annotations, tt.name)
		if got != tt.want {
			t.Errorf("HasAnnotation(%v, %q) = %v, want %v", tt.annotations, tt.name, got, tt.want)
		}
	}
}

// --- RegisterExceptionType tests ---

func TestRegisterExceptionType(t *testing.T) {
	ResetStructTypes()

	// Should not panic
	RegisterExceptionType()

	// Should have registered "Exception"
	id, ok := LookupStructType("Exception")
	if !ok {
		t.Fatal("RegisterExceptionType did not register Exception struct")
	}

	def := GetStructDef(id)
	if def == nil {
		t.Fatal("GetStructDef(Exception) returned nil")
	}
	if def.Name != "Exception" {
		t.Errorf("Exception struct name = %q, want %q", def.Name, "Exception")
	}
	if len(def.Fields) != 1 || def.Fields[0].Name != "message" || def.Fields[0].Type != TypeString {
		t.Errorf("Exception struct fields = %v, want [{message TypeString}]", def.Fields)
	}

	// Calling again should not panic or create a duplicate
	RegisterExceptionType()
	names := AllStructNames()
	count := 0
	for _, n := range names {
		if n == "Exception" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("After two RegisterExceptionType calls, found %d Exception entries, want 1", count)
	}
}
