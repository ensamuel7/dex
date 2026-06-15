package ast

import "fmt"

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
)

// TypeInferred is used during parsing when a let statement has no explicit type annotation.
// The checker resolves this to the actual type from the RHS expression.
const TypeInferred Type = -1

// Struct type system: dynamic IDs starting at 1000
const TypeStructBase Type = 1000

// Channel type system: dynamic IDs starting at 2000
const TypeChanBase Type = 2000

// Task type system: dynamic IDs starting at 3000
const TypeTaskBase Type = 3000

// Function type system: dynamic IDs starting at 4000
const TypeFuncBase Type = 4000

type StructField struct {
	Name      string
	Type      Type
	IsPrivate bool
}

type StructDef struct {
	Name   string
	Fields []StructField
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
	id := TypeStructBase + Type(len(structDefs))
	structByName[def.Name] = id
	structDefs = append(structDefs, def)
	return id
}

func LookupStructType(name string) (Type, bool) {
	id, ok := structByName[name]
	return id, ok
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
	return t >= TypeFuncBase
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

func IsArrayType(t Type) bool {
	return t == TypeArrayInt || t == TypeArrayBool || t == TypeArrayString || t == TypeArrayLong || t == TypeArrayDouble
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
	default:
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
	default:
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
	Path string
}

type Program struct {
	Imports   []Import
	Structs   []StructDef
	Functions []Function
}

type Function struct {
	Name       string
	Params     []Param
	ReturnType Type
	Body       []Stmt
	IsPrivate  bool
}

type Param struct {
	Name string
	Type Type
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
	Name    string
	Type    Type
	Value   Expr
	IsConst bool
}

type ReturnStmt struct {
	Value Expr
}

type ExprStmt struct {
	Expr Expr
}

type IfStmt struct {
	Cond Expr
	Then []Stmt
	Else []Stmt
}

type WhileStmt struct {
	Cond Expr
	Body []Stmt
}

type BlockStmt struct {
	Stmts []Stmt
}

type AssignStmt struct {
	Name  string
	Value Expr
}

type IndexAssignStmt struct {
	Array Expr
	Index Expr
	Value Expr
}

type FieldAssignStmt struct {
	Object Expr
	Field  string
	Value  Expr
}

type BreakStmt struct{}

type ContinueStmt struct{}

type ForStmt struct {
	Init Stmt
	Cond Expr
	Post Stmt
	Body []Stmt
}

type ForeachStmt struct {
	Iterable Expr
	IndexVar string
	ValueVar string
	Body     []Stmt
}

type IncrementStmt struct {
	Name string
}

type DecrementStmt struct {
	Name string
}

type CompoundAssignStmt struct {
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
	Target Expr // nil for implicit (send to own task handle), non-nil for channel
	Value  Expr
}

func (s *SendStmt) stmtNode() {}

// Expressions

type IntLit struct {
	Value int
}

type FloatLit struct {
	Value float64
}

type BoolLit struct {
	Value bool
}

type StringLit struct {
	Value string
}

type Ident struct {
	Name string
}

type BinaryExpr struct {
	Op            BinOp
	Left          Expr
	Right         Expr
	LeftType      Type
	RightType     Type
	HasMixedTypes bool
}

type UnaryExpr struct {
	Op      UnaryOp
	Operand Expr
}

type CallExpr struct {
	Module       string // empty = user-defined function, "http"/"json"/"fmt" = stdlib
	Name         string
	Args         []Expr
	ResolvedType Type // set by checker for return-type polymorphism (e.g. db.col)
}

type ArrayLitExpr struct {
	ElemType Type
	Elems    []Expr
}

type IndexExpr struct {
	Array Expr
	Index Expr
}

type StructLitExpr struct {
	Name        string
	FieldNames  []string
	FieldValues []Expr
}

type FieldAccessExpr struct {
	Object Expr
	Field  string
}

func (e *IntLit) exprNode()          {}
func (e *FloatLit) exprNode()        {}
func (e *BoolLit) exprNode()         {}
func (e *StringLit) exprNode()       {}
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
	Body       []Stmt // block spawn: spawn { ... }
	Call       Expr   // call spawn: spawn fn(args) — must be *CallExpr
	ReturnType Type   // filled by checker: what send() sends or function returns
}

func (e *SpawnExpr) exprNode() {}

// ChannelExpr — channel(type) creates a new channel
type ChannelExpr struct {
	ElemType Type
}

func (e *ChannelExpr) exprNode() {}

// ReceiveExpr — receive(handle_or_channel)
type ReceiveExpr struct {
	Source Expr
}

func (e *ReceiveExpr) exprNode() {}
