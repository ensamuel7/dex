package checker

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

// Generic functions are monomorphised here rather than in the parser, because
// instantiating one needs the argument types and the parser has none: at
// `max(1, 2)` only the checker knows that `1` is an int.
//
// The parser owns the template — it captured the tokens and can re-parse them
// with the type parameters bound — so the two meet at the Instantiator
// interface. The checker infers the type arguments, asks for the instantiation,
// registers its signature, appends it to the program, and rewrites the call to
// name it. The second pass over program.Functions is index-based so an appended
// function is reached and checked like any other.

// Instantiator re-parses a generic template with its type parameters bound. The
// parser implements it; nothing else needs to.
type Instantiator interface {
	InstantiateFunc(name string, args []ast.Type) (*ast.Function, []ast.StructDef, error)
}

// SetGenerics gives the checker the generic function templates and the
// instantiator that can realise them. Called by the driver between parse and
// check; a program with no generics never calls it and pays nothing.
func (c *Checker) SetGenerics(decls []ast.GenericDecl, inst Instantiator) {
	if len(decls) == 0 {
		return
	}
	c.genericFuncs = make(map[string]*ast.GenericDecl, len(decls))
	for i := range decls {
		c.genericFuncs[decls[i].Name] = &decls[i]
	}
	c.instantiator = inst
	c.instantiated = make(map[string]bool)
}

// isGenericFunc reports whether a bare name refers to a generic template.
func (c *Checker) isGenericFunc(name string) bool {
	if c.genericFuncs == nil {
		return false
	}
	_, ok := c.genericFuncs[name]
	return ok
}

// checkGenericCall resolves a call to a generic function: infer the type
// arguments from the call, instantiate, and rewrite the call to name the
// instantiation. The return value is the instantiation's return type.
func (c *Checker) checkGenericCall(e *ast.CallExpr) (ast.Type, error) {
	decl := c.genericFuncs[e.Name]
	if len(e.Args) != len(decl.Params) {
		return 0, c.errAt(e.Pos, "%s() takes exactly %d argument(s), got %d",
			decl.Name, len(decl.Params), len(e.Args))
	}

	argTypes := make([]ast.Type, len(e.Args))
	for i, arg := range e.Args {
		t, err := c.checkExpr(arg)
		if err != nil {
			return 0, err
		}
		argTypes[i] = t
	}

	binding := make(map[string]ast.Type, len(decl.TypeParams))
	for i, p := range decl.Params {
		if err := c.unifyTypeParam(e, decl, p.Type, argTypes[i], i, binding); err != nil {
			return 0, err
		}
	}

	args := make([]ast.Type, len(decl.TypeParams))
	for i, tp := range decl.TypeParams {
		bound, ok := binding[tp]
		if !ok {
			// Nothing in the parameter list mentions it, so no argument can
			// pin it down. Saying which one is the difference between a fixable
			// message and a puzzle.
			return 0, c.errAt(e.Pos,
				"cannot infer type parameter %s of %s() from the arguments; it appears in no parameter's type",
				tp, decl.Name)
		}
		args[i] = bound
	}

	flat, err := c.instantiateFunc(e.Pos, decl, args)
	if err != nil {
		return 0, err
	}

	// From here the call is an ordinary call to a concrete function.
	e.Name = flat
	sig, ok := c.funcs[flat]
	if !ok {
		return 0, c.errAt(e.Pos, "internal error: instantiating %s did not register %s", decl.Name, flat)
	}
	for i, arg := range e.Args {
		if argTypes[i] != sig.Params[i] && !canAssign(sig.Params[i], argTypes[i], arg) {
			return 0, c.errAt(e.Pos, "%s() argument %d must be %s, got %s",
				flat, i+1, typeName(sig.Params[i]), typeName(argTypes[i]))
		}
	}
	return sig.ReturnType, nil
}

// unifyTypeParam matches a parameter's declared type against the argument's,
// binding any type parameter it meets. It descends through arrays and
// optionals, so `fn first<T>(xs: T[]): T` infers T from an int[].
func (c *Checker) unifyTypeParam(e *ast.CallExpr, decl *ast.GenericDecl, paramType, argType ast.Type, idx int, binding map[string]ast.Type) error {
	switch {
	case ast.IsTypeParamType(paramType):
		name := ast.TypeParamName(paramType)
		if bound, ok := binding[name]; ok {
			if bound != argType && !canAssign(bound, argType, e.Args[idx]) {
				return c.errAt(e.Pos,
					"%s() binds %s to %s from an earlier argument, but argument %d is %s",
					decl.Name, name, typeName(bound), idx+1, typeName(argType))
			}
			return nil
		}
		if argType == ast.TypeVoid || argType == ast.TypeNull {
			return c.errAt(e.Pos,
				"cannot infer type parameter %s of %s() from argument %d, which is %s",
				name, decl.Name, idx+1, typeName(argType))
		}
		binding[name] = argType
		return nil

	case ast.IsArrayType(paramType) && ast.IsArrayType(argType):
		return c.unifyTypeParam(e, decl, ast.ElementType(paramType), ast.ElementType(argType), idx, binding)

	case ast.IsOptionalType(paramType) && ast.IsOptionalType(argType):
		return c.unifyTypeParam(e, decl, ast.OptionalInnerType(paramType), ast.OptionalInnerType(argType), idx, binding)

	default:
		// A concrete parameter type: nothing to infer, and the argument check
		// after instantiation reports a mismatch.
		return nil
	}
}

// instantiateFunc realises one instantiation, once. The flat name is the
// identity: a second call with the same type arguments reuses the function
// already built, which is what keeps `max(1,2)` and `max(3,4)` one function.
func (c *Checker) instantiateFunc(pos ast.Pos, decl *ast.GenericDecl, args []ast.Type) (string, error) {
	flat := genericFlatName(decl.Name, args)
	if c.instantiated[flat] {
		return flat, nil
	}
	if c.instantiator == nil {
		return "", c.errAt(pos, "internal error: no instantiator for generic '%s'", decl.Name)
	}

	fn, newStructs, err := c.instantiator.InstantiateFunc(decl.Name, args)
	if err != nil {
		return "", c.errAt(pos, "instantiating %s<%s>: %v", decl.Name, typeArgList(args), err)
	}
	// Marked before the body is checked: a recursive generic calls itself with
	// the same type arguments, and without this that recursion never bottoms
	// out — it would instantiate forever.
	c.instantiated[flat] = true

	c.registerFuncSig(*fn)
	c.pending = append(c.pending, *fn)
	c.pendingStructs = append(c.pendingStructs, newStructs...)
	return flat, nil
}

// genericFlatName mirrors the parser's mangling. The two have to agree, because
// one names the function and the other looks it up.
func genericFlatName(name string, args []ast.Type) string {
	var b strings.Builder
	b.WriteString(name)
	for _, a := range args {
		b.WriteString("_")
		b.WriteString(typeMangleForName(a))
	}
	return b.String()
}

func typeMangleForName(t ast.Type) string {
	switch t {
	case ast.TypeInt:
		return "int"
	case ast.TypeBool:
		return "bool"
	case ast.TypeString:
		return "string"
	case ast.TypeLong:
		return "long"
	case ast.TypeDouble:
		return "double"
	case ast.TypeChar:
		return "char"
	case ast.TypeVoid:
		return "void"
	}
	switch {
	case ast.IsArrayType(t):
		return typeMangleForName(ast.ElementType(t)) + "arr"
	case ast.IsOptionalType(t):
		return typeMangleForName(ast.OptionalInnerType(t)) + "opt"
	case ast.IsStructType(t):
		return ast.StructName(t)
	case ast.IsEnumType(t):
		return ast.EnumName(t)
	case ast.IsInterfaceType(t):
		return ast.InterfaceName(t)
	}
	return "t"
}

func typeArgList(args []ast.Type) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = typeName(a)
	}
	return strings.Join(parts, ", ")
}

// registerFuncSig records a function's signature, the same way the first pass
// does for a function that was written out.
func (c *Checker) registerFuncSig(fn ast.Function) {
	var paramTypes []ast.Type
	var paramNames []string
	var defaultValues []ast.Expr
	for _, p := range fn.Params {
		paramTypes = append(paramTypes, p.Type)
		paramNames = append(paramNames, p.Name)
		defaultValues = append(defaultValues, p.DefaultValue)
	}
	c.funcs[fn.Name] = funcSig{
		Params:        paramTypes,
		ParamNames:    paramNames,
		DefaultValues: defaultValues,
		ReturnType:    fn.ReturnType,
		IsPrivate:     fn.IsPrivate,
	}
}

// A type parameter must never reach codegen: it has no C representation, so one
// that survived is a hole in instantiation rather than a program error. This is
// the net that says so plainly instead of emitting `void`.
func (c *Checker) rejectTypeParams(fn ast.Function) {
	for _, p := range fn.Params {
		if ast.IsTypeParamType(p.Type) {
			c.addError(fmt.Errorf("internal error: parameter '%s' of '%s' kept the type parameter %s after instantiation",
				p.Name, fn.Name, ast.TypeParamName(p.Type)))
		}
	}
	if ast.IsTypeParamType(fn.ReturnType) {
		c.addError(fmt.Errorf("internal error: the return type of '%s' kept the type parameter %s after instantiation",
			fn.Name, ast.TypeParamName(fn.ReturnType)))
	}
}

// registerStructDef records a struct's constructor and method signatures. The
// struct-validation pass does this for every struct written out; an
// instantiation arrives after that pass has run, so it registers here.
func (c *Checker) registerStructDef(sd ast.StructDef) {
	if len(sd.ConstructorParams) > 0 {
		var paramTypes []ast.Type
		for _, cp := range sd.ConstructorParams {
			paramTypes = append(paramTypes, cp.Type)
		}
		c.structConstructors[sd.Name] = paramTypes
	}
	if len(sd.Methods) == 0 {
		return
	}
	methods := make(map[string]funcSig, len(sd.Methods))
	for _, m := range sd.Methods {
		var mParamTypes []ast.Type
		var mParamNames []string
		var mDefaults []ast.Expr
		for _, p := range m.Params {
			mParamTypes = append(mParamTypes, p.Type)
			mParamNames = append(mParamNames, p.Name)
			mDefaults = append(mDefaults, p.DefaultValue)
		}
		methods[m.Name] = funcSig{
			Params:        mParamTypes,
			ParamNames:    mParamNames,
			DefaultValues: mDefaults,
			ReturnType:    m.ReturnType,
			IsPrivate:     m.IsPrivate,
		}
	}
	c.structMethods[sd.Name] = methods
}
