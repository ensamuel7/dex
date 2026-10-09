package checker

import (
	"github.com/ensamuel7/dex/ast"
)

// checkStructMethodCall resolves instance.method() against a struct's declared
// methods. The second result reports whether the struct has such a method at
// all, so a caller can fall through to its other options.
//
// Shared by the named-receiver path and the expression-receiver path, which
// differ only in what they can put in an error message: one has a variable name,
// the other an arbitrary expression.
func (c *Checker) checkStructMethodCall(e *ast.CallExpr, structType ast.Type, display string) (ast.Type, bool, error) {
	def := ast.GetStructDef(structType)
	if def == nil {
		return 0, false, nil
	}
	methods, ok := c.structMethods[def.Name]
	if !ok {
		return 0, false, nil
	}
	sig, ok := methods[e.Name]
	if !ok {
		return 0, false, nil
	}
	e.IsMethodCall = true
	e.StructType = structType
	if len(e.Args) != len(sig.Params) {
		return 0, true, c.errAt(e.Pos, "%s.%s() takes exactly %d argument(s), got %d", display, e.Name, len(sig.Params), len(e.Args))
	}
	for i, arg := range e.Args {
		argType, err := c.checkExpr(arg)
		if err != nil {
			return 0, true, err
		}
		if argType != sig.Params[i] && !canAssign(sig.Params[i], argType, e.Args[i]) {
			return 0, true, c.errAt(e.Pos, "%s.%s() argument %d must be %s, got %s", display, e.Name, i+1, typeName(sig.Params[i]), typeName(argType))
		}
	}
	return sig.ReturnType, true, nil
}
