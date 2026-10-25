package ast

import (
	"fmt"
	"sort"
	"strings"
)

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

// Type parameter system: dynamic IDs starting at 12000.
//
// A type parameter is a real registered type while a generic declaration is
// being parsed, which is what lets `fn max<T>(a: T, b: T): T` parse with the
// ordinary type machinery. It never survives into a checked program: every use
// is monomorphised to a concrete instantiation first, so reaching codegen with
// one is a compiler bug rather than a language feature.
const TypeParamBase Type = 12000

// Opaque C handle system: dynamic IDs starting at 13000.
const TypeHandleBase Type = 13000

type StructField struct {
	Name        string
	Type        Type
	IsPrivate   bool
	Annotations []string
	Doc         string // documentation string for hover

	// Tag is the backtick-delimited metadata written after the field's type,
	// stored verbatim. The compiler never reads it: an annotation such as
	// #[owned] is a directive that changes what is emitted, whereas a tag is
	// data for a library, reached at runtime through reflect.fieldTag. Keeping
	// the two apart is what lets annotations stay a closed, checked set.
	Tag string
}

type StructDef struct {
	// Name is the flat name the type is registered under. A type declared in a
	// user module carries its module: `alpha_Response`, the way a function
	// declared there becomes `alpha_make`. That is what lets two modules each
	// declare a `Response` — they are two types, not one — and what makes the
	// generated C name unique without any further work.
	Name string
	// Module is the module that declared it, empty for the main program and for
	// the struct types a stdlib module provides. Diagnostics read it so a
	// reader is shown `alpha.Response` rather than the flat spelling.
	Module            string
	Fields            []StructField
	Methods           []Function    // methods declared inside the struct body
	ConstructorParams []StructField // auto-constructor params: struct Foo(x: int) { ... }
	Doc               string        // documentation string for hover
	Pos               Pos
	Annotations       []string
	DeclaredOrder     bool
}

// QualifiedStructName is the flat name a module's type is registered under.
func QualifiedStructName(module, name string) string {
	if module == "" {
		return name
	}
	return module + "_" + name
}

// DisplayStructName turns a flat name back into what a reader wrote:
// `alpha_Response` is `alpha.Response`. Anything not declared by a module is
// returned unchanged.
func DisplayStructName(flat string) string {
	if t, ok := LookupStructType(flat); ok {
		if def := GetStructDef(t); def != nil && def.Module != "" {
			return def.Module + "." + strings.TrimPrefix(def.Name, def.Module+"_")
		}
	}
	if t, ok := LookupEnumType(flat); ok {
		if def := GetEnumDef(t); def != nil && def.Module != "" {
			return def.Module + "." + strings.TrimPrefix(def.Name, def.Module+"_")
		}
	}
	if t, ok := LookupInterfaceType(flat); ok {
		if def := GetInterfaceDef(t); def != nil && def.Module != "" {
			return def.Module + "." + strings.TrimPrefix(def.Name, def.Module+"_")
		}
	}
	return flat
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
	reflectedStructs = map[string]bool{}
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

// AllEnumNames returns the names of all registered enum types.
func AllEnumNames() []string {
	names := make([]string, 0, len(enumByName))
	for name := range enumByName {
		names = append(names, name)
	}
	return names
}

// AllInterfaceNames is every registered interface's flat name.
func AllInterfaceNames() []string {
	names := make([]string, 0, len(interfaceByName))
	for name := range interfaceByName {
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
	// A payload-carrying enum is a refcounted pointer to a tagged union, which
	// is what makes a recursive variant representable at all. Saying so here
	// rather than special-casing it everywhere is what gives it retain and
	// release, scope cleanup, the unwind stack, array elements and struct
	// fields without a single one of those sites knowing about enums. A plain
	// enum is still an integer and still answers false.
	return t == TypeString || t == TypeStringBuilder || t == TypeJsonValue || IsArrayType(t) || IsChanType(t) || IsTaskType(t) || IsWeakType(t) || IsMapType(t) || IsFuncType(t) || IsPayloadEnumType(t) || IsHandleType(t)
}

func IsHeapOptionalType(t Type) bool {
	return IsOptionalType(t) && IsHeapType(OptionalInnerType(t))
}

func IsStructOptionalType(t Type) bool {
	return IsOptionalType(t) && IsStructType(OptionalInnerType(t))
}

func NeedsRelease(t Type) bool {
	if IsStructOptionalType(t) {
		return true
	}
	if IsOptionalType(t) {
		return NeedsRelease(OptionalInnerType(t))
	}
	if IsHeapType(t) {
		return true
	}
	// An interface value owns the box holding the struct it was built from. It
	// is not IsHeapType: the value is two pointers and a table passed by value,
	// so the release goes through its _obj field rather than through the value
	// itself, and every site that emits one special-cases it.
	if IsInterfaceType(t) {
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
		// An interface value is a multi-word value like a struct, so it rides
		// the same by-value array: the element is memcpy'd and the array's
		// cleanup releases the box each one owns.
		if IsInterfaceType(elem) {
			return StructArrayTypeOf(elem)
		}
		// `T[]` in a generic signature. The entry exists only so inference can
		// read the element back out and match it against the argument's; the
		// instantiation re-parses with T bound, which produces a real array
		// type. One reaching codegen would be a hole in instantiation, which
		// is what Checker.rejectTypeParams is for.
		if IsTypeParamType(elem) {
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
	Pos   Pos
	Path  string
	Alias string // if set, use as module name instead of filepath.Base(Path)
}

type Program struct {
	Imports       []Import
	Structs       []StructDef
	Enums         []EnumDef
	GlobalLets    []LetStmt // module-level let/const declarations
	Functions     []Function
	Interfaces    []InterfaceDef
	UserModules   []string // module identities of resolved user imports
	ModuleAliases map[string]string
	StructModule  map[string]string // structName -> moduleName (for cross-module structs)
	// Generic declarations are kept as the tokens they were written as rather
	// than as a parsed AST, because instantiating one means re-parsing it with
	// its type parameters bound to concrete types. Substituting over a parsed
	// body would mean a deep copy and a type remap of all forty-nine node
	// kinds; re-parsing reuses the type resolution the parser already does.
	GenericFuncs   []GenericDecl
	GenericStructs []GenericDecl
	// Externals are the C modules this compilation declares. They are carried on
	// the program rather than registered in the stdlib registry, which is a
	// package global populated at init and never reset: the language server is
	// one process compiling many files, so a module registered there would
	// outlive the file that declared it and could shadow a real one.
	Externals []ExternalModule
}

// ExternalModule is an `external module` block: a set of C functions to call, the
// headers they need, and the flags to link against.
type ExternalModule struct {
	Pos      Pos
	Name     string
	Local    string
	Dir      string
	Includes []string
	Links    []string
	Packages []string
	Types    []ExternalType
	Funcs    []ExternalFunc
}

// ExternalType is `type Name closed by "cfunc"`: an opaque C pointer the
// program passes around but never dereferences. Closer is the C function that
// closes it, which becomes the box's destroy hook.
type ExternalType struct {
	Pos    Pos
	Name   string
	Closer string
}

// ExternalFunc is one declaration inside an external block. CName is the symbol to
// call, which defaults to the declared name when the `= "symbol"` form is left
// off.
type ExternalFunc struct {
	Pos        Pos
	Name       string
	CName      string
	Params     []Type
	ParamNames []string
	ReturnType Type
	// OwnedReturn is the `owned` modifier on the return type: the C function
	// malloc'd the string it returned and Dex frees it. Without it the buffer
	// is copied and left alone, which is what getenv and strerror need.
	OwnedReturn bool
	ReturnFreer string
}

// GenericDecl is a generic declaration held as tokens, with the names of its
// type parameters and the identifier it declares.
type GenericDecl struct {
	Pos        Pos
	Name       string
	TypeParams []string
	Tokens     []TokenLike
	// Module is the user module the declaration was read from, so an
	// instantiation registers where the template did.
	Module string
	// Params and ReturnType are the signature with its type parameters left
	// standing, parsed once at capture. Inference needs it: a call site knows
	// the argument types and has to learn which parameter each one binds.
	// The body is not parsed here — only this signature is.
	Params     []Param
	ReturnType Type
}

// TokenLike is the token shape a GenericDecl carries. The ast package cannot
// import the token package without a cycle, so the fields the re-parse needs
// are mirrored here and converted back by the parser.
type TokenLike struct {
	Kind  int
	Value string
	Line  int
	Col   int
	File  string
}

type Function struct {
	Pos         Pos
	Name        string
	Params      []Param
	ReturnType  Type
	Body        []Stmt
	IsPrivate   bool
	Annotations []string
	// Throws records a `throws` in the signature: this function is allowed to
	// let an exception escape. It is a declaration, not a derived fact — the
	// lint pass infers what actually throws and compares the two.
	Throws bool
	// ThrowsTypes are the types named after `throws`, in the order written. A
	// bare `throws` leaves this empty, which declares that something escapes
	// without saying what.
	ThrowsTypes []string
}

type Param struct {
	Pos          Pos
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
	ElemType Type
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

// CatchClause is one handler: catch (e: ParseError) { ... }. A clause naming
// Exception catches everything, which is what every catch did before types
// existed and so what keeps existing code meaning what it meant.
type CatchClause struct {
	Pos      Pos
	VarName  string // the name the exception is bound to
	TypeName string // the type as written
	Type     Type   // resolved by the checker
	Body     []Stmt
}

// IsCatchAll reports whether this clause handles every exception type.
func (c CatchClause) IsCatchAll() bool { return c.TypeName == "Exception" }

// TryCatchStmt — try { ... } catch (e: ParseError) { ... } catch (e: Exception) { ... } finally { ... }
type TryCatchStmt struct {
	Pos         Pos
	Body        []Stmt        // try body
	Catches     []CatchClause // handlers, in the order written
	FinallyBody []Stmt        // finally body (nil if no finally)
}

// CatchesAnything reports whether any clause is a catch-all, which is what
// decides whether an exception can still escape the try.
func (s *TryCatchStmt) CatchesAnything() bool {
	for _, c := range s.Catches {
		if c.IsCatchAll() {
			return true
		}
	}
	return false
}

func (s *TryCatchStmt) stmtNode() {}

// ThrowStmt — throw Exception("message")
type ThrowStmt struct {
	Pos   Pos
	Value Expr
	// ValueType is the thrown value's type, set by the checker. Lint reads it
	// to name what a throw raises: `throw e` and `throw makeErr()` carry their
	// type nowhere in the syntax.
	ValueType Type
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
	Pos   Pos
	Tag   Expr
	Cases []SwitchCase
	// Default is the default arm's body, and is nil both when there is no
	// default and when there is an empty one. HasDefault is what tells those
	// apart, which the exhaustiveness check over a payload enum needs: a
	// `default: {}` is a deliberate catch-all, not a missing arm.
	Default    []Stmt
	HasDefault bool
}

func (s *SwitchStmt) stmtNode() {}

// ThrowableStruct reports whether a type can be thrown, and when it cannot,
// why. A throwable struct carries a `message: string`, which is the one field
// every handler can read, and nothing else that would need releasing: a throw
// is a longjmp, so the frames between it and the catch never run their
// releases, and only the message is staged somewhere that outlives the unwind.
// An int or a bool rides along in the staged payload; a second string, an
// array or a nested struct holding either could only arrive already freed.
func ThrowableStruct(t Type) (ok bool, reason string) {
	if !IsStructType(t) {
		return false, "only a struct can be thrown"
	}
	def := GetStructDef(t)
	if def == nil {
		return false, "unknown struct type"
	}
	hasMessage := false
	for _, f := range def.Fields {
		if f.Name == "message" {
			if f.Type != TypeString {
				return false, fmt.Sprintf("'%s.message' must be a string, got %s", def.Name, typeLabel(f.Type))
			}
			hasMessage = true
			continue
		}
		if NeedsRelease(f.Type) {
			return false, fmt.Sprintf("field '%s' cannot cross a throw: only 'message' and fields that need no releasing survive the unwind", f.Name)
		}
	}
	if !hasMessage {
		return false, fmt.Sprintf("'%s' has no 'message: string' field", def.Name)
	}
	return true, ""
}

// typeLabel names a type for a diagnostic. The checker has a fuller version of
// this; ast cannot import it, and what is needed here is only a name.
func typeLabel(t Type) string {
	switch t {
	case TypeInt:
		return "int"
	case TypeBool:
		return "bool"
	case TypeString:
		return "string"
	case TypeLong:
		return "long"
	case TypeDouble:
		return "double"
	case TypeChar:
		return "char"
	case TypeVoid:
		return "void"
	}
	if IsStructType(t) {
		return StructName(t)
	}
	return "a non-string type"
}

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
	// IsInterfaceCall marks a call dispatched through an interface value's
	// vtable rather than to a known struct's flattened method. StructType holds
	// the interface type in that case.
	IsInterfaceCall bool
	// Recv is the receiver of a method call whose receiver is an arbitrary
	// expression rather than a plain variable name, as in parsed[0].asInt().
	// When it is nil the receiver, if any, is named by Module.
	Recv Expr
	// ResolvedKey is the tag key db.fill() reads, set by the checker. A struct
	// read by two projections gets a filler per key.
	ResolvedKey string
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
	// Name is flat, and carries the declaring module the way a struct's does:
	// see StructDef.Name.
	Name string
	// Module is the module that declared it, empty for the main program.
	Module  string
	Methods []InterfaceMethod
}

// Annotation constants
const (
	AnnotOwned       = "owned"
	AnnotRegion      = "region"
	AnnotNoEscape    = "noEscape"
	AnnotDebugCycles = "debug(cycles)"
	// AnnotAllowPrefix introduces a lint suppression: #[allow(unused-var)].
	// Unlike the others it is not a directive to the compiler — it says what
	// not to report — so the checker lets it through untouched and lint is
	// what reads it.
	AnnotAllowPrefix = "allow("
)

// ParseAllowAnnotation recognises #[allow(<rule>)] and returns the rule named
// inside it.
func ParseAllowAnnotation(annotation string) (string, bool) {
	if !strings.HasPrefix(annotation, AnnotAllowPrefix) || !strings.HasSuffix(annotation, ")") {
		return "", false
	}
	return annotation[len(AnnotAllowPrefix) : len(annotation)-1], true
}

// HasAnnotation checks if the given annotation is present in the list.
func HasAnnotation(annotations []string, name string) bool {
	for _, a := range annotations {
		if a == name {
			return true
		}
	}
	return false
}

// --- Reflection metadata registry -------------------------------------------

// reflectedStructs names the structs some reflect.* call takes as its subject.
// The checker records a name here as it resolves each call; codegen reads the
// set and emits a name table, a kind table and the typed accessors for exactly
// those structs.
//
// It exists so that reflection costs nothing it is not asked for. A program
// that never imports reflect carries no tables, and one that reflects over a
// single struct carries one struct's worth — rather than every struct in the
// program paying for metadata on the chance someone looks at it.
var reflectedStructs = map[string]bool{}

// MarkReflected records that name is the subject of a reflect.* call.
func MarkReflected(name string) {
	reflectedStructs[name] = true
}

// IsReflected reports whether any reflect.* call named this struct, and so
// whether codegen owes it metadata.
func IsReflected(name string) bool {
	return reflectedStructs[name]
}

// ReflectedStructCount reports how many structs carry metadata, for tests that
// assert reflection stayed opt-in.
func ReflectedStructCount() int {
	return len(reflectedStructs)
}

// StmtPos returns the source position of a statement. The Stmt interface
// deliberately carries no methods, so the position lives in a field that only a
// type switch can reach; diagnostics and the debug memory checker both need it
// generically.
func StmtPos(stmt Stmt) Pos {
	switch s := stmt.(type) {
	case *LetStmt:
		return s.Pos
	case *ReturnStmt:
		return s.Pos
	case *ExprStmt:
		return s.Pos
	case *IfStmt:
		return s.Pos
	case *WhileStmt:
		return s.Pos
	case *BlockStmt:
		return s.Pos
	case *AssignStmt:
		return s.Pos
	case *IndexAssignStmt:
		return s.Pos
	case *FieldAssignStmt:
		return s.Pos
	case *BreakStmt:
		return s.Pos
	case *ContinueStmt:
		return s.Pos
	case *ForStmt:
		return s.Pos
	case *ForeachStmt:
		return s.Pos
	case *IncrementStmt:
		return s.Pos
	case *DecrementStmt:
		return s.Pos
	case *CompoundAssignStmt:
		return s.Pos
	case *SendStmt:
		return s.Pos
	case *TryCatchStmt:
		return s.Pos
	case *ThrowStmt:
		return s.Pos
	case *SwitchStmt:
		return s.Pos
	case *DeferStmt:
		return s.Pos
	case *DestructureLetStmt:
		return s.Pos
	}
	return Pos{}
}

// TagValue returns the value a field tag gives for one key, and whether the key
// was present at all — which is distinct from a key whose value is empty.
//
// The format is the one Go uses: space-separated `key:"value"` pairs, so one
// tag can carry several namespaces. Values are verbatim; a tag is raw text, so
// a value cannot itself contain a double quote.
//
// The compiler does not interpret any key — that is a library's business — but
// it does need to *read* one: db.fill has to know which fields a `db:"-"`
// leaves out of a projection, or the columns after such a field would be read
// one position out.
func TagValue(tag, key string) (string, bool) {
	i := 0
	for i < len(tag) {
		for i < len(tag) && (tag[i] == ' ' || tag[i] == '\t') {
			i++
		}
		if i >= len(tag) {
			break
		}
		keyStart := i
		for i < len(tag) && tag[i] != ':' && tag[i] != ' ' {
			i++
		}
		found := tag[keyStart:i]
		if i+1 >= len(tag) || tag[i] != ':' || tag[i+1] != '"' {
			// Not a well-formed pair; skip to the next space rather than
			// guessing what was meant. The checker reports the malformed tag.
			for i < len(tag) && tag[i] != ' ' {
				i++
			}
			continue
		}
		i += 2 // consume :"
		valStart := i
		for i < len(tag) && tag[i] != '"' {
			i++
		}
		val := tag[valStart:i]
		if i < len(tag) {
			i++ // consume closing quote
		}
		if found == key {
			return val, true
		}
	}
	return "", false
}

// Row fillers are emitted per struct *and* per tag key, because a struct can
// be read by more than one projection — db.fill(out, rows, "fleet") skips the
// fields that key marks with "-", which are not the ones "db" marks.
var dbFillKeys = map[string]map[string]bool{}

func MarkDbFilled(structName, key string) {
	if dbFillKeys[structName] == nil {
		dbFillKeys[structName] = map[string]bool{}
	}
	dbFillKeys[structName][key] = true
}

// DbFillKeys lists the tag keys a struct is filled by, in a stable order so the
// generated C does not churn between builds.
func DbFillKeys(structName string) []string {
	keys := make([]string, 0, len(dbFillKeys[structName]))
	for k := range dbFillKeys[structName] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func ResetDbFillKeys() { dbFillKeys = map[string]map[string]bool{} }
