package checker

import (
	"strings"

	"github.com/ensamuel7/dex/ast"
)

// checkInterfaceMethodCall resolves instance.method() against an interface's
// declared signatures. Unlike a struct method call there is nothing to fall
// through to: the receiver's type names the only methods it can have.
func (c *Checker) checkInterfaceMethodCall(e *ast.CallExpr, ifaceType ast.Type, display string) (ast.Type, error) {
	def := ast.GetInterfaceDef(ifaceType)
	if def == nil {
		return 0, c.errAt(e.Pos, "unknown interface type (internal error)")
	}

	var sig *ast.InterfaceMethod
	for i := range def.Methods {
		if def.Methods[i].Name == e.Name {
			sig = &def.Methods[i]
			break
		}
	}
	if sig == nil {
		names := make([]string, len(def.Methods))
		for i, m := range def.Methods {
			names[i] = m.Name + "()"
		}
		if len(names) == 0 {
			return 0, c.errAt(e.Pos, "interface %s declares no methods, so %s.%s() has nothing to call",
				def.Name, display, e.Name)
		}
		return 0, c.errAt(e.Pos, "interface %s has no method '%s' — it declares %s",
			def.Name, e.Name, strings.Join(names, ", "))
	}

	e.IsInterfaceCall = true
	e.StructType = ifaceType

	if len(e.Args) != len(sig.Params) {
		return 0, c.errAt(e.Pos, "%s.%s() takes exactly %d argument(s), got %d",
			display, e.Name, len(sig.Params), len(e.Args))
	}
	for i, arg := range e.Args {
		argType, err := c.checkExpr(arg)
		if err != nil {
			return 0, err
		}
		if argType != sig.Params[i] && !canAssign(sig.Params[i], argType, arg) {
			return 0, c.errAt(e.Pos, "%s.%s() argument %d must be %s, got %s",
				display, e.Name, i+1, typeName(sig.Params[i]), typeName(argType))
		}
	}
	return sig.ReturnType, nil
}

// missingInterfaceMethods names what a struct would need to satisfy an
// interface, so the error says what to write rather than only that it does not
// fit. A method present under the right name but the wrong signature is
// reported as a mismatch, which is the case a bare "missing" would mislead on.
func missingInterfaceMethods(structType, ifaceType ast.Type) []string {
	ifaceDef := ast.GetInterfaceDef(ifaceType)
	structDef := ast.GetStructDef(structType)
	if ifaceDef == nil || structDef == nil {
		return nil
	}

	var problems []string
	for _, im := range ifaceDef.Methods {
		var found *ast.Function
		for i := range structDef.Methods {
			if structDef.Methods[i].Name == im.Name {
				found = &structDef.Methods[i]
				break
			}
		}
		if found == nil {
			problems = append(problems, "missing "+interfaceMethodSig(im))
			continue
		}
		if !methodMatchesSignature(found, im) {
			problems = append(problems, found.Name+"() has the wrong signature, needs "+interfaceMethodSig(im))
		}
	}
	return problems
}

func methodMatchesSignature(m *ast.Function, im ast.InterfaceMethod) bool {
	if len(m.Params) != len(im.Params) || m.ReturnType != im.ReturnType {
		return false
	}
	for i, pt := range im.Params {
		if m.Params[i].Type != pt {
			return false
		}
	}
	return true
}

func interfaceMethodSig(im ast.InterfaceMethod) string {
	var b strings.Builder
	b.WriteString("fn ")
	b.WriteString(im.Name)
	b.WriteString("(")
	for i, pt := range im.Params {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(typeName(pt))
	}
	b.WriteString("): ")
	b.WriteString(typeName(im.ReturnType))
	return b.String()
}

// explainMismatch adds the reason when the target is an interface a struct
// nearly satisfies. "expected Stringer, got Empty" says the assignment failed;
// what a reader needs is which method is missing or which signature is wrong.
func explainMismatch(target, got ast.Type) string {
	if !ast.IsInterfaceType(target) || !ast.IsStructType(got) {
		return ""
	}
	problems := missingInterfaceMethods(got, target)
	if len(problems) == 0 {
		return ""
	}
	return " (" + strings.Join(problems, "; ") + ")"
}
