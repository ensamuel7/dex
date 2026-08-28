package ast

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
	TypeMutex
	// TypeJsonValue is the json module's dynamic JSON document type, written
	// `json.Value` in source. It is a heap value like string and arrays.
	TypeJsonValue
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

// Interface type system: dynamic IDs starting at 11000
const TypeInterfaceBase Type = 11000

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

func IsHeapType(t Type) bool {
	return t == TypeString || t == TypeStringBuilder || t == TypeJsonValue || IsArrayType(t) || IsChanType(t) || IsTaskType(t) || IsWeakType(t) || IsMapType(t) || IsFuncType(t)
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
	GlobalLets   []LetStmt         // module-level let/const declarations
	Functions    []Function
	Interfaces   []InterfaceDef
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
	Name         string
	Type         Type
	Annotations  []string
	DefaultValue Expr // nil = no default
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
	Name        string   // single declaration
	Names       []string // multi-declaration (let x, y, z: int = 0)
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
	Pos           Pos
	Module        string // empty = user-defined function, "http"/"json"/"fmt" = stdlib
	Name          string
	Args          []Expr
	ArgNames      []string // parallel to Args; empty string = positional (set by parser for named args)
	ResolvedType  Type     // set by checker for return-type polymorphism (e.g. db.col)
	IsMethodCall  bool     // set by checker: instance.method() call
	IsConstructor bool     // set by checker: StructName(args) constructor call
	StructType    Type     // set by checker: the struct type for method/constructor calls
	// Recv is the receiver of a method call whose receiver is an arbitrary
	// expression rather than a plain variable name, as in parsed[0].asInt().
	// When it is nil the receiver, if any, is named by Module.
	Recv Expr
}

type ArrayLitExpr struct {
	Pos      Pos
	ElemType Type
	Elems    []Expr
	// AsJsonValue is set by the checker when the literal is being built as a
	// json.Value rather than a typed array, which is what allows the elements to
	// have differing types.
	AsJsonValue bool
}

// ObjectLitExpr is a JSON object literal: { name: "Dex", version: 1 }. Keys are
// bare identifiers or string literals. It always has type json.Value — the
// language has no other object literal — so it needs no type annotation to be
// understood, including when nested inside another literal.
type ObjectLitExpr struct {
	Pos    Pos
	Keys   []string
	Values []Expr
}

type IndexExpr struct {
	Pos   Pos
	Array Expr
	Index Expr
}

type SliceExpr struct {
	Pos   Pos
	Array Expr
	Start Expr // nil means 0
	End   Expr // nil means len
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
	// IsMethodValue is set by the checker when Field names a method rather than
	// a field: the expression evaluates to that method bound to Object, the way
	// Go's method values work. StructType is the receiver's type.
	IsMethodValue bool
	StructType    Type
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
func (e *ObjectLitExpr) exprNode()   {}
func (e *IndexExpr) exprNode()       {}
func (e *SliceExpr) exprNode()       {}
func (e *StructLitExpr) exprNode()   {}
func (e *FieldAccessExpr) exprNode() {}

// NullLit represents the null literal expression.
type NullLit struct {
	Pos Pos
}

func (e *NullLit) exprNode() {}

// MutexLit represents the `mutex` literal expression, which yields a fresh
// unlocked mutex: `let mu: mutex = mutex`.
type MutexLit struct {
	Pos Pos
}

func (e *MutexLit) exprNode() {}

// DeferStmt represents a defer statement: defer expr
// The expression is executed when the enclosing function returns.
type DeferStmt struct {
	Pos  Pos
	Expr Expr // expression (typically a function call) to execute on function exit
}

func (s *DeferStmt) stmtNode() {}

// StringInterpExpr represents a string interpolation expression: "Hello, ${name}!"
// Parts alternates between StringLit (text segments) and interpolated expressions.
type StringInterpExpr struct {
	Pos   Pos
	Parts []Expr // alternating StringLit and interpolated expressions
}

func (e *StringInterpExpr) exprNode() {}

// MatchArm represents a single arm in a match expression.
type MatchArm struct {
	Pos        Pos
	Patterns   []Expr // match values; nil for wildcard
	IsWildcard bool
	Body       Expr // result expression
}

// MatchExpr represents a match expression: match(value) { 1 => "one", _ => "other" }
type MatchExpr struct {
	Pos  Pos
	Tag  Expr
	Arms []MatchArm
	Type Type // resolved result type, set by checker
}

func (e *MatchExpr) exprNode() {}

// DestructureLetStmt represents destructuring: let { name, age } = person
type DestructureLetStmt struct {
	Pos     Pos
	Names   []string // variable names to bind
	Value   Expr     // RHS expression (must be struct type)
	IsConst bool
}

func (s *DestructureLetStmt) stmtNode() {}

// LambdaExpr represents a lambda/closure expression: fn(x: int): int { return x + 1 }
type LambdaExpr struct {
	Pos        Pos
	Params     []Param
	ReturnType Type
	Body       []Stmt
}

func (e *LambdaExpr) exprNode() {}

// InterfaceMethod describes a method signature in an interface definition.
type InterfaceMethod struct {
	Name       string
	Params     []Type
	ReturnType Type
}

// InterfaceDef defines an interface with structural typing.
type InterfaceDef struct {
	Name    string
	Methods []InterfaceMethod
}

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
