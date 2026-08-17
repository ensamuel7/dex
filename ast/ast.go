package ast

import "fmt"

// Pos represents a source position (file, line, and column).
type Pos struct {
	File string
	Line int
	Col  int
}

type Type int

const (
	TypeInt Type = iota
	TypeBool
	TypeString
	TypeVoid
	TypeLong
	TypeDouble
	TypeArrayInt
	TypeArrayBool
	TypeArrayString
	TypeArrayLong
	TypeArrayDouble
	TypeChar
	TypeArrayChar
	TypeStringBuilder
)

// TypeInferred is used during parsing when a let statement has no explicit type annotation.
// The checker resolves this to the actual type from the RHS expression.
const TypeInferred Type = -1

// TypeNull is a sentinel type for null literal expressions.
// Used during type checking; null can only be assigned to optional types.
const TypeNull Type = -2

// Struct type system: dynamic IDs starting at 1000
const TypeStructBase Type = 1000

// Channel type system: dynamic IDs starting at 2000
const TypeChanBase Type = 2000

// Task type system: dynamic IDs starting at 3000
const TypeTaskBase Type = 3000

// Function type system: dynamic IDs starting at 4000
const TypeFuncBase Type = 4000

// Weak reference type system: dynamic IDs starting at 5000
const TypeWeakBase Type = 5000

// Struct array type system: dynamic IDs starting at 6000
const TypeStructArrayBase Type = 6000

// Optional type system: dynamic IDs starting at 7000
const TypeOptionalBase Type = 7000

// Reference type system: dynamic IDs starting at 8000
const TypeRefBase Type = 8000

// Map type system: dynamic IDs starting at 9000
const TypeMapBase Type = 9000

// Enum type system: dynamic IDs starting at 10000
const TypeEnumBase Type = 10000

type StructField struct {
	Name        string
	Type        Type
	IsPrivate   bool
	Annotations []string
	Doc         string // documentation string for hover
}

type StructDef struct {
	Name              string
	Fields            []StructField
	Methods           []Function    // methods declared inside the struct body
	ConstructorParams []StructField // auto-constructor params: struct Foo(x: int) { ... }
	Doc               string        // documentation string for hover
}

// Global struct registry
var (
	structDefs   []StructDef
	structByName map[string]Type
)

func init() {
	ResetStructTypes()
}

func ResetStructTypes() {
	structDefs = nil
	structByName = make(map[string]Type)
}

func RegisterStructType(def StructDef) Type {
	if id, ok := structByName[def.Name]; ok {
		// Update existing entry (e.g. placeholder from pre-scan gets real fields)
		idx := int(id - TypeStructBase)
		if idx >= 0 && idx < len(structDefs) {
			structDefs[idx] = def
		}
		return id
	}
	id := TypeStructBase + Type(len(structDefs))
	structByName[def.Name] = id
	structDefs = append(structDefs, def)
	return id
}

func LookupStructType(name string) (Type, bool) {
	id, ok := structByName[name]
	return id, ok
}

// AllStructNames returns the names of all registered struct types.
func AllStructNames() []string {
	names := make([]string, 0, len(structByName))
	for name := range structByName {
		names = append(names, name)
	}
	return names
}

func GetStructDef(t Type) *StructDef {
	idx := int(t - TypeStructBase)
	if idx < 0 || idx >= len(structDefs) {
		return nil
	}
	return &structDefs[idx]
}

func IsStructType(t Type) bool {
	return t >= TypeStructBase && t < TypeChanBase
}

func StructName(t Type) string {
	def := GetStructDef(t)
	if def == nil {
		return "unknown"
	}
	return def.Name
}

// Channel type registry
var (
	chanTypes  []Type        // element type for each channel type ID
	chanByElem map[Type]Type // element type → channel type ID
)

func init() {
	ResetChanTypes()
}

func ResetChanTypes() {
	chanTypes = nil
	chanByElem = make(map[Type]Type)
}

func ChanTypeOf(elemType Type) Type {
	if id, ok := chanByElem[elemType]; ok {
		return id
	}
	id := TypeChanBase + Type(len(chanTypes))
	chanByElem[elemType] = id
	chanTypes = append(chanTypes, elemType)
	return id
}

func IsChanType(t Type) bool {
	return t >= TypeChanBase && t < TypeTaskBase
}

func ChanElemType(t Type) Type {
	idx := int(t - TypeChanBase)
	if idx < 0 || idx >= len(chanTypes) {
		return TypeVoid
	}
	return chanTypes[idx]
}

// Task type registry
var (
	taskTypes []Type        // return type for each task type ID
	taskByRet map[Type]Type // return type → task type ID
)

func init() {
	ResetTaskTypes()
}

func ResetTaskTypes() {
	taskTypes = nil
	taskByRet = make(map[Type]Type)
}

func TaskTypeOf(returnType Type) Type {
	if id, ok := taskByRet[returnType]; ok {
		return id
	}
	id := TypeTaskBase + Type(len(taskTypes))
	taskByRet[returnType] = id
	taskTypes = append(taskTypes, returnType)
	return id
}

func IsTaskType(t Type) bool {
	return t >= TypeTaskBase && t < TypeFuncBase
}

func TaskReturnType(t Type) Type {
	idx := int(t - TypeTaskBase)
	if idx < 0 || idx >= len(taskTypes) {
		return TypeVoid
	}
	return taskTypes[idx]
}

// Function type registry
type FuncTypeInfo struct {
	Params     []Type
	ReturnType Type
}

var (
	funcTypes    []FuncTypeInfo
	funcTypeKeys map[string]Type
)

func init() {
	ResetFuncTypes()
}

func ResetFuncTypes() {
	funcTypes = nil
	funcTypeKeys = make(map[string]Type)
}

func funcTypeKey(params []Type, returnType Type) string {
	key := "fn("
	for i, p := range params {
		if i > 0 {
			key += ","
		}
		key += fmt.Sprintf("%d", int(p))
	}
	key += fmt.Sprintf("):%d", int(returnType))
	return key
}

func FuncTypeOf(params []Type, returnType Type) Type {
	key := funcTypeKey(params, returnType)
	if id, ok := funcTypeKeys[key]; ok {
		return id
	}
	id := TypeFuncBase + Type(len(funcTypes))
	funcTypeKeys[key] = id
	// Copy params to avoid aliasing
	paramsCopy := make([]Type, len(params))
	copy(paramsCopy, params)
	funcTypes = append(funcTypes, FuncTypeInfo{Params: paramsCopy, ReturnType: returnType})
	return id
}

func IsFuncType(t Type) bool {
	return t >= TypeFuncBase && t < TypeWeakBase
}

func FuncTypeParams(t Type) []Type {
	idx := int(t - TypeFuncBase)
	if idx < 0 || idx >= len(funcTypes) {
		return nil
	}
	return funcTypes[idx].Params
}

func FuncTypeReturn(t Type) Type {
	idx := int(t - TypeFuncBase)
	if idx < 0 || idx >= len(funcTypes) {
		return TypeVoid
	}
	return funcTypes[idx].ReturnType
}

func IsHeapType(t Type) bool {
	return t == TypeString || t == TypeStringBuilder || IsArrayType(t) || IsChanType(t) || IsTaskType(t) || IsWeakType(t) || IsMapType(t)
}

func NeedsRelease(t Type) bool {
	if IsOptionalType(t) {
		return NeedsRelease(OptionalInnerType(t))
	}
	if IsHeapType(t) {
		return true
	}
	if IsStructType(t) {
		def := GetStructDef(t)
		if def != nil {
			for _, f := range def.Fields {
				if NeedsRelease(f.Type) {
					return true
				}
			}
		}
	}
	return false
}

func IsArrayType(t Type) bool {
	return t == TypeArrayInt || t == TypeArrayBool || t == TypeArrayString || t == TypeArrayLong || t == TypeArrayDouble || t == TypeArrayChar || IsStructArrayType(t)
}

func ElementType(t Type) Type {
	switch t {
	case TypeArrayInt:
		return TypeInt
	case TypeArrayBool:
		return TypeBool
	case TypeArrayString:
		return TypeString
	case TypeArrayLong:
		return TypeLong
	case TypeArrayDouble:
		return TypeDouble
	case TypeArrayChar:
		return TypeChar
	default:
		if IsStructArrayType(t) {
			return StructArrayElemType(t)
		}
		return TypeVoid
	}
}

func ArrayTypeOf(elem Type) Type {
	switch elem {
	case TypeInt:
		return TypeArrayInt
	case TypeBool:
		return TypeArrayBool
	case TypeString:
		return TypeArrayString
	case TypeLong:
		return TypeArrayLong
	case TypeDouble:
		return TypeArrayDouble
	case TypeChar:
		return TypeArrayChar
	default:
		if IsStructType(elem) {
			return StructArrayTypeOf(elem)
		}
		if IsEnumType(elem) {
			return StructArrayTypeOf(elem)
		}
		return TypeVoid
	}
}

type BinOp int

const (
	BinAdd BinOp = iota
	BinSub
	BinMul
	BinDiv
	BinMod
	BinEq
	BinNeq
	BinStrictEq
	BinStrictNeq
	BinLt
	BinGt
	BinLte
	BinGte
	BinAnd
	BinOr
)

type UnaryOp int

const (
	UnaryNeg UnaryOp = iota
	UnaryNot
)

type Import struct {
	Path  string
	Alias string // if set, use as module name instead of filepath.Base(Path)
}

type Program struct {
	Imports      []Import
	Structs      []StructDef
	Enums        []EnumDef
	Functions    []Function
	UserModules  []string          // module names (last path segment) of resolved user imports
	StructModule map[string]string // structName -> moduleName (for cross-module structs)
}

type Function struct {
	Name        string
	Params      []Param
	ReturnType  Type
	Body        []Stmt
	IsPrivate   bool
	Annotations []string
}

type Param struct {
	Name        string
	Type        Type
	Annotations []string
}

// Stmt interface
type Stmt interface {
	stmtNode()
}

// Expr interface
type Expr interface {
	exprNode()
}

// Statements

type LetStmt struct {
	Pos         Pos
	Name        string
	Type        Type
	Value       Expr
	IsConst     bool
	Annotations []string
}

type ReturnStmt struct {
	Pos   Pos
	Value Expr
}

type ExprStmt struct {
	Pos  Pos
	Expr Expr
}

type IfStmt struct {
	Pos  Pos
	Cond Expr
	Then []Stmt
	Else []Stmt
}

type WhileStmt struct {
	Pos  Pos
	Cond Expr
	Body []Stmt
}

type BlockStmt struct {
	Pos   Pos
	Stmts []Stmt
}

type AssignStmt struct {
	Pos   Pos
	Name  string
	Value Expr
}

type IndexAssignStmt struct {
	Pos   Pos
	Array Expr
	Index Expr
	Value Expr
}

type FieldAssignStmt struct {
	Pos    Pos
	Object Expr
	Field  string
	Value  Expr
}

type BreakStmt struct {
	Pos Pos
}

type ContinueStmt struct {
	Pos Pos
}

type ForStmt struct {
	Pos  Pos
	Init Stmt
	Cond Expr
	Post Stmt
	Body []Stmt
}

type ForeachStmt struct {
	Pos      Pos
	Iterable Expr
	IndexVar string
	ValueVar string
	Body     []Stmt
}

type IncrementStmt struct {
	Pos  Pos
	Name string
}

type DecrementStmt struct {
	Pos  Pos
	Name string
}

type CompoundAssignStmt struct {
	Pos   Pos
	Name  string
	Op    BinOp
	Value Expr
}

func (s *LetStmt) stmtNode()            {}
func (s *ReturnStmt) stmtNode()         {}
func (s *ExprStmt) stmtNode()           {}
func (s *IfStmt) stmtNode()             {}
func (s *WhileStmt) stmtNode()          {}
func (s *BlockStmt) stmtNode()          {}
func (s *AssignStmt) stmtNode()         {}
func (s *IndexAssignStmt) stmtNode()    {}
func (s *FieldAssignStmt) stmtNode()    {}
func (s *BreakStmt) stmtNode()          {}
func (s *ContinueStmt) stmtNode()       {}
func (s *ForStmt) stmtNode()            {}
func (s *ForeachStmt) stmtNode()        {}
func (s *IncrementStmt) stmtNode()      {}
func (s *DecrementStmt) stmtNode()      {}
func (s *CompoundAssignStmt) stmtNode() {}

// SendStmt — send(value) or send(channel, value)
type SendStmt struct {
	Pos    Pos
	Target Expr // nil for implicit (send to own task handle), non-nil for channel
	Value  Expr
}

func (s *SendStmt) stmtNode() {}

// TryCatchStmt — try { ... } catch (e: Exception) { ... } finally { ... }
type TryCatchStmt struct {
	Pos         Pos
	Body        []Stmt // try body
	CatchVar    string // variable name in catch clause (empty if no catch)
	CatchBody   []Stmt // catch body (nil if no catch)
	FinallyBody []Stmt // finally body (nil if no finally)
}

func (s *TryCatchStmt) stmtNode() {}

// ThrowStmt — throw Exception("message")
type ThrowStmt struct {
	Pos   Pos
	Value Expr
}

func (s *ThrowStmt) stmtNode() {}

// SwitchCase — a single case arm in a switch statement
type SwitchCase struct {
	Pos    Pos
	Values []Expr // one or more match values (e.g. "foo", "bar")
	Body   []Stmt
}

// SwitchStmt — switch (tag) { case val: { ... } default: { ... } }
type SwitchStmt struct {
	Pos     Pos
	Tag     Expr
	Cases   []SwitchCase
	Default []Stmt // nil if no default
}

func (s *SwitchStmt) stmtNode() {}

// RegisterExceptionType registers the built-in Exception struct type if not already registered.
func RegisterExceptionType() {
	if _, exists := LookupStructType("Exception"); !exists {
		RegisterStructType(StructDef{
			Name:              "Exception",
			Fields:            []StructField{{Name: "message", Type: TypeString}},
			ConstructorParams: []StructField{{Name: "message", Type: TypeString}},
			Doc:               "Built-in exception type for error handling.",
		})
	}
}

// Expressions

type IntLit struct {
	Pos   Pos
	Value int
}

type FloatLit struct {
	Pos   Pos
	Value float64
}

type BoolLit struct {
	Pos   Pos
	Value bool
}

type StringLit struct {
	Pos   Pos
	Value string
}

type CharLit struct {
	Pos   Pos
	Value rune
}

type Ident struct {
	Pos  Pos
	Name string
}

type BinaryExpr struct {
	Pos           Pos
	Op            BinOp
	Left          Expr
	Right         Expr
	LeftType      Type
	RightType     Type
	HasMixedTypes bool
}

type UnaryExpr struct {
	Pos     Pos
	Op      UnaryOp
	Operand Expr
}

type CallExpr struct {
	Pos          Pos
	Module       string // empty = user-defined function, "http"/"json"/"fmt" = stdlib
	Name         string
	Args         []Expr
	ResolvedType Type // set by checker for return-type polymorphism (e.g. db.col)
	IsMethodCall bool // set by checker: instance.method() call
	IsConstructor bool // set by checker: StructName(args) constructor call
	StructType   Type // set by checker: the struct type for method/constructor calls
}

type ArrayLitExpr struct {
	Pos      Pos
	ElemType Type
	Elems    []Expr
}

type IndexExpr struct {
	Pos   Pos
	Array Expr
	Index Expr
}

type StructLitExpr struct {
	Pos         Pos
	Name        string
	FieldNames  []string
	FieldValues []Expr
}

type FieldAccessExpr struct {
	Pos    Pos
	Object Expr
	Field  string
}

func (e *IntLit) exprNode()          {}
func (e *FloatLit) exprNode()        {}
func (e *BoolLit) exprNode()         {}
func (e *StringLit) exprNode()       {}
func (e *CharLit) exprNode()         {}
func (e *Ident) exprNode()           {}
func (e *BinaryExpr) exprNode()      {}
func (e *UnaryExpr) exprNode()       {}
func (e *CallExpr) exprNode()        {}
func (e *ArrayLitExpr) exprNode()    {}
func (e *IndexExpr) exprNode()       {}
func (e *StructLitExpr) exprNode()   {}
func (e *FieldAccessExpr) exprNode() {}

// SpawnExpr — spawn { body } or spawn fn(args)
type SpawnExpr struct {
	Pos        Pos
	Body       []Stmt // block spawn: spawn { ... }
	Call       Expr   // call spawn: spawn fn(args) — must be *CallExpr
	ReturnType Type   // filled by checker: what send() sends or function returns
}

func (e *SpawnExpr) exprNode() {}

// ChannelExpr — channel(type) creates a new channel
type ChannelExpr struct {
	Pos      Pos
	ElemType Type
}

func (e *ChannelExpr) exprNode() {}

// ReceiveExpr — receive(handle_or_channel)
type ReceiveExpr struct {
	Pos    Pos
	Source Expr
}

func (e *ReceiveExpr) exprNode() {}

// Weak reference type registry
var (
	weakTypes  []Type        // inner type for each weak type ID
	weakByInner map[Type]Type // inner type → weak type ID
)

func init() {
	ResetWeakTypes()
}

func ResetWeakTypes() {
	weakTypes = nil
	weakByInner = make(map[Type]Type)
}

func WeakTypeOf(innerType Type) Type {
	if id, ok := weakByInner[innerType]; ok {
		return id
	}
	id := TypeWeakBase + Type(len(weakTypes))
	weakByInner[innerType] = id
	weakTypes = append(weakTypes, innerType)
	return id
}

func IsWeakType(t Type) bool {
	return t >= TypeWeakBase && t < TypeStructArrayBase
}

func WeakInnerType(t Type) Type {
	idx := int(t - TypeWeakBase)
	if idx < 0 || idx >= len(weakTypes) {
		return TypeVoid
	}
	return weakTypes[idx]
}

// Struct array type registry
var (
	structArrayTypes  []Type        // element type for each struct array type ID
	structArrayByElem map[Type]Type // element struct type → struct array type ID
)

func init() {
	ResetStructArrayTypes()
}

func ResetStructArrayTypes() {
	structArrayTypes = nil
	structArrayByElem = make(map[Type]Type)
}

func StructArrayTypeOf(elemType Type) Type {
	if id, ok := structArrayByElem[elemType]; ok {
		return id
	}
	id := TypeStructArrayBase + Type(len(structArrayTypes))
	structArrayByElem[elemType] = id
	structArrayTypes = append(structArrayTypes, elemType)
	return id
}

func IsStructArrayType(t Type) bool {
	return t >= TypeStructArrayBase && t < TypeOptionalBase
}

func StructArrayElemType(t Type) Type {
	idx := int(t - TypeStructArrayBase)
	if idx < 0 || idx >= len(structArrayTypes) {
		return TypeVoid
	}
	return structArrayTypes[idx]
}

// Optional type registry
var (
	optionalTypes   []Type        // inner type for each optional type ID
	optionalByInner map[Type]Type // inner type → optional type ID
)

func init() {
	ResetOptionalTypes()
}

func ResetOptionalTypes() {
	optionalTypes = nil
	optionalByInner = make(map[Type]Type)
}

func OptionalTypeOf(innerType Type) Type {
	if id, ok := optionalByInner[innerType]; ok {
		return id
	}
	id := TypeOptionalBase + Type(len(optionalTypes))
	optionalByInner[innerType] = id
	optionalTypes = append(optionalTypes, innerType)
	return id
}

func IsOptionalType(t Type) bool {
	return t >= TypeOptionalBase && t < TypeRefBase
}

func OptionalInnerType(t Type) Type {
	idx := int(t - TypeOptionalBase)
	if idx < 0 || idx >= len(optionalTypes) {
		return TypeVoid
	}
	return optionalTypes[idx]
}

// Reference type registry
var (
	refTypes   []Type        // inner type for each ref type ID
	refByInner map[Type]Type // inner type → ref type ID
)

func init() {
	ResetRefTypes()
}

func ResetRefTypes() {
	refTypes = nil
	refByInner = make(map[Type]Type)
}

func RefTypeOf(innerType Type) Type {
	if id, ok := refByInner[innerType]; ok {
		return id
	}
	id := TypeRefBase + Type(len(refTypes))
	refByInner[innerType] = id
	refTypes = append(refTypes, innerType)
	return id
}

func IsRefType(t Type) bool {
	return t >= TypeRefBase && t < TypeMapBase
}

func RefInnerType(t Type) Type {
	idx := int(t - TypeRefBase)
	if idx < 0 || idx >= len(refTypes) {
		return TypeVoid
	}
	return refTypes[idx]
}

// IsValueType returns true if the type is a value type (not heap-allocated).
func IsValueType(t Type) bool {
	return t == TypeInt || t == TypeBool || t == TypeLong || t == TypeDouble || t == TypeChar || IsEnumType(t)
}

// NullLit represents the null literal expression.
type NullLit struct {
	Pos Pos
}

func (e *NullLit) exprNode() {}

// Map type registry
type MapTypeInfo struct {
	KeyType   Type
	ValueType Type
}

var (
	mapTypes    []MapTypeInfo
	mapTypeKeys map[string]Type
)

func init() {
	ResetMapTypes()
}

func ResetMapTypes() {
	mapTypes = nil
	mapTypeKeys = make(map[string]Type)
}

func mapTypeKey(keyType, valueType Type) string {
	return fmt.Sprintf("map[%d,%d]", int(keyType), int(valueType))
}

func MapTypeOf(keyType, valueType Type) Type {
	key := mapTypeKey(keyType, valueType)
	if id, ok := mapTypeKeys[key]; ok {
		return id
	}
	id := TypeMapBase + Type(len(mapTypes))
	mapTypeKeys[key] = id
	mapTypes = append(mapTypes, MapTypeInfo{KeyType: keyType, ValueType: valueType})
	return id
}

func IsMapType(t Type) bool {
	return t >= TypeMapBase && t < TypeEnumBase
}

func MapKeyType(t Type) Type {
	idx := int(t - TypeMapBase)
	if idx < 0 || idx >= len(mapTypes) {
		return TypeVoid
	}
	return mapTypes[idx].KeyType
}

func MapValueType(t Type) Type {
	idx := int(t - TypeMapBase)
	if idx < 0 || idx >= len(mapTypes) {
		return TypeVoid
	}
	return mapTypes[idx].ValueType
}

// MapLitExpr represents an empty map literal: {}
type MapLitExpr struct {
	Pos     Pos
	MapType Type // set by checker from declared type context
}

func (e *MapLitExpr) exprNode() {}

// Enum type registry
type EnumDef struct {
	Name     string
	Variants []string
}

var (
	enumDefs   []EnumDef
	enumByName map[string]Type
)

func init() {
	ResetEnumTypes()
}

func ResetEnumTypes() {
	enumDefs = nil
	enumByName = make(map[string]Type)
}

func RegisterEnumType(def EnumDef) Type {
	if id, ok := enumByName[def.Name]; ok {
		// Update existing entry (e.g. placeholder from pre-scan gets real variants)
		idx := int(id - TypeEnumBase)
		if idx >= 0 && idx < len(enumDefs) {
			enumDefs[idx] = def
		}
		return id
	}
	id := TypeEnumBase + Type(len(enumDefs))
	enumByName[def.Name] = id
	enumDefs = append(enumDefs, def)
	return id
}

func LookupEnumType(name string) (Type, bool) {
	id, ok := enumByName[name]
	return id, ok
}

func GetEnumDef(t Type) *EnumDef {
	idx := int(t - TypeEnumBase)
	if idx < 0 || idx >= len(enumDefs) {
		return nil
	}
	return &enumDefs[idx]
}

func IsEnumType(t Type) bool {
	return t >= TypeEnumBase
}

func EnumName(t Type) string {
	def := GetEnumDef(t)
	if def == nil {
		return "unknown"
	}
	return def.Name
}

func AllEnumNames() []string {
	names := make([]string, 0, len(enumByName))
	for name := range enumByName {
		names = append(names, name)
	}
	return names
}

// EnumAccessExpr represents accessing an enum variant: Color.Red
type EnumAccessExpr struct {
	Pos      Pos
	EnumName string
	Variant  string
	EnumType Type
}

func (e *EnumAccessExpr) exprNode() {}

// Annotation constants
const (
	AnnotOwned       = "owned"
	AnnotRegion      = "region"
	AnnotNoEscape    = "noEscape"
	AnnotDebugCycles = "debug(cycles)"
)

// HasAnnotation checks if the given annotation is present in the list.
func HasAnnotation(annotations []string, name string) bool {
	for _, a := range annotations {
		if a == name {
			return true
		}
	}
	return false
}
