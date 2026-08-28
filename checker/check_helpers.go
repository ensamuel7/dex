package checker

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

// extractNullCheck detects null comparison patterns in conditions.
// Returns (varName, isNotNull) where isNotNull=true means "x != null".
func extractNullCheck(cond ast.Expr) (string, bool) {
	binExpr, ok := cond.(*ast.BinaryExpr)
	if !ok {
		return "", false
	}
	if binExpr.Op != ast.BinEq && binExpr.Op != ast.BinNeq {
		return "", false
	}
	// Check x != null or x == null
	if ident, ok := binExpr.Left.(*ast.Ident); ok {
		if _, ok := binExpr.Right.(*ast.NullLit); ok {
			return ident.Name, binExpr.Op == ast.BinNeq
		}
	}
	// Check null != x or null == x
	if _, ok := binExpr.Left.(*ast.NullLit); ok {
		if ident, ok := binExpr.Right.(*ast.Ident); ok {
			return ident.Name, binExpr.Op == ast.BinNeq
		}
	}
	return "", false
}

func isValidFieldType(t ast.Type) bool {
	if ast.IsOptionalType(t) {
		return isValidFieldType(ast.OptionalInnerType(t))
	}
	if ast.IsRefType(t) {
		inner := ast.RefInnerType(t)
		return ast.IsStructType(inner) || ast.IsValueType(inner)
	}
	switch t {
	case ast.TypeInt, ast.TypeBool, ast.TypeString, ast.TypeLong, ast.TypeDouble, ast.TypeChar, ast.TypeMutex, ast.TypeJsonValue:
		return true
	default:
		return ast.IsStructType(t) || ast.IsEnumType(t) || ast.IsMapType(t) || ast.IsArrayType(t)
	}
}

func widerNumericType(a, b ast.Type) ast.Type {
	rank := map[ast.Type]int{ast.TypeChar: 0, ast.TypeInt: 1, ast.TypeLong: 2, ast.TypeDouble: 3}
	if rank[a] >= rank[b] {
		return a
	}
	return b
}

func isNumericType(t ast.Type) bool {
	return t == ast.TypeChar || t == ast.TypeInt || t == ast.TypeLong || t == ast.TypeDouble
}

func isStringifiable(t ast.Type) bool {
	switch t {
	case ast.TypeInt, ast.TypeLong, ast.TypeDouble, ast.TypeBool, ast.TypeChar:
		return true
	}
	if ast.IsRefType(t) {
		return isStringifiable(ast.RefInnerType(t))
	}
	return ast.IsStructType(t) || ast.IsEnumType(t) || ast.IsArrayType(t)
}

func isPrimitiveType(t ast.Type) bool {
	return t == ast.TypeInt || t == ast.TypeBool || t == ast.TypeString ||
		t == ast.TypeChar || t == ast.TypeLong || t == ast.TypeDouble || ast.IsEnumType(t)
}

// isIntLiteral returns true if the expression is an integer literal.
func isIntLiteral(expr ast.Expr) bool {
	_, ok := expr.(*ast.IntLit)
	return ok
}

// isCharLiteral returns true if the expression is a char literal.
func isCharLiteral(expr ast.Expr) bool {
	_, ok := expr.(*ast.CharLit)
	return ok
}

// validateAnnotations checks that annotations are valid and compatible with the type.
func validateAnnotations(annotations []string, typ ast.Type, name string) error {
	if len(annotations) == 0 {
		return nil
	}

	// Validate each annotation is known
	validAnnotations := map[string]bool{
		ast.AnnotOwned:       true,
		ast.AnnotRegion:      true,
		ast.AnnotNoEscape:    true,
		ast.AnnotDebugCycles: true,
	}
	memAnnotCount := 0
	for _, a := range annotations {
		if !validAnnotations[a] {
			return fmt.Errorf("unknown annotation '#[%s]' on '%s'", a, name)
		}
		if a == ast.AnnotOwned || a == ast.AnnotRegion || a == ast.AnnotNoEscape {
			memAnnotCount++
		}
	}

	// Mutual exclusivity: only one of owned/region/noEscape
	if memAnnotCount > 1 {
		return fmt.Errorf("annotations #[owned], #[region], and #[noEscape] are mutually exclusive on '%s'", name)
	}

	// Annotations only on heap types
	if !ast.IsHeapType(typ) {
		return fmt.Errorf("annotations are only allowed on heap types (string, arrays, channels), got %s on '%s'", typeName(typ), name)
	}

	return nil
}

// canAssign checks if an expression of exprType can be assigned to a target of targetType,
// allowing implicit widening of int literals to long/double and float literals to double.
func canAssign(targetType, exprType ast.Type, expr ast.Expr) bool {
	if exprType == targetType {
		return true
	}
	// null -> any optional type
	if exprType == ast.TypeNull && ast.IsOptionalType(targetType) {
		return true
	}
	// T -> T? (wrapping non-optional into optional)
	if ast.IsOptionalType(targetType) {
		inner := ast.OptionalInnerType(targetType)
		if exprType == inner {
			return true
		}
		// Allow widening into optional (e.g. int literal -> long?)
		if canAssign(inner, exprType, expr) {
			return true
		}
	}
	// int literal -> long or double
	if isIntLiteral(expr) && (targetType == ast.TypeLong || targetType == ast.TypeDouble) {
		return true
	}
	// char literal -> int, long, or double
	if isCharLiteral(expr) && (targetType == ast.TypeInt || targetType == ast.TypeLong || targetType == ast.TypeDouble) {
		return true
	}
	// weak<T> accepts T (wrapping a strong ref into a weak ref)
	if ast.IsWeakType(targetType) && exprType == ast.WeakInnerType(targetType) {
		return true
	}
	// T -> &T (implicit address-of: pass struct value to ref param)
	if ast.IsRefType(targetType) && exprType == ast.RefInnerType(targetType) {
		return true
	}
	// struct -> interface (structural typing)
	if ast.IsInterfaceType(targetType) && ast.IsStructType(exprType) {
		return structSatisfiesInterface(exprType, targetType)
	}
	// float literal -> double (already types as double, but keep for clarity)
	return false
}

// structSatisfiesInterface checks if a struct type has all methods required by an interface.
func structSatisfiesInterface(structType, ifaceType ast.Type) bool {
	ifaceDef := ast.GetInterfaceDef(ifaceType)
	if ifaceDef == nil {
		return false
	}
	structDef := ast.GetStructDef(structType)
	if structDef == nil {
		return false
	}
	// Need access to the checker's structMethods map, but this is a standalone function.
	// For now, check against the global method registry.
	// This will be called from canAssign which doesn't have checker context.
	// We need a different approach — store method info in the AST.
	// For the initial implementation, we'll check methods on the struct definition directly.
	for _, im := range ifaceDef.Methods {
		found := false
		for _, sm := range structDef.Methods {
			if sm.Name == im.Name {
				// Check param types match
				if len(sm.Params) != len(im.Params) {
					return false
				}
				for i, pt := range im.Params {
					if sm.Params[i].Type != pt {
						return false
					}
				}
				if sm.ReturnType != im.ReturnType {
					return false
				}
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// resolveNamedArgs reorders named arguments to match parameter order.
// After this, ArgNames is cleared and Args is in positional order.
func (c *Checker) resolveNamedArgs(e *ast.CallExpr, sig funcSig) error {
	if len(sig.ParamNames) == 0 {
		return c.errAt(e.Pos, "named arguments used but function has no parameter names")
	}

	newArgs := make([]ast.Expr, len(sig.Params))
	used := make([]bool, len(sig.Params))

	// First: place positional args (those with empty name)
	posIdx := 0
	for i, name := range e.ArgNames {
		if name == "" {
			if posIdx >= len(sig.Params) {
				return c.errAt(e.Pos, "too many positional arguments")
			}
			newArgs[posIdx] = e.Args[i]
			used[posIdx] = true
			posIdx++
		}
	}

	// Second: place named args
	for i, name := range e.ArgNames {
		if name == "" {
			continue
		}
		found := false
		for j, paramName := range sig.ParamNames {
			if paramName == name {
				if used[j] {
					return c.errAt(e.Pos, "duplicate argument for parameter '%s'", name)
				}
				newArgs[j] = e.Args[i]
				used[j] = true
				found = true
				break
			}
		}
		if !found {
			return c.errAt(e.Pos, "unknown parameter name '%s'", name)
		}
	}

	// Fill in defaults for unused params
	for i, isUsed := range used {
		if !isUsed {
			if sig.DefaultValues[i] != nil {
				newArgs[i] = sig.DefaultValues[i]
			} else {
				return c.errAt(e.Pos, "missing argument for required parameter '%s'", sig.ParamNames[i])
			}
		}
	}

	e.Args = newArgs
	e.ArgNames = nil
	return nil
}

// fillDefaultArgs appends default values for missing trailing arguments.
func (c *Checker) fillDefaultArgs(e *ast.CallExpr, sig funcSig) error {
	for i := len(e.Args); i < len(sig.Params); i++ {
		if i < len(sig.DefaultValues) && sig.DefaultValues[i] != nil {
			e.Args = append(e.Args, sig.DefaultValues[i])
		} else {
			return c.errAt(e.Pos, "missing argument for required parameter '%s'", sig.ParamNames[i])
		}
	}
	return nil
}

// collectReferencedVars walks statements and expressions to collect all referenced
// variable names, distinguishing between locally-defined and externally-referenced.
func collectReferencedVars(stmts []ast.Stmt, used, defined map[string]bool) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.LetStmt:
			collectReferencedVarsExpr(s.Value, used)
			defined[s.Name] = true
		case *ast.ExprStmt:
			collectReferencedVarsExpr(s.Expr, used)
		case *ast.ReturnStmt:
			if s.Value != nil {
				collectReferencedVarsExpr(s.Value, used)
			}
		case *ast.AssignStmt:
			collectReferencedVarsExpr(s.Value, used)
			used[s.Name] = true
		case *ast.IfStmt:
			collectReferencedVarsExpr(s.Cond, used)
			collectReferencedVars(s.Then, used, defined)
			collectReferencedVars(s.Else, used, defined)
		case *ast.WhileStmt:
			collectReferencedVarsExpr(s.Cond, used)
			collectReferencedVars(s.Body, used, defined)
		case *ast.ForStmt:
			collectReferencedVars([]ast.Stmt{s.Init}, used, defined)
			collectReferencedVarsExpr(s.Cond, used)
			collectReferencedVars([]ast.Stmt{s.Post}, used, defined)
			collectReferencedVars(s.Body, used, defined)
		case *ast.ForeachStmt:
			collectReferencedVarsExpr(s.Iterable, used)
			defined[s.ValueVar] = true
			if s.IndexVar != "" {
				defined[s.IndexVar] = true
			}
			collectReferencedVars(s.Body, used, defined)
		case *ast.BlockStmt:
			collectReferencedVars(s.Stmts, used, defined)
		case *ast.SendStmt:
			if s.Target != nil {
				collectReferencedVarsExpr(s.Target, used)
			}
			collectReferencedVarsExpr(s.Value, used)
		case *ast.IncrementStmt:
			used[s.Name] = true
		case *ast.DecrementStmt:
			used[s.Name] = true
		case *ast.CompoundAssignStmt:
			used[s.Name] = true
			collectReferencedVarsExpr(s.Value, used)
		case *ast.IndexAssignStmt:
			collectReferencedVarsExpr(s.Array, used)
			collectReferencedVarsExpr(s.Index, used)
			collectReferencedVarsExpr(s.Value, used)
		case *ast.FieldAssignStmt:
			collectReferencedVarsExpr(s.Object, used)
			collectReferencedVarsExpr(s.Value, used)
		case *ast.DestructureLetStmt:
			collectReferencedVarsExpr(s.Value, used)
			for _, name := range s.Names {
				defined[name] = true
			}
		case *ast.DeferStmt:
			collectReferencedVarsExpr(s.Expr, used)
		case *ast.ThrowStmt:
			collectReferencedVarsExpr(s.Value, used)
		case *ast.TryCatchStmt:
			collectReferencedVars(s.Body, used, defined)
			if s.CatchBody != nil {
				defined[s.CatchVar] = true
				collectReferencedVars(s.CatchBody, used, defined)
			}
			if s.FinallyBody != nil {
				collectReferencedVars(s.FinallyBody, used, defined)
			}
		case *ast.SwitchStmt:
			collectReferencedVarsExpr(s.Tag, used)
			for _, sc := range s.Cases {
				collectReferencedVars(sc.Body, used, defined)
			}
			if s.Default != nil {
				collectReferencedVars(s.Default, used, defined)
			}
		}
	}
}

func collectReferencedVarsExpr(expr ast.Expr, used map[string]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.Ident:
		used[e.Name] = true
	case *ast.BinaryExpr:
		collectReferencedVarsExpr(e.Left, used)
		collectReferencedVarsExpr(e.Right, used)
	case *ast.UnaryExpr:
		collectReferencedVarsExpr(e.Operand, used)
	case *ast.CallExpr:
		for _, arg := range e.Args {
			collectReferencedVarsExpr(arg, used)
		}
	case *ast.IndexExpr:
		collectReferencedVarsExpr(e.Array, used)
		collectReferencedVarsExpr(e.Index, used)
	case *ast.SliceExpr:
		collectReferencedVarsExpr(e.Array, used)
		if e.Start != nil {
			collectReferencedVarsExpr(e.Start, used)
		}
		if e.End != nil {
			collectReferencedVarsExpr(e.End, used)
		}
	case *ast.FieldAccessExpr:
		collectReferencedVarsExpr(e.Object, used)
	case *ast.ArrayLitExpr:
		for _, elem := range e.Elems {
			collectReferencedVarsExpr(elem, used)
		}
	case *ast.StructLitExpr:
		for _, v := range e.FieldValues {
			collectReferencedVarsExpr(v, used)
		}
	case *ast.SpawnExpr:
		if e.Body != nil {
			innerDefined := make(map[string]bool)
			collectReferencedVars(e.Body, used, innerDefined)
		}
		if e.Call != nil {
			collectReferencedVarsExpr(e.Call, used)
		}
	case *ast.ReceiveExpr:
		collectReferencedVarsExpr(e.Source, used)
	case *ast.StringInterpExpr:
		for _, part := range e.Parts {
			collectReferencedVarsExpr(part, used)
		}
	case *ast.MatchExpr:
		collectReferencedVarsExpr(e.Tag, used)
		for _, arm := range e.Arms {
			for _, pat := range arm.Patterns {
				collectReferencedVarsExpr(pat, used)
			}
			collectReferencedVarsExpr(arm.Body, used)
		}
	case *ast.LambdaExpr:
		innerDefined := make(map[string]bool)
		for _, p := range e.Params {
			innerDefined[p.Name] = true
		}
		collectReferencedVars(e.Body, used, innerDefined)
	}
}

func typeName(t ast.Type) string {
	switch t {
	case ast.TypeNull:
		return "null"
	case ast.TypeInt:
		return "int"
	case ast.TypeBool:
		return "bool"
	case ast.TypeString:
		return "string"
	case ast.TypeVoid:
		return "void"
	case ast.TypeLong:
		return "long"
	case ast.TypeDouble:
		return "double"
	case ast.TypeArrayInt:
		return "int[]"
	case ast.TypeArrayBool:
		return "bool[]"
	case ast.TypeArrayString:
		return "string[]"
	case ast.TypeArrayLong:
		return "long[]"
	case ast.TypeArrayDouble:
		return "double[]"
	case ast.TypeChar:
		return "char"
	case ast.TypeArrayChar:
		return "char[]"
	case ast.TypeStringBuilder:
		return "StringBuilder"
	case ast.TypeJsonValue:
		return "json.Value"
	case ast.TypeMutex:
		return "mutex"
	default:
		if ast.IsRefType(t) {
			return "&" + typeName(ast.RefInnerType(t))
		}
		if ast.IsOptionalType(t) {
			return typeName(ast.OptionalInnerType(t)) + "?"
		}
		if ast.IsStructArrayType(t) {
			elemType := ast.ElementType(t)
			if ast.IsEnumType(elemType) {
				return ast.EnumName(elemType) + "[]"
			}
			return ast.StructName(elemType) + "[]"
		}
		if ast.IsStructType(t) {
			return ast.StructName(t)
		}
		if ast.IsChanType(t) {
			return "chan " + typeName(ast.ChanElemType(t))
		}
		if ast.IsTaskType(t) {
			return "task " + typeName(ast.TaskReturnType(t))
		}
		if ast.IsFuncType(t) {
			params := ast.FuncTypeParams(t)
			ret := ast.FuncTypeReturn(t)
			s := "fn("
			for i, p := range params {
				if i > 0 {
					s += ", "
				}
				s += typeName(p)
			}
			s += "): " + typeName(ret)
			return s
		}
		if ast.IsWeakType(t) {
			return "weak<" + typeName(ast.WeakInnerType(t)) + ">"
		}
		if ast.IsEnumType(t) {
			return ast.EnumName(t)
		}
		if ast.IsMapType(t) {
			return "map[" + typeName(ast.MapKeyType(t)) + ", " + typeName(ast.MapValueType(t)) + "]"
		}
		if ast.IsInterfaceType(t) {
			return ast.InterfaceName(t)
		}
		return "unknown"
	}
}

// alwaysExits reports whether a statement list unconditionally leaves the
// enclosing block, so that code after it is only reachable when the branch was
// not taken. Used to narrow optionals after a guard clause.
func alwaysExits(stmts []ast.Stmt) bool {
	if len(stmts) == 0 {
		return false
	}
	switch last := stmts[len(stmts)-1].(type) {
	case *ast.ReturnStmt, *ast.ThrowStmt, *ast.BreakStmt, *ast.ContinueStmt:
		return true
	case *ast.IfStmt:
		// Both arms must exit for the whole statement to.
		return last.Else != nil && alwaysExits(last.Then) && alwaysExits(last.Else)
	}
	return false
}

// isJsonEncodable reports whether a value of type t can appear inside a
// json.Value literal. Everything with a JSON representation qualifies; channels,
// functions, mutexes and the like do not.
func isJsonEncodable(t ast.Type) bool {
	switch t {
	case ast.TypeInt, ast.TypeLong, ast.TypeDouble, ast.TypeBool,
		ast.TypeString, ast.TypeChar, ast.TypeJsonValue:
		return true
	case ast.TypeNull:
		// JSON has a null, so a null literal is a legitimate value here even
		// though it needs an optional type everywhere else in the language.
		return true
	}
	if ast.IsStructType(t) || ast.IsArrayType(t) || ast.IsStructArrayType(t) {
		return true
	}
	if ast.IsMapType(t) {
		return ast.MapKeyType(t) == ast.TypeString
	}
	return false
}

// markJsonValue records that a literal is being built as part of a json.Value,
// recursing so that nested literals are marked too. This is what lets
// `[4, id, {}]` treat its `{}` as an empty JSON object rather than a map, and
// what allows the outer array's elements to have differing types.
//
// Only literal nodes need marking: any other expression already has a type, and
// the runtime converts it at the point of construction.
func (c *Checker) markJsonValue(e ast.Expr) error {
	switch lit := e.(type) {
	case *ast.ArrayLitExpr:
		if lit.AsJsonValue {
			return nil
		}
		lit.AsJsonValue = true
		for _, elem := range lit.Elems {
			if err := c.markJsonValue(elem); err != nil {
				return err
			}
		}
	case *ast.MapLitExpr:
		lit.AsJsonValue = true
	case *ast.ObjectLitExpr:
		for _, v := range lit.Values {
			if err := c.markJsonValue(v); err != nil {
				return err
			}
		}
	}
	return nil
}

// jsonValueMethods is the full method surface of json.Value. Accessors are
// spelled asX rather than x because int, string, bool, long and double are
// keywords and could not follow a dot.
var jsonValueMethods = map[string]ast.Type{
	"len":      ast.TypeInt,
	"asInt":    ast.TypeInt,
	"asLong":   ast.TypeLong,
	"asDouble": ast.TypeDouble,
	"asString": ast.TypeString,
	"asBool":   ast.TypeBool,
	"isNull":   ast.TypeBool,
	"isBool":   ast.TypeBool,
	"isNumber": ast.TypeBool,
	"isString": ast.TypeBool,
	"isArray":  ast.TypeBool,
	"isObject": ast.TypeBool,
	"keys":     ast.TypeArrayString,
}

// checkJsonValueMethod type-checks a method call on a json.Value.
func (c *Checker) checkJsonValueMethod(method string, args []ast.Expr, pos ast.Pos) (ast.Type, error) {
	if method == "has" {
		if len(args) != 1 {
			return 0, c.errAt(pos, "json.Value.has() takes exactly 1 argument, got %d", len(args))
		}
		argType, err := c.checkExpr(args[0])
		if err != nil {
			return 0, err
		}
		if argType != ast.TypeString {
			return 0, c.errAt(pos, "json.Value.has() argument must be string, got %s", typeName(argType))
		}
		return ast.TypeBool, nil
	}
	ret, ok := jsonValueMethods[method]
	if !ok {
		return 0, c.errAt(pos, "json.Value has no method '%s'", method)
	}
	if len(args) != 0 {
		return 0, c.errAt(pos, "json.Value.%s() takes no arguments, got %d", method, len(args))
	}
	return ret, nil
}

// applyTargetType gives a literal that carries no type of its own the type it is
// being assigned to. `{}` is an empty map or an empty JSON object depending on
// the target, and `[]` needs its element type from somewhere; without this they
// can only appear on the right of an annotated `let`.
func (c *Checker) applyTargetType(expr ast.Expr, target ast.Type) error {
	// Calls whose return type comes from context — db.col reading a column,
	// json.decode choosing a shape — take it from the field just as they would
	// from a `let` annotation.
	if call, ok := expr.(*ast.CallExpr); ok {
		if call.Module == "db" && call.Name == "col" {
			call.ResolvedType = target
			return nil
		}
		if call.Module == "json" && call.Name == "decode" {
			call.ResolvedType = target
			return nil
		}
	}
	switch lit := expr.(type) {
	case *ast.MapLitExpr:
		if target == ast.TypeJsonValue {
			return c.markJsonValue(lit)
		}
		if ast.IsMapType(target) {
			lit.MapType = target
		}
	case *ast.ArrayLitExpr:
		if target == ast.TypeJsonValue {
			return c.markJsonValue(lit)
		}
		if ast.IsArrayType(target) || ast.IsStructArrayType(target) {
			lit.ElemType = ast.ElementType(target)
		}
	case *ast.ObjectLitExpr:
		return c.markJsonValue(lit)
	}
	return nil
}

// cReservedWords are C keywords and runtime-reserved prefixes that a Dex
// identifier cannot currently use. Dex compiles to C and emits user identifiers
// verbatim, so a collision produces a C syntax error pointing at generated code
// the author never wrote. Catching it here reports the real problem instead.
var cReservedWords = map[string]bool{
	"auto": true, "break": true, "case": true, "char": true, "const": true,
	"continue": true, "default": true, "do": true, "double": true, "else": true,
	"enum": true, "extern": true, "float": true, "for": true, "goto": true,
	"if": true, "inline": true, "int": true, "long": true, "register": true,
	"restrict": true, "return": true, "short": true, "signed": true,
	"sizeof": true, "static": true, "struct": true, "switch": true,
	"typedef": true, "union": true, "unsigned": true, "void": true,
	"volatile": true, "while": true, "bool": true, "complex": true,
	"imaginary": true, "asm": true, "typeof": true,
	// Names the generated program defines for itself.
	"main": true, "printf": true, "malloc": true, "free": true, "realloc": true,
	"calloc": true, "strlen": true, "strcmp": true, "memcpy": true, "NULL": true,
}

// checkIdentifierName rejects a declared name that would collide with C.
func (c *Checker) checkIdentifierName(name string, kind string, pos ast.Pos) error {
	if strings.HasPrefix(name, "dex_") || strings.HasPrefix(name, "Dex_") {
		return c.errAt(pos, "%s name '%s' is reserved: names beginning with 'dex_' or 'Dex_' belong to the runtime", kind, name)
	}
	if cReservedWords[name] {
		return c.errAt(pos, "%s name '%s' is reserved and cannot be used as an identifier", kind, name)
	}
	return nil
}
