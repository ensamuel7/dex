package ast

import "fmt"

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
	weakTypes   []Type        // inner type for each weak type ID
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
	// AsJsonValue is set by the checker when `{}` appears where a json.Value is
	// expected, making it an empty JSON object rather than an empty map.
	AsJsonValue bool
}

func (e *MapLitExpr) exprNode() {}

// Enum type registry
type EnumDef struct {
	// Name is flat, and carries the declaring module the way a struct's does:
	// see StructDef.Name.
	Name string
	// Module is the module that declared it, empty for the main program.
	Module   string
	Variants []string
	// Payloads is parallel to Variants: Payloads[i] are the field types the
	// i-th variant carries, and is empty for a variant that carries none.
	//
	// A plain enum -- every entry empty -- stays exactly what it was, an
	// integer constant with no representation of its own. One with payloads is
	// a different thing: a refcounted tagged union, because a variant whose
	// field is the enum itself has no finite size otherwise. Keeping the two
	// apart is what lets every existing use of an enum go on working.
	Payloads [][]Type
}

// HasPayload reports whether any variant carries fields, which is what decides
// between the two representations.
func (e *EnumDef) HasPayload() bool {
	for _, p := range e.Payloads {
		if len(p) > 0 {
			return true
		}
	}
	return false
}

// VariantPayload returns the field types of a named variant, and whether the
// variant exists at all.
func (e *EnumDef) VariantPayload(name string) ([]Type, bool) {
	for i, v := range e.Variants {
		if v == name {
			if i < len(e.Payloads) {
				return e.Payloads[i], true
			}
			return nil, true
		}
	}
	return nil, false
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
	return t >= TypeEnumBase && t < TypeInterfaceBase
}

// IsPayloadEnumType separates the tagged union from the integer constant. Every
// site that assumed an enum is an int has to ask this before it still can.
func IsPayloadEnumType(t Type) bool {
	if !IsEnumType(t) {
		return false
	}
	def := GetEnumDef(t)
	return def != nil && def.HasPayload()
}

func EnumName(t Type) string {
	def := GetEnumDef(t)
	if def == nil {
		return "unknown"
	}
	return def.Name
}

// EnumAccessExpr represents accessing an enum variant: Color.Red
type EnumAccessExpr struct {
	Pos      Pos
	EnumName string
	Variant  string
	EnumType Type
}

func (e *EnumAccessExpr) exprNode() {}

// EnumCtorExpr builds a payload-carrying variant: Expr.Lit(5), Expr.Add(a, b).
// A variant that carries nothing is still an EnumAccessExpr, so a plain enum
// never reaches this node.
type EnumCtorExpr struct {
	Pos      Pos
	EnumName string
	Variant  string
	EnumType Type
	Args     []Expr
}

func (e *EnumCtorExpr) exprNode() {}

// EnumPatternExpr is a switch case that destructures a variant:
// `case Expr.Add(l, r):` matches the tag and binds the payload to l and r for
// the arm's body. Binds is parallel to the variant's declared fields; "_"
// discards one.
type EnumPatternExpr struct {
	Pos      Pos
	EnumName string
	Variant  string
	EnumType Type
	Binds    []string
}

func (e *EnumPatternExpr) exprNode() {}

// Interface type registry
var (
	interfaceDefs   []InterfaceDef
	interfaceByName map[string]Type
)

func init() {
	ResetInterfaceTypes()
	ResetTypeParams()
}

func ResetInterfaceTypes() {
	interfaceDefs = nil
	interfaceByName = make(map[string]Type)
}

func RegisterInterfaceType(def InterfaceDef) Type {
	if id, ok := interfaceByName[def.Name]; ok {
		idx := int(id - TypeInterfaceBase)
		if idx >= 0 && idx < len(interfaceDefs) {
			interfaceDefs[idx] = def
		}
		return id
	}
	id := TypeInterfaceBase + Type(len(interfaceDefs))
	interfaceByName[def.Name] = id
	interfaceDefs = append(interfaceDefs, def)
	return id
}

func LookupInterfaceType(name string) (Type, bool) {
	id, ok := interfaceByName[name]
	return id, ok
}

func GetInterfaceDef(t Type) *InterfaceDef {
	idx := int(t - TypeInterfaceBase)
	if idx < 0 || idx >= len(interfaceDefs) {
		return nil
	}
	return &interfaceDefs[idx]
}

func IsInterfaceType(t Type) bool {
	return t >= TypeInterfaceBase && t < TypeParamBase
}

// Type parameter registry.
//
// Parameters are interned by name, so `T` in one generic declaration and `T` in
// another are the same Type. That is harmless because a parameter only ever
// appears while its own declaration is being parsed or re-parsed, and the
// substitution map that binds it is per-instantiation.
var (
	typeParamNames  []string
	typeParamByName map[string]Type
)

func init() {
	ResetTypeParams()
}

func ResetTypeParams() {
	typeParamNames = nil
	typeParamByName = make(map[string]Type)
}

func RegisterTypeParam(name string) Type {
	if id, ok := typeParamByName[name]; ok {
		return id
	}
	id := TypeParamBase + Type(len(typeParamNames))
	typeParamByName[name] = id
	typeParamNames = append(typeParamNames, name)
	return id
}

func IsTypeParamType(t Type) bool {
	return t >= TypeParamBase
}

func TypeParamName(t Type) string {
	idx := int(t - TypeParamBase)
	if idx < 0 || idx >= len(typeParamNames) {
		return "unknown"
	}
	return typeParamNames[idx]
}

func InterfaceName(t Type) string {
	def := GetInterfaceDef(t)
	if def == nil {
		return "unknown"
	}
	return def.Name
}

// A handle is an opaque C pointer the program may pass around but never
// dereference. It is a refcounted box rather than a bare pointer: a box can be
// stored, returned and captured, and the release machinery already closes one
// when its last reference goes — including when an exception unwinds past the
// frame holding it, which is the case that otherwise leaks a connection.
//
// Rust reaches move-only semantics here because move is its default for any
// type with a destructor. Refcounting is this language's default, so a handle
// follows it, and no new ownership rule has to be invented or enforced.
type handleInfo struct {
	Module string
	Name   string
	Closer string // the C function that closes it
}

var (
	handleTypes  []handleInfo
	handleByName = map[string]Type{}
)

// HandleTypeOf registers or returns the type for a module's handle.
func HandleTypeOf(module, name, closer string) Type {
	key := module + "." + name
	if id, ok := handleByName[key]; ok {
		return id
	}
	id := TypeHandleBase + Type(len(handleTypes))
	handleByName[key] = id
	handleTypes = append(handleTypes, handleInfo{Module: module, Name: name, Closer: closer})
	return id
}

func LookupHandleType(module, name string) (Type, bool) {
	id, ok := handleByName[module+"."+name]
	return id, ok
}

func IsHandleType(t Type) bool {
	return t >= TypeHandleBase && t < TypeHandleBase+1000
}

func HandleName(t Type) string {
	idx := int(t - TypeHandleBase)
	if idx < 0 || idx >= len(handleTypes) {
		return ""
	}
	return handleTypes[idx].Name
}

func HandleModule(t Type) string {
	idx := int(t - TypeHandleBase)
	if idx < 0 || idx >= len(handleTypes) {
		return ""
	}
	return handleTypes[idx].Module
}

// HandleCloser is the C function that closes the handle, which becomes the
// box's destroy hook.
func HandleCloser(t Type) string {
	idx := int(t - TypeHandleBase)
	if idx < 0 || idx >= len(handleTypes) {
		return ""
	}
	return handleTypes[idx].Closer
}

// ResetHandleTypes clears the registry between compilations, the way the other
// per-program type registries are cleared: a handle belongs to the external
// block that declared it, and the language server compiles many files in one
// process.
func ResetHandleTypes() {
	handleTypes = nil
	handleByName = map[string]Type{}
}

// WiderNumericType is the result type of an arithmetic expression over two
// numeric types. It lives here rather than in the checker because codegen needs
// the same answer: the checker records the operand types on the node and
// codegen reads them back, and two copies of this rank would drift.
func WiderNumericType(a, b Type) Type {
	rank := map[Type]int{TypeChar: 0, TypeInt: 1, TypeLong: 2, TypeDouble: 3}
	if rank[a] >= rank[b] {
		return a
	}
	return b
}
